package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/domain"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/event"
	"github.com/jackc/pgx/v5"
)

var (
	ErrCapacityBusy          = errors.New("worker or environment capacity is busy")
	ErrStaleFence            = errors.New("environment lease fencing ownership lost")
	ErrReleaseUnacknowledged = errors.New("harness release acknowledgement is required")
)

const DefaultHarnessLeaseTTL = 30 * time.Second

type HarnessFence struct {
	ProjectID         string
	EnvironmentID     string
	RunID             string
	AttemptID         string
	RuntimeWorkerID   string
	BrowserWorkerID   string
	ProfileGeneration int64
	FencingToken      int64
}

type LeaseClaim struct {
	ProjectID         string
	EnvironmentID     string
	RunID             string
	RuntimeWorkerID   string
	ProviderAttemptID string
	ProfileGeneration int64
	RuntimeID         string
	TTL               time.Duration
}

type HarnessLeaseClaim struct {
	Attempt   HarnessAttempt
	Fence     HarnessFence
	WorkerURL string
}

func leaseTTL(ttl time.Duration) (time.Duration, error) {
	if ttl == 0 {
		ttl = DefaultHarnessLeaseTTL
	}
	if ttl < 3*time.Millisecond {
		return 0, errors.New("harness lease TTL must be at least three milliseconds")
	}
	return ttl, nil
}

// ClaimHarnessLease commits both capacity reservations and the attempt before
// any SDK or browser work. Lock order is conversation, environment, run, lease,
// attempt, runtime node, browser node across every scheduling operation.
func (s *Store) ClaimHarnessLease(ctx context.Context, p LeaseClaim) (result HarnessLeaseClaim, resultErr error) {
	defer func() { s.ObserveLeaseResult(ctx, p.EnvironmentID, resultErr) }()
	if err := s.requireManagement(); err != nil {
		return result, err
	}
	if strings.TrimSpace(p.RunID) == "" || p.ProjectID == "" || p.EnvironmentID == "" ||
		p.RuntimeWorkerID == "" || p.ProviderAttemptID == "" || p.ProfileGeneration < 1 {
		return result, errors.New("lease run, project, environment, generation, runtime and provider identities are required")
	}
	ttl, err := leaseTTL(p.TTL)
	if err != nil {
		return result, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return result, err
	}
	defer tx.Rollback(ctx)
	env, err := lockEnvironment(ctx, tx, p.EnvironmentID)
	if err != nil {
		return result, err
	}
	var status domain.RunStatus
	var conversationID, projectID string
	var cancelRequested bool
	if err := tx.QueryRow(ctx, `SELECT status,conversation_id,project_id,cancel_requested_at IS NOT NULL FROM runs WHERE id=$1 FOR UPDATE`, p.RunID).
		Scan(&status, &conversationID, &projectID, &cancelRequested); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return result, ErrNotFound
		}
		return result, err
	}
	if projectID != p.ProjectID || env.ProjectID != p.ProjectID || env.ConversationID != conversationID {
		return result, ErrNotFound
	}
	if isTerminalStatus(status) {
		return result, ErrRunAlreadyTerminal
	}
	if env.ProfileGeneration != p.ProfileGeneration {
		return result, ErrStaleFence
	}
	var occupied bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM environment_leases WHERE environment_id=$1 AND state IN ('active','releasing'))`, env.ID).Scan(&occupied); err != nil {
		return result, err
	}
	if occupied || env.State == "leased" {
		return result, ErrCapacityBusy
	}
	if env.State != "ready" {
		return result, ErrConflict
	}
	var conversationState string
	if err := tx.QueryRow(ctx, `SELECT state FROM conversations WHERE id=$1`, conversationID).Scan(&conversationState); err != nil {
		return result, err
	}
	if conversationState != "active" {
		return result, ErrConflict
	}
	var unsafePrior bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM harness_attempts WHERE run_id=$1 AND status NOT IN ('aborted'))`, p.RunID).Scan(&unsafePrior); err != nil {
		return result, err
	}
	if unsafePrior {
		// Management claims never reconcile an active delivery by replaying it.
		return result, ErrCapacityBusy
	}

	var boundBrowserID string
	if err := tx.QueryRow(ctx, `SELECT COALESCE(browser_worker_id,'') FROM environments WHERE id=$1`, env.ID).Scan(&boundBrowserID); err != nil {
		return result, err
	}
	if boundBrowserID == "" {
		return result, ErrCapacityBusy
	}
	tag, err := tx.Exec(ctx, `UPDATE worker_nodes SET in_use=in_use+1,updated_at=now()
		WHERE id=$1 AND role='runtime' AND NOT draining AND in_use<capacity
		AND (project_id IS NULL OR project_id=$2)
		AND (runtime_ids='[]'::jsonb OR runtime_ids ? $3)
		AND last_seen_at + heartbeat_ttl_ms * interval '1 millisecond' > clock_timestamp()`,
		p.RuntimeWorkerID, p.ProjectID, p.RuntimeID)
	if err != nil {
		return result, err
	}
	if tag.RowsAffected() != 1 {
		return result, ErrCapacityBusy
	}
	var browserID, workerURL string
	if err := tx.QueryRow(ctx, `SELECT id,endpoint FROM worker_nodes WHERE role='browser' AND NOT draining AND in_use<capacity
		AND (project_id IS NULL OR project_id=$1)
		AND id=$2
		AND last_seen_at + heartbeat_ttl_ms * interval '1 millisecond' > clock_timestamp()
		FOR UPDATE SKIP LOCKED`, p.ProjectID, boundBrowserID).Scan(&browserID, &workerURL); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return result, ErrCapacityBusy
		}
		return result, err
	}
	if _, err := tx.Exec(ctx, `UPDATE worker_nodes SET in_use=in_use+1,updated_at=now() WHERE id=$1`, browserID); err != nil {
		return result, err
	}
	attemptID, err := newID("hat")
	if err != nil {
		return result, err
	}
	var number int
	var started time.Time
	if err := tx.QueryRow(ctx, `INSERT INTO harness_attempts(id,run_id,attempt_number,provider_attempt_id,status)
		VALUES($1,$2,(SELECT COALESCE(max(attempt_number),0)+1 FROM harness_attempts WHERE run_id=$2),$3,'active')
		RETURNING attempt_number,started_at`, attemptID, p.RunID, p.ProviderAttemptID).Scan(&number, &started); err != nil {
		return result, err
	}
	var token int64
	if err := tx.QueryRow(ctx, `UPDATE environments SET state='leased',fencing_token=fencing_token+1,last_used_at=now(),updated_at=now()
		WHERE id=$1 RETURNING fencing_token`, env.ID).Scan(&token); err != nil {
		return result, err
	}
	fence := HarnessFence{ProjectID: p.ProjectID, EnvironmentID: env.ID, RunID: p.RunID, AttemptID: attemptID,
		RuntimeWorkerID: p.RuntimeWorkerID, BrowserWorkerID: browserID, ProfileGeneration: p.ProfileGeneration, FencingToken: token}
	if _, err := tx.Exec(ctx, `INSERT INTO worker_assignments(attempt_id,run_id,project_id,environment_id,runtime_worker_id,browser_worker_id,profile_generation,fencing_token)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, attemptID, p.RunID, p.ProjectID, env.ID, p.RuntimeWorkerID, browserID, p.ProfileGeneration, token); err != nil {
		return result, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO environment_leases(environment_id,attempt_id,run_id,project_id,runtime_worker_id,browser_worker_id,profile_generation,fencing_token,expires_at)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,clock_timestamp()+$9::bigint*interval '1 microsecond')
		ON CONFLICT(environment_id) DO UPDATE SET attempt_id=EXCLUDED.attempt_id,run_id=EXCLUDED.run_id,project_id=EXCLUDED.project_id,
		runtime_worker_id=EXCLUDED.runtime_worker_id,browser_worker_id=EXCLUDED.browser_worker_id,
		profile_generation=EXCLUDED.profile_generation,fencing_token=EXCLUDED.fencing_token,
		state='active',acquired_at=now(),renewed_at=now(),expires_at=EXCLUDED.expires_at`,
		env.ID, attemptID, p.RunID, p.ProjectID, p.RuntimeWorkerID, browserID, p.ProfileGeneration, token, ttl.Microseconds()); err != nil {
		return result, err
	}
	if _, err := tx.Exec(ctx, `UPDATE runs SET status=CASE WHEN status='cancelling' THEN status ELSE 'running' END,
		started_at=COALESCE(started_at,now()),updated_at=now() WHERE id=$1`, p.RunID); err != nil {
		return result, err
	}
	result = HarnessLeaseClaim{Fence: fence, WorkerURL: workerURL, Attempt: HarnessAttempt{ID: attemptID, RunID: p.RunID, AttemptNumber: number,
		ProviderAttemptID: p.ProviderAttemptID, Status: "active", StartedAt: started, CancelRequested: cancelRequested || status == domain.RunCancelling}}
	return result, tx.Commit(ctx)
}

// lockHarnessFence is called within the transaction which performs the write,
// before idempotency handling: even an exact stale replay must be rejected.
func lockHarnessFence(ctx context.Context, tx pgx.Tx, f HarnessFence, allowReleased bool) (string, error) {
	if f.ProjectID == "" || f.EnvironmentID == "" || f.RunID == "" || f.AttemptID == "" || f.RuntimeWorkerID == "" ||
		f.BrowserWorkerID == "" || f.ProfileGeneration < 1 || f.FencingToken < 1 {
		return "", ErrStaleFence
	}
	env, err := lockEnvironment(ctx, tx, f.EnvironmentID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return "", ErrStaleFence
		}
		return "", err
	}
	if env.ProjectID != f.ProjectID || env.FencingToken != f.FencingToken || env.ProfileGeneration != f.ProfileGeneration {
		return "", ErrStaleFence
	}
	var state string
	var live, identity bool
	err = tx.QueryRow(ctx, `SELECT state,expires_at>clock_timestamp(),
		attempt_id=$2 AND run_id=$3 AND project_id=$4 AND runtime_worker_id=$5 AND browser_worker_id=$6 AND profile_generation=$7 AND fencing_token=$8
		FROM environment_leases WHERE environment_id=$1 FOR UPDATE`,
		f.EnvironmentID, f.AttemptID, f.RunID, f.ProjectID, f.RuntimeWorkerID, f.BrowserWorkerID, f.ProfileGeneration, f.FencingToken).Scan(&state, &live, &identity)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrStaleFence
	}
	if err != nil {
		return "", err
	}
	if !identity || !(allowReleased && state == "released") && (!live || (state != "active" && state != "releasing") || env.State != "leased") {
		return "", ErrStaleFence
	}
	return state, nil
}

func (s *Store) RenewHarnessLease(ctx context.Context, f HarnessFence, ttl time.Duration) (resultErr error) {
	defer func() { s.ObserveLeaseResult(ctx, f.EnvironmentID, resultErr) }()
	if err := s.requireManagement(); err != nil {
		return err
	}
	ttl, err := leaseTTL(ttl)
	if err != nil {
		return err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := lockHarnessFence(ctx, tx, f, false); err != nil {
		return err
	}
	var alive bool
	if err := tx.QueryRow(ctx, `SELECT count(*)=2 FROM worker_nodes WHERE id IN ($1,$2)
		AND last_seen_at + heartbeat_ttl_ms * interval '1 millisecond' > clock_timestamp()`, f.RuntimeWorkerID, f.BrowserWorkerID).Scan(&alive); err != nil {
		return err
	}
	if !alive {
		return ErrStaleFence
	}
	if _, err := tx.Exec(ctx, `UPDATE environment_leases SET renewed_at=now(),expires_at=clock_timestamp()+$2::bigint*interval '1 microsecond' WHERE environment_id=$1`, f.EnvironmentID, ttl.Microseconds()); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// MarkHarnessStarted precedes submission to the SDK. A crash in the small gap
// after this write is intentionally uncertain, never eligible for replay.
func (s *Store) MarkHarnessStarted(ctx context.Context, f HarnessFence) (resultErr error) {
	defer func() { s.ObserveLeaseResult(ctx, f.EnvironmentID, resultErr) }()
	if err := s.requireManagement(); err != nil {
		return err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := lockHarnessFence(ctx, tx, f, false); err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `UPDATE harness_attempts SET sdk_started_at=clock_timestamp() WHERE id=$1 AND run_id=$2 AND status='active' AND sdk_started_at IS NULL`, f.AttemptID, f.RunID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return ErrStaleFence
	}
	return tx.Commit(ctx)
}

// InitializeHarnessTrace fences the first/root evidence, not only streamed
// events. The root span can never appear after ownership has been replaced.
func (s *Store) InitializeHarnessTrace(ctx context.Context, f HarnessFence, started TraceAttemptStart) (resultErr error) {
	defer func() { s.ObserveLeaseResult(ctx, f.EnvironmentID, resultErr) }()
	if err := s.requireManagement(); err != nil {
		return err
	}
	if err := validateTraceAttemptStart(started); err != nil {
		return err
	}
	if started.Span.RunID != f.RunID || (started.Root != nil && started.Root.RunID != f.RunID) {
		return ErrStaleFence
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := lockHarnessFence(ctx, tx, f, false); err != nil {
		return err
	}
	var checkpoint int64
	if err := tx.QueryRow(ctx, `SELECT checkpoint_version FROM runs WHERE id=$1 FOR UPDATE`, f.RunID).Scan(&checkpoint); err != nil {
		return err
	}
	if checkpoint != started.ExpectedCheckpoint {
		return ErrConflict
	}
	if started.Root != nil {
		if _, err := insertStartedSpan(ctx, tx, *started.Root); err != nil {
			return err
		}
	}
	spanID, err := insertStartedSpan(ctx, tx, started.Span)
	if err != nil {
		return err
	}
	if err := insertStartedTraceAttempt(ctx, tx, spanID, started.Attempt); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func releaseCapacity(ctx context.Context, tx pgx.Tx, f HarnessFence, state string) error {
	// The assignment condition protects the counters from duplicate recovery.
	tag, err := tx.Exec(ctx, `UPDATE worker_assignments SET state=$2,released_at=now() WHERE attempt_id=$1 AND state IN ('active','releasing')`, f.AttemptID, state)
	if err != nil || tag.RowsAffected() == 0 {
		return err
	}
	for _, id := range []string{f.RuntimeWorkerID, f.BrowserWorkerID} {
		tag, err := tx.Exec(ctx, `UPDATE worker_nodes SET in_use=in_use-1,updated_at=now() WHERE id=$1 AND in_use>0`, id)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return fmt.Errorf("worker capacity invariant violated for %s", id)
		}
	}
	_, err = tx.Exec(ctx, `UPDATE environment_leases SET state=$2 WHERE environment_id=$1`, f.EnvironmentID, state)
	return err
}

func (s *Store) ReleaseHarnessLease(ctx context.Context, f HarnessFence, acknowledged bool) (resultErr error) {
	defer func() { s.ObserveLeaseResult(ctx, f.EnvironmentID, resultErr) }()
	if err := s.requireManagement(); err != nil {
		return err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	state, err := lockHarnessFence(ctx, tx, f, true)
	if err != nil {
		return err
	}
	if !acknowledged {
		return ErrReleaseUnacknowledged
	}
	if state == "released" {
		return tx.Commit(ctx)
	}
	var terminal bool
	if err := tx.QueryRow(ctx, `SELECT status IN ('succeeded','failed','cancelled') FROM runs WHERE id=$1 FOR UPDATE`, f.RunID).Scan(&terminal); err != nil {
		return err
	}
	if !terminal {
		return ErrConflict
	}
	if err := releaseCapacity(ctx, tx, f, "released"); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE environments SET state='ready',updated_at=now() WHERE id=$1`, f.EnvironmentID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// AbandonUnstartedHarnessLease is not a host release acknowledgement. It only
// reclaims a terminal claim with durable proof that no SDK request was started.
func (s *Store) AbandonUnstartedHarnessLease(ctx context.Context, f HarnessFence) (resultErr error) {
	defer func() { s.ObserveLeaseResult(ctx, f.EnvironmentID, resultErr) }()
	if err := s.requireManagement(); err != nil {
		return err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := lockHarnessFence(ctx, tx, f, false); err != nil {
		return err
	}
	var terminal, started bool
	if err := tx.QueryRow(ctx, `SELECT status IN ('succeeded','failed','cancelled') FROM runs WHERE id=$1 FOR UPDATE`, f.RunID).Scan(&terminal); err != nil {
		return err
	}
	if err := tx.QueryRow(ctx, `SELECT sdk_started_at IS NOT NULL FROM harness_attempts WHERE id=$1 FOR UPDATE`, f.AttemptID).Scan(&started); err != nil {
		return err
	}
	if !terminal || started {
		return ErrConflict
	}
	if err := releaseCapacity(ctx, tx, f, "expired"); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE environments SET state='ready',fencing_token=fencing_token+1,updated_at=now() WHERE id=$1`, f.EnvironmentID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// RecoverExpiredHarnessLeases uses short per-environment transactions. It also
// recovers dead workers conservatively from PostgreSQL heartbeat timestamps.
func (s *Store) RecoverExpiredHarnessLeases(ctx context.Context, limit int) (int, error) {
	if err := s.requireManagement(); err != nil {
		return 0, err
	}
	if limit < 1 || limit > 1000 {
		return 0, errors.New("lease recovery limit must be 1..1000")
	}
	rows, err := s.pool.Query(ctx, `SELECT l.environment_id FROM environment_leases l
		WHERE l.state IN ('active','releasing') AND (l.expires_at<=clock_timestamp() OR EXISTS(
		  SELECT 1 FROM worker_nodes w WHERE w.id IN (l.runtime_worker_id,l.browser_worker_id)
		  AND w.last_seen_at + w.heartbeat_ttl_ms * interval '1 millisecond' <= clock_timestamp()))
		ORDER BY l.expires_at LIMIT $1`, limit)
	if err != nil {
		return 0, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	n := 0
	for _, id := range ids {
		recovered, err := s.recoverHarnessLease(ctx, id)
		if err != nil {
			return n, err
		}
		if recovered {
			n++
		}
	}
	return n, nil
}

func (s *Store) recoverHarnessLease(ctx context.Context, id string) (bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	env, err := lockEnvironment(ctx, tx, id)
	if err != nil {
		return false, err
	}
	var f HarnessFence
	var recoverable bool
	err = tx.QueryRow(ctx, `SELECT environment_id,attempt_id,run_id,project_id,runtime_worker_id,browser_worker_id,profile_generation,fencing_token,
		state IN ('active','releasing') AND (expires_at<=clock_timestamp() OR EXISTS(
		  SELECT 1 FROM worker_nodes w WHERE w.id IN (runtime_worker_id,browser_worker_id)
		  AND w.last_seen_at + w.heartbeat_ttl_ms * interval '1 millisecond' <= clock_timestamp()))
		FROM environment_leases WHERE environment_id=$1 FOR UPDATE`, id).Scan(
		&f.EnvironmentID, &f.AttemptID, &f.RunID, &f.ProjectID, &f.RuntimeWorkerID, &f.BrowserWorkerID, &f.ProfileGeneration, &f.FencingToken, &recoverable)
	if err != nil || !recoverable {
		return false, err
	}
	var status domain.RunStatus
	if err := tx.QueryRow(ctx, `SELECT status FROM runs WHERE id=$1 FOR UPDATE`, f.RunID).Scan(&status); err != nil {
		return false, err
	}
	var started bool
	if err := tx.QueryRow(ctx, `SELECT sdk_started_at IS NOT NULL FROM harness_attempts WHERE id=$1 FOR UPDATE`, f.AttemptID).Scan(&started); err != nil {
		return false, err
	}
	envState := "ready"
	if started {
		envState = "needs_review"
	}
	if !isTerminalStatus(status) && started {
		errorPayload := json.RawMessage(`{"code":"execution_lost","message":"Worker ownership expired after SDK execution started; external effects are uncertain. Review environment before an explicit new run."}`)
		if _, err := tx.Exec(ctx, `UPDATE runs SET status='failed',error=$2,finished_at=now(),updated_at=now() WHERE id=$1`, f.RunID, errorPayload); err != nil {
			return false, err
		}
		if _, err := tx.Exec(ctx, `UPDATE spans SET status='failed',ended_at=COALESCE(ended_at,now()),updated_at=now() WHERE run_id=$1 AND ended_at IS NULL`, f.RunID); err != nil {
			return false, err
		}
		ev := event.Envelope[event.RunCompletion]{SchemaVersion: "agenttrace.event.v1", EventID: "lease-lost:" + f.AttemptID,
			EventType: event.RunCompleted, AggregateID: f.RunID, CorrelationID: f.RunID, OccurredAt: time.Now().UTC(),
			Payload: event.RunCompletion{RunID: f.RunID, Status: domain.RunFailed, Error: errorPayload}}
		payload, err := json.Marshal(ev)
		if err != nil {
			return false, err
		}
		eventID, err := newID("rev")
		if err != nil {
			return false, err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO run_events(id,run_id,seq,event_type,payload,occurred_at)
			VALUES($1,$2,(SELECT COALESCE(max(seq),0)+1 FROM run_events WHERE run_id=$2),$3,$4,$5)`, eventID, f.RunID, ev.EventType, payload, ev.OccurredAt); err != nil {
			return false, err
		}
		hash, err := payloadHash(ev)
		if err != nil {
			return false, err
		}
		if _, err := insertOutbox(ctx, tx, ev.EventID, runEventsTopic, f.RunID, ev, hash); err != nil {
			return false, err
		}
	}
	attemptStatus := "aborted"
	if started {
		attemptStatus = "lost"
	}
	if _, err := tx.Exec(ctx, `UPDATE harness_attempts SET status=$2,finished_at=now() WHERE id=$1 AND status='active'`, f.AttemptID, attemptStatus); err != nil {
		return false, err
	}
	if !started && !isTerminalStatus(status) {
		if _, err := tx.Exec(ctx, `UPDATE runs SET status=CASE WHEN status='cancelling' THEN status ELSE 'queued' END,updated_at=now() WHERE id=$1`, f.RunID); err != nil {
			return false, err
		}
	}
	if err := releaseCapacity(ctx, tx, f, "expired"); err != nil {
		return false, err
	}
	// Increment on revocation as well as acquisition so an expired worker is
	// fenced immediately, even if review prevents a replacement claim.
	if env.FencingToken == f.FencingToken && env.ProfileGeneration == f.ProfileGeneration {
		if _, err := tx.Exec(ctx, `UPDATE environments SET state=$2,fencing_token=fencing_token+1,updated_at=now() WHERE id=$1`, id, envState); err != nil {
			return false, err
		}
	}
	return true, tx.Commit(ctx)
}
