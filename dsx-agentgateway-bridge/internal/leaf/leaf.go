// Copyright 2026 NVIDIA Corporation
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//
// SPDX-License-Identifier: Apache-2.0

package leaf

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/micro"

	"github.com/NVIDIA/dsx-exchange/dsx-agentgateway-bridge/internal/config"
	"github.com/NVIDIA/dsx-exchange/dsx-agentgateway-bridge/internal/health"
	bridgemcp "github.com/NVIDIA/dsx-exchange/dsx-agentgateway-bridge/internal/mcp"
	"github.com/NVIDIA/dsx-exchange/dsx-agentgateway-bridge/internal/shardbus"
	"github.com/NVIDIA/dsx-exchange/dsx-agentgateway-bridge/internal/telemetry"
)

type publisher interface {
	PublishMsg(*nats.Msg) error
}

type leafServices struct {
	requests       micro.Service
	streamRequests micro.Service
}

func Run(ctx context.Context, cfg config.Leaf) error {
	cfg, err := config.NormalizeLeaf(cfg)
	if err != nil {
		return err
	}

	shutdownTracing, err := telemetry.Init(ctx, "dsx-agentgateway-bridge-leaf")
	if err != nil {
		return err
	}
	defer telemetry.Shutdown(shutdownTracing)

	nc, err := nats.Connect(cfg.NATSURL, cfg.NATSOptions...)
	if err != nil {
		return fmt.Errorf("connect to NATS %s: %w", cfg.NATSURL, err)
	}

	registry := newStreamRegistry(ctx, nc, cfg.Timeout)
	services, err := startLeafServices(nc, cfg, registry)
	if err != nil {
		forceCloseLeaf(nc, registry)
		return err
	}
	shutdown := sync.OnceFunc(func() { shutdownLeaf(nc, services, registry, cfg.Timeout) })
	defer shutdown()
	if err := nc.FlushTimeout(shardbus.SubscriptionFlushWait); err != nil {
		return fmt.Errorf("confirm NATS subscriptions: %w", err)
	}
	if err := publishLeafStartup(nc, cfg.SubjectPrefix, cfg.ShardID); err != nil {
		return err
	}
	if err := nc.FlushTimeout(shardbus.SubscriptionFlushWait); err != nil {
		return fmt.Errorf("confirm NATS subscriptions: %w", err)
	}
	healthServer, err := health.Start(cfg.HealthAddr, nc.IsConnected)
	if err != nil {
		return err
	}
	defer health.Shutdown(healthServer)
	slog.Info("bridge-leaf started", "shard_id", cfg.ShardID, "local_gateway", cfg.LocalGatewayOrigin)
	<-ctx.Done()
	slog.Info("bridge-leaf shutting down", "reason", ctx.Err())
	shutdown()
	return nil
}

func shutdownLeaf(nc *nats.Conn, services leafServices, registry *streamRegistry, timeout time.Duration) {
	requestCtx, cancelRequestDrain := context.WithTimeout(context.Background(), timeout)
	defer cancelRequestDrain()
	if err := drainService(requestCtx, nc, services.requests); err != nil {
		slog.Error("drain NATS request service", "error", err)
		forceCloseLeaf(nc, registry)
		return
	}
	registry.waitForRequests(requestCtx)

	streamCtx, cancelStreamDrain := context.WithTimeout(context.Background(), timeout)
	defer cancelStreamDrain()
	if err := drainService(streamCtx, nc, services.streamRequests); err != nil {
		slog.Error("drain NATS stream request service", "error", err)
		forceCloseLeaf(nc, registry)
		return
	}
	registry.close()
	if err := shardbus.DrainConnection(streamCtx, nc); err != nil {
		slog.Error("drain NATS connection", "error", err)
	}
}

func forceCloseLeaf(nc *nats.Conn, registry *streamRegistry) {
	registry.cancel()
	nc.Close()
}

func drainService(ctx context.Context, nc *nats.Conn, service micro.Service) error {
	if err := service.Stop(); err != nil {
		return err
	}
	// Stop starts draining subscriptions. Flush confirms that the server removed
	// their interest, then Barrier follows every locally queued callback.
	if err := nc.FlushWithContext(ctx); err != nil {
		return err
	}
	done := make(chan struct{})
	if err := nc.Barrier(func() { close(done) }); err != nil {
		return err
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func startLeafServices(nc *nats.Conn, cfg config.Leaf, registry *streamRegistry) (leafServices, error) {
	errorHandler := func(_ micro.Service, natsErr *micro.NATSError) {
		slog.Error("bridge leaf NATS service error", "error", natsErr)
	}
	// The request service restores the stream service's connection handlers when
	// it stops, so stream operations remain observable during request draining.
	streamRequests, err := micro.AddService(nc, micro.Config{
		Name:         "dsx_agentgateway_bridge_leaf_stream",
		Version:      "0.1.0",
		Description:  "DSX Agent Gateway bridge leaf SSE stream handlers",
		ErrorHandler: errorHandler,
	})
	if err != nil {
		return leafServices{}, fmt.Errorf("add NATS stream request service: %w", err)
	}
	requests, err := micro.AddService(nc, micro.Config{
		Name:         "dsx_agentgateway_bridge_leaf",
		Version:      "0.1.0",
		Description:  "DSX Agent Gateway bridge leaf NATS request handlers",
		ErrorHandler: errorHandler,
	})
	if err != nil {
		return leafServices{}, fmt.Errorf("add NATS request service: %w", err)
	}
	services := leafServices{requests: requests, streamRequests: streamRequests}

	discoverySubject := shardbus.DiscoverySubject(cfg.SubjectPrefix)
	shardQueue := shardbus.QueueGroup(cfg.ShardID)
	// Discovery fans in shard IDs, but each shard should contribute once even
	// when multiple bridge-leaf replicas serve the same shard.
	if err := requests.AddEndpoint(
		"discovery",
		micro.HandlerFunc(func(req micro.Request) {
			handleShardDiscovery(registry.pub, req.Reply(), cfg.ShardID)
		}),
		micro.WithEndpointSubject(discoverySubject),
		micro.WithEndpointQueueGroup(shardQueue),
	); err != nil {
		return leafServices{}, fmt.Errorf("add NATS endpoint %s group %s: %w", discoverySubject, shardQueue, err)
	}

	listSubject := shardbus.ListSubject(cfg.SubjectPrefix)
	listQueue := shardbus.ListQueueGroup
	// List is one global catalog answer. The global queue group keeps the hub
	// from collecting and merging one catalog per shard.
	if err := requests.AddEndpoint(
		"list",
		micro.HandlerFunc(func(req micro.Request) {
			data, headers, reply := req.Data(), http.Header(req.Headers()), req.Reply()
			registry.requestTasks.Go(func() {
				ctx, cancel := context.WithTimeout(registry.ctx, cfg.Timeout)
				defer cancel()
				handleList(ctx, registry.pub, reply, data, headers, cfg.ShardID, cfg.LocalGatewayOrigin)
			})
		}),
		micro.WithEndpointSubject(listSubject),
		micro.WithEndpointQueueGroup(listQueue),
	); err != nil {
		return leafServices{}, fmt.Errorf("add NATS endpoint %s group %s: %w", listSubject, listQueue, err)
	}

	mcpSubject := shardbus.MCPSubject(cfg.SubjectPrefix, cfg.ShardID)
	streamSubject := shardbus.MCPStreamSubject(cfg.SubjectPrefix, cfg.ShardID, streamRequests.Info().ID)
	// Register the direct endpoint first so an initial SSE reply never advertises
	// a stream subject that is not yet listening.
	if err := streamRequests.AddEndpoint(
		"stream",
		micro.HandlerFunc(func(req micro.Request) {
			headers, reply := nats.Header(req.Headers()), req.Reply()
			registry.streamRequestTasks.Go(func() { registry.handleStreamRequest(headers, reply) })
		}),
		micro.WithEndpointSubject(streamSubject),
		micro.WithEndpointQueueGroupDisabled(),
	); err != nil {
		return leafServices{}, fmt.Errorf("add NATS endpoint %s: %w", streamSubject, err)
	}

	// Directed invocations use a shard-specific subject plus the shard queue
	// group so exactly one HA replica for that shard handles the request.
	if err := requests.AddEndpoint(
		"mcp",
		micro.HandlerFunc(func(req micro.Request) {
			data, headers, reply := req.Data(), http.Header(req.Headers()), req.Reply()
			registry.requestTasks.Go(func() {
				ctx, cancel := context.WithTimeout(registry.ctx, cfg.Timeout)
				defer cancel()
				handleInitialRequest(ctx, data, headers, reply, cfg.LocalGatewayOrigin, registry, streamSubject)
			})
		}),
		micro.WithEndpointSubject(mcpSubject),
		micro.WithEndpointQueueGroup(shardQueue),
	); err != nil {
		return leafServices{}, fmt.Errorf("add NATS endpoint %s group %s: %w", mcpSubject, shardQueue, err)
	}
	return services, nil
}

func publishLeafStartup(pub publisher, subjectPrefix, shardID string) error {
	msg := nats.NewMsg(shardbus.StartupSubject(subjectPrefix))
	msg.Header.Set(shardbus.ShardIDHeader, shardID)
	// Startup is only a cache-refresh hint. Discovery remains the source of
	// truth because notifications can be missed during hub restarts.
	if err := pub.PublishMsg(msg); err != nil {
		return fmt.Errorf("publish %s: %w", msg.Subject, err)
	}
	return nil
}

func handleShardDiscovery(pub publisher, reply, shardID string) {
	if err := publishReply(pub, reply, nil, nats.Header{
		shardbus.ShardIDHeader: []string{shardID},
	}); err != nil {
		slog.Error("respond to discovery request", "shard_id", shardID, "error", err)
	}
}

func handleList(ctx context.Context, pub publisher, reply string, requestData []byte, headers http.Header, shardID, localGatewayOrigin string) {
	response, err := bridgemcp.ForwardStatelessRequest(ctx, requestData, headers, localGatewayOrigin)
	if err != nil {
		slog.Error("forward to local gateway failed", "error", err)
		response, err = marshalRPCErrorForRequest(requestData, bridgemcp.CodeLeafForwardFailed, err.Error())
		if err != nil {
			slog.Error("build list response", "shard_id", shardID, "error", err)
			return
		}
	}
	if err := publishReply(pub, reply, response, nats.Header{
		shardbus.ShardIDHeader: []string{shardID},
	}); err != nil {
		slog.Error("respond to list request", "shard_id", shardID, "error", err)
	}
}

func marshalRPCErrorForRequest(requestData []byte, code int, message string) ([]byte, error) {
	return marshalRPCError(rpcRequestID(requestData), code, message)
}

func rpcRequestID(requestData []byte) mcp.RequestId {
	id := mcp.NewRequestId(nil)
	if req, err := bridgemcp.DecodeRequest(requestData); err == nil {
		// Preserve the caller's id when possible so JSON-RPC clients can match
		// the leaf-generated error to the original request.
		id = req.ID
	}
	return id
}

func marshalRPCError(id mcp.RequestId, code int, message string) ([]byte, error) {
	return json.Marshal(mcp.NewJSONRPCError(id, code, message, nil))
}
