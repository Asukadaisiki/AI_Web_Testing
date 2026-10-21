package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/domain"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/event"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/usage"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestCreateMessageAndRunReplaysMatchingIdempotencyRequest(t *testing.T) {
	ctx, pool, store := newRepositoryTestStore(t)
	conversationID := createConversationFixture(t, ctx, pool)
	params := CreateMessageParams{
		ConversationID: conversationID,
		Content:        "Log in and verify the cart",
		AgentID:        "browser-eval",
		Model:          event.ModelRef{Provider: "scripted", Name: "browser-eval-fixture"},
		IdempotencyKey: "request-1",
	}

	firstMessage, firstRun, err := store.CreateMessageAndRun(ctx, params)
	if err != nil {
		t.Fatalf("first create message and run: %v", err)
	}
	secondMessage, secondRun, err := store.CreateMessageAndRun(ctx, params)
	if err != nil {
		t.Fatalf("replay create message and run: %v", err)
	}

	if firstRun.ID != secondRun.ID || firstMessage.ID != secondMessage.ID {
		t.Fatal("idempotent request returned different identities")
	}
	assertRowCount(t, ctx, pool, "messages", 1)
	assertRowCount(t, ctx, pool, "runs", 1)
	assertRowCount(t, ctx, pool, "outbox_events", 1)
}

func TestCreateMessageAndRunRejectsMismatchedIdempotencyRequest(t *testing.T) {
	ctx, pool, store := newRepositoryTestStore(t)
	conversationID := createConversationFixture(t, ctx, pool)
	params := CreateMessageParams{
		ConversationID: conversationID,
		Content:        "first request",
		AgentID:        "browser-eval",
		Model:          event.ModelRef{Provider: "scripted", Name: "browser-eval-fixture"},
		IdempotencyKey: "request-1",
	}
	if _, _, err := store.CreateMessageAndRun(ctx, params); err != nil {
		t.Fatalf("first create message and run: %v", err)
	}

	params.Content = "different request"
	if _, _, err := store.CreateMessageAndRun(ctx, params); !errors.Is(err, ErrConflict) {
		t.Fatalf("mismatched replay error = %v, want ErrConflict", err)
	}
	assertRowCount(t, ctx, pool, "messages", 1)
	assertRowCount(t, ctx, pool, "runs", 1)
}

func TestCreateMessageAndRunRollsBackWhenOutboxInsertFails(t *testing.T) {
	ctx, pool, store := newRepositoryTestStore(t)
	conversationID := createConversationFixture(t, ctx, pool)
	if _, err := pool.Exec(ctx, `
		ALTER TABLE outbox_events
		ADD CONSTRAINT reject_test_outbox_write CHECK (false)`); err != nil {
		t.Fatalf("reject outbox writes: %v", err)
	}

	_, _, err := store.CreateMessageAndRun(ctx, CreateMessageParams{
		ConversationID: conversationID,
		Content:        "must roll back",
		AgentID:        "browser-eval",
		Model:          event.ModelRef{Provider: "scripted", Name: "browser-eval-fixture"},
		IdempotencyKey: "request-1",
	})
	if err == nil {
		t.Fatal("expected outbox failure")
	}
	assertRowCount(t, ctx, pool, "messages", 0)
	assertRowCount(t, ctx, pool, "runs", 0)
	assertRowCount(t, ctx, pool, "idempotency_keys", 0)
}

func TestClaimRunClaimsOnlyExpectedCheckpoint(t *testing.T) {
	ctx, pool, store := newRepositoryTestStore(t)
	conversationID := createConversationFixture(t, ctx, pool)
	_, run, err := store.CreateMessageAndRun(ctx, CreateMessageParams{
		ConversationID: conversationID,
		Content:        "claim me",
		AgentID:        "browser-eval",
		Model:          event.ModelRef{Provider: "scripted", Name: "browser-eval-fixture"},
		IdempotencyKey: "request-1",
	})
	if err != nil {
		t.Fatalf("create message and run: %v", err)
	}

	claimed, ok, err := store.ClaimRun(ctx, run.ID, 0)
	if err != nil {
		t.Fatalf("claim run: %v", err)
	}
	if !ok {
		t.Fatal("first matching checkpoint must claim the run")
	}
	if claimed.Status != domain.RunRunning || claimed.CheckpointVersion != 1 || claimed.StartedAt == nil {
		t.Fatalf("claimed run = %#v", claimed)
	}

	if _, ok, err := store.ClaimRun(ctx, run.ID, 0); err != nil || ok {
		t.Fatalf("stale checkpoint claim = (ok=%v, err=%v), want (false, nil)", ok, err)
	}
}

func TestStartTraceAttemptDoesNotAdvanceCheckpointAndCompletes(t *testing.T) {
	ctx, pool, store := newRepositoryTestStore(t)
	conversationID := createConversationFixture(t, ctx, pool)
	_, run, err := store.CreateMessageAndRun(ctx, CreateMessageParams{
		ConversationID: conversationID,
		Content:        "start trace attempt",
		AgentID:        "browser-eval",
		Model:          event.ModelRef{Provider: "scripted", Name: "browser-eval-fixture"},
		IdempotencyKey: "start-trace-attempt",
	})
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	if _, ok, err := store.ClaimRun(ctx, run.ID, 0); err != nil || !ok {
		t.Fatalf("claim run = (ok=%v, err=%v)", ok, err)
	}

	now := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	started := TraceAttemptStart{
		ExpectedCheckpoint: 1,
		Span: domain.Span{
			ID: "spn_started", RunID: run.ID, TraceID: "trc_started", SpanID: "model-1",
			Kind: domain.SpanModel, Name: "model", Status: "started",
			Input: json.RawMessage(`{"messages":[{"role":"user","content":"hello"}]}`), StartedAt: now,
		},
		Attempt: TraceAttempt{
			ID: "att_started", AttemptNumber: 1, Provider: "model", Status: "started",
			Request: json.RawMessage(`{"messages":[{"role":"user","content":"hello"}]}`), StartedAt: now,
		},
	}
	if err := store.StartTraceAttempt(ctx, started); err != nil {
		t.Fatalf("start trace attempt: %v", err)
	}
	if err := store.StartTraceAttempt(ctx, started); err != nil {
		t.Fatalf("idempotent start trace attempt: %v", err)
	}

	var checkpoint int64
	if err := pool.QueryRow(ctx, `SELECT checkpoint_version FROM runs WHERE id = $1`, run.ID).Scan(&checkpoint); err != nil {
		t.Fatalf("read checkpoint after start: %v", err)
	}
	if checkpoint != 1 {
		t.Fatalf("checkpoint after start = %d, want 1", checkpoint)
	}
	assertRowCount(t, ctx, pool, "spans", 1)
	assertRowCount(t, ctx, pool, "span_attempts", 1)

	finished := now.Add(time.Second)
	step := TraceStep{
		ExpectedCheckpoint: 1,
		Span: domain.Span{
			ID: "spn_started", RunID: run.ID, TraceID: "trc_started", SpanID: "model-1",
			Kind: domain.SpanModel, Name: "model", Status: "succeeded",
			Input:  json.RawMessage(`{"messages":[{"role":"user","content":"hello"}]}`),
			Output: json.RawMessage(`{"content":"done"}`), StartedAt: finished, EndedAt: &finished,
		},
		Attempt: &TraceAttempt{
			ID: "att_started", AttemptNumber: 1, Provider: "model", Status: "succeeded",
			Request:  json.RawMessage(`{"messages":[{"role":"user","content":"hello"}]}`),
			Response: json.RawMessage(`{"content":"done"}`), StartedAt: finished, EndedAt: &finished,
		},
		Event: event.Envelope[json.RawMessage]{
			SchemaVersion: "agenttrace.event.v1", EventID: "evt_started_complete",
			EventType: "runtime.step.completed", AggregateID: run.ID, CorrelationID: run.ID,
			OccurredAt: finished, Payload: json.RawMessage(`{"checkpoint":1}`),
		},
	}
	if err := store.AppendTraceStep(ctx, step); err != nil {
		t.Fatalf("complete started trace attempt: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT checkpoint_version FROM runs WHERE id = $1`, run.ID).Scan(&checkpoint); err != nil {
		t.Fatalf("read checkpoint after completion: %v", err)
	}
	if checkpoint != 2 {
		t.Fatalf("checkpoint after completion = %d, want 2", checkpoint)
	}
	var status string
	var endedAt *time.Time
	if err := pool.QueryRow(ctx, `SELECT status, ended_at FROM span_attempts WHERE id = 'att_started'`).Scan(&status, &endedAt); err != nil {
		t.Fatalf("load completed attempt: %v", err)
	}
	if status != "succeeded" || endedAt == nil {
		t.Fatalf("completed attempt = status %q ended_at %v, want succeeded with end time", status, endedAt)
	}
}

func TestConsumerRetryCountPersistsAcrossStoreInstances(t *testing.T) {
	ctx, pool, store := newRepositoryTestStore(t)
	if got, err := store.IncrementConsumerRetry(ctx, "runtime", "evt-retry", []byte(`not-json`)); err != nil || got != 1 {
		t.Fatalf("first retry count = (%d, %v), want (1, nil)", got, err)
	}
	second := NewStore(pool)
	if got, err := second.IncrementConsumerRetry(ctx, "runtime", "evt-retry", []byte(`not-json`)); err != nil || got != 2 {
		t.Fatalf("second retry count = (%d, %v), want (2, nil)", got, err)
	}
	if err := second.ClearConsumerRetry(ctx, "runtime", "evt-retry"); err != nil {
		t.Fatalf("clear retry count: %v", err)
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM consumer_retry_counts`).Scan(&count); err != nil {
		t.Fatalf("count retry rows: %v", err)
	}
	if count != 0 {
		t.Fatalf("retry rows = %d, want 0", count)
	}
}

func TestConsumerDLQHandoffMovesRetryStateToOneOutboxEvent(t *testing.T) {
	ctx, pool, store := newRepositoryTestStore(t)
	const (
		consumer = "runtime"
		retryID  = "evt-dlq-handoff"
		outboxID = "evt-dlq-outbox"
		topic    = "agenttrace.run.dlq.v1"
		key      = "run-dlq-handoff"
	)
	dlqPayload := []byte(`{"original":{"event_id":"evt-dlq-handoff"},"error":"permanent failure","attempt":2}`)
	if _, err := store.IncrementConsumerRetry(ctx, consumer, retryID, []byte(`{"event_id":"evt-dlq-handoff"}`)); err != nil {
		t.Fatalf("record first failure: %v", err)
	}
	if _, err := store.IncrementConsumerRetry(ctx, consumer, retryID, []byte(`{"event_id":"evt-dlq-handoff"}`)); err != nil {
		t.Fatalf("record final failure: %v", err)
	}

	if err := store.HandoffConsumerRetryToOutbox(ctx, consumer, retryID, outboxID, topic, key, dlqPayload); err != nil {
		t.Fatalf("handoff consumer retry to outbox: %v", err)
	}
	if err := store.HandoffConsumerRetryToOutbox(ctx, consumer, retryID, outboxID, topic, key, dlqPayload); err != nil {
		t.Fatalf("idempotent handoff consumer retry to outbox: %v", err)
	}

	var retryRows, outboxRows int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM consumer_retry_counts
		WHERE consumer = $1 AND event_id = $2`, consumer, retryID).Scan(&retryRows); err != nil {
		t.Fatalf("count retry state: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM outbox_events
		WHERE event_id = $1 AND topic = $2 AND key = $3 AND payload = $4::jsonb`,
		outboxID, topic, key, dlqPayload,
	).Scan(&outboxRows); err != nil {
		t.Fatalf("count DLQ outbox event: %v", err)
	}
	if retryRows != 0 || outboxRows != 1 {
		t.Fatalf("retry rows=%d outbox rows=%d, want 0 and 1", retryRows, outboxRows)
	}
}

func TestAppendTraceStepCommitsTraceAndReplaysMatchingPayload(t *testing.T) {
	ctx, pool, store := newRepositoryTestStore(t)
	conversationID := createConversationFixture(t, ctx, pool)
	_, run, err := store.CreateMessageAndRun(ctx, CreateMessageParams{
		ConversationID: conversationID,
		Content:        "trace this",
		AgentID:        "browser-eval",
		Model:          event.ModelRef{Provider: "scripted", Name: "browser-eval-fixture"},
		IdempotencyKey: "request-1",
	})
	if err != nil {
		t.Fatalf("create message and run: %v", err)
	}
	if _, ok, err := store.ClaimRun(ctx, run.ID, 0); err != nil || !ok {
		t.Fatalf("claim run = (ok=%v, err=%v)", ok, err)
	}

	now := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	step := TraceStep{
		ExpectedCheckpoint: 1,
		Span: domain.Span{
			ID:        "spn_test",
			RunID:     run.ID,
			TraceID:   "trc_test",
			SpanID:    "model-1",
			Kind:      domain.SpanModel,
			Name:      "Plan next action",
			Status:    "succeeded",
			Input:     json.RawMessage(`{"prompt":"trace this"}`),
			Output:    json.RawMessage(`{"content":"done"}`),
			StartedAt: now,
			EndedAt:   &now,
		},
		Attempt: &TraceAttempt{
			ID:            "att_test",
			AttemptNumber: 1,
			Provider:      "scripted",
			Status:        "succeeded",
			StartedAt:     now,
			EndedAt:       &now,
		},
		Usage: &TraceUsage{
			ID:       "use_test",
			Provider: "scripted",
			Model:    "browser-eval-fixture",
			Usage:    usage.Call(10, 5, 0, 0),
			Details:  json.RawMessage(`{"source":"test"}`),
		},
		Operation: &TraceOperationResult{
			ID:          "opr_test",
			OperationID: "op_test",
			Status:      "succeeded",
			Result:      json.RawMessage(`{"verified":true}`),
			Artifacts:   json.RawMessage(`[]`),
		},
		Event: event.Envelope[json.RawMessage]{
			SchemaVersion: "agenttrace.event.v1",
			EventID:       "evt_trace_test",
			EventType:     "span.completed",
			AggregateID:   run.ID,
			OccurredAt:    now,
			CorrelationID: run.ID,
			Payload:       json.RawMessage(`{"span_id":"model-1"}`),
		},
	}

	if err := store.AppendTraceStep(ctx, step); err != nil {
		t.Fatalf("append trace step: %v", err)
	}
	if err := store.AppendTraceStep(ctx, step); err != nil {
		t.Fatalf("replay trace step: %v", err)
	}
	assertRowCount(t, ctx, pool, "spans", 1)
	assertRowCount(t, ctx, pool, "span_attempts", 1)
	assertRowCount(t, ctx, pool, "usage_records", 1)
	assertRowCount(t, ctx, pool, "operation_results", 1)
	assertRowCount(t, ctx, pool, "run_events", 1)
	assertRowCount(t, ctx, pool, "outbox_events", 2)

	var checkpoint int64
	if err := pool.QueryRow(ctx, `SELECT checkpoint_version FROM runs WHERE id = $1`, run.ID).Scan(&checkpoint); err != nil {
		t.Fatalf("read checkpoint: %v", err)
	}
	if checkpoint != 2 {
		t.Fatalf("checkpoint = %d, want 2", checkpoint)
	}
}

func TestAppendTraceStepRollsBackWhenOutboxInsertFails(t *testing.T) {
	ctx, pool, store := newRepositoryTestStore(t)
	conversationID := createConversationFixture(t, ctx, pool)
	_, run, err := store.CreateMessageAndRun(ctx, CreateMessageParams{
		ConversationID: conversationID,
		Content:        "trace rollback",
		AgentID:        "browser-eval",
		Model:          event.ModelRef{Provider: "scripted", Name: "browser-eval-fixture"},
		IdempotencyKey: "request-1",
	})
	if err != nil {
		t.Fatalf("create message and run: %v", err)
	}
	if _, ok, err := store.ClaimRun(ctx, run.ID, 0); err != nil || !ok {
		t.Fatalf("claim run = (ok=%v, err=%v)", ok, err)
	}
	if _, err := pool.Exec(ctx, `
		ALTER TABLE outbox_events
		ADD CONSTRAINT reject_test_trace_outbox_write CHECK (event_id <> 'evt_trace_test')`); err != nil {
		t.Fatalf("reject trace outbox write: %v", err)
	}

	if err := store.AppendTraceStep(ctx, traceStepFixture(run.ID)); err == nil {
		t.Fatal("expected trace outbox failure")
	}
	assertRowCount(t, ctx, pool, "spans", 0)
	assertRowCount(t, ctx, pool, "span_attempts", 0)
	assertRowCount(t, ctx, pool, "usage_records", 0)
	assertRowCount(t, ctx, pool, "operation_results", 0)
	assertRowCount(t, ctx, pool, "run_events", 0)

	var checkpoint int64
	if err := pool.QueryRow(ctx, `SELECT checkpoint_version FROM runs WHERE id = $1`, run.ID).Scan(&checkpoint); err != nil {
		t.Fatalf("read checkpoint: %v", err)
	}
	if checkpoint != 1 {
		t.Fatalf("checkpoint = %d, want 1 after rollback", checkpoint)
	}
}

func TestAppendTraceStepRejectsMismatchedReplayPayload(t *testing.T) {
	ctx, _, store := newRepositoryTestStore(t)
	conversationID := createConversationFixture(t, ctx, store.pool)
	_, run, err := store.CreateMessageAndRun(ctx, CreateMessageParams{
		ConversationID: conversationID,
		Content:        "trace this",
		AgentID:        "browser-eval",
		Model:          event.ModelRef{Provider: "scripted", Name: "browser-eval-fixture"},
		IdempotencyKey: "request-1",
	})
	if err != nil {
		t.Fatalf("create message and run: %v", err)
	}
	if _, ok, err := store.ClaimRun(ctx, run.ID, 0); err != nil || !ok {
		t.Fatalf("claim run = (ok=%v, err=%v)", ok, err)
	}

	step := traceStepFixture(run.ID)
	if err := store.AppendTraceStep(ctx, step); err != nil {
		t.Fatalf("append trace step: %v", err)
	}
	step.Event.Payload = json.RawMessage(`{"span_id":"different"}`)
	if err := store.AppendTraceStep(ctx, step); !errors.Is(err, ErrConflict) {
		t.Fatalf("mismatched trace replay error = %v, want ErrConflict", err)
	}
}

func newRepositoryTestStore(t *testing.T) (context.Context, *pgxpool.Pool, *Store) {
	t.Helper()

	dsn := os.Getenv("AGENTTRACE_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("AGENTTRACE_TEST_POSTGRES_DSN is not configured")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect test database: %v", err)
	}
	t.Cleanup(pool.Close)
	resetDatabase(t, ctx, pool)
	if err := Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate test database: %v", err)
	}
	return ctx, pool, NewStore(pool)
}

func createConversationFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool) string {
	t.Helper()

	if _, err := pool.Exec(ctx, `INSERT INTO projects (id, name) VALUES ('prj_fixture', 'Fixture')`); err != nil {
		t.Fatalf("insert project fixture: %v", err)
	}
	const conversationID = "conv_fixture"
	if _, err := pool.Exec(ctx, `
		INSERT INTO conversations (id, project_id, title)
		VALUES ($1, 'prj_fixture', 'Fixture conversation')`, conversationID); err != nil {
		t.Fatalf("insert conversation fixture: %v", err)
	}
	return conversationID
}

func traceStepFixture(runID string) TraceStep {
	now := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	return TraceStep{
		ExpectedCheckpoint: 1,
		Span: domain.Span{
			ID:        "spn_test",
			RunID:     runID,
			TraceID:   "trc_test",
			SpanID:    "model-1",
			Kind:      domain.SpanModel,
			Name:      "Plan next action",
			Status:    "succeeded",
			StartedAt: now,
		},
		Event: event.Envelope[json.RawMessage]{
			SchemaVersion: "agenttrace.event.v1",
			EventID:       "evt_trace_test",
			EventType:     "span.completed",
			AggregateID:   runID,
			OccurredAt:    now,
			CorrelationID: runID,
			Payload:       json.RawMessage(`{"span_id":"model-1"}`),
		},
	}
}

func assertRowCount(t *testing.T, ctx context.Context, pool *pgxpool.Pool, table string, want int) {
	t.Helper()

	var got int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM `+table).Scan(&got); err != nil {
		t.Fatalf("count rows in %s: %v", table, err)
	}
	if got != want {
		t.Fatalf("%s rows = %d, want %d", table, got, want)
	}
}
