#!/usr/bin/env bash
# Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

context="${1:?usage: wait-prometheus-crds.sh <kube-context>}"

kubectl --context "${context}" wait \
    --for=condition=Established \
    --timeout=120s \
    crd/servicemonitors.monitoring.coreos.com
