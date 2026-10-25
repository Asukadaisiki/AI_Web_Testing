# Agent Observability and Failed-Run Rerun Design

**Date:** 2026-10-25

**Status:** Ready for review

## 1. Purpose

This change completes the smallest useful AgentTrace workflow:

- manage projects and conversations;
- submit independent agent tasks;
- run several tasks concurrently;
- read a conversation as a timestamped sequence of task attempts;
- inspect why a model selected a tool from observable inputs and outputs;
- rerun a failed task without deleting or rewriting its original evidence.

Evaluation datasets, scoring, experiment management, prompt optimization, and RSI
automation are outside this scope. Langfuse remains an optional asynchronous export
target and is not required for any behavior in this design.

## 2. Existing Capabilities

The implementation already provides:

- durable `Project`, `Conversation`, `Message`, and `Run` records in PostgreSQL;
- atomic message, run, idempotency, and command-outbox creation;
- checkpointed runtime execution;
- Kafka command delivery with eight local partitions;
- a configurable runtime worker pool, defaulting to four concurrent runs;
- PostgreSQL connection-pool sizing based on runtime concurrency;
- durable spans, attempts, usage, operation evidence, and terminal events;
- Redis live updates with PostgreSQL replay;
- concurrent load and race coverage.

The missing product behavior is not a new scheduler. The gaps are:

- no user-facing endpoint for rerunning a terminal failed Run;
- no durable lineage between an original Run and its reruns;
- conversation reads expose messages but not every related Run;
- the UI presents messages and global Run tabs instead of task attempts;
- model inputs and outputs exist in canonical spans, but the UI does not summarize
  the observable context around a tool selection.

## 3. Decisions

### 3.1 Selected approach

A failed-task rerun creates a new Run from the failed Run's immutable specification.
The old Run, Trace, events, usage, and artifacts remain unchanged. The new Run starts
at checkpoint zero and receives new span, operation, event, and artifact identities.

Conversation reads return all Runs in addition to messages. Each Run identifies the
user message that initiated the task and, for reruns, the immediately preceding
failed Run. The frontend uses these relationships to render task attempts inline.

Decision context is a read-time projection of facts already present in model-span
input and output. It shows the latest user objective, the latest tool observation
when present, the selected operation and arguments, and any normal visible assistant
text returned with that selection. It is explicitly not hidden chain-of-thought.

### 3.2 Rejected alternatives

**Reset and reuse the failed Run**

This would mix checkpoints, attempts, usage, timings, and artifacts from separate
executions. It would also make audit and comparison impossible.

**Resume the failed Run from its final checkpoint**

A terminal failure may have occurred after an external side effect, and not every
provider or worker failure has a safe continuation point. The user request is a task
rerun, so the new execution starts from the original immutable task input.

**Replay the old Kafka or DLQ record**

Broker redelivery is infrastructure recovery. Inbox deduplication correctly prevents
it from creating another logical execution. A user rerun needs a new Run and a new
command event.

**Create an evaluation dataset or experiment abstraction**

That introduces concepts the current workflow does not require. A conversation's
messages and Runs are sufficient task data for this MVP.

## 4. Run Lineage Data Model

Add migration `004_run_lineage.sql` with two nullable columns:

```sql
ALTER TABLE runs
  ADD COLUMN source_message_id TEXT REFERENCES messages(id) ON DELETE SET NULL,
  ADD COLUMN retry_of_run_id TEXT REFERENCES runs(id) ON DELETE SET NULL;

CREATE INDEX idx_runs_conversation_created
  ON runs (conversation_id, created_at, id);

CREATE INDEX idx_runs_source_message
  ON runs (source_message_id, created_at, id);
```

`source_message_id` identifies the user message whose task specification the Run
executes. It is set for every newly created Run. Existing rows are backfilled from
the user message whose `messages.run_id` references the Run.

`retry_of_run_id` is null for an original Run and contains the immediate parent for
a rerun. A rerun of a failed rerun is valid, so lineage may contain more than one
generation. The common `source_message_id` groups the entire attempt history without
requiring a separate root column.

The migration does not change the existing `messages.run_id` uniqueness rule. Only
the initiating user message points to the original Run through that column. Reruns
are discovered through `runs.source_message_id`.

The public Run projection adds:

```json
{
  "source_message_id": "msg_original",
  "retry_of_run_id": "run_failed_parent"
}
```

`retry_of_run_id` is omitted for original Runs. The private immutable specification
remains excluded from public responses.

## 5. Failed-Run Rerun API

### 5.1 Endpoint

```http
POST /api/v1/runs/{run_id}/reruns
Content-Type: application/json

{
  "idempotency_key": "client-generated-uuid"
}
```

Successful acceptance returns HTTP 202:

```json
{
  "run_id": "run_new",
  "status": "queued",
  "retry_of_run_id": "run_failed_parent"
}
```

The API uses idempotency scope `run-rerun:{source_run_id}`. Repeating the same key
returns the same new Run. A new intentional rerun uses a new key.

### 5.2 Eligibility

Only a Run whose durable status is exactly `failed` can be rerun.

- unknown Run: HTTP 404;
- queued, running, waiting-input, waiting-approval, cancelling, succeeded, or
  cancelled Run: HTTP 409;
- missing or blank idempotency key: HTTP 400;
- unavailable PostgreSQL: HTTP 503.

Cancelled work is not silently treated as failed work. A future requirement may add
an explicit "run again" action for successful or cancelled Runs, but it is not part
of this change.

### 5.3 Atomic repository operation

`CreateRerun` performs one PostgreSQL transaction:

1. Lock the source Run.
2. Verify its status is `failed`.
3. Resolve and validate its `source_message_id`.
4. Apply the rerun idempotency key.
5. Decode the stored immutable run specification.
6. Allocate a new Run ID and command event ID.
7. Replace only command execution identity:
   - `command.run_id` becomes the new Run ID;
   - `command.checkpoint_version` becomes zero.
8. Insert a queued Run with the same project, conversation, agent, model, messages,
   and operation definitions.
9. Set `source_message_id` to the inherited source message and
   `retry_of_run_id` to the locked failed Run.
10. Insert a new `agenttrace.run-command.v1` outbox envelope keyed by the new Run ID.
11. Commit the idempotency response and all new durable state together.

No message is inserted. In particular, a rerun must not duplicate the user's prompt
in conversation history.

Operation IDs and all runtime identities derive from the new Run ID. The runtime
therefore executes a fresh task instead of deduplicating the old execution.

### 5.4 External-side-effect rule

A rerun repeats the task specification; it does not roll back browser or external
system side effects from the failed attempt. The UI labels the action `Rerun failed
task` rather than `Resume` so this behavior is not ambiguous.

## 6. Conversation Read Model

`GET /api/v1/conversations/{id}` keeps `conversation` and `messages` and adds `runs`:

```json
{
  "conversation": {},
  "messages": [],
  "runs": []
}
```

Runs are ordered by `created_at`, then `id`. Empty collections are encoded as `[]`.
The endpoint returns the same public Run shape as `GET /api/v1/runs/{id}`.

For assistant and response messages, the repository projects `run_id` from
`messages.metadata.run_id` when the direct `messages.run_id` column is null. This
makes every displayed message attributable to an execution without changing the
database uniqueness constraint used by initiating user messages.

The read model supports:

- selecting reruns after a page refresh;
- grouping all attempts under the initiating user message;
- associating assistant output with the Run that produced it;
- showing concurrent Runs in one conversation without relying on a global "latest
  Run" guess.

## 7. Conversation Timeline UI

The center pane becomes an ordered task timeline instead of a flat message list with
global Run tabs.

### 7.1 Time presentation

Each message shows:

- a local calendar date separator when the date changes;
- an absolute local timestamp including seconds;
- the persisted message ordinal as the stable ordering source.

Time never determines ordering. `ordinal` remains authoritative when timestamps are
equal or database clocks have limited precision.

### 7.2 Task and attempt grouping

For each user message:

1. Render the user message and send time.
2. Render compact attempt selectors for Runs sharing its `source_message_id`.
3. Label attempts `Attempt 1`, `Attempt 2`, and so on in Run creation order.
4. Show status, queued/start/finish times, and duration for each attempt.
5. Select the latest attempt by default, while preserving a valid Run selected in
   the URL.
6. Render the selected Run state and its assistant messages beneath the task.

The right Trace pane always follows the selected attempt. Removing global Run tabs
prevents Runs from unrelated messages from appearing as one undifferentiated list.

### 7.3 Rerun action

The selected failed attempt shows a `Rerun failed task` button. The client generates
one idempotency key per intentional rerun and reuses it across transport retries
until HTTP 202 is received. While a request is in flight, the action is disabled.

After HTTP 202 the client:

- refreshes the conversation read model;
- selects the returned Run;
- opens its SSE stream;
- leaves the old failed attempt available.

No rerun control is shown for other statuses.

## 8. Observable Decision Context

The system must not claim to expose private chain-of-thought. It exposes a structured
summary of observable evidence for each model span.

The Trace API adds an optional `decision_context` field to public model spans:

```json
{
  "objective": {
    "role": "user",
    "content": "Run the checkout browser task"
  },
  "prior_observation": {
    "role": "tool",
    "content": "..."
  },
  "selected_operation": {
    "name": "browser_execute_case",
    "arguments": {}
  },
  "visible_explanation": "I will run the supplied browser case."
}
```

Projection rules:

- `objective` is the latest user message in the model request;
- `prior_observation` is the latest tool message after that objective, when present;
- `selected_operation` is present only when the model response contains one tool
  call;
- `visible_explanation` is normal assistant `content` returned by the provider;
- provider `reasoning_content` is not copied into this projection;
- absent evidence is omitted rather than inferred or fabricated;
- values pass through the existing recursive secret-key redaction;
- content fields are capped at 2,000 UTF-8 characters in this projection, while the
  existing canonical span remains the detailed debugging record.
- malformed model-span JSON omits `decision_context` and does not fail the complete
  Trace response.

The Trace details panel displays this projection first under `Decision context` for
model spans. Existing attempt, usage, canonical input/output, operation evidence,
derived browser steps, and failure feedback remain available.

## 9. Concurrency

No new scheduler is introduced. The accepted concurrency architecture remains:

- one immutable command per Run;
- Kafka partitions distribute independent Run keys;
- `AGENTTRACE_RUNTIME_CONCURRENCY` controls the runtime worker limit;
- the bounded runner prevents execution above that limit;
- PostgreSQL pool capacity is at least `2 * concurrency + 2`;
- checkpoint compare-and-set and inbox deduplication protect duplicate delivery;
- message ordinal allocation is serialized per conversation;
- all Run-specific identities include the Run ID.

Reruns enter the same queue and consume the same bounded capacity as original Runs.
They receive no priority and cannot bypass the configured limit.

The local default remains four runtime workers and eight command-topic partitions.
This change must preserve support for concurrent Runs across projects and
conversations, including several Runs in one conversation.

## 10. Failure Handling

- A failure while creating a rerun rolls back the new Run, outbox event, and
  idempotency record together.
- A repeated successful request returns the original rerun response.
- Kafka publication failure leaves the new Run queued with a pending outbox row.
- Runtime or worker failure terminates only that Run; other concurrent Runs continue.
- A failed rerun may itself be rerun, producing another immutable child Run.
- The original failed Run is never changed after reaching its terminal state.
- Conversation deletion remains blocked while any original or rerun Run is active or
  has unsafe delivery state.
- Existing project and conversation deletion must remove lineage rows without
  violating self-references.

## 11. Testing

Implementation follows focused test-driven changes.

### 11.1 Backend unit and integration tests

- migration backfills `source_message_id` for existing Runs;
- new messages set `source_message_id`;
- rerunning a failed Run creates one fresh queued Run and one command outbox event;
- rerun copies the immutable agent, model, input messages, and operation definitions;
- rerun changes Run and command identities and resets checkpoint version;
- rerun inserts no message;
- rerun lineage is correct for multiple generations;
- same idempotency key returns the same Run;
- every non-failed source status returns 409;
- concurrent duplicate rerun requests create one Run;
- conversation reads return original Runs, reruns, and attributable assistant
  messages in stable order;
- decision-context projection handles tool selection, no tool selection, missing
  visible explanation, truncation, malformed JSON, and redaction.

### 11.2 Frontend tests

- date separators and second-level timestamps render correctly;
- Runs group under their source message;
- attempt labels and selection remain stable;
- URL-selected reruns survive refresh;
- a failed attempt exposes the rerun action;
- other statuses do not expose the action;
- successful rerun selection switches SSE and Trace state;
- decision context distinguishes observed context from unavailable explanation.

### 11.3 Focused full-stack acceptance

Add a small deterministic scenario covering only this MVP:

1. Create multiple projects and conversations.
2. Submit at least eight independent tasks concurrently.
3. Assert every task reaches a terminal state, no records cross project or
   conversation boundaries, and observed peak execution is at least the configured
   worker concurrency when enough work exists.
4. Cause one deterministic task to fail because the worker is temporarily
   unavailable.
5. Restore the worker and call the failed-Run rerun endpoint.
6. Assert the original Run remains failed with unchanged Trace and usage.
7. Assert the new Run succeeds, has correct lineage, and has independent Trace,
   operation, usage, event, and artifact identities.
8. Assert repeated submission with the same rerun idempotency key creates no
   additional Run.
9. Assert conversation reads contain one user task and two attempts, not duplicate
   user messages.

The existing broad verification runner may continue to cover infrastructure and
browser behavior, but no dataset, evaluator, score, or RSI fixture is added.

## 12. Documentation

Update:

- `docs/architecture.md` with Run lineage and the distinction between user reruns
  and broker retries;
- `docs/operations.md` with `AGENTTRACE_RUNTIME_CONCURRENCY`, failed-Run rerun
  behavior, and the external-side-effect warning;
- API examples for the new endpoint and conversation `runs` collection.

## 13. Completion Criteria

The change is complete when:

- a failed task can be rerun from the API and workspace;
- the rerun is a new durable Run with explicit lineage;
- the old failed evidence remains unchanged and inspectable;
- conversations show timestamped messages grouped with all task attempts;
- model tool calls show observable decision context without claiming hidden
  chain-of-thought;
- configured multi-Run concurrency remains bounded and verified;
- the focused project, conversation, concurrency, and rerun acceptance scenario
  passes;
- no evaluation dataset or RSI subsystem has been introduced.
