# Auth-callout Service Makefile

.PHONY: add-license-headers build check-license-headers clean clean-modcache deps-update dev dev-build dev-debug dev-debug-suspend dev-debug-suspend-with-delve-logs dev-debug-with-delve-logs dev-lint devspace-all devspace-macos docker-build docker-run fmt godoc help init-hooks init-project install-tools lint pre-commit-run release run run-service test test-helm test-integration test-integration-devspace third-party-licenses version

# Variables
BINARY_NAME=auth-callout
DOCKER_IMAGE=auth-callout:latest
DOCKERFILE_PATH=build/package/Dockerfile
GOFLAGS=-mod=vendor
TEST_FLAGS=-race -v -count 1
GOCMD=go
GO_LICENSES_VERSION=v1.6.0
ADDLICENSE_VERSION=v1.2.0
COPYRIGHT_HOLDER=NVIDIA CORPORATION & AFFILIATES. All rights reserved.
COPYRIGHT_YEAR=2026
GIT_HOOKS_DIR=$(shell git rev-parse --git-path hooks 2>/dev/null || echo .git/hooks)
TOOLS_BIN=$(CURDIR)/tmp/bin
GO_LICENSES=$(TOOLS_BIN)/go-licenses

build: ## Build the binary
	go build $(GOFLAGS) -o $(BINARY_NAME) src/cmd/auth_callout/main.go

clean: ## Clean build artifacts
	rm -f $(BINARY_NAME)
	docker rmi $(DOCKER_IMAGE) 2>/dev/null || true

clean-modcache: ## Clean module cache
	go clean -modcache

deps-update: ## Update dependencies
	go get ./...
	go mod tidy
	go mod vendor

#======================================================================================================================
# DEVSPACE-specific targets
#======================================================================================================================

dev: ## Start with hot reloading
	@echo "Starting with hot reloading..."
	@echo "   Application will restart automatically when you make changes"
	@mkdir -p tmp
	@air

dev-build: ## Build the binary
	go build $(GOFLAGS) -o $(BINARY_NAME) src/cmd/auth_callout/main.go

dev-lint: lint ## Run linter

dev-debug: ## Start with debugger (continues immediately)
	@echo "→ Starting debugger on port 5005 (continues immediately)..."
	@echo "  → Application will start running right away"
	@echo "  → Connect your debugger to localhost:5005 anytime"
	@echo "  → Use VS Code 'Connect to DevSpace Debugger' configuration"
	@echo "  → Use Ctrl+C to stop"
	@mkdir -p tmp
	go build $(GOFLAGS) -gcflags="all=-N -l" -o tmp/$(BINARY_NAME) ./src/cmd/auth_callout/main.go
	@bash -c '$(DELVE_START_SCRIPT)'

dev-debug-with-delve-logs: ## Start debugger with delve logging turned on (continues immediately)
	@echo "→ Starting debugger on port 5005 (continues immediately)..."
	@echo "  → Application will start running right away"
	@echo "  → Connect your debugger to localhost:5005 anytime"
	@echo "  → Use VS Code 'Connect to DevSpace Debugger' configuration"
	@echo "  → Use Ctrl+C to stop"
	@mkdir -p tmp
	go build $(GOFLAGS) -gcflags="all=-N -l" -o tmp/$(BINARY_NAME) ./src/cmd/auth_callout/main.go
	@bash -c '$(DELVE_START_WITH_LOGS_SCRIPT)'

dev-debug-suspend: ## Start with debugger (waits for debugger attachment)
	@echo "→ Starting debugger on port 5005 (suspended)..."
	@echo "  → Application will wait for debugger connection before starting"
	@echo "  → Connect your debugger to localhost:5005 to begin execution"
	@echo "  → Use VS Code 'Connect to DevSpace Debugger' configuration"
	@echo "  → Use Ctrl+C to stop"
	@mkdir -p tmp
	go build $(GOFLAGS) -gcflags="all=-N -l" -o tmp/$(BINARY_NAME) ./src/cmd/auth_callout/main.go
	@bash -c '$(DELVE_SUSPEND_SCRIPT)'

dev-debug-suspend-with-delve-logs: ## Start debugger with delve logging turned on (continues immediately)
	@echo "→ Starting debugger on port 5005 (continues immediately)..."
	@echo "  → Application will start running right away"
	@echo "  → Connect your debugger to localhost:5005 anytime"
	@echo "  → Use VS Code 'Connect to DevSpace Debugger' configuration"
	@echo "  → Use Ctrl+C to stop"
	@mkdir -p tmp
	go build $(GOFLAGS) -gcflags="all=-N -l" -o tmp/$(BINARY_NAME) ./src/cmd/auth_callout/main.go
	@bash -c '$(DELVE_SUSPEND_WITH_LOGS_SCRIPT)'


devspace-all: ## Run all devspace profiles
	devspace dev -p obs,authz,vault -n auth-callout

devspace-macos: ## Run all devspace profiles on macOS
	@echo "#====================================================================================================================="
	@echo "Freezing devspace.yaml to prevent it from being checked in with arm64 changes"
	@echo "#====================================================================================================================="
	git update-index --skip-worktree devspace.yaml
	@echo "#====================================================================================================================="
	echo "Updating devspace.yaml to use arm64"
	@echo "#====================================================================================================================="
	sed -i '' \
		-e 's|TARGETARCH: amd64|TARGETARCH: arm64|' \
		-e 's|TARGETPLATFORM: linux\/amd64|TARGETPLATFORM: linux\/arm64|' \
		-e 's|devImage: $${GOLANG_1_25_ALPINE_DEV_IMAGE}|devImage: golang:1.25-alpine|' \
		-e 's|command: ./local-dev/scripts/devspace_start.sh|command: sh -c "apk add --no-cache bash make delve \&\& exec ./local-dev/scripts/devspace_start.sh"|' \
		devspace.yaml
	make devspace-all

#======================================================================================================================
# EndDEVSPACE-specific targets
#======================================================================================================================

docker-build: ## Build Docker image
	docker build -f $(DOCKERFILE_PATH) -t $(DOCKER_IMAGE) .

docker-run: docker-build ## Run service in Docker
	docker run --rm -p 8000:8000 $(DOCKER_IMAGE)

fmt: ## Format code
	go fmt ./...

godoc: ## Generate and serve documentation locally
	@echo "Starting documentation server at http://localhost:6060"
	@echo "Press Ctrl+C to stop"
	godoc -http=:6060

help: ## Show this help message
  ## Shows all targets in this file with a  '<target name>:  ##<description>' format
	@echo "Available commands:"
	@grep -E '^[a-zA-Z0-9_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-20s\033[0m %s\n", $$1, $$2}'

init-git-hooks: ## Initialize git hooks
	@echo "#====================================================================================================================="
	@echo "# ==> Initializing git hooks..."
	@echo "#====================================================================================================================="
	mkdir -p "$(GIT_HOOKS_DIR)"
	cp scripts/pre-push "$(GIT_HOOKS_DIR)/pre-push"
	chmod +x "$(GIT_HOOKS_DIR)/pre-push"
	@echo "Git hooks initialized successfully"

init-pre-commit-hooks: ## Initialize pre-commit hooks
	@echo "#====================================================================================================================="
	@echo "# ==> Initializing pre-commit hooks..."
	@echo "#====================================================================================================================="
	@mkdir -p "$(GIT_HOOKS_DIR)"
	printf '%s\n%s\n' '#!/bin/sh' 'make -C auth-callout pre-commit-run' > "$(GIT_HOOKS_DIR)/pre-commit"
	chmod +x "$(GIT_HOOKS_DIR)/pre-commit"
	pre-commit install --hook-type commit-msg
	@echo "Pre-commit hooks initialized successfully"

init-project: install-tools ## Initialize project
	cp api/env.example api/.env
	@echo "#====================================================================================================================="
	@echo "# ==> Using namespace auth-callout for devspace..."
	@echo "#====================================================================================================================="
	devspace use namespace auth-callout
	@echo "#====================================================================================================================="
	@echo "# ==> Project initialized. Type 'make run' to build and run the service"
	@echo "#====================================================================================================================="

install-tools: init-pre-commit-hooks ## Install development tools
	@echo "#====================================================================================================================="
	@echo "# ==> Installing Go development tools..."
	@echo "#====================================================================================================================="
	@bash -c 'mkdir -p $$(go env GOPATH)/bin'
	@bash -c 'GOBIN=$$(go env GOPATH)/bin $(GOCMD) install honnef.co/go/tools/cmd/staticcheck@latest'
	@bash -c 'GOBIN=$$(go env GOPATH)/bin $(GOCMD) install golang.org/x/tools/cmd/godoc@latest'
	@bash -c 'GOBIN=$$(go env GOPATH)/bin $(GOCMD) install golang.org/x/tools/cmd/goimports@latest'
	@bash -c 'GOBIN=$$(go env GOPATH)/bin $(GOCMD) install golang.org/x/vuln/cmd/govulncheck@latest'
	@bash -c 'GOBIN=$$(go env GOPATH)/bin $(GOCMD) install github.com/golangci/golangci-lint/cmd/golangci-lint@latest'
	@bash -c 'GOBIN=$$(go env GOPATH)/bin $(GOCMD) install github.com/go-delve/delve/cmd/dlv@latest'
	@bash -c 'GOBIN=$$(go env GOPATH)/bin $(GOCMD) install golang.org/x/tools/gopls@latest'
	@bash -c 'GOBIN=$$(go env GOPATH)/bin $(GOCMD) install sigs.k8s.io/kind@latest'
	@bash -c 'GOBIN=$$(go env GOPATH)/bin $(GOCMD) install github.com/google/go-licenses@$(GO_LICENSES_VERSION)'
	@bash -c 'GOBIN=$$(go env GOPATH)/bin $(GOCMD) install github.com/google/addlicense@$(ADDLICENSE_VERSION)'
	@bash -c 'echo "Tools installed to: $$(go env GOPATH)/bin"'
	@echo "Make sure Go's bin directory is in your PATH"
	@echo "You can add this to your shell profile:"
	@bash -c 'echo "  export PATH=\$$PATH:$$(go env GOPATH)/bin"'


lint: ## Run linter (if available)
	@command -v golangci-lint >/dev/null 2>&1 && golangci-lint run || echo "golangci-lint not installed, skipping..."

pre-commit-run: ## Run pre-commit hooks
	@echo "#====================================================================================================================="
	@echo "# ==> Running pre-commit hooks..."
	@echo "#====================================================================================================================="
	pre-commit run
	@echo "#====================================================================================================================="
	@echo "# ==> Any errors were likely fixed by the pre-commit hooks.  Please 'git add .' the new changes and retry your commit."
	@echo "#====================================================================================================================="

release: clean build lint test test-integration ## Prepare release build
	@echo "Release build complete"

run: build ## Run the service locally
	./$(BINARY_NAME)

run-service: build ## Run the service
	./$(BINARY_NAME)

test: ## Run all tests
	go test $(TEST_FLAGS) ./...

test-helm: ## Run helm chart validations
	bash scripts/test-helm-validation.sh

test-integration:  ## Run integration tests (local service)
	cd tests && go mod tidy && go test $(TEST_FLAGS) ./...

test-integration-devspace:  ## Run integration tests against devspace cluster
	@echo "Running integration tests against devspace cluster..."
	cd tests && go mod tidy && AUTH_CALLOUT_SERVICE_URL=http://auth-callout.127-0-0-1.nip.io:8080 go test $(TEST_FLAGS) ./...

add-license-headers: ## Add SPDX license headers to source files
	addlicense -l apache -c "$(COPYRIGHT_HOLDER)" -s=only -y $(COPYRIGHT_YEAR) -ignore "vendor/**" -ignore "tests/**" -v src/

check-license-headers: ## Verify SPDX license headers are present
	addlicense -l apache -c "$(COPYRIGHT_HOLDER)" -s=only -y $(COPYRIGHT_YEAR) -ignore "vendor/**" -ignore "tests/**" -check src/

$(GO_LICENSES):
	@mkdir -p "$(TOOLS_BIN)"
	GOBIN="$(TOOLS_BIN)" GOFLAGS= $(GOCMD) install github.com/google/go-licenses@$(GO_LICENSES_VERSION)

third-party-licenses: $(GO_LICENSES) ## Regenerate THIRD_PARTY_LICENSES.csv
	PATH="$(TOOLS_BIN):$$PATH" bash scripts/regenerate-third-party-licenses.sh ../THIRD_PARTY_LICENSES.csv

version: ## Show current version
	@echo "auth-callout version $$(git describe --tags --abbrev=0 2>/dev/null || echo 'no-tag')"

#======================================================================================================================
# Shared defs
#======================================================================================================================

define DELVE_CONTINUE_COMMAND
dlv exec --headless --continue --listen=:5005 --api-version=2 --accept-multiclient
endef

define DELVE_SUSPEND_COMMAND
dlv exec --headless --listen=:5005 --api-version=2 --accept-multiclient
endef

define DELVE_LOG_COMMAND
$(DELVE_CONTINUE_COMMAND) --log --log-output=debugger,gdbwire,lldbout,debuglineerr,rpc,dap
endef

define DELVE_SUSPEND_WITH_LOGS_COMMAND
$(DELVE_SUSPEND_COMMAND) --log --log-output=debugger,gdbwire,lldbout,debuglineerr,rpc,dap
endef

define DELVE_START_SCRIPT
cleanup() { \
	echo "→ Stopping debugger..."; \
	if [ ! -z "$$DELVE_PID" ]; then \
		kill -TERM $$DELVE_PID 2>/dev/null || true; \
		sleep 1; \
		kill -KILL $$DELVE_PID 2>/dev/null || true; \
	fi; \
	exit 0; \
}; \
trap cleanup INT TERM; \
$(DELVE_CONTINUE_COMMAND) \
	tmp/$(BINARY_NAME) \
	-- --config /etc/config/config.yaml & \
DELVE_PID=$$!; \
echo "✓ Delve started (PID: $$DELVE_PID) - waiting for debugger connection"; \
wait $$DELVE_PID;
endef


define DELVE_START_WITH_LOGS_SCRIPT
cleanup() { \
	echo "→ Stopping debugger..."; \
	if [ ! -z "$$DELVE_PID" ]; then \
		kill -TERM $$DELVE_PID 2>/dev/null || true; \
		sleep 1; \
		kill -KILL $$DELVE_PID 2>/dev/null || true; \
	fi; \
	exit 0; \
}; \
trap cleanup INT TERM; \
$(DELVE_LOG_COMMAND) \
	tmp/$(BINARY_NAME) \
	-- --config /etc/config/config.yaml & \
DELVE_PID=$$!; \
echo "✓ Delve started (PID: $$DELVE_PID) - waiting for debugger connection"; \
wait $$DELVE_PID;
endef

define DELVE_SUSPEND_SCRIPT
cleanup() { \
	echo "→ Stopping debugger..."; \
	if [ ! -z "$$DELVE_PID" ]; then \
		kill -TERM $$DELVE_PID 2>/dev/null || true; \
		sleep 1; \
		kill -KILL $$DELVE_PID 2>/dev/null || true; \
	fi; \
	exit 0; \
}; \
trap cleanup INT TERM; \
$(DELVE_SUSPEND_COMMAND) \
	tmp/$(BINARY_NAME) \
	-- --config /etc/config/config.yaml & \
DELVE_PID=$$!; \
echo "✓ Delve suspended (PID: $$DELVE_PID) - waiting for debugger connection"; \
wait $$DELVE_PID;
endef

define DELVE_SUSPEND_WITH_LOGS_SCRIPT
cleanup() { \
	echo "→ Stopping debugger..."; \
	if [ ! -z "$$DELVE_PID" ]; then \
		kill -TERM $$DELVE_PID 2>/dev/null || true; \
		sleep 1; \
		kill -KILL $$DELVE_PID 2>/dev/null || true; \
	fi; \
	exit 0; \
}; \
trap cleanup INT TERM; \
$(DELVE_SUSPEND_WITH_LOGS_COMMAND) \
	tmp/$(BINARY_NAME) \
	-- --config /etc/config/config.yaml & \
DELVE_PID=$$!; \
echo "✓ Delve suspended (PID: $$DELVE_PID) - waiting for debugger connection"; \
wait $$DELVE_PID;
endef
