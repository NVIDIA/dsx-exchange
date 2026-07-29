// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

// Auth and dispatch behavior.
package functional

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/NVIDIA/dsx-exchange/tests/agent-gateway/functional/internal/runner"
	"github.com/mark3labs/mcp-go/mcp"
)

// pickFirstToolMatching scans the operator catalogue and returns the prefixed
// name whose bare suffix matches `bareName`. Falls back to the bare name if no
// match.
func pickFirstToolMatching(t *testing.T, ctx context.Context, bareName string) string {
	t.Helper()
	s := runner.NewSession(t, "operator")
	t.Cleanup(s.Close)
	names, err := s.ListToolNames(ctx)
	if err != nil {
		t.Fatalf("tools/list lookup for %q: %v", bareName, err)
	}
	for _, n := range names {
		if stripTargetPrefix(n) == bareName {
			return n
		}
	}
	return bareName
}

func TestToolsCallAllowed(t *testing.T) {
	runner.ParallelReadOnly(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	toolName := "mcp-backend-a-mcp_echo"
	s := runner.NewSession(t, tenantBUnlimited)
	t.Cleanup(s.Close)
	req := mcp.CallToolRequest{}
	req.Params.Name = toolName
	req.Params.Arguments = map[string]any{"message": "hello"}
	res, err := s.Client.CallTool(ctx, req)
	if err != nil {
		t.Fatalf("tools/call(%q): %v", toolName, err)
	}
	if res == nil || (len(res.Content) == 0 && !res.IsError) {
		t.Fatalf("tools/call(%q) returned no content: %+v", toolName, res)
	}
}

func TestToolsCallDenied(t *testing.T) {
	runner.ParallelReadOnly(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	toolName := "mcp-backend-b-mcp_longRunningOperation"
	s := runner.NewSession(t, tenantAUnlimited)
	t.Cleanup(s.Close)
	req := mcp.CallToolRequest{}
	req.Params.Name = toolName
	req.Params.Arguments = map[string]any{}
	res, err := s.Client.CallTool(ctx, req)
	// Acceptable deny shapes are an SDK error or result.IsError.
	if err != nil {
		return
	}
	if res != nil && res.IsError {
		return
	}
	t.Fatalf("%s tools/call(%q) returned no explicit denial (result: %+v)", tenantAUnlimited, toolName, res)
}

func TestToolsCallUnknownTool(t *testing.T) {
	runner.ParallelReadOnly(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	s := runner.NewSession(t, tenantBUnlimited)
	t.Cleanup(s.Close)
	req := mcp.CallToolRequest{}
	req.Params.Name = "does-not-exist"
	req.Params.Arguments = map[string]any{}
	res, err := s.Client.CallTool(ctx, req)
	if err != nil {
		// SDK surfaced a JSON-RPC error envelope as a Go error — pass.
		// Make sure no HTML leaked through.
		if strings.Contains(strings.ToLower(err.Error()), "<html") {
			t.Fatalf("HTML in error: %v", err)
		}
		return
	}
	if res != nil && res.IsError {
		return
	}
	if res != nil && len(res.Content) == 0 {
		t.Fatalf("tools/call for unknown tool returned no clear error signal")
	}
	// If we got here with a populated result, regression: gateway
	// silently routed an unknown tool somewhere.
	t.Fatalf("tools/call(does-not-exist) returned a non-error result: %+v", res)
}

func TestErrorEnvelopeDeny(t *testing.T) {
	runner.ParallelReadOnly(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	toolName := "mcp-backend-b-mcp_longRunningOperation"
	s := runner.NewSession(t, tenantAUnlimited)
	t.Cleanup(s.Close)
	req := mcp.CallToolRequest{}
	req.Params.Name = toolName
	req.Params.Arguments = map[string]any{}
	res, err := s.Client.CallTool(ctx, req)

	// The deny body / error must not leak internal cluster identifiers.
	combined := []byte{}
	if err != nil {
		combined = append(combined, []byte(err.Error())...)
	}
	if res != nil {
		b, _ := json.Marshal(res)
		combined = append(combined, b...)
	}
	if err == nil && (res == nil || !res.IsError) {
		t.Fatalf("%s tools/call(%q) returned no explicit denial (result: %+v)", tenantAUnlimited, toolName, res)
	}
	leaks := []string{
		"mcp-backend-a.csc-mcp-backends.svc.cluster.local",
		"mcp-backend-b.csc-mcp-backends.svc.cluster.local",
		"human-oidc.agent-gateway-fixtures.svc.cluster.local",
		"service-oidc.agent-gateway-fixtures.svc.cluster.local",
		"svid-issuer.agent-gateway-fixtures.svc.cluster.local",
	}
	combinedLower := strings.ToLower(string(combined))
	for _, leak := range leaks {
		if strings.Contains(combinedLower, strings.ToLower(leak)) {
			t.Fatalf("deny envelope leaked internal identifier %q (combined: %s)", leak, combined)
		}
	}
}

func TestErrorEnvelopeBadRequest(t *testing.T) {
	runner.ParallelReadOnly(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	tok := runner.MintToken(t, "operator")
	resp := runner.PostMCP(t, ctx, tok, []byte(`{"not":"a-valid-mcp-request"}`))
	if bytes.Contains(bytes.ToLower(resp.Body), []byte("<html")) {
		t.Fatalf("HTML in malformed-request response body: %s", resp.Body)
	}
	// FR-007: body must carry an error signal. Either a JSON-RPC
	// envelope or a known phrase from the gateway's pre-parse path.
	if _, msg, ok := runner.JSONRPCError(resp.Body); ok && msg != "" {
		return
	}
	bodyLower := strings.ToLower(string(resp.Body))
	for _, phrase := range []string{"invalid", "bad request", "jsonrpc", "authorization failed", "permission denied", "forbidden"} {
		if strings.Contains(bodyLower, phrase) {
			return
		}
	}
	t.Fatalf("no recognizable error signal for bad request (status %d, body: %s)", resp.Status, resp.Body)
}
