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

package mcp

import (
	"encoding/json"
	"errors"
	"fmt"

	mcptransport "github.com/mark3labs/mcp-go/client/transport"
	mcplib "github.com/mark3labs/mcp-go/mcp"
)

// JSON-RPC reserves -32000 through -32099 for implementation-defined server
// errors. These codes are emitted by the bridge itself, not by upstream MCP
// servers.
const (
	CodeLeafForwardFailed = -32003
	CodeBridgeUnavailable = -32004
)

func DecodeRequest(body []byte) (mcptransport.JSONRPCRequest, error) {
	var req mcptransport.JSONRPCRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return req, err
	}
	if req.JSONRPC != mcplib.JSONRPC_VERSION {
		return req, fmt.Errorf("invalid jsonrpc version %q", req.JSONRPC)
	}
	if req.Method == "" {
		return req, errors.New("missing method")
	}
	return req, nil
}

func ParseCallParams(raw any) (mcplib.CallToolParams, error) {
	var params mcplib.CallToolParams
	if raw == nil {
		return params, errors.New("missing params")
	}
	if err := DecodeParams(raw, &params); err != nil {
		return params, fmt.Errorf("invalid params: %w", err)
	}
	if params.Name == "" {
		return params, errors.New("missing tool name")
	}
	if params.Arguments == nil {
		params.Arguments = map[string]any{}
	}
	return params, nil
}

func DecodeParams(raw any, into any) error {
	switch typed := raw.(type) {
	case json.RawMessage:
		if len(typed) == 0 {
			return errors.New("missing params")
		}
		return json.Unmarshal(typed, into)
	default:
		// Some callers pass already-decoded maps. Re-marshal so every path still
		// goes through the target struct's JSON tags.
		data, err := json.Marshal(typed)
		if err != nil {
			return err
		}
		return json.Unmarshal(data, into)
	}
}
