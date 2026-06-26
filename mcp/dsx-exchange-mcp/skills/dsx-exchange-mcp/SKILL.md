---
name: dsx-exchange-mcp
description: >-
  Use DSX Exchange MCP tools for schema discovery, retained metadata reads, and
  bounded live MQTT sampling. Use when an agent needs to find, describe, get,
  fetch, read, sample, watch, listen, monitor, or subscribe to DSX Exchange
  topics or values. Prefer the MCP client's native background agent, subagent,
  task, or equivalent async mechanism for dsx_exchange_subscribe calls so the
  active chat stays responsive.
---

# DSX Exchange MCP

## Core Workflow

- Use `dsx_exchange_find_topics` when the user describes a signal, asset, or
  domain but does not provide an exact topic filter.
- Use `dsx_exchange_describe_topic` when the user provides a topic or topic
  filter and needs schema context, payload shape, parameters, examples, or
  related metadata/value topics.
- Treat `describe_topic` `schema_no_match` results as a bad schema-catalog
  argument, not proof that the live broker has no data. Retry with
  `dsx_exchange_find_topics` using inferred `domain`, `role`, `object_type`,
  `point_type`, or `query` terms before calling broker-backed tools.
- Use `dsx_exchange_read_retained` for retained metadata discovery. For BMS
  value requests, read related `/Metadata/` topics first when `describe_topic`
  returns them, then use that metadata to decide which `/Value/` topics to
  sample with `dsx_exchange_subscribe`.
- Use `dsx_exchange_subscribe` only for bounded live sampling. Always provide a
  finite `max_messages` and `max_duration_s`.

## BMS Discovery

- When the user asks for BMS values by signal name, asset, or point type,
  prefer `dsx_exchange_find_topics` before `describe_topic`; only describe the
  returned topic filters.
- For BMS value sampling, read retained related `/Metadata/` topics first when
  available. Treat retained metadata as the point/topic inventory, not the live
  value result. Use it to choose which concrete `/Value/` topics matter, then
  subscribe to those value filters.
- Do not call `dsx_exchange_read_retained` on guessed `/Value/` topics.

## Non-BMS Discovery

- For non-BMS schemas such as `power-management`, `nico`, and
  `spiffe-exchange`, do not infer BMS-style `/Metadata/` and `/Value/`
  companion topics unless `describe_topic` returns them.
- Use `dsx_exchange_find_topics` or `dsx_exchange_describe_topic` to get the
  event topic filter, then use `dsx_exchange_subscribe` for bounded live
  sampling when the user asks to listen, watch, fetch, or get events.

## Background Subscribe

- Run every `dsx_exchange_subscribe` call through the MCP client's native
  background, subagent, task, or equivalent async mechanism when one is
  available.
- Apply this by default whenever live sampling is needed, even if the user asks
  to "get", "fetch", "read", "sample", "watch", "listen", or "monitor" values
  without explicitly asking for background execution.
- Keep schema discovery, topic description, and retained reads inline; only live
  subscription calls need background execution.
- If no background mechanism is available, use a short bounded subscribe window
  and tell the user that the active chat may block until the tool call returns.

## Subscribe Defaults

Use these defaults unless the user asks for a different sampling window or the
topic rate requires a narrower cap:

```json
{
  "max_messages": 100,
  "max_duration_s": 30
}
```

- Narrow broad topic filters before subscribing when possible.
- Prefer one focused subscription over several broad subscriptions.
- Summarize returned messages by topic, latest value, source timestamp, quality,
  range, and stop reason.

## Avoid

- Do not use shell MQTT clients such as `mosquitto_sub` unless DSX Exchange MCP
  is unavailable and the user approves the fallback.
- Do not ask the user for bearer tokens as tool arguments. The MCP client and
  server transport are responsible for passing authentication headers.
