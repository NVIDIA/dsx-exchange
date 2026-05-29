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

if [ -z "${E2E_PREREQS_BIN:-}" ]; then
  go_bin="$(go env GOBIN 2>/dev/null || true)"
  go_path="$(go env GOPATH 2>/dev/null || true)"
  if [ -n "${go_bin}" ]; then
    E2E_PREREQS_BIN="${go_bin}"
  elif [ -n "${go_path}" ]; then
    E2E_PREREQS_BIN="${go_path}/bin"
  else
    E2E_PREREQS_BIN="${HOME}/.local/bin"
  fi
fi

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

mkdir -p "${E2E_PREREQS_BIN}"
export PATH="${E2E_PREREQS_BIN}:${PATH}"

tool_path() {
  command -v "$1" 2>/dev/null || true
}

warn_version_mismatch() {
  name="$1"
  expected="$2"
  actual="$3"
  path="$4"
  expected_without_v="${expected#v}"

  if [ -z "${actual}" ]; then
    echo "WARNING: ${name} already installed at ${path}; expected ${expected}, but version could not be detected" >&2
    return
  fi

  if ! printf '%s\n' "${actual}" | grep -Fq "${expected}" \
    && ! printf '%s\n' "${actual}" | grep -Fq "${expected_without_v}"
  then
    echo "WARNING: ${name} already installed at ${path}; expected ${expected}, found ${actual}" >&2
  fi
}

warn_command_version() {
  name="$1"
  expected="$2"
  path="$3"
  shift 3

  actual="$("${path}" "$@" 2>&1 | head -1 || true)"
  warn_version_mismatch "${name}" "${expected}" "${actual}" "${path}"
}

warn_go_module_version() {
  name="$1"
  expected="$2"
  path="$3"

  if ! command -v go >/dev/null 2>&1; then
    warn_version_mismatch "${name}" "${expected}" "" "${path}"
    return
  fi

  actual="$(go version -m "${path}" 2>/dev/null | awk '$1 == "mod" { print $3; exit }' || true)"
  warn_version_mismatch "${name}" "${expected}" "${actual}" "${path}"
}

install_binary_url() {
  name="$1"
  version="$2"
  url="$3"
  shift 3

  existing="$(tool_path "${name}")"
  if [ -n "${existing}" ] && [ -x "${existing}" ]; then
    echo "${name} already installed at ${existing}"
    warn_command_version "${name}" "${version}" "${existing}" "$@"
    return
  fi

  echo "Installing ${name} ${version} to ${E2E_PREREQS_BIN}/${name}"
  curl -fsSL -o "${tmp_dir}/${name}" "${url}"
  install -m 0755 "${tmp_dir}/${name}" "${E2E_PREREQS_BIN}/${name}"
}

install_helm() {
  existing="$(tool_path helm)"
  if [ -n "${existing}" ] && [ -x "${existing}" ]; then
    echo "helm already installed at ${existing}"
    warn_command_version helm "${HELM_VERSION}" "${existing}" version --short
    return
  fi

  echo "Installing helm ${HELM_VERSION} to ${E2E_PREREQS_BIN}/helm"
  curl -fsSL -o "${tmp_dir}/helm.tar.gz" \
    "https://get.helm.sh/helm-${HELM_VERSION}-${tool_os}-${tool_arch}.tar.gz"
  tar -xzf "${tmp_dir}/helm.tar.gz" -C "${tmp_dir}"
  install -m 0755 "${tmp_dir}/${tool_os}-${tool_arch}/helm" "${E2E_PREREQS_BIN}/helm"
}

install_go_tool() {
  name="$1"
  module="$2"
  version="$3"
  shift 3
  existing="$(tool_path "${name}")"
  if [ -n "${existing}" ] && [ -x "${existing}" ]; then
    echo "${name} already installed at ${existing}"
    if [ "$#" -gt 0 ]; then
      warn_command_version "${name}" "${version}" "${existing}" "$@"
    else
      warn_go_module_version "${name}" "${version}" "${existing}"
    fi
    return
  fi

  if ! command -v go >/dev/null 2>&1; then
    echo "ERROR: go is required to install ${name}" >&2
    exit 1
  fi

  echo "Installing ${name} ${version} to ${E2E_PREREQS_BIN}/${name}"
  GOBIN="${E2E_PREREQS_BIN}" go install "${module}@${version}"
}

install_binary_url kind "${KIND_VERSION}" \
  "https://kind.sigs.k8s.io/dl/${KIND_VERSION}/kind-${tool_os}-${tool_arch}" \
  version
install_binary_url kubectl "${KUBECTL_VERSION}" \
  "https://dl.k8s.io/release/${KUBECTL_VERSION}/bin/${tool_os}/${tool_arch}/kubectl" \
  version --client=true
install_helm
install_go_tool cfssl github.com/cloudflare/cfssl/cmd/cfssl "${CFSSL_VERSION}" version
install_go_tool cfssljson github.com/cloudflare/cfssl/cmd/cfssljson "${CFSSL_VERSION}" --version
install_go_tool nsc github.com/nats-io/nsc/v2 "${NSC_VERSION}" --version
install_go_tool nk github.com/nats-io/nkeys/nk "${NKEYS_VERSION}"
install_go_tool yq github.com/mikefarah/yq/v4 "${YQ_VERSION}" --version

kind version
kubectl version --client=true
helm version
cfssl version
nsc --version
nk -v
yq --version
