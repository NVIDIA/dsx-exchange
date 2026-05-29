#!/bin/bash
# Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
# SPDX-License-Identifier: Apache-2.0


set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
KIND_CONFIG_DIR=""

# Check prerequisites
command -v kind >/dev/null 2>&1 || { echo "ERROR: kind is required but not installed" >&2; exit 1; }
command -v kubectl >/dev/null 2>&1 || { echo "ERROR: kubectl is required but not installed" >&2; exit 1; }
command -v docker >/dev/null 2>&1 || { echo "ERROR: docker is required but not installed" >&2; exit 1; }
command -v jq >/dev/null 2>&1 || { echo "ERROR: jq is required but not installed" >&2; exit 1; }

cleanup() {
  if [ -n "${KIND_CONFIG_DIR}" ] && [ -d "${KIND_CONFIG_DIR}" ]; then
    rm -rf "${KIND_CONFIG_DIR}"
  fi
}

trap cleanup EXIT

normalize_dockerhub_mirror() {
  local mirror="${KIND_DOCKERHUB_MIRROR:-}"

  mirror="${mirror%/}"
  if [ -n "${mirror}" ]; then
    case "${mirror}" in
      http://*|https://*) ;;
      *) mirror="https://${mirror}" ;;
    esac
  fi

  printf '%s' "${mirror}"
}

DOCKERHUB_MIRROR_ENDPOINT="$(normalize_dockerhub_mirror)"
DOCKERHUB_MIRROR_HOST="${DOCKERHUB_MIRROR_ENDPOINT#http://}"
DOCKERHUB_MIRROR_HOST="${DOCKERHUB_MIRROR_HOST#https://}"

preload_dockerhub_image() {
  local image="$1"
  local mirror_path="${image}"
  local mirrored_image

  if [ -z "${DOCKERHUB_MIRROR_HOST}" ]; then
    return 0
  fi

  if docker image inspect "${image}" >/dev/null 2>&1; then
    echo "${image} already exists locally, skipping mirror pull"
    return 0
  fi

  case "${mirror_path}" in
    */*) ;;
    *) mirror_path="library/${mirror_path}" ;;
  esac

  mirrored_image="${DOCKERHUB_MIRROR_HOST}/${mirror_path}"
  echo "Preloading ${image} from Docker Hub mirror ${DOCKERHUB_MIRROR_HOST}..."
  docker pull "${mirrored_image}"
  docker tag "${mirrored_image}" "${image}"
}

preload_kind_node_images() {
  if [ -z "${DOCKERHUB_MIRROR_HOST}" ]; then
    return 0
  fi

  grep -h '^[[:space:]]*image:' "${PROJECT_ROOT}"/infra/kind/*.yaml \
    | awk '{print $2}' \
    | sort -u \
    | while IFS= read -r image; do
        preload_dockerhub_image "${image}"
      done
}

kind_config_with_mirror() {
  local config_file="$1"
  local output_file

  if [ -z "${DOCKERHUB_MIRROR_ENDPOINT}" ]; then
    printf '%s' "${config_file}"
    return 0
  fi

  if [ -z "${KIND_CONFIG_DIR}" ]; then
    KIND_CONFIG_DIR="$(mktemp -d)"
  fi

  output_file="${KIND_CONFIG_DIR}/$(basename "${config_file}")"
  cp "${config_file}" "${output_file}"
  cat >> "${output_file}" <<EOF
containerdConfigPatches:
- |-
  [plugins."io.containerd.grpc.v1.cri".registry.mirrors."docker.io"]
    endpoint = ["${DOCKERHUB_MIRROR_ENDPOINT}"]
EOF

  printf '%s' "${output_file}"
}

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

echo "Creating clusters in parallel..."

csc_config="$(kind_config_with_mirror "$PROJECT_ROOT/infra/kind/csc.yaml")"
cpc1_config="$(kind_config_with_mirror "$PROJECT_ROOT/infra/kind/cpc-1.yaml")"
cpc2_config="$(kind_config_with_mirror "$PROJECT_ROOT/infra/kind/cpc-2.yaml")"

preload_kind_node_images

# Function to create a cluster
create_cluster() {
  local cluster_name=$1
  local config_file=$2

  if kind get clusters | grep -q "^${cluster_name}$"; then
    echo "${cluster_name} already exists, skipping"
  else
    echo "Creating ${cluster_name}..."
    kind create cluster --config "${config_file}"
  fi
}

pids=()

# Create all clusters in parallel
create_cluster "csc" "$csc_config" &
pids+=("$!")
create_cluster "cpc-1" "$cpc1_config" &
pids+=("$!")
create_cluster "cpc-2" "$cpc2_config" &
pids+=("$!")

# Wait for all cluster creations to complete
for pid in "${pids[@]}"; do
  wait "${pid}"
done

# Wait for all clusters to be ready (in parallel)
echo "Waiting for clusters to be ready..."
pids=()
kubectl wait --for=condition=Ready nodes --all --timeout=2m --context "kind-csc" &
pids+=("$!")
kubectl wait --for=condition=Ready nodes --all --timeout=2m --context "kind-cpc-1" &
pids+=("$!")
kubectl wait --for=condition=Ready nodes --all --timeout=2m --context "kind-cpc-2" &
pids+=("$!")

for pid in "${pids[@]}"; do
  wait "${pid}"
done

echo "Clusters created successfully"
