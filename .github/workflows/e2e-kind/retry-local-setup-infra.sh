#!/usr/bin/env bash
# Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

max_attempts="${SETUP_INFRA_ATTEMPTS:-3}"

case "${max_attempts}" in
  ""|*[!0-9]*)
    echo "ERROR: SETUP_INFRA_ATTEMPTS must be a positive integer" >&2
    exit 1
    ;;
esac

if [ "${max_attempts}" -lt 1 ]; then
  echo "ERROR: SETUP_INFRA_ATTEMPTS must be a positive integer" >&2
  exit 1
fi

for attempt in $(seq 1 "${max_attempts}"); do
  echo "Running local setup-infra (attempt ${attempt}/${max_attempts})..."
  if make -C local setup-infra; then
    exit 0
  fi

  if [ "${attempt}" -eq "${max_attempts}" ]; then
    echo "ERROR: local setup-infra failed after ${max_attempts} attempts" >&2
    exit 1
  fi

  echo "::warning::local setup-infra failed on attempt ${attempt}; cleaning e2e state before retry"
  make clean-e2e || true
  sleep "$((attempt * 15))"
done
