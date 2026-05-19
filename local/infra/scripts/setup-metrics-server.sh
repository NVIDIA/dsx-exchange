#!/bin/bash
# Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
# SPDX-License-Identifier: Apache-2.0


set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

command -v kubectl >/dev/null 2>&1 || { echo "ERROR: kubectl is required" >&2; exit 1; }

echo "Deploying metrics-server to all clusters in parallel..."

# Function to deploy metrics-server to a cluster
deploy_to_cluster() {
  local cluster=$1
  local context="kind-${cluster}"

  echo "Deploying metrics-server to ${cluster}..."

  # Apply the standard metrics-server manifest (components.yaml works with modern K8s versions)
  kubectl apply -f https://github.com/kubernetes-sigs/metrics-server/releases/latest/download/components.yaml --context "${context}"

  # Patch metrics-server for Kind compatibility (disable TLS verification)
  echo "Patching metrics-server for Kind cluster compatibility in ${cluster}..."
  kubectl patch deployment metrics-server -n kube-system --context "${context}" \
    --type='json' \
    -p='[{"op": "add", "path": "/spec/template/spec/containers/0/args/-", "value": "--kubelet-insecure-tls"}]'

  echo "Waiting for metrics-server to be ready in ${cluster}..."
  kubectl wait --for=condition=Available deployment/metrics-server -n kube-system --timeout=2m --context "${context}" || {
    echo "WARNING: metrics-server not ready in ${cluster} within timeout"
  }
}

# Deploy to all clusters in parallel
pids=()

deploy_to_cluster "csc" &
pids+=("$!")
deploy_to_cluster "cpc-1" &
pids+=("$!")
deploy_to_cluster "cpc-2" &
pids+=("$!")

# Wait for all deployments to complete
for pid in "${pids[@]}"; do
  wait "${pid}"
done

echo "Metrics-server deployed successfully to all clusters"
echo "Test with: kubectl top nodes --context kind-csc"
