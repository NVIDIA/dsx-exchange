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

package functional

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/NVIDIA/dsx-exchange/tests/agent-gateway/functional/internal/runner"
	"github.com/mark3labs/mcp-go/mcp"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	cscGatewayNS       = "csc-dsx-agentgateway"
	cscGatewayName     = cscGatewayNS
	cscGatewaySelector = "gateway.networking.k8s.io/gateway-name=" + cscGatewayName
	cscBridgeService   = cscGatewayName + "-bridge"
	cscBridgeTarget    = cscBridgeService + "-mcp"
	cscPolicyName      = cscGatewayName + "-authz"
	cscBackendNS       = "csc-mcp-backends"
	cpc1GatewayNS      = "cpc-1-dsx-agentgateway"
	cpc1BackendNS      = "cpc-1-mcp-backends"
	cpc2GatewayNS      = "cpc-2-dsx-agentgateway"
	cpc2BackendNS      = "cpc-2-mcp-backends"
	bridgePodSelector  = "app.kubernetes.io/component=dsx-agentgateway-bridge"

	cscHeaderTool         = "mcp-backend-a-mcp_headers"
	cscOperatorOnlyTool   = "mcp-backend-b-mcp_echo"
	bridgeHeaderTool      = cscBridgeTarget + "_mcp-backend-a-mcp_headers"
	bridgeLongRunningTool = cscBridgeTarget + "_mcp-backend-a-mcp_longRunningOperation"
	bridgeListShardsTool  = cscBridgeTarget + "_dsx_bridge_list_shards"
	bridgePrompt          = cscBridgeTarget + "_mcp-backend-a-mcp_simple_prompt"
)

func TestBridgeListIncludesLocalAndReachableLeafTools(t *testing.T) {
	runner.ParallelReadOnly(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	s := runner.NewSession(t, "operator")
	t.Cleanup(s.Close)

	tools := requireToolsEventually(t, ctx, s, []string{cscHeaderTool, bridgeHeaderTool, bridgeListShardsTool}, "steady bridge list")
	names := toolNames(tools.Tools)
	if findTool(tools.Tools, cscHeaderTool) == nil {
		t.Fatalf("operator tools/list missing local CSC tool %q (saw %v)", cscHeaderTool, names)
	}
	tool := findTool(tools.Tools, bridgeHeaderTool)
	if tool == nil {
		t.Fatalf("operator tools/list missing bridge leaf tool %q (saw %v)", bridgeHeaderTool, names)
	}
	assertShardSelector(t, tool)
	if findTool(tools.Tools, bridgeListShardsTool) == nil {
		t.Fatalf("operator tools/list missing bridge shard listing tool (saw %v)", names)
	}
}

func TestBridgeRequestsEmitSafeInfoCompletionLogs(t *testing.T) {
	runner.ParallelReadOnly(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	since := time.Now()
	s := runner.NewSession(t, "operator")
	t.Cleanup(s.Close)
	requireToolsEventually(t, ctx, s, []string{bridgeHeaderTool}, "bridge request logging")

	var logs string
	found := runner.WaitForContext(ctx, 250*time.Millisecond, func() bool {
		logs = runner.LogsByLabelSinceTime(t, cscGatewayNS, bridgePodSelector, since)
		return strings.Contains(logs, `"msg":"bridge request completed"`) &&
			strings.Contains(logs, `"level":"INFO"`) &&
			strings.Contains(logs, `"method":"POST"`) &&
			strings.Contains(logs, `"status":200`) &&
			strings.Contains(logs, `"duration":`)
	})
	if !found {
		t.Fatalf("bridge logs missing completed info request record:\n%s", logs)
	}
	if strings.Contains(logs, s.Token) {
		t.Fatal("bridge request log contains caller bearer token")
	}
}

func TestBridgeListDoesNotRequireCallerSelectedCluster(t *testing.T) {
	runner.ParallelReadOnly(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	s := runner.NewSession(t, "operator")
	t.Cleanup(s.Close)

	tools := requireToolsEventually(t, ctx, s, []string{bridgeHeaderTool}, "caller-selected cluster removal")
	tool := findTool(tools.Tools, bridgeHeaderTool)
	if tool == nil {
		t.Fatalf("operator tools/list missing %q", bridgeHeaderTool)
	}
	if _, ok := tool.InputSchema.Properties["dsx_cluster_id"]; ok {
		t.Fatalf("%s schema still exposes dsx_cluster_id: %+v", bridgeHeaderTool, tool.InputSchema)
	}
	assertShardSelector(t, tool)
}

func TestBridgeListShardsReturnsReachableShardIDs(t *testing.T) {
	runner.ParallelReadOnly(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	s := runner.NewSession(t, "operator")
	t.Cleanup(s.Close)
	requireToolsEventually(t, ctx, s, []string{bridgeListShardsTool}, "bridge shard list call")

	shards := waitForBridgeShards(t, ctx, s, []string{"cpc-1", "cpc-2"}, "bridge shard list call")
	if !slices.Equal(shards, []string{"cpc-1", "cpc-2"}) {
		t.Fatalf("bridge shards = %v, want [cpc-1 cpc-2]", shards)
	}
}

func TestBridgePromptsForwardThroughOneLeaf(t *testing.T) {
	runner.ParallelReadOnly(t)

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	s := runner.NewSession(t, "operator")
	t.Cleanup(s.Close)

	prompts := requirePromptsEventually(t, ctx, s, []string{bridgePrompt}, "bridge prompt discovery")
	prompt := findPrompt(prompts.Prompts, bridgePrompt)
	if prompt == nil {
		t.Fatalf("operator prompts/list missing %q", bridgePrompt)
	}
	assertPromptHasShardArgument(t, prompt)

	req := mcp.GetPromptRequest{}
	req.Params.Name = bridgePrompt
	req.Params.Arguments = map[string]string{"shard_id": "cpc-1"}
	res, err := s.Client.GetPrompt(ctx, req)
	if err != nil {
		t.Fatalf("prompts/get(%s): %v", bridgePrompt, err)
	}
	assertBridgeBackend(t, promptText(t, res), cpc1BackendNS+"/mcp-backend-a", "prompts/get")
}

func TestBridgeUnauthorizedTenantCannotListRemoteTools(t *testing.T) {
	runner.ParallelReadOnly(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	s := runner.NewSession(t, tenantBUnlimited)
	t.Cleanup(s.Close)

	tools := requireToolsEventually(t, ctx, s, []string{cscHeaderTool}, "unauthorized bridge list")
	names := toolNames(tools.Tools)
	for _, name := range names {
		if strings.HasPrefix(name, cscBridgeTarget+"_") {
			t.Fatalf("%s discovered bridge tool %q (all tools: %v)", tenantBUnlimited, name, names)
		}
	}
}

func TestBridgeCallUsesOneQueueLeaf(t *testing.T) {
	runner.ParallelReadOnly(t)

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	s := runner.NewSession(t, "operator")
	t.Cleanup(s.Close)
	requireToolsEventually(t, ctx, s, []string{bridgeHeaderTool}, "queue-group invocation")

	headers := callBridgeHeaderTool(t, ctx, s, bridgeHeaderTool, "cpc-1", nil)
	if got := headerValue(headers, "x-dsx-backend-id"); got != cpc1BackendNS+"/mcp-backend-a" {
		t.Fatalf("tools/call shard cpc-1 reached %q, want CPC-1 backend", got)
	}
	headers = callBridgeHeaderTool(t, ctx, s, bridgeHeaderTool, "cpc-2", nil)
	if got := headerValue(headers, "x-dsx-backend-id"); got != cpc2BackendNS+"/mcp-backend-a" {
		t.Fatalf("tools/call shard cpc-2 reached %q, want CPC-2 backend", got)
	}
}

func TestBridgeCallRejectsMissingShardID(t *testing.T) {
	runner.ParallelReadOnly(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	s := runner.NewSession(t, "operator")
	t.Cleanup(s.Close)
	requireToolsEventually(t, ctx, s, []string{bridgeHeaderTool}, "missing shard_id rejection")

	req := mcp.CallToolRequest{}
	req.Params.Name = bridgeHeaderTool
	req.Params.Arguments = map[string]any{}
	assertBridgeCallInvalidParams(t, ctx, s, req, "missing shard_id")
}

func TestBridgeCallRejectsInvalidShardID(t *testing.T) {
	runner.ParallelReadOnly(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	s := runner.NewSession(t, "operator")
	t.Cleanup(s.Close)
	requireToolsEventually(t, ctx, s, []string{bridgeHeaderTool}, "invalid shard_id rejection")

	req := mcp.CallToolRequest{}
	req.Params.Name = bridgeHeaderTool
	req.Params.Arguments = map[string]any{"shard_id": "bad prefix"}
	assertBridgeCallInvalidParams(t, ctx, s, req, "invalid shard_id")
}

func TestBridgeCallDoesNotFanOutAcrossLeaves(t *testing.T) {
	runner.ParallelReadOnly(t)

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	s := runner.NewSession(t, "operator")
	t.Cleanup(s.Close)
	requireToolsEventually(t, ctx, s, []string{bridgeHeaderTool}, "fanout marker test")

	marker := fmt.Sprintf("bridge-fanout-%d", time.Now().UnixNano())
	headers := callBridgeHeaderTool(t, ctx, s, bridgeHeaderTool, "cpc-1", http.Header{
		"X-Dsx-Test-Invocation": []string{marker},
	})
	reached := headerValue(headers, "x-dsx-backend-id")
	assertBridgeBackend(t, reached, cpc1BackendNS+"/mcp-backend-a", "tools/call")

	reachedNS := strings.TrimSuffix(reached, "/mcp-backend-a")
	waitForBackendInvocationLog(t, reachedNS, marker)
	for _, ns := range []string{cpc1BackendNS, cpc2BackendNS} {
		if ns == reachedNS {
			continue
		}
		if logs := waitForUnexpectedBackendInvocationLog(t, ns, marker, 3*time.Second); logs != "" {
			t.Fatalf("bridge queue call marker reached %s but also appeared in sibling %s logs: %s", reachedNS, ns, logs)
		}
	}
}

func TestBridgeCallPassesCallerJWT(t *testing.T) {
	runner.ParallelReadOnly(t)

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	s := runner.NewSession(t, "operator")
	t.Cleanup(s.Close)

	headers := callBridgeHeaderTool(t, ctx, s, bridgeHeaderTool, "cpc-1", nil)
	if got := headerValue(headers, "authorization"); got != "Bearer "+s.Token {
		t.Fatalf("%s Authorization passthrough = %q, want caller bearer", bridgeHeaderTool, got)
	}
}

func TestBridgeStreamsSSEBeforeLeafResponseCompletes(t *testing.T) {
	runner.ParallelReadOnly(t)

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	s := runner.NewSession(t, "operator")
	t.Cleanup(s.Close)
	requireToolsEventually(t, ctx, s, []string{bridgeLongRunningTool}, "bridge streaming fixture")

	marker := fmt.Sprintf("bridge-stream-cancel-%d", time.Now().UnixNano())
	_, reader, cancelStream := openBridgeSSE(t, ctx, s, 77, map[string]any{
		"shard_id":           "cpc-1",
		"bridge_stream":      true,
		"block_until_cancel": true,
	}, marker)
	assertBridgeProgressEvent(t, readSSEEvent(t, reader))
	cancelStream()
	waitForBackendLog(t, cpc1BackendNS, "Cancelled MCP request "+marker, 30*time.Second)
}

func TestBridgeStreamsSSEThroughFinalResultAndEOF(t *testing.T) {
	runner.ParallelReadOnly(t)

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	s := runner.NewSession(t, "operator")
	t.Cleanup(s.Close)
	requireToolsEventually(t, ctx, s, []string{bridgeLongRunningTool}, "complete bridge stream fixture")

	_, reader, _ := openBridgeSSE(t, ctx, s, 78, map[string]any{
		"shard_id":      "cpc-1",
		"bridge_stream": true,
	}, "")
	finalEvent := readSSEEvent(t, reader)
	if strings.Contains(finalEvent, `"method":"notifications/progress"`) {
		assertBridgeProgressEvent(t, finalEvent)
		finalEvent = readSSEEvent(t, reader)
	}
	if !strings.Contains(finalEvent, `"id":78`) ||
		!strings.Contains(finalEvent, `"result"`) ||
		!strings.Contains(finalEvent, cpc1BackendNS+"/mcp-backend-a") {
		t.Fatalf("final bridge SSE event = %q, want matching CPC result", finalEvent)
	}
	if _, err := reader.ReadByte(); !errors.Is(err, io.EOF) {
		t.Fatalf("read after final bridge SSE event = %v, want EOF", err)
	}
}

func TestBridgeKeepsIdleSSEOpenAcrossStreamReadTimeout(t *testing.T) {
	runner.ParallelReadOnly(t)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	s := runner.NewSession(t, "operator")
	t.Cleanup(s.Close)
	requireToolsEventually(t, ctx, s, []string{bridgeLongRunningTool}, "idle bridge stream fixture")

	_, reader, _ := openBridgeSSE(t, ctx, s, 79, map[string]any{
		"shard_id":              "cpc-1",
		"bridge_stream":         true,
		"idle_before_result_ms": 12000,
	}, "")
	assertBridgeProgressEvent(t, readSSEEvent(t, reader))
	finalEvent := readSSEEvent(t, reader)
	if !strings.Contains(finalEvent, `"id":79`) || !strings.Contains(finalEvent, `"result"`) {
		t.Fatalf("post-idle bridge SSE event = %q, want final result", finalEvent)
	}
	if _, err := reader.ReadByte(); !errors.Is(err, io.EOF) {
		t.Fatalf("read after post-idle result = %v, want EOF", err)
	}
}

func TestBridgeUnauthorizedTenantCannotInvokeRemoteTool(t *testing.T) {
	runner.ParallelReadOnly(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	s := runner.NewSession(t, tenantBUnlimited)
	t.Cleanup(s.Close)

	req := mcp.CallToolRequest{}
	req.Params.Name = bridgeHeaderTool
	req.Params.Arguments = map[string]any{"shard_id": "cpc-1"}
	assertBridgeCallRejected(t, ctx, s, req)
}

func TestDestructiveBridgeDiscoverySkipsUnavailableLeaves(t *testing.T) {
	runner.DestructiveFunctional(t)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	scaleBridgeLeaf(t, cpc2GatewayNS, 0)
	t.Cleanup(func() { restoreBridgeLeaf(t, cpc2GatewayNS, bridgeHeaderTool) })
	waitForNoBridgeLeafPods(t, cpc2GatewayNS)

	s := runner.NewSession(t, "operator")
	t.Cleanup(s.Close)

	requireToolsEventually(t, ctx, s, []string{bridgeHeaderTool}, "CPC-2 bridge leaf down")
	shards := waitForBridgeShards(t, ctx, s, []string{"cpc-1"}, "CPC-2 bridge leaf down")
	if !slices.Equal(shards, []string{"cpc-1"}) {
		t.Fatalf("bridge shards with CPC-2 leaf down = %v, want [cpc-1]", shards)
	}
}

func TestDestructiveBridgeCallToReachableLeafSurvivesSiblingLeafOutage(t *testing.T) {
	runner.DestructiveFunctional(t)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	scaleBridgeLeaf(t, cpc2GatewayNS, 0)
	t.Cleanup(func() { restoreBridgeLeaf(t, cpc2GatewayNS, bridgeHeaderTool) })
	waitForNoBridgeLeafPods(t, cpc2GatewayNS)

	s := runner.NewSession(t, "operator")
	t.Cleanup(s.Close)

	headers := callBridgeHeaderToolEventually(t, ctx, s, bridgeHeaderTool, "CPC-2 bridge leaf down")
	if got := headerValue(headers, "x-dsx-backend-id"); got != cpc1BackendNS+"/mcp-backend-a" {
		t.Fatalf("bridge call reached %q while CPC-2 bridge was down, want CPC-1 backend", got)
	}
}

func TestDestructiveAgentgatewayListSkipsUnavailableMCPTarget(t *testing.T) {
	runner.DestructiveFunctional(t)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	runner.ScaleDeployment(t, cscBackendNS, "mcp-backend-b", 0)
	t.Cleanup(func() {
		runner.ScaleDeployment(t, cscBackendNS, "mcp-backend-b", 1)
		runner.WaitForDeploymentRolloutsReady(t, cscBackendNS, "app.kubernetes.io/instance=backend-b", 90*time.Second)
	})
	runner.WaitForPodsGone(t, cscBackendNS, "app.kubernetes.io/instance=backend-b", 60*time.Second)

	s := runner.NewSession(t, "operator")
	t.Cleanup(s.Close)

	tools := requireToolsEventually(t, ctx, s, []string{cscHeaderTool, bridgeListShardsTool}, "backend-b target down")
	names := toolNames(tools.Tools)
	if slices.Contains(names, cscOperatorOnlyTool) {
		t.Fatalf("tools/list with backend-b target down still included %q: %v", cscOperatorOnlyTool, names)
	}
}

func TestDestructiveBridgeListReturnsLocalToolsWhenNoLeavesReachable(t *testing.T) {
	runner.DestructiveFunctional(t)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	scaleBridgeLeaf(t, cpc1GatewayNS, 0)
	scaleBridgeLeaf(t, cpc2GatewayNS, 0)
	t.Cleanup(func() {
		scaleBridgeLeaf(t, cpc1GatewayNS, 2)
		scaleBridgeLeaf(t, cpc2GatewayNS, 2)
		waitForBridgeCatalog(t, bridgeHeaderTool)
	})
	waitForNoBridgeLeafPods(t, cpc1GatewayNS)
	waitForNoBridgeLeafPods(t, cpc2GatewayNS)

	s := runner.NewSession(t, "operator")
	t.Cleanup(s.Close)

	names := listToolNamesEventually(t, ctx, s, "all bridge leaves down")
	if !slices.Contains(names, cscHeaderTool) {
		t.Fatalf("tools/list with all bridge leaves down missing local CSC tool %q: %v", cscHeaderTool, names)
	}
	if slices.Contains(names, bridgeHeaderTool) {
		t.Fatalf("tools/list with all bridge leaves down still included %q: %v", bridgeHeaderTool, names)
	}
}

func TestDestructiveBridgeLocalCallWorksWhenNoLeavesReachable(t *testing.T) {
	runner.DestructiveFunctional(t)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	scaleBridgeLeaf(t, cpc1GatewayNS, 0)
	scaleBridgeLeaf(t, cpc2GatewayNS, 0)
	t.Cleanup(func() {
		scaleBridgeLeaf(t, cpc1GatewayNS, 2)
		scaleBridgeLeaf(t, cpc2GatewayNS, 2)
		waitForBridgeCatalog(t, bridgeHeaderTool)
	})
	waitForNoBridgeLeafPods(t, cpc1GatewayNS)
	waitForNoBridgeLeafPods(t, cpc2GatewayNS)

	s := runner.NewSession(t, "operator")
	t.Cleanup(s.Close)

	req := mcp.CallToolRequest{}
	req.Params.Name = cscHeaderTool
	req.Params.Arguments = map[string]any{}
	if _, err := s.Client.CallTool(ctx, req); err != nil {
		t.Fatalf("local CSC tools/call with all bridge leaves down: %v", err)
	}
}

func TestDestructiveBridgeNATSOutageDoesNotBreakLocalTools(t *testing.T) {
	runner.DestructiveFunctional(t)

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	s := runner.NewSession(t, "operator")
	t.Cleanup(s.Close)

	prepareNATSOutage(t)

	names := listToolNamesEventually(t, ctx, s, "NATS outage")
	if !slices.Contains(names, cscHeaderTool) {
		t.Fatalf("tools/list during NATS outage missing local CSC tool %q: %v", cscHeaderTool, names)
	}

	req := mcp.CallToolRequest{}
	req.Params.Name = cscHeaderTool
	req.Params.Arguments = map[string]any{}
	if _, err := s.Client.CallTool(ctx, req); err != nil {
		t.Fatalf("local CSC tools/call during NATS outage: %v", err)
	}
}

func TestDestructiveBridgeNATSOutageReturnsRemoteErrors(t *testing.T) {
	runner.DestructiveFunctional(t)

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	s := runner.NewSession(t, "operator")
	t.Cleanup(s.Close)

	prepareNATSOutage(t)

	names := listToolNamesEventually(t, ctx, s, "NATS outage")
	if slices.Contains(names, bridgeHeaderTool) {
		t.Fatalf("tools/list during NATS outage still included bridge tool %q: %v", bridgeHeaderTool, names)
	}
	assertBridgeLeafUnavailable(t, ctx, s, bridgeHeaderTool)
}

func TestDestructiveBridgeNATSOutageReadinessSplit(t *testing.T) {
	runner.DestructiveFunctional(t)

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	prepareNATSOutage(t)

	for _, tc := range []struct {
		name   string
		ns     string
		labels string
		port   int
	}{
		{name: "hub", ns: cscGatewayNS, labels: bridgePodSelector, port: 3001},
		{name: "cpc-1 leaf", ns: cpc1GatewayNS, labels: bridgePodSelector, port: 3001},
		{name: "cpc-2 leaf", ns: cpc2GatewayNS, labels: bridgePodSelector, port: 3001},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			pod := firstRunningPodName(t, tc.ns, tc.labels)
			podProxyGET(t, ctx, tc.ns, pod, tc.port, "livez")
			requirePodProxyGETFails(t, ctx, tc.ns, pod, tc.port, "readyz")
		})
	}
}

func TestDestructiveBridgeLeafReplicaLossDoesNotBreakInvocation(t *testing.T) {
	runner.DestructiveFunctional(t)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pods := runner.ListPods(t, cpc1GatewayNS, bridgePodSelector, "status.phase=Running")
	if len(pods) < 2 {
		t.Fatalf("CPC-1 bridge leaf has %d Running Pod(s), want >=2", len(pods))
	}
	waitForBridgeCatalog(t, bridgeHeaderTool)

	victim := pods[0].Name
	runner.DeletePodNow(t, cpc1GatewayNS, victim)
	deleteCtx, deleteCancel := context.WithTimeout(ctx, 30*time.Second)
	defer deleteCancel()
	if err := runner.PodDeleted(deleteCtx, runner.K8s(t), cpc1GatewayNS, victim); err != nil {
		t.Fatalf("deleted CPC-1 bridge leaf Pod %s did not disappear: %v", victim, err)
	}
	t.Cleanup(func() {
		runner.WaitForDeploymentRolloutsReady(t, cpc1GatewayNS, bridgePodSelector, 90*time.Second)
	})

	s := runner.NewSession(t, "operator")
	t.Cleanup(s.Close)

	headers := callBridgeHeaderToolEventually(t, ctx, s, bridgeHeaderTool, "one bridge leaf Pod delete")
	assertBridgeBackend(t, headerValue(headers, "x-dsx-backend-id"), cpc1BackendNS+"/mcp-backend-a", "tools/call after leaf replica loss")
}

func TestDestructiveBridgeLeafGracefulDrainCompletesSSE(t *testing.T) {
	runner.DestructiveFunctional(t)

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	t.Cleanup(func() { restoreBridgeLeaf(t, cpc1GatewayNS, bridgeLongRunningTool) })
	scaleBridgeLeaf(t, cpc1GatewayNS, 1)
	runner.WaitForNoPodsTerminating(t, cpc1GatewayNS, bridgePodSelector, 60*time.Second)
	pods := runner.ListPods(t, cpc1GatewayNS, bridgePodSelector, "status.phase=Running")
	if len(pods) != 1 {
		t.Fatalf("CPC-1 bridge leaf has %d Running Pods before graceful drain, want 1", len(pods))
	}
	victim := pods[0].Name

	s := runner.NewSession(t, "operator")
	t.Cleanup(s.Close)
	requireToolsEventually(t, ctx, s, []string{bridgeLongRunningTool}, "graceful bridge stream drain")
	marker := fmt.Sprintf("bridge-stream-drain-%d", time.Now().UnixNano())
	_, reader, _ := openBridgeSSE(t, ctx, s, 80, map[string]any{
		"shard_id":              "cpc-1",
		"bridge_stream":         true,
		"idle_before_result_ms": 6000,
		"idle_marker_ms":        4000,
	}, marker)
	assertBridgeProgressEvent(t, readSSEEvent(t, reader))
	waitForBackendLog(t, cpc1BackendNS, "Idle MCP request "+marker, 15*time.Second)

	deleteStarted := time.Now().Add(-time.Second)
	deleteCtx, cancelDelete := context.WithTimeout(ctx, 20*time.Second)
	defer cancelDelete()
	runner.DeletePodGracefully(t, cpc1GatewayNS, victim)
	if !runner.WaitFor(5*time.Second, 100*time.Millisecond, func() bool {
		logs := runner.BestEffortLogsByLabelSinceTime(t, cpc1GatewayNS, bridgePodSelector, deleteStarted)
		return strings.Contains(logs, "[pod/"+victim+"/dsx-agentgateway-bridge]") && strings.Contains(logs, `"msg":"bridge-leaf shutting down"`)
	}) {
		t.Fatalf("CPC-1 bridge leaf Pod %s did not begin graceful shutdown", victim)
	}

	finalEvent := readSSEEvent(t, reader)
	if !strings.Contains(finalEvent, `"id":80`) || !strings.Contains(finalEvent, `"result"`) {
		t.Fatalf("final bridge SSE event after graceful drain = %q, want result", finalEvent)
	}
	if _, err := reader.ReadByte(); !errors.Is(err, io.EOF) {
		t.Fatalf("read after graceful bridge SSE drain = %v, want EOF", err)
	}
	if err := runner.PodDeleted(deleteCtx, runner.K8s(t), cpc1GatewayNS, victim); err != nil {
		t.Fatalf("gracefully drained CPC-1 bridge leaf Pod %s did not disappear: %v", victim, err)
	}
}

func TestDestructiveBridgeHubReplicaLossDoesNotBreakDiscovery(t *testing.T) {
	runner.DestructiveFunctional(t)

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	runner.WaitForDeploymentRolloutsReady(t, cscGatewayNS, bridgePodSelector, 90*time.Second)
	runner.WaitForReadyEndpointCount(t, cscGatewayNS, cscBridgeService, 2, 30*time.Second)
	pods := runner.ListPods(t, cscGatewayNS, bridgePodSelector, "status.phase=Running")
	if len(pods) < 2 {
		t.Fatalf("CSC bridge hub has %d Running Pod(s), want >=2", len(pods))
	}
	waitForBridgeCatalog(t, bridgeHeaderTool)

	victim := pods[0].Name
	runner.DeletePodNow(t, cscGatewayNS, victim)
	deleteCtx, deleteCancel := context.WithTimeout(ctx, 30*time.Second)
	defer deleteCancel()
	if err := runner.PodDeleted(deleteCtx, runner.K8s(t), cscGatewayNS, victim); err != nil {
		t.Fatalf("deleted CSC bridge hub Pod %s did not disappear: %v", victim, err)
	}
	if err := runner.ReadyEndpointExcludesPod(deleteCtx, runner.K8s(t), cscGatewayNS, cscBridgeService, victim); err != nil {
		t.Fatalf("deleted CSC bridge hub Pod %s remained a ready endpoint: %v", victim, err)
	}
	t.Cleanup(func() {
		runner.WaitForDeploymentRolloutsReady(t, cscGatewayNS, bridgePodSelector, 90*time.Second)
	})

	s := runner.NewSession(t, "operator")
	t.Cleanup(s.Close)
	tools, err := s.Client.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil {
		t.Fatalf("tools/list immediately after bridge hub Pod delete: %v", err)
	}
	names := toolNames(tools.Tools)
	if !slices.Contains(names, cscHeaderTool) {
		t.Fatalf("tools/list immediately after bridge hub Pod delete missing local tool %q: %v", cscHeaderTool, names)
	}
	req := mcp.CallToolRequest{}
	req.Params.Name = cscHeaderTool
	res, err := s.Client.CallTool(ctx, req)
	if err != nil {
		t.Fatalf("local tools/call immediately after bridge hub Pod delete: %v", err)
	}
	if res == nil {
		t.Fatal("local tools/call immediately after bridge hub Pod delete returned nil result")
	}

	runner.WaitForDeploymentRolloutsReady(t, cscGatewayNS, bridgePodSelector, 90*time.Second)
	runner.WaitForReadyEndpointCount(t, cscGatewayNS, cscBridgeService, 2, 30*time.Second)
	requireFreshSessionBridgeCatalogEventually(t, ctx, bridgeHeaderTool)
}

func requireFreshSessionBridgeCatalogEventually(t *testing.T, ctx context.Context, wantTool string) {
	t.Helper()
	var lastNames []string
	if runner.WaitForContext(ctx, 500*time.Millisecond, func() bool {
		s := runner.NewSession(t, "operator")
		defer s.Close()
		tools, err := s.Client.ListTools(ctx, mcp.ListToolsRequest{})
		if err != nil {
			return false
		}
		lastNames = toolNames(tools.Tools)
		return slices.Contains(lastNames, wantTool)
	}) {
		return
	}
	t.Fatalf("fresh session after bridge hub rollout missing %q: %v", wantTool, lastNames)
}

func callBridgeHeaderTool(t *testing.T, ctx context.Context, s *runner.Session, toolName, shardID string, headers http.Header) map[string]string {
	t.Helper()
	req := mcp.CallToolRequest{}
	req.Header = headers
	req.Params.Name = toolName
	req.Params.Arguments = map[string]any{"shard_id": shardID}
	res, err := s.Client.CallTool(ctx, req)
	if err != nil {
		t.Fatalf("tools/call(%s): %v", toolName, err)
	}
	return decodeHeaderEchoResult(t, res)
}

func callBridgeHeaderToolEventually(t *testing.T, ctx context.Context, s *runner.Session, toolName, condition string) map[string]string {
	t.Helper()
	var headers map[string]string
	var lastErr error
	if runner.WaitForContext(ctx, 500*time.Millisecond, func() bool {
		req := mcp.CallToolRequest{}
		req.Params.Name = toolName
		req.Params.Arguments = map[string]any{"shard_id": "cpc-1"}
		res, err := s.Client.CallTool(ctx, req)
		if err != nil {
			lastErr = err
			return false
		}
		headers = decodeHeaderEchoResult(t, res)
		return true
	}) {
		return headers
	}
	t.Fatalf("tools/call(%s) after %s did not recover: %v", toolName, condition, lastErr)
	return nil
}

func waitForBridgeShards(t *testing.T, ctx context.Context, s *runner.Session, want []string, condition string) []string {
	t.Helper()
	var payload struct {
		Shards []string `json:"shards"`
	}
	var lastErr error
	if runner.WaitForContext(ctx, 500*time.Millisecond, func() bool {
		req := mcp.CallToolRequest{}
		req.Params.Name = bridgeListShardsTool
		req.Params.Arguments = map[string]any{}
		res, err := s.Client.CallTool(ctx, req)
		if err != nil {
			lastErr = err
			return false
		}
		if err := json.Unmarshal([]byte(textToolResult(t, res)), &payload); err != nil {
			lastErr = err
			return false
		}
		if slices.Equal(payload.Shards, want) {
			return true
		}
		lastErr = fmt.Errorf("got shards %v, want %v", payload.Shards, want)
		return false
	}) {
		return payload.Shards
	}
	t.Fatalf("tools/call(%s) after %s did not return %v: %v", bridgeListShardsTool, condition, want, lastErr)
	return nil
}

func waitForBackendInvocationLog(t *testing.T, ns, marker string) {
	t.Helper()
	waitForBackendLog(t, ns, marker, 10*time.Second)
}

func waitForBackendLog(t *testing.T, ns, want string, timeout time.Duration) {
	t.Helper()
	if !runner.WaitFor(timeout, 500*time.Millisecond, func() bool {
		return strings.Contains(backendALogsBestEffort(t, ns), want)
	}) {
		t.Fatalf("backend logs in %s did not contain %q", ns, want)
	}
}

func waitForUnexpectedBackendInvocationLog(t *testing.T, ns, marker string, duration time.Duration) string {
	t.Helper()
	var logs string
	if runner.WaitFor(duration, 100*time.Millisecond, func() bool {
		logs = backendALogsBestEffort(t, ns)
		return strings.Contains(logs, marker)
	}) {
		return logs
	}
	return ""
}

func backendALogsBestEffort(t *testing.T, ns string) string {
	t.Helper()
	return runner.BestEffortLogsByLabel(t, ns, "app.kubernetes.io/instance=backend-a", 5*time.Minute)
}

func assertBridgeBackend(t *testing.T, got, want, condition string) {
	t.Helper()
	if got == want || strings.Contains(got, want) {
		return
	}
	t.Fatalf("%s reached backend marker %q, want %q", condition, got, want)
}

func assertBridgeCallInvalidParams(t *testing.T, ctx context.Context, s *runner.Session, req mcp.CallToolRequest, want string) {
	t.Helper()
	res, err := s.Client.CallTool(ctx, req)
	if err != nil {
		if errors.Is(err, mcp.ErrInvalidParams) && strings.Contains(strings.ToLower(err.Error()), strings.ToLower(want)) {
			return
		}
		t.Fatalf("tools/call(%q) error = %v, want invalid params containing %q", req.Params.Name, err, want)
	}
	if res != nil && res.IsError {
		text := strings.ToLower(textToolResult(t, res))
		if strings.Contains(text, strings.ToLower(want)) {
			return
		}
		t.Fatalf("tools/call(%q) result error = %q, want %q", req.Params.Name, text, want)
	}
	t.Fatalf("tools/call(%q) returned non-error result: %+v", req.Params.Name, res)
}

func assertBridgeCallRejected(t *testing.T, ctx context.Context, s *runner.Session, req mcp.CallToolRequest) {
	t.Helper()
	res, err := s.Client.CallTool(ctx, req)
	if err != nil {
		return
	}
	if res != nil && res.IsError {
		return
	}
	t.Fatalf("tools/call(%q) returned non-error result: %+v", req.Params.Name, res)
}

func assertBridgeLeafUnavailable(t *testing.T, ctx context.Context, s *runner.Session, toolName string) {
	t.Helper()
	req := mcp.CallToolRequest{}
	req.Params.Name = toolName
	req.Params.Arguments = map[string]any{"shard_id": "cpc-1"}
	res, err := s.Client.CallTool(ctx, req)
	if err != nil {
		msg := strings.ToLower(err.Error())
		if strings.Contains(msg, "no healthy backends") ||
			strings.Contains(msg, "bridge queue unavailable") ||
			strings.Contains(msg, "bridge shard unavailable") ||
			strings.Contains(msg, "method not found") {
			return
		}
		t.Fatalf("tools/call(%q) returned unexpected outage error: %v", toolName, err)
	}
	if res != nil && res.IsError {
		text := strings.ToLower(textToolResult(t, res))
		if strings.Contains(text, "no healthy backends") || strings.Contains(text, "bridge queue unavailable") || strings.Contains(text, "bridge shard unavailable") {
			return
		}
		t.Fatalf("tools/call(%q) result error = %q, want bridge outage error", toolName, text)
	}
	t.Fatalf("tools/call(%q) during NATS outage returned non-error result: %+v", toolName, res)
}

func scaleBridgeLeaf(t *testing.T, ns string, replicas int32) {
	t.Helper()
	runner.ScaleDeployment(t, ns, ns+"-bridge", replicas)
	if replicas > 0 {
		runner.WaitForDeploymentRolloutsReady(t, ns, bridgePodSelector, 90*time.Second)
	}
}

func restoreBridgeLeaf(t *testing.T, ns string, wantTools ...string) {
	t.Helper()
	scaleBridgeLeaf(t, ns, 2)
	waitForBridgeCatalog(t, wantTools...)
}

func waitForNoBridgeLeafPods(t *testing.T, ns string) {
	t.Helper()
	runner.WaitForPodsGone(t, ns, bridgePodSelector, 60*time.Second)
}

func prepareNATSOutage(t *testing.T) {
	t.Helper()
	name := natsStatefulSetName(t)
	replicas := runner.StatefulSetReplicas(t, "nats", name)
	if replicas == 0 {
		t.Fatalf("NATS StatefulSet nats/%s already has zero replicas", name)
	}
	t.Cleanup(func() { restoreNATS(t, name, replicas) })
	runner.ScaleStatefulSet(t, "nats", name, 0)
	waitForNoNATSPods(t)
}

func restoreNATS(t *testing.T, name string, replicas int32) {
	t.Helper()
	runner.ScaleStatefulSet(t, "nats", name, replicas)
	runner.WaitForStatefulSetReady(t, "nats", name, 90*time.Second)
	for _, ns := range []string{cscGatewayNS, cpc1GatewayNS, cpc2GatewayNS} {
		runner.WaitForDeploymentRolloutsReady(t, ns, bridgePodSelector, 90*time.Second)
	}
	waitForBridgeCatalog(t, bridgeHeaderTool)
}

func natsStatefulSetName(t *testing.T) string {
	t.Helper()
	list, err := runner.K8s(t).AppsV1().StatefulSets("nats").List(context.Background(), metav1.ListOptions{
		LabelSelector: "app.kubernetes.io/instance=nats,app.kubernetes.io/name=nats",
	})
	if err != nil {
		t.Fatalf("list NATS StatefulSets: %v", err)
	}
	if len(list.Items) != 1 {
		names := make([]string, 0, len(list.Items))
		for _, item := range list.Items {
			names = append(names, item.Name)
		}
		t.Fatalf("found NATS StatefulSets %v, want exactly one", names)
	}
	return list.Items[0].Name
}

func waitForNoNATSPods(t *testing.T) {
	t.Helper()
	runner.WaitForPodsGone(t, "nats", "app.kubernetes.io/instance=nats,app.kubernetes.io/name=nats", 60*time.Second)
}

func listToolNamesEventually(t *testing.T, ctx context.Context, s *runner.Session, condition string) []string {
	t.Helper()
	var names []string
	var lastErr error
	if runner.WaitFor(15*time.Second, 500*time.Millisecond, func() bool {
		var err error
		names, err = s.ListToolNames(ctx)
		if err != nil {
			lastErr = err
			return false
		}
		return true
	}) {
		return names
	}
	t.Fatalf("tools/list after %s did not recover: %v", condition, lastErr)
	return nil
}

func waitForBridgeCatalog(t *testing.T, wantTools ...string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	s := runner.NewSession(t, "operator")
	defer s.Close()
	requireToolsEventually(t, ctx, s, wantTools, "bridge restore")
}

func requireToolsEventually(t *testing.T, ctx context.Context, s *runner.Session, want []string, condition string) *mcp.ListToolsResult {
	t.Helper()
	var tools *mcp.ListToolsResult
	var names []string
	var lastErr error
	if runner.WaitForContext(ctx, 500*time.Millisecond, func() bool {
		var err error
		tools, err = s.Client.ListTools(ctx, mcp.ListToolsRequest{})
		if err != nil {
			lastErr = err
			return false
		}
		names = toolNames(tools.Tools)
		for _, name := range want {
			if !slices.Contains(names, name) {
				return false
			}
		}
		return true
	}) {
		return tools
	}
	t.Fatalf("tools/list after %s missing %v (last err=%v, saw %v)", condition, want, lastErr, names)
	return nil
}

func requirePromptsEventually(t *testing.T, ctx context.Context, s *runner.Session, want []string, condition string) *mcp.ListPromptsResult {
	t.Helper()
	var prompts *mcp.ListPromptsResult
	var names []string
	var lastErr error
	if runner.WaitForContext(ctx, 500*time.Millisecond, func() bool {
		var err error
		prompts, err = s.Client.ListPrompts(ctx, mcp.ListPromptsRequest{})
		if err != nil {
			lastErr = err
			return false
		}
		names = promptNames(prompts.Prompts)
		for _, name := range want {
			if !slices.Contains(names, name) {
				return false
			}
		}
		return true
	}) {
		return prompts
	}
	t.Fatalf("prompts/list after %s missing %v (last err=%v, saw %v)", condition, want, lastErr, names)
	return nil
}

func findTool(tools []mcp.Tool, name string) *mcp.Tool {
	for i := range tools {
		if tools[i].Name == name {
			return &tools[i]
		}
	}
	return nil
}

func findPrompt(prompts []mcp.Prompt, name string) *mcp.Prompt {
	for i := range prompts {
		if prompts[i].Name == name {
			return &prompts[i]
		}
	}
	return nil
}

func assertShardSelector(t *testing.T, tool *mcp.Tool) {
	t.Helper()
	if tool == nil {
		t.Fatalf("missing tool for schema assertion")
	}
	if _, ok := tool.InputSchema.Properties["shard_id"]; ok {
		if slices.Contains(tool.InputSchema.Required, "shard_id") {
			return
		}
		t.Fatalf("%s schema includes shard_id but does not require it: %+v", tool.Name, tool.InputSchema)
	}
	t.Fatalf("%s schema missing bridge shard_id selector: %+v", tool.Name, tool.InputSchema)
}

func assertPromptHasShardArgument(t *testing.T, prompt *mcp.Prompt) {
	t.Helper()
	if prompt == nil {
		t.Fatalf("missing prompt for shard argument assertion")
	}
	for _, arg := range prompt.Arguments {
		if arg.Name == "shard_id" && arg.Required {
			return
		}
	}
	t.Fatalf("%s prompt missing required bridge shard_id argument: %+v", prompt.Name, prompt.Arguments)
}

func textToolResult(t *testing.T, res *mcp.CallToolResult) string {
	t.Helper()
	if res == nil {
		t.Fatalf("tool returned nil result")
	}
	for _, item := range res.Content {
		switch v := item.(type) {
		case mcp.TextContent:
			if v.Text != "" {
				return v.Text
			}
		case *mcp.TextContent:
			if v.Text != "" {
				return v.Text
			}
		}
	}
	t.Fatalf("tool returned no text content: %+v", res)
	return ""
}

func promptText(t *testing.T, res *mcp.GetPromptResult) string {
	t.Helper()
	if res == nil {
		t.Fatalf("prompt returned nil result")
	}
	for _, message := range res.Messages {
		switch v := message.Content.(type) {
		case mcp.TextContent:
			if v.Text != "" {
				return v.Text
			}
		case *mcp.TextContent:
			if v.Text != "" {
				return v.Text
			}
		}
	}
	t.Fatalf("prompt returned no text content: %+v", res)
	return ""
}

func readSSEEvent(t *testing.T, reader *bufio.Reader) string {
	t.Helper()
	var event strings.Builder
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("read SSE event: %v", err)
		}
		event.WriteString(line)
		if line == "\n" || line == "\r\n" {
			return event.String()
		}
	}
}

func openBridgeSSE(
	t *testing.T,
	ctx context.Context,
	s *runner.Session,
	id int,
	arguments map[string]any,
	marker string,
) (*http.Response, *bufio.Reader, context.CancelFunc) {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      bridgeLongRunningTool,
			"arguments": arguments,
		},
	})
	if err != nil {
		t.Fatalf("encode bridge stream request: %v", err)
	}
	streamCtx, cancelStream := context.WithCancel(ctx)
	t.Cleanup(cancelStream)
	req, err := http.NewRequestWithContext(streamCtx, http.MethodPost, runner.GatewayURL(t), bytes.NewReader(body))
	if err != nil {
		t.Fatalf("build bridge stream request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+s.Token)
	req.Header.Set("Mcp-Session-Id", s.Client.GetSessionId())
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if marker != "" {
		req.Header.Set("X-Dsx-Test-Invocation", marker)
	}
	client := &http.Client{Transport: &http.Transport{DisableKeepAlives: true}}
	t.Cleanup(client.CloseIdleConnections)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("POST bridge streaming call: %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("bridge streaming status = %d, want 200", resp.StatusCode)
	}
	if got := resp.Header.Get("Content-Type"); !strings.HasPrefix(got, "text/event-stream") {
		t.Fatalf("bridge streaming Content-Type = %q, want text/event-stream", got)
	}
	return resp, bufio.NewReader(resp.Body), cancelStream
}

func assertBridgeProgressEvent(t *testing.T, event string) {
	t.Helper()
	if !strings.Contains(event, `"method":"notifications/progress"`) ||
		!strings.Contains(event, "bridge-stream-fixture") ||
		!strings.Contains(event, cpc1BackendNS+"/mcp-backend-a") {
		t.Fatalf("first bridge SSE event = %q, want CPC progress notification", event)
	}
}

func toolNames(tools []mcp.Tool) []string {
	out := make([]string, 0, len(tools))
	for _, tool := range tools {
		out = append(out, tool.Name)
	}
	sort.Strings(out)
	return out
}

func promptNames(prompts []mcp.Prompt) []string {
	out := make([]string, 0, len(prompts))
	for _, prompt := range prompts {
		out = append(out, prompt.Name)
	}
	sort.Strings(out)
	return out
}
