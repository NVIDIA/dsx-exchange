# Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
# SPDX-License-Identifier: Apache-2.0

.PHONY: add-license-headers check-license-headers help

add-license-headers: ## Add SPDX license headers to source files
	bash ../scripts/license.sh fix

check-license-headers: ## Verify SPDX license headers are present
	bash ../scripts/license.sh check

help: ## Show this help message
	@echo "Available commands:"
	@grep -E '^[a-zA-Z0-9_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-30s\033[0m %s\n", $$1, $$2}'
