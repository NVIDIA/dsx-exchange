# Auth Callout

NATS auth callout service for JWT-based authentication.

[[_TOC_]]

## Deployment

See [deploy/README.md](./deploy/README.md) for Helm chart configuration.

## Configuration

The auth callout connects to a NATS server and handles authentication requests. It requires:

- **NATS server** with [auth callout](https://docs.nats.io/running-a-nats-service/configuration/securing_nats/auth_callout) enabled
- **NKey seeds** for NATS connection and JWT signing
- **OIDC provider** (for OAuth2) or **CA certificate** (for mTLS)

Two config files are required: `config.yaml` (service config) and `permissions.json` (client mappings).

### Service Config

```yaml
host:
  port: 8000
  name: "auth-callout"

nats:
  url: "nats://nats:4222"
  nkey-seed: "SUAM..."      # connection NKey seed (from Vault)
  issuer-seed: "SAAG..."    # account signing key seed (from Vault)
  xkey-seed: "SXAE..."      # encryption XKey seed (optional, from Vault)

jwks:
  url: "https://keycloak/realms/master/protocol/openid-connect/certs"
  issuer: "https://keycloak/realms/master"

mtls:
  ca-path: "/etc/ssl/certs/ca.crt"

permissions:
  file: "/config/permissions.json"

observability:
  logging:
    level: "info"
  metrics:
    enabled: true
    provider: "prometheus"
    prometheus:
      port: 9090
  tracing:
    enabled: false
```

### Generating NKeys

The service requires NKey seeds for NATS authentication. Generate using [nsc](https://github.com/nats-io/nsc):

```bash
# Install nsc
go install github.com/nats-io/nsc/v2@latest

# User NKey (for nkey-seed) - connects to NATS
nsc generate nkey --user
# Output: seed (SU...) and public key (U...)

# Account signing key (for issuer-seed) - signs user JWTs
nsc generate nkey --account
# Output: seed (SA...) and public key (A...)

# XKey (for xkey-seed, optional) - encrypts auth callout responses
nsc generate nkey --curve
# Output: seed (SX...) and public key (X...)
```

| Key | Seed Prefix | Public Key Prefix | Purpose |
|-----|-------------|-------------------|---------|
| `nkey-seed` | `SU` | `U` | Auth callout connects to NATS |
| `issuer-seed` | `SA` | `A` | Signs user JWTs for authenticated clients |
| `xkey-seed` | `SX` | `X` | Encrypts responses (optional) |

Seeds are secrets (store in Vault). Public keys are configured in NATS server.

See [NATS NKeys documentation](https://docs.nats.io/running-a-nats-service/configuration/securing_nats/auth_intro/nkey_auth).

### Permissions Config

Client mappings in `permissions.json`. Each auth type (`oauth2`, `mtls`, `nkey`) is a map of client entries.

**Environment Variable Expansion**: String values support `${VAR}` syntax for fields like `public_key`, `account`, `identity`, `subject`, and `azp`. JSON Keys are not expanded, just values.

**Example configuration:**

```json
{
  "oauth2": {
    "frontend-app": {
      "subject": "frontend-client-id",
      "account": "APP",
      "permissions": {
        "pub": { "allow": ["events.>"] },
        "sub": { "allow": ["notifications.>"] }
      }
    },
    "admin-client": {
      "azp": "admin-client-id",
      "account": "ADMIN",
      "permissions": {
        "pub": { "allow": [">"] },
        "sub": { "allow": [">"] }
      }
    }
  },
  "mtls": {
    "gateway": {
      "identity": "CN=gateway",
      "account": "DEVICES",
      "permissions": {
        "pub": { "allow": ["sensors.>"] },
        "sub": { "allow": ["commands.>"] }
      }
    }
  },
  "nkey": {
    "service": {
      "public_key": "UABC123...",
      "account": "SERVICE",
      "permissions": {
        "pub": { "allow": [">"] },
        "sub": { "allow": [">"] }
      }
    }
  },
  "noauth": {
    "account": "ANONYMOUS",
    "permissions": {
      "pub": { "allow": ["public.>"] },
      "sub": { "allow": ["public.>"] }
    }
  }
}
```

**Auth Types:**

| Type | Identifier Field | Description |
|------|------------------|-------------|
| `oauth2` | `azp` or `subject` | JWT token validated via JWKS |
| `mtls` | `identity` | X.509 certificate Common Name |
| `nkey` | `public_key` | NATS NKey public key |
| `noauth` | (none) | Anonymous access fallback |

**Permission Fields:**

- `pub.allow` - subjects the client can publish to
- `sub.allow` - subjects the client can subscribe to
- `resp` (optional) - request-reply settings (`max`, `ttl`)

Subject wildcards: `*` matches one token, `>` matches one or more tokens.

See [NATS Authorization](https://docs.nats.io/running-a-nats-service/configuration/securing_nats/authorization) for details.

## Development

### Prerequisites

- Docker
- kind
- helm
- kubectl
- nsc

### Quick Start

```bash
# Create cluster and start dev mode
devspace run fresh

# Or if cluster exists
devspace dev
```

### Ports

| Port | Service |
|------|---------|
| 4222 | NATS |
| 1883 | MQTT (NATS) |
| 8000 | Auth Callout API (health) |
| 9090 | Auth Callout Metrics |

### Commands

```bash
# Inside dev container
make dev          # Hot reload
make test         # Run tests
make lint         # Lint code
```

### Metrics

Prometheus metrics at `http://auth-callout.127-0-0-1.nip.io:9090/metrics`

**Auth Callout** (appear after requests):

| Metric | Type | Description |
|--------|------|-------------|
| `auth_requests_total` | counter | Total NATS auth callout requests |
| `auth_errors_total` | counter | Total NATS auth callout errors |
| `auth_request_duration_seconds` | histogram | Duration of auth requests |
| `auth_oauth2_attempts_total` | counter | OAuth2 authentication attempts |
| `auth_oauth2_failures_total` | counter | OAuth2 authentication failures |
| `auth_mtls_attempts_total` | counter | mTLS authentication attempts |
| `auth_mtls_failures_total` | counter | mTLS authentication failures |
| `auth_nkey_attempts_total` | counter | NKey authentication attempts |
| `auth_nkey_failures_total` | counter | NKey authentication failures |
| `auth_noauth_attempts_total` | counter | NoAuth authentication attempts |
| `auth_noauth_failures_total` | counter | NoAuth authentication failures |

**Go Runtime**:

| Metric | Type | Description |
|--------|------|-------------|
| `go_goroutines` | gauge | Current goroutines |
| `go_threads` | gauge | OS threads created |
| `go_gc_duration_seconds` | summary | GC pause duration |
| `go_gc_gogc_percent` | gauge | GOGC setting |
| `go_gc_gomemlimit_bytes` | gauge | GOMEMLIMIT setting |
| `go_info` | gauge | Go version info |
| `go_sched_gomaxprocs_threads` | gauge | GOMAXPROCS |
| `go_memstats_alloc_bytes` | gauge | Heap bytes in use |
| `go_memstats_heap_objects` | gauge | Allocated objects |
| `go_memstats_sys_bytes` | gauge | Total memory from OS |

**Process**:

| Metric | Type | Description |
|--------|------|-------------|
| `process_cpu_seconds_total` | counter | CPU time used |
| `process_resident_memory_bytes` | gauge | Resident memory |
| `process_virtual_memory_bytes` | gauge | Virtual memory |
| `process_open_fds` | gauge | Open file descriptors |
| `process_max_fds` | gauge | Max file descriptors |
| `process_start_time_seconds` | gauge | Process start time |
| `process_network_receive_bytes_total` | counter | Network bytes received |
| `process_network_transmit_bytes_total` | counter | Network bytes sent |
 