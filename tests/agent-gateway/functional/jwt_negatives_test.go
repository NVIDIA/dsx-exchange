// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package functional

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/NVIDIA/dsx-exchange/tests/agent-gateway/functional/internal/runner"
)

const rawToolsList = `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`

func requireAuthRejected(t *testing.T, resp runner.RawResponse) {
	t.Helper()
	if resp.Status != 401 && resp.Status != 403 {
		t.Fatalf("expected auth rejection with 401/403, got %d (body: %s)", resp.Status, resp.Body)
	}
}

func TestJWTWrongIssuerKey(t *testing.T) {
	runner.ParallelReadOnly(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	tok := runner.MintToken(t, "wrong-key-svid")
	requireAuthRejected(t, runner.PostMCP(t, ctx, tok, []byte(rawToolsList)))
}

func TestClientCredentialsOIDCWrongAudiencesRejected(t *testing.T) {
	runner.ParallelReadOnly(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	tok := runner.MintTokenClient(t, "service-agent", "wrong-audience")
	requireAuthRejected(t, runner.PostMCP(t, ctx, tok, []byte(rawToolsList)))
}

func TestJWTExpired(t *testing.T) {
	runner.ParallelReadOnly(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	tok := runner.MintExpiredToken(t, tenantAUnlimited)
	requireAuthRejected(t, runner.PostMCP(t, ctx, tok, []byte(rawToolsList)))
}

func TestJWTUnconfiguredIssuer(t *testing.T) {
	runner.ParallelReadOnly(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	tok := runner.MintToken(t, "unconfigured-issuer")
	requireAuthRejected(t, runner.PostMCP(t, ctx, tok, []byte(rawToolsList)))
}

func TestJWTMalformedBearerRejected(t *testing.T) {
	runner.ParallelReadOnly(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	requireAuthRejected(t, runner.PostMCP(t, ctx, "not-a-jwt", []byte(rawToolsList)))
}

func TestHumanOIDCOperatorAllowed(t *testing.T) {
	runner.ParallelReadOnly(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	s := runner.NewSession(t, "operator")
	t.Cleanup(s.Close)
	names, err := s.ListToolNames(ctx)
	if err != nil {
		t.Fatalf("operator tools/list: %v", err)
	}
	if len(names) == 0 {
		t.Fatalf("operator token produced an empty tool catalogue")
	}
}

func TestHumanOIDCViewerUsesUnprivilegedMCPPolicy(t *testing.T) {
	runner.ParallelReadOnly(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	s := runner.NewSession(t, "viewer")
	t.Cleanup(s.Close)
	names, err := s.ListToolNames(ctx)
	if err != nil {
		t.Fatalf("viewer tools/list: %v", err)
	}
	if !containsBare(names, "echo") {
		t.Fatalf("viewer missing echo from unprivileged MCP target (saw %v)", names)
	}
	if containsName(names, "mcp-backend-b-mcp_echo") {
		t.Fatalf("viewer saw operator-only backend-b tool (saw %v)", names)
	}
}

func TestClientCredentialsOIDCWithAdditionalAudienceUsesUnprivilegedMCPPolicy(t *testing.T) {
	runner.ParallelReadOnly(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	s := runner.NewSession(t, "service-agent")
	t.Cleanup(s.Close)
	names, err := s.ListToolNames(ctx)
	if err != nil {
		t.Fatalf("client credentials tools/list: %v", err)
	}
	if !containsBare(names, "echo") {
		t.Fatalf("client credentials token missing echo from unprivileged MCP target (saw %v)", names)
	}
	if containsName(names, "mcp-backend-b-mcp_echo") {
		t.Fatalf("client credentials token saw operator-only backend-b tool (saw %v)", names)
	}
}

func TestSVIDOtherTenantUsesUnprivilegedMCPPolicy(t *testing.T) {
	runner.ParallelReadOnly(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	tok, sid := initSession(t, ctx, "tenant-c")
	cases := []struct {
		method  string
		body    string
		allowed string
		names   []string
	}{
		{
			method:  "tools/list",
			body:    `{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`,
			allowed: "mcp-backend-a-mcp_echo",
			names:   []string{"mcp-backend-b-mcp_longRunningOperation", "mcp-backend-b-mcp_annotatedMessage", "mcp-backend-b-mcp_structuredContent", "mcp-backend-b-mcp_getResourceLinks"},
		},
		{
			method:  "prompts/list",
			body:    `{"jsonrpc":"2.0","id":3,"method":"prompts/list","params":{}}`,
			allowed: "mcp-backend-a-mcp_simple_prompt",
			names:   []string{"mcp-backend-b-mcp_simple_prompt"},
		},
		{
			method:  "resources/list",
			body:    `{"jsonrpc":"2.0","id":4,"method":"resources/list","params":{}}`,
			allowed: "mcp-backend-a-mcp+dsx://fixture/static",
			names:   []string{"mcp-backend-b-mcp+dsx://fixture/static"},
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.method, func(t *testing.T) {
			resp := postSessionMCP(t, ctx, tok, sid, []byte(tc.body))
			requireHTTPSuccess(t, resp)
			if !bytes.Contains(resp.Body, []byte(tc.allowed)) {
				t.Fatalf("%s missing allowed catalog name %q (body: %s)", tc.method, tc.allowed, resp.Body)
			}
			for _, leak := range tc.names {
				if bytes.Contains(resp.Body, []byte(leak)) {
					t.Fatalf("%s leaked catalog name %q (status %d body: %s)", tc.method, leak, resp.Status, resp.Body)
				}
			}
		})
	}
}

func TestSVIDMalformedSubjectDeniedByCEL(t *testing.T) {
	runner.ParallelReadOnly(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	tok := runner.MintToken(t, "bad-svid")
	requireAuthRejected(t, runner.PostMCP(t, ctx, tok, []byte(rawToolsList)))
}
