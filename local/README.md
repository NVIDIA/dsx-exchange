# DSX Event Bus

This repository contains the NATS event bus implementation for the AI Factory DSX platform.

For architecture details, see [docs/architecture.md](../docs/architecture.md).

## Quick Start

### Prerequisites

Version-pinned where there is a known compatibility break; unpinned tools work with any recent release.

- Docker Desktop or equivalent
- [Kind](https://kind.sigs.k8s.io/)
- [kubectl](https://kubernetes.io/docs/tasks/tools/)
- [Helm](https://helm.sh/) 4.0+ — local deploy uses `--force` which requires Helm 4
- [Go](https://go.dev/doc/install) 1.25+ — required by `go.mod`
- Make

### MacOS Tweaks

MetalLB doesn't work out of the box on MacOS.

<https://waddles.org/2024/06/04/kind-with-metallb-on-macos/>

TLDR

```bash
brew install chipmk/tap/docker-mac-net-connect
sudo brew services start chipmk/tap/docker-mac-net-connect
```

Now you can hit IPs from MetalLB from your local machine.

You may need to restart the service if it stops working.

```bash
sudo brew services restart chipmk/tap/docker-mac-net-connect
```

### Setup Infrastructure

```bash
# Create all three Kind clusters and deploy infrastructure
make setup-infra

# Verify infrastructure is ready
make verify-infra
```

### Deploy Event Bus

```bash
# Deploy NATS to all layers
make deploy-nats
```

### Run Tests

Performance and benchmark targets require MetalLB (installed by `make setup-infra`). Without it, `kubectl port-forward` is used as a fallback but cannot sustain benchmark throughput — tests fail silently with connectivity errors that do not indicate the root cause.

```bash
# Run functional tests against all candidates
make test-functional

# Run performance e2e smoke tests (requires MetalLB)
make test-performance

# Run full performance benchmarks (requires MetalLB)
make benchmark-performance

# Run MQTT benchmark smoke suite (requires MetalLB)
make benchmark-basic

# Run full MQTT benchmark suite (requires MetalLB)
make benchmark-basic-full

# Publish looping dummy BMS data to the CSC MQTT broker
make dummy-bms
```

For the testing strategy (functional and performance coverage), see
[docs/testing.md](../docs/testing.md).

## Common Commands

```bash
# Infrastructure Setup
make setup-clusters          # Create all Kind clusters (CSC, CPC-1, CPC-2)
make setup-infra             # Deploy MetalLB, Envoy Gateway, cert-manager, metrics-server, Keycloak, Prometheus
make setup-metallb           # Deploy MetalLB only
make setup-envoy-gateway     # Deploy Envoy Gateway only
make setup-cert-manager      # Deploy cert-manager only
make setup-metrics-server    # Deploy metrics-server only
make setup-keycloak          # Deploy Keycloak only
make setup-observability     # Deploy Prometheus/Grafana only
make verify-infra            # Verify infrastructure is ready

# Event Bus Deployment
make deploy-nats             # Deploy NATS to all layers
make validate-nats           # Validate NATS deployment

# Testing
make test-functional         # Run functional tests (MQTT + federation)
make test-performance        # Run performance e2e smoke tests
make benchmark-performance   # Run full performance benchmarks
make benchmark-basic         # Run MQTT benchmark smoke suite
make benchmark-basic-full    # Run full MQTT benchmark basic suite
make dummy-bms               # Publish looping dummy BMS data

# Monitoring & Cleanup
make status                  # Check deployment status
make clean-nats              # Delete NATS namespaces
make clean                   # Delete all Kind clusters

# Development
make lint                    # Run linters
make help                    # Show all available targets
```

## Development

### Known Issues

- **TODO: Fix mTLS JetStream with Synadia support** - JetStream API requests (`$JS.API.*`) are not routing through NATS-mTLS leaf nodes. Need to investigate Synadia NATS configuration for enabling JetStream API forwarding through leaf nodes without local JetStream persistence. mTLS tests are currently skipped.

### MQTT Benchmark Suite

Run standardized MQTT broker benchmarks following the [Open MQTT Benchmark Suite](https://github.com/emqx/mqttbs):

```bash
# Run all Basic scenarios (automatically discovers CSC NATS gateway)
make benchmark-basic

# Run individual scenarios
cd mqttbs
GATEWAY_IP=$(kubectl --context kind-csc get gateway -A -l app.kubernetes.io/component=event-bus-gateway -o jsonpath='{.items[0].status.addresses[0].value}')
./mqttbs run connection-10k --broker tcp://$GATEWAY_IP:1883
./mqttbs run fanout-1k --broker tcp://$GATEWAY_IP:1883 --duration 30s
./mqttbs run p2p-1k --broker tcp://$GATEWAY_IP:1883
./mqttbs run fanin-1k --broker tcp://$GATEWAY_IP:1883

# View available scenarios
./mqttbs list
```

See [mqttbs/README.md](mqttbs/README.md) for details.

### Run Local Tests

```bash
cd mqtt-client
go test -v -count=1 ./tests/functional/...
go test -v -count=1 ./tests/performance/...
```

### Dummy BMS Data

`mqtt-client/cmd/dummy-bms` keeps the local CSC demo populated with
representative BMS MQTT traffic. It replays `mqtt-client/examples/dsx_exemplar.csv`
on a loop, validates rendered messages against the BMS AsyncAPI schema before
publishing, retains metadata topics, and publishes value topics as live readings.
Rows are scheduled by absolute publish time so one slow publish does not shift
the rest of the scenario.

Run against the local Kind environment:

```bash
make dummy-bms
```

The dummy BMS target uses the same local e2e environment and gateway
port-forward setup as the functional and performance tests. It publishes to the
CSC broker URL exported by that wrapper.
