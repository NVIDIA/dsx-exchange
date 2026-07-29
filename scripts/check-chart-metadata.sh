#!/usr/bin/env bash
# Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

repo_root="$(git rev-parse --show-toplevel)"
cd "${repo_root}"

max_description_len=255
failed=0

fail() {
  local chart="${1:?chart required}"
  local message="${2:?message required}"
  printf '%s %s\n' "$chart" "$message" >&2
  failed=1
}

for chart in deploy/*/Chart.yaml local/*/charts/*/Chart.yaml; do
  name="$(yq -r '.name // ""' "$chart")"
  version="$(yq -r '.version // ""' "$chart")"
  description="$(yq -r '.description // ""' "$chart")"

  if [[ -z "$name" ]]; then
    fail "$chart" "is missing .name"
  fi
  if [[ -z "$version" ]]; then
    fail "$chart" "is missing .version"
  fi
  if [[ -z "$description" ]]; then
    fail "$chart" "is missing .description"
    continue
  fi

  description_len=${#description}
  if (( description_len > max_description_len )); then
    fail "$chart" "description is ${description_len} characters; max is ${max_description_len}"
  fi
done

exit "$failed"
