# Makefile for nv-config Go Library

# Go parameters
GOCMD=go
GOBUILD=$(GOCMD) build
GOCLEAN=$(GOCMD) clean
GOTEST=$(GOCMD) test
GOGET=$(GOCMD) get
GOMOD=$(GOCMD) mod
GOFMT=gofmt
GOVET=$(GOCMD) vet

# Binary name
BASIC_NAME=basic
BASIC_PATH=examples/$(BASIC_NAME)
LIBRARY_NAME=library
LIBRARY_PATH=examples/$(LIBRARY_NAME)
SERVICE_NAME=service
SERVICE_PATH=examples/$(SERVICE_NAME)
EXAMPLES_PATH=examples

# Build flags
BUILD_FLAGS=-v
TEST_FLAGS=-race

# Coverage
COVERAGE_OUT=coverage.out
COVERAGE_HTML=coverage.html

.PHONY: all build clean clean-vendor clean-all test test-verbose test-race test-integration coverage lint fmt vet help deps \
	build-example run-example coverage-open fmt-check install-tools check-tools security doc mod-verify mod-tidy \
	update-deps vendor ci watch-test init-hooks setup-pre-commit pre-commit-run pre-commit-update version tag

all: clean fmt vet test build security vendor pre-commit-run run-example

help: ## Display this help screen
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-20s\033[0m %s\n", $$1, $$2}'

deps: ## Download dependencies
	$(GOMOD) download
	$(GOMOD) tidy

build: ## Build the library
	$(GOBUILD) $(BUILD_FLAGS) ./...

build-example: ## Build the example binaries
	cd $(EXAMPLES_PATH)/$(BASIC_NAME) && $(GOBUILD) $(BUILD_FLAGS) -o $(BASIC_NAME) main.go
	cd $(EXAMPLES_PATH)/$(SERVICE_NAME) && $(GOBUILD) $(BUILD_FLAGS) -o $(SERVICE_NAME) main.go
	# Build library examples (verify they compile)
	cd $(EXAMPLES_PATH)/$(LIBRARY_NAME)/database && $(GOBUILD) $(BUILD_FLAGS) ./...
	cd $(EXAMPLES_PATH)/$(LIBRARY_NAME)/httpclient && $(GOBUILD) $(BUILD_FLAGS) ./...
	cd $(EXAMPLES_PATH)/$(LIBRARY_NAME)/redis && $(GOBUILD) $(BUILD_FLAGS) ./...
	cd $(EXAMPLES_PATH)/$(LIBRARY_NAME)/tracing-otel && $(GOBUILD) $(BUILD_FLAGS) ./...
	cd $(EXAMPLES_PATH)/$(LIBRARY_NAME)/tracing-lightstep && $(GOBUILD) $(BUILD_FLAGS) ./...

run-example: build-example ## Build and run the example
# 	cd examples/basic && go run -race main.go

clean: ## Clean build artifacts
	$(GOCLEAN)
	rm -f $(BINARY_PATH)
	rm -f $(COVERAGE_OUT) $(COVERAGE_HTML)
	# Remove example binaries
	rm -f $(EXAMPLES_PATH)/$(BASIC_NAME)/$(BASIC_NAME)
	rm -f $(EXAMPLES_PATH)/$(SERVICE_NAME)/$(SERVICE_NAME)

clean-vendor: ## Remove vendor directory
	rm -rf vendor

clean-all: clean clean-vendor ## Clean everything including vendor directory

test: ## Run unit tests
	$(GOTEST) $(TEST_FLAGS) ./...

test-verbose: ## Run tests with verbose output
	$(GOTEST) $(TEST_FLAGS) -v ./...

test-integration: ## Run integration tests
	$(GOTEST) $(TEST_FLAGS) ./test/...

test-short: ## Run tests in short mode
	$(GOTEST) $(TEST_FLAGS) -short ./...

test-bench: ## Run benchmarks
	$(GOTEST) -bench=. -benchmem ./...

coverage: ## Generate test coverage report
	$(GOTEST) -coverprofile=$(COVERAGE_OUT) $$(go list ./... | grep -v '/examples')
	$(GOCMD) tool cover -html=$(COVERAGE_OUT) -o $(COVERAGE_HTML)
	$(GOCMD) tool cover -func=$(COVERAGE_OUT)

coverage-open: coverage ## Generate coverage report and open in browser
	@bash -c 'if command -v open >/dev/null 2>&1; then \
		open $(COVERAGE_HTML); \
	elif command -v xdg-open >/dev/null 2>&1; then \
		xdg-open $(COVERAGE_HTML); \
	else \
		echo "Please open $(COVERAGE_HTML) manually"; \
	fi'

fmt: ## Format Go code
	$(GOFMT) -w .

fmt-check: ## Check if code is formatted
	@if [ -n "$$($(GOFMT) -l .)" ]; then \
		echo "❌ Code is not formatted. Files that need formatting:"; \
		$(GOFMT) -l .; \
		echo "💡 Run 'make fmt' to fix formatting issues."; \
		exit 1; \
	else \
		echo "✅ Code is properly formatted"; \
	fi

vet: ## Run go vet
	$(GOVET) ./...

lint: ## Run staticcheck linter
	@bash -c 'if command -v staticcheck >/dev/null 2>&1; then \
		staticcheck ./...; \
	elif [ -f "$$(go env GOPATH)/bin/staticcheck" ]; then \
		$$(go env GOPATH)/bin/staticcheck ./...; \
	else \
		echo "Installing staticcheck..."; \
		GOBIN=$$(go env GOPATH)/bin $(GOCMD) install honnef.co/go/tools/cmd/staticcheck@latest; \
		$$(go env GOPATH)/bin/staticcheck ./...; \
	fi'

install-tools: ## Install development tools
	@echo "Installing Go development tools..."
	@bash -c 'mkdir -p $$(go env GOPATH)/bin'
	@bash -c 'GOBIN=$$(go env GOPATH)/bin $(GOCMD) install honnef.co/go/tools/cmd/staticcheck@latest'
	@bash -c 'GOBIN=$$(go env GOPATH)/bin $(GOCMD) install golang.org/x/tools/cmd/godoc@latest'
	@bash -c 'GOBIN=$$(go env GOPATH)/bin $(GOCMD) install golang.org/x/tools/cmd/goimports@latest'
	@bash -c 'GOBIN=$$(go env GOPATH)/bin $(GOCMD) install golang.org/x/vuln/cmd/govulncheck@latest'
	@bash -c 'GOBIN=$$(go env GOPATH)/bin $(GOCMD) install github.com/golangci/golangci-lint/cmd/golangci-lint@latest'
	@bash -c 'echo "Tools installed to: $$(go env GOPATH)/bin"'
	@echo "Make sure Go's bin directory is in your PATH"
	@echo "You can add this to your shell profile:"
	@bash -c 'echo "  export PATH=\$$PATH:$$(go env GOPATH)/bin"'

security: ## Run security checks
	@bash -c 'if command -v govulncheck >/dev/null 2>&1; then \
		govulncheck ./...; \
	elif [ -f "$$(go env GOPATH)/bin/govulncheck" ]; then \
		$$(go env GOPATH)/bin/govulncheck ./...; \
	else \
		echo "Installing govulncheck..."; \
		GOBIN=$$(go env GOPATH)/bin $(GOCMD) install golang.org/x/vuln/cmd/govulncheck@latest; \
		$$(go env GOPATH)/bin/govulncheck ./...; \
	fi'

doc: ## Generate and serve documentation locally
	@echo "Starting documentation server at http://localhost:6060"
	@echo "Press Ctrl+C to stop"
	@bash -c 'if command -v godoc >/dev/null 2>&1; then \
		godoc -http=:6060; \
	elif [ -f "$$(go env GOPATH)/bin/godoc" ]; then \
		$$(go env GOPATH)/bin/godoc -http=:6060; \
	else \
		echo "Installing godoc..."; \
		GOBIN=$$(go env GOPATH)/bin $(GOCMD) install golang.org/x/tools/cmd/godoc@latest; \
		$$(go env GOPATH)/bin/godoc -http=:6060; \
	fi'

mod-verify: ## Verify dependencies
	$(GOMOD) verify

mod-tidy: ## Tidy dependencies
	$(GOMOD) tidy

update-deps: ## Update dependencies
	$(GOGET) -u ./...
	$(GOMOD) tidy

vendor: ## Create vendor directory with dependencies
	$(GOMOD) vendor

ci: clean security fmt fmt-check vet test coverage ## Run CI pipeline locally

# Release targets
version: ## Show current version
	@echo "Current version: $$(git describe --tags --abbrev=0 2>/dev/null || echo 'No tags found')"

tag: ## Create a new tag (usage: make tag VERSION=v1.0.0)
ifndef VERSION
	@echo "Usage: make tag VERSION=v1.0.0"
	@exit 1
endif
	git tag -a $(VERSION) -m "Release $(VERSION)"
	git push origin $(VERSION)

# Development helpers
watch-test: ## Watch for changes and run tests (requires inotify-tools)
	@bash -c 'if command -v inotifywait >/dev/null 2>&1; then \
		while true; do \
			inotifywait -r -e modify,create,delete --include="\.go$$" .; \
			clear; \
			echo "Running tests..."; \
			$(GOTEST) ./...; \
		done; \
	else \
		echo "Error: inotifywait not found. Install inotify-tools package."; \
		exit 1; \
	fi'

check-tools: ## Check if development tools are installed and accessible
	@echo "Checking development tools..."
	@bash -c 'echo "Go version: $$(go version)"'
	@bash -c 'echo "GOPATH: $$(go env GOPATH)"'
	@bash -c 'echo "GOBIN: $$(go env GOPATH)/bin"'
	@echo ""
	@echo "Tool availability:"
	@bash -c 'printf "  staticcheck: "; if command -v staticcheck >/dev/null 2>&1; then echo "✅ available"; elif [ -f "$$(go env GOPATH)/bin/staticcheck" ]; then echo "⚠️  installed but not in PATH"; else echo "❌ not installed"; fi'
	@bash -c 'printf "  goimports: "; if command -v goimports >/dev/null 2>&1; then echo "✅ available"; elif [ -f "$$(go env GOPATH)/bin/goimports" ]; then echo "⚠️  installed but not in PATH"; else echo "❌ not installed"; fi'
	@bash -c 'printf "  golangci-lint: "; if command -v golangci-lint >/dev/null 2>&1; then echo "✅ available"; elif [ -f "$$(go env GOPATH)/bin/golangci-lint" ]; then echo "⚠️  installed but not in PATH"; else echo "❌ not installed"; fi'
	@bash -c 'printf "  govulncheck: "; if command -v govulncheck >/dev/null 2>&1; then echo "✅ available"; elif [ -f "$$(go env GOPATH)/bin/govulncheck" ]; then echo "⚠️  installed but not in PATH"; else echo "❌ not installed"; fi'
	@bash -c 'printf "  godoc: "; if command -v godoc >/dev/null 2>&1; then echo "✅ available"; elif [ -f "$$(go env GOPATH)/bin/godoc" ]; then echo "⚠️  installed but not in PATH"; else echo "❌ not installed"; fi'
	@bash -c 'printf "  pre-commit: "; if command -v pre-commit >/dev/null 2>&1; then echo "✅ available"; else echo "❌ not installed"; fi'
	@echo ""
	@echo "If tools show '⚠️  installed but not in PATH', add this to your shell profile:"
	@bash -c 'echo "  export PATH=\$$PATH:$$(go env GOPATH)/bin"'

init-hooks: setup-pre-commit install-tools ## Initialize git hooks
	@mkdir -p .git/hooks
	echo "#!/bin/sh\nmake pre-commit-run" > .git/hooks/pre-commit
	chmod +x .git/hooks/pre-commit
	@echo "Git hooks initialized successfully"

setup-pre-commit: ## Install and setup pre-commit
	@bash -c 'if command -v pip >/dev/null 2>&1; then \
		pip install pre-commit; \
	elif command -v pip3 >/dev/null 2>&1; then \
		pip3 install pre-commit; \
	else \
		echo "Error: pip not found. Please install Python and pip first."; \
		exit 1; \
	fi'
	@bash -c 'pre-commit install'

pre-commit-run: ## Run pre-commit on all files
	# Make sure the file permissions are sane for YAMLFileProvider
	chmod 0644 examples/basic/config.yaml
	chmod 0644 examples/service/config.yaml
	@bash -c 'if command -v pre-commit >/dev/null 2>&1; then \
		pre-commit run --all-files; \
	else \
		echo "Error: pre-commit not installed. Run '\''make setup-pre-commit'\'' first."; \
		exit 1; \
	fi'

pre-commit-update: ## Update pre-commit hooks
	@bash -c 'if command -v pre-commit >/dev/null 2>&1; then \
		pre-commit autoupdate; \
	else \
		echo "Error: pre-commit not installed. Run '\''make setup-pre-commit'\'' first."; \
		exit 1; \
	fi'
