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
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/nats-io/nats.go"

	bridgemcp "github.com/NVIDIA/dsx-exchange/dsx-agentgateway-bridge/internal/mcp"
	"github.com/NVIDIA/dsx-exchange/dsx-agentgateway-bridge/internal/shardbus"
)

// handleInitialRequest owns the forwarded HTTP request until a JSON response
// completes or an SSE stream closes.
func handleInitialRequest(initialCtx context.Context, requestData []byte, headers http.Header, reply, localGatewayOrigin string, registry *streamRegistry, streamSubject string) {
	stream := newStream(registry.ctx)
	var body io.ReadCloser
	defer func() {
		stream.cancel()
		closeResponseBody(body)
	}()
	// The initial deadline covers response headers and a complete JSON body.
	// SSE replaces it with the stream task lifetime once its headers arrive.
	stopInitialCancel := context.AfterFunc(initialCtx, stream.cancel)
	defer stopInitialCancel()

	resp, err := bridgemcp.OpenStatelessResponse(
		stream.ctx,
		requestData,
		headers,
		localGatewayOrigin,
	)
	if err != nil {
		slog.Error("forward initial request to local gateway", "error", err)
		respondInitialError(registry.pub, reply, requestData, err)
		return
	}
	body = resp.Body

	if bridgemcp.IsEventStream(resp.Header) {
		if stopped := stopInitialCancel(); !stopped || initialCtx.Err() != nil {
			err := initialCtx.Err()
			if err == nil {
				err = context.Canceled
			}
			respondInitialError(registry.pub, reply, requestData, err)
			return
		}
		streamID := reply
		registry.register(streamID, stream)
		defer registry.take(streamID)
		if err := respondStreamStart(registry.pub, resp.StatusCode, resp.Header, streamID, streamSubject); err != nil {
			slog.Error("respond to stream start", "error", err)
			return
		}
		body = stream.serveReads(body, rpcRequestID(requestData), registry.idleTimeout)
		return
	}
	respondBufferedJSON(registry.pub, reply, requestData, resp)
}

func respondBufferedJSON(pub publisher, reply string, requestData []byte, resp *http.Response) {
	if !bridgemcp.IsJSONResponse(resp.Header) {
		respondInitialError(pub, reply, requestData, fmt.Errorf("local response Content-Type %q is not application/json", resp.Header.Get("Content-Type")))
		return
	}
	responseBody, err := bridgemcp.ReadLimitedResponseBody(resp.Body)
	if err != nil {
		slog.Error("read buffered local gateway response failed", "error", err)
		respondInitialError(pub, reply, requestData, err)
		return
	}
	if err := respondInitialResponse(pub, reply, resp.StatusCode, resp.Header, responseBody); err != nil {
		slog.Error("respond to buffered JSON response", "error", err)
	}
}

func respondInitialError(pub publisher, reply string, requestData []byte, err error) {
	data, encodeErr := marshalRPCErrorForRequest(requestData, bridgemcp.CodeLeafForwardFailed, err.Error())
	if encodeErr != nil {
		slog.Error("encode initial forward error", "error", encodeErr)
		return
	}
	if err := respondInitialResponse(pub, reply, http.StatusOK, http.Header{"Content-Type": []string{"application/json"}}, data); err != nil {
		slog.Error("respond to initial forward error", "error", err)
	}
}

// respondStreamStart uses the initial reply inbox as the stream ID and places
// the leaf's direct stream subject in the native NATS Reply field.
func respondStreamStart(pub publisher, status int, headers http.Header, streamID, streamSubject string) error {
	return pub.PublishMsg(&nats.Msg{
		Subject: streamID,
		Reply:   streamSubject,
		Header: nats.Header{
			shardbus.HTTPStatusHeader:      []string{strconv.Itoa(status)},
			shardbus.HTTPContentTypeHeader: []string{headers.Get("Content-Type")},
			shardbus.StreamIDHeader:        []string{streamID},
		},
	})
}

func respondInitialResponse(pub publisher, reply string, status int, headers http.Header, data []byte) error {
	return publishReply(pub, reply, data, nats.Header{
		shardbus.HTTPStatusHeader:      []string{strconv.Itoa(status)},
		shardbus.HTTPContentTypeHeader: []string{headers.Get("Content-Type")},
	})
}
