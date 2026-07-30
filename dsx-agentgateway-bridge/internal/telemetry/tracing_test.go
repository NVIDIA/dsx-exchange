// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package telemetry

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

func TestInitKeepsMetricsAndPropagationWhenTraceExporterDisabled(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "")
	t.Setenv("OTEL_TRACES_EXPORTER", "none")
	t.Setenv("OTEL_PROPAGATORS", "")
	metricsEndpoint := configurePrometheusExporter(t)

	shutdown, err := Init(context.Background(), "test-component")
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	if shutdown == nil {
		t.Fatal("Init() shutdown = nil")
	}
	t.Cleanup(func() {
		if err := shutdown(context.Background()); err != nil {
			t.Fatalf("shutdown telemetry: %v", err)
		}
	})

	handler := otelhttp.NewHandler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}), "test")
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/test", nil))
	resp := waitForPrometheus(t, metricsEndpoint+"/metrics")
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read Prometheus scrape: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Prometheus scrape status=%d body=%s", resp.StatusCode, body)
	}
	for _, metric := range []string{"http_server_request_duration_seconds", "go_memory_used_bytes"} {
		if !strings.Contains(string(body), metric) {
			t.Fatalf("Prometheus scrape missing %s: %s", metric, body)
		}
	}

	ctx := contextWithFixedSpan(t)
	headers := http.Header{}
	InjectTraceContext(ctx, headers)
	if got := headers.Get("Traceparent"); !strings.HasPrefix(got, "00-11111111111111111111111111111111-2222222222222222-") {
		t.Fatalf("Traceparent = %q, want propagation without trace export", got)
	}
}

func TestInitAcceptsPartialResource(t *testing.T) {
	t.Setenv("OTEL_RESOURCE_ATTRIBUTES", "valid=yes,missing-value")
	t.Setenv("OTEL_TRACES_EXPORTER", "none")
	t.Setenv("OTEL_METRICS_EXPORTER", "none")

	shutdown, err := Init(context.Background(), "test-component")
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	if err := shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown telemetry: %v", err)
	}
}

func TestInitHonorsStandardMetricsExporterDisable(t *testing.T) {
	t.Setenv("OTEL_TRACES_EXPORTER", "none")
	t.Setenv("OTEL_METRICS_EXPORTER", "none")

	shutdown, err := Init(context.Background(), "test-component")
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	t.Cleanup(func() {
		if err := shutdown(context.Background()); err != nil {
			t.Fatalf("shutdown telemetry: %v", err)
		}
	})
}

func configurePrometheusExporter(t *testing.T) string {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve Prometheus port: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatalf("release Prometheus port: %v", err)
	}
	t.Setenv("OTEL_METRICS_EXPORTER", "prometheus")
	t.Setenv("OTEL_EXPORTER_PROMETHEUS_HOST", "127.0.0.1")
	t.Setenv("OTEL_EXPORTER_PROMETHEUS_PORT", strconv.Itoa(port))
	return "http://127.0.0.1:" + strconv.Itoa(port)
}

func waitForPrometheus(t *testing.T, endpoint string) *http.Response {
	t.Helper()

	client := &http.Client{Timeout: time.Second}
	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()
	retry := time.NewTicker(10 * time.Millisecond)
	defer retry.Stop()
	for {
		resp, err := client.Get(endpoint)
		if err == nil {
			return resp
		}
		select {
		case <-deadline.C:
			t.Fatalf("scrape autoexport Prometheus server: %v", err)
		case <-retry.C:
		}
	}
}

func TestTracerProviderHonorsStandardSamplerEnv(t *testing.T) {
	t.Setenv("OTEL_TRACES_SAMPLER", "parentbased_traceidratio")
	t.Setenv("OTEL_TRACES_SAMPLER_ARG", "0")

	exporter := tracetest.NewInMemoryExporter()
	tp := newTracerProvider(exporter, resource.Empty())
	t.Cleanup(func() {
		if err := tp.Shutdown(context.Background()); err != nil {
			t.Fatalf("shutdown tracer provider: %v", err)
		}
	})

	_, span := tp.Tracer("test").Start(context.Background(), "dropped")
	span.End()
	if err := tp.ForceFlush(context.Background()); err != nil {
		t.Fatalf("force flush tracer provider: %v", err)
	}
	if got := exporter.GetSpans(); len(got) != 0 {
		t.Fatalf("exported %d span(s), want none when OTEL_TRACES_SAMPLER_ARG=0", len(got))
	}
}

func TestTraceContextInjectionAndExtractionRoundTrip(t *testing.T) {
	installTraceContextPropagator(t)
	ctx := contextWithFixedSpan(t)
	headers := http.Header{}

	InjectTraceContext(ctx, headers)
	if got := headers.Get("Traceparent"); !strings.HasPrefix(got, "00-11111111111111111111111111111111-2222222222222222-") {
		t.Fatalf("Traceparent = %q, want fixed trace/span IDs", got)
	}
	extracted := ExtractTraceContext(context.Background(), headers)
	got := trace.SpanContextFromContext(extracted)
	want := trace.SpanContextFromContext(ctx)
	if got.TraceID() != want.TraceID() || got.SpanID() != want.SpanID() {
		t.Fatalf("extracted span context = %s/%s, want %s/%s", got.TraceID(), got.SpanID(), want.TraceID(), want.SpanID())
	}
}

func installTraceContextPropagator(t *testing.T) {
	t.Helper()

	previous := otel.GetTextMapPropagator()
	otel.SetTextMapPropagator(propagation.TraceContext{})
	t.Cleanup(func() { otel.SetTextMapPropagator(previous) })
}

func contextWithFixedSpan(t *testing.T) context.Context {
	t.Helper()

	traceID, err := trace.TraceIDFromHex("11111111111111111111111111111111")
	if err != nil {
		t.Fatalf("parse trace ID: %v", err)
	}
	spanID, err := trace.SpanIDFromHex("2222222222222222")
	if err != nil {
		t.Fatalf("parse span ID: %v", err)
	}
	spanContext := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    traceID,
		SpanID:     spanID,
		TraceFlags: trace.FlagsSampled,
	})
	return trace.ContextWithSpanContext(context.Background(), spanContext)
}
