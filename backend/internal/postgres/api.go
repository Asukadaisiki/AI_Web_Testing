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

// Project is the durable top-level workspace projection.
type Project struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Metadata  json.RawMessage `json:"metadata"`
	CreatedAt time.Time       `json:"created_at"`
	UpdatedAt time.Time       `json:"updated_at"`
}

// Conversation is an ordered message thread inside a project.
type Conversation struct {
	ID        string          `json:"id"`
	ProjectID string          `json:"project_id"`
	Title     string          `json:"title,omitempty"`
	Metadata  json.RawMessage `json:"metadata"`
	CreatedAt time.Time       `json:"created_at"`
	UpdatedAt time.Time       `json:"updated_at"`
}

type CreateProjectParams struct {
	Name     string
	Metadata json.RawMessage
}

type CreateConversationParams struct {
	ProjectID string
	Title     string
	Metadata  json.RawMessage
}

// SubmitResponseParams describes a user response to a durable wait state.
type SubmitResponseParams struct {
	RunID          string
	Kind           string
	Content        string
	IdempotencyKey string
	Continuation   bool
}

func (s *Store) ListProjects(ctx context.Context) ([]Project, error) {
	if s == nil || s.pool == nil {
		return nil, errors.New("postgres store is not configured")
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, name, metadata, created_at, updated_at
		FROM projects
		ORDER BY created_at, id`)
	if err != nil {
		return nil, fmt.Errorf("list projects: %w", err)
	}
	defer rows.Close()

	var projects []Project
	for rows.Next() {
		project, err := scanProject(rows)
		if err != nil {
			return nil, err
		}
		projects = append(projects, project)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate projects: %w", err)
	}
	return projects, nil
}

// GetProject returns the canonical project projection by ID.
func (s *Store) GetProject(ctx context.Context, projectID string) (Project, error) {
	if s == nil || s.pool == nil {
		return Project{}, errors.New("postgres store is not configured")
	}
	project, err := scanProject(s.pool.QueryRow(ctx, `
		SELECT id, name, metadata, created_at, updated_at
		FROM projects
		WHERE id = $1`, projectID))
	if err != nil {
		return Project{}, fmt.Errorf("get project: %w", err)
	}
	return project, nil
}

func (s *Store) CreateProject(ctx context.Context, p CreateProjectParams) (Project, error) {
	if s == nil || s.pool == nil {
		return Project{}, errors.New("postgres store is not configured")
	}
	if strings.TrimSpace(p.Name) == "" {
		return Project{}, errors.New("project name is required")
	}
	id, err := newID("prj")
	if err != nil {
		return Project{}, err
	}
	project, err := scanProject(s.pool.QueryRow(ctx, `
		INSERT INTO projects (id, name, metadata)
		VALUES ($1, $2, $3::jsonb)
		RETURNING id, name, metadata, created_at, updated_at`,
		id, p.Name, requiredJSON(p.Metadata, `{}`)))
	if err != nil {
		return Project{}, fmt.Errorf("create project: %w", err)
	}
	return project, nil
}

func (s *Store) ListConversations(ctx context.Context, projectID string) ([]Conversation, error) {
	if s == nil || s.pool == nil {
		return nil, errors.New("postgres store is not configured")
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, project_id, COALESCE(title, ''), metadata, created_at, updated_at
		FROM conversations
		WHERE project_id = $1
		ORDER BY created_at, id`, projectID)
	if err != nil {
		return nil, fmt.Errorf("list conversations: %w", err)
	}
	defer rows.Close()

	var conversations []Conversation
	for rows.Next() {
		conversation, err := scanConversation(rows)
		if err != nil {
			return nil, err
		}
		conversations = append(conversations, conversation)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate conversations: %w", err)
	}
	return conversations, nil
}

func (s *Store) CreateConversation(ctx context.Context, p CreateConversationParams) (Conversation, error) {
	if s == nil || s.pool == nil {
		return Conversation{}, errors.New("postgres store is not configured")
	}
	if strings.TrimSpace(p.ProjectID) == "" {
		return Conversation{}, errors.New("project id is required")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Conversation{}, fmt.Errorf("begin create conversation: %w", err)
	}
	defer tx.Rollback(ctx)
	if err := lockProject(ctx, tx, p.ProjectID); err != nil {
		return Conversation{}, err
	}
	id, err := newID("conv")
	if err != nil {
		return Conversation{}, err
	}
	conversation, err := scanConversation(tx.QueryRow(ctx, `
		INSERT INTO conversations (id, project_id, title, metadata)
		VALUES ($1, $2, NULLIF($3, ''), $4::jsonb)
		RETURNING id, project_id, COALESCE(title, ''), metadata, created_at, updated_at`,
		id, p.ProjectID, p.Title, requiredJSON(p.Metadata, `{}`)))
	if err != nil {
		return Conversation{}, fmt.Errorf("create conversation: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Conversation{}, fmt.Errorf("commit create conversation: %w", err)
	}
	return conversation, nil
}

func (s *Store) GetConversation(ctx context.Context, conversationID string) (Conversation, []domain.Message, error) {
	if s == nil || s.pool == nil {
		return Conversation{}, nil, errors.New("postgres store is not configured")
	}
	conversation, err := scanConversation(s.pool.QueryRow(ctx, `
		SELECT id, project_id, COALESCE(title, ''), metadata, created_at, updated_at
		FROM conversations WHERE id = $1`, conversationID))
	if err != nil {
		return Conversation{}, nil, fmt.Errorf("get conversation: %w", err)
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, conversation_id, COALESCE(run_id, ''), ordinal, role, content, metadata, created_at
		FROM messages
		WHERE conversation_id = $1
		ORDER BY ordinal ASC`, conversationID)
	if err != nil {
		return Conversation{}, nil, fmt.Errorf("list conversation messages: %w", err)
	}
	defer rows.Close()
	var messages []domain.Message
	for rows.Next() {
		message, err := scanMessage(rows)
		if err != nil {
			return Conversation{}, nil, err
		}
		messages = append(messages, message)
	}
	if err := rows.Err(); err != nil {
		return Conversation{}, nil, fmt.Errorf("iterate conversation messages: %w", err)
	}
	return conversation, messages, nil
}

func (s *Store) GetRun(ctx context.Context, runID string) (domain.Run, error) {
	if s == nil || s.pool == nil {
		return domain.Run{}, errors.New("postgres store is not configured")
	}
	run, err := scanRun(s.pool.QueryRow(ctx, `
		SELECT id, project_id, conversation_id, status, specification, checkpoint_version,
		       cancel_requested_at, started_at, finished_at, created_at, updated_at
		FROM runs WHERE id = $1`, runID))
	if err != nil {
		return domain.Run{}, fmt.Errorf("get run: %w", err)
	}
	return run, nil
}

func (s *Store) ListTrace(ctx context.Context, runID string) ([]domain.Span, error) {
	if s == nil || s.pool == nil {
		return nil, errors.New("postgres store is not configured")
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, run_id, trace_id, span_id, COALESCE(parent_span_id, ''), kind, name, status,
		       input, output, started_at, ended_at, created_at, updated_at
		FROM spans
		WHERE run_id = $1
		ORDER BY started_at, id`, runID)
	if err != nil {
		return nil, fmt.Errorf("list trace: %w", err)
	}
	defer rows.Close()
	var spans []domain.Span
	for rows.Next() {
		var span domain.Span
		if err := rows.Scan(
			&span.ID, &span.RunID, &span.TraceID, &span.SpanID, &span.ParentSpanID,
			&span.Kind, &span.Name, &span.Status, &span.Input, &span.Output,
			&span.StartedAt, &span.EndedAt, &span.CreatedAt, &span.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan trace span: %w", err)
		}
		spans = append(spans, span)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate trace spans: %w", err)
	}
	return spans, nil
}

// TraceDetails is the canonical workspace projection for a run's spans and
// their persisted attempts, usage, and operation evidence.
type TraceDetails struct {
	Spans      []domain.Span
	Attempts   []TraceAttemptDetail
	Usage      []TraceUsageDetail
	Operations []TraceOperationDetail
}

// TraceAttemptDetail intentionally omits request and response bodies. Span
// input/output remain the public payload boundary.
type TraceAttemptDetail struct {
	ID            string
	SpanID        string
	AttemptNumber int
	Provider      string
	OperationID   string
	Status        string
	Error         json.RawMessage
	StartedAt     time.Time
	EndedAt       *time.Time
}

// TraceUsageDetail is one canonical token and cost record.
type TraceUsageDetail struct {
	ID            string
	SpanID        string
	SpanAttemptID string
	Provider      string
	Model         string
	InputTokens   int64
	OutputTokens  int64
	TotalTokens   int64
	CostMicros    int64
	Details       json.RawMessage
	CreatedAt     time.Time
}

// TraceOperationDetail is the result and artifact evidence for one operation.
type TraceOperationDetail struct {
	ID            string
	SpanID        string
	SpanAttemptID string
	OperationID   string
	Status        string
	Result        json.RawMessage
	Artifacts     json.RawMessage
	Error         json.RawMessage
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// GetTraceDetails reads one internally consistent trace projection.
func (s *Store) GetTraceDetails(ctx context.Context, runID string) (TraceDetails, error) {
	if s == nil || s.pool == nil {
		return TraceDetails{}, errors.New("postgres store is not configured")
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{
		IsoLevel:   pgx.RepeatableRead,
		AccessMode: pgx.ReadOnly,
	})
	if err != nil {
		return TraceDetails{}, fmt.Errorf("begin trace details: %w", err)
	}
	defer tx.Rollback(ctx)

	var details TraceDetails
	spanRows, err := tx.Query(ctx, `
		SELECT id, run_id, trace_id, span_id, COALESCE(parent_span_id, ''), kind, name, status,
		       input, output, started_at, ended_at, created_at, updated_at
		FROM spans
		WHERE run_id = $1
		ORDER BY started_at, id`, runID)
	if err != nil {
		return TraceDetails{}, fmt.Errorf("list detailed trace spans: %w", err)
	}
	for spanRows.Next() {
		var span domain.Span
		if err := spanRows.Scan(
			&span.ID, &span.RunID, &span.TraceID, &span.SpanID, &span.ParentSpanID,
			&span.Kind, &span.Name, &span.Status, &span.Input, &span.Output,
			&span.StartedAt, &span.EndedAt, &span.CreatedAt, &span.UpdatedAt,
		); err != nil {
			spanRows.Close()
			return TraceDetails{}, fmt.Errorf("scan detailed trace span: %w", err)
		}
		details.Spans = append(details.Spans, span)
	}
	if err := spanRows.Err(); err != nil {
		spanRows.Close()
		return TraceDetails{}, fmt.Errorf("iterate detailed trace spans: %w", err)
	}
	spanRows.Close()

	attemptRows, err := tx.Query(ctx, `
		SELECT attempt.id, span.span_id, attempt.attempt_number,
		       COALESCE(attempt.provider, ''), COALESCE(attempt.operation_id, ''),
		       attempt.status, attempt.error, attempt.started_at, attempt.ended_at
		FROM span_attempts AS attempt
		JOIN spans AS span ON span.id = attempt.span_id
		WHERE span.run_id = $1
		ORDER BY span.started_at, span.id, attempt.attempt_number`, runID)
	if err != nil {
		return TraceDetails{}, fmt.Errorf("list trace attempts: %w", err)
	}
	for attemptRows.Next() {
		var attempt TraceAttemptDetail
		if err := attemptRows.Scan(
			&attempt.ID, &attempt.SpanID, &attempt.AttemptNumber,
			&attempt.Provider, &attempt.OperationID, &attempt.Status, &attempt.Error,
			&attempt.StartedAt, &attempt.EndedAt,
		); err != nil {
			attemptRows.Close()
			return TraceDetails{}, fmt.Errorf("scan trace attempt: %w", err)
		}
		details.Attempts = append(details.Attempts, attempt)
	}
	if err := attemptRows.Err(); err != nil {
		attemptRows.Close()
		return TraceDetails{}, fmt.Errorf("iterate trace attempts: %w", err)
	}
	attemptRows.Close()

	usageRows, err := tx.Query(ctx, `
		SELECT usage.id, COALESCE(span.span_id, ''), COALESCE(usage.span_attempt_id, ''),
		       COALESCE(usage.provider, ''), COALESCE(usage.model, ''),
		       usage.input_tokens, usage.output_tokens, usage.total_tokens,
		       usage.cost_micros, usage.details, usage.created_at
		FROM usage_records AS usage
		LEFT JOIN span_attempts AS attempt ON attempt.id = usage.span_attempt_id
		LEFT JOIN spans AS span ON span.id = attempt.span_id
		WHERE usage.run_id = $1
		ORDER BY usage.created_at, usage.id`, runID)
	if err != nil {
		return TraceDetails{}, fmt.Errorf("list trace usage: %w", err)
	}
	for usageRows.Next() {
		var record TraceUsageDetail
		if err := usageRows.Scan(
			&record.ID, &record.SpanID, &record.SpanAttemptID,
			&record.Provider, &record.Model, &record.InputTokens, &record.OutputTokens,
			&record.TotalTokens, &record.CostMicros, &record.Details, &record.CreatedAt,
		); err != nil {
			usageRows.Close()
			return TraceDetails{}, fmt.Errorf("scan trace usage: %w", err)
		}
		details.Usage = append(details.Usage, record)
	}
	if err := usageRows.Err(); err != nil {
		usageRows.Close()
		return TraceDetails{}, fmt.Errorf("iterate trace usage: %w", err)
	}
	usageRows.Close()

	operationRows, err := tx.Query(ctx, `
		SELECT operation.id, COALESCE(span.span_id, ''),
		       COALESCE(operation.span_attempt_id, ''), operation.operation_id,
		       operation.status, operation.result, operation.artifacts, operation.error,
		       operation.created_at, operation.updated_at
		FROM operation_results AS operation
		LEFT JOIN span_attempts AS attempt ON attempt.id = operation.span_attempt_id
		LEFT JOIN spans AS span ON span.id = attempt.span_id
		WHERE operation.run_id = $1
		ORDER BY operation.created_at, operation.id`, runID)
	if err != nil {
		return TraceDetails{}, fmt.Errorf("list trace operation evidence: %w", err)
	}
	for operationRows.Next() {
		var operation TraceOperationDetail
		if err := operationRows.Scan(
			&operation.ID, &operation.SpanID, &operation.SpanAttemptID,
			&operation.OperationID, &operation.Status, &operation.Result,
			&operation.Artifacts, &operation.Error, &operation.CreatedAt,
			&operation.UpdatedAt,
		); err != nil {
			operationRows.Close()
			return TraceDetails{}, fmt.Errorf("scan trace operation evidence: %w", err)
		}
		details.Operations = append(details.Operations, operation)
	}
	if err := operationRows.Err(); err != nil {
		operationRows.Close()
		return TraceDetails{}, fmt.Errorf("iterate trace operation evidence: %w", err)
	}
	operationRows.Close()

	if err := tx.Commit(ctx); err != nil {
		return TraceDetails{}, fmt.Errorf("commit trace details read: %w", err)
	}
	return details, nil
}

// RequestCancel marks cancellation durably and emits an ordered replay event.
func (s *Store) RequestCancel(ctx context.Context, runID string) error {
	if s == nil || s.pool == nil {
		return errors.New("postgres store is not configured")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin request cancel: %w", err)
	}
	defer tx.Rollback(ctx)
	run, err := scanRun(tx.QueryRow(ctx, `
		SELECT id, project_id, conversation_id, status, specification, checkpoint_version,
		       cancel_requested_at, started_at, finished_at, created_at, updated_at
		FROM runs WHERE id = $1 FOR UPDATE`, runID))
	if err != nil {
		return fmt.Errorf("lock run for cancel: %w", err)
	}
	if run.CancelRequestedAt != nil {
		return tx.Commit(ctx)
	}
	if run.Status == domain.RunSucceeded || run.Status == domain.RunFailed || run.Status == domain.RunCancelled {
		return tx.Commit(ctx)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE runs
		SET cancel_requested_at = now(), status = 'cancelling', updated_at = now()
		WHERE id = $1`, runID); err != nil {
		return fmt.Errorf("mark run cancellation: %w", err)
	}
	if err := appendRunEvent(ctx, tx, runID, "run.cancel.requested", map[string]string{"run_id": runID}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// SubmitResponse persists a response and queues a continuation in one transaction.
func (s *Store) SubmitResponse(ctx context.Context, p SubmitResponseParams) error {
	if s == nil || s.pool == nil {
		return errors.New("postgres store is not configured")
	}
	if strings.TrimSpace(p.RunID) == "" || strings.TrimSpace(p.Kind) == "" ||
		strings.TrimSpace(p.Content) == "" || strings.TrimSpace(p.IdempotencyKey) == "" {
		return errors.New("run id, response kind, content, and idempotency key are required")
	}
	requestHash, err := payloadHash(p)
	if err != nil {
		return err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin submit response: %w", err)
	}
	defer tx.Rollback(ctx)
	var conversationID string
	if err := tx.QueryRow(ctx, `SELECT conversation_id FROM runs WHERE id = $1`, p.RunID).Scan(&conversationID); errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("run %q: %w", p.RunID, ErrNotFound)
	} else if err != nil {
		return fmt.Errorf("read response run conversation: %w", err)
	}
	if _, err := lockConversation(ctx, tx, conversationID); err != nil {
		return err
	}
	run, err := scanRun(tx.QueryRow(ctx, `
		SELECT id, project_id, conversation_id, status, specification, checkpoint_version,
		       cancel_requested_at, started_at, finished_at, created_at, updated_at
		FROM runs WHERE id = $1 FOR UPDATE`, p.RunID))
	if err != nil {
		return fmt.Errorf("lock run for response: %w", err)
	}
	idempotencyID, err := newID("idem")
	if err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `
		INSERT INTO idempotency_keys (id, scope, key, request_hash, response)
		VALUES ($1, $2, $3, $4, '{}'::jsonb)
		ON CONFLICT (scope, key) DO NOTHING`,
		idempotencyID, "run-response:"+p.RunID, p.IdempotencyKey, requestHash)
	if err != nil {
		return fmt.Errorf("insert response idempotency key: %w", err)
	}
	if tag.RowsAffected() == 0 {
		var storedHash string
		if err := tx.QueryRow(ctx, `
			SELECT request_hash FROM idempotency_keys
			WHERE scope = $1 AND key = $2
			FOR UPDATE`, "run-response:"+p.RunID, p.IdempotencyKey).Scan(&storedHash); err != nil {
			return fmt.Errorf("read response idempotency key: %w", err)
		}
		if storedHash != requestHash {
			return ErrConflict
		}
		return tx.Commit(ctx)
	}
	if run.Status != domain.RunWaitingInput && run.Status != domain.RunWaitingApproval {
		return ErrConflict
	}
	var ordinal int64
	if err := tx.QueryRow(ctx, `
		SELECT COALESCE(MAX(ordinal), 0) + 1
		FROM messages WHERE conversation_id = $1`, run.ConversationID).Scan(&ordinal); err != nil {
		return fmt.Errorf("next response message ordinal: %w", err)
	}
	messageID, err := newID("msg")
	if err != nil {
		return err
	}
	metadata, err := json.Marshal(map[string]string{
		"response_kind": p.Kind,
		"run_id":        p.RunID,
	})
	if err != nil {
		return fmt.Errorf("marshal response metadata: %w", err)
	}
	message, err := scanMessage(tx.QueryRow(ctx, `
		INSERT INTO messages (id, conversation_id, ordinal, role, content, metadata)
		VALUES ($1, $2, $3, 'user', $4, $5::jsonb)
		RETURNING id, conversation_id, COALESCE(run_id, ''), ordinal, role, content, metadata, created_at`,
		messageID, run.ConversationID, ordinal, p.Content, metadata))
	if err != nil {
		return fmt.Errorf("persist response message: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE runs SET status = 'running', updated_at = now()
		WHERE id = $1`, run.ID); err != nil {
		return fmt.Errorf("resume run after response: %w", err)
	}
	if err := appendRunEvent(ctx, tx, run.ID, "run.response.received", map[string]any{
		"run_id": run.ID, "message_id": message.ID, "kind": p.Kind,
	}); err != nil {
		return err
	}
	var specification runSpecification
	if err := json.Unmarshal(run.Specification, &specification); err != nil {
		return fmt.Errorf("decode continuation specification: %w", err)
	}
	continuation := specification.Command
	continuation.Messages = []domain.Message{message}
	continuation.CheckpointVersion = run.CheckpointVersion
	envelopeID, err := newID("evt")
	if err != nil {
		return err
	}
	envelope := event.Envelope[event.RunCommand]{
		SchemaVersion: "agenttrace.run-command.v1",
		EventID:       envelopeID,
		EventType:     "run.continuation.requested",
		AggregateID:   run.ID,
		CorrelationID: run.ID,
		CausationID:   message.ID,
		OccurredAt:    time.Now().UTC(),
		Payload:       continuation,
	}
	hash, err := payloadHash(envelope)
	if err != nil {
		return err
	}
	inserted, err := insertOutbox(ctx, tx, envelopeID, runCommandsTopic, run.ID, envelope, hash)
	if err != nil {
		return err
	}
	if !inserted {
		return ErrConflict
	}
	return tx.Commit(ctx)
}

func (s *Store) PendingOutbox(ctx context.Context) (int, error) {
	if s == nil || s.pool == nil {
		return 0, errors.New("postgres store is not configured")
	}
	var count int
	if err := s.pool.QueryRow(ctx, `
		SELECT count(*) FROM outbox_events WHERE published_at IS NULL`).Scan(&count); err != nil {
		return 0, fmt.Errorf("count pending outbox: %w", err)
	}
	return count, nil
}

func appendRunEvent(ctx context.Context, tx pgx.Tx, runID, eventType string, payload any) error {
	eventID, err := newID("evt")
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	envelope := event.Envelope[any]{
		SchemaVersion: "agenttrace.event.v1",
		EventID:       eventID,
		EventType:     eventType,
		AggregateID:   runID,
		CorrelationID: runID,
		OccurredAt:    now,
		Payload:       payload,
	}
	hash, err := payloadHash(envelope)
	if err != nil {
		return err
	}
	var sequence int64
	if err := tx.QueryRow(ctx, `
		SELECT COALESCE(MAX(seq), 0) + 1 FROM run_events WHERE run_id = $1`, runID).Scan(&sequence); err != nil {
		return fmt.Errorf("next %s event sequence: %w", eventType, err)
	}
	encoded, err := json.Marshal(envelope)
	if err != nil {
		return fmt.Errorf("marshal %s event: %w", eventType, err)
	}
	rowID, err := newID("rev")
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO run_events (id, run_id, seq, event_type, payload, occurred_at)
		VALUES ($1, $2, $3, $4, $5::jsonb, $6)`,
		rowID, runID, sequence, eventType, encoded, now); err != nil {
		return fmt.Errorf("insert %s event: %w", eventType, err)
	}
	inserted, err := insertOutbox(ctx, tx, eventID, runEventsTopic, runID, envelope, hash)
	if err != nil {
		return err
	}
	if !inserted {
		return ErrConflict
	}
	return nil
}

func scanProject(row rowScanner) (Project, error) {
	var project Project
	if err := row.Scan(&project.ID, &project.Name, &project.Metadata, &project.CreatedAt, &project.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Project{}, ErrNotFound
		}
		return Project{}, fmt.Errorf("scan project: %w", err)
	}
	return project, nil
}

func scanConversation(row rowScanner) (Conversation, error) {
	var conversation Conversation
	if err := row.Scan(
		&conversation.ID, &conversation.ProjectID, &conversation.Title, &conversation.Metadata,
		&conversation.CreatedAt, &conversation.UpdatedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Conversation{}, ErrNotFound
		}
		return Conversation{}, fmt.Errorf("scan conversation: %w", err)
	}
	return conversation, nil
}
