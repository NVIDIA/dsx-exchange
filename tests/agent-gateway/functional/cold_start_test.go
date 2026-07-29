// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

// Cold-start CEL target binding. agentgateway evaluates
// `mcp.<category>.target` per-request; an earlier build had a
// cold-vs-warm divergence on the first MCP-CEL request after Pod
// restart. Force a cold dataplane via rolling restart, drive a
// prefixed tools/call, and verify the dataplane access-log records
// `mcp.target` matching the policy-bound target. The access-log
// signal is image-agnostic; we deliberately don't parse the
// upstream's response body.
package functional

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/NVIDIA/dsx-exchange/tests/agent-gateway/functional/internal/runner"
)

func jsonUnmarshal(b []byte, out any) error { return json.Unmarshal(b, out) }

// rolloutAndDrain restarts the dataplane and waits for new
// replicas to be Ready. The Service routes only to ready
// endpoints so the runner's NodePort lands on a fresh dataplane.
func rolloutAndDrain(t *testing.T) {
	t.Helper()
	runner.RolloutRestart(t, cscGatewayNS, cscGatewaySelector)
	runner.WaitForDeploymentRolloutsReady(t, cscGatewayNS, cscGatewaySelector, 180*time.Second)
}

// targetFromAccessLogBySession reads the gateway access log and
// returns the mcp.target of the latest tools/call entry whose
// mcp.session.id matches sid. Session-id correlation pins the
// read to this test's request.
func targetFromAccessLogBySession(t *testing.T, sid string, since time.Time) string {
	t.Helper()
	out := runner.LogsByLabel(t, cscGatewayNS, cscGatewaySelector, time.Since(since)+time.Second)
	needle := `"mcp.session.id":"` + sid + `"`
	var got string
	for _, line := range strings.Split(out, "\n") {
		if !strings.Contains(line, `"mcp.method.name":"tools/call"`) {
			continue
		}
		if !strings.Contains(line, needle) {
			continue
		}
		if _, rest, ok := strings.Cut(line, `"mcp.target":"`); ok {
			if v, _, ok := strings.Cut(rest, `"`); ok {
				got = v
			}
		}
	}
	return got
}

// waitForRolloutToFinishDraining waits until no Pod under the
// gateway selector has a deletionTimestamp.
func waitForRolloutToFinishDraining(t *testing.T) {
	t.Helper()
	runner.WaitForNoPodsTerminating(t, cscGatewayNS, cscGatewaySelector, 60*time.Second)
}

func TestDestructiveTargetBindingColdStart(t *testing.T) {
	runner.DestructiveFunctional(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Cold dataplane: rolling restart + wait for old Pods to
	// finish terminating so the access-log scrape can't see
	// stale entries.
	rolloutAndDrain(t)
	waitForRolloutToFinishDraining(t)

	// Cold-first operator prefixed tools/call. Raw HTTP so we
	// know the session id and can correlate the access-log line
	// to this specific request.
	const coldTarget = "mcp-backend-b-mcp"
	coldTool := coldTarget + "_echo"
	t0 := time.Now().Add(-1 * time.Second)
	tokB, sidB := initSession(t, ctx, "operator")
	callBody := []byte(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"` + coldTool + `","arguments":{"message":"cold-first"}}}`)
	resp := postWithSession(t, ctx, tokB, sidB, callBody)
	if resp.Status != 200 {
		t.Fatalf("cold-first tools/call(%q) returned HTTP %d (body %s) — target binding may have evaluated empty on the very first cold request", coldTool, resp.Status, resp.Body)
	}
	assertCallSucceeded(t, "cold-first "+coldTool, resp.Body)
	// Wait briefly for kubectl logs to surface the just-finished
	// access-log entry. The gateway flushes its access log
	// synchronously on response close, but kubectl's stream can
	// be a half-second behind kubelet's container-log file.
	var coldTarget1 string
	if !runner.WaitFor(10*time.Second, 250*time.Millisecond, func() bool {
		coldTarget1 = targetFromAccessLogBySession(t, sidB, t0)
		return coldTarget1 != ""
	}) {
		t.Fatalf("cold-first %s did not produce a tools/call access-log entry for session %s within 10s — the access-log signal is the load-bearing provenance check", coldTool, sidB[:min(40, len(sidB))])
	}
	if coldTarget1 != coldTarget {
		t.Fatalf("cold-first %s dispatched to mcp.target=%q, expected %q (cross-backend drift?)", coldTool, coldTarget1, coldTarget)
	}

	// Cold list-restart phase: a SECOND rolling restart so
	// tools/list runs as the cold-first MCP-CEL request on its
	// own cold dataplane.
	rolloutAndDrain(t)
	waitForRolloutToFinishDraining(t)

	// CATALOGUE EQUALITY phase. Non-operator tenants see the same
	// configured CSC-local backend-a MCP target. Remote CPC tools are
	// covered by bridge_routing_test.
	listLocalBare := func(tenant string) []string {
		s := runner.NewSession(t, tenant)
		t.Cleanup(s.Close)
		names, err := s.ListToolNames(ctx)
		if err != nil {
			t.Fatalf("%s tools/list: %v", tenant, err)
		}
		bare := make([]string, 0, len(names))
		for _, n := range names {
			if strings.HasPrefix(n, cscBridgeTarget+"_") {
				continue
			}
			bare = append(bare, stripTargetPrefix(n))
		}
		sort.Strings(bare)
		return bare
	}
	gotA := listLocalBare(tenantAUnlimited)
	expectedA := []string{
		"add", "annotatedMessage", "echo", "getResourceLinks",
		"getResourceReference", "getTinyImage", "headers",
		"longRunningOperation", "printEnv", "sampleLLM",
		"structuredContent",
	}
	sort.Strings(expectedA)
	if !equalSlices(gotA, expectedA) {
		t.Fatalf("%s catalogue mismatch: got %v, want %v", tenantAUnlimited, gotA, expectedA)
	}

	gotB := listLocalBare(tenantBUnlimited)
	expectedB := []string{
		"add", "annotatedMessage", "echo", "getResourceLinks",
		"getResourceReference", "getTinyImage", "headers",
		"longRunningOperation", "printEnv", "sampleLLM",
		"structuredContent",
	}
	sort.Strings(expectedB)
	if !equalSlices(gotB, expectedB) {
		t.Fatalf("%s catalogue mismatch: got %v, want %v", tenantBUnlimited, gotB, expectedB)
	}

	// ALLOW/DENY DISPATCH PROBE — second tools/call, also via
	// raw HTTP + session-id correlation.
	t1 := time.Now().Add(-1 * time.Second)
	tokAllow, sidAllow := initSession(t, ctx, "operator")
	allowName := coldTarget + "_echo"
	allowBody := []byte(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"` + allowName + `","arguments":{"message":"allow"}}}`)
	allowResp := postWithSession(t, ctx, tokAllow, sidAllow, allowBody)
	if allowResp.Status != 200 {
		t.Fatalf("allow %s returned HTTP %d (body %s)", allowName, allowResp.Status, allowResp.Body)
	}
	assertCallSucceeded(t, "allow "+allowName, allowResp.Body)
	var allowTarget string
	if !runner.WaitFor(10*time.Second, 250*time.Millisecond, func() bool {
		allowTarget = targetFromAccessLogBySession(t, sidAllow, t1)
		return allowTarget != ""
	}) {
		t.Fatalf("allow %s did not produce a tools/call access-log entry within 10s", allowName)
	}
	if allowTarget != coldTarget {
		t.Fatalf("allow %s dispatched to mcp.target=%q, expected %q", allowName, allowTarget, coldTarget)
	}

	// DENY: a non-operator principal calling operator-only backend-b must be
	// rejected by the authz policy. Deny shape: HTTP 4xx, OR 200
	// with a JSON-RPC error envelope, OR 200 with isError=true.
	// The access log records `mcp.target` from the parsed prefix
	// regardless of dispatch outcome, so it cannot distinguish
	// "non-operator principal reached backend-b" from "non-operator principal
	// requested backend-b but was denied". The HTTP response is the deny
	// signal and is what we assert here.
	tokDeny, sidDeny := initSession(t, ctx, tenantBUnlimited)
	denyName := "mcp-backend-b-mcp_echo"
	denyBody := []byte(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"` + denyName + `","arguments":{"message":"deny"}}}`)
	denyResp := postWithSession(t, ctx, tokDeny, sidDeny, denyBody)
	if denyResp.Status == 200 {
		body := denyResp.Body
		if !strings.Contains(body, `"error"`) && !strings.Contains(body, `"isError":true`) && strings.Contains(body, `"result"`) && strings.Contains(body, `"content"`) {
			t.Fatalf("deny %s returned 200 with non-error content (body %s) — cross-backend leak", denyName, body[:min(200, len(body))])
		}
	}
	_ = sidDeny
}

type rawResp struct {
	Status int
	Body   string
}

// assertCallSucceeded validates that a tools/call response is a
// real success: HTTP 200 with a non-error JSON-RPC result and
// non-empty content. HTTP 200 alone can mask a JSON-RPC error
// envelope.
func assertCallSucceeded(t *testing.T, label, body string) {
	t.Helper()
	parsed := lastSSEData([]byte(body))
	var page struct {
		Result *struct {
			Content []struct {
				Type string `json:"type"`
			} `json:"content"`
			IsError bool `json:"isError"`
		} `json:"result"`
		Error *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := jsonUnmarshal(parsed, &page); err != nil {
		t.Fatalf("%s: response body parse: %v (body %s)", label, err, parsed)
	}
	if page.Error != nil {
		t.Fatalf("%s: tools/call returned JSON-RPC error code=%d msg=%q (body %s)", label, page.Error.Code, page.Error.Message, parsed)
	}
	if page.Result == nil {
		t.Fatalf("%s: tools/call returned 200 but no .result (body %s)", label, parsed)
	}
	if page.Result.IsError {
		t.Fatalf("%s: tools/call returned .result.isError=true (body %s)", label, parsed)
	}
	if len(page.Result.Content) == 0 {
		t.Fatalf("%s: tools/call returned empty .result.content — degenerate response (body %s)", label, parsed)
	}
}

// postWithSession drives a tools/call POST against the gateway
// with a known bearer + Mcp-Session-Id and captures the HTTP
// status + body. The session id correlation downstream binds the
// access-log line to THIS request, so we drive raw HTTP rather
// than the SDK (which encapsulates the session id).
func postWithSession(t *testing.T, ctx context.Context, bearer, sid string, body []byte) rawResp {
	t.Helper()
	gw := runner.GatewayURL(t)
	httpc := &http.Client{Timeout: 30 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, gw, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("build postWithSession request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+bearer)
	req.Header.Set("Mcp-Session-Id", sid)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	r, err := httpc.Do(req)
	if err != nil {
		t.Fatalf("postWithSession: %v", err)
	}
	defer r.Body.Close()
	buf, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatalf("read postWithSession response: %v", err)
	}
	return rawResp{Status: r.StatusCode, Body: string(buf)}
}

func equalSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
