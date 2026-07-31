#!/usr/bin/env bash
# Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
# SPDX-License-Identifier: Apache-2.0

set -Eeuo pipefail

repo_root="$(git rev-parse --show-toplevel)"
cache_chart="${repo_root}/local/scripts/cache-helm-chart.sh"
cache_file="${repo_root}/local/scripts/cache-http-file.sh"
cache_dir="${repo_root}/.cache/helm"

"${cache_chart}" gateway-crds-helm-v1.8.3.tgz \
  oci://docker.io/envoyproxy/gateway-crds-helm v1.8.3
"${cache_chart}" gateway-helm-v1.8.3.tgz \
  oci://docker.io/envoyproxy/gateway-helm v1.8.3
"${cache_chart}" metrics-server-3.13.0.tgz \
  metrics-server 3.13.0 https://kubernetes-sigs.github.io/metrics-server/
"${cache_chart}" metallb-0.15.2.tgz \
  metallb 0.15.2 https://metallb.github.io/metallb
"${cache_chart}" kube-prometheus-stack-86.1.0.tgz \
  kube-prometheus-stack 86.1.0 https://prometheus-community.github.io/helm-charts
"${cache_chart}" \
  prometheus-operator-crds-30.0.1.tgz \
  prometheus-operator-crds \
  30.0.1 \
  https://prometheus-community.github.io/helm-charts
"${cache_file}" \
  manifests/gateway-api-experimental-v1.5.1.yaml \
  https://github.com/kubernetes-sigs/gateway-api/releases/download/v1.5.1/experimental-install.yaml \
  64ec76609a6ac885e0405dea79ca509c229fa019d342f0857aa8b6bdc8b8ba92
"${cache_file}" manifests/cert-manager-v1.16.2.yaml \
  https://github.com/cert-manager/cert-manager/releases/download/v1.16.2/cert-manager.yaml \
  1d51cdecd442f1f5f89783e9e0169b95d372724da203cc75dd7a5c4e50a10ce6
if [[ -d "${repo_root}/deploy/nats-event-bus" ]]; then
  event_bus_charts="${repo_root}/deploy/nats-event-bus/charts"
  "${cache_chart}" nats-2.12.6.tgz nats 2.12.6 https://nats-io.github.io/k8s/helm/charts/
  "${cache_chart}" nack-0.33.2.tgz nack 0.33.2 https://nats-io.github.io/k8s/helm/charts/
  "${cache_chart}" surveyor-0.20.7.tgz surveyor 0.20.7 https://nats-io.github.io/k8s/helm/charts/

  mkdir -p "${event_bus_charts}"
  for archive in nats-2.12.6.tgz nack-0.33.2.tgz surveyor-0.20.7.tgz; do
    cmp -s "${cache_dir}/${archive}" "${event_bus_charts}/${archive}" ||
      cp "${cache_dir}/${archive}" "${event_bus_charts}/${archive}"
  done

  auth_callout="${event_bus_charts}/auth-callout-0.1.1.tgz"
  if [[ ! -s "${auth_callout}" ]] ||
    [[ -n "$(find "${repo_root}/auth-callout/deploy" -type f -not -path '*/charts/*' -newer "${auth_callout}" -print -quit)" ]]; then
    helm package "${repo_root}/auth-callout/deploy" --destination "${event_bus_charts}" >/dev/null
  fi
fi

if [[ -d "${repo_root}/deploy/dsx-agent-gateway" ]]; then
  gateway_charts="${repo_root}/deploy/dsx-agent-gateway/charts"
  "${cache_chart}" nats-2.14.2.tgz nats 2.14.2 https://nats-io.github.io/k8s/helm/charts/
  "${cache_chart}" opentelemetry-collector-0.164.1.tgz opentelemetry-collector 0.164.1 https://open-telemetry.github.io/opentelemetry-helm-charts
  "${cache_chart}" opentelemetry-operator-0.119.0.tgz opentelemetry-operator 0.119.0 https://open-telemetry.github.io/opentelemetry-helm-charts
  "${cache_chart}" agentgateway-crds-v1.3.1.tgz oci://cr.agentgateway.dev/charts/agentgateway-crds v1.3.1
  "${cache_chart}" agentgateway-v1.3.1.tgz oci://cr.agentgateway.dev/charts/agentgateway v1.3.1
  "${cache_chart}" valkey-0.9.4.tgz valkey 0.9.4 https://valkey-io.github.io/valkey-helm

  mkdir -p "${gateway_charts}"
  for archive in agentgateway-v1.3.1.tgz valkey-0.9.4.tgz; do
    cmp -s "${cache_dir}/${archive}" "${gateway_charts}/${archive}" ||
      cp "${cache_dir}/${archive}" "${gateway_charts}/${archive}"
  done
fi
