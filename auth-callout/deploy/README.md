# auth-callout Service Helm Chart

This Helm chart deploys the auth-callout service for NATS authentication.

## Prerequisites

- **Kubernetes**: 1.27+
- **Helm**: 3.12+
- **NATS server** with auth callout enabled ([NATS Auth Callout](https://docs.nats.io/running-a-nats-service/configuration/securing_nats/auth_callout))
- **NKey seeds** for authentication (see [Generating NKeys](../README.md#generating-nkeys))
- **Vault** (optional) for secret injection

For OAuth2 authentication:

- **OIDC provider** with JWKS endpoint (e.g., Keycloak)

For mTLS authentication:

- **CA certificate** for client certificate validation

### Resource Requirements

| Resource | Default |
|----------|---------|
| CPU Request | 50m |
| Memory Request | 64Mi |

### Network

| Port | Purpose |
|------|---------|
| 8080 | Health checks (`/healthz`) |
| 9090 | Prometheus metrics (if enabled) |

## Service Configuration

### NATS Connection

The auth callout connects to a NATS server and registers as an authorization service:

```yaml
serviceConfig:
  nats:
    url: "nats://nats:4222"    # NATS server URL
    nkey-seed: ""              # User NKey seed (from Vault)
    issuer-seed: ""            # Account signing key seed (from Vault)
    xkey-seed: ""              # XKey seed for encryption (optional, from Vault)
```

Seeds are injected via Vault. See [Vault Integration](#vault-integration).

### OAuth2/JWKS Configuration

For OAuth2 authentication, configure the JWKS endpoint:

```yaml
serviceConfig:
  jwks:
    url: "https://keycloak/realms/master/protocol/openid-connect/certs"
    issuer: "https://keycloak/realms/master"
```

### mTLS Configuration

For mTLS authentication, configure the CA certificate path:

```yaml
serviceConfig:
  mtls:
    ca-path: "/etc/ssl/certs/ca.crt"
```

## Permissions Configuration

### Using External ConfigMap

Parent charts can provide a pre-generated permissions ConfigMap:

```yaml
permissionsConfigMap: "my-permissions-configmap"
```

When set, the chart does not generate its own ConfigMap. The external ConfigMap must contain a `permissions.json` key.

### Inline Permissions

Configure client mappings in the `permissions` section. Each auth type (`oauth2`, `mtls`, `nkey`) is a map where keys are client names and values define their permissions:

```yaml
permissions:                    # top-level Helm value
  oauth2:                       # auth type (map of clients)
    frontend-app:               # client name (any string)
      subject: "frontend-client-id" # can specify subject, azp, or both
      account: "APP"
      required_scope: "nats"  # scope required in JWT (defaults to "mqtt")
      permissions:
        pub:
          allow: ["events.>"]
        sub:
          allow: ["notifications.>"]
    backend-service:            # another client
      azp: "backend-client-azp"
      account: "APP"
      required_scope: "nats"
      permissions:
        pub:
          allow: ["internal.>"]
        sub:
          allow: ["internal.>"]
```

**OAuth2 Required Scope:**

Each OAuth2 client can specify a `required_scope` that must be present in the JWT token. If not specified, defaults to `"mqtt"`. The scope is checked against both the `scope` claim (space-separated string per RFC 8693) and the `scopes` claim (array format used by some providers).

Subject wildcards:

- `*` matches a single token (e.g., `sensors.*` matches `sensors.temp` but not `sensors.temp.cpu`)
- `>` matches one or more tokens (e.g., `sensors.>` matches `sensors.temp` and `sensors.temp.cpu`)

See [NATS Authorization documentation](https://docs.nats.io/running-a-nats-service/configuration/securing_nats/authorization) for full details.

### Auth Type Examples

Each auth type can have any number of client entries:

```yaml
permissions:
  # OAuth2 - JWT tokens validated via JWKS (multiple clients)
  oauth2:
    mqtt-client:
      azp: "mqtt-client-id"      # JWT authorized party claim
      account: "APP"
      required_scope: "mqtt"     # scope required in JWT (default)
      permissions:
        pub:
          allow: ["events.>"]
        sub:
          allow: ["commands.>"]
    admin-client:
      azp: "admin-client-id"
      account: "ADMIN"
      required_scope: "nats:admin"  # custom scope for admin
      permissions:
        pub:
          allow: [">"]
        sub:
          allow: [">"]

  # mTLS - X.509 certificate authentication (multiple clients)
  mtls:
    gateway:
      identity: "CN=gateway"     # certificate Common Name (exact match)
      account: "DEVICES"
      permissions:
        pub:
          allow: ["sensors.>"]
        sub:
          allow: ["commands.>"]
    edge-device-1:
      identity: "CN=edge-device-1"
      account: "DEVICES"
      permissions:
        pub:
          allow: ["telemetry.>"]
        sub:
          allow: ["config.>"]

  # NKey - NATS NKey public key
  nkey:
    service:
      public_key: "UABC123..."   # NKey public key (seeds in Vault)
      account: "SERVICE"
      permissions:
        pub:
          allow: [">"]
        sub:
          allow: [">"]

  # NoAuth - anonymous access (single entry, omit to disable)
  noauth:
    account: "ANONYMOUS"
    permissions:
      pub:
        allow: ["public.>"]
      sub:
        allow: ["public.>"]
```

## Configuration Validation

The chart includes built-in validation to ensure proper configuration before deployment. The validation checks are automatically executed during `helm install` or `helm template` operations.

### Metrics Configuration

The service supports two metrics export strategies:

**Prometheus Export (default):**
Expose metrics endpoint for Prometheus scraping:

```yaml
serviceConfig:
  observability:
    metrics:
      enabled: true
      provider: prometheus
      prometheus:
        port: 9090

# ServiceMonitor for Prometheus Operator
serviceMonitor:
  enabled: true
  interval: "15s"
  path: "/metrics"
```

**OTLP Export:**
Push metrics to an OpenTelemetry collector:

```yaml
serviceConfig:
  observability:
    metrics:
      enabled: true
      provider: otlp
      otlp:
        endpoint: otel-collector:4317
        https: false
        export-interval-sec: 30
        export-timeout-sec: 10
```

**ServiceMonitor:**

ServiceMonitor is only created when:

- `serviceConfig.observability.metrics.enabled: true`
- `serviceConfig.observability.metrics.provider: prometheus`
- `serviceMonitor.enabled: true`

### Health Checks Configuration

The chart includes comprehensive health check probe configuration:

```yaml
healthChecks:
  # Liveness probe - determines when to restart container
  livenessProbe:
    enabled: true
    httpGet:
      path: "/healthz"
      port: "http"
      scheme: "HTTP"
    initialDelaySeconds: 10
    periodSeconds: 10
    timeoutSeconds: 5
    failureThreshold: 3
    successThreshold: 1

  # Readiness probe - determines when container is ready to serve traffic
  readinessProbe:
    enabled: true
    httpGet:
      path: "/healthz"
      port: "http"
      scheme: "HTTP"
    initialDelaySeconds: 5
    periodSeconds: 5
    timeoutSeconds: 3
    failureThreshold: 3
    successThreshold: 1

  # Startup probe - for slow-starting containers
  startupProbe:
    enabled: false  # Disabled by default
    httpGet:
      path: "/healthz"
      port: "http"
      scheme: "HTTP"
    initialDelaySeconds: 0
    periodSeconds: 10
    timeoutSeconds: 5
    failureThreshold: 30  # Allow up to 5 minutes for startup
    successThreshold: 1
```

**Available Health Endpoints:**

- `/healthz` - Primary health check endpoint (recommended)
- `/v1/` - Basic connectivity test

## Installation Examples

### Basic

```bash
helm install my-release ./deploy
```

### With metrics enabled (OTLP)

```bash
helm install my-release ./deploy \
  --set serviceConfig.observability.metrics.enabled=true \
  --set serviceConfig.observability.metrics.provider=otlp
```

### With Prometheus metrics and ServiceMonitor

```bash
helm install my-release ./deploy \
  --set serviceConfig.observability.metrics.enabled=true \
  --set serviceConfig.observability.metrics.provider=prometheus \
  --set serviceMonitor.enabled=true
```

### With custom health check configuration

```bash
helm install my-release ./deploy \
  --set healthChecks.livenessProbe.initialDelaySeconds=15 \
  --set healthChecks.readinessProbe.periodSeconds=10 \
  --set healthChecks.startupProbe.enabled=true
```

### With health checks disabled

```bash
helm install my-release ./deploy \
  --set healthChecks.livenessProbe.enabled=false \
  --set healthChecks.readinessProbe.enabled=false
```

## Validation

```bash
# Check pod is running
kubectl get pods -l app.kubernetes.io/name=auth-callout

# Test authentication by connecting to NATS
nats pub test.topic "hello" --server nats://$NATS_URL:4222
```

## Validation Error Examples

The chart will fail with helpful error messages for invalid configurations:

```text
Error: Unsupported metrics provider 'invalid'. Supported providers are: prometheus, otlp

Error: When serviceConfig.observability.metrics.provider is 'prometheus', serviceConfig.observability.metrics.prometheus.port must be specified

Error: ServiceMonitor requires metrics.enabled=true and metrics.provider=prometheus
```

## Vault Integration

This chart supports Vault Agent injection for dynamic secret management. Vault templates require special syntax because Helm must create template text that Vault will process later.

**📖 See the [Vault Template Development Guide](../docs/VAULT_TEMPLATE_GUIDE.md)** for:

- Understanding the 3-stage rendering pipeline (values.yaml → annotation → pod file)
- Backtick escape syntax for creating Vault templates with Helm
- Step-by-step guide to adding new secrets
- Common pitfalls and solutions
- Testing and verification at each stage

**Quick Example:**

```yaml
vault:
  enabled: true
  secrets:
    nats: "secret/data/myapp/nats"
  template:
    filename: "config-secrets.yaml"
    content: |
      nats:
        {{`{{- with secret "`}}{{ .Values.vault.secrets.nats }}{{`" }}`}}
        nkey-seed: "{{`{{ .Data.data.nkey_seed }}`}}"
        issuer-seed: "{{`{{ .Data.data.issuer_seed }}`}}"
        xkey-seed: "{{`{{ .Data.data.xkey_seed }}`}}"
        {{`{{- end }}`}}
```

See `../docs/examples/values-vault-example.yaml` for a complete working example.

## Values File Structure

See `values.yaml` for the complete configuration structure and all available options.
