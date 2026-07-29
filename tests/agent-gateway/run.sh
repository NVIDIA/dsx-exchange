#!/usr/bin/env bash
# Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
# SPDX-License-Identifier: Apache-2.0

# run.sh — runs functional tests against the gateway NodePort, drives k6
# perf, and regenerates tests/agent-gateway/artifacts/results.md.
# Expects the local stack has already been deployed.
set -Eeuo pipefail

REPO_ROOT="$(git rev-parse --show-toplevel)"
cd "$REPO_ROOT"

ARTIFACTS="tests/agent-gateway/artifacts"
LIB="tests/agent-gateway/lib"
PHASE_TIMINGS_FILE="$ARTIFACTS/phase-timings.tsv"
PHASE_TIMINGS_RUN_ID_FILE="$ARTIFACTS/phase-timings.run-id"
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
RUN_PERF_PROFILE_EFFECTIVE="${RUN_PERF_PROFILE:-}"
if [[ -z "$RUN_PERF_PROFILE_EFFECTIVE" ]]; then
  if [[ "${RUN_PERF_BENCHMARK:-0}" == "1" ]]; then
    RUN_PERF_PROFILE_EFFECTIVE="benchmark"
  else
    RUN_PERF_PROFILE_EFFECTIVE="smoke"
  fi
fi

log() { printf '\n==> %s\n' "$*"; }

# shellcheck disable=SC1091
source "$LIB/phase-timing.sh"

PHASE_TIMINGS_RUN_ID="$(date -u +%Y%m%dT%H%M%SZ)-$$"
printf '%s\n' "$PHASE_TIMINGS_RUN_ID" > "$PHASE_TIMINGS_RUN_ID_FILE"
export PHASE_TIMINGS_RUN_ID
phase_timing_init "agent-gateway-e2e" append

phase_timing_prune_script_rows "agent-gateway-e2e"
trap phase_err_trap ERR

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
    (cd tests/agent-gateway/functional && RUN_DESTRUCTIVE_FUNCTIONAL=1 go test -count=1 -parallel "$parallel" -json -run "^${DESTRUCTIVE_TEST_PREFIX}" ./... -timeout "$E2E_DESTRUCTIVE_TIMEOUT") > "$json_out" 2>"$stderr_out" || rc=$?
  else
    (cd tests/agent-gateway/functional && RUN_DESTRUCTIVE_FUNCTIONAL=0 go test -count=1 -parallel "$parallel" -json -run '^Test' -skip "^${DESTRUCTIVE_TEST_PREFIX}" ./... -timeout "$E2E_FUNCTIONAL_TIMEOUT") > "$json_out" 2>"$stderr_out" || rc=$?
  fi
  stop_heartbeat

  return "$rc"
}

finish() {
  local rc=$?
  if (( rc == 0 )); then
    phase_timing_finish OK || true
  else
    phase_timing_finish FAIL || true
  fi
  stop_heartbeat
  trap - EXIT
  exit "$rc"
}
trap finish EXIT
# The Gateway resource and test HTTPRoute/ReferenceGrant fixtures are owned
# by Skaffold-managed manifests. This script only exercises the deployed
# dataplane and leaves stack ownership with Skaffold-managed manifests.

log "Wait for gateway dataplane to become routable at $GATEWAY_URL"
phase_start "e2e: wait for gateway dataplane"
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
phase_end "e2e: wait for gateway dataplane"

log "Reset rate-limit counters"
phase_start "e2e: reset rate-limit counters"
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
phase_end "e2e: reset rate-limit counters"

log "Functional suite (Go) against $GATEWAY_URL"
phase_start "e2e: functional suite"
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
if (( SUITE_RC != 0 )); then
  phase_end "e2e: functional suite" "FAIL"
else
  phase_end "e2e: functional suite"
fi

DESTRUCTIVE_RC=0
if [[ "$RUN_DESTRUCTIVE_FUNCTIONAL" == "1" ]]; then
  log "Destructive functional suite (Go) against $GATEWAY_URL"
  phase_start "e2e: destructive functional suite"
  run_functional_phase "destructive functional suite" "destructive" "1" "$DESTRUCTIVE_JSON" "$DESTRUCTIVE_STDERR" || DESTRUCTIVE_RC=$?
  cat "$DESTRUCTIVE_JSON" >> "$SUITE_JSON"
  if (( DESTRUCTIVE_RC != 0 )); then
    phase_end "e2e: destructive functional suite" "FAIL"
  else
    phase_end "e2e: destructive functional suite"
  fi
else
  echo "  destructive functional suite disabled; set RUN_DESTRUCTIVE_FUNCTIONAL=1 for the full destructive pass"
  : > "$DESTRUCTIVE_JSON"
  : > "$DESTRUCTIVE_STDERR"
fi
if (( DESTRUCTIVE_RC != 0 && SUITE_RC == 0 )); then
  SUITE_RC="$DESTRUCTIVE_RC"
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
src, out, skip_out, failed_out = sys.argv[1], sys.argv[2], sys.argv[3], sys.argv[4]
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
  "$ARTIFACTS"/tools-list*.pod.log "$ARTIFACTS"/tools-call*.pod.log

log "k6 perf phase A — normal load (imbalance diagnostic)"
phase_start "e2e: k6 perf phase A"
PERF_RC=0
start_heartbeat "k6 perf phase A"
bash "$LIB/run-perf.sh" "$PERF_GATEWAY_URL" || PERF_RC=$?
stop_heartbeat
if (( PERF_RC != 0 )); then
  echo "perf: phase A returned $PERF_RC; report regen continues so the failure is recorded" >&2
  phase_end "e2e: k6 perf phase A" "FAIL"
else
  phase_end "e2e: k6 perf phase A"
fi

if [[ "$RUN_DESTRUCTIVE_FUNCTIONAL" == "1" ]]; then
  log "k6 perf phase B — replica-loss (p99 threshold under load shed)"
  phase_start "e2e: k6 perf phase B replica-loss"
  PERF_RC_B=0
  start_heartbeat "k6 perf phase B replica-loss"
  ARTIFACT_SUFFIX=".replica-loss" REPLICA_LOSS=1 bash "$LIB/run-perf.sh" "$PERF_GATEWAY_URL" || PERF_RC_B=$?
  stop_heartbeat
  if (( PERF_RC_B != 0 )); then
    echo "perf: phase B (replica-loss) returned $PERF_RC_B" >&2
    PERF_RC=$PERF_RC_B
    phase_end "e2e: k6 perf phase B replica-loss" "FAIL"
  else
    phase_end "e2e: k6 perf phase B replica-loss"
  fi
else
  echo "  k6 replica-loss phase disabled; set RUN_DESTRUCTIVE_FUNCTIONAL=1 for the full destructive pass"
fi

log "Generate results.md"
phase_start "e2e: generate results.md"
agw_version="$(awk '/- name: agentgateway/{f=1} f && /version:/{gsub(/"/, "", $2); print $2; exit}' deploy/dsx-agent-gateway/Chart.yaml 2>/dev/null)"
: "${agw_version:=unknown}"
verdict="**Verdict:** functional ${FUNCTIONAL_FAIL} fail / ${FUNCTIONAL_PASS} pass / ${FUNCTIONAL_SKIP} skip across ${FUNCTIONAL_TOTAL} tests."
if [[ "$RUN_DESTRUCTIVE_FUNCTIONAL" == "1" ]]; then
  verdict="${verdict} Destructive functional ${DESTRUCTIVE_FAIL} fail / ${DESTRUCTIVE_PASS} pass / ${DESTRUCTIVE_SKIP} skip across ${DESTRUCTIVE_TOTAL} tests."
else
  verdict="${verdict} Destructive functional tests were not run; set \`RUN_DESTRUCTIVE_FUNCTIONAL=1\` for the full destructive pass."
fi

cat > "$ARTIFACTS/results.md" <<EOF
# Validation — DSX Agent Gateway (agentgateway)

**Generated:** $(date -u +%Y-%m-%dT%H:%M:%SZ)
**Gateway:** agentgateway $agw_version
**Runner:** Go functional tests targeting NodePort \`$GATEWAY_URL\`
**Go parallelism:** functional=${E2E_GO_PARALLEL}, destructive=1

$verdict

## Functional matrix

| Phase | Test | Status |
|---|---|---|
EOF
while IFS= read -r line; do
  read -r status pkg name <<< "$line"
  phase="functional"
  if [[ "$name" == "$DESTRUCTIVE_TEST_PREFIX"* ]]; then
    phase="destructive functional"
  fi
  printf '| %s | %s/%s | %s |\n' "$phase" "$pkg" "$name" "$status"
done < "$SUMMARY" >> "$ARTIFACTS/results.md"

render_perf_table_files() {
  local table_label="$1"; shift
  cat >> "$ARTIFACTS/results.md" <<HEADER

## Performance — $table_label

| Scenario | VUs | RPS (ok) | count (ok) | p50 | p95 | p99 | max |
|---|---|---|---|---|---|---|---|
HEADER
  local any=0
  local f
  for f in "$@"; do
    [[ -s "$f" ]] || continue
    any=1
    local scenario
    scenario="$(basename "$f" .summary.json)"
    local vu_max rps_ok cnt_ok p50 p95 p99 maxv
    # `success_200.rate` is the per-second rate over the run window
    # (~26-30 RPS at the chart's tenantRequestsPerSecond=30 limit);
    # success_200.count is the absolute count, reported alongside
    # the rate so the throughput floor (count >= 900) is readable.
    read -r vu_max rps_ok cnt_ok p50 p95 p99 maxv < <(jq -r '
      [
        ((.metrics.vus_max.values.max // .metrics.vus_max.max // 0) | tostring),
        ((.metrics.success_200.values.rate // .metrics.success_200.rate // 0) | tostring),
        ((.metrics.success_200.values.count // .metrics.success_200.count // 0) | tostring),
        ((.metrics.http_req_duration."p(50)" // .metrics.http_req_duration.med // 0) | tostring),
        ((.metrics.http_req_duration."p(95)" // 0) | tostring),
        ((.metrics.http_req_duration."p(99)" // 0) | tostring),
        ((.metrics.http_req_duration.max // 0) | tostring)
      ] | @tsv' "$f" 2>/dev/null | tr '\t' ' ')
    : "${vu_max:=0}" "${rps_ok:=0}" "${cnt_ok:=0}" "${p50:=0}" "${p95:=0}" "${p99:=0}" "${maxv:=0}"
    printf '| %s | %s | %.1f | %s | %.0f | %.0f | %.0f | %.0f |\n' \
      "$scenario" "$vu_max" "$rps_ok" "$cnt_ok" "$p50" "$p95" "$p99" "$maxv" >> "$ARTIFACTS/results.md"
  done
  if (( any == 0 )); then
    printf '| (no summary.json captured) | — | — | — | — | — | — | — |\n' >> "$ARTIFACTS/results.md"
  fi
}
results_perf_profile="$RUN_PERF_PROFILE_EFFECTIVE"
if [[ "$results_perf_profile" == "benchmark" ]]; then
  results_replica_loss_at="${REPLICA_LOSS_AT_SECONDS:-25}"
  results_success_floor="${RUN_PERF_SUCCESS_200_MIN:-900}"
  results_window="10s ramp, 60s hold, 5s ramp-down at 100 VUs"
else
  results_replica_loss_at="${REPLICA_LOSS_AT_SECONDS:-5}"
  results_success_floor="${RUN_PERF_SUCCESS_200_MIN:-30}"
  results_window="2s ramp, 8s hold, 2s ramp-down at 20 VUs"
fi
render_perf_table_files "Steady-state (${results_perf_profile}, gateway.replicaCount=3)" \
  "$ARTIFACTS/tools-list.summary.json" "$ARTIFACTS/tools-call.summary.json"
render_perf_table_files "Replica-loss (${results_perf_profile}, one Pod deleted at t=${results_replica_loss_at}s)" \
  "$ARTIFACTS/tools-list.replica-loss.summary.json" "$ARTIFACTS/tools-call.replica-loss.summary.json"
phase_end "e2e: generate results.md"

phase_timing_prune_other_runs
timing_table="$ARTIFACTS/stage-timings.md"
python3 - "$PHASE_TIMINGS_FILE" "$PHASE_TIMINGS_RUN_ID" > "$timing_table" <<'PY'
import csv
import sys

path = sys.argv[1]
run_id = sys.argv[2]
rows = []
try:
    with open(path, newline="") as f:
        for row in csv.DictReader(f, delimiter="\t"):
            if row.get("run_id") != run_id:
                continue
            if not row.get("script") or not row.get("phase"):
                continue
            try:
                duration = int(row.get("duration_seconds", "0"))
            except ValueError:
                continue
            rows.append(
                {
                    "script": row["script"],
                    "phase": row["phase"],
                    "duration": duration,
                    "start": row.get("start_utc", ""),
                    "end": row.get("end_utc", ""),
                    "status": row.get("status", ""),
                }
            )
except FileNotFoundError:
    rows = []

print("## Stage timings")
print()
print("| Script | Phase | Duration (s) | Status | Start | End |")
print("|---|---|---:|---|---|---|")
for row in sorted(rows, key=lambda r: r["duration"], reverse=True):
    print(
        f"| `{row['script']}` | {row['phase']} | {row['duration']} | {row['status']} | {row['start']} | {row['end']} |"
    )
PY
cat "$timing_table" >> "$ARTIFACTS/results.md"

if [[ "$RUN_DESTRUCTIVE_FUNCTIONAL" == "1" ]]; then
  replica_loss_note="Replica-loss scenarios are co-scheduled in one Phase B window with one dataplane Pod deleted mid-window."
  reproduction_command="RUN_DESTRUCTIVE_FUNCTIONAL=1 make e2e"
else
  replica_loss_note="Replica-loss scenarios were not run. Set RUN_DESTRUCTIVE_FUNCTIONAL=1 for the full destructive pass."
  reproduction_command="make e2e"
fi

cat >> "$ARTIFACTS/results.md" <<EOF

Latencies in ms. RPS is \`success_200.rate\` from the k6 summary:
HTTP 200 responses per second over the run window. This run used the
\`${results_perf_profile}\` k6 profile (${results_window}).
\`count (ok)\` is the absolute HTTP 200 count over the same run
window; the success floor for this profile is
\`success_200:count>=${results_success_floor}\` in tests/agent-gateway/perf/*.js.
\`p50/p95/p99\` are http_req_duration percentiles over
expected_response=true requests. Steady-state scenarios are
co-scheduled in one Phase A window.
$replica_loss_note
Use \`RUN_PERF_BENCHMARK=1\` or \`make perf-benchmark\` for the
sustained benchmark profile.

## Reproduction

\`\`\`bash
$reproduction_command
\`\`\`

Output directory: \`tests/agent-gateway/artifacts/\` — \`functional-test.json\` carries the
\`go test -json\` stream, \`tests-summary.txt\` the per-test status table,
\`*.summary.json\` + \`*.pod.log\` the k6 evidence.
EOF

if (( SUITE_RC != 0 || FAIL != 0 )); then
  log "e2e: FAIL — functional suite failed"
  exit 1
fi
if (( PERF_RC != 0 )); then
  log "e2e: FAIL — perf jobs failed (PERF_RC=$PERF_RC); see tests/agent-gateway/artifacts/*.summary.json"
  exit 1
fi
log "e2e: OK"
