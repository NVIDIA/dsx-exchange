# dsx-exchange-mcp Architecture

This document is for a new developer trying to understand how the code works. It
is intentionally code-centric: which files own which behavior, how a request flows
through the service, and how configuration shapes runtime behavior.

The server is designed to run **standalone**. Any HTTP MCP client that speaks
Streamable HTTP can call it directly at `/mcp`. AgentGateway (or any
other reverse proxy) is an optional front door for production aggregation and
coarse auth — not a requirement for the server to function.

## Big Picture

`dsx-exchange-mcp` is an MCP server that exposes DSX Exchange data over MCP.

At runtime it does three main things:

1. Serves MCP over HTTP at `/mcp`.
2. Exposes embedded exchange specs as MCP resources.
3. Exposes schema exploration and bounded MQTT/NATS reads as MCP tools.

Standalone deployment shape:

```text
MCP client or auth-capable proxy
  -> HTTP POST /mcp
  -> dsx-exchange-mcp process or pod
  -> MQTT/NATS broker (when a broker-backed tool runs)
```

The same binary and container work in all of these placements:


| Placement                  | Typical use                                                 |
| -------------------------- | ----------------------------------------------------------- |
| Local process (`make run`) | Dev, prompt eval, direct MCP client checks                  |
| Docker container           | Portable standalone service                                 |
| Kubernetes Deployment      | Production or local Kind backend                            |
| Behind a gateway           | Optional — gateway forwards the same HTTP contract upstream |


The server does not implement topic authorization itself. It defers to the event bus.

In `jwt_passthrough` mode it takes the caller bearer from the incoming HTTP
request and presents it to the broker as the MQTT password. NATS auth-callout /
ACLs enforce topic access.

In `noauth` mode it sends no MQTT credentials, matching local Event Bus
deployments that allow anonymous fallback.

## Request Flow

Every MCP request hits the same HTTP entrypoint regardless of who sits in front
of the server. The upstream caller — desktop MCP client, test harness, load
generator, auth-capable proxy, or gateway — is responsible for attaching
credentials to the HTTP request. This service reads those headers and does not
mint, refresh, or store tokens.

### Broker-backed tool (e.g. `dsx_exchange_subscribe`)

```text
MCP caller
  POST /mcp with optional Authorization: Bearer <jwt>
  optional identity headers (x-mcp-*, Mcp-Session-Id)
    |
    v
cmd/dsx-exchange-mcp/main.go
  accepts HTTP /mcp
  wraps handler with auth.Middleware
    |
    v
internal/auth/caller.go
  extracts bearer + identity headers into request context
    |
    v
internal/server/tools.go
  validates MCP tool args
  applies max message / duration limits
  calls mqttbus.Collect(...)
    |
    v
internal/mqttbus/client.go
  creates short-lived MQTT client
  uses bearer as MQTT password (jwt_passthrough) or no credentials (noauth)
  subscribes to topic filter
  collects bounded messages
    |
    v
internal/server/tools.go
  writes audit log
  returns MCP result
```

### Schema-only paths (resources and discovery tools)

For MCP resource reads (`dsx-exchange://specs/...`) and schema tools
(`dsx_exchange_find_topics`, `dsx_exchange_describe_topic`), the flow stops
inside `internal/specs` or `internal/schemaindex`. No MQTT connection is
opened and no bearer is required.

## Deployment Modes

### Standalone (direct `/mcp`)

The primary integration surface is Streamable HTTP on `MCP_ADDR` (default
`:8080`):

```text
http://<host>:8080/mcp
```

Configure any MCP client that supports Streamable HTTP with that URL. For
broker-backed tools in `jwt_passthrough` mode, the client (or an adjacent token
proxy) must send `Authorization: Bearer <jwt>` on **each** MCP request. The
server does not cache credentials across requests.

Local Kind deploys this way by default: port-forward the backend Service and
point the client at `http://127.0.0.1:18080/mcp` with `MCP_MQTT_AUTH_MODE=noauth`.

### Optional gateway front door

In multi-upstream production topologies, a gateway may sit in front of one or
more MCP backends. From this server's perspective nothing changes: it still
accepts the same `/mcp` requests and reads the same headers. See
[Optional Gateway Integration](#optional-gateway-integration) below.

## File Map


| Path                                                     | Responsibility                                                                                                                |
| -------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------- |
| `cmd/dsx-exchange-mcp/main.go`                           | Process entrypoint. Reads env config, builds the MCP server, registers HTTP routes, starts `ListenAndServe`.                  |
| `internal/server/server.go`                              | Creates the MCP server instance and registers tools/resources.                                                                |
| `internal/server/tools.go`                               | Defines MCP tools, parses tool inputs, describes schema topics, enforces bounds, calls MQTT collection, and emits audit logs. |
| `internal/server/resources.go`                           | Defines MCP resources backed by embedded DSX specs.                                                                           |
| `internal/specs/specs.go`                                | Exposes raw spec resources from the embedded `schemas/` tree.                                                                 |
| `internal/schemaindex/index.go`                          | Parses AsyncAPI channel/message/operation primitives into a topic catalogue for schema exploration tools.                     |
| `schemas/`                                               | Generated copy of the monorepo root `schemas/`, embedded into the binary by `schemas/embed.go`.                               |
| `internal/mqttbus/client.go`                             | MQTT/NATS client logic: connect, subscribe, collect messages, classify broker errors.                                         |
| `internal/auth/caller.go`                               | Pulls caller bearer and optional identity headers from the HTTP request into Go context.                                      |
| `deploy/helm/dsx-exchange-mcp/templates/deployment.yaml` | Kubernetes Deployment: env vars, probes, security context, runtime class.                                                     |
| `deploy/helm/dsx-exchange-mcp/templates/service.yaml`    | Kubernetes Service exposing the MCP port (optionally annotated for gateway discovery).                                        |
| `deploy/helm/dsx-exchange-mcp/values.yaml`               | Default deploy-time configuration.                                                                                            |


## Process Startup

The binary starts in `cmd/dsx-exchange-mcp/main.go`.

```go
addr := envOr("MCP_ADDR", ":8080")
natsURL := envOr("NATS_URL", "tcp://nats:1883")
```

The entrypoint builds one `server.Config` from environment variables:

```go
cfg := server.Config{
	MQTT: mqttbus.Config{
		BrokerURL: natsURL,
		Username:  envOr("MQTT_USERNAME", mqttbus.DefaultUsername),
		AuthMode:  mqttbus.AuthMode(envOr("MCP_MQTT_AUTH_MODE", string(mqttbus.DefaultAuthMode))),
	},
	DefaultMaxMessages:  intEnvOr("MCP_DEFAULT_MAX_MESSAGES", 100),
	MaxMessages:         intEnvOr("MCP_MAX_MESSAGES", 1000),
	DefaultDurationS:    intEnvOr("MCP_DEFAULT_MAX_DURATION_S", 30),
	MaxDurationS:        intEnvOr("MCP_MAX_DURATION_S", 30),
}
```

Then it creates the MCP server and attaches it to HTTP:

```go
srv := server.Build(cfg)
handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server {
	return srv
}, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true})

mux.Handle("/mcp", auth.Middleware(handler))
mux.HandleFunc("/healthz/live", healthOK)
mux.HandleFunc("/healthz/ready", healthOK)
```

Important detail: this service uses stateless MCP Streamable HTTP. Each tool
call is a bounded request/response operation. It does not currently maintain
long-lived background subscriptions for clients.

## MCP Server Construction

`internal/server/server.go` owns MCP server creation.

```go
srv := mcp.NewServer(&mcp.Implementation{
	Name:    "dsx-exchange-mcp",
	Version: "0.1.0",
}, nil)

registerTools(srv, cfg)
registerResources(srv)
```

The same `*mcp.Server` is returned for each HTTP request:

```go
// Build returns a singleton MCP server. The Streamable HTTP handler uses the
// same server for every request; per-request caller bearer tokens flow through
// context injected by auth.Middleware.
```

That means per-caller information must not be stored globally on the server
object. Caller-specific data flows through `context.Context`.

## Auth And Caller Credentials

Authentication is split between **HTTP request headers** (what this server
reads) and **broker enforcement** (what auth-callout decides at MQTT CONNECT /
SUBSCRIBE).

This server is not the identity policy engine. It extracts credentials from
the incoming HTTP request and, for broker-backed tools, delegates them to the
broker. Whoever calls `/mcp` — client, proxy, or gateway — must supply the
headers below.

### HTTP contract


| Header                        | Required                                       | Used for                                                                 |
| ----------------------------- | ---------------------------------------------- | ------------------------------------------------------------------------ |
| `Authorization: Bearer <jwt>` | Required for broker tools in `jwt_passthrough` | Delegated credential presented as MQTT password                          |
| `Mcp-Session-Id`              | Optional                                       | Session correlation in audit logs; relevant when a gateway pins sessions |
| `x-mcp-tenant`                | Optional                                       | Audit label                                                              |
| `x-mcp-issuer`                | Optional                                       | Audit label                                                              |
| `x-mcp-sub`                   | Optional                                       | Audit label                                                              |
| `x-mcp-spiffe-id`             | Optional                                       | Audit label                                                              |


The middleware in `internal/auth/caller.go`:

```go
caller := Caller{
	Bearer:    bearerFromHeader(r.Header.Get("Authorization")),
	SessionID: r.Header.Get("Mcp-Session-Id"),
	Tenant:    r.Header.Get("x-mcp-tenant"),
	Issuer:    r.Header.Get("x-mcp-issuer"),
	Subject:   r.Header.Get("x-mcp-sub"),
	SpiffeID:  r.Header.Get("x-mcp-spiffe-id"),
}
r = r.WithContext(WithCaller(r.Context(), caller))
```

The bearer is never accepted as a tool argument and is never logged. Audit logs
record only `bearer_present` plus the optional identity labels.

### MQTT auth modes

Controlled by `MCP_MQTT_AUTH_MODE`:


| Mode                        | HTTP bearer                      | MQTT CONNECT                                    |
| --------------------------- | -------------------------------- | ----------------------------------------------- |
| `jwt_passthrough` (default) | Required for broker-backed tools | `username=<MQTT_USERNAME>`, `password=<bearer>` |
| `noauth`                    | Ignored for MQTT                 | No username or password                         |


Broker-backed tools in `jwt_passthrough` return structured `missing_bearer`
when the request has no bearer. Schema tools work without a bearer in either
mode.

### Credential path to the broker

```text
HTTP Authorization: Bearer <jwt>
  -> auth.Middleware stores bearer in request context
  -> mqttbus.Collect receives caller.Bearer
  -> Paho MQTT SetPassword(bearer)
  -> NATS auth-callout validates token and enforces topic ACLs
```

Responsibility split:


| Layer                           | Responsibility                                                                    |
| ------------------------------- | --------------------------------------------------------------------------------- |
| MCP caller / proxy / gateway    | Obtain and attach caller credentials on each HTTP request                         |
| `dsx-exchange-mcp`              | Extract credentials, translate MCP tools to embedded specs and bounded MQTT reads |
| NATS/MQTT broker + auth-callout | Authenticate the delegated token (or noauth profile) and enforce topic ACLs       |


For gateway-specific auth interactions when a gateway is deployed, see
`docs/gateway-auth-interactions.md`.

## MCP Tools

Tool registration lives in `internal/server/tools.go`.

Current tools:


| Tool                          | Purpose                                                   | MQTT |
| ----------------------------- | --------------------------------------------------------- | ---- |
| `dsx_exchange_find_topics`    | Search embedded AsyncAPI index for relevant topics        | No   |
| `dsx_exchange_describe_topic` | Describe channel schema, retained/live behavior, examples | No   |
| `dsx_exchange_subscribe`      | Subscribe and collect a bounded batch of live messages    | Yes  |
| `dsx_exchange_read_retained`  | Drain retained messages for a topic filter                | Yes  |


The subscribe tool is registered like this:

```go
mcp.AddTool(srv, &mcp.Tool{
	Name:        toolSubscribe,
	Description: "Subscribe to DSX Exchange MQTT topics and return a bounded batch of messages.",
}, func(ctx context.Context, req *mcp.CallToolRequest, args subscribeArgs) (*mcp.CallToolResult, any, error) {
	return collectTool(ctx, cfg, toolSubscribe, args.TopicFilter, args.MaxMessages, args.MaxDurationS, false)
})
```

The two MQTT data tools eventually call `collectTool`, which:

1. Reads caller identity from context.
2. Applies max message and max duration defaults.
3. Calls MQTT collection.
4. Converts the result into MCP content.
5. Emits an audit log.

The MQTT call is direct:

```go
res, err := mqttbus.Collect(ctx, cfg.MQTT, caller.Bearer, topicFilter, mqttbus.CollectOptions{
	MaxMessages: maxMessages,
	MaxDuration: time.Duration(maxDurationS) * time.Second,
	RetainedOnly: retainedOnly,
})
```

If a tool fails, the service returns a structured MCP error result rather than a
raw Go error:

```go
return &mcp.CallToolResult{
	Content: []mcp.Content{&mcp.TextContent{Text: string(payload)}},
	IsError: true,
}, nil, nil
```

This matters for clients: the MCP transport request may succeed while the tool
result itself is an error.

## MCP Resources

Resource registration lives in `internal/server/resources.go`.

There is an index resource:

```go
mcp.AddResource(srv, &mcp.Resource{
	URI:         "dsx-exchange://specs/",
	Name:        "DSX Exchange spec index",
	MIMEType:    "application/json",
	Description: "Index of embedded DSX Exchange topic specifications.",
}, readIndex)
```

And one resource per embedded domain:

```go
uri := "dsx-exchange://specs/" + domain
mcp.AddResource(srv, &mcp.Resource{
	URI:      uri,
	Name:     "DSX Exchange " + domain + " spec",
	MIMEType: mimeTypeForSpec(domain),
}, readSpec(domain, uri))
```

The embedded specs come from the repository-root `schemas/` package:

```go
//go:embed README.md cloud-events-example.yaml asyncapi/*/*.yaml
var FS embed.FS
```

`make sync-specs` refreshes those files from the monorepo root `schemas/`:

```make
sync-specs:
	rm -rf schemas/asyncapi schemas/cloud-events-example.yaml schemas/README.md
	mkdir -p schemas
	cp -R $(SCHEMA_SRC)/. schemas/
```

Resource calls are therefore local file reads from embedded data. They do not
call NATS/MQTT.

## MQTT/NATS Client Behavior

The MQTT implementation is in `internal/mqttbus/client.go`.

The default username is:

```go
const DefaultUsername = "oauthtoken"
```

In `jwt_passthrough` mode, `Collect` requires a bearer token:

```go
if strings.TrimSpace(bearer) == "" {
	return CollectResult{}, &BusError{Code: ErrMissingBearer, Message: "missing caller bearer token"}
}
```

The MQTT client uses the caller bearer as the password:

```go
opts := mqtt.NewClientOptions().
	AddBroker(cfg.BrokerURL).
	SetClientID(fmt.Sprintf("dsx-exchange-mcp-%d", time.Now().UnixNano())).
	SetUsername(username).
	SetPassword(bearer).
	SetCleanSession(true).
	SetAutoReconnect(false)
```

Then it subscribes using the requested topic filter:

```go
token := c.Subscribe(topicFilter, 0, nil)
```

The collection loop stops for bounded reasons:


| Stop reason        | Meaning                                                               |
| ------------------ | --------------------------------------------------------------------- |
| `max_messages`     | Hit requested or configured message count.                            |
| `max_duration`     | Hit requested or configured duration.                                 |
| `retained_idle`    | Retained-read mode saw no more retained messages for the idle window. |
| `max_result_bytes` | Payload would exceed configured response size.                        |
| `client_cancelled` | Request context was cancelled.                                        |
| `completed`        | Normal completion path.                                               |


Payload conversion is also handled here. UTF-8 payloads are returned as strings;
non-UTF-8 payloads are base64 encoded:

```go
if utf8.Valid(payload) {
	msg.Payload = string(payload)
	msg.PayloadEncoding = "utf-8"
} else {
	msg.Payload = base64.StdEncoding.EncodeToString(payload)
	msg.PayloadEncoding = "base64"
}
```

### MQTT collection boundary

`internal/mqttbus/client.go` exposes `Collect` for bounded subscribe/read flows.
Each tool call creates a temporary MQTT client, collects messages until a limit
or timeout, then disconnects. There is no long-lived server-side subscription
state in the current public MCP surface.

## Kubernetes Deployment

The Helm chart under `deploy/helm/dsx-exchange-mcp` deploys the standalone
server as its own Deployment and Service. Gateway registration is optional and
configured in the gateway chart, not here.

Default values include two replicas and the NATS/MQTT endpoint:

```yaml
replicaCount: 2

natsURL: tcp://nats.nats.svc:1883

mqtt:
  authMode: jwt_passthrough
  username: oauthtoken
  connectTimeoutSeconds: 5
  subscribeTimeoutSeconds: 5
  maxResultBytes: 1048576
```

The Deployment maps those values into environment variables:

```yaml
- name: MCP_ADDR
  value: ":8080"
- name: NATS_URL
  value: {{ .Values.natsURL | quote }}
- name: MCP_MQTT_AUTH_MODE
  value: {{ .Values.mqtt.authMode | quote }}
- name: MQTT_USERNAME
  value: {{ .Values.mqtt.username | quote }}
- name: MCP_MAX_MESSAGES
  value: {{ .Values.limits.maxMessages | quote }}
- name: MCP_MAX_DURATION_S
  value: {{ .Values.limits.maxDurationSeconds | quote }}
```

The chart also configures health probes:

```yaml
livenessProbe:
  httpGet:
    path: /healthz/live
    port: mcp
readinessProbe:
  httpGet:
    path: /healthz/ready
    port: mcp
```

And a locked-down runtime profile:

```yaml
securityContext:
  runAsNonRoot: true
  readOnlyRootFilesystem: true
  capabilities:
    drop:
      - ALL
```

The default `values.yaml` also sets:

```yaml
runtimeClassName: kata
```

Local Kind overrides in `values.kind.yaml` use `MCP_MQTT_AUTH_MODE=noauth` and
point at the in-cluster Event Bus broker so the backend can be exercised without
a gateway or bearer token.

## Observability

There are two observability paths in the current code.

### Health

`cmd/dsx-exchange-mcp/main.go` exposes:

```text
/healthz/live
/healthz/ready
```

Both currently return HTTP 204 with no response body.

### Audit Logs

Every tool call emits an audit log from `internal/server/tools.go`:

```go
slog.Info("mcp tool call",
	"audit", true,
	"tool", tool,
	"caller_tenant", caller.Tenant,
	"caller_issuer", caller.Issuer,
	"caller_subject", caller.Subject,
	"caller_spiffe_id", caller.SpiffeID,
	"bearer_present", caller.Bearer != "",
	"topic_filter", topicFilter,
	"decision", decision,
	"message_count", messageCount,
	"stopped_reason", stoppedReason,
	"error_code", errorCode,
)
```

Use these logs to correlate caller identity labels, requested topic filter,
broker decision, result size, and error code.

## Local Development

Common Make targets:

```make
build:
	go build ./cmd/dsx-exchange-mcp

run: sync-specs build
	go run ./cmd/dsx-exchange-mcp

test:
	go test ./...
```

Direct local path:

```text
make run
# configure MCP client with http://127.0.0.1:8080/mcp
```

Kind path (Event Bus + MCP backend, no gateway):

```text
make -C local skaffold-run
make port-forward-kind
# configure MCP client with http://127.0.0.1:18080/mcp
```

## Optional Gateway Integration

When deployed behind a Latinum MCP Gateway, this server is one upstream backend
among potentially many. The gateway validates caller JWTs, applies coarse MCP
authorization, and forwards the original HTTP headers unchanged. From the
server's perspective the request flow is identical to a direct client call.

```text
MCP client
  -> Gateway /mcp
  -> Kubernetes Service dsx-exchange-mcp:<mcp port>
  -> pod /mcp
```

The Helm Service optionally advertises MCP to gateway discovery:

```yaml
ports:
  - name: mcp
    port: 8080
    targetPort: mcp
    appProtocol: agentgateway.dev/mcp
```

A gateway upstream entry targets this service by name, namespace, labels, port,
and pod selector. In multi-upstream gateway deployments, tool names may appear
with an upstream prefix (for example
`dsx-exchange-mcp-mcp_dsx_exchange_subscribe`). The exact external name depends
on gateway upstream naming.

See `docs/gateway-auth-interactions.md` for the full gateway ↔ upstream ↔ broker
auth matrix.

## What To Change For Common Tasks

### Add a new MCP tool

Start in:

```text
internal/server/tools.go
```

Add the tool registration next to the existing `mcp.AddTool` calls. If the tool
touches MQTT, prefer adding focused behavior in `internal/mqttbus` rather than
embedding client logic in the server layer.

### Change topic validation or MQTT error handling

Start in:

```text
internal/mqttbus/client.go
```

This file owns topic filter validation, connection setup, subscribe behavior,
message conversion, and broker error classification.

### Add or change embedded specs

Start with:

```text
make sync-specs
```

Then inspect:

```text
schemas/
internal/specs/specs.go
internal/server/resources.go
internal/schemaindex/index.go
```

### Change Service metadata for gateway discovery

Start in:

```text
deploy/helm/dsx-exchange-mcp/templates/service.yaml
deploy/helm/dsx-exchange-mcp/values.yaml
```

Only needed when registering the backend with an MCP gateway. Direct standalone
clients use the Service ClusterIP or a port-forward and do not depend on
`appProtocol`.

### Change runtime limits

Start in:

```text
deploy/helm/dsx-exchange-mcp/values.yaml
cmd/dsx-exchange-mcp/main.go
internal/server/tools.go
```

The chart sets deploy defaults, `main.go` reads env vars, and `tools.go` applies
bounds per request.

## Current Design Boundaries

The current implementation is intentionally thin:

1. It does not store durable watch state.
2. It does not maintain cross-pod subscription continuity.
3. It does not reimplement broker authorization.
4. It does not expose a long-lived async subscription API.
5. It does not persist MQTT messages outside the request.
6. It does not mint, refresh, or cache caller JWTs — every broker-backed tool
  call expects fresh credentials on the HTTP request.

That means a pod restart can interrupt an in-flight bounded tool call. Clients
should retry tool calls.
