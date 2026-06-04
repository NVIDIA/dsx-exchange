# Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
# SPDX-License-Identifier: Apache-2.0

.PHONY: help install-e2e-prereqs test test-serial test-dev
.PHONY: skaffold-run skaffold-run-serial skaffold-dev
.PHONY: clean status dummy-bms benchmark benchmark-full
.PHONY: setup-clusters
.PHONY: skaffold-build-artifacts
.PHONY: skaffold-deploy-csc skaffold-deploy-cpc-1 skaffold-deploy-cpc-2

PERFORMANCE_E2E_ENV ?= PERF_TEST_PAIRS=1 PERF_TEST_DURATION=2s PERF_TEST_WARMUP=1s PERF_PUBLISH_DELAY=5ms PERF_MIN_SUCCESS_RATE=99
FUNCTIONAL_E2E_TIMEOUT ?= 3m
BENCHMARK_BASIC_FLAGS ?= --duration 5s --connection-clients 50 --connection-rate 25 --fanout-subscribers 20 --p2p-clients 20 --fanin-publishers 20 --fanin-subscribers 3 --fanin-topics 20
CSC_BROKER_URL ?= tcp://172.18.200.1:1883
SKAFFOLD ?= skaffold
SKAFFOLD_CACHE_FILE ?= $(HOME)/.skaffold/cache
SKAFFOLD_REMOTE_CACHE_DIR ?= $(HOME)/.skaffold/remote-cache
SKAFFOLD_BUILD_ARTIFACTS ?= $(CURDIR)/.skaffold-build-artifacts.json
SKAFFOLD_RUN_LABEL ?= skaffold.dev/run-id=local-e2e
SKAFFOLD_BUILD_FLAGS ?= --cache-artifacts=true --sync-remote-cache=missing
SKAFFOLD_DEPLOY_FLAGS ?= --sync-remote-cache=missing --label="$(SKAFFOLD_RUN_LABEL)"
SKAFFOLD_DEV_FLAGS ?= --cleanup=false --cache-artifacts=false --sync-remote-cache=missing --label="$(SKAFFOLD_RUN_LABEL)"
SKAFFOLD_INFRA_MODULES := infra-csc infra-cpc-1 infra-cpc-2
SKAFFOLD_NATS_MODULES := nats-csc nats-cpc-1 nats-cpc-2
SKAFFOLD_STACK_MODULES := $(SKAFFOLD_INFRA_MODULES) $(SKAFFOLD_NATS_MODULES)
SKAFFOLD_CSC_MODULES := infra-csc nats-csc
SKAFFOLD_CPC_1_MODULES := infra-cpc-1 nats-cpc-1
SKAFFOLD_CPC_2_MODULES := infra-cpc-2 nats-cpc-2
SKAFFOLD_CLUSTER_TARGETS := skaffold-deploy-csc skaffold-deploy-cpc-1 skaffold-deploy-cpc-2
GO_PATH ?= $(shell go env GOPATH 2>/dev/null)
GO_BIN ?= $(shell go env GOBIN 2>/dev/null)
E2E_PREREQS_BIN ?= $(if $(GO_BIN),$(GO_BIN),$(if $(GO_PATH),$(GO_PATH)/bin,$(HOME)/.local/bin))
export PATH := $(E2E_PREREQS_BIN):$(PATH)

define skaffold_module_args
$(foreach module,$(1),--module "$(module)")
endef

help: ## Show this help message
	@echo "DSX Event Bus - Evaluation Framework"
	@echo ""
	@echo "Available targets:"
	@grep -E '^[a-zA-Z0-9_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "  %-35s %s\n", $$1, $$2}'

# ============================================================================
# Infrastructure Setup
# ============================================================================

install-e2e-prereqs: ## Install local Kind e2e CLI prerequisites into E2E_PREREQS_BIN
	E2E_PREREQS_BIN="$(E2E_PREREQS_BIN)" bash ./infra/scripts/install-e2e-prereqs.sh

test: ## Run the full local Kind e2e suite
	$(MAKE) skaffold-run
	$(MAKE) test-dev

test-serial: ## Run the full local Kind e2e suite with serial deploy
	$(MAKE) skaffold-run-serial
	$(MAKE) test-dev

test-dev: ## Run the local e2e tests against an already running local stack
	cd mqtt-client && go test -count=1 -v ./tests/functional/ -timeout $(FUNCTIONAL_E2E_TIMEOUT)
	cd mqtt-client && $(PERFORMANCE_E2E_ENV) go test -count=1 -v ./tests/performance/ -timeout 10m

setup-clusters:
	./infra/scripts/setup-clusters.sh

skaffold-run: install-e2e-prereqs ## Deploy the complete local dev stack
	$(MAKE) setup-clusters
	$(MAKE) skaffold-build-artifacts
	$(MAKE) -j$(words $(SKAFFOLD_CLUSTER_TARGETS)) $(SKAFFOLD_CLUSTER_TARGETS)

skaffold-run-serial: install-e2e-prereqs ## Deploy the complete local dev stack serially
	$(MAKE) setup-clusters
	$(MAKE) skaffold-build-artifacts
	cd .. && "$(SKAFFOLD)" deploy -f skaffold.yaml $(call skaffold_module_args,$(SKAFFOLD_STACK_MODULES)) $(SKAFFOLD_DEPLOY_FLAGS) --build-artifacts="$(SKAFFOLD_BUILD_ARTIFACTS)" --remote-cache-dir="$(SKAFFOLD_REMOTE_CACHE_DIR)/serial"

skaffold-build-artifacts:
	cd .. && "$(SKAFFOLD)" build -f skaffold.yaml $(SKAFFOLD_BUILD_FLAGS) --cache-file="$(SKAFFOLD_CACHE_FILE).build" --remote-cache-dir="$(SKAFFOLD_REMOTE_CACHE_DIR)/build" --build-concurrency=1 --file-output="$(SKAFFOLD_BUILD_ARTIFACTS)"

skaffold-deploy-csc:
	cd .. && "$(SKAFFOLD)" deploy -f skaffold.yaml $(call skaffold_module_args,$(SKAFFOLD_CSC_MODULES)) $(SKAFFOLD_DEPLOY_FLAGS) --build-artifacts="$(SKAFFOLD_BUILD_ARTIFACTS)" --remote-cache-dir="$(SKAFFOLD_REMOTE_CACHE_DIR)/$@"

skaffold-deploy-cpc-1:
	cd .. && "$(SKAFFOLD)" deploy -f skaffold.yaml $(call skaffold_module_args,$(SKAFFOLD_CPC_1_MODULES)) $(SKAFFOLD_DEPLOY_FLAGS) --build-artifacts="$(SKAFFOLD_BUILD_ARTIFACTS)" --remote-cache-dir="$(SKAFFOLD_REMOTE_CACHE_DIR)/$@"

skaffold-deploy-cpc-2:
	cd .. && "$(SKAFFOLD)" deploy -f skaffold.yaml $(call skaffold_module_args,$(SKAFFOLD_CPC_2_MODULES)) $(SKAFFOLD_DEPLOY_FLAGS) --build-artifacts="$(SKAFFOLD_BUILD_ARTIFACTS)" --remote-cache-dir="$(SKAFFOLD_REMOTE_CACHE_DIR)/$@"

skaffold-dev: install-e2e-prereqs ## Run Skaffold dev for the complete local dev stack
	$(MAKE) setup-clusters
	cd .. && "$(SKAFFOLD)" dev -f skaffold.yaml $(call skaffold_module_args,$(SKAFFOLD_STACK_MODULES)) $(SKAFFOLD_DEV_FLAGS) --cache-file="$(SKAFFOLD_CACHE_FILE).dev" --remote-cache-dir="$(SKAFFOLD_REMOTE_CACHE_DIR)/dev" --build-concurrency=1

clean: ## Delete Kind clusters and generated local e2e artifacts
	./infra/scripts/cleanup.sh
	rm -rf .skaffold-build-artifacts.json nats/keys nats/nsc nats/certs nats/secrets mqtt-client/tests/performance/reports
	rm -rf ../deploy/nats-event-bus/charts ../deploy/nats-event-bus/Chart.lock

# ============================================================================
# Testing
# ============================================================================

dummy-bms: ## Publish looping dummy BMS data to the CSC MQTT broker
	cd mqtt-client && go run ./cmd/dummy-bms --broker "$(CSC_BROKER_URL)" --csv examples/dsx_exemplar.csv --schema ../../schemas/asyncapi/bms/bms.yaml

# ============================================================================
# Monitoring & Debugging
# ============================================================================

status: ## Check deployment status
	@echo "CSC Cluster:"
	@kubectl get pods -n event-bus --context kind-csc 2>/dev/null || echo "  NATS: Not deployed"
	@echo ""
	@echo "CPC-1 Cluster:"
	@kubectl get pods -n event-bus --context kind-cpc-1 2>/dev/null || echo "  NATS: Not deployed"
	@echo ""
	@echo "CPC-2 Cluster:"
	@kubectl get pods -n event-bus --context kind-cpc-2 2>/dev/null || echo "  NATS: Not deployed"

benchmark: ## Run the MQTT benchmark smoke suite
	cd mqttbs && go run ./cmd/mqttbs run basic-suite --broker "$(CSC_BROKER_URL)" $(BENCHMARK_BASIC_FLAGS)

benchmark-full: ## Run the full MQTT benchmark suite
	cd mqttbs && go run ./cmd/mqttbs run basic-suite --broker "$(CSC_BROKER_URL)"
