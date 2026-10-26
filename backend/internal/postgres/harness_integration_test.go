package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/domain"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/event"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/testpostgres"
	"github.com/jackc/pgx/v5/pgxpool"
)

func newHarnessTestStore(t *testing.T) (context.Context, *pgxpool.Pool, *Store) {
	t.Helper()
	dsn := os.Getenv("AGENTTRACE_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("AGENTTRACE_TEST_POSTGRES_DSN is not configured")
	}
	if _, err := testpostgres.DatabaseName(dsn); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect PostgreSQL: %v", err)
	}
	t.Cleanup(pool.Close)
	if _, err := pool.Exec(ctx, `DROP SCHEMA public CASCADE; CREATE SCHEMA public`); err != nil {
		t.Fatalf("reset PostgreSQL: %v", err)
	}
	if err := Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate PostgreSQL: %v", err)
	}
	return ctx, pool, NewStore(pool)
}

func seedRunForHarness(t *testing.T, ctx context.Context, store *Store) domain.Run {
	t.Helper()
	project, err := store.CreateProject(ctx, CreateProjectParams{Name: "harness"})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	conversation, err := store.CreateConversation(ctx, CreateConversationParams{
		ProjectID: project.ID, Title: "harness",
	})
	if err != nil {
		t.Fatalf("create conversation: %v", err)
	}
	_, run, err := store.CreateMessageAndRun(ctx, CreateMessageParams{
		ConversationID: conversation.ID,
		Content:        "run the harness",
		AgentID:        "browser-eval",
		Model:          event.ModelRef{Provider: "pi", Name: "browser-model"},
		IdempotencyKey: "harness-" + t.Name(),
	})
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	return run
}

func TestPiBeginHarnessAttemptClaimsRunAndAllocatesAttempt(t *testing.T) {
	ctx, _, store := newHarnessTestStore(t)
	run := seedRunForHarness(t, ctx, store)

	attempt, err := store.BeginHarnessAttempt(ctx, run.ID, "pi-att-1")
	if err != nil {
		t.Fatalf("begin harness attempt: %v", err)
	}
	if attempt.AttemptNumber != 1 {
		t.Fatalf("attempt number = %d, want 1", attempt.AttemptNumber)
	}
	if attempt.Status != "active" {
		t.Fatalf("attempt status = %q, want active", attempt.Status)
	}
	claimed, err := store.GetRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	if claimed.Status != domain.RunRunning || claimed.StartedAt == nil {
		t.Fatalf("claimed run = %#v, want running with started_at", claimed)
	}

	// A second delivery (crash + redelivery) allocates attempt 2.
	attempt2, err := store.BeginHarnessAttempt(ctx, run.ID, "pi-att-2")
	if err != nil {
		t.Fatalf("second harness attempt: %v", err)
	}
	if attempt2.AttemptNumber != 2 {
		t.Fatalf("second attempt number = %d, want 2", attempt2.AttemptNumber)
	}
}

func TestPiBeginHarnessAttemptRejectsTerminalRun(t *testing.T) {
	ctx, _, store := newHarnessTestStore(t)
	run := seedRunForHarness(t, ctx, store)
	attempt, err := store.BeginHarnessAttempt(ctx, run.ID, "pi-att")
	if err != nil {
		t.Fatalf("begin harness attempt: %v", err)
	}
	if err := store.FinishHarnessAttempt(ctx, HarnessFinish{
		RunID: run.ID, AttemptID: attempt.ID, Status: domain.RunSucceeded,
		Event: event.Envelope[json.RawMessage]{
			SchemaVersion: "agenttrace.event.v1",
			EventID:       "evt-terminal",
			EventType:     "harness.completed",
			AggregateID:   run.ID,
			CorrelationID: run.ID,
			OccurredAt:    time.Now().UTC(),
			Payload:       json.RawMessage(`{}`),
		},
	}); err != nil {
		t.Fatalf("finish run: %v", err)
	}

	_, err = store.BeginHarnessAttempt(ctx, run.ID, "pi-att-replay")
	if !errors.Is(err, ErrRunAlreadyTerminal) {
		t.Fatalf("begin on terminal run = %v, want ErrRunAlreadyTerminal", err)
	}
}

func TestPiAppendHarnessEventValidatesSequenceAndReplay(t *testing.T) {
	ctx, _, store := newHarnessTestStore(t)
	run := seedRunForHarness(t, ctx, store)
	attempt, err := store.BeginHarnessAttempt(ctx, run.ID, "pi-att")
	if err != nil {
		t.Fatalf("begin attempt: %v", err)
	}

	event1 := HarnessEventRecord{
		RunID:          run.ID,
		AttemptID:      attempt.ID,
		SourceSequence: 1,
		SourceType:     "harness.started",
		Event:          harnessEnvelope(run.ID, attempt.ID, 1, "harness.started", `{}`),
	}
	if err := store.AppendHarnessEvent(ctx, event1); err != nil {
		t.Fatalf("append first event: %v", err)
	}

	// Exact replay is a no-op.
	if err := store.AppendHarnessEvent(ctx, event1); err != nil {
		t.Fatalf("replay first event = %v, want nil", err)
	}

	// Same source identity, different payload is a conflict.
	conflict := event1
	conflict.Event = harnessEnvelope(run.ID, attempt.ID, 1, "harness.started", `{"changed":true}`)
	if !errors.Is(store.AppendHarnessEvent(ctx, conflict), ErrConflict) {
		t.Fatalf("conflict replay did not return ErrConflict")
	}

	// Out-of-order sequence is a conflict.
	late := HarnessEventRecord{
		RunID:          run.ID,
		AttemptID:      attempt.ID,
		SourceSequence: 3,
		SourceType:     "pi.message.completed",
		Event:          harnessEnvelope(run.ID, attempt.ID, 3, "pi.message.completed", `{}`),
	}
	if !errors.Is(store.AppendHarnessEvent(ctx, late), ErrConflict) {
		t.Fatalf("out-of-order sequence did not return ErrConflict")
	}

	// The correct next sequence succeeds.
	event2 := HarnessEventRecord{
		RunID:          run.ID,
		AttemptID:      attempt.ID,
		SourceSequence: 2,
		SourceType:     "pi.message.completed",
		Event:          harnessEnvelope(run.ID, attempt.ID, 2, "pi.message.completed", `{"text":"hi"}`),
	}
	if err := store.AppendHarnessEvent(ctx, event2); err != nil {
		t.Fatalf("append second event: %v", err)
	}
}

func TestPiAppendHarnessEventPersistsSpanAttemptUsageAndOperation(t *testing.T) {
	ctx, pool, store := newHarnessTestStore(t)
	run := seedRunForHarness(t, ctx, store)
	attempt, err := store.BeginHarnessAttempt(ctx, run.ID, "pi-att")
	if err != nil {
		t.Fatalf("begin attempt: %v", err)
	}

	now := time.Now().UTC()
	span := domain.Span{
		ID:        "span-model-1", RunID: run.ID, TraceID: "trace-1", SpanID: "span-model-1",
		ParentSpanID: "span-run", Kind: domain.SpanModel, Name: "message", Status: "succeeded",
		StartedAt: now, EndedAt: &now,
	}
	trAttempt := TraceAttempt{
		ID: "att-model-1", AttemptNumber: 1, Provider: "model", Status: "succeeded",
		StartedAt: now, EndedAt: &now,
	}
	usage := TraceUsage{ID: "usage-1", Provider: "model", Model: "m"}
	usage.Usage.PromptTokens = 10
	usage.Usage.CompletionTokens = 5
	op := TraceOperationResult{
		ID: "op-1", OperationID: "opid-1", Status: "succeeded",
		Result: json.RawMessage(`{"ok":true}`),
	}

	insertRootSpan(t, ctx, pool, run.ID, "span-run")

	if err := store.AppendHarnessEvent(ctx, HarnessEventRecord{
		RunID: run.ID, AttemptID: attempt.ID, SourceSequence: 1, SourceType: "pi.message.completed",
		Event:   harnessEnvelope(run.ID, attempt.ID, 1, "pi.message.completed", `{"text":"hi"}`),
		Span:    &span,
		Attempt: &trAttempt,
		Usage:   &usage,
		Operation: &op,
	}); err != nil {
		t.Fatalf("append event with evidence: %v", err)
	}

	var spans, attempts, usages, operations int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM spans WHERE run_id = $1`, run.ID).Scan(&spans); err != nil {
		t.Fatalf("count spans: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM span_attempts WHERE id = $1`, trAttempt.ID).Scan(&attempts); err != nil {
		t.Fatalf("count attempts: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM usage_records WHERE id = $1`, usage.ID).Scan(&usages); err != nil {
		t.Fatalf("count usage: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM operation_results WHERE id = $1`, op.ID).Scan(&operations); err != nil {
		t.Fatalf("count operations: %v", err)
	}
	if spans != 2 || attempts != 1 || usages != 1 || operations != 1 {
		t.Fatalf("evidence counts = spans:%d attempts:%d usage:%d ops:%d", spans, attempts, usages, operations)
	}
}

func TestPiFinishHarnessAttemptSetsTerminalStateAndRootSpan(t *testing.T) {
	ctx, pool, store := newHarnessTestStore(t)
	run := seedRunForHarness(t, ctx, store)
	attempt, err := store.BeginHarnessAttempt(ctx, run.ID, "pi-att")
	if err != nil {
		t.Fatalf("begin attempt: %v", err)
	}
	now := time.Now().UTC()
	insertRootSpan(t, ctx, pool, run.ID, "span-run")

	if err := store.FinishHarnessAttempt(ctx, HarnessFinish{
		RunID: run.ID, AttemptID: attempt.ID, Status: domain.RunFailed,
		Error:     json.RawMessage(`{"message":"boom"}`),
		Assistant: "assistant text",
		Event: event.Envelope[json.RawMessage]{
			SchemaVersion: "agenttrace.event.v1",
			EventID:       "evt-terminal-1",
			EventType:     "harness.completed",
			AggregateID:   run.ID,
			CorrelationID: run.ID,
			OccurredAt:    now,
			Payload:       json.RawMessage(`{"message":"boom"}`),
		},
	}); err != nil {
		t.Fatalf("finish attempt: %v", err)
	}

	finished, err := store.GetRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	if finished.Status != domain.RunFailed || finished.FinishedAt == nil {
		t.Fatalf("finished run = %#v, want failed with finished_at", finished)
	}
	var rootStatus string
	if err := pool.QueryRow(ctx, `SELECT status FROM spans WHERE run_id = $1 AND kind = 'run'`, run.ID).Scan(&rootStatus); err != nil {
		t.Fatalf("read root span status: %v", err)
	}
	if rootStatus != string(domain.RunFailed) {
		t.Fatalf("root span status = %q, want failed", rootStatus)
	}
	var attemptStatus string
	if err := pool.QueryRow(ctx, `SELECT status FROM harness_attempts WHERE id = $1`, attempt.ID).Scan(&attemptStatus); err != nil {
		t.Fatalf("read attempt status: %v", err)
	}
	if attemptStatus != string(domain.RunFailed) {
		t.Fatalf("attempt status = %q, want failed", attemptStatus)
	}
}

func TestPiCrashRedeliveryAllocatesNewAttemptWithoutOverwritingEvidence(t *testing.T) {
	ctx, pool, store := newHarnessTestStore(t)
	run := seedRunForHarness(t, ctx, store)

	attempt1, err := store.BeginHarnessAttempt(ctx, run.ID, "pi-att-1")
	if err != nil {
		t.Fatalf("begin attempt 1: %v", err)
	}
	now := time.Now().UTC()
	insertRootSpan(t, ctx, pool, run.ID, "span-run")
	// First attempt writes a model span.
	span1 := domain.Span{
		ID: "span-m1", RunID: run.ID, TraceID: "trace-1", SpanID: "span-m1",
		ParentSpanID: "span-run", Kind: domain.SpanModel, Name: "message", Status: "succeeded",
		StartedAt: now, EndedAt: &now,
	}
	att1 := TraceAttempt{ID: "att-m1", AttemptNumber: 1, Provider: "model", Status: "succeeded", StartedAt: now, EndedAt: &now}
	if err := store.AppendHarnessEvent(ctx, HarnessEventRecord{
		RunID: run.ID, AttemptID: attempt1.ID, SourceSequence: 1, SourceType: "pi.message.completed",
		Event: harnessEnvelope(run.ID, attempt1.ID, 1, "pi.message.completed", `{"text":"first"}`),
		Span: &span1, Attempt: &att1,
	}); err != nil {
		t.Fatalf("append attempt 1 event: %v", err)
	}

	// Process crashes; redelivery allocates attempt 2.
	attempt2, err := store.BeginHarnessAttempt(ctx, run.ID, "pi-att-2")
	if err != nil {
		t.Fatalf("begin attempt 2: %v", err)
	}
	if attempt2.AttemptNumber != 2 {
		t.Fatalf("attempt 2 number = %d, want 2", attempt2.AttemptNumber)
	}
	// Attempt 2 writes its own model span with a different identity.
	span2 := span1
	span2.ID = "span-m2"
	span2.SpanID = "span-m2"
	att2 := att1
	att2.ID = "att-m2"
	if err := store.AppendHarnessEvent(ctx, HarnessEventRecord{
		RunID: run.ID, AttemptID: attempt2.ID, SourceSequence: 1, SourceType: "pi.message.completed",
		Event: harnessEnvelope(run.ID, attempt2.ID, 1, "pi.message.completed", `{"text":"second"}`),
		Span: &span2, Attempt: &att2,
	}); err != nil {
		t.Fatalf("append attempt 2 event: %v", err)
	}

	var modelSpans int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM spans WHERE run_id = $1 AND kind = 'model'`, run.ID).Scan(&modelSpans); err != nil {
		t.Fatalf("count model spans: %v", err)
	}
	if modelSpans != 2 {
		t.Fatalf("model spans = %d, want 2 (prior evidence preserved)", modelSpans)
	}

	// Each attempt has exactly one source sequence 1 event.
	var attempt1Events, attempt2Events int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM run_events WHERE harness_attempt_id = $1`, attempt1.ID).Scan(&attempt1Events); err != nil {
		t.Fatalf("count attempt 1 events: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM run_events WHERE harness_attempt_id = $1`, attempt2.ID).Scan(&attempt2Events); err != nil {
		t.Fatalf("count attempt 2 events: %v", err)
	}
	if attempt1Events != 1 || attempt2Events != 1 {
		t.Fatalf("attempt events = %d / %d, want 1 / 1", attempt1Events, attempt2Events)
	}
}

func harnessEnvelope(runID, attemptID string, seq int64, eventType, payload string) event.Envelope[json.RawMessage] {
	return event.Envelope[json.RawMessage]{
		SchemaVersion: "agenttrace.event.v1",
		EventID:       "evt-" + runID + "-" + attemptID + "-" + itoa(seq),
		EventType:     eventType,
		AggregateID:   runID,
		CorrelationID: runID,
		OccurredAt:    time.Now().UTC(),
		Payload:       json.RawMessage(payload),
	}
}

func itoa(i int64) string { return strconv.FormatInt(i, 10) }

func insertRootSpan(t *testing.T, ctx context.Context, pool *pgxpool.Pool, runID, spanID string) {
	t.Helper()
	now := time.Now().UTC()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin root span tx: %v", err)
	}
	if _, err := insertStartedSpan(ctx, tx, domain.Span{
		ID: spanID, RunID: runID, TraceID: "trace-1", SpanID: spanID,
		Kind: domain.SpanRun, Name: "run", Status: "running", StartedAt: now,
	}); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("insert root span: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit root span: %v", err)
	}
}
