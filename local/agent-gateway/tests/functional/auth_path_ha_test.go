// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package functional

import (
	"context"
	"testing"
	"time"

	"github.com/NVIDIA/dsx-exchange/local/agent-gateway/tests/functional/internal/runner"
)

func TestDestructiveGatewayDataplanePodKillNoCallerVisibleFailure(t *testing.T) {
	runner.DestructiveFunctional(t)

	pods := runner.ListPods(t, cscGatewayNS, cscGatewaySelector, "status.phase=Running")
	if len(pods) < 2 {
		t.Fatalf("DSX Agent Gateway has %d Running dataplane Pod(s), want >=2 for HA", len(pods))
	}
	victim := pods[0].Name
	t.Cleanup(func() {
		runner.WaitForDeploymentRolloutsReady(t, cscGatewayNS, cscGatewaySelector, 120*time.Second)
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	tok := runner.MintToken(t, tenantAUnlimited)
	body := []byte(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"auth-path-ha","version":"1"}}}`)

	warm, err := runner.DoPostMCP(t, ctx, tok, body)
	if err != nil {
		t.Fatalf("warm auth-path probe before deleting dataplane Pod: %v", err)
	}
	if warm.Status != 200 {
		t.Fatalf("warm auth-path probe before deleting dataplane Pod returned %d (body: %s)", warm.Status, warm.Body)
	}

	const minPostDeleteProbes = 10
	cs := runner.K8s(t)
	runner.DeletePodNow(t, cscGatewayNS, victim)

	podDeleted := make(chan error, 1)
	go func() {
		podDeleted <- runner.PodDeleted(ctx, cs, cscGatewayNS, victim)
	}()
	select {
	case err := <-podDeleted:
		if err != nil {
			t.Fatalf("victim dataplane Pod %s was not deleted: %v", victim, err)
		}
	case <-ctx.Done():
		t.Fatalf("auth-path HA probe timed out waiting for deleted Pod %s: %v", victim, ctx.Err())
	}
	if err := runner.ReadyEndpointExcludesPod(ctx, cs, cscGatewayNS, cscGatewayName, victim); err != nil {
		t.Fatalf("service endpoints still targeted deleted Pod %s: %v", victim, err)
	}
	var lastConvergeErr error
	var lastConvergeStatus int
	var lastConvergeBody []byte
	if !runner.WaitForContext(ctx, 250*time.Millisecond, func() bool {
		resp, err := runner.DoPostMCP(t, ctx, tok, body)
		lastConvergeErr = err
		lastConvergeStatus = resp.Status
		lastConvergeBody = resp.Body
		return err == nil && resp.Status == 200
	}) {
		t.Fatalf("dataplane path did not return 200 after deleting Pod %s (last err=%v status=%d body=%s)", victim, lastConvergeErr, lastConvergeStatus, lastConvergeBody)
	}

	rolloutReady := make(chan error, 1)
	go func() {
		rolloutReady <- runner.DeploymentRolloutsReady(ctx, cs, cscGatewayNS, cscGatewaySelector)
	}()
	select {
	case err := <-rolloutReady:
		if err != nil {
			t.Fatalf("dataplane rollout did not recover after deleting Pod %s: %v", victim, err)
		}
	case <-ctx.Done():
		t.Fatalf("auth-path HA probe timed out: %v", ctx.Err())
	}
	deadline, _ := ctx.Deadline()
	runner.WaitForReadyEndpointCount(t, cscGatewayNS, cscGatewayName, 2, time.Until(deadline))

	for probe := 1; probe <= minPostDeleteProbes; probe++ {
		var lastErr error
		var lastStatus int
		var lastBody []byte
		var fatal string
		if !runner.WaitForContext(ctx, 250*time.Millisecond, func() bool {
			resp, err := runner.DoPostMCP(t, ctx, tok, body)
			lastErr = err
			lastStatus = resp.Status
			lastBody = resp.Body
			if err != nil {
				return false
			}
			if resp.Status == 200 {
				return true
			}
			fatal = "unexpected status"
			return true
		}) {
			t.Fatalf("post-delete probe %d/%d did not return 200 (last err=%v status=%d body=%s)", probe, minPostDeleteProbes, lastErr, lastStatus, lastBody)
		}
		if fatal != "" {
			t.Fatalf("post-delete probe %d/%d returned %s %d (body: %s)", probe, minPostDeleteProbes, fatal, lastStatus, lastBody)
		}
	}
}
