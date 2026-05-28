# Deployment

This guide covers deploying the DSX Event Bus into a DSX AI Factory. Each Kubernetes cluster (one CSC, one or more CPCs) runs its own event bus instance. CPC instances federate to the CSC via NATS leaf node connections through a Kubernetes Gateway API controller.

Before starting, ensure all infrastructure prerequisites, secrets, and certificates are provisioned. See [Pre-Deployment](pre-deployment.md) for the full checklist. For day-2 operations, monitoring, and configuration tuning, see [Operations](operations.md).

## Evaluation Install (~10 minutes)

To evaluate DSX Exchange locally without Vault, VSO, or production certificate infrastructure, use the `local/` evaluation framework. This creates Kind clusters and deploys a fully functional event bus:

```bash
make -C local setup-infra      # Kind clusters + MetalLB + Envoy Gateway + cert-manager + Keycloak
make -C local deploy-nats       # Deploy NATS event bus to all clusters
make -C local validate-nats     # Verify connectivity
```

See `local/README.md` for the full set of evaluation targets including functional tests, performance benchmarks, and MQTT client tooling.

If you already have access to a running broker and need to build or test an MQTT
integration application, use the [Integrator Quickstart](integrator-quickstart.md)
instead of this operator deployment flow.

The rest of this page covers the production deployment path.

## Prerequisites

Version-pinned where there is a known compatibility break; see [Pre-Deployment](pre-deployment.md) for details.

- Kubernetes 1.27+ — Gateway API CRDs require this minimum
- Helm 4.0+ — Helm 3 is not supported
- Envoy Gateway 1.5+ or compatible Gateway API controller — TCPRoute/TLSRoute `v1alpha2` APIs
- MetalLB 0.13+ or cloud LoadBalancer — CRD-based config (`IPAddressPool`)
- cert-manager
- Prometheus Operator
- Keycloak or OIDC provider (if using OAuth2)
- Secrets pipeline that materializes Kubernetes Secrets (e.g., Vault with Vault Secrets Operator)

### Resource Requirements (defaults — configurable per deployment)

| Component | Replicas | CPU Request | Memory Request | CPU Limit | Memory Limit |
|-----------|----------|-------------|----------------|-----------|--------------|
| NATS | 3 | 200m | 512Mi | 1000m | 2Gi |
| NATS mTLS | 1 | 100m | 256Mi | 500m | 1Gi |
| Auth Callout | 1 | 10m | 32Mi | 100m | 128Mi |
| NACK | 1 | 10m | 32Mi | 100m | 128Mi |
| Surveyor | 1 | 10m | 32Mi | 100m | 128Mi |

## Install Order

1. Infrastructure (Gateway API controller, MetalLB, cert-manager)
2. Keycloak or OIDC provider (if using OAuth2)
3. Build the auth-callout container image — see [Pre-Deployment](pre-deployment.md#auth-callout-container-image)
4. CSC cluster event bus
5. CPC cluster event buses (connect to CSC via leaf nodes)

## Secrets

All NKey secrets must be provisioned before deploying the event bus. Any secrets pipeline that materializes Kubernetes Secrets works (see [Pre-Deployment — Secrets Pipeline](pre-deployment.md#secrets-pipeline)). Generate keys locally with the provided script:

```bash
# Generate secrets for CSC with CPC IDs 1 and 2
./deploy/scripts/generate-nkeys.sh -c csc 1 2

# Generate secrets for CPC-1
./deploy/scripts/generate-nkeys.sh -c cpc-1
```

See [Authentication](authentication.md) for details on the auth model and required keys.

## Deploying the Event Bus

### CSC Cluster

```bash
helm dependency update ./deploy/nats-event-bus

helm install dsx ./deploy/nats-event-bus \
  -n dsx --create-namespace \
  -f values-common.yaml \
  -f values-csc.yaml
```

CSC values configure the cluster type, list of CPC IDs that will connect, and auth permissions:

```yaml
global:
  eventBus:
    clusterType: csc
    cpcIds: ["1", "2"]
    auth:
      permissions:
        oauth2:
          mqtt-client:
            azp: "mqtt-client"
            account: "CSC"
            permissions:
              pub:
                allow: ["events.>"]
              sub:
                allow: ["events.>"]
        mtls:
          mqtt-client:
            identity: "CN=mqtt-client.csc"
            account: "CSC"
        noauth:
          account: "CSC"
```

If using OAuth2, configure the JWKS endpoint and issuer so the auth-callout can validate tokens. Without these, OAuth2 connections are silently rejected:

```yaml
auth-callout:
  serviceConfig:
    jwks:
      url: "https://keycloak.example.com/realms/event-bus/protocol/openid-connect/certs"
      issuer: "https://keycloak.example.com/realms/event-bus"
```

The CSC also needs CPC leaf user public keys to authorize incoming leaf connections. The chart generates auth-callout env refs from `global.eventBus.cpcIds`; create matching `nats-leaf-cpc-{id}` secrets with a `pubkey` key.

### CPC Clusters

```bash
helm install dsx ./deploy/nats-event-bus \
  -n dsx --create-namespace \
  -f values-common.yaml \
  -f values-cpc.yaml \
  -f values-cpc-1.yaml    # cluster-specific overrides
```

CPC values set the cluster type, cluster ID, CSC endpoint, and cross-layer routing:

```yaml
global:
  eventBus:
    clusterType: cpc
    clusterId: "1"
    cscEndpoint: "nats://csc-gateway:7422"
    crossLayer:
      cscExports: ["broadcast.>"]
      cscPrefixedExports: ["command.>"]
      cpcExports: ["sensor.>"]
```

## Cross-Layer Routing

Cross-layer settings control which topics are copied between the CPC local topic space and the CSC unified topic space:

| Direction | Config Key | Behavior |
|-----------|-----------|----------|
| CPC to CSC | `cpcExports` | Copied with `cpc.{id}.` prefix added |
| CSC to all CPCs | `cscExports` | Broadcast to all CPC topic spaces |
| CSC to specific CPC | `cscPrefixedExports` | `cpc.{id}.` prefix stripped on delivery |

## Gateway Configuration

Configure Gateway API listeners and TCPRoute/TLSRoute resources for external access:

```yaml
global:
  eventBus:
    gateway:
      routes:
        mqtt:
          enabled: true
          kind: TCPRoute
          gatewayName: event-bus-gateway
          gatewayNamespace: event-bus
          sectionName: mqtt
        natsClient:
          enabled: true
          kind: TCPRoute
          gatewayName: event-bus-gateway
          gatewayNamespace: event-bus
          sectionName: nats-client
        natsLeafnode:
          enabled: true
          kind: TCPRoute
          gatewayName: event-bus-gateway
          gatewayNamespace: event-bus
          sectionName: nats-leafnode
        mqttMtls:
          enabled: true
          kind: TLSRoute
          gatewayName: event-bus-gateway
          gatewayNamespace: event-bus
          sectionName: mqtt-mtls
```

## Validation

```bash
# Check all pods are running
kubectl get pods -n dsx

# Test MQTT connectivity
mqttx pub -h $GATEWAY_IP -p 1883 -t test/topic -m "hello" \
  -u "oauthtoken" -P "$ACCESS_TOKEN" -V 3.1.1

# Test NATS connectivity
nats pub test.topic "hello" --server nats://$GATEWAY_IP:4222
```

## Monitoring

NATS Surveyor exports Prometheus metrics from the NATS cluster. The mTLS cluster's SYS account is federated to the main cluster via leaf node, enabling centralized monitoring of both instances.

| Signal | Tool | Endpoint |
|--------|------|----------|
| NATS metrics | Surveyor | `:7777/metrics` |
| Auth-callout metrics | Prometheus client | `:9090/metrics` |
| Tracing | OpenTelemetry | OTLP gRPC `:4317` |

Key NATS metric families:

- `nats_core_*` — server metrics (connections, messages, bytes)
- `nats_account_*` — per-account metrics
- `nats_jetstream_*` — stream and consumer metrics
