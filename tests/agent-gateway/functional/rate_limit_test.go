// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

// Rate-limit behavior.
package functional

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/NVIDIA/dsx-exchange/tests/agent-gateway/functional/internal/runner"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

const (
	rateLimitFallbackProbeTimeout = 15 * time.Second
	rateLimitProbeBody            = `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"mcp-backend-a-mcp_headers","arguments":{}}}`
)

// initSession opens a streamable-HTTP MCP session via raw HTTP and
// returns the bearer token + Mcp-Session-Id. The rate-limit tests
// need raw control over follow-up posts (status histograms across
// hundreds of POSTs) so the SDK's session encapsulation is the
// wrong layer here.
func initSession(t *testing.T, ctx context.Context, tenant string) (token, sid string) {
	return initSessionTimeout(t, ctx, tenant, 10*time.Second)
}

// initSessionTimeout is a variant for tests that drive the gateway
// while a backing service (Valkey, RLS) is degraded — under
// `failureMode: FailOpen` the dataplane waits for the rate-limit
// backend deadline before forwarding. The per-request HTTP timeout
// must accommodate that deadline and the response latency.
func initSessionTimeout(t *testing.T, ctx context.Context, tenant string, timeout time.Duration) (token, sid string) {
	t.Helper()
	gw := runner.GatewayURL(t)
	tok := runner.MintToken(t, tenant)

	httpc := &http.Client{Timeout: timeout}
	post := func(body string, sessionID string) (*http.Response, []byte, error) {
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, gw, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+tok)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		if sessionID != "" {
			req.Header.Set("Mcp-Session-Id", sessionID)
		}
		r, err := httpc.Do(req)
		if err != nil {
			return nil, nil, err
		}
		defer r.Body.Close()
		_, b := readAll(t, r)
		return r, b, nil
	}

	var lastStatus int
	var lastBody []byte
	var lastErr error
	retryCtx, retryCancel := context.WithTimeout(ctx, 3*time.Second)
	defer retryCancel()
	initialized := runner.WaitForContext(retryCtx, 500*time.Millisecond, func() bool {
		resp, body, err := post(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"go-runner","version":"1"}}}`, "")
		if err != nil {
			lastErr = err
			return false
		}
		sid = resp.Header.Get("Mcp-Session-Id")
		if sid != "" {
			_, _, _ = post(`{"jsonrpc":"2.0","method":"notifications/initialized"}`, sid)
			return true
		}
		lastStatus = resp.StatusCode
		lastBody = body
		return false
	})
	if initialized {
		return tok, sid
	}
	t.Fatalf("initialize for %s did not return Mcp-Session-Id after retries (last err=%v status=%d body=%s)", tenant, lastErr, lastStatus, lastBody)
	return "", ""
}

// burst issues `n` POSTs to /mcp with the supplied bearer + session
// + extra headers, and returns the status-code histogram.
func burst(t *testing.T, ctx context.Context, n int, bearer, sid string, extra http.Header, body string) map[int]int {
	t.Helper()
	gw := runner.GatewayURL(t)
	// The one-pod-loss path may briefly ride local kind failover latency
	// while kube-proxy and the client connection pool converge. Keep the
	// assertion on response correctness, not a 5s local-machine cutoff.
	httpc := &http.Client{Timeout: 10 * time.Second}
	hist := make(map[int]int)
	var mu sync.Mutex
	var wg sync.WaitGroup
	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req, _ := http.NewRequestWithContext(ctx, http.MethodPost, gw, strings.NewReader(body))
			req.Header.Set("Authorization", "Bearer "+bearer)
			req.Header.Set("Mcp-Session-Id", sid)
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Accept", "application/json, text/event-stream")
			for k, vs := range extra {
				for _, v := range vs {
					req.Header.Add(k, v)
				}
			}
			r, err := httpc.Do(req)
			if err != nil {
				mu.Lock()
				hist[-1]++
				mu.Unlock()
				return
			}
			r.Body.Close()
			mu.Lock()
			hist[r.StatusCode]++
			mu.Unlock()
		}()
	}
	wg.Wait()
	return hist
}

func TestDestructiveFunctionalTenantDoesNotThrottle(t *testing.T) {
	runner.DestructiveFunctional(t)
	runner.CleanupRateLimitState(t)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	tok, sid := initSession(t, ctx, tenantAUnlimited)
	const burstN = 120
	hist := burst(t, ctx, burstN, tok, sid, nil, rateLimitProbeBody)
	t.Logf("%s burst histogram: %v", tenantAUnlimited, hist)
	if hist[-1] > 0 {
		t.Fatalf("%s burst saw %d transport failures (hist %v)", tenantAUnlimited, hist[-1], hist)
	}
	if hist[429] > 0 {
		t.Fatalf("%s burst saw %d HTTP 429s (hist %v). Non-rate-limit tests must not share the low-budget tenant buckets", tenantAUnlimited, hist[429], hist)
	}
	if hist[200] != burstN {
		t.Fatalf("%s burst got %d/%d HTTP 200 responses (hist %v)", tenantAUnlimited, hist[200], burstN, hist)
	}
}

// tenant-a saturating burst observes 429s. The simultaneous tenant-b probe
// stream is reported as a histogram and not asserted on in the default mode.
func TestDestructiveRateLimitPerTenant(t *testing.T) {
	runner.DestructiveFunctional(t)
	runner.CleanupRateLimitState(t)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	tokA, sidA := initSession(t, ctx, "tenant-a")
	tokB, sidB := initSession(t, ctx, "tenant-b")
	runner.FlushAndWaitForRateLimitWindow(t)

	// Burst tenant-a until at least one 429 lands. Up to 200 reqs
	// guarantees the descriptor exhausts at the chart's
	// `tenantRequestsPerSecond=30` budget.
	histA := burst(t, ctx, 200, tokA, sidA, nil, rateLimitProbeBody)
	if histA[429] == 0 {
		t.Fatalf("tenant-a saturating burst saw 0 of %d 429s — rate-limit filter not enforcing (histogram %v)", 200, histA)
	}

	// Tenant-b 30 sequential probes. The default mode records this as
	// supporting evidence.
	hist := burst(t, ctx, 30, tokB, sidB, nil, rateLimitProbeBody)
	t.Logf("tenant-b probe histogram during tenant-a saturation: %v", hist)

	runner.FlushAndWaitForRateLimitWindow(t)
}

// Trust-me headers without a valid bearer must not grant access.
func TestHeaderSmuggling(t *testing.T) {
	runner.ParallelReadOnly(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	gw := runner.GatewayURL(t)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, gw, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`))
	req.Header.Set("X-Forwarded-User", "admin")
	req.Header.Set("X-Remote-User", "tenant-b")
	req.Header.Set("X-Authenticated-User", "tenant-b@local")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	httpc := &http.Client{Timeout: 10 * time.Second}
	r, err := httpc.Do(req)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer r.Body.Close()
	if r.StatusCode != 401 && r.StatusCode != 403 {
		t.Fatalf("trust-me header smuggling — expected 401/403, got %d", r.StatusCode)
	}
}

// Authenticated tenant-a-shaped principal forging caller-supplied identity
// headers must still receive its filtered catalogue.
func TestHeaderSpoofAuthenticated(t *testing.T) {
	runner.ParallelReadOnly(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	gw := runner.GatewayURL(t)
	tok := runner.MintToken(t, tenantAUnlimited)
	httpc := &http.Client{Timeout: 10 * time.Second}

	spoofHeaders := http.Header{
		"X-Authenticated-User": []string{"tenant-b@local"},
		"X-Forwarded-User":     []string{"tenant-b@local"},
		"X-Forwarded-Tenant":   []string{"tenant-b"},
		"X-Remote-User":        []string{"tenant-b"},
		"X-Tenant":             []string{"tenant-b"},
	}
	post := func(body, sid string) (*http.Response, []byte) {
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, gw, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+tok)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		if sid != "" {
			req.Header.Set("Mcp-Session-Id", sid)
		}
		for k, vs := range spoofHeaders {
			for _, v := range vs {
				req.Header.Add(k, v)
			}
		}
		r, err := httpc.Do(req)
		if err != nil {
			t.Fatalf("post: %v", err)
		}
		defer r.Body.Close()
		_, b := readAll(t, r)
		return r, b
	}
	resp, _ := post(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"spoof","version":"1"}}}`, "")
	sid := resp.Header.Get("Mcp-Session-Id")
	if sid == "" {
		t.Fatalf("initialize did not return Mcp-Session-Id under spoof headers")
	}
	post(`{"jsonrpc":"2.0","method":"notifications/initialized"}`, sid)
	_, body := post(`{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`, sid)
	body = lastSSEData(body)

	// Parse names and confirm the tenant-a-shaped principal sees only the
	// configured unprivileged MCP target.
	var page struct {
		Result struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
		} `json:"result"`
	}
	mustUnmarshal(t, body, &page)
	if len(page.Result.Tools) == 0 {
		t.Fatalf("spoofed tools/list returned no tools (body: %s)", body)
	}
	names := make([]string, 0, len(page.Result.Tools))
	for _, tool := range page.Result.Tools {
		names = append(names, tool.Name)
	}
	assertOnlyUnprivilegedLocalToolTarget(t, "tenant-a", names)
}

// tenant-a forging caller-supplied tenant headers on a saturating burst must not
// consume tenant-b's RLS budget. Concurrent tenant-b probes must continue to
// succeed.
func TestDestructiveHeaderSpoofRateLimit(t *testing.T) {
	runner.DestructiveFunctional(t)
	runner.CleanupRateLimitState(t)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	tokA, sidA := initSession(t, ctx, "tenant-a")
	tokB, sidB := initSession(t, ctx, "tenant-b")
	runner.FlushAndWaitForRateLimitWindow(t)

	spoof := http.Header{
		"X-Authenticated-User": []string{"tenant-b@local"},
		"X-Forwarded-User":     []string{"tenant-b@local"},
		"X-Forwarded-Tenant":   []string{"tenant-b"},
		"X-Remote-User":        []string{"tenant-b"},
		"X-Tenant":             []string{"tenant-b"},
	}

	// Spoof burst + victim probes run concurrently. The spoof
	// burst must observe ≥1 429 on tenant-a's own bucket as a
	// positive control — without that, the test could pass on
	// a gateway that drops every spoofed request before RLS.
	const burstN = 120
	const probeN = 20
	var (
		burstMu    sync.Mutex
		burstHist  = make(map[int]int)
		victimMu   sync.Mutex
		victimHist = make(map[int]int)
		gw         = runner.GatewayURL(t)
	)
	httpc := &http.Client{Timeout: 5 * time.Second}
	burstSpoof := func() {
		var local sync.WaitGroup
		for range burstN {
			local.Add(1)
			go func() {
				defer local.Done()
				req, _ := http.NewRequestWithContext(ctx, http.MethodPost, gw, strings.NewReader(rateLimitProbeBody))
				req.Header.Set("Authorization", "Bearer "+tokA)
				req.Header.Set("Mcp-Session-Id", sidA)
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("Accept", "application/json, text/event-stream")
				for k, vs := range spoof {
					for _, v := range vs {
						req.Header.Add(k, v)
					}
				}
				r, err := httpc.Do(req)
				if err != nil {
					burstMu.Lock()
					burstHist[-1]++
					burstMu.Unlock()
					return
				}
				r.Body.Close()
				burstMu.Lock()
				burstHist[r.StatusCode]++
				burstMu.Unlock()
			}()
		}
		local.Wait()
	}
	victimProbes := func() {
		for range probeN {
			req, _ := http.NewRequestWithContext(ctx, http.MethodPost, gw, strings.NewReader(rateLimitProbeBody))
			req.Header.Set("Authorization", "Bearer "+tokB)
			req.Header.Set("Mcp-Session-Id", sidB)
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Accept", "application/json, text/event-stream")
			r, err := httpc.Do(req)
			if err != nil {
				victimMu.Lock()
				victimHist[-1]++
				victimMu.Unlock()
				continue
			}
			r.Body.Close()
			victimMu.Lock()
			victimHist[r.StatusCode]++
			victimMu.Unlock()
		}
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); burstSpoof() }()
	go func() { defer wg.Done(); victimProbes() }()
	wg.Wait()

	t.Logf("spoof burst (tenant-a) histogram: %v", burstHist)
	t.Logf("victim (tenant-b) histogram during spoof burst: %v", victimHist)

	// Positive control: spoof burst MUST hit RLS. tenant-a's own
	// bucket should saturate (>=1 429 from tenant-a). Without this,
	// the test would pass against a gateway that drops requests
	// before RLS ever runs.
	if burstHist[-1] > 0 {
		t.Fatalf("spoof burst saw %d transport failures (hist %v)", burstHist[-1], burstHist)
	}
	if burstHist[429] == 0 {
		t.Fatalf("spoof burst saw 0 429s on tenant-a's own bucket — RLS may not be enforcing on this code path; without that positive control the test cannot prove the spoof was actually rate-limit-counted (burst hist %v)", burstHist)
	}
	// Tenant isolation: spoofed caller-supplied tenant headers must
	// not spend tenant-b's budget. Every victim probe must succeed
	// with HTTP 200 — any 429 means the spoof header crossed into
	// the RLS descriptor, any other status (5xx, transport failure)
	// means tenant-b is degraded for an unrelated reason which is
	// itself a regression.
	for status, count := range victimHist {
		if status != 200 {
			t.Fatalf("victim (tenant-b) saw status=%d count=%d during spoof burst (full hist %v) — expected 20/20 HTTP 200 for tenant isolation", status, count, victimHist)
		}
	}
	if victimHist[200] != probeN {
		t.Fatalf("victim (tenant-b) saw only %d/%d HTTP 200 during spoof burst — isolation regression", victimHist[200], probeN)
	}
}

// readPolicyFailureMode reads the live AgentgatewayPolicy's
// `traffic.rateLimit.global.failureMode` (default FailOpen) and
// fatals on an unrecognized value. Both fail-mode tests share this.
func readPolicyFailureMode(t *testing.T) string {
	t.Helper()
	policy := runner.GetUnstructured(t, runner.AgentgatewayPolicyResource, cscGatewayNS, cscPolicyName)
	mode, _, _ := unstructured.NestedString(policy.Object, "spec", "traffic", "rateLimit", "global", "failureMode")
	if mode == "" {
		mode = "FailOpen"
	}
	if mode != "FailOpen" && mode != "FailClosed" {
		t.Fatalf("AgentgatewayPolicy.spec.traffic.rateLimit.global.failureMode=%q is neither FailOpen nor FailClosed", mode)
	}
	return mode
}

// outageProbeHist drives `n` probes against the gateway with the
// supplied bearer/session, classifies every 200 as a real MCP
// catalogue, and returns the status histogram (`-1` = transport
// failure / client timeout). The per-probe deadline is the http
// client's Timeout.
func outageProbe(t *testing.T, ctx context.Context, httpc *http.Client, bearer, sid string) (int, error) {
	t.Helper()
	gw := runner.GatewayURL(t)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, gw, strings.NewReader(`{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`))
	req.Header.Set("Authorization", "Bearer "+bearer)
	req.Header.Set("Mcp-Session-Id", sid)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	r, err := httpc.Do(req)
	if err != nil {
		return -1, err
	}
	defer r.Body.Close()
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return -1, fmt.Errorf("read response: %w", err)
	}
	if r.StatusCode != http.StatusOK {
		return r.StatusCode, nil
	}

	// Validate every 200 is an actual MCP catalogue. A FailOpen
	// window must still dispatch tools/list to the upstream — a
	// 200 with no `.result.tools` indicates a degenerate response
	// that the assertion must not paper over.
	parsed := lastSSEData(body)
	var page struct {
		Result struct {
			Tools []json.RawMessage `json:"tools"`
		} `json:"result"`
		Error *json.RawMessage `json:"error"`
	}
	if err := json.Unmarshal(parsed, &page); err != nil {
		return r.StatusCode, fmt.Errorf("decode MCP catalogue: %w, body %s", err, parsed)
	}
	if page.Error != nil {
		return r.StatusCode, fmt.Errorf("MCP JSON-RPC error %s, body %s", *page.Error, parsed)
	}
	if len(page.Result.Tools) == 0 {
		return r.StatusCode, fmt.Errorf("empty MCP catalogue, body %s", parsed)
	}
	return r.StatusCode, nil
}

func outageProbeHist(t *testing.T, ctx context.Context, n int, httpc *http.Client, bearer, sid string, parallel bool) map[int]int {
	t.Helper()
	hist := make(map[int]int)
	var probeErrs []error
	var mu sync.Mutex
	probe := func() {
		status, err := outageProbe(t, ctx, httpc, bearer, sid)
		mu.Lock()
		defer mu.Unlock()
		hist[status]++
		if err != nil {
			probeErrs = append(probeErrs, err)
		}
	}
	if !parallel {
		for range n {
			probe()
		}
	} else {
		var wg sync.WaitGroup
		for range n {
			wg.Go(probe)
		}
		wg.Wait()
	}
	if len(probeErrs) > 0 {
		t.Fatalf("outage probes returned invalid responses: %v", probeErrs)
	}
	return hist
}

func requireRateLimitEnforcing(t *testing.T, ctx context.Context, bearer, sid string, n int) map[int]int {
	t.Helper()

	var hist map[int]int
	attempt := 0
	enforceCtx, enforceCancel := context.WithTimeout(ctx, 45*time.Second)
	defer enforceCancel()
	enforcing := runner.WaitForContext(enforceCtx, time.Second, func() bool {
		attempt++
		runner.FlushAndWaitForRateLimitWindow(t)
		hist = burst(t, ctx, n, bearer, sid, nil, rateLimitProbeBody)
		if hist[429] > 0 {
			return true
		}
		t.Logf("pre-outage saturation attempt %d saw 0 429s; waiting for RLS enforcement (hist %v)", attempt, hist)
		return false
	})
	if !enforcing {
		t.Fatalf("pre-outage saturation saw 0 429s before deadline — RLS not enforcing pre-outage (last histogram %v)", hist)
	}
	return hist
}

// TestRateLimitOutage_RLSDown — covers the failure mode `failureMode`
// actually defends against: the rate-limit gRPC service itself
// becomes unreachable (TCP RST / DNS / connect error). FailOpen
// must serve, FailClosed must throttle. Either path returns a
// status fast because agentgateway's gRPC client gets an explicit
// error, not a hang.
func TestDestructiveRateLimitOutage_RLSDown(t *testing.T) {
	runner.DestructiveFunctional(t)
	runner.CleanupRateLimitState(t)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	const ns = cscGatewayNS
	const rlsDeploy = cscGatewayName + "-ratelimit"
	const rlsLabel = "app.kubernetes.io/instance=" + cscGatewayName + ",app.kubernetes.io/component=ratelimit"

	failureMode := readPolicyFailureMode(t)
	t.Logf("AgentgatewayPolicy failureMode: %s", failureMode)

	origReplicas := runner.DeploymentReplicas(t, ns, rlsDeploy)
	if origReplicas == 0 {
		origReplicas = 2
	}
	t.Cleanup(func() {
		runner.ScaleDeployment(t, ns, rlsDeploy, origReplicas)
		runner.WaitForDeploymentRolloutsReady(t, ns, rlsLabel, 120*time.Second)
	})

	runner.FlushAndWaitForRateLimitWindow(t)
	tok, sid := initSession(t, ctx, "tenant-a")

	// Pre-outage positive control: rate-limit must be enforcing now.
	preHist := requireRateLimitEnforcing(t, ctx, tok, sid, 100)
	t.Logf("pre-outage saturation histogram: %v", preHist)

	runner.ScaleDeployment(t, ns, rlsDeploy, 0)
	runner.WaitForPodsGone(t, ns, rlsLabel, 60*time.Second)
	runner.WaitForReadyEndpointCount(t, ns, rlsDeploy, 0, 30*time.Second)

	httpc := &http.Client{Timeout: 5 * time.Second}
	outHist := outageProbeHist(t, ctx, 5, httpc, tok, sid, false)
	t.Logf("RLS-down probe histogram: %v", outHist)

	if outHist[-1] > 0 {
		t.Fatalf("RLS-down probes saw %d transport failures (hist %v) — gateway should not stall when the rate-limit gRPC backend is unreachable; failureMode is supposed to fire on this exact failure", outHist[-1], outHist)
	}
	for status := range outHist {
		if status >= 500 && status < 600 {
			t.Fatalf("RLS-down probe returned %d (hist %v) — neither FailOpen nor FailClosed should produce 5xx on a clean RLS pod outage", status, outHist)
		}
	}
	switch failureMode {
	case "FailOpen":
		if outHist[429] > 0 {
			t.Fatalf("FailOpen: RLS-down probes saw %d 429s (hist %v) — fail-closed semantics under FailOpen policy", outHist[429], outHist)
		}
		if outHist[200] == 0 {
			t.Fatalf("FailOpen: 0 HTTP 200 with valid catalogues (hist %v) — FailOpen contract not certified for RLS-pod-down", outHist)
		}
	case "FailClosed":
		if outHist[200] > 0 {
			t.Fatalf("FailClosed: RLS-down probes saw %d 200s (hist %v) — fail-open semantics under FailClosed policy", outHist[200], outHist)
		}
		if outHist[429] == 0 {
			t.Fatalf("FailClosed: 0 HTTP 429 (hist %v) — FailClosed contract not certified for RLS-pod-down", outHist)
		}
	}
}

func TestDestructiveRLSReplicaLossNoCallerVisibleFailure(t *testing.T) {
	runner.DestructiveFunctional(t)
	runner.CleanupRateLimitState(t)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	const ns = cscGatewayNS
	const rlsDeploy = cscGatewayName + "-ratelimit"
	const rlsLabel = "app.kubernetes.io/instance=" + cscGatewayName + ",app.kubernetes.io/component=ratelimit"

	runner.WaitForDeploymentRolloutsReady(t, ns, rlsLabel, 120*time.Second)
	pods := runner.ListPods(t, ns, rlsLabel, "status.phase=Running")
	if len(pods) < 2 {
		t.Fatalf("RLS has %d Running Pod(s), want >=2 for replica-loss coverage", len(pods))
	}

	tok, sid := initSession(t, ctx, tenantAUnlimited)
	// Full-suite destructive runs can briefly ride local kind failover
	// latency while kube-proxy and the client connection pool converge.
	// Keep the assertion on response correctness, not a client cutoff
	// racing the rate-limit backend deadline.
	httpc := &http.Client{Timeout: rateLimitFallbackProbeTimeout}
	warm := outageProbeHist(t, ctx, 3, httpc, tok, sid, false)
	if warm[200] != 3 {
		t.Fatalf("warm RLS replica-loss probes = %v, want 3 HTTP 200", warm)
	}

	victim := pods[0].Name
	runner.DeletePodNow(t, ns, victim)
	deleteCtx, deleteCancel := context.WithTimeout(ctx, 30*time.Second)
	defer deleteCancel()
	if err := runner.PodDeleted(deleteCtx, runner.K8s(t), ns, victim); err != nil {
		t.Fatalf("deleted RLS Pod %s did not disappear: %v", victim, err)
	}
	if err := runner.ReadyEndpointExcludesPod(deleteCtx, runner.K8s(t), ns, rlsDeploy, victim); err != nil {
		t.Fatalf("deleted RLS Pod %s remained a ready endpoint: %v", victim, err)
	}
	runner.WaitForReadyEndpointCount(t, ns, rlsDeploy, 1, 30*time.Second)
	t.Cleanup(func() {
		runner.WaitForDeploymentRolloutsReady(t, ns, rlsLabel, 120*time.Second)
		runner.WaitForReadyEndpointCount(t, ns, rlsDeploy, 2, 60*time.Second)
	})

	hist := outageProbeHist(t, ctx, 10, httpc, tok, sid, false)
	t.Logf("RLS one-pod-loss probe histogram: %v", hist)
	if hist[-1] > 0 || hist[429] > 0 {
		t.Fatalf("RLS one-pod loss produced transport failure or throttling for unlimited tenant (hist %v)", hist)
	}
	for status := range hist {
		if status >= 500 {
			t.Fatalf("RLS one-pod loss produced caller-visible %d (hist %v)", status, hist)
		}
	}
	if hist[200] != 10 {
		t.Fatalf("RLS one-pod loss returned %d/10 HTTP 200 responses (hist %v)", hist[200], hist)
	}

	runner.WaitForDeploymentRolloutsReady(t, ns, rlsLabel, 120*time.Second)
	runner.FlushAndWaitForRateLimitWindow(t)
	limitedTok, limitedSID := initSession(t, ctx, "tenant-a")
	requireRateLimitEnforcing(t, ctx, limitedTok, limitedSID, 100)
}

// TestRateLimitOutage_ValkeyDown covers counter-store loss. The
// production contract is caller-visible availability: FailOpen
// serves while throttling is temporarily unenforced, FailClosed
// rejects quickly, and neither mode may hang callers.
func TestDestructiveRateLimitOutage_ValkeyDown(t *testing.T) {
	runner.DestructiveFunctional(t)
	runner.CleanupRateLimitState(t)
	const outageProbeCount = 30

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	const ns = cscGatewayNS
	const valkeySTS = cscGatewayName + "-valkey"
	const valkeyLabel = "app.kubernetes.io/instance=" + cscGatewayName + ",app.kubernetes.io/name=valkey"
	if !runner.StatefulSetExists(t, ns, valkeySTS) {
		t.Fatalf("statefulset %s/%s missing — destructive e2e requires valkey.enabled=true", ns, valkeySTS)
	}

	failureMode := readPolicyFailureMode(t)
	t.Logf("AgentgatewayPolicy failureMode: %s", failureMode)

	origReplicas := runner.StatefulSetReplicas(t, ns, valkeySTS)
	if origReplicas == 0 {
		origReplicas = 3
	}
	t.Cleanup(func() {
		runner.ScaleStatefulSet(t, ns, valkeySTS, origReplicas)
		runner.WaitForStatefulSetReady(t, ns, valkeySTS, 120*time.Second)
	})

	runner.FlushAndWaitForRateLimitWindow(t)
	tok, sid := initSession(t, ctx, "tenant-a")

	preHist := requireRateLimitEnforcing(t, ctx, tok, sid, 100)
	t.Logf("pre-outage saturation histogram: %v", preHist)

	runner.ScaleStatefulSet(t, ns, valkeySTS, 0)
	runner.WaitForPodsGone(t, ns, valkeyLabel, 60*time.Second)
	runner.WaitForReadyEndpointCount(t, ns, valkeySTS, 0, 30*time.Second)

	// Full-suite destructive runs can briefly ride local kind failover
	// latency while kube-proxy and the client connection pool converge.
	// Keep the assertion on response correctness, not a 5s local cutoff.
	httpc := &http.Client{Timeout: 10 * time.Second}
	outHist := outageProbeHist(t, ctx, outageProbeCount, httpc, tok, sid, true)
	t.Logf("Valkey-down probe histogram: %v (-1 = client timeout / dataplane stall)", outHist)

	if outHist[-1] > 0 {
		t.Fatalf("Valkey-down probes saw %d transport failures/timeouts (hist %v) — gateway should not stall callers during counter-store loss", outHist[-1], outHist)
	}
	for status := range outHist {
		if status >= 500 && status < 600 {
			t.Fatalf("Valkey-down probe returned %d (hist %v) — counter-store loss should not surface as gateway 5xx", status, outHist)
		}
	}
	switch failureMode {
	case "FailOpen":
		if outHist[429] > 0 {
			t.Fatalf("FailOpen: Valkey-down probes saw %d 429s (hist %v) — throttling should be temporarily unenforced", outHist[429], outHist)
		}
		if outHist[200] != outageProbeCount {
			t.Fatalf("FailOpen: got %d/%d HTTP 200 with valid catalogues (hist %v) — callers must not stall during Valkey-down", outHist[200], outageProbeCount, outHist)
		}
	case "FailClosed":
		if outHist[200] > 0 {
			t.Fatalf("FailClosed: Valkey-down probes saw %d 200s (hist %v) — fail-open semantics under FailClosed policy", outHist[200], outHist)
		}
		if outHist[429] == 0 {
			t.Fatalf("FailClosed: 0 HTTP 429 (hist %v) — FailClosed contract not certified for Valkey-down", outHist)
		}
	}
}

func TestDestructiveValkeyPrimaryLossRecoversRateLimitEnforcement(t *testing.T) {
	runner.DestructiveFunctional(t)
	runner.CleanupRateLimitState(t)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	const ns = cscGatewayNS
	const valkeySTS = cscGatewayName + "-valkey"
	const primaryPod = valkeySTS + "-0"
	if !runner.StatefulSetExists(t, ns, valkeySTS) {
		t.Fatalf("statefulset %s/%s missing — destructive e2e requires valkey.enabled=true", ns, valkeySTS)
	}

	failureMode := readPolicyFailureMode(t)
	t.Logf("AgentgatewayPolicy failureMode: %s", failureMode)

	runner.WaitForStatefulSetReady(t, ns, valkeySTS, 120*time.Second)
	t.Cleanup(func() {
		runner.WaitForStatefulSetReady(t, ns, valkeySTS, 120*time.Second)
	})

	runner.FlushAndWaitForRateLimitWindow(t)
	tok, sid := initSessionTimeout(t, ctx, "tenant-a", rateLimitFallbackProbeTimeout)
	preHist := requireRateLimitEnforcing(t, ctx, tok, sid, 100)
	t.Logf("pre-primary-loss saturation histogram: %v", preHist)
	runner.FlushAndWaitForRateLimitWindow(t)

	runner.DeletePodNow(t, ns, primaryPod)
	deleteCtx, deleteCancel := context.WithTimeout(ctx, 30*time.Second)
	defer deleteCancel()
	if err := runner.PodDeleted(deleteCtx, runner.K8s(t), ns, primaryPod); err != nil {
		t.Fatalf("deleted Valkey primary Pod %s did not disappear: %v", primaryPod, err)
	}

	httpc := &http.Client{Timeout: rateLimitFallbackProbeTimeout}
	outHist := outageProbeHist(t, ctx, 5, httpc, tok, sid, false)
	t.Logf("Valkey primary-loss probe histogram: %v", outHist)
	if outHist[-1] > 0 {
		t.Fatalf("Valkey primary loss caused transport failure/timeout (hist %v)", outHist)
	}
	for status := range outHist {
		if status >= 500 {
			t.Fatalf("Valkey primary loss surfaced caller-visible %d (hist %v)", status, outHist)
		}
	}
	switch failureMode {
	case "FailOpen":
		if outHist[429] > 0 || outHist[200] == 0 {
			t.Fatalf("FailOpen primary-loss histogram = %v, want served requests and no 429s", outHist)
		}
	case "FailClosed":
		if outHist[200] > 0 || outHist[429] == 0 {
			t.Fatalf("FailClosed primary-loss histogram = %v, want quick 429s and no served requests", outHist)
		}
	}

	runner.WaitForStatefulSetReady(t, ns, valkeySTS, 120*time.Second)
	runner.FlushAndWaitForRateLimitWindow(t)
	recoveredTok, recoveredSID := initSession(t, ctx, "tenant-a")
	requireRateLimitEnforcing(t, ctx, recoveredTok, recoveredSID, 100)
}
