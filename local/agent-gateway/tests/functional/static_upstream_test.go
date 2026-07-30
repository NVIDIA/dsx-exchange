// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

// Static upstream routing through the chart-emitted dataplane.
package functional

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/NVIDIA/dsx-exchange/local/agent-gateway/tests/functional/internal/runner"
	"github.com/mark3labs/mcp-go/mcp"
)

const staticHeaderTool = "mcp-backend-b-static_headers"

func TestStaticUpstreamRoutesAndHonorsAuthorization(t *testing.T) {
	runner.ParallelReadOnly(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	operator := runner.NewSession(t, "operator")
	t.Cleanup(operator.Close)
	requireToolsEventually(t, ctx, operator, []string{cscHeaderTool, staticHeaderTool}, "layered upstream discovery")

	req := mcp.CallToolRequest{}
	req.Params.Name = staticHeaderTool
	req.Params.Arguments = map[string]any{}
	res, err := operator.Client.CallTool(ctx, req)
	if err != nil {
		t.Fatalf("tools/call(%s): %v", staticHeaderTool, err)
	}
	headers := decodeHeaderEchoResult(t, res)
	if got := headerValue(headers, "x-dsx-backend-id"); got != cscBackendNS+"/mcp-backend-b" {
		t.Fatalf("static target reached %q, want %q", got, cscBackendNS+"/mcp-backend-b")
	}
	if got, want := headerValue(headers, "authorization"), "Bearer "+operator.Token; got != want {
		t.Fatalf("static target Authorization = %q, want caller bearer", got)
	}

	tenant := runner.NewSession(t, tenantAUnlimited)
	t.Cleanup(tenant.Close)
	names, err := tenant.ListToolNames(ctx)
	if err != nil {
		t.Fatalf("tenant tools/list: %v", err)
	}
	for _, name := range names {
		if strings.HasPrefix(name, "mcp-backend-b-static_") {
			t.Fatalf("%s tools/list leaked static target %q", tenantAUnlimited, name)
		}
	}

	res, err = tenant.Client.CallTool(ctx, req)
	if err == nil && (res == nil || !res.IsError) {
		t.Fatalf("%s tools/call(%s) succeeded", tenantAUnlimited, staticHeaderTool)
	}
}
