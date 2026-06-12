# dsx-exchange-mcp

MCP server that exposes the DSX Exchange AsyncAPI specs as Resources and a
read-only NATS-MQTT bridge as Tools. One server for all DSX Exchange domains.

Runs standalone over Streamable HTTP.

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
  subscribe and collect messages over a bounded window. Use this for live
  values. For watch/listen/monitor requests, MCP clients that support
  background tool calls should run this tool in the background so the main
  agent can keep working. If background execution is unavailable, use short
  sampling windows and repeat the call.
- `dsx_exchange_read_retained(topic_filter, max_messages)` — drain retained
  messages currently held by the broker. Use this for metadata; BMS values are
  not retained (republished on change every ~100 s).

Topic filters use standard MQTT wildcards: `+` (single level), `#` (multi-level,
end of filter only).

Why this split exists: MCP tool calls are fundamentally request/response. A
long MQTT subscription inside one foreground tool call can tie up the MCP client
while it waits for stream data, which is a poor fit for sparse or ongoing
telemetry. The preferred stateless pattern is to use `dsx_exchange_subscribe`
with bounded limits and have agent runtimes run long sampling calls in the
background when they support that primitive. MCP Tasks or response streaming may
eventually provide a cleaner protocol-level answer, but those paths are still
experimental for this use case. The public v1 surface intentionally avoids
server-side watch/listen/monitor state: one MQTT tool call creates a temporary
client, subscribes for a finite window, returns bounded results, and disconnects.

## Auth

The server supports two MQTT auth modes. It does not accept JWTs as tool
arguments.

- `jwt_passthrough` (default): each MCP request may include
  `Authorization: Bearer <jwt>`. Broker-backed tools present that bearer to
  MQTT as `username=<MQTT_USERNAME>`, `password=<jwt>`. The DSX Exchange
  auth-callout validates the JWT and enforces topic ACLs.
- `noauth`: broker-backed tools send no MQTT username or password. Use this
  only with local/dev Event Bus deployments configured with the noauth
  anonymous fallback.

Schema discovery tools do not connect to MQTT and therefore do not require a
bearer. Broker-backed tools in `jwt_passthrough` mode return a structured
`missing_bearer` tool error when the MCP request has no bearer.

## Layout

```
cmd/dsx-exchange-mcp     main, env wiring, HTTP listener
internal/auth            bearer extraction + request identity context
internal/server          MCP server, resource & tool registration
internal/specs           raw AsyncAPI resources from embedded schemas
internal/schemaindex     parsed AsyncAPI topic catalogue for schema tools
internal/mqttbus         paho v3 client wrapper (jwt_passthrough/noauth + TLS)
deploy/helm              chart (kata runtime, readonly rootfs, drop ALL caps)
schemas/                 generated embedded copy of monorepo root schemas/
```

## Build & run

Fast local process path:

```sh
cd mcp/dsx-exchange-mcp
make sync-specs   # copies ../../schemas/ into ./schemas
make test
make build
make build-load
make run          # listens on :8080
```

Configure an MCP client with `http://127.0.0.1:8080/mcp`. Schema resources and
schema discovery tools work without a broker connection. MQTT-backed tools need
`NATS_URL` to point at a reachable broker and need the MCP client to provide
`Authorization: Bearer <jwt>`.

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
| `MCP_MQTT_AUTH_MODE` | `jwt_passthrough` | `jwt_passthrough` or `noauth` |
| `MQTT_USERNAME` | `oauthtoken` | MQTT username used only in `jwt_passthrough` mode |
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
| `MCP_FIND_TOPICS_DEFAULT_LIMIT` | `20` | default schema search result cap |
| `MCP_FIND_TOPICS_MAX_LIMIT` | `100` | hard schema search result cap |
| `LOG_FORMAT` | `json` | structured logs |

Health endpoints are served on the same listener:

- `/healthz/live`
- `/healthz/ready`

TLS trust is deployment configuration, not MCP tool input. For deployed-bus
tests or production, mount the broker root CA and set `MQTT_TLS_CA_FILE`.
Agents provide bearer credentials through MCP request headers and tool
arguments only. In `noauth` local mode, do not provide a dummy token; the MQTT
client intentionally sends no username/password so the Event Bus noauth
fallback can match.

The public schema tree is copied from the monorepo root `schemas/` directory. Override the location with `SCHEMA_SRC=/path/to/schemas make sync-specs`.

## Specs are pinned at build time

`make sync-specs` copies the monorepo schema tree from `../../schemas` into `schemas/`, and `schemas/embed.go`
bakes it into the binary. The image is hermetic — no runtime fetch from GitLab.
Empty domain stubs are filtered out at startup so they don't surface as MCP
resources or schema tool matches.

To update specs, re-run `sync-specs` against a refreshed schema checkout and
cut a new image.

## Deploy to local Kind

The Helm chart lives at `deploy/helm/dsx-exchange-mcp/`. Production-oriented
defaults keep the container non-root with a read-only root filesystem and
`drop: ["ALL"]`; local Kind overrides live in
`deploy/helm/dsx-exchange-mcp/values.kind.yaml`.

The repo root Skaffold flow deploys the local Event Bus stack and this MCP
backend:

```sh
make -C local skaffold-run
```

To deploy or redeploy only the MCP backend after the local stack already exists:

```sh
cd mcp/dsx-exchange-mcp
make skaffold-run-kind
```

Expose the backend for a desktop MCP client:

```sh
make port-forward-kind
```

Configure the MCP client with `http://127.0.0.1:18080/mcp`. In Kind, the MCP pod
uses `tcp://nats.event-bus.svc.cluster.local:1883` from `values.kind.yaml`.
This path intentionally does not require an MCP gateway. The Kind values use
`MCP_MQTT_AUTH_MODE=noauth`, matching the local Event Bus noauth setup.

## Setup checklist

Before an MCP client or load test can call broker-backed tools, verify:

| Item | What the operator provides | Where this MCP expects it |
| --- | --- | --- |
| MCP endpoint | A reachable direct server `/mcp` endpoint | `DSX_EXCHANGE_MCP_URL` for tests/tools |
| MQTT auth mode | `jwt_passthrough` for deployed broker auth, `noauth` for local anonymous fallback | Helm `mqtt.authMode`, runtime `MCP_MQTT_AUTH_MODE` |
| Broker endpoint | MQTT endpoint for the DSX Event Bus | Helm `natsURL`, runtime `NATS_URL` |
| Broker username | OAuth profile username for MQTT CONNECT in `jwt_passthrough` mode | Helm `mqtt.username`, runtime `MQTT_USERNAME` |
| Broker CA | Root/intermediate CA bundle for broker TLS | Secret referenced by `mqtt.tls.caCertSecret.name/key` |
| TLS server name | Broker certificate server name, if needed | Helm `mqtt.tls.serverName`, runtime `MQTT_TLS_SERVER_NAME` |
| Caller JWT | Fresh user/service bearer from approved secret manager flow when using `jwt_passthrough` | MCP `Authorization: Bearer ...`; load secret key `bearer` |
| Allowed topics | Topics the caller JWT is authorized to read | E2E/load env topic inputs |

If schema tools work but broker-backed tools return auth or subscribe errors,
debug in this order: bearer freshness, broker CA trust, broker URL/server name,
and topic ACLs.

Do not commit bearer tokens, CA files, cluster snapshots, or environment-specific
broker endpoints.

## E2E against deployed bus

Deployed-bus tests are opt-in because they require external broker, JWT, topic,
and CA setup. Stage 1 tests the MQTT bridge directly. Stage 2 tests the MCP
protocol path through this server's direct `/mcp` endpoint. Set
`DSX_EXCHANGE_MCP_URL` to the local process or port-forwarded Kind endpoint.

Never commit bearer tokens, CA material, or topic names that are environment
specific or sensitive.

Validation ladder:

```sh
# Direct MQTT bridge to the deployed broker.
RUN_EXCHANGE_E2E_DEPLOYED_BUS=1 go test -mod=vendor ./internal/mqttbus -run TestDeployedBusE2E

# MCP schema/tool path through a direct backend /mcp endpoint.
RUN_EXCHANGE_MCP_SCHEMA_E2E=1 go test -mod=vendor ./internal/server -run TestStagedMCPSchemaDescribeThroughEndpoint

# MCP bounded broker-backed tool path.
RUN_EXCHANGE_MCP_E2E=1 go test -mod=vendor ./internal/server -run TestStagedMCPE2EDeployedBus

# Curated prompt-to-tool fixture replay through the endpoint.
RUN_EXCHANGE_MCP_QUALITY_E2E=1 go test -mod=vendor ./internal/server -run TestStagedMCPQualityFixturesThroughEndpoint
```

Required environment for the staged MCP tests is the setup checklist above plus
`DSX_EXCHANGE_MCP_URL`, `DSX_EXCHANGE_E2E_BEARER`, and the allowed topic inputs
used by the selected test. For direct MQTT tests, provide the broker URL,
`DSX_EXCHANGE_MQTT_AUTH_MODE`, username if non-default, CA/server-name
settings, bearer when using `jwt_passthrough`, and allowed/denied topic inputs
through the `DSX_EXCHANGE_MQTT_*` and `DSX_EXCHANGE_E2E_*` environment
variables.

## Local LLM prompt eval

`TestLocalLLMMCPPromptEval` is an opt-in local harness that runs fixture prompts
through an OpenAI-compatible local LLM endpoint, executes emitted MCP tool calls,
logs the tool trace, and compares the model's final tool plan with
`internal/server/testdata/tool_call_expectations.json`.

Set `DSX_EXCHANGE_MCP_URL` to a local process or port-forwarded Kind `/mcp`
endpoint. If it is unset, the test starts an in-process MCP server.

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
embedded bundle. The MQTT tools use paho v3 and support `jwt_passthrough` and
`noauth` broker modes. Broker-side auth-callout remains the source of truth for
JWT validation, anonymous fallback, and topic ACLs. Public v1 is stateless:
schema discovery plus finite bounded MQTT reads.

## References

- Schema repo — `gitlab-master.nvidia.com/ncp/dsx/event-bus/schema`
- Current v1 scope — `docs/current-v1-scope.md`
- Load validation findings — `docs/load-testing.md`
- MCP spec — https://modelcontextprotocol.io/specification/2025-06-18/
- Go SDK — https://github.com/modelcontextprotocol/go-sdk
