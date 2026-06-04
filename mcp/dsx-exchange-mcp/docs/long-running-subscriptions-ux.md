# Long-Running Subscription UX User Stories

This note captures the end-user experience expected from long-running DSX
Exchange MQTT subscriptions exposed through MCP. It should be considered as an
input to the DSX Exchange MCP SDD.

## UX Model

End users should not interact with raw MQTT clients or unbounded protocol
streams directly. They should ask an agent to watch a factory signal, and the
agent should use MCP tools to create, inspect, summarize, export, and stop a
managed background subscription.

The baseline flow is:

```text
User request
  -> agent starts a subscription through MCP
  -> dsx-exchange-mcp keeps the MQTT subscription active in the background
  -> server buffers matching messages under a subscription_id
  -> user asks for status, summaries, aggregations, raw batches, or export
  -> agent stops the subscription, or TTL/idle expiry cleans it up
```

Streamable HTTP and SSE notifications may improve the live experience, but they
should not be the only way to consume data. Notifications should be lightweight
signals that new data or a state change is available. The reliable contract is
cursor-based reads and server-side summaries over bounded buffers.

## User Stories

| ID | Priority | User | I want... | So that... |
| --- | --- | --- | --- | --- |
| LSUB-1 | P0 | Agent Developer | start a long-running Exchange subscription and receive a `subscription_id` immediately | my agent can monitor live factory signals without holding one tool call open forever |
| LSUB-2 | P0 | AI Factory Operator | ask an agent to watch a topic or domain in natural language | I do not need to know raw MQTT topic hierarchy or keep a terminal open |
| LSUB-3 | P0 | Agent Developer | read buffered messages by cursor with bounded `max_messages` and `max_bytes` limits | my agent can safely process live events without memory spikes or timeouts |
| LSUB-4 | P0 | AI Factory Operator | ask "what happened since I started watching?" | I can get a concise operational summary instead of a raw event dump |
| LSUB-5 | P0 | Site Reliability Engineer | query subscription status such as running, reconnecting, expired, denied, or buffer_overflow | I can understand whether silence means no events or a broken watch |
| LSUB-6 | P1 | Agent Developer | receive optional MCP/SSE notifications when new messages arrive or status changes | clients that support live updates can react quickly without polling aggressively |
| LSUB-7 | P1 | AI Factory Operator | ask for aggregations over the background stream, such as counts, latest values, min/max/avg, or grouping by topic/object type | I can reason about trends and thresholds without pulling every raw message into the model |
| LSUB-8 | P1 | Site Reliability Engineer | dump a bounded raw batch of subscription messages in JSON/JSONL | I can inspect exact event payloads during debugging |
| LSUB-9 | P1 | AI Factory Operator | export a subscription to an approved observability sink such as Flight Recorder or logs | incident evidence can be retained without exposing arbitrary exfiltration paths |
| LSUB-10 | P0 | Security Reviewer | have every subscription start, read, aggregation, export, and stop audited with caller identity and arguments | I can reconstruct agent behavior during incidents |
| LSUB-11 | P0 | AI Factory Operator | stop a subscription explicitly or let it expire by TTL/idle timeout | background watches do not run forever by accident |
| LSUB-12 | P1 | Agent Developer | receive structured errors for ACL denial, authentication failure, reconnect exhaustion, buffer overflow, and expired subscriptions | my agent can recover or explain the failure to the operator |

## Expected User Interactions

Examples of natural-language requests:

```text
Watch BMS leak events for row 3 and tell me if anything changes.
Summarize rack power changes from the last 10 minutes.
Show the latest value per CDU from this watch.
Count NICO state transitions by state since I started watching.
Dump the raw events for the last 5 minutes.
Export this watch to Flight Recorder for one hour.
Stop watching rack leak events.
```

The agent translates these requests into MCP tool calls such as:

```text
dsx_exchange_start_subscription(...)
dsx_exchange_read_subscription(...)
dsx_exchange_subscription_status(...)
dsx_exchange_summarize_subscription(...)
dsx_exchange_aggregate_subscription(...)
dsx_exchange_export_subscription(...)
dsx_exchange_stop_subscription(...)
```

## Notification Behavior

When supported by the MCP client, `dsx-exchange-mcp` can emit lightweight
server-to-client notifications over Streamable HTTP/SSE. Notifications should
announce availability or state, not carry unbounded payloads.

Example notification payload:

```json
{
  "subscription_id": "sub_123",
  "event": "messages_available",
  "count": 17,
  "severity": "warning",
  "summary": "Rack leak event observed for rack R12"
}
```

Clients that do not expose notifications should still work by polling
`status_subscription` and `read_subscription`.

## Data Access Modes

Long-running subscription UX should support three data access modes:

| Mode | Purpose | Guardrails |
| --- | --- | --- |
| Notifications | Tell the client that new data or a status change exists | lightweight only; no large payloads |
| Cursor reads | Retrieve bounded raw or normalized message batches | required cursor, message cap, byte cap |
| Summaries and aggregations | Answer operational questions without dumping every message to the model | bounded windows, explicit groupings, sample examples |

Export is a fourth mode, but only to approved sinks. It should be separately
authorized and audited because unrestricted raw export can become a data
exfiltration path.

## SDD Implications

The SDD should describe long-running subscriptions as a managed lifecycle, not
as an infinite `tools/call` response.

Key design implications:

- `start_subscription` returns quickly with a `subscription_id`, status, cursor,
  and TTL.
- Active MQTT subscriptions are owned by the session-pinned `dsx-exchange-mcp`
  pod.
- `Mcp-Session-Id` and agentgateway stateful routing keep follow-up reads and
  status calls on the owning pod.
- The server stores messages in bounded per-subscription buffers with explicit
  overflow policy.
- Optional SSE/MCP notifications are an acceleration path; cursor reads are the
  reliable baseline.
- Server-side aggregation and summarization tools should be first-class for
  high-volume streams.
- Raw dumps must be bounded. Long-running export should target approved sinks
  only.
- v1 can treat pod restart or session loss as subscription loss requiring
  resubscription. Durable cross-pod replay is a future requirement unless
  explicitly added to v1 scope.

