# Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
# SPDX-License-Identifier: Apache-2.0

SHELL := /bin/bash
.SHELLFLAGS := -euo pipefail -c

ROOT_DIR := $(abspath $(dir $(lastword $(MAKEFILE_LIST))))
MISE ?= mise
export MISE_LOCKED := 1
MISE_EXEC := "$(MISE)" exec --cd "$(ROOT_DIR)" --

PERFORMANCE_E2E_ENV ?= PERF_TEST_PAIRS=1 PERF_TEST_DURATION=2s PERF_TEST_WARMUP=1s PERF_PUBLISH_DELAY=5ms PERF_MIN_SUCCESS_RATE=99
FUNCTIONAL_E2E_TIMEOUT ?= 3m
CSC_BROKER_URL ?= tcp://172.18.200.1:1883
.PHONY: add-license-headers check clean dummy-bms e2e help local-up perf-benchmark skaffold-dev test test-dev third-party-licenses

add-license-headers: ## Add SPDX license headers across repository sources
	$(MISE_EXEC) bash scripts/license.sh fix

check: ## Run static validation checks
	$(MISE_EXEC) bash local/scripts/prepare-dependencies.sh
	$(MISE_EXEC) bash scripts/license.sh check
	$(MISE_EXEC) bash scripts/third-party-licenses.sh check
	$(MISE_EXEC) helm lint auth-callout/deploy
	$(MISE_EXEC) helm template nats-event-bus deploy/nats-event-bus >/dev/null
	$(MISE_EXEC) helm lint deploy/nats-event-bus
	$(MISE_EXEC) helm lint deploy/dsx-agent-gateway --namespace csc-dsx-agentgateway --values local/agent-gateway/values/values.local-common.yaml --values local/agent-gateway/values/values.local.yaml

clean: ## Delete the local Kind cluster and generated secrets
	$(MAKE) -C "$(ROOT_DIR)/local" clean

local-up: ## Deploy the available components to the local Kind cluster
	$(MAKE) -C "$(ROOT_DIR)/local" skaffold-run

e2e: local-up ## Deploy the local stack and run live validation
	$(MAKE) test-dev

test: check ## Run the full validation suite
	$(MISE_EXEC) $(MAKE) -C "$(ROOT_DIR)/auth-callout" test
	$(MISE_EXEC) go -C auth-callout/tests test -short ./...
	$(MISE_EXEC) go -C local/mqtt-client test ./pkg/... ./internal/... ./cmd/...
	$(MISE_EXEC) go -C local/mqttbs test ./...
	$(MISE_EXEC) go -C dsx-agentgateway-bridge test ./...
	$(MISE_EXEC) go -C local/agent-gateway/charts/demoidp/demoidp test -run '^$$' ./...
	$(MISE_EXEC) go -C local/agent-gateway/charts/test-backends/test-mcp-backend test ./...
	$(MAKE) e2e

test-dev: ## Run live validation against an existing local stack
	$(MISE_EXEC) go -C local/mqtt-client test -count=1 -v ./tests/functional/ -timeout $(FUNCTIONAL_E2E_TIMEOUT)
	$(MISE_EXEC) env $(PERFORMANCE_E2E_ENV) go -C local/mqtt-client test -count=1 -v ./tests/performance/ -timeout 10m
	$(MISE_EXEC) bash local/agent-gateway/run.sh

skaffold-dev: ## Run Skaffold dev for the complete local stack
	$(MAKE) -C "$(ROOT_DIR)/local" skaffold-dev

third-party-licenses: ## Regenerate third-party license inventories
	$(MISE_EXEC) bash scripts/third-party-licenses.sh fix

perf-benchmark: ## Run Agent Gateway e2e with the sustained k6 benchmark profile
	$(MISE_EXEC) env RUN_PERF_PROFILE=benchmark bash local/agent-gateway/run.sh

dummy-bms: ## Publish looping dummy BMS data to the local CSC MQTT broker
	$(MISE_EXEC) go -C local/mqtt-client run ./cmd/dummy-bms --broker "$(CSC_BROKER_URL)" --csv examples/dsx_exemplar.csv --schema ../../schemas/asyncapi/bms/bms.yaml

help: ## Show available repository targets
	@grep -E '^[a-zA-Z0-9_.-]+:.*?## .*$$' "$(ROOT_DIR)/Makefile" | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "  %-28s %s\n", $$1, $$2}'
