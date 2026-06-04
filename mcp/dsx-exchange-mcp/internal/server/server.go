// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/NVIDIA/dsx-exchange/mcp/dsx-exchange-mcp/internal/metrics"
	"github.com/NVIDIA/dsx-exchange/mcp/dsx-exchange-mcp/internal/mqttbus"
)

type Config struct {
	MQTT                       mqttbus.Config
	Metrics                    *metrics.Recorder
	DefaultMaxMessages         int
	MaxMessages                int
	DefaultDurationS           int
	MaxDurationS               int
	WatchDefaultTTLS           int
	WatchMaxTTLS               int
	WatchDefaultBufferMessages int
	WatchMaxBufferMessages     int
	WatchDefaultBufferBytes    int
	WatchMaxBufferBytes        int
	WatchMaxPerSession         int
	WatchMaxPerPod             int
	FindTopicsDefaultLimit     int
	FindTopicsMaxLimit         int
}

// Build constructs the singleton MCP server. The same *mcp.Server is returned
// from the StreamableHTTPHandler factory for every request; per-request state
// (caller bearer) flows through ctx via the auth middleware.
func Build(cfg Config) *mcp.Server {
	srv := mcp.NewServer(
		&mcp.Implementation{
			Name:    "dsx-exchange-mcp",
			Version: "0.1.0",
		},
		nil,
	)

	normalizeConfig(&cfg)
	watches := newWatchManager(cfg)
	registerTools(srv, cfg, watches)
	registerResources(srv)
	return srv
}
