# Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
# SPDX-License-Identifier: Apache-2.0

.PHONY: check-e2e-env e2e-kind help install-e2e-prereqs setup-infra setup-clusters setup-metallb setup-envoy-gateway setup-cert-manager setup-metrics-server setup-observability verify-infra clean clean-e2e clean-nats deploy-nats test-functional test-performance validate-nats status lint dummy-bms benchmark-basic benchmark-basic-full benchmark-performance

PERFORMANCE_E2E_ENV ?= PERF_TEST_PAIRS=1 PERF_TEST_DURATION=2s PERF_TEST_WARMUP=1s PERF_PUBLISH_DELAY=5ms PERF_MIN_SUCCESS_RATE=99
BENCHMARK_BASIC_FLAGS ?= --duration 5s --connection-clients 50 --connection-rate 25 --fanout-subscribers 20 --p2p-clients 20 --fanin-publishers 20 --fanin-subscribers 3 --fanin-topics 20
CSC_BROKER_URL ?= tcp://172.18.200.1:1883
CPC1_BROKER_URL ?= tcp://172.18.201.1:1883
CPC2_BROKER_URL ?= tcp://172.18.202.1:1883
MQTT_BROKERS ?= CSC=$(CSC_BROKER_URL),CPC-1=$(CPC1_BROKER_URL),CPC-2=$(CPC2_BROKER_URL)
NATS_BROKERS ?= CSC=nats://172.18.200.1:4222,CPC-1=nats://172.18.201.1:4222,CPC-2=nats://172.18.202.1:4222
MQTT_MTLS_BROKER ?= ssl://172.18.200.1:8883
MQTT_TCP_BROKER ?= $(CSC_BROKER_URL)
KEYCLOAK_URL ?= http://172.18.200.1
LOCAL_FUNCTIONAL_ENV ?= MQTT_BROKERS=$(MQTT_BROKERS) NATS_BROKERS=$(NATS_BROKERS) MQTT_MTLS_BROKER=$(MQTT_MTLS_BROKER) MQTT_TCP_BROKER=$(MQTT_TCP_BROKER) KEYCLOAK_URL=$(KEYCLOAK_URL)
LOCAL_PERFORMANCE_ENV ?= CSC_BROKER_URL=$(CSC_BROKER_URL) CPC1_BROKER_URL=$(CPC1_BROKER_URL) CPC2_BROKER_URL=$(CPC2_BROKER_URL)
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

e2e-kind: install-e2e-prereqs ## Set up local Kind clusters, deploy NATS, and run the e2e suite
	$(MAKE) setup-infra
	$(MAKE) deploy-nats
	$(MAKE) test-functional
	$(MAKE) test-performance

setup-infra: setup-clusters ## Setup complete infrastructure (clusters + metallb + envoy-gateway + cert-manager + metrics-server + keycloak + prometheus)
	$(MAKE) setup-metallb
	$(MAKE) setup-envoy-gateway
	$(MAKE) setup-cert-manager
	$(MAKE) setup-metrics-server
	$(MAKE) setup-observability
	$(MAKE) setup-keycloak
	@echo "Infrastructure setup complete"

setup-clusters: ## Create all Kind clusters (CSC, CPC-1, CPC-2)
	./infra/scripts/setup-clusters.sh

setup-metallb: ## Deploy MetalLB to all clusters
	./infra/scripts/setup-metallb.sh

setup-envoy-gateway: ## Deploy Envoy Gateway controllers
	./infra/scripts/setup-envoy-gateway.sh

setup-metrics-server: ## Deploy metrics-server to all clusters
	./infra/scripts/setup-metrics-server.sh

setup-cert-manager: ## Deploy cert-manager to all clusters
	./infra/scripts/setup-cert-manager.sh

setup-keycloak: ## Deploy Keycloak to all clusters
	./infra/scripts/setup-keycloak.sh

setup-observability: ## Deploy Prometheus and Grafana (optional)
	./infra/scripts/setup-observability.sh

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

deploy-nats: ## Deploy NATS to all layers
	@cd nats && ./deploy.sh csc
	@echo "Deploying NATS to CPC clusters..."
	@cd nats && ./deploy.sh cpc-1
	@cd nats && ./deploy.sh cpc-2
	@echo "CPC NATS deployments complete"

# ============================================================================
# Testing
# ============================================================================

check-e2e-env: ## Check local Kind/NATS prerequisites for e2e tests
	@echo "Checking local e2e prerequisites..."
	@for cluster in csc cpc-1 cpc-2; do \
		kind get clusters | grep -q "^$${cluster}$$" || { \
			echo "Missing Kind cluster: $${cluster}. Run 'make -C local setup-infra deploy-nats' first."; \
			exit 1; \
		}; \
	done
	@for context in kind-csc kind-cpc-1 kind-cpc-2; do \
		kubectl get namespace event-bus --context "$${context}" >/dev/null 2>&1 || { \
			echo "Missing event-bus namespace in $${context}. Run 'make -C local deploy-nats' first."; \
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
	cd mqtt-client && $(LOCAL_FUNCTIONAL_ENV) go test -count=1 -v ./tests/functional/ -timeout 60s

test-performance: check-e2e-env ## Run performance e2e smoke tests
	cd mqtt-client && $(LOCAL_PERFORMANCE_ENV) $(PERFORMANCE_E2E_ENV) go test -count=1 -v ./tests/performance/ -timeout 10m

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
	cd mqtt-client && $(LOCAL_PERFORMANCE_ENV) go test -count=1 -v ./tests/performance/ -timeout 10m
