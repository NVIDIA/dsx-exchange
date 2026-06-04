# V1 Background Watch Benchmark Plan

This note defines the benchmark and failure-test plan for the v1
`dsx-exchange-mcp` background watch design.

## Objective

Validate whether the v1 session-pinned, pod-local watch model is sufficient
before adding external watch state such as Valkey, Redis, JetStream consumers,
or separate MQTT worker pods.

The v1 design intentionally treats background watches as ephemeral,
session-scoped state:

- `start_subscription` returns a `subscription_id` quickly.
- Follow-up status/read/stop calls are routed to the same upstream pod by
  `Mcp-Session-Id` and Agentgateway stateful routing.
- Active watch state, MQTT connections, cursors, and ring buffers are held in
  the owning `dsx-exchange-mcp` pod.
- Pod restart, pod eviction, or MCP session loss terminates active watches and
  requires client resubscription.

The benchmark should determine whether this tradeoff is acceptable for v1
usage, and where the breakpoints are.

See `docs/watch-state-tradeoff-note.md` for the current tradeoff decision:
active MQTT watches stay pod-local, while any Valkey use should be limited to
best-effort status and aggregate snapshots rather than transparent MQTT
failover.

## Non-Goals

- Prove durable cross-pod recovery.
- Hide pod failure from clients.
- Turn Valkey or JetStream into an MCP-owned message database.
- Benchmark unbounded raw MQTT streaming through one infinite MCP tool call.

## Benchmark Questions

The benchmark should answer:

| Question | Decision It Informs |
| --- | --- |
| How many concurrent MCP sessions can one deployment support? | Replica sizing and per-pod limits. |
| How many active watches can each pod hold safely? | Watch admission limits. |
| How many MQTT connections and broker subscriptions are created? | Connection pooling strategy. |
| How do narrow and broad topic filters affect CPU, memory, and drops? | Topic guardrails and dedicated-client thresholds. |
| What is the p95/p99 latency for status and cursor reads? | User-facing UX limits. |
| How quickly do buffers fill under hot topics? | Buffer caps and overflow policy. |
| How expensive are broker reconnects and auth-callout evaluations? | Reconnect and pooling policy. |
| What happens during pod loss, rollout, and broker disruption? | Whether v1 failure semantics are acceptable. |

## Load Shapes

Run each load shape at multiple replica counts, starting with two replicas to
match the deployment default.

| Scenario | Example Levels | Purpose |
| --- | --- | --- |
| Concurrent MCP sessions | 100, 500, 1000 | Validate session pinning, memory, and gateway behavior. |
| Watches per session | 1, 5, 10 | Model normal and power-user agent workflows. |
| Narrow filters | Specific rack/object paths | Baseline operational usage. |
| Domain-wide filters | BMS or NICO domain-level watches | High-volume but plausible usage. |
| Broad wildcard filters | `#` or similar unsafe patterns, if allowed in test | Worst-case/bad-agent pressure. |
| Sparse topics | Low event rate | Idle connection overhead. |
| Hot topics | High event rate | Buffer pressure, drops, and summarization cost. |
| Control-plane churn | Repeated start/status/read/stop | Lifecycle overhead and cleanup leaks. |
| Denied topics | Broker ACL denial | Structured error and audit behavior. |
| Broker unavailable | Connect/subscribe failures | Backoff, error, and retry pressure. |

## Metrics To Capture

### MCP Server

- Active MCP sessions.
- Active background watches.
- Active tool calls.
- Tool calls by tool, status, and error code.
- `start_subscription`, `read_subscription`, `subscription_status`, and
  `stop_subscription` latency p50/p95/p99.
- Active MQTT connections.
- Broker subscriptions.
- Messages and bytes received.
- Buffered messages and bytes.
- Dropped messages and overflow count.
- Per-pod buffer memory.
- Goroutine count.
- Pod CPU and memory.
- Pod restarts.

### MQTT / Broker Path

- MQTT connect latency.
- MQTT subscribe latency.
- MQTT connection failures.
- Subscribe ACL denials.
- Broker reconnect count.
- Reconnect exhaustion count.
- Auth-callout request rate and latency, if available.
- Broker connection count by client identity or auth context, if available.

### Gateway Path

- Gateway request rate.
- Gateway 4xx/5xx by reason.
- Session routing failures.
- Requests missing or changing `Mcp-Session-Id`.
- Upstream dispatch latency.

### User-Visible Results

- Time from `start_subscription` to `running`.
- Time to first message.
- `read_subscription` p95/p99 latency.
- `subscription_status` p95/p99 latency.
- Message loss as represented by buffer overflow counters.
- Number of watches requiring client resubscription during failure tests.

## Suggested Pass / Review Gates

Set exact thresholds per environment before running the benchmark. Initial
review gates can use this shape:

| Gate | Initial Target |
| --- | --- |
| 1000 sessions with 1 watch each | No unbounded memory or goroutine growth. |
| `read_subscription` latency | p95 below 500 ms under normal load. |
| `subscription_status` latency | p95 below 250 ms under normal load. |
| Pod memory | Stays below 70 percent of configured limit. |
| Buffer overflow | Only occurs under configured overflow scenarios. |
| Stop cleanup | MQTT subscriptions, buffers, and goroutines are released. |
| Pod kill behavior | Watch loss is visible and client can resubscribe. |
| Broker ACL denial | Returns structured `topic_acl_denied`, not empty data. |
| Broker unavailable | Returns/updates `bus_unavailable` or `reconnecting` without hot looping. |

## Failure Scenarios

The v1 design does not promise transparent recovery. The tests should verify
clear status, cleanup, and resubscription behavior.

| Failure | Expected V1 Behavior |
| --- | --- |
| Owning MCP pod killed mid-watch | Active watches on that pod are lost; client must resubscribe. |
| Rolling deployment | Terminating pod stops accepting new watch starts; active watches are drained or reported lost where possible. |
| Gateway pod restart | Follow-up requests with the same `Mcp-Session-Id` should still route to the owning upstream pod. |
| MQTT broker disconnect | Watch enters reconnecting or failed status; no silent empty stream. |
| Auth token expiry | Watch ends at or before token expiry unless token refresh/token exchange is added. |
| ACL revoked during watch | Watch stops or fails on reconnect or next broker enforcement point. |
| Buffer overflow | Configured overflow policy applies and status/metrics expose the drop. |
| Client stops polling | Idle timeout eventually cleans up the watch. |
| Client disappears without stop | TTL or idle expiry releases MQTT subscriptions and buffers. |
| Node drain or eviction | Same as pod termination; client resubscribes. |

## When To Add External Watch State

Promote Valkey, Redis, JetStream consumers, or another external state backend
only if benchmark or product data shows one or more of these are required:

- Watches must survive `dsx-exchange-mcp` pod restart or node drain.
- Operators need to inspect all active watches globally across pods.
- `read_subscription` or `subscription_status` cannot reliably stay pinned to
  the owning pod.
- Watch lifetimes are long enough that rollout-driven interruption is common
  and unacceptable.
- Client resubscription creates too much UX friction or misses important
  incident evidence.
- Background watch count or message rate requires separate worker scaling.
- Support needs ownership leases, heartbeats, and cross-pod takeover.

If the goal is only better post-interruption UX, prefer a narrower
status-snapshot store before adding a full distributed ownership model. That
snapshot store can hold TTL-bound heartbeat, last-message, counter, and
aggregate data while active MQTT clients and raw buffers remain pod-local.

If external state is added, keep the responsibility split explicit:

| Data | Preferred Owner |
| --- | --- |
| Durable event replay | NATS JetStream or Flight Recorder. |
| Best-effort watch metadata/status/snapshot watermarks | Valkey, Redis, or approved KV store. |
| Bounded recent MCP buffers | Pod memory for v1; external capped buffers only when needed. |
| Live read cursors and raw ring buffers | Pod memory for v1. |
| Long-term incident evidence | Approved observability sinks, not MCP buffers. |

## Recommended Milestones

1. Implement v1 pod-local background watches with strong limits and metrics.
2. Run the benchmark at 100, 500, and 1000 concurrent sessions.
3. Run failure tests for pod kill, rollout, broker disruption, token expiry,
   ACL denial, buffer overflow, and idle cleanup.
4. Review observed interruption rate and operator/client impact.
5. Decide whether v2 needs external watch state, worker pods, broker-backed
   replay, or only tuning of v1 limits.

## Decision Record Template

After each benchmark run, record:

| Field | Notes |
| --- | --- |
| Date and environment | Cluster, broker, gateway, image versions. |
| Replica count | Gateway and `dsx-exchange-mcp`. |
| Load shape | Sessions, watches/session, topics, message rate. |
| Limits | Buffer caps, TTL, idle timeout, message caps. |
| Results | Latency, CPU, memory, drops, errors, restarts. |
| Failure behavior | What failed, what recovered, what required resubscription. |
| Decision | Keep v1, tune v1, or promote external watch state. |
| Follow-up | Required code, chart, observability, or product changes. |
