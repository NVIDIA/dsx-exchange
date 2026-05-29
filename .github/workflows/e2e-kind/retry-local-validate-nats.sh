#!/usr/bin/env bash
# Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

max_attempts="${VALIDATE_NATS_ATTEMPTS:-5}"

case "${max_attempts}" in
  ""|*[!0-9]*)
    echo "ERROR: VALIDATE_NATS_ATTEMPTS must be a positive integer" >&2
    exit 1
    ;;
esac

if [ "${max_attempts}" -lt 1 ]; then
  echo "ERROR: VALIDATE_NATS_ATTEMPTS must be a positive integer" >&2
  exit 1
fi

for attempt in $(seq 1 "${max_attempts}"); do
  echo "Running local validate-nats (attempt ${attempt}/${max_attempts})..."
  if make -C local validate-nats; then
    exit 0
  fi

  if [ "${attempt}" -eq "${max_attempts}" ]; then
    echo "ERROR: local validate-nats failed after ${max_attempts} attempts" >&2
    exit 1
  fi

  echo "::warning::local validate-nats failed on attempt ${attempt}; waiting before retry"
  sleep "$((attempt * 15))"
done
