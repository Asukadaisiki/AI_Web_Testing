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

// CreateMessageParams is the validated API request persisted as a user message and queued run.
type CreateMessageParams struct {
	ConversationID string
	Content        string
	AgentID        string
	Model          event.ModelRef
	IdempotencyKey string
}

type createMessageResponse struct {
	MessageID string `json:"message_id"`
	RunID     string `json:"run_id"`
}

type runSpecification struct {
	AgentID string           `json:"agent_id"`
	Command event.RunCommand `json:"command"`
}

// CreateMessageAndRun locks an existing conversation and atomically writes the message, run,
// idempotency response, and Kafka outbox command.
func (s *Store) CreateMessageAndRun(
	ctx context.Context, p CreateMessageParams,
) (domain.Message, domain.Run, error) {
	if s == nil || s.pool == nil {
		return domain.Message{}, domain.Run{}, errors.New("postgres store is not configured")
	}
	if err := validateCreateMessageParams(p); err != nil {
		return domain.Message{}, domain.Run{}, err
	}
	requestHash, err := payloadHash(struct {
		ConversationID string         `json:"conversation_id"`
		Content        string         `json:"content"`
		AgentID        string         `json:"agent_id"`
		Model          event.ModelRef `json:"model"`
	}{
		ConversationID: p.ConversationID,
		Content:        p.Content,
		AgentID:        p.AgentID,
		Model:          p.Model,
	})
	if err != nil {
		return domain.Message{}, domain.Run{}, err
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.Message{}, domain.Run{}, fmt.Errorf("begin create message and run: %w", err)
	}
	defer tx.Rollback(ctx)

	projectID, err := lockConversation(ctx, tx, p.ConversationID)
	if err != nil {
		return domain.Message{}, domain.Run{}, err
	}
	messageID, err := newID("msg")
	if err != nil {
		return domain.Message{}, domain.Run{}, err
	}
	runID, err := newID("run")
	if err != nil {
		return domain.Message{}, domain.Run{}, err
	}
	response := createMessageResponse{MessageID: messageID, RunID: runID}
	responseJSON, err := json.Marshal(response)
	if err != nil {
		return domain.Message{}, domain.Run{}, fmt.Errorf("marshal idempotency response: %w", err)
	}
	idempotencyID, err := newID("idem")
	if err != nil {
		return domain.Message{}, domain.Run{}, err
	}
	tag, err := tx.Exec(ctx, `
		INSERT INTO idempotency_keys (id, scope, key, request_hash, response)
		VALUES ($1, $2, $3, $4, $5::jsonb)
		ON CONFLICT (scope, key) DO NOTHING`,
		idempotencyID, "conversation-message:"+p.ConversationID, p.IdempotencyKey, requestHash, responseJSON,
	)
	if err != nil {
		return domain.Message{}, domain.Run{}, fmt.Errorf("insert idempotency key: %w", err)
	}
	if tag.RowsAffected() == 0 {
		message, run, err := replayCreatedMessageAndRun(ctx, tx, p.ConversationID, p.IdempotencyKey, requestHash)
		if err != nil {
			return domain.Message{}, domain.Run{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return domain.Message{}, domain.Run{}, fmt.Errorf("commit idempotent replay: %w", err)
		}
		return message, run, nil
	}

	var ordinal int64
	if err := tx.QueryRow(ctx, `
		SELECT COALESCE(MAX(ordinal), 0) + 1
		FROM messages
		WHERE conversation_id = $1`, p.ConversationID).Scan(&ordinal); err != nil {
		return domain.Message{}, domain.Run{}, fmt.Errorf("next message ordinal: %w", err)
	}
	message, err := scanMessage(tx.QueryRow(ctx, `
		INSERT INTO messages (id, conversation_id, ordinal, role, content, metadata)
		VALUES ($1, $2, $3, 'user', $4, '{}'::jsonb)
		RETURNING id, conversation_id, COALESCE(run_id, ''), ordinal, role, content, metadata, created_at`,
		messageID, p.ConversationID, ordinal, p.Content,
	))
	if err != nil {
		return domain.Message{}, domain.Run{}, fmt.Errorf("insert message: %w", err)
	}

	eventID, err := newID("evt")
	if err != nil {
		return domain.Message{}, domain.Run{}, err
	}
	command := event.RunCommand{
		RunID:             runID,
		ConversationID:    p.ConversationID,
		Model:             p.Model,
		Messages:          []domain.Message{message},
		CheckpointVersion: 0,
	}
	specification, err := json.Marshal(runSpecification{AgentID: p.AgentID, Command: command})
	if err != nil {
		return domain.Message{}, domain.Run{}, fmt.Errorf("marshal run specification: %w", err)
	}
	run, err := scanRun(tx.QueryRow(ctx, `
		INSERT INTO runs (id, project_id, conversation_id, status, specification)
		VALUES ($1, $2, $3, 'queued', $4::jsonb)
		RETURNING id, project_id, conversation_id, status, specification, checkpoint_version,
		          cancel_requested_at, started_at, finished_at, created_at, updated_at`,
		runID, projectID, p.ConversationID, specification,
	))
	if err != nil {
		return domain.Message{}, domain.Run{}, fmt.Errorf("insert run: %w", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE messages SET run_id = $1 WHERE id = $2`, run.ID, message.ID); err != nil {
		return domain.Message{}, domain.Run{}, fmt.Errorf("attach message to run: %w", err)
	}
	message.RunID = run.ID

	outboxEvent := event.Envelope[event.RunCommand]{
		SchemaVersion: "agenttrace.event.v1",
		EventID:       eventID,
		EventType:     "run.requested",
		AggregateID:   run.ID,
		OccurredAt:    run.CreatedAt,
		CorrelationID: run.ID,
		CausationID:   message.ID,
		Payload:       command,
	}
	outboxHash, err := payloadHash(outboxEvent)
	if err != nil {
		return domain.Message{}, domain.Run{}, err
	}
	inserted, err := insertOutbox(ctx, tx, eventID, runCommandsTopic, run.ID, outboxEvent, outboxHash)
	if err != nil {
		return domain.Message{}, domain.Run{}, err
	}
	if !inserted {
		return domain.Message{}, domain.Run{}, fmt.Errorf("new outbox event %q: %w", eventID, ErrConflict)
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.Message{}, domain.Run{}, fmt.Errorf("commit create message and run: %w", err)
	}
	return message, run, nil
}

func validateCreateMessageParams(p CreateMessageParams) error {
	switch {
	case strings.TrimSpace(p.ConversationID) == "":
		return errors.New("conversation id is required")
	case strings.TrimSpace(p.Content) == "":
		return errors.New("message content is required")
	case strings.TrimSpace(p.AgentID) == "":
		return errors.New("agent id is required")
	case strings.TrimSpace(p.Model.Provider) == "" || strings.TrimSpace(p.Model.Name) == "":
		return errors.New("model provider and name are required")
	case strings.TrimSpace(p.IdempotencyKey) == "":
		return errors.New("idempotency key is required")
	default:
		return nil
	}
}

func replayCreatedMessageAndRun(
	ctx context.Context,
	tx pgx.Tx,
	conversationID, key, requestHash string,
) (domain.Message, domain.Run, error) {
	var (
		storedHash string
		response   createMessageResponse
	)
	err := tx.QueryRow(ctx, `
		SELECT request_hash, response
		FROM idempotency_keys
		WHERE scope = $1 AND key = $2
		FOR UPDATE`,
		"conversation-message:"+conversationID, key,
	).Scan(&storedHash, &response)
	if err != nil {
		return domain.Message{}, domain.Run{}, fmt.Errorf("read idempotency replay: %w", err)
	}
	if storedHash != requestHash {
		return domain.Message{}, domain.Run{}, ErrConflict
	}
	message, err := scanMessage(tx.QueryRow(ctx, `
		SELECT id, conversation_id, COALESCE(run_id, ''), ordinal, role, content, metadata, created_at
		FROM messages
		WHERE id = $1`, response.MessageID))
	if err != nil {
		return domain.Message{}, domain.Run{}, fmt.Errorf("read idempotent message: %w", err)
	}
	run, err := scanRun(tx.QueryRow(ctx, `
		SELECT id, project_id, conversation_id, status, specification, checkpoint_version,
		       cancel_requested_at, started_at, finished_at, created_at, updated_at
		FROM runs
		WHERE id = $1`, response.RunID))
	if err != nil {
		return domain.Message{}, domain.Run{}, fmt.Errorf("read idempotent run: %w", err)
	}
	return message, run, nil
}

// ClaimRun atomically transitions a queued or already-running run for the expected checkpoint.
func (s *Store) ClaimRun(
	ctx context.Context, runID string, expectedCheckpoint int64,
) (domain.Run, bool, error) {
	if s == nil || s.pool == nil {
		return domain.Run{}, false, errors.New("postgres store is not configured")
	}
	run, err := scanRun(s.pool.QueryRow(ctx, `
		UPDATE runs
		SET status = 'running', checkpoint_version = checkpoint_version + 1,
		    started_at = COALESCE(started_at, now()), updated_at = now()
		WHERE id = $1
		  AND status IN ('queued', 'running')
		  AND checkpoint_version = $2
		RETURNING id, project_id, conversation_id, status, specification, checkpoint_version,
		          cancel_requested_at, started_at, finished_at, created_at, updated_at`,
		runID, expectedCheckpoint,
	))
	if errors.Is(err, ErrNotFound) {
		return domain.Run{}, false, nil
	}
	if err != nil {
		return domain.Run{}, false, fmt.Errorf("claim run %q: %w", runID, err)
	}
	return run, true, nil
}
