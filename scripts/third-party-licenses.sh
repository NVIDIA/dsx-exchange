#!/usr/bin/env bash
# Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
# SPDX-License-Identifier: Apache-2.0

set -Eeuo pipefail

repo_root="$(git rev-parse --show-toplevel)"
cd "${repo_root}"

case "${1:-fix}" in
  check|fix) mode="${1:-fix}" ;;
  *)
    echo "usage: scripts/third-party-licenses.sh [check|fix]" >&2
    exit 2
    ;;
esac

command -v go-licenses >/dev/null 2>&1 || {
  echo "third-party licenses: go-licenses is required" >&2
  exit 1
}

output="${repo_root}/${THIRD_PARTY_LICENSES_OUTPUT:-THIRD_PARTY_LICENSES.csv}"
raw="$(mktemp)"
normalized="$(mktemp)"
warnings="$(mktemp)"
cache_dir=""
if [[ -z "${GOCACHE:-}" ]]; then
  cache_dir="$(mktemp -d)"
  export GOCACHE="${cache_dir}"
fi
trap 'rm -f "${raw}" "${normalized}" "${warnings}"; [[ -z "${cache_dir}" ]] || rm -rf "${cache_dir}"' EXIT

module_dirs=()
if [[ -n "${GO_MODULE_DIRS:-}" ]]; then
  read -r -a module_dirs <<<"${GO_MODULE_DIRS}"
else
  while IFS= read -r go_mod; do
    module_dirs+=("$(dirname "${go_mod}")")
  done < <(
    find . \
      -path './.cache' -prune -o \
      -path './.git' -prune -o \
      -path '*/vendor' -prune -o \
      -name go.mod -print | sort
  )
fi

for module_dir in "${module_dirs[@]}"; do
  module_path="$(cd "${module_dir}" && go list -m)"
  if [[ -d "${module_dir}/vendor" ]]; then
    go_flags="-mod=vendor"
  else
    go_flags="-mod=readonly"
  fi

  if ! (
    cd "${module_dir}"
    module_goroot="$(go env GOROOT)"
    GOROOT="${module_goroot}" GOOS=linux GOARCH=amd64 GOFLAGS="${go_flags}" \
      go-licenses report ./...
  ) 2>>"${warnings}" |
    awk -F, -v module="${module_path}" '$1 != module && index($1, module "/") != 1' >>"${raw}"; then
    cat "${warnings}" >&2
    exit 1
  fi
done

while IFS=, read -r package_name license_url license_name; do
  if [[ "${license_url}" == "Unknown" && "${license_name}" == "Unknown" ]]; then
    for module_dir in "${module_dirs[@]}"; do
      package_dir="${module_dir}/vendor/${package_name}"
      [[ -d "${package_dir}" ]] || continue
      spdx="$(
        find "${package_dir}" -type f -print0 |
          xargs -0 awk '
            /SPDX-License-Identifier:/ {
              sub(/^.*SPDX-License-Identifier:[[:space:]]*/, "")
              gsub(/^[[:space:]]+|[[:space:]]+$/, "")
              print
            }
          ' |
          sort -u
      )"
      if [[ -n "${spdx}" && "$(printf '%s\n' "${spdx}" | wc -l | tr -d ' ')" == "1" ]]; then
        license_name="${spdx}"
        break
      fi
    done
  fi
  printf '%s,%s,%s\n' "${package_name}" "${license_url}" "${license_name}"
done <"${raw}" >"${normalized}"

while IFS= read -r override; do
  package_name="${override%%,*}"
  grep -q "^${package_name}," "${raw}" && printf '%s\n' "${override}" >>"${normalized}"
done <<'EOF'
github.com/klauspost/compress,Unknown,MIT
github.com/klauspost/compress,Unknown,BSD-3-Clause
go.opentelemetry.io/otel,Unknown,BSD-3-Clause
go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc,Unknown,BSD-3-Clause
go.opentelemetry.io/otel/exporters/otlp/otlptrace,Unknown,BSD-3-Clause
go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc,Unknown,BSD-3-Clause
go.opentelemetry.io/otel/exporters/prometheus,Unknown,BSD-3-Clause
go.opentelemetry.io/otel/log,Unknown,BSD-3-Clause
go.opentelemetry.io/otel/metric,Unknown,BSD-3-Clause
go.opentelemetry.io/otel/sdk,Unknown,BSD-3-Clause
go.opentelemetry.io/otel/sdk/metric,Unknown,BSD-3-Clause
go.opentelemetry.io/otel/trace,Unknown,BSD-3-Clause
gopkg.in/yaml.v3,Unknown,MIT
EOF

LC_ALL=C awk -F, '!seen[$1 "," $3]++' "${normalized}" | sort >"${raw}"

if [[ "${mode}" == "fix" ]]; then
  mv "${raw}" "${output}"
  raw=""
elif [[ ! -f "${output}" ]] || ! cmp -s "${raw}" "${output}"; then
  echo "third-party licenses: ${output#"${repo_root}"/} is stale; run 'make third-party-licenses'" >&2
  exit 1
fi

[[ -z "${DSX_LICENSE_VERBOSE:-}" || ! -s "${warnings}" ]] || cat "${warnings}" >&2
