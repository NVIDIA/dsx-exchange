// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

// RLS cross-replica fairness. Drives a known burst as one tenant
// and reads the Valkey counter directly to verify per-tenant
// accounting matches request count, the configured budget fires
// at the 429 boundary, and an under-budget tenant never crosses.
package functional

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/NVIDIA/dsx-exchange/tests/agent-gateway/functional/internal/runner"
)

func TestDestructiveCrossReplicaFairness(t *testing.T) {
	runner.DestructiveFunctional(t)
	runner.CleanupRateLimitState(t)

	const (
		ns        = cscGatewayNS
		valkeySTS = cscGatewayName + "-valkey"
		// Match the rate-limit domain and `tenant` descriptor emitted
		// by the chart's AgentgatewayPolicy.
		keyPrefix = "dsx-agent-gateway_tenant_"
		// Match values.yaml `rateLimit.tenantRequestsPerSecond`.
		budget = 30
	)

	if !runner.StatefulSetExists(t, ns, valkeySTS) {
		t.Fatalf("statefulset %s/%s missing — destructive e2e requires valkey.enabled=true", ns, valkeySTS)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	tokA, sidA := initSession(t, ctx, "tenant-a")
	tokB, sidB := initSession(t, ctx, "tenant-b")

	// Flush the Valkey primary so prior tests' counters don't
	// inflate this test's reads. Sessions are initialized first because those
	// requests use the same tenant buckets measured below.
	runner.FlushAndWaitForRateLimitWindow(t)
	rdb := runner.NewValkeyClient(t)

	// Stage 1: tenant-a saturating burst. Send 60 requests
	// concurrently — twice the per-second budget, so 429s should
	// appear after the first ~30 succeed.
	const overBurst = 60
	stage1Start := time.Now().Unix()
	hist := burst(t, ctx, overBurst, tokA, sidA, nil, rateLimitProbeBody)
	t.Logf("tenant-a burst histogram (n=%d, budget=%d): %v", overBurst, budget, hist)
	if hist[-1] > 0 {
		t.Fatalf("tenant-a saturating burst saw %d transport failures (hist %v)", hist[-1], hist)
	}
	if hist[200] == 0 {
		t.Fatalf("tenant-a saturating burst saw 0 HTTP 200 — gateway not serving? hist %v", hist)
	}
	if hist[429] == 0 {
		t.Fatalf("tenant-a saturating burst saw 0 HTTP 429 — RLS not enforcing the per-tenant budget; cross-replica fairness contract cannot be verified (hist %v)", hist)
	}

	// Stage 2: tenant-b under-budget. Send N=10 sequential
	// requests over ~1 second, comfortably below the 30 RPS
	// budget.
	const underBurst = 10
	httpc := &http.Client{Timeout: 10 * time.Second}
	gw := runner.GatewayURL(t)
	stage2Start := time.Now().Unix()
	histB := make(map[int]int)
	for range underBurst {
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, gw,
			strings.NewReader(rateLimitProbeBody))
		req.Header.Set("Authorization", "Bearer "+tokB)
		req.Header.Set("Mcp-Session-Id", sidB)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		r, err := httpc.Do(req)
		if err != nil {
			histB[-1]++
			continue
		}
		r.Body.Close()
		histB[r.StatusCode]++
	}
	t.Logf("tenant-b under-budget probe histogram (n=%d, budget=%d): %v", underBurst, budget, histB)
	if histB[-1] > 0 {
		t.Fatalf("tenant-b under-budget probe saw %d transport failures (hist %v)", histB[-1], histB)
	}
	if histB[429] > 0 {
		t.Fatalf("tenant-b under-budget probe saw %d 429s — bucket attribution leak from tenant-a (hist %v)", histB[429], histB)
	}

	stage2End := time.Now().Unix()

	// Read Valkey counters by listing keys under the per-tenant
	// prefix and summing every key whose timestamp suffix is in
	// the test's burst window.
	sumCounters := func(tenant string, since, until int64) int64 {
		t.Helper()
		keys, err := rdb.Keys(ctx, keyPrefix+tenant+"_*").Result()
		if err != nil {
			t.Fatalf("valkey KEYS %s%s_*: %v", keyPrefix, tenant, err)
		}
		var total int64
		for _, key := range keys {
			ix := strings.LastIndex(key, "_")
			if ix < 0 {
				continue
			}
			ts, err := strconv.ParseInt(key[ix+1:], 10, 64)
			if err != nil {
				continue
			}
			// counter window = unix-second the request landed in;
			// allow ±1 second of boundary slack.
			if ts < since-1 || ts > until+1 {
				continue
			}
			val, err := rdb.Get(ctx, key).Result()
			if err != nil {
				continue
			}
			n, err := strconv.ParseInt(strings.TrimSpace(val), 10, 64)
			if err != nil {
				continue
			}
			total += n
		}
		return total
	}

	stage1End := stage2Start // tenant-a burst completed before tenant-b started
	var tenantATotal, tenantBTotal int64
	runner.WaitFor(3*time.Second, 100*time.Millisecond, func() bool {
		tenantATotal = sumCounters("tenant-a", stage1Start, stage1End)
		tenantBTotal = sumCounters("tenant-b", stage2Start, stage2End)
		return tenantATotal >= int64(overBurst*8/10) && tenantBTotal >= int64(underBurst*5/10)
	})
	t.Logf("Valkey counters within burst windows: tenant-a=%d (sent=%d), tenant-b=%d (sent=%d)", tenantATotal, overBurst, tenantBTotal, underBurst)

	// envoyproxy/ratelimit increments the counter on every request
	// (both 200s and 429s). Under multi-replica RLS the per-replica
	// flushes can lag and the window-suffix join slips by ±1s.
	// Big-burst tolerance is ±20%; small-burst (10 samples) tolerates
	// ±50% because Poisson noise + window-edge slip dominate.
	if tenantATotal < int64(overBurst*8/10) || tenantATotal > int64(overBurst*12/10) {
		t.Fatalf("tenant-a counter total %d does not match burst size %d within ±20%% — RLS attribution mismatch across replicas", tenantATotal, overBurst)
	}
	if tenantBTotal < int64(underBurst*5/10) || tenantBTotal > int64(underBurst*15/10) {
		t.Fatalf("tenant-b counter total %d does not match probe size %d within ±50%% — RLS attribution mismatch", tenantBTotal, underBurst)
	}

	// 429 boundary contract: the per-second budget is `budget`
	// (30); tenant-a's burst of 60 should see budget hits in
	// HTTP 200 and (overBurst - budget) hits in HTTP 429, ±10
	// for boundary slack (a request that lands at the very
	// start of a fresh 1s window can succeed if the previous
	// window's budget rolled over).
	gotPasses := hist[200]
	gotRejects := hist[429]
	const slack = 10
	if gotPasses < budget-slack || gotPasses > budget+slack {
		t.Fatalf("tenant-a HTTP 200 count %d does not match budget %d within ±%d — 429 boundary did not fire at the configured limit", gotPasses, budget, slack)
	}
	expectRejects := overBurst - budget
	if gotRejects < expectRejects-slack || gotRejects > expectRejects+slack {
		t.Fatalf("tenant-a HTTP 429 count %d does not match expected %d within ±%d — 429 boundary did not fire at the configured limit", gotRejects, expectRejects, slack)
	}

}
