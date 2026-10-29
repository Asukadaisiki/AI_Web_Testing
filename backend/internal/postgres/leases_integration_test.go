package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/domain"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/event"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func leaseTestScan(t *testing.T, row pgx.Row, destinations ...any) {
	t.Helper()
	if err := row.Scan(destinations...); err != nil {
		t.Fatal(err)
	}
}

func leaseTestStore(t *testing.T) (context.Context, *pgxpool.Pool, *Store) {
	t.Helper()
	dsn := os.Getenv("AGENTTRACE_LEASE_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("AGENTTRACE_LEASE_TEST_POSTGRES_DSN is not configured")
	}
	name, err := repositoryTestDatabaseName(dsn)
	if err != nil || name == "" {
		t.Fatalf("unsafe lease test database: %v", err)
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
	return ctx, pool, NewStore(pool)
}

func leaseFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool, s *Store, suffix string) LeaseClaim {
	t.Helper()
	p, err := s.CreateProject(ctx, managedProjectParams(CreateProjectParams{Name: suffix}))
	if err != nil {
		t.Fatal(err)
	}
	c, err := s.CreateConversation(ctx, CreateConversationParams{ProjectID: p.ID, Title: suffix})
	if err != nil {
		t.Fatal(err)
	}
	_, run, err := s.CreateMessageAndRun(ctx, managedMessageParams(t, ctx, s, CreateMessageParams{ConversationID: c.ID, Content: "test", Model: event.ModelRef{Provider: "scripted", Name: "test"}, IdempotencyKey: suffix}))
	if err != nil {
		t.Fatal(err)
	}
	env, err := s.GetConversationEnvironment(ctx, c.ID)
	if err != nil {
		t.Fatal(err)
	}

	return LeaseClaim{RunID: run.ID, ProjectID: p.ID, EnvironmentID: env.ID, ProfileGeneration: 1, RuntimeID: "pi", RuntimeWorkerID: "runtime", ProviderAttemptID: "provider_" + suffix, TTL: time.Minute}
}

func registerUnboundLeaseWorkers(t *testing.T, ctx context.Context, s *Store, capacity int) {
	t.Helper()
	for _, w := range []WorkerRegistration{
		{ID: "runtime", Role: "runtime", Capacity: capacity, Version: "test"},
		{ID: "browser", Role: "browser", Endpoint: "http://trusted-browser:8100", Capacity: capacity, Version: "test"},
	} {
		if _, err := s.RegisterWorker(ctx, w); err != nil {
			t.Fatal(err)
		}
	}
}

func registerLeaseWorkers(t *testing.T, ctx context.Context, s *Store, capacity int) {
	t.Helper()
	registerUnboundLeaseWorkers(t, ctx, s, capacity)
	rows, err := s.pool.Query(ctx, `SELECT id FROM environments WHERE browser_worker_id IS NULL`)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for rows.Next() {
		var id string
		leaseTestScan(t, rows, &id)
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	for _, id := range ids {
		if err := s.BindEnvironmentWorker(ctx, id, "browser"); err != nil {
			t.Fatal(err)
		}
	}
}

func TestLeaseConcurrentClaimAllocatesOneAssignment(t *testing.T) {
	ctx, pool, s := leaseTestStore(t)
	p := leaseFixture(t, ctx, pool, s, "concurrent")
	registerLeaseWorkers(t, ctx, s, 4)
	if _, err := s.RegisterWorker(ctx, WorkerRegistration{ID: "runtime2", Role: "runtime", Capacity: 4}); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	results := make(chan error, 12)
	for i := range 12 {
		wg.Add(1)
		request := p
		if i%2 != 0 {
			request.RuntimeWorkerID = "runtime2"
		}
		go func(request LeaseClaim) {
			defer wg.Done()
			_, err := s.ClaimHarnessLease(ctx, request)
			results <- err
		}(request)
	}
	wg.Wait()
	close(results)
	success := 0
	for err := range results {
		if err == nil {
			success++
		} else if !errors.Is(err, ErrCapacityBusy) {
			t.Fatalf("unexpected claim error: %v", err)
		}
	}
	if success != 1 {
		t.Fatalf("successful claims: %d", success)
	}
	assertRowCount(t, ctx, pool, "harness_attempts", 1)
	assertRowCount(t, ctx, pool, "worker_assignments", 1)
	assertRowCount(t, ctx, pool, "environment_leases", 1)
	var total int
	if err := pool.QueryRow(ctx, `SELECT sum(in_use) FROM worker_nodes`).Scan(&total); err != nil || total != 2 {
		t.Fatalf("capacity allocated: %d %v", total, err)
	}
}

func TestLeaseBusyAndDrainLeaveRunQueuedWithoutAttempt(t *testing.T) {
	ctx, pool, s := leaseTestStore(t)
	p := leaseFixture(t, ctx, pool, s, "busy")
	registerLeaseWorkers(t, ctx, s, 1)
	if _, err := s.SetWorkerDraining(ctx, "", "browser", "admin", true); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ClaimHarnessLease(ctx, p); !errors.Is(err, ErrCapacityBusy) {
		t.Fatalf("drained claim: %v", err)
	}
	assertRowCount(t, ctx, pool, "harness_attempts", 0)
	var status string
	var used int
	leaseTestScan(t, pool.QueryRow(ctx, `SELECT status FROM runs WHERE id=$1`, p.RunID), &status)
	leaseTestScan(t, pool.QueryRow(ctx, `SELECT in_use FROM worker_nodes WHERE id='runtime'`), &used)
	if status != "queued" || used != 0 {
		t.Fatalf("busy mutated run/capacity: %s %d", status, used)
	}
	if _, err := s.RegisterWorker(ctx, WorkerRegistration{ID: "browser", Role: "browser", Endpoint: "http://trusted-browser:8100", Capacity: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ClaimHarnessLease(ctx, p); !errors.Is(err, ErrCapacityBusy) {
		t.Fatalf("register cleared drain: %v", err)
	}
}

func TestLeaseFencesEvidenceFinishAndReleaseAndWaitsForAcknowledgement(t *testing.T) {
	ctx, pool, s := leaseTestStore(t)
	p := leaseFixture(t, ctx, pool, s, "fence")
	registerLeaseWorkers(t, ctx, s, 1)
	claim, err := s.ClaimHarnessLease(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	stale := claim.Fence
	stale.FencingToken++
	ev := event.Envelope[json.RawMessage]{SchemaVersion: "agenttrace.event.v1", EventID: "ev", EventType: "harness.started", AggregateID: p.RunID, CorrelationID: p.RunID, OccurredAt: time.Now().UTC(), Payload: json.RawMessage(`{}`)}
	record := HarnessEventRecord{RunID: p.RunID, AttemptID: claim.Attempt.ID, SourceSequence: 1, SourceType: "harness.started", Event: ev, Fence: stale}
	if err := s.AppendHarnessEvent(ctx, record); !errors.Is(err, ErrStaleFence) {
		t.Fatalf("stale evidence: %v", err)
	}
	finish := HarnessFinish{RunID: p.RunID, AttemptID: claim.Attempt.ID, Status: domain.RunSucceeded, Assistant: "ok", Event: ev, Fence: stale}
	if _, err := s.FinishHarnessAttempt(ctx, finish); !errors.Is(err, ErrStaleFence) {
		t.Fatalf("stale finish: %v", err)
	}
	if err := s.ReleaseHarnessLease(ctx, stale, true); !errors.Is(err, ErrStaleFence) {
		t.Fatalf("stale release: %v", err)
	}
	assertRowCount(t, ctx, pool, "run_events", 0)
	record.Fence = claim.Fence
	if err := s.AppendHarnessEvent(ctx, record); err != nil {
		t.Fatal(err)
	}
	finish.Fence = claim.Fence
	if _, err := s.FinishHarnessAttempt(ctx, finish); err != nil {
		t.Fatal(err)
	}
	if err := s.ReleaseHarnessLease(ctx, claim.Fence, false); !errors.Is(err, ErrReleaseUnacknowledged) {
		t.Fatalf("release without host ack: %v", err)
	}
	var used int
	leaseTestScan(t, pool.QueryRow(ctx, `SELECT sum(in_use) FROM worker_nodes`), &used)
	if used != 2 {
		t.Fatalf("capacity released before ack: %d", used)
	}
	if err := s.ReleaseHarnessLease(ctx, claim.Fence, true); err != nil {
		t.Fatal(err)
	}
	leaseTestScan(t, pool.QueryRow(ctx, `SELECT sum(in_use) FROM worker_nodes`), &used)
	if used != 0 {
		t.Fatalf("capacity leaked after ack: %d", used)
	}
	if err := s.ReleaseHarnessLease(ctx, claim.Fence, true); err != nil {
		t.Fatalf("release replay: %v", err)
	}
	if err := s.AppendHarnessEvent(ctx, record); !errors.Is(err, ErrStaleFence) {
		t.Fatalf("released evidence replay accepted: %v", err)
	}
}

func TestLeaseRecoveryNeverReplaysStartedSDK(t *testing.T) {
	for _, started := range []bool{false, true} {
		t.Run(map[bool]string{false: "unstarted", true: "started"}[started], func(t *testing.T) {
			ctx, pool, s := leaseTestStore(t)
			p := leaseFixture(t, ctx, pool, s, "recovery")
			registerLeaseWorkers(t, ctx, s, 1)
			claim, err := s.ClaimHarnessLease(ctx, p)
			if err != nil {
				t.Fatal(err)
			}
			if started {
				if err := s.MarkHarnessStarted(ctx, claim.Fence); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := pool.Exec(ctx, `UPDATE environment_leases SET expires_at=clock_timestamp()-interval '1 second'`); err != nil {
				t.Fatal(err)
			}
			if err := s.RenewHarnessLease(ctx, claim.Fence, time.Minute); !errors.Is(err, ErrStaleFence) {
				t.Fatalf("expired renewal: %v", err)
			}
			if n, err := s.RecoverExpiredHarnessLeases(ctx, 10); err != nil || n != 1 {
				t.Fatalf("recovery: %d %v", n, err)
			}
			var runStatus, attemptStatus, envState string
			leaseTestScan(t, pool.QueryRow(ctx, `SELECT status FROM runs WHERE id=$1`, p.RunID), &runStatus)
			leaseTestScan(t, pool.QueryRow(ctx, `SELECT status FROM harness_attempts WHERE id=$1`, claim.Attempt.ID), &attemptStatus)
			leaseTestScan(t, pool.QueryRow(ctx, `SELECT state FROM environments WHERE id=$1`, p.EnvironmentID), &envState)
			if started {
				if runStatus != "failed" || attemptStatus != "lost" || envState != "needs_review" {
					t.Fatalf("unsafe started recovery: %s %s %s", runStatus, attemptStatus, envState)
				}
				if _, err := s.ClaimHarnessLease(ctx, p); !errors.Is(err, ErrRunAlreadyTerminal) {
					t.Fatalf("started attempt replayed: %v", err)
				}
			} else {
				next, err := s.ClaimHarnessLease(ctx, p)
				if err != nil || next.Fence.FencingToken <= claim.Fence.FencingToken || next.Attempt.ID == claim.Attempt.ID {
					t.Fatalf("safe recovery did not fence old attempt: %#v %v", next, err)
				}
			}
			if err := s.ReleaseHarnessLease(ctx, claim.Fence, true); !errors.Is(err, ErrStaleFence) {
				t.Fatalf("recovered stale release: %v", err)
			}
		})
	}
}

func TestLeaseCapacityAcrossIndependentEnvironmentsAndWorkerIsolation(t *testing.T) {
	ctx, pool, s := leaseTestStore(t)
	p1 := leaseFixture(t, ctx, pool, s, "one")
	registerLeaseWorkers(t, ctx, s, 1)
	p2 := leaseFixture(t, ctx, pool, s, "two")
	if _, err := s.ClaimHarnessLease(ctx, p1); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ClaimHarnessLease(ctx, p2); !errors.Is(err, ErrCapacityBusy) {
		t.Fatalf("capacity oversubscribed: %v", err)
	}
	if _, err := s.RegisterWorker(ctx, WorkerRegistration{ID: "runtime", Role: "runtime", Capacity: 2}); err != nil {
		t.Fatal(err)
	}
	// A browser dedicated to another project cannot satisfy this claim.
	if _, err := s.RegisterWorker(ctx, WorkerRegistration{ID: "foreign", Role: "browser", ProjectID: p1.ProjectID, Capacity: 1, Endpoint: "http://foreign:8100"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ClaimHarnessLease(ctx, p2); !errors.Is(err, ErrCapacityBusy) {
		t.Fatalf("cross-project browser assignment: %v", err)
	}
	if _, err := s.SetWorkerDraining(ctx, p2.ProjectID, "foreign", "actor", true); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-project drain: %v", err)
	}
	if _, err := s.RegisterWorker(ctx, WorkerRegistration{ID: "browser2", Role: "browser", ProjectID: p2.ProjectID, Capacity: 1, Endpoint: "http://assigned:8100"}); err != nil {
		t.Fatal(err)
	}
	if err := s.BindEnvironmentWorker(ctx, p2.EnvironmentID, "browser2"); err != nil {
		t.Fatal(err)
	}
	claim, err := s.ClaimHarnessLease(ctx, p2)
	if err != nil || claim.WorkerURL != "http://assigned:8100" {
		t.Fatalf("second worker selection: %#v %v", claim, err)
	}
	var used int
	leaseTestScan(t, pool.QueryRow(ctx, `SELECT in_use FROM worker_nodes WHERE id='runtime'`), &used)
	if used != 2 {
		t.Fatalf("runtime capacity after concurrent environments: %d", used)
	}
	view, err := s.GetWorkerServices(ctx, p2.ProjectID)
	if err != nil || view.ActiveLeases != 1 || view.Runs["running"] != 1 {
		t.Fatalf("project services facts: %#v %v", view, err)
	}
	for _, w := range view.Workers {
		if w.ID == "foreign" {
			t.Fatal("foreign worker exposed in project service view")
		}
	}
}

func TestLeaseClaimAndFinalizeRollbackOnDatabaseFault(t *testing.T) {
	ctx, pool, s := leaseTestStore(t)
	p := leaseFixture(t, ctx, pool, s, "fault")
	registerLeaseWorkers(t, ctx, s, 1)
	var initialToken int
	leaseTestScan(t, pool.QueryRow(ctx, `SELECT fencing_token FROM environments WHERE id=$1`, p.EnvironmentID), &initialToken)
	if _, err := pool.Exec(ctx, `CREATE FUNCTION lease_reject() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected lease fault'; END $$;
		CREATE TRIGGER lease_reject BEFORE INSERT ON environment_leases FOR EACH ROW EXECUTE FUNCTION lease_reject()`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ClaimHarnessLease(ctx, p); err == nil {
		t.Fatal("injected claim fault ignored")
	}
	assertRowCount(t, ctx, pool, "harness_attempts", 0)
	var used, token int
	var state string
	leaseTestScan(t, pool.QueryRow(ctx, `SELECT sum(in_use) FROM worker_nodes`), &used)
	leaseTestScan(t, pool.QueryRow(ctx, `SELECT fencing_token,state FROM environments WHERE id=$1`, p.EnvironmentID), &token, &state)
	if used != 0 || token != initialToken || state != "ready" {
		t.Fatalf("claim rollback left partial facts: used=%d token=%d state=%s", used, token, state)
	}
	if _, err := pool.Exec(ctx, `DROP TRIGGER lease_reject ON environment_leases`); err != nil {
		t.Fatal(err)
	}
	claim, err := s.ClaimHarnessLease(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `CREATE TRIGGER finish_reject BEFORE INSERT ON run_events FOR EACH ROW EXECUTE FUNCTION lease_reject()`); err != nil {
		t.Fatal(err)
	}
	ev := event.Envelope[json.RawMessage]{SchemaVersion: "agenttrace.event.v1", EventID: "fault-finish", EventType: event.RunCompleted, AggregateID: p.RunID, CorrelationID: p.RunID, OccurredAt: time.Now().UTC()}
	if _, err := s.FinishHarnessAttempt(ctx, HarnessFinish{RunID: p.RunID, AttemptID: claim.Attempt.ID, Fence: claim.Fence, Status: domain.RunSucceeded, Assistant: "never committed", Event: ev}); err == nil {
		t.Fatal("injected finalize fault ignored")
	}
	leaseTestScan(t, pool.QueryRow(ctx, `SELECT status FROM runs WHERE id=$1`, p.RunID), &state)
	if state != "running" {
		t.Fatalf("finalize rollback changed canonical status: %s", state)
	}
	var assistants int
	leaseTestScan(t, pool.QueryRow(ctx, `SELECT count(*) FROM messages WHERE role='assistant'`), &assistants)
	if assistants != 0 {
		t.Fatal("assistant message survived failed finalize")
	}
}

func TestLeaseDeadWorkerAndGenerationFenceFailClosed(t *testing.T) {
	ctx, pool, s := leaseTestStore(t)
	p := leaseFixture(t, ctx, pool, s, "heartbeat")
	registerLeaseWorkers(t, ctx, s, 1)
	claim, err := s.ClaimHarnessLease(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE worker_nodes SET last_seen_at=clock_timestamp()-interval '1 minute' WHERE id='browser'`); err != nil {
		t.Fatal(err)
	}
	if err := s.RenewHarnessLease(ctx, claim.Fence, time.Minute); !errors.Is(err, ErrStaleFence) {
		t.Fatalf("dead browser lease renewed: %v", err)
	}
	if n, err := s.RecoverExpiredHarnessLeases(ctx, 10); n != 1 || err != nil {
		t.Fatalf("dead worker not recovered: %d %v", n, err)
	}
	if err := s.HeartbeatWorker(ctx, "browser"); err != nil {
		t.Fatal(err)
	}
	next, err := s.ClaimHarnessLease(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE environments SET profile_generation=profile_generation+1 WHERE id=$1`, p.EnvironmentID); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkHarnessStarted(ctx, next.Fence); !errors.Is(err, ErrStaleFence) {
		t.Fatalf("stale profile started SDK: %v", err)
	}
}

func TestLeaseRootEvidenceAndUncertainCompletionAreFenced(t *testing.T) {
	ctx, pool, s := leaseTestStore(t)
	p := leaseFixture(t, ctx, pool, s, "root")
	registerLeaseWorkers(t, ctx, s, 1)
	claim, err := s.ClaimHarnessLease(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	stale := claim.Fence
	stale.FencingToken++
	now := time.Now().UTC()
	start := TraceAttemptStart{Span: domain.Span{ID: "root", RunID: p.RunID, TraceID: "trace", SpanID: "root", Kind: domain.SpanRun, Name: "run", Status: "started", StartedAt: now},
		Attempt: TraceAttempt{ID: "root-attempt", AttemptNumber: 1, Status: "started", StartedAt: now}}
	if err := s.InitializeHarnessTrace(ctx, stale, start); !errors.Is(err, ErrStaleFence) {
		t.Fatalf("stale root trace accepted: %v", err)
	}
	assertRowCount(t, ctx, pool, "spans", 0)
	if err := s.InitializeHarnessTrace(ctx, claim.Fence, start); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkHarnessStarted(ctx, claim.Fence); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE runs SET status='cancelling',cancel_requested_at=now() WHERE id=$1`, p.RunID); err != nil {
		t.Fatal(err)
	}
	ev := event.Envelope[json.RawMessage]{SchemaVersion: "agenttrace.event.v1", EventID: "uncertain", EventType: event.RunCompleted, AggregateID: p.RunID, CorrelationID: p.RunID, OccurredAt: now}
	if _, err := s.FinishHarnessAttempt(ctx, HarnessFinish{RunID: p.RunID, AttemptID: claim.Attempt.ID, Fence: claim.Fence, Status: domain.RunFailed, Uncertain: true, Event: ev}); err != nil {
		t.Fatal(err)
	}
	var envState, attemptState string
	var runState string
	var used int
	leaseTestScan(t, pool.QueryRow(ctx, `SELECT state FROM environments WHERE id=$1`, p.EnvironmentID), &envState)
	leaseTestScan(t, pool.QueryRow(ctx, `SELECT status FROM harness_attempts WHERE id=$1`, claim.Attempt.ID), &attemptState)
	leaseTestScan(t, pool.QueryRow(ctx, `SELECT status FROM runs WHERE id=$1`, p.RunID), &runState)
	leaseTestScan(t, pool.QueryRow(ctx, `SELECT sum(in_use) FROM worker_nodes`), &used)
	if envState != "needs_review" || attemptState != "lost" || runState != "failed" || used != 2 {
		t.Fatalf("crash incorrectly released facts: %s %s %s %d", envState, attemptState, runState, used)
	}
	if _, err := pool.Exec(ctx, `UPDATE environment_leases SET expires_at=clock_timestamp()-interval '1 second'`); err != nil {
		t.Fatal(err)
	}
	if n, err := s.RecoverExpiredHarnessLeases(ctx, 10); n != 1 || err != nil {
		t.Fatalf("uncertain recovery: %d %v", n, err)
	}
	leaseTestScan(t, pool.QueryRow(ctx, `SELECT sum(in_use) FROM worker_nodes`), &used)
	if used != 0 {
		t.Fatal("uncertain recovery leaked capacity")
	}
}

func TestLeaseUnstartedAbandonmentRequiresDurableNoSDKProof(t *testing.T) {
	for _, started := range []bool{false, true} {
		t.Run(map[bool]string{false: "unstarted", true: "started"}[started], func(t *testing.T) {
			ctx, pool, s := leaseTestStore(t)
			p := leaseFixture(t, ctx, pool, s, "abandon")
			registerLeaseWorkers(t, ctx, s, 1)
			claim, err := s.ClaimHarnessLease(ctx, p)
			if err != nil {
				t.Fatal(err)
			}
			if started {
				if err := s.MarkHarnessStarted(ctx, claim.Fence); err != nil {
					t.Fatal(err)
				}
			}
			ev := event.Envelope[json.RawMessage]{SchemaVersion: "agenttrace.event.v1", EventID: "abandon", EventType: event.RunCompleted, AggregateID: p.RunID, CorrelationID: p.RunID, OccurredAt: time.Now().UTC()}
			if _, err := s.FinishHarnessAttempt(ctx, HarnessFinish{RunID: p.RunID, AttemptID: claim.Attempt.ID, Fence: claim.Fence, Status: domain.RunCancelled, Event: ev}); err != nil {
				t.Fatal(err)
			}
			err = s.AbandonUnstartedHarnessLease(ctx, claim.Fence)
			var used int
			leaseTestScan(t, pool.QueryRow(ctx, `SELECT sum(in_use) FROM worker_nodes`), &used)
			if started && (!errors.Is(err, ErrConflict) || used != 2) || !started && (err != nil || used != 0) {
				t.Fatalf("unstarted abandonment violated proof: started=%v used=%d err=%v", started, used, err)
			}
		})
	}
}
