package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/domain"
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

// TraceAttemptStart records an external call before it begins.
type TraceAttemptStart struct {
	ExpectedCheckpoint int64
	Root               *domain.Span
	Span               domain.Span
	Attempt            TraceAttempt
}

func validateTraceAttemptStart(started TraceAttemptStart) error {
	span := started.Span
	attempt := started.Attempt
	if started.Root != nil {
		root := *started.Root
		switch {
		case root.ID == "" || root.RunID != span.RunID || root.TraceID != span.TraceID:
			return errors.New("trace root must belong to the started span")
		case root.SpanID == "" || root.Kind != domain.SpanRun || root.Name == "" || root.Status == "":
			return errors.New("trace root span is invalid")
		case root.ParentSpanID != "" || root.StartedAt.IsZero() || root.EndedAt != nil:
			return errors.New("trace root must be unfinished and parentless")
		}
	}
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
			input, output, started_at, ended_at, source_sequence
		) VALUES (
			$1, $2, $3, $4, NULLIF($5, ''), $6, $7, $8,
			$9::jsonb, $10::jsonb, $11, $12, NULLIF($13, 0)
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
			source_sequence = COALESCE(EXCLUDED.source_sequence, spans.source_sequence),
			updated_at = now()
		RETURNING id`,
		span.ID, span.RunID, span.TraceID, span.SpanID, span.ParentSpanID,
		span.Kind, span.Name, span.Status, nullableJSON(span.Input), nullableJSON(span.Output),
		span.StartedAt, span.EndedAt, span.SourceSequence,
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
