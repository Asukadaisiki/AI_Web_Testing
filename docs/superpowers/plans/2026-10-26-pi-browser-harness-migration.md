# Pi Browser Harness Migration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the production Go model/operation loop with a Run-scoped Pi Browser Harness process while preserving deterministic browser authoring, the Python Playwright worker contract, and EvalHub's durable Run and Trace history.

**Architecture:** A Node.js/TypeScript package embeds Pi 1.4.0, force-loads one browser authoring skill, and exposes only Run-scoped browser MCP tools backed by deterministic TypeScript logic and the existing worker HTTP API. The Go runtime remains the Kafka consumer and PostgreSQL Trace writer; it launches one harness process per active Run, strictly decodes JSONL events, maps them to durable Trace records, and derives terminal Run status only from objective browser execution evidence.

**Tech Stack:** Node.js >=22.19.0, TypeScript, Vitest, `@earendil-works/pi-coding-agent` 1.4.0, Model Context Protocol over stdio, Go 1.24, PostgreSQL 16, Kafka 3.9, Redis 7, Python 3.12/FastAPI/Playwright, Docker Compose.

**Spec:** `docs/superpowers/specs/2026-10-26-pi-browser-harness-migration-design.md`

## Global Constraints

- Pin `@earendil-works/pi-coding-agent` to exactly `1.4.0`; its published engine requirement is Node.js `>=22.19.0`.
- Pi owns model selection, conversation context, retries, turns, and tool dispatch.
- EvalHub PostgreSQL remains the only source of truth for Run state, Trace data, cancellation, reruns, and audit history.
- Pi sessions use `SessionManager.inMemory()` and must not create a durable Pi session file or harness-local database.
- The harness process accepts exactly one `pi-browser-harness.v1` request on stdin and reserves stdout for newline-delimited protocol records; diagnostics go to stderr.
- Only the browser MCP namespace is active. Pi built-in file, shell, edit, write, web, codemode, and tool-search tools are unavailable.
- The full `browser-case-authoring` skill is force-loaded for every Run with `/skill:browser-case-authoring <goal>`.
- `ask_user` is not exposed. Missing required input produces `input_required`.
- Browser tools execute sequentially against one Run-scoped authoring session.
- Every authoring action has an objective expectation, and only the latest observation may ground a target.
- `finish_case` validates the case and runs it in a fresh browser context; only `ExecutionStatus=passed` can produce a successful evaluation.
- Assistant prose and Pi settlement never override the objective execution verdict.
- Stable v2 tool names, contract shapes, and error codes remain compatible.
- The Python worker endpoints and browser case contract remain unchanged unless a contract test proves a missing endpoint.
- Existing Kafka inbox deduplication, leases, retry accounting, DLQ, PostgreSQL store, Redis live updates, rerun lineage, and Trace APIs remain authoritative.
- New behavior is test-first. Every red test must fail for the intended missing behavior before production code is written.
- The v2 migration source is the `v2` Git branch. Do not restore `backend/internal/agentruntime`, `backend/internal/planner`, or `backend/internal/worker` into the production Go path.

## File Map And Ownership

| Path | Responsibility |
|---|---|
| `pi-browser-harness/src/run-protocol.ts` | Strict stdin request and stdout event schemas, sequence allocation, terminal verdict rules |
| `pi-browser-harness/src/main.ts` | One-request process lifecycle, signal handling, stderr diagnostics |
| `pi-browser-harness/src/pi-session.ts` | Pi 1.4.0 resource/model/session construction and forced skill invocation |
| `pi-browser-harness/src/trace-events.ts` | Pi event to harness JSONL event normalization |
| `pi-browser-harness/src/extension.ts` | Run-scoped MCP registration, non-browser tool blocking, shutdown cleanup |
| `pi-browser-harness/src/mcp/server.ts` | Executable local stdio MCP child, Run-scoped CaseBuilder owner, and sequential tool handlers |
| `pi-browser-harness/src/mcp/schemas.ts` | Stable browser tool names and JSON schemas |
| `pi-browser-harness/src/mcp/tool-result.ts` | Model text plus bounded structured result encoding |
| `pi-browser-harness/src/browser/contract.ts` | TypeScript mirror of the retained Go/Python browser contract |
| `pi-browser-harness/src/browser/worker-client.ts` | Typed transport for `/sessions`, `/navigate`, `/act`, `/execute`, and close |
| `pi-browser-harness/src/browser/grounding.ts` | Deterministic hint/spec/candidate grounding |
| `pi-browser-harness/src/browser/observation-view.ts` | Bounded model-facing page and committed-step views |
| `pi-browser-harness/src/browser/failure-ledger.ts` | Page fingerprints and repeated-strategy rejection |
| `pi-browser-harness/src/browser/replay.ts` | Exact committed-prefix restoration and dry-run repair |
| `pi-browser-harness/src/browser/case-builder.ts` | Run-scoped authoring state and objective finish verdict |
| `pi-browser-harness/resources/system-prompt.md` | Invariant runtime rules only |
| `pi-browser-harness/resources/skills/browser-case-authoring/SKILL.md` | Detailed authoring and repair strategy |
| `backend/internal/piharness/protocol.go` | Go mirror of `pi-browser-harness.v1` |
| `backend/internal/piharness/process.go` | Child process, stdin/stdout/stderr, cancellation, deadline, exit handling |
| `backend/internal/runtime/pi_runner.go` | Harness event validation, Trace persistence, terminal Run completion |
| `backend/internal/postgres/harness.go` | Atomic harness attempt allocation and sequence-aware event persistence |
| `backend/internal/runtime/pi_store.go` | Runtime adapter for Pi event-derived Trace data |
| `backend/cmd/agenttrace-runtime/main.go` | Production wiring to the Pi runner |
| `compose.yaml` | Node-capable runtime and harness command/configuration |

---

### Task 1: Scaffold The Pi Package And Strict Run Protocol

**Files:**
- Create: `pi-browser-harness/package.json`
- Create: `pi-browser-harness/package-lock.json`
- Create: `pi-browser-harness/tsconfig.json`
- Create: `pi-browser-harness/vitest.config.ts`
- Create: `pi-browser-harness/src/run-protocol.ts`
- Create: `pi-browser-harness/test/run-protocol.test.ts`

**Interfaces:**
- Produces `parseRunRequest(input: string): RunRequest`.
- Produces `HarnessEventWriter.write(type, payload)` with monotonic sequence numbers.
- Produces `deriveTerminalVerdict(state): "succeeded" | "failed"`.

- [ ] **Step 1: Write the protocol tests**

Create tests that use this exact valid request:

```ts
const request = {
  protocol_version: "pi-browser-harness.v1",
  run_id: "run_1",
  attempt_id: "attempt_1",
  goal: "verify the cart",
  model: { provider: "scripted", name: "browser-fixture", reasoning_effort: "high" },
  worker_url: "http://worker:8100",
  artifact_scope: "run_1",
};
```

Assert that unknown fields, blank IDs, relative `worker_url`, and any protocol version other than `pi-browser-harness.v1` are rejected. Assert that emitted events contain the request IDs, UTC timestamp, and sequences `1, 2, 3`. Assert that a settled assistant message without a prior passing `browser.case.completed` verdict is failed.

- [ ] **Step 2: Run the test and verify RED**

Run:

```bash
cd pi-browser-harness
npm test -- --run test/run-protocol.test.ts
```

Expected: FAIL because the package and protocol module do not exist.

- [ ] **Step 3: Create the package and protocol implementation**

Use this manifest contract:

```json
{
  "name": "@agenttrace/pi-browser-harness",
  "private": true,
  "version": "0.1.0",
  "type": "module",
  "engines": { "node": ">=22.19.0" },
  "pi": {
    "extensions": ["./dist/extension.js"],
    "skills": ["./resources/skills"]
  },
  "dependencies": {
    "@earendil-works/pi-coding-agent": "1.4.0",
    "@modelcontextprotocol/sdk": "1.20.2",
    "typebox": "1.3.27"
  },
  "devDependencies": {
    "@types/node": "22.19.19",
    "typescript": "5.9.3",
    "vitest": "4.0.4"
  }
}
```

Implement discriminated event types exactly as specified: `harness.started`, `pi.message.completed`, `pi.usage`, `pi.tool.started`, `pi.tool.completed`, `browser.observation`, `browser.case.completed`, `harness.settled`, and `harness.failed`.

- [ ] **Step 4: Verify protocol tests and typecheck**

Run:

```bash
cd pi-browser-harness
npm ci --cache /tmp/agenttrace-npm-cache
npm test -- --run test/run-protocol.test.ts
npm run typecheck
```

Expected: all commands exit zero.

- [ ] **Step 5: Commit**

```bash
git add pi-browser-harness
git commit -m "build: scaffold Pi browser harness protocol"
```

---

### Task 2: Mirror The Browser Contract And Implement The Worker Client

**Files:**
- Create: `pi-browser-harness/src/browser/contract.ts`
- Create: `pi-browser-harness/src/browser/worker-client.ts`
- Create: `pi-browser-harness/test/contract-parity.test.ts`
- Create: `pi-browser-harness/test/worker-client.test.ts`
- Modify: `fixtures/contract/case_contract.json`

**Interfaces:**
- Produces TypeScript types for `Case`, `Step`, `Observation`, `ActRequest`, `ActResponse`, and `ExecutionResult`.
- Produces `WorkerClient.openSession`, `navigate`, `act`, `execute`, and `closeSession`.
- Produces `WorkerTransportError` with `statusCode`, `code`, and `detail`.

- [ ] **Step 1: Write failing contract and transport tests**

Load `fixtures/contract/case_contract.json` and assert every valid fixture is accepted and every invalid fixture returns the same stable error code as `backend/internal/contract/contract_test.go` and `worker/tests/test_contract_conformance.py`.

Use an in-process HTTP test server to assert exact routes and bodies:

```text
POST   /sessions
POST   /sessions/{browser_session_id}/navigate
POST   /sessions/{browser_session_id}/act
POST   /execute
DELETE /sessions/{browser_session_id}
```

Assert that AbortSignal cancels fetch, non-2xx worker errors preserve typed error fields, malformed success JSON becomes `worker_error`, and responses larger than the configured bound are rejected.

- [ ] **Step 2: Run the tests and verify RED**

Run:

```bash
cd pi-browser-harness
npm test -- --run test/contract-parity.test.ts test/worker-client.test.ts
```

Expected: FAIL because the contract and client are missing.

- [ ] **Step 3: Implement the retained contract**

Port the normalization and validation behavior from current `backend/internal/contract/**`, not from prompts. Keep these constants exact:

```ts
export const CASE_VERSION = "loop.case.v1";
export const DEFAULT_CONDITION_TIMEOUT_MS = 10_000;
export const DEFAULT_STEP_TIMEOUT_MS = 20_000;
```

The fixture must cover all current actions and stable codes, including `step_target_ungrounded`, `condition_unknown_type`, `goto_value_not_absolute`, and `step_unexpected_submit`.

- [ ] **Step 4: Implement the bounded worker client**

Default timeout is five minutes. `openSession` sends `{session_id: artifactScope}` and requires a non-empty returned browser session ID. `execute` sends `{session_id: artifactScope, case: artifact}`. The client contains transport only; no grounding or case-building decisions.

- [ ] **Step 5: Run cross-language contract verification**

Run:

```bash
cd pi-browser-harness && npm test -- --run test/contract-parity.test.ts test/worker-client.test.ts
cd ../backend && go test ./internal/contract -v
cd ../worker && uv run python -m unittest tests.test_contract_conformance
```

Expected: all commands exit zero.

- [ ] **Step 6: Commit**

```bash
git add pi-browser-harness fixtures/contract/case_contract.json
git commit -m "feat: add Pi harness browser contract client"
```

---

### Task 3: Port Grounding, Compact Observations, And Failure Fingerprints

**Files:**
- Create: `pi-browser-harness/src/browser/grounding.ts`
- Create: `pi-browser-harness/src/browser/observation-view.ts`
- Create: `pi-browser-harness/src/browser/failure-ledger.ts`
- Create: `pi-browser-harness/test/grounding.test.ts`
- Create: `pi-browser-harness/test/observation-view.test.ts`
- Create: `pi-browser-harness/test/failure-ledger.test.ts`

**Interfaces:**
- Produces `resolveTarget(request, observation): GroundedTarget`.
- Produces `buildPageView(observation, goal, lastResult): PageView`.
- Produces `pageFingerprint(observation): string` and `FailureLedger`.

- [ ] **Step 1: Port the v2 table tests before implementation**

Translate the cases from:

```text
v2:backend/internal/planner/resolve_test.go
v2:backend/internal/planner/failures_test.go
```

Preserve these exact resolution codes:

```text
target_not_found
target_ambiguous
target_hint_empty
scope_not_found
candidate_not_found
candidate_incompatible
strategy_repeated
```

Add compact-view bounds: at most 20 elements, 12 action candidates, 8 scopes, and 5 blockers; prioritization must retain goal- and last-result-relevant entries.

- [ ] **Step 2: Run the tests and verify RED**

Run:

```bash
cd pi-browser-harness
npm test -- --run test/grounding.test.ts test/observation-view.test.ts test/failure-ledger.test.ts
```

Expected: FAIL because the deterministic modules are missing.

- [ ] **Step 3: Implement deterministic grounding**

Port by responsibility from `v2:backend/internal/planner/resolve.go`. Candidate IDs are valid only in the latest observation and only for the advertised action. Semantic fallback is accepted only when exactly one material candidate matches. The returned locator must be one of the locators already verified on the resolved element.

- [ ] **Step 4: Implement compact views and failure fingerprints**

Port `PageFingerprint` canonicalization from `v2:backend/internal/planner/failures.go`: canonical URL, title, sorted candidate IDs, visible control values keyed by canonical locator, and blocker kinds. Keep at most eight unique failure signatures and reject a repeated action/target on an unchanged page.

- [ ] **Step 5: Verify**

Run:

```bash
cd pi-browser-harness
npm test -- --run test/grounding.test.ts test/observation-view.test.ts test/failure-ledger.test.ts
npm run typecheck
```

Expected: all commands exit zero.

- [ ] **Step 6: Commit**

```bash
git add pi-browser-harness
git commit -m "feat: port deterministic browser grounding"
```

---

### Task 4: Port Case Building, Transactional Authoring, Replay, And Dry-Run Repair

**Files:**
- Create: `pi-browser-harness/src/browser/case-builder.ts`
- Create: `pi-browser-harness/src/browser/replay.ts`
- Create: `pi-browser-harness/test/case-builder.test.ts`
- Create: `pi-browser-harness/test/replay.test.ts`
- Create: `pi-browser-harness/test/assertions.test.ts`

**Interfaces:**
- Produces `CaseBuilder.create(deps): Promise<CaseBuilder>`.
- Produces `CaseBuilder.call(toolName, args, signal): Promise<ToolOutcome>`.
- Produces `CaseBuilder.close(): Promise<void>`.
- Produces `prepareDryRunRepair(execution): Promise<ToolResult>`.

- [ ] **Step 1: Write failing transactional authoring tests**

Translate behavior from:

```text
v2:backend/internal/planner/planner.go
v2:backend/internal/planner/replay.go
v2:backend/internal/planner/assertions_test.go
v2:backend/internal/planner/replay_test.go
```

Assert:

- `open_page` must be first and use an absolute URL.
- Every target action requires at least one objective expectation.
- A failed authoring action never commits a step.
- A successful action commits only after the worker reports `passed`.
- `assert_text` and `assert_url` evaluate current observation without mutating the page.
- Replay replaces the browser session and restores only the exact committed prefix.
- A failed fresh dry run truncates from the first failed step and replays the retained prefix.
- Replay failure is fatal with `committed_prefix_replay_failed`.
- Navigation retries transient worker failures three times and reports `navigation_failed`; overload pages report `site_overloaded`.

- [ ] **Step 2: Run the tests and verify RED**

Run:

```bash
cd pi-browser-harness
npm test -- --run test/case-builder.test.ts test/replay.test.ts test/assertions.test.ts
```

Expected: FAIL because the case builder is missing.

- [ ] **Step 3: Implement the case builder**

Keep authoring state private: artifact scope, browser session ID, goal, base URL, latest observation, committed steps, and failure ledger. Derive every `Step` with the retained contract helpers, validate it immediately, execute it through the worker, then commit it only after objective success.

- [ ] **Step 4: Implement finish and repair**

`finish_case` builds a normalized case and calls `/execute` in a fresh worker context. A passing result returns both case and execution. A failed result remains repairable, removes the failed tail, restores the retained prefix, and returns `dry_run_failed` with a bounded failed-step summary. No caller may infer success from assistant text.

- [ ] **Step 5: Verify**

Run:

```bash
cd pi-browser-harness
npm test -- --run test/case-builder.test.ts test/replay.test.ts test/assertions.test.ts
npm run typecheck
```

Expected: all commands exit zero.

- [ ] **Step 6: Commit**

```bash
git add pi-browser-harness
git commit -m "feat: port browser case authoring state"
```

---

### Task 5: Expose The Stable Browser Tools Through A Run-Scoped MCP Server

**Files:**
- Create: `pi-browser-harness/src/mcp/schemas.ts`
- Create: `pi-browser-harness/src/mcp/tool-result.ts`
- Create: `pi-browser-harness/src/mcp/server.ts`
- Create: `pi-browser-harness/test/tool-schemas.test.ts`
- Create: `pi-browser-harness/test/mcp-server.test.ts`

**Interfaces:**
- Produces `BROWSER_TOOL_NAMES` containing the 16 target tools and no `ask_user`.
- Produces `createBrowserMcpServer(builder): BrowserMcpServer` and executable `dist/mcp/server.js`.
- Tool structured content contains `ok`, stable error code, summary, compact page, committed steps, and optional dry-run failure.

- [ ] **Step 1: Write failing schema parity tests**

Port the schemas from `v2:backend/internal/planner/tools.go`. Assert this exact order:

```text
open_page
click
input
select
check
uncheck
scroll_into_view
hover
dismiss_dialog
upload_file
assert_text
assert_url
assert_element
assert_attribute
assert_count
finish_case
```

Assert `ask_user` is absent, action schemas require `intent`, `input` requires `value`, and `finish_case` requires `name`.

- [ ] **Step 2: Write failing MCP lifecycle tests**

Use an in-memory MCP transport. Assert tool calls execute one at a time even when requested concurrently, every result has model-facing text plus structured content, observation-changing results carry the latest compact observation, finish results carry the case and objective execution, and `close()` closes the builder exactly once.

- [ ] **Step 3: Run tests and verify RED**

Run:

```bash
cd pi-browser-harness
npm test -- --run test/tool-schemas.test.ts test/mcp-server.test.ts
```

Expected: FAIL because MCP modules are missing.

- [ ] **Step 4: Implement schemas and server**

Use MCP stdio for the production server. The child reads `AGENTTRACE_HARNESS_WORKER_URL`, `AGENTTRACE_HARNESS_ARTIFACT_SCOPE`, and `AGENTTRACE_HARNESS_GOAL`, creates the worker client and CaseBuilder, then serves MCP on stdin/stdout. Serialize calls with a promise queue owned by the Run-scoped server. Never include screenshot bytes or unbounded DOM in tool results. Fatal builder errors terminate the MCP child; recoverable grounding/action/dry-run failures remain typed tool results. SIGTERM and transport close must idempotently close the authoring browser session.

- [ ] **Step 5: Verify**

Run:

```bash
cd pi-browser-harness
npm test -- --run test/tool-schemas.test.ts test/mcp-server.test.ts
npm run typecheck
```

Expected: all commands exit zero.

- [ ] **Step 6: Commit**

```bash
git add pi-browser-harness
git commit -m "feat: expose browser authoring over MCP"
```

---

### Task 6: Embed Pi With Forced Resources, Restricted Tools, And JSONL Events

**Files:**
- Create: `pi-browser-harness/resources/system-prompt.md`
- Create: `pi-browser-harness/resources/skills/browser-case-authoring/SKILL.md`
- Create: `pi-browser-harness/src/extension.ts`
- Create: `pi-browser-harness/src/trace-events.ts`
- Create: `pi-browser-harness/src/pi-session.ts`
- Create: `pi-browser-harness/src/main.ts`
- Create: `pi-browser-harness/test/pi-session.test.ts`
- Create: `pi-browser-harness/test/trace-events.test.ts`
- Create: `pi-browser-harness/test/main.test.ts`

**Interfaces:**
- Produces `createBrowserSession(request, deps): Promise<BrowserSession>`.
- Produces `mapPiEvent(event, state): HarnessEvent[]`.
- Produces the executable `dist/main.js`.

- [ ] **Step 1: Write failing Pi configuration tests**

Inject a session factory and assert:

- `DefaultResourceLoader.systemPromptOverride` returns the checked-in system prompt.
- `appendSystemPromptOverride` returns `[]`.
- only the checked-in browser skill is loaded.
- `SessionManager.inMemory()` and `SettingsManager.inMemory()` are used.
- `createMcpExtension()` is explicitly included.
- `session.bindExtensions({})` is awaited before prompting.
- active tool names match `mcp__browser_harness__*` only.
- the exact first prompt is `/skill:browser-case-authoring ${goal}`.
- session disposal closes MCP and the worker session.

- [ ] **Step 2: Write failing event and process tests**

Assert authoritative completed assistant messages map to `pi.message.completed`, usage maps once to `pi.usage`, tool IDs are preserved, compact observations and objective finish evidence are derived from MCP tool structured results, and `harness.settled` is emitted only after `agent_settled`.

Spawn the CLI with a valid request and a fake session dependency. Assert stdout contains only valid JSONL, logs are on stderr, malformed stdin emits one `harness.failed`, SIGTERM calls `session.abort()` then `session.dispose()`, and settlement without passing finish evidence exits non-zero.

- [ ] **Step 3: Run tests and verify RED**

Run:

```bash
cd pi-browser-harness
npm test -- --run test/pi-session.test.ts test/trace-events.test.ts test/main.test.ts
```

Expected: FAIL because the session and resources are missing.

- [ ] **Step 4: Implement Pi 1.4.0 integration**

Construct Pi with:

```ts
createAgentSession({
  cwd,
  model,
  thinkingLevel,
  modelRuntime,
  resourceLoader,
  tools: ["mcp__browser_harness__*"],
  sessionManager: SessionManager.inMemory(cwd),
  settingsManager: SettingsManager.inMemory({ retry: retrySettings }),
});
```

Register an OpenAI-compatible provider through `ModelRuntime.registerProvider` using server-owned base URL/API key environment values and the request's provider/name. Keep a scripted provider only under the explicit `scripted` provider for deterministic tests.

- [ ] **Step 5: Implement resources and lifecycle**

The system prompt contains only the seven invariant rules in spec section 3.1. The skill includes candidate selection, blocker handling, authoring repair, dry-run repair, final assertion completeness, and minimal-case guidance. The extension registers one direct-exposure stdio server named `browser-harness` whose command is the compiled MCP child and whose Run values are passed through its environment. It blocks every non-browser tool and performs idempotent MCP child cleanup on `session_shutdown`; the child owns and closes the browser session.

- [ ] **Step 6: Verify all harness tests and build**

Run:

```bash
cd pi-browser-harness
npm test -- --run
npm run typecheck
npm run build
```

Expected: all commands exit zero.

- [ ] **Step 7: Commit**

```bash
git add pi-browser-harness
git commit -m "feat: embed Pi browser authoring session"
```

---

### Task 7: Add The Go Harness Process Client And Strict JSONL Decoder

**Files:**
- Create: `backend/internal/piharness/protocol.go`
- Create: `backend/internal/piharness/protocol_test.go`
- Create: `backend/internal/piharness/process.go`
- Create: `backend/internal/piharness/process_test.go`

**Interfaces:**
- Produces `piharness.Request`, `piharness.Event`, and typed payload structs.
- Produces `ProcessRunner.Run(ctx, request, onEvent) error`.
- Produces stable errors for malformed JSONL, duplicate/out-of-order sequence, stderr overflow, premature exit, missing terminal event, and timeout.

- [ ] **Step 1: Write failing protocol decoder tests**

Assert every event requires matching `run_id` and `attempt_id`, sequence starts at 1 and increments by exactly one, timestamps parse as UTC RFC3339Nano, unknown event types fail closed, and a second terminal record is rejected.

- [ ] **Step 2: Write failing process tests**

Use helper subprocesses to cover:

- one request written to stdin and stdin closed;
- stdout line size bound;
- stderr retained as bounded diagnostics without entering protocol output;
- context cancellation sends interrupt, waits the grace period, then kills;
- non-zero exit after `harness.failed` returns the typed harness failure;
- zero exit before terminal event is a premature-exit error;
- malformed JSON and duplicate sequence stop processing.

- [ ] **Step 3: Run tests and verify RED**

Run:

```bash
cd backend
go test ./internal/piharness -v
```

Expected: FAIL because the package is missing.

- [ ] **Step 4: Implement the client**

Default command is:

```text
node /app/pi-browser-harness/dist/main.js
```

Make command, working directory, environment, maximum line bytes, stderr bytes, termination grace, and clock injectable. Never invoke a shell. The callback is called synchronously in sequence order so persistence applies backpressure.

- [ ] **Step 5: Verify**

Run:

```bash
cd backend
go test ./internal/piharness -v
```

Expected: all tests pass.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/piharness
git commit -m "feat: add Pi harness process client"
```

---

### Task 8: Persist Pi Events As EvalHub Trace And Derive Objective Terminal Status

**Files:**
- Create: `backend/internal/runtime/pi_runner.go`
- Create: `backend/internal/runtime/pi_runner_test.go`
- Create: `backend/internal/runtime/pi_store.go`
- Create: `backend/internal/runtime/pi_store_test.go`
- Create: `backend/internal/postgres/harness.go`
- Create: `backend/internal/postgres/harness_integration_test.go`
- Create: `backend/internal/postgres/migrations/005_harness_events.sql`
- Modify: `backend/internal/postgres/runtime.go`
- Modify: `backend/internal/postgres/traces.go`
- Modify: `backend/internal/postgres/store_integration_test.go`
- Modify: `backend/internal/event/envelope.go`
- Modify: `backend/internal/event/envelope_test.go`

**Interfaces:**
- Produces `runtime.PiRunner.Run(context.Context, event.RunCommand) error`.
- Produces `postgres.Store.BeginHarnessAttempt`, `AppendHarnessEvent`, and `FinishHarnessAttempt`.
- Produces `PiTraceStore.StartRun`, `AppendHarnessEvent`, and `CompleteHarnessRun`.
- Keeps `runtime.CommandRunner` unchanged for the Kafka consumer.

- [ ] **Step 1: Write failing event bridge tests**

For one event stream, assert:

- `harness.started` claims the queued Run and creates/continues the root span.
- `pi.message.completed` writes a model span and the authoritative completed response payload.
- `pi.usage` writes usage under the matching model attempt.
- `pi.tool.started` and `pi.tool.completed` write one logical operation span and attempt while preserving Pi tool-call ID in evidence.
- `browser.observation` is persisted as operation output/event payload.
- `browser.case.completed` writes operation evidence plus derived browser-step nodes from execution steps.
- `harness.settled` succeeds only after a passing `browser.case.completed`.
- failed finish, missing finish, `harness.failed`, malformed sequence, and premature exit finish the Run as failed with preserved evidence.
- cancellation finishes the Run as cancelled.
- replay of an already terminal Run starts no child process.
- a process crash followed by Kafka redelivery allocates a new attempt for the same logical Run without overwriting prior evidence.

- [ ] **Step 2: Run tests and verify RED**

Run:

```bash
cd backend
go test ./internal/runtime -run 'TestPi' -v
```

Expected: FAIL because the Pi runner and store are missing.

- [ ] **Step 3: Implement stable identities and persistence**

Add durable harness attempt and source-sequence columns/constraints with migration `005_harness_events.sql`. `BeginHarnessAttempt` atomically claims a queued/active Run and allocates a monotonically increasing attempt before process spawn. `AppendHarnessEvent` permits event-only records and atomically validates attempt identity plus exact next source sequence while writing optional span, attempt, usage, and operation evidence. Exact replay is a no-op; the same source identity with different payload is a conflict.

Derive EvalHub logical span and attempt IDs from Run ID, harness attempt ID, event sequence, and event kind. Do not use Pi IDs as primary keys; store them in provider evidence. Reuse existing PostgreSQL trace helpers behind the new atomic API instead of issuing SQL from the bridge.

- [ ] **Step 4: Implement terminal verdict rules**

The final assistant text is display content only. `ExecutionStatus=passed` is required for `RunSucceeded`; any other settled state is `RunFailed`. A durable cancel request or cancelled process context maps to `RunCancelled`. Return `terminalRunError` after terminal failure so the consumer commits its inbox row instead of retrying completed work.

- [ ] **Step 5: Verify unit and PostgreSQL integration tests**

Run:

```bash
cd backend
go test ./internal/runtime -run 'TestPi' -v
AGENTTRACE_TEST_POSTGRES_DSN="$AGENTTRACE_TEST_POSTGRES_DSN" go test ./internal/postgres -run 'Test.*Pi' -v
```

Expected: unit tests pass. PostgreSQL tests pass when the isolated test DSN is configured; otherwise they report the existing explicit skip.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/runtime backend/internal/postgres
git commit -m "feat: persist Pi harness trace events"
```

---

### Task 9: Switch The Production Runtime, Configuration, And Cancellation Path

**Files:**
- Modify: `backend/internal/config/config.go`
- Modify: `backend/internal/config/config_test.go`
- Modify: `backend/cmd/agenttrace-runtime/main.go`
- Modify: `backend/cmd/agenttrace-runtime/main_test.go`
- Modify: `backend/internal/runtime/consumer_test.go`
- Create: `backend/internal/runtime/retry_store.go`
- Delete: `backend/internal/runtime/engine.go`
- Delete: `backend/internal/runtime/engine_test.go`
- Delete: `backend/internal/runtime/resume_test.go`
- Delete: `backend/internal/runtime/checkpoint.go`
- Modify: `backend/internal/runtime/postgres.go`
- Delete: `backend/internal/model/openai.go`
- Delete: `backend/internal/model/openai_test.go`
- Retain: `backend/internal/model/scripted.go`
- Retain: `backend/internal/model/scripted_test.go`
- Retain: `backend/internal/operation/**`

**Interfaces:**
- `agenttrace-runtime` constructs `runtime.PiRunner` as the sole production `CommandRunner`.
- Produces `config.Harness` with command, directory, provider base URL/key, timeout, termination grace, and output bounds.
- Existing consumer concurrency, inbox, retry, DLQ, lease, and terminal-error contracts remain.

- [ ] **Step 1: Write failing configuration and wiring tests**

Assert defaults:

```text
AGENTTRACE_PI_HARNESS_COMMAND=node
AGENTTRACE_PI_HARNESS_ENTRY=/app/pi-browser-harness/dist/main.js
AGENTTRACE_PI_HARNESS_DIR=/app
AGENTTRACE_PI_HARNESS_TIMEOUT=20m
AGENTTRACE_PI_HARNESS_TERMINATION_GRACE=5s
AGENTTRACE_PI_HARNESS_MAX_LINE_BYTES=4194304
AGENTTRACE_PI_HARNESS_MAX_STDERR_BYTES=65536
```

Assert `selectModel` and HTTP operation dispatcher are no longer called by the runtime entry point. Assert cancellation of a running command propagates to the child process and the consumer still commits terminal failures/cancellations.

- [ ] **Step 2: Run tests and verify RED**

Run:

```bash
cd backend
go test ./internal/config ./cmd/agenttrace-runtime ./internal/runtime -v
```

Expected: FAIL because harness configuration and production wiring are missing.

- [ ] **Step 3: Wire the Pi runner**

Build `piharness.Request` from the immutable `RunCommand`, profile reasoning effort, configured worker URL, and a fresh attempt ID. Keep the existing bounded runner, consumer workers, Kafka inbox, retry store, Redis lease, and DLQ wiring.

- [ ] **Step 4: Remove obsolete production loop code**

Delete the current Go `runtime.Engine` path only after Pi runner tests pass. Move `PostgresRetryStore` unchanged from `runtime/postgres.go` into `runtime/retry_store.go`, then remove only the old `PostgresCheckpointStore` and Engine step adapters. Keep scripted model and operation packages temporarily because existing focused unit/contract fixtures still import them; no production main package may construct either one.

- [ ] **Step 5: Verify all Go tests**

Run:

```bash
cd backend
go test -p 1 ./...
```

Expected: all packages pass.

- [ ] **Step 6: Commit**

```bash
git add backend
git commit -m "refactor: switch runtime to Pi harness"
```

---

### Task 10: Compose Cutover, Canonical Acceptance, Concurrent Isolation, And Cleanup

**Files:**
- Create: `pi-browser-harness/Dockerfile`
- Modify: `compose.yaml`
- Modify: `.env.example`
- Modify: `run_v3_tests.py`
- Create: `integration/v3/pi_harness_scenario.py`
- Create: `integration/v3/test_pi_harness_scenario.py`
- Modify: `integration/v3/concurrency_rerun_scenario.py`
- Modify: `README.md`
- Modify: `docs/architecture.md`
- Modify: `docs/operations.md`
- Modify: `CONTRACT.md`
- Modify: `backend/internal/agentprofile/browser_eval.json`
- Modify: `backend/internal/agentprofile/profile.go`
- Modify: `backend/internal/agentprofile/profile_test.go`
- Modify: `backend/internal/event/envelope.go`
- Modify: `backend/internal/api/v3/messages.go`
- Modify: `worker/loop_worker/main.py`
- Modify: `worker/loop_worker/contracts.py`
- Delete: `worker/loop_worker/idempotency.py`
- Delete: `worker/tests/test_operations_api.py`
- Delete: `backend/internal/operation/dispatcher.go`
- Delete: `backend/internal/operation/dispatcher_test.go`
- Delete: `backend/internal/operation/http.go`
- Delete: `backend/internal/operation/schema.go`
- Delete any remaining AgentServer adapter/configuration introduced on the migration branch.

**Interfaces:**
- Compose runtime image contains Go plus Node.js >=22.19.0 and the built harness.
- Canonical acceptance uses the Pi scripted fixture provider and real Playwright worker.
- Production provider configuration remains environment-owned and never enters Run history as a secret.

- [ ] **Step 1: Write failing integration assertions**

The canonical scenario must assert:

1. the Run passes through Pi without AgentServer;
2. Trace contains completed model messages, usage, browser tool attempts, observations, execution steps, and terminal status;
3. failed browser assertion produces a failed Run with evidence even when assistant text claims success;
4. cancellation releases the harness process and authoring browser session;
5. two concurrent Runs have distinct harness attempt IDs, MCP processes, browser sessions, and artifact scopes;
6. rerun creates a new EvalHub Run and fresh harness attempt;
7. no Pi session file or harness-local database is needed after restart to reconstruct history.

- [ ] **Step 2: Run focused integration tests and verify RED**

Run:

```bash
python3 -m unittest integration.v3.test_pi_harness_scenario
```

Expected: FAIL because Compose still runs the Go model loop and the scenario does not exist.

- [ ] **Step 3: Cut over Compose**

Use a multi-stage harness image or runtime image that builds `pi-browser-harness` with `npm ci` and runs the Go consumer with Node available. Remove `AGENTTRACE_MODEL_SCRIPT` as the default production path. Add server-owned provider base URL, model key, harness entry, and worker URL variables. Do not add a network AgentServer service.

- [ ] **Step 4: Update profile and documentation**

The browser agent profile retains model defaults, reasoning effort, and safety policy, but no longer advertises `browser_execute_case` as the model's only operation. Add `reasoning_effort` to the immutable `event.ModelRef` so a restarted attempt can reconstruct the Pi request from PostgreSQL alone. Remove the obsolete operation callback from new Run commands. Delete the worker's `/operations/browser_execute_case` wrapper and its process-local idempotency cache only after `git grep` proves no caller remains; retain `/sessions`, `/navigate`, `/act`, `/execute`, and close unchanged. Document Pi/MCP/browser tools as the production path and remove claims that the model writes complete case JSON.

- [ ] **Step 5: Run complete verification**

Run:

```bash
cd pi-browser-harness && npm ci --cache /tmp/agenttrace-npm-cache && npm test -- --run && npm run build
cd ../backend && go test -p 1 ./...
cd ../worker && uv run python -m unittest discover -s tests -t .
cd ../web && npm ci --cache /tmp/agenttrace-npm-cache && npm test -- --run && npm run build
cd .. && docker compose config --quiet
python3 run_v3_tests.py
python3 integration/v3/failure_drills.py
```

Expected: all commands exit zero; the canonical and concurrency reports identify Pi harness attempts and no AgentServer.

- [ ] **Step 6: Scan for obsolete production wiring**

Run:

```bash
git grep -nE 'AgentServer|agentserver|browser_execute_case|runtime\\.Engine|NewOpenAI' -- ':!docs/superpowers/**' ':!pi-browser-harness/test/**'
```

Expected: no AgentServer or `runtime.Engine` production references. Any retained `browser_execute_case` references are restricted to historical compatibility tests and worker operation metadata, not model-facing production wiring.

- [ ] **Step 7: Commit**

```bash
git add .
git commit -m "feat: complete Pi browser harness migration"
```
