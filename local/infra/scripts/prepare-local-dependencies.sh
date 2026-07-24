#!/usr/bin/env bash
# Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

repo_root="$(git rev-parse --show-toplevel)"
cache_chart="$repo_root/local/infra/scripts/cache-helm-chart.sh"
cache_dir="$repo_root/.cache/helm"
event_bus_charts="$repo_root/deploy/nats-event-bus/charts"

"$cache_chart" gateway-crds-helm-v1.5.4.tgz oci://docker.io/envoyproxy/gateway-crds-helm v1.5.4
"$cache_chart" gateway-helm-v1.5.4.tgz oci://docker.io/envoyproxy/gateway-helm v1.5.4
"$cache_chart" metrics-server-3.13.0.tgz metrics-server 3.13.0 https://kubernetes-sigs.github.io/metrics-server/
"$cache_chart" metallb-0.15.2.tgz metallb 0.15.2 https://metallb.github.io/metallb
"$cache_chart" kube-prometheus-stack-86.1.0.tgz kube-prometheus-stack 86.1.0 https://prometheus-community.github.io/helm-charts
"$cache_chart" nats-2.12.6.tgz nats 2.12.6 https://nats-io.github.io/k8s/helm/charts/
"$cache_chart" nack-0.33.2.tgz nack 0.33.2 https://nats-io.github.io/k8s/helm/charts/
"$cache_chart" surveyor-0.20.7.tgz surveyor 0.20.7 https://nats-io.github.io/k8s/helm/charts/

mkdir -p "$event_bus_charts"
for archive in nats-2.12.6.tgz nack-0.33.2.tgz surveyor-0.20.7.tgz; do
  cmp -s "$cache_dir/$archive" "$event_bus_charts/$archive" ||
    cp "$cache_dir/$archive" "$event_bus_charts/$archive"
done

auth_callout="$event_bus_charts/auth-callout-0.1.1.tgz"
if [[ ! -s "$auth_callout" ]] ||
  [[ -n "$(find "$repo_root/auth-callout/deploy" -type f -not -path '*/charts/*' -newer "$auth_callout" -print -quit)" ]]; then
  helm package "$repo_root/auth-callout/deploy" --destination "$event_bus_charts" >/dev/null
fi
