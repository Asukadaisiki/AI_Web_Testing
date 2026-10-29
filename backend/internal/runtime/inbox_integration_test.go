package runtime

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	inboxconsumer "github.com/Asukadaisiki/AI_Web_Testing/backend/internal/consumer"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/domain"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/event"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/piharness"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestRuntimeInboxCommitsClaimAndReleasesConnectionBeforeRunner(t *testing.T) {
	ctx, pool, _ := newRuntimePostgresStore(t)
	// A separate one-connection pool makes an open inbox transaction observable.
	config := pool.Config()
	config.MaxConns = 1
	single, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(single.Close)
	calls := 0
	c := Consumer{
		Inbox: DeduplicatedInbox{Deduplicator: inboxconsumer.NewDeduplicator(single, "agenttrace-runtime")},
		Runner: inboxRunnerFunc(func(runCtx context.Context, _ event.RunCommand) error {
			calls++
			queryCtx, cancel := context.WithTimeout(runCtx, 500*time.Millisecond)
			defer cancel()
			var claims int
			if err := single.QueryRow(queryCtx, `
				SELECT count(*) FROM inbox_events
				WHERE event_id = 'evt-short-inbox' AND processed_at IS NULL`).Scan(&claims); err != nil {
				return fmt.Errorf("runner cannot use released database connection: %w", err)
			}
			if claims != 1 {
				return fmt.Errorf("committed claims = %d, want 1 before runner", claims)
			}
			return nil
		}),
	}
	message := commandMessage(t, "evt-short-inbox", commandFor("run-short-inbox"))
	if err := c.Handle(ctx, message); err != nil {
		t.Fatalf("external execution: %v", err)
	}
	if err := c.Handle(ctx, message); err != nil {
		t.Fatalf("completed duplicate: %v", err)
	}
	if calls != 1 {
		t.Fatalf("external executions = %d, want 1", calls)
	}
	var completed int
	if err := single.QueryRow(ctx, `
		SELECT count(*) FROM inbox_events
		WHERE event_id = 'evt-short-inbox' AND processed_at IS NOT NULL`).Scan(&completed); err != nil {
		t.Fatal(err)
	}
	if completed != 1 {
		t.Fatalf("completed inbox rows = %d, want 1", completed)
	}
}

func TestRuntimeInboxConcurrentDuplicateDoesNotWaitOnDatabaseLockOrSpendRetries(t *testing.T) {
	ctx, pool, _ := newRuntimePostgresStore(t)
	started, release := make(chan struct{}), make(chan struct{})
	firstDone, duplicateDone := make(chan error, 1), make(chan error, 1)
	runner := inboxRunnerFunc(func(context.Context, event.RunCommand) error {
		close(started)
		<-release
		return nil
	})
	retries := &memoryRetryStore{}
	makeConsumer := func() *Consumer {
		return &Consumer{
			Inbox:  DeduplicatedInbox{Deduplicator: inboxconsumer.NewDeduplicator(pool, "agenttrace-runtime")},
			Runner: runner, RetryStore: retries, MaxAttempts: 1,
		}
	}
	first, duplicate := makeConsumer(), makeConsumer()
	message := commandMessage(t, "evt-active-inbox", commandFor("run-active-inbox"))
	go func() { firstDone <- first.Handle(ctx, message) }()
	select {
	case <-started:
	case err := <-firstDone:
		t.Fatalf("first execution failed before runner: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("runner did not start")
	}
	defer func() {
		close(release)
		if err := <-firstDone; err != nil {
			t.Errorf("first execution: %v", err)
		}
	}()
	duplicateCtx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	go func() { duplicateDone <- duplicate.Handle(duplicateCtx, message) }()
	select {
	case err := <-duplicateDone:
		if err == nil || errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("active duplicate = %v, want prompt retryable claim contention", err)
		}
	case <-time.After(500 * time.Millisecond):
		cancel()
		<-duplicateDone
		t.Fatal("active duplicate waited on an inbox database lock")
	}
	if len(retries.incremented) != 0 || retries.handoffCount != 0 {
		t.Fatalf("active duplicate spent retries or handed off DLQ: %#v", retries)
	}
	var openTransactions int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM pg_stat_activity
		WHERE datname = current_database() AND pid <> pg_backend_pid()
		  AND xact_start IS NOT NULL`).Scan(&openTransactions); err != nil {
		t.Fatal(err)
	}
	if openTransactions != 0 {
		t.Fatalf("open transactions during runner = %d, want 0", openTransactions)
	}
}

func TestRuntimeInboxRenewsActiveClaimPastTTL(t *testing.T) {
	ctx, pool, _ := newRuntimePostgresStore(t)
	started, release := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	var calls atomic.Int32
	runner := inboxRunnerFunc(func(context.Context, event.RunCommand) error {
		if calls.Add(1) == 1 {
			close(started)
			<-release
		}
		return nil
	})
	d := inboxconsumer.NewDeduplicator(pool, "agenttrace-runtime")
	d.ClaimTTL = 120 * time.Millisecond
	first := Consumer{Inbox: DeduplicatedInbox{Deduplicator: d}, Runner: runner}
	message := commandMessage(t, "evt-renew-inbox", commandFor("run-renew-inbox"))
	go func() { done <- first.Handle(ctx, message) }()
	select {
	case <-started:
	case err := <-done:
		t.Fatalf("delivery before runner: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("runner did not start")
	}
	defer func() {
		close(release)
		if err := <-done; err != nil {
			t.Errorf("first delivery: %v", err)
		}
	}()
	time.Sleep(360 * time.Millisecond)
	duplicate := Consumer{
		Inbox:  DeduplicatedInbox{Deduplicator: inboxconsumer.NewDeduplicator(pool, "agenttrace-runtime")},
		Runner: runner,
	}
	if err := duplicate.Handle(ctx, message); !errors.Is(err, inboxconsumer.ErrClaimInProgress) {
		t.Fatalf("duplicate past TTL = %v, want active claim contention", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("external executions = %d, want 1", calls.Load())
	}
}

func TestRuntimeInboxLostClaimCancelsRunnerWithoutFinalizingNewOwner(t *testing.T) {
	ctx, pool, _ := newRuntimePostgresStore(t)
	started := make(chan struct{})
	done := make(chan error, 1)
	d := inboxconsumer.NewDeduplicator(pool, "agenttrace-runtime")
	d.ClaimTTL = 120 * time.Millisecond
	retries := &memoryRetryStore{}
	c := Consumer{
		Inbox: DeduplicatedInbox{Deduplicator: d}, RetryStore: retries, MaxAttempts: 1,
		Runner: inboxRunnerFunc(func(runCtx context.Context, _ event.RunCommand) error {
			close(started)
			<-runCtx.Done()
			return nil
		}),
	}
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	message := commandMessage(t, "evt-lost-inbox", commandFor("run-lost-inbox"))
	go func() { done <- c.Handle(runCtx, message) }()
	select {
	case <-started:
	case err := <-done:
		t.Fatalf("delivery before runner: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("runner did not start")
	}
	if _, err := pool.Exec(ctx, `
		UPDATE inbox_events
		SET claim_token = 'replacement-owner', claimed_until = now() + interval '1 minute'
		WHERE event_id = 'evt-lost-inbox'`); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if !errors.Is(err, inboxconsumer.ErrClaimLost) {
			t.Fatalf("lost claim delivery = %v, want ownership loss", err)
		}
	case <-time.After(time.Second):
		cancel()
		<-done
		t.Fatal("lost claim did not cancel external runner")
	}
	var token string
	var pending bool
	if err := pool.QueryRow(ctx, `
		SELECT claim_token, processed_at IS NULL FROM inbox_events
		WHERE event_id = 'evt-lost-inbox'`).Scan(&token, &pending); err != nil {
		t.Fatal(err)
	}
	if token != "replacement-owner" || !pending {
		t.Fatalf("stale owner modified replacement claim: token=%q pending=%v", token, pending)
	}
	if len(retries.incremented) != 0 || retries.handoffCount != 0 {
		t.Fatalf("lost claim spent retry budget: %#v", retries)
	}
}

func TestRuntimeInboxFailedHandlerReleasesClaimForImmediateRedelivery(t *testing.T) {
	ctx, pool, _ := newRuntimePostgresStore(t)
	calls := 0
	cause := errors.New("failure before attempt")
	c := Consumer{
		Inbox: DeduplicatedInbox{Deduplicator: inboxconsumer.NewDeduplicator(pool, "agenttrace-runtime")},
		Runner: inboxRunnerFunc(func(context.Context, event.RunCommand) error {
			calls++
			if calls == 1 {
				return cause
			}
			return nil
		}),
	}
	message := commandMessage(t, "evt-retry-inbox", commandFor("run-retry-inbox"))
	if err := c.Handle(ctx, message); !errors.Is(err, cause) {
		t.Fatalf("failed handler = %v, want original cause", err)
	}
	var released bool
	if err := pool.QueryRow(ctx, `
		SELECT processed_at IS NULL AND claim_token IS NULL AND claimed_until IS NULL
		FROM inbox_events WHERE event_id = 'evt-retry-inbox'`).Scan(&released); err != nil {
		t.Fatal(err)
	}
	if !released {
		t.Fatal("failed handler left an active or processed claim")
	}
	for range 2 {
		if err := c.Handle(ctx, message); err != nil {
			t.Fatalf("redelivery: %v", err)
		}
	}
	if calls != 2 {
		t.Fatalf("runner calls = %d, want failure then one success", calls)
	}
}

func TestRuntimeInboxFinalizesDurableOutcomeAfterDeliveryCancellation(t *testing.T) {
	ctx, pool, _ := newRuntimePostgresStore(t)
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	c := Consumer{
		Inbox: DeduplicatedInbox{Deduplicator: inboxconsumer.NewDeduplicator(pool, "agenttrace-runtime")},
		Runner: inboxRunnerFunc(func(context.Context, event.RunCommand) error {
			cancel()
			return &terminalRunError{cause: context.Canceled}
		}),
	}
	if err := c.Handle(runCtx, commandMessage(t, "evt-cancel-inbox", commandFor("run-cancel-inbox"))); err != nil {
		t.Fatalf("durably cancelled run delivery: %v", err)
	}
	var completed bool
	if err := pool.QueryRow(ctx, `
		SELECT processed_at IS NOT NULL AND claim_token IS NULL AND claimed_until IS NULL
		FROM inbox_events WHERE event_id = 'evt-cancel-inbox'`).Scan(&completed); err != nil {
		t.Fatal(err)
	}
	if !completed {
		t.Fatal("cancelled delivery did not finalize durable terminal outcome")
	}
}

func TestRuntimeInboxAbandonedClaimReconcilesAttemptWithoutAnotherSession(t *testing.T) {
	for _, afterAttempt := range []bool{false, true} {
		name := "before_attempt"
		if afterAttempt {
			name = "after_attempt"
		}
		t.Run(name, func(t *testing.T) {
			ctx, pool, store := newRuntimePostgresStore(t)
			command := seedInboxRun(t, ctx, pool)
			traceStore := PostgresPiTraceStore{Store: store}
			if afterAttempt {
				if _, err := traceStore.StartRun(ctx, command); err != nil {
					t.Fatalf("abandoned attempt: %v", err)
				}
			}
			message := commandMessage(t, "evt-abandoned-inbox", command)
			if _, err := pool.Exec(ctx, `
				INSERT INTO inbox_events (id, consumer, event_id, payload, claim_token, claimed_until)
				VALUES ('inb-abandoned', 'agenttrace-runtime', 'evt-abandoned-inbox', $1::jsonb,
				        'dead-worker', now() - interval '1 minute')`, message.Value); err != nil {
				t.Fatal(err)
			}
			calls := 0
			process := inboxHarnessFunc(func(_ context.Context, request piharness.Request, emit func(piharness.Event)) error {
				calls++
				emit(piharness.Event{
					Type: piharness.EventSettled, RunID: request.RunID, AttemptID: request.AttemptID,
					Sequence: 1, Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
					Payload: []byte(`{"verdict":"succeeded","assistant_text":"performed once"}`),
				})
				return nil
			})
			c := Consumer{
				Inbox:  DeduplicatedInbox{Deduplicator: inboxconsumer.NewDeduplicator(pool, "agenttrace-runtime")},
				Runner: PiRunner{Store: traceStore, Process: process},
			}
			for range 2 {
				if err := c.Handle(ctx, message); err != nil {
					t.Fatalf("abandoned delivery reconciliation: %v", err)
				}
			}
			wantStatus, wantCalls := domain.RunSucceeded, 1
			if afterAttempt {
				wantStatus, wantCalls = domain.RunFailed, 0
			}
			finished, err := store.GetRun(ctx, command.RunID)
			if err != nil || finished.Status != wantStatus || finished.FinishedAt == nil {
				t.Fatalf("reconciled run = %#v, %v, want %s", finished, err, wantStatus)
			}
			var attempts, runs, completions, processed int
			if err := pool.QueryRow(ctx, `
				SELECT (SELECT count(*) FROM harness_attempts WHERE run_id = $1),
				       (SELECT count(*) FROM runs),
				       (SELECT count(*) FROM run_events WHERE run_id = $1 AND event_type = 'run.completed'),
				       (SELECT count(*) FROM inbox_events WHERE event_id = 'evt-abandoned-inbox'
				          AND processed_at IS NOT NULL AND claim_token IS NULL AND claimed_until IS NULL)`,
				command.RunID).Scan(&attempts, &runs, &completions, &processed); err != nil {
				t.Fatal(err)
			}
			if calls != wantCalls || attempts != 1 || runs != 1 || completions != 1 || processed != 1 {
				t.Fatalf("sessions/attempts/runs/completions/inbox = %d/%d/%d/%d/%d, want %d/1/1/1/1",
					calls, attempts, runs, completions, processed, wantCalls)
			}
		})
	}
}

func seedInboxRun(t *testing.T, ctx context.Context, pool *pgxpool.Pool) event.RunCommand {
	t.Helper()
	for _, sql := range []string{
		`INSERT INTO projects (id, name) VALUES ('prj-inbox', 'inbox fixture')`,
		`INSERT INTO conversations (id, project_id) VALUES ('conv-inbox', 'prj-inbox')`,
		`INSERT INTO runs (id, project_id, conversation_id, status, specification)
		 VALUES ('run-inbox', 'prj-inbox', 'conv-inbox', 'queued', '{}'::jsonb)`,
	} {
		if _, err := pool.Exec(ctx, sql); err != nil {
			t.Fatal(err)
		}
	}
	command := commandFor("run-inbox")
	command.ConversationID = "conv-inbox"
	return command
}

type inboxRunnerFunc func(context.Context, event.RunCommand) error

func (f inboxRunnerFunc) Run(ctx context.Context, command event.RunCommand) error {
	return f(ctx, command)
}

type inboxHarnessFunc func(context.Context, piharness.Request, func(piharness.Event)) error

func (f inboxHarnessFunc) Run(ctx context.Context, request piharness.Request, emit func(piharness.Event)) error {
	return f(ctx, request, emit)
}
