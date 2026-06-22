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
	"github.com/NVIDIA/dsx-exchange/mcp/dsx-exchange-mcp/internal/mqttbus"
	"github.com/NVIDIA/dsx-exchange/mcp/dsx-exchange-mcp/internal/server"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	addr := envOr("MCP_ADDR", ":8080")
	natsURL := envOr("NATS_URL", "tcp://nats:1883")

	cfg := server.Config{
		MQTT: mqttbus.Config{
			BrokerURL: natsURL,
			Username:  envOr("MQTT_USERNAME", mqttbus.DefaultUsername),
			AuthMode:  mqttbus.AuthMode(envOr("MCP_MQTT_AUTH_MODE", string(mqttbus.DefaultAuthMode))),
			TLS: mqttbus.TLSConfig{
				CAFile:             os.Getenv("MQTT_TLS_CA_FILE"),
				ServerName:         os.Getenv("MQTT_TLS_SERVER_NAME"),
				InsecureSkipVerify: envBool("MQTT_TLS_INSECURE_SKIP_VERIFY", false),
			},
			ConnectTimeout:   time.Duration(envInt("MQTT_CONNECT_TIMEOUT_S", 5)) * time.Second,
			SubscribeTimeout: time.Duration(envInt("MQTT_SUBSCRIBE_TIMEOUT_S", 5)) * time.Second,
			MaxResultBytes:   envInt("MQTT_MAX_RESULT_BYTES", 1048576),
		},
		DefaultMaxMessages:       envInt("MCP_DEFAULT_MAX_MESSAGES", 100),
		MaxMessages:              envInt("MCP_MAX_MESSAGES", 1000),
		DefaultDurationS:         envInt("MCP_DEFAULT_MAX_DURATION_S", 30),
		MaxDurationS:             envInt("MCP_MAX_DURATION_S", 30),
		MQTTCollectMaxConcurrent: envInt("MCP_MQTT_COLLECT_MAX_CONCURRENT_PER_POD", 100),
		FindTopicsDefaultLimit:   envInt("MCP_FIND_TOPICS_DEFAULT_LIMIT", 20),
		FindTopicsMaxLimit:       envInt("MCP_FIND_TOPICS_MAX_LIMIT", 100),
	}

	if err := cfg.MQTT.Validate(); err != nil {
		logger.Error("invalid MQTT configuration", "err", err)
		os.Exit(2)
	}

	srv := server.Build(cfg)

	handler := mcp.NewStreamableHTTPHandler(
		func(*http.Request) *mcp.Server { return srv },
		&mcp.StreamableHTTPOptions{
			Stateless:    true,
			JSONResponse: true,
		},
	)

	mux := http.NewServeMux()
	mux.Handle("/mcp", auth.Middleware(handler))
	mux.HandleFunc("/healthz/live", healthOK)
	mux.HandleFunc("/healthz/ready", healthOK)

	logger.Info("dsx-exchange-mcp listening",
		"addr", addr,
		"nats", natsURL,
		"mqtt_auth_mode", cfg.MQTT.AuthMode,
		"mqtt_username", cfg.MQTT.Username,
		"mqtt_tls_ca_configured", cfg.MQTT.TLS.CAFile != "",
		"mqtt_tls_server_name", cfg.MQTT.TLS.ServerName,
		"max_messages", cfg.MaxMessages,
		"max_duration_s", cfg.MaxDurationS,
		"mqtt_collect_max_concurrent_per_pod", cfg.MQTTCollectMaxConcurrent,
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
