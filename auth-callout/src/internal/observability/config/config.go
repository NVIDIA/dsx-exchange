// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package config

// ObservabilityConfig contains logging, tracing, metrics, and service metadata.
type ObservabilityConfig struct {
	Logging   LoggingConfig   `koanf:"logging"`
	Telemetry TelemetryConfig `koanf:"telemetry"`
	Tracing   TracingConfig   `koanf:"tracing"`
	Metrics   MetricsConfig   `koanf:"metrics"`
}

type LoggingConfig struct {
	Level            string `koanf:"level"`
	ZapConfiguration string `koanf:"zap-configuration"`
}

type TelemetryConfig struct {
	DeploymentEnvironmentName string `koanf:"deployment-environment-name"`
	ServiceName               string `koanf:"service-name"`
	ServiceVersion            string `koanf:"service-version"`
}

type TracingConfig struct {
	Enabled  bool       `koanf:"enabled"`
	HTTPS    bool       `koanf:"https"`
	Provider string     `koanf:"provider"`
	Level    string     `koanf:"level"`
	OTLP     OTLPConfig `koanf:"otlp"`
}

type MetricsConfig struct {
	Enabled    bool             `koanf:"enabled"`
	Provider   string           `koanf:"provider"`
	OTLP       OTLPConfig       `koanf:"otlp"`
	Prometheus PrometheusConfig `koanf:"prometheus"`
}

type OTLPConfig struct {
	Endpoint          string `koanf:"endpoint"`
	HTTPS             bool   `koanf:"https"`
	ExportIntervalSec int    `koanf:"export-interval-sec"`
	ExportTimeoutSec  int    `koanf:"export-timeout-sec"`
}

type PrometheusConfig struct {
	Port int `koanf:"port"`
}
