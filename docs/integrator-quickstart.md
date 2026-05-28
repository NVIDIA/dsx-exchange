# Integrator Quickstart

Use this guide when an event bus broker already exists and you only need to prove that your client can publish and receive its first MQTT message. It does not cover installing the broker; see [Deployment](getting-started.md) for operator setup.

## Prerequisites

- MQTT client tooling such as `mqttx`.
- Network access to the broker's MQTT listener.
- A noauth permissions entry that allows publishing and subscribing on `test.>`.
  This is the NATS subject permission for MQTT topic `test/hello`; the broker maps MQTT `/` topic separators to NATS `.` subjects.

The local evaluation CSC broker provides that noauth path after the event bus is deployed. For production brokers, confirm the configured authentication mode and topic permissions with the operator. See [Authentication](authentication.md) for OAuth2, mTLS, NKey, and noauth configuration.

## Connect to the Broker

Set the broker endpoint you received from the operator:

```bash
export DSX_MQTT_HOST=broker.example.com
export DSX_MQTT_PORT=1883
```

If you are using the local evaluation environment and it is already deployed, start the broker port-forwards in one terminal and run the quickstart commands in the shell it launches. To create the local broker first, use the [Deployment](getting-started.md) evaluation install.

```bash
cd local
./infra/scripts/with-gateway-port-forwards.sh sh
export DSX_MQTT_HOST=127.0.0.1
export DSX_MQTT_PORT=11883
```

## Subscribe

Open a terminal and subscribe before publishing so you can see the message arrive:

```bash
mqttx sub \
  -h "${DSX_MQTT_HOST}" \
  -p "${DSX_MQTT_PORT}" \
  -t "test/hello" \
  -V 3.1.1
```

Leave this command running.

## Publish

Open a second terminal with the same `DSX_MQTT_HOST` and `DSX_MQTT_PORT` values, then publish one message:

```bash
mqttx pub \
  -h "${DSX_MQTT_HOST}" \
  -p "${DSX_MQTT_PORT}" \
  -t "test/hello" \
  -m '{"message":"hello from dsx exchange"}' \
  -V 3.1.1
```

The subscriber terminal should print the payload:

```json
{"message":"hello from dsx exchange"}
```

## Next Steps

- Replace `test/hello` with a topic allowed by your integration's permissions.
- Use the schema pages to choose the correct topic and payload for your domain.
- Switch from noauth to the broker's required authentication mode before production use.
