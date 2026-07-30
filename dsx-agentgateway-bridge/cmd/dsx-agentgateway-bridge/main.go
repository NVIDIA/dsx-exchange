// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/NVIDIA/dsx-exchange/dsx-agentgateway-bridge/internal/config"
	"github.com/NVIDIA/dsx-exchange/dsx-agentgateway-bridge/internal/hub"
	"github.com/NVIDIA/dsx-exchange/dsx-agentgateway-bridge/internal/leaf"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))

	role, err := config.RoleFromArgs(os.Args[1:])
	if err != nil {
		fatalf("%v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	switch role {
	case config.RoleHub:
		cfg, err := config.LoadHub()
		if err != nil {
			fatalf("%v", err)
		}
		if err := hub.Run(ctx, cfg); err != nil {
			fatalf("bridge-hub server failed: %v", err)
		}
	case config.RoleLeaf:
		cfg, err := config.LoadLeaf()
		if err != nil {
			fatalf("%v", err)
		}
		if err := leaf.Run(ctx, cfg); err != nil {
			fatalf("bridge-leaf failed: %v", err)
		}
	default:
		fatalf("unsupported bridge role %q", role)
	}
}

func fatalf(format string, args ...any) {
	slog.Error(fmt.Sprintf(format, args...))
	os.Exit(1)
}
