package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/domain"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/event"
)

func TestManagedCreateAndRerunOutboxFailureRollsBack(t *testing.T) {
	for _, rerun := range []bool{false, true} {
		name := "create"
		if rerun {
			name = "rerun"
		}
		t.Run(name, func(t *testing.T) {
			ctx, pool, s := newRepositoryTestStore(t)
			conversation := createConversationFixture(t, ctx, pool)
			p := managedMessageParams(t, ctx, s, CreateMessageParams{
				ConversationID: conversation, Content: "atomic", IdempotencyKey: name,
				Model: event.ModelRef{Provider: "scripted", Name: "fixture"},
			})
			var source domain.Run
			want := 0
			if rerun {
				_, source = createFailedRerunSource(t, ctx, s, conversation, "source")
				want = 1
			}
			if _, err := pool.Exec(ctx, `ALTER TABLE outbox_events ADD CONSTRAINT reject_new_outbox CHECK (false) NOT VALID`); err != nil {
				t.Fatal(err)
			}
			var err error
			if rerun {
				_, err = s.CreateRerun(ctx, CreateRerunParams{RunID: source.ID, IdempotencyKey: "atomic"})
			} else {
				_, _, err = s.CreateMessageAndRun(ctx, p)
			}
			if err == nil {
				t.Fatal("outbox failure was ignored")
			}
			for _, table := range []string{"runs", "messages", "idempotency_keys", "outbox_events"} {
				assertRowCount(t, ctx, pool, table, want)
			}
		})
	}
}

func TestManagedRerunPinsSnapshotAndReplaysBeforeReadiness(t *testing.T) {
	ctx, pool, s := newRepositoryTestStore(t)
	conversation := createConversationFixture(t, ctx, pool)
	_, source := createFailedRerunSource(t, ctx, s, conversation, "source")
	var original runSpecification
	if err := json.Unmarshal(source.Specification, &original); err != nil {
		t.Fatal(err)
	}
	if err := s.RevokeProjectHarness(ctx, source.ProjectID, original.Execution.Harness.VersionID, "admin"); err != nil {
		t.Fatal(err)
	}
	p := CreateRerunParams{RunID: source.ID, IdempotencyKey: "pinned"}
	rerun, err := s.CreateRerun(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	var copied runSpecification
	if err := json.Unmarshal(rerun.Specification, &copied); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(original.Execution, copied.Execution) || copied.Command.RunID != rerun.ID ||
		copied.Command.CheckpointVersion != 0 || rerun.RetryOfRunID != source.ID || rerun.SourceMessageID != source.SourceMessageID {
		t.Fatalf("rerun changed immutable execution/lineage: %+v", copied)
	}
	if _, err := pool.Exec(ctx, `UPDATE environments SET state='needs_review' WHERE conversation_id=$1`, conversation); err != nil {
		t.Fatal(err)
	}
	replay, err := s.CreateRerun(ctx, p)
	if err != nil || replay.ID != rerun.ID {
		t.Fatalf("matching rerun replay consulted readiness: %+v %v", replay, err)
	}
	assertRowCount(t, ctx, pool, "messages", 1)
	assertRowCount(t, ctx, pool, "runs", 2)
	assertRowCount(t, ctx, pool, "outbox_events", 2)
}

func TestManagedEvidenceOutboxFailureRollsBackAndReplayIsExact(t *testing.T) {
	ctx, pool, s := newHarnessTestStore(t)
	run := seedRunForHarness(t, ctx, s)
	attempt, err := claimFixtureAttempt(t, ctx, s, run.ID, "atomic-evidence")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	record := HarnessEventRecord{
		RunID: run.ID, AttemptID: attempt.ID, Fence: fixtureFence(t, ctx, s, run.ID, attempt.ID),
		SourceSequence: 1, SourceType: "pi.message.completed",
		Event: harnessEnvelope(run.ID, attempt.ID, 1, "pi.message.completed", `{"text":"done"}`),
		Span: &domain.Span{
			ID: "atomic-span", RunID: run.ID, TraceID: "atomic-trace", SpanID: "atomic-span",
			Kind: domain.SpanModel, Name: "message", Status: "succeeded", StartedAt: now, EndedAt: &now,
		},
		Attempt:   &TraceAttempt{ID: "atomic-model", AttemptNumber: 1, Provider: "scripted", Status: "succeeded", StartedAt: now, EndedAt: &now},
		Usage:     &TraceUsage{ID: "atomic-usage", Provider: "scripted", Model: "fixture"},
		Operation: &TraceOperationResult{ID: "atomic-operation", OperationID: "atomic-call", Status: "succeeded", Result: json.RawMessage(`{"ok":true}`)},
	}
	if _, err := pool.Exec(ctx, `ALTER TABLE outbox_events ADD CONSTRAINT reject_new_outbox CHECK (false) NOT VALID`); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendHarnessEvent(ctx, record); err == nil {
		t.Fatal("evidence outbox failure was ignored")
	}
	for _, table := range []string{"spans", "span_attempts", "usage_records", "operation_results", "run_events"} {
		assertRowCount(t, ctx, pool, table, 0)
	}
	assertRowCount(t, ctx, pool, "outbox_events", 1)
	if _, err := pool.Exec(ctx, `ALTER TABLE outbox_events DROP CONSTRAINT reject_new_outbox`); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := s.AppendHarnessEvent(ctx, record); err != nil {
			t.Fatal(err)
		}
	}
	for _, table := range []string{"spans", "span_attempts", "usage_records", "operation_results", "run_events"} {
		assertRowCount(t, ctx, pool, table, 1)
	}
	record.Event.Payload = json.RawMessage(`{"text":"changed"}`)
	if err := s.AppendHarnessEvent(ctx, record); !errors.Is(err, ErrConflict) {
		t.Fatalf("changed evidence replay accepted: %v", err)
	}
}

func TestManagedCompletionAndMessageUseConversationLockAndUniqueOrdinals(t *testing.T) {
	base, pool, s := newHarnessTestStore(t)
	ctx, cancel := context.WithTimeout(base, 10*time.Second)
	defer cancel()
	run := seedRunForHarness(t, ctx, s)
	p := managedMessageParams(t, ctx, s, CreateMessageParams{
		ConversationID: run.ConversationID, Content: "next goal", IdempotencyKey: "next",
		Model: event.ModelRef{Provider: "scripted", Name: "fixture"},
	})
	attempt, err := claimFixtureAttempt(t, ctx, s, run.ID, "ordinal")
	if err != nil {
		t.Fatal(err)
	}
	fence := fixtureFence(t, ctx, s, run.ID, attempt.ID)
	finish := HarnessFinish{RunID: run.ID, AttemptID: attempt.ID, Fence: fence, Status: domain.RunSucceeded,
		Assistant: "done", Event: harnessEnvelope(run.ID, attempt.ID, 1, event.RunCompleted, `{}`)}
	holder, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Rollback(base)
	if _, err := lockConversation(ctx, holder, run.ConversationID); err != nil {
		t.Fatal(err)
	}
	finished, created := make(chan error, 2), make(chan error, 1)
	for range 2 {
		go func() {
			_, err := s.FinishHarnessAttempt(ctx, finish)
			finished <- err
		}()
	}
	go func() {
		_, _, err := s.CreateMessageAndRun(ctx, p)
		created <- err
	}()
	for {
		var waiting int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM pg_stat_activity
			WHERE datname=current_database() AND wait_event_type='Lock'`).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting >= 3 {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("completion and creation did not reach conversation lock")
		case <-time.After(10 * time.Millisecond):
		}
	}
	if err := holder.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := <-finished; err != nil {
			t.Fatalf("concurrent completion: %v", err)
		}
	}
	if err := <-created; !errors.Is(err, ErrConflict) {
		t.Fatalf("message must wait for browser release: %v", err)
	}
	if err := s.ReleaseHarnessLease(ctx, fence, true); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.CreateMessageAndRun(ctx, p); err != nil {
		t.Fatalf("message after acknowledged release: %v", err)
	}
	var messages, ordinals, assistants, completions int
	if err := pool.QueryRow(ctx, `SELECT count(*),count(DISTINCT ordinal),count(*) FILTER (WHERE role='assistant') FROM messages
		WHERE conversation_id=$1`, run.ConversationID).Scan(&messages, &ordinals, &assistants); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM run_events WHERE run_id=$1 AND event_type='run.completed'`, run.ID).Scan(&completions); err != nil {
		t.Fatal(err)
	}
	if messages != 3 || ordinals != 3 || assistants != 1 || completions != 1 {
		t.Fatalf("messages/ordinals/assistants/completions=%d/%d/%d/%d", messages, ordinals, assistants, completions)
	}
}
