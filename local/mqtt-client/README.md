# MQTT Test Client

MQTT performance and functional testing framework for evaluating event bus solutions.

## Features

- **Performance Tests**: Comprehensive throughput testing (QoS 0/1, retained, federation)
- **Functional Tests**: MQTT protocol compliance, HA, federation validation
- **Metrics**: Prometheus metrics for monitoring
- **Reusable Components**: Client and configuration packages

## Testing

```bash
# Run unit tests
go test ./pkg/...

# Run functional tests
go test ./tests/functional/...

# Run performance e2e smoke tests from repo root
make -C .. test-performance

# Run full performance benchmarks from repo root
make -C .. benchmark-performance

# Run all tests with coverage
go test -cover ./...

# Skip performance tests in short mode
go test -short ./...
```

## Performance Tests

### Test Matrix

Performance tests exercise throughput under different conditions:

**QoS and Retained Combinations:**

- **QoS 0**: No persistence
- **QoS 0 + Retained**: Retained messages without persistence guarantee
- **QoS 1**: With persistence
- **QoS 1 + Retained**: Retained messages with persistence guarantee

**Deployment Scenarios:**

- **Local**: Publishers and subscribers on same cluster
- **Federation**: Publishers on one cluster, subscribers on another

**12 Test Combinations:**

1. Local + QoS 0
2. Local + QoS 0 + Retained
3. Local + QoS 1 (persistence)
4. Local + QoS 1 + Retained (persistence)
5. CPC to CSC + QoS 0
6. CPC to CSC + QoS 0 + Retained
7. CPC to CSC + QoS 1 (persistence)
8. CPC to CSC + QoS 1 + Retained (persistence)
9. CSC to CPC + QoS 0
10. CSC to CPC + QoS 0 + Retained
11. CSC to CPC + QoS 1 (persistence)
12. CSC to CPC + QoS 1 + Retained (persistence)

### Local Performance Tests

Test throughput on a single cluster:

```bash
# From local/
make test-performance

# Or run directly when broker URLs are already exported
go test -v ./tests/performance/ -run 'TestThroughput.*_Local'
```

**Tests:**

- `TestThroughputQoS0_Local`: QoS 0
- `TestThroughputQoS0Retained_Local`: QoS 0 with retained
- `TestThroughputQoS1_Local`: QoS 1 with persistence
- `TestThroughputQoS1Retained_Local`: QoS 1 with retained (persistence)

### Federation Performance Tests

Test cross-cluster throughput (CPC1 <-> CSC):

```bash
# From local/
make test-performance

# Or run directly when broker URLs are already exported
go test -v ./tests/performance/ -run 'TestThroughput.*_(CPCtoCSC|CSCtoCPC)'
```

**Tests (CPC1 -> CSC):**

- `TestThroughputQoS0_CPCtoCSC`
- `TestThroughputQoS0Retained_CPCtoCSC`
- `TestThroughputQoS1_CPCtoCSC` (with persistence)
- `TestThroughputQoS1Retained_CPCtoCSC` (persistence)

**Tests (CSC -> CPC1):**

- `TestThroughputQoS0_CSCtoCPC`
- `TestThroughputQoS0Retained_CSCtoCPC`
- `TestThroughputQoS1_CSCtoCPC` (with persistence)
- `TestThroughputQoS1Retained_CSCtoCPC` (persistence)

## Metrics

The performance tests expose Prometheus metrics:

**Metrics:**

- `mqtt_messages_published_total`: Total messages published
- `mqtt_messages_received_total`: Total messages received
- `mqtt_publish_duration_seconds`: Message publish latency histogram
- `mqtt_e2e_latency_seconds`: End-to-end latency histogram (publish to receive)
- `mqtt_connections_active`: Number of active connections
- `mqtt_errors_total`: Total errors by type
- `mqtt_throughput_messages_per_second`: Current throughput gauge

**Labels:**

- `broker`: Broker address
- `broker_pub`: Publisher broker (for federation)
- `broker_sub`: Subscriber broker (for federation)
- `topic`: Message topic
- `qos`: QoS level (0, 1, or 2)
- `retained`: Retained flag (true/false)
- `federation`: Federation mode (true/false)
- `role`: Connection role (publisher/subscriber)
- `direction`: Throughput direction (publish/receive)
- `error_type`: Type of error

## Quick Start

```bash
# From local/, use the Makefile targets against the MetalLB Envoy Gateway IPs.
make test-functional
make test-performance
make dummy-bms

# Run full performance benchmarks instead of the e2e smoke profile.
make benchmark-performance

# Direct test runs use the MetalLB Envoy Gateway IPs.
export CSC_BROKER_URL=tcp://172.18.200.1:1883
export CPC1_BROKER_URL=tcp://172.18.201.1:1883
export CPC2_BROKER_URL=tcp://172.18.202.1:1883
go test -v ./tests/performance/

# Publish looping BMS demo data directly against a reachable broker.
go run ./cmd/dummy-bms --broker tcp://172.18.200.1:1883 --csv examples/dsx_exemplar.csv --schema ../../schemas/asyncapi/bms/bms.yaml

# Run specific test
go test -v ./tests/performance/ -run TestThroughputQoS0_Local

# Skip long-running performance tests
go test -short ./tests/...
```

## Dummy BMS Producer

`cmd/dummy-bms` feeds the local CSC MQTT broker with representative BMS traffic
so the demo environment has realistic data for subscribers, dashboards, and
manual integration checks. It is a replay tool, not a synthetic data generator:
the CSV defines the timing and exact messages to publish, and the command keeps
replaying that sequence until it is stopped.

The producer validates each rendered message against the canonical BMS AsyncAPI
schema before it publishes anything. The sample scenario uses
`{{timestamp_ms}}` for readings that need a fresh event timestamp on each pass.

See `examples/dsx_exemplar.csv` for the raw data sample. You can provide
your own sample or edit the sample for custom data.

From the repo root, run `make dummy-bms` after the local environment is
deployed. For direct runs, pass the broker, CSV, and schema paths explicitly.
Authentication options for real clients are covered in
[authentication.md](../../docs/authentication.md); the local dummy-BMS target
uses the no-auth example profile.

### Dummy BMS Scenario CSV

Scenario files must use exactly this header:

```csv
offset,topic,payload
```

Each row is one MQTT publish:

- `offset` is a Go duration such as `0s`, `500ms`, or `2m`. Offsets must be
  non-negative and non-decreasing because they are replayed relative to the
  start of each pass.
- `topic` must be a concrete topic that matches a BMS AsyncAPI channel. For
  BMS-originated sample data, use
  `BMS/v1/PUB/{Value|Metadata}/{objectType}/{pointType}/{tagPath}`. For
  example, `BMS/v1/PUB/Metadata/Rack/RackPower/site-a/row-1/rack-1/power` and
  `BMS/v1/PUB/Value/Rack/RackPower/site-a/row-1/rack-1/power` are valid
  concrete topics. Empty segments and MQTT wildcards (`+`, `#`) are rejected.
- `payload` must be JSON and must validate against the BMS AsyncAPI schema for
  the selected topic. Any topic parameter fields present in the payload must
  match the topic.
- `{{timestamp_ms}}` in the payload is replaced at publish time with the current
  Unix timestamp in milliseconds.
- `Metadata` topics are published retained. `Value` topics are live,
  non-retained publishes.

The command loops the scenario until interrupted. Add `--once` to publish one
pass and exit:

```bash
go run ./cmd/dummy-bms \
  --broker tcp://172.18.200.1:1883 \
  --csv examples/dsx_exemplar.csv \
  --schema ../../schemas/asyncapi/bms/bms.yaml \
  --once
```

To verify messages while `make dummy-bms` is running, subscribe to the CSC Envoy
Gateway from another terminal:

```bash
mosquitto_sub -h 172.18.200.1 -p 1883 -t 'BMS/v1/PUB/#' -v
```

## Development

### Adding a New Test

1. Add test function in `tests/functional/` or `tests/performance/`
2. Follow the existing patterns for test configuration and execution
3. Update test documentation in this README

### Running Against Local Broker

Use this only for client development against a standalone broker. The local Kind
environment uses the MetalLB Envoy Gateway IPs documented in the quick start
above.

```bash
# Start a local MQTT broker (using Docker)
docker run -d -p 1883:1883 eclipse-mosquitto:latest

# Run tests against that standalone broker
MQTT_BROKER=tcp://127.0.0.1:1883 go test -v ./tests/functional/
CSC_BROKER_URL=tcp://127.0.0.1:1883 CPC1_BROKER_URL=tcp://127.0.0.1:1883 go test -v ./tests/performance/
```

## Troubleshooting

### Connection Refused

```
Error: connection refused
```

**Solution:** Ensure the broker is running and accessible:

```bash
# Test connectivity
telnet 172.18.200.1 1883

# Check broker logs
kubectl logs -n event-bus <pod-name>
```

### High Latency

If you see high latency in benchmarks:

1. Check network connectivity
2. Verify broker is not overloaded
3. Check QoS settings (QoS 0 is fastest)
4. Monitor broker resources (CPU, memory)
