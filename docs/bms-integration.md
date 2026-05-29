# BMS Integration Companion Guide

This guide is for System Integrators and BMS contractors who will configure a Building Management System to publish facility data to DSX Exchange. It covers the concepts, topic structure, metadata fields, and implementation steps needed to complete a BMS integration.

The DSX Exchange AsyncAPI specification is the authoritative contract defining exact MQTT topic structures and payload schemas. This guide provides the context around that specification.

## What is DSX Exchange?

DSX Exchange is NVIDIA's IT/OT integration layer within the NVIDIA DSX Software Suite. It acts as a secure, standardized data hub that connects Building Management System (BMS) data to NVIDIA's software stack — including cluster manager, break-fix, power capping and optimization tools, digital twins, and agentic AI applications.

In practical terms, DSX Exchange includes an event bus — which serves as an MQTT broker. The BMS publishes structured data to this broker. IT-side software systems subscribe to that data. The key principle is that DSX Exchange defines the data contract at the MQTT layer — not at the BMS layer. This means:

* Your BMS can use any internal point naming convention you choose.

* Your BMS can be any SCADA, PLC, DDC, or controls platform that is appropriate for a critical environment and meets your specific needs.

* The only requirement is that when your BMS publishes to the event bus, it follows the topic structure and payload format defined in the AsyncAPI spec.

<Note>
Key Point: DSX Exchange does not care what your BMS calls a point internally. It only cares what you publish to the MQTT broker. The mapping from your BMS tags to DSX Exchange topics is your responsibility. This mapping, and metadata management, might be done internally within your BMS application, or it could be done in middleware/external services.
</Note>

The DSX Exchange event bus is deployed as part of the DSX software stack and is open source (OSS). The BMS is not required to deploy and manage the MQTT broker. You simply configure your BMS MQTT client to publish and subscribe to it while following the DSX Exchange MQTT topic and payload requirements.

## MQTT Fundamentals for BMS System Integrators

If you are experienced with MQTT, you can skip this section. If your BMS background is primarily Modbus, BACnet, or proprietary protocols, this section provides the minimum context you need.

### How MQTT Works

MQTT is a lightweight publish/subscribe messaging protocol. There is no polling — devices publish data when it changes, and subscribers receive it immediately. The key components are:

* **Broker:** The central server (DSX Exchange event bus) that receives all published messages and routes them to subscribers. Your BMS connects to the broker as a client.

* **Publisher:** Any MQTT client that sends data. In this implementation, the BMS is the primary publisher.

* **Subscriber:** Any MQTT client that receives data. IT systems (cluster manager, break-fix systems for tray leaks, MEP agentic AI agents, digital twins, and even the BMS) are examples of subscribers.

* **Topic:** A hierarchical string that identifies what a message contains. Example: BMS/v1/PUB/Value/Rack/RackPower/site1/row3/rack05/power. Topics use forward slashes as separators.

* **Payload:** The message content. In DSX Exchange, all payloads are JSON.

### Value vs. Metadata — A Critical Concept

Every point in the BMS specification has two distinct message types: a Value message and a Metadata message. This is one of the most important concepts to understand before you begin implementation.

* **Value message:** Contains the live reading for a point. Published whenever the value changes, and republished every 100 seconds if it has not changed. The payload is always the same simple structure: a numeric value, a Unix timestamp in milliseconds, and a quality flag. Due to added system overhead, value messages should not be retained. QoS 0 should be used as the default unless critical, time-sensitive data points require a higher QoS.

```json
{ "value": 32.5, "timestamp": 1743620423000, "quality": 1 }
```

* **Metadata message:** Describes what the point is — its engineering units, what object it belongs to, how it relates to other objects, and other context that makes the value interpretable. Published once at startup, then republished every 100 seconds. Metadata should be set as retained by the broker so new subscribers receive it immediately. Metadata is not expected to change very often unless there is a BMS design change.

<Note>
Important: IT-side consumers MUST receive and process metadata before they can correctly interpret values. Metadata is what tells a Digital Twin that a value of 32.5 is a CDU secondary supply temperature in degrees Celsius, not a valve position in percent. Without metadata, the value is just a number.
</Note>

The topic structure reflects this separation:

`BMS/v1/PUB/Value/{objectType}/{pointType}/{tagPath}` ← live readings

`BMS/v1/PUB/Metadata/{objectType}/{pointType}/{tagPath}` ← descriptive context

### The tagPath — Your BMS Point Name

The `{tagPath}` segment at the end of every topic is vendor-defined. This is where your BMS point name (or a derivative of it) typically goes. It is the bridge between your internal naming and the DSX Exchange namespace. Think of the tagPath as a unique identifier: although the tagPath might be meaningful to the BMS, downstream consumers will not parse or use the tagPath to understand what a point is. They will only use it as a unique identifier and will instead use the metadata to understand what the point represents.

Rules for tagPath:

* Must be unique for each point across your entire publication.

* May contain multiple forward slash segments — it can be a hierarchical path.

* Is typically derived from your BMS tag path or point name.

* Does not need to match any specific naming convention.

* The tagPath in the Value topic MUST exactly match the tagPath in the corresponding Metadata topic.

Examples of valid tagPaths:

* `nvidia/titan/pod1/cdu01/secondaryLoop/supplyTemp`
* `DC01/DataHall_A/POD_03/CDU-02/SecLoop/SupTemp`
* `site1/row3/rack05/power`

<Note>
NVIDIA has no requirement on your tagPath format. Use whatever derives cleanly from your BMS tag structure. Consistency within your own deployment is what matters — every subscriber will use the unique tagPath to correlate values with metadata.
</Note>

## Topic Structure and Publisher Rules

### The Three Channel Types

The DSX Exchange event bus has three categories of MQTT channels, each with a distinct publisher and purpose:

| Channel Pattern | Publisher | Purpose |
| :---- | :---- | :---- |
| `BMS/v1/PUB/Value/{objectType}/{pointType}/{tagPath}` | BMS | Live sensor readings, status values, position feedback |
| `BMS/v1/PUB/Metadata/{objectType}/{pointType}/{tagPath}` | BMS | Point descriptions — units, IDs, relationships |
| `BMS/v1/{integration}/Value/{objectType}/{pointType}/{tagPath}` | IT Integration | Setpoint requests, control signals, or other information written back to BMS |

Note that the integration namespace (`BMS/v1/{integration}/...`) has only a Value channel — there is no corresponding Metadata channel. The BMS publishes metadata for every point, including integration-published points. By publishing metadata for integration-published values, the BMS also tells the integration which topic to publish the value on. See [Publisher Rules — Who Publishes What](#publisher-rules--who-publishes-what).

### Publisher Rules — Who Publishes What

These rules are intended to be strictly enforced through ACLs on the DSX Exchange event bus:

* The BMS publishes ALL metadata — including for points whose values are written by IT integrations.

* The BMS publishes its own point values on the BMS/v1/PUB/Value/... namespace.

* IT integrations publish values on the `BMS/v1/{integration}/Value/...` namespace.

* IT integrations NEVER publish metadata on the BMS topic namespace.

<Note>
ACL Implication: Your BMS MQTT client should be granted write access to `BMS/v1/PUB/#` (both Value and Metadata). IT integrations will be granted write access only to `BMS/v1/{their-identifier}/Value/#`. The broker should be configured to enforce these boundaries.
</Note>

### Integration-Published Points — How They Work

Some points in the event bus's BMS namespace are written by IT integrations, not the BMS. An example is the CDU liquid temperature setpoint request — an AI agent (MEPAI) computes an optimal CDU supply temperature and writes a setpoint request back to the BMS.

For these points, the BMS still publishes the metadata. The metadata payload includes an integration field that identifies which IT system owns the value. The IT integration reads this metadata, derives the correct value topic, and publishes to that topic.

How the integration derives its value topic:

```text
Metadata topic:    BMS/v1/PUB/Metadata/CDU/LiquidTemperatureSpRequest/site1/row3/cdu5/tempSpReq
                   ↓ replace PUB → {integration}, Metadata → Value
Integration topic: BMS/v1/MEPAI/Value/CDU/LiquidTemperatureSpRequest/site1/row3/cdu5/tempSpReq
```

As the BMS contractor, your responsibilities for integration-published points are:

* Publish the metadata for the point (including the integration field identifying the owner).

* Subscribe to the integration's value topic on that point.

* When a new value arrives, use the data according to your sequence of operations or project-specific requirements (subject to guardrail validation).

<Note>
Important — Guardrails: The BMS is always the authority on whether to apply a setpoint or action request received from an integration. It is highly recommended that the BMS validate any integration-published setpoint or command against pre-configured safety guardrails (minimum/maximum limits, rate of change limits) before applying it. The BMS should revert to a safe default if the integration stops publishing or sends an out-of-range value. A good way to think about it is that an integration can publish "requests" for the BMS to take an action but the action is ultimately determined by the BMS.
</Note>

## Object Types and Point Types

### Object Types

An objectType represents a category of equipment, device, grouping of devices, or system in the AI Factory. Every MQTT topic includes the objectType as a segment, which allows IT consumers to subscribe selectively.

| objectType | Description |
| :---- | :---- |
| Rack | An NVIDIA compute or support rack. The most important objectType — standardized point types that directly reflect rack liquid and power conditions. Many IT consumers only need Rack data. |
| PowerMeter | An electrical power monitoring device in the distribution path. Provides power path topology via servesId metadata. |
| CDU | Cooling Distribution Unit. Rack liquid cooling equipment.  |
| CoolingTower | Cooling tower in the facility cooling loop.  Generally evaporative cooling. |
| HX | Heat exchangers - inclusive of dry coolers. |
| CRAH | Computer Room Air Handler. |
| CRAC | Computer Room Air Conditioner. |
| AHU | Air Handling Unit. |
| Chiller | Chiller plant equipment. |
| Tank | Liquid storage, buffer, or other tank. |
| Valve | A standalone valve not modeled as part of a larger equipment object. |
| Pump | A standalone pump not modeled as part of a larger equipment object. |
| Fan | A standalone fan not modeled as part of a larger equipment object. |
| Damper | A standalone damper not modeled as part of a larger equipment object. |
| Sensor | A standalone sensor not associated with a specific equipment object. |
| BESS | Battery Energy Storage System.  Will commonly have "associateId" metadata to a PowerMeter objectId. |
| UPS | Uninterruptible Power Supply. Will commonly have "associateId" metadata to a PowerMeter objectId.  |
| ATS | Automatic Transfer Switch. Will commonly have "associateId" metadata to a PowerMeter objectId.  |
| Generator | Backup generator. Will commonly have "associateId" metadata to a PowerMeter objectId.  |
| Shunt | Electrical shunt device. Will commonly have "associateId" metadata to a PowerMeter objectId.  |
| Breaker | Electrical circuit breaker. Will commonly have "associateId" metadata to a PowerMeter objectId.  |
| System | Reserved for IT to OT system-level points such as the BMS or an integration — heartbeat, status, and availability of the BMS itself or connected integrations. |
| GenericObject | Use only when no other objectType applies. Should be used sparingly. |

### Point Types

A pointType identifies the specific measurement, status, or control signal within an objectType. Point types are strictly defined — you may only use the pointTypes listed in the AsyncAPI spec for each objectType.

Point types fall into several categories:

* **Rack-specific points:** RackLiquidSupplyTemperature, RackLiquidReturnTemperature, RackLiquidFlow, RackLiquidDifferentialPressure, RackLiquidDifferentialPressureSp, RackControlValvePosition, RackPower, RackLeakDetect, RackLeakSensorFault, RackLeakDetectTray, RackLiquidIsolationStatus, RackElectricalIsolationStatus, RackLiquidIsolationRequest, RackElectricalIsolationRequest. These point types can only be used with the Rack objectType.

  **Integration-published Rack values:** Three Rack point types have their *values* published by IT integrations rather than the BMS. The BMS publishes the metadata for these points (with the integration field identifying the owning IT system), and the IT integration publishes the values to its derived value topic. See [Integration-Published Points — How They Work](#integration-published-points--how-they-work) for the integration publishing pattern.

* **RackLiquidIsolationRequest and RackElectricalIsolationRequest** — actionable. The BMS is expected to perform liquid or electrical isolation in response to these values per the NVIDIA Controls and Monitoring Reference Design.

* **RackLeakDetectTray** — informational only. Provides tray-level leak visibility from the cluster manager that the BMS cannot detect independently. No BMS action is required in response.

  ***Note on RackLiquidDifferentialPressureSp:** This is the ONLY setpoint pointType — intentional by design. Other setpoints will use the isSetpoint metadata to distinguish the point as a setpoint. Rack DP can be controlled by a valve at the rack level. isSetpoint metadata is NOT required for this pointType — Rack setpoint pointType names are always self-describing.*

* **PowerMeter-specific points:** Voltage, PowerFactor, Frequency, ApparentPower, ActivePower, Current, CurrentLimit, PhaseCurrent. These can only be used with PowerMeter.

* **Mechanical equipment points:** LiquidTemperature, LiquidDifferentialPressure, LiquidFlow, LiquidPressure, AirTemperature, AirDifferentialPressure, AirFlow, AirPressure, AirRelativeHumidity, LeakDetect, ValvePosition, PumpSpeed, FanSpeed, DamperPosition. These apply to CDU, CoolingTower, HX, CRAH, CRAC, AHU, Chiller, Tank, Sensor, and GenericObject.

* **Status/availability points:** Status (complex equipment operating state), Available (equipment availability state). These apply to most objectTypes including System, electrical equipment, and mechanical equipment. Note that the Status pointType is not meant to indicate the position or state of simple devices (objects) such as Valve, Damper, Fan, Pump, and Sensor objectTypes. The binary position or state for these devices can leverage the allowable pointTypes with stateText metadata. Example: If you need to send the Open / Closed position of a Valve objectType, you can use the ValvePosition pointType with stateText metadata for open and closed.

* **Integration-published points:** LiquidTemperatureSpRequest (CDU setpoint request from Agentic AI), RackLiquidIsolationRequest, RackElectricalIsolationRequest (rack isolation requests from IT systems), RackLeakDetectTray (tray-level leak status from IT systems).

* **System/heartbeat points:** HeartbeatTimestampBms, HeartbeatEchoBms, HeartbeatTimestampIntegration, HeartbeatEchoIntegration. Used to monitor connectivity between BMS and IT integrations. Note that HeartbeatTimestampIntegration and HeartbeatEchoIntegration are integration-published (see [Integration-Published Points — How They Work](#integration-published-points--how-they-work)) — the BMS publishes their metadata but the integration publishes their values.

* **Sound:** Sensor and GenericObject only. Acoustic monitoring point.

* **GenericPoint:** A catch-all pointType for vendor-specific or other points that no applicable pointType exists for. Available on all objectTypes. Selectively use when no specific pointType fits. Requires a processArea metadata field to describe the measurement context.

### Choosing objectType and pointType

* Identify the physical equipment or device the point belongs to.

* Match it to the most appropriate objectType available. Prefer specific types over GenericObject.

* Identify the measurement or other data type and match it to the most specific pointType available. Prefer specific types over GenericPoint.

* If multiple points represent the same pointType on the same piece of equipment, or the pointType does not provide enough information to understand what the point is, use the processArea metadata field to differentiate and describe them.

* If no pointType fits your measurement, use GenericPoint with descriptive processArea data.

### Scope of Publication: Comprehensive Baseline vs. Integration-Specific Requirements

A common question from BMS contractors is: how do I know which specific points my deployment requires? The answer depends on two distinct categories of consumers.

**Comprehensive baseline (the 99%).** Most points published to the event bus feed *passive* consumers — digital twins, historians, dashboards, power analytics, and other "free data for all" use cases. These consumers don't tell the BMS in advance what they need; they subscribe to whatever is published and use it. For this category, the BMS contractor should follow the NVIDIA Controls and Monitoring Reference Design and publish nearly every applicable pointType supported by this guide for the equipment installed on the deployment. Together, the Reference Design and this guide get you 99% of the way to a properly instrumented AI Factory BMS publication.

**Integration-specific requirements (the remaining 1%).** *Active* IT integrations — those that write values back to the BMS or rely on specific handshake and sequencing — have unique requirements that cannot be inferred from the Reference Design alone. Examples include:

* Setpoints written back to the BMS by the integration (for example, CDU/LiquidTemperatureSpRequest from an AI optimization agent).

* System or Status points the BMS must publish back so the integration can confirm its setpoints are being honored — for example, whether AI control is currently enabled, whether the operator has disabled it, or whether the BMS has automatically kicked out AI control due to process variable limits being exceeded.

* Handshake or readiness points the integration uses to signal it is ready to be relied on (for example, an AI agent publishing a "ready" status before the BMS begins consuming its setpoints).

* Sequencing or coordination points specific to how a given integration vendor expects the BMS to interact with it.

These points vary by integration vendor and are not standardized across the broader DSX Exchange ecosystem. They are conveyed to the BMS contractor through **integration-specific specifications** — one specification per active integration on the deployment, listing exactly which points, setpoints, and metadata that integration requires. These specifications are normally provided as part of the BMS contractor's bid scope by the owner, GC, or commissioning engineer, who identify in advance which integrations will run on the deployment. If these are not provided, the BMS contractor should submit a Request for Information (RFI) through the appropriate contract tier.

<Note>
Practical Guidance: Treat the Reference Design and this guide as the 99% baseline. Then, for every active integration on the deployment, ask the owner / GC / CE for the corresponding integration specification and add its required points to your publication scope. If an integration is listed but no specification has been provided, request it before BMS design is finalized.
</Note>

## Metadata Fields — A Complete Reference

Metadata is the contextual layer that makes raw values usable by IT consumers. This section explains each metadata field, when it is required, and the reasoning behind the design. Metadata included in the MQTT message can be housed directly in your BMS application or stored indirectly in an external table or database. Example: You could use a table where each row is a BMS point path and each column is applicable metadata for that point. A service (a BMS-provided MQTT client) could publish the metadata and associated value of the BMS point to the appropriate DSX Exchange topic based on that table.

### Universal Fields (Every Metadata Message)

Every metadata message must include:

* **objectType** — The equipment category string (e.g., "CDU", "Rack", "PowerMeter").

* **pointType** — The measurement or status type string (e.g., "LiquidTemperature", "RackPower").

### Engineering Units and State Text

Every point (other than unitless points such as power factor) has either an engineering unit OR state text — never both:

* **engUnit** — Required for applicable analog points (temperature, flow, pressure, power, etc.). Must be a string representing the unit of measure. Examples: "C" (Celsius), "kPa", "LPM", "kW", "%".

* **stateText** — Required for binary or enumerated state points (e.g., status, alarms, isolation states). Contains a JSON array mapping integer values to human-readable labels. The BMS value for these points is always an integer; the stateText is a string that tells consumers what that integer means.

Example stateText for a leak detect point:

```json
[ { "value": 0, "text": "NoLeak" }, { "value": 1, "text": "Leak" } ]
```

<Note>
Design Note: stateText values are not standardized by NVIDIA across BMS points — the labels are yours to define. What matters is that they are present, consistent, and meaningful. IT consumers use them to display human-readable status without needing to know your vendor-specific codes. Note that Rack pointTypes do require specific engineering units or state text.
</Note>

### Rack Identifier Fields

For all Rack objectType points, use these fields instead of objectName/objectId:

* **rackLocationName** — Human-readable name for the rack location as defined in your BMS. Example: "Pod1-RowA-Rack05".

* **rackLocationId** — Stable unique identifier for the rack. This is critical: the rackLocationId must be coordinated between your BMS and the IT systems (Cluster Manager, Break-fix, Power optimization...) **before** deployment. Both sides (IT and OT) must use the same identifier to refer to the same physical rack. This ID should not change once established.

<Note>
Pre-Deployment Requirement: rackLocationId coordination is a required step before go-live. A mismatch here means IT systems cannot associate BMS rack data with the correct compute rack — leak isolation and power optimization will not function correctly.
</Note>

### PowerMeter Identifier Fields

For all PowerMeter objectType points, use these fields:

* **objectName** — Human-readable name for the power meter. Example: "RPP02-1-MBKR".

* **objectId** — Stable unique identifier for the power meter. Example: "DH3-RPP02-1-MBKR".

* **servesId** — Array of objectIds that this power meter serves (feeds power to). This is what allows IT systems to build the power distribution topology from meter to rack. Example: ["POD1-A3-01", "POD1-A3-02", "POD1-A3-03"].

Every point published for a PowerMeter object should include servesId in its metadata to establish its place in the electrical distribution topology. Each metadata message is consumed independently by IT systems — a consumer subscribed to a specific pointType will only see that point's metadata, so servesId must be present on every point for that PowerMeter object, not just one. Without it, other systems connecting to the event bus cannot build the power flow topology. [Associate Mode](#associate-mode) objects must NOT have servesId — topology runs through their parent object, not through them independently or in parallel. servesId on mechanical equipment for fluid flow topology is described in [Equipment Identifier Modes — Named Object vs Associate](#equipment-identifier-modes--named-object-vs-associate).

Because the PowerMeter objectType carries the electrical distribution topology, any equipment that contains a power meter should be published as a PowerMeter object. Additional pointTypes that aren't valid for the PowerMeter objectType are then published under the equipment's own objectType in Associate Mode, with associateId linking them back to the PowerMeter's objectId.

### Equipment Identifier Modes — Named Object vs Associate

For all mechanical, electrical, and System equipment objectTypes, you must choose one of two identifier modes for each point. Rack and PowerMeter use their own dedicated identifier patterns (see [Rack Identifier Fields](#rack-identifier-fields) and [PowerMeter Identifier Fields](#powermeter-identifier-fields)) and do not use the modes described here. This is one of the most nuanced aspects of the BMS metadata.

**Grouping principle.** objectName and objectId identify an *equipment object*, not a *point*. Points published for the same physical or logical piece of equipment should carry the same objectName and objectId across every metadata message. A CDU with 40 points publishes 40 metadata messages, all sharing one objectId. Differentiating points within the same object is the job of pointType and processArea — not a different objectId.

#### Named Object Mode

Use when the equipment is a standalone, independently tracked piece of equipment or system that has its own identity in your BMS.

* **objectName (required)** — Human-readable equipment name. Example: "CDU-01".

* **objectId (required)** — Stable unique identifier. Example: "ABCD1234".

* **servesId (optional)** — Array of objectIds that this equipment serves (feeds fluid or power to). Use this to build the mechanical fluid topology or electrical distribution toward the rack.

Named Object Mode example — an RPP Feeder Breaker Active Power reading:

"objectType": "PowerMeter"

"pointType": "ActivePower"

"objectName": "RPP02-1-FBKR"

"objectId": "DH3-RPP02-1-FBKR"

"servesId": ["POD1-A3-01", "POD1-A3-02", "POD1-A3-03"]

#### Associate Mode

Use when the point represents a sub-component of another equipment object — a measurement or data point that belongs to a parent piece of equipment but does not need its own identity. This is primarily used when a Rack or PowerMeter objectType does not support a pointType that should still be associated with that Rack or PowerMeter object.

* **associateId (required)** — The objectId or rackLocationId of the parent equipment object this point is associated with. Example: "ABCD1234".

Associate Mode example — a shunt trip on an RPP Feeder Breaker:

"associateId": "DH3-RPP02-1-FBKR"

<Note>
Why This Matters: The servesId field is only valid in Named Object Mode. Associate Mode deliberately excludes servesId to prevent duplicate paths in the topology graph. If a shunt is an associate of DH3-RPP02-1-FBKR (PowerMeter objectType), and DH3-RPP02-1-FBKR already has a servesId pointing downstream, the topology graph should not also show the shunt serving those same downstream objects. The Associate/Named Object distinction keeps the topology clean and non-redundant — which is critical for Digital Twins and power/fluid flow analysis.
</Note>

**Why Associate Mode Exists**

The Rack and PowerMeter objectTypes are intentionally designed with a limited, curated set of pointTypes. Rack is kept minimal — only the highest-value liquid, power, leak, and isolation points that IT systems need most. PowerMeter is purpose-built to establish the electrical power path topology from utility to rack via servesId. This intentional constraint makes these objectTypes highly reliable and easy for IT consumers to subscribe to with confidence.

However, real physical equipment is more complex than any single objectType can fully describe. A rack has isolation valves whose status a Digital Twin needs to know. A UPS has operational and status points beyond what PowerMeter supports. Associate Mode is the mechanism that handles this — it allows you to publish additional points using the correct objectType and pointType for the measurement, while maintaining a logical relationship back to the parent object via associateId.

**The Two Primary Use Cases**

The most common application of Associate Mode is with Rack and PowerMeter objectTypes. For Rack: any point that does not belong to the Rack pointType list but is physically associated with a rack — such as a rack isolation valve position or an air temperature sensor on the face of a rack — should be published under the appropriate objectType (e.g., Valve, Sensor) with associateId pointing to the rackLocationId. IT consumers like Digital Twins can subscribe to the Valve objectType and still understand which rack each valve impacts.

For PowerMeter: electrical equipment like a UPS is published as a PowerMeter objectType to establish its place in the power path topology and its servesId relationship toward the rack. Any additional UPS-specific points that are not valid PowerMeter pointTypes are then published as a UPS objectType with associateId pointing to the PowerMeter objectId. This keeps the power path topology clean while still making all relevant UPS data available to consumers who need it.

**Other Applications**

These are the two primary use cases, but Associate Mode is not limited to just these. It is the appropriate tool any time you have points that cannot be expressed within the parent object's allowed pointType list. However, Associate Mode should NOT be used when the pointType already exists on the objectType — a CDU already supports ValvePosition, PumpSpeed, FanSpeed, and DamperPosition, so those should always be published directly under the CDU objectType. When in doubt, use the most specific pointType available on the objectType before reaching for Associate Mode.

### processArea

An optional array of strings that provides additional location or functional context for a point. Used in conjunction with other metadata to make points unique and self-describing.

processArea is especially important when the same pointType appears multiple times on the same equipment object. As an example, all points on a CDU share the same objectId and objectName — processArea is what differentiates them, not a separate object. For a CDU with both primary (facility side) and secondary (rack-facing) loops, use Inlet/Outlet for the primary loop and Supply/Return for the secondary loop:

{/* <!-- rumdl-disable MD064 --> */}

pointType: LiquidTemperature, processArea: ["Primary", "Inlet"]     ← primary loop inlet to HX

pointType: LiquidTemperature, processArea: ["Primary", "Outlet"]    ← primary loop outlet from HX

pointType: LiquidTemperature, processArea: ["Secondary", "Supply"]  ← secondary loop supply

pointType: LiquidTemperature, processArea: ["Secondary", "Return"]  ← secondary loop return

{/* <!-- rumdl-enable --> */}

processArea is not constrained to specific entries. Try to use processArea with consistent tagging and descriptions throughout your deployment. Downstream consumers need to be able to interpret, and possibly parse around, the tags you put into processArea.

processArea is REQUIRED for GenericPoint pointTypes — it is the primary way to describe what a generic point measures or conveys.

### isSetpoint

An optional boolean (`true`/`false`) that indicates whether a point is a setpoint (a target value the system maintains) rather than a sensor reading. When absent, the point is assumed to be a sensor reading or other measured value. Use `true` for any point that represents a control setpoint in your BMS. Note: isSetpoint is NOT used for Rack pointTypes — Rack setpoint pointType names are always self-describing (e.g., RackLiquidDifferentialPressureSp).

### phase

Used only for the PhaseCurrent pointType on PowerMeter objects. Identifies which electrical phase the measurement belongs to. Three separate PhaseCurrent metadata messages are typically published per power meter — one per phase.

**Allowed values:** A, B, C *or* 1, 2, 3. Both conventions are supported because regional standards, owner standards, and equipment nameplate conventions vary across the industry. Either is acceptable.

**Consistency requirements:**

* The phase value should match the phase as shown on the project single-line diagrams. Whatever the single-line diagram calls Phase A (or Phase 1) is what the metadata should report.

* The phase convention MUST be consistent throughout a single deployment. Do not mix A/B/C on some meters and 1/2/3 on others within the same site.

* Phases MUST line up through the electrical distribution. The Phase A current at an upstream meter (e.g., MSB) must represent the same physical phase as the Phase A current at the downstream meter (e.g., RPP, PDU) it feeds. Phase identifiers are not arbitrary local labels — they are how IT consumers correlate and/or aggregate phase loading from utility to rack.

Different deployments may use different conventions. That is acceptable. What is not acceptable is inconsistency within a deployment, or phase labels that do not match the actual electrical topology.

### scope

Used for System/Heartbeat points when the BMS has multiple MQTT clients connected to the event bus. Each MQTT client should have its own heartbeat points, and scope identifies which topics that heartbeat covers. If your BMS uses a single MQTT client, scope is not needed. Not having a scope implies that the heartbeat points are for all BMS data being published to the event bus.

### integration

The integration field identifies which IT integration is responsible for publishing the value for this point. The integration field value becomes the publisher segment in the value topic (`BMS/v1/{integration}/Value/...`). Any point whose value is written to by an IT system rather than the BMS will have this field in its metadata. Examples:

* The points sent from the cluster manager or break-fix integration that tell the BMS to perform liquid isolation on a rack that the IT system has determined has rack tray leaks.

* System heartbeat points published by an integration (HeartbeatTimestampIntegration and HeartbeatEchoIntegration). The integration field on these points derives the value topic publisher namespace; the System object's objectId identifies which System the heartbeat is about. See [System Heartbeat](#system-heartbeat).

* MEP Agentic AI integration points that allow the integration to tell the BMS the AI Agent status is ready and available.

* RackLeakDetectTray — published by an IT integration (typically a cluster manager) that detects tray-level leaks at the server BMC. This point is informational only — it provides visibility into tray leaks that the BMS itself cannot detect, but no BMS action is required in response to this value. (The isolation requests above are the actionable integration-published points per the NVIDIA Controls and Monitoring Reference Design.)

## System Heartbeat

The System objectType is used for BMS-level and integration-level health monitoring. Heartbeat points allow each side to detect whether the other has gone offline. Echo points are optional — they enable round-trip confirmation that messages are flowing in both directions and round-trip timing measurement. Heartbeat value payloads use the standard `{value, timestamp, quality}` structure — the value field carries the Unix timestamp (in milliseconds) being published or echoed. For echo points, the value republished is the timestamp originally received from the other party.

Because a BMS may coordinate with **multiple integrations simultaneously** (for example MEPAI1, MEPAI2, a cluster manager, a digital twin agent, and others), heartbeat points use the System object's objectId to identify *which* System a given heartbeat is about. objectName and objectId are **required** on every heartbeat metadata message.

**Naming convention.** The recommended pattern is for an integration's System objectId to match the same identifier used as its integration metadata field elsewhere. For example, if MEPAI1's CDU/LiquidTemperatureSpRequest metadata carries `integration: "MEPAI1"`, then MEPAI1's heartbeat metadata should carry `objectId: "MEPAI1"`. This keeps each integration's identity consistent throughout the deployment.

**Reading the table.** `Echo<X>` means *"this is published by X,"* not *"this is an echo of X's timestamp."* The publisher of an Echo point is always the party named in the pointType — the timestamp being echoed is from the opposite party.

| pointType | Publisher | objectId identifies | integration metadata | Purpose |
| :---- | :---- | :---- | :---- | :---- |
| HeartbeatTimestampBms | BMS | the BMS | not used | BMS publishes its own timestamp every 10 seconds. Every integration consumes this single heartbeat to detect if the BMS has gone offline. |
| HeartbeatEchoIntegration | Integration | the BMS (being echoed) | required (= integration's namespace) | An integration reads the BMS timestamp and re-publishes it. Allows the BMS to confirm round-trip — i.e., that this specific integration is receiving the BMS heartbeat. |
| HeartbeatTimestampIntegration | Integration | the integration (publishing it) | required (= integration's namespace) | Each integration publishes its own timestamp every 10 seconds on its own value topic. The BMS monitors each integration's heartbeat independently to detect which integrations are online. |
| HeartbeatEchoBms | BMS | the integration  (being echoed) | not used | The BMS reads each integration's timestamp and re-publishes it back. Allows each integration to confirm round-trip — i.e., that the BMS is receiving its heartbeat. The BMS publishes one of these per connected integration. |

The echo points are optional — an integration and BMS may choose to implement them or not. The timestamp points are required for any active integration.

<Note>
Implementation Note: Implement at minimum the HeartbeatTimestampBms point in your BMS. This is an important health signal for IT systems monitoring your BMS connection. Without it, IT consumers have no way to distinguish "no data has changed" from "BMS offline". Each BMS MQTT Client that publishes to the event bus should publish its own dedicated HeartbeatTimestampBms point with a distinct scope value.
</Note>

## Implementation Checklist

Use this checklist to verify your BMS integration before go-live:

### Pre-Configuration

* Obtain broker connection details from the team standing up the DSX Exchange event bus (host, port, credentials, mTLS certificates).

* Coordinate rackLocationId values with the IT team. Both BMS and IT systems must use the same rack identifiers.

* Confirm ACL permissions: your BMS MQTT client should have write access to `BMS/v1/PUB/#` and read access to `BMS/v1/+/Value/#` (to receive integration-published setpoints).

* Confirm integration identifier strings (e.g., "MEPAI", or another agreed-upon name, for the AI temperature optimization agent). The exact identifier string should be defined in each integration's specification document, coordinated by the owner / GC / CE.

### Point Mapping

* For each BMS point you plan to publish, select the appropriate objectType and pointType.

* For each point, determine the identifier mode: Named Object or Associate.

* Define objectId values for all Named Object mode equipment. Each objectId identifies one physical piece of equipment — every point published for that equipment must carry the same objectId across every metadata message. objectIds must be unique per equipment object across your entire publication and stable over time. They can be the same as objectName as long as objectName is unique across your deployment. The BMS decides on these, unlike rackLocationId, which must be coordinated with the IT team.

* Define processArea values for any points that need additional context (especially multiple measurements of the same type on the same equipment object).

* For Rack points, use the coordinated rackLocationId values.

* For PowerMeter points, define servesId arrays that reflect your power distribution topology toward the racks. Define servesId for other equipment (e.g., liquid cooling flow toward the rack).

### Metadata Publication

* Publish required and applicable metadata for ALL points.

* Configure metadata to republish every 100 seconds.

* Set the MQTT retain flag on metadata messages so new subscribers receive them immediately.

* For integration-published points, include the integration field in metadata identifying the owning IT integration.

### Value Publication

* Publish values on change.

* Configure values to republish every 100 seconds when unchanged.

* Ensure the tagPath in value topics exactly matches the tagPath in the corresponding metadata topics.

* Ensure value payloads include value, timestamp (Unix epoch milliseconds), and quality fields.

* Use quality = 1 for good quality readings. Use quality = 0 when the BMS point has a bad or unhealthy reading.

### Integration-Published Points

* For each integration-published point (e.g., CDU LiquidTemperatureSpRequest), subscribe to the integration's value topic.

* Implement guardrails, enable logic, and validation within the BMS. Example: define minimum/maximum allowable setpoint values and reject out-of-range values.

* Implement a heartbeat timeout: if the integration stops publishing, revert to the BMS default or fail-safe control strategy.

* Implement additional control logic per integration and project-specific requirements.

### System Heartbeat

* Implement HeartbeatTimestampBms — publish a Unix timestamp every 10 seconds, with objectName/objectId identifying your BMS (e.g., objectId: "BMS").

* For each connected integration, if you want to participate in the optional round-trip echo pattern or a specific integration requires it, publish one HeartbeatEchoBms per integration, with objectName/objectId identifying the integration whose timestamp is being echoed. The BMS is the publisher of HeartbeatEchoBms (not HeartbeatEchoIntegration — that one is published by the integration).

* Coordinate objectId values for connected integrations with the IT team. The recommended convention is for an integration's System objectId to match the same identifier used as its integration metadata field elsewhere (e.g., `objectId: "MEPAI1"` for the System object whose integration field is "MEPAI1" on other points).

### Validation

* Use the DSX Exchange BMS AsyncAPI spec to validate your metadata and value payload schemas and topic structures.

* Verify that all required metadata fields are present for each pointType. The DSX Exchange BMS AsyncAPI specification is the authoritative source — each pointType schema in the spec defines which metadata fields are required and which are optional.

* Verify that Named Object mode points do not include associateId, and Associate mode points do not include objectName, objectId, or servesId.

* Confirm that rack data is correctly associated with the coordinated rackLocationId values.

* Verify that all Named Object Mode points published for the same physical equipment object carry identical objectName and objectId metadata. A CDU with 40 points should produce 40 metadata messages all sharing one objectId, differentiated by pointType (and processArea where applicable) — never by varying objectId. Associate Mode points for that same equipment object instead carry associateId matching the parent's objectId.

## Common Mistakes and FAQ

### Q: Do I need to rename my BMS points to follow NVIDIA naming?

No. Your internal BMS tag names are yours to keep. Many BMS owners have specific point naming standards that they specify. The tagPath in DSX Exchange topics is where your point name may appear. You simply need to publish to the correct topic structure using your own tagPath values.

### Q: What if I have equipment that does not match any objectType?

Use GenericObject. Convey what the equipment is in your processArea metadata. New equipment that becomes common in AI factories might be added later. The spec is versioned and evolves.

### Q: What if I have a measurement that does not match any pointType?

Use GenericPoint with a descriptive processArea. The processArea is required for GenericPoint and should clearly describe what is being measured. Example: processArea: ["Expansion Tank", "Fluid Conductivity"].

### Q: I don't have an analog fan speed point in my BMS — I only have a binary on/off state for the fan. Can I use the FanSpeed pointType to publish whether the fan is running?

Yes. FanSpeed supports optional stateText metadata. You can send binary or multi-state values with stateText on FanSpeed, PumpSpeed, ValvePosition, and DamperPosition point types.

### Q: What does the Voltage pointType represent when power meters publish several voltage values?

The Voltage pointType is the average RMS line-to-line voltage for the three-phase circuit being monitored — the meter's present (live) RMS reading, averaged across the three line-to-line voltages (V_AB, V_BC, V_CA). Most modern power meters compute this directly and expose it as a single value (sometimes labeled "VLL avg", "V L-L average", or similar).

Do not publish time-averaged, minimum, maximum, or demand voltage values under this pointType — only the live reading. Do not publish line-to-neutral or per-phase voltage under this pointType either. If for a specific reason you need to publish those flavors for integration-specific reasons, use GenericPoint with a descriptive processArea.

### Q: Can I publish the same point to multiple objectTypes?

No. Each point should have exactly one objectType and one pointType combination. If you have the same physical measurement that conceptually belongs to multiple objects, choose the most specific and appropriate objectType and use processArea or servesId to establish relationships.

### Q: How do I handle a CDU with both primary and secondary loop measurements?

Use the CDU objectType for all of them, with the same objectId and objectName for every point on that CDU. Differentiate using processArea. For example: processArea: ["Primary", "Inlet"] for the primary inlet temperature and processArea: ["Secondary", "Supply"] for the secondary supply temperature. processArea differentiates points — not objects.

### Q: When should I use a Valve objectType vs a CDU objectType for a ValvePosition pointType on a CDU?

In almost all cases, valve position feedback on a CDU should be published as a ValvePosition pointType under the CDU objectType. The CDU already supports ValvePosition — use it. The CDU's objectId and objectName cover it and no separate object identity is needed.

The Valve, Damper, Fan, Pump, and Sensor objectTypes exist for standalone devices that cannot be expressed as a pointType on an existing objectType.

### Q: What is the difference between Status and Available?

Status describes the operating state of the equipment — is it running, stopped, starting up, shutting down, etc.? Available describes whether the equipment is ready and available to operate — is it in maintenance mode, has it been manually taken offline, or has it failed and is unable to run? Both use stateText payloads. Many equipment types publish both.

### Q: My BMS polls equipment via Modbus every 2 seconds. How does that affect publish frequency?

Your MQTT publish frequency should match your BMS poll frequency or your BMS internal update frequency — whichever reflects the most current data. Publishing at 1-2 second intervals is optimal for DSX Exchange event bus consumers. The 100-second republish cadence is a floor, not a ceiling. If your data changes more frequently, publish more frequently. If your BMS is not able to publish changing values at least every 2 seconds, the DSX Exchange integrations should still work. However, the value of the data could be diminished to downstream consumers.

### Q: Do I need to implement all objectTypes and pointTypes listed in the spec?

No. You only publish what your BMS actually monitors. If you do not have a cooling tower, you do not publish CoolingTower data. The spec defines what is supported, not what is required. We highly recommend that you follow the NVIDIA Controls and Monitoring Reference Design for minimum points required when designing your BMS.

### Q: Do I need to publish every BMS point to the DSX Exchange event bus?

No. Many BMS points and objects will not be sent to the event bus. The BMS is still the tool Facility Operators use to manage and maintain MEP equipment. DSX Exchange is not meant to replace the BMS. Many configuration points and operator control points should not be accessible to DSX Exchange. Use the allowable pointTypes to help guide which BMS points should be sent to DSX Exchange.

## Appendix — Glossary

* **ACL (Access Control List):** Security rules on the MQTT broker that define which clients can publish or subscribe to which topic namespaces.

* **associateId:** Metadata field used in Associate Mode. Carries the objectId or rackLocationId of the parent equipment object that this point is associated with.

* **AsyncAPI:** An open standard for describing event-driven APIs. The DSX Exchange data catalog is published as an AsyncAPI YAML document, which can be used to validate BMS implementations and auto-generate client code.

* **BMS (Building Management System):** Generic term for any control and monitoring system managing AI Factory infrastructure — including DDC systems, PLC-based systems, and SCADA platforms.

* **CDU (Cooling Distribution Unit):** Equipment that transfers heat from server liquid cooling loops to facility cooling water or air.

* **DSX Exchange:** NVIDIA's IT/OT integration layer within the DSX software stack. It includes the event bus, which serves as an MQTT broker that connects BMS data to IT software systems.

* **engUnit:** Engineering unit metadata field. A string identifying the unit of measure for an analog point (e.g., "C", "kPa", "LPM", "kW", "%"). Mutually exclusive with stateText.

* **IT/OT Convergence:** The integration of Information Technology (compute, software, networking) with Operational Technology (BMS, PLCs, SCADA) in data center infrastructure.

* **integration (metadata field):** Metadata field identifying which IT integration owns the value for a given point. Its string value becomes the publisher segment in the value topic (`BMS/v1/{integration}/Value/...`). See [integration](#integration).

* **MEPAI:** MEP AI optimization agent. Uses CDU telemetry to compute optimal supply temperature setpoints and writes them back to the BMS via DSX Exchange.

* **MQTT:** Message Queuing Telemetry Transport. Lightweight publish/subscribe messaging protocol used by DSX Exchange.

* **objectId:** A stable unique identifier for an equipment object in the BMS AsyncAPI specification. Must be consistent between BMS and IT systems for the same physical device.

* **objectName:** Human-readable equipment name metadata field used in Named Object Mode. Paired with objectId.

* **objectType:** A string identifying the category of physical equipment a point belongs to (e.g., CDU, Rack, PowerMeter).

* **pointType:** A string identifying the measurement or status or other data type (e.g., LiquidTemperature, RackPower).

* **processArea:** Metadata field providing additional context about a point's location or function within an equipment object.

* **rackLocationId:** A stable unique identifier for a rack location that must be coordinated between BMS and IT systems before deployment.

* **servesId:** Metadata field listing the objectIds of downstream equipment that an object feeds. Used to build power and fluid flow topology graphs.

* **SI (System Integrator):** A company or contractor responsible for designing, configuring, and commissioning the BMS for an AI Factory. Synonymous with "BMS Contractor" in this document.

* **scope:** Metadata field used on System/Heartbeat points when a BMS has multiple MQTT clients publishing to the event bus. Identifies which topics that heartbeat covers. See [scope](#scope).

* **stateText:** Metadata field for binary or enumerated state points. A JSON array mapping each integer value to a human-readable label. Mutually exclusive with engUnit.

* **tagPath:** The vendor-defined hierarchical path at the end of every BMS MQTT topic in DSX Exchange. Often derived from the BMS point name or tag path.

## Quick Reference — objectType / pointType Matrix

O = Optional/Supported. Blank = Not applicable.

### Table A — Rack, Power Meter, System and Electrical objectTypes

| pointType | Rack | PowerMeter | BESS | UPS | ATS | Generator | Shunt | Breaker | System |
| ----- | ----- | ----- | ----- | ----- | ----- | ----- | ----- | ----- | ----- |
| RackLiquidSupplyTemperature | **O** |  |  |  |  |  |  |  |  |
| RackLiquidReturnTemperature | **O** |  |  |  |  |  |  |  |  |
| RackLiquidFlow | **O** |  |  |  |  |  |  |  |  |
| RackLiquidDifferentialPressure | **O** |  |  |  |  |  |  |  |  |
| RackLiquidDifferentialPressureSp  | **O** |  |  |  |  |  |  |  |  |
| RackControlValvePosition | **O** |  |  |  |  |  |  |  |  |
| RackPower | **O** |  |  |  |  |  |  |  |  |
| RackLeakDetect | **O** |  |  |  |  |  |  |  |  |
| RackLeakSensorFault | **O** |  |  |  |  |  |  |  |  |
| RackLeakDetectTray | **O** |  |  |  |  |  |  |  |  |
| RackLiquidIsolationStatus | **O** |  |  |  |  |  |  |  |  |
| RackElectricalIsolationStatus | **O** |  |  |  |  |  |  |  |  |
| RackLiquidIsolationRequest | **O** |  |  |  |  |  |  |  |  |
| RackElectricalIsolationRequest | **O** |  |  |  |  |  |  |  |  |
| Voltage |  | **O** |  |  |  |  |  |  |  |
| PowerFactor |  | **O** |  |  |  |  |  |  |  |
| Frequency |  | **O** |  |  |  |  |  |  |  |
| ApparentPower |  | **O** |  |  |  |  |  |  |  |
| ActivePower |  | **O** |  |  |  |  |  |  |  |
| Current |  | **O** |  |  |  |  |  |  |  |
| CurrentLimit |  | **O** |  |  |  |  |  |  |  |
| PhaseCurrent |  | **O** |  |  |  |  |  |  |  |
| Status |  |  | **O** | **O** | **O** | **O** | **O** | **O** | **O** |
| Available |  |  | **O** | **O** | **O** | **O** | **O** | **O** | **O** |
| GenericPoint |  | **O** | **O** | **O** | **O** | **O** | **O** | **O** | **O** |
| HeartbeatTimestampBms |  |  |  |  |  |  |  |  | **O** |
| HeartbeatEchoBms |  |  |  |  |  |  |  |  | **O** |
| HeartbeatTimestampIntegration |  |  |  |  |  |  |  |  | **O** |
| HeartbeatEchoIntegration |  |  |  |  |  |  |  |  | **O** |

### Table B — Mechanical, Sensor, and Generic objectTypes

CT = CoolingTower, Chlr = Chiller, Dmpr = Damper, GO = GenericObject

| pointType | CDU | CT | HX | CRAH | CRAC | AHU | Chlr | Tank | Valve | Pump | Fan | Dmpr | Sensor | GO |
| ----- | ----- | ----- | ----- | ----- | ----- | ----- | ----- | ----- | ----- | ----- | ----- | ----- | ----- | ----- |
| LiquidTemperature | **O** | **O** | **O** | **O** | **O** | **O** | **O** | **O** |  |  |  |  | **O** | **O** |
| LiquidDifferentialPressure | **O** | **O** | **O** | **O** | **O** | **O** | **O** | **O** |  |  |  |  | **O** | **O** |
| LiquidFlow | **O** | **O** | **O** | **O** | **O** | **O** | **O** | **O** |  |  |  |  | **O** | **O** |
| LiquidPressure | **O** | **O** | **O** | **O** | **O** | **O** | **O** | **O** |  |  |  |  | **O** | **O** |
| LiquidTemperatureSpRequest | **O** |  |  |  |  |  |  |  |  |  |  |  |  | **O** |
| AirTemperature | **O** | **O** | **O** | **O** | **O** | **O** | **O** | **O** |  |  |  |  | **O** | **O** |
| AirDifferentialPressure | **O** | **O** | **O** | **O** | **O** | **O** | **O** | **O** |  |  |  |  | **O** | **O** |
| AirFlow | **O** | **O** | **O** | **O** | **O** | **O** | **O** | **O** |  |  |  |  | **O** | **O** |
| AirPressure | **O** | **O** | **O** | **O** | **O** | **O** | **O** | **O** |  |  |  |  | **O** | **O** |
| AirRelativeHumidity | **O** | **O** | **O** | **O** | **O** | **O** | **O** | **O** |  |  |  |  | **O** | **O** |
| LeakDetect | **O** | **O** | **O** | **O** | **O** | **O** | **O** | **O** |  |  |  |  | **O** | **O** |
| ValvePosition | **O** | **O** | **O** | **O** | **O** | **O** | **O** | **O** | **O** |  |  |  |  | **O** |
| PumpSpeed | **O** | **O** | **O** | **O** | **O** | **O** | **O** | **O** |  | **O** |  |  |  | **O** |
| FanSpeed | **O** | **O** | **O** | **O** | **O** | **O** | **O** | **O** |  |  | **O** |  |  | **O** |
| DamperPosition | **O** | **O** | **O** | **O** | **O** | **O** | **O** | **O** |  |  |  | **O** |  | **O** |
| Status | **O** | **O** | **O** | **O** | **O** | **O** | **O** | **O** |  |  |  |  |  | **O** |
| Available | **O** | **O** | **O** | **O** | **O** | **O** | **O** | **O** | **O** | **O** | **O** | **O** | **O** | **O** |
| Sound |  |  |  |  |  |  |  |  |  |  |  |  | **O** | **O** |
| GenericPoint | **O** | **O** | **O** | **O** | **O** | **O** | **O** | **O** | **O** | **O** | **O** | **O** | **O** | **O** |
