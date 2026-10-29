package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/testpostgres"
	"github.com/jackc/pgx/v5/pgxpool"
)

func controlTestStore(t *testing.T) (context.Context, *pgxpool.Pool, *Store, Environment) {
	t.Helper()
	dsn := os.Getenv("AGENTTRACE_CONTROL_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("AGENTTRACE_CONTROL_TEST_POSTGRES_DSN is not configured")
	}
	name, err := testpostgres.DatabaseName(dsn)
	if err != nil || !strings.HasPrefix(name, "agenttrace_test_") {
		t.Fatalf("control tests require isolated agenttrace_test_* database: %v", err)
	}
	for _, key := range []string{"AGENTTRACE_TEST_POSTGRES_DSN", "AGENTTRACE_MANAGEMENT_TEST_POSTGRES_DSN", "AGENTTRACE_REGISTRY_TEST_POSTGRES_DSN", "AGENTTRACE_LEASE_TEST_POSTGRES_DSN"} {
		if other := os.Getenv(key); other != "" {
			otherName, _ := testpostgres.DatabaseName(other)
			if otherName == name {
				t.Fatalf("control database aliases %s", key)
			}
		}
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	resetDatabase(t, ctx, pool)
	if err := Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	store := NewStore(pool)
	store.SetManagementEnabled(true)
	conversation := boundConversation(t, ctx, store)
	environment, err := store.GetConversationEnvironment(ctx, conversation.ID)
	if err != nil {
		t.Fatal(err)
	}
	acknowledgeEnvironment(t, ctx, pool, store, environment.ID, "provision")
	environment, _ = store.GetEnvironment(ctx, environment.ID)
	return ctx, pool, store, environment
}

func createControl(t *testing.T, ctx context.Context, store *Store, env Environment) (ControlSession, string) {
	t.Helper()
	session, token, err := store.CreateControlSession(ctx, CreateControlSessionParams{
		EnvironmentID: env.ID, ProjectID: env.ProjectID, ActorID: "actor", TTL: time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	return session, token
}

func TestControlSessionOneTimeScopedTokenAndExclusiveReservation(t *testing.T) {
	ctx, pool, store, env := controlTestStore(t)
	session, token := createControl(t, ctx, store, env)
	if session.State != "pending" || session.ControlURL != "/api/v1/control-sessions/"+session.ID || len(token) < 40 {
		t.Fatalf("session=%+v token length=%d", session, len(token))
	}
	var hash string
	if err := pool.QueryRow(ctx, `SELECT token_hash FROM control_sessions WHERE id=$1`, session.ID).Scan(&hash); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(token))
	if hash == token || hash != hex.EncodeToString(sum[:]) {
		t.Fatal("token is not SHA256 hashed")
	}
	current, _ := store.GetEnvironment(ctx, env.ID)
	if current.State != "leased" || current.FencingToken != env.FencingToken+1 {
		t.Fatalf("reservation=%+v", current)
	}
	for _, scope := range []struct{ project, actor, token string }{
		{"foreign", "actor", token}, {env.ProjectID, "foreign", token}, {env.ProjectID, "actor", "wrong"},
	} {
		if _, _, err := store.ConsumeControlSession(ctx, session.ID, scope.project, scope.actor, scope.token); !errors.Is(err, ErrConflict) {
			t.Fatalf("invalid scope accepted: %v", err)
		}
	}
	owner, environmentID, err := store.ControlSessionOwner(ctx, session.ID)
	if err != nil || owner != env.ProjectID || environmentID != env.ID {
		t.Fatalf("owner=%s env=%s err=%v", owner, environmentID, err)
	}
	if _, _, err := store.CreateControlSession(ctx, CreateControlSessionParams{EnvironmentID: env.ID, ProjectID: env.ProjectID, ActorID: "another"}); !errors.Is(err, ErrConflict) {
		t.Fatalf("second reservation=%v", err)
	}
	if _, err := store.ResetEnvironment(ctx, env.ID, "actor"); !errors.Is(err, ErrConflict) {
		t.Fatalf("reset live reservation=%v", err)
	}
	if err := store.DeleteEnvironment(ctx, env.ID, "actor"); !errors.Is(err, ErrConflict) {
		t.Fatalf("delete live reservation=%v", err)
	}
	var winners int
	var mu sync.Mutex
	var wg sync.WaitGroup
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			claim, leased, err := store.ConsumeControlSession(ctx, session.ID, env.ProjectID, "actor", token)
			if err == nil {
				mu.Lock()
				winners++
				mu.Unlock()
				if claim.State != "opening" || leased.FencingToken != current.FencingToken {
					t.Errorf("claim=%+v leased=%+v", claim, leased)
				}
			} else if !errors.Is(err, ErrConflict) {
				t.Errorf("consume=%v", err)
			}
		}()
	}
	wg.Wait()
	if winners != 1 {
		t.Fatalf("consume winners=%d", winners)
	}
	if _, err := store.ActivateControlSession(ctx, session.ID, env.ProjectID, "actor", "bsess_private"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.ConsumeControlSession(ctx, session.ID, env.ProjectID, "actor", token); !errors.Is(err, ErrConflict) {
		t.Fatalf("replayed token=%v", err)
	}
}

func TestControlSessionExpiryAndCrashFailClosedWithoutReplay(t *testing.T) {
	for _, opening := range []bool{false, true} {
		t.Run(map[bool]string{false: "pending", true: "opening"}[opening], func(t *testing.T) {
			ctx, pool, store, env := controlTestStore(t)
			session, token := createControl(t, ctx, store, env)
			if opening {
				if _, _, err := store.ConsumeControlSession(ctx, session.ID, env.ProjectID, "actor", token); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := pool.Exec(ctx, `UPDATE control_sessions SET expires_at=now()-interval '1 second' WHERE id=$1`, session.ID); err != nil {
				t.Fatal(err)
			}
			if _, _, err := store.ConsumeControlSession(ctx, session.ID, env.ProjectID, "actor", token); !errors.Is(err, ErrConflict) {
				t.Fatalf("expired consume=%v", err)
			}
			if n, err := store.RecoverExpiredControlSessions(ctx); err != nil || n != 1 {
				t.Fatalf("recovery=%d err=%v", n, err)
			}
			env, _ = store.GetEnvironment(ctx, env.ID)
			if env.State != "needs_review" {
				t.Fatalf("unsafe expiry state=%s", env.State)
			}
			if _, _, err := store.CreateControlSession(ctx, CreateControlSessionParams{EnvironmentID: env.ID, ProjectID: env.ProjectID, ActorID: "actor"}); !errors.Is(err, ErrConflict) {
				t.Fatalf("expired takeover automatically replayed=%v", err)
			}
			if opening {
				if _, err := store.ActivateControlSession(ctx, session.ID, env.ProjectID, "actor", "bsess_late"); !errors.Is(err, ErrConflict) {
					t.Fatalf("late activation=%v", err)
				}
			}
		})
	}
}

func TestControlSessionGatewayClaimStopAckAndGenerationFence(t *testing.T) {
	ctx, _, store, env := controlTestStore(t)
	session, token := createControl(t, ctx, store, env)
	if _, _, err := store.ConsumeControlSession(ctx, session.ID, env.ProjectID, "actor", token); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ActivateControlSession(ctx, session.ID, env.ProjectID, "actor", "bsess_private"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.ClaimControlSessionOperation(ctx, session.ID, env.ProjectID, "other"); !errors.Is(err, ErrConflict) {
		t.Fatalf("other actor gateway=%v", err)
	}
	claim, leased, err := store.ClaimControlSessionOperation(ctx, session.ID, env.ProjectID, "actor")
	if err != nil || claim.ProviderSessionID != "bsess_private" || leased.State != "leased" {
		t.Fatalf("gateway=%+v env=%+v err=%v", claim, leased, err)
	}
	if _, err := store.BeginCloseControlSession(ctx, session.ID, env.ProjectID, "actor"); !errors.Is(err, ErrConflict) {
		t.Fatalf("stop acknowledged inflight input=%v", err)
	}
	if err := store.CompleteControlSessionOperation(ctx, claim, true); err != nil {
		t.Fatal(err)
	}
	closing, err := store.BeginCloseControlSession(ctx, session.ID, env.ProjectID, "actor")
	if err != nil || closing.State != "closing" {
		t.Fatalf("stop=%+v err=%v", closing, err)
	}
	env, _ = store.GetEnvironment(ctx, env.ID)
	if env.State != "leased" {
		t.Fatalf("ready before provider acknowledgement=%s", env.State)
	}
	closed, err := store.CompleteCloseControlSession(ctx, closing)
	if err != nil || closed.State != "closed" {
		t.Fatalf("close ack=%+v err=%v", closed, err)
	}
	reset, err := store.ResetEnvironment(ctx, env.ID, "actor")
	if err != nil || reset.ProfileGeneration != env.ProfileGeneration+1 {
		t.Fatalf("reset=%+v err=%v", reset, err)
	}
	if err := store.CompleteControlSessionOperation(ctx, claim, true); !errors.Is(err, ErrConflict) {
		t.Fatalf("old generation operation restored=%v", err)
	}
	if _, err := store.CompleteCloseControlSession(ctx, closing); !errors.Is(err, ErrConflict) {
		t.Fatalf("old close restored environment=%v", err)
	}
}

func TestControlSessionReservationRejectsNonterminalRunAndInvalidTTL(t *testing.T) {
	ctx, pool, store, env := controlTestStore(t)
	for _, ttl := range []time.Duration{-time.Second, time.Hour} {
		if _, _, err := store.CreateControlSession(ctx, CreateControlSessionParams{EnvironmentID: env.ID, ProjectID: env.ProjectID, ActorID: "actor", TTL: ttl}); !errors.Is(err, ErrConflict) {
			t.Fatalf("invalid TTL accepted=%v", err)
		}
	}
	if _, err := pool.Exec(ctx, `INSERT INTO runs (id,project_id,conversation_id,status,specification) VALUES ('run_block',$1,$2,'queued','{}')`, env.ProjectID, env.ConversationID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.CreateControlSession(ctx, CreateControlSessionParams{EnvironmentID: env.ID, ProjectID: env.ProjectID, ActorID: "actor"}); !errors.Is(err, ErrConflict) {
		t.Fatalf("takeover racing queued run=%v", err)
	}
}

func TestControlSessionConcurrentCreateHasOneWinner(t *testing.T) {
	ctx, _, store, env := controlTestStore(t)
	var wg sync.WaitGroup
	results := make(chan error, 12)
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, err := store.CreateControlSession(ctx, CreateControlSessionParams{EnvironmentID: env.ID, ProjectID: env.ProjectID, ActorID: "actor"})
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	winners := 0
	for err := range results {
		if err == nil {
			winners++
		} else if !errors.Is(err, ErrConflict) {
			t.Errorf("create=%v", err)
		}
	}
	if winners != 1 {
		t.Fatalf("reservation winners=%d", winners)
	}
}

func TestControlSessionLiveLeaseBlocksReservationEvenWithReadyEnvironment(t *testing.T) {
	ctx, pool, store, env := controlTestStore(t)
	if _, err := pool.Exec(ctx, `INSERT INTO runs (id,project_id,conversation_id,status,specification) VALUES ('run_done',$1,$2,'succeeded','{}')`, env.ProjectID, env.ConversationID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO harness_attempts (id,run_id,attempt_number,provider_attempt_id,status)
		VALUES ('attempt_done','run_done',1,'provider_done','succeeded');
		INSERT INTO worker_nodes (id,role,capacity) VALUES ('runtime_blocker','runtime',1),('browser_blocker','browser',1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO environment_leases (environment_id,attempt_id,run_id,project_id,runtime_worker_id,browser_worker_id,profile_generation,fencing_token,expires_at)
		VALUES ($1,'attempt_done','run_done',$2,'runtime_blocker','browser_blocker',1,1,now()+interval '1 minute')`, env.ID, env.ProjectID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.CreateControlSession(ctx, CreateControlSessionParams{EnvironmentID: env.ID, ProjectID: env.ProjectID, ActorID: "actor"}); !errors.Is(err, ErrConflict) {
		t.Fatalf("live lease bypassed=%v", err)
	}
}

func TestControlSessionCancelledCreationAndProviderAckCannotReadyNewGeneration(t *testing.T) {
	ctx, pool, store, env := controlTestStore(t)
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, _, err := store.CreateControlSession(cancelled, CreateControlSessionParams{EnvironmentID: env.ID, ProjectID: env.ProjectID, ActorID: "actor"}); err == nil {
		t.Fatal("cancelled reservation accepted")
	}
	current, _ := store.GetEnvironment(ctx, env.ID)
	if current.State != "ready" || current.FencingToken != env.FencingToken {
		t.Fatalf("cancelled creation mutated environment=%+v", current)
	}
	c, token := createControl(t, ctx, store, env)
	if _, _, err := store.ConsumeControlSession(ctx, c.ID, env.ProjectID, "actor", token); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ActivateControlSession(ctx, c.ID, env.ProjectID, "actor", "bsess_live"); err != nil {
		t.Fatal(err)
	}
	claim, _, err := store.ClaimControlSessionOperation(ctx, c.ID, env.ProjectID, "actor")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE control_sessions SET expires_at=now()-interval '1 second' WHERE id=$1`, c.ID); err != nil {
		t.Fatal(err)
	}
	if n, err := store.RecoverExpiredControlSessions(ctx); n != 1 || err != nil {
		t.Fatalf("recover=%d err=%v", n, err)
	}
	reset, err := store.ResetEnvironment(ctx, env.ID, "actor")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.CompleteControlSessionOperation(ctx, claim, true); !errors.Is(err, ErrConflict) {
		t.Fatalf("late provider ack=%v", err)
	}
	current, _ = store.GetEnvironment(ctx, env.ID)
	if current.State != "resetting" || current.ProfileGeneration != reset.ProfileGeneration {
		t.Fatalf("old request restored ready=%+v", current)
	}
}
