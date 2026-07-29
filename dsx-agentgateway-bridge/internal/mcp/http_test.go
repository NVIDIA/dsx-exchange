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

package mcp

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	mcptransport "github.com/mark3labs/mcp-go/client/transport"
	mcplib "github.com/mark3labs/mcp-go/mcp"
)

func TestLeafForwardUsesOneStatelessLocalMCPPost(t *testing.T) {
	t.Parallel()

	record := forwardStatelessMCP(t, http.Header{})
	if got, want := len(record.rpcMethods), 1; got != want {
		t.Fatalf("POST count = %d, want %d (%v)", got, want, record.rpcMethods)
	}
	if got, want := record.rpcMethods[0], "tools/list"; got != want {
		t.Fatalf("RPC method = %q, want %q", got, want)
	}
}

func TestLeafForwardRemovesIncomingSessionID(t *testing.T) {
	t.Parallel()

	record := forwardStatelessMCP(t, http.Header{"Mcp-Session-Id": []string{"hub-session"}})
	if got := record.headers.Get("Mcp-Session-Id"); got != "" {
		t.Fatalf("stateless tools/list session = %q, want empty", got)
	}
}

func TestLeafForwardRemovesBridgeControlHeaders(t *testing.T) {
	t.Parallel()

	record := forwardStatelessMCP(t, http.Header{
		"DAG-Bridge-Stream": []string{"1"},
		"DAG-Bridge-Test":   []string{"control"},
	})
	for name := range record.headers {
		if strings.HasPrefix(strings.ToLower(name), "dag-bridge-") {
			t.Fatalf("stateless tools/list retained bridge header %q", name)
		}
	}
}

func TestLeafForwardPreservesAuthorizationHeader(t *testing.T) {
	t.Parallel()

	record := forwardStatelessMCP(t, http.Header{"Authorization": []string{"Bearer caller"}})
	if got := record.headers.Get("Authorization"); got != "Bearer caller" {
		t.Fatalf("tools/list Authorization = %q, want caller bearer", got)
	}
}

func TestLeafForwardPreservesCorrelationHeaders(t *testing.T) {
	t.Parallel()

	record := forwardStatelessMCP(t, http.Header{"X-Request-Id": []string{"request-123"}})
	if got := record.headers.Get("X-Request-Id"); got != "request-123" {
		t.Fatalf("tools/list X-Request-Id = %q, want request-123", got)
	}
}

func TestLeafForwardPreservesLocalJSONRPCError(t *testing.T) {
	t.Parallel()

	local := newTestHTTPServer(t, func(w http.ResponseWriter, r *http.Request) error {
		var req mcptransport.JSONRPCRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			return fmt.Errorf("decode request: %w", err)
		}
		if req.Method != "tools/call" {
			return fmt.Errorf("unexpected method %q", req.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(mcplib.NewJSONRPCError(
			req.ID,
			mcplib.INVALID_PARAMS,
			"invalid leaf input",
			map[string]any{"field": "name"},
		)); err != nil {
			return fmt.Errorf("write tool error response: %w", err)
		}
		return nil
	})

	body := []byte(`{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"echo","arguments":{}}}`)
	respData, err := ForwardStatelessRequest(t.Context(), body, http.Header{}, local.URL+"/mcp")
	if err != nil {
		t.Fatalf("ForwardStatelessRequest: %v", err)
	}
	var resp mcptransport.JSONRPCResponse
	if err := json.Unmarshal(respData, &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Error == nil {
		t.Fatalf("expected JSON-RPC error, got %s", respData)
	}
	if resp.Error.Code != -32602 || resp.Error.Message != "invalid leaf input" {
		t.Fatalf("error = code %d message %q, want local gateway error", resp.Error.Code, resp.Error.Message)
	}
	data, ok := resp.Error.Data.(map[string]any)
	if !ok || data["field"] != "name" {
		t.Fatalf("error data = %#v, want field=name", resp.Error.Data)
	}
}

func TestLeafForwardAcceptsStatelessSSEResponse(t *testing.T) {
	t.Parallel()

	local := newTestHTTPServer(t, func(w http.ResponseWriter, r *http.Request) error {
		var req mcptransport.JSONRPCRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			return fmt.Errorf("decode request: %w", err)
		}
		if req.Method != "tools/list" {
			return fmt.Errorf("unexpected method %q", req.Method)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, err := w.Write([]byte(`event: message
data: {"jsonrpc":"2.0","id":1,"result":{"tools":[{"name":"echo","inputSchema":{"type":"object"}}]}}

`))
		if err != nil {
			return fmt.Errorf("write SSE response: %w", err)
		}
		return nil
	})

	body := []byte(`{"jsonrpc":"2.0","id":7,"method":"tools/list","params":{}}`)
	respData, err := ForwardStatelessRequest(t.Context(), body, http.Header{}, local.URL+"/mcp")
	if err != nil {
		t.Fatalf("ForwardStatelessRequest: %v", err)
	}
	var resp mcptransport.JSONRPCResponse
	if err := json.Unmarshal(respData, &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.ID.Value() != int64(7) {
		t.Fatalf("forwarded response id = %s, want caller id 7", resp.ID)
	}
	if len(resp.Result) == 0 {
		t.Fatalf("expected result response, got %s", respData)
	}
}

func TestOpenStatelessResponseStreamsBeforeEOF(t *testing.T) {
	t.Parallel()

	sendSecond := make(chan struct{})
	sendFinal := make(chan struct{})
	local := newTestHTTPServer(t, func(w http.ResponseWriter, r *http.Request) error {
		var req mcptransport.JSONRPCRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			return fmt.Errorf("decode request: %w", err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, ok := w.(http.Flusher)
		if !ok {
			return fmt.Errorf("httptest response writer does not flush")
		}
		if _, err := w.Write([]byte("event: message\ndata: {\"jsonrpc\":\"2.0\",\"method\":\"notifications/progress\"}\n\n")); err != nil {
			return fmt.Errorf("write first SSE response: %w", err)
		}
		flusher.Flush()
		select {
		case <-sendSecond:
		case <-r.Context().Done():
			return nil
		}
		if _, err := w.Write([]byte("event: message\ndata: {\"jsonrpc\":\"2.0\",\"method\":\"notifications/progress\",\"params\":{\"step\":2}}\n\n")); err != nil {
			return fmt.Errorf("write second SSE response: %w", err)
		}
		flusher.Flush()
		select {
		case <-sendFinal:
		case <-r.Context().Done():
			return nil
		}
		if _, err := w.Write([]byte("event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":7,\"result\":{}}\n\n")); err != nil {
			return fmt.Errorf("write final SSE response: %w", err)
		}
		return nil
	})

	body := []byte(`{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"streaming","arguments":{}}}`)
	resp, err := OpenStatelessResponse(
		t.Context(),
		body,
		http.Header{},
		local.URL+"/mcp",
	)
	if err != nil {
		t.Fatalf("OpenStatelessResponse: %v", err)
	}
	defer closeTestBody(t, resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("stream status = %d, want 200", resp.StatusCode)
	}
	if got := resp.Header.Get("Content-Type"); got != "text/event-stream" {
		t.Fatalf("stream Content-Type = %q, want text/event-stream", got)
	}

	first := readResponseChunk(t, resp.Body)
	if !bytes.Contains(first, []byte("notifications/progress")) {
		t.Fatalf("first stream chunk = %q, want progress notification", first)
	}

	close(sendSecond)
	close(sendFinal)
	rest, err := ReadLimitedResponseBody(resp.Body)
	if err != nil {
		t.Fatalf("read forwarded stream: %v", err)
	}
	if !bytes.Contains(rest, []byte(`"step":2`)) || !bytes.Contains(rest, []byte(`"result"`)) {
		t.Fatalf("remaining stream chunk = %q, want second progress event and final result", rest)
	}
}

func TestOpenStatelessResponseCancelsSSEWithContext(t *testing.T) {
	t.Parallel()

	canceled := make(chan struct{})
	local := newTestHTTPServer(t, func(w http.ResponseWriter, r *http.Request) error {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, ok := w.(http.Flusher)
		if !ok {
			return fmt.Errorf("httptest response writer does not flush")
		}
		if _, err := w.Write([]byte("event: message\ndata: {\"jsonrpc\":\"2.0\",\"method\":\"notifications/progress\"}\n\n")); err != nil {
			return fmt.Errorf("write first SSE response: %w", err)
		}
		flusher.Flush()
		<-r.Context().Done()
		close(canceled)
		return nil
	})

	body := []byte(`{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"streaming","arguments":{}}}`)
	ctx, cancel := context.WithCancel(t.Context())
	resp, err := OpenStatelessResponse(
		ctx,
		body,
		http.Header{},
		local.URL+"/mcp",
	)
	if err != nil {
		t.Fatalf("OpenStatelessResponse: %v", err)
	}
	defer closeTestBody(t, resp.Body)
	first := readResponseChunk(t, resp.Body)
	if !bytes.Contains(first, []byte("notifications/progress")) {
		t.Fatalf("first stream chunk = %q, want progress notification", first)
	}
	cancel()
	waitSignal(t, canceled, "SSE request cancellation")
}

func TestOpenStatelessResponsePreservesStatusContentTypeAndBody(t *testing.T) {
	t.Parallel()

	local := newTestHTTPServer(t, func(w http.ResponseWriter, _ *http.Request) error {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		if _, err := w.Write([]byte(`{"jsonrpc":"2.0","id":7,"error":{"code":-32003,"message":"leaf down"}}`)); err != nil {
			return fmt.Errorf("write error response: %w", err)
		}
		return nil
	})

	body := []byte(`{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"echo","arguments":{}}}`)
	resp, err := OpenStatelessResponse(
		t.Context(),
		body,
		http.Header{},
		local.URL+"/mcp",
	)
	if err != nil {
		t.Fatalf("OpenStatelessResponse: %v", err)
	}
	defer closeTestBody(t, resp.Body)
	got, err := ReadLimitedResponseBody(resp.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("response status = %d, want 502", resp.StatusCode)
	}
	if contentType := resp.Header.Get("Content-Type"); contentType != "application/json" {
		t.Fatalf("response Content-Type = %q, want application/json", contentType)
	}
	if !bytes.Contains(got, []byte(`"leaf down"`)) {
		t.Fatalf("response body = %s, want leaf error body", string(got))
	}
}

func TestOpenStatelessResponseDecodesCompressedBodies(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		contentType string
		body        []byte
	}{
		{
			name:        "JSON",
			contentType: "application/json",
			body:        []byte(`{"jsonrpc":"2.0","id":7,"result":{}}`),
		},
		{
			name:        "SSE",
			contentType: "text/event-stream",
			body:        []byte("event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":7,\"result\":{}}\n\n"),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			local := newTestHTTPServer(t, func(w http.ResponseWriter, _ *http.Request) error {
				w.Header().Set("Content-Type", test.contentType)
				w.Header().Set("Content-Encoding", "gzip")
				gzipWriter := gzip.NewWriter(w)
				if _, err := gzipWriter.Write(test.body); err != nil {
					return fmt.Errorf("write compressed response: %w", err)
				}
				if err := gzipWriter.Close(); err != nil {
					return fmt.Errorf("close compressed response: %w", err)
				}
				return nil
			})

			requestBody := []byte(`{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"echo","arguments":{}}}`)
			resp, err := OpenStatelessResponse(
				t.Context(),
				requestBody,
				http.Header{"Accept-Encoding": []string{"gzip"}},
				local.URL+"/mcp",
			)
			if err != nil {
				t.Fatalf("OpenStatelessResponse: %v", err)
			}
			defer closeTestBody(t, resp.Body)

			got, err := ReadLimitedResponseBody(resp.Body)
			if err != nil {
				t.Fatalf("read response: %v", err)
			}
			if contentType := resp.Header.Get("Content-Type"); contentType != test.contentType {
				t.Fatalf("Content-Type = %q, want %q", contentType, test.contentType)
			}
			if encoding := resp.Header.Get("Content-Encoding"); encoding != "" {
				t.Fatalf("Content-Encoding = %q, want empty after transparent decoding", encoding)
			}
			if bytes.HasPrefix(got, []byte{0x1f, 0x8b}) {
				t.Fatalf("response body remains gzip encoded: %x", got[:2])
			}
			if !bytes.Equal(got, test.body) {
				t.Fatalf("response body = %q, want %q", got, test.body)
			}
		})
	}
}

func TestOpenStatelessResponseCancelsJSONBodyWithContext(t *testing.T) {
	t.Parallel()

	responseReady := make(chan struct{})
	canceled := make(chan struct{})
	stopHandler := make(chan struct{})
	stopRequestHandler := sync.OnceFunc(func() { close(stopHandler) })
	local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		select {
		case <-r.Context().Done():
			close(canceled)
		case <-stopHandler:
		}
	}))
	defer local.Close()
	defer stopRequestHandler()

	body := []byte(`{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"echo","arguments":{}}}`)
	ctx, cancel := context.WithCancel(t.Context())
	readErr := make(chan error, 1)
	go func() {
		resp, err := OpenStatelessResponse(
			ctx,
			body,
			http.Header{},
			local.URL+"/mcp",
		)
		if err != nil {
			readErr <- err
			return
		}
		close(responseReady)
		_, err = ReadLimitedResponseBody(resp.Body)
		if closeErr := resp.Body.Close(); err == nil {
			err = closeErr
		}
		readErr <- err
	}()

	waitSignal(t, responseReady, "JSON response")
	cancel()
	waitCtx, cancelWait := context.WithTimeout(t.Context(), time.Second)
	defer cancelWait()
	select {
	case err := <-readErr:
		if err == nil {
			t.Fatal("JSON response body read returned nil, want context cancellation")
		}
	case <-waitCtx.Done():
		t.Fatal("timed out waiting for JSON response body cancellation")
	}
	waitSignal(t, canceled, "JSON request cancellation")
}

func TestForwardStatelessRequestUsesMCPAccept(t *testing.T) {
	t.Parallel()

	record := forwardStatelessMCP(t, http.Header{"Accept": []string{"application/json"}})
	if got, want := record.headers.Get("Accept"), "application/json, text/event-stream"; got != want {
		t.Fatalf("tools/list Accept = %q, want mcp-go default %q", got, want)
	}
	if got := record.headers.Get("Content-Type"); got != jsonContentType {
		t.Fatalf("tools/list Content-Type = %q, want JSON default", got)
	}
}

func TestOpenStatelessResponsePreservesCallerAccept(t *testing.T) {
	t.Parallel()

	seen := http.Header{}
	local := newTestHTTPServer(t, func(w http.ResponseWriter, r *http.Request) error {
		seen = r.Header
		var req mcptransport.JSONRPCRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			return fmt.Errorf("decode request: %w", err)
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(mcplib.NewJSONRPCResultResponse(req.ID, json.RawMessage(`{}`))); err != nil {
			return fmt.Errorf("write response: %w", err)
		}
		return nil
	})

	body := []byte(`{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"streaming","arguments":{}}}`)
	resp, err := OpenStatelessResponse(
		t.Context(),
		body,
		http.Header{
			"Accept":        []string{"application/json"},
			"Content-Type":  []string{"text/plain"},
			"DAG-Bridge-Id": []string{"control"},
		},
		local.URL+"/mcp",
	)
	if err != nil {
		t.Fatalf("OpenStatelessResponse: %v", err)
	}
	defer closeTestBody(t, resp.Body)
	if _, err := ReadLimitedResponseBody(resp.Body); err != nil {
		t.Fatalf("read response: %v", err)
	}
	if got := seen.Get("Accept"); got != "application/json" {
		t.Fatalf("forwarded Accept = %q, want caller value", got)
	}
	if got := seen.Get("Content-Type"); got != jsonContentType {
		t.Fatalf("forwarded Content-Type = %q, want JSON default", got)
	}
	if got := seen.Get("DAG-Bridge-Id"); got != "" {
		t.Fatalf("forwarded bridge control header = %q, want stripped", got)
	}
}

func TestOpenStatelessResponseLeavesMissingAcceptUnset(t *testing.T) {
	t.Parallel()

	seen := make(chan string, 1)
	local := newTestHTTPServer(t, func(w http.ResponseWriter, r *http.Request) error {
		seen <- r.Header.Get("Accept")
		w.Header().Set("Content-Type", "application/json")
		_, err := w.Write([]byte(`{"jsonrpc":"2.0","id":7,"result":{}}`))
		return err
	})

	body := []byte(`{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"streaming","arguments":{}}}`)
	resp, err := OpenStatelessResponse(t.Context(), body, http.Header{}, local.URL+"/mcp")
	if err != nil {
		t.Fatalf("OpenStatelessResponse: %v", err)
	}
	defer closeTestBody(t, resp.Body)
	if _, err := ReadLimitedResponseBody(resp.Body); err != nil {
		t.Fatalf("read response: %v", err)
	}
	if got := <-seen; got != "" {
		t.Fatalf("forwarded Accept = %q, want empty", got)
	}
}

func TestForwardStatelessRequestRejectsOversizedBody(t *testing.T) {
	t.Parallel()

	body := bytes.Repeat([]byte("a"), MaxMessageBytes+1)
	if _, err := ForwardStatelessRequest(t.Context(), body, http.Header{}, "http://localhost/mcp"); err == nil {
		t.Fatalf("ForwardStatelessRequest accepted a body larger than MaxMessageBytes")
	}
}

func TestForwardStatelessRequestRejectsInvalidLocalResponse(t *testing.T) {
	t.Parallel()

	local := newTestHTTPServer(t, func(w http.ResponseWriter, _ *http.Request) error {
		_, err := w.Write([]byte(`{"ok":true}`))
		return err
	})

	body := []byte(`{"jsonrpc":"2.0","id":7,"method":"tools/list","params":{}}`)
	if _, err := ForwardStatelessRequest(t.Context(), body, http.Header{}, local.URL+"/mcp"); err == nil {
		t.Fatalf("ForwardStatelessRequest accepted non-RPC response")
	}
}

type statelessForwardRecord struct {
	rpcMethods []string
	headers    http.Header
}

func forwardStatelessMCP(t *testing.T, headers http.Header) statelessForwardRecord {
	t.Helper()

	var record statelessForwardRecord
	local := newTestHTTPServer(t, func(w http.ResponseWriter, r *http.Request) error {
		var req mcptransport.JSONRPCRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			return fmt.Errorf("decode request: %w", err)
		}
		record.rpcMethods = append(record.rpcMethods, req.Method)
		record.headers = r.Header
		if req.Method != "tools/list" {
			return fmt.Errorf("unexpected method %q", req.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(mcplib.NewJSONRPCResultResponse(
			req.ID,
			mcplib.ListToolsResult{Tools: []mcplib.Tool{{
				Name:           "echo",
				RawInputSchema: json.RawMessage(`{"type":"object"}`),
			}}},
		)); err != nil {
			return fmt.Errorf("write tools/list response: %w", err)
		}
		return nil
	})

	body := []byte(`{"jsonrpc":"2.0","id":7,"method":"tools/list","params":{}}`)
	respData, err := ForwardStatelessRequest(t.Context(), body, headers, local.URL+"/mcp")
	if err != nil {
		t.Fatalf("ForwardStatelessRequest: %v", err)
	}
	var resp mcptransport.JSONRPCResponse
	if err := json.Unmarshal(respData, &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp.Result) == 0 {
		t.Fatalf("expected result response, got %s", respData)
	}
	return record
}

func newTestHTTPServer(t *testing.T, handler func(http.ResponseWriter, *http.Request) error) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := handler(w, r); err != nil {
			t.Errorf("HTTP test handler: %v", err)
			http.Error(w, "test handler failed", http.StatusInternalServerError)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func closeTestBody(t *testing.T, body interface{ Close() error }) {
	t.Helper()
	if err := body.Close(); err != nil {
		t.Errorf("close response body: %v", err)
	}
}

func readResponseChunk(t *testing.T, body interface{ Read([]byte) (int, error) }) []byte {
	t.Helper()
	buf := make([]byte, 1024)
	n, err := body.Read(buf)
	if err != nil {
		t.Fatalf("read stream chunk: %v", err)
	}
	if n == 0 {
		t.Fatal("stream returned empty first chunk")
	}
	return buf[:n]
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
