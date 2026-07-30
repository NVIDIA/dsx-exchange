# DSX Exchange Local Development

This directory contains the local development environment for DSX Exchange.

For Event Bus architecture details, see
[docs/architecture.md](../docs/architecture.md).

## Quick Start

### Prerequisites

- Docker Desktop or equivalent
- [mise](https://mise.jdx.dev/) — installs the locked repository toolchain
- Make

`mise install --locked` installs the tools pinned by the root `mise.toml`
and `mise.lock`, including:

- [Go](https://go.dev/) 1.26.5
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

Use `make local-up` for deploy-only local setup.

### Skaffold

The root `skaffold.yaml` defaults to one `dsx-exchange` Kind cluster. CSC,
CPC-1, and CPC-2 remain separate logical sites through stable event-bus and
Gateway namespaces. Cluster-scoped controllers are installed once, while each
site keeps its fixed Envoy address and event-bus Helm release.

Root Mise handles prerequisites; host scripts create Kind, configure Zot, and
generate NATS secret material. The root Skaffold graph imports the
infrastructure, image builds, secret manifests, and Helm releases for both
products.

Zot persistently caches upstream images. Skaffold reuses its local build cache
when source is unchanged and uses native Kind image loading. Pinned local
dependencies are cached under the ignored root `.cache/` directory. Make stages
chart dependencies into each chart's ignored `charts/` directory before
Skaffold starts.

For iterative development, keep Skaffold running in one terminal:

```bash
make skaffold-dev
```

Then run the e2e test suite from another terminal:

```bash
make test-dev
```

### Agent Gateway

The local stack deploys the Agent Gateway chart in three logical sites:

- CSC hub: `csc-dsx-agentgateway`
- CPC-1 leaf: `cpc-1-dsx-agentgateway`
- CPC-2 leaf: `cpc-2-dsx-agentgateway`

The Skaffold releases are defined in
[`agent-gateway/skaffold.yaml`](agent-gateway/skaffold.yaml).
Common and site-specific chart values are in
[`agent-gateway/values/`](agent-gateway/values/).

### Run Tests

Event Bus performance tests require MetalLB from the local stack. Local MQTT
clients connect through the Envoy Gateway LoadBalancer IPs. On macOS, keep
`docker-mac-net-connect` running so the host can reach those IPs. Linux hosts
normally reach the Docker bridge IPs directly.

`make test` runs the Event Bus and Agent Gateway functional and performance
tests. The default CSC broker endpoint is `tcp://172.18.200.1:1883`; override
`CSC_BROKER_URL` only when testing a different broker.

Full benchmark runs can saturate local hosts because they drive thousands of
MQTT clients through Kind, Envoy Gateway, NATS, and auth-callout. If a full run
fails with EOFs or success-rate misses, check host CPU and pod metrics before
treating it as a networking failure.

For the testing strategy (functional and performance coverage), see
[docs/testing.md](../docs/testing.md).

## Targets

- `make test`: run repository validation, including local deployment and e2e.
- `make local-up`: deploy three logical sites to one Kind cluster.
- `make test-dev`: run functional and performance tests against the deployed stack.
- `make skaffold-dev`: run Skaffold dev for the complete local stack.
- `make perf-benchmark`: run the sustained Agent Gateway k6 profile.
- `make dummy-bms`: publish looping dummy BMS data.
- `make clean`: delete the Kind cluster and generated local artifacts.
- `make help`: show all available targets.

## Development

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
