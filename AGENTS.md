# AGENTS.md

Guidance for AI coding agents working in this repository.

## Repository overview

DSX Exchange is a monorepo for the DSX event bus: AsyncAPI schemas, NATS auth-callout service (Go), Helm charts, Fern docs site, and a Kind-based local evaluation framework.

## Build and test

```bash
make test                      # full validation, including local Kind e2e
make check                     # license headers + helm lint
make -C auth-callout test      # auth-callout unit tests
helm lint auth-callout/deploy
```

Local Kind e2e deploys and functional tests must run outside the sandbox. The
local e2e path builds Docker images, updates Docker buildx state under
`~/.docker`, uses a local registry, and deploys the local stack with Skaffold.
In the sandbox this has failed with Docker buildx permission errors and
host-side networking timeouts. Use the local Make targets with unsandboxed
execution, for example `make -C local skaffold-run` and `make -C local test`.

For local deploy and infrastructure scripts, prefer direct validation over
meta-level tests. Do not add shell tests whose main purpose is to inspect deploy
script text or mock/assert exact command sequences such as Helm repo updates,
Kind image loads, timeouts, or Gateway YAML fields. Validate these changes with
syntax checks, Helm rendering/linting when applicable, and the real affected
local Make target outside the sandbox.

## Skaffold validation checklist

When changing the local Skaffold or Makefile flow, validate the affected paths
outside the sandbox and record what passed or failed:

- [ ] Run `make check` from the repo root.
- [ ] Run `make -C local skaffold-run` from a clean Kind state.
- [ ] Run `make -C local skaffold-run` again against the same state; confirm
      image builds are cached and unchanged services are not rolled.
- [ ] Run `make -C local skaffold-run-serial`; confirm the serial deploy path
      completes and leaves CSC, CPC-1, and CPC-2 healthy.
- [ ] Run `make -C local test`; confirm deploy, functional tests, and
      performance tests pass.
- [ ] Run `make -C local test-dev` against the deployed stack; confirm it only
      runs the functional and performance tests.
- [ ] Run `make -C local skaffold-dev`; confirm exactly one Skaffold dev
      process reaches watch mode and keeps the stack deployed after exit.
- [ ] While `skaffold-dev` is running, edit an event-bus chart/value file;
      confirm the NATS release updates in CSC, CPC-1, and CPC-2. If only one
      cluster updates, this check failed.
- [ ] While `skaffold-dev` is running, edit an infra manifest/value file;
      confirm the affected resource updates in the expected clusters. If the
      changed cluster is not reconciled, this check failed.
- [ ] While `skaffold-dev` is running, edit auth-callout source; confirm the
      image rebuilds once, is pushed to the local registry, and the event-bus
      pods use it.

Verify each observed rollout or resource update with `kubectl` using the
`kind-csc`, `kind-cpc-1`, and `kind-cpc-2` contexts. Leave the local stack
deployed when the user asks to inspect it.

## Commit conventions

Commits follow [Conventional Commits](https://www.conventionalcommits.org/). CI enforces this via commitlint.

```
type(scope): short description
```

Allowed types: `feat`, `fix`, `docs`, `style`, `refactor`, `perf`, `test`, `build`, `ci`, `chore`, `revert`.

All commits must include a DCO sign-off (`git commit -s`). Semantic-release on main generates tags and changelog from commit types.
Keep commit message body lines under 100 characters; commitlint enforces this.

## License headers

Every source file requires an SPDX header:

```
# Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
# SPDX-License-Identifier: Apache-2.0
```

CI checks this. Run `make add-license-headers` to fix.

## Third-party licenses

Regenerate `THIRD_PARTY_LICENSES.csv` when dependencies change in any Go
module (`auth-callout/`, `local/mqtt-client/`, `local/mqttbs/`). Use the repo
target instead of editing it by hand:

```bash
make third-party-licenses
```

Include the regenerated CSV in the same branch as the dependency change that
made it stale.

## Go conventions

- Go modules use vendored dependencies (`-mod=vendor`).
- `auth-callout/` has its own `go.mod`, `.golangci.yml`, and `vendor/`.
- `local/mqtt-client/` and `local/mqttbs/` are separate Go modules.

## Helm chart conventions

- The main chart is `deploy/nats-event-bus/` with `auth-callout/deploy/` as a subchart dependency.
- Values follow the `global.eventBus.*` namespace for bus config, `auth-callout.*` for the subchart.
- Chart validation: `helm lint` + template rendering in CI.

## Fern docs

- Config: `fern/docs.yml` with `global-theme: nvidia`.
- Docs content lives in `docs/` (Markdown and MDX).
- Schema pages are generated from AsyncAPI specs — see `scripts/generate_asyncapi_docs.py`.
- CI runs `fern check`, `tools/check-docs-mdx`, and offline link checking.
- Do not upgrade the Fern CLI version without explicit instruction.

## CI

- GitHub Actions on NV-managed runners (`linux-amd64-cpu4`).
- Triggered on push to `main` and `pull-request/[0-9]+` branches (copy-pr-bot pattern).
- `pull_request` trigger is not used — the copy-pr-bot vets external PRs before CI runs.

## Security

- Never interpolate secrets into shell command strings — use env vars only.
- Validate all `workflow_dispatch` inputs before use.
- `.github/` changes require additional review per CODEOWNERS.

## NKey generation

See `deploy/scripts/generate-nkeys.sh --help` and `deploy/README.md` for usage and output layout.
