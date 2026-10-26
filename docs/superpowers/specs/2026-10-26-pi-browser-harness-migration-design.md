# Pi Browser Harness Migration Design

**Date:** 2026-10-26

**Status:** Ready for review

## 1. Purpose

Replace the repository's custom agent loop with Pi while preserving the existing
browser harness behavior and EvalHub's durable Run and Trace model.

This is an adaptation to Pi, not a new agent platform:

- Pi owns model selection, conversation context, retries, turns, and tool dispatch.
- The browser harness becomes a Pi package containing a system prompt, an Agent
  Skill, an extension, and browser tools exposed through MCP.
- Existing deterministic browser logic remains code behind the tools.
- EvalHub remains the only source of truth for Run state, Trace data, cancellation,
  reruns, and audit history.
- The Python Playwright worker remains the browser execution engine.

The former AgentServer integration is not part of the target architecture.

## 2. Target Topology

```text
EvalHub API
  -> PostgreSQL outbox
  -> Kafka run command
  -> Go agenttrace-runtime
  -> Pi Browser Harness process
       -> @earendil-works/pi-coding-agent SDK
       -> browser system prompt
       -> browser-case-authoring Skill
       -> browser-harness extension
       -> Pi MCP extension
       -> browser-harness MCP tools
            -> deterministic case builder
            -> Python Playwright worker
  -> Pi event bridge
  -> EvalHub PostgreSQL Trace and terminal Run status
```

The Go runtime remains the durable command consumer and Trace writer. It no longer
performs model turns or dispatches model-selected operations. It starts one isolated
Pi Browser Harness process per active Run and consumes a newline-delimited event
stream from that process.

The TypeScript process embeds Pi through `createAgentSession()`. It is not an
AgentServer and does not expose a general agent HTTP API.

## 3. Pi-Native Package Shape

Create one package at `pi-browser-harness/`:

```text
pi-browser-harness/
  package.json
  tsconfig.json
  src/
    main.ts
    run-protocol.ts
    pi-session.ts
    trace-events.ts
    extension.ts
    mcp/
      server.ts
      schemas.ts
      tool-result.ts
    browser/
      worker-client.ts
      case-builder.ts
      grounding.ts
      observation-view.ts
      failure-ledger.ts
      replay.ts
  resources/
    system-prompt.md
    skills/
      browser-case-authoring/
        SKILL.md
  test/
```

`package.json` declares a Pi package and pins
`@earendil-works/pi-coding-agent` to the selected release line. The first
implementation targets version `1.4.x` and Node.js 22.19 or newer.

### 3.1 System prompt

`resources/system-prompt.md` contains only invariant runtime rules:

- author exactly one executable browser case;
- use browser tools instead of writing case JSON;
- only act on the latest observation;
- every action requires an objective expectation;
- never bypass authentication or CAPTCHA;
- finish only through `finish_case`;
- a successful final result requires a passing fresh-context dry run.

The prompt is installed with `DefaultResourceLoader.systemPromptOverride`.
`appendSystemPromptOverride` returns an empty list so user-global Pi prompt fragments
cannot change evaluation behavior.

### 3.2 Skill

`browser-case-authoring/SKILL.md` follows the Agent Skills specification. It contains
the detailed operating strategy that does not belong in the permanent system prompt:

- candidate selection and semantic grounding;
- blocker handling;
- repair after an authoring action fails;
- repair after a fresh dry run fails;
- final assertion completeness checks;
- minimal-case guidance.

The harness invokes the skill explicitly for every Run rather than relying on model
routing. It sends `/skill:browser-case-authoring <goal>` through the Pi session, so
the full skill instructions are present before the first model turn.

### 3.3 Extension and MCP

The extension performs Pi-specific wiring only:

- registers the Run-scoped browser MCP server;
- records lifecycle and provider events needed by EvalHub;
- blocks non-browser tools;
- closes the browser session and MCP server on `session_shutdown`.

The MCP server is a local stdio child created for one Run. It is not a shared
network service and it owns no durable data.

The SDK resource loader explicitly includes `createMcpExtension()`. After
`createAgentSession()`, the harness calls `session.bindExtensions({})` so the MCP
server connects during `session_start`.

The Pi session is created with:

- `SessionManager.inMemory()`;
- no Pi built-in file, shell, edit, or web tools;
- the selected EvalHub model and reasoning level;
- only the browser MCP tool namespace active;
- automatic retry bounded by EvalHub configuration.

MCP is the tool transport. Business rules do not live in the extension or MCP
configuration.

## 4. Browser Tool Surface

The migrated tool names remain compatible with the v2 harness:

- `open_page`
- `click`
- `input`
- `select`
- `check`
- `uncheck`
- `scroll_into_view`
- `hover`
- `dismiss_dialog`
- `upload_file`
- `assert_text`
- `assert_url`
- `assert_element`
- `assert_attribute`
- `assert_count`
- `finish_case`

`ask_user` is not included in the first EvalHub migration. An evaluation Run with
missing required input terminates with `input_required`; it does not wait on
process-local state. Interactive clarification can be added later through an
EvalHub-owned durable waiting-input contract.

Each MCP tool returns:

- model-facing text content;
- structured content containing `ok`, stable error code, summary, compact page view,
  committed steps, and optional dry-run failure;
- no raw unbounded DOM or screenshot bytes.

Tools execute sequentially because they share one Run-scoped browser session and case
builder.

`finish_case` validates the case and calls the worker's full-case execution endpoint
in a fresh browser context. It returns the case plus the objective execution result.
Only `ExecutionStatus=passed` is a successful evaluation result. Assistant prose and
Pi settlement never override this verdict.

## 5. Deterministic Logic Migration

The v2 browser planner is ported to TypeScript by responsibility, not translated as
one large file.

| Existing v2 responsibility | Target |
| --- | --- |
| `agentruntime/loop.go` model loop and tool dispatch | Delete; Pi `AgentSession` owns it |
| `agentruntime/context.go` model transcript reconstruction | Delete; Pi owns transcript and compaction |
| `agentruntime/prompt.go` invariant rules | `resources/system-prompt.md` |
| Planner strategy guidance | `resources/skills/browser-case-authoring/SKILL.md` |
| `planner/tools.go` schemas and dispatch | MCP tool schemas and handlers |
| `planner/planner.go` case building and authoring state | `browser/case-builder.ts` |
| `planner/resolve.go` grounding and compact observations | `grounding.ts`, `observation-view.ts` |
| `planner/failures.go` repeated-strategy rejection | `failure-ledger.ts` |
| `planner/replay.go` committed-prefix restoration | `replay.ts` |
| `worker/client.go` HTTP transport | `browser/worker-client.ts` |
| Go/Python browser contract | Retain as the cross-language behavioral contract |

The TypeScript port must preserve stable error codes and output shapes. Grounding,
step derivation, contract validation, failure fingerprints, replay, and dry-run
repair remain deterministic code. They must not be moved into prompts or left for
the model to reproduce.

The Python worker endpoints `/sessions`, `/navigate`, `/act`, `/execute`, and session
close remain unchanged unless contract tests identify a missing observation endpoint.

## 6. EvalHub Run Integration

### 6.1 Run protocol

The Go runtime starts `pi-browser-harness` with one JSON request on stdin:

```json
{
  "protocol_version": "pi-browser-harness.v1",
  "run_id": "run_...",
  "attempt_id": "attempt_...",
  "goal": "user goal",
  "model": {
    "provider": "provider",
    "name": "model",
    "reasoning_effort": "high"
  },
  "worker_url": "http://worker:8100",
  "artifact_scope": "run_..."
}
```

The harness writes one JSON object per line to stdout. Stdout is reserved for the
protocol; diagnostics go to stderr.

Event types are:

- `harness.started`
- `pi.message.completed`
- `pi.usage`
- `pi.tool.started`
- `pi.tool.completed`
- `browser.observation`
- `browser.case.completed`
- `harness.settled`
- `harness.failed`

Every event includes `run_id`, `attempt_id`, a monotonically increasing sequence,
timestamp, and event-specific payload.

`harness.settled` is emitted only after Pi's `agent_settled` event. It includes the
last assistant text for display, but terminal success is allowed only when a prior
`browser.case.completed` event contains a passing execution result.

### 6.2 Trace mapping

The Go event bridge maps:

| Pi Harness event | EvalHub Trace |
| --- | --- |
| completed assistant message/provider response | model span and usage |
| MCP tool start/end | operation span and attempt |
| browser observation | operation output/event payload |
| `finish_case` result | operation evidence and derived browser step nodes |
| harness terminal event | root Run completion |

Pi tool-call IDs are preserved as provider evidence. EvalHub derives its own stable
logical span and attempt IDs from Run ID, harness attempt, and event sequence.

The existing `runtime.Consumer`, inbox deduplication, leases, retry accounting, DLQ,
PostgreSQL store, Redis live updates, rerun lineage, and Trace APIs remain.

The current Go `runtime.Engine` and `model.Model` loop are no longer on the production
browser-evaluation path. Scripted-model support may remain only as a focused fixture
for existing unit tests until equivalent Pi fixtures cover the same contracts.

## 7. Failure, Cancellation, and Restart

### 7.1 Failure classes

- Invalid tool arguments and recoverable grounding/action failures return structured
  MCP tool errors so Pi may repair the plan.
- A worker outage, invalid protocol record, MCP process exit, replay failure, model
  provider terminal error, or missing `finish_case` is a failed harness attempt.
- A failed `finish_case` dry run is repairable while Pi continues. If Pi reaches
  `agent_settled` without a later passing dry run, the objective evaluation and the
  EvalHub Run are failed. Assistant prose cannot change that result.

### 7.2 Cancellation

When EvalHub observes durable cancellation:

1. cancel the Go process context;
2. send termination to the harness;
3. the harness calls `session.abort()` and `session.dispose()`;
4. the extension closes MCP and the authoring browser session;
5. EvalHub records the Run as cancelled.

Cleanup is idempotent.

### 7.3 Restart and idempotency

EvalHub does not adopt Pi session files as a second source of truth. Pi uses an
in-memory session for each harness attempt.

If the runtime process dies before terminal completion, EvalHub starts a new attempt
for the same logical Run. The attempt starts browser authoring from the beginning.
This is acceptable for the initial evaluation-only scope because fixture data and
browser contexts are disposable.

A completed Run is never started again by Kafka redelivery. The existing inbox and
terminal Run checks remain authoritative.

## 8. Implementation Phases

### Phase 1: Pi package and deterministic browser tools

- Scaffold `pi-browser-harness`.
- Add the system prompt and forced browser skill.
- Port the worker client, case builder, grounding, failure ledger, replay, and tool
  schemas with parity tests against v2 fixtures.
- Start Pi with MCP and verify one local goal reaches a passing `finish_case`.

### Phase 2: EvalHub bridge

- Add the Go harness process client and strict JSONL decoder.
- Add Pi event-to-Trace persistence.
- Wire the existing runtime consumer to the Pi runner.
- Map cancellation, deadlines, stderr, unexpected exit, and terminal verdicts.

### Phase 3: Default-path switch

- Add the harness service/runtime dependencies to Compose.
- Make Pi the default browser-evaluation path.
- Run the canonical fixture scenario and concurrent Run tests.
- Remove AgentServer configuration and adapter code.
- Remove the production wiring for the custom Go model/operation loop.

### Phase 4: Cleanup

- Delete v2 AgentRuntime code after parity acceptance.
- Remove obsolete prompts, operation profiles, scripted production wiring, and docs
  that claim the model writes complete case JSON.
- Keep browser contract fixtures and Python worker tests.

## 9. Verification

Required coverage:

- unit parity tests for every migrated tool schema and stable error code;
- table tests for grounding, ambiguity, failure fingerprints, compact page views, and
  committed-prefix replay;
- contract tests using the shared browser case fixtures;
- Pi session test proving only browser MCP tools are active;
- test proving the browser skill is force-loaded for each goal;
- event bridge tests for message, usage, tool, observation, finish, and settlement;
- malformed JSONL, duplicate sequence, stderr, premature exit, timeout, and
  cancellation tests;
- objective-verdict test proving assistant prose cannot mark a failed dry run passed;
- canonical Compose scenario against the local fixture site;
- concurrent Runs with isolated Pi, MCP, and browser sessions;
- rerun test proving a new EvalHub Run creates a new harness attempt and artifacts.

Migration acceptance requires:

1. the canonical fixture passes through Pi without AgentServer;
2. EvalHub Trace shows model turns, browser tools, execution steps, usage, and
   terminal status;
3. cancellation releases the Pi process and browser session;
4. a failed browser assertion produces a failed Run with preserved evidence;
5. no Pi session file or harness-local database is required to reconstruct EvalHub
   history.

## 10. Explicit Non-Goals

- A general-purpose AgentServer API.
- Durable Pi session storage outside EvalHub.
- Production-site irreversible actions.
- Multi-agent orchestration.
- Interactive `ask_user` waiting in the first migration.
- Rewriting the Python Playwright worker.
- Letting the model author raw case JSON, selectors, or unverified conditions.

## 11. Official Pi References

- SDK: `packages/coding-agent/docs/sdk.md`
- Extensions and MCP registration: `packages/coding-agent/docs/extensions.md`
- Skills: `packages/coding-agent/docs/skills.md`
- Pi packages: `packages/coding-agent/docs/packages.md`
- Upstream repository: `https://github.com/earendil-works/pi`
