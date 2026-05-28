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
EXTRA_ACCOUNTS=()
TEMP_DIRS=()

usage() {
  cat <<EOF
Usage: ${0} [OPTIONS] [cpc-ids...]

Generate NATS Event Bus NKeys to local files.

Without CPC IDs, only the CSC output is generated or left unchanged.
With CPC IDs, CSC and the requested CPC outputs are generated or left unchanged.
Extra accounts get one CPC-to-CSC leaf key pair per requested CPC.

Options:
  -o, --output DIR         Output root directory (default: deploy/secrets)
      --extra-account NAME Generate CPC-to-CSC leaf keys for an extra account
  -h, --help               Show this help message

Arguments:
  cpc-ids                  Optional list of CPC IDs to generate with CSC

Examples:
  ${0}
  ${0} 1 2 3
  ${0} --extra-account LaunchLayer 1 2
  ${0} -o deploy/secrets 1 2
EOF
}

cleanup() {
  local dir

  for dir in "${TEMP_DIRS[@]}"; do
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
  command -v nk >/dev/null 2>&1 || missing+=("nk")

  if [ ${#missing[@]} -gt 0 ]; then
    echo "ERROR: Missing required tools: ${missing[*]}" >&2
    echo "Get nsc and nk from: https://github.com/nats-io/nsc/releases" >&2
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
      --extra-account)
        if [ $# -lt 2 ]; then
          echo "ERROR: $1 requires a value" >&2
          exit 1
        fi
        validate_extra_account_name "$2"
        EXTRA_ACCOUNTS+=("$2")
        shift 2
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

  validate_extra_account_tokens
}

validate_cpc_id() {
  local cpc_id="$1"

  if [[ ! "${cpc_id}" =~ ^[a-z0-9]+$ ]]; then
    echo "ERROR: Invalid CPC ID: ${cpc_id} (use lower-case letters and numbers only)" >&2
    exit 1
  fi
}

validate_extra_account_name() {
  local account_name="$1"
  local account_upper

  if [[ ! "${account_name}" =~ ^[A-Za-z][A-Za-z0-9]*$ ]]; then
    echo "ERROR: Invalid extra account name: ${account_name} (use letters and numbers only, starting with a letter)" >&2
    exit 1
  fi

  account_upper=$(printf '%s' "${account_name}" | tr '[:lower:]' '[:upper:]')
  case "${account_upper}" in
    SYS|AUTH|AUTHX|CSC|CPC)
      echo "ERROR: Invalid extra account name: ${account_name} conflicts with a built-in NATS account" >&2
      exit 1
      ;;
  esac

  case "${account_upper}" in
    CPC*)
      echo "ERROR: Invalid extra account name: ${account_name} must not start with the cpc prefix" >&2
      exit 1
      ;;
  esac
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

validate_extra_account_tokens() {
  local account_name
  local token
  local seen_tokens=""

  for account_name in "${EXTRA_ACCOUNTS[@]}"; do
    token=$(extra_account_secret_token "${account_name}")
    case " ${seen_tokens} " in
      *" ${token} "*)
        echo "ERROR: extra account ${account_name} normalizes to duplicate secret token ${token}" >&2
        exit 1
        ;;
    esac
    seen_tokens="${seen_tokens} ${token}"
  done
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

read_nkey_line() {
  local file="$1"
  local line="$2"

  sed -n "${line}p" "${file}" | tr -d '[:space:]'
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

write_key_pair_secret() {
  local output_dir="$1"
  local secret_name="$2"
  local seed="$3"
  local pubkey="$4"

  write_secret_value "${output_dir}" "${secret_name}" "seed" "${seed}"
  write_secret_value "${output_dir}" "${secret_name}" "pubkey" "${pubkey}"
}

validate_nkey_pair() {
  local type="$1"
  local seed="$2"
  local pubkey="$3"
  local label="$4"
  local seed_prefix
  local pubkey_prefix

  case "${type}" in
    account) seed_prefix="SA"; pubkey_prefix="A" ;;
    curve) seed_prefix="SX"; pubkey_prefix="X" ;;
    user) seed_prefix="SU"; pubkey_prefix="U" ;;
    *) echo "ERROR: unsupported NKey type: ${type}" >&2; exit 1 ;;
  esac

  if [[ -z "${seed}" || -z "${pubkey}" || "${seed}" != ${seed_prefix}* || "${pubkey}" != ${pubkey_prefix}* ]]; then
    echo "ERROR: failed to generate a valid ${type} NKey for ${label}" >&2
    exit 1
  fi
}

generate_nkey_pair() {
  local type="$1"
  local label="$2"
  local nkey_dir
  local nkey_file
  local seed
  local pubkey
  local log

  echo "Generating ${label}..." >&2
  nkey_dir=$(make_temp_dir)
  nkey_file="${nkey_dir}/key.nk"
  log="${nkey_dir}/nsc.log"
  if ! nsc generate nkey "--${type}" > "${nkey_file}" 2> "${log}"; then
    cat "${log}" >&2
    exit 1
  fi
  chmod 600 "${nkey_file}"

  seed=$(read_nkey_line "${nkey_file}" 1)
  pubkey=$(read_nkey_line "${nkey_file}" 2)
  validate_nkey_pair "${type}" "${seed}" "${pubkey}" "${label}"

  printf '%s %s\n' "${seed}" "${pubkey}"
}

generate_base_cluster_secrets() {
  local output_dir="$1"
  local cluster="$2"
  local auth_signing_seed auth_signing_pubkey
  local xkey_seed xkey_pubkey
  local authx_user_seed authx_user_pubkey
  local nack_user_seed nack_user_pubkey
  local mtls_leaf_seed mtls_leaf_pubkey
  local mtls_authx_leaf_seed mtls_authx_leaf_pubkey
  local mtls_sys_leaf_seed mtls_sys_leaf_pubkey
  local surveyor_seed surveyor_pubkey

  read -r auth_signing_seed auth_signing_pubkey <<< "$(generate_nkey_pair account "${cluster} auth signing key")"
  read -r xkey_seed xkey_pubkey <<< "$(generate_nkey_pair curve "${cluster} XKey")"
  read -r authx_user_seed authx_user_pubkey <<< "$(generate_nkey_pair user "${cluster} authx user")"
  read -r nack_user_seed nack_user_pubkey <<< "$(generate_nkey_pair user "${cluster} NACK user")"
  read -r mtls_leaf_seed mtls_leaf_pubkey <<< "$(generate_nkey_pair user "${cluster} mTLS leaf user")"
  read -r mtls_authx_leaf_seed mtls_authx_leaf_pubkey <<< "$(generate_nkey_pair user "${cluster} mTLS authx leaf user")"
  read -r mtls_sys_leaf_seed mtls_sys_leaf_pubkey <<< "$(generate_nkey_pair user "${cluster} mTLS SYS leaf user")"
  read -r surveyor_seed surveyor_pubkey <<< "$(generate_nkey_pair user "${cluster} surveyor user")"

  write_key_pair_secret "${output_dir}" "nats-auth-signing" "${auth_signing_seed}" "${auth_signing_pubkey}"
  write_key_pair_secret "${output_dir}" "nats-xkey" "${xkey_seed}" "${xkey_pubkey}"
  write_key_pair_secret "${output_dir}" "nats-authx-user" "${authx_user_seed}" "${authx_user_pubkey}"
  write_key_pair_secret "${output_dir}" "nats-nack-user" "${nack_user_seed}" "${nack_user_pubkey}"
  write_secret_value "${output_dir}" "nats-nack-user" "nack-user.nk" "${nack_user_seed}"
  write_key_pair_secret "${output_dir}" "nats-mtls-leaf" "${mtls_leaf_seed}" "${mtls_leaf_pubkey}"
  write_key_pair_secret "${output_dir}" "nats-mtls-authx-leaf" "${mtls_authx_leaf_seed}" "${mtls_authx_leaf_pubkey}"
  write_key_pair_secret "${output_dir}" "nats-mtls-sys-leaf" "${mtls_sys_leaf_seed}" "${mtls_sys_leaf_pubkey}"
  write_key_pair_secret "${output_dir}" "nats-surveyor" "${surveyor_seed}" "${surveyor_pubkey}"
  write_secret_value "${output_dir}" "auth-callout-keys" "nkey-seed" "${authx_user_seed}"
  write_secret_value "${output_dir}" "auth-callout-keys" "issuer-seed" "${auth_signing_seed}"
  write_secret_value "${output_dir}" "auth-callout-keys" "xkey-seed" "${xkey_seed}"
}

generate_cluster() {
  local cluster="$1"
  local output_dir

  output_dir=$(cluster_output_dir "${cluster}")

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

  echo "Writing NKey secrets for ${cluster}..."
  generate_base_cluster_secrets "${output_dir}" "${cluster}"

  audit_secret_permissions "${output_dir}"
}

generate_named_leaf_secret_pair() {
  local cpc_id="$1"
  local csc_secret_name="$2"
  local cpc_secret_name="$3"
  local label="$4"
  local csc_output_dir
  local cpc_output_dir
  local seed
  local pubkey

  csc_output_dir=$(cluster_output_dir "csc")
  cpc_output_dir=$(cluster_output_dir "cpc-${cpc_id}")
  read -r seed pubkey <<< "$(generate_nkey_pair user "${label}")"

  write_secret_value "${csc_output_dir}" "${csc_secret_name}" "pubkey" "${pubkey}"
  write_secret_value "${cpc_output_dir}" "${cpc_secret_name}" "seed" "${seed}"
  remove_unused_leaf_keys "${csc_output_dir}" "${csc_secret_name}" "${cpc_output_dir}" "${cpc_secret_name}"
}

secret_key_exists() {
  local output_dir="$1"
  local secret_name="$2"
  local key="$3"

  [ -s "${output_dir}/nkeys/${secret_name}/${key}" ]
}

secret_started() {
  local output_dir="$1"
  local secret_name="$2"

  [ -e "${output_dir}/nkeys/${secret_name}" ]
}

leaf_outputs_match() {
  local csc_output_dir="$1"
  local cpc_output_dir="$2"
  local csc_secret_name="$3"
  local cpc_secret_name="$4"
  local csc_pubkey
  local cpc_pubkey

  csc_pubkey=$(tr -d '[:space:]' < "${csc_output_dir}/nkeys/${csc_secret_name}/pubkey")
  cpc_pubkey=$(nk -inkey "${cpc_output_dir}/nkeys/${cpc_secret_name}/seed" -pubout | tr -d '[:space:]')

  [ "${csc_pubkey}" = "${cpc_pubkey}" ]
}

remove_unused_leaf_keys() {
  local csc_output_dir="$1"
  local csc_secret_name="$2"
  local cpc_output_dir="$3"
  local cpc_secret_name="$4"

  rm -f "${csc_output_dir}/nkeys/${csc_secret_name}/seed"
  rm -f "${cpc_output_dir}/nkeys/${cpc_secret_name}/pubkey"
}

generate_leaf_outputs_pair() {
  local cpc_id="$1"
  local csc_secret_name="$2"
  local cpc_secret_name="$3"
  local label="$4"
  local csc_output_dir
  local cpc_output_dir
  local csc_leaf_exists=false
  local cpc_leaf_exists=false

  csc_output_dir=$(cluster_output_dir "csc")
  cpc_output_dir=$(cluster_output_dir "cpc-${cpc_id}")

  echo ""
  echo "=== CPC-${cpc_id}: ${label} leaf secret ==="

  if secret_key_exists "${csc_output_dir}" "${csc_secret_name}" "pubkey"; then
    csc_leaf_exists=true
  fi
  if secret_key_exists "${cpc_output_dir}" "${cpc_secret_name}" "seed"; then
    cpc_leaf_exists=true
  fi

  if [ "${csc_leaf_exists}" = "true" ] && [ "${cpc_leaf_exists}" = "true" ]; then
    if ! leaf_outputs_match "${csc_output_dir}" "${cpc_output_dir}" "${csc_secret_name}" "${cpc_secret_name}"; then
      echo "ERROR: mismatched leaf secret output for CPC-${cpc_id}" >&2
      exit 1
    fi
    remove_unused_leaf_keys "${csc_output_dir}" "${csc_secret_name}" "${cpc_output_dir}" "${cpc_secret_name}"

    echo "Leaf secrets already exist for CPC-${cpc_id}; leaving them unchanged."
  elif [ "${csc_leaf_exists}" = "false" ] && [ "${cpc_leaf_exists}" = "false" ] \
    && ! secret_started "${csc_output_dir}" "${csc_secret_name}" \
    && ! secret_started "${cpc_output_dir}" "${cpc_secret_name}"; then
    echo "Generating ${label} leaf secret for CPC-${cpc_id}..."
    generate_named_leaf_secret_pair "${cpc_id}" "${csc_secret_name}" "${cpc_secret_name}" "${label} CPC-${cpc_id}"
  else
    echo "ERROR: inconsistent ${label} leaf secret output for CPC-${cpc_id}" >&2
    exit 1
  fi

  audit_secret_permissions "${csc_output_dir}"
  audit_secret_permissions "${cpc_output_dir}"
}

generate_cpc_leaf_outputs() {
  local cpc_id="$1"

  generate_leaf_outputs_pair \
    "${cpc_id}" \
    "nats-leaf-cpc-${cpc_id}" \
    "nats-leaf-csc" \
    "CSC"
}

generate_extra_account_leaf_outputs() {
  local account_name="$1"
  local cpc_id="$2"
  local account_token

  account_token=$(extra_account_secret_token "${account_name}")

  generate_leaf_outputs_pair \
    "${cpc_id}" \
    "nats-leaf-${account_token}-cpc-${cpc_id}" \
    "nats-leaf-${account_token}-csc" \
    "${account_name} extra-account"
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
  local extra_account

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
      for extra_account in "${EXTRA_ACCOUNTS[@]}"; do
        generate_extra_account_leaf_outputs "${extra_account}" "${cpc_id}"
      done
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
