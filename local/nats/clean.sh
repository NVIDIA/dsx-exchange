#!/bin/bash
# Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
# SPDX-License-Identifier: Apache-2.0


set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cluster=${1:-all}

clean_namespace() {
  local context=$1
  echo "Cleaning NATS namespace in ${context}..."

  # Delete Stream resources
  kubectl delete streams --all -n event-bus --context "${context}" --ignore-not-found=true --force

  # Delete namespace
  kubectl delete namespace event-bus --context "${context}" --ignore-not-found=true
}

if [ "${cluster}" = "all" ]; then
  for c in csc cpc-1 cpc-2; do
    if kind get clusters 2>/dev/null | grep -q "^${c}$"; then
      clean_namespace "kind-${c}" &
    fi
  done
  wait

  # Remove all generated keys, nsc data, certificates, and secrets
  echo "Removing generated keys, nsc data, certificates, and secrets..."
  rm -rf "${SCRIPT_DIR}/keys" "${SCRIPT_DIR}/nsc" "${SCRIPT_DIR}/certs" "${SCRIPT_DIR}/secrets"
else
  clean_namespace "kind-${cluster}"

  # Remove cluster-specific keys, nsc data, certificates, and secrets
  echo "Removing generated keys, nsc data, certificates, and secrets for ${cluster}..."
  rm -rf "${SCRIPT_DIR}/keys/${cluster}" "${SCRIPT_DIR}/nsc/${cluster}" "${SCRIPT_DIR}/nsc/${cluster}-mtls" "${SCRIPT_DIR}/certs/${cluster}" "${SCRIPT_DIR}/secrets/${cluster}"
fi

echo "NATS namespace cleanup complete"
