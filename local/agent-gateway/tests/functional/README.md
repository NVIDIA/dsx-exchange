# Functional test runner

`make e2e` runs this Go-based suite against the chart-emitted dataplane.

## Why Go

- Auth behavior is verified through the chart-emitted dataplane.
- `local/agent-gateway/run.sh` runs `go test -json` and fails on
  empty, failed, or skipped tests.
- The suite uses `mark3labs/mcp-go` streamable HTTP transport for MCP response
  framing.
- Kubernetes checks use `client-go` in the same Go module, so the runner does
  not need a second language toolchain.

## Phases

- Functional tests must not mutate cluster state. Call
  `runner.ParallelReadOnly(t)` so they can run concurrently.
- Destructive functional tests mutate shared cluster state. Name them
  `TestDestructive...`, call `runner.DestructiveFunctional(t)`, and keep them
  serial through `RUN_DESTRUCTIVE_FUNCTIONAL=1`.
- Tests wait on observable events. Do not use `time.Sleep`; use watches,
  readiness conditions, request-scoped logs, or a short bounded polling loop
  only when there is no event source.

`local/agent-gateway/run.sh` exports `GATEWAY_URL`, defaulting to
`http://localhost:18180/mcp` for kind. Direct `go test` runs must set it.
Gateway requests use the Kind NodePort, not `kubectl port-forward`.

## Running

```bash
cd local/agent-gateway/tests/functional && GATEWAY_URL=http://localhost:18180/mcp go test ./...
```

Prefer `make e2e` for the normal dev loop. CI runs
`RUN_DESTRUCTIVE_FUNCTIONAL=1 make e2e` for the full destructive pass.
