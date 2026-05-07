#!/bin/bash
# Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
# SPDX-License-Identifier: Apache-2.0

#
# Generate NATS Event Bus NKeys to local files

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DEPLOY_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"

# Default values
CLUSTER="csc"
OUTPUT_DIR=""
CPC_IDS=()

usage() {
  cat <<EOF
Usage: ${0} [OPTIONS] [cpc-ids...]

Generate NATS Event Bus NKeys to local files.

Options:
  -c, --cluster CLUSTER    Cluster name: csc or cpc-{id} (default: csc)
  -o, --output DIR         Output directory (default: ./secrets/{cluster})
  -h, --help               Show this help message

Arguments:
  cpc-ids                  Optional list of CPC IDs for CSC clusters (e.g., 1 2 3)

Examples:
  ${0} -c csc -o ./secrets 1 2 3
  ${0} --cluster cpc-1 --output ./my-secrets
  ${0} csc
EOF
}

check_prerequisites() {
  local missing=()
  
  command -v nsc >/dev/null 2>&1 || missing+=("nsc")
  command -v jq >/dev/null 2>&1 || missing+=("jq")
  
  if [ ${#missing[@]} -gt 0 ]; then
    echo "ERROR: Missing required tools: ${missing[*]}" >&2
    echo "Install nsc from: https://github.com/nats-io/nsc/releases" >&2
    exit 1
  fi
}

parse_args() {
  while [[ $# -gt 0 ]]; do
    case $1 in
      -c|--cluster)
        CLUSTER="$2"
        shift 2
        ;;
      -o|--output)
        OUTPUT_DIR="$2"
        shift 2
        ;;
      -h|--help)
        usage
        exit 0
        ;;
      -*)
        echo "ERROR: Unknown option: $1" >&2
        usage
        exit 1
        ;;
      *)
        if [[ "$1" =~ ^[0-9]+$ ]]; then
          CPC_IDS+=("$1")
        else
          echo "ERROR: Invalid CPC ID: $1 (must be a number)" >&2
          exit 1
        fi
        shift
        ;;
    esac
  done
  
  # Set default output directory if not provided
  if [ -z "${OUTPUT_DIR}" ]; then
    OUTPUT_DIR="${SCRIPT_DIR}/../secrets/${CLUSTER}"
  fi
}

validate_cluster() {
  if [[ "${CLUSTER}" =~ ^cpc-[0-9]+$ ]] || [ "${CLUSTER}" = "csc" ]; then
    return 0
  else
    echo "ERROR: Invalid cluster '${CLUSTER}'. Must be: csc or cpc-{id} (e.g., cpc-1, cpc-2)" >&2
    exit 1
  fi
}

get_dc_account() {
  if [ "${CLUSTER}" = "csc" ]; then
    echo "CSC"
  elif [[ "${CLUSTER}" =~ ^cpc- ]]; then
    echo "CPC"
  fi
}

generate_nsc_keys() {
  local nsc_dir="$1"
  local dc_account="$2"
  
  export NKEYS_PATH="${nsc_dir}/keys"
  
  echo "Creating operator op-${CLUSTER}..."
  nsc --data-dir "${nsc_dir}" add operator --name "op-${CLUSTER}"
  
  echo "Creating AUTH account..."
  nsc --data-dir "${nsc_dir}" add account AUTH
  nsc --data-dir "${nsc_dir}" edit account AUTH --sk generate
  
  echo "Creating AUTHX account..."
  nsc --data-dir "${nsc_dir}" add account AUTHX
  
  echo "Creating AUTHX user (authx)..."
  nsc --data-dir "${nsc_dir}" add user --account AUTHX --name authx
  
  echo "Creating AUTHX leaf user (authx-leaf)..."
  nsc --data-dir "${nsc_dir}" add user --account AUTHX --name "authx-leaf"
  
  echo "Creating ${dc_account} account..."
  nsc --data-dir "${nsc_dir}" add account "${dc_account}"
  
  echo "Creating NACK user..."
  nsc --data-dir "${nsc_dir}" add user --account "${dc_account}" --name nack
  
  echo "Creating mTLS leaf user..."
  nsc --data-dir "${nsc_dir}" add user --account "${dc_account}" --name "mtls-leaf"
  
  echo "Creating SYS account..."
  nsc --data-dir "${nsc_dir}" add account SYS
  
  echo "Creating mTLS SYS leaf user..."
  nsc --data-dir "${nsc_dir}" add user --account SYS --name "mtls-sys-leaf"
  
  echo "Creating surveyor user..."
  nsc --data-dir "${nsc_dir}" add user --account SYS --name "surveyor"
  
  if [ "${CLUSTER}" = "csc" ] && [ ${#CPC_IDS[@]} -gt 0 ]; then
    for cpc_id in "${CPC_IDS[@]}"; do
      echo "Creating CSC leaf user for CPC-${cpc_id}..."
      nsc --data-dir "${nsc_dir}" add user --account CSC --name "leaf-cpc-${cpc_id}"
    done
  fi
  
  echo "Generating XKey..."
  nsc --data-dir "${nsc_dir}" generate nkey --curve > "${OUTPUT_DIR}/nkeys/xkey.nk"
  
  unset NKEYS_PATH
}

extract_key_values() {
  local nsc_dir="$1"
  local keys_export_dir="$2"
  local dc_account="$3"
  
  export NKEYS_PATH="${nsc_dir}/keys"
  
  nsc --data-dir "${nsc_dir}" export keys --account AUTH --accounts --dir "${keys_export_dir}" > /dev/null
  nsc --data-dir "${nsc_dir}" export keys --account AUTHX --users --dir "${keys_export_dir}" > /dev/null
  nsc --data-dir "${nsc_dir}" export keys --account "${dc_account}" --users --dir "${keys_export_dir}" > /dev/null
  nsc --data-dir "${nsc_dir}" export keys --account SYS --users --dir "${keys_export_dir}" > /dev/null
  
  local auth_signing_key
  auth_signing_key=$(nsc --data-dir "${nsc_dir}" describe account AUTH --json 2>/dev/null | jq -r '.nats.signing_keys[0]')
  
  local authx_user_pubkey
  authx_user_pubkey=$(nsc --data-dir "${nsc_dir}" describe user -a AUTHX authx --json 2>/dev/null | jq -r '.sub')
  
  local authx_leaf_user_pubkey
  authx_leaf_user_pubkey=$(nsc --data-dir "${nsc_dir}" describe user -a AUTHX "authx-leaf" --json 2>/dev/null | jq -r '.sub')
  
  local nack_user_pubkey
  nack_user_pubkey=$(nsc --data-dir "${nsc_dir}" describe user -a "${dc_account}" nack --json 2>/dev/null | jq -r '.sub')
  
  local mtls_leaf_user_pubkey
  mtls_leaf_user_pubkey=$(nsc --data-dir "${nsc_dir}" describe user -a "${dc_account}" "mtls-leaf" --json 2>/dev/null | jq -r '.sub')
  
  local mtls_sys_leaf_user_pubkey
  mtls_sys_leaf_user_pubkey=$(nsc --data-dir "${nsc_dir}" describe user -a SYS "mtls-sys-leaf" --json 2>/dev/null | jq -r '.sub')
  
  local surveyor_user_pubkey
  surveyor_user_pubkey=$(nsc --data-dir "${nsc_dir}" describe user -a SYS "surveyor" --json 2>/dev/null | jq -r '.sub')
  
  local xkey_pubkey
  xkey_pubkey=$(sed -n '2p' "${OUTPUT_DIR}/nkeys/xkey.nk" | tr -d '[:space:]')
  
  local xkey_seed
  xkey_seed=$(head -n 1 "${OUTPUT_DIR}/nkeys/xkey.nk")
  
  local auth_signing_seed
  auth_signing_seed=$(head -n 1 "${keys_export_dir}/${auth_signing_key}.nk")
  
  local authx_user_seed
  authx_user_seed=$(head -n 1 "${keys_export_dir}/${authx_user_pubkey}.nk")
  
  local authx_leaf_user_seed
  authx_leaf_user_seed=$(head -n 1 "${keys_export_dir}/${authx_leaf_user_pubkey}.nk" | tr -d '[:space:]')
  
  local nack_user_seed
  nack_user_seed=$(head -n 1 "${keys_export_dir}/${nack_user_pubkey}.nk" | tr -d '[:space:]')
  
  local mtls_leaf_user_seed
  mtls_leaf_user_seed=$(head -n 1 "${keys_export_dir}/${mtls_leaf_user_pubkey}.nk" | tr -d '[:space:]')
  
  local mtls_sys_leaf_user_seed
  mtls_sys_leaf_user_seed=$(head -n 1 "${keys_export_dir}/${mtls_sys_leaf_user_pubkey}.nk" | tr -d '[:space:]')
  
  local surveyor_user_seed
  surveyor_user_seed=$(head -n 1 "${keys_export_dir}/${surveyor_user_pubkey}.nk" | tr -d '[:space:]')
  
  unset NKEYS_PATH
  
  # Write secret files
  write_secret_file "nats-auth-signing" "pubkey" "${auth_signing_key}"
  write_secret_file "nats-auth-signing" "seed" "${auth_signing_seed}"
  
  write_secret_file "nats-xkey" "pubkey" "${xkey_pubkey}"
  write_secret_file "nats-xkey" "seed" "${xkey_seed}"
  
  write_secret_file "nats-authx-user" "pubkey" "${authx_user_pubkey}"
  write_secret_file "nats-authx-user" "seed" "${authx_user_seed}"
  
  write_secret_file "nats-nack-user" "pubkey" "${nack_user_pubkey}"
  write_secret_file "nats-nack-user" "seed" "${nack_user_seed}"
  cp "${keys_export_dir}/${nack_user_pubkey}.nk" "${OUTPUT_DIR}/nkeys/nats-nack-user/nack-user.nk"
  
  write_secret_file "nats-mtls-leaf" "pubkey" "${mtls_leaf_user_pubkey}"
  write_secret_file "nats-mtls-leaf" "seed" "${mtls_leaf_user_seed}"
  
  write_secret_file "nats-mtls-authx-leaf" "pubkey" "${authx_leaf_user_pubkey}"
  write_secret_file "nats-mtls-authx-leaf" "seed" "${authx_leaf_user_seed}"
  
  write_secret_file "nats-mtls-sys-leaf" "pubkey" "${mtls_sys_leaf_user_pubkey}"
  write_secret_file "nats-mtls-sys-leaf" "seed" "${mtls_sys_leaf_user_seed}"
  
  write_secret_file "nats-surveyor" "pubkey" "${surveyor_user_pubkey}"
  write_secret_file "nats-surveyor" "seed" "${surveyor_user_seed}"
  
  write_secret_file "auth-callout-keys" "nkey-seed" "${authx_user_seed}"
  write_secret_file "auth-callout-keys" "issuer-seed" "${auth_signing_seed}"
  write_secret_file "auth-callout-keys" "xkey-seed" "${xkey_seed}"
}

write_secret_file() {
  local secret_name="$1"
  local key="$2"
  local value="$3"
  
  mkdir -p "${OUTPUT_DIR}/nkeys/${secret_name}"
  echo -n "${value}" > "${OUTPUT_DIR}/nkeys/${secret_name}/${key}"
}

generate_cpc_leaf_secrets() {
  local nsc_dir="$1"
  
  if [ "${CLUSTER}" != "csc" ] || [ ${#CPC_IDS[@]} -eq 0 ]; then
    if [ "${CLUSTER}" != "csc" ]; then
      echo "NOTE: For CPC clusters, you need to copy nats-leaf-csc secret from CSC cluster"
      echo "      The CSC cluster generates leaf users for each CPC."
    fi
    return 0
  fi
  
  export NKEYS_PATH="${nsc_dir}/keys"
  
  for cpc_id in "${CPC_IDS[@]}"; do
    local cpc_leaf_pubkey
    cpc_leaf_pubkey=$(nsc --data-dir "${nsc_dir}" describe user -a CSC "leaf-cpc-${cpc_id}" --json 2>/dev/null | jq -r '.sub')
    
    local cpc_leaf_export_dir
    cpc_leaf_export_dir=$(mktemp -d)
    nsc --data-dir "${nsc_dir}" export keys --account CSC --user "leaf-cpc-${cpc_id}" --dir "${cpc_leaf_export_dir}" > /dev/null
    
    local cpc_leaf_seed
    cpc_leaf_seed=$(head -n 1 "${cpc_leaf_export_dir}/${cpc_leaf_pubkey}.nk" | tr -d '[:space:]')
    
    write_secret_file "nats-leaf-cpc-${cpc_id}" "pubkey" "${cpc_leaf_pubkey}"
    write_secret_file "nats-leaf-cpc-${cpc_id}" "seed" "${cpc_leaf_seed}"
    
    rm -rf "${cpc_leaf_export_dir}"
  done
  
  unset NKEYS_PATH
}

main() {
  check_prerequisites
  parse_args "$@"
  validate_cluster
  
  local dc_account
  dc_account=$(get_dc_account)
  
  echo "Generating secrets for ${CLUSTER}..."
  echo "Output directory: ${OUTPUT_DIR}"
  
  mkdir -p "${OUTPUT_DIR}/nkeys"
  
  local nsc_dir
  nsc_dir=$(mktemp -d)
  trap "rm -rf ${nsc_dir}" EXIT
  
  local keys_export_dir
  keys_export_dir=$(mktemp -d)
  trap "rm -rf ${keys_export_dir}" EXIT
  
  echo ""
  echo "=== Generating NSC keys ==="
  generate_nsc_keys "${nsc_dir}" "${dc_account}"
  
  echo ""
  echo "=== Writing NKey secrets ==="
  extract_key_values "${nsc_dir}" "${keys_export_dir}" "${dc_account}"
  
  generate_cpc_leaf_secrets "${nsc_dir}"
  
  rm -rf "${keys_export_dir}"
  
  echo ""
  echo "=== Secret generation complete ==="
  echo ""
  echo "Secrets written to: ${OUTPUT_DIR}"
  echo ""
  echo "Directory structure:"
  ls -R "${OUTPUT_DIR}"
}

main "$@"
