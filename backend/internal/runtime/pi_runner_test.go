package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	inboxconsumer "github.com/Asukadaisiki/AI_Web_Testing/backend/internal/consumer"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/domain"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/event"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/piharness"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/postgres"
)

func TestBuildHarnessRequestDefaultsBlankArtifactScopeToRunID(t *testing.T) {
	command := event.RunCommand{
		RunID: "run-default-scope",
		Model: event.ModelRef{
			Provider:        "openai",
			Name:            "gpt-5",
			ReasoningEffort: "high",
		},
		Messages: []domain.Message{
			{Role: "user", Content: "first goal"},
			{Role: "assistant", Content: "intermediate response"},
			{Role: "user", Content: "final goal"},
		},
		WorkerURL: "http://command-worker:8100",
	}

	request := buildHarnessRequest(command, "", "attempt-default-scope", "high")

	if request.ArtifactScope != "run-default-scope" {
		t.Fatalf("artifact scope = %q, want run ID fallback", request.ArtifactScope)
	}
	if request.RunID != "run-default-scope" || request.AttemptID != "attempt-default-scope" {
		t.Fatalf("run ID / attempt ID = %q / %q", request.RunID, request.AttemptID)
	}
	if request.Goal != "final goal" {
		t.Fatalf("goal = %q, want latest user message", request.Goal)
	}
	if request.Model.Provider != "openai" || request.Model.Name != "gpt-5" || request.Model.ReasoningEffort != "high" {
		t.Fatalf("model = %#v, want command model fields", request.Model)
	}
	if request.WorkerURL != "http://command-worker:8100" {
		t.Fatalf("worker URL = %q, want command fallback", request.WorkerURL)
	}
}

func TestBuildHarnessRequestDefaultsWhitespaceOnlyArtifactScopeToRunID(t *testing.T) {
	command := commandFor("run-whitespace-scope")
	command.ArtifactScope = " \t\n"

	request := buildHarnessRequest(command, "http://runtime-worker:8100", "attempt-whitespace-scope", "high")

	if request.ArtifactScope != "run-whitespace-scope" {
		t.Fatalf("artifact scope = %q, want run ID fallback", request.ArtifactScope)
	}
}

func TestBuildHarnessRequestRetainsGoalWithResponseContext(t *testing.T) {
	for _, tc := range []struct {
		kind    string
		content string
	}{
		{kind: "approval", content: "approved"},
		{kind: "question", content: "use the staging cart"},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			metadata, err := json.Marshal(map[string]string{"response_kind": tc.kind})
			if err != nil {
				t.Fatal(err)
			}
			command := commandFor("run-response-" + tc.kind)
			command.Messages = []domain.Message{
				{Role: "user", Content: "earlier unrelated task"},
				{Role: "assistant", Content: "earlier task finished"},
				{Role: "user", Content: "open the cart and verify checkout"},
				{Role: "user", Content: tc.content, Metadata: metadata},
			}

			request := buildHarnessRequest(command, "", "attempt-response", "high")

			want := "open the cart and verify checkout\n\nUser response (" + tc.kind + "):\n" + tc.content
			if request.Goal != want {
				t.Fatalf("goal = %q, want %q", request.Goal, want)
			}
		})
	}
}

func TestBuildHarnessRequestPreservesExplicitArtifactScope(t *testing.T) {
	command := commandFor("run-explicit-scope")
	command.ArtifactScope = " artifact-scope-explicit "

	request := buildHarnessRequest(command, "http://runtime-worker:8100", "attempt-explicit-scope", "high")

	if request.ArtifactScope != " artifact-scope-explicit " {
		t.Fatalf("artifact scope = %q, want explicit command scope", request.ArtifactScope)
	}
}

func TestPiRunnerDefaultsLegacyBlankReasoningEffortFromConfiguredProfile(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value string
		want  string
	}{
		{name: "empty", value: "", want: "high"},
		{name: "whitespace", value: " \t\n", want: "high"},
		{name: "explicit", value: " low ", want: " low "},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runID := "run-reasoning-" + tc.name
			attemptID := "attempt-reasoning-" + tc.name
			store := &fakePiStore{ref: HarnessAttemptRef{
				ID: runID + "-harness", RunID: runID, AttemptNumber: 1,
				ProviderAttemptID: attemptID,
			}}
			process := &fakeHarnessProcess{
				events: passingEventStream(runID, attemptID),
			}
			runner := PiRunner{
				Process:                process,
				Store:                  store,
				DefaultReasoningEffort: "high",
			}
			command := commandFor(runID)
			command.Model.ReasoningEffort = tc.value

			if err := runner.Run(context.Background(), command); err != nil {
				t.Fatalf("run = %v", err)
			}
			if process.request.Model.ReasoningEffort != tc.want {
				t.Fatalf(
					"request reasoning effort = %q, want %q",
					process.request.Model.ReasoningEffort, tc.want,
				)
			}
		})
	}
}

func TestPiRunnerReplayOfTerminalRunStartsNoProcess(t *testing.T) {
	store := &fakePiStore{startErr: postgres.ErrRunAlreadyTerminal}
	process := &fakeHarnessProcess{err: errors.New("should not run")}
	runner := PiRunner{Process: process, Store: store}

	if err := runner.Run(context.Background(), commandFor("run-terminal-replay")); err != nil {
		t.Fatalf("replay of terminal run = %v, want nil", err)
	}
	if process.calls != 0 {
		t.Fatalf("process calls = %d, want 0 for terminal replay", process.calls)
	}
	if store.completed {
		t.Fatal("CompleteHarnessRun should not be called for terminal replay")
	}
}

func TestPiRunnerSucceedsWhenCasePassesBeforeSettled(t *testing.T) {
	events := passingEventStream("run-passed", "att-passed")
	store := &fakePiStore{ref: HarnessAttemptRef{
		ID: "hat-1", RunID: "run-passed", AttemptNumber: 1, ProviderAttemptID: "att-passed",
	}}
	process := &fakeHarnessProcess{events: events}
	runner := PiRunner{Process: process, Store: store}

	if err := runner.Run(context.Background(), commandFor("run-passed")); err != nil {
		t.Fatalf("passed run = %v, want nil", err)
	}
	if store.outcome.Status != domain.RunSucceeded {
		t.Fatalf("outcome status = %q, want succeeded", store.outcome.Status)
	}
	if store.outcome.Assistant != "final answer" {
		t.Fatalf("assistant = %q, want final answer", store.outcome.Assistant)
	}
	if len(store.events) != len(events) {
		t.Fatalf("persisted events = %d, want %d", len(store.events), len(events))
	}
}

func TestPiRunnerFailsWhenLatestCaseVerdictFailsAfterEarlierPass(t *testing.T) {
	events := []piharness.Event{
		harEvent(piharness.EventStarted, "run-latest-failed", "att-latest-failed", 1, `{}`),
		harEvent(piharness.EventCaseCompleted, "run-latest-failed", "att-latest-failed", 2, `{"verdict":"passed"}`),
		harEvent(piharness.EventCaseCompleted, "run-latest-failed", "att-latest-failed", 3, `{"verdict":"failed"}`),
		harEvent(piharness.EventSettled, "run-latest-failed", "att-latest-failed", 4, `{"assistant_text":"done"}`),
	}
	store := &fakePiStore{ref: HarnessAttemptRef{
		ID: "hat-latest-failed", RunID: "run-latest-failed", AttemptNumber: 1,
		ProviderAttemptID: "att-latest-failed",
	}}
	process := &fakeHarnessProcess{events: events}
	runner := PiRunner{Process: process, Store: store}

	err := runner.Run(context.Background(), commandFor("run-latest-failed"))
	if !isTerminalRunError(err) {
		t.Fatalf("error = %v, want terminalRunError", err)
	}
	if store.outcome.Status != domain.RunFailed {
		t.Fatalf("outcome status = %q, want failed from the latest browser case verdict", store.outcome.Status)
	}
}

func TestPiRunnerFailsWhenSettledWithoutPassedCase(t *testing.T) {
	events := []piharness.Event{
		harEvent(piharness.EventStarted, "run-no-pass", "att-no-pass", 1, `{}`),
		harEvent(piharness.EventCaseCompleted, "run-no-pass", "att-no-pass", 2, `{"verdict":"failed"}`),
		harEvent(piharness.EventSettled, "run-no-pass", "att-no-pass", 3, `{"assistant_text":"done"}`),
	}
	store := &fakePiStore{ref: HarnessAttemptRef{
		ID: "hat-2", RunID: "run-no-pass", AttemptNumber: 1, ProviderAttemptID: "att-no-pass",
	}}
	process := &fakeHarnessProcess{events: events}
	runner := PiRunner{Process: process, Store: store}

	err := runner.Run(context.Background(), commandFor("run-no-pass"))
	if !isTerminalRunError(err) {
		t.Fatalf("error = %v, want terminalRunError", err)
	}
	if store.outcome.Status != domain.RunFailed {
		t.Fatalf("outcome status = %q, want failed", store.outcome.Status)
	}
}

func TestPiRunnerFailsOnHarnessFailedEvent(t *testing.T) {
	events := []piharness.Event{
		harEvent(piharness.EventStarted, "run-failed", "att-failed", 1, `{}`),
		harEvent(piharness.EventFailed, "run-failed", "att-failed", 2, `{"error":"boom"}`),
	}
	store := &fakePiStore{ref: HarnessAttemptRef{
		ID: "hat-3", RunID: "run-failed", AttemptNumber: 1, ProviderAttemptID: "att-failed",
	}}
	process := &fakeHarnessProcess{
		events: events,
		err:    &piharness.HarnessFailure{Message: "boom"},
	}
	runner := PiRunner{Process: process, Store: store}

	err := runner.Run(context.Background(), commandFor("run-failed"))
	if !isTerminalRunError(err) {
		t.Fatalf("error = %v, want terminalRunError", err)
	}
	if store.outcome.Status != domain.RunFailed {
		t.Fatalf("outcome status = %q, want failed", store.outcome.Status)
	}
	if !strings.Contains(store.outcome.Error, "boom") {
		t.Fatalf("outcome error = %q, want boom", store.outcome.Error)
	}
}

func TestPiRunnerCancelledWhenDurableCancelRequestedEvenIfCasePasses(t *testing.T) {
	events := passingEventStream("run-cancel-durable", "att-cancel-durable")
	store := &fakePiStore{ref: HarnessAttemptRef{
		ID: "hat-cd", RunID: "run-cancel-durable", AttemptNumber: 1,
		ProviderAttemptID: "att-cancel-durable",
		CancelRequested:   true,
	}}
	process := &fakeHarnessProcess{events: events}
	runner := PiRunner{Process: process, Store: store}

	err := runner.Run(context.Background(), commandFor("run-cancel-durable"))
	if !isTerminalRunError(err) {
		t.Fatalf("error = %v, want terminalRunError for durable cancel", err)
	}
	if store.outcome.Status != domain.RunCancelled {
		t.Fatalf("outcome status = %q, want cancelled (durable cancel overrides passed case)", store.outcome.Status)
	}
	if process.calls != 0 {
		t.Fatalf("queued cancelled run submitted %d sessions, want none", process.calls)
	}
	if len(store.events) != 0 {
		t.Fatal("queued cancelled run must not generate agent or browser evidence")
	}
}

func TestPiRunnerAcceptsSelectedHarnessVerdictWithoutBrowserEvidence(t *testing.T) {
	for _, verdict := range []string{"succeeded", "failed"} {
		t.Run(verdict, func(t *testing.T) {
			store := &fakePiStore{ref: HarnessAttemptRef{
				ID: "hat-other", RunID: "run-other", AttemptNumber: 1,
				ProviderAttemptID: "att-other",
			}}
			process := &fakeHarnessProcess{events: []piharness.Event{
				harEvent(piharness.EventStarted, "run-other", "att-other", 1, `{}`),
				harEvent(piharness.EventSettled, "run-other", "att-other", 2,
					`{"assistant_text":"other harness finished","verdict":"`+verdict+`"}`),
			}}
			runner := PiRunner{Process: process, Store: store}
			err := runner.Run(context.Background(), commandFor("run-other"))
			if verdict == "succeeded" && err != nil {
				t.Fatalf("non-browser harness success = %v", err)
			}
			if verdict == "failed" && !isTerminalRunError(err) {
				t.Fatalf("harness failure = %v, want terminal error", err)
			}
			if string(store.outcome.Status) != verdict {
				t.Fatalf("status = %q, want selected harness verdict %q", store.outcome.Status, verdict)
			}
			if store.outcome.Assistant != "other harness finished" {
				t.Fatalf("assistant output = %q", store.outcome.Assistant)
			}
		})
	}
}

func TestPiRunnerCancelsActiveProcessWhenDurableCancellationIsRequested(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	store := &fakePiStore{ref: HarnessAttemptRef{
		ID: "hat-active-cancel", RunID: "run-active-cancel", AttemptNumber: 1,
		ProviderAttemptID: "att-active-cancel",
	}}
	process := &blockingHarnessProcess{
		started:  make(chan struct{}),
		canceled: make(chan struct{}),
	}
	runner := PiRunner{Process: process, Store: store}
	done := make(chan error, 1)
	go func() {
		done <- runner.Run(ctx, commandFor("run-active-cancel"))
	}()

	select {
	case <-process.started:
	case <-time.After(time.Second):
		t.Fatal("harness process did not start")
	}
	store.setCancellationRequested(true)

	select {
	case <-process.canceled:
	case <-time.After(time.Second):
		t.Fatal("durable cancellation did not cancel the active harness process")
	}
	err := <-done
	if !isTerminalRunError(err) {
		t.Fatalf("run error = %v, want terminalRunError", err)
	}
	if store.outcome.Status != domain.RunCancelled {
		t.Fatalf("outcome status = %q, want cancelled", store.outcome.Status)
	}
}

func TestPiRunnerCancellationImmediatelyBeforeCompletionOverridesPassedVerdict(t *testing.T) {
	store := &fakePiStore{ref: HarnessAttemptRef{
		ID: "hat-final-cancel", RunID: "run-final-cancel", AttemptNumber: 1,
		ProviderAttemptID: "att-final-cancel",
	}}
	process := &fakeHarnessProcess{
		events: passingEventStream("run-final-cancel", "att-final-cancel"),
		afterEvents: func() {
			store.setCancellationRequested(true)
		},
	}
	runner := PiRunner{Process: process, Store: store}

	err := runner.Run(context.Background(), commandFor("run-final-cancel"))
	if !isTerminalRunError(err) {
		t.Fatalf("run error = %v, want terminalRunError", err)
	}
	if store.outcome.Status != domain.RunCancelled {
		t.Fatalf("outcome status = %q, want cancellation to override passed verdict", store.outcome.Status)
	}
}

func TestPiRunnerReturnsCancellationCommittedAfterFinalCancellationRead(t *testing.T) {
	store := &fakePiStore{
		ref: HarnessAttemptRef{
			ID: "hat-commit-cancel", RunID: "run-commit-cancel", AttemptNumber: 1,
			ProviderAttemptID: "att-commit-cancel",
		},
		completeStatus: domain.RunCancelled,
	}
	process := &fakeHarnessProcess{
		events: passingEventStream("run-commit-cancel", "att-commit-cancel"),
	}
	runner := PiRunner{Process: process, Store: store}

	err := runner.Run(context.Background(), commandFor("run-commit-cancel"))
	if !isTerminalRunError(err) {
		t.Fatalf("run error = %v, want terminalRunError for committed cancellation", err)
	}
	if !strings.Contains(err.Error(), "cancel") {
		t.Fatalf("run error = %v, want cancellation error", err)
	}
}

func TestPiRunnerCompletionPersistenceErrorsAreRetryable(t *testing.T) {
	for _, tc := range []struct {
		name           string
		processErr     error
		completeStatus domain.RunStatus
	}{
		{name: "passed_case_cancelled_status", completeStatus: domain.RunCancelled},
		{name: "failed_outcome", processErr: errors.New("harness failed"), completeStatus: domain.RunFailed},
		{name: "cancelled_outcome", processErr: context.Canceled, completeStatus: domain.RunCancelled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runID := "run-complete-error-" + tc.name
			attemptID := "att-complete-error-" + tc.name
			completeErr := errors.New("terminal persistence failed")
			store := &fakePiStore{
				ref: HarnessAttemptRef{
					ID: runID + "-harness", RunID: runID, AttemptNumber: 1,
					ProviderAttemptID: attemptID,
				},
				completeStatus: tc.completeStatus,
				completeErr:    completeErr,
			}
			process := &fakeHarnessProcess{
				events: passingEventStream(runID, attemptID),
				err:    tc.processErr,
			}
			runner := PiRunner{Process: process, Store: store}

			err := runner.Run(context.Background(), commandFor(runID))
			if !errors.Is(err, completeErr) {
				t.Errorf("run error = %v, want completion persistence cause %v", err, completeErr)
			}
			if isTerminalRunError(err) {
				t.Errorf("run error = %v, want retryable error, not terminalRunError", err)
			}
			if store.completeCalls != 3 || process.calls != 1 {
				t.Errorf("completion calls / executions = %d / %d, want 3 / 1",
					store.completeCalls, process.calls)
			}
		})
	}
}

func TestPiRunnerRetriesTerminalPersistenceWithoutReexecutingSideEffects(t *testing.T) {
	for _, verdict := range []string{"succeeded", "failed"} {
		t.Run(verdict, func(t *testing.T) {
			store := &fakePiStore{
				ref: HarnessAttemptRef{
					ID: "hat-retry", RunID: "run-retry", ProviderAttemptID: "att-retry",
				},
				completeFailures: 1,
			}
			process := &fakeHarnessProcess{events: []piharness.Event{
				harEvent(piharness.EventSettled, "run-retry", "att-retry", 1,
					`{"verdict":"`+verdict+`","assistant_text":"original outcome"}`),
			}}
			consumer := Consumer{
				Inbox: &memoryInbox{}, Runner: PiRunner{Process: process, Store: store},
			}
			message := commandMessage(t, "evt-retry", commandFor("run-retry"))
			if err := consumer.Handle(context.Background(), message); err != nil {
				t.Fatalf("transient completion failure: %v", err)
			}
			if err := consumer.Handle(context.Background(), message); err != nil {
				t.Fatalf("redelivery: %v", err)
			}
			if process.calls != 1 || store.completeCalls != 2 {
				t.Fatalf("executions / completion calls = %d / %d, want 1 / 2",
					process.calls, store.completeCalls)
			}
			if string(store.outcome.Status) != verdict || store.outcome.Assistant != "original outcome" {
				t.Fatalf("lost intended outcome: %#v", store.outcome)
			}
		})
	}
}

func TestPiRunnerPostgresCompletionFailureRedeliveryDoesNotReplay(t *testing.T) {
	for _, tc := range []struct {
		name     string
		failures int
	}{{"transient_concurrent_duplicate", 1}, {"restart_reconciliation", 3}} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, pool, store := newRuntimePostgresStore(t)
			project, err := store.CreateProject(ctx, postgres.CreateProjectParams{Name: "reconciliation"})
			if err != nil {
				t.Fatal(err)
			}
			conversation, err := store.CreateConversation(ctx, postgres.CreateConversationParams{ProjectID: project.ID})
			if err != nil {
				t.Fatal(err)
			}
			_, run, err := store.CreateMessageAndRun(ctx, postgres.CreateMessageParams{
				ConversationID: conversation.ID, Content: "perform once", AgentID: "browser-eval",
				Model: event.ModelRef{Provider: "pi", Name: "test"}, IdempotencyKey: t.Name(),
			})
			if err != nil {
				t.Fatal(err)
			}
			piStore := PostgresPiTraceStore{Store: store}
			faults := &completionFaultStore{PiTraceStore: piStore, failures: tc.failures}
			process := &fakeHarnessProcess{events: []piharness.Event{
				harEvent(piharness.EventSettled, run.ID, "", 1,
					`{"verdict":"succeeded","assistant_text":"performed once"}`),
			}}
			// Events must carry the provider identity allocated by StartRun.
			execution := &identityHarnessProcess{process: process}
			consumer := Consumer{
				Inbox:  DeduplicatedInbox{Deduplicator: inboxconsumer.NewDeduplicator(pool, "agenttrace-runtime")},
				Runner: PiRunner{Store: faults, Process: execution},
			}
			command := commandFor(run.ID)
			command.ConversationID = conversation.ID
			message := commandMessage(t, "evt-reconciliation", command)
			var firstErr error
			if tc.failures == 1 {
				started, release := make(chan struct{}), make(chan struct{})
				var once sync.Once
				defer once.Do(func() { close(release) })
				process.afterEvents = func() { close(started); <-release }
				done := make(chan error, 2)
				go func() { done <- consumer.Handle(ctx, message) }()
				select {
				case <-started:
				case err := <-done:
					t.Fatalf("first delivery failed before execution: %v", err)
				case <-time.After(5 * time.Second):
					t.Fatal("first delivery did not execute")
				}
				if err := consumer.Handle(ctx, message); !inboxconsumer.IsDeliveryDeferred(err) {
					t.Fatalf("active duplicate must defer without a database lock: %v", err)
				}
				var openTransactions int
				if err := pool.QueryRow(ctx, `SELECT count(*) FROM pg_stat_activity
					WHERE datname=current_database() AND state='idle in transaction'`).Scan(&openTransactions); err != nil || openTransactions != 0 {
					t.Fatalf("execution retained transactions=%d err=%v", openTransactions, err)
				}
				once.Do(func() { close(release) })
				if err := <-done; err != nil {
					t.Fatalf("first delivery: %v", err)
				}
				if err := consumer.Handle(ctx, message); err != nil {
					t.Fatalf("completed duplicate replay: %v", err)
				}
			} else {
				firstErr = consumer.Handle(ctx, message)
			}
			if (firstErr != nil) != (tc.failures == 3) {
				t.Fatalf("first delivery = %v, failures = %d", firstErr, tc.failures)
			}
			// A fresh runner/store simulates loss of all in-process outcome state.
			consumer.Runner = PiRunner{Store: PostgresPiTraceStore{Store: postgres.NewStore(pool)}, Process: execution}
			for range 2 {
				if err := consumer.Handle(ctx, message); err != nil {
					t.Fatalf("restart redelivery: %v", err)
				}
			}
			finished, err := store.GetRun(ctx, run.ID)
			want := domain.RunSucceeded
			if tc.failures == 3 {
				want = domain.RunFailed
			}
			if err != nil || finished.Status != want || finished.FinishedAt == nil {
				t.Fatalf("terminal run = %#v, %v, want %s", finished, err, want)
			}
			var attempts, completions, inbox int
			if err := pool.QueryRow(ctx, `
				SELECT (SELECT count(*) FROM harness_attempts WHERE run_id = $1),
				       (SELECT count(*) FROM run_events WHERE run_id = $1 AND event_type = 'run.completed'),
				       (SELECT count(*) FROM inbox_events WHERE event_id = 'evt-reconciliation' AND processed_at IS NOT NULL)`,
				run.ID).Scan(&attempts, &completions, &inbox); err != nil {
				t.Fatal(err)
			}
			if process.calls != 1 || attempts != 1 || completions != 1 || inbox != 1 {
				t.Fatalf("executions / attempts / completions / inbox = %d / %d / %d / %d",
					process.calls, attempts, completions, inbox)
			}
		})
	}
}

type completionFaultStore struct {
	PiTraceStore
	failures int
}

func (s *completionFaultStore) CompleteHarnessRun(ctx context.Context, ref HarnessAttemptRef, outcome PiRunOutcome) (domain.RunStatus, error) {
	if s.failures > 0 {
		s.failures--
		return "", errors.New("terminal write unavailable")
	}
	return s.PiTraceStore.CompleteHarnessRun(ctx, ref, outcome)
}

type identityHarnessProcess struct {
	process *fakeHarnessProcess
}

func (p *identityHarnessProcess) Run(ctx context.Context, request piharness.Request, onEvent func(piharness.Event)) error {
	return p.process.Run(ctx, request, func(ev piharness.Event) {
		ev.AttemptID = request.AttemptID
		onEvent(ev)
	})
}

func TestPiRunnerPersistsTerminalOutcomeAfterDeliveryContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	store := &fakePiStore{ref: HarnessAttemptRef{ID: "hat-cancelled-delivery", RunID: "run-cancelled-delivery"}}
	process := &fakeHarnessProcess{err: context.Canceled, afterEvents: cancel}
	err := (PiRunner{Process: process, Store: store}).Run(ctx, commandFor(store.ref.RunID))
	if !isTerminalRunError(err) || !store.completed || store.outcome.Status != domain.RunCancelled {
		t.Fatalf("cancelled delivery = %v, completion = %#v", err, store.outcome)
	}
}

func TestPiRunnerCancelledOnContextCancellation(t *testing.T) {
	store := &fakePiStore{ref: HarnessAttemptRef{
		ID: "hat-4", RunID: "run-cancel", AttemptNumber: 1, ProviderAttemptID: "att-cancel",
	}}
	process := &fakeHarnessProcess{err: context.Canceled}
	runner := PiRunner{Process: process, Store: store}

	err := runner.Run(context.Background(), commandFor("run-cancel"))
	if !isTerminalRunError(err) {
		t.Fatalf("error = %v, want terminalRunError", err)
	}
	if store.outcome.Status != domain.RunCancelled {
		t.Fatalf("outcome status = %q, want cancelled", store.outcome.Status)
	}
}

func TestPiRunnerFailsOnPrematureExit(t *testing.T) {
	events := []piharness.Event{
		harEvent(piharness.EventStarted, "run-premature", "att-premature", 1, `{}`),
	}
	store := &fakePiStore{ref: HarnessAttemptRef{
		ID: "hat-5", RunID: "run-premature", AttemptNumber: 1, ProviderAttemptID: "att-premature",
	}}
	process := &fakeHarnessProcess{events: events, err: piharness.ErrPrematureExit}
	runner := PiRunner{Process: process, Store: store}

	err := runner.Run(context.Background(), commandFor("run-premature"))
	if !isTerminalRunError(err) {
		t.Fatalf("error = %v, want terminalRunError", err)
	}
	if store.outcome.Status != domain.RunFailed {
		t.Fatalf("outcome status = %q, want failed", store.outcome.Status)
	}
}

func TestPiRunnerPersistsEventsInOrder(t *testing.T) {
	events := passingEventStream("run-order", "att-order")
	store := &fakePiStore{ref: HarnessAttemptRef{
		ID: "hat-6", RunID: "run-order", AttemptNumber: 1, ProviderAttemptID: "att-order",
	}}
	process := &fakeHarnessProcess{events: events}
	runner := PiRunner{Process: process, Store: store}

	if err := runner.Run(context.Background(), commandFor("run-order")); err != nil {
		t.Fatalf("run = %v", err)
	}
	for i, ev := range events {
		if store.events[i].Sequence != ev.Sequence {
			t.Fatalf("event %d sequence = %d, want %d", i, store.events[i].Sequence, ev.Sequence)
		}
	}
}

func TestPiRunnerStopsOnPersistenceFailureAndAwaitsCleanup(t *testing.T) {
	writeErr := errors.New("database write rejected")
	store := &fakePiStore{ref: HarnessAttemptRef{ID: "hat-write", RunID: "run-write", ProviderAttemptID: "att-write"}, appendErr: writeErr}
	process := &persistenceFailureProcess{cleaned: make(chan struct{})}
	runner := PiRunner{Process: process, Store: store}
	err := runner.Run(context.Background(), commandFor("run-write"))
	if !isTerminalRunError(err) || !strings.Contains(err.Error(), writeErr.Error()) {
		t.Fatalf("run error = %v, want durable write failure", err)
	}
	if process.actions != 0 {
		t.Fatal("tool action continued after evidence write failed")
	}
	select {
	case <-process.cleaned:
	default:
		t.Fatal("run returned before resource cleanup")
	}
	if store.outcome.Status != domain.RunFailed {
		t.Fatalf("status = %q, persistence failure must not become cancellation", store.outcome.Status)
	}
	if len(store.events) != 1 {
		t.Fatalf("append attempts = %d, want only first failed event", len(store.events))
	}
}

func TestPiRunnerRequiresHarnessVerdictDespitePassingBrowserCase(t *testing.T) {
	store := &fakePiStore{ref: HarnessAttemptRef{ID: "hat-missing", RunID: "run-missing", ProviderAttemptID: "att-missing"}}
	events := passingEventStream("run-missing", "att-missing")
	events[len(events)-1].Payload = json.RawMessage(`{"assistant_text":"no verdict"}`)
	runner := PiRunner{Process: &fakeHarnessProcess{events: events}, Store: store}
	if err := runner.Run(context.Background(), commandFor("run-missing")); !isTerminalRunError(err) {
		t.Fatalf("missing explicit verdict accepted: %v", err)
	}
}

type persistenceFailureProcess struct {
	actions int
	cleaned chan struct{}
}

func (p *persistenceFailureProcess) Run(ctx context.Context, _ piharness.Request, onEvent func(piharness.Event)) error {
	onEvent(harEvent(piharness.EventStarted, "run-write", "att-write", 1, `{}`))
	select {
	case <-ctx.Done():
		// Simulate cleanup evidence arriving after cancellation; it must not
		// attempt to append behind a missing source sequence.
		onEvent(harEvent(piharness.EventFailed, "run-write", "att-write", 2, `{"error":"cancelled"}`))
		close(p.cleaned)
		return ctx.Err()
	default:
		p.actions++
		close(p.cleaned)
		return nil
	}
}

func passingEventStream(runID, attemptID string) []piharness.Event {
	return []piharness.Event{
		harEvent(piharness.EventStarted, runID, attemptID, 1, `{}`),
		harEvent(piharness.EventMessageCompleted, runID, attemptID, 2, `{"text":"thinking"}`),
		harEvent(piharness.EventUsage, runID, attemptID, 3, `{"input_tokens":10,"output_tokens":5}`),
		harEvent(piharness.EventToolStarted, runID, attemptID, 4, `{"tool_call_id":"tc1","tool_name":"browser"}`),
		harEvent(piharness.EventObservation, runID, attemptID, 5, `{"observation":"page loaded"}`),
		harEvent(piharness.EventToolCompleted, runID, attemptID, 6, `{"tool_call_id":"tc1","tool_name":"browser","is_error":false}`),
		harEvent(piharness.EventCaseCompleted, runID, attemptID, 7, `{"verdict":"passed","steps":[{"action":"click"}]}`),
		harEvent(piharness.EventSettled, runID, attemptID, 8, `{"assistant_text":"final answer","verdict":"succeeded"}`),
	}
}

func harEvent(typ piharness.EventType, runID, attemptID string, seq int64, payload string) piharness.Event {
	return piharness.Event{
		Type:      typ,
		RunID:     runID,
		AttemptID: attemptID,
		Sequence:  seq,
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
		Payload:   json.RawMessage(payload),
	}
}

type fakeHarnessProcess struct {
	events      []piharness.Event
	err         error
	calls       int
	afterEvents func()
	request     piharness.Request
}

func (f *fakeHarnessProcess) Run(_ context.Context, request piharness.Request, onEvent func(piharness.Event)) error {
	f.calls++
	f.request = request
	for _, ev := range f.events {
		onEvent(ev)
	}
	if f.afterEvents != nil {
		f.afterEvents()
	}
	return f.err
}

type blockingHarnessProcess struct {
	started  chan struct{}
	canceled chan struct{}
}

func (f *blockingHarnessProcess) Run(ctx context.Context, _ piharness.Request, _ func(piharness.Event)) error {
	close(f.started)
	<-ctx.Done()
	close(f.canceled)
	return ctx.Err()
}

type fakePiStore struct {
	mu                    sync.Mutex
	ref                   HarnessAttemptRef
	startErr              error
	events                []piharness.Event
	outcome               PiRunOutcome
	completed             bool
	completeStatus        domain.RunStatus
	completeErr           error
	completeFailures      int
	completeCalls         int
	appendErr             error
	cancellationRequested bool
}

func (f *fakePiStore) StartRun(_ context.Context, _ event.RunCommand) (HarnessAttemptRef, error) {
	if f.startErr != nil {
		return HarnessAttemptRef{}, f.startErr
	}
	return f.ref, nil
}

func (f *fakePiStore) AppendHarnessEvent(_ context.Context, _ HarnessAttemptRef, ev piharness.Event) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, ev)
	return f.appendErr
}

func (f *fakePiStore) CompleteHarnessRun(ctx context.Context, _ HarnessAttemptRef, outcome PiRunOutcome) (domain.RunStatus, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return "", err
	}
	f.completeCalls++
	if f.completeFailures > 0 {
		f.completeFailures--
		return "", errors.New("transient terminal persistence failure")
	}
	f.outcome = outcome
	if f.completeStatus != "" {
		f.outcome.Status = f.completeStatus
	}
	f.completed = true
	return f.outcome.Status, f.completeErr
}

func (f *fakePiStore) CancellationRequested(_ context.Context, _ string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.cancellationRequested, nil
}

func (f *fakePiStore) setCancellationRequested(requested bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cancellationRequested = requested
}
