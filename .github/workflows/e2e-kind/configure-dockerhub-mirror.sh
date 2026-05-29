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
escaped_mirror_host="${mirror_host//&/\\&}"

cd "${repo_root}"

echo "Routing Kind node image pulls through Docker Hub mirror..."
sed -i "s#image: kindest/node:#image: ${escaped_mirror_host}/kindest/node:#g" \
  local/infra/kind/*.yaml

echo "Adding Kind containerd Docker Hub mirror patches..."
for config in local/infra/kind/*.yaml; do
  if grep -q '^containerdConfigPatches:' "${config}"; then
    echo "::error::${config} already defines containerdConfigPatches; update the CI mirror override"
    exit 1
  fi

  cat >> "${config}" <<EOF

containerdConfigPatches:
- |-
  [plugins."io.containerd.grpc.v1.cri".registry.mirrors."docker.io"]
    endpoint = ["${mirror}"]
EOF
done

echo "Routing Envoy Gateway OCI chart pulls through Docker Hub mirror..."
sed -i "s#oci://docker.io/#oci://${escaped_mirror_host}/#g" \
  local/infra/scripts/setup-envoy-gateway.sh

echo "Routing auth-callout builder image through Docker Hub mirror..."
sed -i "s#ARG BUILDER_IMG=golang#ARG BUILDER_IMG=${escaped_mirror_host}/library/golang#g" \
  auth-callout/build/package/Dockerfile
