#!/bin/bash
# Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
# SPDX-License-Identifier: Apache-2.0

#
# Deploy NATS Event Bus to a Kind cluster
#
# This script:
# 1. Generates nkeys and creates K8s secrets with standardized names
# 2. Builds and loads the auth-callout image
# 3. Deploys the nats-event-bus Helm chart
#
# Usage: ./deploy.sh [cluster]
#   cluster: csc, cpc-1, or cpc-2 (default: csc)

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
MONOREPO_ROOT="$(cd "${PROJECT_ROOT}/.." && pwd)"

command -v kubectl >/dev/null 2>&1 || { echo "ERROR: kubectl is required" >&2; exit 1; }
command -v helm >/dev/null 2>&1 || { echo "ERROR: helm is required" >&2; exit 1; }
command -v cfssl >/dev/null 2>&1 || { echo "ERROR: cfssl is required (brew install cfssl)" >&2; exit 1; }
command -v jq >/dev/null 2>&1 || { echo "ERROR: jq is required" >&2; exit 1; }
command -v make >/dev/null 2>&1 || { echo "ERROR: make is required" >&2; exit 1; }
command -v docker >/dev/null 2>&1 || { echo "ERROR: docker is required" >&2; exit 1; }
command -v yq >/dev/null 2>&1 || { echo "ERROR: yq is required (brew install yq)" >&2; exit 1; }

# Add NATS Helm repository
if ! helm repo list | grep -q "nats"; then
  helm repo add nats https://nats-io.github.io/k8s/helm/charts/
  helm repo update >/dev/null
fi

cluster=${1:-csc}
kind_cluster="${cluster}"
context="kind-${kind_cluster}"
namespace="event-bus"

echo "Deploying NATS Event Bus to ${cluster}..."

# Create namespace idempotently
kubectl create namespace ${namespace} --context "${context}" --dry-run=client -o yaml | kubectl apply -f - --context "${context}"

# Determine DC account name from cluster
case "${cluster}" in
  csc) DC_ACCOUNT="CSC" ;;
  cpc-*) DC_ACCOUNT="CPC" ;;
  *) echo "Unknown cluster: ${cluster}"; exit 1 ;;
esac

# Generate cluster-specific auth keys if they don't exist
SECRETS_DIR="${SCRIPT_DIR}/secrets/${cluster}"
CLUSTER_XKEY_FILE="${SCRIPT_DIR}/keys/${cluster}/xkey.nk"
SECRETS_NKEYS_DIR="${SECRETS_DIR}/nkeys"

nkeys_complete() {
  local required_files=(
    "auth-callout-keys/issuer-seed"
    "auth-callout-keys/nkey-seed"
    "auth-callout-keys/xkey-seed"
    "nats-auth-signing/pubkey"
    "nats-auth-signing/seed"
    "nats-authx-user/pubkey"
    "nats-authx-user/seed"
    "nats-mtls-authx-leaf/pubkey"
    "nats-mtls-authx-leaf/seed"
    "nats-mtls-leaf/pubkey"
    "nats-mtls-leaf/seed"
    "nats-mtls-sys-leaf/pubkey"
    "nats-mtls-sys-leaf/seed"
    "nats-nack-user/nack-user.nk"
    "nats-nack-user/pubkey"
    "nats-nack-user/seed"
    "nats-surveyor/pubkey"
    "nats-surveyor/seed"
    "nats-xkey/pubkey"
    "nats-xkey/seed"
    "xkey.nk"
  )

  if [ "${cluster}" = "csc" ]; then
    local cpc_id
    local cpc_ids
    cpc_ids=$(yq -r '.eventBus.cpcIds[]' "${SCRIPT_DIR}/k8s/csc/values.yaml" 2>/dev/null || true)
    for cpc_id in ${cpc_ids}; do
      required_files+=("nats-leaf-cpc-${cpc_id}/pubkey")
      required_files+=("nats-leaf-cpc-${cpc_id}/seed")
    done
  fi

  local rel
  for rel in "${required_files[@]}"; do
    if [ ! -s "${SECRETS_NKEYS_DIR}/${rel}" ]; then
      return 1
    fi
  done

  return 0
}

if ! nkeys_complete; then
  if [ -d "${SECRETS_NKEYS_DIR}" ]; then
    echo "Existing auth keys for ${cluster} are incomplete; regenerating..."
    rm -rf "${SECRETS_DIR}"
    rm -f "${CLUSTER_XKEY_FILE}"
  fi

  echo "Generating auth keys for ${cluster}..."
  mkdir -p "${SECRETS_DIR}" "$(dirname "${CLUSTER_XKEY_FILE}")"

  # Get CPC IDs from values.yaml for CSC clusters
  CPC_IDS_ARGS=""
  if [ "${cluster}" = "csc" ]; then
    CSC_VALUES="${SCRIPT_DIR}/k8s/csc/values.yaml"
    if [ -f "${CSC_VALUES}" ]; then
      CPC_IDS=$(yq -r '.eventBus.cpcIds[]' "${CSC_VALUES}" 2>/dev/null | tr '\n' ' ')
      if [ -n "${CPC_IDS}" ]; then
        CPC_IDS_ARGS="${CPC_IDS}"
      fi
    fi
  fi

  # Generate secrets using helper script
  "${MONOREPO_ROOT}/deploy/scripts/generate-nkeys.sh" \
    -c "${cluster}" \
    -o "${SECRETS_DIR}" \
    ${CPC_IDS_ARGS}

  # Copy xkey.nk to expected location for backward compatibility
  cp "${SECRETS_NKEYS_DIR}/xkey.nk" "${CLUSTER_XKEY_FILE}"

  echo "Auth keys generated for ${cluster}"
fi

# Generate mTLS certificates if they don't exist
CERTS_DIR="${SCRIPT_DIR}/certs/${cluster}"

certs_complete() {
  for cert_file in ca.pem server.pem server-key.pem client.pem client-key.pem; do
    if [ ! -s "${CERTS_DIR}/${cert_file}" ]; then
      return 1
    fi
  done

  return 0
}

if ! certs_complete; then
  if [ -d "${CERTS_DIR}" ]; then
    echo "Existing mTLS certificates for ${cluster} are incomplete; regenerating..."
    rm -rf "${CERTS_DIR}"
  fi

  echo "Generating mTLS certificates..."
  "${SCRIPT_DIR}/gen-mtls-certs.sh"
fi

# Create TLS secret for mTLS MQTT
echo "Creating mTLS server TLS secret..."
kubectl create secret generic nats-mtls-server-tls \
  --namespace="${namespace}" \
  --context="${context}" \
  --from-file=ca.crt="${CERTS_DIR}/ca.pem" \
  --from-file=tls.crt="${CERTS_DIR}/server.pem" \
  --from-file=tls.key="${CERTS_DIR}/server-key.pem" \
  --dry-run=client -o yaml | kubectl apply --context="${context}" -f -

# Read keys from generated secret files
echo "Creating NKey secrets..."

AUTH_SIGNING_KEY=$(cat "${SECRETS_NKEYS_DIR}/nats-auth-signing/pubkey")
AUTH_SIGNING_SEED=$(cat "${SECRETS_NKEYS_DIR}/nats-auth-signing/seed")
AUTHX_USER_PUBKEY=$(cat "${SECRETS_NKEYS_DIR}/nats-authx-user/pubkey")
AUTHX_USER_SEED=$(cat "${SECRETS_NKEYS_DIR}/nats-authx-user/seed")
AUTHX_LEAF_USER_PUBKEY=$(cat "${SECRETS_NKEYS_DIR}/nats-mtls-authx-leaf/pubkey")
AUTHX_LEAF_USER_SEED=$(cat "${SECRETS_NKEYS_DIR}/nats-mtls-authx-leaf/seed")
NACK_USER_PUBKEY=$(cat "${SECRETS_NKEYS_DIR}/nats-nack-user/pubkey")
NACK_USER_SEED=$(cat "${SECRETS_NKEYS_DIR}/nats-nack-user/seed")
MTLS_LEAF_USER_PUBKEY=$(cat "${SECRETS_NKEYS_DIR}/nats-mtls-leaf/pubkey")
MTLS_LEAF_USER_SEED=$(cat "${SECRETS_NKEYS_DIR}/nats-mtls-leaf/seed")
MTLS_SYS_LEAF_USER_PUBKEY=$(cat "${SECRETS_NKEYS_DIR}/nats-mtls-sys-leaf/pubkey")
MTLS_SYS_LEAF_USER_SEED=$(cat "${SECRETS_NKEYS_DIR}/nats-mtls-sys-leaf/seed")
SURVEYOR_USER_PUBKEY=$(cat "${SECRETS_NKEYS_DIR}/nats-surveyor/pubkey")
SURVEYOR_USER_SEED=$(cat "${SECRETS_NKEYS_DIR}/nats-surveyor/seed")

XKEY_PUBKEY=$(cat "${SECRETS_NKEYS_DIR}/nats-xkey/pubkey")
XKEY_SEED=$(cat "${SECRETS_NKEYS_DIR}/nats-xkey/seed")

# Read secret names from eventBus.nkeyRefs (single source of truth)
VALUES_FILE="${MONOREPO_ROOT}/deploy/nats-event-bus/values.yaml"
SECRET_AUTHX_USER=$(yq -r '.eventBus.nkeyRefs.authxUserPubkey.valueFrom.secretKeyRef.name' "${VALUES_FILE}")
SECRET_AUTH_SIGNING=$(yq -r '.eventBus.nkeyRefs.authSigningPubkey.valueFrom.secretKeyRef.name' "${VALUES_FILE}")
SECRET_XKEY=$(yq -r '.eventBus.nkeyRefs.xkeyPubkey.valueFrom.secretKeyRef.name' "${VALUES_FILE}")
SECRET_NACK_USER=$(yq -r '.eventBus.nkeyRefs.nackUserPubkey.valueFrom.secretKeyRef.name' "${VALUES_FILE}")
SECRET_MTLS_LEAF=$(yq -r '.eventBus.nkeyRefs.mtlsLeafPubkey.valueFrom.secretKeyRef.name' "${VALUES_FILE}")
SECRET_MTLS_AUTHX_LEAF=$(yq -r '.eventBus.nkeyRefs.mtlsAuthxLeafPubkey.valueFrom.secretKeyRef.name' "${VALUES_FILE}")
SECRET_MTLS_SYS_LEAF=$(yq -r '.eventBus.nkeyRefs.mtlsSysLeafPubkey.valueFrom.secretKeyRef.name' "${VALUES_FILE}")
SECRET_SURVEYOR=$(yq -r '.eventBus.nkeyRefs.surveyorNkeyPubkey.valueFrom.secretKeyRef.name' "${VALUES_FILE}")
SECRET_LEAF_CSC="nats-leaf-csc"  # Standard naming pattern for CPC->CSC leaf

# Create secrets with names from eventBus.nkeyRefs
kubectl create secret generic "${SECRET_AUTHX_USER}" \
  --namespace="${namespace}" \
  --context="${context}" \
  --from-literal=pubkey="${AUTHX_USER_PUBKEY}" \
  --from-literal=seed="${AUTHX_USER_SEED}" \
  --dry-run=client -o yaml | kubectl apply --context="${context}" -f -

kubectl create secret generic "${SECRET_AUTH_SIGNING}" \
  --namespace="${namespace}" \
  --context="${context}" \
  --from-literal=pubkey="${AUTH_SIGNING_KEY}" \
  --from-literal=seed="${AUTH_SIGNING_SEED}" \
  --dry-run=client -o yaml | kubectl apply --context="${context}" -f -

kubectl create secret generic "${SECRET_XKEY}" \
  --namespace="${namespace}" \
  --context="${context}" \
  --from-literal=pubkey="${XKEY_PUBKEY}" \
  --from-literal=seed="${XKEY_SEED}" \
  --dry-run=client -o yaml | kubectl apply --context="${context}" -f -

kubectl create secret generic "${SECRET_NACK_USER}" \
  --namespace="${namespace}" \
  --context="${context}" \
  --from-literal=pubkey="${NACK_USER_PUBKEY}" \
  --from-literal=seed="${NACK_USER_SEED}" \
  --from-file=nack-user.nk="${SECRETS_NKEYS_DIR}/nats-nack-user/nack-user.nk" \
  --dry-run=client -o yaml | kubectl apply --context="${context}" -f -

kubectl create secret generic "${SECRET_MTLS_LEAF}" \
  --namespace="${namespace}" \
  --context="${context}" \
  --from-literal=pubkey="${MTLS_LEAF_USER_PUBKEY}" \
  --from-literal=seed="${MTLS_LEAF_USER_SEED}" \
  --dry-run=client -o yaml | kubectl apply --context="${context}" -f -

kubectl create secret generic "${SECRET_MTLS_AUTHX_LEAF}" \
  --namespace="${namespace}" \
  --context="${context}" \
  --from-literal=pubkey="${AUTHX_LEAF_USER_PUBKEY}" \
  --from-literal=seed="${AUTHX_LEAF_USER_SEED}" \
  --dry-run=client -o yaml | kubectl apply --context="${context}" -f -

kubectl create secret generic "${SECRET_MTLS_SYS_LEAF}" \
  --namespace="${namespace}" \
  --context="${context}" \
  --from-literal=pubkey="${MTLS_SYS_LEAF_USER_PUBKEY}" \
  --from-literal=seed="${MTLS_SYS_LEAF_USER_SEED}" \
  --dry-run=client -o yaml | kubectl apply --context="${context}" -f -

kubectl create secret generic "${SECRET_SURVEYOR}" \
  --namespace="${namespace}" \
  --context="${context}" \
  --from-literal=pubkey="${SURVEYOR_USER_PUBKEY}" \
  --from-literal=seed="${SURVEYOR_USER_SEED}" \
  --dry-run=client -o yaml | kubectl apply --context="${context}" -f -

# For CPCs: create leaf credential secret from CSC's keys
if [ "${cluster}" != "csc" ]; then
  echo "Reading leaf node credentials for ${cluster} from CSC..."
  CSC_SECRETS_DIR="${SCRIPT_DIR}/secrets/csc"

  # Extract CPC ID from cluster name (e.g., cpc-1 -> 1)
  CPC_ID="${cluster#cpc-}"

  if [ -f "${CSC_SECRETS_DIR}/nkeys/nats-leaf-cpc-${CPC_ID}/pubkey" ]; then
    LEAF_USER_PUBKEY=$(cat "${CSC_SECRETS_DIR}/nkeys/nats-leaf-cpc-${CPC_ID}/pubkey")
    LEAF_USER_SEED=$(cat "${CSC_SECRETS_DIR}/nkeys/nats-leaf-cpc-${CPC_ID}/seed")

    kubectl create secret generic "${SECRET_LEAF_CSC}" \
      --namespace="${namespace}" \
      --context="${context}" \
      --from-literal=pubkey="${LEAF_USER_PUBKEY}" \
      --from-literal=seed="${LEAF_USER_SEED}" \
      --dry-run=client -o yaml | kubectl apply --context="${context}" -f -
  else
    echo "ERROR: Leaf credentials for CPC-${CPC_ID} not found in CSC secrets. Generate CSC secrets first." >&2
    exit 1
  fi
fi

# For CSC: create leaf user secrets for each CPC (read CPC IDs from values)
if [ "${cluster}" = "csc" ]; then
  CSC_VALUES="${SCRIPT_DIR}/k8s/csc/values.yaml"
  CPC_IDS=$(yq -r '.eventBus.cpcIds[]' "${CSC_VALUES}" 2>/dev/null | tr '\n' ' ')

  for cpc_id in ${CPC_IDS}; do
    # Secret name follows standard pattern
    SECRET_LEAF_CPC="nats-leaf-cpc-${cpc_id}"

    if [ -f "${SECRETS_NKEYS_DIR}/nats-leaf-cpc-${cpc_id}/pubkey" ]; then
      CPC_LEAF_PUBKEY=$(cat "${SECRETS_NKEYS_DIR}/nats-leaf-cpc-${cpc_id}/pubkey")
      CPC_LEAF_SEED=$(cat "${SECRETS_NKEYS_DIR}/nats-leaf-cpc-${cpc_id}/seed")

      kubectl create secret generic "${SECRET_LEAF_CPC}" \
        --namespace="${namespace}" \
        --context="${context}" \
        --from-literal=pubkey="${CPC_LEAF_PUBKEY}" \
        --from-literal=seed="${CPC_LEAF_SEED}" \
        --dry-run=client -o yaml | kubectl apply --context="${context}" -f -
    fi
  done
fi

# Create auth-callout secret (for auth-callout to connect to NATS)
echo "Creating auth-callout secret..."
kubectl create secret generic auth-callout-keys \
  --namespace="${namespace}" \
  --context="${context}" \
  --from-literal=nkey-seed="${AUTHX_USER_SEED}" \
  --from-literal=issuer-seed="${AUTH_SIGNING_SEED}" \
  --from-literal=xkey-seed="${XKEY_SEED}" \
  --dry-run=client -o yaml | kubectl apply --context="${context}" -f -

# Build and load auth-callout image
AUTH_CALLOUT_DIR="${MONOREPO_ROOT}/auth-callout"
if [ ! -f "${AUTH_CALLOUT_DIR}/Makefile" ]; then
  echo "ERROR: Makefile not found at ${AUTH_CALLOUT_DIR}/Makefile" >&2
  exit 1
fi
OLD_IMAGE_ID=$(docker images -q auth-callout:latest 2>/dev/null)
make -C "${AUTH_CALLOUT_DIR}" docker-build
NEW_IMAGE_ID=$(docker images -q auth-callout:latest 2>/dev/null)

if [ "${OLD_IMAGE_ID}" != "${NEW_IMAGE_ID}" ]; then
  echo "Loading new auth-callout image to kind..."
  kind load docker-image auth-callout:latest --name "${cluster}"
fi

# Install Helm chart
echo "Installing NATS Event Bus Helm chart..."
CHART_DIR="${MONOREPO_ROOT}/deploy/nats-event-bus"
CHARTS_DIR="${CHART_DIR}/charts"
CHART_LOCK="${CHART_DIR}/Chart.lock"
if [ ! -d "${CHARTS_DIR}" ] || [ -z "$(ls -A "${CHARTS_DIR}" 2>/dev/null)" ] || [ "${CHART_LOCK}" -nt "${CHARTS_DIR}" ]; then
  echo "Updating Helm dependencies..."
  helm dependency update "${CHART_DIR}"
else
  echo "Helm dependencies already up to date"
fi

VALUES_FILES="-f ${SCRIPT_DIR}/k8s/local-dev-values.yaml"
if [[ "${cluster}" == cpc-* ]]; then
  VALUES_FILES="${VALUES_FILES} -f ${SCRIPT_DIR}/k8s/cpc/values.yaml"
  VALUES_FILES="${VALUES_FILES} -f ${SCRIPT_DIR}/k8s/cpc/${cluster}.yaml"
else
  VALUES_FILES="${VALUES_FILES} -f ${SCRIPT_DIR}/k8s/${cluster}/values.yaml"
fi

# TODO: Investigate nack jetstream-controller taking ownership of .spec.sources via Update
# This causes server-side apply conflicts with Helm. Using --force-conflicts as workaround.
# See: https://github.com/nats-io/nack
helm upgrade --install nats-event-bus "${CHART_DIR}" \
  --namespace ${namespace} \
  ${VALUES_FILES} \
  --kube-context "${context}" \
  --timeout 2m \
  --force-conflicts \
  --cleanup-on-fail

# Restart auth-callout if image changed
if [ "${OLD_IMAGE_ID}" != "${NEW_IMAGE_ID}" ]; then
  echo "Restarting auth-callout pods..."
  kubectl delete pods -l app.kubernetes.io/name=auth-callout -n ${namespace} --context="${context}" --ignore-not-found=true
fi

# Wait for readiness - use rollout status for deployments/statefulsets to avoid race conditions
echo "Waiting for NATS pods to be ready..."
kubectl rollout status statefulset/nats -n ${namespace} --context "${context}" --timeout=3m
kubectl rollout status statefulset/nats-mtls -n ${namespace} --context "${context}" --timeout=2m
kubectl rollout status deployment/nats-event-bus-surveyor -n ${namespace} --context "${context}" --timeout=2m
kubectl rollout status deployment/nack -n ${namespace} --context "${context}" --timeout=2m

echo "Waiting for auth-callout pods to be ready..."
kubectl rollout status deployment/auth-callout -n ${namespace} --context "${context}" --timeout=2m

echo "Waiting for shared Gateway to be programmed..."
kubectl wait --for=condition=Programmed gateway/shared-gateway -n envoy-gateway-system --context "${context}" --timeout=2m

gateway_ip=$(kubectl get gateway shared-gateway -n envoy-gateway-system --context "${context}" -o jsonpath='{.status.addresses[0].value}' 2>/dev/null || echo "pending")

echo ""
echo "NATS Event Bus deployment complete for ${cluster}"
echo "Gateway IP: ${gateway_ip}"
echo "MQTT (TCP): tcp://${gateway_ip}:1883"
echo "MQTT (mTLS): ssl://${gateway_ip}:8883"
echo "NATS Client: nats://${gateway_ip}:4222"
echo "NATS Leaf Node: nats://${gateway_ip}:7422"
