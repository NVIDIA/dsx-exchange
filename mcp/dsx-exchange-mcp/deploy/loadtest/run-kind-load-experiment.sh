#!/usr/bin/env bash
# Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
LOADTEST_DIR="$ROOT_DIR/dsx-exchange-mcp/deploy/loadtest"

KUBECTL_CONTEXT="${KUBECTL_CONTEXT:-kind-dsx-mcp}"
MCP_NAMESPACE="${MCP_NAMESPACE:-mcp-backends}"
GATEWAY_NAMESPACE="${GATEWAY_NAMESPACE:-mcp-gateway}"
LOAD_NAMESPACE="${LOAD_NAMESPACE:-mcp-loadtest}"
BACKEND_DEPLOYMENT="${BACKEND_DEPLOYMENT:-dsx-exchange-mcp}"
BACKEND_REPLICAS="${BACKEND_REPLICAS:-1}"
SCENARIO="${SCENARIO:-mixed}"
SESSIONS="${SESSIONS:-100}"
SESSION_SWEEP="${SESSION_SWEEP:-}"
STARTUP_RAMP="${STARTUP_RAMP:-30s}"
DURATION="${DURATION:-90s}"
POLL_INTERVAL="${POLL_INTERVAL:-0s}"
GATEWAY_RPS="${GATEWAY_RPS:-1000}"
CLIENT_RPS="${CLIENT_RPS:-$GATEWAY_RPS}"
HTTP_TIMEOUT="${HTTP_TIMEOUT:-60s}"
WATCH_TTL_S="${WATCH_TTL_S:-360}"
SUBSCRIBE_DURATION_S="${SUBSCRIBE_DURATION_S:-1}"
MAX_MESSAGES="${MAX_MESSAGES:-10}"
MAX_BYTES="${MAX_BYTES:-32768}"
BACKEND_CONNECT_TIMEOUT_S="${BACKEND_CONNECT_TIMEOUT_S:-10}"
BACKEND_SUBSCRIBE_TIMEOUT_S="${BACKEND_SUBSCRIBE_TIMEOUT_S:-10}"
BACKEND_COLLECT_MAX_CONCURRENT="${BACKEND_COLLECT_MAX_CONCURRENT:-100}"
BACKEND_WATCH_START_MAX_CONCURRENT="${BACKEND_WATCH_START_MAX_CONCURRENT:-500}"
ENDPOINT="${ENDPOINT:-http://mcp-agentgw.mcp-gateway.svc.cluster.local/mcp}"
TOPIC="${TOPIC:-BMS/v1/PUB/Value/Rack/RackPower/#}"
RETAINED_TOPIC="${RETAINED_TOPIC:-BMS/v1/PUB/Metadata/Rack/RackPower/#}"
STICKY_SESSION_CHECK="${STICKY_SESSION_CHECK:-not_run}"
RESET_BACKEND="${RESET_BACKEND:-1}"
APPLY_BACKEND_ENV="${APPLY_BACKEND_ENV:-1}"
ENSURE_STATEFUL_GATEWAY="${ENSURE_STATEFUL_GATEWAY:-1}"
APPLY_GATEWAY_RATELIMIT="${APPLY_GATEWAY_RATELIMIT:-1}"
STRICT_JOB_SUCCESS="${STRICT_JOB_SUCCESS:-0}"
LOAD_IMAGE="${LOAD_IMAGE:-dsx-exchange-mcp-load:dev}"
REPORT_ROOT="${REPORT_ROOT:-$ROOT_DIR/dsx-exchange-mcp/reports/loadtest/live-$(date -u +%Y%m%d)}"

if [[ -n "$SESSION_SWEEP" ]]; then
  SESSION_LABEL="${SESSION_SWEEP//,/-}"
else
  SESSION_LABEL="$SESSIONS"
fi
EXPERIMENT="${EXPERIMENT:-${SCENARIO}-r${BACKEND_REPLICAS}-${SESSION_LABEL}-ramp-${STARTUP_RAMP}-gateway-${GATEWAY_RPS}}"
SAFE_EXPERIMENT="$(printf '%s' "$EXPERIMENT" | tr -c 'A-Za-z0-9_.-' '-' | sed 's/^-*//; s/-*$//')"
if [[ -z "$SAFE_EXPERIMENT" ]]; then
  SAFE_EXPERIMENT="loadtest"
fi
TIMESTAMP="$(date -u +%Y%m%d-%H%M%S)"
BUNDLE_DIR="$REPORT_ROOT/$SAFE_EXPERIMENT-$TIMESTAMP"
JOB_NAME="${JOB_NAME:-$(printf 'dsx-mcp-%s' "$SAFE_EXPERIMENT" | tr 'A-Z_.' 'a-z--' | cut -c1-60)}"
MANIFEST_NAME="$JOB_NAME.yaml"
MANIFEST_PATH="$BUNDLE_DIR/manifest.yaml"

mkdir -p "$BUNDLE_DIR"

kubectl_cmd() {
  kubectl --context "$KUBECTL_CONTEXT" "$@"
}

capture_cluster_state() {
  local path="$1"
  {
    echo "# captured_at=$(date -u +%FT%TZ)"
    echo
    echo "## dsx-exchange-mcp deployment"
    kubectl_cmd get deployment "$BACKEND_DEPLOYMENT" -n "$MCP_NAMESPACE" -o wide || true
    echo
    kubectl_cmd get pods -n "$MCP_NAMESPACE" -o wide || true
    echo
    echo "## agentgateway backend"
    kubectl_cmd get agentgatewaybackend mcp-agentgw-mcp -n "$GATEWAY_NAMESPACE" -o yaml || true
    echo
    echo "## agentgateway policy"
    kubectl_cmd get agentgatewaypolicy mcp-agentgw-authz -n "$GATEWAY_NAMESPACE" -o yaml || true
    echo
    echo "## rate limit config"
    kubectl_cmd get configmap latinum-mcp-gateway-ratelimit-config -n "$GATEWAY_NAMESPACE" -o yaml || true
    echo
    echo "## gateway pods"
    kubectl_cmd get pods -n "$GATEWAY_NAMESPACE" -o wide || true
  } > "$path"
}

write_token_metadata() {
  local path="$1"
  TOKEN_B64="$(kubectl_cmd get secret dsx-exchange-mcp-load-token -n "$LOAD_NAMESPACE" -o jsonpath='{.data.bearer}' 2>/dev/null || true)" \
    python3 - "$path" <<'PY'
import base64, json, os, sys, time
out = {"present": False, "valid_jwt_shape": False, "ttl_seconds": 0}
raw_b64 = os.environ.get("TOKEN_B64", "")
try:
    token = base64.b64decode(raw_b64).decode().strip()
    out["present"] = bool(token)
    parts = token.split(".")
    if len(parts) >= 2:
        payload = parts[1] + "=" * (-len(parts[1]) % 4)
        claims = json.loads(base64.urlsafe_b64decode(payload))
        out.update({
            "valid_jwt_shape": True,
            "scopes": claims.get("scopes"),
            "ttl_seconds": max(0, int(claims.get("exp", 0) - time.time())),
        })
except Exception as exc:
    out["error"] = type(exc).__name__
open(sys.argv[1], "w").write(json.dumps(out, indent=2, sort_keys=True) + "\n")
PY
}

backend_image_id() {
  kubectl_cmd get pods -n "$MCP_NAMESPACE" -l app=dsx-exchange-mcp \
    -o jsonpath='{.items[0].status.containerStatuses[0].imageID}' 2>/dev/null || true
}

load_image_id() {
  docker image inspect "$LOAD_IMAGE" --format '{{.Id}}' 2>/dev/null || true
}

if [[ "$ENSURE_STATEFUL_GATEWAY" == "1" ]]; then
  kubectl_cmd patch agentgatewaybackend mcp-agentgw-mcp -n "$GATEWAY_NAMESPACE" \
    --type=merge --patch-file "$LOADTEST_DIR/agentgatewaybackend-stateful-routing-patch.yaml"
fi

if [[ "$APPLY_GATEWAY_RATELIMIT" == "1" ]]; then
  case "$GATEWAY_RPS" in
    1000) kubectl_cmd apply -f "$LOADTEST_DIR/gateway-ratelimit-1000-configmap.yaml" ;;
    5000) kubectl_cmd apply -f "$LOADTEST_DIR/gateway-ratelimit-5000-configmap.yaml" ;;
    *) echo "unsupported GATEWAY_RPS=$GATEWAY_RPS; expected 1000 or 5000" >&2; exit 2 ;;
  esac
  kubectl_cmd rollout restart deployment/latinum-mcp-gateway-ratelimit -n "$GATEWAY_NAMESPACE"
  kubectl_cmd rollout status deployment/latinum-mcp-gateway-ratelimit -n "$GATEWAY_NAMESPACE" --timeout=120s
fi

if [[ "$APPLY_BACKEND_ENV" == "1" ]]; then
  kubectl_cmd set env deployment "$BACKEND_DEPLOYMENT" -n "$MCP_NAMESPACE" \
    MQTT_CONNECT_TIMEOUT_S="$BACKEND_CONNECT_TIMEOUT_S" \
    MQTT_SUBSCRIBE_TIMEOUT_S="$BACKEND_SUBSCRIBE_TIMEOUT_S" \
    MCP_MQTT_COLLECT_MAX_CONCURRENT_PER_POD="$BACKEND_COLLECT_MAX_CONCURRENT" \
    MCP_MQTT_WATCH_START_MAX_CONCURRENT_PER_POD="$BACKEND_WATCH_START_MAX_CONCURRENT"
fi

kubectl_cmd scale deployment "$BACKEND_DEPLOYMENT" -n "$MCP_NAMESPACE" --replicas="$BACKEND_REPLICAS"
kubectl_cmd rollout status deployment "$BACKEND_DEPLOYMENT" -n "$MCP_NAMESPACE" --timeout=120s
if [[ "$RESET_BACKEND" == "1" ]]; then
  kubectl_cmd rollout restart deployment "$BACKEND_DEPLOYMENT" -n "$MCP_NAMESPACE"
  kubectl_cmd rollout status deployment "$BACKEND_DEPLOYMENT" -n "$MCP_NAMESPACE" --timeout=120s
fi

BACKEND_IMAGE_ID="$(backend_image_id)"
LOAD_IMAGE_ID="$(load_image_id)"

capture_cluster_state "$BUNDLE_DIR/cluster-state-before.txt"
write_token_metadata "$BUNDLE_DIR/token-metadata.json"
cat > "$BUNDLE_DIR/images.txt" <<EOF
backend_image_id=$BACKEND_IMAGE_ID
load_image_id=$LOAD_IMAGE_ID
load_image=$LOAD_IMAGE
EOF

cat > "$MANIFEST_PATH" <<EOF
apiVersion: batch/v1
kind: Job
metadata:
  name: $JOB_NAME
  namespace: $LOAD_NAMESPACE
spec:
  backoffLimit: 0
  template:
    spec:
      restartPolicy: Never
      containers:
        - name: load
          image: $LOAD_IMAGE
          imagePullPolicy: IfNotPresent
          env:
            - name: DSX_EXCHANGE_MCP_URL
              value: "$ENDPOINT"
            - name: DSX_EXCHANGE_E2E_BEARER
              valueFrom:
                secretKeyRef:
                  name: dsx-exchange-mcp-load-token
                  key: bearer
            - name: DSX_EXCHANGE_MCP_LOAD_SCENARIO
              value: "$SCENARIO"
            - name: DSX_EXCHANGE_MCP_LOAD_EXPERIMENT
              value: "$EXPERIMENT"
            - name: DSX_EXCHANGE_MCP_LOAD_EXPERIMENT_DETAIL
              value: "backend_replicas=$BACKEND_REPLICAS;backend_reset=$RESET_BACKEND;gateway_rps=$GATEWAY_RPS;client_rps=$CLIENT_RPS;mqtt_connect_timeout_s=$BACKEND_CONNECT_TIMEOUT_S;mqtt_subscribe_timeout_s=$BACKEND_SUBSCRIBE_TIMEOUT_S;mqtt_collect_max_concurrent_per_pod=$BACKEND_COLLECT_MAX_CONCURRENT;mqtt_watch_start_max_concurrent_per_pod=$BACKEND_WATCH_START_MAX_CONCURRENT;http_timeout=$HTTP_TIMEOUT;startup_ramp=$STARTUP_RAMP;duration=$DURATION;poll_interval=$POLL_INTERVAL;scenario=$SCENARIO"
            - name: DSX_EXCHANGE_MCP_LOAD_SESSIONS
              value: "$SESSIONS"
            - name: DSX_EXCHANGE_MCP_LOAD_SESSION_SWEEP
              value: "$SESSION_SWEEP"
            - name: DSX_EXCHANGE_MCP_LOAD_BACKEND_REPLICAS
              value: "$BACKEND_REPLICAS"
            - name: DSX_EXCHANGE_MCP_LOAD_STICKY_SESSION_CHECK
              value: "$STICKY_SESSION_CHECK"
            - name: DSX_EXCHANGE_MCP_LOAD_DURATION
              value: "$DURATION"
            - name: DSX_EXCHANGE_MCP_LOAD_STARTUP_RAMP
              value: "$STARTUP_RAMP"
            - name: DSX_EXCHANGE_MCP_LOAD_POLL_INTERVAL
              value: "$POLL_INTERVAL"
            - name: DSX_EXCHANGE_MCP_LOAD_RATE_LIMIT
              value: "$CLIENT_RPS"
            - name: DSX_EXCHANGE_MCP_LOAD_GATEWAY_RATE_LIMIT_RPS
              value: "$GATEWAY_RPS"
            - name: DSX_EXCHANGE_MCP_LOAD_HTTP_TIMEOUT
              value: "$HTTP_TIMEOUT"
            - name: DSX_EXCHANGE_MCP_LOAD_WATCH_TTL_S
              value: "$WATCH_TTL_S"
            - name: DSX_EXCHANGE_MCP_LOAD_SUBSCRIBE_DURATION_S
              value: "$SUBSCRIBE_DURATION_S"
            - name: DSX_EXCHANGE_MCP_LOAD_MAX_MESSAGES
              value: "$MAX_MESSAGES"
            - name: DSX_EXCHANGE_MCP_LOAD_MAX_BYTES
              value: "$MAX_BYTES"
            - name: DSX_EXCHANGE_MCP_LOAD_BACKEND_MQTT_CONNECT_TIMEOUT_S
              value: "$BACKEND_CONNECT_TIMEOUT_S"
            - name: DSX_EXCHANGE_MCP_LOAD_BACKEND_MQTT_SUBSCRIBE_TIMEOUT_S
              value: "$BACKEND_SUBSCRIBE_TIMEOUT_S"
            - name: DSX_EXCHANGE_MCP_LOAD_BACKEND_MQTT_COLLECT_MAX_CONCURRENT_PER_POD
              value: "$BACKEND_COLLECT_MAX_CONCURRENT"
            - name: DSX_EXCHANGE_MCP_LOAD_BACKEND_MQTT_WATCH_START_MAX_CONCURRENT_PER_POD
              value: "$BACKEND_WATCH_START_MAX_CONCURRENT"
            - name: DSX_EXCHANGE_MCP_LOAD_MANIFEST_NAME
              value: "$MANIFEST_NAME"
            - name: DSX_EXCHANGE_MCP_LOAD_BACKEND_IMAGE_ID
              value: "$BACKEND_IMAGE_ID"
            - name: DSX_EXCHANGE_MCP_LOAD_IMAGE_ID
              value: "$LOAD_IMAGE_ID"
            - name: DSX_EXCHANGE_E2E_ALLOWED_TOPIC
              value: "$TOPIC"
            - name: DSX_EXCHANGE_E2E_RETAINED_TOPIC
              value: "$RETAINED_TOPIC"
            - name: DSX_EXCHANGE_MCP_LOAD_REPORT_DIR
              value: /reports
          volumeMounts:
            - name: reports
              mountPath: /reports
          resources:
            requests:
              cpu: 500m
              memory: 512Mi
            limits:
              cpu: "2"
              memory: 2Gi
      volumes:
        - name: reports
          emptyDir: {}
EOF

kubectl_cmd delete job "$JOB_NAME" -n "$LOAD_NAMESPACE" --ignore-not-found
kubectl_cmd apply -f "$MANIFEST_PATH"

JOB_STATE=""
for i in $(seq 1 180); do
  JOB_STATE="$(kubectl_cmd get job "$JOB_NAME" -n "$LOAD_NAMESPACE" -o jsonpath='{.status.succeeded},{.status.failed}' 2>/dev/null || true)"
  echo "poll $i state=$JOB_STATE"
  case "$JOB_STATE" in
    1,*) break ;;
    *,1|*,2|*,3) break ;;
  esac
  sleep 10
done

kubectl_cmd get job "$JOB_NAME" -n "$LOAD_NAMESPACE" -o wide > "$BUNDLE_DIR/job-status.txt"
kubectl_cmd logs "job/$JOB_NAME" -n "$LOAD_NAMESPACE" > "$BUNDLE_DIR/job.log" || true
capture_cluster_state "$BUNDLE_DIR/cluster-state-after.txt"

BUNDLE_DIR="$BUNDLE_DIR" python3 <<'PY'
import csv, json, os, pathlib

bundle = pathlib.Path(os.environ["BUNDLE_DIR"])
text = (bundle / "job.log").read_text()
decoder = json.JSONDecoder()
reports = None
prefix = ""
for idx, ch in enumerate(text):
    if ch not in "[{":
        continue
    try:
        value, end = decoder.raw_decode(text[idx:])
    except json.JSONDecodeError:
        continue
    candidates = value if isinstance(value, list) else [value]
    if candidates and isinstance(candidates[0], dict) and "started_at" in candidates[0]:
        reports = candidates
        prefix = text[:idx]
        break
if reports is None:
    raise SystemExit("could not find load report JSON in job.log")

(bundle / "report.json").write_text(json.dumps(reports if len(reports) > 1 else reports[0], indent=2) + "\n")
(bundle / "report.txt").write_text(prefix.rstrip() + "\n")

header = [
    "started_at","ended_at","experiment","experiment_detail","scenario","sessions",
    "backend_replicas","sticky_session_check","rate_limit_per_second","gateway_rate_limit_rps",
    "manifest_name","backend_image_id","load_image_id","experiment_config_hash",
    "token_ttl_seconds_at_start","endpoint","topic","retained_topic","http_timeout_seconds",
    "startup_ramp_seconds","poll_interval_seconds","subscribe_duration_seconds",
    "max_messages","max_bytes","watch_ttl_seconds","backend_mqtt_connect_timeout_seconds",
    "backend_mqtt_subscribe_timeout_seconds","backend_mqtt_collect_max_concurrent_per_pod",
    "backend_mqtt_watch_start_max_concurrent_per_pod","duration_seconds","throughput_requests_per_second",
    "success_rate_percent","total_requests","successes","failures","expected_tool_errors",
    "initialized_sessions","started_watches","stopped_watches","session_not_found_errors",
    "subscription_not_found_errors","operation","phase","operation_count","operation_successes",
    "operation_failures","p50_ms","p95_ms","p99_ms","operation_errors","errors",
]
def fnum(value):
    return f"{value:.3f}" if isinstance(value, float) else str(value)
with (bundle / "report.csv").open("w", newline="") as out:
    writer = csv.writer(out)
    writer.writerow(header)
    for report in reports:
        errors = ";".join(f"{k}={report.get('errors', {}).get(k)}" for k in sorted(report.get("errors", {})))
        for op in sorted(report["by_operation"]):
            stats = report["by_operation"][op]
            writer.writerow([
                report["started_at"], report["ended_at"], report.get("experiment", ""),
                report.get("experiment_detail", ""), report["scenario"], report["sessions"],
                report.get("backend_replicas", 0), report.get("sticky_session_check", ""),
                report.get("rate_limit_per_second", 0), report.get("gateway_rate_limit_rps", 0),
                report.get("manifest_name", ""), report.get("backend_image_id", ""),
                report.get("load_image_id", ""), report.get("experiment_config_hash", ""),
                report.get("token_ttl_seconds_at_start", 0), report["endpoint"], report["topic"],
                report["retained_topic"], fnum(report["http_timeout_seconds"]),
                fnum(report.get("startup_ramp_seconds", 0)), fnum(report.get("poll_interval_seconds", 0)),
                report["subscribe_duration_seconds"], report["max_messages"], report["max_bytes"],
                report["watch_ttl_seconds"], report.get("backend_mqtt_connect_timeout_seconds", 0),
                report.get("backend_mqtt_subscribe_timeout_seconds", 0),
                report.get("backend_mqtt_collect_max_concurrent_per_pod", 0),
                report.get("backend_mqtt_watch_start_max_concurrent_per_pod", 0),
                fnum(report["duration_seconds"]),
                fnum(report["throughput_requests_per_second"]), fnum(report["success_rate_percent"]),
                report["total_requests"], report["successes"], report["failures"],
                report["expected_tool_errors"], report["initialized_sessions"], report["started_watches"],
                report["stopped_watches"], report.get("session_not_found_errors", 0),
                report.get("subscription_not_found_errors", 0), op, stats["phase"], stats["count"],
                stats["successes"], stats["failures"], fnum(stats["p50_ms"]),
                fnum(stats["p95_ms"]), fnum(stats["p99_ms"]),
                ";".join(f"{k}={stats.get('errors', {}).get(k)}" for k in sorted(stats.get("errors", {}))),
                errors,
            ])

with (bundle / "summary.md").open("w") as out:
    out.write("# MCP Load Test Summary\n\n")
    out.write("| Experiment | Scenario | Sessions | Replicas | Ramp | Poll | Success | Failures | Started Watches | Stopped Watches | Session 404 | Subscription Missing |\n")
    out.write("|---|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|\n")
    for report in reports:
        out.write(
            f"| {report.get('experiment', '')} | {report['scenario']} | {report['sessions']} | "
            f"{report.get('backend_replicas', 0)} | {report.get('startup_ramp_seconds', 0):.0f}s | "
            f"{report.get('poll_interval_seconds', 0):.3f}s | {report['success_rate_percent']:.2f}% | "
            f"{report['failures']} | {report['started_watches']} | {report['stopped_watches']} | "
            f"{report.get('session_not_found_errors', 0)} | {report.get('subscription_not_found_errors', 0)} |\n"
        )

with (bundle / "config.yaml").open("w") as out:
    out.write("runs:\n")
    for report in reports:
        out.write(f"  - experiment: {json.dumps(report.get('experiment', ''))}\n")
        out.write(f"    experiment_config_hash: {json.dumps(report.get('experiment_config_hash', ''))}\n")
        out.write(f"    manifest_name: {json.dumps(report.get('manifest_name', ''))}\n")
        out.write(f"    scenario: {json.dumps(report['scenario'])}\n")
        out.write(f"    sessions: {report['sessions']}\n")
        out.write(f"    backend_replicas: {report.get('backend_replicas', 0)}\n")
        out.write(f"    gateway_rate_limit_rps: {report.get('gateway_rate_limit_rps', 0)}\n")
        out.write(f"    client_rate_limit_per_second: {report.get('rate_limit_per_second', 0)}\n")
        out.write(f"    startup_ramp_seconds: {report.get('startup_ramp_seconds', 0)}\n")
        out.write(f"    poll_interval_seconds: {report.get('poll_interval_seconds', 0)}\n")
        out.write(f"    token_ttl_seconds_at_start: {report.get('token_ttl_seconds_at_start', 0)}\n")
        out.write(f"    backend_mqtt_connect_timeout_seconds: {report.get('backend_mqtt_connect_timeout_seconds', 0)}\n")
        out.write(f"    backend_mqtt_subscribe_timeout_seconds: {report.get('backend_mqtt_subscribe_timeout_seconds', 0)}\n")
        out.write(f"    backend_mqtt_collect_max_concurrent_per_pod: {report.get('backend_mqtt_collect_max_concurrent_per_pod', 0)}\n")
        out.write(f"    backend_mqtt_watch_start_max_concurrent_per_pod: {report.get('backend_mqtt_watch_start_max_concurrent_per_pod', 0)}\n")
PY

echo "bundle written: $BUNDLE_DIR"
echo "$BUNDLE_DIR" > "$REPORT_ROOT/latest-bundle.txt"

if [[ "$STRICT_JOB_SUCCESS" == "1" && "$JOB_STATE" != 1,* ]]; then
  exit 1
fi
