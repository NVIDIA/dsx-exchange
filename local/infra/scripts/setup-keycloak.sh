#!/bin/bash
# Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
# SPDX-License-Identifier: Apache-2.0


set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

command -v kubectl >/dev/null 2>&1 || { echo "ERROR: kubectl is required" >&2; exit 1; }

# Keycloak version to use
KEYCLOAK_VERSION="26.0.7"

deploy_to_cluster() {
  local cluster_name=$1
  local realm_file=$2
  local context="kind-${cluster_name}"

  echo "Deploying Keycloak Operator to ${cluster_name}..."

  # Install CRDs
  kubectl apply -f https://raw.githubusercontent.com/keycloak/keycloak-k8s-resources/${KEYCLOAK_VERSION}/kubernetes/keycloaks.k8s.keycloak.org-v1.yml --context "${context}"
  kubectl apply -f https://raw.githubusercontent.com/keycloak/keycloak-k8s-resources/${KEYCLOAK_VERSION}/kubernetes/keycloakrealmimports.k8s.keycloak.org-v1.yml --context "${context}"

  # Create namespace
  kubectl create namespace keycloak --context "${context}" --dry-run=client -o yaml | kubectl apply --context "${context}" -f -

  # Install Operator
  kubectl -n keycloak apply -f https://raw.githubusercontent.com/keycloak/keycloak-k8s-resources/${KEYCLOAK_VERSION}/kubernetes/kubernetes.yml --context "${context}"

  # Wait for operator to be ready
  kubectl wait --for=condition=available deployment/keycloak-operator -n keycloak --timeout=2m --context "${context}"

  # Create ConfigMap with realm file for import
  kubectl create configmap keycloak-realm-import \
    --from-file="${realm_file}" \
    -n keycloak \
    --context "${context}" \
    --dry-run=client -o yaml | kubectl apply --context "${context}" -f -

  # Deploy Keycloak instance and LoadBalancer service
  kubectl apply -f "${PROJECT_ROOT}/infra/keycloak/keycloak.yaml" --context "${context}"

  # Wait for Keycloak to be ready
  echo "Waiting for Keycloak to be ready in ${cluster_name}..."
  kubectl wait --for=condition=ready keycloak/keycloak -n keycloak --timeout=5m --context "${context}"

  # Deploy HTTPRoute for external access
  kubectl apply -f "${PROJECT_ROOT}/infra/keycloak/httproute.yaml" --context "${context}"

  # Wait for Gateway to be programmed
  echo "Waiting for shared Gateway to be programmed..."
  kubectl wait --for=condition=Programmed gateway/shared-gateway -n envoy-gateway-system --timeout=2m --context "${context}"

  echo "Keycloak deployed to ${cluster_name}"
}

# Deploy to CSC cluster only
echo "Deploying Keycloak Operator to CSC cluster..."

if kind get clusters | grep -q "^csc$"; then
    deploy_to_cluster "csc" "${PROJECT_ROOT}/infra/keycloak/realm-csc.json"
else
    echo "ERROR: CSC cluster not found"
    exit 1
fi

echo "Keycloak Operator deployment complete on CSC cluster"
echo "All clusters will use Keycloak at http://172.18.200.1"

