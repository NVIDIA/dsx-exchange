// Copyright 2026 NVIDIA Corporation
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//
// SPDX-License-Identifier: Apache-2.0

package leaf

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	mcptransport "github.com/mark3labs/mcp-go/client/transport"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/micro"

	bridgemcp "github.com/NVIDIA/dsx-exchange/dsx-agentgateway-bridge/internal/mcp"
	"github.com/NVIDIA/dsx-exchange/dsx-agentgateway-bridge/internal/shardbus"
)

const testSubjectPrefix = "dsx.test.bridge"

const (
	testCallRequest   = `{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{}}`
	testStreamID      = "_INBOX.stream"
	testStreamSubject = "leaf.instance"
)

func TestPublishStartupSendsShardHeader(t *testing.T) {
	t.Parallel()

	pub := &fakePublisher{}
	if err := publishLeafStartup(pub, testSubjectPrefix, "cpc-1"); err != nil {
		t.Fatalf("publishLeafStartup: %v", err)
	}
	if pub.msg == nil {
		t.Fatal("PublishMsg was not called")
	}
	if got, want := pub.msg.Subject, shardbus.StartupSubject(testSubjectPrefix); got != want {
		t.Fatalf("startup subject = %q, want %q", got, want)
	}
	if got := pub.msg.Header.Get(shardbus.ShardIDHeader); got != "cpc-1" {
		t.Fatalf("startup shard header = %q, want cpc-1", got)
	}
}

func TestDiscoveryResponderAddsShardHeader(t *testing.T) {
	t.Parallel()

	pub := &fakePublisher{}
	req := newFakeRequest(pub, nil, "reply")
	handleShardDiscovery(pub, req.reply, "cpc-1")
	resp := nextResponse(t, req)
	if got := resp.headers.Get(shardbus.ShardIDHeader); got != "cpc-1" {
		t.Fatalf("discovery response shard header = %q, want cpc-1", got)
	}
	if len(resp.data) != 0 {
		t.Fatalf("discovery response data = %q, want empty", resp.data)
	}
}

func TestListResponderAddsShardHeaderAndForwardsMCP(t *testing.T) {
	t.Parallel()

	pub := &fakePublisher{}
	req := newFakeRequest(pub, []byte(`{"jsonrpc":"2.0","id":7,"method":"tools/list","params":{}}`), "reply")
	handleList(t.Context(), pub, req.reply, req.data, http.Header(req.headers), "cpc-1", "http:///")
	response := nextResponse(t, req)
	if got := response.headers.Get(shardbus.ShardIDHeader); got != "cpc-1" {
		t.Fatalf("list response shard header = %q, want cpc-1", got)
	}
	assertRPCErrorCode(t, response.data, bridgemcp.CodeLeafForwardFailed)
}

func TestForwardJSONReturnsInlineReply(t *testing.T) {
	t.Parallel()

	local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		if _, err := w.Write([]byte(`{"jsonrpc":"2.0","id":7,"result":{}}`)); err != nil {
			t.Errorf("write JSON response: %v", err)
		}
	}))
	defer local.Close()
	registry := newTestStreamRegistry(t, time.Second)
	req := newFakeRequest(registry.pub.(*fakePublisher), []byte(`{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"json","arguments":{}}}`), "_INBOX.initial")

	registry.requestTasks.Go(func() {
		handleInitialRequest(t.Context(), req.data, http.Header(req.headers), req.reply, local.URL, registry, "direct.subject")
	})
	response := nextResponse(t, req)
	if response.reply != "" {
		t.Fatalf("JSON response reply subject = %q, want empty", response.reply)
	}
	if got := response.headers.Get(shardbus.StreamIDHeader); got != "" {
		t.Fatalf("JSON stream ID = %q, want empty", got)
	}
	if got := response.headers.Get(shardbus.HTTPStatusHeader); got != "202" {
		t.Fatalf("JSON status = %q, want 202", got)
	}
	if !json.Valid(response.data) {
		t.Fatalf("JSON reply is invalid: %s", response.data)
	}
	assertNoResponse(t, req)
}

func TestForwardSSEReturnsStreamSubjectAndID(t *testing.T) {
	t.Parallel()

	local, _ := newBlockingSSEServer(t)
	registry := newTestStreamRegistry(t, 20*time.Millisecond)
	req := newFakeRequest(registry.pub.(*fakePublisher), []byte(testCallRequest), testStreamID)
	initialCtx, cancelInitial := context.WithCancel(t.Context())
	defer cancelInitial()

	registry.requestTasks.Go(func() {
		handleInitialRequest(initialCtx, req.data, http.Header(req.headers), req.reply, local.URL, registry, testStreamSubject)
	})
	response := nextResponse(t, req)
	if len(response.data) != 0 {
		t.Fatalf("SSE metadata body = %q, want empty", response.data)
	}
	if response.reply != testStreamSubject {
		t.Fatalf("SSE stream subject = %q, want %s", response.reply, testStreamSubject)
	}
	if got := response.headers.Get(shardbus.StreamIDHeader); got != testStreamID {
		t.Fatalf("SSE stream ID = %q, want initial reply inbox", got)
	}
	if got := response.headers.Get(shardbus.HTTPContentTypeHeader); got != "text/event-stream" {
		t.Fatalf("SSE content type = %q, want text/event-stream", got)
	}
	if registry.lookup(testStreamID) == nil {
		t.Fatal("SSE stream was not registered")
	}
	cancelInitial()
	waitSignal(t, initialCtx.Done(), "initial context cancellation")
	if pending := readStream(t, registry, testStreamID, "0"); pending.headers.Get(shardbus.StreamFrameHeader) != shardbus.StreamFramePending {
		t.Fatalf("SSE frame after initial deadline = %q, want pending", pending.headers.Get(shardbus.StreamFrameHeader))
	}
}

func TestForwardSSECloseCancelsHTTPRequest(t *testing.T) {
	t.Parallel()

	local, canceled := newBlockingSSEServer(t)
	registry := newTestStreamRegistry(t, time.Second)
	req := newFakeRequest(registry.pub.(*fakePublisher), []byte(testCallRequest), testStreamID)

	registry.requestTasks.Go(func() {
		handleInitialRequest(t.Context(), req.data, http.Header(req.headers), req.reply, local.URL, registry, testStreamSubject)
	})
	nextResponse(t, req)
	if registry.lookup(testStreamID) == nil {
		t.Fatal("SSE stream was not registered")
	}
	closeRequest := newStreamCloseRequest(registry, testStreamID)
	registry.handleStreamRequest(closeRequest.headers, closeRequest.reply)
	if response := nextResponse(t, closeRequest); response.errorCode != "" || len(response.data) != 0 {
		t.Fatalf("close response = data %q error %q, want empty success", response.data, response.errorCode)
	}
	waitSignal(t, canceled, "CLOSE to cancel the upstream HTTP request")
	if registry.lookup(testStreamID) != nil {
		t.Fatal("closed HTTP request remains registered")
	}
}

func TestForwardSSEIdleTimeoutCancelsHTTPRequest(t *testing.T) {
	t.Parallel()

	local, canceled := newBlockingSSEServer(t)
	registry := newTestStreamRegistry(t, time.Second)
	registry.idleTimeout = 20 * time.Millisecond
	req := newFakeRequest(registry.pub.(*fakePublisher), []byte(testCallRequest), testStreamID)

	registry.requestTasks.Go(func() {
		handleInitialRequest(t.Context(), req.data, http.Header(req.headers), req.reply, local.URL, registry, testStreamSubject)
	})
	nextResponse(t, req)
	waitSignal(t, canceled, "idle expiry to cancel the upstream HTTP request")
	if registry.lookup(testStreamID) != nil {
		t.Fatal("idle HTTP request remains registered")
	}
}

func TestForwardSSEStartReplyFailureClosesStream(t *testing.T) {
	t.Parallel()

	local, canceled := newBlockingSSEServer(t)
	registry := newTestStreamRegistry(t, time.Second)
	req := newFakeRequest(registry.pub.(*fakePublisher), []byte(testCallRequest), testStreamID)
	registry.pub.(*fakePublisher).err = errors.New("reply failed")
	handled := make(chan struct{})

	registry.requestTasks.Go(func() {
		defer close(handled)
		handleInitialRequest(t.Context(), req.data, http.Header(req.headers), req.reply, local.URL, registry, testStreamSubject)
	})

	waitSignal(t, handled, "failed SSE start")
	if registry.lookup(testStreamID) != nil {
		t.Fatal("failed SSE start remains registered")
	}
	waitSignal(t, canceled, "failed SSE start cancellation")
	assertNoResponse(t, req)
}

func TestForwardFailureUsesOneJSONRPCReply(t *testing.T) {
	t.Parallel()

	registry := newTestStreamRegistry(t, time.Second)
	req := newFakeRequest(registry.pub.(*fakePublisher), []byte(`{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{}}`), "_INBOX.initial")
	registry.requestTasks.Go(func() {
		handleInitialRequest(t.Context(), req.data, http.Header(req.headers), req.reply, "http:///", registry, testStreamSubject)
	})
	response := nextResponse(t, req)
	if got := response.headers.Get(shardbus.HTTPContentTypeHeader); got != "application/json" {
		t.Fatalf("failure content type = %q, want application/json", got)
	}
	assertRPCErrorCode(t, response.data, bridgemcp.CodeLeafForwardFailed)
	assertNoResponse(t, req)
}

func TestStreamRegistryCloseCancelsForwardBeforeHeaders(t *testing.T) {
	t.Parallel()

	started := make(chan struct{})
	release := make(chan struct{})
	local := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		close(started)
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	defer local.Close()
	releaseServer := sync.OnceFunc(func() { close(release) })
	defer releaseServer()
	registry := newTestStreamRegistry(t, time.Second)
	req := newFakeRequest(registry.pub.(*fakePublisher), []byte(`{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{}}`), "_INBOX.initial")
	handled := make(chan struct{})
	registry.requestTasks.Go(func() {
		handleInitialRequest(t.Context(), req.data, http.Header(req.headers), req.reply, local.URL, registry, testStreamSubject)
		close(handled)
	})

	waitSignal(t, started, "the upstream HTTP request")
	select {
	case <-handled:
		t.Fatal("initial NATS handler returned before its reply")
	default:
	}
	closed := make(chan struct{})
	go func() {
		registry.close()
		close(closed)
	}()
	waitSignal(t, closed, "stream shutdown")
	waitSignal(t, handled, "the initial NATS handler")
	response := nextResponse(t, req)
	assertRPCErrorCode(t, response.data, bridgemcp.CodeLeafForwardFailed)
	if !strings.Contains(string(response.data), "context canceled") {
		t.Fatalf("shutdown response = %s, want HTTP context cancellation", response.data)
	}
	if registry.lookup("_INBOX.initial") != nil {
		t.Fatal("canceled initial HTTP request was registered as a stream")
	}
	releaseServer()
}

func TestInitialDeadlineCancelsBlockedJSONBody(t *testing.T) {
	t.Parallel()

	headersSent := make(chan struct{})
	canceled := make(chan struct{})
	release := make(chan struct{})
	local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write([]byte(`{"jsonrpc":"2.0","id":7,"result":`)); err != nil {
			t.Errorf("write partial JSON response: %v", err)
			return
		}
		w.(http.Flusher).Flush()
		close(headersSent)
		select {
		case <-r.Context().Done():
			close(canceled)
		case <-release:
		}
	}))
	defer local.Close()
	releaseServer := sync.OnceFunc(func() { close(release) })
	defer releaseServer()
	registry := newTestStreamRegistry(t, time.Hour)
	req := newFakeRequest(registry.pub.(*fakePublisher), []byte(`{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{}}`), "_INBOX.initial")
	initialCtx, cancelInitial := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancelInitial()
	handled := make(chan struct{})
	registry.requestTasks.Go(func() {
		handleInitialRequest(initialCtx, req.data, http.Header(req.headers), req.reply, local.URL, registry, testStreamSubject)
		close(handled)
	})

	waitSignal(t, headersSent, "JSON response headers")
	select {
	case <-handled:
		t.Fatal("initial NATS handler returned before the JSON body completed")
	default:
	}
	waitSignal(t, initialCtx.Done(), "initial request deadline")
	waitSignal(t, handled, "the JSON NATS handler")
	waitSignal(t, canceled, "JSON HTTP context cancellation")
	response := nextResponse(t, req)
	assertRPCErrorCode(t, response.data, bridgemcp.CodeLeafForwardFailed)
	if registry.lookup("_INBOX.initial") != nil {
		t.Fatal("blocked JSON request was registered as an SSE stream")
	}
	releaseServer()
}

func TestStreamReadReplaysFrameThenEnds(t *testing.T) {
	t.Parallel()

	registry, body, _ := newControlledStream(t, time.Second)
	body.Send([]byte("frame"), nil)

	first := readStream(t, registry, "stream", "0")
	if string(first.data) != "frame" || first.headers.Get(shardbus.StreamFrameHeader) != "" {
		t.Fatalf("first stream reply = data %q frame %q", first.data, first.headers.Get(shardbus.StreamFrameHeader))
	}
	replay := readStream(t, registry, "stream", "0")
	if string(replay.data) != "frame" {
		t.Fatalf("replayed stream data = %q, want frame", replay.data)
	}
	if got := body.ReadCount(); got != 1 {
		t.Fatalf("body read count after replay = %d, want 1", got)
	}
	body.Send(nil, io.EOF)
	end := readStream(t, registry, "stream", "1")
	if got := end.headers.Get(shardbus.StreamFrameHeader); got != shardbus.StreamFrameEnd {
		t.Fatalf("terminal frame = %q, want end", got)
	}
	if len(end.data) != 0 {
		t.Fatalf("terminal frame body = %q, want empty", end.data)
	}
	assertBodyClosed(t, body.closed)
	replayedEnd := readStream(t, registry, "stream", "1")
	if got := replayedEnd.headers.Get(shardbus.StreamFrameHeader); got != shardbus.StreamFrameEnd {
		t.Fatalf("replayed terminal frame = %q, want end", got)
	}
}

func TestStreamReadLongPollReturnsPendingThenData(t *testing.T) {
	t.Parallel()

	registry, body, _ := newControlledStream(t, 20*time.Millisecond)

	pending := readStream(t, registry, "stream", "0")
	if got := pending.headers.Get(shardbus.StreamFrameHeader); got != shardbus.StreamFramePending {
		t.Fatalf("idle frame = %q, want pending", got)
	}
	body.Send([]byte("later"), nil)
	data := readStream(t, registry, "stream", "0")
	if string(data.data) != "later" {
		t.Fatalf("long-poll data = %q, want later", data.data)
	}
	if got := body.ReadCount(); got != 1 {
		t.Fatalf("body read count = %d, want one in-flight read", got)
	}
}

func TestStreamReadReturnsPendingWhenStreamIsNotServing(t *testing.T) {
	registry := newTestStreamRegistry(t, 20*time.Millisecond)
	stream := newStream(registry.ctx)
	registry.requestTasks.Go(func() { <-stream.ctx.Done() })
	registry.register("stream", stream)
	request := newStreamReadRequest(registry, "stream", "0")
	handled := make(chan struct{})
	registry.streamRequestTasks.Go(func() {
		registry.handleStreamRequest(request.headers, request.reply)
		close(handled)
	})

	waitSignal(t, handled, "READ handler return")
	response := nextResponse(t, request)
	if got := response.headers.Get(shardbus.StreamFrameHeader); got != shardbus.StreamFramePending {
		t.Fatalf("blocked dispatch frame = %q, want pending", got)
	}
}

func TestDuplicateStreamReadsShareOneFrame(t *testing.T) {
	t.Parallel()

	registry, body, _ := newControlledStream(t, time.Second)
	first := newStreamReadRequest(registry, "stream", "0")
	second := newStreamReadRequest(registry, "stream", "0")
	registry.streamRequestTasks.Go(func() { registry.handleStreamRequest(first.headers, first.reply) })
	registry.streamRequestTasks.Go(func() { registry.handleStreamRequest(second.headers, second.reply) })

	body.Send([]byte("frame"), nil)
	for _, request := range []*fakeRequest{first, second} {
		if response := nextResponse(t, request); string(response.data) != "frame" {
			t.Fatalf("duplicate stream read data = %q, want frame", response.data)
		}
	}
	if got := body.ReadCount(); got != 1 {
		t.Fatalf("body read count = %d, want one shared read", got)
	}
}

func TestLateStreamReadDoesNotRewindCursor(t *testing.T) {
	t.Parallel()

	registry, body, _ := newControlledStream(t, time.Second)
	body.Send([]byte("first"), nil)
	readStream(t, registry, "stream", "0")
	body.Send([]byte("second"), nil)
	second := readStream(t, registry, "stream", "1")

	if stale := readStream(t, registry, "stream", "0"); stale.errorCode != "409" {
		t.Fatalf("late read error = %q, want 409", stale.errorCode)
	}
	if replay := readStream(t, registry, "stream", "1"); string(replay.data) != string(second.data) {
		t.Fatalf("replayed data = %q, want %q", replay.data, second.data)
	}
}

func TestInvalidStreamRequestsAreRejected(t *testing.T) {
	t.Parallel()

	registry := newTestStreamRegistry(t, time.Second)
	unknown := readStream(t, registry, "missing", "0")
	if unknown.errorCode != "404" {
		t.Fatalf("unknown stream error = %q, want 404", unknown.errorCode)
	}

	startTestStream(t, registry, "stream", io.NopCloser(strings.NewReader("frame")), []byte(`{"jsonrpc":"2.0","id":7}`))
	invalid := readStream(t, registry, "stream", " 0")
	if invalid.errorCode != "409" {
		t.Fatalf("invalid cursor error = %q, want 409", invalid.errorCode)
	}

	invalidOperation := newFakeRequest(registry.pub.(*fakePublisher), nil, nats.NewInbox())
	invalidOperation.headers.Set(shardbus.StreamOperationHeader, "write")
	registry.handleStreamRequest(invalidOperation.headers, invalidOperation.reply)
	if response := nextResponse(t, invalidOperation); response.errorCode != "400" {
		t.Fatalf("invalid operation error = %q, want 400", response.errorCode)
	}
}

func TestStreamsOnOneLeafReadIndependently(t *testing.T) {
	t.Parallel()

	registry := newTestStreamRegistry(t, time.Second)
	firstBody := newControlledReadCloser()
	secondBody := newControlledReadCloser()
	startTestStream(t, registry, "first", firstBody, []byte(`{"jsonrpc":"2.0","id":1}`))
	startTestStream(t, registry, "second", secondBody, []byte(`{"jsonrpc":"2.0","id":2}`))
	first := newStreamReadRequest(registry, "first", "0")
	second := newStreamReadRequest(registry, "second", "0")
	registry.streamRequestTasks.Go(func() { registry.handleStreamRequest(first.headers, first.reply) })
	registry.streamRequestTasks.Go(func() { registry.handleStreamRequest(second.headers, second.reply) })

	secondBody.Send([]byte("second"), nil)
	if got := nextResponse(t, second); string(got.data) != "second" {
		t.Fatalf("second stream data = %q, want second", got.data)
	}
	assertNoResponse(t, first)
	firstBody.Send([]byte("first"), nil)
	if got := nextResponse(t, first); string(got.data) != "first" {
		t.Fatalf("first stream data = %q, want first", got.data)
	}
}

func TestStreamReadFailureReturnsErrorThenEnds(t *testing.T) {
	t.Parallel()

	registry, body, _ := newControlledStream(t, time.Second)
	body.Send(nil, errors.New("upstream broke"))
	errorFrame := readStream(t, registry, "stream", "0")
	if !strings.Contains(string(errorFrame.data), `"code":-32003`) {
		t.Fatalf("stream error frame = %q, want leaf JSON-RPC error", errorFrame.data)
	}
	if !strings.Contains(string(errorFrame.data), `"id":7`) {
		t.Fatalf("stream error frame = %q, want original request ID", errorFrame.data)
	}
	assertBodyClosed(t, body.closed)
	end := readStream(t, registry, "stream", "1")
	if got := end.headers.Get(shardbus.StreamFrameHeader); got != shardbus.StreamFrameEnd {
		t.Fatalf("frame after stream error = %q, want end", got)
	}
}

func TestStreamReadWithDataAndErrorReturnsDataThenEnds(t *testing.T) {
	t.Parallel()

	registry, body, _ := newControlledStream(t, time.Second)
	result := "event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":7,\"result\":{\"content\":[]}}\n\n"
	body.Send([]byte(result), io.ErrUnexpectedEOF)

	resultFrame := readStream(t, registry, "stream", "0")
	if string(resultFrame.data) != result {
		t.Fatalf("result frame = %q, want %q", resultFrame.data, result)
	}
	assertBodyClosed(t, body.closed)
	end := readStream(t, registry, "stream", "1")
	if got := end.headers.Get(shardbus.StreamFrameHeader); got != shardbus.StreamFrameEnd {
		t.Fatalf("frame after result bytes = %q, want end", got)
	}
	if len(end.data) != 0 {
		t.Fatalf("end frame data = %q, want no synthetic error", end.data)
	}
}

func TestStreamIdleTimeoutClosesAbandonedBody(t *testing.T) {
	t.Parallel()

	registry := newTestStreamRegistry(t, time.Second)
	registry.idleTimeout = 20 * time.Millisecond
	body := newControlledReadCloser()
	startTestStream(t, registry, "stream", body, []byte(testCallRequest))
	waitSignal(t, body.closed, "abandoned stream cleanup")
	if registry.lookup("stream") != nil {
		t.Fatal("expired stream remains registered")
	}
}

func TestStreamIdleTimeoutCoversFrameDeliveryAndRetry(t *testing.T) {
	requestTimeout := 50 * time.Millisecond
	registry := newTestStreamRegistry(t, requestTimeout)
	frameReplyAllowance := time.Second
	droppedReadAttempt := requestTimeout + time.Second
	maximumGapAfterFrame := frameReplyAllowance + requestTimeout + droppedReadAttempt
	if margin := registry.idleTimeout - maximumGapAfterFrame; margin < time.Second {
		t.Fatalf("stream idle margin = %s, want at least one second", margin)
	}
}

func TestValidReadsRenewStreamIdleTimeout(t *testing.T) {
	t.Parallel()

	registry := newTestStreamRegistry(t, 10*time.Millisecond)
	registry.idleTimeout = 100 * time.Millisecond
	body := newControlledReadCloser()
	startTestStream(t, registry, "stream", body, []byte(`{"jsonrpc":"2.0","id":7}`))

	for range 12 {
		response := readStream(t, registry, "stream", "0")
		if got := response.headers.Get(shardbus.StreamFrameHeader); got != shardbus.StreamFramePending {
			t.Fatalf("renewal frame = %q, want pending", got)
		}
	}
	body.Send([]byte("still-open"), nil)
	if response := readStream(t, registry, "stream", "0"); string(response.data) != "still-open" {
		t.Fatalf("post-renewal data = %q, want still-open", response.data)
	}
}

func TestStreamRegistryCloseWaitsForAllTasks(t *testing.T) {
	t.Parallel()

	registry := newTestStreamRegistry(t, time.Second)
	firstBody := newControlledReadCloser()
	secondBody := newControlledReadCloser()
	startTestStream(t, registry, "first", firstBody, []byte(`{"jsonrpc":"2.0","id":1}`))
	startTestStream(t, registry, "second", secondBody, []byte(`{"jsonrpc":"2.0","id":2}`))
	firstRead := newStreamReadRequest(registry, "first", "0")
	secondRead := newStreamReadRequest(registry, "second", "0")
	registry.streamRequestTasks.Go(func() { registry.handleStreamRequest(firstRead.headers, firstRead.reply) })
	registry.streamRequestTasks.Go(func() { registry.handleStreamRequest(secondRead.headers, secondRead.reply) })
	waitSignal(t, firstBody.started, "first stream read")
	waitSignal(t, secondBody.started, "second stream read")

	registry.close()

	for _, closed := range []<-chan struct{}{firstBody.closed, secondBody.closed} {
		assertBodyClosed(t, closed)
	}
	for _, request := range []*fakeRequest{firstRead, secondRead} {
		if response := nextResponse(t, request); response.errorCode != "409" {
			t.Fatalf("shutdown stream read error = %q, want 409", response.errorCode)
		}
	}
	if registry.lookup("first") != nil || registry.lookup("second") != nil {
		t.Fatal("closed stream registry retains a stream")
	}
}

func TestStreamRegistryStartsRequestsConcurrently(t *testing.T) {
	t.Parallel()

	registry := newTestStreamRegistry(t, time.Second)
	release := make(chan struct{})
	releaseRequests := sync.OnceFunc(func() { close(release) })
	defer releaseRequests()
	firstStarted := make(chan struct{})
	secondStarted := make(chan struct{})
	registry.requestTasks.Go(func() {
		close(firstStarted)
		<-release
	})
	registry.requestTasks.Go(func() { close(secondStarted) })

	waitSignal(t, firstStarted, "first request")
	waitSignal(t, secondStarted, "second concurrent request")
	releaseRequests()
}

func TestStreamRegistryWaitsForRequests(t *testing.T) {
	t.Parallel()

	parent, cancelParent := context.WithCancel(t.Context())
	registry := newStreamRegistry(parent, &fakePublisher{}, time.Second)
	t.Cleanup(registry.close)
	release := make(chan struct{})
	finished := make(chan struct{})
	registry.requestTasks.Go(func() {
		<-release
		close(finished)
	})
	cancelParent()
	streamRequestFinished := make(chan struct{})
	registry.streamRequestTasks.Go(func() { close(streamRequestFinished) })
	waitSignal(t, streamRequestFinished, "stream request during request drain")

	waited := make(chan struct{})
	go func() {
		registry.waitForRequests(t.Context())
		close(waited)
	}()
	select {
	case <-registry.ctx.Done():
		t.Fatal("registry canceled request work during its grace period")
	case <-waited:
		t.Fatal("registry stopped waiting before the request finished")
	default:
	}
	close(release)
	waitSignal(t, finished, "request completion")
	waitSignal(t, waited, "request wait")
}

func TestStreamCancellationClosesBodyAndUnregistersStream(t *testing.T) {
	t.Parallel()

	registry, body, stream := newControlledStream(t, time.Second)
	request := newStreamReadRequest(registry, "stream", "0")
	registry.streamRequestTasks.Go(func() { registry.handleStreamRequest(request.headers, request.reply) })
	waitSignal(t, body.started, "stream read")

	stream.cancel()
	if response := nextResponse(t, request); response.errorCode != "409" {
		t.Fatalf("canceled stream read error = %q, want 409", response.errorCode)
	}
	assertBodyClosed(t, body.closed)
	if registry.lookup("stream") != nil {
		t.Fatal("canceled stream remains registered")
	}
}

func TestStreamCloseCancelsStreamAndIsIdempotent(t *testing.T) {
	t.Parallel()

	registry, body, _ := newControlledStream(t, time.Second)
	read := newStreamReadRequest(registry, "stream", "0")
	registry.streamRequestTasks.Go(func() { registry.handleStreamRequest(read.headers, read.reply) })
	waitSignal(t, body.started, "stream read")

	closeRequest := newStreamCloseRequest(registry, "stream")
	registry.handleStreamRequest(closeRequest.headers, closeRequest.reply)
	if response := nextResponse(t, closeRequest); len(response.data) != 0 || response.errorCode != "" {
		t.Fatalf("stream close response = data %q error %q, want empty success", response.data, response.errorCode)
	}
	if response := nextResponse(t, read); response.errorCode != "409" {
		t.Fatalf("closed in-flight read error = %q, want 409", response.errorCode)
	}
	assertBodyClosed(t, body.closed)
	if registry.lookup("stream") != nil {
		t.Fatal("closed stream remains registered")
	}

	repeated := newStreamCloseRequest(registry, "stream")
	registry.handleStreamRequest(repeated.headers, repeated.reply)
	if response := nextResponse(t, repeated); len(response.data) != 0 || response.errorCode != "" {
		t.Fatalf("repeated stream close response = data %q error %q, want empty success", response.data, response.errorCode)
	}
}

func readStream(t *testing.T, registry *streamRegistry, streamID, cursor string) fakeMicroResponse {
	t.Helper()
	req := newStreamReadRequest(registry, streamID, cursor)
	registry.handleStreamRequest(req.headers, req.reply)
	return nextResponse(t, req)
}

func newStreamReadRequest(registry *streamRegistry, streamID, cursor string) *fakeRequest {
	req := newFakeRequest(registry.pub.(*fakePublisher), nil, nats.NewInbox())
	req.headers.Set(shardbus.StreamIDHeader, streamID)
	req.headers.Set(shardbus.StreamCursorHeader, cursor)
	req.headers.Set(shardbus.StreamOperationHeader, shardbus.StreamOperationRead)
	return req
}

func newStreamCloseRequest(registry *streamRegistry, streamID string) *fakeRequest {
	req := newFakeRequest(registry.pub.(*fakePublisher), nil, nats.NewInbox())
	req.headers.Set(shardbus.StreamIDHeader, streamID)
	req.headers.Set(shardbus.StreamOperationHeader, shardbus.StreamOperationClose)
	return req
}

func newBlockingSSEServer(t *testing.T) (*httptest.Server, <-chan struct{}) {
	t.Helper()
	canceled := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
		close(canceled)
	}))
	t.Cleanup(server.Close)
	return server, canceled
}

func newTestStreamRegistry(t *testing.T, timeout time.Duration) *streamRegistry {
	t.Helper()
	registry := newStreamRegistry(t.Context(), &fakePublisher{}, timeout)
	t.Cleanup(registry.close)
	return registry
}

func newControlledStream(t *testing.T, timeout time.Duration) (*streamRegistry, *controlledReadCloser, *streamHandle) {
	t.Helper()
	registry := newTestStreamRegistry(t, timeout)
	body := newControlledReadCloser()
	return registry, body, startTestStream(t, registry, "stream", body, []byte(testCallRequest))
}

func startTestStream(t *testing.T, registry *streamRegistry, id string, body io.ReadCloser, requestData []byte) *streamHandle {
	t.Helper()
	registered := make(chan struct{})
	stream := newStream(registry.ctx)
	registry.requestTasks.Go(func() {
		registry.register(id, stream)
		close(registered)
		body = stream.serveReads(body, rpcRequestID(requestData), registry.idleTimeout)
		registry.take(id)
		stream.cancel()
		closeResponseBody(body)
	})
	<-registered
	return stream
}

func nextResponse(t *testing.T, req *fakeRequest) fakeMicroResponse {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	select {
	case response := <-req.responseC:
		return response
	case <-ctx.Done():
		t.Fatal("timed out waiting for NATS response")
		return fakeMicroResponse{}
	}
}

func waitSignal(t *testing.T, signal <-chan struct{}, description string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	select {
	case <-signal:
	case <-ctx.Done():
		t.Fatalf("timed out waiting for %s", description)
	}
}

func assertRPCErrorCode(t *testing.T, data []byte, want int) {
	t.Helper()
	var response mcptransport.JSONRPCResponse
	if err := json.Unmarshal(data, &response); err != nil {
		t.Fatalf("decode RPC response: %v", err)
	}
	if response.Error == nil || response.Error.Code != want {
		t.Fatalf("RPC error = %+v, want code %d", response.Error, want)
	}
}

func assertNoResponse(t *testing.T, req *fakeRequest) {
	t.Helper()
	select {
	case response := <-req.responseC:
		t.Fatalf("unexpected extra NATS response: %+v", response)
	default:
	}
}

func assertBodyClosed(t *testing.T, closed <-chan struct{}) {
	t.Helper()
	waitSignal(t, closed, "stream body close")
}

type fakePublisher struct {
	mu      sync.Mutex
	msg     *nats.Msg
	replies map[string]chan fakeMicroResponse
	err     error
}

func (f *fakePublisher) PublishMsg(msg *nats.Msg) error {
	if f.err != nil {
		return f.err
	}
	f.mu.Lock()
	f.msg = msg
	responseC := f.replies[msg.Subject]
	f.mu.Unlock()
	if responseC != nil {
		responseC <- fakeMicroResponse{
			data:      msg.Data,
			headers:   msg.Header,
			reply:     msg.Reply,
			errorCode: msg.Header.Get(micro.ErrorCodeHeader),
		}
	}
	return nil
}

func (f *fakePublisher) register(reply string, responseC chan fakeMicroResponse) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.replies == nil {
		f.replies = make(map[string]chan fakeMicroResponse)
	}
	f.replies[reply] = responseC
}

type fakeRequest struct {
	data      []byte
	headers   nats.Header
	reply     string
	responseC chan fakeMicroResponse
}

type fakeMicroResponse struct {
	data      []byte
	headers   nats.Header
	reply     string
	errorCode string
}

func newFakeRequest(pub *fakePublisher, data []byte, reply string) *fakeRequest {
	request := &fakeRequest{
		data:      data,
		headers:   nats.Header{"Content-Type": []string{"application/json"}},
		reply:     reply,
		responseC: make(chan fakeMicroResponse, 2),
	}
	pub.register(reply, request.responseC)
	return request
}

type readResult struct {
	data []byte
	err  error
}

type controlledReadCloser struct {
	results   chan readResult
	started   chan struct{}
	closed    chan struct{}
	startOnce sync.Once
	closeOnce sync.Once
	reads     atomic.Int64
}

func newControlledReadCloser() *controlledReadCloser {
	return &controlledReadCloser{
		results: make(chan readResult, 1),
		started: make(chan struct{}),
		closed:  make(chan struct{}),
	}
}

func (r *controlledReadCloser) Read(data []byte) (int, error) {
	r.reads.Add(1)
	r.startOnce.Do(func() { close(r.started) })
	select {
	case result := <-r.results:
		return copy(data, result.data), result.err
	case <-r.closed:
		return 0, io.ErrClosedPipe
	}
}

func (r *controlledReadCloser) Send(data []byte, err error) {
	r.results <- readResult{data: data, err: err}
}

func (r *controlledReadCloser) ReadCount() int {
	return int(r.reads.Load())
}

func (r *controlledReadCloser) Close() error {
	r.closeOnce.Do(func() { close(r.closed) })
	return nil
}
