# DSX Exchange

DSX Exchange is a monorepo for DSX event bus schemas, authentication, deployment, and local evaluation tooling.

## Overview

DSX Exchange provides the repository pieces needed to describe, deploy, and validate DSX MQTT event bus integrations:

- `schema`: AsyncAPI contracts for DSX Exchange MQTT topics and payloads.
- `auth-callout`: NATS auth callout service for OAuth2, mTLS, NKey, and no-auth profiles.
- `deploy`: Helm chart for the NATS event bus deployment.
- `local`: Kind-based local evaluation environment, NATS deployment scripts, MQTT tests, and benchmark tooling.

The event bus itself is schema agnostic. Schemas document externally visible contracts; NATS and the auth callout enforce routing, federation, and authorization behavior.

## Getting Started

Clone the repository and run the local validation checks:

```bash
git clone https://github.com/NVIDIA/dsx-exchange.git
cd dsx-exchange
make check
```

For local end-to-end validation, create the Kind environment and deploy NATS:

```bash
make -C local setup-infra
make -C local deploy-nats
make -C local validate-nats
make test-e2e
```

Publish looping dummy BMS data into the local CSC MQTT broker:

```bash
make dummy-bms
```

## Requirements

- OS: Linux or macOS with Docker support.
- Tools: `go`, `make`, `helm`, `kubectl`, `kind`, `docker`, `jq`, `yq`, `cfssl`, `nsc`, `addlicense`.
- Kubernetes: local Kind clusters for e2e testing.
- Runtime: Go modules declare their own supported Go versions.

GPU drivers are not required.

## Usage

Use the top-level Makefile for common validation:

```bash
make help
make check-license-headers
make test
make test-helm
make check
```

Run component-specific targets from the directory you are changing:

```bash
make -C auth-callout test
make -C deploy check
make -C local test-functional
make -C local test-performance
```

After the local Kind environment is deployed, run the dummy BMS demo with
`make dummy-bms`.

The local evaluation environment uses the top-level `auth-callout` and `deploy` directories directly.

## Performance

The local performance target is an e2e smoke profile sized for repeatable Kind validation:

```bash
make -C local test-performance
```

Full benchmark runs are available separately:

```bash
make -C local benchmark-performance
make -C local benchmark-basic-full
```

## Releases & Roadmap

- Release notes: [CHANGELOG.md](CHANGELOG.md)
- Third-party license inventory: [THIRD_PARTY_LICENSES.csv](THIRD_PARTY_LICENSES.csv) and [THIRD_PARTY_LICENSES.md](THIRD_PARTY_LICENSES.md)

## Contribution Guidelines

- Start here: [CONTRIBUTING.md](CONTRIBUTING.md)
- Code of Conduct: [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md)

Development quickstart:

```bash
git clone https://github.com/NVIDIA/dsx-exchange.git
cd dsx-exchange
make check
```

## Governance & Maintainers

- Governance: [GOVERNANCE.md](GOVERNANCE.md)
- Maintainers: [MAINTAINERS.md](MAINTAINERS.md)
- Triage policy: use GitHub issue labels and pull request review from repository maintainers.

## Security

- Vulnerability disclosure: [SECURITY.md](SECURITY.md)
- Do not file public issues for security reports.

## Support

- Support level: Maintained, with best-effort public issue triage.
- Help: file a GitHub issue with a focused reproduction or question.
- Response expectations: no guaranteed service-level agreement.

See [SUPPORT.md](SUPPORT.md) for details.

## Community

Use GitHub issues and pull requests for public project discussion, bug reports, feature requests, and contribution review.

## References

- [NATS](https://nats.io/)
- [NATS auth callout](https://docs.nats.io/running-a-nats-service/configuration/securing_nats/auth_callout)
- [AsyncAPI](https://www.asyncapi.com/)
- [CloudEvents MQTT Protocol Binding](https://github.com/cloudevents/spec/blob/main/cloudevents/bindings/mqtt-protocol-binding.md)

## License

This project is licensed under the Apache License 2.0. See [LICENSE](LICENSE) for details.
