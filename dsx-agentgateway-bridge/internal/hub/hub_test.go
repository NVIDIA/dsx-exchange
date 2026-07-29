// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package hub

import (
	"context"
	"strings"
	"testing"

	"github.com/nats-io/nats.go"

	"github.com/NVIDIA/dsx-exchange/dsx-agentgateway-bridge/internal/config"
)

func TestRunHubRejectsInvalidConfig(t *testing.T) {
	t.Parallel()

	err := Run(context.Background(), config.Hub{
		NATSURL:       "nats://example:4222",
		SubjectPrefix: "Invalid Prefix With Spaces",
	})
	if err == nil {
		t.Fatalf("RunHub accepted an invalid subject prefix")
	}
	if !strings.Contains(err.Error(), config.EnvSubjectPrefix) {
		t.Fatalf("error %q does not mention %s", err, config.EnvSubjectPrefix)
	}
}

func TestRunHubAcceptsDynamicDiscoveryWithoutConfiguredLeaves(t *testing.T) {
	t.Parallel()

	_, err := NewHandler(config.Hub{
		NATSURL: "nats://example:4222",
		Bus:     newStaticNATSRequester(t, nil),
	})
	if err != nil {
		t.Fatalf("NewHubHandler rejected dynamic discovery config: %v", err)
	}
}

func TestRunHubReturnsConnectErrorWhenBusUnset(t *testing.T) {
	t.Parallel()

	// Bus is nil so RunHub dials NATS itself. With no reachable server and
	// retry disabled, connectNATS returns synchronously.
	err := Run(context.Background(), config.Hub{
		NATSURL:     "nats://127.0.0.1:1",
		NATSOptions: []nats.Option{nats.RetryOnFailedConnect(false)},
	})
	if err == nil {
		t.Fatalf("RunHub accepted an unreachable NATS server")
	}
}
