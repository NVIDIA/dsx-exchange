# DSX Exchange Schema

The DSX Event Bus itself is schema agnostic. Brokers relay subjects, and enforce prefix rules, and enforce ACLs.
Clients participating in the DSX Exchange program must publish a formal [AsyncAPI](https://asyncapi.com/) definition here covering every exposed subject and payload so downstream consumers can rely on consistent contracts and documentation.
AsyncAPI is our chosen schema format. AsyncAPI is a Linux Foundation project analogous to OpenAPI for async systems. The specification natively models MQTT servers and channels (topics) plus publish/subscribe operations, messages, and security traits, which is sufficient to describe our MQTT endpoints.

The schema's purpose is to expose clear, human-readable documentation for consumers. It does not drive routing, validation, or otherwise alter broker behaviour. Teams may auto-generate SDKs and diffs from those documents, but any such tooling sits outside of this repository. [Modelina](https://www.asyncapi.com/tools/modelina) may be used for model generation. Full client generation is somewhat lacking.

The schema documentation is published to [explore.api.nvidia.com/dsx-exchange](https://explore.api.nvidia.com/dsx-exchange) on every merge to main.

## Cloud Events

Clients that elect to emit CloudEvents should follow the official MQTT Protocol Binding so metadata (type, source, id, datacontenttype) is encoded consistently. Since DSX standardizes on MQTT 3.1.1, publishers use the structured mode defined by the binding.
Adopting CloudEvents remains optional. The binding is simply the formally supported way to represent CloudEvents over MQTT. An [example is given here](cloud-events-example.yaml).

## Repository Structure

Each logical component is given a directory with a single yaml spec file in [/schema](/schema/). Each component's team is responsible for updating and reviewing their own schema through the standard GitHub pull request workflow.

## Running Checks Locally

Run the repository checks before opening a pull request:

```bash
make check-license-headers
```

See `make help` for additional targets.
