// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

// Package runner is the shared MCP client wrapper for the
// functional suite. Targets the gateway dataplane via GATEWAY_URL,
// mints JWTs via the local demo issuer harness, exposes the streamable-HTTP MCP
// transport so each test gets a fully-handshaken session without
// rolling its own SSE merge / session-id propagation.
package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
	utilwait "k8s.io/apimachinery/pkg/util/wait"
)

// GatewayURL returns the URL the runner POSTs MCP JSON-RPC to,
// from GATEWAY_URL. tests/agent-gateway/run.sh sets it; a missing value is a
// harness misconfiguration, not a runtime condition.
func GatewayURL(t *testing.T) string {
	t.Helper()
	u := os.Getenv("GATEWAY_URL")
	if u == "" {
		t.Fatalf("GATEWAY_URL not set; run via make e2e, or export it (e.g. http://localhost:18180/mcp)")
	}
	return u
}

// Session holds an initialized MCP client + the issuing token.
// The transport handles SSE merging and Mcp-Session-Id propagation.
type Session struct {
	Client *client.Client
	Tenant string
	Token  string
}

// NewSession opens a streamable-HTTP MCP session against the gateway
// for `tenant` and returns it once `initialize` + `notifications/initialized`
// have completed. The caller defers Close on the returned Session.
func NewSession(t *testing.T, tenant string) *Session {
	t.Helper()
	return NewSessionWithHeaders(t, tenant, nil)
}

// NewSessionWithHeaders opens a session with additional caller headers.
// Tests use this only when the backend behavior under test needs an
// observable per-request marker through the real gateway path.
func NewSessionWithHeaders(t *testing.T, tenant string, extraHeaders map[string]string) *Session {
	t.Helper()
	gw := GatewayURL(t)
	tok := MintToken(t, tenant)
	headers := map[string]string{"Authorization": "Bearer " + tok}
	for k, v := range extraHeaders {
		headers[k] = v
	}

	var lastErr error
	for attempt := 1; attempt <= 6; attempt++ {
		tr, err := transport.NewStreamableHTTP(gw,
			transport.WithHTTPHeaders(headers),
		)
		if err != nil {
			t.Fatalf("transport.NewStreamableHTTP(%q): %v", gw, err)
		}
		cli := client.NewClient(tr)

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		err = cli.Start(ctx)
		if err == nil {
			initReq := mcp.InitializeRequest{}
			initReq.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
			initReq.Params.ClientInfo = mcp.Implementation{Name: "dsx-agent-gateway-functional-runner", Version: "0.1.0"}
			_, err = cli.Initialize(ctx, initReq)
			if err == nil {
				cancel()
				return &Session{Client: cli, Tenant: tenant, Token: tok}
			}
		}
		lastErr = err
		cancel()
		_ = cli.Close()
		if attempt < 6 {
			_ = waitForDelay(context.Background(), 500*time.Millisecond)
		}
	}
	t.Fatalf("MCP session setup for %s failed after retries: %v", tenant, lastErr)
	return nil
}

// Close releases the streamable-HTTP transport. Always call via
// `t.Cleanup(s.Close)` so an asserting test that fails mid-flight
// still drops the connection.
func (s *Session) Close() {
	if s == nil || s.Client == nil {
		return
	}
	_ = s.Client.Close()
}

// ListToolNames returns every tool name the session's tenant is
// authorized to see. The runner does not paginate today — every
// tested tenant fits well below the MCP page size.
func (s *Session) ListToolNames(ctx context.Context) ([]string, error) {
	res, err := s.Client.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil {
		return nil, fmt.Errorf("tools/list: %w", err)
	}
	out := make([]string, 0, len(res.Tools))
	for _, t := range res.Tools {
		out = append(out, t.Name)
	}
	return out, nil
}

// ListPromptNames mirrors ListToolNames for the prompts category.
func (s *Session) ListPromptNames(ctx context.Context) ([]string, error) {
	res, err := s.Client.ListPrompts(ctx, mcp.ListPromptsRequest{})
	if err != nil {
		return nil, fmt.Errorf("prompts/list: %w", err)
	}
	out := make([]string, 0, len(res.Prompts))
	for _, p := range res.Prompts {
		out = append(out, p.Name)
	}
	return out, nil
}

// RawResponse is a one-shot HTTP probe to $GATEWAY_URL with no MCP
// session handshake. Tests that exercise the gateway's
// pre-authorization paths (unauthenticated rejects, malformed-body
// 400s, HTTP-level CEL denies on non-MCP routes) skip the streamable-
// HTTP transport because there is no session to open.
type RawResponse struct {
	Status int
	Body   []byte
	Header http.Header
}

// DoPostMCP issues a single POST /mcp without going through the MCP
// session handshake. `bearer` is empty for unauthenticated probes.
// The gateway's own URL — including the `/mcp` suffix — comes from
// GatewayURL. `body` is the raw JSON-RPC payload bytes.
func DoPostMCP(t *testing.T, ctx context.Context, bearer string, body []byte) (RawResponse, error) {
	t.Helper()
	gw := GatewayURL(t)
	return doPost(t, ctx, gw, bearer, body)
}

// PostMCP is the fail-fast form of DoPostMCP for tests that only
// inspect HTTP status and response body.
func PostMCP(t *testing.T, ctx context.Context, bearer string, body []byte) RawResponse {
	t.Helper()
	resp, err := DoPostMCP(t, ctx, bearer, body)
	if err != nil {
		t.Fatalf("POST /mcp: %v", err)
	}
	return resp
}

func doPost(t *testing.T, ctx context.Context, url, bearer string, body []byte) (RawResponse, error) {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return RawResponse{}, fmt.Errorf("build POST %s: %w", url, err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	httpc := &http.Client{
		Timeout:   10 * time.Second,
		Transport: &http.Transport{DisableKeepAlives: true},
	}
	defer httpc.CloseIdleConnections()
	resp, err := httpc.Do(req)
	if err != nil {
		return RawResponse{}, fmt.Errorf("POST %s: %w", url, err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	raw := RawResponse{Status: resp.StatusCode, Body: b, Header: resp.Header.Clone()}
	if err != nil {
		return raw, fmt.Errorf("read POST %s response body status=%d: %w", url, resp.StatusCode, err)
	}
	return raw, nil
}

// JSONRPCError parses a body that may carry a JSON-RPC error envelope
// (either bare or SSE-framed). Returns ok=false if no JSON-RPC error
// can be extracted. Used by tests that assert deny-shape.
func JSONRPCError(body []byte) (code int, message string, ok bool) {
	// Try bare JSON first.
	var bare struct {
		Error struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &bare); err == nil && bare.Error.Message != "" {
		return bare.Error.Code, bare.Error.Message, true
	}
	// SSE-framed: take the last `data:` line.
	for _, line := range bytes.Split(body, []byte("\n")) {
		if !bytes.HasPrefix(line, []byte("data: ")) {
			continue
		}
		payload := bytes.TrimPrefix(line, []byte("data: "))
		if err := json.Unmarshal(payload, &bare); err == nil && bare.Error.Message != "" {
			return bare.Error.Code, bare.Error.Message, true
		}
	}
	return 0, "", false
}

// WaitFor calls `check` until it returns true or `timeout` elapses.
// Returns false on timeout.
func WaitFor(timeout time.Duration, interval time.Duration, check func() bool) bool {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return WaitForContext(ctx, interval, check)
}

// WaitForContext calls check immediately, then retries with bounded
// backoff until check returns true or ctx is done.
func WaitForContext(ctx context.Context, interval time.Duration, check func() bool) bool {
	if interval <= 0 {
		interval = 50 * time.Millisecond
	}
	return utilwait.PollUntilContextCancel(ctx, interval, true, func(context.Context) (bool, error) {
		return check(), nil
	}) == nil
}

func waitForDelay(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return nil
	}
	return utilwait.PollUntilContextTimeout(ctx, delay, delay, false, func(context.Context) (bool, error) {
		return true, nil
	})
}
