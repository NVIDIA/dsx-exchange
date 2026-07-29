// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package hub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/nats-io/nats.go"

	"github.com/NVIDIA/dsx-exchange/dsx-agentgateway-bridge/internal/config"
	"github.com/NVIDIA/dsx-exchange/dsx-agentgateway-bridge/internal/health"
	"github.com/NVIDIA/dsx-exchange/dsx-agentgateway-bridge/internal/shardbus"
	"github.com/NVIDIA/dsx-exchange/dsx-agentgateway-bridge/internal/telemetry"
)

const (
	BridgeListShardsToolName = "dsx_bridge_list_shards"
)

type hub struct {
	bus              shardbus.Requester
	timeout          time.Duration
	writeTimeout     time.Duration
	discoveryTimeout time.Duration
	discoveryRefresh time.Duration
	subjectPrefix    string
	shardCache       *shardDiscoveryCache
}

type shardDiscoveryCache struct {
	mu     sync.RWMutex
	ready  bool
	shards []string
	err    error
}

type startupSubscriber interface {
	Subscribe(string, nats.MsgHandler) (*nats.Subscription, error)
	FlushTimeout(time.Duration) error
}

func Run(ctx context.Context, cfg config.Hub) error {
	cfg, err := config.NormalizeHub(cfg)
	if err != nil {
		return err
	}

	shutdownTracing, err := telemetry.Init(ctx, "dsx-agentgateway-bridge-hub")
	if err != nil {
		return err
	}
	defer telemetry.Shutdown(shutdownTracing)

	if cfg.Bus == nil {
		nc, err := nats.Connect(cfg.NATSURL, cfg.NATSOptions...)
		if err != nil {
			return fmt.Errorf("connect to NATS %s: %w", cfg.NATSURL, err)
		}
		defer func() {
			drainCtx, cancel := context.WithTimeout(context.Background(), cfg.Timeout)
			defer cancel()
			if err := shardbus.DrainConnection(drainCtx, nc); err != nil {
				slog.Error("drain NATS connection", "error", err)
			}
		}()
		cfg.Bus = shardbus.Conn{Conn: nc}
		if cfg.Ready == nil {
			cfg.Ready = nc.IsConnected
		}
	}
	if cfg.Ready == nil {
		cfg.Ready = func() bool { return true }
	}

	h := newHub(cfg)
	refreshTrigger := make(chan struct{}, 1)
	if startupNotifications, ok := cfg.Bus.(startupSubscriber); ok {
		subject := shardbus.StartupSubject(cfg.SubjectPrefix)
		startupSub, err := startupNotifications.Subscribe(subject, func(msg *nats.Msg) {
			handleLeafStartupNotification(msg, refreshTrigger)
		})
		if err != nil {
			return fmt.Errorf("subscribe %s: %w", subject, err)
		}
		defer drainSubscription(startupSub)
		if err := startupNotifications.FlushTimeout(shardbus.SubscriptionFlushWait); err != nil {
			return fmt.Errorf("confirm NATS subscriptions: %w", err)
		}
	}
	// A failed initial discovery keeps readiness false, but the process stays
	// alive so the normal refresh loop can recover without a Pod restart.
	if err := h.refreshShardCache(ctx); err != nil {
		slog.Error("initial bridge shard discovery failed", "error", err)
	}
	go h.runShardDiscoveryRefresh(ctx, refreshTrigger)

	ready := cfg.Ready
	mux := http.NewServeMux()
	health.Register(mux, func() bool {
		return ready() && h.shardCache.trafficReady()
	})
	mux.Handle("/", withRequestLog(slog.Default(), http.HandlerFunc(h.handleMCP)))
	return runHTTP(ctx, "bridge-hub", cfg.ListenAddr, cfg.HTTPWriteTimeout, cfg.HTTPRequestTimeout, mux)
}

func drainSubscription(sub *nats.Subscription) {
	if sub == nil {
		return
	}
	if err := sub.Drain(); err != nil && !errors.Is(err, nats.ErrConnectionClosed) {
		slog.Error("drain NATS subscription", "subject", sub.Subject, "error", err)
	}
}

func NewHandler(cfg config.Hub) (http.Handler, error) {
	cfg, err := config.NormalizeHub(cfg)
	if err != nil {
		return nil, err
	}
	if cfg.Bus == nil {
		return nil, fmt.Errorf("hub Config.Bus is required")
	}
	h := newHub(cfg)
	return withRequestLog(slog.Default(), http.HandlerFunc(h.handleMCP)), nil
}

func newHub(cfg config.Hub) hub {
	return hub{
		bus:              cfg.Bus,
		timeout:          cfg.Timeout,
		writeTimeout:     cfg.HTTPWriteTimeout,
		discoveryTimeout: cfg.DiscoveryTimeout,
		discoveryRefresh: cfg.DiscoveryRefresh,
		subjectPrefix:    cfg.SubjectPrefix,
		shardCache:       &shardDiscoveryCache{},
	}
}

func (h hub) writeResponse(w http.ResponseWriter, status int, contentType string, body []byte) {
	if contentType != "" {
		w.Header().Set("Content-Type", contentType)
	}
	if _, err := writeAndFlush(w, h.writeTimeout, status, body); err != nil {
		slog.Error("write HTTP response", "error", err)
	}
}

func (h hub) writeRPC(w http.ResponseWriter, resp any) {
	body, err := json.Marshal(resp)
	if err != nil {
		slog.Error("encode RPC response", "error", err)
		return
	}
	h.writeResponse(w, http.StatusOK, "application/json", body)
}

func (h hub) writeRPCResult(w http.ResponseWriter, id mcp.RequestId, result any) {
	h.writeRPC(w, mcp.NewJSONRPCResultResponse(id, result))
}

func (h hub) writeRPCError(w http.ResponseWriter, id mcp.RequestId, code int, message string) {
	h.writeRPC(w, mcp.NewJSONRPCError(id, code, message, nil))
}

func (h hub) writeRPCParseError(w http.ResponseWriter) {
	h.writeRPCError(w, mcp.NewRequestId(nil), mcp.PARSE_ERROR, "parse error")
}

func newLeafRequest(ctx context.Context, sourceHeaders http.Header, subject string, body []byte) *nats.Msg {
	headers := sourceHeaders.Clone()
	// MCP sessions belong to one HTTP peer. The bridge is stateless across NATS,
	// so leaves must not inherit the caller's session id.
	headers.Del("Mcp-Session-Id")
	telemetry.InjectTraceContext(ctx, headers)
	return &nats.Msg{
		Subject: subject,
		Header:  nats.Header(headers),
		Data:    body,
	}
}
