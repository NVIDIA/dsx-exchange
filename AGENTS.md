# AGENTS.md

Guidance for AI coding agents working in this repository.

## Repository overview

DSX Exchange is a monorepo for the event bus and DSX Agent Gateway: AsyncAPI
schemas, Go services, Helm charts, Fern docs, and one Kind-based local
evaluation framework.

## Build and test

```bash
make test                      # full validation, including local Kind e2e
make check                     # license headers + helm lint
make -C auth-callout test      # auth-callout unit tests
go -C dsx-agentgateway-bridge test ./...
helm lint auth-callout/deploy
```

Local Kind e2e deploys and functional tests must run outside the sandbox. The
local e2e path builds Docker images, updates Docker buildx state under
`~/.docker`, uses a local registry, and deploys the local stack with Skaffold.
In the sandbox this has failed with Docker buildx permission errors and
host-side networking timeouts. Use the root Make targets with unsandboxed
execution, for example `make local-up` and `make test`.

For local deployment and infrastructure changes, prefer direct validation over
meta-level tests. Do not add tests in any language whose primary purpose is to
inspect source text, rendered manifests, deployed resource fields, or exact
command sequences. This includes Helm repo updates, Kind image loads, timeout
values, and Gateway or monitor YAML fields. Validate changes with syntax checks,
Helm rendering or linting when applicable, and the real affected local Make
target and behavior outside the sandbox.

## Skaffold validation checklist

When changing the local Skaffold or Makefile flow, validate the affected paths
outside the sandbox and record what passed or failed:

- [ ] Run `make check` from the repo root.
- [ ] Run `make local-up` from a clean Kind state.
- [ ] Run `make local-up` again against the same state; confirm
      image builds are cached and unchanged services are not rolled.
- [ ] Run `make test`; confirm deploy, functional tests, and
      performance tests pass.
- [ ] Run `make test-dev` against the deployed stack; confirm it only
      runs the functional and performance tests.
- [ ] Run `make skaffold-dev`; confirm exactly one Skaffold dev
      process reaches watch mode and keeps the stack deployed after exit.
- [ ] While `skaffold-dev` is running, edit an event-bus chart/value file;
      confirm the NATS release updates in CSC, CPC-1, and CPC-2. If only one
      logical site updates, this check failed.
- [ ] While `skaffold-dev` is running, edit an infra manifest/value file;
      confirm the affected resource updates in the expected logical sites.
      If the changed site is not reconciled, this check failed.
- [ ] While `skaffold-dev` is running, edit auth-callout source; confirm the
      image rebuilds once, is loaded into the active Kind cluster, and the
      event-bus pods use it.
- [ ] While `skaffold-dev` is running, edit DSX Agent Gateway bridge source;
      confirm the image rebuilds once, is loaded into the active Kind cluster,
      and all three logical-site releases use it.

Verify each observed rollout or resource update in the default topology with
`kubectl --context kind-dsx-exchange` and the `csc-*`, `cpc-1-*`, and `cpc-2-*`
namespaces. Leave the local stack deployed when the user asks to inspect it.

## Commit conventions

Commits follow [Conventional Commits](https://www.conventionalcommits.org/). CI enforces this via commitlint.

```
type(scope): short description
```

Allowed types: `feat`, `fix`, `docs`, `style`, `refactor`, `perf`, `test`, `build`, `ci`, `chore`, `revert`.

All commits must include a DCO sign-off (`git commit -s`). Semantic-release on main generates tags and changelog from commit types.
Keep commit message body lines under 100 characters; commitlint enforces this.

## License headers

Every comment-capable source and configuration file requires an SPDX header:

```text
# Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
# SPDX-License-Identifier: Apache-2.0
```

CI checks this. Markdown remains headerless, and
`local/infra/local-registry/zot-config.json` is excluded because JSON cannot
carry comments. Run `make add-license-headers` to fix supported files.

## Third-party licenses

Regenerate `THIRD_PARTY_LICENSES.csv` when dependencies change in any Go
module. Use the repo target instead of editing it by hand:

```bash
make third-party-licenses
```

Include the regenerated CSV in the same branch as the dependency change that
made it stale.

## Go conventions

- `auth-callout/` has its own `go.mod`, `.golangci.yml`, and vendored
  dependencies (`-mod=vendor`).
- `dsx-agentgateway-bridge/` and local test tools are separate Go modules.
- `local/mqtt-client/` and `local/mqttbs/` are separate Go modules.

## Helm chart conventions

- Production charts are `deploy/nats-event-bus/` and
  `deploy/dsx-agent-gateway/`.
- `auth-callout/deploy/` remains a subchart dependency of the event-bus chart.
- Values follow the `global.eventBus.*` namespace for bus config, `auth-callout.*` for the subchart.
- CI renders the event-bus chart and lints the Agent Gateway chart with local
  validation values before the combined Kind deployment.

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
