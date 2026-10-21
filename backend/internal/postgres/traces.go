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
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/usage"
	"github.com/jackc/pgx/v5"
)

// TraceAttempt is the persisted evidence for one model or operation attempt.
type TraceAttempt struct {
	ID            string
	AttemptNumber int
	Provider      string
	OperationID   string
	Status        string
	Request       json.RawMessage
	Response      json.RawMessage
	Error         json.RawMessage
	StartedAt     time.Time
	EndedAt       *time.Time
}

// TraceUsage is token and cost accounting attributable to a trace attempt.
type TraceUsage struct {
	ID       string
	Provider string
	Model    string
	Usage    usage.Usage
	Details  json.RawMessage
}

// TraceOperationResult is the durable, idempotent response from an operation callback.
type TraceOperationResult struct {
	ID          string
	OperationID string
	Status      string
	Result      json.RawMessage
	Artifacts   json.RawMessage
	Error       json.RawMessage
}

// TraceStep is a complete checkpointed durable Trace update.
type TraceStep struct {
	ExpectedCheckpoint int64
	Span               domain.Span
	Attempt            *TraceAttempt
	Usage              *TraceUsage
	Operation          *TraceOperationResult
	Event              event.Envelope[json.RawMessage]
}

// TraceAttemptStart records an external call before it begins.
type TraceAttemptStart struct {
	ExpectedCheckpoint int64
	Span               domain.Span
	Attempt            TraceAttempt
}

// StartTraceAttempt persists a started trace attempt without advancing the
// completed-step checkpoint. Replays with the same deterministic identities are
// no-ops, so a restart can safely issue the external call again.
func (s *Store) StartTraceAttempt(ctx context.Context, started TraceAttemptStart) error {
	if s == nil || s.pool == nil {
		return errors.New("postgres store is not configured")
	}
	if err := validateTraceAttemptStart(started); err != nil {
		return err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin start trace attempt: %w", err)
	}
	defer tx.Rollback(ctx)

	var checkpoint int64
	err = tx.QueryRow(ctx, `SELECT checkpoint_version FROM runs WHERE id = $1 FOR UPDATE`, started.Span.RunID).Scan(&checkpoint)
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("run %q: %w", started.Span.RunID, ErrNotFound)
	}
	if err != nil {
		return fmt.Errorf("lock run %q: %w", started.Span.RunID, err)
	}
	if checkpoint != started.ExpectedCheckpoint {
		return ErrConflict
	}
	spanID, err := insertStartedSpan(ctx, tx, started.Span)
	if err != nil {
		return err
	}
	if err := insertStartedTraceAttempt(ctx, tx, spanID, started.Attempt); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit start trace attempt: %w", err)
	}
	return nil
}

// AppendTraceStep atomically persists a Trace update, durable event, checkpoint, and outbox row.
func (s *Store) AppendTraceStep(ctx context.Context, step TraceStep) error {
	if s == nil || s.pool == nil {
		return errors.New("postgres store is not configured")
	}
	if err := validateTraceStep(step); err != nil {
		return err
	}
	hash, err := payloadHash(step)
	if err != nil {
		return err
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin append trace step: %w", err)
	}
	defer tx.Rollback(ctx)

	existingHash, err := outboxPayloadHash(ctx, tx, step.Event.EventID)
	if err == nil {
		if existingHash != hash {
			return ErrConflict
		}
		return tx.Commit(ctx)
	}
	if !errors.Is(err, ErrNotFound) {
		return err
	}

	var checkpoint int64
	err = tx.QueryRow(ctx, `SELECT checkpoint_version FROM runs WHERE id = $1 FOR UPDATE`, step.Span.RunID).Scan(&checkpoint)
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("run %q: %w", step.Span.RunID, ErrNotFound)
	}
	if err != nil {
		return fmt.Errorf("lock run %q: %w", step.Span.RunID, err)
	}
	if checkpoint != step.ExpectedCheckpoint {
		existingHash, replayErr := outboxPayloadHash(ctx, tx, step.Event.EventID)
		if replayErr == nil && existingHash == hash {
			return tx.Commit(ctx)
		}
		if replayErr != nil && !errors.Is(replayErr, ErrNotFound) {
			return replayErr
		}
		return ErrConflict
	}

	spanID, err := upsertSpan(ctx, tx, step.Span)
	if err != nil {
		return err
	}
	var attemptID string
	if step.Attempt != nil {
		attemptID, err = insertTraceAttempt(ctx, tx, spanID, *step.Attempt)
		if err != nil {
			return err
		}
	}
	if step.Usage != nil {
		if err := insertTraceUsage(ctx, tx, step.Span.RunID, attemptID, *step.Usage); err != nil {
			return err
		}
	}
	if step.Operation != nil {
		if err := insertTraceOperation(ctx, tx, step.Span.RunID, attemptID, *step.Operation); err != nil {
			return err
		}
	}

	var seq int64
	if err := tx.QueryRow(ctx, `
		SELECT COALESCE(MAX(seq), 0) + 1
		FROM run_events
		WHERE run_id = $1`, step.Span.RunID).Scan(&seq); err != nil {
		return fmt.Errorf("next run event sequence: %w", err)
	}
	eventPayload, err := json.Marshal(step.Event)
	if err != nil {
		return fmt.Errorf("marshal durable event: %w", err)
	}
	eventRowID, err := newID("rev")
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO run_events (id, run_id, seq, event_type, payload, occurred_at)
		VALUES ($1, $2, $3, $4, $5::jsonb, $6)`,
		eventRowID, step.Span.RunID, seq, step.Event.EventType, eventPayload, step.Event.OccurredAt,
	); err != nil {
		return fmt.Errorf("insert durable run event: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE runs
		SET checkpoint_version = checkpoint_version + 1, updated_at = now()
		WHERE id = $1 AND checkpoint_version = $2`,
		step.Span.RunID, step.ExpectedCheckpoint,
	); err != nil {
		return fmt.Errorf("advance run checkpoint: %w", err)
	}
	inserted, err := insertOutbox(ctx, tx, step.Event.EventID, runEventsTopic, step.Span.RunID, step.Event, hash)
	if err != nil {
		return err
	}
	if !inserted {
		storedHash, err := outboxPayloadHash(ctx, tx, step.Event.EventID)
		if err != nil {
			return err
		}
		if storedHash != hash {
			return ErrConflict
		}
		return nil
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit trace step: %w", err)
	}
	return nil
}

func validateTraceStep(step TraceStep) error {
	switch {
	case strings.TrimSpace(step.Span.ID) == "":
		return errors.New("trace span id is required")
	case strings.TrimSpace(step.Span.RunID) == "":
		return errors.New("trace span run id is required")
	case strings.TrimSpace(step.Span.TraceID) == "" || strings.TrimSpace(step.Span.SpanID) == "":
		return errors.New("trace and span IDs are required")
	case strings.TrimSpace(string(step.Span.Kind)) == "":
		return errors.New("trace span kind is required")
	case strings.TrimSpace(step.Span.Name) == "" || strings.TrimSpace(step.Span.Status) == "":
		return errors.New("trace span name and status are required")
	case step.Span.StartedAt.IsZero():
		return errors.New("trace span start time is required")
	case strings.TrimSpace(step.Event.EventID) == "" || strings.TrimSpace(step.Event.EventType) == "":
		return errors.New("trace event id and type are required")
	case step.Event.AggregateID != step.Span.RunID || step.Event.CorrelationID != step.Span.RunID:
		return errors.New("trace event must belong to its run")
	case step.Event.OccurredAt.IsZero():
		return errors.New("trace event occurrence time is required")
	}
	if err := step.Event.Validate(); err != nil {
		return fmt.Errorf("validate trace event: %w", err)
	}
	if step.Attempt != nil {
		if step.Attempt.ID == "" || step.Attempt.AttemptNumber < 1 || step.Attempt.Status == "" || step.Attempt.StartedAt.IsZero() {
			return errors.New("trace attempt id, number, status, and start time are required")
		}
	}
	if step.Usage != nil {
		if step.Usage.ID == "" {
			return errors.New("trace usage id is required")
		}
		if step.Attempt == nil {
			return errors.New("trace usage requires an attempt")
		}
	}
	if step.Operation != nil {
		if step.Operation.ID == "" || step.Operation.OperationID == "" || step.Operation.Status == "" {
			return errors.New("trace operation id, operation id, and status are required")
		}
	}
	return nil
}

func validateTraceAttemptStart(started TraceAttemptStart) error {
	span := started.Span
	attempt := started.Attempt
	switch {
	case strings.TrimSpace(span.ID) == "":
		return errors.New("started trace span id is required")
	case strings.TrimSpace(span.RunID) == "":
		return errors.New("started trace span run id is required")
	case strings.TrimSpace(span.TraceID) == "" || strings.TrimSpace(span.SpanID) == "":
		return errors.New("started trace and span IDs are required")
	case strings.TrimSpace(string(span.Kind)) == "":
		return errors.New("started trace span kind is required")
	case strings.TrimSpace(span.Name) == "" || span.Status != "started":
		return errors.New("started trace span name and status are required")
	case span.StartedAt.IsZero() || span.EndedAt != nil:
		return errors.New("started trace span must have only a start time")
	case attempt.ID == "" || attempt.AttemptNumber < 1 || attempt.Status != "started":
		return errors.New("started trace attempt id, number, and status are required")
	case attempt.StartedAt.IsZero() || attempt.EndedAt != nil:
		return errors.New("started trace attempt must have only a start time")
	}
	return nil
}

func insertStartedSpan(ctx context.Context, tx pgx.Tx, span domain.Span) (string, error) {
	if _, err := tx.Exec(ctx, `
		INSERT INTO spans (
			id, run_id, trace_id, span_id, parent_span_id, kind, name, status,
			input, output, started_at, ended_at
		) VALUES (
			$1, $2, $3, $4, NULLIF($5, ''), $6, $7, $8,
			$9::jsonb, $10::jsonb, $11, $12
		)
		ON CONFLICT (run_id, trace_id, span_id) DO NOTHING`,
		span.ID, span.RunID, span.TraceID, span.SpanID, span.ParentSpanID,
		span.Kind, span.Name, span.Status, nullableJSON(span.Input), nullableJSON(span.Output),
		span.StartedAt, span.EndedAt,
	); err != nil {
		return "", fmt.Errorf("insert started trace span: %w", err)
	}
	var id string
	if err := tx.QueryRow(ctx, `
		SELECT id FROM spans
		WHERE run_id = $1 AND trace_id = $2 AND span_id = $3`,
		span.RunID, span.TraceID, span.SpanID,
	).Scan(&id); err != nil {
		return "", fmt.Errorf("load started trace span: %w", err)
	}
	return id, nil
}

func insertStartedTraceAttempt(ctx context.Context, tx pgx.Tx, spanID string, attempt TraceAttempt) error {
	if _, err := tx.Exec(ctx, `
		INSERT INTO span_attempts (
			id, span_id, attempt_number, provider, operation_id, status,
			request, response, error, started_at, ended_at
		) VALUES (
			$1, $2, $3, NULLIF($4, ''), NULLIF($5, ''), $6,
			$7::jsonb, $8::jsonb, $9::jsonb, $10, $11
		)
		ON CONFLICT (span_id, attempt_number) DO NOTHING`,
		attempt.ID, spanID, attempt.AttemptNumber, attempt.Provider, attempt.OperationID, attempt.Status,
		nullableJSON(attempt.Request), nullableJSON(attempt.Response), nullableJSON(attempt.Error),
		attempt.StartedAt, attempt.EndedAt,
	); err != nil {
		return fmt.Errorf("insert started trace attempt: %w", err)
	}
	return nil
}

func upsertSpan(ctx context.Context, tx pgx.Tx, span domain.Span) (string, error) {
	var persistedID string
	err := tx.QueryRow(ctx, `
		INSERT INTO spans (
			id, run_id, trace_id, span_id, parent_span_id, kind, name, status,
			input, output, started_at, ended_at
		) VALUES (
			$1, $2, $3, $4, NULLIF($5, ''), $6, $7, $8,
			$9::jsonb, $10::jsonb, $11, $12
		)
		ON CONFLICT (run_id, trace_id, span_id) DO UPDATE SET
			parent_span_id = EXCLUDED.parent_span_id,
			kind = EXCLUDED.kind,
			name = EXCLUDED.name,
			status = EXCLUDED.status,
			input = EXCLUDED.input,
			output = EXCLUDED.output,
			started_at = spans.started_at,
			ended_at = EXCLUDED.ended_at,
			updated_at = now()
		RETURNING id`,
		span.ID, span.RunID, span.TraceID, span.SpanID, span.ParentSpanID,
		span.Kind, span.Name, span.Status, nullableJSON(span.Input), nullableJSON(span.Output),
		span.StartedAt, span.EndedAt,
	).Scan(&persistedID)
	if err != nil {
		return "", fmt.Errorf("upsert trace span: %w", err)
	}
	return persistedID, nil
}

func insertTraceAttempt(ctx context.Context, tx pgx.Tx, spanID string, attempt TraceAttempt) (string, error) {
	var persistedID string
	err := tx.QueryRow(ctx, `
		INSERT INTO span_attempts (
			id, span_id, attempt_number, provider, operation_id, status,
			request, response, error, started_at, ended_at
		) VALUES (
			$1, $2, $3, NULLIF($4, ''), NULLIF($5, ''), $6,
			$7::jsonb, $8::jsonb, $9::jsonb, $10, $11
		) ON CONFLICT (span_id, attempt_number) DO UPDATE SET
			status = EXCLUDED.status,
			response = EXCLUDED.response,
			error = EXCLUDED.error,
			ended_at = EXCLUDED.ended_at
		WHERE span_attempts.id = EXCLUDED.id
		  AND span_attempts.provider IS NOT DISTINCT FROM EXCLUDED.provider
		  AND span_attempts.operation_id IS NOT DISTINCT FROM EXCLUDED.operation_id
		  AND span_attempts.request IS NOT DISTINCT FROM EXCLUDED.request
		  AND (
			(span_attempts.status = 'started' AND span_attempts.ended_at IS NULL)
			OR (
				span_attempts.status = EXCLUDED.status
				AND span_attempts.response IS NOT DISTINCT FROM EXCLUDED.response
				AND span_attempts.error IS NOT DISTINCT FROM EXCLUDED.error
				AND span_attempts.ended_at IS NOT DISTINCT FROM EXCLUDED.ended_at
			)
		  )
		RETURNING id`,
		attempt.ID, spanID, attempt.AttemptNumber, attempt.Provider, attempt.OperationID, attempt.Status,
		nullableJSON(attempt.Request), nullableJSON(attempt.Response), nullableJSON(attempt.Error),
		attempt.StartedAt, attempt.EndedAt,
	).Scan(&persistedID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrConflict
	}
	if err != nil {
		return "", fmt.Errorf("insert trace attempt: %w", err)
	}
	return persistedID, nil
}

func insertTraceUsage(ctx context.Context, tx pgx.Tx, runID, attemptID string, record TraceUsage) error {
	value := record.Usage.Normalize()
	tag, err := tx.Exec(ctx, `
		INSERT INTO usage_records (
			id, run_id, span_attempt_id, provider, model,
			input_tokens, output_tokens, total_tokens, cost_micros, details
		) VALUES (
			$1, $2, NULLIF($3, ''), NULLIF($4, ''), NULLIF($5, ''),
			$6, $7, $8, $9, $10::jsonb
		)
		ON CONFLICT (id) DO UPDATE SET id = usage_records.id
		WHERE usage_records.run_id = EXCLUDED.run_id
		  AND usage_records.span_attempt_id IS NOT DISTINCT FROM EXCLUDED.span_attempt_id
		  AND usage_records.provider IS NOT DISTINCT FROM EXCLUDED.provider
		  AND usage_records.model IS NOT DISTINCT FROM EXCLUDED.model
		  AND usage_records.input_tokens = EXCLUDED.input_tokens
		  AND usage_records.output_tokens = EXCLUDED.output_tokens
		  AND usage_records.total_tokens = EXCLUDED.total_tokens
		  AND usage_records.cost_micros = EXCLUDED.cost_micros
		  AND usage_records.details = EXCLUDED.details`,
		record.ID, runID, attemptID, record.Provider, record.Model,
		value.PromptTokens, value.CompletionTokens, value.TotalTokens, value.CostMicros,
		requiredJSON(record.Details, `{}`),
	)
	if err != nil {
		return fmt.Errorf("insert trace usage: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrConflict
	}
	return nil
}

func insertTraceOperation(
	ctx context.Context,
	tx pgx.Tx,
	runID, attemptID string,
	result TraceOperationResult,
) error {
	tag, err := tx.Exec(ctx, `
		INSERT INTO operation_results (
			id, run_id, span_attempt_id, operation_id, status, result, artifacts, error
		) VALUES (
			$1, $2, NULLIF($3, ''), $4, $5, $6::jsonb, $7::jsonb, $8::jsonb
		)
		ON CONFLICT (operation_id) DO UPDATE SET id = operation_results.id
		WHERE operation_results.run_id = EXCLUDED.run_id
		  AND operation_results.span_attempt_id IS NOT DISTINCT FROM EXCLUDED.span_attempt_id
		  AND operation_results.status = EXCLUDED.status
		  AND operation_results.result IS NOT DISTINCT FROM EXCLUDED.result
		  AND operation_results.artifacts = EXCLUDED.artifacts
		  AND operation_results.error IS NOT DISTINCT FROM EXCLUDED.error`,
		result.ID, runID, attemptID, result.OperationID, result.Status,
		nullableJSON(result.Result), requiredJSON(result.Artifacts, `[]`), nullableJSON(result.Error),
	)
	if err != nil {
		return fmt.Errorf("insert operation result: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrConflict
	}
	return nil
}
