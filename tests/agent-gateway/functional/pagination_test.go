// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

// MCP tools/list pagination contract: page 1 either ends the
// catalogue (no cursor) or returns a cursor that page 2 honours.
// Both outcomes are protocol-legal; the test asserts whichever
// applies and does not skip.
package functional

import (
	"context"
	"testing"
	"time"

	"github.com/NVIDIA/dsx-exchange/tests/agent-gateway/functional/internal/runner"
	"github.com/mark3labs/mcp-go/mcp"
)

func TestToolsListPagination(t *testing.T) {
	runner.ParallelReadOnly(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	s := runner.NewSession(t, tenantBUnlimited)
	t.Cleanup(s.Close)

	first, err := s.Client.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil {
		t.Fatalf("page 1 tools/list: %v", err)
	}
	if len(first.Tools) == 0 {
		t.Fatalf("page 1 returned an empty tools array — discovery is broken")
	}

	if first.NextCursor == "" {
		// Single-page catalogue is a legal protocol outcome.
		// `len(first.Tools) > 0` above is the assertion.
		return
	}

	req := mcp.ListToolsRequest{}
	req.Params.Cursor = mcp.Cursor(first.NextCursor)
	second, err := s.Client.ListTools(ctx, req)
	if err != nil {
		t.Fatalf("page 2 tools/list: %v", err)
	}
	if len(second.Tools) == 0 {
		t.Fatalf("page 2 had no tools array")
	}
}
