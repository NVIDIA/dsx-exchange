// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package hub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"math/rand"
	"net/http"
	"slices"
	"time"

	mcptransport "github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/nats-io/nats.go"

	"github.com/NVIDIA/dsx-exchange/dsx-agentgateway-bridge/internal/config"
	"github.com/NVIDIA/dsx-exchange/dsx-agentgateway-bridge/internal/shardbus"
)

func (h hub) refreshShardCache(parent context.Context) error {
	shards, err := h.discoverShardIDs(parent)
	h.shardCache.store(shards, err)
	return err
}

func handleLeafStartupNotification(msg *nats.Msg, refresh chan<- struct{}) {
	shardID := msg.Header.Get(shardbus.ShardIDHeader)
	if !config.ValidShardID(shardID) {
		slog.Warn("ignore invalid bridge leaf startup notification", "shard_id", shardID)
		return
	}
	select {
	case refresh <- struct{}{}:
	default:
		// One queued refresh is enough. Startup notifications only hint that the
		// cache should be recomputed, they are not the shard list itself.
	}
}

func (h hub) runShardDiscoveryRefresh(ctx context.Context, refresh <-chan struct{}) {
	// The initial refresh happens before this loop. Jitter only applies to the
	// recurring path so replicas do not rediscover in lockstep.
	timer := time.NewTimer(jitteredInterval(h.discoveryRefresh))
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-refresh:
		case <-timer.C:
		}
		if err := h.refreshShardCache(ctx); err != nil {
			slog.Error("refresh bridge shard discovery cache", "error", err)
		}
		timer.Reset(jitteredInterval(h.discoveryRefresh))
	}
}

func jitteredInterval(base time.Duration) time.Duration {
	if base <= 0 {
		return base
	}
	spread := base / 10
	if spread <= 0 {
		return base
	}
	return base - spread + time.Duration(rand.Int63n(int64(2*spread)+1))
}

func (c *shardDiscoveryCache) snapshot() ([]string, bool, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.shards, c.ready, c.err
}

func (c *shardDiscoveryCache) store(shards []string, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	// ready means a discovery loop completed. Readiness still checks err so a
	// failed loop is recorded without pretending the hub can serve traffic.
	c.ready = true
	c.shards = shards
	c.err = err
}

// trafficReady requires one completed discovery loop with no discovery error.
// Zero shards is valid: a bridge hub can be ready before any leaves exist.
func (c *shardDiscoveryCache) trafficReady() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.ready && c.err == nil
}

func (h hub) discoverShardIDs(parent context.Context) ([]string, error) {
	// Keep parent to distinguish caller cancellation from the local timeout.
	// A local timeout is a successful zero-shard discovery.
	ctx, cancel := context.WithTimeout(parent, h.discoveryTimeout)
	defer cancel()

	req := mcptransport.JSONRPCRequest{
		JSONRPC: mcp.JSONRPC_VERSION,
		ID:      mcp.NewRequestId(int64(1)),
		Method:  string(mcp.MethodPing),
		Params:  map[string]any{},
	}
	// Discovery only needs the NATS reply header. Ping is the smallest MCP
	// request every stateless leaf can answer without changing catalog state.
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	iter, err := h.bus.RequestManyMsg(
		ctx,
		newLeafRequest(ctx, http.Header{}, shardbus.DiscoverySubject(h.subjectPrefix), body),
	)
	if err != nil {
		if parentErr := parent.Err(); parentErr != nil {
			return nil, parentErr
		}
		// No leaf answer is a successful discovery of zero shards. Invalid
		// shard replies and real NATS errors still make readiness fail.
		if isNoReplyError(err) {
			return nil, nil
		}
		return nil, err
	}
	shards := map[string]struct{}{}
	var errs []error
	for msg, err := range iter {
		if err != nil {
			// The context timeout caps discovery collection; it is not proof
			// that any leaf exists but failed.
			if isNoReplyError(err) {
				continue
			}
			errs = append(errs, err)
			continue
		}
		shardID := msg.Header.Get(shardbus.ShardIDHeader)
		if !config.ValidShardID(shardID) {
			errs = append(errs, fmt.Errorf("invalid discovery shard_id %q", shardID))
			continue
		}
		shards[shardID] = struct{}{}
	}
	if err := parent.Err(); err != nil {
		return nil, err
	}
	return slices.Sorted(maps.Keys(shards)), errors.Join(errs...)
}
