# DSX Event Bus

This repository contains the NATS event bus implementation for the AI Factory DSX platform.

For architecture details, see [docs/architecture.md](../docs/architecture.md).

## Quick Start

### Prerequisites

- Docker Desktop or equivalent
- [mise](https://mise.jdx.dev/) — installs the locked repository toolchain
- Make

`make install-e2e-prereqs` installs the tools pinned by the root `mise.toml`
and `mise.lock`, including:

- [Go](https://go.dev/) 1.26.4
- [Kind](https://kind.sigs.k8s.io/) v0.32.0
- [kubectl](https://kubernetes.io/docs/tasks/tools/) v1.34.0
- [Helm](https://helm.sh/) v4.2.2
- [Skaffold](https://skaffold.dev/) v2.21.0
- cfssl/cfssljson v1.6.5
- nsc v2.14.0
- nk v0.4.15
- yq v4.52.5
- addlicense v1.2.0

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

### Skaffold

The root `skaffold.yaml` defaults to one `dsx-exchange` Kind cluster. CSC,
CPC-1, and CPC-2 remain separate logical sites through stable event-bus and
Gateway namespaces. Cluster-scoped controllers are installed once, while each
site keeps its fixed Envoy address and event-bus Helm release. Set
`MULTI_CLUSTER=1` to place those same site packages in `csc`, `cpc-1`, and
`cpc-2` Kind clusters on a heavyweight host.

Root Mise handles prerequisites; host scripts select the topology, create Kind,
configure the local registry, and generate NATS secret material. The root
Skaffold graph imports shared controller packages, reusable site packages, the
shared image build, secret manifests, and NATS releases.

Zot persistently caches upstream images. Skaffold owns one auth-callout build
artifact, reuses its local build cache when the source is unchanged, and uses
native Kind image loading for the active physical cluster. Pull-through routing
is selected by the image's upstream registry and accepts any repository from
that registry.
Pinned local dependencies are cached under the ignored root `.cache/`
directory. Normal cleanup keeps both caches. Before Skaffold starts, Make
stages cached subcharts into the event-bus chart's standard ignored `charts/`
directory, so deploys never
refresh Helm repositories.

For iterative development, keep Skaffold running in one terminal:

```bash
make skaffold-dev
```

Then run the e2e test suite from another terminal:

```bash
make test-dev
```

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

## Targets

- `make test`: deploy the stack, then run functional and performance tests.
- `make local-up`: deploy three logical sites to one Kind cluster.
- `MULTI_CLUSTER=1 make local-up`: deploy one logical site per Kind cluster.
- `make test-dev`: run the same tests against an already running local stack.
- `make skaffold-run`: deploy the stack without running tests.
- `make skaffold-dev`: run Skaffold dev for the complete local stack.
- `make validate`: check the deployed stack's cluster, Gateway, NATS, and
  Keycloak readiness.
- `make benchmark`: run the MQTT benchmark smoke suite.
- `make benchmark-full`: run the full MQTT benchmark basic suite.
- `make dummy-bms`: publish looping dummy BMS data.
- `make status`: check deployment status.
- `make clean`: delete Kind clusters and generated local artifacts.
- `make help`: show all available targets.

## Development

### Known Issues

- **TODO: Fix mTLS JetStream with Synadia support** - JetStream API requests (`$JS.API.*`) are not routing through NATS-mTLS leaf nodes. Need to investigate Synadia NATS configuration for enabling JetStream API forwarding through leaf nodes without local JetStream persistence. mTLS tests are currently skipped.

### MQTT Benchmark Suite

Run standardized MQTT broker benchmarks following the [Open MQTT Benchmark Suite](https://github.com/emqx/mqttbs):

```bash
# Run individual scenarios
cd mqttbs
GATEWAY_IP=$(kubectl --context kind-dsx-exchange get gateway shared-gateway -n csc-gateway -o jsonpath='{.status.addresses[0].value}')
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
