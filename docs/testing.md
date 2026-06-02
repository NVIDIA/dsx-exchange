# Validation

## Functional Validation

The functional suites validate event-bus behavior against the local Kind
environment:

- MQTT 3.1.1 protocol compliance
- QoS 0, 1, 2 message delivery
- Retained messages and will messages
- High availability and failover
- Federation between layers (CPC &lt;-&gt; CSC)
- Authentication and authorization (OAuth2, mTLS, NKey)
- Topic-based access control

## Local Benchmark Tooling

The local environment includes MQTT benchmark tooling for smoke runs and
operator-driven benchmark runs. These tests report the observed behavior of the
current deployment; they do not define product targets.

**Prerequisite:** Performance and benchmark targets require MetalLB or an
equivalent LoadBalancer installed by `make -C local setup-infra`. On macOS, start
`docker-mac-net-connect` so the host can reach the MetalLB IPs. Linux hosts
normally reach the Docker bridge IPs directly.

Full benchmark targets are sensitive to local host capacity. When they report
EOFs, readloop stalls, or success-rate misses, capture host CPU and Kubernetes
pod metrics with the run; the smoke targets are the portable local validation
path.

The MQTT performance suite exercises combinations of QoS level, retention, and
deployment topology:

| | QoS 0 | QoS 0 + Retained | QoS 1 | QoS 1 + Retained |
|---|---|---|---|---|
| **Local** (same cluster) | Exercised | Exercised | Exercised | Exercised |
| **Federation** (CPC &lt;-&gt; CSC) | Exercised | Exercised | Exercised | Exercised |

Federation runs are bidirectional: CPC-to-CSC and CSC-to-CPC.

### Reported Metrics

The benchmark tooling reports these measurements:

| Metric | Description |
|--------|-------------|
| Messages published/received | Total message counts |
| Publish latency | Time from publish call to broker acknowledgement |
| End-to-end latency | Time from publish to subscriber receipt |
| Throughput | Messages per second (publish and receive) |
| Active connections | Concurrent client count |

The separate MQTT benchmark suite reports scenario-level measurements:

- Connection rates and peak concurrent connections
- Message throughput (publish and subscribe rates)
- End-to-end latency percentiles (avg, P50, P90, P97, P99)
- Success rates
