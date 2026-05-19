#!/bin/bash
# Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
# SPDX-License-Identifier: Apache-2.0

#
# Deploy Prometheus Operator and kube-prometheus-stack to all clusters
# This provides ServiceMonitor CRDs required by NATS Surveyor

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

command -v kubectl >/dev/null 2>&1 || { echo "ERROR: kubectl is required" >&2; exit 1; }
command -v helm >/dev/null 2>&1 || { echo "ERROR: helm is required" >&2; exit 1; }

CLUSTERS="csc cpc-1 cpc-2"

# Add Prometheus Helm repository
helm repo add prometheus-community https://prometheus-community.github.io/helm-charts 2>/dev/null || true
helm repo update >/dev/null

deploy_to_cluster() {
  local cluster="$1"
  local context="kind-${cluster}"

  echo "Deploying observability stack to ${cluster}..."

  # Create monitoring namespace
  kubectl create namespace monitoring --context "$context" --dry-run=client -o yaml | kubectl apply -f - --context "$context"

  # Install kube-prometheus-stack
  helm upgrade --install prometheus prometheus-community/kube-prometheus-stack \
      --namespace monitoring \
      --values "$PROJECT_ROOT/infra/prometheus/values.yaml" \
      --kube-context "$context" \
      --wait \
      --timeout 2m

  echo "Observability stack deployed to ${cluster}"
}

# Deploy to all clusters in parallel
pids=()

for cluster in ${CLUSTERS}; do
  deploy_to_cluster "${cluster}" &
  pids+=("$!")
done

for pid in "${pids[@]}"; do
  wait "${pid}"
done

echo ""
echo "Observability stack deployed to all clusters"
echo "Grafana: kubectl port-forward -n monitoring svc/prometheus-grafana 3000:80 --context kind-csc"
echo "Prometheus: kubectl port-forward -n monitoring svc/prometheus-kube-prometheus-prometheus 9090:9090 --context kind-csc"
