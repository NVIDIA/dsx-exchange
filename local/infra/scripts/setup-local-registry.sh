#!/usr/bin/env bash
# Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
LOCAL_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"
REGISTRY_NAME="kind-registry"
REGISTRY_PORT="5001"
REGISTRY_CONTAINER_PORT="5000"
REGISTRY_VOLUME="${REGISTRY_NAME}-zot-data"
REGISTRY_CONFIG="${LOCAL_DIR}/infra/local-registry/zot-config.json"
REGISTRY_HOSTS="${LOCAL_DIR}/infra/local-registry/hosts"
REGISTRY_CONFIG_HASH_LABEL="dev.dsx-exchange.local-registry.config-sha256"
REGISTRY_BIND_HOST="127.0.0.1"
REGISTRY_READY_HOST="127.0.0.1"
KIND_CLUSTERS="${KIND_CLUSTERS:-dsx-exchange}"

if [[ -n "${KIND_DIND_SERVICE_HOST:-}" ]]; then
  REGISTRY_BIND_HOST="0.0.0.0"
  REGISTRY_READY_HOST="${KIND_DIND_SERVICE_HOST}"
fi

for command in curl docker kind kubectl; do
  command -v "${command}" >/dev/null 2>&1 || {
    echo "ERROR: ${command} is required" >&2
    exit 1
  }
done

docker network inspect kind >/dev/null 2>&1 || {
  echo "ERROR: Docker network 'kind' does not exist. Create a Kind cluster first." >&2
  exit 1
}

registry_image() {
  case "$(docker info --format '{{.Architecture}}')" in
    amd64|x86_64) printf '%s\n' 'ghcr.io/project-zot/zot-linux-amd64:v2.1.10' ;;
    arm64|aarch64) printf '%s\n' 'ghcr.io/project-zot/zot-linux-arm64:v2.1.10' ;;
    *)
      echo "ERROR: unsupported Docker architecture: $(docker info --format '{{.Architecture}}')" >&2
      return 1
      ;;
  esac
}

hash_file() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "${REGISTRY_CONFIG}" | awk '{print $1}'
  else
    shasum -a 256 "${REGISTRY_CONFIG}" | awk '{print $1}'
  fi
}

registry_matches() {
  local image="$1"
  local config_hash="$2"

  [[ "$(docker inspect -f '{{.Config.Image}}' "${REGISTRY_NAME}")" == "${image}" &&
    "$(docker inspect -f '{{with index .NetworkSettings.Ports "5000/tcp"}}{{range .}}{{.HostIp}}:{{.HostPort}}{{end}}{{end}}' "${REGISTRY_NAME}")" == "${REGISTRY_BIND_HOST}:${REGISTRY_PORT}" &&
    "$(docker inspect -f "{{with .Config.Labels}}{{index . \"${REGISTRY_CONFIG_HASH_LABEL}\"}}{{end}}" "${REGISTRY_NAME}")" == "${config_hash}" ]]
}

image="$(registry_image)"
config_hash="$(hash_file)"

if docker inspect "${REGISTRY_NAME}" >/dev/null 2>&1 &&
  ! registry_matches "${image}" "${config_hash}"; then
  docker rm -f "${REGISTRY_NAME}" >/dev/null
fi

if ! docker inspect "${REGISTRY_NAME}" >/dev/null 2>&1; then
  docker create \
    --restart=always \
    --name "${REGISTRY_NAME}" \
    --network kind \
    -p "${REGISTRY_BIND_HOST}:${REGISTRY_PORT}:${REGISTRY_CONTAINER_PORT}" \
    --label "${REGISTRY_CONFIG_HASH_LABEL}=${config_hash}" \
    -v "${REGISTRY_VOLUME}:/var/lib/registry" \
    -v "${REGISTRY_CONFIG}:/etc/zot/config.json:ro" \
    "${image}" >/dev/null
fi

if [[ "$(docker inspect -f '{{.State.Running}}' "${REGISTRY_NAME}")" != "true" ]]; then
  docker start "${REGISTRY_NAME}" >/dev/null
fi

if [[ "$(docker inspect -f='{{json .NetworkSettings.Networks.kind}}' "${REGISTRY_NAME}")" == "null" ]]; then
  docker network connect kind "${REGISTRY_NAME}"
fi

curl --fail --silent --show-error \
  --retry 30 --retry-all-errors --retry-connrefused --retry-delay 1 \
  --connect-timeout 1 --max-time 5 \
  "http://${REGISTRY_READY_HOST}:${REGISTRY_PORT}/v2/" >/dev/null

for cluster in ${KIND_CLUSTERS}; do
  if ! kind get clusters 2>/dev/null | grep -qx "${cluster}"; then
    continue
  fi
  while IFS= read -r node; do
    docker exec "${node}" mkdir -p /etc/containerd/certs.d
    docker cp "${REGISTRY_HOSTS}/." "${node}:/etc/containerd/certs.d"
  done < <(kind get nodes --name "${cluster}")

  context="kind-${cluster}"
  kubectl delete --context "${context}" --namespace kube-public \
    configmap local-registry-hosting --ignore-not-found >/dev/null
done

echo "Persistent upstream image cache ready at localhost:${REGISTRY_PORT}"
