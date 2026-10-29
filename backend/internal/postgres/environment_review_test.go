package postgres

import (
	"errors"
	"testing"
)

func TestEnvironmentReviewRequiresExplicitQuiescentDecision(t *testing.T) {
	ctx, pool, store, env := controlTestStore(t)
	if _, err := store.ReviewEnvironment(ctx, env.ID, "actor"); !errors.Is(err, ErrConflict) {
		t.Fatalf("ready environment reviewed: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE environments SET state='needs_review' WHERE id=$1`, env.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ReviewEnvironment(ctx, env.ID, ""); !errors.Is(err, ErrConflict) {
		t.Fatalf("anonymous review: %v", err)
	}
	reviewed, err := store.ReviewEnvironment(ctx, env.ID, "reviewer")
	if err != nil || reviewed.State != "ready" || reviewed.FencingToken != env.FencingToken+1 || reviewed.ProfileGeneration != env.ProfileGeneration {
		t.Fatalf("review=%+v err=%v", reviewed, err)
	}
	var audits int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM management_audit WHERE action='environment.reviewed' AND actor_id='reviewer'`).Scan(&audits); err != nil || audits != 1 {
		t.Fatalf("review audit count=%d err=%v", audits, err)
	}
}

func TestNeedsReviewAllowsExclusiveInspectionBeforeRecovery(t *testing.T) {
	ctx, pool, store, env := controlTestStore(t)
	if _, err := pool.Exec(ctx, `UPDATE environments SET state='needs_review' WHERE id=$1`, env.ID); err != nil {
		t.Fatal(err)
	}
	session, _, err := store.CreateControlSession(ctx, CreateControlSessionParams{EnvironmentID: env.ID, ProjectID: env.ProjectID, ActorID: "inspector"})
	if err != nil || session.State != "pending" {
		t.Fatalf("needs-review inspection=%+v err=%v", session, err)
	}
	if _, err := store.ReviewEnvironment(ctx, env.ID, "reviewer"); !errors.Is(err, ErrConflict) {
		t.Fatalf("review while inspection active: %v", err)
	}
	claim, err := store.BeginCloseControlSession(ctx, session.ID, env.ProjectID, "inspector")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CompleteCloseControlSession(ctx, claim); err != nil {
		t.Fatal(err)
	}
	current, err := store.GetEnvironment(ctx, env.ID)
	if err != nil || current.State != "needs_review" {
		t.Fatalf("inspection implicitly reviewed environment=%+v err=%v", current, err)
	}
	if _, err := store.ReviewEnvironment(ctx, env.ID, "reviewer"); err != nil {
		t.Fatalf("explicit review after inspection: %v", err)
	}
}
