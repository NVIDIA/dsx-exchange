# DSX Agent Gateway Helm Chart

## What the chart installs

The chart deploys one DSX Agent Gateway that routes `/mcp` requests to selected
in-cluster MCP Services, authenticates callers, and applies tenant authorization
and rate limits. By default it also installs the pinned agentgateway controller,
a two-replica rate-limit service, and Valkey via chart 0.9.4 for
rate-limit counters. The optional bridge connects gateway shards through NATS.

## Prerequisites

- Helm and `kubectl`.
- Gateway API v1.5.1 CRDs and agentgateway v1.3.1 CRDs installed by a cluster
  administrator. This chart does not install those CRDs.
- The DSX observability stack, including Prometheus Operator CRDs and the
  OpenTelemetry Operator resources shown under Operations. Disable the
  corresponding signal in that example when either integration is unavailable.
- A reachable JWKS endpoint for each JWT provider, plus any configured MCP
  upstream Services. Each upstream must expose an MCP port with `appProtocol:
  agentgateway.dev/mcp`.

## Supported deployment model

The DSX Agent Gateway listens on plaintext HTTP port 80 through an internal
`ClusterIP` Service. `NodePort` is the only other supported Service type. An
operator-owned edge can route to this Service. See the
[HTTP-only Envoy Gateway edge example](examples/envoy-edge-route.yaml).

The chart does not configure listener certificates, cert-manager resources, or
`LoadBalancer` Services.

Set `runtimeClassName` to select a RuntimeClass for gateway, rate-limit, and
bridge Pods. An empty value uses the cluster default.

## Authentication and authorization

At least one JWT provider is required. Provider names are arbitrary lowercase
DNS labels. For each provider, configure its issuer, allowed audiences,
complete `jwksUrl`, and `tenantIdExpression`. A token may contain additional
audiences as long as at least one configured audience matches. The JWKS address
may be outside the cluster.

Each provider's CEL expression derives a tenant ID from its verified claims. An
empty tenant ID is rejected. Provider issuers must be unique.

`auth.cel.operatorTenantId` must exactly match a derived tenant ID. That tenant
can use every MCP target. Other tenants can use only the target
names listed in `auth.cel.unprivilegedTenantMCPs`. The selected MCP Service
receives the caller's original bearer token and applies its own authorization.

## MCP upstreams

Use a Service selector to discover ports marked `appProtocol: agentgateway.dev/mcp`.
Static upstreams use an HTTP(S) address. `https://` enables TLS to the upstream.
The map keys are MCP target names, so separate Helm values layers can add or
override upstreams independently. Add either target name to
`auth.cel.unprivilegedTenantMCPs` for non-operator access.

`upstreamRequestTimeout` defaults the deadline for receiving response headers
from every target. Set `upstreams.<name>.requestTimeout` for a slower target.
Streaming response bodies may continue after the header deadline.

```yaml
auth:
  cel:
    unprivilegedTenantMCPs:
      - launchlayer-mcp
      - vendor-mcp

upstreams:
  launchlayer-selector:
    namespace: dsx-agent-gateway
    serviceLabels:
      app.kubernetes.io/name: launchlayer
  vendor-mcp:
    mode: static
    address: https://mcp.vendor.example/mcp
    requestTimeout: 10s
```

## Limit MCP Service discovery

The bundled controller watches all namespaces by default. Set
`agentgateway.discoveryNamespaceSelectors` to restrict discovery to the release
namespace and the namespaces selected by `upstreams`.

## Minimal values

Save this example as `dsx-agent-gateway-values.yaml`. It uses the bundled
rate-limit service and Valkey configuration.

```yaml
auth:
  jwt:
    providers:
      human:
        issuer: https://human.example.com
        audiences:
          - dsx-agent-gateway
        jwksUrl: https://human.example.com/.well-known/jwks.json
        tenantIdExpression: '"oidc:" + jwt.sub'
      service:
        issuer: https://service.example.com
        audiences:
          - dsx-agent-gateway
        jwksUrl: https://service.example.com/.well-known/jwks.json
        tenantIdExpression: '"service:" + jwt.sub'
      svid:
        issuer: https://trust.example.org
        audiences:
          - dsx-agent-gateway
        jwksUrl: https://trust.example.org/keys
        tenantIdExpression: 'jwt.sub.startsWith("spiffe://") && size(jwt.sub.split("/")) > 3 ? jwt.sub.split("/")[2] : ""'
  cel:
    operatorTenantId: oidc:operator
    unprivilegedTenantMCPs:
      - launchlayer-mcp # Service launchlayer, port mcp

upstreams:
  launchlayer-selector:
    namespace: dsx-agent-gateway
    serviceLabels:
      app.kubernetes.io/name: launchlayer
```

## Install and upgrade

Install the pinned Gateway API and agentgateway CRDs:

```bash
kubectl apply --server-side --force-conflicts \
  -f https://github.com/kubernetes-sigs/gateway-api/releases/download/v1.5.1/standard-install.yaml
helm upgrade --install agentgateway-crds \
  oci://cr.agentgateway.dev/charts/agentgateway-crds \
  --namespace agentgateway-system \
  --create-namespace \
  --version v1.3.1
```

Download the chart dependencies:

```bash
helm repo add valkey https://valkey-io.github.io/valkey-helm --force-update
helm dependency build deploy/dsx-agent-gateway
```

Install the chart:

```bash
helm install dsx-agent-gateway deploy/dsx-agent-gateway \
  --namespace dsx-agent-gateway \
  --create-namespace \
  --values dsx-agent-gateway-values.yaml \
  --wait \
  --timeout 5m
```

Apply later values or chart changes with an upgrade:

```bash
helm upgrade dsx-agent-gateway deploy/dsx-agent-gateway \
  --namespace dsx-agent-gateway \
  --values dsx-agent-gateway-values.yaml \
  --wait \
  --timeout 5m
```

## Gateway programming status

Wait until the DSX Agent Gateway is programmed:

```bash
kubectl wait \
  --namespace dsx-agent-gateway \
  --for=condition=Programmed \
  gateway/dsx-agent-gateway \
  --timeout=5m
```

The `Programmed` condition confirms controller programming. Check Pod and
dependency readiness separately.

## Configuration index

The [`values.yaml`](values.yaml) documents this chart's fields and non-obvious
examples inline. Additional native settings passed to the `agentgateway` and
`valkey` subcharts follow their pinned chart versions.

| Section | Purpose |
|---|---|
| `runtimeClassName` | RuntimeClass for gateway, rate-limit, and bridge Pods |
| `agentgateway` | Bundled controller enablement and chart settings |
| `gateway` | Dataplane replicas, scheduling, Service type, and IPv6 |
| `auth` | Generic JWT verification and tenant authorization |
| `upstreams` | MCP Service selectors, static targets, and timeout overrides |
| `upstreamRequestTimeout` | Default upstream response-header deadline |
| `rateLimit` | Tenant limits and the chart-owned rate-limit service |
| `valkey` | Bundled or external Valkey storage |
| `bridge` | Optional hub or leaf bridge and its NATS connection |
| `observability` | Prometheus metrics discovery and OTLP tracing |

For agentgateway concepts, use the
[agentgateway Kubernetes documentation](https://agentgateway.dev/docs/kubernetes/).
The pinned chart defaults are the source of truth for supported native settings.

For native Valkey settings, see the
[Valkey chart 0.9.4 documentation](https://github.com/valkey-io/valkey-helm/tree/valkey-0.9.4/valkey).

## Operations

The chart derives resource names from the Helm release. Each upstream's
`serviceLabels` must match its intended Service in the configured `namespace`.

The dataplane defaults to three replicas. The bundled rate-limit service and an
enabled bridge default to two replicas. Bundled Valkey defaults to one primary
and two replicas. The bundled agentgateway controller defaults to one replica.

When the cluster network plugin enforces `NetworkPolicy`, chart-owned ingress
policies restrict access to the enabled bundled rate-limit service and Valkey.
Only gateway dataplane Pods can reach the rate-limit API. Only bundled
rate-limit and Valkey Pods can reach the Valkey data port. Metrics policies
accept traffic from the release namespace and configured scraper namespaces.

The default `rateLimit.failureMode: FailOpen` keeps requests available when rate
limiting fails, so tenant limits are temporarily unenforced.

Chart-owned metrics and tracing use one shared configuration block. Bundled
Valkey keeps its exporter switch in the upstream chart's native values:

```yaml
observability:
  metrics:
    enabled: true # Set false without Prometheus Operator CRDs.
    # Extra namespaces allowed by metrics NetworkPolicies.
    scrapeNamespaces:
      - dsx-obs
  tracing:
    enabled: true # Set false without the Operator resources below.
    # Operator Instrumentation resource (namespace/name).
    instrumentationRef: dsx-obs/default-instrumentation
    # Operator Collector sidecar resource (namespace/name).
    sidecarRef: dsx-obs/default-sidecar
    # Optional exporter override for all traced components.
    exporter:
      endpoint: http://127.0.0.1:4318
      protocol: http/protobuf

valkey:
  metrics:
    enabled: true # Upstream Valkey exporter sidecar and ServiceMonitor.
```

| Component | Metrics source | Tracing source | Configuration source |
|---|---|---|---|
| DSX Agent Gateway dataplane | Native agentgateway Prometheus endpoint | Native agentgateway tracing | `observability` values and `AgentgatewayPolicy` |
| agentgateway controller | Native controller Prometheus endpoint | Not supported upstream | Upstream agentgateway values and chart-owned discovery |
| Rate-limit service | Native Prometheus support | Native OpenTelemetry support | `observability` values mapped to native settings |
| Bridge | OpenTelemetry Go runtime and HTTP instrumentation | OpenTelemetry Go HTTP instrumentation and SDK | Standard OpenTelemetry environment configuration |
| Bundled Valkey | Upstream Valkey chart exporter sidecar | Not supported upstream | Native `valkey.metrics` values |
| OpenTelemetry Collector sidecar | Collector internal telemetry is external | Local OTLP destination | DSX-managed Operator resources selected by the chart |

When metrics are enabled, the chart creates `ServiceMonitor` and `PodMonitor`
resources for the agentgateway dataplane and controller, rate-limit service,
and bridge. The upstream Valkey chart creates its exporter sidecar, metrics
Service, and `ServiceMonitor`. The DSX observability stack supplies the
`monitoring.coreos.com` CRDs and discovers these resources through its
OpenTelemetry Target Allocator. Helm cannot project a parent value into a
subchart value, so the example keeps the shared metrics switch and the native
Valkey switch aligned. Disabling both removes every monitor and exporter,
removes metrics ports from chart-owned Services, and disables export in the
rate-limit service and bridge. Agentgateway keeps its native listeners inside
its Pods because the upstream API has no disable setting, but the chart does
not expose or advertise them.

When tracing is enabled, the chart adds annotations that use the configured
OpenTelemetry Operator resources for the rate-limit service, bridge, and
agentgateway dataplane. Agentgateway uses its native OTLP/HTTP backend. The
rate-limit service and bridge use Instrumentation-injected SDK configuration by
default. Optional `exporter` values override the endpoint and protocol for all
three components. Disabling tracing removes the tracing backend, frontend
tracing configuration, injection annotations, and collector sidecars. The
agentgateway controller and Valkey components have no upstream tracing
integration.

Every chart-deployed container writes logs to standard output or standard
error. The agentgateway dataplane and controller use their native structured
logs. The chart selects JSON output for the rate-limit service, bridge, and
upstream Valkey exporter where their native configuration supports it. Valkey
writes its native server log format because it has no JSON log setting.

## Controller, Valkey, and bridge

To omit the bundled controller when a compatible cluster controller already
runs:

```yaml
agentgateway:
  enabled: false
```

To run the rate-limit service with an external Valkey Service:

```yaml
valkey:
  enabled: false
  external:
    serviceName: valkey
    namespace: data
    port: 6379
```

The external Valkey owner also owns its metrics exporter and discovery.

The optional bridge runs as a hub or leaf and uses an in-cluster NATS Service.
See the [bridge contract and runtime mapping](../../dsx-agentgateway-bridge/README.md).

## Uninstall

```bash
helm uninstall dsx-agent-gateway --namespace dsx-agent-gateway
```

Uninstall removes resources owned by this Helm release. It leaves the release
namespace, cluster-scoped CRDs, Valkey data PVCs, external workloads and
Services, and other operator-owned resources.
