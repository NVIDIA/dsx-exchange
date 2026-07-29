// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestToolsListIncludesGatewayFixtures(t *testing.T) {
	resp := postRPC(t, `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`, nil)
	result := rpcResult(t, resp)
	tools, ok := result["tools"].([]any)
	if !ok {
		t.Fatalf("tools/list result missing tools array: %#v", result)
	}
	names := map[string]bool{}
	for _, raw := range tools {
		tool, ok := raw.(map[string]any)
		if !ok {
			t.Fatalf("tool has type %T", raw)
		}
		name, _ := tool["name"].(string)
		names[name] = true
	}
	for _, want := range []string{
		"echo", "add", "printEnv", "getTinyImage", "headers",
		"longRunningOperation", "annotatedMessage", "structuredContent",
		"getResourceLinks", "sampleLLM", "getResourceReference",
	} {
		if !names[want] {
			t.Fatalf("tools/list missing %q in %v", want, names)
		}
	}
}

func TestToolsCallEcho(t *testing.T) {
	resp := postRPC(t, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"echo","arguments":{"message":"hello"}}}`, nil)
	if got := firstText(t, rpcResult(t, resp)); got != "hello" {
		t.Fatalf("echo text = %q, want hello", got)
	}
}

func TestToolsCallHeaders(t *testing.T) {
	resp := postRPC(t, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"headers","arguments":{}}}`, map[string]string{
		"Authorization": "Bearer caller",
	})
	var headers map[string]string
	if err := json.Unmarshal([]byte(firstText(t, rpcResult(t, resp))), &headers); err != nil {
		t.Fatalf("headers text is not JSON: %v", err)
	}
	if got := headerValue(headers, "authorization"); got != "Bearer caller" {
		t.Fatalf("authorization header = %q", got)
	}
	if got := headerValue(headers, "x-dsx-backend-id"); got == "" {
		t.Fatalf("missing x-dsx-backend-id in %v", headers)
	}
}

func TestLongRunningOperationCanStreamProgressNotification(t *testing.T) {
	local := httptest.NewServer(newHandler())
	defer local.Close()
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		local.URL,
		bytes.NewBufferString(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"longRunningOperation","arguments":{"bridge_stream":true,"block_until_cancel":true}}}`),
	)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	resp, err := local.Client().Do(req)
	if err != nil {
		t.Fatalf("post streaming request: %v", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Errorf("close response body: %v", err)
		}
	}()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if got := resp.Header.Get("Content-Type"); got != "text/event-stream" {
		t.Fatalf("Content-Type = %q, want text/event-stream", got)
	}
	reader := bufio.NewReader(resp.Body)
	var event strings.Builder
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("read progress event: %v", err)
		}
		event.WriteString(line)
		if line == "\n" {
			break
		}
	}
	if !strings.Contains(event.String(), `"method":"notifications/progress"`) || !strings.Contains(event.String(), "bridge-stream-fixture") {
		t.Fatalf("stream event missing progress notification: %s", event.String())
	}
}

func TestMCPPostLogsInvocationMarker(t *testing.T) {
	var logs bytes.Buffer
	old := log.Writer()
	log.SetOutput(&logs)
	t.Cleanup(func() {
		log.SetOutput(old)
	})

	postRPC(t, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"headers","arguments":{}}}`, map[string]string{
		"X-Dsx-Test-Invocation": "marker-1",
	})
	got := logs.String()
	if !strings.Contains(got, "Received MCP POST request") {
		t.Fatalf("logs missing request marker: %s", got)
	}
	if !strings.Contains(got, "marker-1") {
		t.Fatalf("logs missing invocation marker: %s", got)
	}
}

func TestPromptsGetSimplePrompt(t *testing.T) {
	resp := postRPC(t, `{"jsonrpc":"2.0","id":1,"method":"prompts/get","params":{"name":"simple_prompt"}}`, nil)
	result := rpcResult(t, resp)
	messages, ok := result["messages"].([]any)
	if !ok || len(messages) == 0 {
		t.Fatalf("prompts/get returned no messages: %#v", result)
	}
}

func TestPromptsListIncludesSimplePrompt(t *testing.T) {
	resp := postRPC(t, `{"jsonrpc":"2.0","id":1,"method":"prompts/list","params":{}}`, nil)
	result := rpcResult(t, resp)
	prompts, ok := result["prompts"].([]any)
	if !ok || len(prompts) != 1 {
		t.Fatalf("prompts/list returned %#v, want one prompt", result)
	}
	prompt, ok := prompts[0].(map[string]any)
	if !ok {
		t.Fatalf("prompt item has type %T", prompts[0])
	}
	if got := prompt["name"]; got != "simple_prompt" {
		t.Fatalf("prompt name = %v, want simple_prompt", got)
	}
}

func TestResourcesReadStatic(t *testing.T) {
	resp := postRPC(t, `{"jsonrpc":"2.0","id":1,"method":"resources/read","params":{"uri":"dsx://fixture/static"}}`, nil)
	result := rpcResult(t, resp)
	contents, ok := result["contents"].([]any)
	if !ok || len(contents) == 0 {
		t.Fatalf("resources/read returned no contents: %#v", result)
	}
}

func TestResourcesListIncludesStaticFixture(t *testing.T) {
	resp := postRPC(t, `{"jsonrpc":"2.0","id":1,"method":"resources/list","params":{}}`, nil)
	result := rpcResult(t, resp)
	resources, ok := result["resources"].([]any)
	if !ok || len(resources) != 1 {
		t.Fatalf("resources/list returned %#v, want one resource", result)
	}
	resource, ok := resources[0].(map[string]any)
	if !ok {
		t.Fatalf("resource item has type %T", resources[0])
	}
	if got := resource["uri"]; got != "dsx://fixture/static" {
		t.Fatalf("resource uri = %v, want dsx://fixture/static", got)
	}
}

func TestInvalidMethodReturnsRPCError(t *testing.T) {
	resp := postRPC(t, `{"jsonrpc":"2.0","id":1,"method":"does/not/exist","params":{}}`, nil)
	requireRPCError(t, resp, "not found")
}

func TestUnknownToolReturnsRPCError(t *testing.T) {
	resp := postRPC(t, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"missing","arguments":{}}}`, nil)
	requireRPCError(t, resp, "tool not found")
}

func TestUnknownResourceReturnsRPCError(t *testing.T) {
	resp := postRPC(t, `{"jsonrpc":"2.0","id":1,"method":"resources/read","params":{"uri":"dsx://fixture/missing"}}`, nil)
	requireRPCError(t, resp, "unknown resource")
}

func postRPC(t *testing.T, body string, headers map[string]string) map[string]any {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rr := httptest.NewRecorder()
	newHandler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rr.Code, rr.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v (body %s)", err, rr.Body.String())
	}
	return resp
}

func rpcResult(t *testing.T, resp map[string]any) map[string]any {
	t.Helper()
	if rawErr, ok := resp["error"]; ok && rawErr != nil {
		t.Fatalf("unexpected RPC error: %#v", rawErr)
	}
	result, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("response missing object result: %#v", resp)
	}
	return result
}

func requireRPCError(t *testing.T, resp map[string]any, want string) {
	t.Helper()
	rawErr, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatalf("response missing RPC error: %#v", resp)
	}
	msg, _ := rawErr["message"].(string)
	if !strings.Contains(msg, want) {
		t.Fatalf("error message = %q, want substring %q", msg, want)
	}
}

func firstText(t *testing.T, result map[string]any) string {
	t.Helper()
	content, ok := result["content"].([]any)
	if !ok || len(content) == 0 {
		t.Fatalf("result missing content: %#v", result)
	}
	first, ok := content[0].(map[string]any)
	if !ok {
		t.Fatalf("content item has type %T", content[0])
	}
	text, _ := first["text"].(string)
	return text
}

func headerValue(headers map[string]string, key string) string {
	for k, v := range headers {
		if strings.EqualFold(k, key) {
			return v
		}
	}
	return ""
}
