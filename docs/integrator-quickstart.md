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
- Optional debug tooling such as `mqttx` or `mosquitto-clients`.
- Network access to the broker's MQTT listener.

The local evaluation CSC broker provides a noauth debug path that allows
publishing and subscribing on `test.>`. This is the NATS subject permission for
MQTT topic `test/hello`. The broker maps MQTT `/` topic separators to NATS `.`
subjects.

For production brokers, confirm the configured authentication mode and topic
permissions with the operator. Most software integrations should use OAuth2.
BMS, OT, and device integrations commonly use mTLS with client certificates.
See [Authentication](authentication.md) for OAuth2, mTLS, NKey, and noauth
configuration.

## Connection Settings

Set the broker endpoint and topic you received from the operator:

```bash
export DSX_MQTT_HOST=broker.example.com
export DSX_MQTT_PORT=1883
export DSX_MQTT_TOPIC=test/hello
```

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

For OAuth2 clients, pass the token through your SDK's username/password connect
options:

```bash
export DSX_MQTT_USERNAME=oauth2token
export DSX_MQTT_PASSWORD="<access-token>"
```

For mTLS clients, configure the SDK's TLS options with the CA certificate, client
certificate, and client key supplied by the operator.

## Choose an MQTT SDK

Use the MQTT library that best fits the application you are already building.
Common options include:

| Runtime | SDK examples |
|---------|--------------|
| Go | Eclipse Paho Go (`github.com/eclipse/paho.mqtt.golang`) |
| Python | Eclipse Paho Python (`paho-mqtt`) |
| Node.js | MQTT.js (`mqtt`) |
| Java | Eclipse Paho Java |
| C/C++ | Eclipse Paho C/C++ or libmosquitto |

All SDKs follow the same basic flow:

1. Load broker host, port, topic, and auth material from configuration.
2. Create an MQTT 3.1.1 client with a stable client ID.
3. Connect with the authentication mode assigned by the operator.
4. Subscribe, publish, or both, using topics allowed by your permissions.
5. Handle reconnects, publish acknowledgements, and application shutdown.

## Python SDK Sample

This sample uses the Python Paho MQTT SDK to publish one debug message. Replace
`DSX_MQTT_TOPIC` and the payload with the topic and schema payload for your
integration.

```bash
python3 -m pip install paho-mqtt
```

```python
# dsx_publish.py
import json
import os

import paho.mqtt.client as mqtt


host = os.environ["DSX_MQTT_HOST"]
port = int(os.getenv("DSX_MQTT_PORT", "1883"))
topic = os.getenv("DSX_MQTT_TOPIC", "test/hello")

client = mqtt.Client(
    mqtt.CallbackAPIVersion.VERSION2,
    client_id=os.getenv("DSX_MQTT_CLIENT_ID", "dsx-quickstart-publisher"),
    protocol=mqtt.MQTTv311,
)

username = os.getenv("DSX_MQTT_USERNAME")
password = os.getenv("DSX_MQTT_PASSWORD")
if username or password:
    client.username_pw_set(username, password)

ca_file = os.getenv("DSX_MQTT_CA")
cert_file = os.getenv("DSX_MQTT_CERT")
key_file = os.getenv("DSX_MQTT_KEY")
if ca_file or cert_file or key_file:
    client.tls_set(ca_certs=ca_file, certfile=cert_file, keyfile=key_file)

payload = json.dumps({"message": "hello from dsx exchange"})

client.connect(host, port, keepalive=30)
client.loop_start()
result = client.publish(topic, payload, qos=1)
result.wait_for_publish()
client.loop_stop()
client.disconnect()
```

Run it with the connection settings from the previous section:

```bash
python3 dsx_publish.py
```

To subscribe from an application, use the same connection setup and register a
message handler:

```python
# dsx_subscribe.py
import os

import paho.mqtt.client as mqtt


def on_message(client, userdata, message):
    print(f"{message.topic}: {message.payload.decode()}")


host = os.environ["DSX_MQTT_HOST"]
port = int(os.getenv("DSX_MQTT_PORT", "1883"))
topic = os.getenv("DSX_MQTT_TOPIC", "test/hello")

client = mqtt.Client(
    mqtt.CallbackAPIVersion.VERSION2,
    client_id=os.getenv("DSX_MQTT_CLIENT_ID", "dsx-quickstart-subscriber"),
    protocol=mqtt.MQTTv311,
)
client.on_message = on_message

username = os.getenv("DSX_MQTT_USERNAME")
password = os.getenv("DSX_MQTT_PASSWORD")
if username or password:
    client.username_pw_set(username, password)

ca_file = os.getenv("DSX_MQTT_CA")
cert_file = os.getenv("DSX_MQTT_CERT")
key_file = os.getenv("DSX_MQTT_KEY")
if ca_file or cert_file or key_file:
    client.tls_set(ca_certs=ca_file, certfile=cert_file, keyfile=key_file)

client.connect(host, port, keepalive=30)
client.subscribe(topic, qos=1)
client.loop_forever()
```

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
- Replace `test/hello` with a topic allowed by your integration's permissions.
- Use the schema pages to choose the correct topic and payload for your domain.
- Use OAuth2 for software integrations or mTLS for BMS, OT, and device
  integrations before production use. Keep noauth limited to local evaluation
  and debug environments.
- Keep standalone MQTT CLIs available for debugging broker connectivity and ACLs.
