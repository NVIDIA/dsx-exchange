#!/usr/bin/env bash
# Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

cluster="${1:-csc}"
context="kind-${cluster}"
namespace="event-bus"
nats_client_rollout_timeout="10m"

kubectl rollout status statefulset/nats \
  -n "${namespace}" \
  --context "${context}" \
  --timeout=3m
kubectl rollout status statefulset/nats-mtls \
  -n "${namespace}" \
  --context "${context}" \
  --timeout=2m
kubectl rollout status deployment/nats-event-bus-surveyor \
  -n "${namespace}" \
  --context "${context}" \
  --timeout="${nats_client_rollout_timeout}"
kubectl rollout status deployment/nack \
  -n "${namespace}" \
  --context "${context}" \
  --timeout=2m
kubectl wait \
  --for=condition=Ready \
  stream \
  --all \
  -n "${namespace}" \
  --context "${context}" \
  --timeout=2m
kubectl rollout status deployment/auth-callout \
  -n "${namespace}" \
  --context "${context}" \
  --timeout="${nats_client_rollout_timeout}"
kubectl wait \
  --for=condition=Programmed \
  gateway/shared-gateway \
  -n envoy-gateway-system \
  --context "${context}" \
  --timeout=2m

gateway_ip="$(
  kubectl get gateway shared-gateway \
    -n envoy-gateway-system \
    --context "${context}" \
    -o jsonpath='{.status.addresses[0].value}' 2>/dev/null || echo "pending"
)"

echo "NATS Event Bus deployment complete for ${cluster}"
echo "Gateway IP: ${gateway_ip}"
echo "MQTT (TCP): tcp://${gateway_ip}:1883"
echo "MQTT (mTLS): ssl://${gateway_ip}:8883"
echo "NATS Client: nats://${gateway_ip}:4222"
echo "NATS Leaf Node: nats://${gateway_ip}:7422"
