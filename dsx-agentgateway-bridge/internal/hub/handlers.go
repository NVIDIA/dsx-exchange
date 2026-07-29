// Copyright 2026 NVIDIA Corporation
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//
// SPDX-License-Identifier: Apache-2.0

package hub

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"

	mcptransport "github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"

	bridgemcp "github.com/NVIDIA/dsx-exchange/dsx-agentgateway-bridge/internal/mcp"
	"github.com/NVIDIA/dsx-exchange/dsx-agentgateway-bridge/internal/shardbus"
)

type listShardsResult struct {
	Shards []string `json:"shards"`
}

func bridgeListShardsTool() mcp.Tool {
	return mcp.NewTool(
		BridgeListShardsToolName,
		mcp.WithDescription("List currently reachable DSX bridge shards."),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
		mcp.WithSchemaAdditionalProperties(false),
	)
}

func (h hub) handleMCP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.writeResponse(w, http.StatusMethodNotAllowed, "text/plain; charset=utf-8", []byte("method not allowed\n"))
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, bridgemcp.MaxMessageBytes))
	if err != nil {
		h.writeRPCParseError(w)
		return
	}
	req, err := bridgemcp.DecodeRequest(body)
	if err != nil {
		h.writeRPCParseError(w)
		return
	}
	switch req.Method {
	case string(mcp.MethodInitialize):
		h.writeRPCResult(w, req.ID, mcp.NewInitializeResult(
			mcp.LATEST_PROTOCOL_VERSION,
			mcp.ServerCapabilities{
				Tools: &struct {
					ListChanged bool `json:"listChanged,omitempty"`
				}{},
				Prompts: &struct {
					ListChanged bool `json:"listChanged,omitempty"`
				}{},
				Completions: &struct{}{},
			},
			mcp.Implementation{Name: "dsx-agentgateway-bridge-hub", Version: "0.1.0"},
			"",
		))
	case string(mcp.MethodNotificationInitialized):
		h.writeResponse(w, http.StatusAccepted, "", nil)
	case string(mcp.MethodToolsList):
		h.handleToolsList(w, r, req)
	case string(mcp.MethodToolsCall):
		h.handleToolsCall(w, r, req)
	case string(mcp.MethodPromptsList):
		h.handlePromptsList(w, r, req)
	case string(mcp.MethodPromptsGet):
		h.handlePromptsGet(w, r, req)
	case string(mcp.MethodCompletionComplete):
		h.handleCompletionComplete(w, r, req)
	case string(mcp.MethodPing):
		h.writeRPCResult(w, req.ID, map[string]any{})
	case string(mcp.MethodResourcesList), string(mcp.MethodResourcesTemplatesList), string(mcp.MethodResourcesRead):
		h.writeRPCError(w, req.ID, mcp.METHOD_NOT_FOUND, req.Method+" is not supported by the stateless bridge")
	case "resources/subscribe", "resources/unsubscribe", string(mcp.MethodSetLogLevel),
		string(mcp.MethodListRoots), string(mcp.MethodSamplingCreateMessage), string(mcp.MethodElicitationCreate),
		string(mcp.MethodTasksGet), string(mcp.MethodTasksList), string(mcp.MethodTasksResult), string(mcp.MethodTasksCancel):
		// These methods require server-side session or client-side state. The
		// bridge only supports stateless request forwarding across NATS.
		h.writeRPCError(w, req.ID, mcp.METHOD_NOT_FOUND, req.Method+" requires MCP session or client-side state and is not supported by the stateless bridge")
	case string(mcp.MethodNotificationRootsListChanged), string(mcp.MethodNotificationResourcesListChanged), string(mcp.MethodNotificationPromptsListChanged), string(mcp.MethodNotificationToolsListChanged), string(mcp.MethodNotificationTasksStatus):
		h.writeResponse(w, http.StatusAccepted, "", nil)
	default:
		h.writeRPCError(w, req.ID, mcp.METHOD_NOT_FOUND, "method not found: "+req.Method)
	}
}

func (h hub) handleToolsList(w http.ResponseWriter, r *http.Request, req mcptransport.JSONRPCRequest) {
	var params mcp.PaginatedParams
	if err := bridgemcp.DecodeParams(req.Params, &params); err != nil {
		h.writeRPCError(w, req.ID, mcp.INVALID_PARAMS, err.Error())
		return
	}
	resp, ok := h.requestListResponse(w, r, req)
	if !ok {
		return
	}
	if resp.Error != nil {
		h.writeRPC(w, resp)
		return
	}
	var listed mcp.ListToolsResult
	if err := json.Unmarshal(resp.Result, &listed); err != nil {
		slog.Error("bridge tools/list response decode failed", "error", err)
		h.writeRPCError(w, req.ID, mcp.INTERNAL_ERROR, "invalid leaf response")
		return
	}
	// The bridge's own shard-discovery tool is local to the hub. Leaf tools are
	// augmented below so invocation can be routed back to the selected shard.
	listed.Tools = toolsWithShardID(listed.Tools)
	// The cursor belongs to the leaf catalog. Add the local tool only to the
	// first page so the hub does not change the leaf's page boundaries.
	if params.Cursor == "" {
		listed.Tools = append([]mcp.Tool{bridgeListShardsTool()}, listed.Tools...)
	}
	h.writeRPCResult(w, req.ID, listed)
}

func (h hub) handleToolsCall(w http.ResponseWriter, r *http.Request, req mcptransport.JSONRPCRequest) {
	params, err := bridgemcp.ParseCallParams(req.Params)
	if err != nil {
		h.writeRPCError(w, req.ID, mcp.INVALID_PARAMS, err.Error())
		return
	}
	if params.Name == BridgeListShardsToolName {
		h.handleListShardsCall(w, r, req)
		return
	}
	args, err := callArgumentsMap(params.Arguments)
	if err != nil {
		h.writeRPCError(w, req.ID, mcp.INVALID_PARAMS, err.Error())
		return
	}
	shardID, err := consumeShardID(args)
	if err != nil {
		h.writeRPCError(w, req.ID, mcp.INVALID_PARAMS, err.Error())
		return
	}
	// shard_id is routing metadata for the hub, not part of the leaf tool's API.
	params.Arguments = nilIfEmpty(args)
	req.Params = params
	h.forwardShardRPC(w, r, req, shardID)
}

func (h hub) handleListShardsCall(w http.ResponseWriter, r *http.Request, req mcptransport.JSONRPCRequest) {
	// Do not rediscover inline here. This tool reports the completed discovery
	// cache so callers cannot turn list_shards into a slow control-plane probe.
	shards, ready, err := h.shardCache.snapshot()
	if !ready || err != nil {
		if err != nil {
			slog.Error("bridge shard discovery unavailable", "error", err)
		}
		h.writeRPCError(w, req.ID, bridgemcp.CodeBridgeUnavailable, "bridge shard discovery unavailable")
		return
	}
	result, err := mcp.NewToolResultJSON(listShardsResult{Shards: shards})
	if err != nil {
		h.writeRPCError(w, req.ID, mcp.INTERNAL_ERROR, "failed to encode shard list")
		return
	}
	h.writeRPCResult(w, req.ID, result)
}

func (h hub) handlePromptsList(w http.ResponseWriter, r *http.Request, req mcptransport.JSONRPCRequest) {
	resp, ok := h.requestListResponse(w, r, req)
	if !ok {
		return
	}
	if resp.Error != nil {
		h.writeRPC(w, resp)
		return
	}
	var listed mcp.ListPromptsResult
	if err := json.Unmarshal(resp.Result, &listed); err != nil {
		slog.Error("bridge prompts/list response decode failed", "error", err)
		h.writeRPCError(w, req.ID, mcp.INTERNAL_ERROR, "invalid leaf response")
		return
	}
	listed.Prompts = promptsWithShardID(listed.Prompts)
	h.writeRPCResult(w, req.ID, listed)
}

func (h hub) handlePromptsGet(w http.ResponseWriter, r *http.Request, req mcptransport.JSONRPCRequest) {
	params, err := parsePromptGetParams(req.Params)
	if err != nil {
		h.writeRPCError(w, req.ID, mcp.INVALID_PARAMS, err.Error())
		return
	}
	shardID, err := consumeShardIDFromStrings(params.Arguments)
	if err != nil {
		h.writeRPCError(w, req.ID, mcp.INVALID_PARAMS, err.Error())
		return
	}
	params.Arguments = nilIfEmpty(params.Arguments)
	req.Params = params
	h.forwardShardRPC(w, r, req, shardID)
}

func (h hub) handleCompletionComplete(w http.ResponseWriter, r *http.Request, req mcptransport.JSONRPCRequest) {
	params, err := parseCompletionParams(req.Params)
	if err != nil {
		h.writeRPCError(w, req.ID, mcp.INVALID_PARAMS, err.Error())
		return
	}
	// Prompt completions carry shard_id in context arguments.
	switch params.Ref.(type) {
	case mcp.PromptReference:
		shardID, err := consumeShardIDFromStrings(params.Context.Arguments)
		if err != nil {
			h.writeRPCError(w, req.ID, mcp.INVALID_PARAMS, err.Error())
			return
		}
		params.Context.Arguments = nilIfEmpty(params.Context.Arguments)
		req.Params = params
		h.forwardShardRPC(w, r, req, shardID)
	case mcp.ResourceReference:
		h.writeRPCError(w, req.ID, mcp.INVALID_PARAMS, "resource completion is not supported by the stateless bridge")
	default:
		h.writeRPCError(w, req.ID, mcp.INVALID_PARAMS, "unsupported completion ref")
	}
}

func (h hub) requestListResponse(w http.ResponseWriter, r *http.Request, req mcptransport.JSONRPCRequest) (mcptransport.JSONRPCResponse, bool) {
	body, ok := h.encodeBridgeRequest(w, req)
	if !ok {
		return mcptransport.JSONRPCResponse{}, false
	}
	subject := shardbus.ListSubject(h.subjectPrefix)
	msg, err := h.requestLeafOnce(r, subject, body)
	if err != nil {
		// Global list has no selected shard. If no leaf answers, the bridge has
		// no leaf catalog entries for this list call.
		if r.Context().Err() == nil && isNoReplyError(err) {
			if resp, ok := emptyListResponse(req); ok {
				return resp, true
			}
		}
		slog.Error("bridge leaf request failed", "method", req.Method, "subject", subject, "error", err)
		h.writeRPCError(w, req.ID, bridgemcp.CodeBridgeUnavailable, "bridge list queue unavailable")
		return mcptransport.JSONRPCResponse{}, false
	}
	var resp mcptransport.JSONRPCResponse
	if err := json.Unmarshal(msg.Data, &resp); err != nil {
		slog.Error("bridge leaf response decode failed", "method", req.Method, "subject", subject, "error", err)
		h.writeRPCError(w, req.ID, mcp.INTERNAL_ERROR, "invalid leaf response")
		return mcptransport.JSONRPCResponse{}, false
	}
	return resp, true
}

func (h hub) forwardShardRPC(w http.ResponseWriter, r *http.Request, req mcptransport.JSONRPCRequest, shardID string) {
	subject := shardbus.MCPSubject(h.subjectPrefix, shardID)
	// Directed invocation is different from global list: the caller selected a
	// shard-scoped object, so no answer from that shard is a real RPC failure.
	body, ok := h.encodeBridgeRequest(w, req)
	if !ok {
		return
	}
	responseStarted, err := h.forwardLeafRequest(w, r, subject, body)
	if err == nil {
		return
	}
	slog.Error("bridge leaf stream failed", "method", req.Method, "subject", subject, "shard_id", shardID, "error", err)
	if !responseStarted {
		if errors.Is(err, errInvalidLeafResponse) {
			h.writeRPCError(w, req.ID, mcp.INTERNAL_ERROR, "invalid leaf response")
		} else {
			h.writeRPCError(w, req.ID, bridgemcp.CodeBridgeUnavailable, "bridge shard unavailable")
		}
	}
}

func (h hub) encodeBridgeRequest(w http.ResponseWriter, req mcptransport.JSONRPCRequest) ([]byte, bool) {
	body, err := json.Marshal(req)
	if err != nil {
		h.writeRPCError(w, req.ID, mcp.INTERNAL_ERROR, "failed to encode bridged request")
		return nil, false
	}
	return body, true
}

func emptyListResponse(req mcptransport.JSONRPCRequest) (mcptransport.JSONRPCResponse, bool) {
	var result any
	switch req.Method {
	case string(mcp.MethodToolsList):
		result = mcp.ListToolsResult{}
	case string(mcp.MethodPromptsList):
		result = mcp.ListPromptsResult{}
	default:
		return mcptransport.JSONRPCResponse{}, false
	}
	// mcptransport responses store Result as raw JSON. Encode the typed empty
	// result before constructing the synthetic response.
	raw, err := json.Marshal(result)
	if err != nil {
		return mcptransport.JSONRPCResponse{}, false
	}
	return *mcptransport.NewJSONRPCResultResponse(req.ID, raw), true
}
