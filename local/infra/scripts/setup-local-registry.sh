#!/usr/bin/env bash
# Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

REGISTRY_NAME="kind-registry"
REGISTRY_PORT="5001"
REGISTRY_CONTAINER_PORT="5000"

command -v docker >/dev/null 2>&1 || { echo "ERROR: docker is required" >&2; exit 1; }
command -v kubectl >/dev/null 2>&1 || { echo "ERROR: kubectl is required" >&2; exit 1; }

if ! docker network inspect kind >/dev/null 2>&1; then
  echo "ERROR: Docker network 'kind' does not exist. Run setup-clusters first." >&2
  exit 1
fi

if ! docker inspect "${REGISTRY_NAME}" >/dev/null 2>&1; then
  docker run \
    -d \
    --restart=always \
    -p "127.0.0.1:${REGISTRY_PORT}:${REGISTRY_CONTAINER_PORT}" \
    --name "${REGISTRY_NAME}" \
    registry:2
elif [ "$(docker inspect -f '{{.State.Running}}' "${REGISTRY_NAME}")" != "true" ]; then
  docker start "${REGISTRY_NAME}" >/dev/null
fi

published_port="$(
  docker inspect -f '{{with index .NetworkSettings.Ports "5000/tcp"}}{{range .}}{{.HostIp}}:{{.HostPort}}{{end}}{{end}}' "${REGISTRY_NAME}"
)"

if [ "${published_port}" != "127.0.0.1:${REGISTRY_PORT}" ]; then
  echo "ERROR: ${REGISTRY_NAME} publishes ${published_port:-no host port}, expected 127.0.0.1:${REGISTRY_PORT}" >&2
  echo "Remove or rename the existing container, then rerun setup-clusters." >&2
  exit 1
fi

if [ "$(docker inspect -f='{{json .NetworkSettings.Networks.kind}}' "${REGISTRY_NAME}")" = "null" ]; then
  docker network connect kind "${REGISTRY_NAME}"
fi

for context in kind-csc kind-cpc-1 kind-cpc-2; do
  if kubectl cluster-info --context "${context}" >/dev/null 2>&1; then
    kubectl apply --context "${context}" -f - <<EOF
apiVersion: v1
kind: ConfigMap
metadata:
  name: local-registry-hosting
  namespace: kube-public
data:
  localRegistryHosting.v1: |
    host: "localhost:${REGISTRY_PORT}"
    help: "https://kind.sigs.k8s.io/docs/user/local-registry/"
EOF
  fi
done

echo "Local registry ready at localhost:${REGISTRY_PORT}"
