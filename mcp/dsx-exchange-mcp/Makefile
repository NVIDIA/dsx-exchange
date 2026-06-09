# Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
# SPDX-License-Identifier: Apache-2.0

BINARY := dsx-exchange-mcp
LOAD_BINARY := dsx-exchange-mcp-load
PKG    := github.com/NVIDIA/dsx-exchange/mcp/dsx-exchange-mcp
SCHEMA_SRC  ?= ../../schemas
GOFLAGS    ?= -mod=vendor

.PHONY: build build-load run test tidy vendor lint sync-specs verify-specs image load-image clean

build:
	go build $(GOFLAGS) -o bin/$(BINARY) ./cmd/$(BINARY)

build-load:
	go build $(GOFLAGS) -o bin/$(LOAD_BINARY) ./cmd/$(LOAD_BINARY)

run: sync-specs build
	./bin/$(BINARY)

test:
	go test $(GOFLAGS) ./...

tidy:
	go mod tidy

vendor: tidy
	go mod vendor

lint:
	go vet $(GOFLAGS) $$(go list $(GOFLAGS) ./...)

sync-specs:
	@test -d $(SCHEMA_SRC) || (echo "schema tree not at $(SCHEMA_SRC); set SCHEMA_SRC"; exit 1)
	rm -rf schemas/asyncapi schemas/cloud-events-example.yaml schemas/README.md
	mkdir -p schemas
	cp -R $(SCHEMA_SRC)/. schemas/

verify-specs: sync-specs
	git diff --exit-code -- schemas

image:
	docker build -t $(BINARY):dev .

load-image:
	docker build -f Dockerfile.load -t $(LOAD_BINARY):dev .

clean:
	rm -rf bin
