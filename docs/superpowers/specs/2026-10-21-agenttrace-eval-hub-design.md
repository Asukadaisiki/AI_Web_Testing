# AgentTrace Eval Hub v3 Design

**Date:** 2026-10-21

**Status:** Ready for review

## 1. Purpose

AgentTrace Eval Hub is a local-first agent evaluation workspace. A user selects a
project and conversation, sends an evaluation goal to a selected agent/model, watches
the assistant response and execution Trace in real time, and inspects the durable
result after refresh or restart.

The first complete scenario is browser evaluation:

> A QA engineer asks the Browser Eval Agent to log in to a fixture e-commerce site,
> add a named product with a specified quantity, and verify the final cart state.

The agent plans with native function calling, invokes the existing Playwright worker
through typed operations, records every model/tool/operation step as a Trace, and
returns the case, execution evidence, report, and failure feedback inside one
conversation. Approval and follow-up questions appear inline; the user never leaves
the three-column workspace.

The design must remain explainable under questioning:

- PostgreSQL preserves what happened.
- Kafka ensures accepted work is eventually delivered.
- Redis makes active work feel live.
- None of the three is included only to make the stack look larger.

## 2. Product Surface

The UI is one fixed three-column workspace with no route-level workflow:

1. **Left: project and conversation navigation**
   - Projects, conversation search, new conversation, latest run status.
   - Selecting an item changes the current workspace state without navigating to a
     separate report or execution page.
2. **Center: conversation**
   - User and assistant messages across multiple turns.
   - Inline approval, question, error, report summary, and retry blocks.
   - Bottom composer with Agent and Model selectors.
3. **Right: current-turn Trace**
   - A tree for the selected assistant run.
   - Model, reasoning, tool, operation, usage, and error nodes.
   - Selecting a node shows timestamps, input/output summary, attempts, evidence, and
     error details in the same panel.

The URL may preserve selected IDs as query parameters for refresh and deep linking,
but selection must not cause a page transition.

## 3. Scope

### 3.1 Included in v3 phase 1

- Projects, conversations, messages, runs, spans, operation results, usage, and
  evidence metadata.
- A generic `sendmsg` contract with a model and typed operation definitions.
- A Go ReAct/function-calling runtime.
- The existing Python Playwright worker as the first operation executor.
- PostgreSQL persistence, Kafka KRaft delivery, Redis live streams/leases/cancel hints.
- SSE from the API to the browser, backed by Redis with PostgreSQL recovery.
- Asynchronous Langfuse export as an optional observability mirror.
- Offline scripted-model end-to-end tests and one real Playwright fixture scenario.
- Dependency health and repeatable failure drills.

### 3.2 Explicitly excluded

- A service registry or dynamic plugin marketplace.
- Kafka and RocketMQ at the same time. Phase 1 implements Kafka behind an `EventBus`
  interface; RocketMQ can be a later adapter.
- Redis as a database, durable queue, or source of truth.
- Langfuse as the product UI or source of truth.
- Arbitrary remote code execution and a multi-tenant sandbox platform.
- Authentication, organization billing, public deployment, and horizontal
  autoscaling.
- Exactly-once external model/provider billing. The system guarantees idempotent
  persisted effects; an interrupted provider request may still be billed twice.

## 4. Architecture Decision

### 4.1 Selected approach

Use a modular Go control plane deployed as two processes:

- `agenttrace-api`: HTTP API, SSE gateway, dependency health, static artifact serving,
  and the PostgreSQL outbox relay.
- `agenttrace-runtime`: Kafka command consumer, ReAct loop, model adapter, operation
  dispatcher, durable Trace writer, and run-event outbox producer.

Keep the Python worker as an HTTP operation executor. Keep the React frontend as one
application shell.

This is a modular monolith at the code level, not a collection of independent
microservices. API and Runtime share contracts and repositories, but separate process
lifecycles make queue delivery and restart behavior real and testable.

### 4.2 Rejected alternatives

**Single Go process with goroutines only**

This is simpler, but Kafka would not solve a real boundary. A process crash would stop
both acceptance and execution, and queue recovery would be difficult to demonstrate.

**Independent microservice per capability**

Separate API, runtime, trace, usage, operation, and exporter services would add network
contracts and deployment failure modes before the product needs independent scaling.

**Langfuse-centered UI**

Langfuse is useful for asynchronous observability, but it does not own AgentTrace
projects, conversations, inline approvals, or the required three-column interaction.

## 5. Real Scenario And End-To-End Flow

The canonical acceptance scenario is:

1. The user opens project `Storefront QA`, creates a conversation, selects
   `Browser Eval Agent` and a scripted model, then sends:
   `Log in as qa@example.test, add 2 Alpha Backpacks, and verify cart quantity and user name.`
2. `agenttrace-api` validates the request and, in one PostgreSQL transaction, inserts:
   - the user message;
   - a queued run with an immutable run specification;
   - an outbox event `run.requested`.
3. The API returns `202 Accepted` with message and run IDs. It does not wait for Kafka,
   the model, or Playwright.
4. The outbox relay publishes `run.requested` to
   `agenttrace.run.commands.v1`, records the Kafka metadata, and marks the outbox row
   published.
5. `agenttrace-runtime` consumes the command. It claims the run with a PostgreSQL
   compare-and-set and records `run.started`. Duplicate delivery sees the existing
   claim/checkpoint and does not create a second run or assistant message.
6. Each ReAct iteration:
   - persists a model span attempt;
   - calls the selected model;
   - persists usage and the completed model output;
   - validates any function call against the operation JSON Schema;
   - invokes the operation callback with an `Idempotency-Key`;
   - persists the operation result/evidence;
   - appends the next runtime checkpoint.
7. After every durable state change, the runtime publishes a compact live event to
   `run:{run_id}:live` in Redis. Token deltas may be Redis-only; completed message and
   Trace node content always go to PostgreSQL.
8. The API's SSE endpoint reads the Redis stream from the browser's last event ID.
   When Redis is unavailable or the stream has expired, it reads the PostgreSQL Trace
   projection and continues with durable state.
9. The final assistant message, run status, usage, and root span are committed in one
   PostgreSQL transaction. A `run.completed` outbox event is then relayed to
   `agenttrace.run.events.v1`.
10. An optional Langfuse exporter consumes run events, loads canonical data from
    PostgreSQL, and mirrors the Trace. Export failure never changes the local run.

The user sees one assistant turn and a Trace shaped like:

```text
run: Evaluate storefront checkout
├── model: Plan next action
├── tool: Build grounded browser case
├── operation: Execute case in fresh browser context
│   ├── navigate and sign in
│   ├── add Alpha Backpack
│   └── verify user name and cart quantity
├── report: Derive pass/fail evidence
└── model: Write final evaluation summary
```

The operation runs against a disposable fixture account in a fresh browser context.
This matters for retry semantics: persisted AgentTrace effects are idempotent, while a
remote system side effect is only safe to retry when the operation contract or target
environment makes it idempotent. Phase 1 does not use the browser executor for
irreversible production actions.

## 6. Why Each Infrastructure Component Exists

### 6.1 PostgreSQL: durable fact ledger

PostgreSQL is the only authoritative store for:

- projects, conversations, and ordered messages;
- immutable run specifications and run state;
- spans, span attempts, operation results, usage, and artifact metadata;
- approval/question responses;
- runtime checkpoints;
- idempotency records;
- outbox and consumer inbox records.

If PostgreSQL is unavailable, the API must reject new messages with `503`; accepting
work without a durable record would create a task the user cannot recover or audit.

### 6.2 Kafka: durable asynchronous delivery

Kafka carries work and integration notifications across process lifecycles:

- `agenttrace.run.commands.v1`: accepted run commands.
- `agenttrace.run.events.v1`: durable run/span lifecycle notifications for exporters
  and future evaluators.
- `agenttrace.run.dlq.v1`: poison commands after bounded retry.

The database transaction never includes Kafka. A PostgreSQL outbox bridges the two:
the user message, run, and outbox record commit together; a relay retries publication
until Kafka accepts it. Relay workers claim rows with a time-bounded database lease,
publish outside the claim transaction, and then record broker metadata; an expired
lease makes an interrupted row eligible again. Consumers are at-least-once and
deduplicate by `event_id` plus the PostgreSQL run/checkpoint state.

If Kafka is unavailable, accepted runs remain visibly `queued` with pending outbox
rows. When Kafka returns, they execute without user resubmission.

### 6.3 Redis: disposable live coordination

Redis contains only reconstructible or advisory state:

- `run:{id}:live`: capped Redis Stream for token deltas and Trace updates, with TTL.
- `run:{id}:lease`: runtime heartbeat/owner hint with TTL.
- `run:{id}:cancel`: fast cancellation hint with TTL.

Redis does not store secrets, canonical messages, final Trace nodes, or queue state.
The durable cancel request is also stored in PostgreSQL; Redis only reduces reaction
latency.

If Redis is unavailable, execution continues. SSE degrades to PostgreSQL polling,
token-by-token output may disappear, and the UI shows `live updates degraded`.

## 7. Contracts

### 7.1 Send message

`POST /api/v1/conversations/{conversation_id}/messages`

```json
{
  "content": "Log in and verify the cart",
  "agent_id": "browser-eval",
  "model": {
    "provider": "scripted",
    "name": "browser-eval-fixture"
  },
  "idempotency_key": "client-generated-uuid"
}
```

The API resolves `agent_id` to a versioned agent profile. The immutable run
specification sent through Kafka contains:

```json
{
  "schema_version": "agenttrace.run-command.v1",
  "event_id": "evt_...",
  "run_id": "run_...",
  "conversation_id": "conv_...",
  "model": {"provider": "scripted", "name": "browser-eval-fixture"},
  "messages": [{"role": "user", "content": "Log in and verify the cart"}],
  "operations": [
    {
      "name": "browser_execute_case",
      "description": "Execute a grounded browser evaluation case",
      "input_schema": {"type": "object"},
      "callback": {"kind": "http", "url": "http://worker:8100/operations/browser_execute_case"}
    }
  ]
}
```

Callback URLs and credentials are server-owned configuration. The browser never
supplies arbitrary callback URLs.

### 7.2 Event envelope

Every Kafka and durable domain event uses:

```json
{
  "schema_version": "agenttrace.event.v1",
  "event_id": "evt_...",
  "event_type": "run.requested",
  "aggregate_id": "run_...",
  "occurred_at": "2026-10-21T12:00:00Z",
  "correlation_id": "run_...",
  "causation_id": "msg_...",
  "payload": {}
}
```

The partition key is `run_id`, preserving order within one run. Schema versions are
explicit; unknown major versions go to the DLQ rather than being guessed.

### 7.3 SSE

`GET /api/v1/runs/{run_id}/events`

- Uses SSE `id` only for durable events, as a monotonic per-run PostgreSQL sequence.
- Token deltas are explicitly ephemeral SSE events with no `id`; reconnect restores
  completed message/span content, not every historical token chunk.
- Accepts `Last-Event-ID`.
- Replays durable PostgreSQL events first when necessary.
- Switches to Redis live events when available.
- Sends heartbeat comments every 15 seconds.
- Never treats a missing Redis stream as a missing run.

### 7.4 Operation callback

The runtime sends:

- `Idempotency-Key: {operation_id}`
- `X-AgentTrace-Run-ID`
- `X-AgentTrace-Span-ID`

The executor returns a typed result with status, output, evidence, and error. The
existing worker's browser actions, locators, conditions, evidence collection, and case
execution remain the implementation behind the first operation adapter.

## 8. Data Model

The initial PostgreSQL migration defines:

| Table | Purpose |
|---|---|
| `projects` | Top-level workspace |
| `conversations` | Ordered multi-turn thread inside a project |
| `messages` | User/assistant/system content; assistant message is unique per run |
| `runs` | Immutable spec, lifecycle, cancel state, checkpoint version |
| `spans` | Trace tree nodes with parent, kind, status, timing, input/output |
| `span_attempts` | Retries and provider/operation attempt evidence |
| `usage_records` | Per-attempt token/cost accounting |
| `operation_results` | Idempotent typed operation response and artifacts |
| `artifacts` | Metadata and relative file path; file bytes stay on the artifact volume |
| `run_events` | Monotonic durable events used for replay/projection |
| `idempotency_keys` | API request deduplication and stored response identity |
| `outbox_events` | Transactional events waiting for Kafka |
| `inbox_events` | Consumer event deduplication |

Important constraints:

- `(conversation_id, ordinal)` is unique for messages.
- `messages.run_id` is unique for the assistant message of a run.
- `(run_id, seq)` is unique for durable run events.
- `(run_id, trace_id, span_id)` is unique for spans.
- `operation_results.operation_id` is unique.
- `idempotency_keys(scope, key)` is unique.
- `outbox_events.event_id` and `inbox_events(consumer, event_id)` are unique.
- All JSON payloads use `jsonb`; frequently queried lifecycle fields remain columns.

The existing SQLite database is not migrated in place. v3 starts with a new
PostgreSQL database; v2 remains a reference and regression source.

## 9. Runtime Semantics

### 9.1 ReAct loop

The runtime is a state machine over durable checkpoints:

`queued -> running -> waiting_input|waiting_approval -> running -> succeeded|failed|cancelled`

Each completed model or operation step commits its span, attempt, usage, run event, and
next checkpoint transactionally. A process restart resumes from the last checkpoint.

### 9.2 Idempotency and retries

- API message creation is idempotent by client key.
- Kafka delivery is at-least-once.
- The consumer inserts `inbox_events` and claims/checks the run transactionally.
- Completed span IDs are deterministic from run/checkpoint position.
- Operation calls use stable operation IDs and executor-side deduplication.
- Model attempts have stable logical span IDs and distinct attempt IDs.
- A crash after a provider response but before persistence can repeat a model call.
  Both attempts are recorded when observable; the design does not claim exactly-once
  provider billing.
- A crash after a remote operation side effect but before result persistence can also
  repeat that operation unless the executor durably honors its idempotency key.
  AgentTrace deduplicates its own stored result, but cannot manufacture exactly-once
  semantics for an arbitrary callback. The first browser scenario uses a fresh,
  disposable context and retry-safe fixture data.

### 9.3 Cancellation

The API transaction sets `runs.cancel_requested_at` and appends a durable event. It
also writes the Redis cancel hint. Runtime checks cancellation before model calls,
before operations, and while waiting/retrying. If Redis is down, PostgreSQL polling
still cancels at the next checkpoint boundary.

### 9.4 Backpressure

- Runtime has a configured maximum number of concurrent runs.
- Kafka consumer pauses fetching when that limit is reached.
- Per-run model calls, fresh tokens, prompt tokens, request bytes, operation attempts,
  and wall-clock duration retain explicit limits.
- SSE clients have bounded buffers; slow clients reconnect and replay by event ID.

## 10. Failure And Degradation Matrix

| Failure | Durable behavior | User-visible behavior | Recovery proof |
|---|---|---|---|
| PostgreSQL stopped | New send rejected; consumer does not acknowledge work it cannot commit | Composer reports unavailable; health says `postgres=down` | Restart PG; no phantom message/run exists |
| Kafka stopped before send | Message, run, outbox commit | Run remains `queued`; health says `kafka=down` | Restart Kafka; relay publishes and run completes |
| Runtime killed after consume | Kafka command remains/reappears; checkpoint is authoritative | Trace pauses, then resumes | Restart Runtime; no duplicate assistant message or completed span |
| Redis stopped | PG/Kafka path continues | Coarser polling updates; no token stream; degraded badge | Restart Redis; new live events resume, refresh matches PG |
| Redis flushed | No canonical data lost | Active stream reconnects; historical Trace still visible | Reload run from PostgreSQL |
| Worker stopped | Operation attempt fails/retries according to policy | Failed operation node shows cause | Restart worker and retry run/operation |
| Langfuse stopped | Local run unaffected; exporter retries | No product impact; exporter health degraded | Restart exporter; event replay mirrors Trace |
| Duplicate Kafka event | Inbox/run checkpoint rejects duplicate persisted effect | No visible duplication | Assert unique message/span/result rows |

## 11. Health Contract

`GET /api/v1/health` returns HTTP `200` only when the API can serve reads and accept
durable writes. It reports dependencies separately:

```json
{
  "status": "degraded",
  "dependencies": {
    "postgres": {"status": "ok", "required": true},
    "kafka": {"status": "down", "required": false, "pending_outbox": 3},
    "redis": {"status": "down", "required": false, "mode": "postgres_poll"},
    "worker": {"status": "ok", "required": false},
    "langfuse": {"status": "disabled", "required": false}
  }
}
```

PostgreSQL failure returns `503`. Kafka or Redis failure returns `200` with
`status=degraded` because accepted data remains safe or execution can continue.

## 12. Existing Code Disposition

### 12.1 Reuse directly or behind an adapter

- `backend/internal/contract`: browser case and execution contracts.
- `backend/internal/planner`: grounded planning tools and replay rules.
- `backend/internal/report`: report derivation.
- `backend/internal/feedback`: failure feedback derivation.
- `backend/internal/usage`: usage normalization.
- `backend/internal/agentruntime/llm_openai.go` and scripted LLM behavior, adapted to
  the new model interface.
- `worker/loop_worker/actions.py`, `conditions.py`, `observer.py`, `locators.py`,
  `runner.py`, `evidence.py`, and their tests.
- Contract and browser fixture files.

### 12.2 Modify substantially

- `backend/internal/agentruntime`: separate pure ReAct/checkpoint logic from SQLite
  store and direct worker client dependencies.
- `backend/internal/api`: replace the four-page run API with project/conversation/
  message/run/trace endpoints and SSE recovery.
- `backend/cmd/loopd`: split into API and Runtime commands.
- `web/src/App.tsx`, `api.ts`, and styles: replace routed pages with one workspace.
- `worker/loop_worker/main.py`: add the generic idempotent operation endpoint while
  keeping the existing low-level endpoints during migration.
- `README.md`, configuration, and test runner.

### 12.3 Add

- PostgreSQL migrations and repository packages.
- Kafka event bus, outbox relay, consumer inbox, and DLQ handling.
- Redis live stream/lease/cancel adapter.
- Runtime command and event contracts.
- Trace projection and SSE gateway.
- Agent profile configuration and operation dispatcher.
- Docker Compose, service health checks, and failure-drill scripts.
- Optional Langfuse exporter.
- New three-column React components and browser end-to-end tests.

### 12.4 Retire from the v3 runtime path

- SQLite and `modernc.org/sqlite`.
- In-process `store.Broker`.
- Restart behavior that marks all in-flight runs failed.
- Separate execution/report/injection routes and pages.
- Direct `go runtime -> worker` orchestration from the HTTP request process.

Retired code is removed only after equivalent v3 tests pass. It remains available in
the v2 branch history.

## 13. Test Strategy

### 13.1 Unit and contract tests

- PostgreSQL repositories and state transition compare-and-set behavior.
- Event envelope, run command, operation schema, and SSE event serialization.
- Outbox retry and inbox deduplication.
- ReAct checkpoint resume and deterministic span IDs.
- Redis stream replay/fallback behavior.
- Worker idempotency and existing contract conformance.
- React reducers and Trace tree projection.

### 13.2 Integration tests

- Compose-backed PostgreSQL, Kafka, and Redis tests.
- API transaction creates message + run + outbox atomically.
- Relay publishes and Runtime completes an offline scripted-model run.
- Duplicate command does not duplicate persisted effects.
- SSE resumes from `Last-Event-ID`.
- Existing Playwright fixture completes through the operation callback.

### 13.3 Failure drills

Automated scripts must:

1. stop Kafka, send a message, assert queued/outbox state, restart Kafka, and assert
   completion;
2. stop Redis during a run, assert completion and PostgreSQL Trace recovery;
3. flush Redis and assert historical Trace is unchanged;
4. kill Runtime after command receipt, restart it, and assert idempotent completion;
5. stop PostgreSQL and assert the API refuses new work;
6. stop Langfuse/mock exporter and assert local completion.

### 13.4 UI verification

Playwright verifies desktop and narrow viewport layouts:

- all three columns are present on desktop;
- narrow mode uses resizable/collapsible panes without route navigation;
- text, controls, Trace nodes, and composer never overlap;
- selecting a Trace node updates details in place;
- approval, question, failure, and completed states are usable;
- refresh restores the selected conversation/run from PostgreSQL;
- screenshots are captured for the canonical scenario.

## 14. Definition Of Done

Phase 1 is done only when:

- `docker compose up -d` starts PostgreSQL, Redis, Kafka, API, Runtime, Worker, and Web;
- one command runs all unit, integration, worker, frontend, and failure-drill tests;
- the canonical browser evaluation completes through the three-column UI;
- PostgreSQL contains the full conversation and Trace;
- Kafka outage demonstrates delayed delivery, not loss;
- Redis outage demonstrates degraded streaming, not lost facts;
- Runtime restart demonstrates redelivery without duplicate persisted effects;
- health exposes each dependency separately;
- optional Langfuse export can fail without affecting the product;
- README contains exact startup, test, and failure-drill commands.
