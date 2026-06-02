# Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
# SPDX-License-Identifier: Apache-2.0

.PHONY: check-e2e-env e2e-kind help install-e2e-prereqs setup-infra setup-clusters setup-local-registry setup-metallb setup-envoy-gateway setup-cert-manager setup-metrics-server setup-observability skaffold-run skaffold-dev verify-infra clean clean-e2e clean-nats deploy-nats test-functional test-performance validate-nats status lint dummy-bms benchmark-basic benchmark-basic-full benchmark-performance

PERFORMANCE_E2E_ENV ?= PERF_TEST_PAIRS=1 PERF_TEST_DURATION=2s PERF_TEST_WARMUP=1s PERF_PUBLISH_DELAY=5ms PERF_MIN_SUCCESS_RATE=99
BENCHMARK_BASIC_FLAGS ?= --duration 5s --connection-clients 50 --connection-rate 25 --fanout-subscribers 20 --p2p-clients 20 --fanin-publishers 20 --fanin-subscribers 3 --fanin-topics 20
CSC_BROKER_URL ?= tcp://172.18.200.1:1883
SKAFFOLD ?= skaffold
GO_PATH ?= $(shell go env GOPATH 2>/dev/null)
GO_BIN ?= $(shell go env GOBIN 2>/dev/null)
E2E_PREREQS_BIN ?= $(if $(GO_BIN),$(GO_BIN),$(if $(GO_PATH),$(GO_PATH)/bin,$(HOME)/.local/bin))
export PATH := $(E2E_PREREQS_BIN):$(PATH)

help: ## Show this help message
	@echo "DSX Event Bus - Evaluation Framework"
	@echo ""
	@echo "Available targets:"
	@grep -E '^[a-zA-Z0-9_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "  %-25s %s\n", $$1, $$2}'

# ============================================================================
# Infrastructure Setup
# ============================================================================

install-e2e-prereqs: ## Install local Kind e2e CLI prerequisites into E2E_PREREQS_BIN
	E2E_PREREQS_BIN="$(E2E_PREREQS_BIN)" bash ./infra/scripts/install-e2e-prereqs.sh

e2e-kind: install-e2e-prereqs ## Set up local Kind clusters, deploy with Skaffold, and run the e2e suite
	$(MAKE) skaffold-run
	$(MAKE) validate-nats
	$(MAKE) test-functional
	$(MAKE) test-performance

setup-infra: setup-clusters ## Setup e2e infrastructure with Skaffold (Prometheus CRDs only; use setup-observability for Grafana/Prometheus)
	SKAFFOLD="$(SKAFFOLD)" ./infra/scripts/run-skaffold.sh local-infra-minimal
	@echo "Infrastructure setup complete"

setup-clusters: ## Create all Kind clusters (CSC, CPC-1, CPC-2)
	./infra/scripts/setup-clusters.sh

setup-local-registry: ## Create the local Docker registry used by Kind and Skaffold
	./infra/scripts/setup-local-registry.sh

setup-metallb: ## Deploy MetalLB to all clusters
	SKAFFOLD="$(SKAFFOLD)" ./infra/scripts/setup-metallb.sh

setup-envoy-gateway: ## Deploy Envoy Gateway controllers
	SKAFFOLD="$(SKAFFOLD)" ./infra/scripts/setup-envoy-gateway.sh

setup-metrics-server: ## Deploy metrics-server to all clusters
	SKAFFOLD="$(SKAFFOLD)" ./infra/scripts/setup-metrics-server.sh

setup-cert-manager: ## Deploy cert-manager to all clusters
	SKAFFOLD="$(SKAFFOLD)" ./infra/scripts/setup-cert-manager.sh

setup-keycloak: ## Deploy CSC Keycloak with Skaffold
	SKAFFOLD="$(SKAFFOLD)" ./infra/scripts/setup-keycloak.sh

setup-observability: ## Deploy the full Prometheus and Grafana stack with Skaffold (optional)
	SKAFFOLD="$(SKAFFOLD)" ./infra/scripts/setup-observability.sh

skaffold-run: setup-clusters ## Deploy the complete local dev stack with Skaffold
	SKAFFOLD="$(SKAFFOLD)" ./infra/scripts/run-skaffold.sh local-dev

skaffold-dev: setup-clusters ## Run Skaffold dev for the complete local stack
	cd .. && SKAFFOLD="$(SKAFFOLD)" "$(SKAFFOLD)" dev --module local-dev --cleanup=false

verify-infra: ## Verify infrastructure is ready
	./infra/scripts/verify-infrastructure.sh

clean: ## Delete all Kind clusters
	./infra/scripts/cleanup.sh

clean-e2e: ## Delete Kind clusters and generated local e2e artifacts
	./infra/scripts/cleanup.sh
	rm -rf nats/keys nats/nsc nats/certs nats/secrets mqtt-client/tests/performance/reports

clean-nats: ## Delete NATS namespaces from all clusters
	cd nats && ./clean.sh all

# ============================================================================
# Deployment
# ============================================================================
# Note: Event bus deployment scripts are in their respective folders

deploy-nats: ## Build auth-callout and deploy NATS to all layers with Skaffold
	SKAFFOLD="$(SKAFFOLD)" ./infra/scripts/run-skaffold.sh nats-all

# ============================================================================
# Testing
# ============================================================================

check-e2e-env: ## Check local Kind/NATS prerequisites for e2e tests
	@echo "Checking local e2e prerequisites..."
	@for cluster in csc cpc-1 cpc-2; do \
		kind get clusters | grep -q "^$${cluster}$$" || { \
			echo "Missing Kind cluster: $${cluster}. Run 'make -C local skaffold-run' first."; \
			exit 1; \
		}; \
	done
	@for context in kind-csc kind-cpc-1 kind-cpc-2; do \
		kubectl get namespace event-bus --context "$${context}" >/dev/null 2>&1 || { \
			echo "Missing event-bus namespace in $${context}. Run 'make -C local skaffold-run' first."; \
			exit 1; \
		}; \
	done
	@for context in kind-csc kind-cpc-1 kind-cpc-2; do \
		kubectl get gateway shared-gateway -n envoy-gateway-system --context "$${context}" -o jsonpath='{.status.conditions[?(@.type=="Programmed")].status}' | grep -q True || { \
			echo "shared-gateway is not programmed in $${context}. Run 'make -C local validate-nats' for details."; \
			exit 1; \
		}; \
	done

test-functional: check-e2e-env ## Run functional tests (MQTT + federation)
	cd mqtt-client && go test -count=1 -v ./tests/functional/ -timeout 60s

test-performance: check-e2e-env ## Run performance e2e smoke tests
	cd mqtt-client && $(PERFORMANCE_E2E_ENV) go test -count=1 -v ./tests/performance/ -timeout 10m

dummy-bms: check-e2e-env ## Publish looping dummy BMS data to the CSC MQTT broker
	cd mqtt-client && go run ./cmd/dummy-bms --broker "$(CSC_BROKER_URL)" --csv examples/dsx_exemplar.csv --schema ../../schemas/asyncapi/bms/bms.yaml

validate-nats: ## Validate NATS deployment on all clusters
	@echo "Validating CSC..."
	@cd nats && ./validate.sh csc
	@echo ""
	@echo "Validating CPC-1..."
	@cd nats && ./validate.sh cpc-1
	@echo ""
	@echo "Validating CPC-2..."
	@cd nats && ./validate.sh cpc-2

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

# ============================================================================
# Development
# ============================================================================

lint: ## Run linters
	cd mqtt-client && go vet ./...
	cd mqtt-client && go fmt ./...
	cd mqtt-client && go mod tidy

# ============================================================================
# MQTT Benchmark Suite
# ============================================================================

benchmark-basic: check-e2e-env ## Run MQTT benchmark basic suite
	cd mqttbs && go run ./cmd/mqttbs run basic-suite --broker "$(CSC_BROKER_URL)" $(BENCHMARK_BASIC_FLAGS)

benchmark-basic-full: check-e2e-env ## Run full MQTT benchmark basic suite
	cd mqttbs && go run ./cmd/mqttbs run basic-suite --broker "$(CSC_BROKER_URL)"

benchmark-performance: check-e2e-env ## Run full performance benchmarks
	cd mqtt-client && go test -count=1 -v ./tests/performance/ -timeout 10m
