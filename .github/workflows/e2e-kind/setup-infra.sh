#!/usr/bin/env bash
# Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

repo_root="$(git rev-parse --show-toplevel)"
local_dir="${repo_root}/local"

setup_kind_network() {
  local current_subnet
  local existing_clusters

  echo "Configuring Kind Docker network..."

  if docker network inspect kind >/dev/null 2>&1; then
    current_subnet="$(
      docker network inspect kind |
        jq -r '.[0].IPAM.Config[] | select(.Subnet | contains(".")) | .Subnet'
    )"

    if [ "${current_subnet}" = "172.18.0.0/16" ]; then
      echo "Kind network already configured with 172.18.0.0/16"
      return
    fi

    echo "WARNING: Existing Kind network uses ${current_subnet}, but 172.18.0.0/16 is required"
    existing_clusters="$(kind get clusters 2>/dev/null | wc -l | tr -d ' ')"
    if [ "${existing_clusters}" -gt 0 ]; then
      echo "ERROR: Cannot change network subnet while clusters exist" >&2
      kind get clusters | sed 's/^/  - /' >&2
      exit 1
    fi

    echo "Removing existing Kind network..."
    docker network rm kind
  fi

  echo "Creating Kind network with subnet 172.18.0.0/16..."
  docker network create \
    --driver bridge \
    --subnet=172.18.0.0/16 \
    --gateway=172.18.0.1 \
    kind
}

configure_inotify_limits() {
  local sysctl_cmd=(sysctl -w fs.inotify.max_user_instances=8192)

  echo "Configuring inotify limits for multi-cluster Kind..."
  if command -v sudo >/dev/null 2>&1; then
    sudo "${sysctl_cmd[@]}"
  else
    "${sysctl_cmd[@]}"
  fi
}

create_cluster() {
  local cluster_name=$1
  local config_file=$2
  local attempt
  local context="kind-${cluster_name}"
  local max_attempts=3

  if kind get clusters | grep -q "^${cluster_name}$"; then
    echo "${cluster_name} already exists, skipping"
  else
    for attempt in $(seq 1 "${max_attempts}"); do
      echo "Creating ${cluster_name} (attempt ${attempt}/${max_attempts})..."
      if kind create cluster --retain --config "${config_file}"; then
        break
      fi

      echo "::warning::Kind failed to create ${cluster_name} on attempt ${attempt}"
      collect_kind_logs "${cluster_name}" "${attempt}"
      kind delete cluster --name "${cluster_name}" || true

      if [ "${attempt}" -eq "${max_attempts}" ]; then
        echo "ERROR: failed to create ${cluster_name} after ${max_attempts} attempts" >&2
        exit 1
      fi

      sleep "$((attempt * 15))"
    done
  fi

  echo "Waiting for ${cluster_name} nodes to be ready..."
  kubectl wait --for=condition=Ready nodes --all --timeout=4m --context "${context}"
}

collect_kind_logs() {
  local cluster_name=$1
  local attempt=$2
  local log_dir="${RUNNER_TEMP:-/tmp}/kind-logs/${cluster_name}-attempt-${attempt}"

  mkdir -p "${log_dir}"
  kind export logs --name "${cluster_name}" "${log_dir}" || true

  if [ -d "${log_dir}" ]; then
    echo "Filtered Kind logs from ${log_dir}:"
    grep -RniE 'error|failed|too many open|cgroup|multi-user|kubelet|containerd' "${log_dir}" |
      head -200 || true
  fi
}

configure_inotify_limits
setup_kind_network

echo "Creating clusters sequentially for CI runner stability..."
create_cluster "csc" "${local_dir}/infra/kind/csc.yaml"
create_cluster "cpc-1" "${local_dir}/infra/kind/cpc-1.yaml"
create_cluster "cpc-2" "${local_dir}/infra/kind/cpc-2.yaml"

echo "Installing shared local infrastructure..."
make -C "${local_dir}" setup-metallb
make -C "${local_dir}" setup-envoy-gateway
make -C "${local_dir}" setup-cert-manager
make -C "${local_dir}" setup-metrics-server
make -C "${local_dir}" setup-observability
make -C "${local_dir}" setup-keycloak

echo "Infrastructure setup complete"
