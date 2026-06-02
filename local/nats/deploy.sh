#!/usr/bin/env bash
# Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

cluster="${1:-all}"

case "${cluster}" in
  all)
    module="nats-all"
    ;;
  csc|cpc-1|cpc-2)
    module="nats-${cluster}-local"
    ;;
  *)
    echo "usage: deploy.sh [all|csc|cpc-1|cpc-2]" >&2
    exit 1
    ;;
esac

"${SCRIPT_DIR}/../infra/scripts/run-skaffold.sh" "${module}"
