# Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
# SPDX-License-Identifier: Apache-2.0

BINARY := dsx-exchange-mcp
PKG    := github.com/NVIDIA/dsx-exchange/mcp/dsx-exchange-mcp
SCHEMA_SRC  ?= ../../schemas
GOFLAGS    ?= -mod=vendor
IMAGE_REPOSITORY ?= $(BINARY)
IMAGE_TAG ?= dev
SKAFFOLD   ?= skaffold
KUBECTL    ?= kubectl
KIND_CONTEXT ?= kind-csc
MCP_NAMESPACE ?= mcp-backends
MCP_RELEASE ?= dsx-exchange-mcp
MCP_LOCAL_PORT ?= 18080

.PHONY: build run skaffold-run-kind port-forward-kind test tidy vendor lint sync-specs verify-specs image clean help

help:
	@printf 'DSX Exchange MCP targets:\n'
	@printf '  build              sync specs and build bin/%s\n' "$(BINARY)"
	@printf '  run                sync specs, build, and run the MCP server\n'
	@printf '  test               sync specs and run Go tests with $(GOFLAGS)\n'
	@printf '  lint               sync specs and run Go linters\n'
	@printf '  sync-specs         copy schemas from $(SCHEMA_SRC) into ./schemas\n'
	@printf '  verify-specs       sync specs and verify schema inputs exist\n'
	@printf '  image              build $(IMAGE_REPOSITORY):$(IMAGE_TAG)\n'
	@printf '  skaffold-run-kind  deploy the MCP backend to local Kind\n'
	@printf '  port-forward-kind  expose the Kind service on $(MCP_LOCAL_PORT)\n'
	@printf '  clean              remove local build outputs\n'

build: sync-specs
	mkdir -p bin
	go build $(GOFLAGS) -o bin/$(BINARY) ./cmd/$(BINARY)

run: build
	./bin/$(BINARY)

skaffold-run-kind:
	"$(SKAFFOLD)" run -f skaffold.yaml --cleanup=false

port-forward-kind:
	"$(KUBECTL)" --context "$(KIND_CONTEXT)" -n "$(MCP_NAMESPACE)" port-forward "svc/$(MCP_RELEASE)" "$(MCP_LOCAL_PORT):8080"

test: sync-specs
	go test $(GOFLAGS) ./...

tidy:
	go mod tidy

vendor: tidy
	go mod vendor

lint: sync-specs
	go vet $(GOFLAGS) $$(go list $(GOFLAGS) ./...)
	@command -v golangci-lint >/dev/null 2>&1 && golangci-lint run || echo "golangci-lint not installed, skipping..."

sync-specs:
	@test -d $(SCHEMA_SRC) || (echo "schema tree not at $(SCHEMA_SRC); set SCHEMA_SRC"; exit 1)
	rm -rf schemas/asyncapi schemas/cloud-events-example.yaml schemas/README.md
	mkdir -p schemas
	cp -R $(SCHEMA_SRC)/. schemas/

verify-specs: sync-specs
	@test -f schemas/README.md
	@test -f schemas/cloud-events-example.yaml
	@test -d schemas/asyncapi
	@find schemas/asyncapi -name '*.yaml' -print -quit | grep -q .

image:
	docker build -f Dockerfile -t $(IMAGE_REPOSITORY):$(IMAGE_TAG) ../..

clean:
	rm -rf bin schemas/asyncapi schemas/cloud-events-example.yaml schemas/README.md schemas/.gitignore
