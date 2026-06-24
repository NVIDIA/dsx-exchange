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

<!--
Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
SPDX-License-Identifier: Apache-2.0
-->

# DSX Exchange MCP

## Core Workflow

- Use `dsx_exchange_find_topics` when the user describes a signal, asset, or
  domain but does not provide an exact topic filter.
- Use `dsx_exchange_describe_topic` when the user provides a topic or topic
  filter and needs schema context, payload shape, parameters, examples, or
  related metadata/value topics.
- Use `dsx_exchange_read_retained` for retained metadata and last-known retained
  values. For BMS value topics, read related `/Metadata/` topics first when
  `describe_topic` returns them.
- Use `dsx_exchange_subscribe` only for bounded live sampling. Always provide a
  finite `max_messages` and `max_duration_s`.

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
- Do not use removed server-side watch lifecycle tools such as
  `start_subscription`, `read_subscription`, `subscription_status`, or
  `stop_subscription`.
- Do not ask the user for bearer tokens as tool arguments. The MCP client and
  server transport are responsible for passing authentication headers.
