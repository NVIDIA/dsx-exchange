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

// Multi-replica session continuity. Selector-mode upstreams pin
// per-session affinity to a Service-resolved Pod IP; the
// Mcp-Session-Id cookie keeps calls routed to the same upstream
// through the chart-emitted gateway dataplane.
//
//  1. Backend-a has >=2 Ready endpoints (random selection isn't
//     trivially constant).
//  2. Sensitivity: 4 fresh sessions hit >=2 distinct upstream
//     HOSTNAMEs.
//  3. Mint one session through GATEWAY_URL, warm the upstream pin.
//  4. Delete the dataplane Pod that logged the warmed session.
//  5. Replay the same session through GATEWAY_URL after that Pod is
//     gone and assert the pinned upstream HOSTNAME remains stable.
package functional

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/NVIDIA/dsx-exchange/tests/agent-gateway/functional/internal/runner"
)

func TestDestructiveMultiReplicaSession(t *testing.T) {
	runner.DestructiveFunctional(t)

	const (
		backendNS  = "csc-mcp-backends"
		backendSvc = "mcp-backend-a"
	)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	gwPods := runner.ListPods(t, cscGatewayNS, cscGatewaySelector, "status.phase=Running")
	dataplanes := make([]string, 0, len(gwPods))
	for _, p := range gwPods {
		if p.DeletionTimestamp == nil {
			dataplanes = append(dataplanes, p.Name)
		}
	}
	if len(dataplanes) < 2 {
		t.Fatalf("gateway has fewer than 2 Running dataplane Pods (%v); cannot exercise cross-replica session continuity", dataplanes)
	}

	// Upstream Service must have >=2 Ready endpoints.
	slices, err := runner.K8s(t).DiscoveryV1().EndpointSlices(backendNS).List(ctx, metav1.ListOptions{
		LabelSelector: "kubernetes.io/service-name=" + backendSvc,
	})
	if err != nil {
		t.Fatalf("list endpointslices: %v", err)
	}
	ready := 0
	for _, s := range slices.Items {
		for _, ep := range s.Endpoints {
			if ep.Conditions.Ready != nil && *ep.Conditions.Ready {
				ready++
			}
		}
	}
	if ready < 2 {
		t.Fatalf("upstream %s/%s has %d Ready endpoint(s); cross-replica pinning regression cannot be observed", backendNS, backendSvc, ready)
	}

	tok := runner.MintToken(t, tenantAUnlimited)
	httpc := &http.Client{Timeout: 5 * time.Second}
	postGateway := func(sid, body string) (*http.Response, []byte) {
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost,
			runner.GatewayURL(t),
			strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+tok)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		if sid != "" {
			req.Header.Set("Mcp-Session-Id", sid)
		}
		r, err := httpc.Do(req)
		if err != nil {
			t.Fatalf("post: %v", err)
		}
		defer r.Body.Close()
		_, b := readAll(t, r)
		return r, b
	}
	openSession := func() string {
		initBody := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"multi-replica","version":"1"}}}`
		var sid string
		var lastStatus int
		var lastBody []byte
		if !runner.WaitFor(10*time.Second, 250*time.Millisecond, func() bool {
			resp, body := postGateway("", initBody)
			lastStatus = resp.StatusCode
			lastBody = body
			sid = resp.Header.Get("Mcp-Session-Id")
			return sid != ""
		}) {
			t.Fatalf("Pod initialize did not return Mcp-Session-Id after retries (last status=%d body head: %s)", lastStatus, lastBody[:min(len(lastBody), 300)])
		}
		postGateway(sid, `{"jsonrpc":"2.0","method":"notifications/initialized"}`)
		return sid
	}

	extractHN := func(body []byte) string {
		body = lastSSEData(body)
		var resp struct {
			Result struct {
				Content []struct {
					Text string `json:"text"`
				} `json:"content"`
			} `json:"result"`
		}
		if err := json.Unmarshal(body, &resp); err != nil || len(resp.Result.Content) == 0 {
			return ""
		}
		var env map[string]string
		if err := json.Unmarshal([]byte(resp.Result.Content[0].Text), &env); err != nil {
			return ""
		}
		return env["HOSTNAME"]
	}
	loggedGatewayPod := func(sid string, since time.Time) string {
		out := runner.LogsByLabel(t, cscGatewayNS, cscGatewaySelector, time.Since(since)+time.Second)
		needle := `"mcp.session.id":"` + sid + `"`
		var got string
		for _, line := range strings.Split(out, "\n") {
			if !strings.Contains(line, `"mcp.method.name":"tools/call"`) || !strings.Contains(line, needle) {
				continue
			}
			if rest, ok := strings.CutPrefix(line, "[pod/"); ok {
				if pod, _, ok := strings.Cut(rest, "/"); ok {
					got = pod
				}
			}
		}
		return got
	}

	// Sensitivity probe — 4 fresh sessions on Pod 0 should hit ≥2
	// distinct upstream HOSTNAMEs given 2 Ready endpoints.
	seenSens := map[string]struct{}{}
	for range 4 {
		sid := openSession()
		_, body := postGateway(sid, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"mcp-backend-a-mcp_printEnv","arguments":{}}}`)
		if h := extractHN(body); h != "" {
			seenSens[h] = struct{}{}
		}
	}
	if len(seenSens) < 2 {
		t.Logf("WARN: sensitivity probe only saw %d distinct HOSTNAME(s); kube-proxy may be sticky-by-source — pin assertion below has weaker statistical power", len(seenSens))
	}
	// Mint session through the chart-emitted dataplane and capture
	// the upstream HOSTNAME it pins.
	t0 := time.Now().Add(-1 * time.Second)
	sid := openSession()
	_, warmBody := postGateway(sid, `{"jsonrpc":"2.0","id":99,"method":"tools/call","params":{"name":"mcp-backend-a-mcp_printEnv","arguments":{}}}`)
	pinned := extractHN(warmBody)
	if pinned == "" {
		t.Fatalf("warm-up tools/call did not return HOSTNAME — cannot establish pinning baseline (body head: %s)", warmBody[:min(len(warmBody), 300)])
	}
	t.Logf("warm-up pinned upstream HOSTNAME=%s through %s", pinned, runner.GatewayURL(t))
	var firstPod string
	if !runner.WaitFor(10*time.Second, 250*time.Millisecond, func() bool {
		firstPod = loggedGatewayPod(sid, t0)
		return firstPod != ""
	}) {
		t.Fatalf("warm-up tools/call did not produce an access-log entry for session %s", sid[:min(40, len(sid))])
	}

	runner.DeletePodNow(t, cscGatewayNS, firstPod)
	deleteCtx, deleteCancel := context.WithTimeout(ctx, 60*time.Second)
	defer deleteCancel()
	if err := runner.PodDeleted(deleteCtx, runner.K8s(t), cscGatewayNS, firstPod); err != nil {
		t.Fatalf("deleted dataplane Pod %s did not disappear: %v", firstPod, err)
	}
	runner.WaitForDeploymentRolloutsReady(t, cscGatewayNS, cscGatewaySelector, 180*time.Second)
	runner.WaitForNoPodsTerminating(t, cscGatewayNS, cscGatewaySelector, 60*time.Second)

	// Reuse the session through the chart-emitted dataplane after
	// deleting the Pod that first logged it. This keeps the test on
	// the supported Gateway path while forcing a different dataplane
	// replica to decode the same Mcp-Session-Id.
	const samples = 10
	for s := 0; s < samples; s++ {
		_, body := postGateway(sid, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"mcp-backend-a-mcp_printEnv","arguments":{}}}`)
		if strings.Contains(strings.ToLower(string(body)), "session not found") {
			t.Fatalf("sample %d could not decode session-id minted through %s", s, runner.GatewayURL(t))
		}
		h := extractHN(body)
		if h != pinned {
			t.Fatalf("sample %d dispatched to HOSTNAME=%s (expected pinned %s)", s, h, pinned)
		}
	}
}
