// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package functional

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/NVIDIA/dsx-exchange/local/agent-gateway/tests/functional/internal/runner"
	"github.com/mark3labs/mcp-go/mcp"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

const cscEventBusNS = "csc-event-bus"

func TestSessionLifecycleUnknownSessionRejectedAndReplacementWorks(t *testing.T) {
	runner.ParallelReadOnly(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	tok, sid := initSession(t, ctx, tenantAUnlimited)
	ok := postSessionMCP(t, ctx, tok, sid, []byte(`{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`))
	requireMCPListSuccess(t, ok)

	unknown := postSessionMCP(t, ctx, tok, "missing-session-id", []byte(`{"jsonrpc":"2.0","id":3,"method":"tools/list","params":{}}`))
	requireNoMCPListLeak(t, unknown, []string{"echo", "headers", "mcp-backend-a-mcp_echo"})

	_, replacementSID := initSession(t, ctx, tenantAUnlimited)
	if replacementSID == "" || replacementSID == sid {
		t.Fatalf("replacement session id = %q, original = %q", replacementSID, sid)
	}
	replacement := postSessionMCP(t, ctx, tok, replacementSID, []byte(`{"jsonrpc":"2.0","id":4,"method":"tools/list","params":{}}`))
	requireMCPListSuccess(t, replacement)
}

func TestMalformedMCPParametersReturnStructuredErrors(t *testing.T) {
	runner.ParallelReadOnly(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	tok, sid := initSession(t, ctx, tenantBUnlimited)
	valid := postSessionMCP(t, ctx, tok, sid, []byte(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"mcp-backend-a-mcp_echo","arguments":{"message":"ok"}}}`))
	requireHTTPSuccess(t, valid)

	cases := []struct {
		name string
		body string
	}{
		{
			name: "tools call non-string name",
			body: `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":123,"arguments":{}}}`,
		},
		{
			name: "prompts get missing name",
			body: `{"jsonrpc":"2.0","id":4,"method":"prompts/get","params":{}}`,
		},
		{
			name: "resources read non-string uri",
			body: `{"jsonrpc":"2.0","id":5,"method":"resources/read","params":{"uri":123}}`,
		},
		{
			name: "invalid json",
			body: `{"jsonrpc":"2.0","id":6,"method":"tools/list","params":`,
		},
		{
			name: "missing jsonrpc",
			body: `{"id":7,"method":"tools/list","params":{}}`,
		},
		{
			name: "wrong jsonrpc",
			body: `{"jsonrpc":"1.0","id":8,"method":"tools/list","params":{}}`,
		},
		{
			name: "trailing json",
			body: `{"jsonrpc":"2.0","id":9,"method":"tools/list","params":{}} {}`,
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			resp := postSessionMCP(t, ctx, tok, sid, []byte(tc.body))
			requireStructuredErrorSignal(t, resp)
		})
	}
}

func TestBridgeNotificationsReturnAcceptedThroughGateway(t *testing.T) {
	runner.ParallelReadOnly(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	tok, sid := initSession(t, ctx, tenantAUnlimited)
	for _, method := range []string{
		string(mcp.MethodNotificationInitialized),
		string(mcp.MethodNotificationToolsListChanged),
		string(mcp.MethodNotificationPromptsListChanged),
		string(mcp.MethodNotificationResourcesListChanged),
		string(mcp.MethodNotificationTasksStatus),
	} {
		method := method
		t.Run(method, func(t *testing.T) {
			resp := postSessionMCP(t, ctx, tok, sid, []byte(fmt.Sprintf(`{"jsonrpc":"2.0","method":%q}`, method)))
			if resp.Status != http.StatusAccepted {
				t.Fatalf("%s status = %d, want 202 (body: %s)", method, resp.Status, resp.Body)
			}
			if body := bytes.TrimSpace(resp.Body); len(body) != 0 {
				t.Fatalf("%s body = %q, want empty notification response", method, body)
			}
		})
	}
}

func TestComponentHealthEndpointsLive(t *testing.T) {
	runner.ParallelReadOnly(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	serviceProxyGET(t, ctx, cscGatewayNS, cscGatewayName+"-ratelimit", "http", "healthcheck")
	serviceProxyGET(t, ctx, cscGatewayNS, cscBridgeService, "mcp", "livez")
	serviceProxyGET(t, ctx, cscGatewayNS, cscBridgeService, "mcp", "readyz")
	serviceProxyGET(t, ctx, cscBackendNS, "mcp-backend-a", "mcp", "healthz")

	leafPod := firstRunningPodName(t, cpc1GatewayNS, bridgePodSelector)
	podProxyGET(t, ctx, cpc1GatewayNS, leafPod, 3001, "livez")
	podProxyGET(t, ctx, cpc1GatewayNS, leafPod, 3001, "readyz")

	requireGatewayPathNotSuccessful(t, ctx, "/readyz")
}

func TestPrometheusMetricsEndpointsLive(t *testing.T) {
	runner.ParallelReadOnly(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	s := runner.NewSession(t, tenantAUnlimited)
	t.Cleanup(s.Close)
	if _, err := s.ListToolNames(ctx); err != nil {
		t.Fatalf("tools/list before metrics scrape: %v", err)
	}

	cases := []struct {
		name      string
		ns        string
		service   string
		port      string
		path      string
		target    string
		wantToken string
	}{
		{name: "bridge hub", ns: cscGatewayNS, service: cscBridgeService, port: "metrics", path: "metrics", wantToken: "http_server_request_duration_seconds"},
		{name: "rate-limit service", ns: cscGatewayNS, service: cscGatewayName + "-ratelimit", port: "metrics", path: "metrics", wantToken: "ratelimit_service_config_load_success"},
		{
			name: "Valkey exporter", ns: cscGatewayNS, service: cscGatewayName + "-valkey-metrics", port: "metrics", path: "metrics",
			wantToken: "redis_up 1",
		},
		{name: "agentgateway controller", ns: cscGatewayNS, service: cscGatewayName + "-controller", port: "metrics", path: "metrics", wantToken: "# HELP"},
		{name: "auth callout", ns: cscEventBusNS, service: "auth-callout-metrics", port: "metrics", path: "metrics", wantToken: "auth_requests"},
		{name: "NATS Surveyor", ns: cscEventBusNS, service: "nats-event-bus-csc-surveyor", port: "http", path: "metrics", wantToken: "nats_up 1"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			body := serviceProxyGETWithTarget(t, ctx, tc.ns, tc.service, tc.port, tc.path, tc.target)
			if !bytes.Contains(body, []byte(tc.wantToken)) {
				t.Fatalf("%s metrics body did not contain %q: %.200s", tc.name, tc.wantToken, body)
			}
		})
	}
	podMonitor := schema.GroupVersionResource{Group: "monitoring.coreos.com", Version: "v1", Resource: "podmonitors"}
	nackMonitor := runner.GetUnstructured(t, podMonitor, cscEventBusNS, "nack")
	nackLabels, found, err := unstructured.NestedStringMap(nackMonitor.Object, "spec", "selector", "matchLabels")
	if err != nil || !found || len(nackLabels) == 0 {
		t.Fatalf("PodMonitor %s/nack selector.matchLabels invalid: found=%t err=%v", cscEventBusNS, found, err)
	}
	nackPod := firstRunningPodName(t, cscEventBusNS, labels.SelectorFromSet(nackLabels).String())
	if body := podProxyGET(t, ctx, cscEventBusNS, nackPod, 8080, "metrics"); !bytes.Contains(body, []byte("controller_runtime_reconcile_total")) {
		t.Fatalf("NACK metrics body did not contain controller_runtime_reconcile_total: %.200s", body)
	}
	requireRateLimitRequestMetric(t, ctx)
	leafPod := firstRunningPodName(t, cpc2GatewayNS, bridgePodSelector)
	if body := podProxyGET(t, ctx, cpc2GatewayNS, leafPod, 9464, "metrics"); !bytes.Contains(body, []byte("http_server_request_duration_seconds")) {
		t.Fatalf("bridge leaf metrics body did not contain OpenTelemetry HTTP metrics: %.200s", body)
	}
	gatewayPod := firstRunningPodName(t, cscGatewayNS, cscGatewaySelector)
	if body := podProxyGET(t, ctx, cscGatewayNS, gatewayPod, 15020, "metrics"); !bytes.Contains(body, []byte("agentgateway_requests_total")) {
		t.Fatalf("agentgateway dataplane metrics body did not contain request metrics: %.200s", body)
	}

	requireGatewayPathDoesNotExposeToken(t, ctx, "/metrics", "# HELP")
}

func TestPrometheusMonitorResourcesLive(t *testing.T) {
	runner.ParallelReadOnly(t)

	serviceMonitor := schema.GroupVersionResource{Group: "monitoring.coreos.com", Version: "v1", Resource: "servicemonitors"}
	podMonitor := schema.GroupVersionResource{Group: "monitoring.coreos.com", Version: "v1", Resource: "podmonitors"}
	for _, tc := range []struct {
		name          string
		ns            string
		gvr           schema.GroupVersionResource
		endpointField string
		path          string
		endpointCount int
	}{
		{name: cscGatewayName, ns: cscGatewayNS, gvr: podMonitor, endpointField: "podMetricsEndpoints", path: "/metrics", endpointCount: 1},
		{name: cscGatewayName + "-controller", ns: cscGatewayNS, gvr: serviceMonitor, endpointField: "endpoints", path: "/metrics", endpointCount: 1},
		{name: cscGatewayName + "-ratelimit", ns: cscGatewayNS, gvr: serviceMonitor, endpointField: "endpoints", path: "/metrics", endpointCount: 1},
		{name: cscGatewayName + "-bridge", ns: cscGatewayNS, gvr: serviceMonitor, endpointField: "endpoints", path: "/metrics", endpointCount: 1},
		{name: cscGatewayName + "-valkey", ns: cscGatewayNS, gvr: serviceMonitor, endpointField: "endpoints", endpointCount: 1},
		{name: cpc2GatewayNS + "-bridge", ns: cpc2GatewayNS, gvr: podMonitor, endpointField: "podMetricsEndpoints", path: "/metrics", endpointCount: 1},
		{name: "auth-callout", ns: cscEventBusNS, gvr: serviceMonitor, endpointField: "endpoints", path: "/metrics", endpointCount: 1},
		{name: "nack", ns: cscEventBusNS, gvr: podMonitor, endpointField: "podMetricsEndpoints", path: "/metrics", endpointCount: 1},
		{name: "nats-event-bus-csc-surveyor", ns: cscEventBusNS, gvr: serviceMonitor, endpointField: "endpoints", path: "/metrics", endpointCount: 1},
	} {
		obj := runner.GetUnstructured(t, tc.gvr, tc.ns, tc.name)
		if got := obj.GetLabels()["app.kubernetes.io/managed-by"]; got != "Helm" {
			t.Errorf("%s %s/%s managed-by = %q, want Helm", tc.gvr.Resource, tc.ns, tc.name, got)
		}
		matchLabels, found, err := unstructured.NestedStringMap(obj.Object, "spec", "selector", "matchLabels")
		if err != nil || !found || len(matchLabels) == 0 {
			t.Fatalf("%s %s/%s selector.matchLabels invalid: found=%t err=%v", tc.gvr.Resource, tc.ns, tc.name, found, err)
		}
		if tc.gvr == podMonitor && tc.name == cscGatewayName {
			if got := matchLabels["gateway.networking.k8s.io/gateway-name"]; got != cscGatewayName {
				t.Errorf("PodMonitor gateway-name = %q, want %q", got, cscGatewayName)
			}
		}
		selector := labels.SelectorFromSet(matchLabels).String()
		if tc.gvr == podMonitor {
			if pods := runner.ListPods(t, tc.ns, selector, "status.phase=Running"); len(pods) == 0 {
				t.Fatalf("%s %s/%s selector %q matched no Pods", tc.gvr.Resource, tc.ns, tc.name, selector)
			}
		}
		endpoints, found, err := unstructured.NestedSlice(obj.Object, "spec", tc.endpointField)
		if err != nil || !found || len(endpoints) != tc.endpointCount {
			t.Fatalf("%s %s/%s %s invalid: found=%t count=%d err=%v", tc.gvr.Resource, tc.ns, tc.name, tc.endpointField, found, len(endpoints), err)
		}
		ports := map[string]struct{}{}
		for _, rawEndpoint := range endpoints {
			endpoint, ok := rawEndpoint.(map[string]any)
			if !ok {
				t.Fatalf("%s %s/%s endpoint has type %T", tc.gvr.Resource, tc.ns, tc.name, rawEndpoint)
			}
			for field, want := range map[string]string{"interval": "30s", "scrapeTimeout": "10s"} {
				if got := endpoint[field]; got != want {
					t.Errorf("%s %s/%s endpoint %s = %v, want %s", tc.gvr.Resource, tc.ns, tc.name, field, got, want)
				}
			}
			if got := endpoint["path"]; tc.path != "" && got != tc.path {
				t.Errorf("%s %s/%s endpoint path = %v, want %s", tc.gvr.Resource, tc.ns, tc.name, got, tc.path)
			} else if tc.path == "" && got != nil && got != "/metrics" {
				t.Errorf("%s %s/%s endpoint path = %v, want default /metrics", tc.gvr.Resource, tc.ns, tc.name, got)
			}
			if tc.name == "nack" {
				relabelings, found, err := unstructured.NestedSlice(endpoint, "relabelings")
				if err != nil || !found || len(relabelings) != 1 {
					t.Fatalf("PodMonitor %s/%s relabelings invalid: found=%t count=%d err=%v", tc.ns, tc.name, found, len(relabelings), err)
				}
				relabeling, ok := relabelings[0].(map[string]any)
				sourceLabels, sourceLabelsFound, sourceLabelsErr := unstructured.NestedStringSlice(relabeling, "sourceLabels")
				if !ok || sourceLabelsErr != nil || !sourceLabelsFound || len(sourceLabels) != 1 || sourceLabels[0] != "__meta_kubernetes_pod_ip" ||
					relabeling["targetLabel"] != "__address__" || relabeling["replacement"] != "$1:8080" {
					t.Errorf("PodMonitor %s/%s relabeling = %v, want __address__ replacement $1:8080", tc.ns, tc.name, relabelings[0])
				}
				continue
			}
			if tc.gvr == podMonitor && tc.name == cscGatewayName {
				relabelings, found, err := unstructured.NestedSlice(endpoint, "relabelings")
				if err != nil || !found || len(relabelings) != 1 {
					t.Fatalf("PodMonitor %s/%s relabelings invalid: found=%t count=%d err=%v", tc.ns, tc.name, found, len(relabelings), err)
				}
				relabeling, ok := relabelings[0].(map[string]any)
				sourceLabels, sourceLabelsFound, sourceLabelsErr := unstructured.NestedStringSlice(relabeling, "sourceLabels")
				if !ok || sourceLabelsErr != nil || !sourceLabelsFound || len(sourceLabels) != 1 || sourceLabels[0] != "__meta_kubernetes_pod_label_gateway_networking_k8s_io_gateway_name" ||
					relabeling["targetLabel"] != "gateway_networking_k8s_io_gateway_name" {
					t.Errorf("PodMonitor %s/%s dashboard relabeling = %v", tc.ns, tc.name, relabelings[0])
				}
			}
			port, ok := endpoint["port"].(string)
			if !ok || port == "" {
				t.Errorf("%s %s/%s endpoint port = %v, want named port", tc.gvr.Resource, tc.ns, tc.name, endpoint["port"])
				continue
			}
			ports[port] = struct{}{}
		}
		for port := range ports {
			if tc.gvr == serviceMonitor {
				services, err := runner.K8s(t).CoreV1().Services(tc.ns).List(context.Background(), metav1.ListOptions{LabelSelector: selector})
				if err != nil || len(services.Items) == 0 {
					t.Fatalf("%s %s/%s selector %q matched no Services: %v", tc.gvr.Resource, tc.ns, tc.name, selector, err)
				}
				matchedPort := false
				for _, service := range services.Items {
					for _, servicePort := range service.Spec.Ports {
						matchedPort = matchedPort || servicePort.Name == port
					}
				}
				if !matchedPort {
					t.Errorf("%s %s/%s named port %q exists on no selected Service", tc.gvr.Resource, tc.ns, tc.name, port)
				}
			} else {
				pods := runner.ListPods(t, tc.ns, selector, "")
				if len(pods) == 0 {
					t.Fatalf("%s %s/%s selector %q matched no Pods", tc.gvr.Resource, tc.ns, tc.name, selector)
				}
				matchedPort := false
				for _, pod := range pods {
					for _, container := range pod.Spec.Containers {
						for _, containerPort := range container.Ports {
							matchedPort = matchedPort || containerPort.Name == port
						}
					}
				}
				if !matchedPort {
					t.Errorf("%s %s/%s named port %q exists on no selected Pod", tc.gvr.Resource, tc.ns, tc.name, port)
				}
			}
		}
	}

	if _, err := runner.Dyn(t).Resource(serviceMonitor).Namespace(cpc1GatewayNS).Get(
		context.Background(), cpc1GatewayNS+"-bridge", metav1.GetOptions{},
	); !apierrors.IsNotFound(err) {
		t.Fatalf("leaf bridge ServiceMonitor lookup error = %v, want not found", err)
	}
}

func TestGrafanaDashboardSourcesLive(t *testing.T) {
	runner.ParallelReadOnly(t)

	for _, tc := range []struct {
		name       string
		ns         string
		key        string
		title      string
		panelCount int
	}{
		{name: "nats-surveyor-dashboard", ns: cscEventBusNS, key: "nats-surveyor.json", title: "NATS Surveyor", panelCount: 35},
		{name: cscGatewayName + "-controller-dashboard", ns: cscGatewayNS, key: "agentgateway.json", title: "Agentgateway", panelCount: 13},
	} {
		configMap, err := runner.K8s(t).CoreV1().ConfigMaps(tc.ns).Get(context.Background(), tc.name, metav1.GetOptions{})
		if err != nil {
			t.Fatalf("get ConfigMap %s/%s: %v", tc.ns, tc.name, err)
		}
		if got := configMap.Labels["grafana_dashboard"]; got != "1" {
			t.Errorf("ConfigMap %s/%s grafana_dashboard = %q, want 1", tc.ns, tc.name, got)
		}
		var dashboard struct {
			Title  string            `json:"title"`
			Panels []json.RawMessage `json:"panels"`
		}
		if err := json.Unmarshal([]byte(configMap.Data[tc.key]), &dashboard); err != nil {
			t.Fatalf("parse ConfigMap %s/%s key %s: %v", tc.ns, tc.name, tc.key, err)
		}
		if dashboard.Title != tc.title || len(dashboard.Panels) != tc.panelCount {
			t.Errorf("ConfigMap %s/%s dashboard = %q with %d panels, want %q with %d", tc.ns, tc.name, dashboard.Title, len(dashboard.Panels), tc.title, tc.panelCount)
		}
	}
}

func TestOpenTelemetryOperatorInjectionContract(t *testing.T) {
	runner.ParallelReadOnly(t)

	for _, tc := range []struct {
		name        string
		ns          string
		selector    string
		application string
		serviceName string
		samplerEnv  string
		endpoint    string
	}{
		{name: "agentgateway", ns: cscGatewayNS, selector: cscGatewaySelector, application: "agentgateway", serviceName: "dsx-agent-gateway-agentgateway", endpoint: "http://127.0.0.1:4318"},
		{name: "rate limit", ns: cscGatewayNS, selector: "app.kubernetes.io/instance=" + cscGatewayName + ",app.kubernetes.io/component=ratelimit", application: "ratelimit", serviceName: "dsx-agent-gateway-ratelimit", samplerEnv: "TRACING_SAMPLING_RATE", endpoint: "http://127.0.0.1:4318"},
		{name: "bridge hub", ns: cscGatewayNS, selector: bridgePodSelector, application: "dsx-agentgateway-bridge", serviceName: "dsx-agentgateway-bridge-hub", samplerEnv: "OTEL_TRACES_SAMPLER_ARG", endpoint: "http://127.0.0.1:4318"},
		{name: "bridge leaf", ns: cpc1GatewayNS, selector: bridgePodSelector, application: "dsx-agentgateway-bridge", serviceName: "dsx-agentgateway-bridge-leaf", samplerEnv: "OTEL_TRACES_SAMPLER_ARG", endpoint: "http://127.0.0.1:4318"},
		{name: "auth callout", ns: cscEventBusNS, selector: "app.kubernetes.io/name=auth-callout", application: "auth-callout", serviceName: "auth-callout", endpoint: "http://127.0.0.1:4318"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pods := runner.ListPods(t, tc.ns, tc.selector, "status.phase=Running")
			if len(pods) == 0 {
				t.Fatalf("no running Pods for %s", tc.selector)
			}
			pod := pods[0]
			wantAnnotations := map[string]string{
				"instrumentation.opentelemetry.io/inject-sdk": "dsx-obs/default-instrumentation",
				"sidecar.opentelemetry.io/inject":             "dsx-obs/default-sidecar",
				"resource.opentelemetry.io/service.name":      tc.serviceName,
			}
			for name, want := range wantAnnotations {
				if got := pod.Annotations[name]; got != want {
					t.Errorf("Pod %s annotation %s = %q, want %q", pod.Name, name, got, want)
				}
			}
			if _, exists := pod.Annotations["prometheus.io/scrape"]; exists {
				t.Errorf("Pod %s has legacy prometheus.io/scrape annotation", pod.Name)
			}

			var application, sidecar *corev1.Container
			for i := range pod.Spec.Containers {
				container := &pod.Spec.Containers[i]
				if container.Name == tc.application {
					application = container
				}
			}
			// Kubernetes represents a native sidecar as an always-restarted init container.
			for i := range pod.Spec.InitContainers {
				container := &pod.Spec.InitContainers[i]
				if container.Name == "otc-container" && container.RestartPolicy != nil && *container.RestartPolicy == corev1.ContainerRestartPolicyAlways {
					sidecar = container
				}
			}
			if application == nil || sidecar == nil {
				t.Fatalf("Pod %s containers missing application=%t sidecar=%t", pod.Name, application != nil, sidecar != nil)
			}
			env := make(map[string]string, len(application.Env))
			for _, variable := range application.Env {
				env[variable.Name] = variable.Value
			}
			if got := env["OTEL_EXPORTER_OTLP_ENDPOINT"]; got != tc.endpoint {
				t.Errorf("Pod %s OTEL_EXPORTER_OTLP_ENDPOINT = %q, want %q", pod.Name, got, tc.endpoint)
			}
			if value, exists := env["OTEL_EXPORTER_OTLP_PROTOCOL"]; exists {
				t.Errorf("Pod %s OTEL_EXPORTER_OTLP_PROTOCOL = %q, want SDK default", pod.Name, value)
			}
			if tc.application == "ratelimit" {
				if value, exists := env["TRACING_EXPORTER_PROTOCOL"]; exists {
					t.Errorf("Pod %s TRACING_EXPORTER_PROTOCOL = %q, want application default", pod.Name, value)
				}
			}
			if got := env["OTEL_SERVICE_NAME"]; got != tc.serviceName {
				t.Errorf("Pod %s OTEL_SERVICE_NAME = %q, want %q", pod.Name, got, tc.serviceName)
			}
			if tc.samplerEnv != "" && env[tc.samplerEnv] != "1.0" {
				t.Errorf("Pod %s %s = %q, want unified chart ratio", pod.Name, tc.samplerEnv, env[tc.samplerEnv])
			}
		})
	}
}

func TestObservabilitySignalsCanBeDisabledIndependently(t *testing.T) {
	runner.ParallelReadOnly(t)

	ctx := context.Background()
	monitorResources := []schema.GroupVersionResource{
		{Group: "monitoring.coreos.com", Version: "v1", Resource: "servicemonitors"},
		{Group: "monitoring.coreos.com", Version: "v1", Resource: "podmonitors"},
	}
	backend := schema.GroupVersionResource{Group: "agentgateway.dev", Version: "v1alpha1", Resource: "agentgatewaybackends"}
	for _, tc := range []struct {
		ns      string
		metrics bool
		tracing bool
	}{
		{ns: cpc1GatewayNS, metrics: false, tracing: true},
		{ns: cpc2GatewayNS, metrics: true, tracing: false},
	} {
		t.Run(tc.ns, func(t *testing.T) {
			instanceSelector := "app.kubernetes.io/instance=" + tc.ns
			monitorCount := 0
			for _, gvr := range monitorResources {
				objects, err := runner.Dyn(t).Resource(gvr).Namespace(tc.ns).List(ctx, metav1.ListOptions{LabelSelector: instanceSelector})
				if err != nil {
					t.Fatalf("list %s in %s: %v", gvr.Resource, tc.ns, err)
				}
				monitorCount += len(objects.Items)
			}
			if (monitorCount > 0) != tc.metrics {
				t.Errorf("monitor count = %d, metrics enabled=%t", monitorCount, tc.metrics)
			}

			backendObject, backendErr := runner.Dyn(t).Resource(backend).Namespace(tc.ns).Get(ctx, tc.ns+"-otel", metav1.GetOptions{})
			if tc.tracing && backendErr != nil {
				t.Fatalf("get enabled tracing backend: %v", backendErr)
			}
			if !tc.tracing && !apierrors.IsNotFound(backendErr) {
				t.Fatalf("disabled tracing backend lookup error = %v, want not found", backendErr)
			}
			policy := runner.GetUnstructured(t, runner.AgentgatewayPolicyResource, tc.ns, tc.ns+"-authz")
			_, tracingFound, err := unstructured.NestedFieldNoCopy(policy.Object, "spec", "frontend", "tracing")
			if err != nil || tracingFound != tc.tracing {
				t.Fatalf("policy tracing found=%t err=%v, want %t", tracingFound, err, tc.tracing)
			}
			if tc.tracing {
				port, found, err := unstructured.NestedInt64(backendObject.Object, "spec", "static", "port")
				if err != nil || !found || port != 4318 {
					t.Errorf("agentgateway tracing backend port = %d found=%t err=%v, want 4318", port, found, err)
				}
				protocol, found, err := unstructured.NestedString(policy.Object, "spec", "frontend", "tracing", "protocol")
				if err != nil || !found || protocol != "HTTP" {
					t.Errorf("agentgateway tracing protocol = %q found=%t err=%v, want HTTP", protocol, found, err)
				}
			}

			for _, app := range []struct {
				selector string
				name     string
			}{
				{selector: "gateway.networking.k8s.io/gateway-name=" + tc.ns, name: "agentgateway"},
				{selector: instanceSelector + ",app.kubernetes.io/component=ratelimit", name: "ratelimit"},
				{selector: instanceSelector + ",app.kubernetes.io/component=dsx-agentgateway-bridge", name: "dsx-agentgateway-bridge"},
			} {
				pods := runner.ListPods(t, tc.ns, app.selector, "status.phase=Running")
				if len(pods) == 0 {
					t.Fatalf("no running Pods for %s", app.selector)
				}
				pod := pods[0]
				_, annotationFound := pod.Annotations["sidecar.opentelemetry.io/inject"]
				if annotationFound != tc.tracing {
					t.Errorf("Pod %s tracing annotation found=%t, want %t", pod.Name, annotationFound, tc.tracing)
				}
				sidecarFound := false
				for _, container := range pod.Spec.InitContainers {
					sidecarFound = sidecarFound || container.Name == "otc-container"
				}
				if sidecarFound != tc.tracing {
					t.Errorf("Pod %s collector sidecar found=%t, want %t", pod.Name, sidecarFound, tc.tracing)
				}

				applicationFound := false
				for _, container := range pod.Spec.Containers {
					if container.Name != app.name {
						continue
					}
					applicationFound = true
					env := make(map[string]string, len(container.Env))
					for _, variable := range container.Env {
						env[variable.Name] = variable.Value
					}
					if !tc.tracing {
						for _, name := range []string{"OTEL_EXPORTER_OTLP_ENDPOINT", "OTEL_EXPORTER_OTLP_PROTOCOL", "TRACING_EXPORTER_PROTOCOL"} {
							if value, exists := env[name]; exists {
								t.Errorf("Pod %s %s = %q with tracing disabled", pod.Name, name, value)
							}
						}
					}
					if app.name == "ratelimit" {
						if env["USE_PROMETHEUS"] != strconv.FormatBool(tc.metrics) || env["TRACING_ENABLED"] != strconv.FormatBool(tc.tracing) {
							t.Errorf("Pod %s signal env = metrics:%q tracing:%q", pod.Name, env["USE_PROMETHEUS"], env["TRACING_ENABLED"])
						}
						hasMetricsPort := false
						for _, port := range container.Ports {
							hasMetricsPort = hasMetricsPort || port.Name == "metrics"
						}
						if hasMetricsPort != tc.metrics {
							t.Errorf("Pod %s metrics port found=%t, want %t", pod.Name, hasMetricsPort, tc.metrics)
						}
					}
					if app.name == "dsx-agentgateway-bridge" {
						hasMetricsPort := false
						for _, port := range container.Ports {
							hasMetricsPort = hasMetricsPort || port.Name == "metrics"
						}
						if hasMetricsPort != tc.metrics {
							t.Errorf("Pod %s metrics port found=%t, want %t", pod.Name, hasMetricsPort, tc.metrics)
						}
						if (env["OTEL_METRICS_EXPORTER"] == "none") == tc.metrics {
							t.Errorf("Pod %s metrics exporter = %q, metrics enabled=%t", pod.Name, env["OTEL_METRICS_EXPORTER"], tc.metrics)
						}
						if (env["OTEL_TRACES_EXPORTER"] == "none") == tc.tracing {
							t.Errorf("Pod %s traces exporter = %q, tracing enabled=%t", pod.Name, env["OTEL_TRACES_EXPORTER"], tc.tracing)
						}
					}
				}
				if !applicationFound {
					t.Errorf("Pod %s has no %s container", pod.Name, app.name)
				}
			}

			rateLimitService, err := runner.K8s(t).CoreV1().Services(tc.ns).Get(ctx, tc.ns+"-ratelimit", metav1.GetOptions{})
			if err != nil {
				t.Fatalf("get rate-limit Service: %v", err)
			}
			hasMetricsPort := false
			for _, port := range rateLimitService.Spec.Ports {
				hasMetricsPort = hasMetricsPort || port.Name == "metrics"
			}
			if hasMetricsPort != tc.metrics {
				t.Errorf("rate-limit Service metrics port found=%t, want %t", hasMetricsPort, tc.metrics)
			}

			_, valkeyMetricsErr := runner.K8s(t).CoreV1().Services(tc.ns).Get(ctx, tc.ns+"-valkey-metrics", metav1.GetOptions{})
			if tc.metrics && valkeyMetricsErr != nil {
				t.Errorf("get enabled Valkey metrics Service: %v", valkeyMetricsErr)
			}
			if !tc.metrics && !apierrors.IsNotFound(valkeyMetricsErr) {
				t.Errorf("Valkey metrics Service error=%v, metrics enabled=%t", valkeyMetricsErr, tc.metrics)
			}
			valkeyPods := runner.ListPods(t, tc.ns, instanceSelector+",app.kubernetes.io/name=valkey", "status.phase=Running")
			if len(valkeyPods) == 0 {
				t.Fatal("no running Valkey Pods")
			}
			exporterFound := false
			for _, container := range valkeyPods[0].Spec.Containers {
				if container.Name != "metrics" {
					continue
				}
				exporterFound = true
				envNames := map[string]bool{}
				for _, variable := range container.Env {
					envNames[variable.Name] = true
				}
				if envNames["REDIS_PASSWORD_FILE"] || envNames["REDIS_PASSWORD"] {
					t.Errorf("Valkey exporter credential env = %v, want none", envNames)
				}
			}
			if exporterFound != tc.metrics {
				t.Errorf("Valkey exporter sidecar found=%t, metrics enabled=%t", exporterFound, tc.metrics)
			}
			if _, err := runner.K8s(t).AppsV1().Deployments(tc.ns).Get(ctx, tc.ns+"-valkey-metrics", metav1.GetOptions{}); !apierrors.IsNotFound(err) {
				t.Errorf("standalone Valkey exporter lookup error=%v, want not found", err)
			}

			controllerService, err := runner.K8s(t).CoreV1().Services(tc.ns).Get(ctx, tc.ns+"-controller", metav1.GetOptions{})
			if err != nil {
				t.Fatalf("get controller Service: %v", err)
			}
			controllerMetricsPort := false
			for _, port := range controllerService.Spec.Ports {
				controllerMetricsPort = controllerMetricsPort || port.Name == "metrics"
			}
			if controllerMetricsPort != tc.metrics {
				t.Errorf("controller Service metrics port found=%t, want %t", controllerMetricsPort, tc.metrics)
			}
			controllerPods := runner.ListPods(t, tc.ns, instanceSelector+",agentgateway=agentgateway", "status.phase=Running")
			if len(controllerPods) == 0 || controllerPods[0].Annotations["prometheus.io/scrape"] != "false" {
				t.Errorf("controller legacy scrape annotation was not disabled")
			}

			rateLimitPolicy, err := runner.K8s(t).NetworkingV1().NetworkPolicies(tc.ns).Get(ctx, tc.ns+"-ratelimit-ingress", metav1.GetOptions{})
			if err != nil {
				t.Fatalf("get rate-limit NetworkPolicy: %v", err)
			}
			rateLimitMetricsIngress := false
			for _, ingress := range rateLimitPolicy.Spec.Ingress {
				for _, port := range ingress.Ports {
					rateLimitMetricsIngress = rateLimitMetricsIngress || port.Port != nil && port.Port.IntValue() == 9090
				}
			}
			if rateLimitMetricsIngress != tc.metrics {
				t.Errorf("rate-limit metrics ingress found=%t, want %t", rateLimitMetricsIngress, tc.metrics)
			}

			valkeyPolicy, err := runner.K8s(t).NetworkingV1().NetworkPolicies(tc.ns).Get(ctx, tc.ns+"-valkey-ingress", metav1.GetOptions{})
			if err != nil {
				t.Fatalf("get Valkey NetworkPolicy: %v", err)
			}
			peerIngress := false
			metricsIngress := false
			for _, ingress := range valkeyPolicy.Spec.Ingress {
				for _, port := range ingress.Ports {
					metricsIngress = metricsIngress || port.Port != nil && port.Port.IntValue() == 9121
					if port.Port != nil && port.Port.IntValue() == 6379 {
						for _, from := range ingress.From {
							peerIngress = peerIngress || from.PodSelector != nil && from.PodSelector.MatchLabels["app.kubernetes.io/name"] == "valkey"
						}
					}
				}
			}
			if !peerIngress {
				t.Error("Valkey peer ingress is missing")
			}
			if metricsIngress != tc.metrics {
				t.Errorf("Valkey metrics ingress found=%t, want %t", metricsIngress, tc.metrics)
			}
		})
	}
}

func TestRuntimeJSONLogContract(t *testing.T) {
	runner.ParallelReadOnly(t)

	cases := []struct {
		name        string
		ns          string
		selector    string
		container   string
		allowedText []string
	}{
		{name: "bridge hub", ns: cscGatewayNS, selector: bridgePodSelector, container: "dsx-agentgateway-bridge"},
		{name: "bridge leaf", ns: cpc1GatewayNS, selector: bridgePodSelector, container: "dsx-agentgateway-bridge"},
		{
			name: "rate limit", ns: cscGatewayNS, selector: "app.kubernetes.io/component=ratelimit", container: "ratelimit",
			allowedText: []string{
				`msg="Stats initialized for Prometheus"`,
				`msg="Starting prometheus sink on `,
				`msg="Stats flush interval: `,
				`msg="TracerProvider initialized with following parameters:`,
			},
		},
		{name: "agentgateway dataplane", ns: cscGatewayNS, selector: cscGatewaySelector, container: "agentgateway"},
		{name: "agentgateway controller", ns: cscGatewayNS, selector: "agentgateway=agentgateway,app.kubernetes.io/instance=" + cscGatewayName, container: "controller"},
		{
			name: "Valkey exporter", ns: cscGatewayNS, selector: "app.kubernetes.io/name=valkey,app.kubernetes.io/instance=" + cscGatewayName, container: "metrics",
			allowedText: []string{`msg="Redis Metrics Exporter v`},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			requireJSONLogLines(t, tc.ns, tc.selector, tc.container, tc.allowedText)
		})
	}
}

func TestOpenTelemetryTraceExportThroughGateway(t *testing.T) {
	runner.ParallelReadOnly(t)

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	const traceID = "4bf92f3577b34da6a3ce929d0e0e4736"
	collectorPod := firstRunningPodName(t, "dsx-obs", "app.kubernetes.io/instance=otel-collector")
	since := metav1.NewTime(time.Now())
	stream, err := runner.K8s(t).CoreV1().Pods("dsx-obs").GetLogs(collectorPod, &corev1.PodLogOptions{
		Container: "opentelemetry-collector",
		Follow:    true,
		SinceTime: &since,
	}).Stream(ctx)
	if err != nil {
		t.Fatalf("follow OpenTelemetry Collector logs: %v", err)
	}
	defer stream.Close()

	found := make(chan struct{}, 1)
	scanErr := make(chan error, 1)
	go func() {
		wantServices := map[string]bool{
			"dsx-agent-gateway-agentgateway": false,
			"dsx-agentgateway-bridge-hub":    false,
			"dsx-agent-gateway-ratelimit":    false,
		}
		currentService := ""
		scanner := bufio.NewScanner(stream)
		scanner.Buffer(make([]byte, 64*1024), 1024*1024)
		// The debug exporter prints resource attributes and trace IDs on separate lines.
		for scanner.Scan() {
			line := strings.ToLower(scanner.Text())
			if strings.Contains(line, "resourcespans #") {
				currentService = ""
			}
			for service := range wantServices {
				if strings.Contains(line, "service.name: str("+service+")") {
					currentService = service
				}
			}
			traceSeen := strings.Contains(line, traceID)
			if currentService == "dsx-agent-gateway-ratelimit" {
				// Agentgateway does not propagate request context to the rate-limit RPC.
				traceSeen = strings.Contains(line, "trace id")
			}
			if traceSeen && currentService != "" {
				wantServices[currentService] = true
				allSeen := true
				for _, seen := range wantServices {
					allSeen = allSeen && seen
				}
				if !allSeen {
					continue
				}
				found <- struct{}{}
				return
			}
		}
		if err := scanner.Err(); err != nil {
			scanErr <- err
			return
		}
		scanErr <- io.EOF
	}()

	s := runner.NewSessionWithHeaders(t, tenantAUnlimited, map[string]string{
		"traceparent": "00-" + traceID + "-00f067aa0ba902b7-01",
	})
	t.Cleanup(s.Close)
	if _, err := s.ListToolNames(ctx); err != nil {
		t.Fatalf("tools/list with traceparent: %v", err)
	}

	select {
	case <-found:
	case err := <-scanErr:
		t.Fatalf("OpenTelemetry Collector log stream ended before trace %s: %v", traceID, err)
	case <-ctx.Done():
		t.Fatalf("OpenTelemetry Collector did not export trace %s: %v", traceID, ctx.Err())
	}
}

func postSessionMCP(t *testing.T, ctx context.Context, bearer, sid string, body []byte) runner.RawResponse {
	t.Helper()
	return postSessionMCPTimeout(t, ctx, bearer, sid, body, 10*time.Second)
}

func postSessionMCPTimeout(t *testing.T, ctx context.Context, bearer, sid string, body []byte, timeout time.Duration) runner.RawResponse {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, runner.GatewayURL(t), bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /mcp request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+bearer)
	req.Header.Set("Mcp-Session-Id", sid)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	httpc := &http.Client{Timeout: timeout, Transport: &http.Transport{DisableKeepAlives: true}}
	defer httpc.CloseIdleConnections()
	resp, err := httpc.Do(req)
	if err != nil {
		t.Fatalf("POST /mcp: %v", err)
	}
	defer resp.Body.Close()
	_, b := readAll(t, resp)
	return runner.RawResponse{Status: resp.StatusCode, Body: b, Header: resp.Header.Clone()}
}

func requireMCPListSuccess(t *testing.T, resp runner.RawResponse) {
	t.Helper()
	requireHTTPSuccess(t, resp)
	body := lastSSEData(resp.Body)
	if !bytes.Contains(body, []byte(`"tools"`)) {
		t.Fatalf("tools/list response did not contain a tools result: %s", body)
	}
}

func requireHTTPSuccess(t *testing.T, resp runner.RawResponse) {
	t.Helper()
	if resp.Status != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", resp.Status, resp.Body)
	}
}

func requireNoMCPListLeak(t *testing.T, resp runner.RawResponse, leaks []string) {
	t.Helper()
	if resp.Status == http.StatusOK && bytes.Contains(lastSSEData(resp.Body), []byte(`"tools"`)) {
		for _, leak := range leaks {
			if bytes.Contains(resp.Body, []byte(leak)) {
				t.Fatalf("rejected session leaked catalog name %q (status %d body: %s)", leak, resp.Status, resp.Body)
			}
		}
		t.Fatalf("unknown session returned a tools result (status %d body: %s)", resp.Status, resp.Body)
	}
	for _, leak := range leaks {
		if bytes.Contains(resp.Body, []byte(leak)) {
			t.Fatalf("rejected session leaked catalog name %q (status %d body: %s)", leak, resp.Status, resp.Body)
		}
	}
}

func requireStructuredErrorSignal(t *testing.T, resp runner.RawResponse) {
	t.Helper()
	if bytes.Contains(bytes.ToLower(resp.Body), []byte("<html")) {
		t.Fatalf("HTML in malformed MCP response: %s", resp.Body)
	}
	lower := strings.ToLower(string(resp.Body))
	for _, leak := range []string{
		".svc.cluster.local",
		"csc-mcp-backends",
		"csc-dsx-agentgateway",
		"agent-gateway-fixtures",
	} {
		if strings.Contains(lower, leak) {
			t.Fatalf("malformed MCP response leaked internal identifier %q (status %d body: %s)", leak, resp.Status, resp.Body)
		}
	}
	if _, msg, ok := runner.JSONRPCError(resp.Body); ok && msg != "" {
		return
	}
	for _, phrase := range []string{"invalid", "bad request", "jsonrpc", "missing", "unsupported", "error", "deserialize", "eof"} {
		if strings.Contains(lower, phrase) {
			return
		}
	}
	t.Fatalf("malformed MCP response had no structured error signal (status %d body: %s)", resp.Status, resp.Body)
}

func serviceProxyGET(t *testing.T, ctx context.Context, ns, service, port, path string) []byte {
	return serviceProxyGETWithTarget(t, ctx, ns, service, port, path, "")
}

func serviceProxyGETWithTarget(t *testing.T, ctx context.Context, ns, service, port, path, target string) []byte {
	t.Helper()
	name := fmt.Sprintf("http:%s:%s", service, port)
	request := runner.K8s(t).CoreV1().RESTClient().Get().
		Namespace(ns).
		Resource("services").
		Name(name).
		SubResource("proxy").
		Suffix(path)
	if target != "" {
		request.Param("target", target)
	}
	body, err := request.DoRaw(ctx)
	if err != nil {
		t.Fatalf("GET service proxy %s/%s:%s/%s: %v", ns, service, port, path, err)
	}
	return body
}

func podProxyGET(t *testing.T, ctx context.Context, ns, pod string, port int, path string) []byte {
	t.Helper()
	body, err := podProxyGETRaw(t, ctx, ns, pod, port, path)
	if err != nil {
		t.Fatalf("GET pod proxy %s/%s:%d/%s: %v", ns, pod, port, path, err)
	}
	return body
}

func podProxyGETRaw(t *testing.T, ctx context.Context, ns, pod string, port int, path string) ([]byte, error) {
	t.Helper()
	name := pod + ":" + strconv.Itoa(port)
	return runner.K8s(t).CoreV1().RESTClient().Get().
		Namespace(ns).
		Resource("pods").
		Name(name).
		SubResource("proxy").
		Suffix(path).
		DoRaw(ctx)
}

func requireRateLimitRequestMetric(t *testing.T, ctx context.Context) {
	t.Helper()
	pods := runner.ListPods(t, cscGatewayNS, "app.kubernetes.io/instance="+cscGatewayName+",app.kubernetes.io/component=ratelimit", "status.phase=Running")
	if len(pods) == 0 {
		t.Fatal("no Running rate-limit Pods")
	}
	var lastBody []byte
	var lastErr error
	if runner.WaitForContext(ctx, 500*time.Millisecond, func() bool {
		for _, pod := range pods {
			body, err := podProxyGETRaw(t, ctx, cscGatewayNS, pod.Name, 9090, "metrics")
			if err != nil {
				lastErr = err
				continue
			}
			lastBody = body
			if bytes.Contains(body, []byte("ratelimit_service_total_requests")) {
				return true
			}
		}
		return false
	}) {
		return
	}
	t.Fatalf("rate-limit request metrics were not exported before timeout: last error: %v, last body: %.200s", lastErr, lastBody)
}

func requirePodProxyGETFails(t *testing.T, ctx context.Context, ns, pod string, port int, path string) {
	t.Helper()
	name := pod + ":" + strconv.Itoa(port)
	body, err := runner.K8s(t).CoreV1().RESTClient().Get().
		Namespace(ns).
		Resource("pods").
		Name(name).
		SubResource("proxy").
		Suffix(path).
		DoRaw(ctx)
	if err != nil {
		return
	}
	t.Fatalf("GET pod proxy %s/%s:%d/%s succeeded, want readiness failure (body: %s)", ns, pod, port, path, body)
}

func firstRunningPodName(t *testing.T, ns, labelSelector string) string {
	t.Helper()
	pods, err := runner.K8s(t).CoreV1().Pods(ns).List(context.Background(), metav1.ListOptions{
		LabelSelector: labelSelector,
		FieldSelector: "status.phase=Running",
	})
	if err != nil {
		t.Fatalf("list Pods %s %s: %v", ns, labelSelector, err)
	}
	if len(pods.Items) == 0 {
		t.Fatalf("no Running Pods in %s with %s", ns, labelSelector)
	}
	return pods.Items[0].Name
}

func requireJSONLogLines(t *testing.T, ns, labelSelector, container string, allowedText []string) {
	t.Helper()
	pods := runner.ListPods(t, ns, labelSelector, "status.phase=Running")
	if len(pods) == 0 {
		t.Fatalf("no Running Pods in %s with %s", ns, labelSelector)
	}
	checked := 0
	for _, pod := range pods {
		tail := int64(50)
		logs, err := runner.K8s(t).CoreV1().Pods(ns).GetLogs(pod.Name, &corev1.PodLogOptions{Container: container, TailLines: &tail}).DoRaw(context.Background())
		if apierrors.IsNotFound(err) {
			continue
		}
		if err != nil {
			t.Fatalf("logs %s/%s: %v", ns, pod.Name, err)
		}
		checked++
		jsonLines := 0
		for _, line := range strings.Split(string(logs), "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			var record map[string]any
			if err := json.Unmarshal([]byte(line), &record); err != nil {
				if slices.ContainsFunc(allowedText, func(token string) bool { return strings.Contains(line, token) }) {
					continue
				}
				t.Fatalf("log line from %s/%s container %s is not JSON or a known upstream startup line: %q: %v", ns, pod.Name, container, line, err)
			}
			if len(record) == 0 {
				t.Fatalf("log line from %s/%s container %s is an empty JSON object", ns, pod.Name, container)
			}
			jsonLines++
		}
		if jsonLines == 0 {
			t.Fatalf("pod %s/%s container %s had no JSON log lines", ns, pod.Name, container)
		}
	}
	if checked == 0 {
		t.Fatalf("all Running Pods in %s with %s disappeared before logs could be read", ns, labelSelector)
	}
}

func getGatewayPath(t *testing.T, ctx context.Context, path string) (int, []byte) {
	t.Helper()
	gw := runner.GatewayURL(t)
	idx := strings.LastIndex(gw, "/mcp")
	if idx == -1 {
		t.Fatalf("GATEWAY_URL %q does not end in /mcp", gw)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, gw[:idx]+path, nil)
	if err != nil {
		t.Fatalf("new GET %s: %v", path, err)
	}
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer resp.Body.Close()
	_, body := readAll(t, resp)
	return resp.StatusCode, body
}

func requireGatewayPathNotSuccessful(t *testing.T, ctx context.Context, path string) {
	t.Helper()
	status, body := getGatewayPath(t, ctx, path)
	if status >= 200 && status < 300 {
		t.Fatalf("gateway caller path %s returned successful status %d: %.200s", path, status, body)
	}
}

func requireGatewayPathDoesNotExposeToken(t *testing.T, ctx context.Context, path, forbiddenBodyToken string) {
	t.Helper()
	status, body := getGatewayPath(t, ctx, path)
	if status >= 200 && status < 300 && bytes.Contains(body, []byte(forbiddenBodyToken)) {
		t.Fatalf("gateway caller path %s exposed protected body token %q: %.200s", path, forbiddenBodyToken, body)
	}
}
