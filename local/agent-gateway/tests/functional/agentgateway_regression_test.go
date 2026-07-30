// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package functional

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/NVIDIA/dsx-exchange/local/agent-gateway/tests/functional/internal/runner"
	"github.com/mark3labs/mcp-go/mcp"
)

func TestLegacySSEErrorDoesNotBlockPromptDiscovery(t *testing.T) {
	runner.ParallelReadOnly(t)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	s := runner.NewSession(t, tenantAUnlimited)
	t.Cleanup(s.Close)

	if _, err := s.Client.ListPrompts(ctx, mcp.ListPromptsRequest{}); err != nil {
		t.Fatalf("prompts/list with unsupported legacy SSE upstream: %v", err)
	}
}

func TestLegacyInitializedRequestKeepsInternalError(t *testing.T) {
	runner.ParallelReadOnly(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	token, sid := initSession(t, ctx, tenantAUnlimited)

	resp := postSessionMCP(t, ctx, token, sid, []byte(
		`{"jsonrpc":"2.0","id":"invalid-initialized-request","method":"notifications/initialized"}`,
	))
	code, _, ok := runner.JSONRPCError(resp.Body)
	// Agentgateway preserves the pre-1.4 error contract for session-based clients.
	if resp.Status != http.StatusInternalServerError || !ok || code != -32603 {
		t.Fatalf("legacy initialized request = HTTP %d JSON-RPC %d (body: %s), want 500/-32603", resp.Status, code, resp.Body)
	}

	requireMCPListSuccess(t, postSessionMCP(t, ctx, token, sid, []byte(
		`{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`,
	)))
}

func TestLegacyUnknownTargetRequestsKeepInternalErrors(t *testing.T) {
	runner.ParallelReadOnly(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	token, sid := initSession(t, ctx, tenantAUnlimited)

	for name, body := range map[string]string{
		"tool":     `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"does-not-exist","arguments":{}}}`,
		"resource": `{"jsonrpc":"2.0","id":4,"method":"resources/read","params":{"uri":"does-not-exist+dsx://fixture/static"}}`,
	} {
		t.Run(name, func(t *testing.T) {
			resp := postSessionMCP(t, ctx, token, sid, []byte(body))
			code, _, ok := runner.JSONRPCError(resp.Body)
			// v1.4.x reclassifies stateless requests only; legacy sessions retain 500/-32603.
			if resp.Status != http.StatusInternalServerError || !ok || code != -32603 {
				t.Fatalf("legacy unknown %s = HTTP %d JSON-RPC %d (body: %s), want 500/-32603", name, resp.Status, code, resp.Body)
			}
		})
	}
}

func TestModernStatelessClientDiscoversTools(t *testing.T) {
	runner.ParallelReadOnly(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	token := runner.MintToken(t, tenantAUnlimited)

	resp := postModernMCP(t, ctx, token, 1, "tools/list", "", map[string]any{})
	requireMCPListSuccess(t, resp)
	if sid := resp.Header.Get("Mcp-Session-Id"); sid != "" {
		t.Fatalf("modern tools/list returned legacy session ID %q", sid)
	}
}

func TestModernInitializedRequestReturnsMethodNotFound(t *testing.T) {
	runner.ParallelReadOnly(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	token := runner.MintToken(t, tenantAUnlimited)

	resp := postModernMCP(t, ctx, token, 2, "notifications/initialized", "", map[string]any{})
	code, _, ok := runner.JSONRPCError(resp.Body)
	if resp.Status != http.StatusNotFound || !ok || code != -32601 {
		t.Fatalf("modern initialized request = HTTP %d JSON-RPC %d (body: %s), want 404/-32601", resp.Status, code, resp.Body)
	}
}

func TestModernUnknownTargetRequestsReturnClientErrors(t *testing.T) {
	runner.ParallelReadOnly(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	token := runner.MintToken(t, tenantAUnlimited)

	for name, tc := range map[string]struct {
		method string
		target string
		params map[string]any
	}{
		"tool": {
			method: "tools/call",
			target: "does-not-exist",
			params: map[string]any{"name": "does-not-exist", "arguments": map[string]any{}},
		},
		"resource": {
			method: "resources/read",
			target: "does-not-exist+dsx://fixture/static",
			params: map[string]any{"uri": "does-not-exist+dsx://fixture/static"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			resp := postModernMCP(t, ctx, token, 3, tc.method, tc.target, tc.params)
			code, _, ok := runner.JSONRPCError(resp.Body)
			if resp.Status != http.StatusBadRequest || !ok || code != -32602 {
				t.Fatalf("modern unknown %s = HTTP %d JSON-RPC %d (body: %s), want 400/-32602", name, resp.Status, code, resp.Body)
			}
		})
	}
}

func postModernMCP(
	t *testing.T,
	ctx context.Context,
	bearer string,
	id int,
	method string,
	name string,
	params map[string]any,
) runner.RawResponse {
	t.Helper()
	params["_meta"] = map[string]any{
		"io.modelcontextprotocol/protocolVersion":    "2026-07-28",
		"io.modelcontextprotocol/clientInfo":         map[string]any{"name": "dsx-exchange-tests", "version": "1"},
		"io.modelcontextprotocol/clientCapabilities": map[string]any{},
	}
	body, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
		"params":  params,
	})
	if err != nil {
		t.Fatalf("marshal modern MCP request: %v", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, runner.GatewayURL(t), bytes.NewReader(body))
	if err != nil {
		t.Fatalf("build modern MCP request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+bearer)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Mcp-Protocol-Version", "2026-07-28")
	req.Header.Set("Mcp-Method", method)
	if name != "" {
		req.Header.Set("Mcp-Name", name)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST modern MCP request: %v", err)
	}
	_, responseBody := readAll(t, resp)
	return runner.RawResponse{Status: resp.StatusCode, Body: responseBody, Header: resp.Header.Clone()}
}
