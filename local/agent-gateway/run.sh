#!/usr/bin/env bash
# Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
# SPDX-License-Identifier: Apache-2.0

# run.sh — runs functional tests against the gateway NodePort and drives k6.
# Expects the local stack has already been deployed.
set -Eeuo pipefail

REPO_ROOT="$(git rev-parse --show-toplevel)"
cd "$REPO_ROOT"

TEST_ROOT="local/agent-gateway"
ARTIFACTS="$TEST_ROOT/reports"
FUNCTIONAL="$TEST_ROOT/tests/functional"
PERF_RUNNER="$TEST_ROOT/run-perf.sh"
SVC_NS="csc-dsx-agentgateway"
DEFAULT_KUBE_CONTEXT="kind-dsx-exchange"
export KUBE_CONTEXT="${KUBE_CONTEXT:-$DEFAULT_KUBE_CONTEXT}"
GATEWAY_NAME="${GATEWAY_NAME:-csc-dsx-agentgateway}"
VALKEY_STS="${GATEWAY_NAME}-valkey"
GATEWAY_URL="${GATEWAY_URL:-http://localhost:18180/mcp}"
export GATEWAY_URL
export GATEWAY_NS="$SVC_NS"
export GATEWAY_SELECTOR="gateway.networking.k8s.io/gateway-name=$GATEWAY_NAME"

mkdir -p "$ARTIFACTS"

E2E_GO_PARALLEL="${E2E_GO_PARALLEL:-16}"
RUN_DESTRUCTIVE_FUNCTIONAL="${RUN_DESTRUCTIVE_FUNCTIONAL:-0}"
DESTRUCTIVE_TEST_PREFIX="TestDestructive"
# Functional tests should stay quick; destructive tests serialize cluster mutations.
E2E_FUNCTIONAL_TIMEOUT="${E2E_FUNCTIONAL_TIMEOUT:-15m}"
E2E_DESTRUCTIVE_TIMEOUT="${E2E_DESTRUCTIVE_TIMEOUT:-45m}"
log() { printf '\n==> %s\n' "$*"; }

heartbeat_pid=""
start_heartbeat() {
  local label="${1:?heartbeat label required}"
  (
    trap - EXIT
    while sleep 30; do
      printf '... %s still running at %s\n' "$label" "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
    done
  ) &
  heartbeat_pid=$!
}

stop_heartbeat() {
  if [[ -n "$heartbeat_pid" ]]; then
    kill "$heartbeat_pid" >/dev/null 2>&1 || true
    wait "$heartbeat_pid" 2>/dev/null || true
    heartbeat_pid=""
  fi
}

run_functional_phase() {
  local phase_name="${1:?phase name required}"
  local mode="${2:?functional mode required}"
  local parallel="${3:?go test parallelism required}"
  local json_out="${4:?json output required}"
  local stderr_out="${5:?stderr output required}"
  local rc=0

  start_heartbeat "$phase_name"
  if [[ "$mode" == "destructive" ]]; then
    (cd "$FUNCTIONAL" && RUN_DESTRUCTIVE_FUNCTIONAL=1 go test -count=1 -parallel "$parallel" -json -run "^${DESTRUCTIVE_TEST_PREFIX}" ./... -timeout "$E2E_DESTRUCTIVE_TIMEOUT") 2>"$stderr_out" | tee "$json_out" || rc=$?
  else
    (cd "$FUNCTIONAL" && RUN_DESTRUCTIVE_FUNCTIONAL=0 go test -count=1 -parallel "$parallel" -json -run '^Test' -skip "^${DESTRUCTIVE_TEST_PREFIX}" ./... -timeout "$E2E_FUNCTIONAL_TIMEOUT") 2>"$stderr_out" | tee "$json_out" || rc=$?
  fi
  stop_heartbeat

  return "$rc"
}

finish() {
  local rc=$?
  stop_heartbeat
  trap - EXIT
  exit "$rc"
}
trap finish EXIT
# The Gateway resource and test HTTPRoute/ReferenceGrant fixtures are owned
# by Skaffold-managed manifests. This script only exercises the deployed
# dataplane and leaves stack ownership with Skaffold-managed manifests.

log "Wait for gateway dataplane to become routable at $GATEWAY_URL"
deadline=$(( $(date +%s) + 120 ))
while :; do
  code="$(curl -s -o /dev/null -m 2 -w '%{http_code}' -X POST -H 'Content-Type: application/json' -H 'Accept: application/json, text/event-stream' --data '{"jsonrpc":"2.0","id":0,"method":"initialize","params":{}}' "$GATEWAY_URL" 2>/dev/null || true)"
  case "$code" in 401|403|503) break ;; esac
  if (( $(date +%s) >= deadline )); then
    echo "e2e: gateway never became routable (last code: $code)" >&2
    exit 1
  fi
  sleep 1
done
echo "  gateway routable (last code: $code)"

log "Reset rate-limit counters"
if kubectl --context "$KUBE_CONTEXT" -n "$SVC_NS" get statefulset "$VALKEY_STS" >/dev/null 2>&1; then
  valkey_time() {
    kubectl --context "$KUBE_CONTEXT" -n "$SVC_NS" exec "$VALKEY_STS-0" -c "$VALKEY_STS" -- valkey-cli TIME | sed -n '1p'
  }
  kubectl --context "$KUBE_CONTEXT" -n "$SVC_NS" exec "$VALKEY_STS-0" -c "$VALKEY_STS" -- valkey-cli FLUSHALL >/dev/null
  start_sec="$(valkey_time)"
  deadline=$(( $(date +%s) + 3 ))
  while :; do
    now_sec="$(valkey_time)"
    if [[ "$now_sec" != "$start_sec" ]]; then
      break
    fi
    if (( $(date +%s) >= deadline )); then
      echo "e2e: timed out waiting for Valkey rate-limit window to advance after FLUSHALL" >&2
      exit 1
    fi
    sleep 0.05
  done
else
  echo "  Valkey StatefulSet absent; no RLS counters to reset"
fi

log "Functional suite (Go) against $GATEWAY_URL"
SUITE_JSON="$ARTIFACTS/functional-test.json"
SUITE_RC=0
: > "$SUITE_JSON"
FUNCTIONAL_JSON="$ARTIFACTS/functional-test.readonly.json"
FUNCTIONAL_STDERR="$ARTIFACTS/functional-test.readonly.stderr"
DESTRUCTIVE_JSON="$ARTIFACTS/functional-test.destructive.json"
DESTRUCTIVE_STDERR="$ARTIFACTS/functional-test.destructive.stderr"
# `-count=1` disables the on-disk test cache so the gate proves
# current cluster state, not last-run's PASS replay.
run_functional_phase "functional suite" "functional" "$E2E_GO_PARALLEL" "$FUNCTIONAL_JSON" "$FUNCTIONAL_STDERR" || SUITE_RC=$?
cat "$FUNCTIONAL_JSON" >> "$SUITE_JSON"

if [[ "$RUN_DESTRUCTIVE_FUNCTIONAL" == "1" ]]; then
  log "Destructive functional suite (Go) against $GATEWAY_URL"
  run_functional_phase "destructive functional suite" "destructive" "1" "$DESTRUCTIVE_JSON" "$DESTRUCTIVE_STDERR" || SUITE_RC=$?
  cat "$DESTRUCTIVE_JSON" >> "$SUITE_JSON"
else
  echo "  destructive functional suite disabled; set RUN_DESTRUCTIVE_FUNCTIONAL=1 for the full destructive pass"
  : > "$DESTRUCTIVE_JSON"
  : > "$DESTRUCTIVE_STDERR"
fi

# Collate test results from go test -json output.
SUMMARY="$ARTIFACTS/tests-summary.txt"
SKIP_DETAIL="$ARTIFACTS/skip-events.txt"
FAILED_DETAIL="$ARTIFACTS/failed-tests.txt"
: > "$SUMMARY"
: > "$SKIP_DETAIL"
: > "$FAILED_DETAIL"
python3 - "$SUITE_JSON" "$SUMMARY" "$SKIP_DETAIL" "$FAILED_DETAIL" <<'PY' || true
import json, sys
src, out, skip_out, failed_out = sys.argv[1:]
status = {}
# Both top-level tests AND `/`-delimited subtests, so a subtest
# SKIP can't hide behind a parent PASS.
skip_events = []
output_buf = {}
with open(src) as f:
    for line in f:
        line = line.strip()
        if not line:
            continue
        try:
            ev = json.loads(line)
        except Exception:
            continue
        a = ev.get("Action")
        t = ev.get("Test")
        pkg = ev.get("Package", "")
        if not t:
            continue
        key = (pkg, t)
        if a == "output":
            output_buf.setdefault(key, []).append(ev.get("Output", ""))
            continue
        if a == "skip":
            full = "".join(output_buf.get(key, []))
            collapsed = " ".join(full.split())
            skip_events.append((pkg, t, collapsed))
        if "/" in t:
            continue
        if a == "pass":
            status[key] = "PASS"
        elif a == "fail":
            status[key] = "FAIL"
        elif a == "skip":
            status[key] = "SKIP"
with open(out, "w") as o:
    for pkg, name in sorted(status):
        o.write(f"{status[(pkg, name)]} {pkg} {name}\n")
with open(skip_out, "w") as o:
    for pkg, t, r in skip_events:
        r = r.replace("\n", " ").replace("\t", " ").strip()
        o.write(f"{pkg}\t{t}\t{r}\n")
with open(failed_out, "w") as o:
    for pkg, name in sorted(status):
        if status[(pkg, name)] != "FAIL":
            continue
        o.write(f"FAIL {pkg}/{name}\n")
        out_lines = "".join(output_buf.get((pkg, name), [])).splitlines()
        for line in out_lines[-80:]:
            o.write(f"  {line}\n")
PY

count() { grep -c "^$1 " "$SUMMARY" 2>/dev/null || true; }
PASS=$(count PASS); PASS="${PASS:-0}"
FAIL=$(count FAIL); FAIL="${FAIL:-0}"
SKIP=$(count SKIP); SKIP="${SKIP:-0}"
TOTAL=$((PASS + FAIL + SKIP))
read -r FUNCTIONAL_PASS FUNCTIONAL_FAIL FUNCTIONAL_SKIP FUNCTIONAL_TOTAL < <(
  awk -v prefix="$DESTRUCTIVE_TEST_PREFIX" '
    index($3, prefix) == 1 { next }
    $1 == "PASS" { pass++ }
    $1 == "FAIL" { fail++ }
    $1 == "SKIP" { skip++ }
    END { printf "%d %d %d %d\n", pass, fail, skip, pass + fail + skip }
  ' "$SUMMARY"
)
read -r DESTRUCTIVE_PASS DESTRUCTIVE_FAIL DESTRUCTIVE_SKIP DESTRUCTIVE_TOTAL < <(
  awk -v prefix="$DESTRUCTIVE_TEST_PREFIX" '
    index($3, prefix) != 1 { next }
    $1 == "PASS" { pass++ }
    $1 == "FAIL" { fail++ }
    $1 == "SKIP" { skip++ }
    END { printf "%d %d %d %d\n", pass, fail, skip, pass + fail + skip }
  ' "$SUMMARY"
)
log "Functional results: PASS=$FUNCTIONAL_PASS  FAIL=$FUNCTIONAL_FAIL  SKIP=$FUNCTIONAL_SKIP  TOTAL=$FUNCTIONAL_TOTAL"
if [[ "$RUN_DESTRUCTIVE_FUNCTIONAL" == "1" ]]; then
  log "Destructive functional results: PASS=$DESTRUCTIVE_PASS  FAIL=$DESTRUCTIVE_FAIL  SKIP=$DESTRUCTIVE_SKIP  TOTAL=$DESTRUCTIVE_TOTAL"
else
  log "Destructive functional results: not run (set RUN_DESTRUCTIVE_FUNCTIONAL=1)"
fi
if (( FAIL > 0 )); then
  log "Failed functional test detail"
  sed -n '1,240p' "$FAILED_DETAIL"
fi

if (( TOTAL == 0 )); then
	echo "e2e: FAIL — functional matrix is empty" >&2
	exit 1
fi

# Skipped tests are unobserved assertions. The e2e gate
# guarantees every test runs — any SKIP fails the gate.
unexpected_skips=()
while IFS=$'\t' read -r pkg tpath reason; do
  [[ -z "$tpath" ]] && continue
  unexpected_skips+=("$pkg/$tpath: ${reason:0:120}")
done < "$SKIP_DETAIL"
if (( ${#unexpected_skips[@]} > 0 )); then
  echo "e2e: FAIL — tests SKIPPED: ${unexpected_skips[*]}. The e2e gate forbids skips; either fix the test or fix the precondition." >&2
  exit 1
fi
# k6 runs on the host against the same gateway URL the runner
# uses (strip the /mcp suffix; perf scripts append it themselves).
PERF_GATEWAY_URL="${GATEWAY_URL%/mcp}"
rm -f "$ARTIFACTS"/tools-list*.summary.json "$ARTIFACTS"/tools-call*.summary.json \
  "$ARTIFACTS"/tools-list*.ndjson "$ARTIFACTS"/tools-call*.ndjson \
  "$ARTIFACTS"/tools-list*.pod.log "$ARTIFACTS"/tools-call*.pod.log \
  "$ARTIFACTS"/replica-loss-event.txt

log "k6 perf phase A — normal load (imbalance diagnostic)"
PERF_RC=0
start_heartbeat "k6 perf phase A"
bash "$PERF_RUNNER" "$PERF_GATEWAY_URL" || PERF_RC=$?
stop_heartbeat
if (( PERF_RC != 0 )); then
  echo "perf: phase A returned $PERF_RC" >&2
fi

if [[ "$RUN_DESTRUCTIVE_FUNCTIONAL" == "1" ]]; then
  log "k6 perf phase B — replica-loss (p99 threshold under load shed)"
  PERF_RC_B=0
  start_heartbeat "k6 perf phase B replica-loss"
  ARTIFACT_SUFFIX=".replica-loss" REPLICA_LOSS=1 bash "$PERF_RUNNER" "$PERF_GATEWAY_URL" || PERF_RC_B=$?
  stop_heartbeat
  if (( PERF_RC_B != 0 )); then
    echo "perf: phase B (replica-loss) returned $PERF_RC_B" >&2
    PERF_RC=$PERF_RC_B
  fi
else
  echo "  k6 replica-loss phase disabled; set RUN_DESTRUCTIVE_FUNCTIONAL=1 for the full destructive pass"
fi

if (( SUITE_RC != 0 || FAIL != 0 )); then
  log "e2e: FAIL — functional suite failed"
  exit 1
fi
if (( PERF_RC != 0 )); then
  log "e2e: FAIL — perf jobs failed (PERF_RC=$PERF_RC); see $ARTIFACTS/*.summary.json"
  exit 1
fi
log "e2e: OK"
