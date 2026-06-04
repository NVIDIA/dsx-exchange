# Local LLM MCP Prompt Eval

This eval checks whether a local LLM can read an operator prompt, use the
`dsx-exchange-mcp` MCP tools, and produce the expected tool plan from
`internal/server/testdata/tool_call_expectations.json`.

It is intentionally opt-in because it depends on a local model runtime and can
be nondeterministic.

## What It Proves

- The MCP endpoint is reachable and completes the Streamable HTTP initialize
  flow.
- `tools/list` exposes the DSX Exchange tools, either directly or with the
  Latinum MCP Gateway prefix.
- The LLM actually emits MCP tool calls.
- The harness executes those tool calls and logs the tool trace.
- The LLM's final `planned_tool_calls` JSON contains the expected fixture calls.

By default, only `dsx_exchange_describe_topic` is exposed to the LLM. The model
must still include the planned `dsx_exchange_read_retained` and
`dsx_exchange_subscribe` calls in its final JSON, but the harness does not hit
the live broker unless explicitly enabled.

## LLM Endpoint

The harness expects an OpenAI-compatible local chat completions endpoint:

```sh
export DSX_EXCHANGE_LLM_BASE_URL=http://127.0.0.1:11434/v1
export DSX_EXCHANGE_LLM_MODEL='<local-model-name>'
```

Set `DSX_EXCHANGE_LLM_API_KEY` only if your local endpoint requires one.

## Direct In-Process MCP Server

If `DSX_EXCHANGE_MCP_URL` is not set, the test starts an in-process
`dsx-exchange-mcp` server with a test bearer and calls it directly.

```sh
RUN_EXCHANGE_LLM_MCP_EVAL=1 \
DSX_EXCHANGE_LLM_BASE_URL=http://127.0.0.1:11434/v1 \
DSX_EXCHANGE_LLM_MODEL='<local-model-name>' \
go test ./internal/server -run TestLocalLLMMCPPromptEval -count=1 -v
```

This path is best for fast local iteration on schema discovery behavior.

## Through Latinum MCP Gateway

To evaluate the production-style path, run or port-forward the gateway from the
`dsx-mcp` repo, then point this harness at the gateway `/mcp` endpoint:

```sh
export DSX_EXCHANGE_MCP_URL=http://localhost:18180/mcp
export DSX_EXCHANGE_E2E_BEARER="$TOKEN"

RUN_EXCHANGE_LLM_MCP_EVAL=1 \
DSX_EXCHANGE_LLM_BASE_URL=http://127.0.0.1:11434/v1 \
DSX_EXCHANGE_LLM_MODEL='<local-model-name>' \
go test ./internal/server -run TestLocalLLMMCPPromptEval -count=1 -v
```

The gateway may expose prefixed tools such as
`dsx-exchange-mcp-mcp_dsx_exchange_describe_topic`. The harness passes those
actual names to the LLM, executes them as returned, and normalizes them back to
canonical names when comparing with the fixture.

## Selecting Cases

By default the harness runs only the first fixture to keep local iteration fast.
Run one or more named cases with:

```sh
DSX_EXCHANGE_LLM_EVAL_CASES=bms-rack-temperature-latest,nico-machine-state
```

Run all cases by listing all fixture IDs from
`internal/server/testdata/tool_call_expectations.json`.

## Live Broker Tool Execution

Leave this off for normal prompt-planning evals:

```sh
export DSX_EXCHANGE_LLM_EXECUTE_LIVE_TOOLS=1
```

When enabled, the LLM can execute `dsx_exchange_read_retained` and
`dsx_exchange_subscribe` too. Use it only when the MCP endpoint has a valid
broker configuration, bearer token, topic permissions, and bounded test topics.

## Reading The Output

Run with `-v`. Each fixture logs:

- the natural-language question
- the MCP tool trace the LLM actually emitted
- the model's final user-facing answer
- the final planned tool calls compared against the fixture

Failures are useful evidence. They usually mean one of:

- the model did not call tools
- the model chose overly broad or wrong topic filters
- the schema description did not provide enough signal
- the final JSON was not machine-parseable
- the gateway or local MCP endpoint was not reachable
