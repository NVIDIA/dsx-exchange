# dsx-exchange-mcp

MCP server for DSX Exchange schemas, topic discovery, and read-only MQTT access
to the DSX Event Bus. It runs standalone over Streamable HTTP and serves one
MCP endpoint for all synced DSX Exchange domains.

## What It Exposes

| Surface | Name | Purpose |
| --- | --- | --- |
| Resource | `dsx-exchange://specs/` | Index of embedded AsyncAPI domains. |
| Resource | `dsx-exchange://specs/{domain}` | Raw AsyncAPI YAML for one domain, such as `bms`, `nico`, `power-management`, or `spiffe-exchange`. |
| Tool | `dsx_exchange_find_topics(query, domain, limit)` | Search the embedded AsyncAPI topic catalogue before choosing a broker read. |
| Tool | `dsx_exchange_describe_topic(topic_filter)` | Describe the matching schema channel, payload shape, retained/live behavior, examples, and related metadata/value topics. |
| Tool | `dsx_exchange_read_retained(topic_filter, max_messages)` | Read retained metadata and retained state. For BMS, use retained `/Metadata/` topics before subscribing to live `/Value/` topics. |
| Tool | `dsx_exchange_subscribe(topic_filter, max_messages, max_duration_s)` | Collect live messages over a bounded window, then disconnect. Use this for live values. |

Schema discovery tools use only the embedded AsyncAPI bundle and do not connect
to MQTT. Broker-backed tools create a short-lived MQTT connection for one
request and return within configured message, duration, and byte limits.

Topic filters use standard MQTT wildcards: `+` for one level and `#` for the
final multi-level suffix. For long or sparse live sampling, MCP clients that
support background agents, subagents, tasks, or equivalent execution should run
`dsx_exchange_subscribe` there so the main chat can keep working. See
[skills/dsx-exchange-mcp/SKILL.md](skills/dsx-exchange-mcp/SKILL.md) for
client and agent workflow guidance. Tools are currently scoped to stateless,
finite samples: each subscribe call collects a finite sample and exits.

## Authentication

The server supports two MQTT authentication modes. JWTs are never accepted as
tool arguments.

| Mode | Use case | Behavior |
| --- | --- | --- |
| `jwt_passthrough` | Default mode for deployed DSX Exchange brokers. | Broker-backed tools read `Authorization: Bearer <jwt>` from the MCP request and present it to MQTT as `username=<MQTT_USERNAME>`, `password=<jwt>`. |
| `noauth` | Local/dev Event Bus deployments configured with anonymous fallback. | Broker-backed tools send no MQTT username or password. |
| Schema-only | Resource reads plus `find_topics` and `describe_topic`. | No broker connection is made, so no bearer is required. |

In `jwt_passthrough` mode, broker-backed tools return a structured
`missing_bearer` tool error when the MCP request has no bearer. Broker-side
auth-callout remains the source of truth for JWT validation and topic ACLs.

## Layout

```text
.
|-- cmd/dsx-exchange-mcp/       main, environment wiring, HTTP listener
|-- internal/
|   |-- auth/                    bearer extraction and request identity
|   |-- server/                  MCP server, resources, and tools
|   |-- specs/                   embedded raw AsyncAPI resources
|   |-- schemaindex/             parsed AsyncAPI topic catalogue
|   `-- mqttbus/                 MQTT client wrapper
|-- deploy/helm/                 Helm chart
`-- schemas/                     embedded copy of the repository root schemas
```

For the full server design, schema indexing behavior, authentication flow, and
deployment shape, see [Architecture.md](Architecture.md).

## Usage

Fast local process path:

```sh
cd mcp/dsx-exchange-mcp
make sync-specs
make test
make build
make run
```

Configure an MCP client with `http://127.0.0.1:8080/mcp`. Schema resources and
schema discovery tools work without a broker. MQTT-backed tools also need
`NATS_URL` to point at a reachable broker and, in `jwt_passthrough` mode, a
bearer on the MCP request.

Build the local development image:

```sh
make image
```

Deploy the local Event Bus stack and MCP backend with the repository Skaffold
flow:

```sh
make -C local skaffold-run
```

To redeploy only the MCP backend after the local Kind stack already exists:

```sh
cd mcp/dsx-exchange-mcp
make skaffold-run-kind
```

Expose the Kind backend for a desktop MCP client:

```sh
make port-forward-kind
```

Configure the MCP client with `http://127.0.0.1:18080/mcp`. In Kind, the MCP
pod is installed in `kind-csc`, namespace `mcp-backends`, and uses
`MCP_MQTT_AUTH_MODE=noauth` with the local Event Bus MQTT endpoint from
`values.kind.yaml`.

Run `make sync-specs` before building the server binary or image when the
repository root `schemas/` tree changes. Override the source with
`SCHEMA_SRC=/path/to/schemas make sync-specs`.

## Environment

| Variable | Default | Notes |
| --- | --- | --- |
| `MCP_ADDR` | `:8080` | Listener for `/mcp` and health endpoints. |
| `NATS_URL` | `tcp://nats:1883` | MQTT 3.1.1 facade on the NATS broker. |
| `MCP_MQTT_AUTH_MODE` | `jwt_passthrough` | `jwt_passthrough` or `noauth`. |
| `MQTT_USERNAME` | `oauthtoken` | MQTT username used only in `jwt_passthrough` mode. |
| `MQTT_CONNECT_TIMEOUT_S` | `5` | MQTT CONNECT timeout. |
| `MQTT_SUBSCRIBE_TIMEOUT_S` | `5` | MQTT SUBSCRIBE timeout. |
| `MQTT_TLS_CA_FILE` | unset | Optional root CA bundle for a private broker CA. |
| `MQTT_TLS_SERVER_NAME` | unset | Optional TLS server name override. |
| `MQTT_TLS_INSECURE_SKIP_VERIFY` | `false` | Local-dev only; rejected by Helm unless acknowledged. |
| `MCP_DEFAULT_MAX_MESSAGES` | `100` | Default message cap per tool call. |
| `MCP_MAX_MESSAGES` | `1000` | Hard message cap per tool call. |
| `MCP_DEFAULT_MAX_DURATION_S` | `30` | Default subscribe window. |
| `MCP_MAX_DURATION_S` | `30` | Hard subscribe window cap. |
| `MQTT_MAX_RESULT_BYTES` | `1048576` | Maximum returned topic and payload bytes. |
| `MCP_MQTT_COLLECT_MAX_CONCURRENT_PER_POD` | `100` | Per-pod admission limit for bounded MQTT collectors. |
| `MCP_FIND_TOPICS_DEFAULT_LIMIT` | `20` | Default schema search result cap. |
| `MCP_FIND_TOPICS_MAX_LIMIT` | `100` | Hard schema search result cap. |
| `LOG_FORMAT` | `json` | Structured log format. |

Health endpoints are served on the same listener:

- `/healthz/live`
- `/healthz/ready`

TLS trust is deployment configuration, not MCP tool input. For production or
deployed broker usage, mount the broker root CA and set `MQTT_TLS_CA_FILE`.
Agents provide bearer credentials through MCP request headers only. In `noauth`
local mode, do not provide a dummy token; the MQTT client intentionally sends no
username or password.

## Specs

Specs are pinned at build time. `make sync-specs` copies the repository root
schema tree into `schemas/`, and `schemas/embed.go` bakes it into the binary.
The image uses the already-synced `./schemas` tree and does not fetch schemas
at runtime.

Empty domain stubs are filtered out at startup so they do not surface as MCP
resources or schema tool matches. To update specs, re-run `make sync-specs`
against a refreshed schema checkout and cut a new image.

## Setup Checklist

Before an MCP client can call broker-backed tools, verify:

| Item | What the operator provides | Where this MCP expects it |
| --- | --- | --- |
| MCP endpoint | A reachable direct server `/mcp` endpoint. | `DSX_EXCHANGE_MCP_URL` for tests and tools. |
| MQTT authentication mode | `jwt_passthrough` for deployed broker auth, `noauth` for local anonymous fallback. | Helm `mqtt.authMode`, runtime `MCP_MQTT_AUTH_MODE`. |
| Broker endpoint | MQTT endpoint for the DSX Event Bus. | Helm `natsURL`, runtime `NATS_URL`. |
| Broker username | OAuth profile username for MQTT CONNECT in `jwt_passthrough` mode. | Helm `mqtt.username`, runtime `MQTT_USERNAME`. |
| Broker CA | Root/intermediate CA bundle for broker TLS. | Secret referenced by `mqtt.tls.caCertSecret.name/key`. |
| TLS server name | Broker certificate server name, if needed. | Helm `mqtt.tls.serverName`, runtime `MQTT_TLS_SERVER_NAME`. |
| Caller JWT | Fresh user/service bearer from the deployment's approved identity flow when using `jwt_passthrough`. | MCP `Authorization: Bearer ...`. |
| Allowed topics | Topics the caller JWT is authorized to read. | Broker-side authorization policy. |

If schema tools work but broker-backed tools return auth or subscribe errors,
debug bearer freshness, broker CA trust, broker URL/server name, and topic ACLs.
Do not commit bearer tokens, CA files, cluster snapshots, or
environment-specific broker endpoints.

## Validation

Use these repo-local checks for README-level validation:

```sh
make test
make lint
make build
make image
```

Deployed-broker and local LLM prompt-eval tests remain opt-in checks in the Go
test suite. They require external services or secrets and are not the baseline
README validation path.
