# Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
# SPDX-License-Identifier: Apache-2.0

# NATS Event Bus Deploy Makefile

.PHONY: add-license-headers check-license-headers help

ADDLICENSE_VERSION=v1.2.0
COPYRIGHT_HOLDER=NVIDIA CORPORATION & AFFILIATES. All rights reserved.
COPYRIGHT_YEAR=2026

add-license-headers: ## Add SPDX license headers to source files
	addlicense -l apache -c "$(COPYRIGHT_HOLDER)" -s=only -y $(COPYRIGHT_YEAR) -v .

check-license-headers: ## Verify SPDX license headers are present
	addlicense -l apache -c "$(COPYRIGHT_HOLDER)" -s=only -y $(COPYRIGHT_YEAR) -check .

help: ## Show this help message
	@echo "Available commands:"
	@grep -E '^[a-zA-Z0-9_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-30s\033[0m %s\n", $$1, $$2}'
