// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"
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
