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
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/NVIDIA/dsx-exchange/mcp/dsx-exchange-mcp/internal/auth"
)

func TestLocalLLMMCPPromptEval(t *testing.T) {
	if os.Getenv("RUN_EXCHANGE_LLM_MCP_EVAL") != "1" {
		t.Skip("set RUN_EXCHANGE_LLM_MCP_EVAL=1 to run local LLM MCP prompt eval")
	}

	model := requiredEnv(t, "DSX_EXCHANGE_LLM_MODEL")
	llm := localLLMClient{
		baseURL: strings.TrimRight(envDefault("DSX_EXCHANGE_LLM_BASE_URL", "http://127.0.0.1:11434/v1"), "/"),
		apiKey:  os.Getenv("DSX_EXCHANGE_LLM_API_KEY"),
		model:   model,
		httpc:   &http.Client{Timeout: 2 * time.Minute},
	}
	allowLiveTools := os.Getenv("DSX_EXCHANGE_LLM_EXECUTE_LIVE_TOOLS") == "1"

	mcpClient, cleanup, endpointLabel := newLLMEvalMCPClient(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	sessionID, err := mcpClient.initialize(ctx)
	if err != nil {
		t.Fatalf("initialize MCP endpoint %s failed: %v", endpointLabel, err)
	}
	if err := mcpClient.initialized(ctx, sessionID); err != nil {
		t.Fatalf("notifications/initialized failed for MCP endpoint %s: %v", endpointLabel, err)
	}

	mcpTools, err := mcpClient.listToolDefinitions(ctx, sessionID)
	if err != nil {
		t.Fatalf("tools/list failed for MCP endpoint %s: %v", endpointLabel, err)
	}
	llmTools := llmToolDefinitions(mcpTools, allowLiveTools)
	if len(llmTools) == 0 {
		t.Fatalf("MCP endpoint %s did not expose LLM-safe tools; saw %v", endpointLabel, toolDefinitionNames(mcpTools))
	}

	fixtures := selectLLMEvalFixtures(t, loadToolCallFixtures(t))
	t.Logf("running %d prompt eval fixture(s) through %s using local LLM model %q", len(fixtures), endpointLabel, model)
	t.Logf("LLM-visible tools: %v", llmToolNames(llmTools))

	for _, fixture := range fixtures {
		t.Run(fixture.ID, func(t *testing.T) {
			result, err := runLLMFixtureEval(ctx, llm, mcpClient, sessionID, llmTools, fixture, allowLiveTools)
			if err != nil {
				t.Fatalf("LLM eval failed: %v", err)
			}

			t.Logf("question: %s", fixture.Question)
			t.Logf("tool trace: %s", mustMarshalJSON(t, result.Trace))
			t.Logf("final answer: %s", result.Final.Answer)
			t.Logf("planned calls: %s", mustMarshalJSON(t, result.Final.PlannedToolCalls))

			if len(result.Trace) == 0 {
				t.Fatalf("LLM did not call any MCP tools; final content: %q", result.RawFinalContent)
			}
			if !allowLiveTools && !traceIncludesExpectedDescribe(result.Trace, fixture.ExpectedToolCalls) {
				t.Fatalf("LLM did not execute an expected %s call; trace=%s", toolDescribeTopic, mustMarshalJSON(t, result.Trace))
			}
			if missing := missingExpectedToolCalls(fixture.ExpectedToolCalls, result.Final.PlannedToolCalls); len(missing) > 0 {
				t.Fatalf("LLM final plan missing expected call(s): %s", strings.Join(missing, "; "))
			}
		})
	}
}

type llmEvalResult struct {
	Trace           []llmToolTrace
	Final           llmFinalPlan
	RawFinalContent string
}

type llmToolTrace struct {
	Step      int            `json:"step"`
	Tool      string         `json:"tool"`
	Arguments map[string]any `json:"arguments"`
	IsError   bool           `json:"is_error"`
	Summary   string         `json:"summary"`
}

type llmFinalPlan struct {
	Answer           string            `json:"answer"`
	PlannedToolCalls []fixtureToolCall `json:"planned_tool_calls"`
	Notes            []string          `json:"notes,omitempty"`
}

func newLLMEvalMCPClient(t *testing.T) (*mcpHTTPClient, func(), string) {
	t.Helper()
	if endpoint := os.Getenv("DSX_EXCHANGE_MCP_URL"); endpoint != "" {
		return &mcpHTTPClient{
			endpoint: endpoint,
			bearer:   envDefault("DSX_EXCHANGE_E2E_BEARER", "test-token"),
			httpc:    &http.Client{Timeout: 30 * time.Second},
		}, func() {}, "configured endpoint " + endpoint
	}

	srv := Build(Config{
		DefaultMaxMessages: 100,
		MaxMessages:        1000,
		DefaultDurationS:   30,
		MaxDurationS:       30,
	})
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server {
		return srv
	}, &mcp.StreamableHTTPOptions{
		Stateless:    true,
		JSONResponse: true,
	})

	mux := http.NewServeMux()
	mux.Handle("/mcp", auth.Middleware(handler))
	httpServer := httptest.NewServer(mux)

	return &mcpHTTPClient{
		endpoint: httpServer.URL + "/mcp",
		bearer:   "test-token",
		httpc:    &http.Client{Timeout: 30 * time.Second},
	}, httpServer.Close, "in-process MCP server " + httpServer.URL + "/mcp"
}

type mcpToolDefinition struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

func (c *mcpHTTPClient) listToolDefinitions(ctx context.Context, sessionID string) ([]mcpToolDefinition, error) {
	raw, _, err := c.request(ctx, sessionID, "tools/list", map[string]any{})
	if err != nil {
		return nil, err
	}
	var result struct {
		Tools []mcpToolDefinition `json:"tools"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("decode tools/list result: %w", err)
	}
	return result.Tools, nil
}

func runLLMFixtureEval(ctx context.Context, llm localLLMClient, mcpClient *mcpHTTPClient, sessionID string, tools []chatTool, fixture toolCallFixture, allowLiveTools bool) (llmEvalResult, error) {
	messages := []chatMessage{
		{Role: "system", Content: llmEvalSystemPrompt(allowLiveTools)},
		{Role: "user", Content: fmt.Sprintf("Question: %s\n\nUse the MCP tools to inspect schemas before producing the final plan.", fixture.Question)},
	}

	var trace []llmToolTrace
	maxSteps := envIntDefault("DSX_EXCHANGE_LLM_MAX_STEPS", 8)
	for step := 1; step <= maxSteps; step++ {
		response, err := llm.complete(ctx, chatCompletionRequest{
			Model:       llm.model,
			Messages:    messages,
			Tools:       tools,
			Temperature: 0,
		})
		if err != nil {
			return llmEvalResult{}, err
		}
		if len(response.Choices) == 0 {
			return llmEvalResult{}, fmt.Errorf("LLM returned no choices")
		}

		assistant := response.Choices[0].Message
		if len(assistant.ToolCalls) == 0 {
			final, err := parseLLMFinalPlan(assistant.Content)
			if err != nil {
				return llmEvalResult{Trace: trace, RawFinalContent: assistant.Content}, err
			}
			return llmEvalResult{
				Trace:           trace,
				Final:           final,
				RawFinalContent: assistant.Content,
			}, nil
		}

		messages = append(messages, assistant)
		for _, toolCall := range assistant.ToolCalls {
			args, err := decodeToolArguments(toolCall.Function.Arguments)
			if err != nil {
				return llmEvalResult{Trace: trace}, fmt.Errorf("decode LLM tool arguments for %s: %w", toolCall.Function.Name, err)
			}

			toolResult, err := mcpClient.callTool(ctx, sessionID, toolCall.Function.Name, args)
			if err != nil {
				return llmEvalResult{Trace: trace}, fmt.Errorf("MCP tools/call(%s) failed: %w", toolCall.Function.Name, err)
			}
			trace = append(trace, llmToolTrace{
				Step:      step,
				Tool:      normalizeToolName(toolCall.Function.Name),
				Arguments: args,
				IsError:   toolResult.IsError,
				Summary:   truncateForTrace(toolResult.textSummary(), 1200),
			})
			messages = append(messages, chatMessage{
				Role:       "tool",
				ToolCallID: toolCall.ID,
				Content:    truncateForTrace(toolResult.textSummary(), 5000),
			})
		}
	}
	return llmEvalResult{Trace: trace}, fmt.Errorf("LLM did not produce a final plan after %d tool-use step(s)", maxSteps)
}

func llmEvalSystemPrompt(allowLiveTools bool) string {
	liveToolInstruction := "Only execute schema-description tools. For read_retained and subscribe, include the planned calls in final JSON but do not execute them."
	if allowLiveTools {
		liveToolInstruction = "You may execute all listed MCP tools, including read_retained and subscribe, using bounded arguments."
	}
	return `You are a DSX Exchange MCP client evaluator.
Use the available MCP tools before answering. Prefer dsx_exchange_describe_topic, or the gateway-prefixed equivalent, to discover matching AsyncAPI schema channels and related metadata/value topics.
For "most recent" or snapshot-style requests, plan a retained metadata read before sampling live values when the schema exposes related metadata and value topics.
For live stream requests, plan dsx_exchange_subscribe with bounded max_messages and max_duration_s.
For get/fetch/read/sample/watch/listen/monitor/subscribe requests over live values, plan dsx_exchange_subscribe with a concrete topic_filter plus bounded max_messages and max_duration_s; use max_duration_s=30 unless the deployment documents a higher cap. When the MCP client supports a background agent, subagent, task, or equivalent mechanism, use it by default for every dsx_exchange_subscribe call so the active chat remains usable during the sampling window; direct tools/call may block the active chat until the subscribe returns.
` + liveToolInstruction + `

Final response requirements:
- Return one strict JSON object and no markdown.
- JSON shape: {"answer":"brief user-facing summary","planned_tool_calls":[{"tool":"dsx_exchange_describe_topic","arguments":{"topic_filter":"..."}},{"tool":"dsx_exchange_read_retained","arguments":{"topic_filter":"...","max_messages":1000}},{"tool":"dsx_exchange_subscribe","arguments":{"topic_filter":"...","max_messages":100,"max_duration_s":30}}],"notes":["optional caveat"]}
- For background-watch requests, include {"tool":"dsx_exchange_subscribe","arguments":{"topic_filter":"...","max_messages":100,"max_duration_s":30}} in planned_tool_calls.
- Use unprefixed canonical tool names in planned_tool_calls even if the MCP endpoint exposes gateway-prefixed names.
- Do not invent raw data values. This eval is about choosing the right tools and topic filters.`
}

type localLLMClient struct {
	baseURL string
	apiKey  string
	model   string
	httpc   *http.Client
}

type chatCompletionRequest struct {
	Model       string        `json:"model"`
	Messages    []chatMessage `json:"messages"`
	Tools       []chatTool    `json:"tools,omitempty"`
	Temperature float64       `json:"temperature"`
}

type chatCompletionResponse struct {
	Choices []struct {
		Message chatMessage `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error,omitempty"`
}

type chatMessage struct {
	Role       string        `json:"role"`
	Content    string        `json:"content,omitempty"`
	ToolCalls  []llmToolCall `json:"tool_calls,omitempty"`
	ToolCallID string        `json:"tool_call_id,omitempty"`
}

type llmToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type chatTool struct {
	Type     string       `json:"type"`
	Function chatFunction `json:"function"`
}

type chatFunction struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters"`
}

func (c localLLMClient) complete(ctx context.Context, req chatCompletionRequest) (chatCompletionResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return chatCompletionResponse{}, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return chatCompletionResponse{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.httpc.Do(httpReq)
	if err != nil {
		return chatCompletionResponse{}, fmt.Errorf("call local LLM API at %s: %w", c.baseURL, err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return chatCompletionResponse{}, err
	}
	var decoded chatCompletionResponse
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return chatCompletionResponse{}, fmt.Errorf("decode local LLM response: %w (body: %s)", err, string(raw))
	}
	if resp.StatusCode >= http.StatusBadRequest {
		if decoded.Error != nil {
			return chatCompletionResponse{}, fmt.Errorf("local LLM http %d: %s", resp.StatusCode, decoded.Error.Message)
		}
		return chatCompletionResponse{}, fmt.Errorf("local LLM http %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	return decoded, nil
}

func llmToolDefinitions(tools []mcpToolDefinition, allowLiveTools bool) []chatTool {
	out := make([]chatTool, 0, len(tools))
	for _, tool := range tools {
		normalized := normalizeToolName(tool.Name)
		if normalized != toolDescribeTopic && !(allowLiveTools && (normalized == toolReadRetained || normalized == toolSubscribe)) {
			continue
		}
		parameters := tool.InputSchema
		if parameters == nil {
			parameters = map[string]any{"type": "object"}
		}
		out = append(out, chatTool{
			Type: "function",
			Function: chatFunction{
				Name:        tool.Name,
				Description: tool.Description,
				Parameters:  parameters,
			},
		})
	}
	return out
}

func parseLLMFinalPlan(content string) (llmFinalPlan, error) {
	raw, err := extractJSONObject(content)
	if err != nil {
		return llmFinalPlan{}, err
	}
	var plan llmFinalPlan
	if err := json.Unmarshal(raw, &plan); err != nil {
		return llmFinalPlan{}, fmt.Errorf("decode final JSON plan: %w (content: %s)", err, content)
	}
	for i := range plan.PlannedToolCalls {
		plan.PlannedToolCalls[i].Tool = normalizeToolName(plan.PlannedToolCalls[i].Tool)
	}
	if len(plan.PlannedToolCalls) == 0 {
		return llmFinalPlan{}, fmt.Errorf("final JSON contained no planned_tool_calls: %s", content)
	}
	return plan, nil
}

func extractJSONObject(content string) ([]byte, error) {
	start := strings.Index(content, "{")
	end := strings.LastIndex(content, "}")
	if start < 0 || end < start {
		return nil, fmt.Errorf("final response did not contain a JSON object: %q", content)
	}
	return []byte(content[start : end+1]), nil
}

func decodeToolArguments(raw string) (map[string]any, error) {
	if strings.TrimSpace(raw) == "" {
		return map[string]any{}, nil
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(raw), &args); err != nil {
		return nil, err
	}
	return args, nil
}

func missingExpectedToolCalls(expected, actual []fixtureToolCall) []string {
	var missing []string
	for _, want := range expected {
		found := false
		for _, got := range actual {
			if toolCallsEquivalent(want, got) {
				found = true
				break
			}
		}
		if !found {
			missing = append(missing, fmt.Sprintf("%s %s", want.Tool, mustMarshalComparable(want.Arguments)))
		}
	}
	return missing
}

func toolCallsEquivalent(want, got fixtureToolCall) bool {
	if normalizeToolName(want.Tool) != normalizeToolName(got.Tool) {
		return false
	}
	return canonicalArgs(want.Arguments) == canonicalArgs(got.Arguments)
}

func canonicalArgs(args map[string]any) string {
	normalized := make(map[string]any, len(args))
	for key, value := range args {
		switch number := value.(type) {
		case float64:
			if number == float64(int(number)) {
				normalized[key] = int(number)
			} else {
				normalized[key] = number
			}
		default:
			normalized[key] = value
		}
	}
	raw, _ := json.Marshal(normalized)
	return string(raw)
}

func traceIncludesExpectedDescribe(trace []llmToolTrace, expected []fixtureToolCall) bool {
	for _, want := range expected {
		if normalizeToolName(want.Tool) != toolDescribeTopic {
			continue
		}
		wantFilter, _ := want.Arguments["topic_filter"].(string)
		for _, got := range trace {
			gotFilter, _ := got.Arguments["topic_filter"].(string)
			if got.Tool == toolDescribeTopic && gotFilter == wantFilter && !got.IsError {
				return true
			}
		}
	}
	return false
}

func normalizeToolName(name string) string {
	for _, canonical := range []string{
		toolDescribeTopic,
		toolFindTopics,
		toolReadRetained,
		toolSubscribe,
	} {
		if name == canonical || strings.HasSuffix(name, "_"+canonical) {
			return canonical
		}
	}
	return name
}

func selectLLMEvalFixtures(t *testing.T, fixtures []toolCallFixture) []toolCallFixture {
	t.Helper()
	requested := os.Getenv("DSX_EXCHANGE_LLM_EVAL_CASES")
	if requested == "" {
		if len(fixtures) > 1 {
			return fixtures[:1]
		}
		return fixtures
	}
	wanted := map[string]bool{}
	for _, id := range strings.Split(requested, ",") {
		id = strings.TrimSpace(id)
		if id != "" {
			wanted[id] = true
		}
	}
	var selected []toolCallFixture
	for _, fixture := range fixtures {
		if wanted[fixture.ID] {
			selected = append(selected, fixture)
			delete(wanted, fixture.ID)
		}
	}
	if len(wanted) > 0 {
		t.Fatalf("unknown DSX_EXCHANGE_LLM_EVAL_CASES fixture id(s): %v", wanted)
	}
	return selected
}

func toolDefinitionNames(tools []mcpToolDefinition) []string {
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		names = append(names, tool.Name)
	}
	return names
}

func llmToolNames(tools []chatTool) []string {
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		names = append(names, tool.Function.Name)
	}
	return names
}

func truncateForTrace(s string, max int) string {
	s = strings.TrimSpace(s)
	if len(s) <= max {
		return s
	}
	return s[:max] + "...<truncated>"
}

func envDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func envIntDefault(key string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	var parsed int
	if _, err := fmt.Sscanf(value, "%d", &parsed); err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}

func mustMarshalJSON(t *testing.T, value any) string {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal JSON: %v", err)
	}
	return string(raw)
}

func mustMarshalComparable(value any) string {
	raw, _ := json.Marshal(value)
	return string(raw)
}
