#!/bin/bash
# Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
# SPDX-License-Identifier: Apache-2.0


set -e

cluster=${1:-csc}
kind_cluster="${cluster}"
context="kind-${kind_cluster}"
namespace="event-bus"
validation_failed=false

fail() {
  echo "ERROR: $*"
  validation_failed=true
}

echo "Validating NATS deployment on ${cluster}..."
echo ""

# Check pods
echo "Checking pods..."
kubectl get pods -n ${namespace} --context "${context}"
echo ""

# Check cluster status
echo "Checking NATS cluster..."
routes=$(kubectl exec -n ${namespace} nats-0 --context "${context}" -c nats -- \
  wget -qO- http://localhost:8222/routez | grep -c '"remote_name"')
echo "Cluster routes: ${routes}"
echo ""

# Check JetStream via HTTP endpoint
echo "Checking JetStream..."
kubectl exec -n ${namespace} nats-0 --context "${context}" -c nats -- \
  wget -qO- http://localhost:8222/jsz | grep -E '"streams"|"memory"|"storage"' | head -5
echo ""

# Check MQTT streams
echo "Checking MQTT streams..."
stream_json=$(kubectl exec -n ${namespace} nats-0 --context "${context}" -c nats -- \
  wget -qO- 'http://localhost:8222/jsz?streams=true&config=true')
echo "${stream_json}" | jq -r '.account_details[].stream_detail[]?.name' | sort
echo ""

# Verify memory storage
echo "Verifying memory storage..."
for stream in '$MQTT_msgs' '$MQTT_rmsgs' '$MQTT_sess' '$MQTT_qos2in' '$MQTT_out'; do
  stream_info=$(echo "${stream_json}" | jq -c --arg stream "${stream}" \
    '[.account_details[].stream_detail[]? | select(.name == $stream)][0] // empty')

  if [ -z "${stream_info}" ]; then
    fail "Stream ${stream} not found"
    continue
  fi

  storage=$(echo "${stream_info}" | jq -r '.config.storage')
  replicas=$(echo "${stream_info}" | jq -r '.config.num_replicas')
  echo "${stream}: Storage=${storage}, Replicas=${replicas}"

  if [ "${storage}" != "memory" ]; then
    fail "Stream ${stream} uses ${storage} storage, expected memory"
  fi

  if [ "${replicas}" != "3" ]; then
    fail "Stream ${stream} has ${replicas} replicas, expected 3"
  fi
done
echo ""

# Check Leaf Node connections
echo "Checking Leaf Node federation..."
leafz=$(kubectl exec -n ${namespace} nats-0 --context "${context}" -c nats -- \
  wget -qO- http://localhost:8222/leafz 2>/dev/null)

leaf_count=$(echo "${leafz}" | jq -r '.leafs | length' 2>/dev/null || echo "0")
echo "Leaf node connections: ${leaf_count}"

if [ "${leaf_count}" != "null" ] && [ "${leaf_count}" -gt 0 ]; then
  echo "${leafz}" | jq -r '.leafs[] | "  - \(.name) from \(.ip) (rtt: \(.rtt))"' 2>/dev/null
  echo "Federation: ACTIVE"
else
  echo "Federation: NOT CONNECTED"
  fail "Leaf node federation is not connected"
fi
echo ""

# Check Gateway (deployed in envoy-gateway-system namespace)
echo "Checking Gateway..."
gateway_ns="envoy-gateway-system"
gateway_name=$(kubectl get gateway -n ${gateway_ns} --context "${context}" -o name 2>/dev/null | head -1 | cut -d/ -f2)
if [ -n "${gateway_name}" ]; then
  gateway_ip=$(kubectl get gateway "${gateway_name}" -n ${gateway_ns} --context "${context}" -o jsonpath='{.status.addresses[0].value}' 2>/dev/null || echo "pending")
  gateway_programmed=$(kubectl get gateway "${gateway_name}" -n ${gateway_ns} --context "${context}" -o jsonpath='{.status.conditions[?(@.type=="Programmed")].status}' 2>/dev/null || echo "Unknown")
  gateway_accepted=$(kubectl get gateway "${gateway_name}" -n ${gateway_ns} --context "${context}" -o jsonpath='{.status.conditions[?(@.type=="Accepted")].status}' 2>/dev/null || echo "Unknown")

  echo "Gateway: ${gateway_name}"
  echo "  IP: ${gateway_ip}"
  echo "  Accepted: ${gateway_accepted}"
  echo "  Programmed: ${gateway_programmed}"

  if [ "${gateway_programmed}" = "True" ] && [ "${gateway_ip}" != "pending" ]; then
    echo "  MQTT (TCP): tcp://${gateway_ip}:1883"
    echo "  MQTT (mTLS): ssl://${gateway_ip}:8883"
    echo "  NATS Client: nats://${gateway_ip}:4222"
    echo "  NATS Leaf Node: nats://${gateway_ip}:7422"

    echo "Gateway: READY"
  else
    echo "Gateway: NOT READY"
    fail "Gateway is not programmed or has no IP"
  fi
else
  echo "Gateway: NOT FOUND"
  fail "Gateway not found"
fi
echo ""

echo "NATS validation complete"

if [ "${validation_failed}" = "true" ]; then
  exit 1
fi
