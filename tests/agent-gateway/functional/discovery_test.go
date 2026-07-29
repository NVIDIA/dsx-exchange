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

// Discovery behavior for tool filtering, initialize handshake, and no-leak
// assertions.
package functional

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/NVIDIA/dsx-exchange/tests/agent-gateway/functional/internal/runner"
)

// stripTargetPrefix drops the agentgateway-aggregator `<target>_`
// prefix from a tool name (DELIMITER='_' in mcp/handler.rs reverse-
// splits on the first `_`). Returns the bare name.
func stripTargetPrefix(name string) string {
	if i := strings.Index(name, "_"); i >= 0 {
		return name[i+1:]
	}
	return name
}

// containsBare returns true if the slice contains `bare` as a bare
// name after stripping any `<target>_` prefix.
func containsBare(names []string, bare string) bool {
	for _, n := range names {
		if stripTargetPrefix(n) == bare {
			return true
		}
	}
	return false
}

func containsName(names []string, want string) bool {
	for _, n := range names {
		if n == want {
			return true
		}
	}
	return false
}

func assertOnlyUnprivilegedLocalToolTarget(t *testing.T, tenant string, names []string) {
	t.Helper()
	for _, name := range names {
		if strings.HasPrefix(name, "mcp-backend-b-mcp_") || strings.HasPrefix(name, cscBridgeTarget+"_") {
			t.Fatalf("%s saw operator-only tool target in %q (all tools: %v)", tenant, name, names)
		}
	}
}

// Tenant-a sees the configured unprivileged MCP target. Operator-only targets
// must not appear.
func TestToolsListTenantA(t *testing.T) {
	runner.ParallelReadOnly(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	s := runner.NewSession(t, tenantAUnlimited)
	t.Cleanup(s.Close)

	names, err := s.ListToolNames(ctx)
	if err != nil {
		t.Fatalf("tools/list: %v", err)
	}
	if len(names) == 0 {
		t.Fatalf("no tools returned in result.tools")
	}

	expected := []string{
		"echo", "add", "printEnv", "getTinyImage", "headers",
		"longRunningOperation", "annotatedMessage", "structuredContent",
		"getResourceLinks", "sampleLLM", "getResourceReference",
	}
	for _, want := range expected {
		if !containsName(names, "mcp-backend-a-mcp_"+want) {
			t.Errorf("%s missing expected tool %q (saw %v)", tenantAUnlimited, want, names)
		}
	}
	assertOnlyUnprivilegedLocalToolTarget(t, tenantAUnlimited, names)
}

// Tenant-b gets the same unprivileged MCP target as other non-operator tenants.
func TestToolsListTenantB(t *testing.T) {
	runner.ParallelReadOnly(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	s := runner.NewSession(t, tenantBUnlimited)
	t.Cleanup(s.Close)

	names, err := s.ListToolNames(ctx)
	if err != nil {
		t.Fatalf("tools/list: %v", err)
	}
	if len(names) == 0 {
		t.Fatalf("no tools returned in result.tools")
	}

	expected := []string{
		"echo", "add", "printEnv", "getTinyImage", "headers",
		"longRunningOperation", "annotatedMessage", "structuredContent",
		"getResourceLinks", "sampleLLM", "getResourceReference",
	}
	for _, want := range expected {
		if !containsName(names, "mcp-backend-a-mcp_"+want) {
			t.Errorf("%s missing expected tool %q (saw %v)", tenantBUnlimited, want, names)
		}
	}
	assertOnlyUnprivilegedLocalToolTarget(t, tenantBUnlimited, names)
}

// No JWT -> tools/list rejected by native JWT authentication. No tool
// names may appear in the rejection body.
func TestToolsListUnauth(t *testing.T) {
	runner.ParallelReadOnly(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	resp := runner.PostMCP(t, ctx, "", []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`))
	if resp.Status != 401 && resp.Status != 403 {
		t.Fatalf("expected 401/403 unauth, got %d (body: %s)", resp.Status, resp.Body)
	}
	for _, leak := range []string{"echo", "add", "longRunningOperation", "annotatedMessage"} {
		if bytes.Contains(resp.Body, []byte(leak)) {
			t.Fatalf("tool name %q leaked in unauth response body: %s", leak, resp.Body)
		}
	}
}

func TestCatalogListUnauthDoesNotLeakNames(t *testing.T) {
	runner.ParallelReadOnly(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cases := []struct {
		method string
		names  []string
	}{
		{
			method: "prompts/list",
			names:  []string{"simple_prompt", "mcp-backend-a-mcp_simple_prompt", "mcp-backend-b-mcp_simple_prompt"},
		},
		{
			method: "resources/list",
			names:  []string{"dsx://fixture/static", "mcp-backend-a-mcp+dsx://fixture/static", "mcp-backend-b-mcp+dsx://fixture/static"},
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.method, func(t *testing.T) {
			body := []byte(`{"jsonrpc":"2.0","id":1,"method":"` + tc.method + `","params":{}}`)
			resp := runner.PostMCP(t, ctx, "", body)
			if resp.Status != 401 && resp.Status != 403 {
				t.Fatalf("expected 401/403 unauth for %s, got %d (body: %s)", tc.method, resp.Status, resp.Body)
			}
			for _, leak := range tc.names {
				if bytes.Contains(resp.Body, []byte(leak)) {
					t.Fatalf("unauth %s response leaked catalog name %q (body: %s)", tc.method, leak, resp.Body)
				}
			}
		})
	}
}

// MCP `initialize` + `notifications/initialized` works through the gateway.
func TestInitializeHandshake(t *testing.T) {
	runner.ParallelReadOnly(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	s := runner.NewSession(t, tenantAUnlimited)
	t.Cleanup(s.Close)

	if _, err := s.ListToolNames(ctx); err != nil {
		t.Fatalf("tools/list after initialize: %v", err)
	}
}

// Unauthenticated requests must not leak tool names. Tenant-a must not see
// operator-only MCP targets.
func TestNoListLeak(t *testing.T) {
	runner.ParallelReadOnly(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	knownTools := []string{
		"echo", "add", "longRunningOperation", "annotatedMessage",
		"getTinyImage", "printEnv", "getResourceLinks",
		"structuredContent", "sampleLLM", "getResourceReference",
		"headers",
	}

	resp := runner.PostMCP(t, ctx, "", []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`))
	for _, leak := range knownTools {
		needle := []byte(`"` + leak + `"`)
		if bytes.Contains(resp.Body, needle) {
			t.Fatalf("unauth response leaked tool name %q (status %d, body: %s)", leak, resp.Status, resp.Body)
		}
	}

	s := runner.NewSession(t, tenantAUnlimited)
	t.Cleanup(s.Close)
	names, err := s.ListToolNames(ctx)
	if err != nil {
		t.Fatalf("%s tools/list: %v", tenantAUnlimited, err)
	}
	assertOnlyUnprivilegedLocalToolTarget(t, tenantAUnlimited, names)
}
