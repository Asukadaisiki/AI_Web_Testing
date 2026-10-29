package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/consumer"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/event"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/eventbus"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/registry"
	"github.com/jackc/pgx/v5/pgxpool"
)

func managedDeletionRun(t *testing.T) (context.Context, *pgxpool.Pool, *Store, Conversation, Environment, eventbus.Message) {
	t.Helper()
	ctx, pool, store := newRepositoryTestStore(t)
	store.SetManagementEnabled(true)
	conversation := boundConversation(t, ctx, store)
	environment, err := store.GetConversationEnvironment(ctx, conversation.ID)
	if err != nil {
		t.Fatal(err)
	}
	acknowledgeEnvironment(t, ctx, pool, store, environment.ID, "provision")
	harness, err := store.PublishProjectHarness(ctx, conversation.ProjectID, "", registry.HarnessVersion{
		Name: "Deletion fixture", RuntimeIDs: []string{"pi"}, SystemPrompt: "private-harness-snapshot",
	}, "admin")
	if err != nil {
		t.Fatal(err)
	}
	_, run, err := store.CreateMessageAndRun(ctx, CreateMessageParams{
		ConversationID: conversation.ID, Content: "private-deletion-goal private-deletion-secret",
		AgentID: "pi", RuntimeID: "pi", HarnessVersionID: harness.ID,
		Model: event.ModelRef{Provider: "scripted", Name: "fixture"}, IdempotencyKey: "delete-managed",
	})
	if err != nil {
		t.Fatal(err)
	}
	var message eventbus.Message
	message.Topic = runCommandsTopic
	if err := pool.QueryRow(ctx, `SELECT key, payload FROM outbox_events
		WHERE topic = $1 AND payload->'payload'->>'run_id' = $2`, runCommandsTopic, run.ID).
		Scan(&message.Key, &message.Value); err != nil {
		t.Fatal(err)
	}
	if message.Key != environment.ID || message.Key == run.ID {
		t.Fatalf("managed command key = %q, want environment %q", message.Key, environment.ID)
	}
	for _, content := range []string{"private-deletion-goal", "private-deletion-secret", "private-harness-snapshot"} {
		if !strings.Contains(string(message.Value), content) {
			t.Fatalf("command fixture lacks %q", content)
		}
	}
	return ctx, pool, store, conversation, environment, message
}

func TestManagedDeletionClearsRuntimeContentAndDeduplicatesLateDelivery(t *testing.T) {
	for _, publishDelete := range []bool{false, true} {
		name := "completion_before_publish_ack"
		if publishDelete {
			name = "published_delete"
		}
		t.Run(name, func(t *testing.T) {
			ctx, pool, store, conversation, environment, message := managedDeletionRun(t)
			var command event.Envelope[event.RunCommand]
			if err := json.Unmarshal(message.Value, &command); err != nil {
				t.Fatal(err)
			}
			provision := lifecycleEnvelope(t, ctx, pool, environment.ID, "provision")
			if _, err := pool.Exec(ctx, `UPDATE runs SET status = 'succeeded' WHERE id = $1`, command.Payload.RunID); err != nil {
				t.Fatal(err)
			}
			if _, err := pool.Exec(ctx, `UPDATE outbox_events SET published_at = now() WHERE event_id = $1`, command.EventID); err != nil {
				t.Fatal(err)
			}
			executions := 0
			dedup := consumer.NewDeduplicator(pool, "agenttrace-runtime")
			if err := dedup.HandleExternal(ctx, message, func(context.Context) error {
				executions++
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			orphan := command
			orphan.EventID = "evt-orphan-runtime"
			orphanRaw, err := json.Marshal(orphan)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := pool.Exec(ctx, `INSERT INTO inbox_events (id, consumer, event_id, payload, processed_at)
				VALUES ('orphan', 'agenttrace-runtime', $1, $2, now())`, orphan.EventID, orphanRaw); err != nil {
				t.Fatal(err)
			}
			if _, err := pool.Exec(ctx, `INSERT INTO consumer_retry_counts (consumer, event_id, payload, attempt_count)
				VALUES ('agenttrace-runtime', $1, $2, 1), ('agenttrace-runtime', $3, $4, 1)`,
				command.EventID, message.Value, orphan.EventID, orphanRaw); err != nil {
				t.Fatal(err)
			}
			if _, err := pool.Exec(ctx, `UPDATE environments SET profile_ref = 'private-profile-ref',
				policy = '{"private":"private-policy-content"}' WHERE id = $1`, environment.ID); err != nil {
				t.Fatal(err)
			}
			if err := store.DeleteConversationAs(ctx, conversation.ID, "actor"); err != nil {
				t.Fatal(err)
			}
			deletion := lifecycleEnvelope(t, ctx, pool, environment.ID, "delete")
			claim, acquired, err := store.ClaimEnvironmentCommand(ctx, deletion)
			if err != nil || !acquired {
				t.Fatalf("claim delete = %v, %v", acquired, err)
			}
			if publishDelete {
				if _, err := pool.Exec(ctx, `UPDATE outbox_events SET published_at = now() WHERE event_id = $1`, deletion.EventID); err != nil {
					t.Fatal(err)
				}
			}
			if err := store.CompleteEnvironmentCommand(ctx, claim, EnvironmentProviderResult{}); err != nil {
				t.Fatal(err)
			}
			for _, table := range []string{"runs", "messages", "outbox_events", "consumer_retry_counts", "idempotency_keys", "environments"} {
				assertRowCount(t, ctx, pool, table, 0)
			}
			var retained, contentful int
			if err := pool.QueryRow(ctx, `SELECT count(*), count(*) FILTER (WHERE payload <> '{}' OR processed_at IS NULL)
				FROM inbox_events`).Scan(&retained, &contentful); err != nil {
				t.Fatal(err)
			}
			if retained != 4 || contentful != 0 {
				t.Fatalf("inbox tombstones/contentful = %d/%d, want 4/0", retained, contentful)
			}
			deleted, messages, runs, err := store.GetConversation(ctx, conversation.ID)
			if err != nil || deleted.State != "deleted" || deleted.Title != "" || string(deleted.Metadata) != "{}" ||
				deleted.DeletedAt == nil || len(messages) != 0 || len(runs) != 0 {
				t.Fatalf("deleted conversation = %+v, messages=%v runs=%v err=%v", deleted, messages, runs, err)
			}
			for _, replay := range []eventbus.Message{message, {Topic: runCommandsTopic, Key: environment.ID, Value: orphanRaw}} {
				if err := dedup.HandleExternal(ctx, replay, func(context.Context) error {
					executions++
					return nil
				}); err != nil {
					t.Fatal(err)
				}
			}
			if executions != 1 {
				t.Fatalf("runtime executions after deletion = %d, want 1", executions)
			}
			for _, replay := range []event.Envelope[EnvironmentCommand]{provision, deletion} {
				if err := store.ProcessEnvironmentCommand(ctx, replay, environmentProviderFunc(
					func(context.Context, EnvironmentCommand) (EnvironmentProviderResult, error) {
						t.Error("late lifecycle replay executed provider")
						return EnvironmentProviderResult{}, nil
					})); err != nil {
					t.Fatal(err)
				}
			}
			if err := store.CompleteEnvironmentCommand(ctx, claim, EnvironmentProviderResult{}); err != nil {
				t.Fatalf("repeat completion: %v", err)
			}
			if err := store.DeleteConversationAs(ctx, conversation.ID, "actor"); err != nil {
				t.Fatalf("repeat deletion: %v", err)
			}
			var contentAfterReplay int
			if err := pool.QueryRow(ctx, `SELECT count(*) FROM inbox_events WHERE payload <> '{}'`).Scan(&contentAfterReplay); err != nil || contentAfterReplay != 0 {
				t.Fatalf("replay restored content = %d, err=%v", contentAfterReplay, err)
			}
		})
	}
}

func TestManagedDeletionRejectsPendingEnvironmentKeyedRuntimeDelivery(t *testing.T) {
	for _, blocker := range []string{"queued", "unpublished", "missing_inbox", "pending_inbox", "orphan_inbox"} {
		t.Run(blocker, func(t *testing.T) {
			ctx, pool, store, conversation, environment, message := managedDeletionRun(t)
			var command event.Envelope[event.RunCommand]
			if err := json.Unmarshal(message.Value, &command); err != nil {
				t.Fatal(err)
			}
			if blocker != "queued" {
				if _, err := pool.Exec(ctx, `UPDATE runs SET status = 'succeeded' WHERE id = $1`, command.Payload.RunID); err != nil {
					t.Fatal(err)
				}
			}
			if blocker != "unpublished" {
				if _, err := pool.Exec(ctx, `UPDATE outbox_events SET published_at = now() WHERE event_id = $1`, command.EventID); err != nil {
					t.Fatal(err)
				}
			}
			if blocker == "pending_inbox" {
				if _, err := pool.Exec(ctx, `INSERT INTO inbox_events (id, consumer, event_id, payload)
					VALUES ('pending', 'agenttrace-runtime', $1, $2)`, command.EventID, message.Value); err != nil {
					t.Fatal(err)
				}
			}
			if blocker == "orphan_inbox" {
				if err := consumer.NewDeduplicator(pool, "agenttrace-runtime").HandleExternal(ctx, message,
					func(context.Context) error { return nil }); err != nil {
					t.Fatal(err)
				}
				if _, err := pool.Exec(ctx, `INSERT INTO inbox_events (id, consumer, event_id, payload)
					VALUES ('pending-orphan', 'agenttrace-runtime', 'pending-orphan', $1)`, message.Value); err != nil {
					t.Fatal(err)
				}
			}
			if err := store.DeleteConversationAs(ctx, conversation.ID, "actor"); !errors.Is(err, ErrConflict) {
				t.Fatalf("delete with %s = %v, want ErrConflict (HTTP 409)", blocker, err)
			}
			current, err := store.GetEnvironment(ctx, environment.ID)
			if err != nil || current.State != "ready" {
				t.Fatalf("conflicting deletion changed environment: %+v, %v", current, err)
			}
			assertRowCount(t, ctx, pool, "runs", 1)
			assertRowCount(t, ctx, pool, "messages", 1)
		})
	}
}

func TestManagedDeletionPreservesUnrelatedCommandWithCollidingRoutingKey(t *testing.T) {
	ctx, pool, store, conversation, environment, message := managedDeletionRun(t)
	var command event.Envelope[event.RunCommand]
	if err := json.Unmarshal(message.Value, &command); err != nil {
		t.Fatal(err)
	}
	other, err := store.CreateConversation(ctx, CreateConversationParams{ProjectID: conversation.ProjectID, Title: "Other"})
	if err != nil {
		t.Fatal(err)
	}
	otherEnvironment, err := store.GetConversationEnvironment(ctx, other.ID)
	if err != nil {
		t.Fatal(err)
	}
	acknowledgeEnvironment(t, ctx, pool, store, otherEnvironment.ID, "provision")
	_, otherRun, err := store.CreateMessageAndRun(ctx, CreateMessageParams{
		ConversationID: other.ID, Content: "unrelated-goal", AgentID: "pi", RuntimeID: "pi",
		HarnessVersionID: command.Payload.Execution.Harness.VersionID,
		Model:            event.ModelRef{Provider: "scripted", Name: "fixture"}, IdempotencyKey: "other-run",
	})
	if err != nil {
		t.Fatal(err)
	}
	// A routing-key collision must neither block nor delete another Run's
	// command. The authoritative envelope still belongs to the other Run.
	if _, err := pool.Exec(ctx, `UPDATE outbox_events SET key = $1
		WHERE payload->'payload'->>'run_id' = $2`, command.Payload.RunID, otherRun.ID); err != nil {
		t.Fatal(err)
	}
	var originalOtherPayload []byte
	if err := pool.QueryRow(ctx, `SELECT payload FROM outbox_events
		WHERE payload->'payload'->>'run_id' = $1`, otherRun.ID).Scan(&originalOtherPayload); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE runs SET status = 'succeeded' WHERE id = $1`, command.Payload.RunID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE outbox_events SET published_at = now() WHERE event_id = $1`, command.EventID); err != nil {
		t.Fatal(err)
	}
	if err := consumer.NewDeduplicator(pool, "agenttrace-runtime").HandleExternal(ctx, message,
		func(context.Context) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteConversationAs(ctx, conversation.ID, "actor"); err != nil {
		t.Fatal(err)
	}
	acknowledgeEnvironment(t, ctx, pool, store, environment.ID, "delete")
	var preservedPayload []byte
	var unpublished bool
	if err := pool.QueryRow(ctx, `SELECT payload, published_at IS NULL FROM outbox_events
		WHERE payload->'payload'->>'run_id' = $1`, otherRun.ID).Scan(&preservedPayload, &unpublished); err != nil {
		t.Fatal(err)
	}
	if string(preservedPayload) != string(originalOtherPayload) || !unpublished {
		t.Fatalf("unrelated pending command was altered: %s, unpublished=%v", preservedPayload, unpublished)
	}
	current, messages, runs, err := store.GetConversation(ctx, other.ID)
	if err != nil || current.State != "active" || len(messages) != 1 || len(runs) != 1 || runs[0].ID != otherRun.ID {
		t.Fatalf("unrelated conversation changed: %+v, messages=%v runs=%v err=%v", current, messages, runs, err)
	}
	currentEnvironment, err := store.GetEnvironment(ctx, otherEnvironment.ID)
	if err != nil || currentEnvironment.State != "ready" {
		t.Fatalf("unrelated environment changed: %+v, %v", currentEnvironment, err)
	}
}
