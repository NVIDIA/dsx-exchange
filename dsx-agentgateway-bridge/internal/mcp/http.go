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
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"

	mcptransport "github.com/mark3labs/mcp-go/client/transport"
	mcplib "github.com/mark3labs/mcp-go/mcp"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"github.com/NVIDIA/dsx-exchange/dsx-agentgateway-bridge/internal/telemetry"
)

const (
	MaxMessageBytes = 1 << 20
	jsonContentType = "application/json"
)

var forwardHTTPClient = &http.Client{Transport: otelhttp.NewTransport(http.DefaultTransport)}

func ForwardStatelessRequest(ctx context.Context, body []byte, headers http.Header, localEndpoint string) ([]byte, error) {
	ctx, headers, req, err := prepareStatelessForward(ctx, body, headers)
	if err != nil {
		return nil, err
	}

	transport, err := mcptransport.NewStreamableHTTP(localEndpoint, mcptransport.WithHTTPBasicClient(forwardHTTPClient))
	if err != nil {
		return nil, fmt.Errorf("create local mcp transport: %w", err)
	}
	if err := transport.Start(ctx); err != nil {
		return nil, errors.Join(fmt.Errorf("start local mcp transport: %w", err), transport.Close())
	}

	// mcp-go owns the local HTTP exchange. The caller's JSON-RPC id is restored
	// after the local response so the bridge remains transparent to its caller.
	rpcResp, err := transport.SendRequest(ctx, mcptransport.JSONRPCRequest{
		JSONRPC: mcplib.JSONRPC_VERSION,
		ID:      mcplib.NewRequestId(1),
		Method:  req.Method,
		Params:  req.Params,
		Header:  headers,
	})
	closeErr := transport.Close()
	if err != nil {
		return nil, errors.Join(fmt.Errorf("local %s: %w", req.Method, err), closeErr)
	}
	if closeErr != nil {
		return nil, fmt.Errorf("close local mcp transport: %w", closeErr)
	}
	if rpcResp == nil {
		return nil, errors.New("local RPC response is nil")
	}
	if rpcResp.Error != nil {
		return json.Marshal(mcptransport.NewJSONRPCErrorResponse(req.ID, rpcResp.Error.Code, rpcResp.Error.Message, rpcResp.Error.Data))
	}
	return json.Marshal(mcptransport.NewJSONRPCResultResponse(req.ID, rpcResp.Result))
}

func OpenStatelessResponse(ctx context.Context, body []byte, headers http.Header, localEndpoint string) (*http.Response, error) {
	ctx, headers, _, err := prepareStatelessForward(ctx, body, headers)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, localEndpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create local mcp request: %w", err)
	}
	httpReq.Header = headers

	resp, err := forwardHTTPClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("local mcp post: %w", err)
	}
	return resp, nil
}

func ReadLimitedResponseBody(r io.Reader) ([]byte, error) {
	limited := &io.LimitedReader{R: r, N: MaxMessageBytes + 1}
	body, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	if len(body) > MaxMessageBytes {
		return nil, fmt.Errorf("body exceeds %d-byte limit", MaxMessageBytes)
	}
	return body, nil
}

func IsEventStream(headers http.Header) bool {
	mediaType, _, err := mime.ParseMediaType(headers.Get("Content-Type"))
	return err == nil && strings.EqualFold(mediaType, "text/event-stream")
}

func IsJSONResponse(headers http.Header) bool {
	mediaType, _, err := mime.ParseMediaType(headers.Get("Content-Type"))
	return err == nil && strings.EqualFold(mediaType, "application/json")
}

func prepareStatelessForward(ctx context.Context, body []byte, headers http.Header) (context.Context, http.Header, mcptransport.JSONRPCRequest, error) {
	var req mcptransport.JSONRPCRequest
	if len(body) > MaxMessageBytes {
		return ctx, nil, req, fmt.Errorf("body exceeds %d-byte limit", MaxMessageBytes)
	}
	req, err := DecodeRequest(body)
	if err != nil {
		return ctx, nil, req, fmt.Errorf("decode rpc request: %w", err)
	}

	if headers == nil {
		headers = http.Header{}
	} else {
		headers = headers.Clone()
	}
	// The bridge opens a fresh stateless transport per request. Forwarding the
	// caller's MCP session id would incorrectly bind leaves to a hub session.
	// Let the shared Go transport negotiate response compression so it can
	// transparently decode the response before the bridge relays its body.
	headers.Del("Accept-Encoding")
	headers.Del("Content-Length")
	headers.Del("Mcp-Session-Id")
	ctx = telemetry.ExtractTraceContext(ctx, headers)
	for name := range headers {
		if strings.HasPrefix(strings.ToLower(name), "dag-bridge-") {
			delete(headers, name)
		}
	}
	headers.Set("Content-Type", jsonContentType)
	return ctx, headers, req, nil
}
