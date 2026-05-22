# Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
# SPDX-License-Identifier: Apache-2.0

.PHONY: add-license-headers check check-license-headers clean-e2e help test test-e2e test-helm third-party-licenses

COPYRIGHT_HOLDER := NVIDIA CORPORATION & AFFILIATES. All rights reserved.
COPYRIGHT_YEAR := 2026
LICENSE_TARGETS := local schema
LICENSE_IGNORES := \
	-ignore "**/*.png" \
	-ignore "**/go.sum" \
	-ignore "**/tests/performance/reports/**" \
	-ignore "**/vendor/**"

add-license-headers: ## Add SPDX license headers across repository sources
	addlicense -l apache -c "$(COPYRIGHT_HOLDER)" -s=only -y "$(COPYRIGHT_YEAR)" $(LICENSE_IGNORES) -v $(LICENSE_TARGETS)
	$(MAKE) -C auth-callout add-license-headers
	$(MAKE) -C deploy add-license-headers

check-license-headers: ## Verify SPDX license headers across repository sources
	addlicense -l apache -c "$(COPYRIGHT_HOLDER)" -s=only -y "$(COPYRIGHT_YEAR)" $(LICENSE_IGNORES) -check $(LICENSE_TARGETS)
	$(MAKE) -C auth-callout check-license-headers
	$(MAKE) -C deploy check-license-headers

check: check-license-headers test test-helm ## Run all local validation checks

clean-e2e: ## Delete local Kind clusters and generated e2e artifacts
	$(MAKE) -C local clean-e2e

test: ## Run unit tests that do not require the local Kind environment
	$(MAKE) -C auth-callout test
	cd auth-callout/tests && go test -short ./...
	cd local/mqtt-client && go test ./pkg/...
	cd local/mqttbs && go test ./...

test-e2e: ## Run local functional and performance suites; requires Kind/NATS/Keycloak
	$(MAKE) -C local test-functional
	$(MAKE) -C local test-performance

test-helm: ## Run Helm chart validations
	helm lint auth-callout/deploy
	helm lint deploy/nats-event-bus

third-party-licenses: ## Regenerate third-party license inventory
	$(MAKE) -C auth-callout third-party-licenses

help: ## Show this help message
	@grep -E '^[a-zA-Z0-9_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "  %-28s %s\n", $$1, $$2}'
