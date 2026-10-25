# Agent Observability and Failed-Run Rerun Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add immutable failed-Run reruns, timestamped per-message attempt history, and observable tool-decision context while preserving the existing concurrent runtime.

**Architecture:** PostgreSQL stores explicit Run lineage and creates each rerun plus its outbox command atomically. The conversation API returns messages and public Runs as one read model, while the Trace API derives a bounded decision-context projection from already persisted model-span JSON. React groups Runs under their source user message and selects one attempt for SSE and Trace inspection.

**Tech Stack:** Go 1.24, PostgreSQL 16/pgx, Kafka outbox/inbox, React 19, TypeScript, Vitest/Testing Library, Python 3 integration scenarios, Docker Compose.

**Spec:** `docs/superpowers/specs/2026-10-25-agent-observability-rerun-design.md`

## Global Constraints

- Do not add evaluation datasets, scoring, experiment management, prompt optimization, or RSI automation.
- A rerun is a new Run; never reset, resume, or mutate the failed Run's evidence.
- Only durable status `failed` is eligible for `POST /api/v1/runs/{id}/reruns`.
- Reruns copy the immutable specification, replace the Run ID, and reset checkpoint version to zero.
- Reruns insert no new user message.
- Keep Langfuse optional and outside the request/runtime critical path.
- Decision context must not claim hidden chain-of-thought and must not project `reasoning_content`.
- Preserve `AGENTTRACE_RUNTIME_CONCURRENCY`, the bounded runner, eight local Kafka partitions, and PostgreSQL pool sizing.
- Use existing recursive public Trace redaction before deriving decision context.
- Keep full verification deferred until implementation is complete; use focused tests during each task.

## File Structure

- `backend/internal/postgres/migrations/004_run_lineage.sql`: lineage columns, indexes, and existing-row backfill.
- `backend/internal/domain/models.go`: durable Run lineage fields.
- `backend/internal/postgres/store.go`: canonical `scanRun` lineage scan.
- `backend/internal/postgres/runs.go`: original Run lineage writes and transactional rerun creation.
- `backend/internal/postgres/api.go`: conversation read model with attributable messages and ordered Runs.
- `backend/internal/api/v3/server.go`: repository interface and rerun route.
- `backend/internal/api/v3/runs.go`: public Run conversion and rerun handler.
- `backend/internal/api/v3/conversations.go`: conversation response including Runs.
- `backend/internal/api/v3/decision_context.go`: fail-soft model-span decision projection.
- `web/src/api.ts`: lineage, conversation Runs, decision context, and rerun client contracts.
- `web/src/features/workspace/useWorkspace.ts`: Run selection from conversation Runs and rerun action.
- `web/src/features/conversation/ConversationPane.tsx`: task timeline composition.
- `web/src/features/conversation/MessageBlock.tsx`: second-level timestamps, attempt controls, and failed rerun command.
- `web/src/features/trace/TraceDetails.tsx`: decision-context panel for model spans.
- `web/src/styles.css`: compact timeline, attempt selector, and decision-context styling.
- `integration/v3/concurrency_rerun_scenario.py`: focused deterministic concurrency/rerun acceptance.
- `docs/architecture.md`, `docs/operations.md`: lineage, rerun, and concurrency operations.

---

### Task 1: Persist Run Lineage and Return Complete Conversation History

**Files:**
- Create: `backend/internal/postgres/migrations/004_run_lineage.sql`
- Modify: `backend/internal/domain/models.go:20-35`
- Modify: `backend/internal/postgres/store.go:78-100`
- Modify: every `scanRun(...)` query in `backend/internal/postgres/*.go`
- Modify: `backend/internal/postgres/runs.go:117-182`
- Modify: `backend/internal/postgres/api.go:180-225`
- Test: `backend/internal/postgres/api_integration_test.go`
- Test: `backend/internal/postgres/store_integration_test.go`

**Interfaces:**
- Produces: `domain.Run.SourceMessageID string`
- Produces: `domain.Run.RetryOfRunID string`
- Produces: `Store.GetConversation(context.Context, string) (Conversation, []domain.Message, []domain.Run, error)`
- Consumes: existing `messages.run_id`, `messages.metadata.run_id`, and original Run creation transaction.

- [ ] **Step 1: Write failing lineage and conversation-read tests**

Add integration tests that create an original message/Run, complete it with an assistant message, and assert:

```go
conversation, messages, runs, err := store.GetConversation(ctx, conversationID)
if err != nil {
    t.Fatalf("get conversation: %v", err)
}
if conversation.ID != conversationID || len(runs) != 1 {
    t.Fatalf("conversation=%#v runs=%#v", conversation, runs)
}
if runs[0].SourceMessageID != userMessage.ID || runs[0].RetryOfRunID != "" {
    t.Fatalf("run lineage = %#v", runs[0])
}
if got := messages[len(messages)-1].RunID; got != run.ID {
    t.Fatalf("assistant run_id = %q, want %q", got, run.ID)
}
```

Add a migration regression in the `postgres` package that resets the isolated schema,
calls `applyMigration` for `001_initial.sql` through `003_consumer_retry_counts.sql`,
seeds a Run and linked user message, applies `004_run_lineage.sql`, and asserts
`source_message_id` is backfilled.

- [ ] **Step 2: Run focused PostgreSQL tests and confirm the compile/schema failure**

Run:

```bash
docker compose exec -T api go test ./internal/postgres -run 'Test(CreateMessageSetsRunLineage|GetConversationReturnsRunsAndAttributableMessages|RunLineageMigrationBackfillsExistingRows)$' -count=1
```

Expected: FAIL because `domain.Run` has no lineage fields, `GetConversation` returns three values, and migration 004 does not exist.

- [ ] **Step 3: Add migration 004**

Create:

```sql
ALTER TABLE runs
  ADD COLUMN source_message_id TEXT REFERENCES messages(id) ON DELETE SET NULL,
  ADD COLUMN retry_of_run_id TEXT REFERENCES runs(id) ON DELETE SET NULL;

UPDATE runs AS run
SET source_message_id = message.id
FROM messages AS message
WHERE message.run_id = run.id
  AND message.role = 'user'
  AND run.source_message_id IS NULL;

CREATE INDEX idx_runs_conversation_created
  ON runs (conversation_id, created_at, id);

CREATE INDEX idx_runs_source_message
  ON runs (source_message_id, created_at, id);
```

- [ ] **Step 4: Extend the Run model and every scan query consistently**

Add to `domain.Run`:

```go
SourceMessageID string `json:"source_message_id,omitempty"`
RetryOfRunID    string `json:"retry_of_run_id,omitempty"`
```

Insert `COALESCE(source_message_id, '')` and `COALESCE(retry_of_run_id, '')` after `conversation_id` in every Run `SELECT`/`RETURNING`, and scan those values in the same order in `scanRun`.

Use:

```bash
grep -RIn "SELECT id, project_id, conversation_id\\|RETURNING id, project_id, conversation_id" backend/internal/postgres
```

to prove no `scanRun` query was missed.

- [ ] **Step 5: Write original Run lineage in the creation transaction**

Change the Run insert to:

```sql
INSERT INTO runs (
  id, project_id, conversation_id, source_message_id, status, specification
)
VALUES ($1, $2, $3, $4, 'queued', $5::jsonb)
```

Pass `message.ID` as `source_message_id`; keep `retry_of_run_id` null.

- [ ] **Step 6: Return messages and Runs from one conversation read**

Change `GetConversation` to return four values. Project message Run IDs with:

```sql
COALESCE(run_id, metadata ->> 'run_id', '')
```

Query Runs separately:

```sql
SELECT id, project_id, conversation_id,
       COALESCE(source_message_id, ''), COALESCE(retry_of_run_id, ''),
       status, specification, checkpoint_version,
       cancel_requested_at, started_at, finished_at, created_at, updated_at
FROM runs
WHERE conversation_id = $1
ORDER BY created_at, id
```

Always return empty non-nil slices from the API layer; repository slices may remain nil.

- [ ] **Step 7: Run focused tests and all PostgreSQL package tests**

Run:

```bash
docker compose exec -T api go test ./internal/postgres -run 'Test(CreateMessageSetsRunLineage|GetConversationReturnsRunsAndAttributableMessages|RunLineageMigrationBackfillsExistingRows)$' -count=1
docker compose exec -T api go test ./internal/postgres -count=1
```

Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add backend/internal/postgres/migrations/004_run_lineage.sql backend/internal/domain/models.go backend/internal/postgres
git commit -m "feat: persist run lineage"
```

### Task 2: Create Failed Reruns Atomically

**Files:**
- Modify: `backend/internal/postgres/runs.go`
- Test: `backend/internal/postgres/store_integration_test.go`
- Test: `backend/internal/postgres/api_integration_test.go`
- Test: `backend/internal/postgres/deletion_integration_test.go`

**Interfaces:**
- Consumes: `domain.Run.SourceMessageID`, `domain.Run.RetryOfRunID`, `runSpecification`, `insertOutbox`.
- Produces: `postgres.CreateRerunParams{RunID, IdempotencyKey string}`
- Produces: `Store.CreateRerun(context.Context, CreateRerunParams) (domain.Run, error)`

- [ ] **Step 1: Write failing repository tests for the successful path**

Create a failed source Run and call:

```go
rerun, err := store.CreateRerun(ctx, CreateRerunParams{
    RunID:          source.ID,
    IdempotencyKey: "rerun-1",
})
```

Assert:

```go
if rerun.ID == source.ID || rerun.Status != domain.RunQueued {
    t.Fatalf("rerun = %#v", rerun)
}
if rerun.SourceMessageID != source.SourceMessageID ||
    rerun.RetryOfRunID != source.ID ||
    rerun.CheckpointVersion != 0 {
    t.Fatalf("rerun lineage/checkpoint = %#v", rerun)
}
```

Decode the new outbox envelope and verify the copied model/messages/operations, new `command.run_id`, checkpoint zero, and `causation_id == source.ID`. Assert message count is unchanged.

- [ ] **Step 2: Write failing eligibility, idempotency, and concurrency tests**

Add table-driven tests over every non-failed `domain.RunStatus`, expecting `ErrConflict`.

Call the same failed source/key twice and expect the same new Run ID and one new outbox row.

Start two goroutines with the same source/key, release them together, and assert both receive one shared rerun identity.

Mark the first rerun failed and rerun it with a new key. Assert the second rerun keeps
the original `source_message_id` and sets `retry_of_run_id` to the immediate failed
rerun, not directly to the root.

Extend deletion integration coverage: after original and rerun Runs are terminal and
delivery-safe, deleting their conversation/project removes all lineage rows without
a foreign-key failure.

- [ ] **Step 3: Run the rerun tests and confirm `CreateRerun` is missing**

Run:

```bash
docker compose exec -T api go test ./internal/postgres -run 'TestCreateRerun' -count=1
```

Expected: FAIL because the params and method are undefined.

- [ ] **Step 4: Implement request validation and idempotency replay**

Add:

```go
type CreateRerunParams struct {
    RunID          string
    IdempotencyKey string
}

type createRerunResponse struct {
    RunID string `json:"run_id"`
}
```

Reject blank values. Hash `{run_id: sourceID}` and use scope:

```go
scope := "run-rerun:" + p.RunID
```

On idempotency conflict, lock the idempotency row, verify the stored hash, read the referenced Run with all lineage fields, commit, and return it.

- [ ] **Step 5: Implement the transactional fresh-Run creation**

Inside one transaction:

1. Select the source Run `FOR UPDATE`.
2. Require `status == domain.RunFailed` and non-empty `SourceMessageID`.
3. Decode `runSpecification`.
4. Allocate new Run/event/idempotency IDs.
5. Set:

```go
specification.Command.RunID = rerunID
specification.Command.CheckpointVersion = 0
```

6. Marshal the updated specification.
7. Insert the queued Run with inherited project/conversation/source message and `retry_of_run_id = source.ID`.
8. Build a new command envelope:

```go
event.Envelope[event.RunCommand]{
    SchemaVersion: "agenttrace.run-command.v1",
    EventID: eventID,
    EventType: "run.requested",
    AggregateID: rerun.ID,
    OccurredAt: rerun.CreatedAt,
    CorrelationID: rerun.ID,
    CausationID: source.ID,
    Payload: specification.Command,
}
```

9. Insert the outbox record and commit.

Lock the source before inserting the idempotency key so concurrent requests serialize on one durable source state.

- [ ] **Step 6: Run rerun and full PostgreSQL tests**

Run:

```bash
docker compose exec -T api go test ./internal/postgres -run 'TestCreateRerun' -count=1
docker compose exec -T api go test ./internal/postgres -count=1
```

Expected: PASS with one rerun under concurrent duplicate requests.

- [ ] **Step 7: Commit**

```bash
git add backend/internal/postgres/runs.go backend/internal/postgres/*_test.go
git commit -m "feat: create immutable failed-run reruns"
```

### Task 3: Expose Run Lineage, Conversation Runs, and the Rerun Endpoint

**Files:**
- Modify: `backend/internal/api/v3/server.go:28-43,110-125`
- Modify: `backend/internal/api/v3/runs.go:15-32,146-167`
- Modify: `backend/internal/api/v3/conversations.go:51-67`
- Modify: `backend/internal/api/v3/server_test.go`

**Interfaces:**
- Consumes: `Repository.GetConversation(...) (..., []domain.Run, error)`
- Consumes: `Repository.CreateRerun(context.Context, postgres.CreateRerunParams) (domain.Run, error)`
- Produces: `POST /api/v1/runs/{id}/reruns`
- Produces: public Run fields `source_message_id` and optional `retry_of_run_id`.

- [ ] **Step 1: Add failing API tests**

Test that `GET /api/v1/conversations/{id}` returns non-null `messages` and `runs` arrays and that both conversation Runs and `GET /runs/{id}` include lineage without `specification`.

Test:

```http
POST /api/v1/runs/run_failed/reruns
{"idempotency_key":"rerun-api-1"}
```

expects HTTP 202 and:

```json
{"run_id":"run_new","status":"queued","retry_of_run_id":"run_failed"}
```

Also test blank keys return 400, repository `ErrConflict` maps to 409, and `ErrNotFound` maps to 404.

- [ ] **Step 2: Run focused API tests and confirm interface/route failures**

Run:

```bash
docker compose exec -T api go test ./internal/api/v3 -run 'Test(GetConversationIncludesRuns|GetRunExposesLineageWithoutSpecification|Rerun)' -count=1
```

Expected: FAIL because the repository signature and route are absent.

- [ ] **Step 3: Centralize public Run conversion**

Extend `publicRun` and add:

```go
func toPublicRun(run domain.Run) publicRun {
    return publicRun{
        ID: run.ID,
        ProjectID: run.ProjectID,
        ConversationID: run.ConversationID,
        SourceMessageID: run.SourceMessageID,
        RetryOfRunID: run.RetryOfRunID,
        Status: run.Status,
        CheckpointVersion: run.CheckpointVersion,
        CancelRequestedAt: run.CancelRequestedAt,
        StartedAt: run.StartedAt,
        FinishedAt: run.FinishedAt,
        CreatedAt: run.CreatedAt,
        UpdatedAt: run.UpdatedAt,
    }
}
```

Use it in both Run and conversation handlers.

- [ ] **Step 4: Add repository methods and route**

Update the interface and fake repository, then register:

```go
mux.HandleFunc("POST /api/v1/runs/{id}/reruns", server.handleRerun)
```

Implement strict body decoding:

```go
type rerunRequest struct {
    IdempotencyKey string `json:"idempotency_key"`
}
```

Validate the key, call `CreateRerun`, and return the accepted shape.

- [ ] **Step 5: Return ordered public Runs from conversation reads**

Call the four-value repository method, normalize nil messages/Runs to empty slices, convert every Run with `toPublicRun`, and include:

```go
"runs": publicRuns,
```

- [ ] **Step 6: Run API and backend compile tests**

Run:

```bash
docker compose exec -T api go test ./internal/api/v3 -count=1
docker compose exec -T api go test ./... -run '^$'
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add backend/internal/api/v3
git commit -m "feat: expose failed-run reruns"
```

### Task 4: Project Observable Decision Context

**Files:**
- Create: `backend/internal/api/v3/decision_context.go`
- Create: `backend/internal/api/v3/decision_context_test.go`
- Modify: `backend/internal/api/v3/runs.go:73-90,178-196`
- Test: `backend/internal/api/v3/server_test.go`

**Interfaces:**
- Produces: `projectDecisionContext(input, output json.RawMessage) *publicDecisionContext`
- Produces: optional `decision_context` on public model spans.
- Consumes: already redacted span input/output JSON.

- [ ] **Step 1: Write table-driven projection tests**

Use model Request/Response-shaped JSON and assert:

```go
context := projectDecisionContext(
    json.RawMessage(`{"messages":[
      {"role":"user","content":"Check checkout"},
      {"role":"assistant","content":"calling tool"},
      {"role":"tool","content":"login succeeded"}
    ]}`),
    json.RawMessage(`{"message":{
      "content":"I will validate checkout.",
      "reasoning_content":"must not be projected",
      "tool_calls":[{"name":"browser_execute_case","arguments":{"case":"checkout"}}]
    }}`),
)
```

Expected: objective `Check checkout`, prior observation `login succeeded`, selected operation, visible explanation, and no serialized `reasoning_content`.

Cover no tool call, no visible explanation, 2,001-rune truncation, malformed input/output, and redacted secret-key absence.

- [ ] **Step 2: Run focused tests and confirm the projector is absent**

Run:

```bash
docker compose exec -T api go test ./internal/api/v3 -run 'TestProjectDecisionContext' -count=1
```

Expected: FAIL because the function and types do not exist.

- [ ] **Step 3: Implement focused projection types**

Define:

```go
type publicDecisionMessage struct {
    Role    string `json:"role"`
    Content string `json:"content"`
}

type publicSelectedOperation struct {
    Name      string          `json:"name"`
    Arguments json.RawMessage `json:"arguments"`
}

type publicDecisionContext struct {
    Objective          *publicDecisionMessage   `json:"objective,omitempty"`
    PriorObservation   *publicDecisionMessage   `json:"prior_observation,omitempty"`
    SelectedOperation  *publicSelectedOperation `json:"selected_operation,omitempty"`
    VisibleExplanation string                   `json:"visible_explanation,omitempty"`
}
```

Decode only the fields needed from Request and Response. Find the latest user message, then the latest tool message after it. Use the first tool call. Truncate content with `[]rune`, appending `...` within the 2,000-rune limit.

Return nil when no objective, observation, operation, or explanation can be proven.

- [ ] **Step 4: Attach the fail-soft projection after redaction**

In `handleGetTrace`:

```go
input := redactTraceJSON(span.Input)
output := redactTraceJSON(span.Output)
decision := (*publicDecisionContext)(nil)
if span.Kind == domain.SpanModel || span.Kind == domain.SpanReport {
    decision = projectDecisionContext(input, output)
}
```

Set `DecisionContext` on `publicTraceSpan`. Malformed JSON already becomes nil through redaction and must not fail the request.

- [ ] **Step 5: Run API tests**

Run:

```bash
docker compose exec -T api go test ./internal/api/v3 -run 'Test(ProjectDecisionContext|GetTrace)' -count=1
docker compose exec -T api go test ./internal/api/v3 -count=1
```

Expected: PASS, with existing canonical Trace fields unchanged.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/api/v3/decision_context.go backend/internal/api/v3/decision_context_test.go backend/internal/api/v3/runs.go backend/internal/api/v3/server_test.go
git commit -m "feat: project trace decision context"
```

### Task 5: Extend the Typed Client and Workspace Rerun State

**Files:**
- Modify: `web/src/api.ts`
- Modify: `web/src/api.test.ts`
- Modify: `web/src/features/workspace/useWorkspace.ts`
- Modify: `web/src/features/workspace/useWorkspace.test.ts`
- Modify: `web/src/features/workspace/workspace.test.ts`

**Interfaces:**
- Consumes: conversation `runs`, public Run lineage, span `decision_context`, and rerun endpoint.
- Produces: `rerunRun(runId, {idempotency_key})`
- Produces: `WorkspaceState.rerunFailed(): Promise<boolean>`
- Produces: `latestRunId(detail)` based only on `detail.runs`.

- [ ] **Step 1: Write failing API parser tests**

Mock a conversation response containing one message and two Runs; assert both lineage fields parse. Mock a model span with decision context and assert it reaches `Span.decision_context`.

Mock a rerun response and assert:

```ts
await expect(rerunRun('run-failed', {
  idempotency_key: 'rerun-1',
})).resolves.toEqual({
  run_id: 'run-new',
  status: 'queued',
  retry_of_run_id: 'run-failed',
});
```

- [ ] **Step 2: Run API tests and confirm missing contracts**

Run:

```bash
cd web && npm test -- --run src/api.test.ts
```

Expected: FAIL because `ConversationDetail.runs`, lineage, decision context, and `rerunRun` are absent.

- [ ] **Step 3: Extend strict TypeScript contracts and parsers**

Add:

```ts
export interface DecisionContext {
  objective?: { role: string; content: string };
  prior_observation?: { role: string; content: string };
  selected_operation?: { name: string; arguments: JSONValue };
  visible_explanation?: string;
}
```

Add `source_message_id`, optional `retry_of_run_id`, `ConversationDetail.runs`, optional `Span.decision_context`, `RerunRequest`, and `RerunResponse`. Parse every successful field strictly with existing helpers.

Implement:

```ts
export function rerunRun(
  runId: string,
  request: RerunRequest,
): Promise<RerunResponse> {
  return postJSON(`/runs/${encodeURIComponent(runId)}/reruns`, request, parseRerunResponse);
}
```

- [ ] **Step 4: Write failing workspace selection and rerun tests**

Update fixtures so every conversation has `runs`. Assert:

- restored rerun IDs remain selected after conversation refresh;
- a missing Run selection defaults to the newest `detail.runs` item;
- `rerunFailed()` calls `rerunRun` with a stable key;
- after acceptance it reloads the conversation and selects the returned Run;
- a rejected request retains the key for the next retry.

- [ ] **Step 5: Implement Run selection and rerun behavior**

Replace message-derived Run IDs with:

```ts
function latestRunId(detail: ConversationDetail): string | null {
  return detail.runs.at(-1)?.id ?? null;
}
```

Validate selected Runs against `detail.runs`.

Add:

```ts
const rerunKeys = useRef(new Map<string, string>());
```

`rerunFailed` obtains or creates one key per selected failed Run, calls `rerunRun`, refreshes the conversation, dispatches the new Run ID, and deletes the key only after HTTP 202. Return false when no selected failed Run is loaded.

- [ ] **Step 6: Run focused web tests**

Run:

```bash
cd web && npm test -- --run src/api.test.ts src/features/workspace/useWorkspace.test.ts src/features/workspace/workspace.test.ts
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add web/src/api.ts web/src/api.test.ts web/src/features/workspace
git commit -m "feat: add rerun workspace state"
```

### Task 6: Build the Timestamped Attempt Timeline and Decision Panel

**Files:**
- Create: `web/src/features/conversation/ConversationPane.test.tsx`
- Modify: `web/src/features/conversation/ConversationPane.tsx`
- Modify: `web/src/features/conversation/MessageBlock.tsx`
- Modify: `web/src/features/trace/TraceDetails.tsx`
- Modify: `web/src/features/trace/TraceDetails.test.tsx`
- Modify: `web/src/styles.css`
- Modify: `web/tests/workspace.spec.ts`

**Interfaces:**
- Consumes: `workspace.conversation.runs`, `workspace.rerunFailed`, selected Run state.
- Produces: inline `Attempt N` selectors grouped by `source_message_id`.
- Produces: local date separators and timestamps with seconds.
- Produces: `Decision context` details for model spans.

- [ ] **Step 1: Write failing conversation timeline component tests**

Render a conversation with:

- two user messages on different local dates;
- original and rerun Runs sharing one `source_message_id`;
- one assistant message attributed to the rerun;
- the rerun selected and failed.

Assert:

```ts
expect(screen.getByRole('button', { name: /Attempt 1/ })).toBeInTheDocument();
expect(screen.getByRole('button', { name: /Attempt 2/ })).toHaveAttribute('aria-pressed', 'true');
expect(screen.getByRole('button', { name: 'Rerun failed task' })).toBeInTheDocument();
expect(screen.getAllByText(/\d{2}:\d{2}:\d{2}/)).not.toHaveLength(0);
```

Assert global `Conversation runs` navigation is absent, only the selected task's `RunStateBlock` is rendered, and clicking an attempt calls `selectRun`.

- [ ] **Step 2: Write the failing decision-context UI test**

Create a model span with:

```ts
decision_context: {
  objective: { role: 'user', content: 'Check checkout' },
  prior_observation: { role: 'tool', content: 'Login passed' },
  selected_operation: { name: 'browser_execute_case', arguments: { case: 'checkout' } },
  visible_explanation: 'I will validate checkout.',
}
```

Assert `Decision context`, the objective, observation, operation, arguments, and visible explanation render before canonical input/output. A model span without context must omit the panel.

- [ ] **Step 3: Run focused component tests and confirm layout failures**

Run:

```bash
cd web && npm test -- --run src/features/conversation/ConversationPane.test.tsx src/features/trace/TraceDetails.test.tsx
```

Expected: FAIL because attempt grouping, rerun command, second-level time, and the decision panel are absent.

- [ ] **Step 4: Replace global Run tabs with per-message attempt groups**

Build maps with `useMemo`:

```ts
const runsBySource = new Map<string, Run[]>();
for (const run of detail.runs) {
  const runs = runsBySource.get(run.source_message_id) ?? [];
  runs.push(run);
  runsBySource.set(run.source_message_id, runs);
}
```

Treat only messages whose ID appears in `runsBySource` as task sources. A
`role=user` response to a waiting Run has no source Runs and must remain an
attributable response, not become a new task. Render attempt selectors immediately
after each task-source message, use Run creation order from the API, and associate
all other user/assistant response messages through their projected `message.run_id`.
Ensure each message renders exactly once.

Keep control dimensions stable and use a segmented-control treatment, not cards inside cards.

- [ ] **Step 5: Add explicit time formatting and date separators**

Export small pure helpers for testing:

```ts
export function messageDateKey(value: string): string
export function formatMessageDate(value: string): string
export function formatMessageTime(value: string): string
```

Use `toLocaleDateString` for separators and:

```ts
toLocaleTimeString([], {
  hour: '2-digit',
  minute: '2-digit',
  second: '2-digit',
})
```

Continue rendering in message ordinal order; never sort by timestamp.

- [ ] **Step 6: Add the failed rerun command**

Extend `RunStateBlock` with `onRerun: () => Promise<boolean>`. In the failed branch render a `RotateCcw` icon button with visible label `Rerun failed task`, disabled while busy, and call the workspace action. Do not render it for cancelled, succeeded, queued, or active Runs.

- [ ] **Step 7: Render structured decision context**

For canonical model/report spans with `span.decision_context`, add an open `<details>` section before Attempt:

```tsx
<details open>
  <summary><Route size={14} />Decision context</summary>
  ...
</details>
```

Use explicit labels `Objective`, `Prior observation`, `Selected operation`, and `Visible explanation`. Do not render or mention hidden reasoning.

- [ ] **Step 8: Style and verify responsive constraints**

Remove obsolete `.run-tabs` styles. Add stable segmented attempt controls, date separators, and rerun button spacing. Ensure:

- labels wrap instead of overflowing;
- icon/button dimensions do not shift with status text;
- no nested cards;
- desktop three-column layout and narrow pane switching remain intact.

- [ ] **Step 9: Run focused tests, full web unit tests, and build**

Run:

```bash
cd web && npm test -- --run src/features/conversation/ConversationPane.test.tsx src/features/trace/TraceDetails.test.tsx
cd web && npm test -- --run
cd web && npm run build
```

Expected: PASS.

- [ ] **Step 10: Update and run the focused Playwright workspace assertion**

Update `web/tests/workspace.spec.ts` to verify the attempt selector, second-level timestamp, and Trace decision panel using the deterministic stack.

Run:

```bash
cd web && npm run test:e2e -- --grep "conversation attempts"
```

Expected: PASS.

- [ ] **Step 11: Commit**

```bash
git add web/src/features/conversation web/src/features/trace web/src/styles.css web/tests/workspace.spec.ts
git commit -m "feat: show timestamped run attempts"
```

### Task 7: Add Focused Concurrency/Rerun Acceptance and Operations Documentation

**Files:**
- Create: `integration/v3/concurrency_rerun_scenario.py`
- Create: `integration/v3/test_concurrency_rerun_scenario.py`
- Modify: `run_v3_tests.py`
- Modify: `run_v3_tests_test.py`
- Modify: `docs/architecture.md`
- Modify: `docs/operations.md`

**Interfaces:**
- Consumes: deterministic scripted model stack and `POST /runs/{id}/reruns`.
- Produces: focused acceptance proof for project/conversation isolation, configured concurrency, immutable failed evidence, rerun lineage, and idempotency.

- [ ] **Step 1: Write unit tests for scenario validation before the scenario**

Define expected summary validation:

```python
summary = {
    "requested_runs": 8,
    "terminal_runs": 8,
    "cross_project_runs": 0,
    "cross_conversation_messages": 0,
    "duplicate_message_ordinals": 0,
    "minimum_peak_concurrency": 4,
    "peak_concurrency": 4,
    "source_status": "failed",
    "rerun_status": "succeeded",
    "rerun_count": 1,
    "user_message_count": 1,
    "lineage_valid": True,
    "source_evidence_unchanged": True,
}
validate_summary(summary)
```

Mutate each invariant in subtests and assert `AssertionError`.

- [ ] **Step 2: Run scenario unit tests and confirm the module is absent**

Run:

```bash
cd worker && uv run python -m unittest discover -s ../integration/v3 -p 'test_concurrency_rerun_scenario.py'
```

Expected: FAIL because the scenario module does not exist.

- [ ] **Step 3: Implement concurrent project/conversation submission**

Use the existing request/wait helpers from `real_provider_load_scenario.py`, but require the scripted provider. Create at least two projects, four conversations, and two Runs per conversation with `ThreadPoolExecutor(max_workers=8)`.

Collect:

- terminal status counts;
- project/conversation ownership mismatches;
- duplicate message ordinals;
- peak overlap from `runs.started_at`/`finished_at`;
- pending outbox and unprocessed command counts.

- [ ] **Step 4: Implement deterministic failed-Run rerun verification**

Create a dedicated task, stop the worker with:

```python
compose("stop", "worker")
```

Submit the task and wait for durable `failed`. Snapshot counts and hashes for its spans, usage, operations, events, and artifacts. Restart and health-check the worker, call the rerun endpoint twice with the same key, and wait for the returned Run to succeed.

Assert:

- both rerun responses have one Run ID;
- source remains failed;
- source evidence counts/hashes are unchanged;
- rerun points to source and shares `source_message_id`;
- rerun evidence IDs do not intersect source IDs;
- conversation has one user message and two Runs.

Use `try/finally` so the worker is restarted even when assertions fail.

- [ ] **Step 5: Add the focused scenario to the verification runner**

Insert:

```python
[
    "uv", "run", "--with", "psycopg[binary]>=3.2,<4",
    "python", "integration/v3/concurrency_rerun_scenario.py",
]
```

after the canonical scenario. Update `run_v3_tests_test.py` to assert command order and fail-fast behavior.

- [ ] **Step 6: Run scenario unit tests and runner tests**

Run:

```bash
cd worker && uv run python -m unittest discover -s ../integration/v3 -p 'test_concurrency_rerun_scenario.py'
python3 -m unittest run_v3_tests_test.py
```

Expected: PASS.

- [ ] **Step 7: Update architecture and operations docs**

Document:

- `source_message_id` and `retry_of_run_id`;
- user rerun versus Kafka/DLQ retry;
- only failed Runs are eligible;
- reruns repeat external effects;
- default concurrency four and command partitions eight;
- API curl example:

```bash
curl -X POST http://127.0.0.1:8080/api/v1/runs/<failed-run-id>/reruns \
  -H 'Content-Type: application/json' \
  -d '{"idempotency_key":"<uuid>"}'
```

- [ ] **Step 8: Run formatting and targeted cross-layer verification**

Run:

```bash
gofmt -w backend/internal/domain backend/internal/postgres backend/internal/api/v3
docker compose exec -T api go test ./internal/postgres ./internal/api/v3 -count=1
cd web && npm test -- --run
cd web && npm run build
cd worker && uv run python -m unittest discover -s ../integration/v3 -p 'test_concurrency_rerun_scenario.py'
python3 -m unittest run_v3_tests_test.py
git diff --check
```

Expected: all commands PASS. Do not run `run_v3_tests.py` yet.

- [ ] **Step 9: Commit**

```bash
git add integration/v3/concurrency_rerun_scenario.py integration/v3/test_concurrency_rerun_scenario.py run_v3_tests.py run_v3_tests_test.py docs/architecture.md docs/operations.md
git commit -m "test: cover concurrent failed-run reruns"
```

### Task 8: Review and Final Targeted Verification

**Files:**
- Review: all files changed by Tasks 1-7

**Interfaces:**
- Consumes: completed implementation and all focused test evidence.
- Produces: review findings resolved and an implementation-ready workspace for the later full verification run.

- [ ] **Step 1: Invoke the requesting-code-review skill**

Request review against:

```text
docs/superpowers/specs/2026-10-25-agent-observability-rerun-design.md
docs/superpowers/plans/2026-10-25-agent-observability-rerun.md
```

Require attention to transaction atomicity, lock ordering, idempotency, public specification leakage, stale React requests, and Run lineage deletion behavior.

- [ ] **Step 2: Resolve every confirmed finding with focused regression tests**

For each confirmed issue, first add a test that fails for the reported behavior, then implement the smallest correction and rerun the affected package.

- [ ] **Step 3: Invoke verification-before-completion**

Run the targeted verification set from Task 7 Step 8 again. Inspect actual output and confirm no session remains running.

- [ ] **Step 4: Inspect final repository state**

Run:

```bash
git status --short
git log --oneline -10
```

Expected: no uncommitted implementation files. The full `python3 run_v3_tests.py` remains intentionally deferred until the user asks to run the complete suite.
