#!/bin/bash
# Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
# SPDX-License-Identifier: Apache-2.0


set -e

cluster=${1:-csc}
kind_cluster="${cluster}"
context="kind-${kind_cluster}"
namespace="event-bus"

echo "Validating NATS deployment on ${cluster}..."
echo ""

# Check pods
echo "Checking pods..."
kubectl get pods -n ${namespace} --context "${context}"
echo ""

# Check cluster status
echo "Checking NATS cluster..."
nats_box=$(kubectl get pod -n ${namespace} --context "${context}" -o name | grep nats-box | head -1 | cut -d/ -f2)
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
kubectl exec -n ${namespace} "${nats_box}" --context "${context}" -- nats stream ls -a
echo ""

# Verify memory storage
echo "Verifying memory storage..."
for stream in '$MQTT_msgs' '$MQTT_rmsgs' '$MQTT_sess' '$MQTT_qos2in' '$MQTT_out'; do
  storage=$(kubectl exec -n ${namespace} "${nats_box}" --context "${context}" -- \
    nats stream info "${stream}" | grep "Storage:" | awk '{print $2}')
  replicas=$(kubectl exec -n ${namespace} "${nats_box}" --context "${context}" -- \
    nats stream info "${stream}" | grep "Replicas:" | awk '{print $2}')
  echo "${stream}: Storage=${storage}, Replicas=${replicas}"
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

    # Test connectivity
    echo ""
    echo "Testing connectivity..."
    all_ports_ok=true

    if nc -z -w1 "${gateway_ip}" 1883 2>/dev/null; then
      echo "  MQTT (1883): OK"
    else
      echo "  MQTT (1883): FAILED"
      all_ports_ok=false
    fi

    if nc -z -w1 "${gateway_ip}" 8883 2>/dev/null; then
      echo "  MQTT mTLS (8883): OK"
    else
      echo "  MQTT mTLS (8883): FAILED"
      all_ports_ok=false
    fi

    if nc -z -w1 "${gateway_ip}" 4222 2>/dev/null; then
      echo "  NATS Client (4222): OK"
    else
      echo "  NATS Client (4222): FAILED"
      all_ports_ok=false
    fi

    if nc -z -w1 "${gateway_ip}" 7422 2>/dev/null; then
      echo "  NATS Leaf Node (7422): OK"
    else
      echo "  NATS Leaf Node (7422): FAILED"
      all_ports_ok=false
    fi

    if [ "${all_ports_ok}" = "true" ]; then
      echo ""
      echo "Gateway: READY"
    else
      echo ""
      echo "Gateway: PORTS NOT RESPONDING"
    fi
  else
    echo "Gateway: NOT READY"
  fi
else
  echo "Gateway: NOT FOUND"
fi
echo ""

echo "NATS validation complete"

