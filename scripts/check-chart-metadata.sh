#!/usr/bin/env bash
# Copyright 2026 NVIDIA Corporation
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.
#
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
