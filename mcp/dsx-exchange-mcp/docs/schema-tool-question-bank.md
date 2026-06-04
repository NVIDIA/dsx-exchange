# Schema Tool Question Bank

This question bank documents expected tool-call plans for natural-language
requests that should be answered from the embedded AsyncAPI schema catalogue.
The executable copy lives in
`internal/server/testdata/tool_call_expectations.json`.

These examples validate schema-driven planning only. Runtime tests execute
`dsx_exchange_describe_topic` and validate MQTT filter syntax, but they do not
connect to the live broker for `read_retained` or `subscribe`.

## BMS

### Grab me all of the most recent rack temperature data.

Expected flow:

1. `dsx_exchange_describe_topic({"topic_filter":"BMS/v1/PUB/Metadata/Rack/RackLiquidSupplyTemperature/#"})`
2. `dsx_exchange_describe_topic({"topic_filter":"BMS/v1/PUB/Metadata/Rack/RackLiquidReturnTemperature/#"})`
3. `dsx_exchange_read_retained({"topic_filter":"BMS/v1/PUB/Metadata/Rack/RackLiquidSupplyTemperature/#","max_messages":1000})`
4. `dsx_exchange_read_retained({"topic_filter":"BMS/v1/PUB/Metadata/Rack/RackLiquidReturnTemperature/#","max_messages":1000})`
5. `dsx_exchange_subscribe({"topic_filter":"BMS/v1/PUB/Value/Rack/RackLiquidSupplyTemperature/#","max_messages":100,"max_duration_s":30})`
6. `dsx_exchange_subscribe({"topic_filter":"BMS/v1/PUB/Value/Rack/RackLiquidReturnTemperature/#","max_messages":100,"max_duration_s":30})`

Rationale: metadata is retained and gives point identity/units/relationships;
value topics are live and separate supply/return point types.

### Show me rack liquid isolation status updates.

Expected flow:

1. `dsx_exchange_describe_topic({"topic_filter":"BMS/v1/PUB/Value/Rack/RackLiquidIsolationStatus/#"})`
2. `dsx_exchange_read_retained({"topic_filter":"BMS/v1/PUB/Metadata/Rack/RackLiquidIsolationStatus/#","max_messages":1000})`
3. `dsx_exchange_subscribe({"topic_filter":"BMS/v1/PUB/Value/Rack/RackLiquidIsolationStatus/#","max_messages":100,"max_duration_s":30})`

### What topic should I use for rack power telemetry?

Expected flow:

1. `dsx_exchange_describe_topic({"topic_filter":"BMS/v1/PUB/Value/Rack/RackPower/#"})`
2. `dsx_exchange_read_retained({"topic_filter":"BMS/v1/PUB/Metadata/Rack/RackPower/#","max_messages":1000})`
3. `dsx_exchange_subscribe({"topic_filter":"BMS/v1/PUB/Value/Rack/RackPower/#","max_messages":100,"max_duration_s":30})`

## Power Management

### Listen for power breach alerts from power agents.

Expected flow:

1. `dsx_exchange_describe_topic({"topic_filter":"grid/v1/poweragent/+/powerbreach"})`
2. `dsx_exchange_subscribe({"topic_filter":"grid/v1/poweragent/+/powerbreach","max_messages":100,"max_duration_s":30})`

### Find current power state status events.

Expected flow:

1. `dsx_exchange_describe_topic({"topic_filter":"grid/v1/poweragent/+/powerstate/status"})`
2. `dsx_exchange_subscribe({"topic_filter":"grid/v1/poweragent/+/powerstate/status","max_messages":100,"max_duration_s":30})`

### Which topic has infrastructure enforcement outcomes for power breaches?

Expected flow:

1. `dsx_exchange_describe_topic({"topic_filter":"grid/v1/infra/+/powerbreach/enforcement"})`
2. `dsx_exchange_subscribe({"topic_filter":"grid/v1/infra/+/powerbreach/enforcement","max_messages":100,"max_duration_s":30})`

## NICO

### Subscribe to NICO machine state changes.

Expected flow:

1. `dsx_exchange_describe_topic({"topic_filter":"NICO/v1/machine/+/state"})`
2. `dsx_exchange_subscribe({"topic_filter":"NICO/v1/machine/+/state","max_messages":100,"max_duration_s":30})`
