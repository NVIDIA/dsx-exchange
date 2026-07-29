#!/usr/bin/env bash
# Copyright 2026 NVIDIA Corporation
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.
#
# SPDX-License-Identifier: Apache-2.0

# Run every tests/agent-gateway/perf/*.js as a k6 process on the host against the
# gateway URL the operator deploys against. No in-cluster Jobs —
# load travels the same path production traffic does, including
# kube-proxy / ingress / TLS termination.
#
# Outputs land in tests/agent-gateway/artifacts/<scenario><suffix>.{summary.json,
# ndjson}. k6 exits 99 on threshold breach.
#
# Usage:
#   bash tests/agent-gateway/lib/run-perf.sh <gateway-url-without-/mcp>
set -euo pipefail

REPO_ROOT="$(git rev-parse --show-toplevel)"
cd "$REPO_ROOT"

ARTIFACTS="${ARTIFACTS:-tests/agent-gateway/artifacts}"
TESTS="tests/agent-gateway"
KUBE_CONTEXT="${KUBE_CONTEXT:-kind-dsx-exchange}"
GATEWAY_URL="${1:?gateway URL required (e.g. http://localhost:18180)}"
GATEWAY_NS="${GATEWAY_NS:-csc-dsx-agentgateway}"
GATEWAY_NAME="${GATEWAY_NAME:-csc-dsx-agentgateway}"
GATEWAY_SELECTOR="${GATEWAY_SELECTOR:-gateway.networking.k8s.io/gateway-name=$GATEWAY_NAME}"
ARTIFACT_SUFFIX="${ARTIFACT_SUFFIX:-}"
RUN_PERF_BENCHMARK="${RUN_PERF_BENCHMARK:-0}"
RUN_PERF_PROFILE="${RUN_PERF_PROFILE:-}"
if [[ -z "$RUN_PERF_PROFILE" ]]; then
  if [[ "$RUN_PERF_BENCHMARK" == "1" ]]; then
    RUN_PERF_PROFILE="benchmark"
  else
    RUN_PERF_PROFILE="smoke"
  fi
fi
case "$RUN_PERF_PROFILE" in
  smoke|benchmark) ;;
  *)
    echo "run-perf: RUN_PERF_PROFILE must be smoke or benchmark, got '$RUN_PERF_PROFILE'" >&2
    exit 2
    ;;
esac
if [[ "$RUN_PERF_PROFILE" == "benchmark" ]]; then
  K6_SCENARIO_TIMEOUT_SECONDS="${K6_SCENARIO_TIMEOUT_SECONDS:-180}"
  REPLICA_LOSS_AT_SECONDS="${REPLICA_LOSS_AT_SECONDS:-25}"
else
  K6_SCENARIO_TIMEOUT_SECONDS="${K6_SCENARIO_TIMEOUT_SECONDS:-90}"
  REPLICA_LOSS_AT_SECONDS="${REPLICA_LOSS_AT_SECONDS:-5}"
fi

mkdir -p "$ARTIFACTS"

if ! command -v k6 >/dev/null 2>&1; then
  echo "run-perf: k6 not on PATH. Run mise install --locked." >&2
  exit 2
fi

TOKEN_A="$(cd tests/agent-gateway/functional && go run ./cmd/mint-token tenant-a)"
TOKEN_B="$(cd tests/agent-gateway/functional && go run ./cmd/mint-token tenant-b)"
perf_started_at="$(date -u +%Y-%m-%dT%H:%M:%S)"
echo "==> k6 profile: $RUN_PERF_PROFILE"

# REPLICA_LOSS=1: kill one dataplane Pod mid-run so the k6
# thresholds catch a regression in load-shed absorption. Imbalance
# check below skipped because the killed Pod's request-count drop
# would trip it.
replica_loss_pid=""
if [[ "${REPLICA_LOSS:-0}" == "1" ]]; then
  (
    sleep "$REPLICA_LOSS_AT_SECONDS"
    victim="$(kubectl --context "$KUBE_CONTEXT" -n "$GATEWAY_NS" get pods -l "$GATEWAY_SELECTOR" \
      --field-selector=status.phase=Running \
      -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)"
    if [[ -n "$victim" ]]; then
      delete_at="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
      kubectl --context "$KUBE_CONTEXT" -n "$GATEWAY_NS" delete pod "$victim" --grace-period=0 --force --wait=false >/dev/null 2>&1 || true
      echo "$delete_at $victim" > "$ARTIFACTS/replica-loss-event.txt"
      echo "  replica-loss orchestration: deleted pod/$victim at $delete_at" >&2
    else
      echo "  replica-loss orchestration: SKIP — could not resolve a victim Pod" >&2
    fi
  ) &
  replica_loss_pid=$!
fi

run_k6_scenario() {
  local p="${1:?perf script required}"
  local name summary ndjson k6_rc k6_pid watchdog_pid
  name="$(basename "$p" .js)"
  summary="$ARTIFACTS/${name}${ARTIFACT_SUFFIX}.summary.json"
  ndjson="$ARTIFACTS/${name}${ARTIFACT_SUFFIX}.ndjson"
  rm -f "$summary" "$ndjson"

  echo "  [$name] k6 run $p (against $GATEWAY_URL)"
  k6_rc=0
  GATEWAY_URL="$GATEWAY_URL" \
  TOKEN_A="$TOKEN_A" \
  TOKEN_B="$TOKEN_B" \
  RUN_PERF_PROFILE="$RUN_PERF_PROFILE" \
  RUN_PERF_BENCHMARK="$RUN_PERF_BENCHMARK" \
  RUN_PERF_TARGET_VUS="${RUN_PERF_TARGET_VUS:-}" \
  RUN_PERF_SUCCESS_200_MIN="${RUN_PERF_SUCCESS_200_MIN:-}" \
  RUN_PERF_SESSION_POOL="${RUN_PERF_SESSION_POOL:-}" \
  REPLICA_LOSS="${REPLICA_LOSS:-0}" \
    k6 run --address 127.0.0.1:0 \
      --summary-export="$summary" --out "json=$ndjson" "$p" &
  k6_pid=$!
  (
    sleep "${K6_SCENARIO_TIMEOUT_SECONDS:-180}"
    echo "FAIL: k6 $p timed out after ${K6_SCENARIO_TIMEOUT_SECONDS:-180}s" >&2
    kill "$k6_pid" >/dev/null 2>&1 || true
    sleep 5
    kill -KILL "$k6_pid" >/dev/null 2>&1 || true
  ) &
  watchdog_pid=$!
  wait "$k6_pid" || k6_rc=$?
  kill "$watchdog_pid" >/dev/null 2>&1 || true
  wait "$watchdog_pid" 2>/dev/null || true
  if (( k6_rc != 0 )); then
    echo "FAIL: k6 $p exited $k6_rc — threshold breach (99) or runtime error" >&2
    return "$k6_rc"
  fi
  return 0
}

rc=0
perf_pids=()
perf_names=()
if [[ "${RUN_PERF_SERIAL:-0}" == "1" ]]; then
  echo "==> k6 scenarios serial"
  for p in "$TESTS"/perf/*.js; do
    run_k6_scenario "$p" || rc=1
  done
else
  # tools-list and tools-call use different tenant tokens and write
  # distinct artifacts, so they can share the same k6 phase window.
  # In replica-loss mode the single background Pod deletion lands in
  # both scenarios' windows, proving load-shed behavior without two
  # serialized 75s runs.
  echo "==> k6 scenarios in parallel"
  for p in "$TESTS"/perf/*.js; do
    run_k6_scenario "$p" &
    perf_pids+=("$!")
    perf_names+=("$p")
  done
  for i in "${!perf_pids[@]}"; do
    if wait "${perf_pids[$i]}"; then
      :
    else
      k6_rc=$?
      echo "FAIL: k6 ${perf_names[$i]} exited $k6_rc" >&2
      rc=1
    fi
  done
fi

if [[ -n "$replica_loss_pid" ]]; then
  wait "$replica_loss_pid" 2>/dev/null || true
fi

if [[ "${REPLICA_LOSS:-0}" == "1" ]]; then
  echo "==> Per-replica imbalance check: SKIP (REPLICA_LOSS=1)"
  # Wait for full recovery so the next verify run sees a healthy
  # cluster — otherwise a downstream rollout can race the killed
  # Pod's replacement and cascade.
  echo "==> Waiting for dataplane to recover from replica loss"
  kubectl --context "$KUBE_CONTEXT" -n "$GATEWAY_NS" wait --for=condition=Ready pod \
    -l "$GATEWAY_SELECTOR" \
    --timeout=120s >/dev/null 2>&1 || true
  exit "$rc"
fi

# Per-replica imbalance diagnostic. Count every /mcp access-log entry
# from this perf window and group by Pod. Most requests are expected
# 429s under the test's 30 RPS tenant limit; counting only successful
# MCP method entries turns the diagnostic into a flaky sample of the
# small admitted subset.
#
# Keep this informational on kind NodePort. Kubernetes documents that
# iptables-mode kube-proxy chooses backend Pods at random, and localhost
# NodePort on Docker Desktop has repeatedly produced highly skewed local
# distributions after replica churn. The hard gates remain k6 thresholds,
# p99 regression checks, and the replica-loss run.
echo "==> Per-replica imbalance diagnostic"
imb_artifact="$ARTIFACTS/per-replica-counts.txt"
: > "$imb_artifact"
per_pod_counts="$(kubectl --context "$KUBE_CONTEXT" -n "$GATEWAY_NS" logs -l "$GATEWAY_SELECTOR" \
  --all-pods=true --since-time="${perf_started_at}Z" --tail=-1 2>/dev/null \
  | sed -nE '/"http\.path":"\/mcp"/s|^\[pod/([^/]+)/.*$|\1|p' \
  | sort | uniq -c | awk '{print $1" "$2}')"
echo "$per_pod_counts" > "$imb_artifact"
echo "  per-replica request counts (saved to $imb_artifact):"
echo "$per_pod_counts" | sed 's/^/    /'
count_lines="$(printf '%s\n' "$per_pod_counts" | grep -c '^[0-9]' || true)"
if (( count_lines < 2 )); then
  echo "  per-replica imbalance: SKIP — only $count_lines replica(s) observed serving perf requests"
else
  counts_sorted="$(printf '%s\n' "$per_pod_counts" | awk '{print $1}' | sort -n)"
  median="$(printf '%s\n' "$counts_sorted" | awk -v n="$count_lines" 'NR==int((n+1)/2){print; exit}')"
  # ±20% bound: kind iptables-mode kube-proxy uses statistical
  # randomness, not strict round-robin (10-20% bias documented
  # upstream).
  bad_replicas=()
  while IFS=' ' read -r c pod; do
    [[ -z "$c" || -z "$pod" ]] && continue
    pct="$(awk -v c="$c" -v m="$median" 'BEGIN{d=c-m; if(d<0)d=-d; printf "%d", (d/m)*100}')"
    if (( pct > 20 )); then
      bad_replicas+=("${pod}:count=${c}:median=${median}:delta=${pct}%")
    fi
  done <<< "$per_pod_counts"
  if (( ${#bad_replicas[@]} > 0 )); then
    echo "WARN: per-replica NodePort imbalance exceeded ±20% of median (${count_lines} replicas, median=${median}): ${bad_replicas[*]}" >&2
    echo "  per-replica imbalance: diagnostic only on kind NodePort; k6/p99/replica-loss thresholds remain hard gates"
  else
    echo "  per-replica imbalance: OK (${count_lines} replicas, median=${median})"
  fi
fi

exit "$rc"
