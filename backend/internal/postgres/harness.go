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

// ErrRunAlreadyTerminal is returned by BeginHarnessAttempt when the run is
// already in a terminal state. The runtime must not spawn a child process for
// such a replay.
var ErrRunAlreadyTerminal = errors.New("run is already terminal")

// HarnessAttempt is the durable identity for one subprocess execution of a run.
type HarnessAttempt struct {
	ID                string
	RunID             string
	AttemptNumber     int
	ProviderAttemptID string
	Status            string
	StartedAt         time.Time
	FinishedAt        *time.Time
	// CancelRequested is true when the run had a durable cancel request
	// (cancel_requested_at set or status cancelling) at the time the attempt
	// was claimed. The runtime uses it to force RunCancelled regardless of the
	// harness verdict.
	CancelRequested bool
}

// BeginHarnessAttempt atomically claims a queued or active run and allocates a
// monotonically increasing harness attempt before the process is spawned. A
// replay of an already terminal run returns ErrRunAlreadyTerminal.
func (s *Store) BeginHarnessAttempt(
	ctx context.Context,
	runID, providerAttemptID string,
) (HarnessAttempt, error) {
	if s == nil || s.pool == nil {
		return HarnessAttempt{}, errors.New("postgres store is not configured")
	}
	if strings.TrimSpace(runID) == "" {
		return HarnessAttempt{}, errors.New("harness attempt run id is required")
	}
	if strings.TrimSpace(providerAttemptID) == "" {
		return HarnessAttempt{}, errors.New("harness provider attempt id is required")
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return HarnessAttempt{}, fmt.Errorf("begin harness attempt: %w", err)
	}
	defer tx.Rollback(ctx)

	run, err := scanRun(tx.QueryRow(ctx, `
		SELECT id, project_id, conversation_id,
		       COALESCE(source_message_id, ''), COALESCE(retry_of_run_id, ''),
		       status, specification, checkpoint_version,
		       cancel_requested_at, started_at, finished_at, created_at, updated_at
		FROM runs WHERE id = $1 FOR UPDATE`, runID))
	if errors.Is(err, ErrNotFound) {
		return HarnessAttempt{}, fmt.Errorf("run %q: %w", runID, ErrNotFound)
	}
	if err != nil {
		return HarnessAttempt{}, fmt.Errorf("lock run for harness attempt: %w", err)
	}
	if isTerminalStatus(run.Status) {
		return HarnessAttempt{}, ErrRunAlreadyTerminal
	}

	if _, err := tx.Exec(ctx, `
		UPDATE runs
		SET status = 'running',
		    started_at = COALESCE(started_at, now()),
		    updated_at = now()
		WHERE id = $1
		  AND status IN ('queued', 'running', 'cancelling')`, runID); err != nil {
		return HarnessAttempt{}, fmt.Errorf("claim run for harness attempt: %w", err)
	}

	var attemptNumber int
	if err := tx.QueryRow(ctx, `
		SELECT COALESCE(MAX(attempt_number), 0) + 1
		FROM harness_attempts
		WHERE run_id = $1`, runID).Scan(&attemptNumber); err != nil {
		return HarnessAttempt{}, fmt.Errorf("next harness attempt number: %w", err)
	}
	attemptID, err := newID("hat")
	if err != nil {
		return HarnessAttempt{}, err
	}
	var startedAt time.Time
	if err := tx.QueryRow(ctx, `
		INSERT INTO harness_attempts (
			id, run_id, attempt_number, provider_attempt_id, status, started_at
		) VALUES ($1, $2, $3, $4, 'active', now())
		RETURNING started_at`,
		attemptID, runID, attemptNumber, providerAttemptID,
	).Scan(&startedAt); err != nil {
		return HarnessAttempt{}, fmt.Errorf("insert harness attempt: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return HarnessAttempt{}, fmt.Errorf("commit harness attempt: %w", err)
	}
	return HarnessAttempt{
		ID:                attemptID,
		RunID:             runID,
		AttemptNumber:     attemptNumber,
		ProviderAttemptID: providerAttemptID,
		Status:            "active",
		StartedAt:         startedAt,
		CancelRequested:   run.CancelRequestedAt != nil || run.Status == domain.RunCancelling,
	}, nil
}

// HarnessEventRecord is one validated harness event and the optional trace
// evidence it carries. AppendHarnessEvent persists the event row atomically
// with any span, attempt, usage, or operation evidence.
type HarnessEventRecord struct {
	RunID          string
	AttemptID      string
	SourceSequence int64
	SourceType     string
	Event          event.Envelope[json.RawMessage]
	Span           *domain.Span
	Attempt        *TraceAttempt
	Usage          *TraceUsage
	Operation      *TraceOperationResult
}

// AppendHarnessEvent atomically validates the harness attempt identity and the
// exact next source sequence, then persists the event and its optional trace
// evidence. An exact replay (same source identity and payload) is a no-op. The
// same source identity with a different payload is a conflict.
func (s *Store) AppendHarnessEvent(ctx context.Context, record HarnessEventRecord) error {
	if s == nil || s.pool == nil {
		return errors.New("postgres store is not configured")
	}
	if err := validateHarnessEventRecord(record); err != nil {
		return err
	}
	hash, err := payloadHash(record.Event)
	if err != nil {
		return err
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin append harness event: %w", err)
	}
	defer tx.Rollback(ctx)

	var attemptRunID string
	var attemptStatus string
	if err := tx.QueryRow(ctx, `
		SELECT run_id, status
		FROM harness_attempts
		WHERE id = $1 FOR UPDATE`, record.AttemptID).Scan(&attemptRunID, &attemptStatus); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("harness attempt %q: %w", record.AttemptID, ErrNotFound)
		}
		return fmt.Errorf("lock harness attempt: %w", err)
	}
	if attemptRunID != record.RunID {
		return ErrConflict
	}

	var nextSource int64
	if err := tx.QueryRow(ctx, `
		SELECT COALESCE(MAX(source_sequence), 0) + 1
		FROM run_events
		WHERE harness_attempt_id = $1`, record.AttemptID).Scan(&nextSource); err != nil {
		return fmt.Errorf("next harness source sequence: %w", err)
	}

	var existingPayload []byte
	err = tx.QueryRow(ctx, `
		SELECT payload
		FROM run_events
		WHERE run_id = $1
		  AND harness_attempt_id = $2
		  AND source_sequence = $3`,
		record.RunID, record.AttemptID, record.SourceSequence,
	).Scan(&existingPayload)
	switch {
	case err == nil:
		var existing event.Envelope[json.RawMessage]
		if err := json.Unmarshal(existingPayload, &existing); err != nil {
			return fmt.Errorf("decode existing harness event: %w", err)
		}
		existingHash, err := payloadHash(existing)
		if err != nil {
			return err
		}
		if existingHash != hash {
			return ErrConflict
		}
		return tx.Commit(ctx)
	case !errors.Is(err, pgx.ErrNoRows):
		return fmt.Errorf("read existing harness event: %w", err)
	}

	if record.SourceSequence != nextSource {
		return ErrConflict
	}

	if record.Span != nil {
		// Carry the harness source sequence onto the span so lookups can order
		// spans deterministically by emission order instead of started_at.
		record.Span.SourceSequence = record.SourceSequence
		if _, err := upsertSpan(ctx, tx, *record.Span); err != nil {
			return err
		}
	}
	var attemptID string
	if record.Span != nil && record.Attempt != nil {
		attemptID, err = insertTraceAttempt(ctx, tx, record.Span.ID, *record.Attempt)
		if err != nil {
			return err
		}
	} else if record.Attempt != nil {
		// Usage-only records reference an already-persisted attempt (for
		// example, a pi.usage event that trails pi.message.completed). The
		// attempt id is taken as-is rather than re-inserted.
		attemptID = record.Attempt.ID
	}
	if record.Usage != nil {
		if err := insertTraceUsage(ctx, tx, record.RunID, attemptID, *record.Usage); err != nil {
			return err
		}
	}
	if record.Operation != nil {
		if err := insertTraceOperation(ctx, tx, record.RunID, attemptID, *record.Operation); err != nil {
			return err
		}
	}

	var runSeq int64
	if err := tx.QueryRow(ctx, `
		SELECT COALESCE(MAX(seq), 0) + 1
		FROM run_events
		WHERE run_id = $1`, record.RunID).Scan(&runSeq); err != nil {
		return fmt.Errorf("next run event sequence: %w", err)
	}
	eventPayload, err := json.Marshal(record.Event)
	if err != nil {
		return fmt.Errorf("marshal harness event: %w", err)
	}
	rowID, err := newID("rev")
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO run_events (
			id, run_id, seq, event_type, payload, occurred_at,
			harness_attempt_id, source_sequence, source_type
		) VALUES ($1, $2, $3, $4, $5::jsonb, $6, $7, $8, $9)`,
		rowID, record.RunID, runSeq, record.Event.EventType, eventPayload, record.Event.OccurredAt,
		record.AttemptID, record.SourceSequence, record.SourceType,
	); err != nil {
		return fmt.Errorf("insert harness event: %w", err)
	}
	inserted, err := insertOutbox(ctx, tx, record.Event.EventID, runEventsTopic, record.RunID, record.Event, hash)
	if err != nil {
		return err
	}
	if !inserted {
		storedHash, err := outboxPayloadHash(ctx, tx, record.Event.EventID)
		if err != nil {
			return err
		}
		if storedHash != hash {
			return ErrConflict
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit append harness event: %w", err)
	}
	return nil
}

// HarnessFinish records the terminal outcome of a harness attempt.
type HarnessFinish struct {
	RunID     string
	AttemptID string
	Status    domain.RunStatus
	Error     json.RawMessage
	Assistant string
	Event     event.Envelope[json.RawMessage]
}

// FinishHarnessAttempt atomically records terminal run state, finishes the
// harness attempt and root span, appends the assistant message, and emits the
// terminal durable event. Replaying the same terminal state is a no-op; a
// conflicting terminal state is an error.
func (s *Store) FinishHarnessAttempt(ctx context.Context, finish HarnessFinish) error {
	if s == nil || s.pool == nil {
		return errors.New("postgres store is not configured")
	}
	if strings.TrimSpace(finish.RunID) == "" || strings.TrimSpace(finish.AttemptID) == "" {
		return errors.New("harness finish run and attempt ids are required")
	}
	if finish.Status != domain.RunSucceeded &&
		finish.Status != domain.RunFailed &&
		finish.Status != domain.RunCancelled {
		return errors.New("harness finish status must be terminal")
	}
	if err := finish.Event.Validate(); err != nil {
		return fmt.Errorf("validate harness finish event: %w", err)
	}
	hash, err := payloadHash(finish.Event)
	if err != nil {
		return err
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin finish harness attempt: %w", err)
	}
	defer tx.Rollback(ctx)

	existingHash, err := outboxPayloadHash(ctx, tx, finish.Event.EventID)
	if err == nil {
		if existingHash != hash {
			return ErrConflict
		}
		return tx.Commit(ctx)
	}
	if !errors.Is(err, ErrNotFound) {
		return err
	}

	var conversationID string
	if err := tx.QueryRow(ctx, `
		SELECT conversation_id FROM runs WHERE id = $1`, finish.RunID).Scan(&conversationID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return fmt.Errorf("load harness finish conversation: %w", err)
	}
	if _, err := lockConversation(ctx, tx, conversationID); err != nil {
		return err
	}
	var existingStatus domain.RunStatus
	if err := tx.QueryRow(ctx, `
		SELECT status FROM runs WHERE id = $1 FOR UPDATE`, finish.RunID).Scan(&existingStatus); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return fmt.Errorf("lock run for harness finish: %w", err)
	}
	if isTerminalStatus(existingStatus) {
		if existingStatus != finish.Status {
			return ErrConflict
		}
		return tx.Commit(ctx)
	}

	if _, err := tx.Exec(ctx, `
		UPDATE runs
		SET status = $2, error = $3::jsonb, finished_at = now(), updated_at = now()
		WHERE id = $1`,
		finish.RunID, finish.Status, nullableJSON(finish.Error),
	); err != nil {
		return fmt.Errorf("finish harness run: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE spans
		SET status = $2, ended_at = now(), updated_at = now()
		WHERE run_id = $1
		  AND kind = 'run'
		  AND ended_at IS NULL`,
		finish.RunID, finish.Status); err != nil {
		return fmt.Errorf("finish harness trace root: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE harness_attempts
		SET status = $2, finished_at = now()
		WHERE id = $1`,
		finish.AttemptID, finish.Status); err != nil {
		return fmt.Errorf("finish harness attempt: %w", err)
	}
	if err := appendAssistantMessage(ctx, tx, conversationID, finish.RunID, finish.Assistant); err != nil {
		return err
	}
	var seq int64
	if err := tx.QueryRow(ctx, `
		SELECT COALESCE(MAX(seq), 0) + 1 FROM run_events WHERE run_id = $1`, finish.RunID).Scan(&seq); err != nil {
		return fmt.Errorf("next harness finish event sequence: %w", err)
	}
	payload, err := json.Marshal(finish.Event)
	if err != nil {
		return fmt.Errorf("marshal harness finish event: %w", err)
	}
	rowID, err := newID("rev")
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO run_events (id, run_id, seq, event_type, payload, occurred_at)
		VALUES ($1, $2, $3, $4, $5::jsonb, $6)`,
		rowID, finish.RunID, seq, finish.Event.EventType, payload, finish.Event.OccurredAt,
	); err != nil {
		return fmt.Errorf("insert harness finish event: %w", err)
	}
	inserted, err := insertOutbox(ctx, tx, finish.Event.EventID, runEventsTopic, finish.RunID, finish.Event, hash)
	if err != nil {
		return err
	}
	if !inserted {
		return ErrConflict
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit harness finish: %w", err)
	}
	return nil
}

func validateHarnessEventRecord(record HarnessEventRecord) error {
	switch {
	case strings.TrimSpace(record.RunID) == "":
		return errors.New("harness event run id is required")
	case strings.TrimSpace(record.AttemptID) == "":
		return errors.New("harness event attempt id is required")
	case record.SourceSequence < 1:
		return errors.New("harness event source sequence must be positive")
	case strings.TrimSpace(record.SourceType) == "":
		return errors.New("harness event source type is required")
	case strings.TrimSpace(record.Event.EventID) == "":
		return errors.New("harness event id is required")
	case record.Event.EventType == "":
		return errors.New("harness event type is required")
	case record.Event.AggregateID != record.RunID || record.Event.CorrelationID != record.RunID:
		return errors.New("harness event must belong to its run")
	case record.Event.OccurredAt.IsZero():
		return errors.New("harness event occurrence time is required")
	}
	if err := record.Event.Validate(); err != nil {
		return fmt.Errorf("validate harness event: %w", err)
	}
	if record.Span != nil {
		if record.Span.RunID != record.RunID {
			return errors.New("harness event span must belong to its run")
		}
	}
	if record.Usage != nil && record.Attempt == nil {
		return errors.New("harness usage requires an attempt")
	}
	return nil
}

func isTerminalStatus(status domain.RunStatus) bool {
	return status == domain.RunSucceeded ||
		status == domain.RunFailed ||
		status == domain.RunCancelled
}

// LatestModelSpanID returns the span_id of the most recent model span for a
// run, or an empty string if none exists. The runtime uses it to attach a
// trailing pi.usage record to the matching model attempt. Spans are ordered by
// their harness source sequence (not started_at) so clock skew or fast
// consecutive events cannot mis-attach usage.
func (s *Store) LatestModelSpanID(ctx context.Context, runID string) (string, error) {
	if s == nil || s.pool == nil {
		return "", errors.New("postgres store is not configured")
	}
	var spanID string
	err := s.pool.QueryRow(ctx, `
		SELECT span_id FROM spans
		WHERE run_id = $1 AND kind = 'model'
		ORDER BY source_sequence DESC
		LIMIT 1`, runID).Scan(&spanID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("lookup latest model span: %w", err)
	}
	return spanID, nil
}

// ActiveToolSpanID returns the span_id of the most recent still-started
// operation span for a run, or an empty string if none is active. The runtime
// uses it to attach browser observations and the final case verdict to the
// matching tool operation. Spans are ordered by their harness source sequence
// (not started_at) for deterministic selection under concurrency.
func (s *Store) ActiveToolSpanID(ctx context.Context, runID string) (string, error) {
	if s == nil || s.pool == nil {
		return "", errors.New("postgres store is not configured")
	}
	var spanID string
	err := s.pool.QueryRow(ctx, `
		SELECT span_id FROM spans
		WHERE run_id = $1 AND kind = 'operation' AND status = 'started'
		ORDER BY source_sequence DESC
		LIMIT 1`, runID).Scan(&spanID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("lookup active tool span: %w", err)
	}
	return spanID, nil
}
