// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	_ "embed"
	"fmt"
	"log"

	"github.com/spf13/cobra"
	"go.uber.org/zap"

	"github.com/NVIDIA/dsx-exchange/auth-callout/src/internal/appconfig"
	obslogging "github.com/NVIDIA/dsx-exchange/auth-callout/src/internal/observability/logging"
	obsmetrics "github.com/NVIDIA/dsx-exchange/auth-callout/src/internal/observability/metrics"
	obstracing "github.com/NVIDIA/dsx-exchange/auth-callout/src/internal/observability/tracing"
	"github.com/NVIDIA/dsx-exchange/auth-callout/src/internal/service"
)

//go:embed config/defaults.yaml
var defaultConfigYAML string

// Global config manager
var globalConfig *appconfig.Manager

var rootCmd = &cobra.Command{
	Use:   "auth-callout",
	Short: "NATS Auth Callout service for authenticating NATS connections",
	Long:  `A NATS Auth Callout service that authenticates connections using OAuth2/JWKS, mTLS, NKey, or anonymous authentication.`,
	RunE:  runServer,
}

var generateCmd = &cobra.Command{
	Use:   "generate-config",
	Short: "Generate complete default configuration file",
	Long:  `Generate a complete configuration file showing all available options from service and library defaults`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := globalConfig

		if err := cfg.Load(); err != nil {
			return fmt.Errorf("failed to load defaults: %w", err)
		}

		yamlBytes, err := cfg.ExportYAML()
		if err != nil {
			return fmt.Errorf("failed to export YAML: %w", err)
		}

		fmt.Print(string(yamlBytes))
		return nil
	},
}

// runServer is the main entry point for running the service.
// It loads configuration, sets up logging, tracing, and metrics, then creates
// and runs the service instance.
func runServer(_ *cobra.Command, _ []string) error {
	ctx := context.Background()
	cfgManager := globalConfig

	if err := cfgManager.Load(); err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	var svcConfig service.ServiceConfig
	if err := cfgManager.Unmarshal(&svcConfig); err != nil {
		return fmt.Errorf("failed to unmarshal service config: %w", err)
	}

	logger, undoLogger := obslogging.SetupLoggingFromConfig(&svcConfig.Observability.Logging, &svcConfig.Observability.Telemetry)
	defer undoLogger()

	logger.Warn("config loaded", zap.Any("svcConfig", svcConfig))

	undoTracingProvider := obstracing.ApplyTracing(
		ctx,
		&svcConfig.Observability.Tracing,
		&svcConfig.Observability.Telemetry,
		logger,
	)
	defer undoTracingProvider()

	undoMetricsProvider := obsmetrics.ApplyMetrics(
		ctx,
		&svcConfig.Observability.Metrics,
		&svcConfig.Observability.Telemetry,
		logger,
	)
	defer undoMetricsProvider()

	// Create and run server
	svc := service.New(svcConfig, logger)
	return svc.Run()
}

func main() {
	globalConfig = appconfig.New(defaultConfigYAML)
	globalConfig.BindFlags(rootCmd)

	// Add subcommands
	rootCmd.AddCommand(generateCmd)

	// Execute the root command
	if err := rootCmd.Execute(); err != nil {
		log.Fatalf("Failed to execute: %v", err)
	}
}
