# Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
# SPDX-License-Identifier: Apache-2.0

.PHONY: add-license-headers check check-license-headers clean-e2e dummy-bms help install-e2e-prereqs mcp-build mcp-lint mcp-sync-specs mcp-test skaffold-dev test test-dev third-party-licenses

add-license-headers: ## Add SPDX license headers across repository sources
	bash scripts/license.sh fix

check-license-headers: ## Verify SPDX license headers across repository sources
	bash scripts/license.sh check

check: ## Run static validation checks
	bash scripts/license.sh check
	helm lint auth-callout/deploy
	helm template --dependency-update --repository-config local/helm/repositories.yaml nats-event-bus deploy/nats-event-bus >/dev/null
	helm lint deploy/nats-event-bus
	helm lint mcp/dsx-exchange-mcp/deploy/helm/dsx-exchange-mcp

clean-e2e: ## Delete local Kind clusters and generated e2e artifacts
	$(MAKE) -C local clean

dummy-bms: ## Publish looping dummy BMS data to the local CSC MQTT broker
	$(MAKE) -C local dummy-bms

install-e2e-prereqs: ## Install tools required by local Kind e2e workflows
	$(MAKE) -C local install-e2e-prereqs

mcp-build: ## Build the DSX Exchange MCP server
	$(MAKE) -C mcp/dsx-exchange-mcp build

mcp-lint: ## Run DSX Exchange MCP static checks
	$(MAKE) -C mcp/dsx-exchange-mcp lint

mcp-sync-specs: ## Refresh the DSX Exchange MCP embedded schema copy
	$(MAKE) -C mcp/dsx-exchange-mcp sync-specs

mcp-test: ## Run DSX Exchange MCP unit tests
	$(MAKE) -C mcp/dsx-exchange-mcp test

test: ## Run the full validation suite
	$(MAKE) check
	$(MAKE) -C auth-callout test
	cd auth-callout/tests && go test -short ./...
	cd local/mqtt-client && go test ./pkg/... ./internal/... ./cmd/...
	cd local/mqttbs && go test ./...
	$(MAKE) -C mcp/dsx-exchange-mcp test
	$(MAKE) -C local test

test-dev: ## Run local e2e tests against an already running local stack
	$(MAKE) -C local test-dev

skaffold-dev: ## Run Skaffold dev for the complete local dev stack
	$(MAKE) -C local skaffold-dev

third-party-licenses: ## Regenerate third-party license inventory
	$(MAKE) -C auth-callout third-party-licenses

help: ## Show this help message
	@grep -E '^[a-zA-Z0-9_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "  %-28s %s\n", $$1, $$2}'
