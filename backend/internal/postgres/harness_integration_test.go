package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strconv"
	"strings"
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
	ctx, pool, store := newHarnessTestStore(t)
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

	// A second delivery must reconcile the original attempt, never execute it again.
	_, err = store.BeginHarnessAttempt(ctx, run.ID, "pi-att-2")
	if !errors.Is(err, ErrRunAlreadyTerminal) {
		t.Fatalf("second harness attempt: %v, want ErrRunAlreadyTerminal", err)
	}
	finished, err := store.GetRun(ctx, run.ID)
	if err != nil || finished.Status != domain.RunFailed || finished.FinishedAt == nil {
		t.Fatalf("reconciled run = %#v, %v, want terminal failed", finished, err)
	}
	var attempts int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM harness_attempts WHERE run_id = $1`, run.ID).Scan(&attempts); err != nil {
		t.Fatal(err)
	}
	if attempts != 1 {
		t.Fatalf("attempts = %d, want original attempt only", attempts)
	}
}

func TestPiBeginHarnessAttemptRejectsTerminalRun(t *testing.T) {
	ctx, _, store := newHarnessTestStore(t)
	run := seedRunForHarness(t, ctx, store)
	attempt, err := store.BeginHarnessAttempt(ctx, run.ID, "pi-att")
	if err != nil {
		t.Fatalf("begin harness attempt: %v", err)
	}
	if _, err := store.FinishHarnessAttempt(ctx, HarnessFinish{
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

func TestPiBeginHarnessAttemptRecoveryHonorsCancellation(t *testing.T) {
	ctx, pool, store := newHarnessTestStore(t)
	run := seedRunForHarness(t, ctx, store)
	attempt, err := store.BeginHarnessAttempt(ctx, run.ID, "pi-att-cancel-recovery")
	if err != nil {
		t.Fatal(err)
	}
	// Begin may have changed cancelling to running; retain the durable flag.
	if _, err := pool.Exec(ctx, `UPDATE runs SET cancel_requested_at = now() WHERE id = $1`, run.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.BeginHarnessAttempt(ctx, run.ID, "pi-att-replay"); !errors.Is(err, ErrRunAlreadyTerminal) {
		t.Fatalf("cancelled recovery: %v", err)
	}
	finished, err := store.GetRun(ctx, run.ID)
	if err != nil || finished.Status != domain.RunCancelled || finished.FinishedAt == nil {
		t.Fatalf("recovered cancellation = %#v, %v", finished, err)
	}
	var status string
	if err := pool.QueryRow(ctx, `SELECT status FROM harness_attempts WHERE id = $1`, attempt.ID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "cancelled" {
		t.Fatalf("attempt status = %q, want cancelled", status)
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
		ID: "span-model-1", RunID: run.ID, TraceID: "trace-1", SpanID: "span-model-1",
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
		Event:     harnessEnvelope(run.ID, attempt.ID, 1, "pi.message.completed", `{"text":"hi"}`),
		Span:      &span,
		Attempt:   &trAttempt,
		Usage:     &usage,
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

	if _, err := store.FinishHarnessAttempt(ctx, HarnessFinish{
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
	var terminalRaw json.RawMessage
	if err := pool.QueryRow(ctx, `SELECT payload FROM run_events WHERE run_id = $1 AND event_type = 'run.completed'`, run.ID).Scan(&terminalRaw); err != nil {
		t.Fatalf("read application completion: %v", err)
	}
	var terminal event.Envelope[json.RawMessage]
	if err := json.Unmarshal(terminalRaw, &terminal); err != nil {
		t.Fatal(err)
	}
	var completion struct {
		RunID  string `json:"run_id"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(terminal.Payload, &completion); err != nil {
		t.Fatal(err)
	}
	if completion.RunID != run.ID || completion.Status != "failed" {
		t.Fatalf("completion = %s", terminal.Payload)
	}
	var attemptStatus string
	if err := pool.QueryRow(ctx, `SELECT status FROM harness_attempts WHERE id = $1`, attempt.ID).Scan(&attemptStatus); err != nil {
		t.Fatalf("read attempt status: %v", err)
	}
	if attemptStatus != string(domain.RunFailed) {
		t.Fatalf("attempt status = %q, want failed", attemptStatus)
	}
}

func TestPiFinishHarnessAttemptDurableCancellationWinsBlockedSuccess(t *testing.T) {
	baseCtx, pool, store := newHarnessTestStore(t)
	ctx, cancel := context.WithTimeout(baseCtx, 10*time.Second)
	defer cancel()
	run := seedRunForHarness(t, ctx, store)
	attempt, err := store.BeginHarnessAttempt(ctx, run.ID, "pi-att-cancel-race")
	if err != nil {
		t.Fatalf("begin attempt: %v", err)
	}
	insertRootSpan(t, ctx, pool, run.ID, "span-run")

	lock, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin cancellation transaction: %v", err)
	}
	defer lock.Rollback(baseCtx)
	var lockedStatus string
	if err := lock.QueryRow(ctx, `SELECT status FROM runs WHERE id = $1 FOR UPDATE`, run.ID).Scan(&lockedStatus); err != nil {
		t.Fatalf("lock run: %v", err)
	}
	var lockPID int
	if err := lock.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&lockPID); err != nil {
		t.Fatalf("read lock backend: %v", err)
	}

	finish := HarnessFinish{
		RunID: run.ID, AttemptID: attempt.ID, Status: domain.RunSucceeded,
		Assistant: "passed before cancellation",
		Event: event.Envelope[json.RawMessage]{
			SchemaVersion: "agenttrace.event.v1",
			EventID:       "evt-success-cancel-race",
			EventType:     "harness.completed",
			AggregateID:   run.ID,
			CorrelationID: run.ID,
			OccurredAt:    time.Now().UTC(),
			Payload:       json.RawMessage(`{}`),
		},
	}
	type finishResult struct {
		status domain.RunStatus
		err    error
	}
	done := make(chan finishResult, 1)
	go func() {
		status, err := store.FinishHarnessAttempt(ctx, finish)
		done <- finishResult{status: status, err: err}
	}()
	// Wait for PostgreSQL to confirm that completion reached the locked run.
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var blocked bool
		if err := pool.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM pg_stat_activity
				WHERE datname = current_database()
				  AND wait_event_type = 'Lock'
				  AND query LIKE '%SELECT status FROM runs WHERE id = $1 FOR UPDATE%'
				  AND $1::int = ANY(pg_blocking_pids(pid))
			)`, lockPID).Scan(&blocked); err != nil {
			t.Fatalf("observe blocked completion: %v", err)
		}
		if blocked {
			break
		}
		select {
		case result := <-done:
			t.Fatalf("completion returned before cancellation: %v", result.err)
		case <-ctx.Done():
			t.Fatal("completion did not block on the run lock")
		case <-ticker.C:
		}
	}
	if _, err := lock.Exec(ctx, `
		UPDATE runs SET status = 'cancelling', cancel_requested_at = now(), updated_at = now()
		WHERE id = $1`, run.ID); err != nil {
		t.Fatalf("record durable cancellation: %v", err)
	}
	if err := lock.Commit(ctx); err != nil {
		t.Fatalf("commit durable cancellation: %v", err)
	}
	result := <-done
	if result.err != nil {
		t.Fatalf("finish blocked success: %v", result.err)
	}
	if result.status != "cancelled" {
		t.Errorf("returned status = %q, want cancelled", result.status)
	}

	var runStatus, rootStatus, attemptStatus string
	var runError, terminalEnvelope, outboxEnvelope json.RawMessage
	if err := pool.QueryRow(ctx, `SELECT status, error FROM runs WHERE id = $1`, run.ID).Scan(&runStatus, &runError); err != nil {
		t.Fatalf("read completed run: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT status FROM spans WHERE run_id = $1 AND kind = 'run'`, run.ID).Scan(&rootStatus); err != nil {
		t.Fatalf("read root span: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT status FROM harness_attempts WHERE id = $1`, attempt.ID).Scan(&attemptStatus); err != nil {
		t.Fatalf("read harness attempt: %v", err)
	}
	for name, status := range map[string]string{
		"run": runStatus, "root span": rootStatus, "harness attempt": attemptStatus,
	} {
		if status != "cancelled" {
			t.Errorf("%s status = %q, want cancelled", name, status)
		}
	}
	if !strings.Contains(string(runError), "cancel") {
		t.Errorf("run error = %s, want cancellation error", runError)
	}
	if err := pool.QueryRow(ctx, `
		SELECT payload FROM run_events WHERE run_id = $1 AND event_type = 'run.completed'`,
		run.ID).Scan(&terminalEnvelope); err != nil {
		t.Fatalf("read terminal event: %v", err)
	}
	var terminal event.Envelope[struct {
		Status string          `json:"status"`
		Error  json.RawMessage `json:"error"`
	}]
	if err := json.Unmarshal(terminalEnvelope, &terminal); err != nil {
		t.Fatalf("decode terminal event: %v", err)
	}
	if terminal.Payload.Status != "cancelled" {
		t.Errorf("terminal event status = %q, want cancelled", terminal.Payload.Status)
	}
	if !strings.Contains(string(terminal.Payload.Error), "cancel") {
		t.Errorf("terminal event error = %s, want cancellation error", terminal.Payload.Error)
	}
	if string(terminal.Payload.Error) != string(runError) {
		t.Errorf("terminal event error = %s, differs from run error %s", terminal.Payload.Error, runError)
	}
	if !strings.HasSuffix(terminal.EventID, ":cancelled") {
		t.Errorf("terminal event ID = %q, want effective cancelled identity", terminal.EventID)
	}
	if err := pool.QueryRow(ctx, `SELECT payload FROM outbox_events WHERE event_id = $1`, terminal.EventID).Scan(&outboxEnvelope); err != nil {
		t.Fatalf("read terminal outbox event: %v", err)
	}
	if string(outboxEnvelope) != string(terminalEnvelope) {
		t.Errorf("outbox and durable terminal events differ")
	}

	for _, proposedStatus := range []domain.RunStatus{domain.RunSucceeded, domain.RunFailed, domain.RunCancelled} {
		replay := finish
		replay.Status = proposedStatus
		replay.Error = json.RawMessage(`{"message":"late harness outcome"}`)
		status, err := store.FinishHarnessAttempt(ctx, replay)
		if err != nil || status != "cancelled" {
			t.Fatalf("replay with proposed %q = (%q, %v), want (cancelled, nil)", proposedStatus, status, err)
		}
	}
	var terminalEvents, outboxEvents, assistantMessages int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM run_events WHERE run_id = $1 AND event_type = 'run.completed'`,
		run.ID).Scan(&terminalEvents); err != nil {
		t.Fatalf("count replayed terminal events: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM outbox_events WHERE event_id = $1`, terminal.EventID).Scan(&outboxEvents); err != nil {
		t.Fatalf("count replayed outbox events: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM messages WHERE metadata->>'run_id' = $1 AND role = 'assistant'`,
		run.ID).Scan(&assistantMessages); err != nil {
		t.Fatalf("count replayed assistant messages: %v", err)
	}
	if terminalEvents != 1 || outboxEvents != 1 || assistantMessages != 1 {
		t.Errorf("replay counts = terminal:%d outbox:%d assistant:%d, want 1 each",
			terminalEvents, outboxEvents, assistantMessages)
	}
	conflict := finish
	conflict.Event.OccurredAt = conflict.Event.OccurredAt.Add(time.Second)
	status, err := store.FinishHarnessAttempt(ctx, conflict)
	if status != "cancelled" || !errors.Is(err, ErrConflict) {
		t.Errorf("conflicting replay = (%q, %v), want (cancelled, ErrConflict)", status, err)
	}
}

func TestPiSpanLookupOrdersBySourceSequenceNotStartedAt(t *testing.T) {
	ctx, pool, store := newHarnessTestStore(t)
	run := seedRunForHarness(t, ctx, store)
	attempt, err := store.BeginHarnessAttempt(ctx, run.ID, "pi-att")
	if err != nil {
		t.Fatalf("begin attempt: %v", err)
	}
	insertRootSpan(t, ctx, pool, run.ID, "span-run")

	// Two model spans share the same started_at (clock skew / fast emission)
	// but were produced at different source sequences. The latest-by-sequence
	// span must win regardless of started_at ordering.
	now := time.Now().UTC()
	mkSpan := func(id string, seq int64) domain.Span {
		return domain.Span{
			ID: id, RunID: run.ID, TraceID: "trace-1", SpanID: id,
			ParentSpanID: "span-run", Kind: domain.SpanModel, Name: "message",
			Status: "succeeded", StartedAt: now, EndedAt: &now,
		}
	}
	mkAttempt := func(id string) TraceAttempt {
		return TraceAttempt{
			ID: id, AttemptNumber: 1, Provider: "model", Status: "succeeded",
			StartedAt: now, EndedAt: &now,
		}
	}

	// Insert sequence 1 first, then sequence 2.
	span1 := mkSpan("span-model-seq1", 1)
	att1 := mkAttempt("att-model-seq1")
	if err := store.AppendHarnessEvent(ctx, HarnessEventRecord{
		RunID: run.ID, AttemptID: attempt.ID, SourceSequence: 1,
		SourceType: "pi.message.completed",
		Event:      harnessEnvelope(run.ID, attempt.ID, 1, "pi.message.completed", `{"text":"first"}`),
		Span:       &span1, Attempt: &att1,
	}); err != nil {
		t.Fatalf("append seq1: %v", err)
	}
	span2 := mkSpan("span-model-seq2", 2)
	att2 := mkAttempt("att-model-seq2")
	if err := store.AppendHarnessEvent(ctx, HarnessEventRecord{
		RunID: run.ID, AttemptID: attempt.ID, SourceSequence: 2,
		SourceType: "pi.message.completed",
		Event:      harnessEnvelope(run.ID, attempt.ID, 2, "pi.message.completed", `{"text":"second"}`),
		Span:       &span2, Attempt: &att2,
	}); err != nil {
		t.Fatalf("append seq2: %v", err)
	}

	latest, err := store.LatestModelSpanID(ctx, run.ID)
	if err != nil {
		t.Fatalf("latest model span: %v", err)
	}
	if latest != "span-model-seq2" {
		t.Fatalf("latest model span = %q, want span-model-seq2 (ordered by source_sequence)", latest)
	}

	// Two operation (tool) spans also share started_at; the active one must be
	// the latest by source sequence.
	tool1 := domain.Span{
		ID: "span-tool-seq3", RunID: run.ID, TraceID: "trace-1", SpanID: "span-tool-seq3",
		ParentSpanID: "span-run", Kind: domain.SpanOperation, Name: "browser",
		Status: "started", StartedAt: now,
	}
	toolAtt1 := TraceAttempt{ID: "att-tool-seq3", AttemptNumber: 1, Status: "started", StartedAt: now}
	if err := store.AppendHarnessEvent(ctx, HarnessEventRecord{
		RunID: run.ID, AttemptID: attempt.ID, SourceSequence: 3,
		SourceType: "pi.tool.started",
		Event:      harnessEnvelope(run.ID, attempt.ID, 3, "pi.tool.started", `{"tool_call_id":"tc1"}`),
		Span:       &tool1, Attempt: &toolAtt1,
	}); err != nil {
		t.Fatalf("append tool seq3: %v", err)
	}
	tool2 := domain.Span{
		ID: "span-tool-seq4", RunID: run.ID, TraceID: "trace-1", SpanID: "span-tool-seq4",
		ParentSpanID: "span-run", Kind: domain.SpanOperation, Name: "browser",
		Status: "started", StartedAt: now,
	}
	toolAtt2 := TraceAttempt{ID: "att-tool-seq4", AttemptNumber: 1, Status: "started", StartedAt: now}
	if err := store.AppendHarnessEvent(ctx, HarnessEventRecord{
		RunID: run.ID, AttemptID: attempt.ID, SourceSequence: 4,
		SourceType: "pi.tool.started",
		Event:      harnessEnvelope(run.ID, attempt.ID, 4, "pi.tool.started", `{"tool_call_id":"tc2"}`),
		Span:       &tool2, Attempt: &toolAtt2,
	}); err != nil {
		t.Fatalf("append tool seq4: %v", err)
	}

	active, err := store.ActiveToolSpanID(ctx, run.ID)
	if err != nil {
		t.Fatalf("active tool span: %v", err)
	}
	if active != "span-tool-seq4" {
		t.Fatalf("active tool span = %q, want span-tool-seq4 (ordered by source_sequence)", active)
	}
}

func TestPiCompletedToolAttemptLookupSelectsExactNameAndSourceSequence(t *testing.T) {
	ctx, pool, store := newHarnessTestStore(t)
	run := seedRunForHarness(t, ctx, store)
	harnessAttempt, err := store.BeginHarnessAttempt(ctx, run.ID, "pi-att")
	if err != nil {
		t.Fatalf("begin attempt: %v", err)
	}
	insertRootSpan(t, ctx, pool, run.ID, "span-run")

	now := time.Now().UTC()
	appendCompletedTool := func(
		harnessAttemptID string,
		sequence int64,
		spanID, attemptID, operationID, name string,
	) {
		t.Helper()
		span := domain.Span{
			ID: spanID, RunID: run.ID, TraceID: "trace-1", SpanID: spanID,
			ParentSpanID: "span-run", Kind: domain.SpanOperation, Name: name,
			Status: "succeeded", StartedAt: now, EndedAt: &now,
		}
		attempt := TraceAttempt{
			ID: attemptID, AttemptNumber: 1, OperationID: operationID,
			Status: "succeeded", StartedAt: now, EndedAt: &now,
		}
		if err := store.AppendHarnessEvent(ctx, HarnessEventRecord{
			RunID: run.ID, AttemptID: harnessAttemptID, SourceSequence: sequence,
			SourceType: "pi.tool.completed",
			Event: harnessEnvelope(
				run.ID, harnessAttemptID, sequence, "pi.tool.completed", `{"is_error":false}`,
			),
			Span: &span, Attempt: &attempt,
		}); err != nil {
			t.Fatalf("append completed tool %d: %v", sequence, err)
		}
	}

	const finishTool = "mcp__browser_harness__finish_case"
	appendCompletedTool(
		harnessAttempt.ID,
		1,
		"span-finish",
		"attempt-finish",
		"operation-finish",
		finishTool,
	)
	appendCompletedTool(
		harnessAttempt.ID,
		2,
		"span-click",
		"attempt-click",
		"operation-click",
		"mcp__browser_harness__click",
	)

	got, err := store.LatestCompletedToolAttempt(
		ctx, run.ID, harnessAttempt.ID, finishTool, 3,
	)
	if err != nil {
		t.Fatalf("latest completed finish tool: %v", err)
	}
	if got.SpanID != "span-finish" ||
		got.SpanAttemptID != "attempt-finish" ||
		got.OperationID != "operation-finish" {
		t.Fatalf("completed finish reference = %#v", got)
	}

	appendCompletedTool(
		harnessAttempt.ID,
		3,
		"span-finish-latest",
		"attempt-finish-latest",
		"operation-finish-latest",
		finishTool,
	)
	latest, err := store.LatestCompletedToolAttempt(
		ctx, run.ID, harnessAttempt.ID, finishTool, 4,
	)
	if err != nil {
		t.Fatalf("latest completed finish tool after second finish: %v", err)
	}
	if latest.SpanID != "span-finish-latest" ||
		latest.SpanAttemptID != "attempt-finish-latest" ||
		latest.OperationID != "operation-finish-latest" {
		t.Fatalf("latest completed finish reference = %#v", latest)
	}

	missing, err := store.LatestCompletedToolAttempt(
		ctx, run.ID, harnessAttempt.ID, finishTool, 1,
	)
	if err != nil {
		t.Fatalf("lookup before finish sequence: %v", err)
	}
	if missing != (CompletedToolAttempt{}) {
		t.Fatalf("lookup before finish sequence = %#v, want empty", missing)
	}

	// Seed a historical second attempt to exercise lookup isolation, not replay.
	retryAttempt := HarnessAttempt{ID: "hat-historical"}
	if _, err := pool.Exec(ctx, `
		INSERT INTO harness_attempts (id, run_id, attempt_number, provider_attempt_id, status, started_at)
		VALUES ($1, $2, 2, 'pi-att-historical', 'active', now())`,
		retryAttempt.ID, run.ID); err != nil {
		t.Fatalf("seed historical attempt: %v", err)
	}
	appendCompletedTool(
		retryAttempt.ID,
		1,
		"span-retry-click",
		"attempt-retry-click",
		"operation-retry-click",
		"mcp__browser_harness__click",
	)
	missingCurrentFinish, err := store.LatestCompletedToolAttempt(
		ctx, run.ID, retryAttempt.ID, finishTool, 2,
	)
	if err != nil {
		t.Fatalf("lookup finish in retry attempt: %v", err)
	}
	if missingCurrentFinish != (CompletedToolAttempt{}) {
		t.Fatalf(
			"retry lookup reused prior attempt finish = %#v, want empty",
			missingCurrentFinish,
		)
	}
}

func TestPiCrashRedeliveryReconcilesWithoutOverwritingEvidence(t *testing.T) {
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
		Span:  &span1, Attempt: &att1,
	}); err != nil {
		t.Fatalf("append attempt 1 event: %v", err)
	}

	// A failed terminal write leaves side effects and evidence behind.
	if _, err := pool.Exec(ctx, `
		CREATE FUNCTION reject_terminal_write() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN
			IF NEW.status IN ('succeeded', 'failed', 'cancelled') THEN
				RAISE EXCEPTION 'terminal persistence unavailable';
			END IF;
			RETURN NEW;
		END $$;
		CREATE TRIGGER reject_terminal_write BEFORE UPDATE ON runs
		FOR EACH ROW EXECUTE FUNCTION reject_terminal_write()`); err != nil {
		t.Fatal(err)
	}
	_, err = store.FinishHarnessAttempt(ctx, HarnessFinish{
		RunID: run.ID, AttemptID: attempt1.ID, Status: domain.RunSucceeded,
		Event: harnessEnvelope(run.ID, attempt1.ID, 2, event.RunCompleted, `{}`),
	})
	if err == nil {
		t.Fatal("expected terminal persistence failure")
	}
	// Recovery itself can fail; no new attempt may be allocated in that case.
	restarted := NewStore(pool)
	if _, err := restarted.BeginHarnessAttempt(ctx, run.ID, "pi-att-recovery-failed"); err == nil || errors.Is(err, ErrRunAlreadyTerminal) {
		t.Fatalf("failed reconciliation = %v, want retryable persistence error", err)
	}
	if _, err := pool.Exec(ctx, `DROP TRIGGER reject_terminal_write ON runs`); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, err := restarted.BeginHarnessAttempt(ctx, run.ID, "pi-att-redelivery"); !errors.Is(err, ErrRunAlreadyTerminal) {
			t.Fatalf("restart redelivery = %v, want terminal without new attempt", err)
		}
	}

	var modelSpans int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM spans WHERE run_id = $1 AND kind = 'model'`, run.ID).Scan(&modelSpans); err != nil {
		t.Fatalf("count model spans: %v", err)
	}
	if modelSpans != 1 {
		t.Fatalf("model spans = %d, want 1 (prior evidence preserved, no replay)", modelSpans)
	}

	var attempt1Events, attempts, terminalEvents int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM run_events WHERE harness_attempt_id = $1`, attempt1.ID).Scan(&attempt1Events); err != nil {
		t.Fatalf("count attempt 1 events: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM harness_attempts WHERE run_id = $1`, run.ID).Scan(&attempts); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM run_events WHERE run_id = $1 AND event_type = 'run.completed'`, run.ID).Scan(&terminalEvents); err != nil {
		t.Fatal(err)
	}
	if attempt1Events != 1 || attempts != 1 || terminalEvents != 1 {
		t.Fatalf("evidence / attempts / terminal events = %d / %d / %d, want 1 each", attempt1Events, attempts, terminalEvents)
	}
	var status, rootStatus string
	var finishedAt *time.Time
	var runError json.RawMessage
	if err := pool.QueryRow(ctx, `SELECT status, finished_at FROM harness_attempts WHERE id = $1`, attempt1.ID).Scan(&status, &finishedAt); err != nil {
		t.Fatal(err)
	}
	if status != "failed" || finishedAt == nil {
		t.Fatalf("original attempt = %s / %v, want terminal failed", status, finishedAt)
	}
	if err := pool.QueryRow(ctx, `SELECT status FROM spans WHERE run_id = $1 AND kind = 'run'`, run.ID).Scan(&rootStatus); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT error FROM runs WHERE id = $1`, run.ID).Scan(&runError); err != nil {
		t.Fatal(err)
	}
	if rootStatus != "failed" || !strings.Contains(string(runError), "needs_reconciliation") {
		t.Fatalf("root / error = %s / %s, want failed reconciliation", rootStatus, runError)
	}
	var terminal, outbox json.RawMessage
	if err := pool.QueryRow(ctx, `SELECT payload FROM run_events WHERE run_id = $1 AND event_type = 'run.completed'`, run.ID).Scan(&terminal); err != nil {
		t.Fatal(err)
	}
	var envelope event.Envelope[event.RunCompletion]
	if err := json.Unmarshal(terminal, &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Payload.Status != domain.RunFailed || !envelope.OccurredAt.Equal(attempt1.StartedAt) {
		t.Fatalf("recovery completion = %s, want failed with stable occurrence time", terminal)
	}
	if err := pool.QueryRow(ctx, `SELECT payload FROM outbox_events WHERE event_id = $1`, envelope.EventID).Scan(&outbox); err != nil {
		t.Fatal(err)
	}
	if string(outbox) != string(terminal) {
		t.Fatal("recovery completion not durably queued for UI")
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
