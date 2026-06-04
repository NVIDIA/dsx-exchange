#!/usr/bin/env bash
# Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

failures=0

check() {
  local name=$1
  shift

  if "$@" >/dev/null 2>&1; then
    echo "PASS: ${name}"
  else
    echo "FAIL: ${name}"
    failures=$((failures + 1))
  fi
}

check_json() {
	local name=$1
	local json=$2
	shift 2

	if jq -e "$@" >/dev/null <<<"${json}"; then
    echo "PASS: ${name}"
  else
    echo "FAIL: ${name}"
    failures=$((failures + 1))
  fi
}

cluster_ip() {
  case "$1" in
    csc) echo "172.18.200.1" ;;
    cpc-1) echo "172.18.201.1" ;;
    cpc-2) echo "172.18.202.1" ;;
    *) return 1 ;;
  esac
}

validate_cluster() {
  local cluster=$1
  local context="kind-${cluster}"
  local expected_ip
  expected_ip=$(cluster_ip "${cluster}")

  echo ""
  echo "Validating ${cluster}"

	check "${cluster} Kind cluster exists" bash -c "kind get clusters | grep -qx '${cluster}'"
  check "${cluster} API server" kubectl cluster-info --context "${context}"
  check "${cluster} nodes ready" kubectl wait --for=condition=Ready nodes --all --context "${context}" --timeout=30s

  check "${cluster} MetalLB controller ready" kubectl rollout status deployment/metallb-controller -n metallb-system --context "${context}" --timeout=30s
  check "${cluster} Envoy controller ready" kubectl rollout status deployment/envoy-gateway -n envoy-gateway-system --context "${context}" --timeout=30s
  check "${cluster} metrics-server ready" kubectl rollout status deployment/metrics-server -n kube-system --context "${context}" --timeout=30s
  check "${cluster} NATS ready" kubectl rollout status statefulset/nats -n event-bus --context "${context}" --timeout=30s
  check "${cluster} auth-callout ready" kubectl rollout status deployment/auth-callout -n event-bus --context "${context}" --timeout=30s

  check "${cluster} Envoy pool exists" kubectl get ipaddresspool envoy-pool -n metallb-system --context "${context}"
  check "${cluster} default pool exists" kubectl get ipaddresspool default-pool -n metallb-system --context "${context}"
  check "${cluster} Gateway programmed" kubectl wait --for=condition=Programmed gateway/shared-gateway -n envoy-gateway-system --context "${context}" --timeout=30s

  local gateway_ip
  gateway_ip=$(kubectl get gateway shared-gateway -n envoy-gateway-system --context "${context}" -o jsonpath='{.status.addresses[0].value}' 2>/dev/null || true)
  if [ "${gateway_ip}" = "${expected_ip}" ]; then
    echo "PASS: ${cluster} Gateway IP ${expected_ip}"
  else
    echo "FAIL: ${cluster} Gateway IP expected ${expected_ip}, got ${gateway_ip:-none}"
    failures=$((failures + 1))
  fi

  local stream_json
  stream_json=$(kubectl exec -n event-bus nats-0 --context "${context}" -c nats -- \
    wget -qO- 'http://localhost:8222/jsz?streams=true&config=true')

  for stream in '$MQTT_msgs' '$MQTT_rmsgs' '$MQTT_sess' '$MQTT_qos2in' '$MQTT_out'; do
    check_json "${cluster} ${stream} memory replicated stream" "${stream_json}" \
      --arg stream "${stream}" '[.account_details[].stream_detail[]? | select(.name == $stream and .config.storage == "memory" and .config.num_replicas == 3)] | length > 0'
  done

  local leafz
  leafz=$(kubectl exec -n event-bus nats-0 --context "${context}" -c nats -- \
    wget -qO- http://localhost:8222/leafz)

  if jq -e '.leafs | length > 0' >/dev/null <<<"${leafz}"; then
    echo "PASS: ${cluster} leaf connections present"
  else
    echo "FAIL: ${cluster} leaf connections present"
    failures=$((failures + 1))
  fi
}

for cluster in csc cpc-1 cpc-2; do
  validate_cluster "${cluster}"
done

echo ""
echo "Validating Keycloak admin route"
if curl -fsSL -o /dev/null "http://172.18.200.1/admin/master/console/"; then
  echo "PASS: Keycloak admin console"
else
  echo "FAIL: Keycloak admin console"
  failures=$((failures + 1))
fi

echo ""
if [ "${failures}" -eq 0 ]; then
  echo "Validation passed"
  exit 0
fi

echo "Validation failed: ${failures} check(s)"
exit 1
