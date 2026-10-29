package postgres

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/domain"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/event"
)

func TestGetProjectReturnsCanonicalMetadata(t *testing.T) {
	ctx, pool, store := newRepositoryTestStore(t)
	if _, err := pool.Exec(ctx, `
		INSERT INTO projects (id, name, metadata)
		VALUES ('prj_export', 'Export project', '{"environment":"fixture"}')`); err != nil {
		t.Fatalf("insert project: %v", err)
	}

	project, err := store.GetProject(ctx, "prj_export")
	if err != nil {
		t.Fatalf("get project: %v", err)
	}
	var metadata map[string]string
	if err := json.Unmarshal(project.Metadata, &metadata); err != nil {
		t.Fatalf("decode project metadata: %v", err)
	}
	if project.ID != "prj_export" || project.Name != "Export project" ||
		metadata["environment"] != "fixture" {
		t.Fatalf("project = %#v", project)
	}
}

func TestGetConversationReturnsRunsAndAttributableMessages(t *testing.T) {
	ctx, pool, store := newRepositoryTestStore(t)
	conversationID := createConversationFixture(t, ctx, pool)
	userMessage, run, err := store.CreateMessageAndRun(ctx, managedMessageParams(t, ctx, store, CreateMessageParams{
		ConversationID: conversationID,
		Content:        "verify the cart",

		Model:          event.ModelRef{Provider: "scripted", Name: "browser-eval-fixture"},
		IdempotencyKey: "conversation-history",
	}))
	if err != nil {
		t.Fatalf("create message and run: %v", err)
	}
	completedAt := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	if err := finishFixtureRun(t, ctx, store, run.ID, domain.RunSucceeded, nil, "cart passed",
		event.Envelope[json.RawMessage]{
			SchemaVersion: "agenttrace.event.v1",
			EventID:       "evt-conversation-history",
			EventType:     event.RunCompleted,
			AggregateID:   run.ID,
			CorrelationID: run.ID,
			OccurredAt:    completedAt,
			Payload:       json.RawMessage(`{"status":"succeeded"}`),
		}); err != nil {
		t.Fatalf("finish run: %v", err)
	}

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
	if len(messages) != 2 {
		t.Fatalf("messages = %#v, want user and assistant messages", messages)
	}
	if got := messages[len(messages)-1].RunID; got != run.ID {
		t.Fatalf("assistant run_id = %q, want %q", got, run.ID)
	}
}

func TestCreateRerunIsReturnedByConversationRead(t *testing.T) {
	ctx, pool, store := newRepositoryTestStore(t)
	conversationID := createConversationFixture(t, ctx, pool)
	_, source := createFailedRerunSource(t, ctx, store, conversationID, "conversation-rerun-source")

	rerun, err := store.CreateRerun(ctx, CreateRerunParams{
		RunID:          source.ID,
		IdempotencyKey: "conversation-rerun",
	})
	if err != nil {
		t.Fatalf("create rerun: %v", err)
	}
	_, _, runs, err := store.GetConversation(ctx, conversationID)
	if err != nil {
		t.Fatalf("get conversation: %v", err)
	}
	if len(runs) != 2 {
		t.Fatalf("conversation runs = %#v, want source and rerun", runs)
	}
	var returned domain.Run
	for _, run := range runs {
		if run.ID == rerun.ID {
			returned = run
			break
		}
	}
	if returned.ID != rerun.ID ||
		returned.SourceMessageID != source.SourceMessageID ||
		returned.RetryOfRunID != source.ID {
		t.Fatalf("returned rerun lineage = %#v", returned)
	}
}

func TestCreateMessageQueuesRuntimeCommandSchema(t *testing.T) {
	ctx, pool, store := newRepositoryTestStore(t)
	conversationID := createConversationFixture(t, ctx, pool)
	_, run, err := store.CreateMessageAndRun(ctx, managedMessageParams(t, ctx, store, CreateMessageParams{
		ConversationID: conversationID,
		Content:        "run it",

		Model:          event.ModelRef{Provider: "scripted", Name: "fixture"},
		IdempotencyKey: "message-command-schema",
	}))
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	var payload []byte
	if err := pool.QueryRow(ctx, `
		SELECT payload FROM outbox_events
		WHERE payload->>'aggregate_id' = $1 AND topic='agenttrace.run.commands.v1'`, run.ID).Scan(&payload); err != nil {
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

func TestRequestCancelAppendsDurableEvent(t *testing.T) {
	ctx, pool, store := newRepositoryTestStore(t)
	conversationID := createConversationFixture(t, ctx, pool)
	_, run, err := store.CreateMessageAndRun(ctx, managedMessageParams(t, ctx, store, CreateMessageParams{
		ConversationID: conversationID,
		Content:        "cancel me",

		Model:          event.ModelRef{Provider: "scripted", Name: "fixture"},
		IdempotencyKey: "message-1",
	}))
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
