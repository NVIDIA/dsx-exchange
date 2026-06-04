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

**Tools** — read-only MQTT against the DSX Event Bus:
- `dsx_exchange_describe_topic(topic_filter)` — parse embedded AsyncAPI specs
  and describe the matching schema channel, payload shape, retained/live
  behavior, examples, and related metadata/value topics.
- `dsx_exchange_subscribe(topic_filter, max_messages, max_duration_s)` —
  subscribe and collect messages over a window. Use this for live values.
- `dsx_exchange_read_retained(topic_filter, max_messages)` — drain retained
  messages currently held by the broker. Use this for metadata; BMS values are
  not retained (republished on change every ~100 s).

Topic filters use standard MQTT wildcards: `+` (single level), `#` (multi-level,
end of filter only).

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
make sync-specs   # copies ../../schemas/ into ./schemas
make build
make run          # listens on :8080, expects NATS at tcp://nats:1883
```

Environment:

| Var | Default | Notes |
| --- | --- | --- |
| `MCP_ADDR` | `:8080` | listener for `/mcp` (Streamable HTTP) |
| `NATS_URL` | `tcp://nats:1883` | MQTT 3.1.1 facade on the NATS broker |
| `MQTT_USERNAME` | `oauthtoken` | MQTT username for OAuth2 bearer auth |
| `MQTT_TLS_CA_FILE` | (unset) | optional root CA bundle for private broker CA |
| `MQTT_TLS_SERVER_NAME` | (unset) | optional TLS server name override |
| `MQTT_TLS_INSECURE_SKIP_VERIFY` | `false` | local-dev only; rejected by Helm unless acknowledged |
| `MCP_DEFAULT_MAX_MESSAGES` | `100` | default message cap per tool call |
| `MCP_MAX_MESSAGES` | `1000` | hard message cap per tool call |
| `MCP_DEFAULT_MAX_DURATION_S` | `30` | default subscribe window |
| `MCP_MAX_DURATION_S` | `30` | hard subscribe window cap |
| `MQTT_MAX_RESULT_BYTES` | `1048576` | max returned topic+payload bytes |
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

## E2E against deployed bus

Do not deploy a local NATS/event bus for this path. Gate deployed-bus tests
behind explicit environment variables and point to the shared dev bus:

Stage 1 tests the MQTT bridge directly:

```sh
RUN_EXCHANGE_E2E_DEPLOYED_BUS=1 \
DSX_EXCHANGE_MQTT_URL=tls://event-bus-ytl-dev2.dev.dsx.nvidia.com:1883 \
DSX_EXCHANGE_MQTT_USERNAME=oauth \
DSX_EXCHANGE_MQTT_CA_FILE=/path/to/root-ca.crt \
DSX_EXCHANGE_MQTT_SERVER_NAME=event-bus-ytl-dev2.dev.dsx.nvidia.com \
DSX_EXCHANGE_E2E_BEARER="$TOKEN" \
DSX_EXCHANGE_E2E_ALLOWED_TOPIC='...' \
DSX_EXCHANGE_E2E_DENIED_TOPIC='...' \
go test ./...
```

Stage 2 tests the MCP protocol path through either this server directly or the
gateway. Run this after the server is configured with the same deployed-bus
`NATS_URL`/TLS settings:

```sh
RUN_EXCHANGE_MCP_E2E=1 \
DSX_EXCHANGE_MCP_URL=http://localhost:8080/mcp \
DSX_EXCHANGE_E2E_BEARER="$TOKEN" \
DSX_EXCHANGE_E2E_ALLOWED_TOPIC='...' \
DSX_EXCHANGE_E2E_DENIED_TOPIC='...' \
go test ./internal/server -run TestStagedMCPE2EDeployedBus -count=1 -v
```

When running through the gateway, set `DSX_EXCHANGE_MCP_URL` to the gateway
`/mcp` endpoint. If the gateway prefixes tools, either let the test discover the
`*_dsx_exchange_subscribe` tool or set `DSX_EXCHANGE_E2E_TOOL_NAME` explicitly.

Never commit bearer tokens, CA material, or topic names that are environment
specific or sensitive.

## Local LLM prompt eval

`TestLocalLLMMCPPromptEval` is an opt-in local harness that runs fixture prompts
through an OpenAI-compatible local LLM endpoint, executes emitted MCP tool calls,
logs the tool trace, and compares the model's final tool plan with
`internal/server/testdata/tool_call_expectations.json`.

For the gateway path, set `DSX_EXCHANGE_MCP_URL` to the Latinum MCP Gateway
`/mcp` endpoint, for example `http://localhost:18180/mcp`. If it is unset, the
test starts an in-process MCP server.

See `docs/local-llm-mcp-eval.md`.

## Status

Alpha. Populated specs load and surface as resources when synced into the
embedded bundle. The MQTT tools use paho v3 and pass OAuth2 bearer credentials
to the broker as `username=<MQTT_USERNAME>`, `password=<bearer>`. Broker-side
auth-callout remains the source of truth for topic ACLs.

## References

- Latinum MCP Gateway SDD — `context/Latinum MCP Gateway - SDD (1).pdf`
- Schema repo — `gitlab-master.nvidia.com/ncp/dsx/event-bus/schema`
- MCP spec — https://modelcontextprotocol.io/specification/2025-06-18/
- Go SDK — https://github.com/modelcontextprotocol/go-sdk
