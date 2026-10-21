package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/domain"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/event"
	"github.com/jackc/pgx/v5"
)

// ConsumerRetryIncrement is the outcome of recording a failed consumer delivery.
type ConsumerRetryIncrement struct {
	Attempts     int
	DLQHandedOff bool
}

// LoadRunCommand returns the immutable command stored with a run.
func (s *Store) LoadRunCommand(ctx context.Context, runID string) (domain.Run, event.RunCommand, error) {
	if s == nil || s.pool == nil {
		return domain.Run{}, event.RunCommand{}, errors.New("postgres store is not configured")
	}
	run, err := scanRun(s.pool.QueryRow(ctx, `
		SELECT id, project_id, conversation_id, status, specification, checkpoint_version,
		       cancel_requested_at, started_at, finished_at, created_at, updated_at
		FROM runs WHERE id = $1`, runID))
	if err != nil {
		return domain.Run{}, event.RunCommand{}, fmt.Errorf("load runtime run: %w", err)
	}
	var specification runSpecification
	if err := json.Unmarshal(run.Specification, &specification); err != nil {
		return domain.Run{}, event.RunCommand{}, fmt.Errorf("decode run specification: %w", err)
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, conversation_id, COALESCE(run_id, ''), ordinal, role, content, metadata, created_at
		FROM messages
		WHERE conversation_id = $1
		  AND metadata ->> 'run_id' = $2
		ORDER BY ordinal ASC`,
		run.ConversationID, runID,
	)
	if err != nil {
		return domain.Run{}, event.RunCommand{}, fmt.Errorf("load persisted run responses: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		message, err := scanMessage(rows)
		if err != nil {
			return domain.Run{}, event.RunCommand{}, err
		}
		specification.Command.Messages = append(specification.Command.Messages, message)
	}
	if err := rows.Err(); err != nil {
		return domain.Run{}, event.RunCommand{}, fmt.Errorf("iterate persisted run responses: %w", err)
	}
	return run, specification.Command, nil
}

// ListRunEvents returns the durable event stream in replay order.
func (s *Store) ListRunEvents(ctx context.Context, runID string) ([]event.Envelope[json.RawMessage], error) {
	if s == nil || s.pool == nil {
		return nil, errors.New("postgres store is not configured")
	}
	rows, err := s.pool.Query(ctx, `
		SELECT payload FROM run_events
		WHERE run_id = $1
		ORDER BY seq ASC`, runID)
	if err != nil {
		return nil, fmt.Errorf("list runtime events: %w", err)
	}
	defer rows.Close()
	var events []event.Envelope[json.RawMessage]
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return nil, fmt.Errorf("scan runtime event: %w", err)
		}
		var envelope event.Envelope[json.RawMessage]
		if err := json.Unmarshal(raw, &envelope); err != nil {
			return nil, fmt.Errorf("decode runtime event: %w", err)
		}
		events = append(events, envelope)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate runtime events: %w", err)
	}
	return events, nil
}

// IncrementConsumerRetry atomically counts an unprocessed delivery failure.
func (s *Store) IncrementConsumerRetry(
	ctx context.Context,
	consumerName string,
	eventID string,
	payload []byte,
) (int, error) {
	if s == nil || s.pool == nil {
		return 0, errors.New("postgres store is not configured")
	}
	if strings.TrimSpace(consumerName) == "" || strings.TrimSpace(eventID) == "" {
		return 0, errors.New("consumer retry consumer and event id are required")
	}
	storedPayload := payload
	if !json.Valid(storedPayload) {
		var err error
		storedPayload, err = json.Marshal(map[string]string{"raw": string(payload)})
		if err != nil {
			return 0, fmt.Errorf("marshal malformed consumer payload: %w", err)
		}
	}
	var attempts int
	err := s.pool.QueryRow(ctx, `
		INSERT INTO consumer_retry_counts (consumer, event_id, payload, attempt_count)
		VALUES ($1, $2, $3::jsonb, 1)
		ON CONFLICT (consumer, event_id) DO UPDATE SET
			attempt_count = consumer_retry_counts.attempt_count + 1,
			updated_at = now()
		RETURNING attempt_count`,
		consumerName, eventID, storedPayload,
	).Scan(&attempts)
	if err != nil {
		return 0, fmt.Errorf("increment consumer retry: %w", err)
	}
	return attempts, nil
}

// IncrementConsumerRetryUnlessDLQHandoff atomically records a failure only
// while a matching DLQ handoff does not exist. The transaction-scoped lock is
// shared with HandoffConsumerRetryToOutbox, so a late delivery cannot recreate
// retry state after the handoff removes it.
func (s *Store) IncrementConsumerRetryUnlessDLQHandoff(
	ctx context.Context,
	consumerName string,
	eventID string,
	outboxEventID string,
	payload []byte,
) (ConsumerRetryIncrement, error) {
	if s == nil || s.pool == nil {
		return ConsumerRetryIncrement{}, errors.New("postgres store is not configured")
	}
	if strings.TrimSpace(consumerName) == "" ||
		strings.TrimSpace(eventID) == "" ||
		strings.TrimSpace(outboxEventID) == "" {
		return ConsumerRetryIncrement{}, errors.New("consumer retry identifiers are required")
	}
	storedPayload, err := consumerRetryPayload(payload)
	if err != nil {
		return ConsumerRetryIncrement{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return ConsumerRetryIncrement{}, fmt.Errorf("begin consumer retry increment: %w", err)
	}
	defer tx.Rollback(ctx)
	if err := lockConsumerRetry(ctx, tx, consumerName, eventID); err != nil {
		return ConsumerRetryIncrement{}, err
	}

	var handedOff bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM outbox_events WHERE event_id = $1
		)`, outboxEventID).Scan(&handedOff); err != nil {
		return ConsumerRetryIncrement{}, fmt.Errorf("check consumer retry DLQ handoff: %w", err)
	}
	if handedOff {
		if err := tx.Commit(ctx); err != nil {
			return ConsumerRetryIncrement{}, fmt.Errorf("commit consumer retry handoff check: %w", err)
		}
		return ConsumerRetryIncrement{DLQHandedOff: true}, nil
	}

	var attempts int
	if err := tx.QueryRow(ctx, `
		INSERT INTO consumer_retry_counts (consumer, event_id, payload, attempt_count)
		VALUES ($1, $2, $3::jsonb, 1)
		ON CONFLICT (consumer, event_id) DO UPDATE SET
			attempt_count = consumer_retry_counts.attempt_count + 1,
			updated_at = now()
		RETURNING attempt_count`,
		consumerName, eventID, storedPayload,
	).Scan(&attempts); err != nil {
		return ConsumerRetryIncrement{}, fmt.Errorf("increment consumer retry: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return ConsumerRetryIncrement{}, fmt.Errorf("commit consumer retry increment: %w", err)
	}
	return ConsumerRetryIncrement{Attempts: attempts}, nil
}

func consumerRetryPayload(payload []byte) ([]byte, error) {
	if json.Valid(payload) {
		return payload, nil
	}
	storedPayload, err := json.Marshal(map[string]string{"raw": string(payload)})
	if err != nil {
		return nil, fmt.Errorf("marshal malformed consumer payload: %w", err)
	}
	return storedPayload, nil
}

func lockConsumerRetry(ctx context.Context, tx pgx.Tx, consumerName string, eventID string) error {
	if _, err := tx.Exec(ctx, `
		SELECT pg_advisory_xact_lock(
			hashtextextended($1 || ':' || $2, 0::bigint)
		)`, consumerName, eventID); err != nil {
		return fmt.Errorf("lock consumer retry transition: %w", err)
	}
	return nil
}

// ClearConsumerRetry removes failure state after a successful delivery or DLQ handoff.
func (s *Store) ClearConsumerRetry(ctx context.Context, consumerName string, eventID string) error {
	if s == nil || s.pool == nil {
		return errors.New("postgres store is not configured")
	}
	if strings.TrimSpace(consumerName) == "" || strings.TrimSpace(eventID) == "" {
		return errors.New("consumer retry consumer and event id are required")
	}
	if _, err := s.pool.Exec(ctx, `
		DELETE FROM consumer_retry_counts
		WHERE consumer = $1 AND event_id = $2`, consumerName, eventID); err != nil {
		return fmt.Errorf("clear consumer retry: %w", err)
	}
	return nil
}

// HandoffConsumerRetryToOutbox atomically replaces retry state with one
// idempotent DLQ outbox record. The relay owns broker publication afterward.
func (s *Store) HandoffConsumerRetryToOutbox(
	ctx context.Context,
	consumerName string,
	retryEventID string,
	outboxEventID string,
	topic string,
	key string,
	payload []byte,
) error {
	if s == nil || s.pool == nil {
		return errors.New("postgres store is not configured")
	}
	if strings.TrimSpace(consumerName) == "" ||
		strings.TrimSpace(retryEventID) == "" ||
		strings.TrimSpace(outboxEventID) == "" ||
		strings.TrimSpace(topic) == "" {
		return errors.New("consumer retry handoff identifiers are required")
	}
	if !json.Valid(payload) {
		return errors.New("consumer retry DLQ payload must be valid JSON")
	}
	hash, err := payloadHash(json.RawMessage(payload))
	if err != nil {
		return fmt.Errorf("hash consumer retry DLQ payload: %w", err)
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin consumer retry DLQ handoff: %w", err)
	}
	defer tx.Rollback(ctx)
	if err := lockConsumerRetry(ctx, tx, consumerName, retryEventID); err != nil {
		return err
	}

	var present bool
	err = tx.QueryRow(ctx, `
		SELECT true
		FROM consumer_retry_counts
		WHERE consumer = $1 AND event_id = $2
		FOR UPDATE`, consumerName, retryEventID).Scan(&present)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("lock consumer retry for DLQ handoff: %w", err)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		present = false
	}

	var storedHash, storedTopic, storedKey string
	err = tx.QueryRow(ctx, `
		SELECT headers ->> 'payload_hash', topic, key
		FROM outbox_events
		WHERE event_id = $1
		FOR UPDATE`, outboxEventID).Scan(&storedHash, &storedTopic, &storedKey)
	switch {
	case err == nil:
		if storedHash != hash || storedTopic != topic || storedKey != key {
			return ErrConflict
		}
	case errors.Is(err, pgx.ErrNoRows):
		if !present {
			return tx.Commit(ctx)
		}
		inserted, err := insertOutbox(
			ctx,
			tx,
			outboxEventID,
			topic,
			key,
			json.RawMessage(payload),
			hash,
		)
		if err != nil {
			return err
		}
		if !inserted {
			return ErrConflict
		}
	default:
		return fmt.Errorf("read consumer retry DLQ outbox event: %w", err)
	}
	if present {
		if _, err := tx.Exec(ctx, `
			DELETE FROM consumer_retry_counts
			WHERE consumer = $1 AND event_id = $2`, consumerName, retryEventID); err != nil {
			return fmt.Errorf("clear consumer retry after DLQ handoff: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit consumer retry DLQ handoff: %w", err)
	}
	return nil
}

// HasConsumerRetryDLQHandoff reports whether a retry event already has a
// durable DLQ outbox handoff, so a broker replay can be acknowledged.
func (s *Store) HasConsumerRetryDLQHandoff(ctx context.Context, outboxEventID string) (bool, error) {
	if s == nil || s.pool == nil {
		return false, errors.New("postgres store is not configured")
	}
	if strings.TrimSpace(outboxEventID) == "" {
		return false, errors.New("consumer retry DLQ outbox event id is required")
	}
	var handedOff bool
	if err := s.pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM outbox_events WHERE event_id = $1
		)`, outboxEventID).Scan(&handedOff); err != nil {
		return false, fmt.Errorf("check consumer retry DLQ handoff: %w", err)
	}
	return handedOff, nil
}

// FinishRun atomically records terminal state, a durable event, and its outbox row.
func (s *Store) FinishRun(
	ctx context.Context,
	runID string,
	status domain.RunStatus,
	runError json.RawMessage,
	envelope event.Envelope[json.RawMessage],
) error {
	if s == nil || s.pool == nil {
		return errors.New("postgres store is not configured")
	}
	if err := envelope.Validate(); err != nil {
		return fmt.Errorf("validate completion event: %w", err)
	}
	hash, err := payloadHash(envelope)
	if err != nil {
		return err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin complete run: %w", err)
	}
	defer tx.Rollback(ctx)

	existingHash, err := outboxPayloadHash(ctx, tx, envelope.EventID)
	if err == nil {
		if existingHash != hash {
			return ErrConflict
		}
		return tx.Commit(ctx)
	}
	if !errors.Is(err, ErrNotFound) {
		return err
	}

	tag, err := tx.Exec(ctx, `
		UPDATE runs
		SET status = $2, error = $3::jsonb, finished_at = now(), updated_at = now()
		WHERE id = $1
		  AND status NOT IN ('succeeded', 'failed', 'cancelled')`,
		runID, status, nullableJSON(runError))
	if err != nil {
		return fmt.Errorf("finish run: %w", err)
	}
	if tag.RowsAffected() == 0 {
		var existing domain.RunStatus
		if err := tx.QueryRow(ctx, `SELECT status FROM runs WHERE id = $1`, runID).Scan(&existing); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrNotFound
			}
			return fmt.Errorf("read completed run: %w", err)
		}
		if existing != status {
			return ErrConflict
		}
		return tx.Commit(ctx)
	}
	var seq int64
	if err := tx.QueryRow(ctx, `
		SELECT COALESCE(MAX(seq), 0) + 1 FROM run_events WHERE run_id = $1`, runID).Scan(&seq); err != nil {
		return fmt.Errorf("next completion event sequence: %w", err)
	}
	payload, err := json.Marshal(envelope)
	if err != nil {
		return fmt.Errorf("marshal completion event: %w", err)
	}
	rowID, err := newID("rev")
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO run_events (id, run_id, seq, event_type, payload, occurred_at)
		VALUES ($1, $2, $3, $4, $5::jsonb, $6)`,
		rowID, runID, seq, envelope.EventType, payload, envelope.OccurredAt,
	); err != nil {
		return fmt.Errorf("insert completion event: %w", err)
	}
	inserted, err := insertOutbox(ctx, tx, envelope.EventID, runEventsTopic, runID, envelope, hash)
	if err != nil {
		return err
	}
	if !inserted {
		return ErrConflict
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit completion: %w", err)
	}
	return nil
}
