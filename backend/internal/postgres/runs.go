package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/domain"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/event"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/execution"
	"github.com/jackc/pgx/v5"
)

// CreateMessageParams is the validated API request persisted as a user message and queued run.
type CreateMessageParams struct {
	ConversationID   string
	Content          string
	Model            event.ModelRef
	IdempotencyKey   string
	RuntimeID        string
	HarnessVersionID string
}

type createMessageResponse struct {
	MessageID string `json:"message_id"`
	RunID     string `json:"run_id"`
}

// CreateRerunParams identifies a failed Run and an idempotency key for its fresh execution.
type CreateRerunParams struct {
	RunID          string
	IdempotencyKey string
}

type createRerunResponse struct {
	RunID string `json:"run_id"`
}

type runSpecification struct {
	// Retain stored managed provenance; callers cannot select a legacy agent.
	AgentID   string           `json:"agent_id"`
	Command   event.RunCommand `json:"command"`
	Execution *execution.Spec  `json:"execution,omitempty"`
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
	if !execution.KnownRuntime(p.RuntimeID) || p.HarnessVersionID == "" {
		return domain.Message{}, domain.Run{}, ErrConflict
	}
	// Keep the established managed request hash's null/omission semantics.
	requestHash, err := payloadHash(struct {
		ConversationID   string            `json:"conversation_id"`
		Content          string            `json:"content"`
		AgentID          string            `json:"agent_id"`
		Model            event.ModelRef    `json:"model"`
		Operations       []event.Operation `json:"operations"`
		Execution        *execution.Spec   `json:"execution,omitempty"`
		RuntimeID        string            `json:"runtime_id,omitempty"`
		HarnessVersionID string            `json:"harness_version_id,omitempty"`
	}{
		ConversationID:   p.ConversationID,
		Content:          p.Content,
		AgentID:          p.RuntimeID,
		Model:            p.Model,
		Operations:       nil,
		Execution:        nil,
		RuntimeID:        p.RuntimeID,
		HarnessVersionID: p.HarnessVersionID,
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

	environment, err := s.checkConversationRunnable(ctx, tx, p.ConversationID)
	if err != nil {
		return domain.Message{}, domain.Run{}, err
	}
	harness, err := resolveProjectHarnessTx(ctx, tx, projectID, p.HarnessVersionID, p.RuntimeID)
	if err != nil {
		return domain.Message{}, domain.Run{}, err
	}
	selectedExecution := &execution.Spec{RuntimeID: p.RuntimeID, Harness: harness}
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
		Execution:         selectedExecution,
		ProjectID:         projectID,
		EnvironmentID:     environment.ID,
		ProfileGeneration: environment.ProfileGeneration,
	}
	specification, err := json.Marshal(runSpecification{AgentID: p.RuntimeID, Command: command, Execution: selectedExecution})
	if err != nil {
		return domain.Message{}, domain.Run{}, fmt.Errorf("marshal run specification: %w", err)
	}
	run, err := scanRun(tx.QueryRow(ctx, `
		INSERT INTO runs (
			id, project_id, conversation_id, source_message_id, status, specification
		)
		VALUES ($1, $2, $3, $4, 'queued', $5::jsonb)
		RETURNING id, project_id, conversation_id,
		          COALESCE(source_message_id, ''), COALESCE(retry_of_run_id, ''),
		          status, specification, checkpoint_version,
		          cancel_requested_at, started_at, finished_at, created_at, updated_at`,
		runID, projectID, p.ConversationID, message.ID, specification,
	))
	if err != nil {
		return domain.Message{}, domain.Run{}, fmt.Errorf("insert run: %w", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE messages SET run_id = $1 WHERE id = $2`, run.ID, message.ID); err != nil {
		return domain.Message{}, domain.Run{}, fmt.Errorf("attach message to run: %w", err)
	}
	message.RunID = run.ID

	outboxEvent := event.Envelope[event.RunCommand]{
		SchemaVersion: "agenttrace.run-command.v1",
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
	inserted, err := insertOutbox(ctx, tx, eventID, runCommandsTopic, environment.ID, outboxEvent, outboxHash)
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
		SELECT id, project_id, conversation_id,
		       COALESCE(source_message_id, ''), COALESCE(retry_of_run_id, ''),
		       status, specification, checkpoint_version,
		       cancel_requested_at, started_at, finished_at, created_at, updated_at
		FROM runs
		WHERE id = $1`, response.RunID))
	if err != nil {
		return domain.Message{}, domain.Run{}, fmt.Errorf("read idempotent run: %w", err)
	}
	return message, run, nil
}

// CreateRerun atomically queues a fresh execution of a failed Run's immutable specification.
func (s *Store) CreateRerun(ctx context.Context, p CreateRerunParams) (domain.Run, error) {
	if s == nil || s.pool == nil {
		return domain.Run{}, errors.New("postgres store is not configured")
	}
	if err := validateCreateRerunParams(p); err != nil {
		return domain.Run{}, err
	}
	requestHash, err := payloadHash(struct {
		RunID string `json:"run_id"`
	}{RunID: p.RunID})
	if err != nil {
		return domain.Run{}, err
	}
	scope := "run-rerun:" + p.RunID

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.Run{}, fmt.Errorf("begin create rerun: %w", err)
	}
	defer tx.Rollback(ctx)

	var conversationID string
	if err := tx.QueryRow(ctx, `
		SELECT conversation_id
		FROM runs
		WHERE id = $1`, p.RunID).Scan(&conversationID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Run{}, fmt.Errorf("rerun source %q: %w", p.RunID, ErrNotFound)
		}
		return domain.Run{}, fmt.Errorf("read rerun source conversation: %w", err)
	}
	projectID, err := lockConversation(ctx, tx, conversationID)
	if err != nil {
		return domain.Run{}, err
	}
	source, err := scanRun(tx.QueryRow(ctx, `
		SELECT id, project_id, conversation_id,
		       COALESCE(source_message_id, ''), COALESCE(retry_of_run_id, ''),
		       status, specification, checkpoint_version,
		       cancel_requested_at, started_at, finished_at, created_at, updated_at
		FROM runs
		WHERE id = $1 AND project_id = $2 AND conversation_id = $3
		FOR UPDATE`, p.RunID, projectID, conversationID))
	if err != nil {
		return domain.Run{}, fmt.Errorf("lock rerun source: %w", err)
	}
	if source.Status != domain.RunFailed || source.SourceMessageID == "" {
		return domain.Run{}, ErrConflict
	}

	var specification runSpecification
	if err := json.Unmarshal(source.Specification, &specification); err != nil {
		return domain.Run{}, fmt.Errorf("decode rerun specification: %w", err)
	}
	rerunID, err := newID("run")
	if err != nil {
		return domain.Run{}, err
	}
	eventID, err := newID("evt")
	if err != nil {
		return domain.Run{}, err
	}
	idempotencyID, err := newID("idem")
	if err != nil {
		return domain.Run{}, err
	}
	responseJSON, err := json.Marshal(createRerunResponse{RunID: rerunID})
	if err != nil {
		return domain.Run{}, fmt.Errorf("marshal rerun idempotency response: %w", err)
	}
	tag, err := tx.Exec(ctx, `
		INSERT INTO idempotency_keys (id, scope, key, request_hash, response)
		VALUES ($1, $2, $3, $4, $5::jsonb)
		ON CONFLICT (scope, key) DO NOTHING`,
		idempotencyID, scope, p.IdempotencyKey, requestHash, responseJSON,
	)
	if err != nil {
		return domain.Run{}, fmt.Errorf("insert rerun idempotency key: %w", err)
	}
	if tag.RowsAffected() == 0 {
		rerun, err := replayCreatedRerun(ctx, tx, scope, p.IdempotencyKey, requestHash)
		if err != nil {
			return domain.Run{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return domain.Run{}, fmt.Errorf("commit rerun idempotent replay: %w", err)
		}
		return rerun, nil
	}

	specification, err = validateManagedRunTx(ctx, tx, source)
	if err != nil {
		return domain.Run{}, err
	}
	environment, err := s.checkConversationRunnable(ctx, tx, conversationID)
	if err != nil {
		return domain.Run{}, err
	}
	specification.Command.RunID = rerunID
	specification.Command.CheckpointVersion = 0
	if environment.ID != "" {
		specification.Command.ProjectID = projectID
		specification.Command.EnvironmentID = environment.ID
		specification.Command.ProfileGeneration = environment.ProfileGeneration
		specification.Command.FencingToken = 0
		specification.Command.WorkerURL = ""
	}
	specificationJSON, err := json.Marshal(specification)
	if err != nil {
		return domain.Run{}, fmt.Errorf("marshal rerun specification: %w", err)
	}
	rerun, err := scanRun(tx.QueryRow(ctx, `
		INSERT INTO runs (
			id, project_id, conversation_id, source_message_id, retry_of_run_id, status, specification
		)
		VALUES ($1, $2, $3, $4, $5, 'queued', $6::jsonb)
		RETURNING id, project_id, conversation_id,
		          COALESCE(source_message_id, ''), COALESCE(retry_of_run_id, ''),
		          status, specification, checkpoint_version,
		          cancel_requested_at, started_at, finished_at, created_at, updated_at`,
		rerunID, source.ProjectID, source.ConversationID, source.SourceMessageID, source.ID, specificationJSON,
	))
	if err != nil {
		return domain.Run{}, fmt.Errorf("insert rerun: %w", err)
	}
	envelope := event.Envelope[event.RunCommand]{
		SchemaVersion: "agenttrace.run-command.v1",
		EventID:       eventID,
		EventType:     "run.requested",
		AggregateID:   rerun.ID,
		OccurredAt:    rerun.CreatedAt,
		CorrelationID: rerun.ID,
		CausationID:   source.ID,
		Payload:       specification.Command,
	}
	outboxHash, err := payloadHash(envelope)
	if err != nil {
		return domain.Run{}, err
	}
	inserted, err := insertOutbox(ctx, tx, eventID, runCommandsTopic, environment.ID, envelope, outboxHash)
	if err != nil {
		return domain.Run{}, err
	}
	if !inserted {
		return domain.Run{}, fmt.Errorf("new rerun outbox event %q: %w", eventID, ErrConflict)
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.Run{}, fmt.Errorf("commit create rerun: %w", err)
	}
	return rerun, nil
}

func validateCreateRerunParams(p CreateRerunParams) error {
	if strings.TrimSpace(p.RunID) == "" || strings.TrimSpace(p.IdempotencyKey) == "" {
		return errors.New("run id and idempotency key are required")
	}
	return nil
}

func replayCreatedRerun(
	ctx context.Context,
	tx pgx.Tx,
	scope, key, requestHash string,
) (domain.Run, error) {
	var (
		storedHash string
		response   createRerunResponse
	)
	if err := tx.QueryRow(ctx, `
		SELECT request_hash, response
		FROM idempotency_keys
		WHERE scope = $1 AND key = $2
		FOR UPDATE`,
		scope, key,
	).Scan(&storedHash, &response); err != nil {
		return domain.Run{}, fmt.Errorf("read rerun idempotency replay: %w", err)
	}
	if storedHash != requestHash {
		return domain.Run{}, ErrConflict
	}
	rerun, err := scanRun(tx.QueryRow(ctx, `
		SELECT id, project_id, conversation_id,
		       COALESCE(source_message_id, ''), COALESCE(retry_of_run_id, ''),
		       status, specification, checkpoint_version,
		       cancel_requested_at, started_at, finished_at, created_at, updated_at
		FROM runs
		WHERE id = $1`, response.RunID))
	if err != nil {
		return domain.Run{}, fmt.Errorf("read idempotent rerun: %w", err)
	}
	return rerun, nil
}
