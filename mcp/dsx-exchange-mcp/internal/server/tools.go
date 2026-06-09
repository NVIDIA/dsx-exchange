// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/NVIDIA/dsx-exchange/mcp/dsx-exchange-mcp/internal/auth"
	"github.com/NVIDIA/dsx-exchange/mcp/dsx-exchange-mcp/internal/mqttbus"
	"github.com/NVIDIA/dsx-exchange/mcp/dsx-exchange-mcp/internal/schemaindex"
)

const (
	toolSubscribe     = "dsx_exchange_subscribe"
	toolReadRetained  = "dsx_exchange_read_retained"
	toolDescribeTopic = "dsx_exchange_describe_topic"
	toolFindTopics    = "dsx_exchange_find_topics"
)

type subscribeInput struct {
	TopicFilter  string `json:"topic_filter" jsonschema:"MQTT topic filter for live messages; supports + and # wildcards. For BMS values use BMS/v1/PUB/Value/# or a specific AsyncAPI value path such as BMS/v1/PUB/Value/Rack/RackLiquidIsolationStatus/#."`
	MaxMessages  int    `json:"max_messages,omitempty" jsonschema:"stop after this many messages (default 100)"`
	MaxDurationS int    `json:"max_duration_s,omitempty" jsonschema:"stop after this many seconds (default 30; use the max when waiting for sparse live values)"`
}

type readRetainedInput struct {
	TopicFilter string `json:"topic_filter" jsonschema:"MQTT topic filter to read retained messages from. For BMS, use Metadata paths such as BMS/v1/PUB/Metadata/#. Do not use this for live Value paths; use dsx_exchange_subscribe instead."`
	MaxMessages int    `json:"max_messages,omitempty" jsonschema:"safety cap on returned messages (default 1000)"`
}

type describeTopicInput struct {
	TopicFilter string `json:"topic_filter" jsonschema:"MQTT topic filter or concrete topic to explain using embedded AsyncAPI schemas. Example: BMS/v1/PUB/Value/Rack/RackLiquidIsolationStatus/#."`
}

type findTopicsInput struct {
	Domain          string `json:"domain,omitempty" jsonschema:"Optional AsyncAPI domain, for example bms, power-management, nico, or spiffe-exchange."`
	Query           string `json:"query,omitempty" jsonschema:"Optional free-text search over AsyncAPI domain, channel, address, description, operations, and message summaries."`
	Role            string `json:"role,omitempty" jsonschema:"Optional topic role hint: metadata, value, or event."`
	ObjectType      string `json:"object_type,omitempty" jsonschema:"Optional BMS object type such as Rack, CDU, System, AHU, or Chiller."`
	PointType       string `json:"point_type,omitempty" jsonschema:"Optional BMS point type such as RackLiquidIsolationStatus, RackPower, or RackLeakDetectTray."`
	OperationAction string `json:"operation_action,omitempty" jsonschema:"Optional AsyncAPI operation action filter such as send or receive."`
	Limit           int    `json:"limit,omitempty" jsonschema:"Maximum schema topics to return."`
}

type collectOutput struct {
	Messages      []mqttbus.Message `json:"messages"`
	Count         int               `json:"count"`
	DurationMS    int64             `json:"duration_ms"`
	StoppedReason string            `json:"stopped_reason"`
	Truncated     bool              `json:"truncated"`
}

type describeTopicOutput struct {
	TopicFilter string              `json:"topic_filter"`
	Count       int                 `json:"count"`
	Matches     []schemaindex.Topic `json:"matches"`
}

type findTopicsOutput struct {
	Count   int                 `json:"count"`
	Matches []schemaindex.Topic `json:"matches"`
}

type structuredError struct {
	Error errorBody `json:"error"`
}

type errorBody struct {
	Code              string `json:"code"`
	Message           string `json:"message"`
	RetryAfterSeconds int    `json:"retry_after_seconds,omitempty"`
}

func registerTools(s *mcp.Server, cfg Config, watches *watchManager) {
	mcp.AddTool(s, &mcp.Tool{
		Name: toolSubscribe,
		Description: "Subscribe to a DSX Exchange MQTT topic filter and return live messages " +
			"received within bounded limits. Use this for BMS Value channels; BMS live value paths " +
			"are under BMS/v1/PUB/Value/{objectType}/{pointType}/{tagPath}. Good discovery filters " +
			"are BMS/v1/PUB/Value/# and BMS/v1/PUB/Value/Rack/RackLiquidIsolationStatus/#. " +
			"Consult dsx-exchange://specs/* before inventing topic segments such as Data or Telemetry. " +
			"The caller bearer is passed to MQTT as the configured OAuth username/password=<bearer>; " +
			"DSX Exchange auth-callout enforces OAuth2 validity and topic ACLs.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in subscribeInput) (*mcp.CallToolResult, collectOutput, error) {
		maxMessages := in.MaxMessages
		durationS := in.MaxDurationS
		return collectTool(ctx, cfg, toolSubscribe, in.TopicFilter, maxMessages, durationS, false)
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: toolReadRetained,
		Description: "Read currently-retained messages on a DSX Exchange MQTT topic filter. " +
			"Use this for retained BMS Metadata, for example BMS/v1/PUB/Metadata/#, before " +
			"deriving specific value topics. Do not use this tool for BMS live Value channels; " +
			"a zero-count retained_idle result means no retained messages matched that filter. " +
			"The caller bearer is passed to MQTT as the configured OAuth username/password=<bearer>.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in readRetainedInput) (*mcp.CallToolResult, collectOutput, error) {
		return collectTool(ctx, cfg, toolReadRetained, in.TopicFilter, in.MaxMessages, cfg.DefaultDurationS, true)
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: toolDescribeTopic,
		Description: "Schema Exploration: describe the AsyncAPI channel matching a DSX Exchange topic filter. " +
			"Returns the schema channel, payload shape, retained/live behavior, examples, and related metadata/value topics. " +
			"Use this before subscribing when the caller knows roughly which MQTT path they want but needs schema context.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in describeTopicInput) (*mcp.CallToolResult, describeTopicOutput, error) {
		start := time.Now()
		if cfg.Metrics != nil {
			cfg.Metrics.BeginToolCall()
			defer cfg.Metrics.EndToolCall()
		}
		result, out, err := describeTopicTool(ctx, in)
		if cfg.Metrics != nil {
			cfg.Metrics.RecordToolCall(toolDescribeTopic, toolResultErrorCode(result, err), "", time.Since(start), out.Count)
		}
		return result, out, err
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: toolFindTopics,
		Description: "Schema Exploration: find AsyncAPI-derived DSX Exchange MQTT topic filters by domain, text query, role, object type, point type, or operation action. " +
			"Use this before starting a long-running subscription when the caller describes a domain or signal but does not know the raw MQTT topic path. " +
			"Returned topic filters still require broker ACL approval when used by MQTT tools.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in findTopicsInput) (*mcp.CallToolResult, findTopicsOutput, error) {
		start := time.Now()
		if cfg.Metrics != nil {
			cfg.Metrics.BeginToolCall()
			defer cfg.Metrics.EndToolCall()
		}
		result, out, err := findTopicsTool(ctx, cfg, in)
		if cfg.Metrics != nil {
			cfg.Metrics.RecordToolCall(toolFindTopics, toolResultErrorCode(result, err), "", time.Since(start), out.Count)
		}
		return result, out, err
	})

	registerWatchTools(s, cfg, watches)
}

func describeTopicTool(ctx context.Context, in describeTopicInput) (*mcp.CallToolResult, describeTopicOutput, error) {
	topicFilter := strings.TrimSpace(in.TopicFilter)
	if topicFilter == "" {
		return describeTopicError(mqttbus.CodeInvalidTopicFilter, "topic_filter is required")
	}
	if err := mqttbus.ValidateTopicFilter(topicFilter); err != nil {
		return describeTopicError(mqttbus.ErrorCode(err), publicMessage(err))
	}

	idx, err := schemaindex.Default()
	if err != nil {
		return describeTopicError(mqttbus.CodeInternalError, err.Error())
	}

	matches := idx.Describe(topicFilter)
	out := describeTopicOutput{
		TopicFilter: topicFilter,
		Count:       len(matches),
		Matches:     matches,
	}
	raw, _ := json.Marshal(out)
	slog.Info("schema topic description",
		"audit", true,
		"tool", toolDescribeTopic,
		"caller_tenant", auth.FromContext(ctx).Tenant,
		"caller_subject", auth.FromContext(ctx).Subject,
		"topic_filter", topicFilter,
		"match_count", out.Count,
	)
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: fmt.Sprintf("matched %d schema channels", out.Count)},
			&mcp.TextContent{Text: string(raw)},
		},
	}, out, nil
}

func describeTopicError(code, message string) (*mcp.CallToolResult, describeTopicOutput, error) {
	if code == "" {
		code = mqttbus.CodeInternalError
	}
	body := structuredError{Error: errorBody{Code: code, Message: message}}
	raw, _ := json.Marshal(body)
	return &mcp.CallToolResult{
		IsError: true,
		Content: []mcp.Content{&mcp.TextContent{Text: string(raw)}},
	}, describeTopicOutput{}, nil
}

func findTopicsTool(ctx context.Context, cfg Config, in findTopicsInput) (*mcp.CallToolResult, findTopicsOutput, error) {
	limit, err := applyFindTopicsLimit(cfg, in.Limit)
	if err != nil {
		return findTopicsError(mqttbus.ErrorCode(err), publicMessage(err))
	}
	idx, err := schemaindex.Default()
	if err != nil {
		return findTopicsError(mqttbus.CodeInternalError, err.Error())
	}
	matches := idx.Search(schemaindex.SearchOptions{
		Domain:          in.Domain,
		Query:           in.Query,
		Role:            in.Role,
		ObjectType:      in.ObjectType,
		PointType:       in.PointType,
		OperationAction: in.OperationAction,
		Limit:           limit,
	})
	out := findTopicsOutput{
		Count:   len(matches),
		Matches: matches,
	}
	raw, _ := json.Marshal(out)
	slog.Info("schema topic search",
		"audit", true,
		"tool", toolFindTopics,
		"caller_tenant", auth.FromContext(ctx).Tenant,
		"caller_subject", auth.FromContext(ctx).Subject,
		"domain", strings.TrimSpace(in.Domain),
		"query", strings.TrimSpace(in.Query),
		"role", strings.TrimSpace(in.Role),
		"object_type", strings.TrimSpace(in.ObjectType),
		"point_type", strings.TrimSpace(in.PointType),
		"match_count", out.Count,
	)
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: fmt.Sprintf("matched %d schema topics", out.Count)},
			&mcp.TextContent{Text: string(raw)},
		},
	}, out, nil
}

func findTopicsError(code, message string) (*mcp.CallToolResult, findTopicsOutput, error) {
	if code == "" {
		code = mqttbus.CodeInternalError
	}
	body := structuredError{Error: errorBody{Code: code, Message: message}}
	raw, _ := json.Marshal(body)
	return &mcp.CallToolResult{
		IsError: true,
		Content: []mcp.Content{&mcp.TextContent{Text: string(raw)}},
	}, findTopicsOutput{}, nil
}

func collectTool(
	ctx context.Context,
	cfg Config,
	tool, topicFilter string,
	maxMessages int,
	durationS int,
	retainedOnly bool,
) (*mcp.CallToolResult, collectOutput, error) {
	start := time.Now()
	caller := auth.FromContext(ctx)
	if cfg.Metrics != nil {
		cfg.Metrics.BeginToolCall()
		defer cfg.Metrics.EndToolCall()
	}

	maxMessages, durationS, err := applyLimits(cfg, maxMessages, durationS)
	if err != nil {
		return finishTool(tool, caller, topicFilter, maxMessages, durationS, start, collectOutput{}, err, cfg)
	}

	if !cfg.collectAdmission.tryAcquire() {
		return finishTool(tool, caller, topicFilter, maxMessages, durationS, start, collectOutput{}, admissionLimitedError(), cfg)
	}
	defer cfg.collectAdmission.release()

	result, err := mqttbus.Collect(ctx, cfg.MQTT, caller.Bearer, topicFilter, maxMessages, time.Duration(durationS)*time.Second, retainedOnly)
	out := collectOutput{
		Messages:      append([]mqttbus.Message{}, result.Messages...),
		Count:         len(result.Messages),
		DurationMS:    result.Duration.Milliseconds(),
		StoppedReason: result.StoppedReason,
		Truncated:     result.Truncated,
	}
	return finishTool(tool, caller, topicFilter, maxMessages, durationS, start, out, err, cfg)
}

func finishTool(
	tool string,
	caller auth.Caller,
	topicFilter string,
	maxMessages int,
	durationS int,
	start time.Time,
	out collectOutput,
	err error,
	cfg Config,
) (*mcp.CallToolResult, collectOutput, error) {
	duration := time.Since(start)
	if out.DurationMS == 0 {
		out.DurationMS = duration.Milliseconds()
	}

	code := errorCode(err)
	if cfg.Metrics != nil {
		cfg.Metrics.RecordToolCall(tool, code, out.StoppedReason, duration, out.Count)
	}
	auditToolCall(tool, caller, topicFilter, maxMessages, durationS, out, duration, code)

	if err != nil {
		body := structuredError{Error: errorBody{Code: code, Message: publicMessage(err), RetryAfterSeconds: retryAfterSeconds(err)}}
		raw, _ := json.Marshal(body)
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: string(raw)}},
		}, collectOutput{}, nil
	}

	raw, _ := json.Marshal(out)
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: fmt.Sprintf("collected %d messages", out.Count)},
			&mcp.TextContent{Text: string(raw)},
		},
	}, out, nil
}

func normalizeConfig(cfg *Config) {
	if cfg.DefaultMaxMessages <= 0 {
		cfg.DefaultMaxMessages = 100
	}
	if cfg.MaxMessages <= 0 {
		cfg.MaxMessages = 1000
	}
	if cfg.DefaultDurationS <= 0 {
		cfg.DefaultDurationS = 30
	}
	if cfg.MaxDurationS <= 0 {
		cfg.MaxDurationS = 30
	}
	if cfg.MQTTCollectMaxConcurrent <= 0 {
		cfg.MQTTCollectMaxConcurrent = 100
	}
	if cfg.MQTTWatchStartMaxConcurrent <= 0 {
		cfg.MQTTWatchStartMaxConcurrent = 500
	}
	if cfg.WatchDefaultTTLS <= 0 {
		cfg.WatchDefaultTTLS = 300
	}
	if cfg.WatchMaxTTLS <= 0 {
		cfg.WatchMaxTTLS = 900
	}
	if cfg.WatchDefaultTTLS > cfg.WatchMaxTTLS {
		cfg.WatchDefaultTTLS = cfg.WatchMaxTTLS
	}
	if cfg.WatchDefaultBufferMessages <= 0 {
		cfg.WatchDefaultBufferMessages = 100
	}
	if cfg.WatchMaxBufferMessages <= 0 {
		cfg.WatchMaxBufferMessages = 1000
	}
	if cfg.WatchDefaultBufferMessages > cfg.WatchMaxBufferMessages {
		cfg.WatchDefaultBufferMessages = cfg.WatchMaxBufferMessages
	}
	if cfg.WatchDefaultBufferBytes <= 0 {
		cfg.WatchDefaultBufferBytes = 262144
	}
	if cfg.WatchMaxBufferBytes <= 0 {
		cfg.WatchMaxBufferBytes = 1048576
	}
	if cfg.WatchDefaultBufferBytes > cfg.WatchMaxBufferBytes {
		cfg.WatchDefaultBufferBytes = cfg.WatchMaxBufferBytes
	}
	if cfg.WatchMaxPerSession <= 0 {
		cfg.WatchMaxPerSession = 10
	}
	if cfg.WatchMaxPerPod <= 0 {
		cfg.WatchMaxPerPod = 1000
	}
	if cfg.FindTopicsDefaultLimit <= 0 {
		cfg.FindTopicsDefaultLimit = 20
	}
	if cfg.FindTopicsMaxLimit <= 0 {
		cfg.FindTopicsMaxLimit = 100
	}
	if cfg.FindTopicsDefaultLimit > cfg.FindTopicsMaxLimit {
		cfg.FindTopicsDefaultLimit = cfg.FindTopicsMaxLimit
	}
}

func applyLimits(cfg Config, maxMessages, durationS int) (int, int, error) {
	if maxMessages == 0 {
		maxMessages = cfg.DefaultMaxMessages
	}
	if durationS == 0 {
		durationS = cfg.DefaultDurationS
	}
	if maxMessages <= 0 {
		return maxMessages, durationS, &mqttbus.BusError{Code: mqttbus.CodeInvalidArgument, Message: "max_messages must be greater than zero"}
	}
	if durationS <= 0 {
		return maxMessages, durationS, &mqttbus.BusError{Code: mqttbus.CodeInvalidArgument, Message: "max_duration_s must be greater than zero"}
	}
	if maxMessages > cfg.MaxMessages {
		return maxMessages, durationS, &mqttbus.BusError{Code: mqttbus.CodeInvalidArgument, Message: fmt.Sprintf("max_messages exceeds cap %d", cfg.MaxMessages)}
	}
	if durationS > cfg.MaxDurationS {
		return maxMessages, durationS, &mqttbus.BusError{Code: mqttbus.CodeInvalidArgument, Message: fmt.Sprintf("max_duration_s exceeds cap %d", cfg.MaxDurationS)}
	}
	return maxMessages, durationS, nil
}

func applyFindTopicsLimit(cfg Config, limit int) (int, error) {
	if limit == 0 {
		limit = cfg.FindTopicsDefaultLimit
	}
	if limit <= 0 {
		return limit, &mqttbus.BusError{Code: mqttbus.CodeInvalidArgument, Message: "limit must be greater than zero"}
	}
	if limit > cfg.FindTopicsMaxLimit {
		return limit, &mqttbus.BusError{Code: mqttbus.CodeInvalidArgument, Message: fmt.Sprintf("limit exceeds cap %d", cfg.FindTopicsMaxLimit)}
	}
	return limit, nil
}

func errorCode(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, context.Canceled) {
		return "caller_cancelled"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "deadline_exceeded"
	}
	return mqttbus.ErrorCode(err)
}

func toolResultErrorCode(result *mcp.CallToolResult, err error) string {
	if err != nil {
		return errorCode(err)
	}
	if result == nil || !result.IsError {
		return ""
	}
	for _, item := range result.Content {
		text, ok := item.(*mcp.TextContent)
		if !ok || text.Text == "" {
			continue
		}
		var body structuredError
		if json.Unmarshal([]byte(text.Text), &body) == nil && body.Error.Code != "" {
			return body.Error.Code
		}
	}
	return mqttbus.CodeInternalError
}

func publicMessage(err error) string {
	var busErr *mqttbus.BusError
	if errors.As(err, &busErr) {
		return busErr.Message
	}
	if errors.Is(err, context.Canceled) {
		return "caller cancelled the request"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "request deadline exceeded"
	}
	return "tool call failed"
}

func admissionLimitedError() error {
	return &mqttbus.BusError{
		Code:              mqttbus.CodeMQTTAdmissionLimited,
		Message:           "too many MQTT-backed tool calls are starting; retry later",
		RetryAfterSeconds: 1,
	}
}

func retryAfterSeconds(err error) int {
	var busErr *mqttbus.BusError
	if errors.As(err, &busErr) && busErr.RetryAfterSeconds > 0 {
		return busErr.RetryAfterSeconds
	}
	return 0
}

func auditToolCall(
	tool string,
	caller auth.Caller,
	topicFilter string,
	maxMessages int,
	durationS int,
	out collectOutput,
	duration time.Duration,
	code string,
) {
	decision := "allowed"
	if code != "" {
		decision = "error"
	}
	slog.Info("tool invocation",
		"audit", true,
		"tool", tool,
		"caller_tenant", caller.Tenant,
		"caller_issuer", caller.Issuer,
		"caller_subject", caller.Subject,
		"caller_spiffe_id", caller.SpiffeID,
		"mcp_session_id", caller.SessionID,
		"bearer_present", caller.Bearer != "",
		"topic_filter", topicFilter,
		"max_messages", maxMessages,
		"max_duration_s", durationS,
		"decision", decision,
		"message_count", out.Count,
		"stopped_reason", out.StoppedReason,
		"truncated", out.Truncated,
		"duration_ms", duration.Milliseconds(),
		"error_code", code,
	)
}
