package postgres

import (
	"encoding/json"
	"testing"

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
