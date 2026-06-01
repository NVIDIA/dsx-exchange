# Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
# SPDX-License-Identifier: Apache-2.0

.PHONY: add-license-headers check check-license-headers clean-e2e dummy-bms e2e-kind help install-e2e-prereqs test test-e2e test-helm third-party-licenses

add-license-headers: ## Add SPDX license headers across repository sources
	bash scripts/license.sh fix

check-license-headers: ## Verify SPDX license headers across repository sources
	bash scripts/license.sh check

check: check-license-headers test test-helm ## Run all local validation checks

clean-e2e: ## Delete local Kind clusters and generated e2e artifacts
	$(MAKE) -C local clean-e2e

dummy-bms: ## Publish looping dummy BMS data to the local CSC MQTT broker
	$(MAKE) -C local dummy-bms

e2e-kind: ## Set up the local Kind e2e environment and run the full e2e suite
	$(MAKE) -C local e2e-kind

install-e2e-prereqs: ## Install tools required by local Kind e2e workflows
	$(MAKE) -C local install-e2e-prereqs

test: ## Run unit tests that do not require the local Kind environment
	$(MAKE) -C auth-callout test
	cd auth-callout/tests && go test -short ./...
	cd local/mqtt-client && go test ./pkg/... ./internal/... ./cmd/...
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
