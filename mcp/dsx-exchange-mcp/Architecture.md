# dsx-exchange-mcp Architecture

This document is for a new developer trying to understand how the code works. It is intentionally code-centric: which files own which behavior, how a request flows through the service, and how this MCP server plugs into Agent Gateway / Latinum MCP Gateway.

## Big Picture

`dsx-exchange-mcp` is an MCP server that exposes DSX exchange data over MCP.

At runtime it does three main things:

1. Serves MCP over HTTP at `/mcp`.
2. Exposes embedded exchange specs as MCP resources.
3. Exposes schema exploration and bounded MQTT/NATS reads as MCP tools.

In production it is expected to sit behind Gateway:

```text
MCP client
  -> Latinum / Agent Gateway
  -> Kubernetes Service: dsx-exchange-mcp
  -> dsx-exchange-mcp pod
  -> MQTT/NATS broker
```

The MCP server does not implement topic authorization itself. The caller JWT is passed through Gateway, forwarded to this service, then used as the MQTT password when connecting to NATS/MQTT. NATS auth callout / ACLs enforce topic access.

## Request Flow

For an MCP tool call such as `dsx_exchange_subscribe`:

```text
client
  sends MCP request with JWT
    |
    v
gateway
  validates identity
  forwards Authorization: Bearer <jwt>
  forwards x-mcp-* identity headers
    |
    v
cmd/dsx-exchange-mcp/main.go
  accepts HTTP /mcp
  wraps handler with auth.Middleware
    |
    v
internal/auth/context.go
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
  creates MQTT client
  uses bearer token as MQTT password
  subscribes to topic filter
  collects bounded messages
    |
    v
internal/server/tools.go
  writes audit log
  returns MCP result
```

For an MCP resource read, the flow stops inside `internal/specs`; no MQTT connection is opened.

## File Map

| Path | Responsibility |
| --- | --- |
| `cmd/dsx-exchange-mcp/main.go` | Process entrypoint. Reads env config, builds the MCP server, registers HTTP routes, starts `ListenAndServe`. |
| `internal/server/server.go` | Creates the MCP server instance and registers tools/resources. |
| `internal/server/tools.go` | Defines MCP tools, parses tool inputs, describes schema topics, enforces bounds, calls MQTT collection, and emits audit logs. |
| `internal/server/resources.go` | Defines MCP resources backed by embedded DSX specs. |
| `internal/specs/specs.go` | Exposes raw spec resources from the embedded `schemas/` tree. |
| `internal/schemaindex/index.go` | Parses AsyncAPI channel/message/operation primitives into a topic catalogue for schema exploration tools. |
| `schemas/` | Generated copy of the monorepo root `schemas/`, embedded into the binary by `schemas/embed.go`. |
| `internal/mqttbus/client.go` | MQTT/NATS client logic: connect, subscribe, collect messages, classify broker errors. |
| `internal/auth/context.go` | Pulls Gateway-provided bearer and identity headers into Go context. |
| `deploy/helm/dsx-exchange-mcp/templates/deployment.yaml` | Kubernetes Deployment: env vars, probes, security context, runtime class. |
| `deploy/helm/dsx-exchange-mcp/templates/service.yaml` | Kubernetes Service that Gateway discovers/routes to. |
| `deploy/helm/dsx-exchange-mcp/values.yaml` | Default deploy-time configuration. |

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
}, nil)

mux.Handle("/mcp", auth.Middleware(handler))
mux.HandleFunc("/healthz/live", healthOK)
mux.HandleFunc("/healthz/ready", healthOK)
```

Important detail: this service uses MCP Streamable HTTP, but the current tools are bounded request/response calls. It does not currently maintain long-lived background subscriptions for clients.

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

That means per-caller information must not be stored globally on the server object. Caller-specific data flows through `context.Context`.

## Auth And JWT Passthrough

`internal/auth/context.go` extracts identity from the incoming HTTP request.

Gateway is expected to forward:

| Header | Used for |
| --- | --- |
| `Authorization: Bearer <jwt>` | Delegated credential used as MQTT password. |
| `x-mcp-tenant` | Audit label. |
| `x-mcp-issuer` | Audit label. |
| `x-mcp-sub` | Audit label. |
| `x-mcp-spiffe-id` | Audit label. |

The middleware:

```go
caller := Caller{
	Bearer:   bearerFromHeader(r.Header.Get("Authorization")),
	Tenant:   r.Header.Get("x-mcp-tenant"),
	Issuer:   r.Header.Get("x-mcp-issuer"),
	Subject:  r.Header.Get("x-mcp-sub"),
	SpiffeID: r.Header.Get("x-mcp-spiffe-id"),
}
r = r.WithContext(context.WithValue(r.Context(), ctxKey{}, caller))
```

The code comment describes the intended trust boundary:

```go
// The raw bearer is used only as the delegated credential for the MQTT/NATS
// password. The x-mcp-* fields are audit labels emitted by gateway ext_authz.
```

So this service is not the main identity policy engine. It preserves identity for audit and delegates topic authorization to the broker by connecting with the caller token.

## MCP Tools

Tool registration lives in `internal/server/tools.go`.

Current tools:

| Tool | Purpose |
| --- | --- |
| `dsx_exchange_describe_topic` | Describe the AsyncAPI channel matching a topic filter, including payload shape, retained/live behavior, examples, and related metadata/value topics. |
| `dsx_exchange_subscribe` | Subscribe to a topic filter and collect a bounded batch of live messages. |
| `dsx_exchange_read_retained` | Subscribe briefly and return retained messages for a topic filter. |

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

If a tool fails, the service returns a structured MCP error result rather than a raw Go error:

```go
return &mcp.CallToolResult{
	Content: []mcp.Content{&mcp.TextContent{Text: string(payload)}},
	IsError: true,
}, nil, nil
```

This matters for clients: the MCP transport request may succeed while the tool result itself is an error.

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

Resource calls are therefore local file reads from embedded data. They do not call NATS/MQTT.

`dsx_exchange_describe_topic` also does not call NATS/MQTT. It reads the embedded AsyncAPI catalogue through `internal/schemaindex`.

## MQTT/NATS Client Behavior

The MQTT implementation is in `internal/mqttbus/client.go`.

The default username is:

```go
const DefaultUsername = "oauthtoken"
```

`Collect` requires a bearer token:

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

| Stop reason | Meaning |
| --- | --- |
| `max_messages` | Hit requested or configured message count. |
| `max_duration` | Hit requested or configured duration. |
| `retained_idle` | Retained-read mode saw no more retained messages for the idle window. |
| `max_result_bytes` | Payload would exceed configured response size. |
| `client_cancelled` | Request context was cancelled. |
| `completed` | Normal completion path. |

Payload conversion is also handled here. UTF-8 payloads are returned as strings; non-UTF-8 payloads are base64 encoded:

```go
if utf8.Valid(payload) {
	msg.Payload = string(payload)
	msg.PayloadEncoding = "utf-8"
} else {
	msg.Payload = base64.StdEncoding.EncodeToString(payload)
	msg.PayloadEncoding = "base64"
}
```

### Current Streaming Boundary

`internal/mqttbus/client.go` also has a lower-level `Stream` function:

```go
// Stream opens an MQTT subscription and invokes onMessage for every received
// message until the context is cancelled or a bound is reached. It is intended
// for async task workers that need to persist messages outside this package.
```

That is scaffolding for a future async/background watch design. It is not currently registered as an MCP tool. The current MQTT data tools collect bounded batches inside the request lifecycle.

## Gateway Integration

In production, MCP clients should normally talk to Gateway, not directly to the pod.

The intended Gateway-facing shape is:

```text
MCP client
  -> Gateway /mcp
  -> upstream route for dsx-exchange-mcp
  -> Kubernetes Service dsx-exchange-mcp:<mcp port>
  -> pod /mcp
```

This repo's Helm Service advertises the MCP port:

```yaml
ports:
  - name: {{ .Values.service.portName }}
    port: {{ .Values.service.port }}
    targetPort: http
    protocol: TCP
    appProtocol: agentgateway.dev/mcp
```

The important field is:

```yaml
appProtocol: agentgateway.dev/mcp
```

That tells Gateway discovery that this service port speaks MCP.

A Gateway upstream entry is expected to target this service by service name, namespace, labels, port, and pod selector. The README shows the shape:

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

In multi-upstream Gateway deployments, tool names may be exposed with an upstream prefix. For example, the local tool `dsx_exchange_subscribe` may appear to an external client as something like:

```text
dsx-exchange-mcp-mcp_dsx_exchange_subscribe
```

The exact external name depends on Gateway's upstream naming behavior.

### JWT Passthrough Contract

The service expects Gateway to forward the caller token:

```text
Authorization: Bearer <caller JWT>
```

The service does not exchange this token. It passes it to MQTT/NATS as the password:

```text
Gateway-validated JWT
  -> Authorization header to dsx-exchange-mcp
  -> auth.Middleware stores bearer in context
  -> mqttbus.Collect receives caller.Bearer
  -> Paho MQTT SetPassword(bearer)
  -> NATS auth callout / ACL policy
```

This gives a clean responsibility split:

| Component | Responsibility |
| --- | --- |
| Gateway | Validate incoming identity, route MCP traffic, forward delegated identity. |
| `dsx-exchange-mcp` | Translate MCP resources/tools to local embedded specs and MQTT reads. |
| NATS/MQTT broker | Authenticate delegated token and enforce topic ACLs. |

## Kubernetes Deployment

The Helm chart under `deploy/helm/dsx-exchange-mcp` owns production deployment shape.

Default values include two replicas and the NATS/MQTT endpoint:

```yaml
replicaCount: 2

natsURL: tcp://nats.nats.svc:1883

mqtt:
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
    port: http
readinessProbe:
  httpGet:
    path: /healthz/ready
    port: http
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

That means pods are intended to run with the configured Kata runtime class in the target cluster.

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

These logs are where you correlate Gateway identity, requested topic filter, broker decision, result size, and error code.

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


## What To Change For Common Tasks

### Add a new MCP tool

Start in:

```text
internal/server/tools.go
```

Add the tool registration next to the existing `mcp.AddTool` calls. If the tool touches MQTT, prefer adding focused behavior in `internal/mqttbus` rather than embedding client logic in the server layer.

### Change topic validation or MQTT error handling

Start in:

```text
internal/mqttbus/client.go
```

This file owns topic filter validation, connection setup, subscribe behavior, message conversion, and broker error classification.

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

### Change Gateway-facing deployment metadata

Start in:

```text
deploy/helm/dsx-exchange-mcp/templates/service.yaml
deploy/helm/dsx-exchange-mcp/values.yaml
```

Gateway discovery depends on the Service name, labels, port name, and `appProtocol`.

### Change runtime limits

Start in:

```text
deploy/helm/dsx-exchange-mcp/values.yaml
cmd/dsx-exchange-mcp/main.go
internal/server/tools.go
```

The chart sets deploy defaults, `main.go` reads env vars, and `tools.go` applies bounds per request.

## Current Design Boundaries

The current implementation is intentionally thin:

1. It does not store durable watch state.
2. It does not maintain cross-pod subscription continuity.
3. It does not reimplement broker authorization.
4. It does not expose a long-lived async subscription API yet.
5. It does not persist MQTT messages outside the request.

That means a pod restart can interrupt an in-flight bounded tool call. Clients should retry tool calls. If future UX requires long-lived background watches, the likely next code boundary is to build around `mqttbus.Stream` with an explicit task/watch model and a bounded external or broker-backed message store.
