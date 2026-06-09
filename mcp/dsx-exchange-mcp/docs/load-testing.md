<!--
Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
SPDX-License-Identifier: Apache-2.0
-->

# DSX Exchange MCP Load Testing

This note explains how to reproduce the current load-test methodology and
records the stable findings from local gateway-backed experiments. Raw report
bundles are intentionally not committed; they belong under ignored `reports/`.

## What The Harness Tests

`cmd/dsx-exchange-mcp-load` creates many independent MCP clients. Each client
performs its own MCP initialize flow, keeps its own `Mcp-Session-Id`, lists
tools, and then runs one workload scenario. This is not an LLM test; it is a
protocol and backend-capacity test.

Scenarios:

| Scenario | Purpose |
| --- | --- |
| `discovery` | Exercise schema tools only: `find_topics` and `describe_topic`. |
| `schema-resources` | Exercise MCP resources: `resources/list` and `resources/read`. |
| `bounded-read` | Exercise broker-facing bounded reads: retained reads and short live subscribes. |
| `watch` | Start, read, status-check, and stop background watches. |
| `watch-hold` | Start watches and hold them open to measure startup and active-watch pressure. |
| `watch-status-hold` | Start watches, then poll aggregated `subscription_status`. |
| `sticky-check` | Verify a subscription can be read/statused/stopped on the same MCP session. |
| `mixed` | Mix schema tools, bounded MQTT tools, and background watches. |

`deploy/loadtest/run-kind-load-experiment.sh` wraps the load binary for local
Kind/gateway experiments. It records the manifest, image IDs, token TTL
metadata, cluster state before/after, JSON/TXT/CSV reports, per-operation
latency, and per-operation error attribution.

## Reproduction Requirements

The load harness depends on systems outside this MCP repo. Before running it,
the operator needs:

- a reachable MCP gateway `/mcp` endpoint
- the `dsx-exchange-mcp` backend deployed behind that gateway
- stateful MCP session routing enabled when testing background watches
- backend configuration for the target Event Bus MQTT endpoint through Helm
  `natsURL`
- broker username configured through Helm `mqtt.username`
- broker TLS trust configured through Helm `mqtt.tls.caCertSecret.name/key`
- broker TLS server name configured through Helm `mqtt.tls.serverName` when the
  certificate requires it
- a fresh caller JWT from the approved secret manager or Vault flow
- the JWT available to the load job as a Kubernetes secret named
  `dsx-exchange-mcp-load-token` with key `bearer`
- the MCP backend image and load-generator image available to the cluster

Do not commit tokens, CA material, cluster snapshots, local endpoint names, or
raw generated report bundles.

## Build And Run Path

Start from the MCP module root:

```sh
make sync-specs
make image
make load-image
```

`make image` builds the backend image as `dsx-exchange-mcp:dev`.
`make load-image` builds the load-generator image as
`dsx-exchange-mcp-load:dev`. Make those images available to the local cluster or
registry using the repo's existing deployment flow.

After the gateway, backend, broker, CA trust, and load token secret are in
place, run the reusable wrapper:

```sh
deploy/loadtest/run-kind-load-experiment.sh
```

The wrapper creates a Kubernetes Job for the load generator, applies the
selected gateway rate-limit helper when requested, records backend and load
image IDs, captures cluster state, and writes the report bundle under ignored
`reports/`.

## Setup Checklist

Use this checklist before treating load-test failures as MCP bugs:

| Check | Expected state |
| --- | --- |
| Gateway endpoint | MCP clients can reach the gateway `/mcp` endpoint. |
| Gateway bearer passthrough | The caller bearer reaches `dsx-exchange-mcp` as `Authorization`. |
| Stateful routing | Calls with the same `Mcp-Session-Id` route to the same backend pod. |
| Broker endpoint | Backend `NATS_URL` points at the intended MQTT endpoint. |
| Broker username | Backend `MQTT_USERNAME` matches the broker OAuth profile. |
| Broker CA | Backend has a mounted CA file and `MQTT_TLS_CA_FILE` points to it. |
| TLS server name | Backend server-name override matches the broker certificate when required. |
| Load JWT secret | Load namespace has `dsx-exchange-mcp-load-token` with data key `bearer`. |
| JWT freshness | Token TTL is long enough for the full experiment. |
| Topic ACLs | The load topics are authorized for the caller identity. |

If `discovery` passes but `bounded-read`, `watch`, or `mixed` fail, the next
checks are bearer freshness, broker CA/server-name settings, topic ACLs, broker
availability, and MQTT admission limits.

## Important Knobs

Record these for every run so the result is reproducible:

| Knob | Meaning |
| --- | --- |
| `SCENARIO` | Workload shape, such as `discovery`, `mixed`, or `watch-status-hold`. |
| `SESSION_SWEEP` / `SESSIONS` | Concurrent MCP client/session counts. |
| `STARTUP_RAMP` | Time window used to spread client startup. `0s` means an instant startup burst. |
| `DURATION` | Total wall-clock runtime after the load job starts. |
| `GATEWAY_RPS` | Gateway tenant rate-limit setting used during the run. |
| `CLIENT_RPS` | Load-generator request rate limit. |
| `BACKEND_REPLICAS` | Number of `dsx-exchange-mcp` backend pods. |
| `BACKEND_CONNECT_TIMEOUT_S` | Backend MQTT connect timeout. |
| `BACKEND_SUBSCRIBE_TIMEOUT_S` | Backend MQTT subscribe timeout. |
| `BACKEND_COLLECT_MAX_CONCURRENT` | Per-pod admission limit for bounded MQTT collectors. |
| `BACKEND_WATCH_START_MAX_CONCURRENT` | Per-pod admission limit for watch startup. |
| `TOPIC` / `RETAINED_TOPIC` | Allowed live and retained topic filters used by broker-facing scenarios. |
| `RESET_BACKEND` | Whether the backend is restarted before the run. |

Ramp and reset are important. A zero-ramp run measures thundering-herd startup
behavior. A ramped run is closer to organic production growth and makes it
easier to separate steady-state capacity from burst admission failures. Resetting
the backend before a run makes startup cost visible and avoids carrying state
from an earlier experiment.

## Findings From Current Experiments

These findings came from local Kind/gateway experiments using 100, 500, and
1000 MCP sessions, 1-3 backend replicas, 30s/60s startup ramps, raised gateway
RPS variants, and MQTT timeout/admission experiments.

### Schema Discovery Is Mostly Healthy

The `discovery` scenario, which only calls `find_topics` and `describe_topic`,
was healthy through the gateway:

| Sessions | Backend replicas | Startup ramp | Success |
| ---: | ---: | ---: | ---: |
| 100 | 1 | 30s | 99.70% |
| 500 | 1 | 30s | 98.62% |
| 1000 | 1 | 30s | 97.39% |

Failures were mostly client context deadlines near the wall-clock end of the
run, plus a small number of HTTP request failures. This indicates the schema
tools themselves are not the bottleneck.

### Gateway Resource Proxy Needs Follow-Up

The `schema-resources` scenario was unhealthy through the gateway:

| Sessions | Backend replicas | Startup ramp | Success |
| ---: | ---: | ---: | ---: |
| 100 | 1 | 30s | 0.75% |
| 500 | 1 | 30s | 3.10% |
| 1000 | 1 | 30s | 6.41% |

Direct backend validation of the same resource methods passed at 100%. That
points to gateway resource proxy/config/protocol behavior rather than backend
resource registration.

### Mixed Load Bottlenecks On MQTT-Backed Tools

The `mixed` scenario combines cheap schema calls with expensive broker-facing
calls. At high session counts, failures were dominated by:

- bounded `subscribe`
- `read_retained`
- `start_subscription`
- MQTT admission limiting
- broker unavailable or MQTT subscribe/connect deadline errors

Schema tools in the same mixed run had only small deadline/HTTP failures. The
reason is shared-path pressure: even a cheap schema call still goes through the
client, gateway, session lookup/routing, backend HTTP handler, JSON-RPC decode,
tool dispatch, JSON encode, and response path. When many MQTT calls are waiting
on connect/subscribe or admission, they consume shared gateway/backend capacity
and cheap calls can miss client deadlines.

### Watch Status Scales Better Than Mixed Bounded MQTT

`watch-status-hold` performed much better than mixed load once watches were
started and clients mostly polled `subscription_status`:

| Scenario | Replicas | Sessions | Success |
| --- | ---: | ---: | ---: |
| `watch-status-hold` | 1 | 500 | 99.996% |
| `watch-status-hold` | 2 | 500 | 100.000% |
| `watch-status-hold` | 3 | 500 | 100.000% |
| `watch-status-hold` | 2 | 1000 | 98.743% |
| `watch-status-hold` | 3 | 1000 | 98.864% |

This supports the v1 UX direction: prefer aggregated `subscription_status` for
operator-facing follow-up rather than repeatedly returning raw buffered data.

### Replicas Help Steady State, Not Broker Startup Storms

Adding backend replicas helped the watch-status steady state, but it did not
fix the mixed workload. The dominant mixed-load cost was concurrent MQTT
connect/subscribe against the external broker, not pure CPU work inside one MCP
pod. More pods can increase the number of simultaneous broker connection
attempts, so scaling replicas without admission control can move the bottleneck
to the broker/auth/network path faster.

### Timeout Alone Is Not A Fix

Raising MQTT connect/subscribe timeouts from 10s to 20s or 30s did not resolve
high-concurrency mixed failures. Longer timeouts can reduce premature failures
when the system is merely slow, but they can also hide overload by making
requests sit in limbo longer. Admission limits and startup ramping gave clearer
signals about whether a run was burst-limited or steady-state-limited.

## Current Interpretation

For current v1, the MCP server is useful and bounded when:

- schema discovery is used freely
- retained/live bounded reads stay small
- background watches are pod-local and session-pinned
- `subscription_status` is the normal follow-up surface
- raw `read_subscription` is treated as detail/debug, not the primary UX

The next scale work should focus on gateway resource handling, sticky-session
validation, MQTT startup backpressure, and pod-failure behavior before adding
durable external watch state.
