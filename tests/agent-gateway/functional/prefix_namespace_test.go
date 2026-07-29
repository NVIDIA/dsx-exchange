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

// Prefix-namespace contract. Multi-target AgentgatewayBackend
// auto-multiplexes when targets.len() > 1, so each aggregated tool
// or prompt is published as `<target>_<bare>` and reverse-split on
// the first `_`.
package functional

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/NVIDIA/dsx-exchange/tests/agent-gateway/functional/internal/runner"
	"github.com/mark3labs/mcp-go/mcp"
)

func TestPrefixNamespaceTools(t *testing.T) {
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
		t.Fatalf("tools/list returned no tools — cannot verify prefix shape")
	}
	allowed := []string{"mcp-backend-a-mcp_"}
	for _, n := range names {
		ok := false
		for _, want := range allowed {
			if strings.HasPrefix(n, want) {
				ok = true
				break
			}
		}
		if !ok {
			t.Fatalf("tools/list name %q does not match any expected prefix %v (other names: %v)", n, allowed, names)
		}
	}
}

func TestPrefixNamespacePrompts(t *testing.T) {
	runner.ParallelReadOnly(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	s := runner.NewSession(t, tenantBUnlimited)
	t.Cleanup(s.Close)

	names, err := s.ListPromptNames(ctx)
	if err != nil {
		t.Fatalf("prompts/list: %v", err)
	}
	if len(names) == 0 {
		t.Fatalf("prompts/list returned no prompts — cannot verify prefix shape")
	}
	const want = "mcp-backend-a-mcp_"
	for _, n := range names {
		if !strings.HasPrefix(n, want) {
			t.Fatalf("prompts/list name %q does not match expected prefix %q (other names: %v)", n, want, names)
		}
	}
}

func TestPrefixNamespaceBareNameRejected(t *testing.T) {
	runner.ParallelReadOnly(t)

	// Bare-name dispatch must not silently land on a backend.
	// Probe with both tenants so a default-to-first-target bug cannot
	// false-green the test.
	for _, tenant := range []string{tenantAUnlimited, tenantBUnlimited} {
		t.Run(tenant, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			s := runner.NewSession(t, tenant)
			t.Cleanup(s.Close)

			req := mcp.CallToolRequest{}
			req.Params.Name = "echo"
			req.Params.Arguments = map[string]any{"message": "x"}
			res, err := s.Client.CallTool(ctx, req)
			if err != nil {
				// JSON-RPC error envelope from the gateway is the
				// expected outcome when bare-name dispatch has no
				// `<target>_` prefix to reverse-split on. Pass.
				return
			}
			if res != nil && res.IsError {
				return
			}
			// A successful call body would mean the prefix contract
			// is bypassable for this principal — that is the
			// regression the test is meant to catch.
			t.Fatalf("bare-name tools/call(echo) for %s returned a non-error result — prefix contract is bypassable on this principal", tenant)
		})
	}
}

func TestPrefixNamespacePromptBareNameRejected(t *testing.T) {
	runner.ParallelReadOnly(t)

	for _, tenant := range []string{tenantAUnlimited, tenantBUnlimited} {
		t.Run(tenant, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			s := runner.NewSession(t, tenant)
			t.Cleanup(s.Close)

			req := mcp.GetPromptRequest{}
			req.Params.Name = "simple_prompt"
			res, err := s.Client.GetPrompt(ctx, req)
			if err != nil {
				return
			}
			if res != nil && len(res.Messages) > 0 {
				t.Fatalf("bare-name prompts/get(simple_prompt) for %s returned a non-empty messages payload — prefix contract is bypassable on this principal", tenant)
			}
		})
	}
}

func TestPrefixNamespacePromptOperatorOnlyTargetRejected(t *testing.T) {
	runner.ParallelReadOnly(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	s := runner.NewSession(t, tenantAUnlimited)
	t.Cleanup(s.Close)

	names, err := s.ListPromptNames(ctx)
	if err != nil {
		t.Fatalf("prompts/list: %v", err)
	}
	for _, name := range names {
		if strings.HasPrefix(name, "mcp-backend-b-mcp_") {
			t.Fatalf("%s prompts/list leaked operator-only target prompt %q (all prompts: %v)", tenantAUnlimited, name, names)
		}
	}

	req := mcp.GetPromptRequest{}
	req.Params.Name = "mcp-backend-b-mcp_simple_prompt"
	res, err := s.Client.GetPrompt(ctx, req)
	if err != nil {
		return
	}
	if res != nil && len(res.Messages) > 0 {
		t.Fatalf("%s prompts/get(mcp-backend-b-mcp_simple_prompt) returned a non-empty messages payload", tenantAUnlimited)
	}
}

func TestPrefixNamespacePromptPositive(t *testing.T) {
	runner.ParallelReadOnly(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	s := runner.NewSession(t, "operator")
	t.Cleanup(s.Close)

	req := mcp.GetPromptRequest{}
	req.Params.Name = "mcp-backend-b-mcp_simple_prompt"
	res, err := s.Client.GetPrompt(ctx, req)
	if err != nil {
		t.Fatalf("prompts/get(mcp-backend-b-mcp_simple_prompt): %v", err)
	}
	if res == nil || len(res.Messages) == 0 {
		t.Fatalf("prefixed prompts/get(mcp-backend-b-mcp_simple_prompt) did not return a non-empty .messages — prompt dispatch path is broken on the prefixed name")
	}
}
