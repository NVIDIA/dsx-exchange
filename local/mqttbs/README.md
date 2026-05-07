# MQTT Benchmark Suite (mqttbs)

Broker-agnostic MQTT benchmark tool implementing the [Open MQTT Benchmark Suite](https://github.com/emqx/mqttbs) specification.

## Build

```bash
go build -o mqttbs ./cmd/mqttbs
```

## Usage

```bash
# List scenarios
./mqttbs list

# Run a scenario
./mqttbs run connection-10k --broker tcp://localhost:1883

# Run with authentication
./mqttbs run fanout-1k --broker tcp://broker:1883 --username user --password pass

# Run all Basic scenarios
./mqttbs run basic-suite --broker tcp://localhost:1883
```

## Scenarios

| Scenario | Description |
|----------|-------------|
| `connection-10k` | 10,000 clients connect within 100 seconds |
| `fanout-1k` | 1 publisher -> 1,000 subscribers, 1 msg/sec |
| `p2p-1k` | 1,000 publishers -> 1,000 subscribers, 1 msg/sec each |
| `fanin-1k` | 1,000 publishers -> 5 subscribers, 1 msg/sec each |
| `basic-suite` | Run all above scenarios sequentially |

All scenarios use MQTT 3.1.1 with QoS 1.

## Metrics

- Connection rates and concurrent connections
- Message throughput (publish/subscribe rates)
- End-to-end latency (avg, P50, P90, P97, P99)
- Success rates

## Reports

Results saved to `./results/` in JSON and text formats:

- `report-<scenario>-<timestamp>.json`
- `report-<scenario>-<timestamp>.txt`
