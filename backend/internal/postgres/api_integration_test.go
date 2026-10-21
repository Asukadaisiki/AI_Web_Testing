package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/domain"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/event"
)

func TestResponsePersistsMessageAndContinuationOutbox(t *testing.T) {
	ctx, pool, store := newRepositoryTestStore(t)
	conversationID := createConversationFixture(t, ctx, pool)
	_, run, err := store.CreateMessageAndRun(ctx, CreateMessageParams{
		ConversationID: conversationID,
		Content:        "waiting for approval",
		AgentID:        "browser-eval",
		Model:          event.ModelRef{Provider: "scripted", Name: "fixture"},
		IdempotencyKey: "message-1",
	})
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE runs SET status = 'waiting_approval' WHERE id = $1`, run.ID); err != nil {
		t.Fatalf("put run in approval wait: %v", err)
	}

	if err := store.SubmitResponse(ctx, SubmitResponseParams{
		RunID:          run.ID,
		Kind:           "approval",
		Content:        "approved",
		IdempotencyKey: "response-1",
		Continuation:   true,
	}); err != nil {
		t.Fatalf("submit response: %v", err)
	}

	var (
		content string
		kind    string
		topic   string
		payload []byte
	)
	if err := pool.QueryRow(ctx, `
		SELECT content, metadata ->> 'response_kind'
		FROM messages
		WHERE conversation_id = $1
		ORDER BY ordinal DESC
		LIMIT 1`, conversationID).Scan(&content, &kind); err != nil {
		t.Fatalf("read persisted response: %v", err)
	}
	if content != "approved" || kind != "approval" {
		t.Fatalf("response message = (%q, %q)", content, kind)
	}
	if err := pool.QueryRow(ctx, `
		SELECT topic, payload
		FROM outbox_events
		WHERE key = $1
		  AND topic = $2
		  AND payload ->> 'event_type' = 'run.continuation.requested'
		LIMIT 1`, run.ID, runCommandsTopic).Scan(&topic, &payload); err != nil {
		t.Fatalf("read continuation outbox: %v", err)
	}
	if topic != runCommandsTopic {
		t.Fatalf("continuation topic = %q, want %q", topic, runCommandsTopic)
	}
	var envelope event.Envelope[event.RunCommand]
	if err := json.Unmarshal(payload, &envelope); err != nil {
		t.Fatalf("decode continuation envelope: %v", err)
	}
	if envelope.SchemaVersion != "agenttrace.run-command.v1" {
		t.Fatalf("continuation schema = %q", envelope.SchemaVersion)
	}
	if len(envelope.Payload.Messages) != 1 || envelope.Payload.Messages[0].Content != "approved" {
		t.Fatalf("continuation payload = %#v", envelope.Payload)
	}
}

func TestCreateMessageQueuesRuntimeCommandSchema(t *testing.T) {
	ctx, pool, store := newRepositoryTestStore(t)
	conversationID := createConversationFixture(t, ctx, pool)
	_, run, err := store.CreateMessageAndRun(ctx, CreateMessageParams{
		ConversationID: conversationID,
		Content:        "run it",
		AgentID:        "browser-eval",
		Model:          event.ModelRef{Provider: "scripted", Name: "fixture"},
		IdempotencyKey: "message-command-schema",
	})
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	var payload []byte
	if err := pool.QueryRow(ctx, `
		SELECT payload FROM outbox_events
		WHERE key = $1`, run.ID).Scan(&payload); err != nil {
		t.Fatalf("read command outbox: %v", err)
	}
	var envelope event.Envelope[event.RunCommand]
	if err := json.Unmarshal(payload, &envelope); err != nil {
		t.Fatalf("decode command envelope: %v", err)
	}
	if envelope.SchemaVersion != "agenttrace.run-command.v1" {
		t.Fatalf("command schema = %q, want runtime schema", envelope.SchemaVersion)
	}
}

func TestLoadRunCommandIncludesPersistedResponse(t *testing.T) {
	ctx, pool, store := newRepositoryTestStore(t)
	conversationID := createConversationFixture(t, ctx, pool)
	_, run, err := store.CreateMessageAndRun(ctx, CreateMessageParams{
		ConversationID: conversationID,
		Content:        "need approval",
		AgentID:        "browser-eval",
		Model:          event.ModelRef{Provider: "scripted", Name: "fixture"},
		IdempotencyKey: "message-response-load",
	})
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE runs SET status = 'waiting_approval' WHERE id = $1`, run.ID); err != nil {
		t.Fatalf("put run in approval wait: %v", err)
	}
	if err := store.SubmitResponse(ctx, SubmitResponseParams{
		RunID:          run.ID,
		Kind:           "approval",
		Content:        "approved",
		IdempotencyKey: "response-load",
		Continuation:   true,
	}); err != nil {
		t.Fatalf("submit response: %v", err)
	}
	_, command, err := store.LoadRunCommand(ctx, run.ID)
	if err != nil {
		t.Fatalf("load run command: %v", err)
	}
	if len(command.Messages) != 2 || command.Messages[1].Content != "approved" {
		t.Fatalf("loaded messages = %#v", command.Messages)
	}
}

func TestRequestCancelAppendsDurableEvent(t *testing.T) {
	ctx, pool, store := newRepositoryTestStore(t)
	conversationID := createConversationFixture(t, ctx, pool)
	_, run, err := store.CreateMessageAndRun(ctx, CreateMessageParams{
		ConversationID: conversationID,
		Content:        "cancel me",
		AgentID:        "browser-eval",
		Model:          event.ModelRef{Provider: "scripted", Name: "fixture"},
		IdempotencyKey: "message-1",
	})
	if err != nil {
		t.Fatalf("create run: %v", err)
	}

	if err := store.RequestCancel(ctx, run.ID); err != nil {
		t.Fatalf("request cancel: %v", err)
	}
	got, err := store.GetRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	if got.CancelRequestedAt == nil || got.Status != domain.RunCancelling {
		t.Fatalf("run after cancel = %#v", got)
	}
	var events int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM run_events
		WHERE run_id = $1 AND event_type = 'run.cancel.requested'`, run.ID).Scan(&events); err != nil {
		t.Fatalf("count cancellation events: %v", err)
	}
	if events != 1 {
		t.Fatalf("cancellation event count = %d, want 1", events)
	}
}

func TestSubmitResponseSerializesWithConcurrentMessageCreation(t *testing.T) {
	ctx, pool, store := newRepositoryTestStore(t)
	conversationID := createConversationFixture(t, ctx, pool)
	_, run, err := store.CreateMessageAndRun(ctx, CreateMessageParams{
		ConversationID: conversationID,
		Content:        "need approval",
		AgentID:        "browser-eval",
		Model:          event.ModelRef{Provider: "scripted", Name: "browser-eval-fixture"},
		IdempotencyKey: "initial-message",
	})
	if err != nil {
		t.Fatalf("create waiting run: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE runs SET status = 'waiting_approval' WHERE id = $1`, run.ID); err != nil {
		t.Fatalf("put run in approval wait: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		CREATE OR REPLACE FUNCTION test_delay_message_insert() RETURNS trigger AS $$
		BEGIN
			PERFORM pg_sleep(0.1);
			RETURN NEW;
		END;
		$$ LANGUAGE plpgsql;
		CREATE TRIGGER test_delay_message_insert
		BEFORE INSERT ON messages
		FOR EACH ROW EXECUTE FUNCTION test_delay_message_insert();`); err != nil {
		t.Fatalf("install overlap trigger: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DROP TRIGGER IF EXISTS test_delay_message_insert ON messages`)
		_, _ = pool.Exec(context.Background(), `DROP FUNCTION IF EXISTS test_delay_message_insert()`)
	})

	start := make(chan struct{})
	errs := make(chan error, 2)
	var workers sync.WaitGroup
	workers.Add(2)
	go func() {
		defer workers.Done()
		<-start
		_, _, err := store.CreateMessageAndRun(ctx, CreateMessageParams{
			ConversationID: conversationID,
			Content:        "ordinary concurrent message",
			AgentID:        "browser-eval",
			Model:          event.ModelRef{Provider: "scripted", Name: "browser-eval-fixture"},
			IdempotencyKey: "ordinary-message",
		})
		errs <- err
	}()
	go func() {
		defer workers.Done()
		<-start
		errs <- store.SubmitResponse(ctx, SubmitResponseParams{
			RunID:          run.ID,
			Kind:           "approval",
			Content:        "approved",
			IdempotencyKey: "concurrent-response",
			Continuation:   true,
		})
	}()
	close(start)
	done := make(chan struct{})
	go func() {
		workers.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("concurrent message creation and response insertion deadlocked")
	}
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent write failed: %v", err)
		}
	}

	rows, err := pool.Query(ctx, `
		SELECT ordinal
		FROM messages
		WHERE conversation_id = $1
		ORDER BY ordinal`, conversationID)
	if err != nil {
		t.Fatalf("read message ordinals: %v", err)
	}
	defer rows.Close()
	var ordinals []int64
	for rows.Next() {
		var ordinal int64
		if err := rows.Scan(&ordinal); err != nil {
			t.Fatalf("scan ordinal: %v", err)
		}
		ordinals = append(ordinals, ordinal)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate message ordinals: %v", err)
	}
	if got, want := len(ordinals), 3; got != want {
		t.Fatalf("message count = %d, want %d; ordinals = %#v", got, want, ordinals)
	}
	for index, ordinal := range ordinals {
		if ordinal != int64(index+1) {
			t.Fatalf("ordinals = %#v, want [1 2 3]", ordinals)
		}
	}
}

func TestSubmitResponseMissingRunReturnsNotFound(t *testing.T) {
	ctx, _, store := newRepositoryTestStore(t)
	err := store.SubmitResponse(ctx, SubmitResponseParams{
		RunID:          "run_missing",
		Kind:           "approval",
		Content:        "approved",
		IdempotencyKey: "missing-run",
		Continuation:   true,
	})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("submit response error = %v, want ErrNotFound", err)
	}
}
