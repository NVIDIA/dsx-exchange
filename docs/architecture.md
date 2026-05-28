# Architecture

## DSX Exchange

A gigawatt-scale AI factory is fifty or more independent systems — GPU clusters, building management systems, power distribution units, cooling infrastructure, grid interfaces, network switches — each designed by a different vendor, none designed to talk to each other. AI workloads make this worse: a training job launch swings rack power from 20% to 95% in seconds, creating thermal and electrical transients that existing BMS controllers were not built to handle.

DSX Exchange is the integration layer within DSX OS that connects these systems. It gives the factory four capabilities:

- **Legible** — every signal (power, cooling, grid, compute, network) readable in one place through a common schema
- **Coordinated** — control loops close in real time: DPS sees power headroom, NICo reacts to BMS leak events, the scheduler responds to grid curtailment via DSX Flex
- **Agent-operable** — agents observe factory state through the MCP Gateway and act through constrained, audited control surfaces
- **Auditable** — every publisher authenticated, every subscriber authorized, every action logged with caller identity

DSX Exchange consists of four components:

| Component | What It Is |
|-----------|-----------|
| DSX Event Bus | NATS with MQTT 3.1.1, HA clustering, leaf-node federation |
| AsyncAPI Schema | Formal topic definitions and payload contracts per service team |
| Auth-Callout Service | OAuth2/mTLS/NKey authentication with topic-level ACLs |
| MCP Interface Layer | MCP Gateway aggregating read-only MCP servers |

## DSX Event Bus

The DSX Event Bus is a [NATS](https://nats.io/)-based messaging platform that provides MQTT 3.1.1 connectivity, [JetStream](https://docs.nats.io/nats-concepts/jetstream) persistence, and multi-cluster [leaf node federation](https://docs.nats.io/running-a-nats-service/configuration/leafnodes) across the AI Factory topology. It is the transport that makes the rest of DSX Exchange possible.

Concrete signal paths it enables today:

- **BMS -> DPS**: real-time power telemetry so DPS can close the MaxLPS dynamic power allocation loop (recovering up to 40% stranded capacity)
- **BMS -> NICo**: coolant leak events so NICo can cordon nodes and migrate workloads in seconds, not minutes
- **Grid -> DSX Flex -> Scheduler**: demand-response curtailment signals so the factory can shed load within seconds
- **GPU telemetry -> thermal optimization agents**: real-time thermal data so predictive agents can pre-adjust cooling setpoints before temperature spikes

## System Overview

![Event Bus Architecture](assets/images/event-bus-architecture.png)

PlantUML sources are in `assets/diagrams/`.

```mermaid
flowchart TB
    subgraph CSC[CSC Cluster — Common Services]
        CSC_GW[Gateway]
        CSC_NATS[NATS Cluster ×3]
        CSC_MTLS[NATS mTLS ×1]
        CSC_AUTH[Auth Callout]
        CSC_NACK[NACK Controller]
        CSC_SURV[Surveyor]
        KC[Keycloak]

        CSC_GW -->|TCP 1883, 4222, 7422| CSC_NATS
        CSC_GW -->|TLS 8883 passthrough| CSC_MTLS
        CSC_AUTH -->|auth requests| CSC_NATS
        CSC_MTLS -->|leaf node| CSC_NATS
        CSC_NACK -->|stream mgmt| CSC_NATS
        CSC_SURV -->|SYS account| CSC_NATS
    end

    subgraph CPC1[CPC-1 Cluster]
        CPC1_GW[Gateway]
        CPC1_NATS[NATS Cluster ×3]
        CPC1_MTLS[NATS mTLS ×1]
        CPC1_AUTH[Auth Callout]

        CPC1_GW --> CPC1_NATS
        CPC1_GW --> CPC1_MTLS
        CPC1_AUTH --> CPC1_NATS
        CPC1_MTLS -->|leaf node| CPC1_NATS
    end

    subgraph CPCN[CPC-N Cluster]
        CPCN_GW[Gateway]
        CPCN_NATS[NATS Cluster ×3]
        CPCN_AUTH[Auth Callout]

        CPCN_GW --> CPCN_NATS
        CPCN_AUTH --> CPCN_NATS
    end

    ExtClient[External Clients] -->|MQTT / NATS| CSC_GW
    ExtClient -->|MQTT / NATS| CPC1_GW
    ExtClient -->|MQTT / NATS| CPCN_GW

    CPC1_NATS -->|leaf node :7422| CSC_GW
    CPCN_NATS -->|leaf node :7422| CSC_GW

    CSC_AUTH -.->|JWKS| KC
    CPC1_AUTH -.->|JWKS| KC
```

## Cluster Topology

The AI Factory deploys one Common Services Cluster (CSC) and one or more Control Plane Clusters (CPCs). Each cluster runs an independent NATS event bus instance. CPCs federate to the CSC via NATS leaf node connections through a Kubernetes Gateway API-compatible controller.

Each cluster is a separate Kubernetes cluster with overlapping internal networks. All inter-cluster communication flows through Gateway LoadBalancer services with unique external IPs. The cluster operator provides the Gateway API implementation (Envoy Gateway is used in the reference deployment but any conformant controller works).

### Components Per Cluster (defaults)

Replica counts are defaults for a reference deployment. Actual values are configurable and depend on the scale of the data center deployment.

| Component | Default Replicas | Purpose |
|-----------|-----------------|---------|
| NATS (main) | 3 | MQTT/NATS clients, JetStream persistence |
| NATS (mTLS) | 1 | mTLS-authenticated MQTT endpoint, optional |
| Auth Callout | 1 | Authenticates connections for both NATS instances |
| NACK | 1 | JetStream controller for declarative stream management |
| Surveyor | 1 | Prometheus metrics exporter |

## Topic Spaces

The event bus provides two types of topic space:

**CSC Topic Space** is a unified namespace that spans all clusters. Messages published here are visible to subscribers in every cluster. Clients see full topic paths including CPC prefixes (e.g., `cpc.1.telemetry.temp`).

**CPC Topic Spaces** are per-cluster local namespaces. Messages stay within that CPC unless the topic is configured for cross-layer routing. Clients use simple names without prefixes (e.g., `telemetry/temp`).

### Cross-Layer Routing

Detailed routing diagrams for every publish scenario (local, cross-space, federated) are in `assets/diagrams/routing/`.

Cross-layer configuration controls which topics are copied between CPC local and CSC unified topic spaces:

| Direction | Config Key | Behavior |
|-----------|-----------|----------|
| CPC -> CSC | `cpcExports` | Copied with `cpc.{id}.` prefix added |
| CSC -> all CPCs | `cscExports` | Broadcast to all CPC topic spaces |
| CSC -> specific CPC | `cscPrefixedExports` | `cpc.{id}.` prefix stripped on delivery |

Routing is enforced by NATS account import/export rules generated from `global.eventBus.crossLayer` Helm values. The Gateway controller does no topic filtering — it passes TCP traffic transparently.

**The three lists must not overlap.** A subject pattern that appears in more than one list creates cyclic NATS imports that crash the NATS pod (CrashLoopBackOff) with no user-facing error at install time.

## MQTT Support

NATS provides native MQTT 3.1.1 support. MQTT topic separators (`/`) are mapped to NATS subject tokens (`.`) — for example, the MQTT topic `telemetry/temp` becomes the NATS subject `telemetry.temp`. This mapping is transparent to MQTT clients.

Supported QoS levels:

- **QoS 0** — at most once (fire and forget)
- **QoS 1** — at least once (acknowledged delivery, backed by JetStream)
- **QoS 2** — exactly once (backed by JetStream)

## Networking

### Exposed Ports

| Port | Protocol | Service | Description |
|------|----------|---------|-------------|
| 1883 | MQTT | nats | Standard MQTT 3.1.1 (TLS terminated at Envoy) |
| 4222 | NATS | nats | NATS client connections |
| 7422 | NATS | nats | Leaf node connections (cross-cluster federation) |
| 8883 | MQTT | nats-mtls | mTLS MQTT 3.1.1 (TCP passthrough, TLS at NATS pod) |

### Layer Isolation

- **Network isolation**: separate Kubernetes clusters with overlapping internal subnets
- **Gateway enforcement**: inter-cluster traffic flows only through Gateway API LoadBalancers
- **Topic filtering**: NATS account configuration enforces which subjects cross layer boundaries

## Persistence

JetStream provides message persistence. MQTT QoS 1 messages and retained messages are stored in JetStream streams managed declaratively by the NACK controller.

- The built-in CPC account uses local JetStream for its own topic space and domain mapping to the CSC for cross-layer persistence; extra accounts keep their own account configuration on each cluster and are bridged by leaf nodes.
- Stream configuration (storage type, replicas, max bytes) is set via `mqttStreams` Helm values

## Observability

| Signal | Tool | Endpoint |
|--------|------|----------|
| Metrics (NATS) | Surveyor | `:7777/metrics` (Prometheus) |
| Metrics (auth-callout) | Prometheus client | `:9090/metrics` |
| Tracing | OpenTelemetry -> Tempo | OTLP gRPC `:4317` |
| Dashboards | Grafana | Prometheus + Tempo data sources |

The mTLS cluster's SYS account is federated to the main cluster via leaf node, enabling centralized monitoring of both NATS instances from a single Surveyor.
