// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/NVIDIA/dsx-exchange/mcp/dsx-exchange-mcp/internal/auth"
	"github.com/NVIDIA/dsx-exchange/mcp/dsx-exchange-mcp/internal/mqttbus"
)

func TestApplyLimitsDefaultsAndCaps(t *testing.T) {
	cfg := Config{
		DefaultMaxMessages: 10,
		MaxMessages:        20,
		DefaultDurationS:   5,
		MaxDurationS:       30,
	}

	msgs, dur, err := applyLimits(cfg, 0, 0)
	if err != nil {
		t.Fatalf("applyLimits defaults failed: %v", err)
	}
	if msgs != 10 || dur != 5 {
		t.Fatalf("defaults = (%d,%d), want (10,5)", msgs, dur)
	}

	_, _, err = applyLimits(cfg, 21, 5)
	if got := mqttbus.ErrorCode(err); got != mqttbus.CodeInvalidArgument {
		t.Fatalf("max message cap code = %q, want %q", got, mqttbus.CodeInvalidArgument)
	}

	_, _, err = applyLimits(cfg, 10, 31)
	if got := mqttbus.ErrorCode(err); got != mqttbus.CodeInvalidArgument {
		t.Fatalf("duration cap code = %q, want %q", got, mqttbus.CodeInvalidArgument)
	}
}

func TestCollectToolAdmissionLimitFailsFast(t *testing.T) {
	cfg := Config{
		DefaultMaxMessages: 10,
		MaxMessages:        20,
		DefaultDurationS:   5,
		MaxDurationS:       30,
	}
	normalizeConfig(&cfg)
	cfg.collectAdmission = newAdmissionLimiter(1)
	if !cfg.collectAdmission.tryAcquire() {
		t.Fatal("pre-acquire collect admission failed")
	}
	ctx := auth.WithCaller(context.Background(), auth.Caller{
		Bearer:    "token",
		SessionID: "session-1",
	})

	result, _, err := collectTool(ctx, cfg, toolSubscribe, "BMS/v1/PUB/Value/Rack/RackPower/#", 1, 1, false)
	if err != nil {
		t.Fatalf("collectTool returned transport error: %v", err)
	}
	if result == nil || !result.IsError {
		t.Fatalf("collectTool IsError = %v, want true", result != nil && result.IsError)
	}
	text, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("error content type = %T, want *mcp.TextContent", result.Content[0])
	}
	var body structuredError
	if err := json.Unmarshal([]byte(text.Text), &body); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	if body.Error.Code != mqttbus.CodeMQTTAdmissionLimited || body.Error.RetryAfterSeconds != 1 {
		t.Fatalf("error body = %#v, want mqtt_admission_limited with retry_after_seconds=1", body.Error)
	}
}

func TestDescribeTopicToolMatchesSchema(t *testing.T) {
	_, out, err := describeTopicTool(context.Background(), describeTopicInput{
		TopicFilter: "BMS/v1/PUB/Value/Rack/RackLiquidIsolationStatus/#",
	})
	if err != nil {
		t.Fatalf("describeTopicTool returned transport error: %v", err)
	}
	if out.Count == 0 {
		t.Fatal("describeTopicTool returned no matches")
	}
	if got := out.Matches[0].RelatedTopics[0].TopicFilter; got != "BMS/v1/PUB/Metadata/Rack/RackLiquidIsolationStatus/#" {
		t.Fatalf("related metadata topic = %q", got)
	}
}

func TestDescribeTopicToolRequiresFilter(t *testing.T) {
	result, _, err := describeTopicTool(context.Background(), describeTopicInput{})
	if err != nil {
		t.Fatalf("describeTopicTool returned transport error: %v", err)
	}
	if result == nil || !result.IsError {
		t.Fatalf("describeTopicTool empty filter IsError = %v, want true", result != nil && result.IsError)
	}
}

func TestFindTopicsToolMatchesSelector(t *testing.T) {
	cfg := Config{}
	normalizeConfig(&cfg)
	_, out, err := findTopicsTool(context.Background(), cfg, findTopicsInput{
		Domain:     "bms",
		Role:       "value",
		ObjectType: "Rack",
		PointType:  "RackLiquidIsolationStatus",
	})
	if err != nil {
		t.Fatalf("findTopicsTool returned transport error: %v", err)
	}
	if out.Count != 1 {
		t.Fatalf("findTopicsTool count = %d, want 1: %#v", out.Count, out.Matches)
	}
	if got := out.Matches[0].TopicFilter; got != "BMS/v1/PUB/Value/Rack/RackLiquidIsolationStatus/#" {
		t.Fatalf("topic filter = %q, want RackLiquidIsolationStatus value filter", got)
	}
}

type toolCallFixture struct {
	ID                string             `json:"id"`
	Domain            string             `json:"domain"`
	Question          string             `json:"question"`
	ExpectedToolCalls []fixtureToolCall  `json:"expected_tool_calls"`
	ExpectedSchema    fixtureSchemaCheck `json:"expected_schema"`
}

type fixtureToolCall struct {
	Tool      string         `json:"tool"`
	Arguments map[string]any `json:"arguments"`
}

type fixtureSchemaCheck struct {
	Domain        string   `json:"domain"`
	Channels      []string `json:"channels"`
	RelatedTopics []string `json:"related_topics"`
}

func TestToolCallExpectationFixtures(t *testing.T) {
	raw, err := os.ReadFile("testdata/tool_call_expectations.json")
	if err != nil {
		t.Fatalf("read tool-call fixture: %v", err)
	}
	var fixtures []toolCallFixture
	if err := json.Unmarshal(raw, &fixtures); err != nil {
		t.Fatalf("unmarshal tool-call fixture: %v", err)
	}
	if len(fixtures) == 0 {
		t.Fatal("tool-call fixture is empty")
	}

	cfg := Config{
		DefaultMaxMessages: 100,
		MaxMessages:        1000,
		DefaultDurationS:   30,
		MaxDurationS:       30,
	}
	normalizeConfig(&cfg)

	for _, fixture := range fixtures {
		t.Run(fixture.ID, func(t *testing.T) {
			if fixture.Question == "" {
				t.Fatal("fixture question is required")
			}
			if fixture.Domain == "" {
				t.Fatal("fixture domain is required")
			}
			if len(fixture.ExpectedToolCalls) == 0 {
				t.Fatal("expected_tool_calls is required")
			}

			seenChannels := map[string]bool{}
			seenRelatedTopics := map[string]bool{}
			described := false

			for i, call := range fixture.ExpectedToolCalls {
				topicFilter := stringArg(t, fixture.ID, i, call.Arguments, "topic_filter")
				if err := mqttbus.ValidateTopicFilter(topicFilter); err != nil {
					t.Fatalf("call %d %s topic_filter %q is invalid: %v", i, call.Tool, topicFilter, err)
				}

				switch call.Tool {
				case toolDescribeTopic:
					described = true
					result, out, err := describeTopicTool(context.Background(), describeTopicInput{TopicFilter: topicFilter})
					if err != nil {
						t.Fatalf("describe topic transport error for %q: %v", topicFilter, err)
					}
					if result == nil || result.IsError {
						t.Fatalf("describe topic result for %q is error: %#v", topicFilter, result)
					}
					if out.Count == 0 {
						t.Fatalf("describe topic returned no schema matches for %q", topicFilter)
					}
					for _, match := range out.Matches {
						if match.Domain == fixture.ExpectedSchema.Domain {
							seenChannels[match.Channel] = true
						}
						for _, related := range match.RelatedTopics {
							seenRelatedTopics[related.TopicFilter] = true
						}
					}
				case toolReadRetained:
					maxMessages := intArg(t, fixture.ID, i, call.Arguments, "max_messages")
					if _, _, err := applyLimits(cfg, maxMessages, cfg.DefaultDurationS); err != nil {
						t.Fatalf("read_retained limits invalid for %q: %v", topicFilter, err)
					}
				case toolSubscribe:
					maxMessages := intArg(t, fixture.ID, i, call.Arguments, "max_messages")
					maxDurationS := intArg(t, fixture.ID, i, call.Arguments, "max_duration_s")
					if _, _, err := applyLimits(cfg, maxMessages, maxDurationS); err != nil {
						t.Fatalf("subscribe limits invalid for %q: %v", topicFilter, err)
					}
				default:
					t.Fatalf("call %d has unknown tool %q", i, call.Tool)
				}
			}

			if !described {
				t.Fatal("fixture must include at least one dsx_exchange_describe_topic call")
			}
			for _, channel := range fixture.ExpectedSchema.Channels {
				if !seenChannels[channel] {
					t.Fatalf("expected schema channel %q was not observed; saw %#v", channel, seenChannels)
				}
			}
			for _, topic := range fixture.ExpectedSchema.RelatedTopics {
				if !seenRelatedTopics[topic] {
					t.Fatalf("expected related topic %q was not observed; saw %#v", topic, seenRelatedTopics)
				}
			}
		})
	}
}

func TestMCPClientListsAndCallsDescribeTopic(t *testing.T) {
	fixtures := loadToolCallFixtures(t)
	session, cleanup := newTestMCPClient(t)
	defer cleanup()

	tools, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools failed: %v", err)
	}
	toolNames := map[string]bool{}
	for _, tool := range tools.Tools {
		toolNames[tool.Name] = true
	}
	for _, name := range []string{toolDescribeTopic, toolFindTopics, toolReadRetained, toolSubscribe} {
		if !toolNames[name] {
			t.Fatalf("ListTools did not expose %q; saw %#v", name, toolNames)
		}
	}

	subscribeTool := toolByName(t, tools.Tools, toolSubscribe)
	if got := subscribeTool.Meta["x-dsx-exchange-background-preferred"]; got != true {
		t.Fatalf("%s metadata background-preferred = %#v, want true", toolSubscribe, got)
	}
	if got := subscribeTool.Meta["x-dsx-exchange-bounded-window"]; got != true {
		t.Fatalf("%s metadata bounded-window = %#v, want true", toolSubscribe, got)
	}

	for _, fixture := range fixtures {
		t.Run(fixture.ID, func(t *testing.T) {
			for i, call := range fixture.ExpectedToolCalls {
				if call.Tool != toolDescribeTopic {
					continue
				}
				topicFilter := stringArg(t, fixture.ID, i, call.Arguments, "topic_filter")
				result, err := session.CallTool(context.Background(), &mcp.CallToolParams{
					Name:      toolDescribeTopic,
					Arguments: call.Arguments,
				})
				if err != nil {
					t.Fatalf("CallTool(%s, %q) returned client error: %v", toolDescribeTopic, topicFilter, err)
				}
				if result.IsError {
					t.Fatalf("CallTool(%s, %q) returned tool error: %s", toolDescribeTopic, topicFilter, textContentSummary(result))
				}

				var out describeTopicOutput
				if err := json.Unmarshal([]byte(lastTextContent(t, result)), &out); err != nil {
					t.Fatalf("decode CallTool(%s, %q) JSON content: %v", toolDescribeTopic, topicFilter, err)
				}
				if out.TopicFilter != topicFilter {
					t.Fatalf("MCP result topic_filter = %q, want %q", out.TopicFilter, topicFilter)
				}
				if out.Count == 0 {
					t.Fatalf("MCP result for %q has no schema matches", topicFilter)
				}
				if !hasDomainChannel(out, fixture.ExpectedSchema.Domain, fixture.ExpectedSchema.Channels) {
					t.Fatalf("MCP result for %q missing expected domain/channel; result=%#v", topicFilter, out.Matches)
				}
			}
		})
	}
}

func toolByName(t *testing.T, tools []*mcp.Tool, name string) *mcp.Tool {
	t.Helper()
	for _, tool := range tools {
		if tool.Name == name {
			return tool
		}
	}
	t.Fatalf("tools/list did not expose %q", name)
	return nil
}

func TestMCPClientDescribeTopicInvalidFilterReturnsToolError(t *testing.T) {
	session, cleanup := newTestMCPClient(t)
	defer cleanup()

	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: toolDescribeTopic,
		Arguments: map[string]any{
			"topic_filter": "BMS/#/bad",
		},
	})
	if err != nil {
		t.Fatalf("CallTool invalid filter returned client/protocol error: %v", err)
	}
	if !result.IsError {
		t.Fatalf("CallTool invalid filter IsError=false; content=%s", textContentSummary(result))
	}
	if got := textContentSummary(result); !strings.Contains(got, mqttbus.CodeInvalidTopicFilter) {
		t.Fatalf("invalid filter error content = %q, want code %q", got, mqttbus.CodeInvalidTopicFilter)
	}
}

func stringArg(t *testing.T, fixtureID string, callIndex int, args map[string]any, key string) string {
	t.Helper()
	value, ok := args[key]
	if !ok {
		t.Fatalf("%s call %d missing %q", fixtureID, callIndex, key)
	}
	str, ok := value.(string)
	if !ok || str == "" {
		t.Fatalf("%s call %d %q = %#v, want non-empty string", fixtureID, callIndex, key, value)
	}
	return str
}

func intArg(t *testing.T, fixtureID string, callIndex int, args map[string]any, key string) int {
	t.Helper()
	value, ok := args[key]
	if !ok {
		t.Fatalf("%s call %d missing %q", fixtureID, callIndex, key)
	}
	number, ok := value.(float64)
	if !ok || number != float64(int(number)) {
		t.Fatalf("%s call %d %q = %#v, want integer", fixtureID, callIndex, key, value)
	}
	return int(number)
}

func loadToolCallFixtures(t *testing.T) []toolCallFixture {
	t.Helper()
	raw, err := os.ReadFile("testdata/tool_call_expectations.json")
	if err != nil {
		t.Fatalf("read tool-call fixture: %v", err)
	}
	var fixtures []toolCallFixture
	if err := json.Unmarshal(raw, &fixtures); err != nil {
		t.Fatalf("unmarshal tool-call fixture: %v", err)
	}
	if len(fixtures) == 0 {
		t.Fatal("tool-call fixture is empty")
	}
	return fixtures
}

func newTestMCPClient(t *testing.T) (*mcp.ClientSession, func()) {
	t.Helper()
	return newTestMCPClientWithConfig(t, Config{
		DefaultMaxMessages: 100,
		MaxMessages:        1000,
		DefaultDurationS:   30,
		MaxDurationS:       30,
	})
}

func newTestMCPClientWithConfig(t *testing.T, cfg Config) (*mcp.ClientSession, func()) {
	t.Helper()
	srv := Build(cfg)
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server {
		return srv
	}, &mcp.StreamableHTTPOptions{
		Stateless:    true,
		JSONResponse: true,
	})

	mux := http.NewServeMux()
	mux.Handle("/mcp", auth.Middleware(handler))
	httpServer := httptest.NewServer(mux)

	client := mcp.NewClient(&mcp.Implementation{
		Name:    "dsx-exchange-mcp-test-client",
		Version: "0.1.0",
	}, nil)
	transport := &mcp.StreamableClientTransport{
		Endpoint:             httpServer.URL + "/mcp",
		HTTPClient:           &http.Client{Transport: authHeaderTransport{base: http.DefaultTransport}},
		DisableStandaloneSSE: true,
	}
	session, err := client.Connect(context.Background(), transport, nil)
	if err != nil {
		httpServer.Close()
		t.Fatalf("connect MCP test client: %v", err)
	}
	cleanup := func() {
		_ = session.Close()
		httpServer.Close()
	}
	return session, cleanup
}

type authHeaderTransport struct {
	base http.RoundTripper
}

func (t authHeaderTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}
	req.Header.Set("Authorization", "Bearer test-token")
	req.Header.Set("x-mcp-tenant", "test-tenant")
	req.Header.Set("x-mcp-sub", "test-subject")
	return base.RoundTrip(req)
}

func lastTextContent(t *testing.T, result *mcp.CallToolResult) string {
	t.Helper()
	for i := len(result.Content) - 1; i >= 0; i-- {
		if text, ok := result.Content[i].(*mcp.TextContent); ok {
			return text.Text
		}
	}
	t.Fatalf("tool result contains no text content: %#v", result.Content)
	return ""
}

func textContentSummary(result *mcp.CallToolResult) string {
	var parts []string
	for _, content := range result.Content {
		if text, ok := content.(*mcp.TextContent); ok {
			parts = append(parts, text.Text)
		}
	}
	return strings.Join(parts, "\n")
}

func hasDomainChannel(out describeTopicOutput, domain string, channels []string) bool {
	wanted := map[string]bool{}
	for _, channel := range channels {
		wanted[channel] = true
	}
	for _, match := range out.Matches {
		if match.Domain == domain && wanted[match.Channel] {
			return true
		}
	}
	return false
}

func TestCollectOutputZeroMessagesJSON(t *testing.T) {
	raw, err := json.Marshal(collectOutput{
		Messages: []mqttbus.Message{},
	})
	if err != nil {
		t.Fatalf("marshal collectOutput: %v", err)
	}
	if got, want := string(raw), `{"messages":[],"count":0,"duration_ms":0,"stopped_reason":"","truncated":false}`; got != want {
		t.Fatalf("collectOutput JSON = %s, want %s", got, want)
	}
}
