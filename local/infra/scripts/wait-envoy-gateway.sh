#!/usr/bin/env bash
# Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

context="${1:?usage: wait-envoy-gateway.sh <kube-context>}"

kubectl wait \
  --for=condition=Established \
  crd/gatewayclasses.gateway.networking.k8s.io \
  crd/gateways.gateway.networking.k8s.io \
  crd/httproutes.gateway.networking.k8s.io \
  crd/tcproutes.gateway.networking.k8s.io \
  crd/tlsroutes.gateway.networking.k8s.io \
  crd/backendtrafficpolicies.gateway.envoyproxy.io \
  --timeout=90s \
  --context "${context}"

kubectl rollout status deployment/envoy-gateway \
  -n envoy-gateway-system \
  --timeout=5m \
  --context "${context}"
