<!--
Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
SPDX-License-Identifier: Apache-2.0
-->

# DSX Exchange MCP Current V1 Scope

This note records the current implementation scope for `dsx-exchange-mcp`.
When older planning docs conflict with this note, use this note as the current
source of truth.

## Document Precedence

Planning docs should be read newest-first. Later docs capture newer product and
engineering decisions, so they supersede older SDD language when scope or
priority differs.

For the current branch:

1. `current-v1-scope.md`
2. `mcp-tasks-vs-explicit-async-tools.md`
3. `long-running-subscriptions-ux.md`
4. `dsx-exchange-mcp-sdd.md`
5. Earlier tradeoff, benchmark, discussion, and eval notes

The SDD remains useful for architecture context, but it is broader than the
current implementation target.

## In Scope For Current V1

Current v1 is a focused MCP interface for schema-aware, read-only access to DSX
Exchange topics.

In scope:

- Expose embedded AsyncAPI specs as MCP resources.
- Provide schema/topic discovery with `dsx_exchange_describe_topic` and
  `dsx_exchange_find_topics`.
- Provide bounded MQTT reads with `dsx_exchange_read_retained` and
  `dsx_exchange_subscribe`.
- Pass the caller bearer through to MQTT as the broker credential.
- Let the broker and auth-callout remain authoritative for topic ACL decisions.
- Return structured tool errors for missing bearer, invalid topics, broker
  unavailability, auth failure, and ACL denial.
- Provide pod-local background watch tools:
  - `dsx_exchange_start_subscription`
  - `dsx_exchange_read_subscription`
  - `dsx_exchange_subscription_status`
  - `dsx_exchange_stop_subscription`
- Keep active watch state, MQTT connections, cursors, and raw ring buffers
  pod-local and session-pinned.
- Use short TTLs, bounded buffers, per-session limits, per-pod limits, metrics,
  and audit logs to keep this safe.
- Document that pod restart, pod eviction, rollout interruption, or MCP session
  loss can end a watch and require the client to start a new one.

## Explicitly Out Of Scope For Current V1

Do not treat these as current v1 gaps:

- Filtering MCP resource or schema-tool discovery by caller permissions.
- Hiding schema domains or schema helper tools before the caller attempts a
  broker-backed MQTT read.
- Adding a separate entitlement API solely for current v1 discovery filtering.
- Implementing `dsx_exchange_bms_metadata_snapshot`.
- Implementing `dsx_exchange_build_bms_graph`.
- Implementing `dsx_exchange_summarize_subscription`.
- Implementing `dsx_exchange_aggregate_subscription`.
- Implementing `dsx_exchange_export_subscription`.
- Implementing MCP notifications for watch events.
- Making watches durable across pod restart or cross-pod failover.
- Storing raw JWTs, refreshing caller tokens, or resuming MQTT clients without a
  fresh authenticated request.
- Adding Valkey, Redis, JetStream consumers, or worker pods for v1 watch state.

These may be revisited later, but they are not required to call the current
branch useful or complete for its intended scope.

## Possible Later Work

Aggregation is the most plausible next feature after this scope because it can
reduce high-volume streams into smaller operator-facing results. If added, it
should be introduced as a focused extension to the existing pod-local watch
model before adding distributed watch state.

Durable watch state, external workers, cross-pod recovery, entitlement-driven
discovery filtering, graph construction, and export sinks should wait for clear
product demand or benchmark evidence.

## Completion Bar

For this scope, the branch is complete enough when:

- Default MCP unit tests pass.
- Helm rendering/linting for the MCP chart passes.
- The MCP server can be deployed behind the gateway with stateful session
  routing.
- A caller can discover schema topics, read retained metadata, collect bounded
  live messages, and use start/read/status/stop background watches.
- Unauthorized MQTT topics fail through broker-backed structured errors instead
  of being treated as empty data.
- Docs and examples describe the smaller v1 scope instead of implying the full
  SDD backlog is required now.
