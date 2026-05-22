# BMS Integration Companion Guide

This guide is for System Integrators and BMS contractors who will configure a Building Management System to publish facility data to the DSX Event Bus. It covers the concepts, topic structure, metadata fields, and implementation steps needed to complete a BMS integration.

The DSX Exchange AsyncAPI specification is the authoritative contract defining exact MQTT topic structures and payload schemas. This guide provides the context around that specification.

## 1. What is DSX Exchange?

DSX Exchange is the integration layer within DSX OS that connects the systems inside an AI factory — GPU clusters, building management systems, power distribution, cooling infrastructure, grid interfaces, and network switches. The BMS publishes structured telemetry and metadata to the DSX Event Bus, and IT-side software systems (cluster management, power optimization, leak detection, digital twins, AI agents) subscribe to that data.

The data contract is defined at the MQTT layer, not at the BMS layer:

- Your BMS can use any internal point naming convention.
- Your BMS can be any platform — any SCADA or DDC software appropriate for a critical environment.
- The only requirement is that when your BMS publishes to DSX Exchange, it follows the topic structure and payload format defined in the AsyncAPI spec.

The BMS does not deploy or manage the MQTT broker. You configure your BMS MQTT client to connect, publish, and subscribe.

## 2. MQTT Fundamentals for BMS System Integrators

If you are experienced with MQTT, skip to section 3. If your BMS background is primarily Modbus, BACnet, or proprietary protocols, this provides the minimum context you need.

MQTT is a lightweight publish/subscribe messaging protocol. There is no polling — devices publish data when it changes, and subscribers receive it immediately.

| Concept | Description |
|---------|-------------|
| **Broker** | The central server that receives all published messages and routes them to subscribers. DSX Exchange is the broker. Your BMS connects to it as a client. |
| **Publisher** | Any MQTT client that sends data. The BMS is the primary publisher. |
| **Subscriber** | Any MQTT client that receives data. IT systems are subscribers. |
| **Topic** | A hierarchical string that identifies what a message contains. Topics use `/` as separators. |
| **Payload** | The message content. In DSX Exchange, all payloads are JSON. |
| **QoS** | Quality of Service. QoS 0 = at most once (default for telemetry). QoS 1 = at least once (for critical signals). |
| **Retain** | A retained message is stored by the broker and delivered immediately to new subscribers. Used for metadata so subscribers don't wait for the next publish cycle. |

### 2.1 Value vs. Metadata — A Critical Concept

DSX Exchange uses a two-message pattern for every data point.

**Value messages** carry the live reading — a numeric value, a timestamp, and a quality flag. They are published on change and republished periodically (every 100 seconds) if unchanged. Value messages are lightweight and high-frequency.

**Metadata messages** describe what the point is — its engineering units, what equipment it belongs to, how it relates to other equipment, and other context that makes the value interpretable. Metadata is published at startup, republished periodically (every 100 seconds), and retained by the broker so new subscribers receive it immediately.

**Consumers must receive metadata before they can correctly interpret values.** Metadata is what tells a downstream system that a value of `32.5` is a CDU secondary supply temperature in degrees Celsius, not a valve position in percent. Without metadata, the value is just a number.

Value and metadata are published on separate topics with matching identifiers so consumers can correlate them.

### 2.2 The tagPath — Your Point Name

The `{tagPath}` is a vendor-defined hierarchical path that uniquely identifies a single point. It may contain multiple `/` segments and is usually derived from the BMS point name.

Each `{tagPath}` must be unique for a given point. Your internal BMS tag names are yours to keep — the tagPath is where they appear in the MQTT topic structure.

### 2.3 MQTT Wildcards

- `#` — multi-level wildcard. Subscribe to all topics under a hierarchy: `BMS/v1/PUB/Value/Rack/#` receives all rack values.
- `+` — single-level wildcard. Match exactly one topic level: `BMS/v1/PUB/Value/+/RackPower/#` receives power values for all object types.

## 3. Topic Structure and Publisher Rules

### 3.1 The Three Channel Types

Every point in DSX Exchange uses one of three channel types:

| Channel | Publisher | Topic Pattern | Purpose |
|---------|-----------|---------------|---------|
| BMS Value | BMS | `BMS/v1/PUB/Value/{objectType}/{pointType}/{tagPath}` | Live telemetry readings |
| BMS Metadata | BMS | `BMS/v1/PUB/Metadata/{objectType}/{pointType}/{tagPath}` | Point descriptions and relationships |
| Integration Value | Integration | `BMS/v1/{integration}/Value/{objectType}/{pointType}/{tagPath}` | Setpoint requests and commands from IT systems |

The topic path has five segments:

| Segment | Description |
|---------|-------------|
| `BMS/v1` | Fixed namespace and version prefix |
| `PUB` or `{integration}` | Publisher identifier — `PUB` for BMS, integration name for external systems |
| `Value` or `Metadata` | Message type |
| `{objectType}/{pointType}` | Equipment category and measurement type (restricted to spec-defined values) |
| `{tagPath}` | Vendor-defined hierarchical point identifier |

### 3.2 Publisher Rules — Who Publishes What

- **BMS publishes all metadata** — including for points whose values are written by integrations.
- **BMS publishes its own point values** on `BMS/v1/PUB/Value/...`.
- **Integrations** are any system (MQTT client) external to the BMS. When they need to send messages to the BMS, they publish values on `BMS/v1/{integration}/Value/...`. Integrations do **not** publish metadata.

### 3.3 Integration-Published Points — How They Work

Some points are owned by external integrations rather than the BMS. For these points, the BMS still publishes metadata, but the metadata payload contains an `integration` field identifying which integration must publish the value.

| Metadata field | Type | Meaning |
|----------------|------|---------|
| `integration` | string | Identifier of the integration that must publish the value for this point |

**Topic derivation rule:** The integration derives its value topic from the BMS metadata topic by substituting the publisher segment and topic type:

```text
BMS/v1/  PUB      /Metadata/{objectType}/{pointType}/{tagPath}
         ↓ replace PUB → {integration}, Metadata → Value
BMS/v1/{integration}/Value/{objectType}/{pointType}/{tagPath}
```

| Segment | Metadata topic | Value topic |
|---------|---------------|-------------|
| Publisher | `PUB` | value of `integration` field in metadata |
| TopicType | `Metadata` | `Value` |
| Remainder | `{objectType}/{pointType}/{tagPath}` | `{objectType}/{pointType}/{tagPath}` |

**Contract:** When an integration receives a BMS metadata message and the `integration` field matches its own identifier, the integration MUST:

1. Note the full metadata topic.
2. Derive the value topic by substituting `PUB` → own identifier and `Metadata` → `Value`, keeping `{objectType}/{pointType}/{tagPath}` unchanged.
3. Publish value messages to that derived topic.

Integrations MUST NOT:

- Publish values for points whose metadata `integration` field does not match their own identifier.
- Publish metadata (BMS is the sole metadata publisher).

**Example flow:**

```text
BMS publishes metadata →
  Topic:   BMS/v1/PUB/Metadata/CDU/LiquidTemperatureSpRequest/site1/row3/cdu5
  Payload: { ..., "integration": "MEPAI", ... }

Integration "MEPAI" receives the metadata, recognises its identifier,
derives its value topic →
  BMS/v1/PUB/Metadata/CDU/LiquidTemperatureSpRequest/site1/row3/cdu5
            ↓ PUB→MEPAI, Metadata→Value
  BMS/v1/MEPAI/Value/CDU/LiquidTemperatureSpRequest/site1/row3/cdu5

MEPAI publishes its setpoint reading to that derived topic.
```

This contract governs all integration-published points, including `Rack/RackLiquidIsolationRequest`, `Rack/RackElectricalIsolationRequest`, `System/HeartbeatEchoBms`, `System/HearbeatTimestampIntegration`, `System/HeartbeatEchoIntegration`, `CDU/LiquidTemperatureSpRequest`, and any future integration-owned pointTypes.

Note: For the `System/HeartbeatEchoBms` point, although the BMS publishes integration metadata, the BMS writes to the value and not the integration.

## 4. Object Types and Point Types

### 4.1 Object Types

Object types are restricted to specific strings defined in the AsyncAPI spec. They represent BMS equipment or device categories:

| Object Type | Category | Description |
|-------------|----------|-------------|
| **System** | System | The BMS or an integration. Heartbeat and system-to-system communication points live here. |
| **Rack** | Compute | IT rack with liquid cooling, power, and leak detection. The most standardized and commonly consumed object type. |
| **PowerMeter** | Electrical | Electrical power measurement. Contains metadata for understanding the electrical power path. |
| **ATS** | Electrical | Automatic Transfer Switch |
| **Breaker** | Electrical | Circuit Breaker |
| **Generator** | Electrical | Backup Generator |
| **Shunt** | Electrical | Electrical Shunt |
| **UPS** | Electrical | Uninterruptible Power Supply |
| **AHU** | Mechanical | Air Handling Unit |
| **CDU** | Mechanical | Coolant Distribution Unit |
| **Chiller** | Mechanical | Chiller |
| **Cooling Tower** | Mechanical | Cooling Tower |
| **CRAC** | Mechanical | Computer Room Air Conditioner |
| **CRAH** | Mechanical | Computer Room Air Handler |
| **Damper** | Mechanical | Air Damper |
| **Fan** | Mechanical | Fan |
| **HX** | Mechanical | Heat Exchanger |
| **Pump** | Mechanical | Pump |
| **Sensor** | Mechanical | Standalone Sensor |
| **Tank** | Mechanical | Storage Tank |
| **Valve** | Mechanical | Valve |
| **GenericObject** | Reserved | Only use when no other object type applies |

**Rack** is the most important object type for most IT-side consumers. Mechanical and electrical design at the rack is more standardized than gray-space systems, making rack data easier to ingest and understand. Many integrations will only consume rack data.

**PowerMeter** contains the metadata needed to understand the electrical power path. This data is critical for power management strategies.

**Electrical equipment** object types use metadata (`servesId`, `associateId`) to map where equipment sits in the power path, associating it with power meters.

**Mechanical equipment** object types use similar relationship metadata to map liquid and air flow paths.

### 4.2 Point Types

Point types are restricted to specific strings defined in the AsyncAPI spec. Each point type is also restricted to specific object types — some apply to multiple object types while others are specific to one.

The AsyncAPI schema reference is the authoritative source for the complete list of point types per object type. Refer to the BMS Event Bus schema for the full enumeration.

### 4.3 Choosing objectType and pointType

When mapping your BMS points to DSX Exchange:

1. Identify the equipment category → select the objectType.
2. Identify the measurement or status → select the pointType from that objectType's allowed list.
3. If the equipment doesn't fit any specific category, use `GenericObject` as a last resort.

## 5. Metadata Fields — Complete Reference

### 5.1 Universal Fields

These fields apply to all metadata messages regardless of object type:

| Field | Type | Description |
|-------|------|-------------|
| `engUnit` | string | Engineering unit for the measurement (e.g., °C, kPa, LPM, kW). Mutually exclusive with `stateText`. |
| `stateText` | array | State label mapping for binary/integer values (e.g., `[{value: 0, text: "Off"}, {value: 1, text: "On"}]`). Mutually exclusive with `engUnit`. |

**Rule:** Every metadata message carries either `engUnit` or `stateText`, never both. Engineering units describe continuous measurements. State text describes discrete states.

### 5.2 Rack Identifier Fields

These fields are specific to Rack metadata and are critical for IT-side correlation:

| Field | Type | Description |
|-------|------|-------------|
| `rackLocationName` | string | Human-readable name for the rack location |
| `rackLocationId` | string | Unique identifier for the rack location. Can be the same as `rackLocationName` if it is both human-readable and unique. **This must match the identifier used by IT-side systems** so both sides reference the same physical rack. |

### 5.3 PowerMeter Identifier Fields

PowerMeter metadata contains the fields needed to understand the electrical power path. The `servesId` relationship is particularly important — it maps which downstream equipment a power meter feeds.

### 5.4 Equipment Identifier Modes — Named Object vs. Associate

Equipment metadata uses one of two mutually exclusive identification modes:

**Named Object Mode** — use when the equipment is identified directly by name and ID:

| Field | Required | Description |
|-------|----------|-------------|
| `objectName` | Yes | Human-readable equipment name |
| `objectId` | Yes | Stable unique identifier for the equipment |
| `servesId` | Optional | List of objectIds of entities this equipment serves (one-way relationship — typically electrical power path or fluid flow path towards the rack) |

`associateId` must NOT be present in Named Object mode.

**Associate Mode** — use when the equipment is referenced via an association with another object:

| Field | Required | Description |
|-------|----------|-------------|
| `associateId` | Yes | objectId of the associated entity. The intent is for this object to be considered part of the other object, especially for `servesId` relationships. Commonly used to prevent parallel power paths when electrical equipment needs to be associated with power meters. |

`objectName`, `objectId`, and `servesId` must NOT be present in Associate mode.

### 5.5 processArea

Used to provide additional location or purpose context for a point. Combined with other metadata, `processArea` makes points unique and unambiguous.

**Example:** For a CDU object type with a LiquidTemperature point type, the process area could include "Secondary" and "Supply". This makes it clear the sensor is on the secondary side of the CDU on the supply line — not the primary side or the return line.

### 5.6 isSetpoint

Set to `true` to indicate a point is a setpoint (a target value the system is trying to maintain) as opposed to a sensor reading.

### 5.7 phase

Identifies which phase of a 3-phase electrical system the point is associated with.

### 5.8 scope

Used for System heartbeat points. If the BMS has multiple MQTT clients connected to DSX Exchange, each publishing different data, each client should have separate heartbeat points. Scope identifies which MQTT topics/namespace the heartbeat point is associated with — and thereby which topics are affected when a heartbeat is lost.

### 5.9 integration

Indicates which integration a point is associated with. Also indicates the namespace (`[Publisher]`) an integration is required to write the corresponding value back to. See section 3.3 for the full integration publishing contract.

## 6. System Heartbeat

DSX Exchange uses a system heartbeat pattern so IT systems can detect if the BMS has gone offline and vice versa.

| Point Type | Publisher | Frequency | Description |
|------------|-----------|-----------|-------------|
| `HeartbeatTimestampBms` | BMS | Every 10 seconds | BMS publishes a current timestamp |
| `HearbeatTimestampIntegration` | Integration | Every 10 seconds | Integration publishes a current timestamp |
| `HeartbeatEchoBms` | BMS | On receipt | BMS reads the integration timestamp and re-publishes it (echo) |
| `HeartbeatEchoIntegration` | Integration | On receipt | Integration reads the BMS timestamp and re-publishes it (echo) |

An integration may choose to use the echo points or not. The echo provides round-trip confirmation — if the echo stops, the sender knows the other side is not receiving.

Each BMS MQTT client that publishes to DSX Exchange should publish its own dedicated heartbeat point. Use the `scope` metadata field to distinguish which client each heartbeat belongs to.

## 7. Publish Frequency

- Publish value messages on change.
- Republish unchanged values every 100 seconds as a heartbeat floor. This allows subscribers to distinguish "value hasn't changed" from "publisher is offline."
- If your BMS polls equipment more frequently (e.g., every 2 seconds via Modbus), match your MQTT publish frequency to your poll frequency. The 100-second cadence is a floor, not a ceiling.
- Metadata is published once at startup and subsequently republished every 100 seconds. Metadata is retained by the broker.

## 8. Authentication

BMS devices connect to DSX Exchange using mTLS (mutual TLS) with X.509 client certificates. The BMS presents a client certificate at connection time, and the auth-callout service validates it and grants topic-level publish/subscribe permissions.

See [Authentication](authentication.md) for details on the auth model and how permissions are configured.

## 9. Implementation Checklist

### 9.1 Pre-Configuration

- [ ] Obtain broker connection details: host, port, TLS certificates
- [ ] Provision client certificate for your BMS MQTT client
- [ ] Confirm ACL permissions: which topic namespaces your client can publish and subscribe to
- [ ] Agree on rack identifiers with the IT team (`rackLocationId` must match both sides)
- [ ] Agree on integration identifiers if IT systems will write setpoint requests

### 9.2 Point Mapping

- [ ] Map each BMS point to an `{objectType}` and `{pointType}` from the AsyncAPI spec
- [ ] Define the `{tagPath}` for each point (your vendor-specific hierarchical path)
- [ ] Identify which points use `engUnit` vs. `stateText`
- [ ] Identify which points are integration-published (carry the `integration` metadata field)
- [ ] Choose Named Object mode or Associate mode for each equipment metadata entry

### 9.3 Metadata Publication

- [ ] Publish metadata for every point at BMS startup
- [ ] Verify metadata is retained by the broker (new subscribers receive it immediately)
- [ ] Include all required fields per objectType (see AsyncAPI spec for per-type requirements)
- [ ] Set up 100-second periodic metadata republish

### 9.4 Value Publication

- [ ] Publish values on change for all BMS-published points
- [ ] Set up 100-second minimum republish cadence for unchanged values
- [ ] Match publish frequency to poll frequency where applicable

### 9.5 Integration-Published Points

- [ ] Publish metadata with `integration` field for all integration-owned points
- [ ] Verify that each integration can derive its value topic from the metadata topic
- [ ] Test the full metadata → topic derivation → value publish flow

### 9.6 System Heartbeat

- [ ] Configure BMS to publish `HeartbeatTimestampBms` every 10 seconds
- [ ] If using echo points, configure BMS to read and re-publish integration timestamps
- [ ] Set `scope` metadata if running multiple BMS MQTT clients

### 9.7 Validation

- [ ] Verify all topics match the AsyncAPI spec structure
- [ ] Verify metadata payloads pass schema validation
- [ ] Confirm subscribers receive metadata before first value
- [ ] Test failover: disconnect and reconnect the BMS MQTT client, verify metadata is re-published and retained messages are delivered

## FAQ

**Do I need to rename my BMS points?**
No. Your internal BMS tag names are yours to keep. The `{tagPath}` segment in the MQTT topic is where your point names appear. You publish to the correct topic structure using your own naming.

**What if I have equipment that does not match any defined category?**
The AsyncAPI spec includes a `GenericObject` type as a catch-all. Describe the equipment in the metadata. The spec is versioned and evolves as new equipment types become common in AI factories.

**Do I need to publish every BMS point to DSX Exchange?**
No. DSX Exchange is not a replacement for your BMS. Many configuration and operator control points should remain internal to the BMS. The AsyncAPI spec defines what categories of data are supported.

**My BMS polls via Modbus every 2 seconds. How does that affect publish frequency?**
Your MQTT publish frequency should match your BMS poll or internal update frequency. 2-second intervals are optimal. The 100-second republish cadence is a minimum floor for unchanged values.

**What is the relationship between objectId and rackLocationId?**
`objectId` identifies a specific piece of equipment (a CDU, a breaker, etc.). `rackLocationId` identifies a physical rack location. A rack object uses `rackLocationId` for IT-side correlation. Other equipment objects use `objectId` and optionally `servesId` to map where they sit relative to racks and power meters.
