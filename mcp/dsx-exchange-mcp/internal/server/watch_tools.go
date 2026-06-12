// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"
	"encoding/json"
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
	toolStartSubscription  = "dsx_exchange_start_subscription"
	toolReadSubscription   = "dsx_exchange_read_subscription"
	toolStatusSubscription = "dsx_exchange_subscription_status"
	toolStopSubscription   = "dsx_exchange_stop_subscription"
)

type startSubscriptionInput struct {
	TopicFilter       string          `json:"topic_filter,omitempty" jsonschema:"Explicit MQTT topic filter to watch. Use either topic_filter or selector, not both."`
	Selector          findTopicsInput `json:"selector,omitempty" jsonschema:"AsyncAPI selector used to derive one topic filter when the caller does not know the raw MQTT path."`
	TTLSeconds        int             `json:"ttl_seconds,omitempty" jsonschema:"Watch TTL in seconds. Defaults to configured value and is capped by MCP_WATCH_MAX_TTL_S."`
	BufferMaxMessages int             `json:"buffer_max_messages,omitempty" jsonschema:"Maximum messages retained in the pod-local ring buffer."`
	BufferMaxBytes    int             `json:"buffer_max_bytes,omitempty" jsonschema:"Maximum payload/topic bytes retained in the pod-local ring buffer."`
}

type readSubscriptionInput struct {
	SubscriptionID string `json:"subscription_id" jsonschema:"Subscription ID returned by dsx_exchange_start_subscription."`
	Cursor         string `json:"cursor,omitempty" jsonschema:"Last cursor seen by the caller. Empty means read from the beginning of the retained local buffer."`
	MaxMessages    int    `json:"max_messages,omitempty" jsonschema:"Maximum messages to return from the local buffer."`
	MaxBytes       int    `json:"max_bytes,omitempty" jsonschema:"Maximum topic/payload bytes to return from the local buffer."`
}

type subscriptionIDInput struct {
	SubscriptionID string `json:"subscription_id" jsonschema:"Subscription ID returned by dsx_exchange_start_subscription."`
}

func registerWatchTools(s *mcp.Server, cfg Config, watches *watchManager) {
	mcp.AddTool(s, &mcp.Tool{
		Name: toolStartSubscription,
		Description: "Start a pod-local background MQTT watch and return a subscription_id immediately after the initial MQTT subscribe succeeds or is accepted as starting. " +
			"The caller bearer is used only in memory as the MQTT password. Broker/auth-callout enforces topic ACLs. " +
			"Requires stateful MCP routing via Mcp-Session-Id. The watch is lost on owning pod restart or session loss.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in startSubscriptionInput) (*mcp.CallToolResult, watchStartOutput, error) {
		return startSubscriptionTool(ctx, cfg, watches, in)
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: toolReadSubscription,
		Description: "Read a bounded raw batch of messages from a pod-local background watch by cursor. " +
			"Use this as a debug or detail path when raw payloads are needed; prefer dsx_exchange_subscription_status for scalable update summaries. " +
			"This reads only the owning pod's in-memory ring buffer; if the session or pod-local state was lost, the tool returns subscription_not_found or session_lost.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in readSubscriptionInput) (*mcp.CallToolResult, watchReadOutput, error) {
		return readSubscriptionTool(ctx, cfg, watches, in)
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        toolStatusSubscription,
		Description: "Return pod-local status, counters, watermarks, expiry, last error, and bounded per-topic update summaries for a background watch owned by the current Mcp-Session-Id.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in subscriptionIDInput) (*mcp.CallToolResult, watchStatusOutput, error) {
		return statusSubscriptionTool(ctx, cfg, watches, in)
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        toolStopSubscription,
		Description: "Stop a pod-local background watch owned by the current Mcp-Session-Id, disconnect its MQTT stream, and release the local buffer.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in subscriptionIDInput) (*mcp.CallToolResult, watchStopOutput, error) {
		return stopSubscriptionTool(ctx, cfg, watches, in)
	})
}

func startSubscriptionTool(ctx context.Context, cfg Config, watches *watchManager, in startSubscriptionInput) (*mcp.CallToolResult, watchStartOutput, error) {
	start := time.Now()
	caller := auth.FromContext(ctx)

	topicFilter, err := resolveSubscriptionTopic(cfg, in)
	if err != nil {
		recordWatchAudit(toolStartSubscription, caller, "", "", 0, start, err)
		return toolErrorFromErr[watchStartOutput](err)
	}

	out, err := watches.start(watchStartRequest{
		Caller:            caller,
		TopicFilter:       topicFilter,
		TTLS:              in.TTLSeconds,
		BufferMaxMessages: in.BufferMaxMessages,
		BufferMaxBytes:    in.BufferMaxBytes,
	})
	recordWatchAudit(toolStartSubscription, caller, out.SubscriptionID, topicFilter, 0, start, err)
	if err != nil {
		return toolErrorFromErr[watchStartOutput](err)
	}
	return toolOK("started subscription "+out.SubscriptionID, out)
}

func readSubscriptionTool(ctx context.Context, cfg Config, watches *watchManager, in readSubscriptionInput) (*mcp.CallToolResult, watchReadOutput, error) {
	start := time.Now()
	caller := auth.FromContext(ctx)
	out, err := watches.read(watchReadRequest{
		Caller:         caller,
		SubscriptionID: in.SubscriptionID,
		Cursor:         in.Cursor,
		MaxMessages:    in.MaxMessages,
		MaxBytes:       in.MaxBytes,
	})
	recordWatchAudit(toolReadSubscription, caller, in.SubscriptionID, "", out.Count, start, err)
	if err != nil {
		return toolErrorFromErr[watchReadOutput](err)
	}
	return toolOK(fmt.Sprintf("read %d subscription messages", out.Count), out)
}

func statusSubscriptionTool(ctx context.Context, cfg Config, watches *watchManager, in subscriptionIDInput) (*mcp.CallToolResult, watchStatusOutput, error) {
	start := time.Now()
	caller := auth.FromContext(ctx)
	out, err := watches.status(watchStatusRequest{
		Caller:         caller,
		SubscriptionID: in.SubscriptionID,
	})
	recordWatchAudit(toolStatusSubscription, caller, in.SubscriptionID, out.TopicFilter, 0, start, err)
	if err != nil {
		return toolErrorFromErr[watchStatusOutput](err)
	}
	return toolOK("subscription status "+out.Status, out)
}

func stopSubscriptionTool(ctx context.Context, cfg Config, watches *watchManager, in subscriptionIDInput) (*mcp.CallToolResult, watchStopOutput, error) {
	start := time.Now()
	caller := auth.FromContext(ctx)
	out, err := watches.stop(watchStopRequest{
		Caller:         caller,
		SubscriptionID: in.SubscriptionID,
	})
	recordWatchAudit(toolStopSubscription, caller, in.SubscriptionID, "", 0, start, err)
	if err != nil {
		return toolErrorFromErr[watchStopOutput](err)
	}
	return toolOK("stopped subscription "+out.SubscriptionID, out)
}

func resolveSubscriptionTopic(cfg Config, in startSubscriptionInput) (string, error) {
	topicFilter := strings.TrimSpace(in.TopicFilter)
	hasSelector := selectorPresent(in.Selector)
	if topicFilter != "" && hasSelector {
		return "", &mqttbus.BusError{Code: mqttbus.CodeInvalidArgument, Message: "use either topic_filter or selector, not both"}
	}
	if topicFilter != "" {
		if err := mqttbus.ValidateTopicFilter(topicFilter); err != nil {
			return "", err
		}
		return topicFilter, nil
	}
	if !hasSelector {
		return "", &mqttbus.BusError{Code: mqttbus.CodeInvalidArgument, Message: "topic_filter or selector is required"}
	}
	idx, err := schemaindex.Default()
	if err != nil {
		return "", &mqttbus.BusError{Code: mqttbus.CodeInternalError, Message: err.Error()}
	}
	matches := idx.Search(schemaindex.SearchOptions{
		Domain:          in.Selector.Domain,
		Query:           in.Selector.Query,
		Role:            in.Selector.Role,
		ObjectType:      in.Selector.ObjectType,
		PointType:       in.Selector.PointType,
		OperationAction: in.Selector.OperationAction,
		Limit:           cfg.FindTopicsMaxLimit,
	})
	if len(matches) == 0 {
		return "", &mqttbus.BusError{Code: codeSchemaTopicNotFound, Message: "selector did not match any AsyncAPI topic"}
	}
	if len(matches) > 1 {
		return "", &mqttbus.BusError{Code: codeSchemaTopicAmbiguous, Message: fmt.Sprintf("selector matched %d AsyncAPI topics; call dsx_exchange_find_topics and choose a topic_filter", len(matches))}
	}
	if err := mqttbus.ValidateTopicFilter(matches[0].TopicFilter); err != nil {
		return "", err
	}
	return matches[0].TopicFilter, nil
}

func selectorPresent(in findTopicsInput) bool {
	return strings.TrimSpace(in.Domain) != "" ||
		strings.TrimSpace(in.Query) != "" ||
		strings.TrimSpace(in.Role) != "" ||
		strings.TrimSpace(in.ObjectType) != "" ||
		strings.TrimSpace(in.PointType) != "" ||
		strings.TrimSpace(in.OperationAction) != ""
}

func toolOK[T any](summary string, out T) (*mcp.CallToolResult, T, error) {
	raw, _ := json.Marshal(out)
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: summary},
			&mcp.TextContent{Text: string(raw)},
		},
	}, out, nil
}

func toolError[T any](code, message string) (*mcp.CallToolResult, T, error) {
	return toolErrorWithRetry[T](code, message, 0)
}

func toolErrorFromErr[T any](err error) (*mcp.CallToolResult, T, error) {
	return toolErrorWithRetry[T](mqttbus.ErrorCode(err), publicMessage(err), retryAfterSeconds(err))
}

func toolErrorWithRetry[T any](code, message string, retryAfter int) (*mcp.CallToolResult, T, error) {
	var zero T
	if code == "" {
		code = mqttbus.CodeInternalError
	}
	body := structuredError{Error: errorBody{Code: code, Message: message, RetryAfterSeconds: retryAfter}}
	raw, _ := json.Marshal(body)
	return &mcp.CallToolResult{
		IsError: true,
		Content: []mcp.Content{&mcp.TextContent{Text: string(raw)}},
	}, zero, nil
}

func recordWatchAudit(tool string, caller auth.Caller, subscriptionID, topicFilter string, messages int, start time.Time, err error) {
	code := errorCode(err)
	duration := time.Since(start)
	decision := "allowed"
	if code != "" {
		decision = "error"
	}
	slog.Info("watch tool invocation",
		"audit", true,
		"tool", tool,
		"caller_tenant", caller.Tenant,
		"caller_issuer", caller.Issuer,
		"caller_subject", caller.Subject,
		"caller_spiffe_id", caller.SpiffeID,
		"mcp_session_id", caller.SessionID,
		"bearer_present", caller.Bearer != "",
		"subscription_id", subscriptionID,
		"topic_filter", topicFilter,
		"decision", decision,
		"message_count", messages,
		"duration_ms", duration.Milliseconds(),
		"error_code", code,
	)
}
