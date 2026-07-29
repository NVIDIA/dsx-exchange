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

// Caller-JWT passthrough behavior.
package functional

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/NVIDIA/dsx-exchange/tests/agent-gateway/functional/internal/runner"
	"github.com/mark3labs/mcp-go/mcp"
)

func TestPassthroughEvidence(t *testing.T) {
	runner.ParallelReadOnly(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	s := runner.NewSession(t, tenantAUnlimited)
	t.Cleanup(s.Close)

	names, err := s.ListToolNames(ctx)
	if err != nil {
		t.Fatalf("tools/list: %v", err)
	}
	const toolName = "mcp-backend-a-mcp_headers"
	found := false
	for _, name := range names {
		if name == toolName {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("%s tools/list did not include %q (names: %v)", tenantAUnlimited, toolName, names)
	}

	req := mcp.CallToolRequest{}
	req.Params.Name = toolName
	req.Params.Arguments = map[string]any{}
	res, err := s.Client.CallTool(ctx, req)
	if err != nil {
		t.Fatalf("tools/call(%q): %v", toolName, err)
	}
	headers := decodeHeaderEchoResult(t, res)
	auth := headerValue(headers, "authorization")
	want := "Bearer " + s.Token
	if auth == "" {
		t.Fatalf("MCP backend did not receive an Authorization header (headers: %v)", headers)
	}
	if auth != want {
		t.Fatalf("echoed Authorization header did not match caller JWT (got %d-byte header, expected %d-byte bearer)", len(auth), len(want))
	}
}

func decodeHeaderEchoResult(t *testing.T, res *mcp.CallToolResult) map[string]string {
	t.Helper()
	if res == nil {
		t.Fatalf("headers tool returned nil result")
	}
	for _, item := range res.Content {
		text := ""
		switch v := item.(type) {
		case mcp.TextContent:
			text = v.Text
		case *mcp.TextContent:
			text = v.Text
		}
		if text == "" {
			continue
		}
		var headers map[string]string
		if err := json.Unmarshal([]byte(text), &headers); err != nil {
			t.Fatalf("headers tool text was not a JSON object: %v (text: %s)", err, text)
		}
		return headers
	}
	t.Fatalf("headers tool returned no text content: %+v", res)
	return nil
}

func headerValue(headers map[string]string, key string) string {
	for k, v := range headers {
		if strings.EqualFold(k, key) {
			return v
		}
	}
	return ""
}

// TestPassthroughIsolated — tools/call must not fan out across
// backends. Counts `Received MCP POST request` lines in each
// backend's stdout before/after a burst of N follow-up calls inside
// a single warmed session and asserts the aggregate delta is below
// the broadcast threshold (3N/2).
func TestDestructivePassthroughIsolated(t *testing.T) {
	runner.DestructiveFunctional(t)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	tok := runner.MintToken(t, tenantAUnlimited)

	// Warm a session via raw HTTP so the test controls session reuse.
	gw := runner.GatewayURL(t)

	post := func(sid, body string) (*http.Response, []byte, error) {
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, gw, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+tok)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		if sid != "" {
			req.Header.Set("Mcp-Session-Id", sid)
		}
		httpc := &http.Client{
			Timeout:   10 * time.Second,
			Transport: &http.Transport{DisableKeepAlives: true},
		}
		defer httpc.CloseIdleConnections()
		r, err := httpc.Do(req)
		if err != nil {
			return nil, nil, err
		}
		defer r.Body.Close()
		b, err := io.ReadAll(r.Body)
		return r, b, err
	}

	var sid, firstAllowed string
	warmCtx, warmCancel := context.WithTimeout(ctx, 10*time.Second)
	defer warmCancel()
	warmed := runner.WaitForContext(warmCtx, 200*time.Millisecond, func() bool {
		resp, _, err := post("", `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"iso-31","version":"1"}}}`)
		if err != nil {
			return false
		}
		sid = resp.Header.Get("Mcp-Session-Id")
		if sid == "" {
			return false
		}
		if _, _, err := post(sid, `{"jsonrpc":"2.0","method":"notifications/initialized"}`); err != nil {
			sid = ""
			return false
		}
		_, body, err := post(sid, `{"jsonrpc":"2.0","id":99,"method":"tools/list","params":{}}`)
		if err != nil {
			sid = ""
			return false
		}
		body = lastSSEData(body)
		var probe struct {
			Result struct {
				Tools []struct {
					Name string `json:"name"`
				} `json:"tools"`
			} `json:"result"`
		}
		if err := json.Unmarshal(body, &probe); err == nil {
			for _, tool := range probe.Result.Tools {
				if strings.HasPrefix(tool.Name, "mcp-backend-a-mcp_") || strings.HasPrefix(tool.Name, "mcp-backend-b-mcp_") {
					firstAllowed = tool.Name
					break
				}
			}
			if firstAllowed != "" {
				return true
			}
		}
		sid = ""
		return false
	})
	if !warmed {
		t.Fatalf("could not warm session with an mcp-backend-a/b tool in the catalogue")
	}

	countPostsSince := func(deploy string, since time.Time) int {
		// `deploy` is the Deployment name, for example mcp-backend-a.
		// The chart's instance label is the suffix, for example backend-a.
		instance := strings.TrimPrefix(deploy, "mcp-")
		out := runner.LogsByLabelSinceTime(t, "csc-mcp-backends", "app.kubernetes.io/instance="+instance, since)
		const marker = "Received MCP POST request"
		n := 0
		for _, line := range strings.Split(out, "\n") {
			if strings.HasSuffix(line, marker) || line == marker {
				n++
			}
		}
		return n
	}
	logWindowStart := time.Now().Add(-1 * time.Second)
	baseA := countPostsSince("mcp-backend-a", logWindowStart)
	baseB := countPostsSince("mcp-backend-b", logWindowStart)

	// Require both a successful call AND a routed log delta.
	// "delta < threshold" alone passes on a gateway that rejects
	// every call (delta=0).
	const N = 5
	successes := 0
	for i := 1; i <= N; i++ {
		body := fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"method":"tools/call","params":{"name":%q,"arguments":{"message":"iso-%d"}}}`, i+1, firstAllowed, i)
		resp, b, err := post(sid, body)
		if err != nil {
			t.Fatalf("tools/call iter %d post: %v", i, err)
		}
		if resp.StatusCode != 200 {
			t.Fatalf("tools/call iter %d returned HTTP %d (body %s) — gateway is rejecting calls; isolation assertion has no signal", i, resp.StatusCode, b)
		}
		parsed := lastSSEData(b)
		var page struct {
			Result *json.RawMessage `json:"result"`
			Error  *json.RawMessage `json:"error"`
		}
		if err := json.Unmarshal(parsed, &page); err != nil {
			t.Fatalf("tools/call iter %d body parse: %v (body %s)", i, err, parsed)
		}
		if page.Error != nil {
			t.Fatalf("tools/call iter %d returned JSON-RPC error %s (body %s) — call rejected, isolation assertion has no signal", i, string(*page.Error), parsed)
		}
		if page.Result == nil {
			t.Fatalf("tools/call iter %d returned 200 but no .result (body %s) — degenerate response, isolation assertion has no signal", i, parsed)
		}
		successes++
	}
	if successes != N {
		t.Fatalf("only %d/%d tools/call succeeded — isolation assertion has no signal", successes, N)
	}

	var afterA, afterB int
	if !runner.WaitFor(20*time.Second, 250*time.Millisecond, func() bool {
		afterA = countPostsSince("mcp-backend-a", logWindowStart)
		afterB = countPostsSince("mcp-backend-b", logWindowStart)
		return afterA+afterB-baseA-baseB >= N
	}) {
		t.Fatalf("backend logs did not show %d routed calls within timeout (backend-a delta=%d, backend-b delta=%d)", N, afterA-baseA, afterB-baseB)
	}
	deltaA := afterA - baseA
	deltaB := afterB - baseB
	total := deltaA + deltaB
	threshold := (3 * N) / 2
	t.Logf("backend-a POST delta=%d, backend-b POST delta=%d, total=%d, broadcast threshold=%d (routed expectation ~%d)", deltaA, deltaB, total, threshold, N)

	// Lower-bound: at least one routed call must actually land on
	// a backend. Without this, log-collection failure modes that
	// produce delta=0 would pass the threshold check.
	if total < N {
		t.Fatalf("backends saw only %d POSTs across both deployments. Expected at least %d (one per call). Either log capture is broken or the gateway is short-circuiting before dispatch.", total, N)
	}
	if total > threshold {
		t.Fatalf("tools/call appears to fan out — total POST delta %d exceeds broadcast threshold %d (routed expectation ~%d)", total, threshold, N)
	}
}

// mustUnmarshal parses `body` into `out` or t.Fatalfs.
func mustUnmarshal(t *testing.T, body []byte, out any) {
	t.Helper()
	if err := json.Unmarshal(body, out); err != nil {
		t.Fatalf("json.Unmarshal: %v\nbody: %s", err, body)
	}
}
