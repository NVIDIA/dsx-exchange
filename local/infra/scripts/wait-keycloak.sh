#!/usr/bin/env bash
# Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

context="${1:?usage: wait-keycloak.sh <kube-context>}"

kubectl wait \
  --for=condition=ready \
  keycloak/keycloak \
  -n keycloak \
  --timeout=10m \
  --context "${context}"
kubectl wait \
  --for=condition=Programmed \
  gateway/shared-gateway \
  -n envoy-gateway-system \
  --timeout=2m \
  --context "${context}"
