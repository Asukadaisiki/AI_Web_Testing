package postgres

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/domain"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/event"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/testpostgres"
)

func managedMessageParams(t *testing.T, ctx context.Context, s *Store, p CreateMessageParams) CreateMessageParams {
	t.Helper()
	p.RuntimeID = "pi"
	p.HarnessVersionID = testpostgres.PrepareManagedConversation(t, ctx, s.pool, p.ConversationID)
	return p
}

func managedProjectParams(p CreateProjectParams) CreateProjectParams {
	if p.LangfuseProjectID == "" {
		p.LangfuseProjectID = "lf_fixture_" + p.Name
	}
	return p
}

func claimFixtureAttempt(t *testing.T, ctx context.Context, s *Store, runID, provider string) (HarnessAttempt, error) {
	t.Helper()
	run, err := s.GetRun(ctx, runID)
	if err != nil {
		return HarnessAttempt{}, err
	}
	var spec runSpecification
	if err := json.Unmarshal(run.Specification, &spec); err != nil {
		return HarnessAttempt{}, err
	}
	registerLeaseWorkers(t, ctx, s, 16)
	claim, err := s.ClaimHarnessLease(ctx, LeaseClaim{RunID: runID, ProjectID: run.ProjectID, EnvironmentID: spec.Command.EnvironmentID,
		ProfileGeneration: spec.Command.ProfileGeneration, RuntimeID: spec.Command.Execution.RuntimeID,
		RuntimeWorkerID: "runtime", ProviderAttemptID: provider, TTL: time.Minute})
	return claim.Attempt, err
}

func fixtureFence(t *testing.T, ctx context.Context, s *Store, runID, attemptID string) HarnessFence {
	t.Helper()
	var f HarnessFence
	if err := s.pool.QueryRow(ctx, `SELECT project_id,environment_id,run_id,attempt_id,runtime_worker_id,browser_worker_id,profile_generation,fencing_token
		FROM environment_leases WHERE run_id=$1 AND attempt_id=$2`, runID, attemptID).Scan(&f.ProjectID, &f.EnvironmentID, &f.RunID,
		&f.AttemptID, &f.RuntimeWorkerID, &f.BrowserWorkerID, &f.ProfileGeneration, &f.FencingToken); err != nil {
		t.Fatal(err)
	}
	return f
}

func appendFixtureEvent(t *testing.T, ctx context.Context, s *Store, record HarnessEventRecord) error {
	record.Fence = fixtureFence(t, ctx, s, record.RunID, record.AttemptID)
	return s.AppendHarnessEvent(ctx, record)
}

func finishFixtureAttempt(t *testing.T, ctx context.Context, s *Store, finish HarnessFinish) (domain.RunStatus, error) {
	finish.Fence = fixtureFence(t, ctx, s, finish.RunID, finish.AttemptID)
	return s.FinishHarnessAttempt(ctx, finish)
}

func finishFixtureRun(t *testing.T, ctx context.Context, s *Store, runID string, status domain.RunStatus, runError json.RawMessage, assistant string, ev event.Envelope[json.RawMessage]) error {
	attempt, err := claimFixtureAttempt(t, ctx, s, runID, "provider-"+runID)
	if err != nil {
		// A terminal replay uses the already allocated fencing identity.
		if err := s.pool.QueryRow(ctx, `SELECT id FROM harness_attempts WHERE run_id=$1 ORDER BY attempt_number DESC LIMIT 1`, runID).Scan(&attempt.ID); err != nil {
			return err
		}
	}
	_, err = finishFixtureAttempt(t, ctx, s, HarnessFinish{RunID: runID, AttemptID: attempt.ID, Status: status, Error: runError, Assistant: assistant, Event: ev})
	return err
}
