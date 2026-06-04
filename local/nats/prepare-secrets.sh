#!/usr/bin/env bash
# Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
MONOREPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"

command -v cfssl >/dev/null 2>&1 || { echo "ERROR: cfssl is required" >&2; exit 1; }
command -v cfssljson >/dev/null 2>&1 || { echo "ERROR: cfssljson is required" >&2; exit 1; }

with_lock() {
  local lock_dir="$1"
  shift

  mkdir -p "$(dirname "${lock_dir}")"
  while ! mkdir "${lock_dir}" 2>/dev/null; do
    sleep 1
  done

  trap 'rmdir "${lock_dir}" 2>/dev/null || true' EXIT INT TERM
  "$@"
  rmdir "${lock_dir}" 2>/dev/null || true
  trap - EXIT INT TERM
}

certs_ready() {
  [ -s "${SCRIPT_DIR}/certs/csc/server.pem" ] &&
    [ -s "${SCRIPT_DIR}/certs/cpc-1/server.pem" ] &&
    [ -s "${SCRIPT_DIR}/certs/cpc-2/server.pem" ]
}

generate_nkeys() {
  "${MONOREPO_ROOT}/deploy/scripts/generate-nkeys.sh" \
    -o "${SCRIPT_DIR}/secrets" \
    --extra-account LaunchLayer \
    1 2
}

generate_certs() {
  certs_ready && return 0

  "${SCRIPT_DIR}/gen-mtls-certs.sh"
}

with_lock "${SCRIPT_DIR}/secrets/.generate.lock" generate_nkeys
with_lock "${SCRIPT_DIR}/certs/.generate.lock" generate_certs

echo "NATS Event Bus secret files ready"
