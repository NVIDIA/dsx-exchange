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
	"errors"
	"io"
	"log/slog"
	"strconv"
	"sync"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/micro"

	bridgemcp "github.com/NVIDIA/dsx-exchange/dsx-agentgateway-bridge/internal/mcp"
	"github.com/NVIDIA/dsx-exchange/dsx-agentgateway-bridge/internal/shardbus"
)

const streamChunkSize = 32 * 1024

// streamRegistry routes READ and CLOSE requests to request-scoped streams.
// Each task retains the HTTP body and cursor state referenced by its handle.
type streamRegistry struct {
	mu                 sync.Mutex
	streams            map[string]*streamHandle
	ctx                context.Context
	cancel             context.CancelFunc
	requestTasks       sync.WaitGroup
	streamRequestTasks sync.WaitGroup
	pub                publisher
	longPollTimeout    time.Duration
	// idleTimeout bounds an SSE task when Core NATS loses metadata or cleanup.
	idleTimeout time.Duration
}

// streamHandle is the registry's cancellation and READ channel for one task.
type streamHandle struct {
	ctx    context.Context
	cancel context.CancelFunc
	reads  chan streamRead
}

type streamRead struct {
	cursor uint64
	result chan *frameFuture
}

// frameFuture caches one completed frame. Every repeated READ for its cursor
// can publish the same result, including after an earlier Core NATS reply loss.
type frameFuture struct {
	ready     chan struct{}
	data      []byte
	frameType string
	resolved  bool
}

type bodyRead struct {
	data []byte
	err  error
}

func newStreamRegistry(parent context.Context, pub publisher, longPollTimeout time.Duration) *streamRegistry {
	ctx, cancel := context.WithCancel(context.WithoutCancel(parent))
	return &streamRegistry{
		streams:         make(map[string]*streamHandle),
		ctx:             ctx,
		cancel:          cancel,
		pub:             pub,
		longPollTimeout: longPollTimeout,
		idleTimeout:     2*(longPollTimeout+time.Second) + time.Second,
	}
}

func newStream(parent context.Context) *streamHandle {
	ctx, cancel := context.WithCancel(parent)
	return &streamHandle{
		ctx:    ctx,
		cancel: cancel,
		reads:  make(chan streamRead),
	}
}

func (s *streamRegistry) register(id string, stream *streamHandle) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.streams[id] = stream
}

func (s *streamRegistry) lookup(id string) *streamHandle {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.streams[id]
}

func (s *streamRegistry) take(id string) *streamHandle {
	s.mu.Lock()
	defer s.mu.Unlock()
	stream := s.streams[id]
	delete(s.streams, id)
	return stream
}

func (s *streamRegistry) waitForRequests(ctx context.Context) {
	stopTimeout := context.AfterFunc(ctx, s.cancel)
	s.requestTasks.Wait()
	stopTimeout()
}

func (s *streamRegistry) close() {
	s.cancel()
	s.requestTasks.Wait()
	s.streamRequestTasks.Wait()
}

func (s *streamRegistry) handleStreamRequest(headers nats.Header, reply string) {
	switch operation := headers.Get(shardbus.StreamOperationHeader); operation {
	case shardbus.StreamOperationRead:
		s.handleRead(headers, reply)
	case shardbus.StreamOperationClose:
		s.handleClose(headers, reply)
	default:
		respondStreamRequestError(s.pub, reply, "400", "unsupported stream operation")
	}
}

func (s *streamRegistry) handleClose(headers nats.Header, reply string) {
	streamID := headers.Get(shardbus.StreamIDHeader)
	// CLOSE remains successful after removal so a lost CLOSE reply can be
	// retried without reviving or leaking the stream task.
	if stream := s.take(streamID); stream != nil {
		stream.cancel()
	}
	if err := publishReply(s.pub, reply, nil, nil); err != nil {
		slog.Error("respond to stream close", "error", err)
	}
}

func (s *streamRegistry) handleRead(headers nats.Header, reply string) {
	streamID := headers.Get(shardbus.StreamIDHeader)
	stream := s.lookup(streamID)
	if stream == nil {
		respondStreamRequestError(s.pub, reply, "404", "stream not found")
		return
	}
	cursor, err := strconv.ParseUint(headers.Get(shardbus.StreamCursorHeader), 10, 64)
	if err != nil {
		respondStreamRequestError(s.pub, reply, "409", "invalid stream cursor")
		return
	}
	s.replyToStreamRead(stream, reply, cursor)
}

func (stream *streamHandle) frameForCursor(ctx context.Context, cursor uint64) (*frameFuture, error) {
	// Buffer the result so the stream task can finish dispatch if cancellation makes
	// this caller stop waiting first.
	result := make(chan *frameFuture, 1)
	select {
	case stream.reads <- streamRead{cursor: cursor, result: result}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	select {
	case future := <-result:
		if future == nil {
			return nil, errors.New("stale or skipped stream cursor")
		}
		return future, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (s *streamRegistry) replyToStreamRead(stream *streamHandle, reply string, cursor uint64) {
	ctx, cancel := context.WithTimeout(stream.ctx, s.longPollTimeout)
	defer cancel()
	future, err := stream.frameForCursor(ctx, cursor)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			publishStreamReply(s.pub, reply, nil, streamResponseHeaders(shardbus.StreamFramePending))
		} else {
			respondStreamRequestError(s.pub, reply, "409", err.Error())
		}
		return
	}
	select {
	case <-future.ready:
		publishStreamReply(s.pub, reply, future.data, streamResponseHeaders(future.frameType))
	case <-ctx.Done():
		if stream.ctx.Err() == nil {
			publishStreamReply(s.pub, reply, nil, streamResponseHeaders(shardbus.StreamFramePending))
		} else {
			respondStreamRequestError(s.pub, reply, "409", "stream is closed")
		}
	}
}

func (stream *streamHandle) serveReads(body io.ReadCloser, requestID mcp.RequestId, idleTimeout time.Duration) io.ReadCloser {
	idle := time.NewTimer(idleTimeout)
	defer idle.Stop()
	cursor := uint64(0)
	future := newFrameFuture()
	endNext := false
	var read <-chan bodyRead

	// The stream task starts at most one body read for the current cursor. Repeating
	// that cursor returns its cached future. Cursor n+1 acknowledges frame n
	// and permits the next read. An END frame is sticky and remains replayable.
	for {
		select {
		case <-stream.ctx.Done():
			return body
		case <-idle.C:
			return body
		case request := <-stream.reads:
			switch {
			case request.cursor == cursor:
			case request.cursor == cursor+1 && future.resolved && future.frameType != shardbus.StreamFrameEnd:
				cursor = request.cursor
				future = newFrameFuture()
			default:
				request.result <- nil
				continue
			}
			idle.Reset(idleTimeout)
			if !future.resolved && read == nil {
				if endNext {
					future.complete(nil, shardbus.StreamFrameEnd)
					endNext = false
				} else {
					read = startBodyRead(body)
				}
			}
			request.result <- future
		case result := <-read:
			read = nil
			// Give the hub a full idle window to write this frame and recover if
			// its next READ is dropped.
			idle.Reset(idleTimeout)
			if result.err != nil {
				closeResponseBody(body)
				body = nil
			}
			if len(result.data) > 0 {
				future.complete(result.data, "")
				// Once response bytes have been relayed, do not guess whether an
				// accompanying read error preceded or followed a terminal result.
				// End the stream instead of synthesizing a second response.
				if result.err != nil {
					endNext = true
				}
				continue
			}
			endNext = completeReadError(future, requestID, result.err)
		}
	}
}

func newFrameFuture() *frameFuture {
	return &frameFuture{ready: make(chan struct{})}
}

func (f *frameFuture) complete(data []byte, frameType string) {
	// Only the stream task completes a future. Closing ready publishes the assigned
	// frame safely to every responder waiting on this cursor.
	f.data = data
	f.frameType = frameType
	f.resolved = true
	close(f.ready)
}

func completeReadError(future *frameFuture, requestID mcp.RequestId, err error) bool {
	if errors.Is(err, io.EOF) {
		future.complete(nil, shardbus.StreamFrameEnd)
		return false
	}
	data, encodeErr := marshalRPCError(requestID, bridgemcp.CodeLeafForwardFailed, err.Error())
	if encodeErr != nil {
		slog.Error("encode interrupted stream error", "error", encodeErr)
		future.complete(nil, shardbus.StreamFrameEnd)
		return false
	}
	future.complete(sseErrorEvent(data), "")
	// Publish a JSON-RPC SSE error as data first. The true result caches an end
	// frame for the next cursor, so the hub delivers the error before closing.
	return true
}

func sseErrorEvent(data []byte) []byte {
	// A failed upstream read may follow bytes that ended mid-event. Start with
	// an empty line so the bridge-generated error is always a separate event.
	out := make([]byte, 0, len("\n\nevent: message\ndata: \n\n")+len(data))
	out = append(out, "\n\nevent: message\ndata: "...)
	out = append(out, data...)
	out = append(out, "\n\n"...)
	return out
}

func startBodyRead(body io.Reader) <-chan bodyRead {
	// Buffer the result so closing a canceled HTTP body cannot leave its reader
	// blocked while reporting completion to a stream task that has already exited.
	result := make(chan bodyRead, 1)
	go func() {
		buf := make([]byte, streamChunkSize)
		for {
			n, err := body.Read(buf)
			if n != 0 || err != nil {
				result <- bodyRead{data: buf[:n], err: err}
				return
			}
		}
	}()
	return result
}

func closeResponseBody(body io.Closer) {
	if body == nil {
		return
	}
	if err := body.Close(); err != nil {
		slog.Error("close local gateway response body", "error", err)
	}
}

func streamResponseHeaders(frameType string) nats.Header {
	headers := nats.Header{}
	if frameType != "" {
		headers[shardbus.StreamFrameHeader] = []string{frameType}
	}
	return headers
}

func publishStreamReply(pub publisher, reply string, data []byte, headers nats.Header) {
	if err := publishReply(pub, reply, data, headers); err != nil {
		slog.Error("respond to stream read", "error", err)
	}
}

func publishReply(pub publisher, reply string, data []byte, headers nats.Header) error {
	return pub.PublishMsg(&nats.Msg{Subject: reply, Header: headers, Data: data})
}

func respondStreamRequestError(pub publisher, reply, code, description string) {
	publishStreamReply(pub, reply, nil, nats.Header{
		micro.ErrorCodeHeader: []string{code},
		micro.ErrorHeader:     []string{description},
	})
}
