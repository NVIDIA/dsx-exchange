#!/usr/bin/env bash
# Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
# SPDX-License-Identifier: Apache-2.0

#
# Generate NATS Event Bus NKeys to local files

set -euo pipefail
umask 077

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DEPLOY_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"

CLUSTER=""
OUTPUT_DIR=""
CPC_IDS=()
FORCE=false
TEMP_DIRS=()

usage() {
  cat <<EOF
Usage: ${0} [OPTIONS] -c CLUSTER [cpc-ids...]

Generate NATS Event Bus NKeys to local files.

Options:
  -c, --cluster CLUSTER    Cluster name: csc or cpc-{id}
  -o, --output DIR         Output directory (default: deploy/secrets/{cluster})
      --force              Overwrite an existing non-empty output directory
  -h, --help               Show this help message

Arguments:
  cpc-ids                  Optional list of CPC IDs for CSC clusters (e.g., 1 2 3)

Examples:
  ${0} -c csc -o deploy/secrets/csc 1 2 3
  ${0} --cluster cpc-1 --output deploy/secrets/cpc-1
  ${0} --cluster csc 1 2
EOF
}

cleanup() {
  local dir

  for dir in "${TEMP_DIRS[@]:-}"; do
    if [ -n "${dir}" ] && [ -d "${dir}" ]; then
      rm -rf "${dir}"
    fi
  done
}

make_temp_dir() {
  local dir

  dir=$(mktemp -d)
  chmod 700 "${dir}"
  TEMP_DIRS+=("${dir}")
  echo "${dir}"
}

check_prerequisites() {
  local missing=()

  command -v nsc >/dev/null 2>&1 || missing+=("nsc")
  command -v jq >/dev/null 2>&1 || missing+=("jq")

  if [ ${#missing[@]} -gt 0 ]; then
    echo "ERROR: Missing required tools: ${missing[*]}" >&2
    echo "Get nsc from: https://github.com/nats-io/nsc/releases" >&2
    exit 1
  fi
}

parse_args() {
  if [ "$#" -eq 0 ]; then
    echo "ERROR: no arguments supplied" >&2
    usage >&2
    exit 1
  fi

  while [[ $# -gt 0 ]]; do
    case $1 in
      -c|--cluster)
        if [ $# -lt 2 ]; then
          echo "ERROR: $1 requires a value" >&2
          exit 1
        fi
        CLUSTER="$2"
        shift 2
        ;;
      -o|--output)
        if [ $# -lt 2 ]; then
          echo "ERROR: $1 requires a value" >&2
          exit 1
        fi
        OUTPUT_DIR="$2"
        shift 2
        ;;
      --force)
        FORCE=true
        shift
        ;;
      -h|--help)
        usage
        exit 0
        ;;
      -*)
        echo "ERROR: Unknown option: $1" >&2
        usage >&2
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

  if [ -z "${CLUSTER}" ]; then
    echo "ERROR: cluster is required; pass -c csc or -c cpc-{id}" >&2
    usage >&2
    exit 1
  fi

  if [ -z "${OUTPUT_DIR}" ]; then
    OUTPUT_DIR="${DEPLOY_DIR}/secrets/${CLUSTER}"
  fi
}

validate_cluster() {
  if [[ "${CLUSTER}" =~ ^cpc-[0-9]+$ ]] || [ "${CLUSTER}" = "csc" ]; then
    return 0
  fi

  echo "ERROR: Invalid cluster '${CLUSTER}'. Must be: csc or cpc-{id} (e.g., cpc-1, cpc-2)" >&2
  exit 1
}

prepare_output_dir() {
  local nkeys_dir="${OUTPUT_DIR}/nkeys"

  if [ -z "${OUTPUT_DIR}" ] || [ "${OUTPUT_DIR}" = "/" ]; then
    echo "ERROR: refusing unsafe output directory: ${OUTPUT_DIR}" >&2
    exit 1
  fi

  if [ -d "${nkeys_dir}" ] && [ -n "$(find "${nkeys_dir}" -mindepth 1 -maxdepth 1 -print -quit)" ]; then
    if [ "${FORCE}" != "true" ]; then
      echo "ERROR: output directory already contains generated NKeys: ${nkeys_dir}" >&2
      echo "Re-run with --force to intentionally replace these secrets." >&2
      exit 1
    fi

    rm -rf "${nkeys_dir}"
  fi

  mkdir -p "${nkeys_dir}"
  chmod 700 "${nkeys_dir}"
}

get_dc_account() {
  if [ "${CLUSTER}" = "csc" ]; then
    echo "CSC"
  elif [[ "${CLUSTER}" =~ ^cpc- ]]; then
    echo "CPC"
  fi
}

run_nsc_quiet() {
  local log_dir
  local log

  log_dir=$(make_temp_dir)
  log="${log_dir}/nsc.log"

  if ! nsc "$@" > "${log}" 2>&1; then
    cat "${log}" >&2
    exit 1
  fi
}

generate_nsc_keys() {
  local nsc_dir="$1"
  local dc_account="$2"

  export NKEYS_PATH="${nsc_dir}/keys"

  echo "Creating operator op-${CLUSTER}..."
  run_nsc_quiet --data-dir "${nsc_dir}" add operator --name "op-${CLUSTER}"

  echo "Creating AUTH account..."
  run_nsc_quiet --data-dir "${nsc_dir}" add account AUTH
  run_nsc_quiet --data-dir "${nsc_dir}" edit account AUTH --sk generate

  echo "Creating AUTHX account..."
  run_nsc_quiet --data-dir "${nsc_dir}" add account AUTHX

  echo "Creating AUTHX user (authx)..."
  run_nsc_quiet --data-dir "${nsc_dir}" add user --account AUTHX --name authx

  echo "Creating AUTHX leaf user (authx-leaf)..."
  run_nsc_quiet --data-dir "${nsc_dir}" add user --account AUTHX --name "authx-leaf"

  echo "Creating ${dc_account} account..."
  run_nsc_quiet --data-dir "${nsc_dir}" add account "${dc_account}"

  echo "Creating NACK user..."
  run_nsc_quiet --data-dir "${nsc_dir}" add user --account "${dc_account}" --name nack

  echo "Creating mTLS leaf user..."
  run_nsc_quiet --data-dir "${nsc_dir}" add user --account "${dc_account}" --name "mtls-leaf"

  echo "Creating SYS account..."
  run_nsc_quiet --data-dir "${nsc_dir}" add account SYS

  echo "Creating mTLS SYS leaf user..."
  run_nsc_quiet --data-dir "${nsc_dir}" add user --account SYS --name "mtls-sys-leaf"

  echo "Creating surveyor user..."
  run_nsc_quiet --data-dir "${nsc_dir}" add user --account SYS --name "surveyor"

  if [ "${CLUSTER}" = "csc" ] && [ ${#CPC_IDS[@]} -gt 0 ]; then
    for cpc_id in "${CPC_IDS[@]}"; do
      echo "Creating CSC leaf user for CPC-${cpc_id}..."
      run_nsc_quiet --data-dir "${nsc_dir}" add user --account CSC --name "leaf-cpc-${cpc_id}"
    done
  fi

  echo "Generating XKey..."
  nsc --data-dir "${nsc_dir}" generate nkey --curve > "${nsc_dir}/xkey.nk"
  chmod 600 "${nsc_dir}/xkey.nk"

  unset NKEYS_PATH
}

extract_key_values() {
  local nsc_dir="$1"
  local keys_export_dir="$2"
  local dc_account="$3"

  export NKEYS_PATH="${nsc_dir}/keys"

  run_nsc_quiet --data-dir "${nsc_dir}" export keys --account AUTH --accounts --dir "${keys_export_dir}"
  run_nsc_quiet --data-dir "${nsc_dir}" export keys --account AUTHX --users --dir "${keys_export_dir}"
  run_nsc_quiet --data-dir "${nsc_dir}" export keys --account "${dc_account}" --users --dir "${keys_export_dir}"
  run_nsc_quiet --data-dir "${nsc_dir}" export keys --account SYS --users --dir "${keys_export_dir}"

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
  xkey_pubkey=$(sed -n '2p' "${nsc_dir}/xkey.nk" | tr -d '[:space:]')

  local xkey_seed
  xkey_seed=$(head -n 1 "${nsc_dir}/xkey.nk")

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

  write_secret_file "nats-auth-signing" "pubkey" "${auth_signing_key}"
  write_secret_file "nats-auth-signing" "seed" "${auth_signing_seed}"

  write_secret_file "nats-xkey" "pubkey" "${xkey_pubkey}"
  write_secret_file "nats-xkey" "seed" "${xkey_seed}"

  write_secret_file "nats-authx-user" "pubkey" "${authx_user_pubkey}"
  write_secret_file "nats-authx-user" "seed" "${authx_user_seed}"

  write_secret_file "nats-nack-user" "pubkey" "${nack_user_pubkey}"
  write_secret_file "nats-nack-user" "seed" "${nack_user_seed}"
  cp "${keys_export_dir}/${nack_user_pubkey}.nk" "${OUTPUT_DIR}/nkeys/nats-nack-user/nack-user.nk"
  chmod 600 "${OUTPUT_DIR}/nkeys/nats-nack-user/nack-user.nk"

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
  local secret_dir="${OUTPUT_DIR}/nkeys/${secret_name}"
  local target="${secret_dir}/${key}"
  local tmp

  mkdir -p "${secret_dir}"
  chmod 700 "${secret_dir}"
  tmp=$(mktemp "${secret_dir}/.${key}.XXXXXX")
  printf '%s' "${value}" > "${tmp}"
  chmod 600 "${tmp}"
  mv "${tmp}" "${target}"
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
    cpc_leaf_export_dir=$(make_temp_dir)
    run_nsc_quiet --data-dir "${nsc_dir}" export keys --account CSC --user "leaf-cpc-${cpc_id}" --dir "${cpc_leaf_export_dir}"

    local cpc_leaf_seed
    cpc_leaf_seed=$(head -n 1 "${cpc_leaf_export_dir}/${cpc_leaf_pubkey}.nk" | tr -d '[:space:]')

    write_secret_file "nats-leaf-cpc-${cpc_id}" "pubkey" "${cpc_leaf_pubkey}"
    write_secret_file "nats-leaf-cpc-${cpc_id}" "seed" "${cpc_leaf_seed}"
  done

  unset NKEYS_PATH
}

audit_secret_permissions() {
  local bad

  bad=$(find "${OUTPUT_DIR}/nkeys" -type f ! -perm 600 -print)
  if [ -n "${bad}" ]; then
    echo "ERROR: generated secret files must be mode 600:" >&2
    echo "${bad}" >&2
    exit 1
  fi

  bad=$(find "${OUTPUT_DIR}/nkeys" -type d ! -perm 700 -print)
  if [ -n "${bad}" ]; then
    echo "ERROR: generated secret directories must be mode 700:" >&2
    echo "${bad}" >&2
    exit 1
  fi
}

main() {
  parse_args "$@"
  check_prerequisites
  validate_cluster
  trap cleanup EXIT

  local dc_account
  dc_account=$(get_dc_account)

  echo "Generating secrets for ${CLUSTER}..."
  echo "Output directory: ${OUTPUT_DIR}"

  prepare_output_dir

  local nsc_dir
  nsc_dir=$(make_temp_dir)

  local keys_export_dir
  keys_export_dir=$(make_temp_dir)

  echo ""
  echo "=== Generating NSC keys ==="
  generate_nsc_keys "${nsc_dir}" "${dc_account}"

  echo ""
  echo "=== Writing NKey secrets ==="
  extract_key_values "${nsc_dir}" "${keys_export_dir}" "${dc_account}"

  generate_cpc_leaf_secrets "${nsc_dir}"
  audit_secret_permissions

  echo ""
  echo "=== Secret generation complete ==="
  echo ""
  echo "Secrets written to: ${OUTPUT_DIR}"
  echo ""
  echo "Directory structure:"
  ls -R "${OUTPUT_DIR}"
}

main "$@"
