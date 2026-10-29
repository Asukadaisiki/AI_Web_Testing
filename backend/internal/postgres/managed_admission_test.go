package postgres

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/event"
)

func TestLeaseRejectsHistoricalAndMismatchedManagedSpecifications(t *testing.T) {
	for _, kind := range []string{"snapshotless", "global", "mismatch", "forged"} {
		t.Run(kind, func(t *testing.T) {
			ctx, pool, s := leaseTestStore(t)
			p := leaseFixture(t, ctx, pool, s, kind)
			registerLeaseWorkers(t, ctx, s, 1)
			run, err := s.GetRun(ctx, p.RunID)
			if err != nil {
				t.Fatal(err)
			}
			var spec runSpecification
			if err := json.Unmarshal(run.Specification, &spec); err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "snapshotless":
				spec.Execution, spec.Command.Execution = nil, nil
			case "global":
				spec.Execution.Harness.ProjectID = ""
				spec.Command.Execution.Harness.ProjectID = ""
			case "mismatch":
				spec.Execution.Harness.SystemPrompt = "different"
			case "forged":
				spec.Execution.Harness.SystemPrompt = "forged"
				spec.Command.Execution.Harness.SystemPrompt = "forged"
			}
			raw, _ := json.Marshal(spec)
			if _, err := pool.Exec(ctx, `UPDATE runs SET specification=$2 WHERE id=$1`, p.RunID, raw); err != nil {
				t.Fatal(err)
			}
			if _, err := s.ClaimHarnessLease(ctx, p); !errors.Is(err, ErrConflict) {
				t.Fatalf("invalid managed source allocated capacity: %v", err)
			}
			assertRowCount(t, ctx, pool, "harness_attempts", 0)
			assertRowCount(t, ctx, pool, "environment_leases", 0)
		})
	}
}

func TestHistoricalSnapshotlessRunReadableButCannotRerun(t *testing.T) {
	ctx, pool, s := newRepositoryTestStore(t)
	conversation := createConversationFixture(t, ctx, pool)
	_, source := createFailedRerunSource(t, ctx, s, conversation, "historical")
	if _, err := pool.Exec(ctx, `UPDATE runs SET specification='{"agent_id":"browser-eval","command":{}}' WHERE id=$1`, source.ID); err != nil {
		t.Fatal(err)
	}
	read, err := s.GetRun(ctx, source.ID)
	if err != nil || read.ID != source.ID {
		t.Fatalf("historical read lost: %+v %v", read, err)
	}
	if _, err := s.CreateRerun(ctx, CreateRerunParams{RunID: source.ID, IdempotencyKey: "no-upgrade"}); !errors.Is(err, ErrConflict) {
		t.Fatalf("historical rerun fabricated current execution: %v", err)
	}
	assertRowCount(t, ctx, pool, "runs", 1)
}

func TestManagedMessageReplaysOldRequestHashAfterRevocationAndReadinessLoss(t *testing.T) {
	ctx, pool, s := newRepositoryTestStore(t)
	conversation := createConversationFixture(t, ctx, pool)
	p := managedMessageParams(t, ctx, s, CreateMessageParams{ConversationID: conversation, Content: "inspect",
		Model: event.ModelRef{Provider: "scripted", Name: "fixture"}, IdempotencyKey: "existing"})
	message, run, err := s.CreateMessageAndRun(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	// Independently reproduce the pre-cleanup managed input DTO, including null
	// operations and omitted caller execution, rather than reuse the new DTO.
	raw, _ := json.Marshal(map[string]any{"conversation_id": conversation, "content": p.Content,
		"agent_id": "pi", "model": p.Model, "operations": nil, "runtime_id": "pi", "harness_version_id": p.HarnessVersionID})
	var oldDTO any
	if err := json.Unmarshal(raw, &oldDTO); err != nil {
		t.Fatal(err)
	}
	oldHash, err := payloadHash(oldDTO)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE idempotency_keys SET request_hash=$2 WHERE scope=$1`, "conversation-message:"+conversation, oldHash); err != nil {
		t.Fatal(err)
	}
	var project string
	if err := pool.QueryRow(ctx, `SELECT project_id FROM conversations WHERE id=$1`, conversation).Scan(&project); err != nil {
		t.Fatal(err)
	}
	if err := s.RevokeProjectHarness(ctx, project, p.HarnessVersionID, "admin"); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE environments SET state='needs_review' WHERE conversation_id=$1`, conversation); err != nil {
		t.Fatal(err)
	}
	replayedMessage, replayedRun, err := s.CreateMessageAndRun(ctx, p)
	if err != nil || replayedMessage.ID != message.ID || replayedRun.ID != run.ID {
		t.Fatalf("old hash replay changed identities: %+v %+v %v", replayedMessage, replayedRun, err)
	}
	assertRowCount(t, ctx, pool, "runs", 1)
	assertRowCount(t, ctx, pool, "outbox_events", 1)
}
