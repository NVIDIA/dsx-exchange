# DSX Event Bus

This repository contains the NATS event bus implementation for the AI Factory DSX platform.

For architecture details, see [docs/architecture.md](../docs/architecture.md).

## Quick Start

### Prerequisites

Install Docker, Go, and Make first:

- Docker Desktop or equivalent
- [Go](https://go.dev/doc/install) 1.25+ — required by `go.mod`
- Make

`make install-e2e-prereqs` installs missing local e2e tools into
`E2E_PREREQS_BIN`. If a tool is already on `PATH`, the target reuses it and
warns when its version differs from the expected version:

- [Kind](https://kind.sigs.k8s.io/) v0.31.0
- [kubectl](https://kubernetes.io/docs/tasks/tools/) v1.31.9
- [Helm](https://helm.sh/) v4.2.0
- [Skaffold](https://skaffold.dev/) v2.21.0
- cfssl/cfssljson v1.6.5
- nsc v2.14.0
- nk v0.4.15
- yq v4.53.2

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

### Setup Local Stack

Run local e2e targets from a host shell with Docker access. Sandboxed shells can
fail on Docker buildx permissions or host network access.

```bash
make test
```

Use `make skaffold-run` for deploy-only local setup.

### Skaffold Setup

The root `skaffold.yaml` imports the local infra and NATS modules:

- `local/infra/skaffold.yaml`: MetalLB, Envoy Gateway, cert-manager,
  metrics-server, Prometheus, and CSC Keycloak.
- `local/nats/skaffold.yaml`: auth-callout image build and the NATS event-bus
  chart for CSC, CPC-1, and CPC-2.

Use the Make targets for local runs. `make skaffold-run` deploys the three
clusters in parallel with cluster-scoped `skaffold run` commands. `make
skaffold-run-serial` runs the same modules in one `skaffold run` for comparison
or debugging.

The full e2e targets follow the same default:

- `make test`: deploy the stack in parallel, then run functional and
  performance tests.
- `make test-serial`: deploy the stack serially, then run the same tests.
- `make test-dev`: run the same tests against an already running local stack.

For iterative development, keep Skaffold running in one terminal:

```bash
make skaffold-dev
```

Then run the e2e test suite from another terminal:

```bash
make test-dev
```

Host scripts still handle local prerequisites, Kind cluster creation, the local
Docker registry, and generated NATS secret material. Skaffold builds the
auth-callout image, deploys chart releases with Helm, applies raw local
manifests, and watches local changes in dev mode. It keeps resources after a run
so follow-up tests can use the deployed environment. Its artifact cache lives
under `~/.skaffold` unless `SKAFFOLD_CACHE_FILE` or
`SKAFFOLD_REMOTE_CACHE_DIR` is overridden.

CI uses the existing `DOCKERHUB_MIRROR` value for Docker daemon and Kind image
pulls.

### Run Tests

Performance and benchmark targets require MetalLB from the local stack. Local
clients connect through the Envoy Gateway LoadBalancer IPs. On macOS, keep
`docker-mac-net-connect` running so the host can reach those IPs. Linux hosts
normally reach the Docker bridge IPs directly.

`make test` runs the full local e2e suite. The default CSC broker endpoint
is `tcp://172.18.200.1:1883`; override `CSC_BROKER_URL` only when testing a
different broker.

Full benchmark targets can saturate local hosts because they drive thousands of
MQTT clients through Kind, Envoy Gateway, NATS, and auth-callout. If a full run
fails with EOFs or success-rate misses, check host CPU and pod metrics before
treating it as a networking failure.

For the testing strategy (functional and performance coverage), see
[docs/testing.md](../docs/testing.md).

## Common Commands

```bash
# Local Setup
make test                    # Deploy local stack and run the e2e suite
make test-serial             # Run the same e2e suite with serial deploy
make test-dev                # Run the e2e suite against the current stack
make skaffold-run            # Deploy required infra and NATS
make skaffold-run-serial     # Deploy required infra and NATS in one Skaffold run
make skaffold-dev            # Run Skaffold dev for the complete local dev stack

# Benchmarks and demos
make benchmark               # Run MQTT benchmark smoke suite
make benchmark-full          # Run full MQTT benchmark basic suite
make dummy-bms               # Publish looping dummy BMS data

# Monitoring & Cleanup
make status                  # Check deployment status
make clean                   # Delete Kind clusters and generated local artifacts
make help                    # Show all available targets
```

## Development

### Known Issues

- **TODO: Fix mTLS JetStream with Synadia support** - JetStream API requests (`$JS.API.*`) are not routing through NATS-mTLS leaf nodes. Need to investigate Synadia NATS configuration for enabling JetStream API forwarding through leaf nodes without local JetStream persistence. mTLS tests are currently skipped.

### MQTT Benchmark Suite

Run standardized MQTT broker benchmarks following the [Open MQTT Benchmark Suite](https://github.com/emqx/mqttbs):

```bash
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

The dummy BMS target uses the same local e2e environment and Envoy Gateway
LoadBalancer path as the functional and performance tests. It publishes to the
CSC broker at `tcp://172.18.200.1:1883` unless `CSC_BROKER_URL` is overridden.
