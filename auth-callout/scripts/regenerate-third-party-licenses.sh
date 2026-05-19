#!/usr/bin/env bash

# SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

output="${1:-../THIRD_PARTY_LICENSES.csv}"
licenses="$(mktemp)"
warnings="$(mktemp)"
trap 'rm -f "$licenses" "$warnings"' EXIT

if ! GOOS=linux GOARCH=amd64 GOFLAGS=-mod=vendor go-licenses report ./... 2> "$warnings" \
	| awk -F, '$1 !~ /^github\.com\/NVIDIA\/dsx-exchange\/auth-callout\//' \
	> "$licenses"; then
	cat "$warnings" >&2
	exit 1
fi

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

sort -u "$licenses" > "$output"
