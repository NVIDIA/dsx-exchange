#!/usr/bin/env bash
# Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

module="${1:?usage: run-skaffold.sh <module> [skaffold args...]}"
shift

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../../.." && pwd)"
SKAFFOLD_BIN="${SKAFFOLD:-skaffold}"

command -v "${SKAFFOLD_BIN}" >/dev/null 2>&1 || {
  echo "ERROR: skaffold is required. Run 'make -C local install-e2e-prereqs' first." >&2
  exit 1
}

cd "${REPO_ROOT}"
"${SKAFFOLD_BIN}" run --module "${module}" --cleanup=false "$@"
