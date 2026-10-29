package postgres

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/event"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/execution"
)

func TestHarnessCatalogSnapshotsSurviveUpdateDeleteRerunAndIdempotency(t *testing.T) {
	ctx, pool, store := newRepositoryTestStore(t)
	h, err := store.CreateHarness(ctx, execution.HarnessSnapshot{
		Name: "Generic", SystemPrompt: "original", Skills: []execution.Skill{},
		MCP: map[string]execution.MCPServer{},
	})
	if err != nil {
		t.Fatal(err)
	}
	project, err := store.CreateProject(ctx, CreateProjectParams{Name: "composition"})
	if err != nil {
		t.Fatal(err)
	}
	conversation, err := store.CreateConversation(ctx, CreateConversationParams{ProjectID: project.ID})
	if err != nil {
		t.Fatal(err)
	}
	p := CreateMessageParams{
		ConversationID: conversation.ID, Content: "hello", AgentID: "codex",
		Model:     event.ModelRef{Provider: "openai-responses", Name: "model", ReasoningEffort: "high"},
		Execution: &execution.Spec{RuntimeID: "codex", Harness: h}, IdempotencyKey: "first",
	}
	_, run, err := store.CreateMessageAndRun(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	_, replay, err := store.CreateMessageAndRun(ctx, p)
	if err != nil || replay.ID != run.ID {
		t.Fatalf("same execution replay: %v", err)
	}
	p.Execution.RuntimeID = "claude-code"
	if _, _, err := store.CreateMessageAndRun(ctx, p); !errors.Is(err, ErrConflict) {
		t.Fatalf("changed runtime idempotency: %v", err)
	}
	p.Execution.RuntimeID = "codex"
	updated := h
	updated.SystemPrompt = "edited"
	updated, err = store.UpdateHarness(ctx, h.ID, updated)
	if err != nil || updated.Revision != h.Revision+1 {
		t.Fatalf("update revision: %#v %v", updated, err)
	}
	p.Execution.Harness = updated
	if _, _, err := store.CreateMessageAndRun(ctx, p); !errors.Is(err, ErrConflict) {
		t.Fatalf("changed snapshot idempotency: %v", err)
	}
	if _, err := store.DeleteHarness(ctx, h.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetHarness(ctx, h.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted harness get: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE runs SET status = 'failed' WHERE id = $1`, run.ID); err != nil {
		t.Fatal(err)
	}
	rerun, err := store.CreateRerun(ctx, CreateRerunParams{RunID: run.ID, IdempotencyKey: "rerun"})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{run.ID, rerun.ID} {
		stored, err := store.GetRun(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		var spec runSpecification
		if err := json.Unmarshal(stored.Specification, &spec); err != nil {
			t.Fatal(err)
		}
		if spec.Execution.Harness.SystemPrompt != "original" || spec.Command.Execution.Harness.Revision != 1 {
			t.Fatal("saved snapshot changed after catalog mutation")
		}
		var command event.Envelope[event.RunCommand]
		var raw []byte
		if err := pool.QueryRow(ctx, `SELECT payload FROM outbox_events WHERE key = $1 AND topic = $2`, id, runCommandsTopic).Scan(&raw); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(raw, &command); err != nil {
			t.Fatal(err)
		}
		if command.Payload.Execution.Harness.SystemPrompt != "original" {
			t.Fatal("outbox did not preserve snapshot")
		}
	}
}

func TestBuiltinHarnessSeedPreservesEditsAndDeletion(t *testing.T) {
	ctx, _, store := newRepositoryTestStore(t)
	h := execution.HarnessSnapshot{
		ID: execution.BuiltinHarnessID, Name: "Browser Harness", Revision: 1,
		SystemPrompt: "actual prompt", Skills: []execution.Skill{}, MCP: map[string]execution.MCPServer{},
	}
	if err := store.SeedBuiltinHarness(ctx, h); err != nil {
		t.Fatal(err)
	}
	h.SystemPrompt = "user edit"
	edited, err := store.UpdateHarness(ctx, h.ID, h)
	if err != nil {
		t.Fatal(err)
	}
	h.SystemPrompt = "new bundled prompt"
	if err := store.SeedBuiltinHarness(ctx, h); err != nil {
		t.Fatal(err)
	}
	stored, err := store.GetHarness(ctx, h.ID)
	if err != nil || stored.SystemPrompt != edited.SystemPrompt || stored.Revision != edited.Revision {
		t.Fatal("restart overwrote user edit")
	}
	if _, err := store.DeleteHarness(ctx, h.ID); err != nil {
		t.Fatal(err)
	}
	if err := store.SeedBuiltinHarness(ctx, h); err != nil {
		t.Fatal(err)
	}
	harnesses, err := store.ListHarnesses(ctx)
	if err != nil || len(harnesses) != 0 {
		t.Fatal("restart resurrected deleted builtin Harness")
	}
}
