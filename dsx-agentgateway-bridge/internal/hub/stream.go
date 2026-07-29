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
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/micro"

	bridgemcp "github.com/NVIDIA/dsx-exchange/dsx-agentgateway-bridge/internal/mcp"
	"github.com/NVIDIA/dsx-exchange/dsx-agentgateway-bridge/internal/shardbus"
)

var errInvalidLeafResponse = errors.New("invalid leaf response")

// forwardLeafRequest relays either one complete JSON reply or an SSE response.
// The returned bool reports whether the HTTP response has started, after which
// callers can no longer replace a transport failure with a JSON-RPC error.
func (h hub) forwardLeafRequest(w http.ResponseWriter, r *http.Request, subject string, body []byte) (bool, error) {
	initialReply, err := h.requestLeafOnce(r, subject, body)
	if err != nil {
		return false, fmt.Errorf("send bridge request: %w", err)
	}
	streamID := initialReply.Header.Get(shardbus.StreamIDHeader)
	if streamID == "" {
		return writeBridgeResponse(w, initialReply, h.writeTimeout)
	}
	streamWriteTimeout := min(h.writeTimeout, h.timeout)

	// CLOSE cancels an unfinished upstream response or acknowledges that a
	// cached EOF no longer needs replay. Deferring it covers every exit path.
	defer h.closeBridgeStream(r, initialReply.Reply, streamID)
	status, err := responseStatus(initialReply)
	if err != nil {
		return false, fmt.Errorf("%w: %v", errInvalidLeafResponse, err)
	}
	w.Header().Set("Content-Type", initialReply.Header.Get(shardbus.HTTPContentTypeHeader))
	// A successful downstream write must leave time for the next READ before
	// the leaf's two-attempt idle window expires.
	responseStarted, err := writeAndFlush(w, streamWriteTimeout, status, nil)
	if err != nil {
		return responseStarted, fmt.Errorf("write bridge stream headers: %w", err)
	}

	for cursor := uint64(0); ; {
		frame, err := h.requestStreamFrame(r, initialReply.Reply, streamID, cursor)
		if err != nil {
			return true, fmt.Errorf("read bridge stream: %w", err)
		}
		switch frameType := frame.Header.Get(shardbus.StreamFrameHeader); frameType {
		case shardbus.StreamFramePending:
			// A pending long poll has no bytes to acknowledge, so retry the same
			// cursor. Data advances the cursor only after the HTTP write succeeds.
			continue
		case shardbus.StreamFrameEnd:
			return true, nil
		case "":
			if _, err := writeAndFlush(w, streamWriteTimeout, 0, frame.Data); err != nil {
				return true, fmt.Errorf("write bridge stream body: %w", err)
			}
			cursor++
		default:
			return true, fmt.Errorf("invalid bridge stream frame %q", frameType)
		}
	}
}

// requestStreamFrame retries timeout failures with the same cursor so a lost
// Core NATS READ or reply can use the leaf's cached frame. Caller cancellation,
// no responders, service errors, and other transport errors are terminal.
func (h hub) requestStreamFrame(r *http.Request, subject, streamID string, cursor uint64) (*nats.Msg, error) {
	for {
		ctx, cancel := context.WithTimeout(r.Context(), h.timeout+time.Second)
		msg := newLeafRequest(ctx, http.Header{}, subject, nil)
		msg.Header.Set(shardbus.StreamIDHeader, streamID)
		msg.Header.Set(shardbus.StreamCursorHeader, strconv.FormatUint(cursor, 10))
		msg.Header.Set(shardbus.StreamOperationHeader, shardbus.StreamOperationRead)
		resp, err := h.bus.RequestMsgWithContext(ctx, msg)
		cancel()
		if err == nil {
			if description := resp.Header.Get(micro.ErrorHeader); description != "" {
				return nil, fmt.Errorf("leaf returned stream error %s: %s", resp.Header.Get(micro.ErrorCodeHeader), description)
			}
			return resp, nil
		}
		if errors.Is(err, nats.ErrNoResponders) || r.Context().Err() != nil {
			return nil, err
		}
		if !errors.Is(err, nats.ErrTimeout) && !errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}
	}
}

// closeBridgeStream best-effort retries an idempotent CLOSE within one cleanup
// timeout. CLOSE cancels an active stream task or releases its replayable EOF state.
func (h hub) closeBridgeStream(r *http.Request, subject, streamID string) {
	// A disconnected caller is the main reason cleanup is needed. WithoutCancel
	// retains request values while giving CLOSE its own bounded lifetime.
	ctx, cancelCleanup := context.WithTimeout(context.WithoutCancel(r.Context()), h.timeout)
	defer cancelCleanup()
	for ctx.Err() == nil {
		attemptCtx, cancel := context.WithTimeout(ctx, time.Second)
		msg := newLeafRequest(attemptCtx, http.Header{}, subject, nil)
		msg.Header.Set(shardbus.StreamIDHeader, streamID)
		msg.Header.Set(shardbus.StreamOperationHeader, shardbus.StreamOperationClose)
		_, err := h.bus.RequestMsgWithContext(attemptCtx, msg)
		cancel()
		if err == nil {
			return
		}
		if errors.Is(err, nats.ErrNoResponders) {
			return
		}
		if !errors.Is(err, nats.ErrTimeout) && !errors.Is(err, context.DeadlineExceeded) {
			slog.Error("close bridge stream", "error", err)
			return
		}
	}
	slog.Warn("close bridge stream timed out", "stream_id", streamID)
}

func writeBridgeResponse(w http.ResponseWriter, msg *nats.Msg, timeout time.Duration) (bool, error) {
	if len(msg.Data) > bridgemcp.MaxMessageBytes {
		return false, fmt.Errorf("%w: body exceeds %d-byte limit", errInvalidLeafResponse, bridgemcp.MaxMessageBytes)
	}
	status, err := responseStatus(msg)
	if err != nil {
		return false, fmt.Errorf("%w: %v", errInvalidLeafResponse, err)
	}
	w.Header().Set("Content-Type", msg.Header.Get(shardbus.HTTPContentTypeHeader))
	committed, err := writeAndFlush(w, timeout, status, msg.Data)
	if err != nil {
		return committed, fmt.Errorf("write bridge response: %w", err)
	}
	return committed, nil
}

func responseStatus(msg *nats.Msg) (int, error) {
	rawStatus := msg.Header.Get(shardbus.HTTPStatusHeader)
	status, err := strconv.Atoi(rawStatus)
	if err != nil || status < 100 || status > 999 {
		return 0, fmt.Errorf("invalid bridge HTTP status %q", rawStatus)
	}
	return status, nil
}
