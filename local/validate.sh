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

validate_site() {
  local site=$1
  local event_bus_namespace
  local gateway_ns
  local expected_ip
  event_bus_namespace="${site}-event-bus"
  gateway_ns="${site}-gateway"
  expected_ip=$(cluster_ip "${site}")

  echo ""
  echo "Validating ${site}"

  check "${site} NATS ready" kubectl rollout status statefulset/nats -n "${event_bus_namespace}" --context kind-dsx-exchange --timeout=30s
  check "${site} auth-callout ready" kubectl rollout status deployment/auth-callout -n "${event_bus_namespace}" --context kind-dsx-exchange --timeout=30s

  check "${site} Envoy pool exists" kubectl get ipaddresspool "${site}-envoy-pool" -n metallb-system --context kind-dsx-exchange
  check "${site} default pool exists" kubectl get ipaddresspool "${site}-default-pool" -n metallb-system --context kind-dsx-exchange
  check "${site} Gateway programmed" kubectl wait --for=condition=Programmed gateway/shared-gateway -n "${gateway_ns}" --context kind-dsx-exchange --timeout=30s

  local gateway_ip
  gateway_ip=$(kubectl get gateway shared-gateway -n "${gateway_ns}" --context kind-dsx-exchange -o jsonpath='{.status.addresses[0].value}' 2>/dev/null || true)
  if [ "${gateway_ip}" = "${expected_ip}" ]; then
    echo "PASS: ${site} Gateway IP ${expected_ip}"
  else
    echo "FAIL: ${site} Gateway IP expected ${expected_ip}, got ${gateway_ip:-none}"
    failures=$((failures + 1))
  fi

  local stream_json
  if stream_json=$(kubectl exec -n "${event_bus_namespace}" nats-0 --context kind-dsx-exchange -c nats -- \
    wget -qO- 'http://localhost:8222/jsz?streams=true&config=true'); then
    for stream in '$MQTT_msgs' '$MQTT_rmsgs' '$MQTT_sess' '$MQTT_qos2in' '$MQTT_out'; do
      check_json "${site} ${stream} memory replicated stream" "${stream_json}" \
        --arg stream "${stream}" '[.account_details[].stream_detail[]? | select(.name == $stream and .config.storage == "memory" and .config.num_replicas == 3)] | length > 0'
    done
  else
    echo "FAIL: ${site} stream config readable"
    failures=$((failures + 1))
  fi

  local leafz
  local leaf_connections=false
  for pod in nats-0 nats-1 nats-2; do
    if leafz=$(kubectl exec -n "${event_bus_namespace}" "${pod}" --context kind-dsx-exchange -c nats -- \
      wget -qO- http://localhost:8222/leafz) &&
      jq -e '.leafs | length > 0' >/dev/null <<<"${leafz}"; then
      leaf_connections=true
      break
    fi
  done

  if [ "${leaf_connections}" = true ]; then
    echo "PASS: ${site} leaf connections present"
  else
    echo "FAIL: ${site} leaf connections present"
    failures=$((failures + 1))
  fi
}

check "Kind cluster exists" bash -c "kind get clusters | grep -qx dsx-exchange"
check "API server" kubectl cluster-info --context kind-dsx-exchange
check "node ready" kubectl wait --for=condition=Ready nodes --all --context kind-dsx-exchange --timeout=30s
check "MetalLB controller ready" kubectl rollout status deployment/metallb-controller -n metallb-system --context kind-dsx-exchange --timeout=30s
check "Envoy controller ready" kubectl rollout status deployment/envoy-gateway -n envoy-gateway-system --context kind-dsx-exchange --timeout=30s
check "metrics-server ready" kubectl rollout status deployment/metrics-server -n kube-system --context kind-dsx-exchange --timeout=30s

for site in csc cpc-1 cpc-2; do
  validate_site "${site}"
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
