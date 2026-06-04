// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/NVIDIA/dsx-exchange/mcp/dsx-exchange-mcp/internal/auth"
	"github.com/NVIDIA/dsx-exchange/mcp/dsx-exchange-mcp/internal/metrics"
	"github.com/NVIDIA/dsx-exchange/mcp/dsx-exchange-mcp/internal/mqttbus"
	"github.com/NVIDIA/dsx-exchange/mcp/dsx-exchange-mcp/internal/server"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	addr := envOr("MCP_ADDR", ":8080")
	natsURL := envOr("NATS_URL", "tcp://nats:1883")
	recorder := metrics.NewRecorder()

	cfg := server.Config{
		MQTT: mqttbus.Config{
			BrokerURL: natsURL,
			Username:  envOr("MQTT_USERNAME", mqttbus.DefaultUsername),
			TLS: mqttbus.TLSConfig{
				CAFile:             os.Getenv("MQTT_TLS_CA_FILE"),
				ServerName:         os.Getenv("MQTT_TLS_SERVER_NAME"),
				InsecureSkipVerify: envBool("MQTT_TLS_INSECURE_SKIP_VERIFY", false),
			},
			ConnectTimeout:   time.Duration(envInt("MQTT_CONNECT_TIMEOUT_S", 5)) * time.Second,
			SubscribeTimeout: time.Duration(envInt("MQTT_SUBSCRIBE_TIMEOUT_S", 5)) * time.Second,
			MaxResultBytes:   envInt("MQTT_MAX_RESULT_BYTES", 1048576),
		},
		Metrics:                    recorder,
		DefaultMaxMessages:         envInt("MCP_DEFAULT_MAX_MESSAGES", 100),
		MaxMessages:                envInt("MCP_MAX_MESSAGES", 1000),
		DefaultDurationS:           envInt("MCP_DEFAULT_MAX_DURATION_S", 30),
		MaxDurationS:               envInt("MCP_MAX_DURATION_S", 30),
		WatchDefaultTTLS:           envInt("MCP_WATCH_DEFAULT_TTL_S", 300),
		WatchMaxTTLS:               envInt("MCP_WATCH_MAX_TTL_S", 900),
		WatchDefaultBufferMessages: envInt("MCP_WATCH_DEFAULT_BUFFER_MESSAGES", 100),
		WatchMaxBufferMessages:     envInt("MCP_WATCH_MAX_BUFFER_MESSAGES", 1000),
		WatchDefaultBufferBytes:    envInt("MCP_WATCH_DEFAULT_BUFFER_BYTES", 262144),
		WatchMaxBufferBytes:        envInt("MCP_WATCH_MAX_BUFFER_BYTES", 1048576),
		WatchMaxPerSession:         envInt("MCP_WATCH_MAX_PER_SESSION", 10),
		WatchMaxPerPod:             envInt("MCP_WATCH_MAX_PER_POD", 1000),
		FindTopicsDefaultLimit:     envInt("MCP_FIND_TOPICS_DEFAULT_LIMIT", 20),
		FindTopicsMaxLimit:         envInt("MCP_FIND_TOPICS_MAX_LIMIT", 100),
	}

	srv := server.Build(cfg)

	handler := mcp.NewStreamableHTTPHandler(
		func(*http.Request) *mcp.Server { return srv },
		nil,
	)

	mux := http.NewServeMux()
	mux.Handle("/mcp", auth.Middleware(handler))
	mux.HandleFunc("/healthz/live", healthOK)
	mux.HandleFunc("/healthz/ready", healthOK)
	mux.Handle("/metrics", recorder.Handler())

	logger.Info("dsx-exchange-mcp listening",
		"addr", addr,
		"nats", natsURL,
		"mqtt_username", cfg.MQTT.Username,
		"mqtt_tls_ca_configured", cfg.MQTT.TLS.CAFile != "",
		"mqtt_tls_server_name", cfg.MQTT.TLS.ServerName,
		"max_messages", cfg.MaxMessages,
		"max_duration_s", cfg.MaxDurationS,
		"watch_max_ttl_s", cfg.WatchMaxTTLS,
		"watch_max_per_pod", cfg.WatchMaxPerPod,
	)
	if err := http.ListenAndServe(addr, mux); err != nil {
		logger.Error("server exited", "err", err)
		os.Exit(1)
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		n, err := strconv.Atoi(v)
		if err == nil {
			return n
		}
		slog.Warn("invalid integer env var; using fallback", "key", key, "value", v, "fallback", fallback)
	}
	return fallback
}

func envBool(key string, fallback bool) bool {
	if v := os.Getenv(key); v != "" {
		b, err := strconv.ParseBool(v)
		if err == nil {
			return b
		}
		slog.Warn("invalid boolean env var; using fallback", "key", key, "value", v, "fallback", fallback)
	}
	return fallback
}

func healthOK(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusNoContent)
}
