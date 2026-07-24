# Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
# SPDX-License-Identifier: Apache-2.0

SHELL := /bin/bash
.SHELLFLAGS := -euo pipefail -c

MISE ?= mise
export MISE_LOCKED := 1
MISE_EXEC := $(MISE) exec --

.PHONY: add-license-headers check check-license-headers clean-e2e dummy-bms help install-e2e-prereqs local-up preflight prepare-local-dependencies skaffold-dev test test-dev third-party-licenses

preflight: ## Install the locked repository toolchain
	$(MISE) install --locked

add-license-headers: ## Add SPDX license headers across repository sources
	$(MISE_EXEC) bash scripts/license.sh fix

check-license-headers: ## Verify SPDX license headers across repository sources
	$(MISE_EXEC) bash scripts/license.sh check

prepare-local-dependencies:
	$(MISE_EXEC) bash local/infra/scripts/prepare-local-dependencies.sh

check: prepare-local-dependencies ## Run static validation checks
	$(MISE_EXEC) bash scripts/license.sh check
	$(MISE_EXEC) helm lint auth-callout/deploy
	$(MISE_EXEC) helm template nats-event-bus deploy/nats-event-bus >/dev/null
	$(MISE_EXEC) helm lint deploy/nats-event-bus

clean-e2e: ## Delete local Kind clusters and generated e2e artifacts
	$(MISE_EXEC) $(MAKE) -C local clean

dummy-bms: ## Publish looping dummy BMS data to the local CSC MQTT broker
	$(MISE_EXEC) $(MAKE) -C local dummy-bms

local-up: ## Deploy three logical sites to one Kind cluster by default
	$(MISE_EXEC) $(MAKE) -C local skaffold-run

install-e2e-prereqs: ## Install tools required by local Kind e2e workflows
	$(MISE) install --locked

test: ## Run the full validation suite
	$(MAKE) check
	$(MISE_EXEC) $(MAKE) -C auth-callout test
	cd auth-callout/tests && $(MISE_EXEC) go test -short ./...
	cd local/mqtt-client && $(MISE_EXEC) go test ./pkg/... ./internal/... ./cmd/...
	cd local/mqttbs && $(MISE_EXEC) go test ./...
	$(MISE_EXEC) $(MAKE) -C local test

test-dev: ## Run local e2e tests against an already running local stack
	$(MISE_EXEC) $(MAKE) -C local test-dev

skaffold-dev: ## Run Skaffold dev for the complete local dev stack
	$(MISE_EXEC) $(MAKE) -C local skaffold-dev

third-party-licenses: ## Regenerate third-party license inventory
	$(MISE_EXEC) bash scripts/third-party-licenses.sh

help: ## Show this help message
	@grep -E '^[a-zA-Z0-9_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "  %-28s %s\n", $$1, $$2}'
