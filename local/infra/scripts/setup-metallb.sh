#!/bin/bash
# Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
# SPDX-License-Identifier: Apache-2.0


set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

command -v kubectl >/dev/null 2>&1 || { echo "ERROR: kubectl is required" >&2; exit 1; }
command -v helm >/dev/null 2>&1 || { echo "ERROR: helm is required" >&2; exit 1; }
command -v docker >/dev/null 2>&1 || { echo "ERROR: docker is required" >&2; exit 1; }
command -v jq >/dev/null 2>&1 || { echo "ERROR: jq is required" >&2; exit 1; }

# Detect Docker network CIDR for Kind (IPv4 only)
echo "Detecting Docker network configuration..."
DOCKER_SUBNET=$(docker network inspect kind | jq -r '.[0].IPAM.Config[] | select(.Subnet | contains(".")) | .Subnet')
if [ -z "$DOCKER_SUBNET" ]; then
  echo "ERROR: Could not detect IPv4 subnet for Kind network" >&2
  exit 1
fi
DOCKER_BASE=$(echo "$DOCKER_SUBNET" | cut -d'.' -f1-2)
echo "Docker network: $DOCKER_SUBNET (base: $DOCKER_BASE)"

# Add MetalLB Helm repository
if ! helm repo list | grep -q "metallb"; then
    helm repo add metallb https://metallb.github.io/metallb
    helm repo update >/dev/null
fi

deploy_to_cluster() {
  local cluster_name=$1
  local cluster_num=$2
  local context="kind-${cluster_name}"

  echo "Deploying MetalLB to ${cluster_name}..."

  helm upgrade --install metallb metallb/metallb \
    --version 0.15.2 \
    --namespace metallb-system \
    --create-namespace \
    --kube-context "${context}" \
    --wait \
    --timeout 5m

  kubectl wait --for=condition=ready pod \
    -l app.kubernetes.io/name=metallb \
    -n metallb-system \
    --timeout=5m \
    --context "${context}"

  # Wait for CRDs to be ready before creating resources
  echo "Waiting for MetalLB CRDs to be ready..."
  for i in {1..30}; do
    if kubectl get crd ipaddresspools.metallb.io --context "${context}" >/dev/null 2>&1 && \
       kubectl get crd l2advertisements.metallb.io --context "${context}" >/dev/null 2>&1; then
      break
    fi
    if [ $i -eq 30 ]; then
      echo "ERROR: MetalLB CRDs not ready after 30 seconds" >&2
      exit 1
    fi
    sleep 1
  done

  # Create MetalLB configuration dynamically based on Docker network
  echo "Configuring MetalLB IP pool for ${cluster_name}..."
  cat <<EOF | kubectl apply -f - --context "${context}"
---
apiVersion: metallb.io/v1beta1
kind: IPAddressPool
metadata:
  name: ${cluster_name}-pool
  namespace: metallb-system
spec:
  addresses:
  - ${DOCKER_BASE}.${cluster_num}.1-${DOCKER_BASE}.${cluster_num}.254
  autoAssign: true
  avoidBuggyIPs: false
---
apiVersion: metallb.io/v1beta1
kind: L2Advertisement
metadata:
  name: ${cluster_name}-l2-advert
  namespace: metallb-system
spec:
  ipAddressPools:
  - ${cluster_name}-pool
  interfaces:
  - eth0
EOF

  # Verify pool was created successfully
  if kubectl get ipaddresspool "${cluster_name}-pool" -n metallb-system --context "${context}" >/dev/null 2>&1; then
    echo "✓ IPAddressPool ${cluster_name}-pool created successfully"
  else
    echo "ERROR: Failed to create IPAddressPool for ${cluster_name}" >&2
    exit 1
  fi
}

# Deploy to all clusters in parallel with assigned IP pool numbers
echo "Deploying MetalLB to all clusters in parallel..."
pids=()

if kind get clusters | grep -q "^csc$"; then
    deploy_to_cluster "csc" "200" &
    pids+=("$!")
fi

if kind get clusters | grep -q "^cpc-1$"; then
    deploy_to_cluster "cpc-1" "201" &
    pids+=("$!")
fi

if kind get clusters | grep -q "^cpc-2$"; then
    deploy_to_cluster "cpc-2" "202" &
    pids+=("$!")
fi

# Wait for all deployments to complete
for pid in "${pids[@]}"; do
  wait "${pid}"
done

echo "MetalLB deployed successfully"
echo "IP Pools: ${DOCKER_BASE}.200.x, ${DOCKER_BASE}.201.x, ${DOCKER_BASE}.202.x"
