# dsx-exchange-mcp

MCP server that exposes the DSX Exchange AsyncAPI specs as Resources and a
read-only NATS-MQTT bridge as Tools. One server for all DSX Exchange domains.

Runs as one of the upstream MCP servers behind the Latinum MCP Gateway
(agentgateway).

## What it exposes

**Resources** — AsyncAPI 3.1.0 specs, embedded at build time:
- `dsx-exchange://specs/` — index of available domains
- `dsx-exchange://specs/{domain}` — raw YAML for one domain
  (e.g. `bms`, `nico`, `power-management`, `spiffe-exchange`)

**Tools** — schema discovery plus read-only MQTT against the DSX Event Bus:

Schema discovery tools do not connect to MQTT. They inspect the embedded
AsyncAPI bundle so the client can choose a valid topic before touching the
broker:

- `dsx_exchange_find_topics(query, domain, limit)` — search the embedded
  AsyncAPI index for relevant Exchange topics before choosing a concrete
  broker read.
- `dsx_exchange_describe_topic(topic_filter)` — parse embedded AsyncAPI specs
  and describe the matching schema channel, payload shape, retained/live
  behavior, examples, and related metadata/value topics.

Bounded MQTT tools create a short-lived broker connection for one request and
return within configured message, duration, and byte limits:

- `dsx_exchange_subscribe(topic_filter, max_messages, max_duration_s)` —
  subscribe and collect messages over a window. Use this for live values.
- `dsx_exchange_read_retained(topic_filter, max_messages)` — drain retained
  messages currently held by the broker. Use this for metadata; BMS values are
  not retained (republished on change every ~100 s).

Background watch tools are the v1 stand-in for long MQTT subscriptions. They
start a pod-local MQTT watch, then let the client poll status or bounded raw
buffer reads instead of blocking one MCP request for a long time:

- `dsx_exchange_start_subscription(topic_filter|selector, ttl_seconds, ...)` —
  start a pod-local background MQTT watch and return a `subscription_id`.
- `dsx_exchange_read_subscription(subscription_id, cursor, max_messages, max_bytes)` —
  read a bounded raw batch from the watch buffer for detail/debug use.
- `dsx_exchange_subscription_status(subscription_id)` — inspect watch status,
  counters, bounded per-topic update summaries, watermarks, expiry, and last
  error.
- `dsx_exchange_stop_subscription(subscription_id)` — stop a watch and release
  its local buffer.

Topic filters use standard MQTT wildcards: `+` (single level), `#` (multi-level,
end of filter only).

Why background watches exist: MCP tool calls are fundamentally request/response.
A long MQTT subscription inside one tool call can tie up the MCP client while it
waits for stream data, which is a poor fit for sparse or ongoing telemetry.
MCP Tasks may eventually be a cleaner protocol-level answer, but that feature is
still experimental. The current watch tools provide a bounded, explicit v1
pattern: start the MQTT work, return quickly, poll `subscription_status` for
aggregated updates, and stop the watch when done. Watches remain pod-local,
TTL-limited, buffer-limited, and session-pinned.

## Auth

The server holds **no credentials of its own**. The caller's JWT flows through
end-to-end:

1. The Latinum MCP Gateway validates the JWT via `ext_authz` and forwards
   `Authorization: Bearer <jwt>` unchanged (SDD §2.4).
2. This server validates request shape and safety limits, but does not
   duplicate DSX Exchange broker authZ policy in v1.
3. For tool calls, the same bearer is presented to NATS as the MQTT CONNECT
   password (`username=oauthtoken`, `password=<bearer>`). The NATS auth-callout
   service validates it and enforces topic ACLs keyed on the OAuth2 identity.

The DSX Exchange broker/auth-callout is the source of truth for token validity
and topic ACLs; this server maps broker failures into structured MCP errors.

## Layout

```
cmd/dsx-exchange-mcp     main, env wiring, HTTP listener
internal/auth            bearer extraction + gateway identity context
internal/server          MCP server, resource & tool registration
internal/specs           raw AsyncAPI resources from embedded schemas
internal/schemaindex     parsed AsyncAPI topic catalogue for schema tools
internal/mqttbus         paho v3 client wrapper (OAuth2 password + TLS)
deploy/helm              chart (kata runtime, readonly rootfs, drop ALL caps)
schemas/                 generated embedded copy of monorepo root schemas/
```

## Build & run

```sh
cd mcp/dsx-exchange-mcp
make sync-specs   # copies ../../schemas/ into ./schemas
make test
make build
make run          # listens on :8080, expects NATS at tcp://nats:1883
```

Images:

```sh
make image       # builds dsx-exchange-mcp:dev
make load-image  # builds dsx-exchange-mcp-load:dev
```

Run `make sync-specs` before building the server binary or image when the
monorepo `schemas/` tree has changed. The image uses the already-synced
`./schemas` tree and does not fetch schemas at runtime.

Environment:

| Var | Default | Notes |
| --- | --- | --- |
| `MCP_ADDR` | `:8080` | listener for `/mcp` (Streamable HTTP) |
| `NATS_URL` | `tcp://nats:1883` | MQTT 3.1.1 facade on the NATS broker |
| `MQTT_USERNAME` | `oauthtoken` | MQTT username for OAuth2 bearer auth |
| `MQTT_CONNECT_TIMEOUT_S` | `5` | timeout for MQTT CONNECT |
| `MQTT_SUBSCRIBE_TIMEOUT_S` | `5` | timeout for MQTT SUBSCRIBE |
| `MQTT_TLS_CA_FILE` | (unset) | optional root CA bundle for private broker CA |
| `MQTT_TLS_SERVER_NAME` | (unset) | optional TLS server name override |
| `MQTT_TLS_INSECURE_SKIP_VERIFY` | `false` | local-dev only; rejected by Helm unless acknowledged |
| `MCP_DEFAULT_MAX_MESSAGES` | `100` | default message cap per tool call |
| `MCP_MAX_MESSAGES` | `1000` | hard message cap per tool call |
| `MCP_DEFAULT_MAX_DURATION_S` | `30` | default subscribe window |
| `MCP_MAX_DURATION_S` | `30` | hard subscribe window cap |
| `MQTT_MAX_RESULT_BYTES` | `1048576` | max returned topic+payload bytes |
| `MCP_MQTT_COLLECT_MAX_CONCURRENT_PER_POD` | `100` | per-pod admission limit for bounded MQTT collectors |
| `MCP_MQTT_WATCH_START_MAX_CONCURRENT_PER_POD` | `500` | per-pod admission limit for watch start MQTT setup |
| `MCP_WATCH_DEFAULT_TTL_S` | `300` | default background watch TTL |
| `MCP_WATCH_MAX_TTL_S` | `900` | hard background watch TTL cap |
| `MCP_WATCH_DEFAULT_BUFFER_MESSAGES` | `100` | default watch ring-buffer message cap |
| `MCP_WATCH_MAX_BUFFER_MESSAGES` | `1000` | hard watch ring-buffer message cap |
| `MCP_WATCH_DEFAULT_BUFFER_BYTES` | `262144` | default watch ring-buffer byte cap |
| `MCP_WATCH_MAX_BUFFER_BYTES` | `1048576` | hard watch ring-buffer byte cap |
| `MCP_WATCH_MAX_PER_SESSION` | `10` | active background watch cap per MCP session |
| `MCP_WATCH_MAX_PER_POD` | `1000` | active background watch cap per pod |
| `MCP_FIND_TOPICS_DEFAULT_LIMIT` | `20` | default schema search result cap |
| `MCP_FIND_TOPICS_MAX_LIMIT` | `100` | hard schema search result cap |
| `LOG_FORMAT` | `json` | structured logs |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | (unset) | reserved for future OTLP push export; scrape `/metrics` today |

Health and metrics endpoints are served on the same listener:

- `/healthz/live`
- `/healthz/ready`
- `/metrics` — Prometheus-compatible process/tool metrics

TLS trust is deployment configuration, not MCP tool input. For deployed-bus
tests or production, mount the broker root CA and set `MQTT_TLS_CA_FILE`; agents
only provide bearer credentials and tool arguments. The caller bearer is passed
to MQTT as `password=<bearer>` with the configured `MQTT_USERNAME`; broker
OAuth and topic ACL enforcement remain broker-side.

The public schema tree is copied from the monorepo root `schemas/` directory. Override the location with `SCHEMA_SRC=/path/to/schemas make sync-specs`.

## Specs are pinned at build time

`make sync-specs` copies the monorepo schema tree from `../../schemas` into `schemas/`, and `schemas/embed.go`
bakes it into the binary. The image is hermetic — no runtime fetch from GitLab.
Empty domain stubs are filtered out at startup so they don't surface as MCP
resources or schema tool matches.

To update specs, re-run `sync-specs` against a refreshed schema checkout and
cut a new image.

## Deploy

Helm chart at `deploy/helm/dsx-exchange-mcp/`. Defaults match the DSX security
posture from the gateway SDD: `runtimeClassName: kata`, non-root, read-only
root filesystem, `drop: ["ALL"]`, two replicas, preferred pod anti-affinity,
and a PodDisruptionBudget. The Service exposes a single ClusterIP on the MCP
port with `appProtocol: agentgateway.dev/mcp`; the gateway's
`AgentgatewayBackend` points at it. Local kind deployments should override
`runtimeClassName: ""`.

Example gateway upstream entry:

```yaml
upstreams:
  - serviceName: dsx-exchange-mcp
    portName: mcp
    namespace: mcp-backends
    serviceLabels:
      app: dsx-exchange-mcp
    port: 8080
    podSelector:
      app: dsx-exchange-mcp
```

The derived gateway target name is `dsx-exchange-mcp-mcp`, so tools are
prefixed as `dsx-exchange-mcp-mcp_dsx_exchange_subscribe` in multi-upstream
gateway deployments.

## Using it locally or behind the gateway

For a local backend-only loop, run the MCP server with a broker URL, MQTT
username, and any required broker CA trust. The MCP client must provide a bearer
token in the `Authorization` header for broker-backed tools.

For the production-style path, put this server behind the Latinum MCP Gateway
and verify the setup checklist below.

## Setup checklist

Before an MCP client or load test can call broker-backed tools, verify:

| Item | What the operator provides | Where this MCP expects it |
| --- | --- | --- |
| Gateway route | A reachable Latinum MCP Gateway `/mcp` endpoint | `DSX_EXCHANGE_MCP_URL` for tests/tools |
| Stateful routing | Gateway routes the same `Mcp-Session-Id` to the same backend pod | Required for `start/read/status/stop_subscription` |
| Broker endpoint | MQTT endpoint for the DSX Event Bus | Helm `natsURL`, runtime `NATS_URL` |
| Broker username | OAuth profile username for MQTT CONNECT | Helm `mqtt.username`, runtime `MQTT_USERNAME` |
| Broker CA | Root/intermediate CA bundle for broker TLS | Secret referenced by `mqtt.tls.caCertSecret.name/key` |
| TLS server name | Broker certificate server name, if needed | Helm `mqtt.tls.serverName`, runtime `MQTT_TLS_SERVER_NAME` |
| Caller JWT | Fresh user/service bearer from approved secret manager flow | MCP `Authorization: Bearer ...`; load secret key `bearer` |
| Allowed topics | Topics the caller JWT is authorized to read | E2E/load env topic inputs |

If schema tools work but broker-backed tools return auth or subscribe errors,
debug in this order: bearer freshness, broker CA trust, broker URL/server name,
topic ACLs, then gateway bearer passthrough.

Do not commit bearer tokens, CA files, cluster snapshots, or environment-specific
broker/gateway endpoints.

## E2E against deployed bus

Deployed-bus tests are opt-in because they require external broker, gateway,
JWT, topic, and CA setup. Stage 1 tests the MQTT bridge directly. Stage 2 tests
the MCP protocol path through either this server directly or the gateway. When
running through the gateway, point `DSX_EXCHANGE_MCP_URL` at the gateway `/mcp`
endpoint. If the gateway prefixes tools, either let the test discover the
`*_dsx_exchange_subscribe` tool or set `DSX_EXCHANGE_E2E_TOOL_NAME`.

Never commit bearer tokens, CA material, or topic names that are environment
specific or sensitive.

Validation ladder:

```sh
# Direct MQTT bridge to the deployed broker.
RUN_EXCHANGE_E2E_DEPLOYED_BUS=1 go test -mod=vendor ./internal/mqttbus -run TestDeployedBusE2E

# MCP schema/tool path through a direct backend or gateway /mcp endpoint.
RUN_EXCHANGE_MCP_SCHEMA_E2E=1 go test -mod=vendor ./internal/server -run TestStagedMCPSchemaDescribeThroughEndpoint

# MCP bounded broker-backed tool path.
RUN_EXCHANGE_MCP_E2E=1 go test -mod=vendor ./internal/server -run TestStagedMCPE2EDeployedBus

# MCP async watch start/read/status/stop path.
RUN_EXCHANGE_MCP_WATCH_E2E=1 go test -mod=vendor ./internal/server -run TestStagedMCPWatchThroughEndpoint

# Curated prompt-to-tool fixture replay through the endpoint.
RUN_EXCHANGE_MCP_QUALITY_E2E=1 go test -mod=vendor ./internal/server -run TestStagedMCPQualityFixturesThroughEndpoint
```

Required environment for the staged MCP tests is the setup checklist above plus
`DSX_EXCHANGE_MCP_URL`, `DSX_EXCHANGE_E2E_BEARER`, and the allowed topic inputs
used by the selected test. For direct MQTT tests, provide the broker URL,
username if non-default, CA/server-name settings, bearer, and allowed/denied
topic inputs through the `DSX_EXCHANGE_MQTT_*` and `DSX_EXCHANGE_E2E_*`
environment variables.

## Local LLM prompt eval

`TestLocalLLMMCPPromptEval` is an opt-in local harness that runs fixture prompts
through an OpenAI-compatible local LLM endpoint, executes emitted MCP tool calls,
logs the tool trace, and compares the model's final tool plan with
`internal/server/testdata/tool_call_expectations.json`.

For the gateway path, set `DSX_EXCHANGE_MCP_URL` to the Latinum MCP Gateway
`/mcp` endpoint. If it is unset, the test starts an in-process MCP server.

See `docs/local-llm-mcp-eval.md`.

## Quality and load validation

`cmd/dsx-exchange-mcp-load` creates many MCP sessions and records JSON, text,
and CSV reports with per-operation latency and error attribution. Prompt quality
is covered by fixture-based Go tests and the opt-in local LLM eval.

Use `docs/load-testing.md` for load-test scenarios, reproduction requirements,
and summarized findings from the current branch. Raw report bundles are local
evidence and should stay under ignored `reports/`.

## Status

Alpha. Populated specs load and surface as resources when synced into the
embedded bundle. The MQTT tools use paho v3 and pass OAuth2 bearer credentials
to the broker as `username=<MQTT_USERNAME>`, `password=<bearer>`. Broker-side
auth-callout remains the source of truth for topic ACLs. Background watches are
pod-local, session-pinned, and intentionally limited to start/read/status/stop
for v1.

## References

- Latinum MCP Gateway SDD — `context/Latinum MCP Gateway - SDD (1).pdf`
- Schema repo — `gitlab-master.nvidia.com/ncp/dsx/event-bus/schema`
- Current v1 scope — `docs/current-v1-scope.md`
- Load validation findings — `docs/load-testing.md`
- MCP spec — https://modelcontextprotocol.io/specification/2025-06-18/
- Go SDK — https://github.com/modelcontextprotocol/go-sdk
