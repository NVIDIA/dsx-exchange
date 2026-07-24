#!/usr/bin/env bash
# Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

archive="${1:?archive name required}"
chart="${2:?chart required}"
version="${3:?version required}"
repo="${4:-}"
cache_dir="$(git rev-parse --show-toplevel)/.cache/helm"
target="${cache_dir}/${archive}"

[[ -s "${target}" ]] && exit 0

tmp_dir="$(mktemp -d)"
trap 'rm -rf "${tmp_dir}"' EXIT

args=(helm pull "${chart}" --version "${version}" --destination "${tmp_dir}")
[[ -z "${repo}" ]] || args+=(--repo "${repo}")
"${args[@]}" >/dev/null

mkdir -p "${cache_dir}"
mv "${tmp_dir}/${archive}" "${target}"
