package runtime

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/event"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/piharness"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/postgres"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/testpostgres"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func piLeaseTestScan(t *testing.T, row pgx.Row, destinations ...any) {
	t.Helper()
	if err := row.Scan(destinations...); err != nil {
		t.Fatal(err)
	}
}

func TestPiManagedPostgresLifecycleRetainsCapacityUntilAcknowledged(t *testing.T) {
	dsn := os.Getenv("AGENTTRACE_LEASE_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("AGENTTRACE_LEASE_TEST_POSTGRES_DSN is not configured")
	}
	if _, err := testpostgres.DatabaseName(dsn); err != nil {
		t.Fatal(err)
	}
	for _, ack := range []bool{true, false} {
		t.Run(map[bool]string{true: "released", false: "crashed"}[ack], func(t *testing.T) {
			ctx := context.Background()
			pool, err := pgxpool.New(ctx, dsn)
			if err != nil {
				t.Fatal(err)
			}
			defer pool.Close()
			if _, err := pool.Exec(ctx, `DROP SCHEMA public CASCADE; CREATE SCHEMA public`); err != nil {
				t.Fatal(err)
			}
			if err := postgres.Migrate(ctx, pool); err != nil {
				t.Fatal(err)
			}
			store := postgres.NewStore(pool)
			rememberFixturePool(t, store, pool)
			project, err := store.CreateProject(ctx, managedProjectParams(postgres.CreateProjectParams{Name: "lease"}))
			if err != nil {
				t.Fatal(err)
			}
			conversation, err := store.CreateConversation(ctx, postgres.CreateConversationParams{ProjectID: project.ID, Title: "lease"})
			if err != nil {
				t.Fatal(err)
			}
			_, run, err := store.CreateMessageAndRun(ctx, managedMessageParams(t, ctx, store, postgres.CreateMessageParams{ConversationID: conversation.ID, Content: "test", Model: event.ModelRef{Provider: "scripted", Name: "test"}, IdempotencyKey: "lease"}))
			if err != nil {
				t.Fatal(err)
			}
			environment, err := store.GetConversationEnvironment(ctx, conversation.ID)
			if err != nil {
				t.Fatal(err)
			}

			for _, node := range []postgres.WorkerRegistration{
				{ID: "runtime", Role: "runtime", Capacity: 1},
				{ID: "browser", Role: "browser", Endpoint: "http://assigned:8100", Capacity: 1},
			} {
				if _, err := store.RegisterWorker(ctx, node); err != nil {
					t.Fatal(err)
				}
			}
			if err := store.BindEnvironmentWorker(ctx, environment.ID, "browser"); err != nil {
				t.Fatal(err)
			}
			process := &releaseAwareProcess{ack: ack}
			process.events = []piharness.Event{
				harEvent(piharness.EventStarted, run.ID, "allocated-by-store", 1, `{}`),
				harEvent(piharness.EventSettled, run.ID, "allocated-by-store", 2, `{"assistant_text":"done","verdict":"succeeded"}`),
			}
			process.afterEvents = func() {
				var started bool
				var inUse, openTransactions int
				piLeaseTestScan(t, pool.QueryRow(ctx, `SELECT sdk_started_at IS NOT NULL FROM harness_attempts WHERE run_id=$1`, run.ID), &started)
				piLeaseTestScan(t, pool.QueryRow(ctx, `SELECT sum(in_use) FROM worker_nodes`), &inUse)
				piLeaseTestScan(t, pool.QueryRow(ctx, `SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND state='idle in transaction'`), &openTransactions)
				if !started || inUse != 2 || openTransactions != 0 {
					t.Errorf("SDK lifecycle durable state: started=%v in_use=%d open_transactions=%d", started, inUse, openTransactions)
				}
			}
			if !ack {
				process.err = piharness.ErrPrematureExit
			}
			command := commandFor(run.ID)
			command.ProjectID, command.EnvironmentID, command.ProfileGeneration = project.ID, environment.ID, 1
			command.ConversationID = conversation.ID
			traceStore := PostgresPiTraceStore{Store: store, RuntimeWorkerID: "runtime", LeaseTTL: time.Minute}
			command = managedFixtureCommand(t, ctx, traceStore, command)
			runner := PiRunner{Process: process, Store: traceStore}
			err = runner.Run(ctx, command)
			if ack && err != nil || !ack && !isTerminalRunError(err) {
				t.Fatalf("run lifecycle: %v", err)
			}
			var runState, envState, attemptState string
			var used int
			piLeaseTestScan(t, pool.QueryRow(ctx, `SELECT status FROM runs WHERE id=$1`, run.ID), &runState)
			piLeaseTestScan(t, pool.QueryRow(ctx, `SELECT state FROM environments WHERE id=$1`, environment.ID), &envState)
			piLeaseTestScan(t, pool.QueryRow(ctx, `SELECT status FROM harness_attempts WHERE run_id=$1`, run.ID), &attemptState)
			piLeaseTestScan(t, pool.QueryRow(ctx, `SELECT sum(in_use) FROM worker_nodes`), &used)
			if ack {
				if runState != "succeeded" || envState != "ready" || used != 0 || process.request.WorkerURL != "http://assigned:8100" {
					t.Fatalf("acknowledged lifecycle: %s %s %d %#v", runState, envState, used, process.request)
				}
			} else {
				if runState != "failed" || envState != "needs_review" || attemptState != "lost" || used != 2 {
					t.Fatalf("crashed lifecycle: %s %s %s %d", runState, envState, attemptState, used)
				}
				if _, err := store.ClaimHarnessLease(ctx, postgres.LeaseClaim{RunID: run.ID, ProjectID: project.ID, EnvironmentID: environment.ID, ProfileGeneration: 1, RuntimeWorkerID: "runtime", ProviderAttemptID: "never-replayed"}); !errors.Is(err, postgres.ErrRunAlreadyTerminal) {
					t.Fatalf("crashed SDK replayed: %v", err)
				}
			}
		})
	}
}
