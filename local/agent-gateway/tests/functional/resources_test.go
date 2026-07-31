// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package functional

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/NVIDIA/dsx-exchange/local/agent-gateway/tests/functional/internal/runner"
	"github.com/mark3labs/mcp-go/mcp"
)

func TestResourcesListIgnoresUnsupportedBridgeTarget(t *testing.T) {
	runner.ParallelReadOnly(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	s := runner.NewSession(t, tenantAUnlimited)
	t.Cleanup(s.Close)

	result, err := s.Client.ListResources(ctx, mcp.ListResourcesRequest{})
	if err != nil {
		t.Fatalf("resources/list with unsupported bridge target: %v", err)
	}
	// Agentgateway v1.4 keeps successful resource catalogs when another target
	// rejects resources/list, so the stateless bridge no longer fails fanout.
	for _, resource := range result.Resources {
		if resource.URI == "mcp-backend-a-mcp+dsx://fixture/static" {
			return
		}
	}
	t.Fatalf("resources/list = %#v, want backend resource", result.Resources)
}

func TestResourcesReadWithMultiplexing(t *testing.T) {
	runner.ParallelReadOnly(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	s := runner.NewSession(t, tenantAUnlimited)
	t.Cleanup(s.Close)

	req := mcp.ReadResourceRequest{}
	req.Params.URI = "mcp-backend-a-mcp+dsx://fixture/static"
	result, err := s.Client.ReadResource(ctx, req)
	if err != nil {
		t.Fatalf("resources/read through multi-target AgentgatewayBackend: %v", err)
	}
	if result == nil || len(result.Contents) != 1 {
		t.Fatalf("resources/read result = %#v, want one resource content", result)
	}
	content, ok := result.Contents[0].(mcp.TextResourceContents)
	if !ok || !strings.Contains(content.Text, "resource=fixture-static") {
		t.Fatalf("resources/read content = %#v, want static fixture content", result.Contents[0])
	}
}

func TestResourcesReadUnknownResourceWithMultiplexing(t *testing.T) {
	runner.ParallelReadOnly(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	s := runner.NewSession(t, tenantAUnlimited)
	t.Cleanup(s.Close)

	req := mcp.ReadResourceRequest{}
	req.Params.URI = "mcp-backend-a-mcp+dsx://fixture/missing"
	_, err := s.Client.ReadResource(ctx, req)
	if err == nil {
		t.Fatal("resources/read unexpectedly succeeded for an unknown resource")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "unknown resource") {
		t.Fatalf("resources/read error = %q, want unknown resource", err)
	}
}

func TestResourceTemplatesListIgnoresUnsupportedBridgeTarget(t *testing.T) {
	runner.ParallelReadOnly(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	s := runner.NewSession(t, tenantAUnlimited)
	t.Cleanup(s.Close)

	result, err := s.Client.ListResourceTemplates(ctx, mcp.ListResourceTemplatesRequest{})
	if err != nil {
		t.Fatalf("resources/templates/list with unsupported bridge target: %v", err)
	}
	// Agentgateway v1.4 keeps successful template catalogs when another target
	// rejects resources/templates/list, matching resources/list fanout.
	for _, template := range result.ResourceTemplates {
		if template.URITemplate != nil &&
			template.URITemplate.Raw() == "mcp-backend-a-mcp+dsx://fixture/{name}" {
			return
		}
	}
	t.Fatalf("resources/templates/list = %#v, want backend resource template", result.ResourceTemplates)
}

func TestResourceReferenceCompletionUnsupportedAtGateway(t *testing.T) {
	runner.ParallelReadOnly(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	tok, sid := initSession(t, ctx, tenantAUnlimited)

	resp := postSessionMCP(t, ctx, tok, sid, []byte(`{"jsonrpc":"2.0","id":7,"method":"completion/complete","params":{"ref":{"type":"ref/resource","uri":"`+cscBridgeTarget+`+dsx://fixture/static"},"argument":{"name":"name","value":"s"},"context":{"arguments":{"shard_id":"cpc-1"}}}}`))
	_, msg, ok := runner.JSONRPCError(resp.Body)
	if !ok {
		t.Fatalf("completion/complete returned no JSON-RPC error (body: %s)", resp.Body)
	}
	msg = strings.ToLower(msg)
	if !strings.Contains(msg, "unknown resource") || !strings.Contains(msg, "dsx://fixture/static") {
		t.Fatalf("completion/complete error = %q, want unknown bridge resource", msg)
	}
}

func TestResourceCompletionUnsupportedAtBridgeHub(t *testing.T) {
	runner.ParallelReadOnly(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	body, err := runner.K8s(t).CoreV1().RESTClient().Post().
		Namespace(cscGatewayNS).
		Resource("services").
		Name("http:"+cscBridgeService+":mcp").
		SubResource("proxy").
		Suffix("mcp").
		SetHeader("Content-Type", "application/json").
		SetHeader("Accept", "application/json, text/event-stream").
		Body([]byte(`{"jsonrpc":"2.0","id":7,"method":"completion/complete","params":{"ref":{"type":"ref/resource","uri":"dsx://fixture/static"},"argument":{"name":"name","value":"s"},"context":{"arguments":{"shard_id":"cpc-1"}}}}`)).
		DoRaw(ctx)
	if err != nil {
		t.Fatalf("POST bridge hub /mcp: %v", err)
	}
	_, msg, ok := runner.JSONRPCError(body)
	if !ok {
		t.Fatalf("bridge hub completion/complete returned no JSON-RPC error (body: %s)", body)
	}
	msg = strings.ToLower(msg)
	if !strings.Contains(msg, "resource completion") || !strings.Contains(msg, "not supported") {
		t.Fatalf("bridge hub completion/complete error = %q, want resource completion not supported", msg)
	}
}
