package integration_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/agentservice"
	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestPostgresAgentRunCancellationCAS(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	if err := db.PingContext(ctx); err != nil {
		t.Fatal(err)
	}

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	actorID := insertUser(t, db, "agent-cancel-"+suffix+"@example.com")
	runIDs := make([]string, 0, 3)
	t.Cleanup(func() {
		for _, runID := range runIDs {
			_, _ = db.ExecContext(ctx, `DELETE FROM agent_events WHERE run_id = $1`, runID)
			_, _ = db.ExecContext(ctx, `DELETE FROM agent_runs WHERE id = $1`, runID)
		}
		_, _ = db.ExecContext(ctx, `DELETE FROM users WHERE id = $1`, actorID)
	})

	service := agentservice.NewService(agentservice.NewPostgresRepository(db))
	waiting, err := service.StartOwnedProjectRun(
		ctx,
		actorID,
		"cancel-"+suffix,
		0,
		"wait for cancellation",
	)
	if err != nil {
		t.Fatalf("StartOwnedProjectRun() error = %v", err)
	}
	runIDs = append(runIDs, waiting.ID)
	pendingToolID := "pending-tool"
	pendingStepID := "pending-step"
	waiting.Status = agentservice.RunStatusWaitingUser
	waiting.PendingToolCallID = &pendingToolID
	waiting.PendingStepID = &pendingStepID
	if err := service.SaveRun(ctx, waiting); err != nil {
		t.Fatalf("SaveRun(waiting) error = %v", err)
	}
	stale := waiting

	cancelled, err := service.CancelOwnedRun(ctx, waiting.ID, actorID, "integration timeout")
	if err != nil {
		t.Fatalf("CancelOwnedRun() error = %v", err)
	}
	if cancelled.Status != agentservice.RunStatusCancelled ||
		cancelled.PendingToolCallID != nil ||
		cancelled.PendingStepID != nil {
		t.Fatalf("cancelled run = %#v", cancelled)
	}
	if err := service.SaveRun(ctx, stale); !errors.Is(err, agentservice.ErrRunCancelled) {
		t.Fatalf("stale SaveRun() error = %v, want ErrRunCancelled", err)
	}
	replayed, err := service.CancelOwnedRun(ctx, waiting.ID, actorID, "duplicate")
	if err != nil || replayed.Status != agentservice.RunStatusCancelled {
		t.Fatalf("idempotent cancel = %#v, %v", replayed, err)
	}
	events, err := service.ListEvents(ctx, waiting.ID, 0)
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}
	if len(events) != 2 ||
		events[1].Type != agentservice.EventRunCancelled ||
		events[1].Payload["reason"] != "integration timeout" {
		t.Fatalf("cancel events = %#v", events)
	}

	completed, err := service.StartOwnedProjectRun(
		ctx,
		actorID,
		"complete-"+suffix,
		0,
		"complete first",
	)
	if err != nil {
		t.Fatalf("StartOwnedProjectRun(completed) error = %v", err)
	}
	runIDs = append(runIDs, completed.ID)
	completed, err = service.CompleteRun(ctx, completed)
	if err != nil {
		t.Fatalf("CompleteRun() error = %v", err)
	}
	completedAfterCancel, err := service.CancelRun(ctx, completed.ID, "too late")
	if err != nil || completedAfterCancel.Status != agentservice.RunStatusCompleted {
		t.Fatalf("cancel completed run = %#v, %v", completedAfterCancel, err)
	}

	cancelFirst, err := service.StartOwnedProjectRun(
		ctx,
		actorID,
		"cancel-first-"+suffix,
		0,
		"cancel first",
	)
	if err != nil {
		t.Fatalf("StartOwnedProjectRun(cancel first) error = %v", err)
	}
	runIDs = append(runIDs, cancelFirst.ID)
	staleRunning := cancelFirst
	if _, err := service.CancelRun(ctx, cancelFirst.ID, "won race"); err != nil {
		t.Fatalf("CancelRun(cancel first) error = %v", err)
	}
	afterComplete, err := service.CompleteRun(ctx, staleRunning)
	if err != nil || afterComplete.Status != agentservice.RunStatusCancelled {
		t.Fatalf("complete cancelled run = %#v, %v", afterComplete, err)
	}
}
