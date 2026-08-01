# Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
# SPDX-License-Identifier: Apache-2.0

SHELL := /bin/bash
.SHELLFLAGS := -euo pipefail -c

ROOT_DIR := $(abspath $(dir $(lastword $(MAKEFILE_LIST)))/..)
MISE ?= mise
MISE_EXEC := MISE_LOCKED=1 "$(MISE)" exec --cd "$(ROOT_DIR)" --
SKAFFOLD ?= skaffold
SKAFFOLD_FLAGS ?= --cleanup=false --digest-source=tag --iterative-status-check=true --label=skaffold.dev/run-id=local-e2e
override SKAFFOLD_FLAGS += --kube-context=kind-dsx-exchange
SKAFFOLD_TAG ?=
override SKAFFOLD_FLAGS += $(if $(SKAFFOLD_TAG),--tag=$(SKAFFOLD_TAG))
SKAFFOLD_BUILD_MIRROR_REGISTRY ?= localhost:5001

.PHONY: clean install-e2e-prereqs prepare-local-dependencies setup-cluster skaffold-deploy skaffold-dev skaffold-run

install-e2e-prereqs:
	MISE_LOCKED=1 "$(MISE)" --cd "$(ROOT_DIR)" install --locked

prepare-local-dependencies:
	$(MISE_EXEC) bash local/scripts/prepare-dependencies.sh

setup-cluster:
	$(MISE_EXEC) bash local/scripts/setup-cluster.sh

skaffold-run: install-e2e-prereqs prepare-local-dependencies setup-cluster
	$(MAKE) skaffold-deploy

skaffold-deploy:
	$(MISE_EXEC) env SKAFFOLD_BUILD_MIRROR_REGISTRY="$(SKAFFOLD_BUILD_MIRROR_REGISTRY)" "$(SKAFFOLD)" run -f skaffold.yaml --module local-stack $(SKAFFOLD_FLAGS)

skaffold-dev: install-e2e-prereqs prepare-local-dependencies setup-cluster
	$(MISE_EXEC) env SKAFFOLD_BUILD_MIRROR_REGISTRY="$(SKAFFOLD_BUILD_MIRROR_REGISTRY)" "$(SKAFFOLD)" dev -f skaffold.yaml --module local-stack $(SKAFFOLD_FLAGS)

clean:
	$(MISE_EXEC) bash local/scripts/cleanup.sh
