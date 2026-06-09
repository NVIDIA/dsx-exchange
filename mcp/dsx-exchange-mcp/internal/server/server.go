// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/NVIDIA/dsx-exchange/mcp/dsx-exchange-mcp/internal/auth"
	"github.com/NVIDIA/dsx-exchange/mcp/dsx-exchange-mcp/internal/metrics"
	"github.com/NVIDIA/dsx-exchange/mcp/dsx-exchange-mcp/internal/mqttbus"
)

type Config struct {
	MQTT                        mqttbus.Config
	Metrics                     *metrics.Recorder
	DefaultMaxMessages          int
	MaxMessages                 int
	DefaultDurationS            int
	MaxDurationS                int
	MQTTCollectMaxConcurrent    int
	MQTTWatchStartMaxConcurrent int
	WatchDefaultTTLS            int
	WatchMaxTTLS                int
	WatchDefaultBufferMessages  int
	WatchMaxBufferMessages      int
	WatchDefaultBufferBytes     int
	WatchMaxBufferBytes         int
	WatchMaxPerSession          int
	WatchMaxPerPod              int
	FindTopicsDefaultLimit      int
	FindTopicsMaxLimit          int

	collectAdmission    *admissionLimiter
	watchStartAdmission *admissionLimiter
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
	cfg.MQTT.Metrics = cfg.Metrics
	cfg.collectAdmission = newAdmissionLimiter(cfg.MQTTCollectMaxConcurrent)
	cfg.watchStartAdmission = newAdmissionLimiter(cfg.MQTTWatchStartMaxConcurrent)
	srv.AddReceivingMiddleware(callerContextMiddleware(cfg.Metrics))
	watches := newWatchManager(cfg)
	registerTools(srv, cfg, watches)
	registerResources(srv)
	return srv
}

func callerContextMiddleware(rec *metrics.Recorder) func(mcp.MethodHandler) mcp.MethodHandler {
	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			sessionID := ""
			if session := req.GetSession(); session != nil {
				sessionID = strings.TrimSpace(session.ID())
			}
			if extra := req.GetExtra(); extra != nil {
				caller := auth.CallerFromHeaders(extra.Header)
				if caller.SessionID == "" {
					caller.SessionID = sessionID
				}
				ctx = auth.WithCaller(ctx, caller)
			} else if sessionID != "" {
				ctx = auth.WithSessionID(ctx, sessionID)
			}
			if rec != nil {
				rec.ObserveSession(sessionID)
			}
			return next(ctx, method, req)
		}
	}
}
