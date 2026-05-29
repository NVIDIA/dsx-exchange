#!/usr/bin/env bash
# Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

KIND_VERSION="${KIND_VERSION:-v0.31.0}"
KUBECTL_VERSION="${KUBECTL_VERSION:-v1.31.9}"
HELM_VERSION="${HELM_VERSION:-v4.2.0}"
CFSSL_VERSION="${CFSSL_VERSION:-v1.6.5}"
NSC_VERSION="${NSC_VERSION:-v2.14.0}"
NKEYS_VERSION="${NKEYS_VERSION:-v0.4.15}"
YQ_VERSION="${YQ_VERSION:-v4.53.2}"
E2E_TOOLS_BIN="${E2E_TOOLS_BIN:-$(pwd)/tmp/bin}"

tool_os="$(uname -s | tr '[:upper:]' '[:lower:]')"
machine="$(uname -m)"

case "${machine}" in
  x86_64|amd64) tool_arch="amd64" ;;
  arm64|aarch64) tool_arch="arm64" ;;
  *)
    echo "ERROR: unsupported architecture: ${machine}" >&2
    exit 1
    ;;
esac

case "${tool_os}" in
  linux|darwin) ;;
  *)
    echo "ERROR: unsupported OS: ${tool_os}" >&2
    exit 1
    ;;
esac

tmp_dir="$(mktemp -d)"
trap 'rm -rf "${tmp_dir}"' EXIT

mkdir -p "${E2E_TOOLS_BIN}"

curl -fsSL -o "${E2E_TOOLS_BIN}/kind" \
  "https://kind.sigs.k8s.io/dl/${KIND_VERSION}/kind-${tool_os}-${tool_arch}"
chmod +x "${E2E_TOOLS_BIN}/kind"

curl -fsSL -o "${E2E_TOOLS_BIN}/kubectl" \
  "https://dl.k8s.io/release/${KUBECTL_VERSION}/bin/${tool_os}/${tool_arch}/kubectl"
chmod +x "${E2E_TOOLS_BIN}/kubectl"

curl -fsSL -o "${tmp_dir}/helm.tar.gz" \
  "https://get.helm.sh/helm-${HELM_VERSION}-${tool_os}-${tool_arch}.tar.gz"
tar -xzf "${tmp_dir}/helm.tar.gz" -C "${tmp_dir}"
install -m 0755 "${tmp_dir}/${tool_os}-${tool_arch}/helm" "${E2E_TOOLS_BIN}/helm"

GOBIN="${E2E_TOOLS_BIN}" go install "github.com/cloudflare/cfssl/cmd/cfssl@${CFSSL_VERSION}"
GOBIN="${E2E_TOOLS_BIN}" go install "github.com/cloudflare/cfssl/cmd/cfssljson@${CFSSL_VERSION}"
GOBIN="${E2E_TOOLS_BIN}" go install "github.com/nats-io/nsc/v2@${NSC_VERSION}"
GOBIN="${E2E_TOOLS_BIN}" go install "github.com/nats-io/nkeys/nk@${NKEYS_VERSION}"
GOBIN="${E2E_TOOLS_BIN}" go install "github.com/mikefarah/yq/v4@${YQ_VERSION}"

"${E2E_TOOLS_BIN}/kind" version
"${E2E_TOOLS_BIN}/kubectl" version --client=true
"${E2E_TOOLS_BIN}/helm" version
"${E2E_TOOLS_BIN}/cfssl" version
"${E2E_TOOLS_BIN}/nsc" --version
"${E2E_TOOLS_BIN}/nk" -v
"${E2E_TOOLS_BIN}/yq" --version
