#!/usr/bin/env bash
# Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

context="${1:?usage: wait-metallb.sh <kube-context>}"

kubectl wait \
  --for=condition=Established \
  crd/ipaddresspools.metallb.io \
  crd/l2advertisements.metallb.io \
  --timeout=60s \
  --context "${context}"

for i in {1..60}; do
  webhook_addresses="$(
    kubectl get endpoints metallb-webhook-service \
      -n metallb-system \
      --context "${context}" \
      -o jsonpath='{.subsets[*].addresses[*].ip}' 2>/dev/null || true
  )"
  if [ -n "${webhook_addresses}" ]; then
    break
  fi
  if [ "${i}" -eq 60 ]; then
    echo "ERROR: MetalLB webhook endpoint not ready in ${context}" >&2
    exit 1
  fi
  sleep 1
done

kubectl rollout status deployment/metallb-controller \
  -n metallb-system \
  --timeout=5m \
  --context "${context}"
kubectl rollout status daemonset/metallb-speaker \
  -n metallb-system \
  --timeout=5m \
  --context "${context}"
