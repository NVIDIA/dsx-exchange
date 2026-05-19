#!/bin/bash
# Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
# SPDX-License-Identifier: Apache-2.0


set -e

PASS=0
FAIL=0

check() {
  local name=$1
  local command=$2

  if bash -c "$command" >/dev/null 2>&1; then
    echo "PASS: ${name}"
    PASS=$((PASS + 1))
    return 0
  else
    echo "FAIL: ${name}"
    FAIL=$((FAIL + 1))
    return 0
  fi
}

echo "Verifying Infrastructure Setup"
echo ""

# Check Kind clusters
echo "Checking Kind Clusters..."
check "CSC cluster exists" "kind get clusters | grep -q '^csc$'"
check "CPC-1 cluster exists" "kind get clusters | grep -q '^cpc-1$'"
check "CPC-2 cluster exists" "kind get clusters | grep -q '^cpc-2$'"
echo ""

# Check cluster health
echo "Checking Cluster Health..."
check "CSC cluster API server" "kubectl cluster-info --context kind-csc"
check "CPC-1 cluster API server" "kubectl cluster-info --context kind-cpc-1"
check "CPC-2 cluster API server" "kubectl cluster-info --context kind-cpc-2"
echo ""

# Check nodes
echo "Checking Cluster Nodes..."
check "CSC nodes ready" "kubectl get nodes --context kind-csc --no-headers | grep -v NotReady"
check "CPC-1 nodes ready" "kubectl get nodes --context kind-cpc-1 --no-headers | grep -v NotReady"
check "CPC-2 nodes ready" "kubectl get nodes --context kind-cpc-2 --no-headers | grep -v NotReady"
echo ""

# Check MetalLB
echo "Checking MetalLB..."
check "CSC MetalLB namespace" "kubectl get namespace metallb-system --context kind-csc"
check "CPC-1 MetalLB namespace" "kubectl get namespace metallb-system --context kind-cpc-1"
check "CPC-2 MetalLB namespace" "kubectl get namespace metallb-system --context kind-cpc-2"
check "CSC MetalLB controller" "kubectl get pods -n metallb-system -l app.kubernetes.io/name=metallb,app.kubernetes.io/component=controller --context kind-csc --no-headers | grep Running"
check "CPC-1 MetalLB controller" "kubectl get pods -n metallb-system -l app.kubernetes.io/name=metallb,app.kubernetes.io/component=controller --context kind-cpc-1 --no-headers | grep Running"
check "CPC-2 MetalLB controller" "kubectl get pods -n metallb-system -l app.kubernetes.io/name=metallb,app.kubernetes.io/component=controller --context kind-cpc-2 --no-headers | grep Running"
check "CSC MetalLB IP pool" "kubectl get ipaddresspools -n metallb-system --context kind-csc --no-headers"
check "CPC-1 MetalLB IP pool" "kubectl get ipaddresspools -n metallb-system --context kind-cpc-1 --no-headers"
check "CPC-2 MetalLB IP pool" "kubectl get ipaddresspools -n metallb-system --context kind-cpc-2 --no-headers"
echo ""

# Check Envoy Gateway
echo "Checking Envoy Gateway..."
if kubectl get namespace envoy-gateway-system --context kind-csc >/dev/null 2>&1; then
    check "CSC Envoy Gateway namespace" "kubectl get namespace envoy-gateway-system --context kind-csc"
    check "CPC-1 Envoy Gateway namespace" "kubectl get namespace envoy-gateway-system --context kind-cpc-1"
    check "CPC-2 Envoy Gateway namespace" "kubectl get namespace envoy-gateway-system --context kind-cpc-2"
    check "CSC Envoy Gateway pods" "kubectl get pods -n envoy-gateway-system --context kind-csc --no-headers | grep Running"
    check "CPC-1 Envoy Gateway pods" "kubectl get pods -n envoy-gateway-system --context kind-cpc-1 --no-headers | grep Running"
    check "CPC-2 Envoy Gateway pods" "kubectl get pods -n envoy-gateway-system --context kind-cpc-2 --no-headers | grep Running"
    check "CSC GatewayClass" "kubectl get gatewayclass --context kind-csc --no-headers"
    check "CPC-1 GatewayClass" "kubectl get gatewayclass --context kind-cpc-1 --no-headers"
    check "CPC-2 GatewayClass" "kubectl get gatewayclass --context kind-cpc-2 --no-headers"
else
    echo "Envoy Gateway not installed"
fi
echo ""

# Check Metrics Server
echo "Checking Metrics Server..."
check "CSC metrics-server" "kubectl get deployment metrics-server -n kube-system --context kind-csc --no-headers | grep -E '1/1|2/2'"
check "CPC-1 metrics-server" "kubectl get deployment metrics-server -n kube-system --context kind-cpc-1 --no-headers | grep -E '1/1|2/2'"
check "CPC-2 metrics-server" "kubectl get deployment metrics-server -n kube-system --context kind-cpc-2 --no-headers | grep -E '1/1|2/2'"
echo ""

# Check Observability (Optional)
echo "Checking Observability Stack (Optional)..."
if kubectl get namespace monitoring --context kind-csc >/dev/null 2>&1; then
    check "Monitoring namespace" "kubectl get namespace monitoring --context kind-csc"
    check "Prometheus Operator" "kubectl get pods -n monitoring -l app=kube-prometheus-stack-operator --context kind-csc --no-headers | grep Running"
    check "Prometheus server" "kubectl get pods -n monitoring -l app.kubernetes.io/name=prometheus --context kind-csc --no-headers | grep Running"
    check "Grafana" "kubectl get pods -n monitoring -l app.kubernetes.io/name=grafana --context kind-csc --no-headers | grep Running"
    check "Alertmanager" "kubectl get pods -n monitoring -l app.kubernetes.io/name=alertmanager --context kind-csc --no-headers | grep Running"
    check "Node Exporter" "kubectl get pods -n monitoring -l app.kubernetes.io/name=prometheus-node-exporter --context kind-csc --no-headers | grep Running"
    check "Kube State Metrics" "kubectl get pods -n monitoring -l app.kubernetes.io/name=kube-state-metrics --context kind-csc --no-headers | grep Running"
else
    echo "Observability stack not installed (use 'make setup-observability' to install)"
fi
echo ""

# Summary
echo "Verification Summary"
echo "Passed: $PASS"
echo "Failed: $FAIL"
echo ""

if [ $FAIL -eq 0 ]; then
    echo "All checks passed"
    exit 0
else
    echo "Some checks failed"
    exit 1
fi
