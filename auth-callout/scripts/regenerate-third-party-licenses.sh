#!/usr/bin/env bash

# SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
auth_dir="$(cd "$script_dir/.." && pwd)"
repo_dir="$(cd "$auth_dir/.." && pwd)"

output="${1:-$repo_dir/THIRD_PARTY_LICENSES.csv}"
go_licenses="$auth_dir/tmp/bin/go-licenses"
if [[ ! -x "$go_licenses" ]]; then
	if ! go_licenses="$(command -v go-licenses)"; then
		echo "go-licenses not found; run 'make -C auth-callout install-tools' first" >&2
		exit 1
	fi
fi
cache_dir=""
if [[ -z "${GOCACHE:-}" ]]; then
	cache_dir="$(mktemp -d)"
	export GOCACHE="$cache_dir"
fi
licenses="$(mktemp)"
warnings="$(mktemp)"
trap 'rm -f "$licenses" "$warnings"; if [[ -n "$cache_dir" ]]; then rm -rf "$cache_dir"; fi' EXIT

report_module() {
	local module_dir="$1"
	local goflags="$2"

	if ! (
		cd "$module_dir"
		GOOS=linux GOARCH=amd64 GOFLAGS="$goflags" "$go_licenses" report ./...
	) 2>> "$warnings" | awk -F, '$1 !~ /^github\.com\/NVIDIA\/dsx-exchange\//' >> "$licenses"; then
		cat "$warnings" >&2
		exit 1
	fi
}

report_module "$auth_dir" "-mod=vendor"
report_module "$repo_dir/local/mqtt-client" ""

if [[ -n "${DSX_LICENSE_VERBOSE:-}" && -s "$warnings" ]]; then
	cat "$warnings" >&2
fi

# go-licenses v1 still supports vendored module projects, but its classifier
# collapses some multi-license packages to the first detected license.
cat >> "$licenses" <<'LICENSE_OVERRIDES'
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
LICENSE_OVERRIDES

awk -F, '!seen[$1 "," $3]++' "$licenses" | sort > "$output"
