#!/bin/bash
# Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
# SPDX-License-Identifier: Apache-2.0


set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

command -v kubectl >/dev/null 2>&1 || { echo "ERROR: kubectl is required" >&2; exit 1; }
command -v helm >/dev/null 2>&1 || { echo "ERROR: helm is required" >&2; exit 1; }

deploy_to_cluster() {
  local cluster_name=$1
  local context="kind-${cluster_name}"

  echo "Deploying Envoy Gateway to ${cluster_name}..."

  # Install CRDs separately using helm template and server-side apply
  echo "Installing Envoy Gateway CRDs..."
  helm template eg oci://docker.io/envoyproxy/gateway-crds-helm \
    --version v1.5.4 \
    --set crds.gatewayAPI.enabled=true \
    --set crds.gatewayAPI.channel=experimental \
    --set crds.envoyGateway.enabled=true \
    | kubectl apply --server-side -f - --context "${context}"

  # Install Envoy Gateway without CRDs
  echo "Installing Envoy Gateway..."
  helm upgrade --install eg oci://docker.io/envoyproxy/gateway-helm \
    --version v1.5.4 \
    --namespace envoy-gateway-system \
    --create-namespace \
    --skip-crds \
    --kube-context "${context}" \
    --wait \
    --timeout 5m

  kubectl wait --for=condition=available deployment/envoy-gateway \
    --namespace envoy-gateway-system \
    --timeout=5m \
    --context "${context}"

  # Create GatewayClass
  kubectl apply -f "${PROJECT_ROOT}/infra/envoy-gateway/gatewayclass.yaml" --context "${context}"

  # Create shared Gateway
  kubectl apply -f "${PROJECT_ROOT}/infra/envoy-gateway/gateway.yaml" --context "${context}"
}

# Deploy to all clusters in parallel
echo "Deploying Envoy Gateway to all clusters in parallel..."
pids=()

for cluster in csc cpc-1 cpc-2; do
    if kind get clusters | grep -q "^${cluster}$"; then
        deploy_to_cluster "$cluster" &
        pids+=("$!")
    fi
done

# Wait for all deployments to complete
for pid in "${pids[@]}"; do
  wait "${pid}"
done

echo "Envoy Gateway deployed successfully"
