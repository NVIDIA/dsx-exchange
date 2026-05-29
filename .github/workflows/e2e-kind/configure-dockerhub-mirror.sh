#!/usr/bin/env bash
# Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

repo_root="$(git rev-parse --show-toplevel)"
buildkit_config="/etc/buildkit/buildkitd.toml"
mirror="${DOCKERHUB_MIRROR:-}"

if [ -z "${mirror}" ] && [ -f "${buildkit_config}" ]; then
  mirror="$(
    awk -F"'" '/mirrors =/ { print $2; exit }' "${buildkit_config}" || true
  )"
fi

mirror="${mirror%/}"

if [ -z "${mirror}" ]; then
  echo "::error::Docker Hub mirror not configured"
  exit 1
fi

case "${mirror}" in
  http://*|https://*) ;;
  *) mirror="https://${mirror}" ;;
esac

mirror_host="${mirror#http://}"
mirror_host="${mirror_host#https://}"
mirror_host="${mirror_host%/}"

cd "${repo_root}"

require_command() {
  local command_name="$1"

  if ! command -v "${command_name}" >/dev/null 2>&1; then
    echo "::error::required command not found: ${command_name}"
    exit 1
  fi
}

restart_docker() {
  if command -v systemctl >/dev/null 2>&1; then
    sudo systemctl restart docker
  else
    sudo service docker restart
  fi
}

configure_host_docker_mirror() {
  local daemon_config="/etc/docker/daemon.json"
  local current_config
  local merged_config

  current_config="$(mktemp)"
  merged_config="$(mktemp)"

  if sudo test -f "${daemon_config}"; then
    sudo cat "${daemon_config}" > "${current_config}"
  else
    printf '{}\n' > "${current_config}"
  fi

  jq --arg mirror "${mirror}" \
    '.["registry-mirrors"] = (((.["registry-mirrors"] // []) + [$mirror]) | unique)' \
    "${current_config}" > "${merged_config}"

  if sudo test -f "${daemon_config}" && sudo cmp -s "${merged_config}" "${daemon_config}"; then
    echo "Docker daemon already has Docker Hub mirror ${mirror}"
    return
  fi

  echo "Configuring Docker daemon Docker Hub mirror..."
  sudo mkdir -p "$(dirname "${daemon_config}")"
  sudo install -m 0644 "${merged_config}" "${daemon_config}"
  restart_docker
  docker info >/dev/null
}

configure_kind_containerd_mirror() {
  local source_config
  local config
  local kind_config_dir

  kind_config_dir="${RUNNER_TEMP:-/tmp}/dsx-e2e-kind-config"
  mkdir -p "${kind_config_dir}"

  for source_config in local/infra/kind/*.yaml; do
    config="${kind_config_dir}/$(basename "${source_config}")"
    cp "${source_config}" "${config}"

    if grep -q '^containerdConfigPatches:' "${config}"; then
      echo "::error::${source_config} already defines containerdConfigPatches; update the CI mirror override"
      exit 1
    fi

    cat >> "${config}" <<EOF

containerdConfigPatches:
- |-
  [plugins."io.containerd.grpc.v1.cri".registry.mirrors."docker.io"]
    endpoint = ["${mirror}"]
EOF
  done

  if [ -z "${GITHUB_ENV:-}" ]; then
    echo "::error::GITHUB_ENV is required to pass KIND_CONFIG_DIR to later CI steps"
    exit 1
  fi

  echo "KIND_CONFIG_DIR=${kind_config_dir}" >> "${GITHUB_ENV}"
}

configure_envoy_gateway_mirror() {
  if [ -z "${GITHUB_ENV:-}" ]; then
    echo "::error::GITHUB_ENV is required to pass ENVOY_GATEWAY_OCI_REGISTRY to later CI steps"
    exit 1
  fi

  echo "ENVOY_GATEWAY_OCI_REGISTRY=${mirror_host}" >> "${GITHUB_ENV}"
}

require_command "docker"
require_command "jq"
require_command "sudo"

configure_host_docker_mirror

echo "Writing CI Kind configs with Docker Hub containerd mirror patches..."
configure_kind_containerd_mirror

echo "Routing Envoy Gateway OCI chart pulls through Docker Hub mirror..."
configure_envoy_gateway_mirror
