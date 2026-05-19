#!/bin/bash
# Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
# SPDX-License-Identifier: Apache-2.0


set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

command -v kubectl >/dev/null 2>&1 || { echo "ERROR: kubectl is required" >&2; exit 1; }

deploy_to_cluster() {
  local cluster_name=$1
  local context="kind-${cluster_name}"

  echo "Deploying cert-manager to ${cluster_name}..."

  # Install cert-manager
  kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.16.2/cert-manager.yaml --context "${context}"

  kubectl wait --for=condition=available deployment/cert-manager -n cert-manager --context "${context}" --timeout=2m
  kubectl wait --for=condition=available deployment/cert-manager-webhook -n cert-manager --context "${context}" --timeout=1m
  kubectl wait --for=condition=available deployment/cert-manager-cainjector -n cert-manager --context "${context}" --timeout=1m
}

# Deploy to all clusters in parallel
pids=()

for cluster in csc cpc-1 cpc-2; do
    if kind get clusters 2>/dev/null | grep -q "^${cluster}$"; then
        deploy_to_cluster "$cluster" &
        pids+=("$!")
    fi
done

for pid in "${pids[@]}"; do
  wait "${pid}"
done

echo "cert-manager deployed successfully"
