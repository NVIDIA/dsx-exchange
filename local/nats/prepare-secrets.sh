#!/usr/bin/env bash
# Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
# SPDX-License-Identifier: Apache-2.0

# Prepare NATS Event Bus secrets for a Kind cluster.
#
# Usage: ./prepare-secrets.sh [cluster]
#   cluster: csc, cpc-1, or cpc-2 (default: csc)

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
MONOREPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"

command -v kubectl >/dev/null 2>&1 || { echo "ERROR: kubectl is required" >&2; exit 1; }
command -v cfssl >/dev/null 2>&1 || { echo "ERROR: cfssl is required (brew install cfssl)" >&2; exit 1; }
command -v cfssljson >/dev/null 2>&1 || { echo "ERROR: cfssljson is required (brew install cfssl)" >&2; exit 1; }
command -v yq >/dev/null 2>&1 || { echo "ERROR: yq is required (brew install yq)" >&2; exit 1; }

cluster=${1:-csc}
case "${cluster}" in
  csc|cpc-1|cpc-2) ;;
  *) echo "Unknown cluster: ${cluster}"; exit 1 ;;
esac

context="kind-${cluster}"
namespace="event-bus"

echo "Preparing NATS Event Bus secrets for ${cluster}..."

# Create namespace idempotently
kubectl create namespace "${namespace}" --context "${context}" --dry-run=client -o yaml | kubectl apply -f - --context "${context}"

# Ensure local NKey output exists for the selected cluster
SECRETS_ROOT="${SCRIPT_DIR}/secrets"
SECRETS_DIR="${SECRETS_ROOT}/${cluster}"
SECRETS_NKEYS_DIR="${SECRETS_DIR}/nkeys"

with_lock() {
  local lock_dir="$1"
  local deadline
  shift

  mkdir -p "$(dirname "${lock_dir}")"
  deadline=$(( $(date +%s) + 600 ))
  while ! mkdir "${lock_dir}" 2>/dev/null; do
    if [ "$(date +%s)" -ge "${deadline}" ]; then
      echo "ERROR: timed out waiting for lock ${lock_dir}" >&2
      return 1
    fi
    sleep 1
  done

  trap "status=\$?; rmdir \"${lock_dir}\" 2>/dev/null || true; trap - EXIT INT TERM; exit \"\${status}\"" EXIT INT TERM
  set +e
  "$@"
  local status=$?
  set -e

  trap - EXIT INT TERM
  if ! rmdir "${lock_dir}" 2>/dev/null; then
    echo "WARNING: lock ${lock_dir} was already removed" >&2
  fi
  return "${status}"
}

get_cpc_ids() {
  yq -r '(.global.eventBus.cpcIds // [])[]' "${SCRIPT_DIR}/k8s/csc/values.yaml"
}

values_files_for_cluster() {
  printf '%s\n' "${SCRIPT_DIR}/k8s/local-dev-values.yaml"
  case "${cluster}" in
    csc)
      printf '%s\n' "${SCRIPT_DIR}/k8s/csc/values.yaml"
      ;;
    cpc-1|cpc-2)
      printf '%s\n' "${SCRIPT_DIR}/k8s/cpc/values.yaml"
      printf '%s\n' "${SCRIPT_DIR}/k8s/cpc/${cluster}.yaml"
      ;;
  esac
}

get_extra_accounts() {
  local values_files=()
  local values_file

  while IFS= read -r values_file; do
    values_files+=("${values_file}")
  done < <(values_files_for_cluster)

  yq eval-all '. as $item ireduce ({}; . * $item) | (.global.eventBus.extraAccounts // {}) | to_entries[] | select(.value.enabled != false) | .key' "${values_files[@]}" \
    | sort -u
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
CPC_IDS_OUTPUT=$(get_cpc_ids)
while IFS= read -r cpc_id; do
  if [ -n "${cpc_id}" ]; then
    CPC_IDS_ARGS+=("${cpc_id}")
  fi
done <<< "${CPC_IDS_OUTPUT}"

EXTRA_ACCOUNTS=()
EXTRA_ACCOUNT_ARGS=()
EXTRA_ACCOUNTS_OUTPUT=$(get_extra_accounts)
while IFS= read -r account_name; do
  if [ -n "${account_name}" ]; then
    EXTRA_ACCOUNTS+=("${account_name}")
    EXTRA_ACCOUNT_ARGS+=("--extra-account" "${account_name}")
  fi
done <<< "${EXTRA_ACCOUNTS_OUTPUT}"

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
    for cpc_id in ${CPC_IDS_ARGS[@]+"${CPC_IDS_ARGS[@]}"}; do
      required_files+=("nats-leaf-cpc-${cpc_id}/pubkey")

      for account_name in ${EXTRA_ACCOUNTS[@]+"${EXTRA_ACCOUNTS[@]}"}; do
        account_token=$(extra_account_secret_token "${account_name}")
        required_files+=("nats-leaf-${account_token}-cpc-${cpc_id}/pubkey")
      done
    done
  else
    required_files+=("nats-leaf-csc/seed")

    for account_name in ${EXTRA_ACCOUNTS[@]+"${EXTRA_ACCOUNTS[@]}"}; do
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

generate_nkeys_if_needed() {
  if [ -d "${SECRETS_NKEYS_DIR}" ] && nkeys_complete; then
    return 0
  fi

  echo "Generating local auth key outputs..."
  mkdir -p "${SECRETS_ROOT}"

  "${MONOREPO_ROOT}/deploy/scripts/generate-nkeys.sh" \
    -o "${SECRETS_ROOT}" \
    ${EXTRA_ACCOUNT_ARGS[@]+"${EXTRA_ACCOUNT_ARGS[@]}"} \
    ${CPC_IDS_ARGS[@]+"${CPC_IDS_ARGS[@]}"}

  if ! nkeys_complete; then
    echo "ERROR: generated auth keys for ${cluster} are incomplete: ${SECRETS_NKEYS_DIR}" >&2
    exit 1
  fi

  echo "Auth keys generated for ${cluster}"
}

if [ ! -d "${SECRETS_NKEYS_DIR}" ] || ! nkeys_complete; then
  with_lock "${SECRETS_ROOT}/.nkeys.lock" generate_nkeys_if_needed
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

generate_certs_if_needed() {
  if certs_complete; then
    return 0
  fi

  if [ -d "${CERTS_DIR}" ]; then
    echo "Existing mTLS certificates for ${cluster} are incomplete; regenerating..."
    rm -rf "${CERTS_DIR}"
  fi

  echo "Generating mTLS certificates..."
  "${SCRIPT_DIR}/gen-mtls-certs.sh"
}

if ! certs_complete; then
  with_lock "${SCRIPT_DIR}/certs/.generate.lock" generate_certs_if_needed
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

echo "Creating NKey secrets..."

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

apply_secret() {
  local secret_name="$1"
  shift

  kubectl create secret generic "${secret_name}" \
    --namespace="${namespace}" \
    --context="${context}" \
    "$@" \
    --dry-run=client -o yaml | kubectl apply --context="${context}" -f -
}

apply_secret "${SECRET_AUTHX_USER}" \
  --from-file=pubkey="${SECRETS_NKEYS_DIR}/nats-authx-user/pubkey" \
  --from-file=seed="${SECRETS_NKEYS_DIR}/nats-authx-user/seed"

apply_secret "${SECRET_AUTH_SIGNING}" \
  --from-file=pubkey="${SECRETS_NKEYS_DIR}/nats-auth-signing/pubkey" \
  --from-file=seed="${SECRETS_NKEYS_DIR}/nats-auth-signing/seed"

apply_secret "${SECRET_XKEY}" \
  --from-file=pubkey="${SECRETS_NKEYS_DIR}/nats-xkey/pubkey" \
  --from-file=seed="${SECRETS_NKEYS_DIR}/nats-xkey/seed"

apply_secret "${SECRET_NACK_USER}" \
  --from-file=pubkey="${SECRETS_NKEYS_DIR}/nats-nack-user/pubkey" \
  --from-file=seed="${SECRETS_NKEYS_DIR}/nats-nack-user/seed" \
  --from-file=nack-user.nk="${SECRETS_NKEYS_DIR}/nats-nack-user/nack-user.nk"

apply_secret "${SECRET_MTLS_LEAF}" \
  --from-file=pubkey="${SECRETS_NKEYS_DIR}/nats-mtls-leaf/pubkey" \
  --from-file=seed="${SECRETS_NKEYS_DIR}/nats-mtls-leaf/seed"

apply_secret "${SECRET_MTLS_AUTHX_LEAF}" \
  --from-file=pubkey="${SECRETS_NKEYS_DIR}/nats-mtls-authx-leaf/pubkey" \
  --from-file=seed="${SECRETS_NKEYS_DIR}/nats-mtls-authx-leaf/seed"

apply_secret "${SECRET_MTLS_SYS_LEAF}" \
  --from-file=pubkey="${SECRETS_NKEYS_DIR}/nats-mtls-sys-leaf/pubkey" \
  --from-file=seed="${SECRETS_NKEYS_DIR}/nats-mtls-sys-leaf/seed"

apply_secret "${SECRET_SURVEYOR}" \
  --from-file=pubkey="${SECRETS_NKEYS_DIR}/nats-surveyor/pubkey" \
  --from-file=seed="${SECRETS_NKEYS_DIR}/nats-surveyor/seed"

# For CPCs: create leaf credential secret from CSC's keys
if [ "${cluster}" != "csc" ]; then
  echo "Creating leaf node credential secret for ${cluster}..."

  apply_secret "${SECRET_LEAF_CSC}" \
    --from-file=seed="${SECRETS_NKEYS_DIR}/nats-leaf-csc/seed"

  for account_name in ${EXTRA_ACCOUNTS[@]+"${EXTRA_ACCOUNTS[@]}"}; do
    account_token=$(extra_account_secret_token "${account_name}")
    extra_leaf_secret="nats-leaf-${account_token}-csc"

    echo "Creating ${account_name} leaf node credential secret for ${cluster}..."

    apply_secret "${extra_leaf_secret}" \
      --from-file=seed="${SECRETS_NKEYS_DIR}/${extra_leaf_secret}/seed"
  done
fi

# For CSC: create leaf user secrets for each CPC (read CPC IDs from values)
if [ "${cluster}" = "csc" ]; then
  for cpc_id in ${CPC_IDS_ARGS[@]+"${CPC_IDS_ARGS[@]}"}; do
    # Secret name follows standard pattern
    SECRET_LEAF_CPC="nats-leaf-cpc-${cpc_id}"

    if [ -f "${SECRETS_NKEYS_DIR}/nats-leaf-cpc-${cpc_id}/pubkey" ]; then
      apply_secret "${SECRET_LEAF_CPC}" \
        --from-file=pubkey="${SECRETS_NKEYS_DIR}/nats-leaf-cpc-${cpc_id}/pubkey"
    fi

    for account_name in ${EXTRA_ACCOUNTS[@]+"${EXTRA_ACCOUNTS[@]}"}; do
      account_token=$(extra_account_secret_token "${account_name}")
      SECRET_EXTRA_LEAF_CPC="nats-leaf-${account_token}-cpc-${cpc_id}"

      if [ -f "${SECRETS_NKEYS_DIR}/${SECRET_EXTRA_LEAF_CPC}/pubkey" ]; then
        apply_secret "${SECRET_EXTRA_LEAF_CPC}" \
          --from-file=pubkey="${SECRETS_NKEYS_DIR}/${SECRET_EXTRA_LEAF_CPC}/pubkey"
      fi
    done
  done
fi

# Create auth-callout secret (for auth-callout to connect to NATS)
echo "Creating auth-callout secret..."
apply_secret auth-callout-keys \
  --from-file=nkey-seed="${SECRETS_NKEYS_DIR}/nats-authx-user/seed" \
  --from-file=issuer-seed="${SECRETS_NKEYS_DIR}/nats-auth-signing/seed" \
  --from-file=xkey-seed="${SECRETS_NKEYS_DIR}/nats-xkey/seed"

echo "NATS Event Bus secrets prepared for ${cluster}"
