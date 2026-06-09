// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package metrics

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRecorderPrometheusSurfaceIncludesLoadSignals(t *testing.T) {
	rec := NewRecorder()
	rec.ObserveSession("session-a")
	rec.BeginToolCall()
	rec.EndToolCall()
	rec.BeginWatch()
	rec.EndWatch()
	rec.BeginMQTTConnection()
	rec.EndMQTTConnection()
	rec.RecordWatchMessage()
	rec.RecordWatchDrop(2)
	rec.RecordToolCall("dsx_exchange_subscription_status", "", "", 20*time.Millisecond, 0)

	req := httptest.NewRequest("GET", "/metrics", nil)
	resp := httptest.NewRecorder()
	rec.Handler().ServeHTTP(resp, req)
	body := resp.Body.String()

	for _, want := range []string{
		"dsx_exchange_mcp_active_sessions_recent 1",
		"dsx_exchange_mcp_active_background_watches 0",
		"dsx_exchange_mcp_active_mqtt_connections 0",
		"dsx_exchange_mcp_runtime_goroutines",
		"dsx_exchange_mcp_runtime_heap_alloc_bytes",
		"dsx_exchange_mcp_tool_calls_total{tool=\"dsx_exchange_subscription_status\"} 1",
		"dsx_exchange_mcp_tool_duration_seconds_bucket{tool=\"dsx_exchange_subscription_status\",le=\"0.025\"} 1",
		"dsx_exchange_mcp_background_watch_messages_total 1",
		"dsx_exchange_mcp_background_watch_dropped_messages_total 2",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("metrics body missing %q:\n%s", want, body)
		}
	}
}

func TestRecorderPrunesStaleSessions(t *testing.T) {
	rec := NewRecorder()
	rec.sessionTTL = time.Minute
	rec.sessions["old"] = time.Now().Add(-2 * time.Minute)
	rec.sessions["new"] = time.Now()

	if got := rec.activeSessionCountLocked(time.Now()); got != 1 {
		t.Fatalf("active sessions = %d, want 1", got)
	}
	if _, ok := rec.sessions["old"]; ok {
		t.Fatal("stale session was not pruned")
	}
}
