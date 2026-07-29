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
