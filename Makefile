# Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
# SPDX-License-Identifier: Apache-2.0

.PHONY: add-license-headers check-license-headers help

COPYRIGHT_HOLDER := NVIDIA CORPORATION & AFFILIATES. All rights reserved.
COPYRIGHT_YEAR := 2026
LICENSE_TARGETS := local schema
LICENSE_IGNORES := \
	-ignore "**/*.png" \
	-ignore "**/go.sum" \
	-ignore "**/tests/performance/reports/**" \
	-ignore "**/vendor/**"

add-license-headers: ## Add SPDX license headers to local and schema files
	addlicense -l apache -c "$(COPYRIGHT_HOLDER)" -s=only -y "$(COPYRIGHT_YEAR)" $(LICENSE_IGNORES) -v $(LICENSE_TARGETS)

check-license-headers: ## Verify SPDX license headers on local and schema files
	addlicense -l apache -c "$(COPYRIGHT_HOLDER)" -s=only -y "$(COPYRIGHT_YEAR)" $(LICENSE_IGNORES) -check $(LICENSE_TARGETS)

help: ## Show this help message
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "  %-28s %s\n", $$1, $$2}'
