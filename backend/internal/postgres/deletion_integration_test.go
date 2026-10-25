package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/domain"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/event"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestDeleteConversationRequiresTerminalPublishedProcessedRuns(t *testing.T) {
	ctx, pool, store := newRepositoryTestStore(t)
	conversationID := createConversationFixture(t, ctx, pool)
	_, run, err := store.CreateMessageAndRun(ctx, CreateMessageParams{
		ConversationID: conversationID,
		Content:        "delete only after delivery is safe",
		AgentID:        "browser-eval",
		Model:          event.ModelRef{Provider: "scripted", Name: "browser-eval-fixture"},
		IdempotencyKey: "delete-safety",
	})
	if err != nil {
		t.Fatalf("create run: %v", err)
	}

	if err := store.DeleteConversation(ctx, conversationID); !errors.Is(err, ErrConflict) {
		t.Fatalf("delete queued conversation error = %v, want ErrConflict", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE runs SET status = 'succeeded' WHERE id = $1`, run.ID); err != nil {
		t.Fatalf("finish run fixture: %v", err)
	}
	if err := store.DeleteConversation(ctx, conversationID); !errors.Is(err, ErrConflict) {
		t.Fatalf("delete with unpublished command error = %v, want ErrConflict", err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE outbox_events SET published_at = now()
		WHERE key = $1 AND topic = $2`, run.ID, runCommandsTopic); err != nil {
		t.Fatalf("publish command fixture: %v", err)
	}
	if err := store.DeleteConversation(ctx, conversationID); !errors.Is(err, ErrConflict) {
		t.Fatalf("delete with unprocessed command error = %v, want ErrConflict", err)
	}

	markRuntimeCommandProcessed(t, ctx, pool, run.ID)
	var eventID string
	var originalPayload []byte
	if err := pool.QueryRow(ctx, `
		SELECT event_id, payload
		FROM inbox_events
		WHERE consumer = 'agenttrace-runtime'`).Scan(&eventID, &originalPayload); err != nil {
		t.Fatalf("read retained runtime command before deletion: %v", err)
	}
	if !strings.Contains(string(originalPayload), "delete only after delivery is safe") {
		t.Fatalf("runtime command fixture does not contain deleted content: %s", originalPayload)
	}
	if err := store.DeleteConversation(ctx, conversationID); err != nil {
		t.Fatalf("delete safe conversation: %v", err)
	}
	if err := store.DeleteConversation(ctx, conversationID); err != nil {
		t.Fatalf("repeat delete conversation: %v", err)
	}

	assertRowCount(t, ctx, pool, "projects", 1)
	assertRowCount(t, ctx, pool, "conversations", 0)
	assertRowCount(t, ctx, pool, "runs", 0)
	assertRowCount(t, ctx, pool, "messages", 0)
	assertRowCount(t, ctx, pool, "outbox_events", 0)
	assertRowCount(t, ctx, pool, "idempotency_keys", 0)
	assertRowCount(t, ctx, pool, "consumer_retry_counts", 0)
	assertRowCount(t, ctx, pool, "inbox_events", 1)

	var retainedPayload string
	if err := pool.QueryRow(ctx, `
		SELECT payload::text
		FROM inbox_events
		WHERE consumer = 'agenttrace-runtime'
		  AND event_id = $1
		  AND processed_at IS NOT NULL`, eventID).Scan(&retainedPayload); err != nil {
		t.Fatalf("read retained runtime command tombstone: %v", err)
	}
	if retainedPayload != "{}" {
		t.Fatalf("retained runtime command payload = %s, want {}", retainedPayload)
	}
	tag, err := pool.Exec(ctx, `
		INSERT INTO inbox_events (id, consumer, event_id, payload)
		VALUES ('inbox_replayed_after_delete', 'agenttrace-runtime', $1, $2::jsonb)
		ON CONFLICT (consumer, event_id) DO NOTHING`,
		eventID, originalPayload,
	)
	if err != nil {
		t.Fatalf("replay retained runtime command: %v", err)
	}
	if tag.RowsAffected() != 0 {
		t.Fatalf("replayed runtime command inserted %d rows, want 0", tag.RowsAffected())
	}
}

func TestDeleteProjectIsIdempotentAndScoped(t *testing.T) {
	ctx, pool, store := newRepositoryTestStore(t)
	firstConversationID := createConversationFixture(t, ctx, pool)
	const secondConversationID = "conv_fixture_second"
	if _, err := pool.Exec(ctx, `
		INSERT INTO conversations (id, project_id, title)
		VALUES ($1, 'prj_fixture', 'Second fixture conversation')`,
		secondConversationID,
	); err != nil {
		t.Fatalf("create second conversation fixture: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO projects (id, name) VALUES ('prj_unrelated', 'Unrelated')`); err != nil {
		t.Fatalf("create unrelated project fixture: %v", err)
	}

	runs := make([]domain.Run, 0, 2)
	for index, conversationID := range []string{firstConversationID, secondConversationID} {
		_, run, err := store.CreateMessageAndRun(ctx, CreateMessageParams{
			ConversationID: conversationID,
			Content:        "project deletion fixture",
			AgentID:        "browser-eval",
			Model:          event.ModelRef{Provider: "scripted", Name: "browser-eval-fixture"},
			IdempotencyKey: "delete-project-" + string(rune('a'+index)),
		})
		if err != nil {
			t.Fatalf("create project run %d: %v", index, err)
		}
		runs = append(runs, run)
		if _, err := pool.Exec(ctx, `
			UPDATE runs SET status = 'succeeded' WHERE id = $1`, run.ID); err != nil {
			t.Fatalf("make project run %d terminal: %v", index, err)
		}
		if _, err := pool.Exec(ctx, `
			UPDATE outbox_events SET published_at = now()
			WHERE key = $1 AND topic = $2`, run.ID, runCommandsTopic); err != nil {
			t.Fatalf("make project run %d published: %v", index, err)
		}
		markRuntimeCommandProcessed(t, ctx, pool, run.ID)
	}

	if err := store.DeleteProject(ctx, "prj_fixture"); err != nil {
		t.Fatalf("delete project: %v", err)
	}
	if err := store.DeleteProject(ctx, "prj_fixture"); err != nil {
		t.Fatalf("repeat delete project: %v", err)
	}

	assertRowCount(t, ctx, pool, "projects", 1)
	assertRowCount(t, ctx, pool, "conversations", 0)
	assertRowCount(t, ctx, pool, "runs", 0)
	assertRowCount(t, ctx, pool, "messages", 0)
	assertRowCount(t, ctx, pool, "outbox_events", 0)
	assertRowCount(t, ctx, pool, "idempotency_keys", 0)
	assertRowCount(t, ctx, pool, "inbox_events", len(runs))

	var unrelated int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM projects
		WHERE id = 'prj_unrelated' AND name = 'Unrelated'`).Scan(&unrelated); err != nil {
		t.Fatalf("read unrelated project: %v", err)
	}
	if unrelated != 1 {
		t.Fatalf("unrelated project rows = %d, want 1", unrelated)
	}
}

func TestDeleteProjectDoesNotDeadlockConcurrentMessageCreation(t *testing.T) {
	ctx, pool, store := newRepositoryTestStore(t)
	conversationID := createConversationFixture(t, ctx, pool)
	holder, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin project lock holder: %v", err)
	}
	if _, err := holder.Exec(ctx, `
		SELECT id FROM projects WHERE id = 'prj_fixture' FOR UPDATE`); err != nil {
		t.Fatalf("hold project lock: %v", err)
	}

	deleteResult := make(chan error, 1)
	go func() {
		deleteResult <- store.DeleteProject(ctx, "prj_fixture")
	}()
	time.Sleep(50 * time.Millisecond)

	createResult := make(chan error, 1)
	go func() {
		_, _, err := store.CreateMessageAndRun(ctx, CreateMessageParams{
			ConversationID: conversationID,
			Content:        "concurrent with deletion",
			AgentID:        "browser-eval",
			Model:          event.ModelRef{Provider: "scripted", Name: "browser-eval-fixture"},
			IdempotencyKey: "concurrent-delete-message",
		})
		createResult <- err
	}()
	time.Sleep(50 * time.Millisecond)
	if err := holder.Commit(ctx); err != nil {
		t.Fatalf("release project lock: %v", err)
	}

	deadline := time.After(5 * time.Second)
	var deleteErr, createErr error
	for deleteResult != nil || createResult != nil {
		select {
		case deleteErr = <-deleteResult:
			deleteResult = nil
		case createErr = <-createResult:
			createResult = nil
		case <-deadline:
			t.Fatal("concurrent delete and create did not finish")
		}
	}
	for operation, err := range map[string]error{
		"delete project": deleteErr,
		"create message": createErr,
	} {
		if err != nil && strings.Contains(strings.ToLower(err.Error()), "deadlock") {
			t.Fatalf("%s returned a deadlock: %v", operation, err)
		}
	}
	if deleteErr != nil && !errors.Is(deleteErr, ErrConflict) {
		t.Fatalf("delete project error = %v, want nil or ErrConflict", deleteErr)
	}
	if createErr != nil && !errors.Is(createErr, ErrNotFound) {
		t.Fatalf("create message error = %v, want nil or ErrNotFound", createErr)
	}
}

func markRuntimeCommandProcessed(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	runID string,
) {
	t.Helper()
	var eventID string
	var payload []byte
	if err := pool.QueryRow(ctx, `
		SELECT event_id, payload
		FROM outbox_events
		WHERE key = $1 AND topic = $2`,
		runID, runCommandsTopic,
	).Scan(&eventID, &payload); err != nil {
		t.Fatalf("read command outbox for %s: %v", runID, err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO inbox_events
			(id, consumer, event_id, payload, processed_at)
		VALUES ($1, 'agenttrace-runtime', $2, $3::jsonb, now())`,
		"inbox_delete_"+eventID, eventID, payload,
	); err != nil {
		t.Fatalf("mark runtime command processed for %s: %v", runID, err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO consumer_retry_counts
			(consumer, event_id, payload, attempt_count)
		VALUES ('agenttrace-runtime', $1, $2::jsonb, 1)`,
		eventID, payload,
	); err != nil {
		t.Fatalf("create consumer retry fixture for %s: %v", runID, err)
	}
}
