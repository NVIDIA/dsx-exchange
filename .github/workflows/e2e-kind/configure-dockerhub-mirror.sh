#!/usr/bin/env bash
# Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

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
  local tmp_dir

  tmp_dir="$(mktemp -d)"
  trap 'rm -rf "${tmp_dir}"' RETURN
  current_config="${tmp_dir}/daemon.json"
  merged_config="${tmp_dir}/daemon.merged.json"

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

require_command "docker"
require_command "jq"
require_command "sudo"

configure_host_docker_mirror
