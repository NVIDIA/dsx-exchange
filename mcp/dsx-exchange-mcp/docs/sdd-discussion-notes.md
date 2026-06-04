# DSX Exchange MCP SDD Discussion Notes

This note captures future SDD topics for `dsx-exchange-mcp`. The near-term
assumption is that the MCP server supplements the existing DSX Event Bus and
does not change NATS, MQTT, broker auth, or auth-callout behavior.

## Current Constraint

The current MCP server can only learn effective MQTT access by attempting MQTT
operations with the caller bearer. It does not have a supported API to ask
auth-callout for the caller's effective NATS permissions before connecting.

Broker enforcement remains authoritative:

1. The MCP gateway validates the caller and forwards the bearer.
2. `dsx-exchange-mcp` presents that bearer as the MQTT password.
3. NATS/auth-callout validates the bearer and returns NATS user permissions.
4. The broker enforces subscribe and publish ACLs on the MQTT connection.

## What Can Be Done With Only The MCP Server

### ACL Probe By MQTT Connect

For discovery or preflight, the MCP server can open a short-lived MQTT
connection with the caller bearer and attempt a bounded subscribe against one
or more candidate filters.

Example uses:

- Check whether `BMS/v1/PUB/Metadata/#` is readable before showing the BMS
  schema resource.
- Check whether `NICO/v1/#` is denied and avoid exposing NICO-specific tools or
  resources to that caller.
- Validate a user-supplied `topic_filter` before spending a longer collection
  window on it.

This is accurate because the broker is the final enforcement point. It is also
expensive and should be bounded.

Recommended limits:

- short connect timeout
- short subscribe timeout
- max probe count per request
- no payload return for pure authorization probes
- cache result for a short TTL no longer than token expiry
- fail closed when probe auth or connectivity is ambiguous

### Schema Filtering By Probe

Until an entitlement API exists, schema/resource filtering can be approximated
by probing canonical topic filters for each schema domain.

Example mapping:

| Schema | Probe Filter |
| --- | --- |
| `bms` | `BMS/v1/PUB/Metadata/#` |
| `nico` | `NICO/v1/#` |
| `power-management` | approved power-management metadata/event prefix |

If the probe succeeds, expose that schema resource. If it fails with ACL denial,
hide the schema. If it fails due to broker/network availability, fail closed or
return a degraded discovery error rather than exposing everything.

This is a tactical POC approach, not the preferred production design.

## MQTT Client Reuse

MQTT connections are authenticated at connect time. A connection carries the
effective broker identity and ACLs produced by auth-callout for that caller.
Reusing a connection across callers can leak broader permissions.

Safe rule:

> Do not share MQTT clients across distinct effective caller identities or ACL
> sets.

For the current MCP server, the safest model is one short-lived MQTT client per
tool call. That is simple, stateless, and avoids cross-session privilege bleed.

If connection reuse becomes necessary for scale, pool only by a key that
captures the effective broker authorization context:

- issuer
- subject
- authorized party / service id
- tenant or persona
- broker account
- token scopes
- token expiry
- policy version or permissions hash, if available

Without a policy version or entitlement hash, pooling should be conservative:
per caller token or per exact identity with short TTL, never cross-tenant and
never cross-persona.

## Scalability Considerations

One MQTT connection per tool call is acceptable for a bounded POC, but it has
costs:

- connection setup latency on every tool call
- TLS handshake cost
- broker auth-callout load for repeated connects
- broker connection churn
- higher tail latency when agents call multiple filters in sequence

Mitigations that do not require event-bus changes:

- cache ACL probe results for a short TTL
- cap concurrent MQTT connections per pod
- cap concurrent probes per caller/session
- prefer broad-but-approved discovery probes over many narrow probes
- keep subscribe/read tools bounded by messages, duration, and result bytes
- expose metrics for active MQTT connections, connect failures, subscribe ACL
  failures, probe cache hits, and per-caller throttling

If sustained long-running subscriptions are required, they should be designed
separately. The default MCP tool model should remain bounded request/response.

## Future SDD Topic: Entitlement API

A production design should discuss adding a read-only entitlement or
authorization-check API to auth-callout, or to a sibling policy service that
uses the same permission manager.

Possible API shapes:

- `POST /v1/authorize` for exact checks such as `mqtt.subscribe` on a topic
  filter.
- `GET /v1/mcp-entitlements` for schema/tool discovery filtering.

The API must not become a second source of truth. It should expose the same
effective decision that auth-callout would apply during MQTT connection setup,
while the broker remains the final enforcement point.

Open approval questions for the SDD:

- Is auth-callout allowed to expose effective permissions to MCP backends?
- Should tenant callers receive raw topic allowlists or only coarse schema
  capabilities?
- What identity should the MCP server use when calling the entitlement API?
- What are the cache TTL and invalidation rules when permissions hot-reload?
- Should entitlement failures fail closed for all personas?
- Does connection pooling require an explicit policy version or ACL hash?

## Future SDD Topic: Long-Lived MQTT Subscriptions

The SDD should distinguish bounded MCP collection tools from long-lived MQTT
subscriptions. A normal `tools/call` should not be treated as an unbounded raw
MQTT stream. Streamable HTTP can carry SSE-framed responses and notifications,
but long-running firehose-style tool calls create poor UX and scaling pressure.

Recommended v1 posture:

- Keep existing read/subscribe tools bounded by message count, duration, and
  result bytes.
- Add a managed subscription control plane only if sustained monitoring is
  required.
- Treat live MQTT delivery as a background activity owned by the MCP session's
  backend pod.
- Prefer bounded reads, cursors, and summaries as the model-facing data path.
- Use server-to-client notifications as an optional acceleration path, not the
  only way for the client to observe updates.

Possible managed-subscription tools:

- `dsx_exchange_start_subscription(topic_filter, ttl_s, buffer_max_messages,
  buffer_max_bytes, drop_policy)`
- `dsx_exchange_read_subscription(subscription_id, cursor, max_messages)`
- `dsx_exchange_subscription_status(subscription_id)`
- `dsx_exchange_list_subscriptions()`
- `dsx_exchange_stop_subscription(subscription_id)`

The start call should return quickly with a subscription acknowledgement:

```json
{
  "subscription_id": "sub_123",
  "status": "running",
  "next_cursor": "0",
  "resource_uri": "dsx-exchange://subscriptions/sub_123"
}
```

The MCP client can then poll/read bounded batches or ask for summaries. Clients
that support MCP resource subscriptions or Streamable HTTP server-to-client
notifications can additionally receive update notifications and decide when to
read the buffered data.

## Future SDD Topic: Stateful Sessions And Pod Ownership

Long-lived subscription state should be tied to stateful MCP sessions. With
Agentgateway selector-based targets and stateful session routing, the gateway
can pin subsequent requests carrying the same `Mcp-Session-Id` to the same
resolved upstream pod.

Recommended v1 posture:

- Use Streamable HTTP for remote MCP transport.
- Require stateful MCP sessions for managed subscriptions.
- Use selector-based Agentgateway upstream targets so gateway-managed
  session pinning can resolve and pin individual backend pods.
- Keep active subscription state in the owning `dsx-exchange-mcp` pod memory.
- Accept that pod restart, eviction, or session loss terminates in-memory
  subscriptions and requires client resubscription.
- Do not require Redis, Valkey, or other shared state in v1 unless recovery,
  cross-pod visibility, or durable replay becomes a requirement.

State kept in the owning pod should include:

- MCP session ID
- caller identity / auth context fingerprint
- subscription ID
- topic filter
- MQTT connection key
- subscription status
- buffer cursor and ring-buffer state
- last broker error or disconnect reason
- TTL and idle-expiry timestamps

This model scales by adding pods. New sessions are distributed across pods by
the gateway's initial routing decision; existing sessions continue to the
pinned backend pod.

## Future SDD Topic: MQTT Connection Strategy For Subscriptions

The SDD should explicitly separate logical MCP subscriptions from MQTT
connections. One MQTT connection can carry many topic subscriptions, but it is
authenticated at MQTT CONNECT time and therefore carries the permissions of the
bearer used as its password.

Unsafe rule:

> Do not use one shared MQTT connection per pod for all callers.

Safer rule:

> Pool MQTT connections per pod by broker configuration and effective caller
> authorization context.

A conservative pool key should include:

- broker URL and TLS config
- MQTT username
- issuer
- subject or authorized party / service ID
- audience
- scopes
- tenant or persona
- token expiry bucket
- policy version or permissions hash, if available

For demo traffic that uses one SSA service account, sharing a connection across
sessions from that same effective auth context may be acceptable. For production
traffic with distinct users, tenants, personas, or bearer scopes, connections
must not be shared across auth boundaries.

The pod should support:

- many MCP sessions
- multiple logical subscriptions per MCP session
- multiple MQTT connections keyed by auth context
- many topic filters per eligible MQTT connection
- internal fan-out from MQTT messages to per-subscription buffers

If multiple active subscriptions share one MQTT client, the implementation needs
a demux layer:

1. MQTT message arrives.
2. Match the topic to active filters owned by the same auth context.
3. Write the message or an aggregate into each matching subscription buffer.
4. Emit optional status/update notifications.

The SDD should also define unsubscribe reference counting so one logical
subscription cannot remove a broker subscription still needed by another
logical subscription.

## Future SDD Topic: Lifecycle, Failure, And Backpressure

Managed subscriptions need explicit lifecycle and safety behavior:

- `start_subscription` and `stop_subscription` should be idempotent where
  possible.
- Each subscription must have TTL and idle timeout controls.
- Token expiry must close or recreate affected MQTT connections.
- If the server cannot refresh a caller bearer, subscriptions tied to that
  bearer must end before or at token expiry.
- Broker disconnects and reconnect attempts should update subscription status.
- Topic ACL denial should surface as authorization failure, not as an empty
  stream.
- MQTT connect auth failures should surface as authentication failure.
- Buffer overflow policy must be explicit: drop oldest, drop newest, aggregate,
  or fail the subscription.
- Rolling deploys should either drain subscriptions gracefully or document that
  clients must resubscribe after session loss.

Suggested lifecycle/status notifications:

- subscribed
- unsubscribed
- reconnecting
- disconnected
- authorization_denied
- authentication_failed
- buffer_overflow
- expired

The SDD should define per-pod limits and metrics:

- active MCP sessions
- active logical subscriptions
- active MQTT connections
- broker subscriptions
- MQTT messages/sec in
- MCP/SSE notifications/sec out
- buffered messages and bytes
- dropped messages
- authn/authz failures
- reconnect count
- event loop / handler latency

## Recommended SDD Position For V1

The SDD should likely state:

> The DSX Exchange MCP server uses Streamable HTTP with stateful MCP sessions.
> Agentgateway selector-based session routing pins a client session to one
> `dsx-exchange-mcp` pod. For v1, active MQTT subscription state is held in that
> owning pod's memory and is lost on pod restart. MQTT connections are pooled
> per pod by broker config and caller authorization context, not globally. A
> pooled MQTT connection may carry multiple topic subscriptions for the same
> auth context. The server fans incoming MQTT messages into bounded
> per-subscription buffers. MCP clients consume those buffers through explicit
> read/status calls, with optional server-to-client notifications for clients
> that support live updates.

## Future SDD Topic: Schema Discovery Based On MQTT ACLs

MCP schema/resource visibility should correspond to the caller's effective MQTT
subscribe authorization. A caller should not see DSX Exchange schema domains or
channels that they cannot subscribe to through the broker. The MCP server must
not trust client-provided claims alone for schema visibility.

Broker enforcement remains the final source of truth:

1. Gateway validates the caller and forwards the bearer.
2. `dsx-exchange-mcp` uses the bearer for MQTT when reading data.
3. NATS/auth-callout mints NATS permissions for that bearer.
4. The broker enforces topic subscribe ACLs.

The open design problem is how `dsx-exchange-mcp` can filter `/schema` or
`dsx-exchange://specs/*` discovery without brute-force probing every possible
topic.

### Tactical POC Approach: Canonical ACL Probes

If the MCP server only has a caller bearer and no entitlement API, the only
fully accurate authorization check is to ask the broker:

```text
MQTT CONNECT with caller bearer
MQTT SUBSCRIBE candidate filter
observe SUBACK success or denial
```

This should not enumerate concrete topics. At most, probe a small number of
canonical schema filters:

| Schema / Domain | Candidate Probe |
| --- | --- |
| BMS metadata | `BMS/v1/PUB/Metadata/#` |
| BMS values | `BMS/v1/PUB/Value/#` |
| NICO | `NICO/v1/#` |
| Power management | canonical power-management prefix |

Limitations:

- A broad probe can produce false negatives for narrow ACLs. For example, a
  caller allowed only `BMS/v1/PUB/Metadata/Rack/#` may be denied on
  `BMS/v1/PUB/Metadata/#` even though part of the BMS schema is relevant.
- Broker or network errors are ambiguous and should fail closed or return a
  degraded discovery result.
- Probe results should be cached only for a short TTL no longer than token
  expiry.
- Probe count and timeout must be tightly bounded.

This is acceptable for a POC or demo, but it is not the preferred production
design.

### Preferred Approach: Entitlement Or ACL Introspection API

The cleaner design is an internal read-only authorization API backed by the
same permission manager used by auth-callout. Today auth-callout resolves:

```text
JWT claims: sub / azp / scope
  -> UserProfile
  -> jwt.Permissions
  -> pub/sub allow/deny ACLs
  -> signed NATS user JWT
```

The missing capability is a supported internal API that exposes the effective
decision or effective permissions for schema filtering. Possible shapes:

```http
POST /v1/authorize
{
  "token": "<caller jwt>",
  "action": "mqtt.subscribe",
  "topic_filter": "BMS/v1/PUB/Metadata/#"
}
```

or:

```http
POST /v1/effective-permissions
{
  "token": "<caller jwt>",
  "protocol": "mqtt"
}
```

Example response:

```json
{
  "subject": "...",
  "azp": "...",
  "account": "...",
  "subscribe": {
    "allow": ["BMS.v1.PUB.Metadata.>", "BMS.v1.PUB.Value.Rack.>"],
    "deny": ["NICO.v1.>"]
  },
  "policy_version": "..."
}
```

The API must not become a second source of truth. It should expose the same
effective permissions that auth-callout would apply while minting the NATS user
JWT. The broker still enforces the final data access decision.

### Best Product Contract: Schema Entitlements

Rather than exposing raw topic ACLs directly to MCP clients, the authorization
service or MCP server can map effective permissions into schema capabilities:

```json
{
  "resources": ["dsx-exchange://specs/bms"],
  "channels": ["bms.rackMetadata", "bms.rackValue"],
  "recommended_filters": [
    "BMS/v1/PUB/Metadata/Rack/#",
    "BMS/v1/PUB/Value/Rack/#"
  ]
}
```

This gives agents a cleaner discovery surface while keeping the raw ACLs
internal. Broker SUBACK remains final enforcement for actual MQTT reads.

### Pattern Intersection Instead Of Topic Enumeration

The MCP server should maintain a compiled schema access index derived from
AsyncAPI channels:

```text
Schema channel: bms.rackMetadata
MQTT filter:    BMS/v1/PUB/Metadata/Rack/#
NATS filter:    BMS.v1.PUB.Metadata.Rack.>

Schema channel: bms.rackValue
MQTT filter:    BMS/v1/PUB/Value/Rack/#
NATS filter:    BMS.v1.PUB.Value.Rack.>
```

NATS MQTT topic/subject conversion:

| MQTT | NATS |
| --- | --- |
| `/` | `.` |
| `+` | `*` |
| `#` | `>` |

At discovery time:

```text
1. Get caller effective subscribe ACLs.
2. For each schema channel in the access index:
     if channel filter intersects acl.sub.allow
     and is not fully excluded by acl.sub.deny:
       expose channel/resource
3. Return filtered schema index/resources.
```

Examples:

```text
ACL allow: BMS.v1.PUB.Metadata.>
Schema:    BMS.v1.PUB.Metadata.Rack.>
Result:    visible

ACL allow: BMS.v1.PUB.Metadata.Rack.>
Schema:    BMS.v1.PUB.Metadata.CDU.>
Result:    hidden

ACL allow: BMS.v1.PUB.>
ACL deny:  BMS.v1.PUB.Metadata.Secret.>
Schema:    BMS.v1.PUB.Metadata.>
Result:    partially visible; remove or annotate denied channel subset
```

The SDD should define deny precedence and how to represent partially visible
schemas. Conservative behavior is to hide a channel when deny/allow
intersection cannot be represented safely.

### Recommended SDD Position

The SDD should likely state:

> Schema/resource discovery is filtered from the caller's effective MQTT
> subscribe authorization. The MCP server must not infer schema access from
> untrusted client claims alone. Broker enforcement remains the source of truth
> for final message access, but discovery filtering should use an internal
> entitlement API backed by the same permission manager that auth-callout uses
> to mint NATS user JWTs.

And:

> The MCP server maintains a compiled schema access index that maps AsyncAPI
> resources and channels to canonical MQTT topic filters and equivalent NATS
> subject filters. Given effective subscribe allow/deny filters, the server
> computes visible schema resources by wildcard-pattern intersection. It does
> not enumerate concrete topic instances.

Open approval questions:

- Can auth-callout expose effective permissions or authorization decisions to
  MCP backends?
- Should MCP clients see raw topic ACLs, schema capabilities, or only filtered
  resources?
- What identity and credential does `dsx-exchange-mcp` use to call the
  entitlement API?
- How are policy version, hot reload, and cache invalidation represented?
- How should partially visible schema domains be presented?
- Should ambiguous entitlement failures fail closed for all callers?
