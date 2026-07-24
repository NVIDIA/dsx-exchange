#!/usr/bin/env bash
# Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
# SPDX-License-Identifier: Apache-2.0

set -Eeuo pipefail

REPO_ROOT="$(git rev-parse --show-toplevel)"
cd "${REPO_ROOT}"

HEADER_OWNER="NVIDIA CORPORATION & AFFILIATES. All rights reserved."
HEADER_YEAR="2026"

usage=$'Usage:\n  bash scripts/license.sh check\n  bash scripts/license.sh fix'

case "${1:-}" in
	check)
		mode="check"
		;;
	fix)
		mode="fix"
		;;
	-h|--help|help|'')
		echo "${usage}"
		;;
	*)
		echo "${usage}"
		exit 2
		;;
esac

[[ -n "${mode:-}" ]] || exit 0

if ! command -v addlicense >/dev/null 2>&1; then
	echo "license: addlicense is required" >&2
	exit 1
fi

args=(
	-c "${HEADER_OWNER}"
	-y "${HEADER_YEAR}"
	-l apache
	-s=only
	-ignore '.cache/**'
	-ignore '.git/**'
	-ignore '.mise/**'
	-ignore '**/*.md'
	-ignore '**/*.png'
	-ignore '**/*.sum'
	-ignore '**/Chart.lock'
	-ignore '**/tmp/**'
	-ignore '**/vendor/**'
	-ignore 'auth-callout/vault-agent/templates/**'
	-ignore 'deploy/**/charts/**'
	-ignore 'docs/schema-viewer/**'
	-ignore 'LICENSE'
	-ignore 'local/**/charts/**/charts/**'
	-ignore 'local/infra/local-registry/zot-config.json'
	-ignore 'local/mqtt-client/tests/performance/reports/**'
	-ignore 'local/event-bus/certs/**'
	-ignore 'local/event-bus/keys/**'
	-ignore 'local/event-bus/nsc/**'
	-ignore 'local/event-bus/secrets/**'
	-ignore 'local/**/secret-generators/**/generated/**'
	-ignore 'tests/agent-gateway/artifacts/**'
	-ignore 'THIRD_PARTY_LICENSES*'
)
[[ "${mode}" == "check" ]] && args=(-check "${args[@]}")

addlicense "${args[@]}" .
