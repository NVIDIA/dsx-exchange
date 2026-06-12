<!--
Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
SPDX-License-Identifier: Apache-2.0
-->

# DSX Exchange MCP Load Testing

This note explains how to reproduce the current load-test methodology and keeps
the stable findings from local experiments. Raw report bundles are intentionally
not committed; they belong under ignored `reports/`.

## What The Harness Tests

`cmd/dsx-exchange-mcp-load` creates many independent MCP clients. Each client
performs its own MCP initialize flow, lists tools, and then runs one workload
scenario. Current `dsx-exchange-mcp` uses stateless JSON Streamable HTTP, so a
server may not return `Mcp-Session-Id`; the harness handles both stateless and
stateful endpoints. This is not an LLM test; it is a protocol and
backend-capacity test.

Scenarios:

| Scenario | Purpose |
| --- | --- |
| `discovery` | Exercise schema tools only: `find_topics` and `describe_topic`. |
| `schema-resources` | Exercise MCP resources: `resources/list` and `resources/read`. |
| `bounded-read` | Exercise broker-facing bounded reads: retained reads and short live subscribes. |
| `mixed` | Mix schema tools and bounded MQTT tools. |
| `mixed-stateless` | Alias-style scenario with the same public-surface intent as `mixed`. |

Legacy watch scenarios (`watch`, `watch-hold`, `watch-status-hold`, and
`sticky-check`) remain in the harness for historical report comparison, but
current public v1 does not expose watch/listen/monitor lifecycle tools.

`deploy/loadtest/run-kind-load-experiment.sh` wraps the load binary for local
Kind/gateway experiments. It records the manifest, image IDs, token TTL
metadata, cluster state before/after, JSON/TXT/CSV reports, per-operation
latency, and per-operation error attribution.

## Reproduction Requirements

The load harness depends on systems outside this MCP repo. Before running
gateway-backed deployed-bus tests, the operator needs:

- a reachable MCP `/mcp` endpoint, either direct backend or gateway
- backend configuration for the target Event Bus MQTT endpoint through Helm
  `natsURL`
- MQTT auth mode configured through Helm `mqtt.authMode`
- broker username configured through Helm `mqtt.username` when using
  `jwt_passthrough`
- broker TLS trust configured through Helm `mqtt.tls.caCertSecret.name/key`
- broker TLS server name configured through Helm `mqtt.tls.serverName` when the
  certificate requires it
- a fresh caller JWT from the approved secret manager or Vault flow when using
  `jwt_passthrough`
- the JWT available to the load job as a Kubernetes secret named
  `dsx-exchange-mcp-load-token` with key `bearer` when using
  `jwt_passthrough`
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
| MCP endpoint | MCP clients can reach the direct backend or gateway `/mcp` endpoint. |
| MQTT auth mode | Backend uses `jwt_passthrough` for deployed auth or `noauth` for local anonymous fallback. |
| Bearer passthrough | In `jwt_passthrough`, the caller bearer reaches `dsx-exchange-mcp` as `Authorization`. |
| Broker endpoint | Backend `NATS_URL` points at the intended MQTT endpoint. |
| Broker username | In `jwt_passthrough`, backend `MQTT_USERNAME` matches the broker OAuth profile. |
| Broker CA | Backend has a mounted CA file and `MQTT_TLS_CA_FILE` points to it. |
| TLS server name | Backend server-name override matches the broker certificate when required. |
| Load JWT secret | In `jwt_passthrough`, load namespace has `dsx-exchange-mcp-load-token` with data key `bearer`. |
| JWT freshness | In `jwt_passthrough`, token TTL is long enough for the full experiment. |
| Topic ACLs | The load topics are authorized for the caller identity. |

If `discovery` passes but `bounded-read` or `mixed` fails, the next checks are
auth mode, bearer freshness, broker CA/server-name settings, topic ACLs, broker
availability, and MQTT admission limits.

## Important Knobs

Record these for every run so the result is reproducible:

| Knob | Meaning |
| --- | --- |
| `SCENARIO` | Workload shape, such as `discovery`, `schema-resources`, `bounded-read`, or `mixed`. |
| `SESSION_SWEEP` / `SESSIONS` | Concurrent MCP client/session counts. |
| `STARTUP_RAMP` | Time window used to spread client startup. `0s` means an instant startup burst. |
| `DURATION` | Total wall-clock runtime after the load job starts. |
| `GATEWAY_RPS` | Gateway tenant rate-limit setting used during the run. |
| `CLIENT_RPS` | Load-generator request rate limit. |
| `BACKEND_REPLICAS` | Number of `dsx-exchange-mcp` backend pods. |
| `BACKEND_CONNECT_TIMEOUT_S` | Backend MQTT connect timeout. |
| `BACKEND_SUBSCRIBE_TIMEOUT_S` | Backend MQTT subscribe timeout. |
| `BACKEND_COLLECT_MAX_CONCURRENT` | Per-pod admission limit for bounded MQTT collectors. |
| `TOPIC` / `RETAINED_TOPIC` | Allowed live and retained topic filters used by broker-facing scenarios. |
| `RESET_BACKEND` | Whether the backend is restarted before the run. |

Ramp and reset are important. A zero-ramp run measures thundering-herd startup
behavior. A ramped run is closer to organic production growth and makes it
easier to separate steady-state capacity from burst admission failures. Resetting
the backend before a run makes startup cost visible and avoids carrying state
from an earlier experiment.

## Findings From Current Experiments

These findings came from local Kind/gateway experiments using 100, 500, and
1000 MCP clients, 1-3 backend replicas, 30s/60s startup ramps, raised gateway
RPS variants, and MQTT timeout/admission experiments. Some older experiments
included watch lifecycle tools; those results are kept only as historical
capacity evidence.

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
- MQTT admission limiting
- broker unavailable or MQTT subscribe/connect deadline errors

Schema tools in the same mixed run had only small deadline/HTTP failures. The
reason is shared-path pressure: even a cheap schema call still goes through the
client, gateway, session lookup/routing, backend HTTP handler, JSON-RPC decode,
tool dispatch, JSON encode, and response path. When many MQTT calls are waiting
on connect/subscribe or admission, they consume shared gateway/backend capacity
and cheap calls can miss client deadlines.

### Historical Watch Status Result

`watch-status-hold` performed much better than mixed load once watches were
started and clients mostly polled `subscription_status`:

| Scenario | Replicas | Sessions | Success |
| --- | ---: | ---: | ---: |
| `watch-status-hold` | 1 | 500 | 99.996% |
| `watch-status-hold` | 2 | 500 | 100.000% |
| `watch-status-hold` | 3 | 500 | 100.000% |
| `watch-status-hold` | 2 | 1000 | 98.743% |
| `watch-status-hold` | 3 | 1000 | 98.864% |

This was useful evidence that lightweight status polling is cheaper than
repeated broker startup. The public v1 direction has since moved away from
server-side watch state, so new UX validation should focus on finite
`dsx_exchange_subscribe` calls that clients can run in the background when they
support that primitive.

### Replicas Help Steady State, Not Broker Startup Storms

Adding backend replicas did not automatically fix mixed bounded MQTT load. The
dominant cost was concurrent MQTT connect/subscribe against the external broker,
not pure CPU work inside one MCP pod. More pods can increase the number of
simultaneous broker connection attempts, so scaling replicas without admission
control can move the bottleneck to the broker/auth/network path faster.

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
- long listen/watch/monitor prompts are implemented as finite
  `dsx_exchange_subscribe` calls
- MCP clients run long bounded calls in the background when they support that
  UX primitive

The next scale work should focus on gateway resource handling, MQTT startup
backpressure, and pod-failure behavior. Durable external watch state should stay
out of scope unless product/load evidence shows bounded background tool calls
are not enough.
