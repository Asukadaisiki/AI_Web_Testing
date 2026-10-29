package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/event"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/testpostgres"
	"github.com/jackc/pgx/v5/pgxpool"
)

func managementTestStore(t *testing.T) (context.Context, *pgxpool.Pool, *Store) {
	t.Helper()
	dsn := os.Getenv("AGENTTRACE_MANAGEMENT_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("AGENTTRACE_MANAGEMENT_TEST_POSTGRES_DSN is not configured")
	}
	if _, err := testpostgres.DatabaseName(dsn); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	resetDatabase(t, ctx, pool)
	// Apply 008 before additive management migrations, independently of parent integration.
	if _, err := pool.Exec(ctx, `CREATE TABLE schema_migrations (version TEXT PRIMARY KEY, applied_at TIMESTAMPTZ NOT NULL DEFAULT now())`); err != nil {
		t.Fatal(err)
	}
	for _, version := range []string{
		"001_initial.sql", "002_outbox_claim_token.sql", "003_consumer_retry_counts.sql",
		"004_run_lineage.sql", "005_harness_events.sql", "006_harness_span_sequence.sql",
		"007_harness_span_attempt.sql", "008_harness_catalog.sql", "011_management_environments.sql",
	} {
		if err := applyMigration(ctx, pool, version); err != nil {
			t.Fatal(err)
		}
	}
	store := NewStore(pool)
	store.SetManagementEnabled(true)
	t.Cleanup(func() { store.SetManagementEnabled(false) })
	return ctx, pool, store
}

func boundConversation(t *testing.T, ctx context.Context, store *Store) Conversation {
	t.Helper()
	project, err := store.CreateProject(ctx, CreateProjectParams{Name: "Managed", LangfuseProjectID: "lf_managed"})
	if err != nil {
		t.Fatal(err)
	}
	conversation, err := store.CreateConversation(ctx, CreateConversationParams{ProjectID: project.ID, Title: "Session", CreatedBy: "actor"})
	if err != nil {
		t.Fatal(err)
	}
	return conversation
}

func lifecycleEnvelope(t *testing.T, ctx context.Context, pool *pgxpool.Pool, environmentID, kind string) event.Envelope[EnvironmentCommand] {
	t.Helper()
	var raw []byte
	if err := pool.QueryRow(ctx, `SELECT payload FROM outbox_events WHERE key = $1 AND payload->>'event_type' = $2 ORDER BY created_at DESC LIMIT 1`,
		environmentID, "environment."+kind+"_requested").Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var envelope event.Envelope[EnvironmentCommand]
	if err := json.Unmarshal(raw, &envelope); err != nil {
		t.Fatal(err)
	}
	return envelope
}

func acknowledgeEnvironment(t *testing.T, ctx context.Context, pool *pgxpool.Pool, store *Store, environmentID, kind string) {
	t.Helper()
	envelope := lifecycleEnvelope(t, ctx, pool, environmentID, kind)
	claim, acquired, err := store.ClaimEnvironmentCommand(ctx, envelope)
	if err != nil || !acquired {
		t.Fatalf("claim = %v, %v", acquired, err)
	}
	if err := store.CompleteEnvironmentCommand(ctx, claim, EnvironmentProviderResult{ProfileRef: "managed/profile"}); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE outbox_events SET published_at = now() WHERE event_id = $1`, envelope.EventID); err != nil {
		t.Fatal(err)
	}
}

func TestManagementConversationAtomicEnvironmentAndBinding(t *testing.T) {
	ctx, pool, store := managementTestStore(t)
	conversation := boundConversation(t, ctx, store)
	environment, err := store.GetConversationEnvironment(ctx, conversation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if conversation.State != "active" || environment.State != "provisioning" ||
		environment.ProjectID != conversation.ProjectID || environment.ProfileGeneration != 1 || environment.FencingToken != 1 {
		t.Fatalf("conversation=%+v environment=%+v", conversation, environment)
	}
	envelope := lifecycleEnvelope(t, ctx, pool, environment.ID, "provision")
	if envelope.AggregateID != environment.ID || envelope.Payload.EnvironmentID != environment.ID ||
		envelope.Payload.ProjectID != conversation.ProjectID || envelope.Payload.ConversationID != conversation.ID {
		t.Fatalf("envelope=%+v", envelope)
	}
	binding, err := store.GetProjectBinding(ctx, conversation.ProjectID)
	if err != nil || binding.LangfuseProjectID != "lf_managed" {
		t.Fatalf("binding=%+v err=%v", binding, err)
	}
	if _, err := store.BindProject(ctx, conversation.ProjectID, "another"); !errors.Is(err, ErrConflict) {
		t.Fatalf("rebind=%v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE project_bindings SET langfuse_project_id = 'mutated' WHERE agenttrace_project_id = $1`, conversation.ProjectID); err == nil {
		t.Fatal("binding identity changed through SQL")
	}
	if _, err := pool.Exec(ctx, `DELETE FROM project_bindings WHERE agenttrace_project_id = $1`, conversation.ProjectID); err == nil {
		t.Fatal("binding identity deleted through SQL")
	}
	if _, err := store.CreateProject(ctx, CreateProjectParams{Name: "Duplicate", LangfuseProjectID: "lf_managed"}); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate binding=%v", err)
	}
	assertRowCount(t, ctx, pool, "projects", 1)
	if _, err := pool.Exec(ctx, `CREATE FUNCTION reject_environment_outbox() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'outbox failure'; END $$; CREATE TRIGGER reject_environment_outbox BEFORE INSERT ON outbox_events FOR EACH ROW EXECUTE FUNCTION reject_environment_outbox()`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateConversation(ctx, CreateConversationParams{ProjectID: conversation.ProjectID}); err == nil {
		t.Fatal("creation succeeded with failed outbox")
	}
	assertRowCount(t, ctx, pool, "conversations", 1)
	assertRowCount(t, ctx, pool, "environments", 1)
}

func TestManagementLifecycleAcknowledgementResetAndTombstone(t *testing.T) {
	ctx, pool, store := managementTestStore(t)
	conversation := boundConversation(t, ctx, store)
	environment, _ := store.GetConversationEnvironment(ctx, conversation.ID)
	if err := store.DeleteConversation(ctx, conversation.ID); !errors.Is(err, ErrConflict) {
		t.Fatalf("delete while provisioning=%v", err)
	}
	acknowledgeEnvironment(t, ctx, pool, store, environment.ID, "provision")
	environment, _ = store.GetEnvironment(ctx, environment.ID)
	if environment.State != "ready" {
		t.Fatalf("ack state=%s", environment.State)
	}
	if _, err := store.ArchiveConversation(ctx, conversation.ID, "actor"); err != nil {
		t.Fatal(err)
	}
	environment, err := store.ResetEnvironment(ctx, environment.ID, "actor")
	if err != nil || environment.State != "resetting" || environment.ProfileGeneration != 2 {
		t.Fatalf("reset=%+v err=%v", environment, err)
	}
	if _, err := store.ResetEnvironment(ctx, environment.ID, "actor"); !errors.Is(err, ErrConflict) {
		t.Fatalf("double reset=%v", err)
	}
	acknowledgeEnvironment(t, ctx, pool, store, environment.ID, "reset")
	if err := store.DeleteConversation(ctx, conversation.ID); err != nil {
		t.Fatal(err)
	}
	deleting, _, _, _ := store.GetConversation(ctx, conversation.ID)
	if deleting.State != "deleting" {
		t.Fatalf("delete became synchronous: %+v", deleting)
	}
	acknowledgeEnvironment(t, ctx, pool, store, environment.ID, "delete")
	deleted, messages, runs, err := store.GetConversation(ctx, conversation.ID)
	if err != nil || deleted.State != "deleted" || deleted.Title != "" || deleted.DeletedAt == nil || len(messages) != 0 || len(runs) != 0 {
		t.Fatalf("tombstone=%+v messages=%v runs=%v err=%v", deleted, messages, runs, err)
	}
	if _, err := store.GetEnvironment(ctx, environment.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("environment retained=%v", err)
	}
	conversations, err := store.ListConversations(ctx, conversation.ProjectID)
	if err != nil || len(conversations) != 0 {
		t.Fatalf("list included tombstone: %+v, %v", conversations, err)
	}
	assertRowCount(t, ctx, pool, "management_audit", 5)
}

func TestManagementDeleteAndResetGuardRunLeaseAndInbox(t *testing.T) {
	for _, blocker := range []string{"run", "lease", "inbox"} {
		t.Run(blocker, func(t *testing.T) {
			ctx, pool, store := managementTestStore(t)
			conversation := boundConversation(t, ctx, store)
			environment, _ := store.GetConversationEnvironment(ctx, conversation.ID)
			acknowledgeEnvironment(t, ctx, pool, store, environment.ID, "provision")
			switch blocker {
			case "run":
				_, err := pool.Exec(ctx, `INSERT INTO runs (id, project_id, conversation_id, status, specification) VALUES ('run_blocker', $1, $2, 'running', '{}')`, conversation.ProjectID, conversation.ID)
				if err != nil {
					t.Fatal(err)
				}
			case "lease":
				_, err := pool.Exec(ctx, `UPDATE environments SET state = 'leased' WHERE id = $1`, environment.ID)
				if err != nil {
					t.Fatal(err)
				}
			case "inbox":
				_, err := pool.Exec(ctx, `INSERT INTO inbox_events (id, consumer, event_id, payload) VALUES ('unfinished', 'other', 'unfinished', jsonb_build_object('payload', jsonb_build_object('environment_id', $1::text)))`, environment.ID)
				if err != nil {
					t.Fatal(err)
				}
			}
			if err := store.DeleteConversation(ctx, conversation.ID); !errors.Is(err, ErrConflict) {
				t.Fatalf("%s delete=%v", blocker, err)
			}
			if _, err := store.ResetEnvironment(ctx, environment.ID, "actor"); !errors.Is(err, ErrConflict) {
				t.Fatalf("%s reset=%v", blocker, err)
			}
		})
	}
}

func TestEnvironmentClaimCommitsBeforeProviderAndDeduplicates(t *testing.T) {
	ctx, pool, store := managementTestStore(t)
	conversation := boundConversation(t, ctx, store)
	environment, _ := store.GetConversationEnvironment(ctx, conversation.ID)
	envelope := lifecycleEnvelope(t, ctx, pool, environment.ID, "provision")
	provider := environmentProviderFunc(func(ctx context.Context, command EnvironmentCommand) (EnvironmentProviderResult, error) {
		var state string
		if err := pool.QueryRow(ctx, `SELECT state FROM environment_commands WHERE event_id = $1`, envelope.EventID).Scan(&state); err != nil {
			return EnvironmentProviderResult{}, err
		}
		if state != "running" {
			t.Fatalf("uncommitted claim=%s", state)
		}
		if _, acquired, err := store.ClaimEnvironmentCommand(ctx, envelope); err != nil || acquired {
			t.Fatalf("concurrent claim=%v err=%v", acquired, err)
		}
		return EnvironmentProviderResult{ProfileRef: "managed/profile"}, nil
	})
	if err := store.ProcessEnvironmentCommand(ctx, envelope, provider); err != nil {
		t.Fatal(err)
	}
	if _, acquired, err := store.ClaimEnvironmentCommand(ctx, envelope); err != nil || acquired {
		t.Fatalf("duplicate claim=%v err=%v", acquired, err)
	}
}

func TestEnvironmentProviderFailureRemainsRetryableWithoutAcknowledgingInflightWork(t *testing.T) {
	ctx, pool, store := managementTestStore(t)
	conversation := boundConversation(t, ctx, store)
	environment, _ := store.GetConversationEnvironment(ctx, conversation.ID)
	envelope := lifecycleEnvelope(t, ctx, pool, environment.ID, "provision")
	providerErr := errors.New("provider unavailable")
	provider := environmentProviderFunc(func(context.Context, EnvironmentCommand) (EnvironmentProviderResult, error) {
		return EnvironmentProviderResult{}, providerErr
	})
	if err := store.ProcessEnvironmentCommand(ctx, envelope, provider); !errors.Is(err, providerErr) {
		t.Fatalf("provider error=%v", err)
	}
	var state string
	if err := pool.QueryRow(ctx, `SELECT state FROM environment_commands WHERE event_id = $1`, envelope.EventID).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != "pending" {
		t.Fatalf("failed idempotent lifecycle operation is not retryable: %s", state)
	}
	claim, acquired, err := store.ClaimEnvironmentCommand(ctx, envelope)
	if err != nil || !acquired {
		t.Fatalf("retry claim=%v err=%v", acquired, err)
	}
	if err := store.ProcessEnvironmentCommand(ctx, envelope, provider); !errors.Is(err, ErrConflict) {
		t.Fatalf("inflight command was acknowledged: %v", err)
	}
	if err := store.CompleteEnvironmentCommand(ctx, claim, EnvironmentProviderResult{}); err != nil {
		t.Fatal(err)
	}
}

func TestEnvironmentConcurrentAcknowledgementIsIdempotent(t *testing.T) {
	for _, kind := range []string{"provision", "reset", "delete"} {
		t.Run(kind, func(t *testing.T) {
			ctx, pool, store := managementTestStore(t)
			conversation := boundConversation(t, ctx, store)
			environment, _ := store.GetConversationEnvironment(ctx, conversation.ID)
			if kind != "provision" {
				acknowledgeEnvironment(t, ctx, pool, store, environment.ID, "provision")
			}
			if kind == "reset" {
				if _, err := store.ResetEnvironment(ctx, environment.ID, "actor"); err != nil {
					t.Fatal(err)
				}
			}
			if kind == "delete" {
				if err := store.DeleteConversation(ctx, conversation.ID); err != nil {
					t.Fatal(err)
				}
			}
			envelope := lifecycleEnvelope(t, ctx, pool, environment.ID, kind)
			claim, acquired, err := store.ClaimEnvironmentCommand(ctx, envelope)
			if err != nil || !acquired {
				t.Fatalf("claim=%v err=%v", acquired, err)
			}
			holder, err := pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer holder.Rollback(ctx)
			if err := lockProject(ctx, holder, conversation.ProjectID); err != nil {
				t.Fatal(err)
			}
			start := make(chan struct{})
			errs := make(chan error, 2)
			var wg sync.WaitGroup
			for range 2 {
				wg.Add(1)
				go func() {
					defer wg.Done()
					<-start
					errs <- store.CompleteEnvironmentCommand(ctx, claim, EnvironmentProviderResult{})
				}()
			}
			close(start)
			deadline := time.Now().Add(5 * time.Second)
			for {
				var waiters int
				if err := pool.QueryRow(ctx, `SELECT count(*) FROM pg_stat_activity WHERE datname = current_database() AND wait_event_type = 'Lock'`).Scan(&waiters); err != nil {
					t.Fatal(err)
				}
				if waiters >= 2 {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("acknowledgements did not reach the lock barrier")
				}
				time.Sleep(5 * time.Millisecond)
			}
			if err := holder.Commit(ctx); err != nil {
				t.Fatal(err)
			}
			wg.Wait()
			close(errs)
			for err := range errs {
				if err != nil {
					t.Fatalf("concurrent %s ack=%v", kind, err)
				}
			}
		})
	}
}

func TestEnvironmentCompletionRejectsMutatedClaimAndStaleToken(t *testing.T) {
	ctx, pool, store := managementTestStore(t)
	conversation := boundConversation(t, ctx, store)
	environment, _ := store.GetConversationEnvironment(ctx, conversation.ID)
	envelope := lifecycleEnvelope(t, ctx, pool, environment.ID, "provision")
	claim, acquired, err := store.ClaimEnvironmentCommand(ctx, envelope)
	if err != nil || !acquired {
		t.Fatalf("claim=%v err=%v", acquired, err)
	}
	for _, mutate := range []func(*EnvironmentCommandClaim){
		func(c *EnvironmentCommandClaim) { c.Token = "stale" },
		func(c *EnvironmentCommandClaim) { c.Command.ActorID = "spoofed" },
		func(c *EnvironmentCommandClaim) { c.Command.ProjectID = "foreign" },
		func(c *EnvironmentCommandClaim) { c.Command.ProfileGeneration++ },
	} {
		altered := claim
		mutate(&altered)
		if err := store.CompleteEnvironmentCommand(ctx, altered, EnvironmentProviderResult{}); !errors.Is(err, ErrConflict) {
			t.Fatalf("mutated claim accepted: %+v err=%v", altered, err)
		}
	}
}

func TestManagementReadinessGateAndLegacyOptOut(t *testing.T) {
	ctx, pool, store := managementTestStore(t)
	conversation := boundConversation(t, ctx, store)
	environment, _ := store.GetConversationEnvironment(ctx, conversation.ID)
	for _, state := range []string{"provisioning", "leased", "resetting", "needs_review", "deleting", "broken", "ready"} {
		if _, err := pool.Exec(ctx, `UPDATE environments SET state = $2 WHERE id = $1`, environment.ID, state); err != nil {
			t.Fatal(err)
		}
		_, err := store.CheckConversationRunnable(ctx, conversation.ID)
		if state == "ready" && err != nil || state != "ready" && !errors.Is(err, ErrConflict) {
			t.Fatalf("state=%s gate=%v", state, err)
		}
	}
	if _, err := store.ArchiveConversation(ctx, conversation.ID, "actor"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CheckConversationRunnable(ctx, conversation.ID); !errors.Is(err, ErrConflict) {
		t.Fatalf("archived gate=%v", err)
	}
	baseline := NewStore(pool)
	if baseline.ManagementEnabled() {
		t.Fatal("management must default false")
	}
	project, err := baseline.CreateProject(ctx, CreateProjectParams{Name: "Legacy"})
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := baseline.CreateConversation(ctx, CreateConversationParams{ProjectID: project.ID})
	if err != nil {
		t.Fatal(err)
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM environments WHERE conversation_id = $1`, legacy.ID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("legacy environment count=%d err=%v", count, err)
	}
}

func TestEnvironmentCrashRecoveryLoadsCanonicalDurableClaim(t *testing.T) {
	ctx, pool, store := managementTestStore(t)
	conversation := boundConversation(t, ctx, store)
	environment, _ := store.GetConversationEnvironment(ctx, conversation.ID)
	envelope := lifecycleEnvelope(t, ctx, pool, environment.ID, "provision")
	original, acquired, err := store.ClaimEnvironmentCommand(ctx, envelope)
	if err != nil || !acquired {
		t.Fatalf("claim=%v err=%v", acquired, err)
	}
	restarted := NewStore(pool)
	restarted.SetManagementEnabled(true)
	recovered, err := restarted.GetEnvironmentCommandClaim(ctx, envelope.EventID)
	if err != nil || recovered.Token != original.Token || recovered.Command.EnvironmentID != environment.ID {
		t.Fatalf("recovery=%+v err=%v", recovered, err)
	}
	if err := restarted.CompleteEnvironmentCommand(ctx, recovered, EnvironmentProviderResult{ProfileRef: "managed/recovered"}); err != nil {
		t.Fatal(err)
	}
}

func TestManagementGuardsPersistentLeaseEvenIfEnvironmentProjectionIsReady(t *testing.T) {
	ctx, pool, store := managementTestStore(t)
	conversation := boundConversation(t, ctx, store)
	environment, _ := store.GetConversationEnvironment(ctx, conversation.ID)
	acknowledgeEnvironment(t, ctx, pool, store, environment.ID, "provision")
	// Phase 2 runs before the scheduler migration. This fixture verifies the
	// optional durable lease guard independently of the scheduler API.
	if _, err := pool.Exec(ctx, `CREATE TABLE environment_leases (environment_id TEXT PRIMARY KEY, state TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO environment_leases VALUES ($1, 'active')`, environment.ID); err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteConversation(ctx, conversation.ID); !errors.Is(err, ErrConflict) {
		t.Fatalf("live lease delete=%v", err)
	}
	if _, err := store.ResetEnvironment(ctx, environment.ID, "actor"); !errors.Is(err, ErrConflict) {
		t.Fatalf("live lease reset=%v", err)
	}
}

func TestManagementConversationDeletionPreservesOtherEnvironment(t *testing.T) {
	ctx, pool, store := managementTestStore(t)
	first := boundConversation(t, ctx, store)
	second, err := store.CreateConversation(ctx, CreateConversationParams{ProjectID: first.ProjectID, Title: "Other"})
	if err != nil {
		t.Fatal(err)
	}
	firstEnvironment, _ := store.GetConversationEnvironment(ctx, first.ID)
	secondEnvironment, _ := store.GetConversationEnvironment(ctx, second.ID)
	acknowledgeEnvironment(t, ctx, pool, store, firstEnvironment.ID, "provision")
	if err := store.DeleteConversation(ctx, first.ID); err != nil {
		t.Fatal(err)
	}
	acknowledgeEnvironment(t, ctx, pool, store, firstEnvironment.ID, "delete")
	other, err := store.GetEnvironment(ctx, secondEnvironment.ID)
	if err != nil || other.ConversationID != second.ID || other.State != "provisioning" {
		t.Fatalf("other environment=%+v err=%v", other, err)
	}
	conversations, err := store.ListConversations(ctx, first.ProjectID)
	if err != nil || len(conversations) != 1 || conversations[0].ID != second.ID {
		t.Fatalf("conversations=%+v err=%v", conversations, err)
	}
}

type environmentProviderFunc func(context.Context, EnvironmentCommand) (EnvironmentProviderResult, error)

func (f environmentProviderFunc) ApplyEnvironment(ctx context.Context, command EnvironmentCommand) (EnvironmentProviderResult, error) {
	return f(ctx, command)
}
