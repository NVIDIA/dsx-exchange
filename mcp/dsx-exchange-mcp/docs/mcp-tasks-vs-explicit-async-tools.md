# MCP Tasks vs Explicit Async Tools

This note compares two ways for `dsx-exchange-mcp` to expose long-running MQTT
subscriptions through MCP.

## Summary

`dsx-exchange-mcp` can provide asynchronous subscription behavior even without
native MCP Tasks by exposing ordinary tools that create, inspect, fetch, and
cancel background work. Native MCP Tasks move that same lifecycle into the MCP
protocol so the client or host can understand and manage it generically.

The backend distributed-systems requirements are mostly the same in both cases:
Valkey-backed task state, worker lease/heartbeat, bounded result buffers,
cancellation, expiry, no raw JWT persistence, and clear failover semantics.

## Explicit Async Tools

In this model, async behavior is represented as normal MCP tools.

Example tool surface:

| Tool | Purpose |
| --- | --- |
| `dsx_exchange_start_subscription` | Starts a background MQTT watch and returns immediately with a task/subscription ID. |
| `dsx_exchange_task_status` | Reads task state, heartbeat, counters, expiry, and error information. |
| `dsx_exchange_task_result` | Reads buffered or final subscription results. |
| `dsx_exchange_cancel_task` | Requests cooperative cancellation. |
| `dsx_exchange_list_tasks` | Lists recent tasks visible to the caller. |

Typical flow:

```text
MCP client
  -> tools/call dsx_exchange_start_subscription(...)
  <- { "task_id": "watch_123", "status": "working", "poll_interval_s": 5 }

MCP client
  -> tools/call dsx_exchange_task_status({ "task_id": "watch_123" })
  <- { "status": "working", "message_count": 42 }

MCP client
  -> tools/call dsx_exchange_task_result({ "task_id": "watch_123" })
  <- { "status": "completed", "messages": [...] }
```

This is still asynchronous because the initial tool call does not hold the HTTP
request open for the full MQTT subscription lifetime. It returns a handle, while
the server continues work in the background.

The downside is that the async contract lives in tool descriptions and agent
behavior. The agent must remember the task ID, decide when to poll, choose when
to fetch results, and call the correct cancellation tool.

## Native MCP Tasks

Native MCP Tasks represent async work at the protocol level.

Expected flow:

```text
MCP client
  -> tools/call dsx_exchange_watch(...) with task support
  <- CreateTaskResult / task_id

MCP host or client
  -> tasks/get(task_id)
  <- task status/progress

MCP host or client
  -> tasks/result(task_id)
  <- final CallToolResult

MCP host or client
  -> tasks/cancel(task_id)
  <- cancellation acknowledgement
```

Native Tasks let the server advertise task capability and allow the MCP host to
own the async loop instead of relying on the model to learn a custom tool
workflow.

## Added Benefit of Native MCP Tasks

| Benefit | Why it matters |
| --- | --- |
| Host-owned polling | The MCP client/host can poll `tasks/get` without relying on the LLM to remember to call a status tool. |
| Protocol-owned task ID | The task ID is part of MCP state, not just text in a tool result. |
| Standard status UX | Clients can show running, completed, failed, cancelled, progress, and result availability consistently. |
| Deferred result retrieval | `tasks/result` provides a standard path to fetch the final tool result later. |
| Capability negotiation | The server can advertise which tools support task mode. |
| Tool execution semantics | A tool can declare task behavior such as required, optional, or forbidden. |
| Standard cancellation | `tasks/cancel` avoids a custom cancel tool per server. |
| Better reconnect behavior | A client can reconnect and continue polling a known task ID using protocol semantics. |
| Less prompt engineering | Tool descriptions do not need to teach every client the start/status/result/cancel loop. |
| Future interoperability | Gateways, dashboards, traces, and agent runtimes can understand task lifecycle generically. |

## What Native Tasks Do Not Solve

Native MCP Tasks improve the protocol and client UX. They do not eliminate the
backend work needed for long-running DSX Exchange subscriptions.

Both approaches still require:

- Valkey or equivalent durable task metadata and bounded result storage.
- Worker lease and heartbeat records.
- MQTT client lifecycle management.
- Cancellation checks and broker disconnect.
- Task TTL and result retention.
- Caller access checks for task status and result reads.
- A policy for token expiry while a watch is running.
- No raw JWT persistence in task state.
- Explicit failover semantics; pod failure may still create a subscription gap.

## SDK Implications

As of this note, the Go MCP SDK does not expose native Tasks APIs. The Python SDK
has experimental Tasks support, but using it would not remove the state,
failover, and JWT lifecycle work above.

For a conservative Go v1, explicit async tools are the lower-churn path. The
internal task model should still align with MCP Tasks terminology (`working`,
`completed`, `failed`, `cancelled`, expiry, result retrieval, cancellation) so
the server can migrate to native MCP Tasks when Go SDK support and client
support are ready.

## Recommendation

For v1:

1. Keep bounded synchronous tools for simple reads and short live samples.
2. Add explicit async tools for long-running MQTT watches.
3. Store task state/results in Valkey for cross-pod visibility and recovery
   metadata.
4. Do not store raw JWTs; use the current caller bearer only when starting or
   resuming an MQTT client.
5. Document best-effort failover: a later authenticated request can resume a
   watch, but events may be missed during pod outage.
6. Track native MCP Tasks as a future API-layer migration once the Go SDK and
   target MCP clients support it.

References:

- MCP Tasks specification: https://modelcontextprotocol.io/specification/2025-11-25/basic/utilities/tasks
- MCP Tasks overview: https://modelcontextprotocol.io/extensions/tasks/overview
- Go SDK Tasks issue: https://github.com/modelcontextprotocol/go-sdk/issues/626
- Python SDK Tasks issue: https://github.com/modelcontextprotocol/python-sdk/issues/1546
