# Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
# SPDX-License-Identifier: Apache-2.0

.PHONY: add-license-headers check check-license-headers clean-e2e dummy-bms help install-e2e-prereqs test test-dev third-party-licenses

add-license-headers: ## Add SPDX license headers across repository sources
	bash scripts/license.sh fix

check-license-headers: ## Verify SPDX license headers across repository sources
	bash scripts/license.sh check

check: ## Run static validation checks
	bash scripts/license.sh check
	helm lint auth-callout/deploy
	helm template --dependency-update nats-event-bus deploy/nats-event-bus >/dev/null
	helm lint deploy/nats-event-bus

clean-e2e: ## Delete local Kind clusters and generated e2e artifacts
	$(MAKE) -C local clean

dummy-bms: ## Publish looping dummy BMS data to the local CSC MQTT broker
	$(MAKE) -C local dummy-bms

install-e2e-prereqs: ## Install tools required by local Kind e2e workflows
	$(MAKE) -C local install-e2e-prereqs

test: ## Run the full validation suite
	$(MAKE) check
	$(MAKE) -C auth-callout test
	cd auth-callout/tests && go test -short ./...
	cd local/mqtt-client && go test ./pkg/... ./internal/... ./cmd/...
	cd local/mqttbs && go test ./...
	$(MAKE) -C local test

test-dev: ## Run local e2e tests against an already running local stack
	$(MAKE) -C local test-dev

third-party-licenses: ## Regenerate third-party license inventory
	$(MAKE) -C auth-callout third-party-licenses

help: ## Show this help message
	@grep -E '^[a-zA-Z0-9_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "  %-28s %s\n", $$1, $$2}'
