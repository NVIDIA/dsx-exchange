#!/bin/bash
# Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
# SPDX-License-Identifier: Apache-2.0


set -Eeuo pipefail

repo_root="$(git rev-parse --show-toplevel)"

echo "Deleting DSX Exchange Kind cluster..."
if kind get clusters 2>/dev/null | grep -q "^dsx-exchange$"; then
  kind delete cluster --name dsx-exchange
fi

rm -rf \
  "${repo_root}/local/event-bus/keys" \
  "${repo_root}/local/event-bus/nsc" \
  "${repo_root}/local/event-bus/certs" \
  "${repo_root}/local/event-bus/secrets" \
  "${repo_root}/local/idp/secret-generator/generated"

echo "Cleanup complete"
