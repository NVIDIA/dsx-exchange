// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package metrics

import (
	"fmt"
	"net/http"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Recorder keeps a small Prometheus-compatible metrics surface without adding
// a dependency on a metrics framework. The service can swap this for OTel or
// prometheus/client_golang later without changing tool code.
type Recorder struct {
	activeCalls           int64
	activeWatches         int64
	activeMQTTConnections int64
	watchMessages         uint64
	watchDropped          uint64

	mu             sync.Mutex
	sessionTTL     time.Duration
	sessions       map[string]time.Time
	toolCalls      map[string]uint64
	toolErrors     map[labelKey]uint64
	toolDuration   map[string]time.Duration
	toolBuckets    map[labelKey]uint64
	messageCounts  map[string]uint64
	stoppedReasons map[labelKey]uint64
}

type labelKey struct {
	Tool  string
	Value string
}

func NewRecorder() *Recorder {
	return &Recorder{
		sessionTTL:     10 * time.Minute,
		sessions:       map[string]time.Time{},
		toolCalls:      map[string]uint64{},
		toolErrors:     map[labelKey]uint64{},
		toolDuration:   map[string]time.Duration{},
		toolBuckets:    map[labelKey]uint64{},
		messageCounts:  map[string]uint64{},
		stoppedReasons: map[labelKey]uint64{},
	}
}

func (r *Recorder) BeginToolCall() {
	atomic.AddInt64(&r.activeCalls, 1)
}

func (r *Recorder) EndToolCall() {
	atomic.AddInt64(&r.activeCalls, -1)
}

func (r *Recorder) ObserveSession(sessionID string) {
	if r == nil || strings.TrimSpace(sessionID) == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sessions[sessionID] = time.Now()
}

func (r *Recorder) BeginMQTTConnection() {
	if r == nil {
		return
	}
	atomic.AddInt64(&r.activeMQTTConnections, 1)
}

func (r *Recorder) EndMQTTConnection() {
	if r == nil {
		return
	}
	atomic.AddInt64(&r.activeMQTTConnections, -1)
}

func (r *Recorder) BeginWatch() {
	if r == nil {
		return
	}
	atomic.AddInt64(&r.activeWatches, 1)
}

func (r *Recorder) EndWatch() {
	if r == nil {
		return
	}
	atomic.AddInt64(&r.activeWatches, -1)
}

func (r *Recorder) RecordWatchMessage() {
	if r == nil {
		return
	}
	atomic.AddUint64(&r.watchMessages, 1)
}

func (r *Recorder) RecordWatchDrop(n int64) {
	if r == nil || n <= 0 {
		return
	}
	atomic.AddUint64(&r.watchDropped, uint64(n))
}

func (r *Recorder) RecordToolCall(tool, code, stoppedReason string, duration time.Duration, messages int) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	r.toolCalls[tool]++
	r.toolDuration[tool] += duration
	for _, bucket := range durationBuckets {
		if duration.Seconds() <= bucket {
			r.toolBuckets[labelKey{Tool: tool, Value: formatBucket(bucket)}]++
		}
	}
	if messages > 0 {
		r.messageCounts[tool] += uint64(messages)
	}
	if code != "" {
		r.toolErrors[labelKey{Tool: tool, Value: code}]++
	}
	if stoppedReason != "" {
		r.stoppedReasons[labelKey{Tool: tool, Value: stoppedReason}]++
	}
}

var durationBuckets = []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30}

func (r *Recorder) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		r.writePrometheus(w)
	})
}

func (r *Recorder) writePrometheus(w http.ResponseWriter) {
	if r == nil {
		fmt.Fprintln(w, "# no metrics recorder configured")
		return
	}

	r.mu.Lock()
	activeSessions := r.activeSessionCountLocked(time.Now())
	toolCalls := cloneStringMap(r.toolCalls)
	toolDuration := cloneDurationMap(r.toolDuration)
	toolBuckets := cloneLabelMap(r.toolBuckets)
	messageCounts := cloneStringMap(r.messageCounts)
	toolErrors := cloneLabelMap(r.toolErrors)
	stoppedReasons := cloneLabelMap(r.stoppedReasons)
	r.mu.Unlock()

	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)

	fmt.Fprintln(w, "# HELP dsx_exchange_mcp_active_sessions_recent MCP sessions observed on this pod within the recent-session TTL.")
	fmt.Fprintln(w, "# TYPE dsx_exchange_mcp_active_sessions_recent gauge")
	fmt.Fprintf(w, "dsx_exchange_mcp_active_sessions_recent %d\n", activeSessions)

	fmt.Fprintln(w, "# HELP dsx_exchange_mcp_active_tool_calls Tool calls currently in flight.")
	fmt.Fprintln(w, "# TYPE dsx_exchange_mcp_active_tool_calls gauge")
	fmt.Fprintf(w, "dsx_exchange_mcp_active_tool_calls %d\n", atomic.LoadInt64(&r.activeCalls))

	fmt.Fprintln(w, "# HELP dsx_exchange_mcp_active_background_watches Background watches currently active in this pod.")
	fmt.Fprintln(w, "# TYPE dsx_exchange_mcp_active_background_watches gauge")
	fmt.Fprintf(w, "dsx_exchange_mcp_active_background_watches %d\n", atomic.LoadInt64(&r.activeWatches))

	fmt.Fprintln(w, "# HELP dsx_exchange_mcp_active_mqtt_connections MQTT connections currently open by this pod.")
	fmt.Fprintln(w, "# TYPE dsx_exchange_mcp_active_mqtt_connections gauge")
	fmt.Fprintf(w, "dsx_exchange_mcp_active_mqtt_connections %d\n", atomic.LoadInt64(&r.activeMQTTConnections))

	fmt.Fprintln(w, "# HELP dsx_exchange_mcp_runtime_goroutines Goroutines currently running in this pod.")
	fmt.Fprintln(w, "# TYPE dsx_exchange_mcp_runtime_goroutines gauge")
	fmt.Fprintf(w, "dsx_exchange_mcp_runtime_goroutines %d\n", runtime.NumGoroutine())

	fmt.Fprintln(w, "# HELP dsx_exchange_mcp_runtime_heap_alloc_bytes Bytes of allocated heap objects.")
	fmt.Fprintln(w, "# TYPE dsx_exchange_mcp_runtime_heap_alloc_bytes gauge")
	fmt.Fprintf(w, "dsx_exchange_mcp_runtime_heap_alloc_bytes %d\n", mem.HeapAlloc)

	fmt.Fprintln(w, "# HELP dsx_exchange_mcp_runtime_sys_bytes Bytes of memory obtained from the OS.")
	fmt.Fprintln(w, "# TYPE dsx_exchange_mcp_runtime_sys_bytes gauge")
	fmt.Fprintf(w, "dsx_exchange_mcp_runtime_sys_bytes %d\n", mem.Sys)

	fmt.Fprintln(w, "# HELP dsx_exchange_mcp_tool_calls_total Total tool calls by tool.")
	fmt.Fprintln(w, "# TYPE dsx_exchange_mcp_tool_calls_total counter")
	for _, tool := range sortedKeys(toolCalls) {
		fmt.Fprintf(w, "dsx_exchange_mcp_tool_calls_total{tool=\"%s\"} %d\n", promLabel(tool), toolCalls[tool])
	}

	fmt.Fprintln(w, "# HELP dsx_exchange_mcp_tool_errors_total Tool errors by tool and code.")
	fmt.Fprintln(w, "# TYPE dsx_exchange_mcp_tool_errors_total counter")
	for _, k := range sortedLabelKeys(toolErrors) {
		fmt.Fprintf(w, "dsx_exchange_mcp_tool_errors_total{tool=\"%s\",code=\"%s\"} %d\n", promLabel(k.Tool), promLabel(k.Value), toolErrors[k])
	}

	fmt.Fprintln(w, "# HELP dsx_exchange_mcp_tool_duration_seconds Tool duration histogram by tool.")
	fmt.Fprintln(w, "# TYPE dsx_exchange_mcp_tool_duration_seconds histogram")
	for _, tool := range sortedKeys(toolCalls) {
		for _, bucket := range durationBuckets {
			le := formatBucket(bucket)
			fmt.Fprintf(w, "dsx_exchange_mcp_tool_duration_seconds_bucket{tool=\"%s\",le=\"%s\"} %d\n", promLabel(tool), le, toolBuckets[labelKey{Tool: tool, Value: le}])
		}
		fmt.Fprintf(w, "dsx_exchange_mcp_tool_duration_seconds_bucket{tool=\"%s\",le=\"+Inf\"} %d\n", promLabel(tool), toolCalls[tool])
		fmt.Fprintf(w, "dsx_exchange_mcp_tool_duration_seconds_sum{tool=\"%s\"} %.6f\n", promLabel(tool), toolDuration[tool].Seconds())
		fmt.Fprintf(w, "dsx_exchange_mcp_tool_duration_seconds_count{tool=\"%s\"} %d\n", promLabel(tool), toolCalls[tool])
	}

	fmt.Fprintln(w, "# HELP dsx_exchange_mcp_mqtt_messages_collected_total MQTT messages returned by tool.")
	fmt.Fprintln(w, "# TYPE dsx_exchange_mcp_mqtt_messages_collected_total counter")
	for _, tool := range sortedKeys(messageCounts) {
		fmt.Fprintf(w, "dsx_exchange_mcp_mqtt_messages_collected_total{tool=\"%s\"} %d\n", promLabel(tool), messageCounts[tool])
	}

	fmt.Fprintln(w, "# HELP dsx_exchange_mcp_stopped_reasons_total Tool stop reasons by tool.")
	fmt.Fprintln(w, "# TYPE dsx_exchange_mcp_stopped_reasons_total counter")
	for _, k := range sortedLabelKeys(stoppedReasons) {
		fmt.Fprintf(w, "dsx_exchange_mcp_stopped_reasons_total{tool=\"%s\",reason=\"%s\"} %d\n", promLabel(k.Tool), promLabel(k.Value), stoppedReasons[k])
	}

	fmt.Fprintln(w, "# HELP dsx_exchange_mcp_background_watch_messages_total MQTT messages received by background watches.")
	fmt.Fprintln(w, "# TYPE dsx_exchange_mcp_background_watch_messages_total counter")
	fmt.Fprintf(w, "dsx_exchange_mcp_background_watch_messages_total %d\n", atomic.LoadUint64(&r.watchMessages))

	fmt.Fprintln(w, "# HELP dsx_exchange_mcp_background_watch_dropped_messages_total MQTT messages dropped from background watch buffers.")
	fmt.Fprintln(w, "# TYPE dsx_exchange_mcp_background_watch_dropped_messages_total counter")
	fmt.Fprintf(w, "dsx_exchange_mcp_background_watch_dropped_messages_total %d\n", atomic.LoadUint64(&r.watchDropped))
}

func (r *Recorder) activeSessionCountLocked(now time.Time) int {
	if r.sessionTTL <= 0 {
		r.sessionTTL = 10 * time.Minute
	}
	cutoff := now.Add(-r.sessionTTL)
	for sessionID, lastSeen := range r.sessions {
		if lastSeen.Before(cutoff) {
			delete(r.sessions, sessionID)
		}
	}
	return len(r.sessions)
}

func cloneStringMap(in map[string]uint64) map[string]uint64 {
	out := make(map[string]uint64, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func cloneDurationMap(in map[string]time.Duration) map[string]time.Duration {
	out := make(map[string]time.Duration, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func cloneLabelMap(in map[labelKey]uint64) map[labelKey]uint64 {
	out := make(map[labelKey]uint64, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func sortedKeys(m map[string]uint64) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func sortedLabelKeys(m map[labelKey]uint64) []labelKey {
	out := make([]labelKey, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Tool == out[j].Tool {
			return out[i].Value < out[j].Value
		}
		return out[i].Tool < out[j].Tool
	})
	return out
}

func promLabel(s string) string {
	return strings.NewReplacer("\\", "\\\\", "\n", "\\n", "\"", "\\\"").Replace(s)
}

func formatBucket(v float64) string {
	s := fmt.Sprintf("%.3f", v)
	s = strings.TrimRight(s, "0")
	return strings.TrimRight(s, ".")
}
