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

Performance tests validate throughput under different conditions:

**QoS and Retained Combinations:**

- **QoS 0**: No persistence, target 200k msgs/sec
- **QoS 0 + Retained**: Retained messages without persistence guarantee
- **QoS 1**: With persistence, target 20k msgs/sec
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

- `TestThroughputQoS0_Local`: Target 200k msgs/sec
- `TestThroughputQoS0Retained_Local`: QoS 0 with retained
- `TestThroughputQoS1_Local`: Target 20k msgs/sec (with persistence)
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

### Performance Targets

**REQ-18 Throughput**

- 200,000 msgs/sec QoS 0 (1KB messages, no persistence)
- 20,000 msgs/sec QoS 1 (1KB messages, with persistence)

**REQ-19 Persistence Performance**

- Achieved via QoS 1 tests (same as throughput with QoS 1)
- Retained messages with QoS 0 and QoS 1

**REQ-4, REQ-6 Federation Performance**

- Cross-layer routing (CPC1 <-> CSC)
- Bidirectional throughput validation
- Federation latency overhead measurement

**TODO: REQ-20 Connection Scaling**

- 10,000 concurrent clients per server

**TODO: REQ-32 Message Size**

- Support up to 4MB messages

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
# From local/, use the Makefile targets to create localhost port-forwards.
make test-functional
make test-performance

# Run full performance benchmarks instead of the e2e smoke profile.
make benchmark-performance

# Direct test runs require reachable broker URLs.
export CSC_BROKER_URL=tcp://127.0.0.1:11883
export CPC1_BROKER_URL=tcp://127.0.0.1:11884
go test -v ./tests/performance/

# Run specific test
go test -v ./tests/performance/ -run TestThroughputQoS0_Local

# Skip long-running performance tests
go test -short ./tests/...
```

## Development

### Adding a New Test

1. Add test function in `tests/functional/` or `tests/performance/`
2. Follow the existing patterns for test configuration and execution
3. Update test documentation in this README

### Running Against Local Broker

```bash
# Start a local MQTT broker (using Docker)
docker run -d -p 1883:1883 eclipse-mosquitto:latest

# Set broker URL
export CSC_BROKER_URL=tcp://127.0.0.1:1883

# Run tests
go test -v ./tests/functional/
go test -v ./tests/performance/
```

## Troubleshooting

### Connection Refused

```
Error: connection refused
```

**Solution:** Ensure the broker is running and accessible:

```bash
# Test connectivity
telnet localhost 1883

# Check broker logs
kubectl logs -n event-bus-nats <pod-name>
```

### High Latency

If you see high latency in benchmarks:

1. Check network connectivity
2. Verify broker is not overloaded
3. Check QoS settings (QoS 0 is fastest)
4. Monitor broker resources (CPU, memory)
