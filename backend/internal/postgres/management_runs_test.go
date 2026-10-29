package postgres

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/event"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/registry"
)

func TestManagementRunPinsSnapshotAndReplaysAfterRevocation(t *testing.T) {
	ctx, pool, store := newRepositoryTestStore(t)

	project, err := store.CreateProject(ctx, CreateProjectParams{Name: "managed", LangfuseProjectID: "lf-managed"})
	if err != nil {
		t.Fatal(err)
	}
	conversation, err := store.CreateConversation(ctx, CreateConversationParams{ProjectID: project.ID, Title: "thread"})
	if err != nil {
		t.Fatal(err)
	}
	environment, err := store.GetConversationEnvironment(ctx, conversation.ID)
	if err != nil {
		t.Fatal(err)
	}
	harness, err := store.PublishProjectHarness(ctx, project.ID, "", registry.HarnessVersion{Name: "Minimal", RuntimeIDs: []string{"pi"}}, "admin")
	if err != nil {
		t.Fatal(err)
	}
	params := CreateMessageParams{ConversationID: conversation.ID, Content: "inspect",
		Model: event.ModelRef{Provider: "scripted", Name: "fixture"}, IdempotencyKey: "key",
		RuntimeID: "pi", HarnessVersionID: harness.ID}
	if _, _, err := store.CreateMessageAndRun(ctx, params); !errors.Is(err, ErrConflict) {
		t.Fatalf("provisioning environment accepted: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE environments SET state='ready' WHERE id=$1`, environment.ID); err != nil {
		t.Fatal(err)
	}
	_, run, err := store.CreateMessageAndRun(ctx, params)
	if err != nil {
		t.Fatal(err)
	}
	var snapshot runSpecification
	if err := json.Unmarshal(run.Specification, &snapshot); err != nil {
		t.Fatal(err)
	}
	if snapshot.Execution.Harness.VersionID != harness.ID || snapshot.Command.EnvironmentID != environment.ID || snapshot.Command.ProfileGeneration != 1 {
		t.Fatalf("missing immutable identity: %s", run.Specification)
	}
	var key string
	if err := pool.QueryRow(ctx, `SELECT key FROM outbox_events WHERE payload->>'aggregate_id'=$1`, run.ID).Scan(&key); err != nil {
		t.Fatal(err)
	}
	if key != environment.ID {
		t.Fatalf("command partition key %q", key)
	}
	if err := store.RevokeProjectHarness(ctx, project.ID, harness.ID, "admin"); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE environments SET state='needs_review' WHERE id=$1`, environment.ID); err != nil {
		t.Fatal(err)
	}
	_, replay, err := store.CreateMessageAndRun(ctx, params)
	if err != nil || replay.ID != run.ID {
		t.Fatalf("idempotent replay changed: %v %#v", err, replay)
	}
	params.IdempotencyKey = "new-key"
	if _, _, err := store.CreateMessageAndRun(ctx, params); !errors.Is(err, ErrConflict) {
		t.Fatalf("needs_review accepted: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE environments SET state='ready',profile_generation=2 WHERE id=$1`, environment.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE runs SET status='failed' WHERE id=$1`, run.ID); err != nil {
		t.Fatal(err)
	}
	rerun, err := store.CreateRerun(ctx, CreateRerunParams{RunID: run.ID, IdempotencyKey: "retry"})
	if err != nil {
		t.Fatal(err)
	}
	var retried runSpecification
	if err := json.Unmarshal(rerun.Specification, &retried); err != nil {
		t.Fatal(err)
	}
	if retried.Execution.Harness.VersionID != harness.ID || retried.Command.ProfileGeneration != 2 {
		t.Fatalf("rerun lost original Harness or current profile: %s", rerun.Specification)
	}
}
