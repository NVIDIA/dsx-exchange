// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/NVIDIA/dsx-exchange/mcp/dsx-exchange-mcp/internal/auth"
	"github.com/NVIDIA/dsx-exchange/mcp/dsx-exchange-mcp/internal/mqttbus"
)

func TestWatchManagerLifecycleReadOverflowAndStop(t *testing.T) {
	cfg := Config{
		WatchDefaultTTLS:           30,
		WatchMaxTTLS:               60,
		WatchDefaultBufferMessages: 2,
		WatchMaxBufferMessages:     10,
		WatchDefaultBufferBytes:    1024,
		WatchMaxBufferBytes:        2048,
		WatchMaxPerSession:         2,
		WatchMaxPerPod:             10,
	}
	normalizeConfig(&cfg)
	m := newWatchManager(cfg)
	m.newID = func() string { return "sub_test" }
	m.retention = time.Millisecond
	m.runner = fakeStreamRunner([]mqttbus.Message{
		{Topic: "BMS/v1/PUB/Value/Rack/RackPower/a", Payload: `{"value":1}`, PayloadEncoding: "utf8", ReceivedAt: time.Unix(1, 0)},
		{Topic: "BMS/v1/PUB/Value/Rack/RackPower/a", Payload: `{"value":2}`, PayloadEncoding: "utf8", ReceivedAt: time.Unix(2, 0)},
		{Topic: "BMS/v1/PUB/Value/Rack/RackPower/a", Payload: `{"value":3}`, PayloadEncoding: "utf8", ReceivedAt: time.Unix(3, 0)},
	})
	caller := testCaller()

	start, err := m.start(watchStartRequest{
		Caller:      caller,
		TopicFilter: "BMS/v1/PUB/Value/Rack/RackPower/#",
	})
	if err != nil {
		t.Fatalf("start returned error: %v", err)
	}
	if start.SubscriptionID != "sub_test" || start.Status != watchStatusRunning {
		t.Fatalf("start = %#v, want running sub_test", start)
	}

	read, err := m.read(watchReadRequest{
		Caller:         caller,
		SubscriptionID: "sub_test",
		Cursor:         "0",
		MaxMessages:    10,
		MaxBytes:       2048,
	})
	if err != nil {
		t.Fatalf("read returned error: %v", err)
	}
	if read.Count != 2 {
		t.Fatalf("read count = %d, want 2", read.Count)
	}
	if read.Messages[0].Cursor != "2" || read.Messages[1].Cursor != "3" {
		t.Fatalf("read cursors = %#v, want retained messages 2 and 3", read.Messages)
	}
	if read.DroppedCount != 1 {
		t.Fatalf("dropped_count = %d, want 1", read.DroppedCount)
	}

	status, err := m.status(watchStatusRequest{
		Caller:         caller,
		SubscriptionID: "sub_test",
	})
	if err != nil {
		t.Fatalf("status returned error: %v", err)
	}
	if status.MessageCount != 3 || status.DroppedCount != 1 {
		t.Fatalf("status = %#v, want 3 messages and 1 drop", status)
	}
	if status.UpdateCount != 1 || status.UpdatesTruncated {
		t.Fatalf("status updates = count %d truncated %v, want one untruncated update", status.UpdateCount, status.UpdatesTruncated)
	}
	if status.UpdatesDropped != 0 {
		t.Fatalf("updates_dropped = %d, want 0", status.UpdatesDropped)
	}
	if len(status.Updates) != 1 {
		t.Fatalf("status updates len = %d, want 1", len(status.Updates))
	}
	update := status.Updates[0]
	if update.Topic != "BMS/v1/PUB/Value/Rack/RackPower/a" || update.Count != 3 || update.LatestCursor != "3" {
		t.Fatalf("status update = %#v, want latest cursor 3 with count 3", update)
	}
	if update.LatestPayload != `{"value":3}` || update.LatestReceivedAt != time.Unix(3, 0) {
		t.Fatalf("status update latest = payload %q time %s, want value 3 at unix 3", update.LatestPayload, update.LatestReceivedAt)
	}
	if update.Numeric == nil {
		t.Fatal("status update numeric aggregate missing")
	}
	if update.Numeric.Field != "value" || update.Numeric.Count != 3 || update.Numeric.Min != 1 || update.Numeric.Max != 3 || update.Numeric.Mean != 2 || update.Numeric.Latest != 3 {
		t.Fatalf("numeric aggregate = %#v, want field=value count=3 min=1 max=3 mean=2 latest=3", update.Numeric)
	}

	stop, err := m.stop(watchStopRequest{
		Caller:         caller,
		SubscriptionID: "sub_test",
	})
	if err != nil {
		t.Fatalf("stop returned error: %v", err)
	}
	if stop.Status != watchStatusStopped {
		t.Fatalf("stop status = %q, want stopped", stop.Status)
	}

	_, err = m.read(watchReadRequest{
		Caller:         caller,
		SubscriptionID: "sub_test",
	})
	if got := mqttbus.ErrorCode(err); got != codeSubscriptionNotFound {
		t.Fatalf("read after stop code = %q, want %q", got, codeSubscriptionNotFound)
	}
}

func TestWatchManagerStatusAggregatesBoundedTopicUpdates(t *testing.T) {
	cfg := Config{
		WatchDefaultTTLS:           30,
		WatchMaxTTLS:               60,
		WatchDefaultBufferMessages: 2,
		WatchMaxBufferMessages:     100,
		WatchDefaultBufferBytes:    8192,
		WatchMaxBufferBytes:        8192,
		WatchMaxPerSession:         2,
		WatchMaxPerPod:             10,
	}
	normalizeConfig(&cfg)
	m := newWatchManager(cfg)
	m.newID = func() string { return "sub_aggregate" }

	messages := make([]mqttbus.Message, 0, maxWatchStatusUpdates+1)
	for i := 0; i < maxWatchStatusUpdates+1; i++ {
		payload := fmt.Sprintf(`{"value":%d}`, i)
		if i == maxWatchStatusUpdates {
			payload = strings.Repeat("x", maxWatchStatusPayloadBytes+10)
		}
		messages = append(messages, mqttbus.Message{
			Topic:           fmt.Sprintf("BMS/v1/PUB/Value/Rack/RackPower/%03d", i),
			Payload:         payload,
			PayloadEncoding: "utf8",
			ReceivedAt:      time.Unix(int64(i), 0),
		})
	}
	m.runner = fakeStreamRunner(messages)
	caller := testCaller()

	start, err := m.start(watchStartRequest{
		Caller:      caller,
		TopicFilter: "BMS/v1/PUB/Value/Rack/RackPower/#",
	})
	if err != nil {
		t.Fatalf("start returned error: %v", err)
	}

	status, err := m.status(watchStatusRequest{
		Caller:         caller,
		SubscriptionID: start.SubscriptionID,
	})
	if err != nil {
		t.Fatalf("status returned error: %v", err)
	}
	if status.UpdateCount != maxWatchStatusUpdates {
		t.Fatalf("update_count = %d, want %d", status.UpdateCount, maxWatchStatusUpdates)
	}
	if len(status.Updates) != maxWatchStatusUpdates || status.UpdatesDropped != 1 || !status.UpdatesTruncated {
		t.Fatalf("updates len/dropped/truncated = %d/%d/%v, want %d/1/true", len(status.Updates), status.UpdatesDropped, status.UpdatesTruncated, maxWatchStatusUpdates)
	}
	latest := status.Updates[0]
	if latest.Topic != "BMS/v1/PUB/Value/Rack/RackPower/050" || latest.LatestCursor != "51" {
		t.Fatalf("latest update = %#v, want newest topic/cursor", latest)
	}
	if len(latest.LatestPayload) != maxWatchStatusPayloadBytes || !latest.LatestPayloadTruncated {
		t.Fatalf("latest payload len/truncated = %d/%v, want %d/true", len(latest.LatestPayload), latest.LatestPayloadTruncated, maxWatchStatusPayloadBytes)
	}

	_, err = m.stop(watchStopRequest{
		Caller:         caller,
		SubscriptionID: start.SubscriptionID,
	})
	if err != nil {
		t.Fatalf("stop returned error: %v", err)
	}
}

func TestWatchManagerStatusOmitsNumericForMetadataAndTracksCloudEventValue(t *testing.T) {
	cfg := Config{
		WatchDefaultTTLS:           30,
		WatchMaxTTLS:               60,
		WatchDefaultBufferMessages: 10,
		WatchMaxBufferMessages:     100,
		WatchDefaultBufferBytes:    8192,
		WatchMaxBufferBytes:        8192,
		WatchMaxPerSession:         2,
		WatchMaxPerPod:             10,
	}
	normalizeConfig(&cfg)
	m := newWatchManager(cfg)
	m.newID = func() string { return "sub_mixed_aggregates" }
	m.runner = fakeStreamRunner([]mqttbus.Message{
		{Topic: "BMS/v1/PUB/Metadata/Rack/RackPower/a", Payload: `{"unit":"kW","displayName":"Rack A"}`, PayloadEncoding: "utf8", ReceivedAt: time.Unix(1, 0)},
		{Topic: "grid/v1/poweragent/a/powerstate/status", Payload: `{"specversion":"1.0","data":{"value":10}}`, PayloadEncoding: "utf8", ReceivedAt: time.Unix(2, 0)},
		{Topic: "grid/v1/poweragent/a/powerstate/status", Payload: `{"specversion":"1.0","data":{"value":20}}`, PayloadEncoding: "utf8", ReceivedAt: time.Unix(3, 0)},
	})
	caller := testCaller()

	start, err := m.start(watchStartRequest{
		Caller:      caller,
		TopicFilter: "BMS/v1/PUB/#",
	})
	if err != nil {
		t.Fatalf("start returned error: %v", err)
	}
	status, err := m.status(watchStatusRequest{
		Caller:         caller,
		SubscriptionID: start.SubscriptionID,
	})
	if err != nil {
		t.Fatalf("status returned error: %v", err)
	}

	updates := map[string]watchTopicUpdateOutput{}
	for _, update := range status.Updates {
		updates[update.Topic] = update
	}
	metadata := updates["BMS/v1/PUB/Metadata/Rack/RackPower/a"]
	if metadata.Count != 1 || metadata.Numeric != nil {
		t.Fatalf("metadata update = %#v, want count-only without numeric aggregate", metadata)
	}
	event := updates["grid/v1/poweragent/a/powerstate/status"]
	if event.Numeric == nil {
		t.Fatal("CloudEvent-style numeric aggregate missing")
	}
	if event.Numeric.Field != "data.value" || event.Numeric.Count != 2 || event.Numeric.Min != 10 || event.Numeric.Max != 20 || event.Numeric.Mean != 15 || event.Numeric.Latest != 20 {
		t.Fatalf("CloudEvent numeric aggregate = %#v, want field=data.value count=2 min=10 max=20 mean=15 latest=20", event.Numeric)
	}
}

func TestWatchManagerStartAdmissionLimitFailsFast(t *testing.T) {
	cfg := Config{
		WatchDefaultTTLS:           30,
		WatchMaxTTLS:               60,
		WatchDefaultBufferMessages: 10,
		WatchMaxBufferMessages:     100,
		WatchDefaultBufferBytes:    8192,
		WatchMaxBufferBytes:        8192,
		WatchMaxPerSession:         2,
		WatchMaxPerPod:             10,
	}
	normalizeConfig(&cfg)
	cfg.watchStartAdmission = newAdmissionLimiter(1)
	if !cfg.watchStartAdmission.tryAcquire() {
		t.Fatal("pre-acquire watch-start admission failed")
	}
	m := newWatchManager(cfg)

	_, err := m.start(watchStartRequest{
		Caller:      testCaller(),
		TopicFilter: "BMS/v1/PUB/Value/Rack/RackPower/#",
	})
	if got := mqttbus.ErrorCode(err); got != mqttbus.CodeMQTTAdmissionLimited {
		t.Fatalf("start admission error code = %q, want %q", got, mqttbus.CodeMQTTAdmissionLimited)
	}
	if retry := retryAfterSeconds(err); retry != 1 {
		t.Fatalf("retry_after_seconds = %d, want 1", retry)
	}
}

func TestWatchManagerRequiresStatefulSession(t *testing.T) {
	cfg := Config{}
	normalizeConfig(&cfg)
	m := newWatchManager(cfg)
	caller := testCaller()
	caller.SessionID = ""

	_, err := m.start(watchStartRequest{
		Caller:      caller,
		TopicFilter: "BMS/v1/PUB/Value/Rack/RackPower/#",
	})
	if got := mqttbus.ErrorCode(err); got != codeStatefulSessionRequired {
		t.Fatalf("start without session code = %q, want %q", got, codeStatefulSessionRequired)
	}
}

func fakeStreamRunner(messages []mqttbus.Message) streamRunner {
	return func(ctx context.Context, _ mqttbus.Config, _, _ string, opts mqttbus.StreamOptions, onMessage func(mqttbus.Message) error) (mqttbus.StreamResult, error) {
		for _, msg := range messages {
			if err := onMessage(msg); err != nil {
				return mqttbus.StreamResult{}, err
			}
		}
		if opts.OnSubscribed != nil {
			opts.OnSubscribed()
		}
		<-ctx.Done()
		return mqttbus.StreamResult{Count: len(messages), StoppedReason: mqttbus.StoppedCallerCancel}, ctx.Err()
	}
}

func testCaller() auth.Caller {
	return auth.Caller{
		Bearer:    "test-token",
		SessionID: "session-1",
		Tenant:    "tenant-1",
		Issuer:    "issuer-1",
		Subject:   "subject-1",
		SpiffeID:  "spiffe://test",
	}
}
