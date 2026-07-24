#!/bin/bash
# Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
# SPDX-License-Identifier: Apache-2.0


set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

# Check prerequisites
command -v kind >/dev/null 2>&1 || { echo "ERROR: kind is required but not installed" >&2; exit 1; }
command -v kubectl >/dev/null 2>&1 || { echo "ERROR: kubectl is required but not installed" >&2; exit 1; }
command -v docker >/dev/null 2>&1 || { echo "ERROR: docker is required but not installed" >&2; exit 1; }
command -v jq >/dev/null 2>&1 || { echo "ERROR: jq is required but not installed" >&2; exit 1; }

# Ensure Kind Docker network uses 172.18.0.0/16 subnet
echo "Configuring Kind Docker network..."
KIND_NETWORK_EXISTS=false
if docker network inspect kind >/dev/null 2>&1; then
  KIND_NETWORK_EXISTS=true
  CURRENT_SUBNET=$(docker network inspect kind | jq -r '.[0].IPAM.Config[] | select(.Subnet | contains(".")) | .Subnet' 2>/dev/null || echo "")

  if [ "$CURRENT_SUBNET" != "172.18.0.0/16" ]; then
    echo "WARNING: Existing Kind network uses ${CURRENT_SUBNET}, but 172.18.0.0/16 is required"
    EXISTING_CLUSTERS=$(kind get clusters 2>/dev/null | wc -l | tr -d ' ')
    if [ "$EXISTING_CLUSTERS" -gt 0 ]; then
      echo "ERROR: Cannot change network subnet while clusters exist. Please delete clusters first:" >&2
      kind get clusters | sed 's/^/  - /' >&2
      exit 1
    fi
    echo "Removing existing Kind network..."
    docker network rm kind
    KIND_NETWORK_EXISTS=false
  else
    echo "Kind network already configured with 172.18.0.0/16"
  fi
fi

if [ "$KIND_NETWORK_EXISTS" = false ]; then
  echo "Creating Kind network with subnet 172.18.0.0/16..."
  docker network create \
    --driver bridge \
    --subnet=172.18.0.0/16 \
    --gateway=172.18.0.1 \
    kind
fi

create_cluster() {
  local cluster_name=$1
  local config_file=$2

  if [ ! -f "${config_file}" ]; then
    echo "ERROR: Kind config not found: ${config_file}" >&2
    exit 1
  fi

  if kind get clusters | grep -q "^${cluster_name}$"; then
    echo "${cluster_name} already exists, skipping"
  else
    echo "Creating ${cluster_name}..."
    kind create cluster --config "${config_file}"
  fi
}

if [[ "${MULTI_CLUSTER:-0}" == "1" ]]; then
  clusters=(csc cpc-1 cpc-2)
else
  clusters=(dsx-exchange)
fi

for cluster in "${clusters[@]}"; do
  create_cluster "${cluster}" "${PROJECT_ROOT}/infra/kind/${cluster}.yaml"
done

for cluster in "${clusters[@]}"; do
  kubectl wait --for=condition=Ready nodes --all --timeout=2m --context "kind-${cluster}"
done

echo "Clusters created successfully"

KIND_CLUSTERS="${clusters[*]}" "${SCRIPT_DIR}/setup-local-registry.sh"
