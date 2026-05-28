# Integrator Quickstart

Use this guide to start writing an integration application that connects to an
existing DSX Exchange MQTT broker. DSX Exchange uses standard MQTT 3.1.1, so you
do not need a DSX-specific client library. Build your integration with an
existing MQTT SDK for your runtime, then use the broker endpoint, authentication
material, topics, and schemas supplied by the DSX Exchange operator.

The examples below show both application level SDK usage and manual broker
interaction. The standalone MQTT CLI commands are included to help debug
connectivity, credentials, and topic permissions while you develop the
application. They are not the recommended shape for a production integration.

This page assumes a broker already exists. For broker installation and operator
setup, see [Deployment](getting-started.md).

## Prerequisites

- Broker host, port, and authentication details from the operator.
- Topic permissions for the messages your integration will publish or subscribe.
- An MQTT SDK for the language or platform your application uses.
- Optional debug tooling such as `mqttx`.
- Network access to the broker's MQTT listener.

For authentication and topic permission configuration, see
[Authentication](authentication.md). Most software integrations should use
OAuth2. BMS, OT, and device integrations commonly use mTLS with client
certificates.

## Connection Settings

Set the broker endpoint, authentication material, and topic configuration you
received from the operator in your application configuration.

If you are using the local evaluation environment and it is already deployed,
start the broker port forwards in one terminal and leave that terminal open
while you test. The script starts `kubectl port-forward` processes, then opens a
shell. The port forwards stop when you exit that shell. To create the local
broker first, use the [Deployment](getting-started.md) evaluation install.

```bash
cd local
./infra/scripts/with-gateway-port-forwards.sh sh
```

In the shell opened by that script, or in another terminal while that shell stays
open, use the local CSC broker endpoint:

```bash
export DSX_MQTT_HOST=127.0.0.1
export DSX_MQTT_PORT=11883
export DSX_MQTT_TOPIC=test/hello
```

For OAuth2 clients, set the MQTT username to `oauthtoken` and pass the access
token as the MQTT password.

For mTLS clients, configure the SDK's TLS options with the CA certificate, client
certificate, and client key supplied by the operator.

## Choose an MQTT SDK

Use the MQTT library that fits the application you are already building. These
are examples, not a required list:

| Runtime | SDK examples |
|---------|--------------|
| Go | [Eclipse Paho Go](https://eclipse.dev/paho/clients/golang/) |
| Python | [Eclipse Paho Python](https://eclipse.dev/paho/files/paho.mqtt.python/html/) |
| Node.js | [MQTT.js](https://github.com/mqttjs/MQTT.js) |
| Java | [Eclipse Paho Java](https://eclipse.dev/paho/clients/java/) |
| C | [Eclipse Paho C](https://eclipse.dev/paho/clients/c/) |
| C++ | [Eclipse Paho C++](https://eclipse.dev/paho/clients/cpp/) |

All SDKs follow the same basic flow:

1. Load broker host, port, topic, and auth material from configuration.
2. Create an MQTT 3.1.1 client with a stable client ID.
3. Connect with the authentication mode assigned by the operator.
4. Subscribe, publish, or both, using topics allowed by your permissions.
5. Handle reconnects, publish acknowledgements, and application shutdown.

## CLI Debug Smoke Test

Use a standalone MQTT CLI when you need to isolate broker access, credentials, or
topic permissions from your application code. Keep one terminal subscribed before
publishing from another terminal.

```bash
mqttx sub \
  -h "${DSX_MQTT_HOST}" \
  -p "${DSX_MQTT_PORT}" \
  -t "${DSX_MQTT_TOPIC}" \
  -V 3.1.1
```

```bash
mqttx pub \
  -h "${DSX_MQTT_HOST}" \
  -p "${DSX_MQTT_PORT}" \
  -t "${DSX_MQTT_TOPIC}" \
  -m '{"message":"hello from dsx exchange"}' \
  -V 3.1.1
```

The subscriber should print the payload:

```json
{"message":"hello from dsx exchange"}
```

## Next Steps

- Build your integration as an application using the MQTT SDK for your runtime.
- Use the schema pages to choose the correct topics and payloads for your domain.
- Use OAuth2 for software integrations or mTLS for BMS, OT, and device
  integrations before production use. Keep noauth limited to local evaluation
  and debug environments.
