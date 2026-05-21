# Validated Capabilities

The DSX Event Bus is validated across three dimensions: protocol compliance, performance characteristics, and benchmark scenarios.

## Functional Validation

The following capabilities are validated through automated test suites:

- MQTT 3.1.1 protocol compliance
- QoS 0, 1, 2 message delivery
- Retained messages and will messages
- High availability and failover
- Federation between layers (CPC &lt;-&gt; CSC)
- Authentication and authorization (OAuth2, mTLS, NKey)
- Topic-based access control

## Performance Characteristics

### Throughput Targets

| Capability | Target | Condition |
|------------|--------|-----------|
| Throughput | 200,000 msgs/sec | QoS 0, 1KB messages, per bus server |
| Persistence | 20,000 msgs/sec | QoS 1, 1KB messages |
| Connections | 10,000 concurrent | Per server |
| Message Size | Up to 4MB | Per message |

### Test Matrix

Performance is validated across 8 combinations of QoS level, retention, and deployment topology:

| | QoS 0 | QoS 0 + Retained | QoS 1 | QoS 1 + Retained |
|---|---|---|---|---|
| **Local** (same cluster) | Validated | Validated | Validated | Validated |
| **Federation** (CPC &lt;-&gt; CSC) | Validated | Validated | Validated | Validated |

Federation tests run bidirectionally (CPC-to-CSC and CSC-to-CPC), with a latency overhead comparison against local-only delivery.

### Performance Metrics

The following metrics are captured during performance validation:

| Metric | Description |
|--------|-------------|
| Messages published/received | Total message counts |
| Publish latency | Time from publish call to broker acknowledgement |
| End-to-end latency | Time from publish to subscriber receipt |
| Throughput | Messages per second (publish and receive) |
| Active connections | Concurrent client count |

## Benchmark Scenarios

The DSX Event Bus is benchmarked using custom MQTT scenarios built for this deployment. All scenarios use MQTT 3.1.1 with QoS 1.

| Scenario | Description |
|----------|-------------|
| Connection (10k) | 10,000 clients connect within 100 seconds |
| Fan-out (1k) | 1 publisher, 1,000 subscribers, 1 msg/sec |
| Point-to-point (1k) | 1,000 publishers, 1,000 subscribers, 1 msg/sec each |
| Fan-in (1k) | 1,000 publishers, 5 subscribers, 1 msg/sec each |

### Reported Metrics

- Connection rates and peak concurrent connections
- Message throughput (publish and subscribe rates)
- End-to-end latency percentiles (avg, P50, P90, P97, P99)
- Success rates

## Requirements Coverage

| Requirement | Validation |
|:------------|:-----------|
| MQTT Protocol Support | Functional test suite |
| High Availability | Failover test suite |
| Horizontal Scalability | Performance scale-out tests |
| Federation | Functional + performance (bidirectional) |
| Authentication | Functional (OAuth2, mTLS, NKey, noauth) |
| Authorization | Functional (topic-level ACLs) |
| Throughput (200k msgs/sec) | Performance benchmarks |
| Persistence (20k msgs/sec) | Performance benchmarks |
| Client Count (10k) | Connection benchmark scenario |
