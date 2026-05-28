# Auth Callout

NATS auth callout service for JWT-based authentication.

## Deployment

See [deploy/README.md](./deploy/README.md) for Helm chart configuration.

## Configuration

For authentication configuration (NKeys, OAuth2/JWKS, mTLS, permissions), see
[docs/authentication.md](../docs/authentication.md).

For Helm chart values and deployment, see [deploy/README.md](./deploy/README.md).

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

Prometheus metrics at `http://auth-callout.127-0-0-1.nip.io:9090/metrics`.

For the full metrics reference, see [docs/operations.md](../docs/operations.md).
 