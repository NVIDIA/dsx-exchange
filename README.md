# DSX Exchange

DSX Exchange is a monorepo for DSX event bus schemas, authentication, deployment, and local evaluation tooling.

Documentation for DSX Exchange is available at [https://docs.nvidia.com/dsx-exchange](https://docs.nvidia.com/dsx-exchange).

## Overview

DSX Exchange provides the repository pieces needed to describe, deploy, and validate DSX MQTT event bus integrations:

- `schemas`: AsyncAPI contracts for DSX Exchange MQTT topics and payloads.
- `auth-callout`: NATS auth callout service for OAuth2, mTLS, NKey, and no-auth profiles.
- `deploy`: Helm chart for the NATS event bus deployment.
- `local`: Kind-based local evaluation environment, Skaffold deployment, MQTT tests, and benchmark tooling.

The event bus itself is schema agnostic. Schemas document externally visible contracts; NATS and the auth callout enforce routing, federation, and authorization behavior.

## Requirements

- OS: Linux or macOS with Docker support.
- Tools: `mise`, `make`, and Docker. Mise installs the remaining tools from the
  locked root toolchain.
- Kubernetes: local Kind clusters for e2e testing.
- Runtime: Go modules use the Go version pinned in `mise.toml`.

GPU drivers are not required.

## Getting Started

Clone the repository, install the local e2e prerequisites, and run the local
validation checks:

```bash
git clone https://github.com/NVIDIA/dsx-exchange.git
cd dsx-exchange
make install-e2e-prereqs
make test
```

If you already have a DSX Exchange broker and need to build or test an MQTT
integration application, start with the
[Integrator Quickstart](https://docs.nvidia.com/dsx-exchange/integrator-quickstart).

Publish looping dummy BMS data into the local CSC MQTT broker:

```bash
make dummy-bms
```

## Usage

Use the top-level Makefile for common validation:

```bash
make help
make test
```

Run component-specific targets from the directory you are changing, and use
`make check` for repo-level license and chart validation:

```bash
make -C auth-callout test
make check
```

After the local Kind environment is deployed, run the dummy BMS demo with
`make dummy-bms`.

The local evaluation environment uses the top-level `auth-callout` and `deploy` directories directly.

## Performance

The full local e2e target includes a performance smoke profile sized for
repeatable Kind validation:

```bash
make test
```

Full benchmark runs are available separately:

```bash
make -C local benchmark
make -C local benchmark-full
```

## Releases & Roadmap

- Release notes: [CHANGELOG.md](CHANGELOG.md)
- Third-party license inventory: [THIRD_PARTY_LICENSES.csv](THIRD_PARTY_LICENSES.csv) and [THIRD_PARTY_LICENSES.md](THIRD_PARTY_LICENSES.md)

### Versioning

DSX Exchange follows [Semantic Versioning](https://semver.org/) (`vX.Y.Z`), automated via semantic-release. A new version is published automatically when a semantic-release compliant commit is merged to `main`.

| Commit prefix | Version bump | When to use |
|---------------|-------------|-------------|
| `fix:` | Patch (Z) | Bug fixes, CVE remediation |
| `feat:` | Minor (Y) | New features, backward-compatible changes |
| `feat!:` or `BREAKING CHANGE:` | Major (X) | Breaking API, schema, or chart changes |

### Roadmap

Upcoming work is tracked in [GitHub Issues](https://github.com/NVIDIA/dsx-exchange/issues). See [CONTRIBUTING.md](CONTRIBUTING.md) for how to get involved.

## Contribution Guidelines

- Start here: [CONTRIBUTING.md](CONTRIBUTING.md)
- Code of Conduct: [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md)

Development quickstart:

```bash
git clone https://github.com/NVIDIA/dsx-exchange.git
cd dsx-exchange
make test
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
