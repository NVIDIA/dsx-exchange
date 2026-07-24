#!/usr/bin/env bash
# Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

relative_path="${1:?cache-relative path required}"
url="${2:?URL required}"
expected_sha256="${3:?SHA-256 required}"
repo_root="$(git rev-parse --show-toplevel)"
cache_root="${repo_root}/.cache"
target="${cache_root}/${relative_path}"

if [[ -s "${target}" ]]; then
  cached_sha256="$(openssl dgst -sha256 "${target}")"
  cached_sha256="${cached_sha256##* }"
  [[ "${cached_sha256}" == "${expected_sha256}" ]] && exit 0
  rm -f "${target}"
fi

tmp_dir="$(mktemp -d)"
trap 'rm -rf "${tmp_dir}"' EXIT
download="${tmp_dir}/download"

curl --fail --location --silent --show-error --retry 3 \
  --output "${download}" "${url}"
actual_sha256="$(openssl dgst -sha256 "${download}")"
actual_sha256="${actual_sha256##* }"
if [[ "${actual_sha256}" != "${expected_sha256}" ]]; then
  echo "cache: SHA-256 mismatch for ${url}" >&2
  exit 1
fi

mkdir -p "$(dirname "${target}")"
mv "${download}" "${target}"
