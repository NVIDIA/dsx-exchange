#!/usr/bin/env bash
# Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

if [ "$#" -eq 0 ]; then
  echo "Usage: $0 <command> [args...]" >&2
  exit 2
fi

command -v kubectl >/dev/null 2>&1 || { echo "ERROR: kubectl is required" >&2; exit 1; }
command -v nc >/dev/null 2>&1 || { echo "ERROR: nc is required" >&2; exit 1; }

pids=()
logs=()

cleanup() {
  local pid
  for pid in "${pids[@]}"; do
    if kill -0 "${pid}" >/dev/null 2>&1; then
      kill "${pid}" >/dev/null 2>&1 || true
    fi
  done
  wait >/dev/null 2>&1 || true

  local log
  for log in "${logs[@]}"; do
    rm -f "${log}"
  done
}

trap cleanup EXIT

gateway_service() {
  local context="$1"

  kubectl get svc \
    --context "${context}" \
    -n envoy-gateway-system \
    -l gateway.envoyproxy.io/owning-gateway-name=shared-gateway \
    -o jsonpath='{.items[0].metadata.name}'
}

wait_for_local_port() {
  local port="$1"
  local log="$2"

  for _ in {1..60}; do
    if nc -z 127.0.0.1 "${port}" >/dev/null 2>&1; then
      return 0
    fi

    if ! kill -0 "${pids[-1]}" >/dev/null 2>&1; then
      echo "ERROR: port-forward for local port ${port} exited before becoming ready" >&2
      cat "${log}" >&2
      return 1
    fi

    sleep 0.5
  done

  echo "ERROR: timed out waiting for local port ${port}" >&2
  cat "${log}" >&2
  return 1
}

start_forward() {
  local name="$1"
  local context="$2"
  local local_port="$3"
  local remote_port="$4"
  local service
  local log

  if nc -z 127.0.0.1 "${local_port}" >/dev/null 2>&1; then
    echo "ERROR: local port ${local_port} is already in use" >&2
    exit 1
  fi

  service=$(gateway_service "${context}")
  if [ -z "${service}" ]; then
    echo "ERROR: shared Gateway service not found in ${context}" >&2
    exit 1
  fi

  log=$(mktemp)
  logs+=("${log}")

  echo "Forwarding ${name}: 127.0.0.1:${local_port} -> ${context}/${service}:${remote_port}"
  kubectl port-forward \
    --context "${context}" \
    --address 127.0.0.1 \
    -n envoy-gateway-system \
    "svc/${service}" \
    "${local_port}:${remote_port}" \
    > "${log}" 2>&1 &
  pids+=("$!")

  wait_for_local_port "${local_port}" "${log}"
}

start_forward "CSC MQTT" "kind-csc" 11883 1883
start_forward "CPC-1 MQTT" "kind-cpc-1" 11884 1883
start_forward "CPC-2 MQTT" "kind-cpc-2" 11885 1883
start_forward "CSC MQTT mTLS" "kind-csc" 18883 8883
start_forward "CSC Keycloak HTTP" "kind-csc" 18080 80

export CSC_BROKER_URL="tcp://127.0.0.1:11883"
export CPC1_BROKER_URL="tcp://127.0.0.1:11884"
export CPC2_BROKER_URL="tcp://127.0.0.1:11885"
export MQTT_BROKERS="CSC=${CSC_BROKER_URL},CPC-1=${CPC1_BROKER_URL},CPC-2=${CPC2_BROKER_URL}"
export MQTT_MTLS_BROKER="ssl://localhost:18883"
export MQTT_TCP_BROKER="${CSC_BROKER_URL}"
export KEYCLOAK_URL="http://127.0.0.1:18080"

"$@"
