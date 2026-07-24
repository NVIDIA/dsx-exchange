# Infrastructure Setup

This directory contains the infrastructure configuration for the DSX Event Bus evaluation environment.

## Overview

The infrastructure consists of:

- one Kind cluster by default, or one cluster per site with `MULTI_CLUSTER=1`
- MetalLB for LoadBalancer services
- Envoy Gateway controllers
- Metrics Server for resource metrics (CPU/memory)
- Keycloak for OAuth2 authentication (development)
- Prometheus for ServiceMonitor-backed metrics

## Quick Start

On macOS, install and start `docker-mac-net-connect` before running local tests
from the host. Linux hosts normally reach the Docker bridge IPs directly. See
[local/README.md](../README.md#macos-tweaks).

From the repository root:

```bash
make -C local test
```

See [local/README.md](../README.md) for deploy-only, dev, test, and benchmark
targets.

## Topologies

The default `kind-dsx-exchange` topology runs all logical sites on one physical
cluster. Stable namespaces keep site resources isolated:

- CSC: `csc-gateway`, `csc-event-bus`
- CPC-1: `cpc-1-gateway`, `cpc-1-event-bus`
- CPC-2: `cpc-2-gateway`, `cpc-2-event-bus`

`MULTI_CLUSTER=1` uses `kind-csc`, `kind-cpc-1`, and `kind-cpc-2`. It deploys
the same site namespaces, chart values, Gateway resources, and fixed addresses,
but installs cluster-scoped controllers once in each physical cluster.

## MetalLB Setup

MetalLB provides LoadBalancer service type support in Kind clusters.

**Why MetalLB?**
MetalLB provides stable external IPs from the Docker network. CPC leaf
connections use the CSC Envoy address in both topologies, so the default
single-cluster tests still exercise the Gateway path.

**Gateway IPs (on Docker network 172.18.0.0/16):**

- CSC: 172.18.200.1
- CPC-1: 172.18.201.1
- CPC-2: 172.18.202.1

These IPs are **separate and non-overlapping**, allowing logical sites to use
the same Envoy addresses in either topology.

**Configuration:**

```yaml
apiVersion: metallb.io/v1beta1
kind: IPAddressPool
metadata:
  name: csc-envoy-pool
  namespace: metallb-system
spec:
  addresses:
    # Reserved for the shared Envoy Gateway.
    - 172.18.200.1/32
  autoAssign: false
---
apiVersion: metallb.io/v1beta1
kind: IPAddressPool
metadata:
  name: csc-default-pool
  namespace: metallb-system
spec:
  addresses:
    # Available for future LoadBalancer services.
    - 172.18.200.2-172.18.200.254
---
apiVersion: metallb.io/v1beta1
kind: L2Advertisement
metadata:
  name: csc-l2-advert
  namespace: metallb-system
spec:
  ipAddressPools:
    - csc-envoy-pool
    - csc-default-pool
  interfaces:
    - eth0
```

## Envoy Gateway Setup

Envoy Gateway provides modern, high-performance HTTP/HTTPS ingress and API gateway capabilities.

**Usage:**

Each site owns a `shared-gateway` in its stable `*-gateway` namespace. It
provides TCP listeners for NATS (ports 1883, 4222, 7422), a TLS
passthrough listener for mTLS MQTT (port 8883), and an HTTP listener (port 80)
for Keycloak.

Example HTTPRoute for Keycloak:

```yaml
apiVersion: gateway.networking.k8s.io/v1
kind: HTTPRoute
metadata:
  name: keycloak
  namespace: keycloak
spec:
  parentRefs:
    - name: shared-gateway
      namespace: csc-gateway
  rules:
    - matches:
        - path:
            type: PathPrefix
            value: /
      backendRefs:
        - name: keycloak-service
          port: 8080
```

## cert-manager

cert-manager provides automatic certificate management for TLS certificates.
It is installed once per physical cluster.

## Metrics Server

Kubernetes Metrics Server provides resource metrics (CPU/memory) for nodes and pods, enabling `kubectl top` commands and Horizontal Pod Autoscaling (HPA).

**Usage:**

```bash
# View node metrics
kubectl top nodes --context kind-dsx-exchange

# View pod metrics
kubectl top pods -n csc-event-bus --context kind-dsx-exchange
```

## Keycloak (OAuth2 Authentication)

Keycloak provides OAuth2/OpenID Connect authentication for testing the event
bus auth callout service. A single instance attaches to the CSC Gateway, and
all sites access it through `172.18.200.1`.

**Configuration:**

- **Realm**: `event-bus` (auto-imported at startup via ConfigMap `keycloak-realm-import`)
- **Grant Type**: Client Credentials (machine-to-machine authentication)
- **Scope**: `mqtt` (required for MQTT access)
- **Clients** (service accounts with client credentials enabled, shared across all sites):
  - `mqtt-client` / `mqtt-client-secret` (full access to test topics)
  - `mqtt-publisher` / `mqtt-publisher-secret` (publish only)
  - `mqtt-subscriber` / `mqtt-subscriber-secret` (subscribe only)

**Access:**

Keycloak is exposed via Envoy Gateway HTTPRoute on port 80 at the CSC cluster's MetalLB LoadBalancer IP: `172.18.200.1`. On macOS, keep `docker-mac-net-connect` running so the host can reach this address. Linux hosts normally reach the Docker bridge IPs directly.

```bash
# Verify Keycloak from the host
curl http://172.18.200.1/realms/event-bus/.well-known/openid-configuration
```

**Token Endpoint (all sites):**

- `http://172.18.200.1/realms/event-bus/protocol/openid-connect/token`

**JWKS Endpoint (used by auth-callout in all sites):**

- `http://172.18.200.1/realms/event-bus/protocol/openid-connect/certs`

**Access Keycloak Admin Console:**

Open `http://172.18.200.1/admin/master/console/`.

Admin credentials: `admin/admin`.

**Testing:**

```bash
# Obtain a token using client credentials grant
curl -X POST "http://172.18.200.1/realms/event-bus/protocol/openid-connect/token" \
  -H 'Content-Type: application/x-www-form-urlencoded' \
  -d 'grant_type=client_credentials' \
  -d 'client_id=mqtt-client' \
  -d 'client_secret=mqtt-client-secret' \
  -d 'scope=mqtt'
```

**Architecture:**

- Single Keycloak instance behind the CSC Gateway
- All sites access via external IP (172.18.200.1)
- Simplified configuration with shared OAuth2 clients
- Consistent authentication across all sites

**Note:** This is a minimal development setup using:

- H2 in-memory database (no persistence)
- HTTP only (no TLS)
- Single replica
- Not suitable for production

## Prometheus

The local stack installs a lightweight kube-prometheus-stack once per physical
cluster.

**Components:**

- Prometheus Operator
- Prometheus Server

**Access Prometheus:**

```bash
# Port-forward to Prometheus
kubectl port-forward -n monitoring svc/prometheus-kube-prometheus-prometheus 9090:9090 --context kind-dsx-exchange

# Open http://localhost:9090
```

## Network Architecture

```plantuml
@startuml network-architecture

skinparam componentStyle rectangle
skinparam backgroundColor white

package "Host Machine" {

    package "CSC Cluster (Common Services)" as csc {
        component "Envoy\nGateway" as csc_gw
        note left of csc_gw
            Internal:
            - 10.244.0.0/16 (pods)
            - 10.96.0.0/12 (services)

            External (via MetalLB):
            - Envoy: 172.18.200.1
            - Other LB: 172.18.200.2-.254
        end note
    }

    package "CPC Cluster 1 (Control Plane)" as cpc1 {
        component "Envoy\nGateway" as cpc1_gw
        note left of cpc1_gw
            Internal:
            - 10.244.0.0/16 (pods)
            - 10.96.0.0/12 (services)

            External (via MetalLB):
            - Envoy: 172.18.201.1
            - Other LB: 172.18.201.2-.254
        end note
    }

    package "CPC Cluster 2..N (Control Plane)" as cpc2 {
        component "Envoy\nGateway" as cpc2_gw
        note left of cpc2_gw
            Internal:
            - 10.244.0.0/16 (pods)
            - 10.96.0.0/12 (services)

            External (via MetalLB):
            - Envoy: 172.18.202.1
            - Other LB: 172.18.202.2-.254
        end note
    }
}

note bottom
    Docker Network: 172.18.0.0/16

    MetalLB IPs:
    - CSC Envoy: 172.18.200.1
    - CPC-1 Envoy: 172.18.201.1
    - CPC-2 Envoy: 172.18.202.1
end note

cpc1_gw --> csc_gw : LoadBalancer\nservices
cpc2_gw --> csc_gw : LoadBalancer\nservices

@enduml
```

**Key Design Points:**

The default topology validates namespace isolation, routing, chart wiring,
reconciliation, caching, and watchers in one cluster. It does not prove
cross-cluster network or failure isolation.

The opt-in multi-cluster topology additionally validates:

1. **Overlapping Internal Networks**: All physical clusters use the same internal address space (10.244.0.0/16 for pods, 10.96.0.0/12 for services).

2. **Gateway-Only Communication**: Clusters are completely isolated. All inter-cluster communication flows through Envoy Gateway LoadBalancer services with unique external IPs.

3. **Network Isolation**: Each cluster is a separate Kind cluster on the same Docker network but with isolated internal networking. They cannot directly route to each other's pod or service IPs.

4. **External Access**: Each site uses its documented `.1` MetalLB address in both topologies.

5. **Federation Model**: Event bus federation happens via:
   - CPC -> CSC: MQTT bridge through Envoy Gateway LoadBalancer

## Topology Awareness

Worker nodes are labeled with topology zones for anti-affinity:

- `topology.kubernetes.io/zone=zone-a`
- `topology.kubernetes.io/zone=zone-b`
- `topology.kubernetes.io/zone=zone-c`

**Usage in Deployments:**

```yaml
spec:
  affinity:
    podAntiAffinity:
      requiredDuringSchedulingIgnoredDuringExecution:
      - labelSelector:
          matchExpressions:
          - key: app
            operator: In
            values:
            - nats
        topologyKey: topology.kubernetes.io/zone
```

This ensures high availability by spreading pods across different zones.

## Resource Requirements

**Minimum:**

- CPU: 4 cores
- Memory: 8 GB
- Disk: 20 GB

**Recommended:**

- CPU: 8 cores
- Memory: 16 GB
- Disk: 50 GB

**Per Physical Cluster:**

- Control plane: ~1 CPU, ~2 GB RAM
- Each worker: ~500m CPU, ~1 GB RAM

## Troubleshooting

### Cluster Creation Fails

```bash
# Check Docker resources
docker system info

# Increase Docker Desktop resources:
# Settings -> Resources -> Advanced
# - CPUs: 4+
# - Memory: 8GB+

# Clean up and retry
make -C local clean
make -C local setup-clusters
```

### MetalLB Not Working

```bash
# Check MetalLB pods
kubectl get pods -n metallb-system --context kind-dsx-exchange

# Check logs
kubectl logs -n metallb-system -l app=metallb --context kind-dsx-exchange

# Verify IP pools
kubectl get ipaddresspools -n metallb-system --context kind-dsx-exchange
```

### Envoy Gateway Not Working

```bash
# Check Envoy Gateway controller
kubectl get pods -n envoy-gateway-system --context kind-dsx-exchange

# Check Gateway resources
kubectl get gateway -A --context kind-dsx-exchange
kubectl get httproute -A --context kind-dsx-exchange

# Check Gateway status
kubectl describe gateway shared-gateway -n csc-gateway --context kind-dsx-exchange

# Get LoadBalancer IP from Gateway resource
GATEWAY_IP=$(kubectl get gateway shared-gateway -n csc-gateway --context kind-dsx-exchange -o jsonpath='{.status.addresses[0].value}')
echo "Gateway IP: $GATEWAY_IP"

# Test gateway HTTP listener
curl http://${GATEWAY_IP}/
```

### Keycloak Not Working

```bash
# Check Keycloak pods
kubectl get pods -n keycloak --context kind-dsx-exchange

# Check logs
kubectl logs -n keycloak -l app.kubernetes.io/name=keycloak --context kind-dsx-exchange

# Check realm import ConfigMap. The import key is realm-event-bus.json and the
# Keycloak realm inside that file is event-bus.
kubectl get configmap keycloak-realm-import -n keycloak --context kind-dsx-exchange -o yaml

# Test token endpoint via external IP using client credentials
curl -X POST "http://172.18.200.1/realms/event-bus/protocol/openid-connect/token" \
  -H 'Content-Type: application/x-www-form-urlencoded' \
  -d 'grant_type=client_credentials' \
  -d 'client_id=mqtt-client' \
  -d 'client_secret=mqtt-client-secret' \
  -d 'scope=mqtt'
```

### Prometheus Not Scraping

```bash
# Check ServiceMonitor resources
kubectl get servicemonitor -A --context kind-dsx-exchange

# Check Prometheus targets
# Access Prometheus UI and check Status -> Targets

# Verify service labels match ServiceMonitor selector
kubectl get svc -n csc-event-bus -o yaml --context kind-dsx-exchange
```

## Cleanup

```bash
# Delete the active topology
make -C local clean

# Or delete individually
kind delete cluster --name dsx-exchange
kind delete cluster --name csc
kind delete cluster --name cpc-1
kind delete cluster --name cpc-2
```

## Next Steps

After the local stack is ready:

1. Reconcile the local stack after config or image changes:

   ```bash
   make -C local skaffold-run
   ```

2. Run tests:

   ```bash
   make -C local test
   ```

3. Inspect Prometheus targets:

   ```bash
   kubectl port-forward -n monitoring svc/prometheus-kube-prometheus-prometheus 9090:9090 --context kind-dsx-exchange
   ```
