#!/usr/bin/env bash
# Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

context="${1:?usage: wait-shared-gateway.sh <kube-context>}"

kubectl wait \
  --for=condition=Programmed \
  gateway/shared-gateway \
  -n envoy-gateway-system \
  --timeout=5m \
  --context "${context}"
