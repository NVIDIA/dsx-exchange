// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package tracing

import (
	"context"

	"github.com/uptrace/opentelemetry-go-extra/otelzap"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.uber.org/zap"
	"google.golang.org/grpc/credentials/insecure"

	obsconfig "github.com/NVIDIA/dsx-exchange/auth-callout/src/internal/observability/config"
)

// ApplyTracing configures the global trace provider.
func ApplyTracing(ctx context.Context, cfg *obsconfig.TracingConfig, telemetry *obsconfig.TelemetryConfig, logger *otelzap.Logger) func() {
	if !cfg.Enabled {
		return func() {}
	}
	if cfg.Provider != "" && cfg.Provider != "otlp" {
		logger.Warn("tracing disabled for unsupported provider", zap.String("provider", cfg.Provider))
		return func() {}
	}

	opts := []otlptracegrpc.Option{}
	if cfg.OTLP.Endpoint != "" {
		opts = append(opts, otlptracegrpc.WithEndpoint(cfg.OTLP.Endpoint))
	}
	if !cfg.HTTPS && !cfg.OTLP.HTTPS {
		opts = append(opts, otlptracegrpc.WithTLSCredentials(insecure.NewCredentials()))
	}

	exporter, err := otlptracegrpc.New(ctx, opts...)
	if err != nil {
		logger.Warn("tracing disabled after exporter setup failure", zap.Error(err))
		return func() {}
	}

	serviceName := telemetry.ServiceName
	if serviceName == "" {
		serviceName = "auth-callout"
	}
	res, err := resource.New(ctx,
		resource.WithAttributes(
			attribute.String("service.name", serviceName),
			attribute.String("service.version", telemetry.ServiceVersion),
			attribute.String("deployment.environment.name", telemetry.DeploymentEnvironmentName),
		),
	)
	if err != nil {
		logger.Warn("trace resource setup failed", zap.Error(err))
		res = resource.Default()
	}

	provider := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(provider)

	return func() {
		if err := provider.Shutdown(context.Background()); err != nil {
			logger.Warn("trace provider shutdown failed", zap.Error(err))
		}
	}
}

// LevelInfo starts a lightweight span around an operation.
func LevelInfo(ctx context.Context) (context.Context, func()) {
	tracer := otel.Tracer("auth-callout")
	ctx, span := tracer.Start(ctx, "auth-callout.request")
	return ctx, func() {
		span.End()
	}
}
