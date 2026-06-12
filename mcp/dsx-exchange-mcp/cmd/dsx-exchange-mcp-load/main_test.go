// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"encoding/base64"
	"encoding/csv"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRecorderSnapshotKeepsStartupVisible(t *testing.T) {
	start := time.Now().Add(-2 * time.Second)
	rec := &recorder{
		startedAt:              start,
		endpoint:               "http://gateway/mcp",
		experiment:             "baseline",
		experimentDetail:       "mqtt_connect_timeout_s=5;mqtt_subscribe_timeout_s=5",
		scenario:               "watch-hold",
		sessions:               2,
		backendReplicas:        1,
		stickySessionCheck:     "not_run",
		rateLimit:              100,
		gatewayRateLimit:       1000,
		manifestName:           "kind-gateway-load-job.yaml",
		backendImageID:         "sha256:backend",
		loadImageID:            "sha256:load",
		experimentConfigHash:   "sha256:abc123",
		tokenTTLSecondsAtStart: 600,
		httpTimeout:            30 * time.Second,
		startupRamp:            30 * time.Second,
		pollInterval:           time.Second,
		subscribeDuration:      1,
		maxMessages:            10,
		maxBytes:               32768,
		watchTTL:               30,
		backendConnectS:        5,
		backendSubscribeS:      5,
		backendCollectMax:      100,
		backendWatchStartMax:   500,
		byOperation:            map[string]*operationStats{},
		errors:                 map[string]uint64{},
		errorSamples:           map[string][]string{},
	}
	rec.record("initialize", 100*time.Millisecond, true, nil)
	rec.record("start_subscription", 250*time.Millisecond, true, nil)
	rec.record("subscription_status", 10*time.Millisecond, true, nil)
	rec.record("stop_subscription", 20*time.Millisecond, true, nil)
	rec.recordInitializedSession()
	rec.recordStartedWatch()
	rec.recordStoppedWatch()

	report := rec.snapshot(start.Add(2 * time.Second))
	if report.TotalRequests != 4 || report.Successes != 4 || report.Failures != 0 {
		t.Fatalf("request counts = total %d success %d failure %d", report.TotalRequests, report.Successes, report.Failures)
	}
	if report.InitializedSessions != 1 || report.StartedWatches != 1 || report.StoppedWatches != 1 {
		t.Fatalf("lifecycle counts = sessions %d started %d stopped %d", report.InitializedSessions, report.StartedWatches, report.StoppedWatches)
	}
	if got := report.ByOperation["initialize"].Phase; got != "startup" {
		t.Fatalf("initialize phase = %q, want startup", got)
	}
	if got := report.ByOperation["subscription_status"].Phase; got != "steady" {
		t.Fatalf("subscription_status phase = %q, want steady", got)
	}
	if got := report.ByOperation["stop_subscription"].Phase; got != "cleanup" {
		t.Fatalf("stop_subscription phase = %q, want cleanup", got)
	}
	if report.ThroughputRPS != 2 {
		t.Fatalf("throughput = %f, want 2", report.ThroughputRPS)
	}
	if report.SuccessRate != 100 {
		t.Fatalf("success rate = %f, want 100", report.SuccessRate)
	}
	if report.Experiment != "baseline" || report.BackendConnectS != 5 || report.BackendSubscribeS != 5 || report.StartupRampSeconds != 30 {
		t.Fatalf("experiment metadata = %q connect=%d subscribe=%d startup_ramp=%f", report.Experiment, report.BackendConnectS, report.BackendSubscribeS, report.StartupRampSeconds)
	}
	if report.BackendCollectMax != 100 || report.BackendWatchStartMax != 500 {
		t.Fatalf("admission metadata = collect %d watch_start %d, want 100/500", report.BackendCollectMax, report.BackendWatchStartMax)
	}
	if report.BackendReplicas != 1 || report.StickySessionCheck != "not_run" {
		t.Fatalf("scale metadata = replicas %d sticky %q", report.BackendReplicas, report.StickySessionCheck)
	}
	if report.GatewayRateLimit != 1000 || report.ManifestName != "kind-gateway-load-job.yaml" || report.BackendImageID != "sha256:backend" || report.LoadImageID != "sha256:load" {
		t.Fatalf("repro metadata = gateway %d manifest %q backend %q load %q", report.GatewayRateLimit, report.ManifestName, report.BackendImageID, report.LoadImageID)
	}
	if report.ExperimentConfigHash != "sha256:abc123" || report.TokenTTLSecondsAtStart != 600 || report.PollIntervalSeconds != 1 {
		t.Fatalf("config metadata = hash %q token_ttl=%d poll=%f", report.ExperimentConfigHash, report.TokenTTLSecondsAtStart, report.PollIntervalSeconds)
	}
}

func TestRecorderCountsContextFailures(t *testing.T) {
	rec := &recorder{
		startedAt:    time.Now(),
		byOperation:  map[string]*operationStats{},
		errors:       map[string]uint64{},
		errorSamples: map[string][]string{},
	}
	ctx := t.Context()
	_, err := measure(ctx, rec, "initialize", func(context.Context) error {
		return errors.New("context deadline exceeded")
	})
	if err == nil {
		t.Fatal("measure returned nil error")
	}
	report := rec.snapshot(time.Now())
	if report.Failures != 1 {
		t.Fatalf("failures = %d, want 1", report.Failures)
	}
	if report.Errors["context_deadline"] != 1 {
		t.Fatalf("context_deadline errors = %d, want 1", report.Errors["context_deadline"])
	}
	if got := report.ErrorSamples["context_deadline"]; len(got) != 1 || got[0] != "context deadline exceeded" {
		t.Fatalf("context_deadline samples = %#v, want context deadline exceeded", got)
	}
}

func TestMeasureSkipsParentDeadlineCancellation(t *testing.T) {
	rec := &recorder{
		startedAt:    time.Now(),
		byOperation:  map[string]*operationStats{},
		errors:       map[string]uint64{},
		errorSamples: map[string][]string{},
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel()
	<-ctx.Done()

	if _, err := measure(ctx, rec, "subscribe", func(context.Context) error {
		return context.DeadlineExceeded
	}); err == nil {
		t.Fatal("measure returned nil error, want deadline")
	}

	report := rec.snapshot(time.Now())
	if report.TotalRequests != 0 || report.Failures != 0 {
		t.Fatalf("parent deadline cancellation was recorded: total=%d failures=%d", report.TotalRequests, report.Failures)
	}
}

func TestClassifyErrorExtractsStructuredToolCode(t *testing.T) {
	err := errors.New(`unexpected_tool_error:{"error":{"code":"mqtt_admission_limited","message":"retry later","retry_after_seconds":1}}`)
	if got := classifyError(err); got != "tool_error_mqtt_admission_limited" {
		t.Fatalf("classifyError = %q, want tool_error_mqtt_admission_limited", got)
	}
}

func TestRecorderSnapshotMarksStickyCheckResult(t *testing.T) {
	start := time.Now().Add(-time.Second)
	rec := &recorder{
		startedAt:          start,
		scenario:           "sticky-check",
		stickySessionCheck: "running",
		byOperation:        map[string]*operationStats{},
		errors:             map[string]uint64{},
		errorSamples:       map[string][]string{},
	}
	rec.record("subscription_status", time.Millisecond, true, nil)
	pass := rec.snapshot(start.Add(time.Second))
	if pass.StickySessionCheck != "pass" {
		t.Fatalf("sticky_session_check = %q, want pass", pass.StickySessionCheck)
	}

	rec.record("read_subscription", time.Millisecond, false, errors.New("unexpected_tool_error:subscription_not_found"))
	rec.record("subscription_status", time.Millisecond, false, errors.New("http_404:session not found"))
	fail := rec.snapshot(start.Add(2 * time.Second))
	if fail.StickySessionCheck != "fail" {
		t.Fatalf("sticky_session_check = %q, want fail", fail.StickySessionCheck)
	}
	if fail.SessionNotFoundErrors != 1 || fail.SubscriptionNotFoundErrors != 1 {
		t.Fatalf("sticky error counters = session %d subscription %d, want 1/1", fail.SessionNotFoundErrors, fail.SubscriptionNotFoundErrors)
	}
}

func TestWaitStartupRampSpreadsSessionStarts(t *testing.T) {
	start := time.Now()
	if ok := waitStartupRamp(t.Context(), 60*time.Millisecond, 0, 3); !ok {
		t.Fatal("session 0 unexpectedly skipped")
	}
	if elapsed := time.Since(start); elapsed > 20*time.Millisecond {
		t.Fatalf("session 0 waited %s, want immediate", elapsed)
	}

	start = time.Now()
	if ok := waitStartupRamp(t.Context(), 60*time.Millisecond, 2, 3); !ok {
		t.Fatal("session 2 unexpectedly skipped")
	}
	if elapsed := time.Since(start); elapsed < 35*time.Millisecond || elapsed > 150*time.Millisecond {
		t.Fatalf("session 2 waited %s, want roughly 40ms", elapsed)
	}
}

func TestWaitStartupRampHonorsContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if ok := waitStartupRamp(ctx, time.Minute, 1, 2); ok {
		t.Fatal("waitStartupRamp returned true after cancellation")
	}
}

func TestEffectivePollIntervalDefaults(t *testing.T) {
	if got := effectivePollInterval(config{scenario: "watch-status-hold"}); got != time.Second {
		t.Fatalf("watch-status poll interval = %s, want 1s", got)
	}
	if got := effectivePollInterval(config{scenario: "sticky-check"}); got != 250*time.Millisecond {
		t.Fatalf("sticky poll interval = %s, want 250ms", got)
	}
	if got := effectivePollInterval(config{scenario: "sticky-check", pollInterval: time.Second}); got != time.Second {
		t.Fatalf("override poll interval = %s, want 1s", got)
	}
}

func TestExperimentConfigHashExcludesBearer(t *testing.T) {
	cfg := config{
		endpoint:          "http://gateway/mcp",
		bearer:            "secret-a",
		scenario:          "sticky-check",
		sessions:          100,
		duration:          time.Minute,
		gatewayRateLimit:  1000,
		topic:             "BMS/#",
		retainedTopic:     "BMS/meta/#",
		subscribeDuration: 1,
		maxMessages:       10,
		maxBytes:          1024,
		watchTTL:          60,
		httpTimeout:       30 * time.Second,
	}
	a := experimentConfigHash(cfg)
	cfg.bearer = "secret-b"
	b := experimentConfigHash(cfg)
	if a == "" || a != b {
		t.Fatalf("hash changed when only bearer changed: %q vs %q", a, b)
	}
	cfg.sessions = 101
	if c := experimentConfigHash(cfg); c == a {
		t.Fatalf("hash did not change after config change: %q", c)
	}
}

func TestTokenTTLSeconds(t *testing.T) {
	claims, err := json.Marshal(map[string]int64{"exp": time.Now().Add(time.Minute).Unix()})
	if err != nil {
		t.Fatal(err)
	}
	token := "header." + base64.RawURLEncoding.EncodeToString(claims) + ".signature"
	if got := tokenTTLSeconds(token); got < 1 || got > 60 {
		t.Fatalf("token ttl = %d, want within 1..60", got)
	}
	if got := tokenTTLSeconds("not-a-jwt"); got != 0 {
		t.Fatalf("invalid token ttl = %d, want 0", got)
	}
}

func TestWriteCSVReportIncludesOperationRows(t *testing.T) {
	start := time.Date(2026, 6, 8, 12, 0, 0, 0, time.UTC)
	report := runReport{
		StartedAt:                  start,
		EndedAt:                    start.Add(time.Minute),
		DurationSeconds:            60,
		ThroughputRPS:              10,
		SuccessRate:                99.5,
		Endpoint:                   "http://gateway/mcp",
		Experiment:                 "mixed-baseline",
		ExperimentDetail:           "gateway_rps=1000;mqtt_connect_timeout_s=5;mqtt_subscribe_timeout_s=5",
		Scenario:                   "watch-hold",
		Sessions:                   500,
		BackendReplicas:            3,
		StickySessionCheck:         "planned",
		RateLimit:                  1000,
		GatewayRateLimit:           5000,
		ManifestName:               "kind-gateway-load-job.yaml",
		BackendImageID:             "sha256:backend",
		LoadImageID:                "sha256:load",
		ExperimentConfigHash:       "sha256:abc123",
		TokenTTLSecondsAtStart:     600,
		Topic:                      "BMS/v1/PUB/Value/#",
		RetainedTopic:              "BMS/v1/PUB/Metadata/#",
		HTTPTimeoutSeconds:         30,
		StartupRampSeconds:         30,
		PollIntervalSeconds:        1,
		SubscribeDurationS:         1,
		MaxMessages:                10,
		MaxBytes:                   32768,
		WatchTTLS:                  180,
		BackendConnectS:            5,
		BackendSubscribeS:          5,
		BackendCollectMax:          100,
		BackendWatchStartMax:       500,
		TotalRequests:              1000,
		Successes:                  995,
		Failures:                   5,
		InitializedSessions:        500,
		StartedWatches:             491,
		StoppedWatches:             491,
		SessionNotFoundErrors:      2,
		SubscriptionNotFoundErrors: 3,
		ByOperation: map[string]operationSnapshot{
			"start_subscription": {
				Phase:           "startup",
				Count:           500,
				Successes:       491,
				Failures:        9,
				P50Milliseconds: 5683.353,
				P95Milliseconds: 7466.277,
				P99Milliseconds: 8002.154,
			},
		},
		Errors: map[string]uint64{"unexpected_tool_error": 9},
	}

	path := filepath.Join(t.TempDir(), "report.csv")
	if err := writeCSVReport(path, []runReport{report}); err != nil {
		t.Fatalf("writeCSVReport returned error: %v", err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open csv: %v", err)
	}
	defer f.Close()
	rows, err := csv.NewReader(f).ReadAll()
	if err != nil {
		t.Fatalf("read csv: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("csv rows = %d, want 2", len(rows))
	}
	header := rows[0]
	row := rows[1]
	col := func(name string) string {
		for i, h := range header {
			if h == name {
				return row[i]
			}
		}
		t.Fatalf("missing csv column %q", name)
		return ""
	}
	if got := col("sessions"); got != "500" {
		t.Fatalf("sessions column = %q, want 500", got)
	}
	if got := col("backend_replicas"); got != "3" {
		t.Fatalf("backend_replicas column = %q, want 3", got)
	}
	if got := col("sticky_session_check"); got != "planned" {
		t.Fatalf("sticky_session_check column = %q, want planned", got)
	}
	if got := col("gateway_rate_limit_rps"); got != "5000" {
		t.Fatalf("gateway_rate_limit_rps column = %q, want 5000", got)
	}
	if got := col("manifest_name"); got != "kind-gateway-load-job.yaml" {
		t.Fatalf("manifest_name column = %q, want kind-gateway-load-job.yaml", got)
	}
	if got := col("backend_image_id"); got != "sha256:backend" {
		t.Fatalf("backend_image_id column = %q, want sha256:backend", got)
	}
	if got := col("load_image_id"); got != "sha256:load" {
		t.Fatalf("load_image_id column = %q, want sha256:load", got)
	}
	if got := col("experiment_config_hash"); got != "sha256:abc123" {
		t.Fatalf("experiment_config_hash column = %q, want sha256:abc123", got)
	}
	if got := col("token_ttl_seconds_at_start"); got != "600" {
		t.Fatalf("token_ttl_seconds_at_start column = %q, want 600", got)
	}
	if got := col("experiment"); got != "mixed-baseline" {
		t.Fatalf("experiment column = %q, want mixed-baseline", got)
	}
	if got := col("backend_mqtt_connect_timeout_seconds"); got != "5" {
		t.Fatalf("backend_mqtt_connect_timeout_seconds column = %q, want 5", got)
	}
	if got := col("backend_mqtt_collect_max_concurrent_per_pod"); got != "100" {
		t.Fatalf("backend_mqtt_collect_max_concurrent_per_pod column = %q, want 100", got)
	}
	if got := col("backend_mqtt_watch_start_max_concurrent_per_pod"); got != "500" {
		t.Fatalf("backend_mqtt_watch_start_max_concurrent_per_pod column = %q, want 500", got)
	}
	if got := col("http_timeout_seconds"); got != "30.000" {
		t.Fatalf("http_timeout_seconds column = %q, want 30.000", got)
	}
	if got := col("startup_ramp_seconds"); got != "30.000" {
		t.Fatalf("startup_ramp_seconds column = %q, want 30.000", got)
	}
	if got := col("poll_interval_seconds"); got != "1.000" {
		t.Fatalf("poll_interval_seconds column = %q, want 1.000", got)
	}
	if got := col("session_not_found_errors"); got != "2" {
		t.Fatalf("session_not_found_errors column = %q, want 2", got)
	}
	if got := col("subscription_not_found_errors"); got != "3" {
		t.Fatalf("subscription_not_found_errors column = %q, want 3", got)
	}
	if got := col("operation"); got != "start_subscription" {
		t.Fatalf("operation column = %q, want start_subscription", got)
	}
	if got := col("p95_ms"); got != "7466.277" {
		t.Fatalf("p95_ms column = %q, want 7466.277", got)
	}
	if got := col("errors"); got != "unexpected_tool_error=9" {
		t.Fatalf("errors column = %q, want unexpected_tool_error=9", got)
	}
}
