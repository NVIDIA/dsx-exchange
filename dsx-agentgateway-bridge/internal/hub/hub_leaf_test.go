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

package hub

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"iter"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	mcptransport "github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/micro"
	"github.com/synadia-io/orbit.go/natsext"

	"github.com/NVIDIA/dsx-exchange/dsx-agentgateway-bridge/internal/config"
	bridgemcp "github.com/NVIDIA/dsx-exchange/dsx-agentgateway-bridge/internal/mcp"
	"github.com/NVIDIA/dsx-exchange/dsx-agentgateway-bridge/internal/shardbus"
)

const (
	testSubjectPrefix = "dsx.test.bridge"
	testSSEResult     = "event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{}}\n\n"
)

func TestLeafUsesExpectedQueueNames(t *testing.T) {
	if got, want := shardbus.QueueGroup("cpc-1"), "dsx-agentgateway-bridge-cpc-1"; got != want {
		t.Fatalf("shardbus.QueueGroup() = %q, want %q", got, want)
	}
	if got, want := shardbus.MCPSubject(testSubjectPrefix, "cpc-1"), subject("leaf.cpc-1.mcp"); got != want {
		t.Fatalf("shardbus.MCPSubject() = %q, want %q", got, want)
	}
	streamSubject := shardbus.MCPStreamSubject(testSubjectPrefix, "cpc-1", "instance-id")
	if got, want := streamSubject, subject("leaf.cpc-1.mcp.instance-id"); got != want {
		t.Fatalf("shardbus.MCPStreamSubject() = %q, want %q", got, want)
	}
	if got, want := shardbus.ListSubject(testSubjectPrefix), subject("leaf.list"); got != want {
		t.Fatalf("shardbus.ListSubject() = %q, want %q", got, want)
	}
	if got, want := shardbus.ListQueueGroup, "dsx-agentgateway-bridge-list"; got != want {
		t.Fatalf("shardbus.ListQueueGroup = %q, want %q", got, want)
	}
}

func TestNewHubHandlerAcceptsValidConfig(t *testing.T) {
	handler, err := NewHandler(config.Hub{
		NATSURL: "nats://example:4222",
		Bus:     newStaticNATSRequester(t, nil),
	})
	if err != nil {
		t.Fatalf("NewHandler() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "http://bridge/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`))
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("initialize status = %d, want %d; body=%s", rr.Code, http.StatusOK, rr.Body.String())
	}
	var resp mcptransport.JSONRPCResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode initialize response: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("initialize returned RPC error: %+v", resp.Error)
	}
}

func TestNewHubHandlerRejectsInvalidConfig(t *testing.T) {
	_, err := NewHandler(config.Hub{
		NATSURL:       "nats://example:4222",
		Bus:           newStaticNATSRequester(t, nil),
		SubjectPrefix: "bad prefix",
	})
	if err == nil {
		t.Fatal("NewHandler() error = nil, want invalid config error")
	}
	if !strings.Contains(err.Error(), config.EnvSubjectPrefix) {
		t.Fatalf("NewHandler() error = %q, want %s validation error", err, config.EnvSubjectPrefix)
	}
}

func TestHubRejectsNonPost(t *testing.T) {
	rr := serveHubRPC(t, testHub(newStaticNATSRequester(t, nil)), http.MethodGet, "")
	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("HTTP status = %d, want %d", rr.Code, http.StatusMethodNotAllowed)
	}
}

func TestHubRejectsMalformedJSON(t *testing.T) {
	resp := postHubRPC(t, testHub(newStaticNATSRequester(t, nil)), `{not-json`)
	expectRPCErrorCode(t, resp, mcp.PARSE_ERROR)
}

func TestHubRejectsOversizedRequest(t *testing.T) {
	resp := postHubRPC(t, testHub(newStaticNATSRequester(t, nil)), strings.Repeat("x", bridgemcp.MaxMessageBytes+1))
	expectRPCErrorCode(t, resp, mcp.PARSE_ERROR)
}

func TestHubInitializeAdvertisesStatelessCapabilities(t *testing.T) {
	resp := postHubRPC(t, testHub(newStaticNATSRequester(t, nil)), `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`)
	if resp.Error != nil {
		t.Fatalf("initialize returned RPC error: %+v", resp.Error)
	}
	var result mcp.InitializeResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("decode initialize result: %v", err)
	}
	if result.ServerInfo.Name != "dsx-agentgateway-bridge-hub" {
		t.Fatalf("server name = %q, want bridge hub", result.ServerInfo.Name)
	}
	if result.Capabilities.Tools == nil || result.Capabilities.Prompts == nil || result.Capabilities.Completions == nil || result.Capabilities.Resources != nil {
		t.Fatalf("initialize capabilities = %+v, want tools/prompts/completions without resources", result.Capabilities)
	}
}

func TestHubAcceptsNotifications(t *testing.T) {
	h := testHub(newStaticNATSRequester(t, nil))
	for _, method := range []string{
		string(mcp.MethodNotificationInitialized),
		string(mcp.MethodNotificationToolsListChanged),
		string(mcp.MethodNotificationPromptsListChanged),
		string(mcp.MethodNotificationResourcesListChanged),
		string(mcp.MethodNotificationTasksStatus),
	} {
		t.Run(method, func(t *testing.T) {
			rr := serveHubRPC(t, h, http.MethodPost, fmt.Sprintf(`{"jsonrpc":"2.0","method":%q}`, method))
			if rr.Code != http.StatusAccepted {
				t.Fatalf("HTTP status = %d, want %d", rr.Code, http.StatusAccepted)
			}
			if rr.Body.Len() != 0 {
				t.Fatalf("notification body = %q, want empty", rr.Body.String())
			}
		})
	}
}

func TestHubRejectsStatefulMethods(t *testing.T) {
	h := testHub(newStaticNATSRequester(t, nil))
	for _, method := range []string{
		"resources/subscribe",
		string(mcp.MethodSetLogLevel),
		string(mcp.MethodTasksList),
	} {
		t.Run(method, func(t *testing.T) {
			resp := postHubRPC(t, h, fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"method":%q,"params":{}}`, method))
			expectRPCErrorCode(t, resp, mcp.METHOD_NOT_FOUND)
		})
	}
}

func TestHubToolsListUsesGlobalListQueueAndAddsShardControls(t *testing.T) {
	bus := newStaticNATSRequester(t, map[string]fakeNATSResponse{
		shardbus.ListSubject(testSubjectPrefix): {
			header: nats.Header{shardbus.ShardIDHeader: []string{"cpc-1"}},
			data:   serializedRPCResponse(t, listToolsRPCResponse("remote-tool")),
		},
	})

	resp := postHubRPC(t, testHub(bus), `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`)
	listed := decodeListedTools(t, resp)
	if names := toolNames(listed.Tools); !slices.Equal(names, []string{BridgeListShardsToolName, "remote-tool"}) {
		t.Fatalf("tools/list names = %v, want bridge shard tool and one remote tool", names)
	}
	shardTool := findTool(t, listed.Tools, BridgeListShardsToolName)
	for _, tt := range []struct {
		name string
		got  *bool
		want bool
	}{
		{name: "read_only", got: shardTool.Annotations.ReadOnlyHint, want: true},
		{name: "non_destructive", got: shardTool.Annotations.DestructiveHint, want: false},
		{name: "idempotent", got: shardTool.Annotations.IdempotentHint, want: true},
		{name: "closed_world", got: shardTool.Annotations.OpenWorldHint, want: false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got == nil || *tt.got != tt.want {
				t.Fatalf("annotation = %v, want %t", tt.got, tt.want)
			}
		})
	}
	assertShardSchema(t, findTool(t, listed.Tools, "remote-tool"))
	assertCalls(t, bus.recordedCalls(), []expectedCall{{subject: shardbus.ListSubject(testSubjectPrefix)}})
}

func TestHubToolsListOmitsLocalToolAfterFirstPage(t *testing.T) {
	bus := newStaticNATSRequester(t, map[string]fakeNATSResponse{
		shardbus.ListSubject(testSubjectPrefix): {
			data: serializedRPCResponse(t, listToolsRPCResponse("remote-tool")),
		},
	})

	resp := postHubRPC(t, testHub(bus), `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{"cursor":"next-page"}}`)
	if names := toolNames(decodeListedTools(t, resp).Tools); !slices.Equal(names, []string{"remote-tool"}) {
		t.Fatalf("tools/list names = %v, want only remote tool", names)
	}
}

func TestHubToolsListPassesThroughLeafRPCError(t *testing.T) {
	const leafRejectedListCode = -32011

	leafErr := mcp.NewJSONRPCError(mcp.NewRequestId(int64(1)), leafRejectedListCode, "leaf rejected list", nil)
	bus := newStaticNATSRequester(t, map[string]fakeNATSResponse{
		shardbus.ListSubject(testSubjectPrefix): {data: serializedRPCResponse(t, leafErr)},
	})

	resp := postHubRPC(t, testHub(bus), `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`)
	expectRPCErrorCode(t, resp, leafRejectedListCode)
	assertCalls(t, bus.recordedCalls(), []expectedCall{{subject: shardbus.ListSubject(testSubjectPrefix)}})
}

func TestHubPromptsListUsesGlobalListQueue(t *testing.T) {
	bus := newStaticNATSRequester(t, map[string]fakeNATSResponse{
		shardbus.ListSubject(testSubjectPrefix): {
			header: nats.Header{shardbus.ShardIDHeader: []string{"cpc-1"}},
			data: serializedRPCResponse(t, mcp.NewJSONRPCResultResponse(
				mcp.NewRequestId(int64(1)),
				mcp.ListPromptsResult{Prompts: []mcp.Prompt{{Name: "prompt-a"}}},
			)),
		},
	})

	resp := postHubRPC(t, testHub(bus), `{"jsonrpc":"2.0","id":1,"method":"prompts/list","params":{}}`)
	if resp.Error != nil {
		t.Fatalf("prompts/list returned RPC error: %+v", resp.Error)
	}
	assertCalls(t, bus.recordedCalls(), []expectedCall{{subject: shardbus.ListSubject(testSubjectPrefix)}})
}

func TestHubListMethodsReturnEmptyWhenNoLeafAnswers(t *testing.T) {
	methods := []string{
		string(mcp.MethodToolsList),
		string(mcp.MethodPromptsList),
	}
	errs := []struct {
		name string
		err  error
	}{
		{"no_responders", nats.ErrNoResponders},
		{"context_timeout", context.DeadlineExceeded},
		{"nats_timeout", nats.ErrTimeout},
	}
	for _, method := range methods {
		for _, tc := range errs {
			t.Run(method+"/"+tc.name, func(t *testing.T) {
				bus := newStaticNATSRequester(t, map[string]fakeNATSResponse{
					shardbus.ListSubject(testSubjectPrefix): {err: tc.err},
				})

				resp := postHubRPC(t, testHub(bus), requestBodyForMethod(method))
				assertEmptyListResult(t, method, resp)
				assertCalls(t, bus.recordedCalls(), []expectedCall{{subject: shardbus.ListSubject(testSubjectPrefix)}})
			})
		}
	}
}

func TestHubListDoesNotReturnEmptyAfterCallerDeadline(t *testing.T) {
	bus := newStaticNATSRequester(t, map[string]fakeNATSResponse{
		shardbus.ListSubject(testSubjectPrefix): {err: context.DeadlineExceeded},
	})
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	req := httptest.NewRequest(http.MethodPost, "http://bridge/mcp", strings.NewReader(requestBodyForMethod(string(mcp.MethodToolsList)))).WithContext(ctx)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	testHub(bus).handleMCP(rr, req)

	var resp mcptransport.JSONRPCResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	expectRPCErrorCode(t, resp, bridgemcp.CodeBridgeUnavailable)
}

func TestHubInvocationsFailWhenSelectedShardDoesNotAnswer(t *testing.T) {
	methods := []string{
		string(mcp.MethodToolsCall),
		string(mcp.MethodPromptsGet),
		string(mcp.MethodCompletionComplete),
	}
	errs := []struct {
		name string
		err  error
	}{
		{"no_responders", nats.ErrNoResponders},
		{"context_timeout", context.DeadlineExceeded},
		{"nats_timeout", nats.ErrTimeout},
	}
	for _, method := range methods {
		for _, tc := range errs {
			t.Run(method+"/"+tc.name, func(t *testing.T) {
				bus := newStaticNATSRequester(t, map[string]fakeNATSResponse{
					shardbus.MCPSubject(testSubjectPrefix, "cpc-2"): {err: tc.err},
				})

				resp := postHubRPC(t, testHub(bus), requestBodyForMethod(method))
				expectRPCErrorCode(t, resp, bridgemcp.CodeBridgeUnavailable)
				assertCalls(t, bus.recordedCalls(), []expectedCall{{subject: shardbus.MCPSubject(testSubjectPrefix, "cpc-2")}})
			})
		}
	}
}

func TestHubInvocationsUseShardQueueAndStripShardSelector(t *testing.T) {
	methods := []string{
		string(mcp.MethodToolsCall),
		string(mcp.MethodPromptsGet),
		string(mcp.MethodCompletionComplete),
	}
	for _, method := range methods {
		t.Run(method, func(t *testing.T) {
			bus := newStaticNATSRequester(t, map[string]fakeNATSResponse{
				shardbus.MCPSubject(testSubjectPrefix, "cpc-2"): jsonBridgeResponse(serializedRPCResponse(t, invocationRPCResponseForMethod(method))),
			})

			resp := postHubRPC(t, testHub(bus), requestBodyForMethod(method))
			if resp.Error != nil {
				t.Fatalf("%s returned RPC error: %+v", method, resp.Error)
			}
			calls := bus.recordedCalls()
			assertCalls(t, calls, []expectedCall{{subject: shardbus.MCPSubject(testSubjectPrefix, "cpc-2")}})
			if !calls[0].hasContextDeadline {
				t.Fatal("initial shard request context had no deadline")
			}
			assertForwardedRequestStrippedShardID(t, calls[0].data, method)
		})
	}
}

func TestHubDoesNotRepeatInitialRequestAfterDroppedReply(t *testing.T) {
	subject := shardbus.MCPSubject(testSubjectPrefix, "cpc-2")
	bus := newStaticNATSRequester(t, nil)
	bus.queuedResponses = map[string][]fakeNATSResponse{
		subject: {
			{err: nats.ErrTimeout},
			jsonBridgeResponse(serializedRPCResponse(t, callToolRPCResponse())),
		},
	}

	resp := postHubRPC(t, testHub(bus), requestBodyForMethod(string(mcp.MethodToolsCall)))
	expectRPCErrorCode(t, resp, bridgemcp.CodeBridgeUnavailable)
	assertCalls(t, bus.recordedCalls(), []expectedCall{{subject: subject}})
}

func TestHubResponseStatusValidatesHTTPRange(t *testing.T) {
	for _, test := range []struct {
		value string
		want  int
		valid bool
	}{
		{value: "100", want: 100, valid: true},
		{value: "200", want: 200, valid: true},
		{value: "999", want: 999, valid: true},
		{value: "", valid: false},
		{value: "invalid", valid: false},
		{value: "99", valid: false},
		{value: "1000", valid: false},
	} {
		t.Run(test.value, func(t *testing.T) {
			msg := &nats.Msg{Header: nats.Header{shardbus.HTTPStatusHeader: []string{test.value}}}
			got, err := responseStatus(msg)
			if test.valid {
				if err != nil || got != test.want {
					t.Fatalf("responseStatus() = %d, %v, want %d, nil", got, err, test.want)
				}
				return
			}
			if err == nil {
				t.Fatalf("responseStatus() = %d, nil, want error", got)
			}
		})
	}
}

func TestHubPullsSSEFramesFromStreamSubject(t *testing.T) {
	streamID := "_INBOX.stream"
	bus, subject, streamSubject := newSSERequester(t, streamID,
		streamFrameResponse(shardbus.StreamFramePending, ""),
		streamFrameResponse("", testSSEResult),
		streamFrameResponse(shardbus.StreamFrameEnd, ""),
		fakeNATSResponse{},
	)

	rr := serveHubRPC(t, testHub(bus), http.MethodPost, requestBodyForMethod(string(mcp.MethodToolsCall)))
	if rr.Code != http.StatusOK {
		t.Fatalf("HTTP status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
	if got := rr.Header().Get("Content-Type"); got != "text/event-stream" {
		t.Fatalf("Content-Type = %q, want text/event-stream", got)
	}
	if !strings.Contains(rr.Body.String(), `"result"`) {
		t.Fatalf("SSE body = %q, want result payload", rr.Body.String())
	}
	calls := bus.recordedCalls()
	assertCalls(t, calls, []expectedCall{{subject: subject}, {subject: streamSubject}, {subject: streamSubject}, {subject: streamSubject}, {subject: streamSubject}})
	if got := calls[1].header.Get(shardbus.StreamCursorHeader); got != "0" {
		t.Fatalf("first cursor = %q, want 0", got)
	}
	if got := calls[2].header.Get(shardbus.StreamCursorHeader); got != "0" {
		t.Fatalf("cursor after pending = %q, want 0", got)
	}
	if got := calls[3].header.Get(shardbus.StreamCursorHeader); got != "1" {
		t.Fatalf("cursor after data = %q, want 1", got)
	}
	if got := calls[4].header.Get(shardbus.StreamOperationHeader); got != shardbus.StreamOperationClose {
		t.Fatalf("final stream operation = %q, want close", got)
	}
	for i := 1; i < len(calls); i++ {
		if got := calls[i].header.Get(shardbus.StreamIDHeader); got != streamID {
			t.Fatalf("read %d stream ID = %q, want %q", i, got, streamID)
		}
		if i < len(calls)-1 {
			if got := calls[i].header.Get(shardbus.StreamOperationHeader); got != shardbus.StreamOperationRead {
				t.Fatalf("read %d operation = %q, want read", i, got)
			}
		}
	}
}

func TestWriteAndFlushSetsAndClearsDeadline(t *testing.T) {
	w := newControlledResponseWriter()

	committed, err := writeAndFlush(w, time.Second, http.StatusOK, []byte("frame"))
	if err != nil {
		t.Fatalf("writeAndFlush() error = %v", err)
	}
	if !committed {
		t.Fatal("writeAndFlush() committed = false, want true")
	}

	if got := len(w.deadlines); got != 2 {
		t.Fatalf("write deadlines = %d, want set and clear", got)
	}
	if w.deadlines[0].IsZero() {
		t.Fatal("first write deadline is zero")
	}
	if !w.deadlines[1].IsZero() {
		t.Fatalf("cleared write deadline = %v, want zero", w.deadlines[1])
	}
	if !w.Flushed {
		t.Fatal("response was not flushed")
	}
	if got := w.Body.String(); got != "frame" {
		t.Fatalf("body = %q, want frame", got)
	}
}

func TestRequestTimeoutCancelsRequestContext(t *testing.T) {
	t.Parallel()

	done := make(chan error, 1)
	h := withRequestTimeout(100*time.Millisecond, http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
		done <- r.Context().Err()
	}))

	go h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "http://bridge/mcp", nil))

	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("request context error = %v, want %v", err, context.DeadlineExceeded)
		}
	case <-time.After(time.Second):
		t.Fatal("request timeout did not cancel the request context")
	}
}

func TestRequestLogRecordsCompletionWithoutSensitiveInput(t *testing.T) {
	const secret = "do-not-log-this-request-data"
	var output bytes.Buffer
	h := withRequestLog(
		slog.New(slog.NewJSONHandler(&output, nil)),
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusAccepted)
		}),
	)
	req := httptest.NewRequest(http.MethodPost, "http://bridge/"+secret, strings.NewReader(secret))
	req.Header.Set("Authorization", "Bearer "+secret)
	h.ServeHTTP(httptest.NewRecorder(), req)

	var record map[string]any
	if err := json.Unmarshal(output.Bytes(), &record); err != nil {
		t.Fatalf("decode request log: %v", err)
	}
	if got := record["level"]; got != "INFO" {
		t.Errorf("level = %v, want INFO", got)
	}
	if got := record["msg"]; got != "bridge request completed" {
		t.Errorf("msg = %v, want bridge request completed", got)
	}
	if got := record["method"]; got != http.MethodPost {
		t.Errorf("method = %v, want POST", got)
	}
	if got := record["status"]; got != float64(http.StatusAccepted) {
		t.Errorf("status = %v, want %d", got, http.StatusAccepted)
	}
	if _, ok := record["duration"]; !ok {
		t.Error("duration is missing")
	}
	if strings.Contains(output.String(), secret) {
		t.Fatalf("request log contains sensitive request data: %s", output.String())
	}
}

func TestHubHTTPServerUsesConfiguredWriteTimeout(t *testing.T) {
	want := 17 * time.Second
	srv := newHTTPServer("test", "127.0.0.1:0", want, time.Minute, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

	if srv.WriteTimeout != want {
		t.Fatalf("WriteTimeout = %s, want %s", srv.WriteTimeout, want)
	}
	if srv.WriteTimeout == 0 {
		t.Fatal("WriteTimeout = 0, want ordinary HTTP routes bounded")
	}
}

func TestHubRPCResponseUsesPerWriteDeadline(t *testing.T) {
	w := newControlledResponseWriter()
	req := httptest.NewRequest(http.MethodPost, "http://bridge/mcp", strings.NewReader(requestBodyForMethod(string(mcp.MethodPing))))

	testHub(nil).handleMCP(w, req)

	if got := w.Code; got != http.StatusOK {
		t.Fatalf("response status = %d, want %d", got, http.StatusOK)
	}
	if got := len(w.deadlines); got != 2 || w.deadlines[0].IsZero() || !w.deadlines[1].IsZero() {
		t.Fatalf("generated JSON deadlines = %v, want set then clear", w.deadlines)
	}
	if !w.Flushed {
		t.Fatal("generated JSON response was not flushed")
	}
}

func TestHubDoesNotAppendRPCErrorAfterJSONWriteFailure(t *testing.T) {
	subject := shardbus.MCPSubject(testSubjectPrefix, "cpc-2")
	bus := newStaticNATSRequester(t, map[string]fakeNATSResponse{
		subject: jsonBridgeResponse(serializedRPCResponse(t, callToolRPCResponse())),
	})
	w := newControlledResponseWriter()
	w.writeErr = errors.New("caller stopped reading")
	req := httptest.NewRequest(http.MethodPost, "http://bridge/mcp", strings.NewReader(requestBodyForMethod(string(mcp.MethodToolsCall))))
	req.Header.Set("Content-Type", "application/json")

	testHub(bus).handleMCP(w, req)

	if got := w.writes; got != 1 {
		t.Fatalf("response writes = %d, want one attempted leaf response write", got)
	}
	if got := w.Code; got != http.StatusOK {
		t.Fatalf("response status = %d, want %d", got, http.StatusOK)
	}
	if got := len(w.deadlines); got != 2 || w.deadlines[0].IsZero() || !w.deadlines[1].IsZero() {
		t.Fatalf("JSON response deadlines = %v, want set then clear", w.deadlines)
	}
	assertCalls(t, bus.recordedCalls(), []expectedCall{{subject: subject}})
}

func TestHubRetriesDroppedStreamReplyWithSameCursor(t *testing.T) {
	bus, subject, streamSubject := newSSERequester(t, "stream-id",
		fakeNATSResponse{err: nats.ErrTimeout},
		fakeNATSResponse{err: context.DeadlineExceeded},
		streamFrameResponse("", testSSEResult),
		streamFrameResponse(shardbus.StreamFrameEnd, ""),
		fakeNATSResponse{},
	)

	rr := serveHubRPC(t, testHub(bus), http.MethodPost, requestBodyForMethod(string(mcp.MethodToolsCall)))
	if rr.Code != http.StatusOK {
		t.Fatalf("HTTP status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
	if strings.Count(rr.Body.String(), `"result"`) != 1 {
		t.Fatalf("SSE body = %q, want one recovered frame", rr.Body.String())
	}
	calls := bus.recordedCalls()
	assertCalls(t, calls, []expectedCall{{subject: subject}, {subject: streamSubject}, {subject: streamSubject}, {subject: streamSubject}, {subject: streamSubject}, {subject: streamSubject}})
	for i := 1; i <= 3; i++ {
		if got := calls[i].header.Get(shardbus.StreamCursorHeader); got != "0" {
			t.Fatalf("stream request attempt %d cursor = %q, want 0", i, got)
		}
	}
	if got := calls[5].header.Get(shardbus.StreamOperationHeader); got != shardbus.StreamOperationClose {
		t.Fatalf("final stream operation = %q, want close", got)
	}
}

func TestHubStopsRetryingStreamReadWhenRequestCancels(t *testing.T) {
	t.Parallel()

	streamSubject := shardbus.MCPStreamSubject(testSubjectPrefix, "cpc-2", "leaf-instance")
	bus := newStaticNATSRequester(t, map[string]fakeNATSResponse{
		streamSubject: {err: nats.ErrTimeout},
	})
	ctx, cancel := context.WithCancel(t.Context())
	req := httptest.NewRequest(http.MethodPost, "http://bridge/mcp", nil).WithContext(ctx)
	done := make(chan error, 1)

	go func() {
		_, err := testHub(bus).requestStreamFrame(req, streamSubject, "stream-id", 0)
		done <- err
	}()

	select {
	case <-bus.callRecorded:
		cancel()
	case <-time.After(time.Second):
		t.Fatal("requestStreamFrame() did not issue a stream read")
	}

	select {
	case err := <-done:
		if !errors.Is(err, nats.ErrTimeout) {
			t.Fatalf("requestStreamFrame() error = %v, want %v", err, nats.ErrTimeout)
		}
	case <-time.After(time.Second):
		t.Fatal("requestStreamFrame() did not stop after request cancellation")
	}
}

func TestHubDoesNotRetryTerminalStreamErrors(t *testing.T) {
	for _, test := range []struct {
		name         string
		response     fakeNATSResponse
		want         error
		wantContains string
	}{
		{name: "no responders", response: fakeNATSResponse{err: nats.ErrNoResponders}, want: nats.ErrNoResponders},
		{name: "micro service error", response: fakeNATSResponse{header: nats.Header{
			micro.ErrorCodeHeader: []string{"404"},
			micro.ErrorHeader:     []string{"stream not found"},
		}}, wantContains: "404: stream not found"},
	} {
		t.Run(test.name, func(t *testing.T) {
			streamSubject := shardbus.MCPStreamSubject(testSubjectPrefix, "cpc-2", "leaf-instance")
			bus := newStaticNATSRequester(t, map[string]fakeNATSResponse{streamSubject: test.response})
			req := httptest.NewRequest(http.MethodPost, "http://bridge/mcp", nil)

			_, err := testHub(bus).requestStreamFrame(req, streamSubject, "stream-id", 7)
			if test.want != nil && !errors.Is(err, test.want) {
				t.Fatalf("requestStreamFrame() error = %v, want %v", err, test.want)
			}
			if test.wantContains != "" && (err == nil || !strings.Contains(err.Error(), test.wantContains)) {
				t.Fatalf("requestStreamFrame() error = %v, want %q", err, test.wantContains)
			}
			calls := bus.recordedCalls()
			assertCalls(t, calls, []expectedCall{{subject: streamSubject}})
			if got := calls[0].header.Get(shardbus.StreamCursorHeader); got != "7" {
				t.Fatalf("stream request cursor = %q, want 7", got)
			}
		})
	}
}

func TestHubCloseStreamRetriesWithCanceledCallerContext(t *testing.T) {
	streamSubject := shardbus.MCPStreamSubject(testSubjectPrefix, "cpc-2", "leaf-instance")
	bus := newStaticNATSRequester(t, nil)
	bus.queuedResponses = map[string][]fakeNATSResponse{
		streamSubject: {
			{err: nats.ErrTimeout},
			{err: context.DeadlineExceeded},
			{},
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req := httptest.NewRequest(http.MethodPost, "http://bridge/mcp", nil).WithContext(ctx)

	testHub(bus).closeBridgeStream(req, streamSubject, "stream-id")

	calls := bus.recordedCalls()
	assertCalls(t, calls, []expectedCall{{subject: streamSubject}, {subject: streamSubject}, {subject: streamSubject}})
	for i, call := range calls {
		if !call.hasContextDeadline {
			t.Fatalf("close attempt %d has no deadline", i)
		}
		if call.contextErr != nil {
			t.Fatalf("close attempt %d context error = %v, want live context", i, call.contextErr)
		}
		if got := call.header.Get(shardbus.StreamOperationHeader); got != shardbus.StreamOperationClose {
			t.Fatalf("close attempt %d operation = %q, want close", i, got)
		}
		if got := call.header.Get(shardbus.StreamIDHeader); got != "stream-id" {
			t.Fatalf("close attempt %d stream ID = %q, want stream-id", i, got)
		}
	}
}

func TestHubStreamReadFailureStillClosesStream(t *testing.T) {
	bus, subject, streamSubject := newSSERequester(t, "stream-id",
		fakeNATSResponse{err: errors.New("read failed")},
		fakeNATSResponse{},
	)
	req := httptest.NewRequest(http.MethodPost, "http://bridge/mcp", nil)

	responseStarted, err := testHub(bus).forwardLeafRequest(httptest.NewRecorder(), req, subject, []byte(`{}`))

	if err == nil || !strings.Contains(err.Error(), "read failed") {
		t.Fatalf("forwardLeafRequest() error = %v, want read failure", err)
	}
	if !responseStarted {
		t.Fatal("forwardLeafRequest() responseStarted = false after SSE headers")
	}
	calls := bus.recordedCalls()
	assertCalls(t, calls, []expectedCall{{subject: subject}, {subject: streamSubject}, {subject: streamSubject}})
	if got := calls[2].header.Get(shardbus.StreamOperationHeader); got != shardbus.StreamOperationClose {
		t.Fatalf("cleanup operation = %q, want close", got)
	}
}

func TestHubUnknownStreamFrameFailsAndClosesStream(t *testing.T) {
	bus, subject, streamSubject := newSSERequester(t, "stream-id",
		streamFrameResponse("unknown", ""),
		fakeNATSResponse{},
	)
	req := httptest.NewRequest(http.MethodPost, "http://bridge/mcp", nil)

	responseStarted, err := testHub(bus).forwardLeafRequest(httptest.NewRecorder(), req, subject, []byte(`{}`))

	if err == nil || !strings.Contains(err.Error(), `invalid bridge stream frame "unknown"`) {
		t.Fatalf("forwardLeafRequest() error = %v, want invalid-frame error", err)
	}
	if !responseStarted {
		t.Fatal("forwardLeafRequest() responseStarted = false after SSE headers")
	}
	calls := bus.recordedCalls()
	assertCalls(t, calls, []expectedCall{{subject: subject}, {subject: streamSubject}, {subject: streamSubject}})
	if got := calls[2].header.Get(shardbus.StreamOperationHeader); got != shardbus.StreamOperationClose {
		t.Fatalf("cleanup operation = %q, want close", got)
	}
}

func TestHubCloseStreamStopsOnNoResponders(t *testing.T) {
	streamSubject := shardbus.MCPStreamSubject(testSubjectPrefix, "cpc-2", "missing-instance")
	bus := newStaticNATSRequester(t, map[string]fakeNATSResponse{
		streamSubject: {err: nats.ErrNoResponders},
	})
	req := httptest.NewRequest(http.MethodPost, "http://bridge/mcp", nil)

	testHub(bus).closeBridgeStream(req, streamSubject, "stream-id")

	assertCalls(t, bus.recordedCalls(), []expectedCall{{subject: streamSubject}})
}

func TestHubInvocationsForwardLeafJSONRPCErrors(t *testing.T) {
	subject := shardbus.MCPSubject(testSubjectPrefix, "cpc-2")
	data := serializedRPCResponse(t, mcp.NewJSONRPCError(
		mcp.NewRequestId(int64(1)),
		bridgemcp.CodeLeafForwardFailed,
		"leaf failed before response start",
		nil,
	))
	bus := newStaticNATSRequester(t, map[string]fakeNATSResponse{subject: jsonBridgeResponse(data)})

	resp := postHubRPC(t, testHub(bus), requestBodyForMethod(string(mcp.MethodToolsCall)))
	expectRPCErrorCode(t, resp, bridgemcp.CodeLeafForwardFailed)
}

func TestHubRejectsOversizedBufferedLeafResponse(t *testing.T) {
	subject := shardbus.MCPSubject(testSubjectPrefix, "cpc-2")
	bus := newStaticNATSRequester(t, map[string]fakeNATSResponse{
		subject: jsonBridgeResponse(bytes.Repeat([]byte(" "), bridgemcp.MaxMessageBytes+1)),
	})

	resp := postHubRPC(t, testHub(bus), requestBodyForMethod(string(mcp.MethodToolsCall)))
	expectRPCErrorCode(t, resp, mcp.INTERNAL_ERROR)
}

func TestHubResourceInvocationMethodsAreUnsupported(t *testing.T) {
	tests := []struct {
		name string
		body string
		code int
	}{
		{
			name: "resources_list",
			body: `{"jsonrpc":"2.0","id":1,"method":"resources/list","params":{}}`,
			code: mcp.METHOD_NOT_FOUND,
		},
		{
			name: "resource_templates_list",
			body: `{"jsonrpc":"2.0","id":1,"method":"resources/templates/list","params":{}}`,
			code: mcp.METHOD_NOT_FOUND,
		},
		{
			name: "resources_read",
			body: `{"jsonrpc":"2.0","id":1,"method":"resources/read","params":{"uri":"dsx://fixture/static"}}`,
			code: mcp.METHOD_NOT_FOUND,
		},
		{
			name: "resource_completion",
			body: `{"jsonrpc":"2.0","id":1,"method":"completion/complete","params":{"ref":{"type":"ref/resource","uri":"dsx://fixture/static"},"argument":{"name":"name","value":"s"},"context":{"arguments":{"shard_id":"cpc-1"}}}}`,
			code: mcp.INVALID_PARAMS,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			bus := newStaticNATSRequester(t, nil)

			resp := postHubRPC(t, testHub(bus), tc.body)
			expectRPCErrorCode(t, resp, tc.code)
			assertCalls(t, bus.recordedCalls(), nil)
		})
	}
}

func TestHubToolsCallRequiresToolNameBeforeForwarding(t *testing.T) {
	bus := newStaticNATSRequester(t, map[string]fakeNATSResponse{
		shardbus.MCPSubject(testSubjectPrefix, "cpc-1"): jsonBridgeResponse(serializedRPCResponse(t, callToolRPCResponse())),
	})

	resp := postHubRPC(t, testHub(bus), `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"arguments":{}}}`)
	expectRPCErrorCode(t, resp, mcp.INVALID_PARAMS)
	if calls := bus.recordedCalls(); len(calls) != 0 {
		t.Fatalf("bus calls = %d, want none", len(calls))
	}
}

func TestHubBridgeListShardsReturnsDiscoveredShardIDsOnly(t *testing.T) {
	bus := newManyNATSRequester(t, map[string][]fakeNATSResponse{
		shardbus.DiscoverySubject(testSubjectPrefix): {
			discoveryResponseRaw("cpc-2", []byte("{not-json")),
			discoveryResponseRaw("cpc-1", []byte("")),
			discoveryResponseRaw("cpc-2", []byte("ignored duplicate")),
		},
	}, nil)
	h := testHub(bus)
	if err := h.refreshShardCache(context.Background()); err != nil {
		t.Fatalf("refreshShardCache() error = %v", err)
	}

	resp := postHubRPC(t, h, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"dsx_bridge_list_shards","arguments":{}}}`)
	shards := decodeBridgeShardList(t, resp)
	if !slices.Equal(shards, []string{"cpc-1", "cpc-2"}) {
		t.Fatalf("bridge shard list = %v, want sorted unique shard IDs", shards)
	}
	calls := bus.recordedCalls()
	assertCalls(t, calls, []expectedCall{{subject: shardbus.DiscoverySubject(testSubjectPrefix), many: true}})
	if calls[0].requestManyOpts != 0 {
		t.Fatalf("discovery RequestMany options = %d, want bounded context without stall options", calls[0].requestManyOpts)
	}
	if !calls[0].hasContextDeadline {
		t.Fatalf("discovery RequestMany context had no deadline")
	}
}

func TestHubBridgeListShardsUsesCachedDiscovery(t *testing.T) {
	bus := newManyNATSRequester(t, map[string][]fakeNATSResponse{
		shardbus.DiscoverySubject(testSubjectPrefix): {
			discoveryResponseRaw("cpc-1", nil),
			discoveryResponseRaw("cpc-2", nil),
		},
	}, nil)
	h := testHub(bus)
	if err := h.refreshShardCache(context.Background()); err != nil {
		t.Fatalf("refreshShardCache() error = %v", err)
	}

	first := decodeBridgeShardList(t, postHubRPC(t, h, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"dsx_bridge_list_shards","arguments":{}}}`))
	second := decodeBridgeShardList(t, postHubRPC(t, h, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"dsx_bridge_list_shards","arguments":{}}}`))
	if !slices.Equal(first, []string{"cpc-1", "cpc-2"}) || !slices.Equal(second, first) {
		t.Fatalf("cached shard lists = %v then %v, want stable sorted cache", first, second)
	}
	assertCalls(t, bus.recordedCalls(), []expectedCall{{subject: shardbus.DiscoverySubject(testSubjectPrefix), many: true}})
}

func TestHubShardDiscoveryReportsInvalidShardID(t *testing.T) {
	bus := newManyNATSRequester(t, map[string][]fakeNATSResponse{
		shardbus.DiscoverySubject(testSubjectPrefix): {
			discoveryResponseRaw("bad prefix", nil),
		},
	}, nil)
	h := testHub(bus)

	err := h.refreshShardCache(context.Background())
	if err == nil {
		t.Fatal("refreshShardCache() error = nil, want invalid shard ID error")
	}
	shards, ready, _ := h.shardCache.snapshot()
	if !ready {
		t.Fatal("shard cache was not marked ready after discovery attempt")
	}
	if len(shards) != 0 {
		t.Fatalf("cached shards = %v, want none", shards)
	}
}

func TestHubShardDiscoveryReadyWhenNoLeafAnswers(t *testing.T) {
	errs := []struct {
		name string
		err  error
	}{
		{"no_responders", nats.ErrNoResponders},
		{"context_timeout", context.DeadlineExceeded},
		{"nats_timeout", nats.ErrTimeout},
	}
	for _, tc := range errs {
		t.Run("request_many/"+tc.name, func(t *testing.T) {
			bus := newManyNATSRequester(t, map[string][]fakeNATSResponse{
				shardbus.DiscoverySubject(testSubjectPrefix): nil,
			}, nil)
			bus.manyErrs = map[string]error{shardbus.DiscoverySubject(testSubjectPrefix): tc.err}
			assertEmptyDiscoveryReady(t, testHub(bus), bus)
		})
		t.Run("iterator/"+tc.name, func(t *testing.T) {
			bus := newManyNATSRequester(t, map[string][]fakeNATSResponse{
				shardbus.DiscoverySubject(testSubjectPrefix): {{err: tc.err}},
			}, nil)
			assertEmptyDiscoveryReady(t, testHub(bus), bus)
		})
	}
}

func TestHubShardDiscoveryPropagatesParentCancellation(t *testing.T) {
	bus := newManyNATSRequester(t, map[string][]fakeNATSResponse{
		shardbus.DiscoverySubject(testSubjectPrefix): nil,
	}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := testHub(bus).refreshShardCache(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("refreshShardCache() error = %v, want context cancellation", err)
	}
}

func assertEmptyDiscoveryReady(t *testing.T, h hub, bus *fakeNATSRequester) {
	t.Helper()
	if err := h.refreshShardCache(context.Background()); err != nil {
		t.Fatalf("refreshShardCache() error = %v, want nil", err)
	}
	shards, ready, err := h.shardCache.snapshot()
	if !ready || err != nil || len(shards) != 0 || !h.shardCache.trafficReady() {
		t.Fatalf("empty clean discovery state: ready=%v shards=%v err=%v trafficReady=%v, want ready empty nil true", ready, shards, err, h.shardCache.trafficReady())
	}
	assertCalls(t, bus.recordedCalls(), []expectedCall{{subject: shardbus.DiscoverySubject(testSubjectPrefix), many: true}})
}

func TestHubTrafficReadinessRequiresCompletedDiscoveryWithoutErrors(t *testing.T) {
	cache := &shardDiscoveryCache{}
	if cache.trafficReady() {
		t.Fatal("new shard cache is traffic-ready before first discovery")
	}

	cache.store(nil, nil)
	if !cache.trafficReady() {
		t.Fatal("clean discovery with zero shards is not traffic-ready")
	}

	cache.store(nil, errors.New("discovery failed"))
	if cache.trafficReady() {
		t.Fatal("failed empty shard discovery is traffic-ready")
	}

	cache.store([]string{"cpc-1"}, errors.New("partial discovery warning"))
	if cache.trafficReady() {
		t.Fatal("discovery with shards and an error is traffic-ready")
	}

	cache.store([]string{"cpc-1"}, nil)
	if !cache.trafficReady() {
		t.Fatal("clean discovery with a shard is not traffic-ready")
	}
}

func TestHubLeafStartupNotificationTriggersShardCacheRefresh(t *testing.T) {
	bus := newManyNATSRequester(t, map[string][]fakeNATSResponse{
		shardbus.DiscoverySubject(testSubjectPrefix): {
			discoveryResponseRaw("cpc-1", nil),
		},
	}, nil)
	h := testHub(bus)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	refresh := make(chan struct{}, 1)
	done := make(chan struct{})

	go func() {
		h.runShardDiscoveryRefresh(ctx, refresh)
		close(done)
	}()
	handleLeafStartupNotification(&nats.Msg{
		Header: nats.Header{shardbus.ShardIDHeader: []string{"cpc-1"}},
	}, refresh)

	waitCtx, cancelWait := context.WithTimeout(t.Context(), time.Second)
	defer cancelWait()
	select {
	case <-bus.callRecorded:
	case <-waitCtx.Done():
		t.Fatal("startup notification did not request a shard cache refresh")
	}
	cancel()
	select {
	case <-done:
	case <-waitCtx.Done():
		t.Fatal("shard cache refresh loop did not stop")
	}
	shards, ready, err := h.shardCache.snapshot()
	if !ready || err != nil || !slices.Equal(shards, []string{"cpc-1"}) {
		t.Fatalf("startup refresh state: ready=%v shards=%v err=%v", ready, shards, err)
	}
}

func TestHubLeafStartupNotificationIgnoresInvalidShardID(t *testing.T) {
	refresh := make(chan struct{}, 1)
	handleLeafStartupNotification(&nats.Msg{
		Header: nats.Header{shardbus.ShardIDHeader: []string{"bad prefix"}},
	}, refresh)
	select {
	case <-refresh:
		t.Fatal("invalid startup shard ID triggered refresh")
	default:
	}
}

func TestHubLeafNATSRequestsPreserveCallerHeadersAndStripSessionID(t *testing.T) {
	bus := newStaticNATSRequester(t, map[string]fakeNATSResponse{
		shardbus.MCPSubject(testSubjectPrefix, "cpc-1"): jsonBridgeResponse(serializedRPCResponse(t, callToolRPCResponse())),
	})
	headers := http.Header{
		"Mcp-Session-Id": []string{"session"},
		"X-Keep":         []string{"value"},
	}

	postHubRPCWithHeaders(t, testHub(bus), `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"remote-tool","arguments":{"shard_id":"cpc-1","rack":"r1"}}}`, headers)

	calls := bus.recordedCalls()
	assertCalls(t, calls, []expectedCall{{subject: shardbus.MCPSubject(testSubjectPrefix, "cpc-1")}})
	gotHeaders := http.Header(calls[0].header)
	if gotHeaders.Get("Authorization") != "Bearer caller" {
		t.Fatalf("forwarded Authorization = %q, want caller header", gotHeaders.Get("Authorization"))
	}
	if gotHeaders.Get("X-Keep") != "value" {
		t.Fatalf("forwarded X-Keep = %q, want value", gotHeaders.Get("X-Keep"))
	}
	if gotHeaders.Get("Mcp-Session-Id") != "" {
		t.Fatalf("forwarded Mcp-Session-Id = %q, want stripped", gotHeaders.Get("Mcp-Session-Id"))
	}
}

func TestHubQueueRequestFailureReturnsStructuredError(t *testing.T) {
	bus := newStaticNATSRequester(t, map[string]fakeNATSResponse{
		shardbus.ListSubject(testSubjectPrefix): {err: errors.New("no responders")},
	})

	resp := postHubRPC(t, testHub(bus), `{"jsonrpc":"2.0","id":1,"method":"prompts/list","params":{}}`)
	expectRPCErrorCode(t, resp, bridgemcp.CodeBridgeUnavailable)
}

func TestHubMalformedLeafResponseReturnsStructuredError(t *testing.T) {
	bus := newStaticNATSRequester(t, map[string]fakeNATSResponse{
		shardbus.ListSubject(testSubjectPrefix): {data: []byte("{bad-json")},
	})

	resp := postHubRPC(t, testHub(bus), `{"jsonrpc":"2.0","id":1,"method":"prompts/list","params":{}}`)
	expectRPCErrorCode(t, resp, mcp.INTERNAL_ERROR)
}

type fakeNATSResponse struct {
	header nats.Header
	data   []byte
	reply  string
	err    error
}

type fakeNATSCall struct {
	subject            string
	header             nats.Header
	data               []byte
	requestMany        bool
	requestManyOpts    int
	hasContextDeadline bool
	contextErr         error
}

type controlledResponseWriter struct {
	*httptest.ResponseRecorder
	deadlines []time.Time
	writes    int
	writeErr  error
}

func newControlledResponseWriter() *controlledResponseWriter {
	return &controlledResponseWriter{ResponseRecorder: httptest.NewRecorder()}
}

func (w *controlledResponseWriter) Write(body []byte) (int, error) {
	w.writes++
	if w.writeErr != nil {
		return 0, w.writeErr
	}
	return w.ResponseRecorder.Write(body)
}

func (w *controlledResponseWriter) SetWriteDeadline(deadline time.Time) error {
	w.deadlines = append(w.deadlines, deadline)
	return nil
}

type fakeNATSRequester struct {
	t *testing.T

	mu              sync.Mutex
	calls           []fakeNATSCall
	many            map[string][]fakeNATSResponse
	manyErrs        map[string]error
	responses       map[string]fakeNATSResponse
	queuedResponses map[string][]fakeNATSResponse
	callRecorded    chan struct{}
}

func newStaticNATSRequester(t *testing.T, responses map[string]fakeNATSResponse) *fakeNATSRequester {
	t.Helper()
	return newManyNATSRequester(t, nil, responses)
}

func newSSERequester(t *testing.T, streamID string, streamResponses ...fakeNATSResponse) (*fakeNATSRequester, string, string) {
	t.Helper()
	subject := shardbus.MCPSubject(testSubjectPrefix, "cpc-2")
	streamSubject := shardbus.MCPStreamSubject(testSubjectPrefix, "cpc-2", "leaf-instance")
	bus := newStaticNATSRequester(t, nil)
	bus.queuedResponses = map[string][]fakeNATSResponse{
		subject:       {sseStartResponse(streamSubject, streamID)},
		streamSubject: streamResponses,
	}
	return bus, subject, streamSubject
}

func sseStartResponse(streamSubject, streamID string) fakeNATSResponse {
	return fakeNATSResponse{
		reply: streamSubject,
		header: nats.Header{
			shardbus.HTTPStatusHeader:      []string{"200"},
			shardbus.HTTPContentTypeHeader: []string{"text/event-stream"},
			shardbus.StreamIDHeader:        []string{streamID},
		},
	}
}

func jsonBridgeResponse(data []byte) fakeNATSResponse {
	return fakeNATSResponse{
		header: nats.Header{
			shardbus.HTTPStatusHeader:      []string{"200"},
			shardbus.HTTPContentTypeHeader: []string{"application/json"},
		},
		data: data,
	}
}

func streamFrameResponse(frame, data string) fakeNATSResponse {
	header := nats.Header{}
	if frame != "" {
		header.Set(shardbus.StreamFrameHeader, frame)
	}
	return fakeNATSResponse{header: header, data: []byte(data)}
}

func newManyNATSRequester(t *testing.T, many map[string][]fakeNATSResponse, responses map[string]fakeNATSResponse) *fakeNATSRequester {
	t.Helper()
	return &fakeNATSRequester{
		t:            t,
		many:         many,
		responses:    responses,
		callRecorded: make(chan struct{}, 1),
	}
}

func (f *fakeNATSRequester) RequestMsgWithContext(ctx context.Context, msg *nats.Msg) (*nats.Msg, error) {
	f.record(ctx, msg, false, 0)
	resp, ok := f.nextResponse(msg.Subject)
	if !ok {
		return nil, fmt.Errorf("unexpected single-request subject: %s", msg.Subject)
	}
	if resp.err != nil {
		return nil, resp.err
	}
	return &nats.Msg{
		Header: resp.header,
		Data:   resp.data,
		Reply:  resp.reply,
	}, nil
}

func (f *fakeNATSRequester) nextResponse(subject string) (fakeNATSResponse, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if responses, ok := f.queuedResponses[subject]; ok {
		if len(responses) == 0 {
			return fakeNATSResponse{}, false
		}
		resp := responses[0]
		f.queuedResponses[subject] = responses[1:]
		return resp, true
	}
	resp, ok := f.responses[subject]
	return resp, ok
}

func (f *fakeNATSRequester) RequestManyMsg(ctx context.Context, msg *nats.Msg, opts ...natsext.RequestManyOpt) (iter.Seq2[*nats.Msg, error], error) {
	f.record(ctx, msg, true, len(opts))
	if err := f.manyErrs[msg.Subject]; err != nil {
		return nil, err
	}
	responses, ok := f.many[msg.Subject]
	if !ok {
		return nil, fmt.Errorf("unexpected request-many subject: %s", msg.Subject)
	}
	return func(yield func(*nats.Msg, error) bool) {
		for _, resp := range responses {
			if resp.err != nil {
				if !yield(nil, resp.err) {
					return
				}
				continue
			}
			if !yield(&nats.Msg{
				Header: resp.header,
				Data:   resp.data,
			}, nil) {
				return
			}
		}
	}, nil
}

func (f *fakeNATSRequester) record(ctx context.Context, msg *nats.Msg, requestMany bool, opts int) {
	f.mu.Lock()
	_, hasDeadline := ctx.Deadline()
	f.calls = append(f.calls, fakeNATSCall{
		subject:            msg.Subject,
		header:             msg.Header,
		data:               msg.Data,
		requestMany:        requestMany,
		requestManyOpts:    opts,
		hasContextDeadline: hasDeadline,
		contextErr:         ctx.Err(),
	})
	f.mu.Unlock()
	select {
	case f.callRecorded <- struct{}{}:
	default:
	}
}

func (f *fakeNATSRequester) recordedCalls() []fakeNATSCall {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.calls
}

type expectedCall struct {
	subject string
	many    bool
}

func assertCalls(t *testing.T, got []fakeNATSCall, want []expectedCall) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("bus calls = %d, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i].subject != want[i].subject || got[i].requestMany != want[i].many {
			t.Fatalf("bus call %d = subject %q many=%v, want subject %q many=%v", i, got[i].subject, got[i].requestMany, want[i].subject, want[i].many)
		}
	}
}

func testHub(bus shardbus.Requester) hub {
	return hub{
		bus:              bus,
		timeout:          time.Second,
		writeTimeout:     time.Second,
		discoveryTimeout: time.Second,
		discoveryRefresh: time.Hour,
		subjectPrefix:    testSubjectPrefix,
		shardCache:       &shardDiscoveryCache{},
	}
}

func postHubRPC(t *testing.T, h hub, body string) mcptransport.JSONRPCResponse {
	t.Helper()

	rr := serveHubRPCWithHeaders(t, h, http.MethodPost, body, nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("HTTP status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
	var resp mcptransport.JSONRPCResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode RPC response %s: %v", rr.Body.String(), err)
	}
	return resp
}

func postHubRPCWithHeaders(t *testing.T, h hub, body string, headers http.Header) mcptransport.JSONRPCResponse {
	t.Helper()

	rr := serveHubRPCWithHeaders(t, h, http.MethodPost, body, headers)
	if rr.Code != http.StatusOK {
		t.Fatalf("HTTP status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
	var resp mcptransport.JSONRPCResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode RPC response %s: %v", rr.Body.String(), err)
	}
	return resp
}

func serveHubRPC(t *testing.T, h hub, method, body string) *httptest.ResponseRecorder {
	t.Helper()

	return serveHubRPCWithHeaders(t, h, method, body, nil)
}

func serveHubRPCWithHeaders(t *testing.T, h hub, method, body string, headers http.Header) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(method, "http://bridge/mcp", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer caller")
	for k, values := range headers {
		req.Header.Del(k)
		for _, value := range values {
			req.Header.Add(k, value)
		}
	}
	rr := httptest.NewRecorder()
	h.handleMCP(rr, req)
	return rr
}

func requestBodyForMethod(method string) string {
	switch method {
	case string(mcp.MethodToolsCall):
		return `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"remote-tool","arguments":{"shard_id":"cpc-2","rack":"r1"}}}`
	case string(mcp.MethodPromptsGet):
		return `{"jsonrpc":"2.0","id":1,"method":"prompts/get","params":{"name":"prompt-a","arguments":{"shard_id":"cpc-2","rack":"r1"}}}`
	case string(mcp.MethodCompletionComplete):
		return `{"jsonrpc":"2.0","id":1,"method":"completion/complete","params":{"ref":{"type":"ref/prompt","name":"prompt-a"},"argument":{"name":"rack","value":"r"},"context":{"arguments":{"shard_id":"cpc-2","tenant":"tenant-a"}}}}`
	default:
		return fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"method":%q,"params":{}}`, method)
	}
}

func serializedRPCResponse(t *testing.T, rpc any) []byte {
	t.Helper()

	data, err := json.Marshal(rpc)
	if err != nil {
		t.Fatalf("encode RPC response: %v", err)
	}
	return data
}

func discoveryResponseRaw(shardID string, data []byte) fakeNATSResponse {
	return fakeNATSResponse{
		header: nats.Header{shardbus.ShardIDHeader: []string{shardID}},
		data:   data,
	}
}

func listToolsRPCResponse(names ...string) mcp.JSONRPCResponse {
	tools := make([]mcp.Tool, 0, len(names))
	for _, name := range names {
		tools = append(tools, mcp.Tool{Name: name, RawInputSchema: json.RawMessage(`{"type":"object"}`)})
	}
	return mcp.NewJSONRPCResultResponse(
		mcp.NewRequestId(int64(1)),
		mcp.ListToolsResult{Tools: tools},
	)
}

func callToolRPCResponse() mcp.JSONRPCResponse {
	return mcp.NewJSONRPCResultResponse(
		mcp.NewRequestId(int64(1)),
		map[string]any{"content": []any{map[string]any{"type": "text", "text": "ok"}}},
	)
}

func invocationRPCResponseForMethod(method string) mcp.JSONRPCResponse {
	switch method {
	case string(mcp.MethodToolsCall):
		return callToolRPCResponse()
	case string(mcp.MethodPromptsGet):
		return mcp.NewJSONRPCResultResponse(mcp.NewRequestId(int64(1)), mcp.GetPromptResult{})
	case string(mcp.MethodCompletionComplete):
		return mcp.NewJSONRPCResultResponse(mcp.NewRequestId(int64(1)), mcp.CompleteResult{})
	default:
		return mcp.NewJSONRPCResultResponse(mcp.NewRequestId(int64(1)), map[string]any{})
	}
}

func decodeListedTools(t *testing.T, resp mcptransport.JSONRPCResponse) mcp.ListToolsResult {
	t.Helper()
	if resp.Error != nil {
		t.Fatalf("RPC error: %+v", resp.Error)
	}
	var listed mcp.ListToolsResult
	if err := json.Unmarshal(resp.Result, &listed); err != nil {
		t.Fatalf("decode tools/list result: %v", err)
	}
	return listed
}

func assertEmptyListResult(t *testing.T, method string, resp mcptransport.JSONRPCResponse) {
	t.Helper()
	if resp.Error != nil {
		t.Fatalf("%s returned RPC error: %+v", method, resp.Error)
	}
	switch method {
	case string(mcp.MethodToolsList):
		listed := decodeListedTools(t, resp)
		if names := toolNames(listed.Tools); !slices.Equal(names, []string{BridgeListShardsToolName}) {
			t.Fatalf("tools/list names = %v, want only bridge shard tool", names)
		}
	case string(mcp.MethodPromptsList):
		var listed mcp.ListPromptsResult
		if err := json.Unmarshal(resp.Result, &listed); err != nil {
			t.Fatalf("decode prompts/list result: %v", err)
		}
		if len(listed.Prompts) != 0 {
			t.Fatalf("prompts/list = %v, want empty", listed.Prompts)
		}
	default:
		t.Fatalf("unsupported list method %q", method)
	}
}

func decodeBridgeShardList(t *testing.T, resp mcptransport.JSONRPCResponse) []string {
	t.Helper()
	if resp.Error != nil {
		t.Fatalf("RPC error: %+v", resp.Error)
	}
	var result struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		StructuredContent listShardsResult `json:"structuredContent"`
	}
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("decode shard list tool result: %v", err)
	}
	if len(result.Content) != 1 || result.Content[0].Type != "text" {
		t.Fatalf("shard list content = %+v, want one text result", result.Content)
	}
	var payload listShardsResult
	if err := json.Unmarshal([]byte(result.Content[0].Text), &payload); err != nil {
		t.Fatalf("decode shard list text %q: %v", result.Content[0].Text, err)
	}
	if !slices.Equal(result.StructuredContent.Shards, payload.Shards) {
		t.Fatalf("shard list structured content = %v, want %v", result.StructuredContent.Shards, payload.Shards)
	}
	return payload.Shards
}

func decodeForwardedRequest(t *testing.T, data []byte) mcptransport.JSONRPCRequest {
	t.Helper()
	req, err := bridgemcp.DecodeRequest(data)
	if err != nil {
		t.Fatalf("decode forwarded RPC request: %v", err)
	}
	return req
}

func assertForwardedRequestStrippedShardID(t *testing.T, data []byte, method string) {
	t.Helper()
	req := decodeForwardedRequest(t, data)
	if req.Method != method {
		t.Fatalf("forwarded method = %q, want %q", req.Method, method)
	}
	switch method {
	case string(mcp.MethodToolsCall):
		params, err := bridgemcp.ParseCallParams(req.Params)
		if err != nil {
			t.Fatalf("decode forwarded tool params: %v", err)
		}
		args, err := callArgumentsMap(params.Arguments)
		if err != nil {
			t.Fatalf("decode forwarded tool args: %v", err)
		}
		if _, ok := args[ShardIDArgument]; ok {
			t.Fatalf("forwarded tool args still include %s: %+v", ShardIDArgument, args)
		}
	case string(mcp.MethodPromptsGet):
		params, err := parsePromptGetParams(req.Params)
		if err != nil {
			t.Fatalf("decode forwarded prompt params: %v", err)
		}
		if _, ok := params.Arguments[ShardIDArgument]; ok {
			t.Fatalf("forwarded prompt args still include %s: %+v", ShardIDArgument, params.Arguments)
		}
	case string(mcp.MethodCompletionComplete):
		params, err := parseCompletionParams(req.Params)
		if err != nil {
			t.Fatalf("decode forwarded completion params: %v", err)
		}
		if _, ok := params.Context.Arguments[ShardIDArgument]; ok {
			t.Fatalf("forwarded completion context still includes %s: %+v", ShardIDArgument, params.Context.Arguments)
		}
	}
}

func expectRPCErrorCode(t *testing.T, resp mcptransport.JSONRPCResponse, want int) {
	t.Helper()
	if resp.Error == nil {
		t.Fatalf("RPC response had no error: %+v", resp)
	}
	if resp.Error.Code != want {
		t.Fatalf("RPC error code = %d, want %d; message=%q", resp.Error.Code, want, resp.Error.Message)
	}
}

func toolNames(tools []mcp.Tool) []string {
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		names = append(names, tool.Name)
	}
	return names
}

func findTool(t *testing.T, tools []mcp.Tool, name string) mcp.Tool {
	t.Helper()
	for _, tool := range tools {
		if tool.Name == name {
			return tool
		}
	}
	t.Fatalf("tool %q not found in %v", name, toolNames(tools))
	return mcp.Tool{}
}

func assertShardSchema(t *testing.T, tool mcp.Tool) {
	t.Helper()
	if _, ok := tool.InputSchema.Properties[ShardIDArgument]; !ok {
		t.Fatalf("%s schema missing %s: %+v", tool.Name, ShardIDArgument, tool.InputSchema)
	}
	if !slices.Contains(tool.InputSchema.Required, ShardIDArgument) {
		t.Fatalf("%s required fields = %v, want %s", tool.Name, tool.InputSchema.Required, ShardIDArgument)
	}
}

func subject(suffix string) string {
	return testSubjectPrefix + "." + suffix
}
