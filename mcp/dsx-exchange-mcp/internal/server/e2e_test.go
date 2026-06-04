// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

func TestStagedMCPE2EDeployedBus(t *testing.T) {
	if os.Getenv("RUN_EXCHANGE_MCP_E2E") != "1" {
		t.Skip("set RUN_EXCHANGE_MCP_E2E=1 to run staged MCP e2e")
	}

	endpoint := requiredEnv(t, "DSX_EXCHANGE_MCP_URL")
	bearer := requiredEnv(t, "DSX_EXCHANGE_E2E_BEARER")
	allowedTopic := requiredEnv(t, "DSX_EXCHANGE_E2E_ALLOWED_TOPIC")

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	client := &mcpHTTPClient{
		endpoint: endpoint,
		bearer:   bearer,
		httpc:    &http.Client{Timeout: 30 * time.Second},
	}

	sessionID, err := client.initialize(ctx)
	if err != nil {
		t.Fatalf("initialize through MCP endpoint failed: %v", err)
	}
	if err := client.initialized(ctx, sessionID); err != nil {
		t.Fatalf("notifications/initialized failed: %v", err)
	}

	tools, err := client.listTools(ctx, sessionID)
	if err != nil {
		t.Fatalf("tools/list failed: %v", err)
	}
	toolName := chooseSubscribeToolName(tools, os.Getenv("DSX_EXCHANGE_E2E_TOOL_NAME"))
	if toolName == "" {
		t.Fatalf("tools/list did not expose dsx_exchange_subscribe (tools: %v)", tools)
	}

	res, err := client.callTool(ctx, sessionID, toolName, map[string]any{
		"topic_filter":   allowedTopic,
		"max_messages":   1,
		"max_duration_s": 5,
	})
	if err != nil {
		t.Fatalf("tools/call(%q allowed topic) failed: %v", toolName, err)
	}
	if res.IsError {
		t.Fatalf("tools/call(%q allowed topic) returned MCP tool error: %s", toolName, res.textSummary())
	}

	if deniedTopic := os.Getenv("DSX_EXCHANGE_E2E_DENIED_TOPIC"); deniedTopic != "" {
		denied, err := client.callTool(ctx, sessionID, toolName, map[string]any{
			"topic_filter":   deniedTopic,
			"max_messages":   1,
			"max_duration_s": 5,
		})
		if err == nil && !denied.IsError {
			t.Fatalf("tools/call(%q denied topic) unexpectedly succeeded", toolName)
		}
	}
}

func TestStagedMCPSchemaDescribeThroughEndpoint(t *testing.T) {
	if os.Getenv("RUN_EXCHANGE_MCP_SCHEMA_E2E") != "1" {
		t.Skip("set RUN_EXCHANGE_MCP_SCHEMA_E2E=1 to run staged MCP schema e2e")
	}

	endpoint := requiredEnv(t, "DSX_EXCHANGE_MCP_URL")
	bearer := requiredEnv(t, "DSX_EXCHANGE_E2E_BEARER")

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	client := &mcpHTTPClient{
		endpoint: endpoint,
		bearer:   bearer,
		httpc:    &http.Client{Timeout: 30 * time.Second},
	}

	sessionID, err := client.initialize(ctx)
	if err != nil {
		t.Fatalf("initialize through MCP endpoint failed: %v", err)
	}
	if err := client.initialized(ctx, sessionID); err != nil {
		t.Fatalf("notifications/initialized failed: %v", err)
	}

	tools, err := client.listTools(ctx, sessionID)
	if err != nil {
		t.Fatalf("tools/list failed: %v", err)
	}
	toolName := chooseDescribeTopicToolName(tools, os.Getenv("DSX_EXCHANGE_E2E_DESCRIBE_TOOL_NAME"))
	if toolName == "" {
		t.Fatalf("tools/list did not expose %s (tools: %v)", toolDescribeTopic, tools)
	}
	t.Logf("using schema tool %q from endpoint %s", toolName, endpoint)

	for _, fixture := range loadToolCallFixtures(t) {
		t.Run(fixture.ID, func(t *testing.T) {
			for i, call := range fixture.ExpectedToolCalls {
				if call.Tool != toolDescribeTopic {
					continue
				}
				topicFilter := stringArg(t, fixture.ID, i, call.Arguments, "topic_filter")
				res, err := client.callTool(ctx, sessionID, toolName, call.Arguments)
				if err != nil {
					t.Fatalf("tools/call(%q, %q) failed: %v", toolName, topicFilter, err)
				}
				if res.IsError {
					t.Fatalf("tools/call(%q, %q) returned MCP tool error: %s", toolName, topicFilter, res.textSummary())
				}

				var out describeTopicOutput
				if err := json.Unmarshal([]byte(res.lastText()), &out); err != nil {
					t.Fatalf("decode schema response for %q: %v; content=%s", topicFilter, err, res.textSummary())
				}
				if out.TopicFilter != topicFilter {
					t.Fatalf("schema response topic_filter = %q, want %q", out.TopicFilter, topicFilter)
				}
				if out.Count == 0 {
					t.Fatalf("schema response for %q returned no matches", topicFilter)
				}
				if !hasDomainChannel(out, fixture.ExpectedSchema.Domain, fixture.ExpectedSchema.Channels) {
					t.Fatalf("schema response for %q missing expected domain/channel; result=%#v", topicFilter, out.Matches)
				}
				t.Logf("%s -> %d match(es); first=%s/%s", topicFilter, out.Count, out.Matches[0].Domain, out.Matches[0].Channel)
			}
		})
	}
}

type mcpHTTPClient struct {
	endpoint string
	bearer   string
	httpc    *http.Client
	nextID   int
}

type rpcResponse struct {
	Result json.RawMessage `json:"result"`
	Error  *rpcError       `json:"error"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type toolCallResult struct {
	IsError bool `json:"isError"`
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
}

func (r toolCallResult) textSummary() string {
	var texts []string
	for _, item := range r.Content {
		if item.Text != "" {
			texts = append(texts, item.Text)
		}
	}
	return strings.Join(texts, "\n")
}

func (r toolCallResult) lastText() string {
	for i := len(r.Content) - 1; i >= 0; i-- {
		if r.Content[i].Text != "" {
			return r.Content[i].Text
		}
	}
	return ""
}

func (c *mcpHTTPClient) initialize(ctx context.Context) (string, error) {
	_, sessionID, err := c.request(ctx, "", "initialize", map[string]any{
		"protocolVersion": "2025-06-18",
		"capabilities":    map[string]any{},
		"clientInfo": map[string]any{
			"name":    "dsx-exchange-mcp-e2e",
			"version": "0.1.0",
		},
	})
	return sessionID, err
}

func (c *mcpHTTPClient) initialized(ctx context.Context, sessionID string) error {
	_, _, err := c.post(ctx, sessionID, map[string]any{
		"jsonrpc": "2.0",
		"method":  "notifications/initialized",
	})
	return err
}

func (c *mcpHTTPClient) listTools(ctx context.Context, sessionID string) ([]string, error) {
	raw, _, err := c.request(ctx, sessionID, "tools/list", map[string]any{})
	if err != nil {
		return nil, err
	}
	var result struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("decode tools/list result: %w", err)
	}
	names := make([]string, 0, len(result.Tools))
	for _, tool := range result.Tools {
		names = append(names, tool.Name)
	}
	return names, nil
}

func (c *mcpHTTPClient) callTool(ctx context.Context, sessionID, name string, args map[string]any) (toolCallResult, error) {
	raw, _, err := c.request(ctx, sessionID, "tools/call", map[string]any{
		"name":      name,
		"arguments": args,
	})
	if err != nil {
		return toolCallResult{}, err
	}
	var result toolCallResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return toolCallResult{}, fmt.Errorf("decode tools/call result: %w", err)
	}
	return result, nil
}

func (c *mcpHTTPClient) request(ctx context.Context, sessionID, method string, params map[string]any) (json.RawMessage, string, error) {
	c.nextID++
	resp, newSessionID, err := c.post(ctx, sessionID, map[string]any{
		"jsonrpc": "2.0",
		"id":      c.nextID,
		"method":  method,
		"params":  params,
	})
	if err != nil {
		return nil, newSessionID, err
	}
	if resp.Error != nil {
		return nil, newSessionID, fmt.Errorf("json-rpc error %d: %s", resp.Error.Code, resp.Error.Message)
	}
	return resp.Result, newSessionID, nil
}

func (c *mcpHTTPClient) post(ctx context.Context, sessionID string, payload map[string]any) (rpcResponse, string, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return rpcResponse{}, "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return rpcResponse{}, "", err
	}
	req.Header.Set("Authorization", "Bearer "+c.bearer)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if sessionID != "" {
		req.Header.Set("Mcp-Session-Id", sessionID)
	}

	res, err := c.httpc.Do(req)
	if err != nil {
		return rpcResponse{}, "", err
	}
	defer res.Body.Close()

	raw, err := io.ReadAll(res.Body)
	if err != nil {
		return rpcResponse{}, res.Header.Get("Mcp-Session-Id"), err
	}
	if res.StatusCode >= http.StatusBadRequest {
		return rpcResponse{}, res.Header.Get("Mcp-Session-Id"), fmt.Errorf("http %d: %s", res.StatusCode, strings.TrimSpace(string(raw)))
	}

	data := lastMCPResponseData(raw)
	if len(data) == 0 {
		return rpcResponse{}, res.Header.Get("Mcp-Session-Id"), nil
	}
	var decoded rpcResponse
	if err := json.Unmarshal(data, &decoded); err != nil {
		return rpcResponse{}, res.Header.Get("Mcp-Session-Id"), fmt.Errorf("decode MCP response: %w (body: %s)", err, string(data))
	}
	return decoded, res.Header.Get("Mcp-Session-Id"), nil
}

func lastMCPResponseData(body []byte) []byte {
	body = bytes.TrimSpace(body)
	if len(body) == 0 || bytes.HasPrefix(body, []byte("{")) {
		return body
	}
	var last []byte
	for _, line := range bytes.Split(body, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if !bytes.HasPrefix(line, []byte("data:")) {
			continue
		}
		data := bytes.TrimSpace(bytes.TrimPrefix(line, []byte("data:")))
		if len(data) == 0 || bytes.Equal(data, []byte("[DONE]")) {
			continue
		}
		last = append(last[:0], data...)
	}
	return last
}

func chooseSubscribeToolName(names []string, explicit string) string {
	return chooseToolName(names, toolSubscribe, explicit)
}

func chooseDescribeTopicToolName(names []string, explicit string) string {
	return chooseToolName(names, toolDescribeTopic, explicit)
}

func chooseToolName(names []string, baseName string, explicit string) string {
	if explicit != "" {
		for _, name := range names {
			if name == explicit {
				return name
			}
		}
		return ""
	}
	for _, name := range names {
		if name == baseName {
			return name
		}
	}
	for _, name := range names {
		if strings.HasSuffix(name, "_"+baseName) {
			return name
		}
	}
	return ""
}

func requiredEnv(t *testing.T, key string) string {
	t.Helper()
	v := os.Getenv(key)
	if v == "" {
		t.Fatalf("%s is required", key)
	}
	return v
}
