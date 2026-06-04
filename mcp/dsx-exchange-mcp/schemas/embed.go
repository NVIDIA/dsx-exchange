// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

// Package schemas embeds the DSX Exchange public schema tree.
package schemas

import "embed"

// FS contains the schema files copied from the monorepo root schemas directory.
//
//go:embed README.md cloud-events-example.yaml asyncapi/*/*.yaml
var FS embed.FS
