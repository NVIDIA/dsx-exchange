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

## Authentication

DSX Exchange supports three authentication modes. Choose based on your
environment:

| Mode | When to use | Credentials needed |
|------|-------------|-------------------|
| **noauth** | Local evaluation and debugging | None — connect without credentials |
| **OAuth2** | Software integrations, agents, MCP | MQTT username `oauthtoken`, access token as password |
| **mTLS** | BMS, OT, and device integrations | CA cert, client cert, client key |

The local evaluation environment deploys with noauth enabled by default, so the
CLI examples in this guide work without any credentials. For production, the
operator configures OAuth2, mTLS, or both. Ask your operator which mode and
credentials to use.

For the full auth model and permission configuration, see
[Authentication](authentication.md).

## Connection Settings

Set the broker endpoint, authentication material, and topic configuration you
received from the operator in your application configuration.

If you are using the local evaluation environment and it is already deployed,
connect to the CSC Envoy Gateway at its MetalLB address. To create the local
broker first, use the [Deployment](getting-started.md) evaluation install. On
macOS, install and start `docker-mac-net-connect` from the local quick start so
the host can reach the MetalLB IPs.

```bash
cd local
make setup-infra deploy-nats
```

Use the local CSC broker endpoint:

```bash
export DSX_MQTT_HOST=172.18.200.1
export DSX_MQTT_PORT=1883
export DSX_MQTT_TOPIC=test/hello
```

### Adding credentials for production

For OAuth2, set the MQTT username to `oauthtoken` and pass the access token as
the MQTT password. Obtain a token from the OIDC provider configured by your
operator (e.g., Keycloak):

```bash
mqttx pub \
  -h "${DSX_MQTT_HOST}" -p "${DSX_MQTT_PORT}" \
  -t "${DSX_MQTT_TOPIC}" -m '{"temp":22.5}' \
  -u oauthtoken -P "${ACCESS_TOKEN}" -V 3.1.1
```

For mTLS, configure the SDK's TLS options with the CA certificate, client
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
