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

package shardbus

import (
	"context"
	"errors"
	"iter"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/synadia-io/orbit.go/natsext"
)

const (
	ShardIDHeader         = "DAG-Bridge-Shard-Id"
	SubscriptionFlushWait = 5 * time.Second
	// Catalog list is global so one leaf answers instead of every shard.
	ListQueueGroup = "dsx-agentgateway-bridge-list"
)

type Requester interface {
	RequestMsgWithContext(context.Context, *nats.Msg) (*nats.Msg, error)
	RequestManyMsg(context.Context, *nats.Msg, ...natsext.RequestManyOpt) (iter.Seq2[*nats.Msg, error], error)
}

type Conn struct {
	*nats.Conn
}

func (n Conn) RequestManyMsg(ctx context.Context, msg *nats.Msg, opts ...natsext.RequestManyOpt) (iter.Seq2[*nats.Msg, error], error) {
	return natsext.RequestManyMsg(ctx, n.Conn, msg, opts...)
}

// DrainConnection waits until NATS has flushed pending publications and
// closed the connection. A canceled context forces the connection closed.
func DrainConnection(ctx context.Context, nc *nats.Conn) error {
	closed := nc.StatusChanged(nats.CLOSED)
	defer nc.RemoveStatusListener(closed)
	if nc.IsClosed() {
		return nil
	}
	if err := nc.Drain(); err != nil {
		if errors.Is(err, nats.ErrConnectionClosed) {
			return nil
		}
		nc.Close()
		return err
	}
	select {
	case <-closed:
		return nil
	case <-ctx.Done():
		nc.Close()
		return ctx.Err()
	}
}

func subject(prefix, suffix string) string {
	return prefix + "." + suffix
}

func DiscoverySubject(prefix string) string {
	return subject(prefix, "leaf.discover")
}

func ListSubject(prefix string) string {
	return subject(prefix, "leaf.list")
}

func StartupSubject(prefix string) string {
	return subject(prefix, "leaf.startup")
}

func MCPSubject(prefix, shardID string) string {
	return subject(prefix, "leaf."+shardID+".mcp")
}

func MCPStreamSubject(prefix, shardID, instanceID string) string {
	return MCPSubject(prefix, shardID) + "." + instanceID
}

func QueueGroup(shardID string) string {
	// One queue group per shard keeps HA replicas from duplicate-answering the
	// same directed shard request.
	return "dsx-agentgateway-bridge-" + shardID
}
