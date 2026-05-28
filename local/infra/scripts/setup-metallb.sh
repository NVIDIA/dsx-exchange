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
command -v kind >/dev/null 2>&1 || { echo "ERROR: kind is required" >&2; exit 1; }

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

apply_metallb_resources() {
  local cluster_name=$1
  local cluster_num=$2
  local context=$3

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
}

wait_metallb_webhook() {
  local context=$1
  local webhook_addresses

  echo "Waiting for MetalLB webhook endpoint..."
  for i in {1..60}; do
    webhook_addresses=$(kubectl get endpoints metallb-webhook-service \
      -n metallb-system \
      --context "${context}" \
      -o jsonpath='{.subsets[*].addresses[*].ip}' 2>/dev/null || true)
    if [ -n "${webhook_addresses}" ]; then
      return 0
    fi
    if [ "${i}" -eq 60 ]; then
      echo "ERROR: MetalLB webhook endpoint not ready after 60 seconds" >&2
      return 1
    fi
    sleep 1
  done
}

apply_metallb_config() {
  local cluster_name=$1
  local cluster_num=$2
  local context=$3

  # Wait for CRDs to be ready before creating resources
  echo "Waiting for MetalLB CRDs to be ready..."
  for i in {1..30}; do
    if kubectl get crd ipaddresspools.metallb.io --context "${context}" >/dev/null 2>&1 && \
       kubectl get crd l2advertisements.metallb.io --context "${context}" >/dev/null 2>&1; then
      kubectl wait --for=condition=Established \
        crd/ipaddresspools.metallb.io \
        crd/l2advertisements.metallb.io \
        --timeout=30s \
        --context "${context}"
      break
    fi
    if [ $i -eq 30 ]; then
      echo "ERROR: MetalLB CRDs not ready after 30 seconds" >&2
      return 1
    fi
    sleep 1
  done

  wait_metallb_webhook "${context}"

  # Create MetalLB configuration dynamically based on Docker network
  echo "Configuring MetalLB IP pool for ${cluster_name}..."
  if ! apply_metallb_resources "${cluster_name}" "${cluster_num}" "${context}"; then
    echo "ERROR: Failed to apply MetalLB address resources for ${cluster_name}" >&2
    return 1
  fi

  # Verify address resources were created successfully
  if kubectl get ipaddresspool "${cluster_name}-pool" -n metallb-system --context "${context}" >/dev/null 2>&1 && \
     kubectl get l2advertisement "${cluster_name}-l2-advert" -n metallb-system --context "${context}" >/dev/null 2>&1; then
    echo "✓ MetalLB address resources for ${cluster_name} created successfully"
  else
    echo "ERROR: Failed to create MetalLB address resources for ${cluster_name}" >&2
    return 1
  fi
}

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
    --timeout 5m

  kubectl rollout status deployment/metallb-controller \
    -n metallb-system \
    --timeout=5m \
    --context "${context}"

  apply_metallb_config "${cluster_name}" "${cluster_num}" "${context}"

  kubectl rollout status daemonset/metallb-speaker \
    -n metallb-system \
    --timeout=5m \
    --context "${context}"
}

# Deploy to all clusters in parallel with assigned IP pool numbers
echo "Deploying MetalLB to all clusters in parallel..."
clusters=("csc:200" "cpc-1:201" "cpc-2:202")
kind_clusters=$(kind get clusters) || { echo "ERROR: Failed to list Kind clusters" >&2; exit 1; }
missing_clusters=()

for cluster in "${clusters[@]}"; do
  cluster_name=${cluster%%:*}
  if ! grep -qx "${cluster_name}" <<< "${kind_clusters}"; then
    missing_clusters+=("${cluster_name}")
  fi
done

if [ "${#missing_clusters[@]}" -ne 0 ]; then
  echo "ERROR: Missing Kind cluster(s): ${missing_clusters[*]}" >&2
  exit 1
fi

pids=()

for cluster in "${clusters[@]}"; do
  cluster_name=${cluster%%:*}
  cluster_num=${cluster##*:}
  deploy_to_cluster "${cluster_name}" "${cluster_num}" &
  pids+=("$!")
done

if [ "${#pids[@]}" -eq 0 ]; then
  echo "ERROR: No MetalLB deployments were started" >&2
  exit 1
fi

# Wait for all deployments to complete
status=0
for pid in "${pids[@]}"; do
  if ! wait "${pid}"; then
    status=1
  fi
done

if [ "${status}" -ne 0 ]; then
  echo "ERROR: MetalLB deployment failed for one or more clusters" >&2
  exit "${status}"
fi

echo "MetalLB deployed successfully"
echo "IP Pools: ${DOCKER_BASE}.200.x, ${DOCKER_BASE}.201.x, ${DOCKER_BASE}.202.x"
