# Contributing to DSX Exchange

Thank you for your interest in contributing to DSX Exchange.

## Developer Certificate of Origin

All contributions must include a Developer Certificate of Origin sign-off.

```bash
git commit -s -m "type(scope): short description"
```

The sign-off certifies that you wrote the contribution or otherwise have the right to submit it under this repository's license. See [developercertificate.org](https://developercertificate.org/) for the full DCO text.

## Fork and Setup

Fork and clone the repository:

```bash
git clone https://github.com/<your-username>/dsx-exchange.git
cd dsx-exchange
git remote add upstream https://github.com/NVIDIA/dsx-exchange.git
```

Keep changes focused. Use a separate branch for each concern:

```bash
git checkout -b fix/nats-example
```

## Development

Follow existing conventions in the directory you are changing.

Useful checks:

```bash
make check-license-headers
make add-license-headers

cd auth-callout && go test ./...
cd local/mqtt-client && go test ./pkg/...
cd local/mqttbs && go test ./...
helm lint deploy/nats-event-bus
helm lint auth-callout/deploy
```

The full local functional and performance suites require a running Kind/NATS/Keycloak environment.

## License Headers

Source files must include the repository SPDX header:

```text
Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
SPDX-License-Identifier: Apache-2.0
```

Use the Makefile targets rather than adding headers by hand.

## Third-Party Licenses

Update top-level third-party license files when adding or removing dependencies:

- `THIRD_PARTY_LICENSES.csv` for Go module dependency reports
- `THIRD_PARTY_LICENSES.md` for Helm chart dependencies
