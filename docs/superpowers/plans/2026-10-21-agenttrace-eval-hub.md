# AgentTrace Eval Hub v3 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build and locally verify AgentTrace Eval Hub as a three-column agent evaluation workspace with PostgreSQL facts, Kafka delivery, Redis live updates, a resumable Go runtime, and the existing Playwright worker as its first operation executor.

**Architecture:** Keep one Go module with shared contracts but run two processes: `agenttrace-api` accepts durable work and serves SSE, while `agenttrace-runtime` consumes Kafka commands and executes a checkpointed ReAct loop. PostgreSQL is authoritative, Kafka is at-least-once delivery through a transactional outbox, Redis is disposable live coordination, and Langfuse is an optional asynchronous mirror.

**Tech Stack:** Go 1.24, `pgx/v5`, `franz-go`, `go-redis/v9`, PostgreSQL 16, Kafka 3.9 KRaft, Redis 7, Python 3.12/FastAPI/Playwright, React 18/TypeScript/Vite, Docker Compose, Playwright.

**Spec:** `docs/superpowers/specs/2026-10-21-agenttrace-eval-hub-design.md`

## Global Constraints

- The product name is exactly `AgentTrace Eval Hub`.
- The UI remains one left/center/right workspace; selecting a conversation, run, report, approval, or Trace node must not navigate to a separate workflow page.
- PostgreSQL is the only source of truth for messages, runs, Trace nodes, operation results, usage, idempotency, outbox, and inbox.
- Kafka uses `agenttrace.run.commands.v1`, `agenttrace.run.events.v1`, and `agenttrace.run.dlq.v1`; delivery is at-least-once and every consumer deduplicates by `event_id`.
- Redis stores only capped/expiring live streams, leases, and cancel hints; deleting Redis data must not delete history.
- Phase 1 implements Kafka behind `EventBus`; it does not implement RocketMQ.
- Langfuse is optional and asynchronous; it never serves product reads or blocks run completion.
- Callback URLs and credentials come from server-owned agent profiles, never arbitrary browser input.
- The existing browser contract, planner, report, feedback, usage, worker actions, worker conditions, locator logic, evidence collection, and fixtures are reused unless a task explicitly replaces their integration boundary.
- External model billing is not exactly once; persisted messages, spans, operation results, and run transitions are idempotent.
- New behavior is test-first. Every red test must fail for the intended missing behavior before implementation.
- Existing user changes in `test.config.json`, `worker/uv.lock`, and unrelated untracked files must not be overwritten or reverted.
- Execution starts in an isolated `v3` worktree created with the `using-git-worktrees` skill.

## File Map And Disposition

### Reuse with focused adaptation

- `backend/internal/contract/**`
- `backend/internal/planner/**`
- `backend/internal/report/**`
- `backend/internal/feedback/**`
- `backend/internal/usage/**`
- `backend/internal/agentruntime/llm_openai.go`
- `backend/internal/agentruntime/llm_scripted.go`
- `worker/loop_worker/actions.py`
- `worker/loop_worker/conditions.py`
- `worker/loop_worker/evidence.py`
- `worker/loop_worker/locators.py`
- `worker/loop_worker/observer.py`
- `worker/loop_worker/runner.py`
- `worker/fixtures/**`
- `fixtures/**`

### Replace in the v3 runtime path

- `backend/internal/store/**`: SQLite repository becomes PostgreSQL repositories.
- `backend/internal/store.Broker`: replaced by Redis live streams plus PostgreSQL replay.
- `backend/cmd/loopd`: split into `agenttrace-api` and `agenttrace-runtime`.
- `backend/internal/api/server.go`: replace four-page endpoints with project/conversation/message/run/Trace APIs.
- `web/src/App.tsx`: replace routed pages with one workspace shell.
- `web/src/pages/**`: retire after equivalent inline center/right panel features pass.

### New ownership boundaries

| Package/path | Responsibility |
|---|---|
| `backend/internal/domain` | IDs, lifecycle types, message/run/span models |
| `backend/internal/event` | Versioned envelopes and command/event payloads |
| `backend/internal/postgres` | Migrations, transactions, repositories, outbox/inbox |
| `backend/internal/eventbus` | Kafka-neutral interface |
| `backend/internal/eventbus/kafka` | Franz-go producer/consumer adapter |
| `backend/internal/live` | Redis-neutral live stream/lease/cancel interface |
| `backend/internal/live/redis` | Go Redis adapter |
| `backend/internal/runtime` | Checkpointed ReAct coordinator |
| `backend/internal/operation` | Operation definitions, validation, dispatch |
| `backend/internal/trace` | Durable Trace writer and projection |
| `backend/internal/export/langfuse` | Optional Kafka-driven mirror |
| `web/src/features/workspace` | Three-column shell and selection state |
| `web/src/features/conversation` | Messages, inline decisions, composer |
| `web/src/features/trace` | Trace tree and node details |
| `integration/v3` | Compose E2E and failure drills |

---

### Task 1: Create The Isolated v3 Baseline And Local Infrastructure

**Files:**
- Create: `compose.yaml`
- Create: `.env.example`
- Create: `backend/internal/config/config.go`
- Create: `backend/internal/config/config_test.go`
- Create: `backend/cmd/agenttrace-api/main.go`
- Create: `backend/cmd/agenttrace-runtime/main.go`
- Modify: `backend/go.mod`
- Modify: `web/package.json`

**Interfaces:**
- Produces `config.Load() (config.Config, error)`.
- Produces executable entry points that load configuration and fail clearly until later tasks wire dependencies.
- Produces Compose services named `postgres`, `redis`, `kafka`, `worker`, `api`, `runtime`, and `web`.

- [ ] **Step 1: Create the isolated worktree**

Run through the `using-git-worktrees` skill:

```bash
git worktree add .worktrees/v3 -b v3
cd .worktrees/v3
```

Expected: the new worktree is clean and contains this spec and plan.

- [ ] **Step 2: Write failing configuration tests**

Create `backend/internal/config/config_test.go`:

```go
package config

import "testing"

func TestLoadRequiresPostgresAndKafka(t *testing.T) {
	t.Setenv("AGENTTRACE_POSTGRES_DSN", "")
	t.Setenv("AGENTTRACE_KAFKA_BROKERS", "")
	if _, err := Load(); err == nil {
		t.Fatal("expected missing dependency configuration error")
	}
}

func TestLoadAllowsRedisToBeDisabled(t *testing.T) {
	t.Setenv("AGENTTRACE_POSTGRES_DSN", "postgres://agenttrace:agenttrace@localhost:5432/agenttrace?sslmode=disable")
	t.Setenv("AGENTTRACE_KAFKA_BROKERS", "localhost:9092")
	t.Setenv("AGENTTRACE_REDIS_ADDR", "")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Redis.Enabled {
		t.Fatal("redis must be optional")
	}
}
```

- [ ] **Step 3: Run the tests and verify the package is missing**

Run:

```bash
cd backend && go test ./internal/config -run TestLoad -v
```

Expected: FAIL because `config.Load` does not exist.

- [ ] **Step 4: Implement typed configuration and dependency pins**

Define these public shapes in `backend/internal/config/config.go`:

```go
type Config struct {
	HTTPAddr string
	Postgres Postgres
	Kafka Kafka
	Redis Redis
	WorkerURL string
	ArtifactDir string
	RuntimeConcurrency int
}

type Postgres struct{ DSN string }
type Kafka struct {
	Brokers []string
	CommandTopic string
	EventTopic string
	DLQTopic string
	GroupID string
}
type Redis struct {
	Enabled bool
	Addr string
	Password string
	DB int
}
```

Use `AGENTTRACE_` environment names. Pin Go dependencies with:

```bash
cd backend
go get github.com/jackc/pgx/v5@latest
go get github.com/twmb/franz-go@latest
go get github.com/redis/go-redis/v9@latest
```

Create `compose.yaml` with PostgreSQL 16, Redis 7, and single-node Kafka 3.9 KRaft
health checks. API and Runtime must wait for PostgreSQL and Kafka health; Redis remains
optional at application level even though Compose starts it.

- [ ] **Step 5: Verify configuration and Compose syntax**

Run:

```bash
cd backend && go test ./internal/config -v
cd .. && docker compose config --quiet
```

Expected: tests PASS and Compose exits zero.

- [ ] **Step 6: Commit the baseline**

```bash
git add compose.yaml .env.example backend web/package.json
git commit -m "build: establish AgentTrace v3 infrastructure"
```

---

### Task 2: Define Domain, Event, And Database Contracts

**Files:**
- Create: `backend/internal/domain/models.go`
- Create: `backend/internal/domain/status.go`
- Create: `backend/internal/domain/status_test.go`
- Create: `backend/internal/event/envelope.go`
- Create: `backend/internal/event/envelope_test.go`
- Create: `backend/internal/postgres/migrations/001_initial.sql`
- Create: `backend/internal/postgres/migrate.go`
- Create: `backend/internal/postgres/testdb_test.go`

**Interfaces:**
- Produces `domain.RunStatus`, `domain.SpanKind`, `domain.Message`, `domain.Run`, and `domain.Span`.
- Produces `event.Envelope[T]` and `event.RunCommand`.
- Produces `postgres.Migrate(ctx, pool) error`.

- [ ] **Step 1: Write failing lifecycle and envelope tests**

`domain/status_test.go` must assert:

```go
func TestRunStatusTransitions(t *testing.T) {
	cases := []struct{ from, to RunStatus; allowed bool }{
		{RunQueued, RunRunning, true},
		{RunRunning, RunWaitingInput, true},
		{RunWaitingInput, RunRunning, true},
		{RunRunning, RunSucceeded, true},
		{RunSucceeded, RunRunning, false},
		{RunCancelled, RunRunning, false},
	}
	for _, tc := range cases {
		if got := CanTransition(tc.from, tc.to); got != tc.allowed {
			t.Fatalf("%s -> %s = %v", tc.from, tc.to, got)
		}
	}
}
```

`event/envelope_test.go` must marshal and unmarshal an
`agenttrace.run-command.v1` envelope and reject an unknown major schema version.

- [ ] **Step 2: Run tests and verify missing contracts**

Run:

```bash
cd backend && go test ./internal/domain ./internal/event -v
```

Expected: FAIL because the packages are incomplete.

- [ ] **Step 3: Implement the domain and event contracts**

Use these run states exactly:

```go
const (
	RunQueued RunStatus = "queued"
	RunRunning RunStatus = "running"
	RunWaitingInput RunStatus = "waiting_input"
	RunWaitingApproval RunStatus = "waiting_approval"
	RunCancelling RunStatus = "cancelling"
	RunSucceeded RunStatus = "succeeded"
	RunFailed RunStatus = "failed"
	RunCancelled RunStatus = "cancelled"
)
```

Define span kinds `run`, `model`, `reasoning`, `tool`, `operation`, and `report`.
`event.RunCommand` contains `RunID`, `ConversationID`, `Model`, `Messages`,
`Operations`, and `CheckpointVersion`.

- [ ] **Step 4: Write the complete initial PostgreSQL migration**

`001_initial.sql` creates all tables and constraints from spec section 8. It must also
create:

```sql
CREATE INDEX idx_messages_conversation_ordinal
  ON messages (conversation_id, ordinal);
CREATE INDEX idx_spans_run_parent
  ON spans (run_id, parent_span_id, started_at);
CREATE INDEX idx_run_events_replay
  ON run_events (run_id, seq);
CREATE INDEX idx_outbox_pending
  ON outbox_events (created_at)
  WHERE published_at IS NULL;
```

The migration runner stores applied versions in `schema_migrations` and executes each
new migration in a transaction. `outbox_events` includes `next_attempt_at`,
`locked_by`, and `locked_until`, so relay workers can recover an interrupted claim
without holding a database transaction open during Kafka I/O.

- [ ] **Step 5: Verify migration against Compose PostgreSQL**

Run:

```bash
docker compose up -d postgres
cd backend && AGENTTRACE_TEST_POSTGRES_DSN='postgres://agenttrace:agenttrace@127.0.0.1:5432/agenttrace?sslmode=disable' go test ./internal/postgres -v
```

Expected: migration applies twice without error and required unique constraints are
present.

- [ ] **Step 6: Commit contracts and schema**

```bash
git add backend/internal/domain backend/internal/event backend/internal/postgres
git commit -m "feat: define AgentTrace domain and database schema"
```

---

### Task 3: Implement PostgreSQL Transactions, Repositories, And Idempotency

**Files:**
- Create: `backend/internal/postgres/store.go`
- Create: `backend/internal/postgres/projects.go`
- Create: `backend/internal/postgres/conversations.go`
- Create: `backend/internal/postgres/runs.go`
- Create: `backend/internal/postgres/traces.go`
- Create: `backend/internal/postgres/outbox.go`
- Create: `backend/internal/postgres/store_integration_test.go`
- Modify: `backend/internal/usage/usage.go`

**Interfaces:**
- Produces:

```go
type CreateMessageParams struct {
	ConversationID string
	Content string
	AgentID string
	Model event.ModelRef
	IdempotencyKey string
}

func (s *Store) CreateMessageAndRun(
	ctx context.Context, p CreateMessageParams,
) (domain.Message, domain.Run, error)

func (s *Store) ClaimRun(
	ctx context.Context, runID string, expectedCheckpoint int64,
) (domain.Run, bool, error)

func (s *Store) AppendTraceStep(
	ctx context.Context, step TraceStep,
) error
```

- [ ] **Step 1: Write transaction and idempotency integration tests**

The test sends the same `CreateMessageParams` twice and asserts:

```go
if firstRun.ID != secondRun.ID || firstMessage.ID != secondMessage.ID {
	t.Fatal("idempotent request returned different identities")
}
assertRowCount(t, pool, "messages", 1)
assertRowCount(t, pool, "runs", 1)
assertRowCount(t, pool, "outbox_events", 1)
```

A second test forces outbox insertion failure inside the transaction and asserts zero
message and run rows.

- [ ] **Step 2: Run the integration test and verify failure**

Run:

```bash
cd backend && AGENTTRACE_TEST_POSTGRES_DSN='postgres://agenttrace:agenttrace@127.0.0.1:5432/agenttrace?sslmode=disable' go test ./internal/postgres -run 'TestCreateMessage|TestClaimRun' -v
```

Expected: FAIL because repository methods are missing.

- [ ] **Step 3: Implement transaction-scoped repositories**

Use `pgxpool.Pool`, generated prefixed IDs, `SELECT ... FOR UPDATE`, and PostgreSQL
`jsonb`. `CreateMessageAndRun` must lock and validate the existing conversation,
insert the idempotency row first, then insert the message, run, and outbox rows in one
transaction. Duplicate keys read the stored response IDs and return them.

`ClaimRun` must use:

```sql
UPDATE runs
SET status = 'running', checkpoint_version = checkpoint_version + 1,
    started_at = COALESCE(started_at, now()), updated_at = now()
WHERE id = $1
  AND status IN ('queued', 'running')
  AND checkpoint_version = $2
RETURNING ...;
```

- [ ] **Step 4: Implement durable Trace writes**

`AppendTraceStep` transactionally upserts the span, inserts its attempt/usage/operation
result when present, appends the next monotonic `run_events.seq`, updates the run
checkpoint, and inserts an event outbox row. A unique conflict is treated as an
idempotent replay only when the stored payload hash matches.

- [ ] **Step 5: Run PostgreSQL tests**

Run:

```bash
cd backend && AGENTTRACE_TEST_POSTGRES_DSN='postgres://agenttrace:agenttrace@127.0.0.1:5432/agenttrace?sslmode=disable' go test ./internal/postgres ./internal/usage -v
```

Expected: PASS.

- [ ] **Step 6: Commit repositories**

```bash
git add backend/internal/postgres backend/internal/usage
git commit -m "feat: add durable AgentTrace repositories"
```

---

### Task 4: Add Kafka EventBus, Transactional Outbox Relay, And Inbox Deduplication

**Files:**
- Create: `backend/internal/eventbus/eventbus.go`
- Create: `backend/internal/eventbus/fake.go`
- Create: `backend/internal/eventbus/kafka/client.go`
- Create: `backend/internal/eventbus/kafka/client_integration_test.go`
- Create: `backend/internal/outbox/relay.go`
- Create: `backend/internal/outbox/relay_test.go`
- Create: `backend/internal/consumer/deduplicator.go`
- Create: `backend/internal/consumer/deduplicator_test.go`

**Interfaces:**
- Produces:

```go
type EventBus interface {
	Publish(ctx context.Context, topic, key string, value []byte) (Metadata, error)
	Consume(ctx context.Context, topic, group string, handler Handler) error
	Close() error
}

type Handler func(context.Context, Message) error
```

- [ ] **Step 1: Write failing relay tests with the fake bus**

Tests must cover:

```go
func TestRelayMarksPublishedOnlyAfterBrokerAck(t *testing.T)
func TestRelayLeavesRowPendingWhenPublishFails(t *testing.T)
func TestDeduplicatorRunsHandlerOnceForDuplicateEventID(t *testing.T)
```

The fake records topic, key, payload, and configured errors.

- [ ] **Step 2: Run tests and verify missing behavior**

Run:

```bash
cd backend && go test ./internal/outbox ./internal/consumer -v
```

Expected: FAIL because relay and deduplicator do not exist.

- [ ] **Step 3: Implement the interface and relay**

The relay claims a bounded batch with `FOR UPDATE SKIP LOCKED`, writes `locked_by` and
`locked_until`, and commits that short claim transaction. It publishes outside the
database transaction, then sets `published_at`, `broker_partition`, and
`broker_offset`. On failure it clears the lease, increments `attempt_count`, stores a
bounded error string, and schedules exponential retry without deleting the row.
Another relay may reclaim an unpublished row after `locked_until`.

- [ ] **Step 4: Implement Franz-go and Kafka integration test**

Kafka messages use `run_id` as the record key. Consumer auto-commit is disabled; the
adapter commits only after the handler transaction succeeds. The integration test:

1. publishes two messages for one run;
2. consumes them in order;
3. invokes the deduplicator twice with the same `event_id`;
4. asserts one `inbox_events` row and one persisted effect.

- [ ] **Step 5: Run unit and Compose-backed tests**

Run:

```bash
docker compose up -d kafka
cd backend && go test ./internal/eventbus/... ./internal/outbox ./internal/consumer -v
```

Expected: PASS.

- [ ] **Step 6: Commit durable delivery**

```bash
git add backend/internal/eventbus backend/internal/outbox backend/internal/consumer
git commit -m "feat: add Kafka delivery with outbox and inbox"
```

---

### Task 5: Add Redis Live Streams With PostgreSQL Fallback

**Files:**
- Create: `backend/internal/live/live.go`
- Create: `backend/internal/live/memory.go`
- Create: `backend/internal/live/redis/client.go`
- Create: `backend/internal/live/redis/client_integration_test.go`
- Create: `backend/internal/stream/service.go`
- Create: `backend/internal/stream/service_test.go`

**Interfaces:**
- Produces:

```go
type LiveStore interface {
	Append(ctx context.Context, runID string, event LiveEvent) error
	Read(ctx context.Context, runID, afterID string, block time.Duration) ([]LiveEvent, error)
	SetLease(ctx context.Context, runID, owner string, ttl time.Duration) error
	RequestCancel(ctx context.Context, runID string, ttl time.Duration) error
	IsCancelRequested(ctx context.Context, runID string) (bool, error)
}
```

- [ ] **Step 1: Write stream fallback tests**

`stream/service_test.go` must assert:

```go
func TestReplayStartsWithDurableEvents(t *testing.T)
func TestReplayFallsBackWhenRedisIsUnavailable(t *testing.T)
func TestMissingRedisStreamDoesNotMeanMissingRun(t *testing.T)
func TestLiveEventsAfterDurableSequenceAreNotDuplicated(t *testing.T)
```

- [ ] **Step 2: Run tests and verify failure**

Run:

```bash
cd backend && go test ./internal/live/... ./internal/stream -v
```

Expected: FAIL because live and stream services are missing.

- [ ] **Step 3: Implement Redis key semantics**

Use:

```text
run:{id}:live   XADD MAXLEN ~ 2000, EXPIRE 24h
run:{id}:lease  SET owner EX 30
run:{id}:cancel SET 1 EX 24h
```

`Append` failure is returned to metrics/logging but must never roll back a committed
PostgreSQL Trace step. `Read` supports blocking reads and clean context cancellation.

- [ ] **Step 4: Implement durable-first replay**

`stream.Service.Subscribe` reads `run_events` with `seq > Last-Event-ID`, emits them in
order, then tails Redis. Durable Redis notifications carry the corresponding
PostgreSQL sequence and are deduplicated against replay. Token deltas are emitted
without an SSE `id`; they are not replayable. When Redis errors, the service polls
PostgreSQL every second until Redis recovers or the request closes.

- [ ] **Step 5: Run Redis integration tests**

Run:

```bash
docker compose up -d redis postgres
cd backend && go test ./internal/live/... ./internal/stream -v
```

Expected: PASS, including a test that executes `FLUSHDB` and still replays PostgreSQL
events.

- [ ] **Step 6: Commit the live layer**

```bash
git add backend/internal/live backend/internal/stream
git commit -m "feat: add recoverable Redis live streams"
```

---

### Task 6: Refactor The Agent Runtime Into A Checkpointed ReAct Engine

**Files:**
- Create: `backend/internal/runtime/engine.go`
- Create: `backend/internal/runtime/checkpoint.go`
- Create: `backend/internal/runtime/consumer.go`
- Create: `backend/internal/runtime/engine_test.go`
- Create: `backend/internal/runtime/resume_test.go`
- Create: `backend/internal/model/model.go`
- Create: `backend/internal/model/scripted.go`
- Create: `backend/internal/model/openai.go`
- Modify: `backend/internal/agentruntime/llm_openai.go`
- Modify: `backend/internal/agentruntime/llm_scripted.go`
- Modify: `backend/internal/agentruntime/message.go`
- Modify: `backend/internal/agentruntime/loop.go`

**Interfaces:**
- Produces:

```go
type Model interface {
	Complete(context.Context, Request) (Response, error)
	Label() string
}

type CheckpointStore interface {
	Load(context.Context, string) (Checkpoint, error)
	CommitStep(context.Context, StepResult) error
	CompleteRun(context.Context, Completion) error
}

type Engine struct {
	Model Model
	Operations operation.Dispatcher
	Store CheckpointStore
	Live live.LiveStore
}
```

- [ ] **Step 1: Write failing engine tests**

Tests use a scripted model and fake dispatcher:

```go
func TestEnginePersistsModelAndOperationSpans(t *testing.T)
func TestEngineResumesAfterCompletedOperation(t *testing.T)
func TestEngineStopsAtModelCallBudget(t *testing.T)
func TestEngineHonorsDurableCancelWithoutRedis(t *testing.T)
func TestDuplicateCommandDoesNotCreateSecondAssistantMessage(t *testing.T)
```

The resume test runs one model/tool step, injects a process error, creates a new Engine,
and asserts the second Engine starts at checkpoint 1 without dispatching operation 0.

- [ ] **Step 2: Run runtime tests and verify failure**

Run:

```bash
cd backend && go test ./internal/runtime ./internal/model -v
```

Expected: FAIL because the new interfaces and engine are missing.

- [ ] **Step 3: Extract model adapters and deterministic step identities**

Move provider HTTP behavior behind `model.Model`; preserve current usage normalization,
request-size limits, token budgets, scripted fixtures, and tool-call parsing. Derive
logical span IDs from `run_id + checkpoint_index + span_kind`, and attempt IDs from
logical span ID plus attempt number.

- [ ] **Step 4: Implement checkpointed ReAct execution**

For every iteration:

1. load the durable checkpoint;
2. check PostgreSQL cancel state and optional Redis hint;
3. persist a started attempt;
4. call model or operation;
5. atomically commit result, usage, durable run event, next checkpoint, and outbox;
6. best-effort append the compact live event;
7. continue until assistant completion or a terminal limit/error.

Do not keep answer channels or in-flight ownership only in memory. Waiting input and
approval responses are persisted messages/events that enqueue a continuation command.

- [ ] **Step 5: Wire the Kafka runtime consumer**

`runtime.Consumer` decodes only `agenttrace.run-command.v1`, runs inbox
deduplication/claim, renews the Redis lease while active, and routes malformed or
permanently failing commands to `agenttrace.run.dlq.v1` after the configured attempt
limit.

- [ ] **Step 6: Run runtime and retained v2 logic tests**

Run:

```bash
cd backend && go test ./internal/runtime ./internal/model ./internal/agentruntime ./internal/planner ./internal/contract ./internal/report ./internal/feedback ./internal/usage -v
```

Expected: PASS. Existing planner/contract/report behavior remains covered.

- [ ] **Step 7: Commit the runtime**

```bash
git add backend/internal/runtime backend/internal/model backend/internal/agentruntime
git commit -m "feat: add checkpointed ReAct runtime"
```

---

### Task 7: Add Typed Operation Dispatch And An Idempotent Playwright Adapter

**Files:**
- Create: `backend/internal/operation/schema.go`
- Create: `backend/internal/operation/dispatcher.go`
- Create: `backend/internal/operation/http.go`
- Create: `backend/internal/operation/dispatcher_test.go`
- Create: `backend/internal/agentprofile/profile.go`
- Create: `backend/internal/agentprofile/browser_eval.json`
- Modify: `worker/loop_worker/contracts.py`
- Modify: `worker/loop_worker/main.py`
- Create: `worker/loop_worker/idempotency.py`
- Create: `worker/tests/test_operations_api.py`

**Interfaces:**
- Produces `operation.Dispatcher.Dispatch(ctx, Call) (Result, error)`.
- Adds `POST /operations/browser_execute_case`.
- Adds `GET /operations` for worker-owned operation metadata.

- [ ] **Step 1: Write failing Go schema and callback tests**

Tests assert that:

- invalid arguments never call HTTP;
- callback URLs not present in the server profile are rejected;
- headers include stable operation/run/span IDs;
- a retry reuses the same `Idempotency-Key`.

Run:

```bash
cd backend && go test ./internal/operation ./internal/agentprofile -v
```

Expected: FAIL because the packages are missing.

- [ ] **Step 2: Write failing worker idempotency tests**

`test_operations_api.py` calls the same operation twice and asserts:

```python
assert first.status_code == 200
assert second.status_code == 200
assert first.json() == second.json()
assert app.state.operation_calls["op_same"] == 1
```

It also sends the same key with a different body and expects HTTP `409` with
`idempotency_conflict`.

- [ ] **Step 3: Run worker tests and verify failure**

Run:

```bash
cd worker && uv run python -m unittest tests.test_operations_api -v
```

Expected: FAIL because the operation route is missing.

- [ ] **Step 4: Implement server-owned profiles and Go dispatch**

`browser_eval.json` contains the model defaults, operation name, JSON Schema, timeout,
retry policy, and worker callback URL template. `operation/http.go` validates inputs,
sets tracing/idempotency headers, enforces response size/time limits, and decodes one
typed result envelope.

- [ ] **Step 5: Implement worker operation idempotency**

Use an in-process bounded cache for phase 1 keyed by operation ID and request hash.
Persist the operation result in PostgreSQL on the Go side before acknowledging the
Kafka command. The worker cache prevents duplicate side effects during a live process;
the stable operation result/checkpoint prevents redispatch after Runtime restart once
the result has committed. The plan does not claim exactly-once callback side effects
across simultaneous Runtime and Worker failure. The browser profile therefore creates
a fresh context, uses disposable fixture data, and marks irreversible production
operations unsupported.

The route adapts the existing `validate_case` and `run_case` functions rather than
copying browser logic.

- [ ] **Step 6: Run operation and worker regression tests**

Run:

```bash
cd backend && go test ./internal/operation ./internal/agentprofile -v
cd ../worker && uv run python -m unittest discover -s tests -t . -v
```

Expected: PASS.

- [ ] **Step 7: Commit operation execution**

```bash
git add backend/internal/operation backend/internal/agentprofile worker/loop_worker worker/tests/test_operations_api.py
git commit -m "feat: add idempotent browser operation execution"
```

---

### Task 8: Build The v3 API, SSE Gateway, Cancellation, And Health

**Files:**
- Create: `backend/internal/api/v3/server.go`
- Create: `backend/internal/api/v3/projects.go`
- Create: `backend/internal/api/v3/conversations.go`
- Create: `backend/internal/api/v3/messages.go`
- Create: `backend/internal/api/v3/runs.go`
- Create: `backend/internal/api/v3/events.go`
- Create: `backend/internal/api/v3/health.go`
- Create: `backend/internal/api/v3/server_test.go`
- Modify: `backend/cmd/agenttrace-api/main.go`
- Modify: `backend/cmd/agenttrace-runtime/main.go`

**Interfaces:**
- Produces:
  - `GET/POST /api/v1/projects`
  - `GET/POST /api/v1/projects/{id}/conversations`
  - `GET /api/v1/conversations/{id}`
  - `POST /api/v1/conversations/{id}/messages`
  - `GET /api/v1/runs/{id}`
  - `GET /api/v1/runs/{id}/trace`
  - `GET /api/v1/runs/{id}/events`
  - `POST /api/v1/runs/{id}/cancel`
  - `POST /api/v1/runs/{id}/responses`
  - `GET /api/v1/agents`
  - `GET /api/v1/health`

- [ ] **Step 1: Write failing HTTP contract tests**

Use `httptest` plus fake dependencies. Tests must assert:

```go
func TestSendMessageReturns202AndStableIDs(t *testing.T)
func TestSendMessageDuplicateIdempotencyKeyReturnsSameRun(t *testing.T)
func TestEventsResumeFromLastEventID(t *testing.T)
func TestCancelPersistsWhenRedisFails(t *testing.T)
func TestHealthReturns503OnlyWhenPostgresIsDown(t *testing.T)
func TestHealthReportsKafkaAndRedisSeparately(t *testing.T)
```

- [ ] **Step 2: Run API tests and verify failure**

Run:

```bash
cd backend && go test ./internal/api/v3 -v
```

Expected: FAIL because v3 handlers are missing.

- [ ] **Step 3: Implement APIs with strict request limits**

Decode one JSON value with unknown fields rejected, cap request bodies, map domain
errors to stable error codes, and never expose callback credentials. Message creation
returns:

```json
{"message_id":"msg_...","run_id":"run_...","status":"queued"}
```

SSE writes `id` only for durable PostgreSQL-sequenced events. Ephemeral token deltas
write `event` and JSON `data` without `id`, so `Last-Event-ID` always remains a valid
durable replay cursor. The handler flushes each event, sends a comment heartbeat every
15 seconds, and exits on request cancellation.

- [ ] **Step 4: Wire process composition roots**

`agenttrace-api` opens PostgreSQL, migrates, creates Kafka/Redis clients, starts the
outbox relay, and serves HTTP. `agenttrace-runtime` opens PostgreSQL, Kafka, optional
Redis, model adapters, profiles, and operation dispatcher, then consumes commands.
Both shut down on SIGINT/SIGTERM and close dependencies in reverse order.

- [ ] **Step 5: Run API and command tests**

Run:

```bash
cd backend && go test ./cmd/agenttrace-api ./cmd/agenttrace-runtime ./internal/api/v3 -v
```

Expected: PASS.

- [ ] **Step 6: Commit the control plane**

```bash
git add backend/cmd/agenttrace-api backend/cmd/agenttrace-runtime backend/internal/api/v3
git commit -m "feat: expose AgentTrace conversation and trace APIs"
```

---

### Task 9: Add The Optional Langfuse Event Exporter

**Files:**
- Create: `backend/internal/export/langfuse/exporter.go`
- Create: `backend/internal/export/langfuse/client.go`
- Create: `backend/internal/export/langfuse/exporter_test.go`
- Create: `backend/cmd/agenttrace-exporter/main.go`
- Modify: `compose.yaml`
- Modify: `.env.example`

**Interfaces:**
- Produces `langfuse.Exporter.Handle(ctx, eventbus.Message) error`.
- Consumes `agenttrace.run.events.v1`.
- Reads canonical Trace data from PostgreSQL before export.

- [ ] **Step 1: Write failing non-blocking exporter tests**

Tests assert:

- run completion is already committed before exporter invocation;
- transient HTTP failure returns an error for Kafka retry but does not alter the run;
- duplicate event export uses a stable Langfuse idempotency identity;
- disabled configuration creates no consumer.

- [ ] **Step 2: Run exporter tests and verify failure**

Run:

```bash
cd backend && go test ./internal/export/langfuse -v
```

Expected: FAIL because the exporter is missing.

- [ ] **Step 3: Implement the mirror**

Map project/conversation/run/span IDs to Langfuse session/trace/observation identities.
Never read product state back from Langfuse. Redact configured secret fields from
inputs/outputs before sending. Failed events remain retryable and move to the shared
DLQ after the consumer attempt limit.

- [ ] **Step 4: Verify against a mock Langfuse server**

Run:

```bash
cd backend && go test ./internal/export/langfuse -v
```

Expected: PASS with the mock recording one trace and its ordered spans.

- [ ] **Step 5: Commit optional export**

```bash
git add backend/internal/export backend/cmd/agenttrace-exporter compose.yaml .env.example
git commit -m "feat: mirror traces to Langfuse asynchronously"
```

---

### Task 10: Build The Three-Column Workspace And Typed Client

**Files:**
- Modify: `web/package.json`
- Modify: `web/src/App.tsx`
- Modify: `web/src/api.ts`
- Modify: `web/src/styles.css`
- Create: `web/src/features/workspace/Workspace.tsx`
- Create: `web/src/features/workspace/useWorkspace.ts`
- Create: `web/src/features/workspace/workspace.test.ts`
- Create: `web/src/features/projects/ProjectConversationPane.tsx`
- Create: `web/src/features/conversation/ConversationPane.tsx`
- Create: `web/src/features/conversation/Composer.tsx`
- Create: `web/src/features/conversation/MessageBlock.tsx`
- Create: `web/src/features/trace/TracePane.tsx`
- Create: `web/src/features/trace/TraceTree.tsx`
- Create: `web/src/features/trace/TraceDetails.tsx`
- Create: `web/src/features/trace/projectTrace.ts`
- Create: `web/src/features/trace/projectTrace.test.ts`
- Delete after replacement passes: `web/src/pages/ExecutionDetailPage.tsx`
- Delete after replacement passes: `web/src/pages/InjectionPage.tsx`
- Delete after replacement passes: `web/src/pages/InputSessionPage.tsx`
- Delete after replacement passes: `web/src/pages/ReportPage.tsx`

**Interfaces:**
- Produces one `Workspace` route.
- Produces typed `Project`, `Conversation`, `Message`, `Run`, `Span`, `RunEvent`,
  `Health`, and request/response functions in `api.ts`.
- Produces `projectTrace(spans) -> TraceNode[]`.

- [ ] **Step 1: Add frontend test dependencies and write failing state tests**

Add Vitest, Testing Library, and jsdom. Tests assert:

```ts
it('keeps selected project conversation run and span in one workspace state')
it('projects an out-of-order span list into a stable parent-child tree')
it('merges replayed SSE events without duplicating a sequence')
it('keeps the selected node when a sibling live update arrives')
```

- [ ] **Step 2: Run tests and verify failure**

Run:

```bash
cd web && npm test -- --run
```

Expected: FAIL because workspace state and Trace projection are missing.

- [ ] **Step 3: Implement the typed client and selection state**

Use query parameters `project`, `conversation`, `run`, and `span` only to restore
selection; do not define execution/report/injection routes. `useWorkspace` owns
selection, initial fetch, SSE connection, replay deduplication, and refetch after
terminal events.

- [ ] **Step 4: Implement the fixed workspace layout**

Desktop grid:

```css
.workspace {
  display: grid;
  grid-template-columns: minmax(220px, 280px) minmax(420px, 1fr) minmax(300px, 420px);
  min-height: 100dvh;
}
```

The left pane contains projects/conversations, center contains the message history and
sticky composer, and right contains Trace tree/details. Use existing design conventions,
Lucide icons for icon controls, 8px maximum control/card radius, stable row dimensions,
and no nested cards.

At narrow widths, preserve the same workspace state with pane tabs/collapse controls;
do not introduce route navigation.

- [ ] **Step 5: Implement complete interaction states**

Center pane handles queued, streaming, waiting input, waiting approval, failed,
cancelled, and completed blocks. Composer provides Agent and Model menus and disables
duplicate submission by generating one idempotency key per draft send. Right pane
supports keyboard-selectable Trace nodes, attempt/usage/evidence details, and live
status updates.

- [ ] **Step 6: Run frontend tests and build**

Run:

```bash
cd web && npm test -- --run && npm run build
```

Expected: PASS.

- [ ] **Step 7: Commit the workspace**

```bash
git add web
git commit -m "feat: build the AgentTrace three-column workspace"
```

---

### Task 11: Run The Canonical End-To-End Scenario

**Files:**
- Create: `fixtures/scripts/agenttrace_browser_eval.json`
- Create: `integration/v3/canonical_scenario.py`
- Create: `integration/v3/assert_postgres.py`
- Create: `integration/v3/wait_for_stack.py`
- Modify: `test.config.json` only after merging the user's current values intentionally

**Interfaces:**
- Produces one deterministic scripted-model scenario that reaches the real Playwright
  worker and stores a complete conversation/Trace.

- [ ] **Step 1: Write the failing canonical integration test**

The test must:

1. create project `Storefront QA`;
2. create a conversation;
3. send the canonical browser goal with an idempotency key;
4. wait for terminal SSE/run state;
5. assert the assistant message and report summary;
6. assert a Trace root with model and operation children;
7. assert browser evidence metadata and final usage;
8. query PostgreSQL and assert one user message, one assistant message, one run, and no
   unpublished completion event.

- [ ] **Step 2: Run the scenario and observe the first missing integration**

Run:

```bash
docker compose up -d --build
python integration/v3/canonical_scenario.py
```

Expected: FAIL at the first unwired model/operation/Compose boundary, with the run ID
printed for diagnosis.

- [ ] **Step 3: Add the scripted model fixture and finish service wiring**

The fixture emits the same sequence on every run: plan, grounded browser operation,
report, final assistant response. Wire Compose artifact volumes so Worker writes and
API serves the same path. Keep all ports/configuration in `test.config.json` and
environment variables; preserve the user's existing changes while adding v3 keys.

- [ ] **Step 4: Run backend, worker, frontend, and canonical tests**

Run:

```bash
cd backend && go test ./...
cd ../worker && uv run python -m unittest discover -s tests -t .
cd ../web && npm test -- --run && npm run build
cd .. && python integration/v3/canonical_scenario.py
```

Expected: all commands PASS.

- [ ] **Step 5: Commit the canonical closed loop**

```bash
git add fixtures/scripts/agenttrace_browser_eval.json integration/v3 test.config.json compose.yaml
git commit -m "test: prove the AgentTrace browser evaluation loop"
```

---

### Task 12: Automate Failure Drills And Duplicate-Delivery Proofs

**Files:**
- Create: `integration/v3/failure_drills.py`
- Create: `integration/v3/test_kafka_outage.py`
- Create: `integration/v3/test_redis_outage.py`
- Create: `integration/v3/test_runtime_restart.py`
- Create: `integration/v3/test_postgres_outage.py`
- Create: `integration/v3/test_exporter_outage.py`
- Create: `integration/v3/test_duplicate_delivery.py`

**Interfaces:**
- Produces `python integration/v3/failure_drills.py` as one deterministic resilience
  entry point.

- [ ] **Step 1: Write failure assertions before orchestration**

Each test uses a unique project/conversation and asserts:

```text
Kafka down: message/run/outbox exist, status queued, completion occurs after restart.
Redis down: run completes, Trace reloads from PostgreSQL, health says postgres_poll.
Runtime restart: run completes with one assistant message and unique completed spans.
PostgreSQL down: send returns 503 and no Kafka command is produced.
Exporter down: local run succeeds and export event retries.
Duplicate command: inbox contains one event effect and no duplicate result rows.
```

- [ ] **Step 2: Run the drill suite and verify missing orchestration**

Run:

```bash
python integration/v3/failure_drills.py
```

Expected: FAIL because service stop/start and assertions are not implemented.

- [ ] **Step 3: Implement bounded Docker Compose drills**

Use `docker compose stop/start/kill` only for named v3 services. Every wait has a
deadline, prints current health and run/outbox state on failure, and restores the
service in `finally`. Do not delete volumes between the stop and recovery assertion.

- [ ] **Step 4: Run the full drill suite twice**

Run:

```bash
python integration/v3/failure_drills.py
python integration/v3/failure_drills.py
```

Expected: both runs PASS, proving cleanup and idempotency.

- [ ] **Step 5: Commit resilience verification**

```bash
git add integration/v3
git commit -m "test: verify AgentTrace infrastructure failure semantics"
```

---

### Task 13: Verify The UI In A Real Browser

**Files:**
- Create: `web/tests/workspace.spec.ts`
- Create: `web/playwright.config.ts`
- Create: `artifacts/ui/.gitkeep`
- Modify: `web/package.json`

**Interfaces:**
- Produces `npm run test:e2e`.

- [ ] **Step 1: Write Playwright tests for desktop and narrow viewports**

Tests assert:

- desktop first viewport shows left project list, center conversation/composer, and
  right Trace without overlap;
- sending the canonical goal streams status and ends completed;
- selecting an operation node shows input, output, attempt, and evidence;
- refresh restores project/conversation/run/span selection;
- approval and question blocks submit in place;
- narrow viewport switches panes without changing the route;
- no horizontal text clipping or control overlap is visible.

- [ ] **Step 2: Run E2E and verify missing UI behavior**

Run:

```bash
cd web && npm run test:e2e
```

Expected: FAIL at the first incomplete selector or responsive state.

- [ ] **Step 3: Fix only observed layout and interaction defects**

Keep stable pane widths, explicit min/max constraints, non-overlapping composer and
scroll regions, visible focus states, and `prefers-reduced-motion` behavior. Capture
screenshots at 1440x900 and 390x844 under `artifacts/ui/`.

- [ ] **Step 4: Re-run unit, build, and browser tests**

Run:

```bash
cd web && npm test -- --run && npm run build && npm run test:e2e
```

Expected: PASS with nonblank screenshots and no console errors.

- [ ] **Step 5: Commit browser verification**

```bash
git add web artifacts/ui/.gitkeep
git commit -m "test: verify AgentTrace workspace in browser"
```

---

### Task 14: Remove Retired v2 Runtime Paths And Document Operations

**Files:**
- Delete: `backend/cmd/loopd/main.go`
- Delete: `backend/cmd/loopd/main_test.go`
- Delete: `backend/internal/store/store.go`
- Delete: `backend/internal/store/runs.go`
- Delete: `backend/internal/store/sessions.go`
- Delete: `backend/internal/store/artifacts.go`
- Delete: `backend/internal/store/usage.go`
- Delete: superseded tests under `backend/internal/store`
- Delete: `web/src/components/SessionList.tsx`
- Delete: superseded v2 page components retained through Task 10
- Modify: `backend/go.mod`
- Modify: `backend/go.sum`
- Modify: `README.md`
- Modify: `CONTRACT.md`
- Create: `docs/operations.md`
- Create: `docs/architecture.md`
- Create: `run_v3_tests.py`

**Interfaces:**
- Produces one documented local startup command and one full verification command.

- [ ] **Step 1: Prove retired imports are unused**

Run:

```bash
cd backend && go list -deps ./... | grep 'modernc.org/sqlite'
```

Expected before cleanup: the dependency is present only through retired store paths.

- [ ] **Step 2: Remove retired paths and SQLite**

Delete files only after Tasks 1-13 pass. Run:

```bash
cd backend
go mod tidy
```

Verify `modernc.org/sqlite` is absent from `go.mod` and `go.sum`. Keep browser case
contract documentation in `CONTRACT.md`, and add the v3 run/event/operation contract
locations instead of leaving references to SQLite or the four old pages.

- [ ] **Step 3: Write exact operator documentation**

`README.md` must include:

```bash
cp .env.example .env
docker compose up -d --build
python run_v3_tests.py
python integration/v3/failure_drills.py
```

`docs/operations.md` explains health output, outbox backlog inspection, DLQ inspection,
Redis degraded mode, runtime restart, and data reset. `docs/architecture.md` includes
the canonical scenario and the sentence:

```text
PostgreSQL preserves facts; Kafka preserves delivery; Redis preserves live experience.
```

- [ ] **Step 4: Implement the full test runner**

`run_v3_tests.py` runs, in order:

```text
Go unit and integration tests
Python worker tests
React unit tests and build
Canonical Compose scenario
Failure drills
Playwright UI verification
```

It stops on first failure, prints the exact failed command, and leaves service logs
available for diagnosis.

- [ ] **Step 5: Run final verification from a clean Compose state**

Run:

```bash
docker compose down -v
docker compose up -d --build
python run_v3_tests.py
git status --short
```

Expected: all layers PASS. `git status` contains only intentional generated screenshots
or no changes; no database volume, secret, token, cache, or browser binary is tracked.

- [ ] **Step 6: Commit the v3 cutover**

```bash
git add -A backend web README.md CONTRACT.md docs run_v3_tests.py
git commit -m "refactor: complete AgentTrace Eval Hub v3 cutover"
```

## Final Acceptance Checklist

- [ ] The canonical browser evaluation completes from the three-column UI.
- [ ] The same idempotency key returns the same message/run IDs.
- [ ] PostgreSQL contains the complete multi-turn conversation and Trace.
- [ ] Kafka outage leaves a queued run and pending outbox, then recovers automatically.
- [ ] Redis outage removes token streaming but not completion or historical Trace.
- [ ] Runtime restart resumes from a durable checkpoint without duplicate persisted effects.
- [ ] PostgreSQL outage prevents acceptance and returns `503`.
- [ ] Langfuse/exporter outage has no product-path effect.
- [ ] `GET /api/v1/health` reports PostgreSQL, Kafka, Redis, Worker, and Langfuse separately.
- [ ] Desktop and narrow browser screenshots show no overlap or clipped text.
- [ ] SQLite and the in-process Broker are absent from the v3 runtime.
- [ ] `python run_v3_tests.py` passes from a clean Compose state.
