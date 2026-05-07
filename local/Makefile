# Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
# SPDX-License-Identifier: Apache-2.0

.PHONY: help setup-infra setup-clusters setup-metallb setup-envoy-gateway setup-cert-manager setup-metrics-server setup-observability verify-infra clean clean-nats deploy-nats test-functional test-performance validate-nats status lint benchmark-basic

help: ## Show this help message
	@echo "DSX Event Bus - Evaluation Framework"
	@echo ""
	@echo "Available targets:"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "  %-25s %s\n", $$1, $$2}'

# ============================================================================
# Infrastructure Setup
# ============================================================================

setup-infra: setup-clusters ## Setup complete infrastructure (clusters + metallb + envoy-gateway + cert-manager + metrics-server + keycloak + prometheus)
	@echo "Deploying MetalLB, Envoy Gateway, cert-manager, Metrics Server, Keycloak, and Prometheus in parallel..."
	@./infra/scripts/setup-metallb.sh & \
	./infra/scripts/setup-envoy-gateway.sh & \
	./infra/scripts/setup-cert-manager.sh & \
	./infra/scripts/setup-metrics-server.sh & \
	./infra/scripts/setup-keycloak.sh & \
	./infra/scripts/setup-observability.sh & \
	wait
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

clean-nats: ## Delete NATS namespaces from all clusters
	cd nats && ./clean.sh all

# ============================================================================
# Deployment
# ============================================================================
# Note: Event bus deployment scripts are in their respective folders

deploy-nats: ## Deploy NATS to all layers
	@cd nats && ./deploy.sh csc
	@echo "Deploying NATS to CPC clusters in parallel..."
	@cd nats && ./deploy.sh cpc-1 & \
	cd nats && ./deploy.sh cpc-2 & \
	wait
	@echo "CPC NATS deployments complete"

# ============================================================================
# Testing
# ============================================================================

test-functional: ## Run functional tests (MQTT + federation)
	cd mqtt-client && go test -count=1 -v ./tests/functional/ -timeout 60s

test-performance: ## Run performance benchmarks
	cd mqtt-client && go test -count=1 -v ./tests/performance/ -timeout 10m

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

benchmark-basic: ## Run MQTT benchmark basic suite
	@GATEWAY_IP=$$(kubectl --context kind-csc get gateway -A -l app.kubernetes.io/component=event-bus-gateway -o jsonpath='{.items[0].status.addresses[0].value}') && \
	cd mqttbs && go run ./cmd/mqttbs run basic-suite --broker tcp://$$GATEWAY_IP:1883
