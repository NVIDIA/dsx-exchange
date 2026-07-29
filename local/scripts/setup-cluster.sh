#!/usr/bin/env bash
# Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
# SPDX-License-Identifier: Apache-2.0


set -Eeuo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
LOCAL_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"

# Check prerequisites
command -v kind >/dev/null 2>&1 || { echo "ERROR: kind is required but not installed" >&2; exit 1; }
command -v kubectl >/dev/null 2>&1 || { echo "ERROR: kubectl is required but not installed" >&2; exit 1; }
command -v docker >/dev/null 2>&1 || { echo "ERROR: docker is required but not installed" >&2; exit 1; }
command -v jq >/dev/null 2>&1 || { echo "ERROR: jq is required but not installed" >&2; exit 1; }
if [[ -n "${KIND_DIND_SERVICE_HOST:-}" ]]; then
  command -v yq >/dev/null 2>&1 || { echo "ERROR: yq is required for Kind in Docker CI" >&2; exit 1; }
fi

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

while IFS= read -r existing_cluster; do
  [[ -z "${existing_cluster}" ]] && continue
  if [[ "${existing_cluster}" != "dsx-exchange" ]]; then
    echo "ERROR: Kind cluster '${existing_cluster}' is already running" >&2
    echo "Run 'kind delete cluster --name ${existing_cluster}' before creating dsx-exchange." >&2
    exit 1
  fi
done < <(kind get clusters 2>/dev/null || true)

if [[ ! -f "${LOCAL_DIR}/infra/kind/dsx-exchange.yaml" ]]; then
  echo "ERROR: Kind config not found: ${LOCAL_DIR}/infra/kind/dsx-exchange.yaml" >&2
  exit 1
fi

if kind get clusters | grep -qx dsx-exchange; then
  echo "dsx-exchange already exists, skipping"
else
  echo "Creating dsx-exchange..."
  if [[ -z "${KIND_DIND_SERVICE_HOST:-}" ]]; then
    kind create cluster --config "${LOCAL_DIR}/infra/kind/dsx-exchange.yaml"
  else
    kind create cluster --config=<(yq eval '
      .networking.apiServerAddress = "0.0.0.0" |
      (.nodes[] | select(.role == "control-plane").kubeadmConfigPatches) += [
        "kind: ClusterConfiguration\napiServer:\n  certSANs:\n    - " +
        strenv(KIND_DIND_SERVICE_HOST) + "\n"
      ]
    ' "${LOCAL_DIR}/infra/kind/dsx-exchange.yaml")
  fi
fi

kind export kubeconfig --name dsx-exchange >/dev/null

if [[ -n "${KIND_DIND_SERVICE_HOST:-}" ]]; then
  api_port="$(yq -er '.networking.apiServerPort' "${LOCAL_DIR}/infra/kind/dsx-exchange.yaml")"
  kubectl config set-cluster kind-dsx-exchange \
    --server="https://${KIND_DIND_SERVICE_HOST}:${api_port}" >/dev/null
fi

kubectl wait --for=condition=Ready nodes --all --timeout=2m --context kind-dsx-exchange
echo "Cluster created successfully"
"${SCRIPT_DIR}/setup-local-registry.sh"
