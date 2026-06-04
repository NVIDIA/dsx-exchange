# Background Watch State Tradeoff Note

This note captures the current design position for `dsx-exchange-mcp`
long-running MQTT watches after comparing pod-local state, Valkey-backed state,
and broker-backed recovery.

## Decision

Active background watches remain pod-local for the first implementation. The
MQTT client, subscription callbacks, raw ring buffer, and live cursor state are
owned by the `dsx-exchange-mcp` pod selected by Agentgateway stateful MCP
session routing.

Valkey, if introduced, should be limited to best-effort watch status and
aggregate snapshots. It should not be used in v1 for transparent MQTT client
failover, lease-based ownership transfer, raw message replay, or token refresh.

## Why Not Store The MQTT Client

An MQTT client is a live TCP connection plus in-process callback state. It
cannot survive pod restart by being stored in a key/value database. When the
owning pod dies, the MQTT connection and in-memory callbacks are gone.

The server can store only metadata about the watch:

- `subscription_id`
- topic filter or schema-derived selector
- owner pod identity
- creation, expiry, and last heartbeat timestamps
- last message timestamp
- message counters and drop counters
- latest/oldest snapshot watermarks
- latest status and error code
- bounded aggregate snapshots

This metadata can improve user-facing status after interruption, but it does
not recreate the MQTT subscription.

## Credential Constraint

The gateway validates each MCP tool call and forwards the caller bearer to the
upstream. `dsx-exchange-mcp` uses that bearer as the MQTT password when opening
the broker connection. Broker/auth-callout remains authoritative for topic ACLs.

The MCP server should not persist raw JWTs or manage caller token refresh. This
means a replacement pod cannot autonomously reconnect a caller-scoped MQTT
subscription after owner pod failure unless a later authenticated tool call
provides a fresh bearer, or a separate approved token-exchange mechanism is
added.

Because of that constraint, Valkey-backed lease takeover would create the
appearance of failover without solving the credential needed to resume the MQTT
stream.

## Valkey Usage That Fits

A narrow Valkey use can still be valuable if product UX needs post-interruption
status:

- TTL-bound watch status records.
- Owner heartbeat and last-message timestamps.
- Message, byte, and drop counters.
- Last known status such as `running`, `expired`, `interrupted`, `failed`, or
  `buffer_overflow`.
- Latest and oldest snapshot watermarks for explaining what the snapshot
  covered.
- Periodic aggregate snapshots per topic, such as count, min, max, mean, last
  value, and frequency.

This state is best-effort. If Valkey is unavailable, the active pod-local watch
can continue. Snapshot writes may fail, and post-mortem status may be missing
if the pod later dies.

## Valkey Usage To Avoid For V1

Avoid using Valkey for:

- MQTT client persistence.
- Raw JWT persistence.
- Transparent MQTT reconnect after pod failure.
- Lease-based active owner promotion.
- Live read cursors or replay positions used to continue a pod-local stream
  after pod failure.
- Raw ring buffers, exact message batches, or raw replay.
- A durable telemetry database.
- Replica reads for ownership, live state, or lease decisions.

These uses require stronger consistency, failover, token lifecycle, and recovery
semantics than the current v1 goal needs.

## Deployment Implication

For best-effort watch status snapshots, Valkey can be treated like a transient
cache:

- One writable primary endpoint is sufficient for v1.
- Replicas are optional and mainly useful for future HA promotion, not read
  scaling.
- Reads and writes should go to the primary to avoid stale status and snapshot
  confusion.
- Persistence is optional because records are TTL-bound and not source-of-truth
  telemetry.
- If Valkey fails, the system should fail open to local-only watch behavior.

This differs from gateway RLS Valkey. Gateway RLS stores short-lived shared rate
limit counters where loss only weakens throttling temporarily. Watch snapshots
are user-visible observability state, so they should be framed as best-effort
and not as a reliability boundary.

## Valkey Snapshot TTL

Snapshot records should expire soon after they stop helping the user understand
an interruption. The default TTL should be:

```text
snapshot_ttl = min(watch_expires_at + 30 minutes, created_at + 2 hours)
```

With a 15-minute maximum watch TTL, this keeps most snapshot records for up to
45 minutes after creation. That gives the user or agent time to ask what
happened after an interruption without keeping stale monitoring state around as
if it were durable telemetry.

Before watch expiry, the owning pod-local state is authoritative. After expiry
or interruption, a snapshot is useful only as last-known context. Longer-term
incident evidence belongs in audit logs, Flight Recorder, metrics, logs, or
another approved observability system rather than Valkey watch snapshots.

## User-Facing Failure Semantics

Pod-local state is authoritative. A Valkey snapshot can only explain the last
known state; it cannot prove that a watch is still active or resume raw reads.

| Tool | Local State Exists | Local State Missing, No Snapshot | Local State Missing, Snapshot Exists |
| --- | --- | --- | --- |
| `dsx_exchange_start_subscription` | Create a new authenticated pod-local watch. | Create a new authenticated pod-local watch. | Create a new authenticated pod-local watch; old snapshot remains historical context until TTL. |
| `dsx_exchange_read_subscription` | Read the owning pod's local buffer and return bounded messages plus next cursor. | Return `subscription_not_found` or `session_lost`. | Return `interrupted` with snapshot metadata or aggregates and no raw messages. |
| `dsx_exchange_subscription_status` | Return live pod-local status. | Return `subscription_not_found` or `session_lost`. | Return `interrupted` with last heartbeat, last message time, counters, and aggregate snapshot. |
| `dsx_exchange_stop_subscription` | Stop the local MQTT subscription, release buffers, and return `stopped`. | Return `subscription_not_found`. | Mark the snapshot `stopped` or return idempotent `stopped` with `stopped_reason=session_lost`; no MQTT cleanup is possible. |

If the owning pod dies and no Valkey snapshot is available, a later read/status
call should return `subscription_not_found` or `session_lost`.

If a stale Valkey snapshot is available, a later read/status call may return:

```json
{
  "subscription_id": "sub_123",
  "status": "interrupted",
  "interrupted_reason": "owner_heartbeat_stale",
  "last_heartbeat_at": "2026-06-03T19:10:02Z",
  "last_message_at": "2026-06-03T19:09:58Z",
  "message_count": 1842,
  "aggregates": [
    {
      "topic": "BMS/v1/PUB/Value/Rack/Power/Rack02",
      "count": 120,
      "min": 28.4,
      "max": 35.9,
      "mean": 31.2,
      "last_value": 32.1
    }
  ]
}
```

The agent can then explain that the watch was interrupted and offer to restart
it through a fresh authenticated tool call. Messages after the interruption may
be missed.

The `interrupted` status is informational only. It does not mean the watch is
still active, recoverable, or eligible for raw cursor reads. The recovery path is
a fresh authenticated `dsx_exchange_start_subscription` call.

## Current Recommendation

Implement pod-local watches first with short TTLs, clear interruption status,
and strong bounds. Add best-effort Valkey status and aggregate snapshots only if
the post-interruption UX needs more than `subscription_not_found` or
`session_lost`.
