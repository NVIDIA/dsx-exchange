// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package logging

import (
	"context"
	"log"
	"strings"

	"github.com/uptrace/opentelemetry-go-extra/otelzap"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	obsconfig "github.com/NVIDIA/dsx-exchange/auth-callout/src/internal/observability/config"
)

type loggerKey struct{}

type zapWriter struct {
	logger *zap.Logger
}

// SetupLoggingFromConfig creates the service logger and returns a cleanup function.
func SetupLoggingFromConfig(cfg *obsconfig.LoggingConfig, telemetry *obsconfig.TelemetryConfig) (*otelzap.Logger, func()) {
	level := zap.NewAtomicLevelAt(parseLevel(cfg.Level))

	var zapCfg zap.Config
	if strings.EqualFold(cfg.ZapConfiguration, "development") {
		zapCfg = zap.NewDevelopmentConfig()
	} else {
		zapCfg = zap.NewProductionConfig()
	}
	zapCfg.Level = level

	zapLogger, err := zapCfg.Build()
	if err != nil {
		zapLogger = zap.NewNop()
	}

	serviceName := telemetry.ServiceName
	if serviceName == "" {
		serviceName = "auth-callout"
	}

	logger := otelzap.New(zapLogger.With(zap.String("service.name", serviceName)))
	return logger, func() {
		_ = zapLogger.Sync()
	}
}

// NewLoggerWithZapWriter adapts zap for packages that expect a standard logger.
func NewLoggerWithZapWriter(logger *zap.Logger) *log.Logger {
	return log.New(&zapWriter{logger: logger.Named("http")}, "", 0)
}

// AttachLoggerToContext returns a context carrying logger.
func AttachLoggerToContext(ctx context.Context, logger *otelzap.Logger) context.Context {
	return context.WithValue(ctx, loggerKey{}, logger)
}

// GetLogger returns the logger attached to ctx, or a no-op logger.
func GetLogger(ctx context.Context) *otelzap.Logger {
	if logger, ok := ctx.Value(loggerKey{}).(*otelzap.Logger); ok && logger != nil {
		return logger
	}
	return otelzap.New(zap.NewNop())
}

func (w *zapWriter) Write(p []byte) (int, error) {
	msg := strings.TrimSpace(string(p))
	if msg != "" {
		w.logger.Error(msg)
	}
	return len(p), nil
}

func parseLevel(level string) zapcore.Level {
	switch strings.ToLower(level) {
	case "debug":
		return zapcore.DebugLevel
	case "warn", "warning":
		return zapcore.WarnLevel
	case "error":
		return zapcore.ErrorLevel
	default:
		return zapcore.InfoLevel
	}
}
