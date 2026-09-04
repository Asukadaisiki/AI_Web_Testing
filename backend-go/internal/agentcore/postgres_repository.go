package agentcore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
)

type PostgresRepository struct {
	db *sql.DB
}

func NewPostgresRepository(db *sql.DB) *PostgresRepository {
	return &PostgresRepository{db: db}
}

func (r *PostgresRepository) CreateRun(ctx context.Context, run AgentRun) error {
	transcript, err := json.Marshal(run.Transcript)
	if err != nil {
		return fmt.Errorf("encode run transcript: %w", err)
	}
	_, err = r.db.ExecContext(
		ctx,
		`INSERT INTO agent_runs (
			id, conversation_id, project_id, status, input, pending_tool_call_id,
			pending_step_id, latest_generation_id, approved_generation_id,
			transcript_json, last_event_seq, created_at, updated_at
		) VALUES ($1, $2, NULLIF($3, 0), $4, $5, $6, $7, $8, $9, $10, 0, $11, $12)`,
		run.ID,
		run.ConversationID,
		run.ProjectID,
		run.Status,
		run.Input,
		run.PendingToolCallID,
		run.PendingStepID,
		run.LatestGenerationID,
		run.ApprovedGenerationID,
		transcript,
		run.CreatedAt,
		run.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert agent run: %w", err)
	}
	return nil
}

func (r *PostgresRepository) GetRun(ctx context.Context, runID string) (AgentRun, error) {
	var run AgentRun
	var transcript []byte
	err := r.db.QueryRowContext(
		ctx,
		`SELECT id, conversation_id, COALESCE(project_id, 0), status, input, pending_tool_call_id,
		        pending_step_id, latest_generation_id, approved_generation_id,
		        transcript_json, created_at, updated_at
		   FROM agent_runs
		  WHERE id = $1`,
		runID,
	).Scan(
		&run.ID,
		&run.ConversationID,
		&run.ProjectID,
		&run.Status,
		&run.Input,
		&run.PendingToolCallID,
		&run.PendingStepID,
		&run.LatestGenerationID,
		&run.ApprovedGenerationID,
		&transcript,
		&run.CreatedAt,
		&run.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return AgentRun{}, ErrRunNotFound
	}
	if err != nil {
		return AgentRun{}, fmt.Errorf("select agent run: %w", err)
	}
	if err := json.Unmarshal(transcript, &run.Transcript); err != nil {
		return AgentRun{}, fmt.Errorf("decode run transcript: %w", err)
	}
	return run, nil
}

func (r *PostgresRepository) SaveRun(ctx context.Context, run AgentRun) error {
	transcript, err := json.Marshal(run.Transcript)
	if err != nil {
		return fmt.Errorf("encode run transcript: %w", err)
	}
	result, err := r.db.ExecContext(
		ctx,
		`UPDATE agent_runs
		    SET status = $2,
		        pending_tool_call_id = $3,
		        pending_step_id = $4,
		        latest_generation_id = $5,
		        approved_generation_id = $6,
		        transcript_json = $7,
		        updated_at = $8
		  WHERE id = $1`,
		run.ID,
		run.Status,
		run.PendingToolCallID,
		run.PendingStepID,
		run.LatestGenerationID,
		run.ApprovedGenerationID,
		transcript,
		run.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("update agent run: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read updated row count: %w", err)
	}
	if affected == 0 {
		return ErrRunNotFound
	}
	return nil
}

func (r *PostgresRepository) AppendEvent(ctx context.Context, event Event) (Event, error) {
	payload, err := json.Marshal(event.Payload)
	if err != nil {
		return Event{}, fmt.Errorf("encode agent event payload: %w", err)
	}
	transaction, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return Event{}, fmt.Errorf("begin agent event transaction: %w", err)
	}
	defer transaction.Rollback()

	err = transaction.QueryRowContext(
		ctx,
		`UPDATE agent_runs
		    SET last_event_seq = last_event_seq + 1
		  WHERE id = $1
		  RETURNING last_event_seq`,
		event.RunID,
	).Scan(&event.Seq)
	if errors.Is(err, sql.ErrNoRows) {
		return Event{}, ErrRunNotFound
	}
	if err != nil {
		return Event{}, fmt.Errorf("allocate agent event sequence: %w", err)
	}

	_, err = transaction.ExecContext(
		ctx,
		`INSERT INTO agent_events (
			run_id, seq, event_type, conversation_id, step_id, tool_call_id,
			parent_id, checkpoint_id, payload_json, created_at
		) VALUES ($1, $2, $3, $4, NULLIF($5, ''), NULLIF($6, ''),
		          NULLIF($7, ''), NULLIF($8, ''), $9, $10)`,
		event.RunID,
		event.Seq,
		event.Type,
		event.ConversationID,
		event.StepID,
		event.ToolCallID,
		event.ParentID,
		event.CheckpointID,
		payload,
		event.Timestamp,
	)
	if err != nil {
		return Event{}, fmt.Errorf("insert agent event: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return Event{}, fmt.Errorf("commit agent event: %w", err)
	}
	return event, nil
}

func (r *PostgresRepository) ListEvents(
	ctx context.Context,
	runID string,
	afterSeq int64,
) ([]Event, error) {
	var exists bool
	if err := r.db.QueryRowContext(
		ctx,
		`SELECT EXISTS(SELECT 1 FROM agent_runs WHERE id = $1)`,
		runID,
	).Scan(&exists); err != nil {
		return nil, fmt.Errorf("check agent run: %w", err)
	}
	if !exists {
		return nil, ErrRunNotFound
	}

	rows, err := r.db.QueryContext(
		ctx,
		`SELECT seq, event_type, conversation_id, step_id, tool_call_id,
		        parent_id, checkpoint_id, payload_json, created_at
		   FROM agent_events
		  WHERE run_id = $1 AND seq > $2
		  ORDER BY seq`,
		runID,
		afterSeq,
	)
	if err != nil {
		return nil, fmt.Errorf("select agent events: %w", err)
	}
	defer rows.Close()

	events := make([]Event, 0)
	for rows.Next() {
		event := Event{RunID: runID}
		var stepID, toolCallID, parentID, checkpointID sql.NullString
		var payload []byte
		if err := rows.Scan(
			&event.Seq,
			&event.Type,
			&event.ConversationID,
			&stepID,
			&toolCallID,
			&parentID,
			&checkpointID,
			&payload,
			&event.Timestamp,
		); err != nil {
			return nil, fmt.Errorf("scan agent event: %w", err)
		}
		event.StepID = stepID.String
		event.ToolCallID = toolCallID.String
		event.ParentID = parentID.String
		event.CheckpointID = checkpointID.String
		if err := json.Unmarshal(payload, &event.Payload); err != nil {
			return nil, fmt.Errorf("decode agent event payload: %w", err)
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate agent events: %w", err)
	}
	return events, nil
}
