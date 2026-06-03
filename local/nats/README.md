# NATS Event Bus Deployment

NATS deployment configuration for the DSX Event Bus evaluation.

For architecture and chart configuration details, see
[deploy/README.md](../../deploy/README.md) and
[docs/architecture.md](../../docs/architecture.md).

## Deployment

### Prerequisites

- Kind clusters created (CSC, CPC-1, CPC-2)
- Helm 4.0+
- kubectl configured with cluster contexts
- Skaffold

### Deploy Complete Local Stack

```bash
# From the repository root
make -C local skaffold-run
```

## Configuration

This folder contains local evaluation overrides in `k8s/`. Chart configuration
is documented in [deploy/README.md](../../deploy/README.md). Auth permissions
are documented in [docs/authentication.md](../../docs/authentication.md).

## Testing

### Test MQTT Connectivity

```bash
# Terminal 1
mosquitto_sub -h 172.18.200.1 -p 1883 -t "csc/#" -q 1

# Terminal 2
mosquitto_pub -h 172.18.200.1 -p 1883 -t "csc/test" -m "hello" -q 1
```

## Performance Tuning

See NATS documentation for performance tuning. Configuration is in `k8s/*/values.yaml`.

## Monitoring

For monitoring configuration and metrics reference, see
[docs/operations.md](../../docs/operations.md).

### Accessing Metrics Locally

Metrics are scraped by the local observability stack. See
[local/infra/README.md](../infra/README.md) for the Prometheus local access
command.

Key NATS metrics:

- `nats_server_in_msgs` / `nats_server_out_msgs` — message throughput
- `nats_server_in_bytes` / `nats_server_out_bytes` — byte throughput
- `nats_server_slow_consumers` — slow consumer count
- `jetstream_stream_messages` / `jetstream_stream_bytes` — JetStream usage

## References

- https://docs.nats.io/
- https://docs.nats.io/running-a-nats-service/configuration/mqtt
- https://docs.nats.io/nats-concepts/jetstream
- https://docs.nats.io/running-a-nats-service/configuration/leafnodes
- https://github.com/nats-io/k8s/tree/main/helm/charts/nats

## Troubleshooting

### Pod Not Starting

```bash
kubectl get events -n event-bus --context kind-csc
kubectl logs -n event-bus <pod-name> --context kind-csc
```

### MQTT Connection Failed

Check MQTT is enabled in configuration. Verify Gateway TCPRoute is configured correctly.

### Federation Not Working

Check leaf node connections and Gateway configuration. Verify topic filtering at Gateway.

### High Memory Usage

Check JetStream stream usage and adjust retention policies as needed.
