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

# Ensure local NKey output exists for the selected cluster
SECRETS_ROOT="${SCRIPT_DIR}/secrets"
SECRETS_DIR="${SECRETS_ROOT}/${cluster}"
SECRETS_NKEYS_DIR="${SECRETS_DIR}/nkeys"

get_cpc_ids() {
  yq -r '(.global.eventBus.cpcIds // [])[]' "${SCRIPT_DIR}/k8s/csc/values.yaml" 2>/dev/null || true
}

get_extra_accounts() {
  local values_file

  for values_file in \
    "${SCRIPT_DIR}/k8s/local-dev-values.yaml" \
    "${SCRIPT_DIR}/k8s/csc/values.yaml" \
    "${SCRIPT_DIR}/k8s/cpc/values.yaml"
  do
    if [ -f "${values_file}" ]; then
      yq -r '(.global.eventBus.extraAccounts // {}) | to_entries[] | select(.value.enabled != false) | .key' "${values_file}" 2>/dev/null || true
    fi
  done | sort -u
}

extra_account_secret_token() {
  local account_name="$1"
  local token

  token=$(printf '%s' "${account_name}" \
    | tr '[:upper:]' '[:lower:]' \
    | sed -E 's/[^a-z0-9-]+/-/g; s/^-+//; s/-+$//')

  if [ -z "${token}" ]; then
    echo "ERROR: extra account name ${account_name} normalizes to an empty secret token" >&2
    exit 1
  fi

  printf '%s' "${token}"
}

CPC_IDS_ARGS=()
while IFS= read -r cpc_id; do
  if [ -n "${cpc_id}" ]; then
    CPC_IDS_ARGS+=("${cpc_id}")
  fi
done < <(get_cpc_ids)

EXTRA_ACCOUNTS=()
EXTRA_ACCOUNT_ARGS=()
while IFS= read -r account_name; do
  if [ -n "${account_name}" ]; then
    EXTRA_ACCOUNTS+=("${account_name}")
    EXTRA_ACCOUNT_ARGS+=("--extra-account" "${account_name}")
  fi
done < <(get_extra_accounts)

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
  )

  local account_name
  local account_token

  if [ "${cluster}" = "csc" ]; then
    local cpc_id
    for cpc_id in "${CPC_IDS_ARGS[@]}"; do
      required_files+=("nats-leaf-cpc-${cpc_id}/pubkey")

      for account_name in "${EXTRA_ACCOUNTS[@]}"; do
        account_token=$(extra_account_secret_token "${account_name}")
        required_files+=("nats-leaf-${account_token}-cpc-${cpc_id}/pubkey")
      done
    done
  else
    required_files+=("nats-leaf-csc/seed")

    for account_name in "${EXTRA_ACCOUNTS[@]}"; do
      account_token=$(extra_account_secret_token "${account_name}")
      required_files+=("nats-leaf-${account_token}-csc/seed")
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

if [ ! -d "${SECRETS_NKEYS_DIR}" ]; then
  echo "Generating local auth key outputs..."
  mkdir -p "${SECRETS_ROOT}"

  # Generate secrets using helper script
  "${MONOREPO_ROOT}/deploy/scripts/generate-nkeys.sh" \
    -o "${SECRETS_ROOT}" \
    "${EXTRA_ACCOUNT_ARGS[@]}" \
    "${CPC_IDS_ARGS[@]}"

  echo "Auth keys generated for ${cluster}"
elif ! nkeys_complete; then
  echo "Generating missing local auth key outputs..."
  "${MONOREPO_ROOT}/deploy/scripts/generate-nkeys.sh" \
    -o "${SECRETS_ROOT}" \
    "${EXTRA_ACCOUNT_ARGS[@]}" \
    "${CPC_IDS_ARGS[@]}"

  if ! nkeys_complete; then
    echo "ERROR: existing auth keys for ${cluster} are incomplete: ${SECRETS_NKEYS_DIR}" >&2
    exit 1
  fi
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

# Create secrets with the standard names used by the chart.
SECRET_AUTHX_USER="nats-authx-user"
SECRET_AUTH_SIGNING="nats-auth-signing"
SECRET_XKEY="nats-xkey"
SECRET_NACK_USER="nats-nack-user"
SECRET_MTLS_LEAF="nats-mtls-leaf"
SECRET_MTLS_AUTHX_LEAF="nats-mtls-authx-leaf"
SECRET_MTLS_SYS_LEAF="nats-mtls-sys-leaf"
SECRET_SURVEYOR="nats-surveyor"
SECRET_LEAF_CSC="nats-leaf-csc"

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
  echo "Creating leaf node credential secret for ${cluster}..."
  LEAF_USER_SEED=$(cat "${SECRETS_NKEYS_DIR}/nats-leaf-csc/seed")

  kubectl create secret generic "${SECRET_LEAF_CSC}" \
    --namespace="${namespace}" \
    --context="${context}" \
    --from-literal=seed="${LEAF_USER_SEED}" \
    --dry-run=client -o yaml | kubectl apply --context="${context}" -f -

  for account_name in "${EXTRA_ACCOUNTS[@]}"; do
    account_token=$(extra_account_secret_token "${account_name}")
    extra_leaf_secret="nats-leaf-${account_token}-csc"

    echo "Creating ${account_name} leaf node credential secret for ${cluster}..."
    EXTRA_LEAF_USER_SEED=$(cat "${SECRETS_NKEYS_DIR}/${extra_leaf_secret}/seed")

    kubectl create secret generic "${extra_leaf_secret}" \
      --namespace="${namespace}" \
      --context="${context}" \
      --from-literal=seed="${EXTRA_LEAF_USER_SEED}" \
      --dry-run=client -o yaml | kubectl apply --context="${context}" -f -
  done
fi

# For CSC: create leaf user secrets for each CPC (read CPC IDs from values)
if [ "${cluster}" = "csc" ]; then
  CSC_VALUES="${SCRIPT_DIR}/k8s/csc/values.yaml"
  CPC_IDS=$(yq -r '(.global.eventBus.cpcIds // [])[]' "${CSC_VALUES}" 2>/dev/null | tr '\n' ' ')

  for cpc_id in ${CPC_IDS}; do
    # Secret name follows standard pattern
    SECRET_LEAF_CPC="nats-leaf-cpc-${cpc_id}"

    if [ -f "${SECRETS_NKEYS_DIR}/nats-leaf-cpc-${cpc_id}/pubkey" ]; then
      CPC_LEAF_PUBKEY=$(cat "${SECRETS_NKEYS_DIR}/nats-leaf-cpc-${cpc_id}/pubkey")

      kubectl create secret generic "${SECRET_LEAF_CPC}" \
        --namespace="${namespace}" \
        --context="${context}" \
        --from-literal=pubkey="${CPC_LEAF_PUBKEY}" \
        --dry-run=client -o yaml | kubectl apply --context="${context}" -f -
    fi

    for account_name in "${EXTRA_ACCOUNTS[@]}"; do
      account_token=$(extra_account_secret_token "${account_name}")
      SECRET_EXTRA_LEAF_CPC="nats-leaf-${account_token}-cpc-${cpc_id}"

      if [ -f "${SECRETS_NKEYS_DIR}/${SECRET_EXTRA_LEAF_CPC}/pubkey" ]; then
        EXTRA_CPC_LEAF_PUBKEY=$(cat "${SECRETS_NKEYS_DIR}/${SECRET_EXTRA_LEAF_CPC}/pubkey")

        kubectl create secret generic "${SECRET_EXTRA_LEAF_CPC}" \
          --namespace="${namespace}" \
          --context="${context}" \
          --from-literal=pubkey="${EXTRA_CPC_LEAF_PUBKEY}" \
          --dry-run=client -o yaml | kubectl apply --context="${context}" -f -
      fi
    done
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
make -C "${AUTH_CALLOUT_DIR}" docker-build

echo "Loading auth-callout image to kind..."
kind load docker-image auth-callout:latest --name "${cluster}"

# Install Helm chart
echo "Installing NATS Event Bus Helm chart..."
CHART_DIR="${MONOREPO_ROOT}/deploy/nats-event-bus"
echo "Updating Helm dependencies..."
helm dependency update "${CHART_DIR}"

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

# Restart auth-callout so pods use the image just loaded into this Kind cluster.
echo "Restarting auth-callout pods..."
kubectl delete pods -l app.kubernetes.io/name=auth-callout -n ${namespace} --context="${context}" --ignore-not-found=true

# Wait for readiness - use rollout status for deployments/statefulsets to avoid race conditions
echo "Waiting for NATS pods to be ready..."
kubectl rollout status statefulset/nats -n ${namespace} --context "${context}" --timeout=3m
kubectl rollout status statefulset/nats-mtls -n ${namespace} --context "${context}" --timeout=2m
kubectl rollout status deployment/nats-event-bus-surveyor -n ${namespace} --context "${context}" --timeout=2m
kubectl rollout status deployment/nack -n ${namespace} --context "${context}" --timeout=2m

echo "Waiting for JetStream streams to be ready..."
kubectl wait --for=condition=Ready stream --all -n ${namespace} --context "${context}" --timeout=2m

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
