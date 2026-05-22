#!/usr/bin/env bash
# Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
# SPDX-License-Identifier: Apache-2.0

#
# Generate NATS Event Bus NKeys to local files

set -euo pipefail
umask 077

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DEPLOY_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"

OUTPUT_ROOT=""
CPC_IDS=()
TEMP_DIRS=()

usage() {
  cat <<EOF
Usage: ${0} [OPTIONS] [cpc-ids...]

Generate NATS Event Bus NKeys to local files.

Without CPC IDs, only the CSC output is generated or left unchanged.
With CPC IDs, CSC and the requested CPC outputs are generated or left unchanged.

Options:
  -o, --output DIR         Output root directory (default: deploy/secrets)
  -h, --help               Show this help message

Arguments:
  cpc-ids                  Optional list of CPC IDs to generate with CSC

Examples:
  ${0}
  ${0} 1 2 3
  ${0} -o deploy/secrets 1 2
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
  while [[ $# -gt 0 ]]; do
    case $1 in
      -o|--output)
        if [ $# -lt 2 ]; then
          echo "ERROR: $1 requires a value" >&2
          exit 1
        fi
        OUTPUT_ROOT="$2"
        shift 2
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
        validate_cpc_id "$1"
        CPC_IDS+=("$1")
        shift
        ;;
    esac
  done

  if [ -z "${OUTPUT_ROOT}" ]; then
    OUTPUT_ROOT="${DEPLOY_DIR}/secrets"
  fi
}

validate_cpc_id() {
  local cpc_id="$1"

  if [[ ! "${cpc_id}" =~ ^[A-Za-z0-9._-]+$ ]]; then
    echo "ERROR: Invalid CPC ID: ${cpc_id} (use letters, numbers, '.', '_', or '-')" >&2
    exit 1
  fi
}

prepare_output_root() {
  if [ -z "${OUTPUT_ROOT}" ] || [ "${OUTPUT_ROOT}" = "/" ]; then
    echo "ERROR: refusing unsafe output root: ${OUTPUT_ROOT}" >&2
    exit 1
  fi

  mkdir -p "${OUTPUT_ROOT}"
  chmod 700 "${OUTPUT_ROOT}"
}

cluster_output_dir() {
  local cluster="$1"

  echo "${OUTPUT_ROOT}/${cluster}"
}

prepare_output_dir() {
  local output_dir="$1"
  local nkeys_dir="${output_dir}/nkeys"

  if [ -z "${output_dir}" ] || [ "${output_dir}" = "/" ]; then
    echo "ERROR: refusing unsafe output directory: ${output_dir}" >&2
    exit 1
  fi

  mkdir -p "${output_dir}"
  chmod 700 "${output_dir}"
  mkdir -p "${nkeys_dir}"
  chmod 700 "${nkeys_dir}"
}

get_dc_account() {
  local cluster="$1"

  if [ "${cluster}" = "csc" ]; then
    echo "CSC"
  elif [[ "${cluster}" =~ ^cpc- ]]; then
    echo "CPC"
  else
    echo "ERROR: invalid cluster: ${cluster}" >&2
    exit 1
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

run_nsc_store_quiet() {
  local nsc_dir="$1"

  shift
  run_nsc_quiet \
    --config-dir "${nsc_dir}/config" \
    --data-dir "${nsc_dir}/store" \
    --keystore-dir "${nsc_dir}/keys" \
    "$@"
}

nsc_store() {
  local nsc_dir="$1"

  shift
  nsc \
    --config-dir "${nsc_dir}/config" \
    --data-dir "${nsc_dir}/store" \
    --keystore-dir "${nsc_dir}/keys" \
    "$@"
}

generate_nsc_keys() {
  local nsc_dir="$1"
  local cluster="$2"
  local dc_account="$3"

  export NKEYS_PATH="${nsc_dir}/keys"

  echo "Creating operator op-${cluster}..."
  run_nsc_store_quiet "${nsc_dir}" add operator --name "op-${cluster}"

  echo "Creating AUTH account..."
  run_nsc_store_quiet "${nsc_dir}" add account AUTH
  run_nsc_store_quiet "${nsc_dir}" edit account AUTH --sk generate

  echo "Creating AUTHX account..."
  run_nsc_store_quiet "${nsc_dir}" add account AUTHX

  echo "Creating AUTHX user (authx)..."
  run_nsc_store_quiet "${nsc_dir}" add user --account AUTHX --name authx

  echo "Creating AUTHX leaf user (authx-leaf)..."
  run_nsc_store_quiet "${nsc_dir}" add user --account AUTHX --name "authx-leaf"

  echo "Creating ${dc_account} account..."
  run_nsc_store_quiet "${nsc_dir}" add account "${dc_account}"

  echo "Creating NACK user..."
  run_nsc_store_quiet "${nsc_dir}" add user --account "${dc_account}" --name nack

  echo "Creating mTLS leaf user..."
  run_nsc_store_quiet "${nsc_dir}" add user --account "${dc_account}" --name "mtls-leaf"

  echo "Creating SYS account..."
  run_nsc_store_quiet "${nsc_dir}" add account SYS

  echo "Creating mTLS SYS leaf user..."
  run_nsc_store_quiet "${nsc_dir}" add user --account SYS --name "mtls-sys-leaf"

  echo "Creating surveyor user..."
  run_nsc_store_quiet "${nsc_dir}" add user --account SYS --name "surveyor"

  echo "Generating XKey..."
  nsc_store "${nsc_dir}" generate nkey --curve > "${nsc_dir}/xkey.nk"
  chmod 600 "${nsc_dir}/xkey.nk"

  unset NKEYS_PATH
}

extract_key_values() {
  local nsc_dir="$1"
  local keys_export_dir="$2"
  local output_dir="$3"
  local dc_account="$4"

  export NKEYS_PATH="${nsc_dir}/keys"

  run_nsc_store_quiet "${nsc_dir}" export keys --account AUTH --accounts --dir "${keys_export_dir}"
  run_nsc_store_quiet "${nsc_dir}" export keys --account AUTHX --users --dir "${keys_export_dir}"
  run_nsc_store_quiet "${nsc_dir}" export keys --account "${dc_account}" --users --dir "${keys_export_dir}"
  run_nsc_store_quiet "${nsc_dir}" export keys --account SYS --users --dir "${keys_export_dir}"

  local auth_signing_key
  auth_signing_key=$(nsc_store "${nsc_dir}" describe account AUTH --json 2>/dev/null | jq -r '.nats.signing_keys[0]')

  local authx_user_pubkey
  authx_user_pubkey=$(nsc_store "${nsc_dir}" describe user -a AUTHX authx --json 2>/dev/null | jq -r '.sub')

  local authx_leaf_user_pubkey
  authx_leaf_user_pubkey=$(nsc_store "${nsc_dir}" describe user -a AUTHX "authx-leaf" --json 2>/dev/null | jq -r '.sub')

  local nack_user_pubkey
  nack_user_pubkey=$(nsc_store "${nsc_dir}" describe user -a "${dc_account}" nack --json 2>/dev/null | jq -r '.sub')

  local mtls_leaf_user_pubkey
  mtls_leaf_user_pubkey=$(nsc_store "${nsc_dir}" describe user -a "${dc_account}" "mtls-leaf" --json 2>/dev/null | jq -r '.sub')

  local mtls_sys_leaf_user_pubkey
  mtls_sys_leaf_user_pubkey=$(nsc_store "${nsc_dir}" describe user -a SYS "mtls-sys-leaf" --json 2>/dev/null | jq -r '.sub')

  local surveyor_user_pubkey
  surveyor_user_pubkey=$(nsc_store "${nsc_dir}" describe user -a SYS "surveyor" --json 2>/dev/null | jq -r '.sub')

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

  write_secret_value "${output_dir}" "nats-auth-signing" "pubkey" "${auth_signing_key}"
  write_secret_value "${output_dir}" "nats-auth-signing" "seed" "${auth_signing_seed}"

  write_secret_value "${output_dir}" "nats-xkey" "pubkey" "${xkey_pubkey}"
  write_secret_value "${output_dir}" "nats-xkey" "seed" "${xkey_seed}"

  write_secret_value "${output_dir}" "nats-authx-user" "pubkey" "${authx_user_pubkey}"
  write_secret_value "${output_dir}" "nats-authx-user" "seed" "${authx_user_seed}"

  write_secret_value "${output_dir}" "nats-nack-user" "pubkey" "${nack_user_pubkey}"
  write_secret_value "${output_dir}" "nats-nack-user" "seed" "${nack_user_seed}"
  cp "${keys_export_dir}/${nack_user_pubkey}.nk" "${output_dir}/nkeys/nats-nack-user/nack-user.nk"
  chmod 600 "${output_dir}/nkeys/nats-nack-user/nack-user.nk"

  write_secret_value "${output_dir}" "nats-mtls-leaf" "pubkey" "${mtls_leaf_user_pubkey}"
  write_secret_value "${output_dir}" "nats-mtls-leaf" "seed" "${mtls_leaf_user_seed}"

  write_secret_value "${output_dir}" "nats-mtls-authx-leaf" "pubkey" "${authx_leaf_user_pubkey}"
  write_secret_value "${output_dir}" "nats-mtls-authx-leaf" "seed" "${authx_leaf_user_seed}"

  write_secret_value "${output_dir}" "nats-mtls-sys-leaf" "pubkey" "${mtls_sys_leaf_user_pubkey}"
  write_secret_value "${output_dir}" "nats-mtls-sys-leaf" "seed" "${mtls_sys_leaf_user_seed}"

  write_secret_value "${output_dir}" "nats-surveyor" "pubkey" "${surveyor_user_pubkey}"
  write_secret_value "${output_dir}" "nats-surveyor" "seed" "${surveyor_user_seed}"

  write_secret_value "${output_dir}" "auth-callout-keys" "nkey-seed" "${authx_user_seed}"
  write_secret_value "${output_dir}" "auth-callout-keys" "issuer-seed" "${auth_signing_seed}"
  write_secret_value "${output_dir}" "auth-callout-keys" "xkey-seed" "${xkey_seed}"
}

write_secret_value() {
  local output_dir="$1"
  local secret_name="$2"
  local key="$3"
  local value="$4"
  local secret_dir="${output_dir}/nkeys/${secret_name}"
  local target="${secret_dir}/${key}"
  local tmp

  mkdir -p "${secret_dir}"
  chmod 700 "${secret_dir}"
  tmp=$(mktemp "${secret_dir}/.${key}.XXXXXX")
  printf '%s' "${value}" > "${tmp}"
  chmod 600 "${tmp}"
  mv "${tmp}" "${target}"
}

generate_cluster() {
  local cluster="$1"
  local output_dir
  local dc_account
  local nsc_dir
  local keys_export_dir

  output_dir=$(cluster_output_dir "${cluster}")
  dc_account=$(get_dc_account "${cluster}")

  echo ""
  echo "=== ${cluster}: NKey secrets ==="
  echo "Output directory: ${output_dir}"

  if [ -d "${output_dir}/nkeys" ]; then
    echo "Secrets already exist for ${cluster}; leaving them unchanged."
    audit_secret_permissions "${output_dir}"
    return 0
  fi

  echo "Generating secrets for ${cluster}..."
  prepare_output_dir "${output_dir}"

  nsc_dir=$(make_temp_dir)
  keys_export_dir=$(make_temp_dir)

  echo "Generating NSC keys for ${cluster}..."
  generate_nsc_keys "${nsc_dir}" "${cluster}" "${dc_account}"

  echo "Writing NKey secrets for ${cluster}..."
  extract_key_values "${nsc_dir}" "${keys_export_dir}" "${output_dir}" "${dc_account}"

  audit_secret_permissions "${output_dir}"
}

copy_leaf_secret() {
  local source_output_dir="$1"
  local source_secret_name="$2"
  local target_output_dir="$3"
  local target_secret_name="$4"
  local seed
  local pubkey

  seed=$(tr -d '[:space:]' < "${source_output_dir}/nkeys/${source_secret_name}/seed")
  pubkey=$(tr -d '[:space:]' < "${source_output_dir}/nkeys/${source_secret_name}/pubkey")

  write_secret_value "${target_output_dir}" "${target_secret_name}" "seed" "${seed}"
  write_secret_value "${target_output_dir}" "${target_secret_name}" "pubkey" "${pubkey}"
}

generate_leaf_secret_pair() {
  local cpc_id="$1"
  local csc_output_dir
  local cpc_output_dir
  local csc_secret_name="nats-leaf-cpc-${cpc_id}"
  local cpc_secret_name="nats-leaf-csc"
  local leaf_file
  local leaf_dir
  local seed
  local pubkey

  csc_output_dir=$(cluster_output_dir "csc")
  cpc_output_dir=$(cluster_output_dir "cpc-${cpc_id}")
  leaf_dir=$(make_temp_dir)
  leaf_file="${leaf_dir}/leaf.nk"
  nsc_store "${leaf_dir}" generate nkey --user > "${leaf_file}"
  chmod 600 "${leaf_file}"

  seed=$(sed -n '1p' "${leaf_file}" | tr -d '[:space:]')
  pubkey=$(sed -n '2p' "${leaf_file}" | tr -d '[:space:]')
  if [[ -z "${seed}" || -z "${pubkey}" || "${seed}" != SU* || "${pubkey}" != U* ]]; then
    echo "ERROR: failed to generate a valid CPC leaf user NKey for CPC-${cpc_id}" >&2
    exit 1
  fi

  write_secret_value "${csc_output_dir}" "${csc_secret_name}" "seed" "${seed}"
  write_secret_value "${csc_output_dir}" "${csc_secret_name}" "pubkey" "${pubkey}"
  copy_leaf_secret "${csc_output_dir}" "${csc_secret_name}" "${cpc_output_dir}" "${cpc_secret_name}"
}

leaf_secret_exists() {
  local output_dir="$1"
  local secret_name="$2"
  local secret_dir="${output_dir}/nkeys/${secret_name}"

  [ -s "${secret_dir}/seed" ] && [ -s "${secret_dir}/pubkey" ]
}

leaf_secret_started() {
  local output_dir="$1"
  local secret_name="$2"

  [ -e "${output_dir}/nkeys/${secret_name}" ]
}

leaf_secrets_match() {
  local csc_output_dir="$1"
  local cpc_output_dir="$2"
  local csc_secret_name="$3"
  local cpc_secret_name="$4"

  cmp -s "${csc_output_dir}/nkeys/${csc_secret_name}/seed" "${cpc_output_dir}/nkeys/${cpc_secret_name}/seed" \
    && cmp -s "${csc_output_dir}/nkeys/${csc_secret_name}/pubkey" "${cpc_output_dir}/nkeys/${cpc_secret_name}/pubkey"
}

generate_cpc_leaf_outputs() {
  local cpc_id="$1"
  local csc_output_dir
  local cpc_output_dir
  local csc_secret_name="nats-leaf-cpc-${cpc_id}"
  local cpc_secret_name="nats-leaf-csc"
  local csc_leaf_exists=false
  local cpc_leaf_exists=false

  csc_output_dir=$(cluster_output_dir "csc")
  cpc_output_dir=$(cluster_output_dir "cpc-${cpc_id}")

  echo ""
  echo "=== CPC-${cpc_id}: CSC leaf secret ==="

  if leaf_secret_exists "${csc_output_dir}" "${csc_secret_name}"; then
    csc_leaf_exists=true
  fi
  if leaf_secret_exists "${cpc_output_dir}" "${cpc_secret_name}"; then
    cpc_leaf_exists=true
  fi

  if [ "${csc_leaf_exists}" = "true" ] && [ "${cpc_leaf_exists}" = "true" ]; then
    if ! leaf_secrets_match "${csc_output_dir}" "${cpc_output_dir}" "${csc_secret_name}" "${cpc_secret_name}"; then
      echo "ERROR: mismatched leaf secret output for CPC-${cpc_id}" >&2
      exit 1
    fi

    echo "Leaf secrets already exist for CPC-${cpc_id}; leaving them unchanged."
  elif [ "${csc_leaf_exists}" = "false" ] && [ "${cpc_leaf_exists}" = "false" ] \
    && ! leaf_secret_started "${csc_output_dir}" "${csc_secret_name}" \
    && ! leaf_secret_started "${cpc_output_dir}" "${cpc_secret_name}"; then
    echo "Generating CSC leaf secret for CPC-${cpc_id}..."
    generate_leaf_secret_pair "${cpc_id}"
  else
    echo "ERROR: inconsistent leaf secret output for CPC-${cpc_id}" >&2
    exit 1
  fi

  audit_secret_permissions "${csc_output_dir}"
  audit_secret_permissions "${cpc_output_dir}"
}

audit_secret_permissions() {
  local output_dir="$1"
  local bad

  bad=$(find "${output_dir}/nkeys" -type f ! -perm 600 -print)
  if [ -n "${bad}" ]; then
    echo "ERROR: generated secret files must be mode 600:" >&2
    echo "${bad}" >&2
    exit 1
  fi

  bad=$(find "${output_dir}/nkeys" -type d ! -perm 700 -print)
  if [ -n "${bad}" ]; then
    echo "ERROR: generated secret directories must be mode 700:" >&2
    echo "${bad}" >&2
    exit 1
  fi
}

main() {
  local cpc_id

  parse_args "$@"
  check_prerequisites
  trap cleanup EXIT

  echo "Generating NATS Event Bus NKey outputs..."
  echo "Output directory: ${OUTPUT_ROOT}"

  prepare_output_root
  generate_cluster "csc"

  if [ ${#CPC_IDS[@]} -gt 0 ]; then
    for cpc_id in "${CPC_IDS[@]}"; do
      generate_cluster "cpc-${cpc_id}"
      generate_cpc_leaf_outputs "${cpc_id}"
    done
  fi

  echo ""
  echo "=== Secret generation complete ==="
  echo ""
  echo "Secrets written under: ${OUTPUT_ROOT}"
  echo ""
  echo "Directory structure:"
  ls -R "${OUTPUT_ROOT}"
}

main "$@"
