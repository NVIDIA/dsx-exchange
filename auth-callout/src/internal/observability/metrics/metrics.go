// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package metrics

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/uptrace/opentelemetry-go-extra/otelzap"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.uber.org/zap"
	"google.golang.org/grpc/credentials/insecure"

	obsconfig "github.com/NVIDIA/dsx-exchange/auth-callout/src/internal/observability/config"
)

// GetMeter returns the service meter.
func GetMeter(serviceName string) metric.Meter {
	if serviceName == "" {
		serviceName = "auth-callout"
	}
	return otel.Meter(serviceName)
}

// ApplyMetrics configures the global metrics provider and optional Prometheus endpoint.
func ApplyMetrics(ctx context.Context, cfg *obsconfig.MetricsConfig, telemetry *obsconfig.TelemetryConfig, logger *otelzap.Logger) func() {
	if !cfg.Enabled {
		return func() {}
	}

	provider, cleanupProvider, err := buildProvider(ctx, cfg)
	if err != nil {
		logger.Warn("metrics disabled after provider setup failure", zap.Error(err))
		return func() {}
	}
	otel.SetMeterProvider(provider)

	var server *http.Server
	if cfg.Provider == "prometheus" {
		port := cfg.Prometheus.Port
		if port == 0 {
			port = 9090
		}
		server = &http.Server{
			Addr:              fmt.Sprintf(":%d", port),
			Handler:           promhttp.Handler(),
			ReadHeaderTimeout: 5 * time.Second,
		}
		go func() {
			logger.Info("metrics endpoint started", zap.Int("port", port))
			if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				logger.Error("metrics endpoint failed", zap.Error(err))
			}
		}()
	}

	return func() {
		if server != nil {
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			_ = server.Shutdown(shutdownCtx)
			cancel()
		}
		cleanupProvider()
	}
}

func buildProvider(ctx context.Context, cfg *obsconfig.MetricsConfig) (*sdkmetric.MeterProvider, func(), error) {
	switch cfg.Provider {
	case "prometheus":
		reader, err := prometheus.New()
		if err != nil {
			return nil, nil, err
		}
		provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
		return provider, func() { _ = provider.Shutdown(context.Background()) }, nil
	case "otlp":
		opts := []otlpmetricgrpc.Option{}
		if cfg.OTLP.Endpoint != "" {
			opts = append(opts, otlpmetricgrpc.WithEndpoint(cfg.OTLP.Endpoint))
		}
		if !cfg.OTLP.HTTPS {
			opts = append(opts, otlpmetricgrpc.WithTLSCredentials(insecure.NewCredentials()))
		}
		exporter, err := otlpmetricgrpc.New(ctx, opts...)
		if err != nil {
			return nil, nil, err
		}
		interval := time.Duration(cfg.OTLP.ExportIntervalSec) * time.Second
		if interval == 0 {
			interval = 30 * time.Second
		}
		reader := sdkmetric.NewPeriodicReader(exporter, sdkmetric.WithInterval(interval))
		provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
		return provider, func() { _ = provider.Shutdown(context.Background()) }, nil
	default:
		return nil, nil, fmt.Errorf("unsupported metrics provider %q", cfg.Provider)
	}
}
