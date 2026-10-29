package postgres

import (
	"testing"
	"time"
)

func TestControlCloseAcknowledgementAfterExpiryPreservesReview(t *testing.T) {
	ctx, _, store, env := controlTestStore(t)
	session, token, err := store.CreateControlSession(ctx, CreateControlSessionParams{
		EnvironmentID: env.ID, ProjectID: env.ProjectID, ActorID: "actor", TTL: 250 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.ConsumeControlSession(ctx, session.ID, env.ProjectID, "actor", token); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ActivateControlSession(ctx, session.ID, env.ProjectID, "actor", "bsess_close_expiry"); err != nil {
		t.Fatal(err)
	}
	claim, err := store.BeginCloseControlSession(ctx, session.ID, env.ProjectID, "actor")
	if err != nil {
		t.Fatal(err)
	}
	// The real provider acknowledgement crosses TTL without the sweeper
	// intervening. Finalization must independently preserve quarantine.
	time.Sleep(time.Until(session.ExpiresAt) + 50*time.Millisecond)
	closed, err := store.CompleteCloseControlSession(ctx, claim)
	if err != nil {
		t.Fatal(err)
	}
	current, err := store.GetEnvironment(ctx, env.ID)
	if err != nil {
		t.Fatal(err)
	}
	if closed.State != "closed" || !closed.ReviewRequired || current.State != "needs_review" {
		t.Fatalf("expired close state=%s review=%t environment=%s", closed.State, closed.ReviewRequired, current.State)
	}
	if n, err := store.RecoverExpiredControlSessions(ctx); err != nil || n != 0 {
		t.Fatalf("closed session must already preserve expiry: recovered=%d err=%v", n, err)
	}
}
