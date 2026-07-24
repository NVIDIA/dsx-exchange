# Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
# SPDX-License-Identifier: Apache-2.0

.PHONY: help install-e2e-prereqs test test-dev
.PHONY: skaffold-run skaffold-dev
.PHONY: clean status validate dummy-bms benchmark benchmark-full
.PHONY: setup-clusters prepare-local-dependencies

PERFORMANCE_E2E_ENV ?= PERF_TEST_PAIRS=1 PERF_TEST_DURATION=2s PERF_TEST_WARMUP=1s PERF_PUBLISH_DELAY=5ms PERF_MIN_SUCCESS_RATE=99
FUNCTIONAL_E2E_TIMEOUT ?= 3m
BENCHMARK_BASIC_FLAGS ?= --duration 5s --connection-clients 50 --connection-rate 25 --fanout-subscribers 20 --p2p-clients 20 --fanin-publishers 20 --fanin-subscribers 3 --fanin-topics 20
CSC_BROKER_URL ?= tcp://172.18.200.1:1883
MISE ?= mise
SKAFFOLD ?= skaffold
SKAFFOLD_FLAGS ?= --cleanup=false --label=skaffold.dev/run-id=local-e2e
SKAFFOLD_BUILD_MIRROR_REGISTRY ?= localhost:5001

ifeq ($(MULTI_CLUSTER),1)
CSC_CONTEXT := kind-csc
CPC1_CONTEXT := kind-cpc-1
CPC2_CONTEXT := kind-cpc-2
else
CSC_CONTEXT := kind-dsx-exchange
CPC1_CONTEXT := kind-dsx-exchange
CPC2_CONTEXT := kind-dsx-exchange
endif

help: ## Show this help message
	@echo "DSX Event Bus - Evaluation Framework"
	@echo ""
	@echo "Available targets:"
	@grep -E '^[a-zA-Z0-9_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "  %-35s %s\n", $$1, $$2}'

# ============================================================================
# Infrastructure Setup
# ============================================================================

install-e2e-prereqs: ## Install the locked repository toolchain
	cd .. && MISE_LOCKED=1 "$(MISE)" install --locked

test: ## Run the full local Kind e2e suite
	$(MAKE) skaffold-run
	$(MAKE) test-dev

test-dev: ## Run the local e2e tests against an already running local stack
	cd mqtt-client && go test -count=1 -v ./tests/functional/ -timeout $(FUNCTIONAL_E2E_TIMEOUT)
	cd mqtt-client && $(PERFORMANCE_E2E_ENV) go test -count=1 -v ./tests/performance/ -timeout 10m

setup-clusters:
	./infra/scripts/setup-clusters.sh

prepare-local-dependencies:
	./infra/scripts/prepare-local-dependencies.sh

skaffold-run: install-e2e-prereqs prepare-local-dependencies ## Deploy the complete local dev stack
	$(MAKE) setup-clusters
	cd .. && SKAFFOLD_BUILD_MIRROR_REGISTRY="$(SKAFFOLD_BUILD_MIRROR_REGISTRY)" "$(SKAFFOLD)" run -f skaffold.yaml $(SKAFFOLD_FLAGS)

skaffold-dev: install-e2e-prereqs prepare-local-dependencies ## Run Skaffold dev for the complete local dev stack
	$(MAKE) setup-clusters
	cd .. && SKAFFOLD_BUILD_MIRROR_REGISTRY="$(SKAFFOLD_BUILD_MIRROR_REGISTRY)" "$(SKAFFOLD)" dev -f skaffold.yaml $(SKAFFOLD_FLAGS)

clean: ## Delete Kind clusters and generated local e2e artifacts
	./infra/scripts/cleanup.sh
	rm -rf nats/keys nats/nsc nats/certs nats/secrets mqtt-client/tests/performance/reports
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
	@echo "CSC site:"
	@kubectl get pods -n csc-event-bus --context $(CSC_CONTEXT) 2>/dev/null || echo "  NATS: Not deployed"
	@echo ""
	@echo "CPC-1 site:"
	@kubectl get pods -n cpc-1-event-bus --context $(CPC1_CONTEXT) 2>/dev/null || echo "  NATS: Not deployed"
	@echo ""
	@echo "CPC-2 site:"
	@kubectl get pods -n cpc-2-event-bus --context $(CPC2_CONTEXT) 2>/dev/null || echo "  NATS: Not deployed"

validate: ## Validate the deployed local stack
	./validate.sh

benchmark: ## Run the MQTT benchmark smoke suite
	cd mqttbs && go run ./cmd/mqttbs run basic-suite --broker "$(CSC_BROKER_URL)" $(BENCHMARK_BASIC_FLAGS)

benchmark-full: ## Run the full MQTT benchmark suite
	cd mqttbs && go run ./cmd/mqttbs run basic-suite --broker "$(CSC_BROKER_URL)"
