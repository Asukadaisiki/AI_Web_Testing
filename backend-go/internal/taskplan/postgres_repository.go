package taskplan

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

func (r *PostgresRepository) CreateVersion(
	ctx context.Context,
	plan Plan,
) (Plan, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return Plan{}, fmt.Errorf("begin task plan transaction: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(
		ctx,
		`SELECT pg_advisory_xact_lock(hashtext($1))`,
		plan.RunID,
	); err != nil {
		return Plan{}, fmt.Errorf("lock task plan run: %w", err)
	}
	if err := tx.QueryRowContext(
		ctx,
		`SELECT COALESCE(MAX(version), 0) + 1
		   FROM task_plans
		  WHERE run_id = $1`,
		plan.RunID,
	).Scan(&plan.Version); err != nil {
		return Plan{}, fmt.Errorf("select next task plan version: %w", err)
	}
	if _, err := tx.ExecContext(
		ctx,
		`UPDATE task_plans
		    SET status = 'superseded', updated_at = $2
		  WHERE run_id = $1 AND status <> 'superseded'`,
		plan.RunID,
		plan.UpdatedAt,
	); err != nil {
		return Plan{}, fmt.Errorf("supersede task plan: %w", err)
	}
	forbidden, err := json.Marshal(plan.ForbiddenActions)
	if err != nil {
		return Plan{}, fmt.Errorf("encode forbidden actions: %w", err)
	}
	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO task_plans (
			id, schema_version, run_id, actor_user_id, project_id, version,
			goal, status, plan_sha256, max_side_effect,
			forbidden_actions_json, bound_generation_id, created_at, updated_at
		) VALUES (
			$1, $2, $3, NULLIF($4, 0), NULLIF($5, 0), $6,
			$7, $8, $9, $10, $11, $12, $13, $14
		)`,
		plan.ID,
		plan.SchemaVersion,
		plan.RunID,
		plan.ActorUserID,
		plan.ProjectID,
		plan.Version,
		plan.Goal,
		plan.Status,
		plan.PlanSHA256,
		plan.MaxSideEffect,
		string(forbidden),
		plan.BoundGenerationID,
		plan.CreatedAt,
		plan.UpdatedAt,
	); err != nil {
		return Plan{}, fmt.Errorf("insert task plan: %w", err)
	}
	if err := insertSteps(ctx, tx, plan); err != nil {
		return Plan{}, err
	}
	if err := tx.Commit(); err != nil {
		return Plan{}, fmt.Errorf("commit task plan version: %w", err)
	}
	return plan, nil
}

func (r *PostgresRepository) GetCurrent(
	ctx context.Context,
	runID string,
) (Plan, error) {
	var plan Plan
	var forbidden []byte
	err := r.db.QueryRowContext(
		ctx,
		`SELECT id, schema_version, run_id, COALESCE(actor_user_id, 0),
		        COALESCE(project_id, 0), version, goal, status, plan_sha256,
		        max_side_effect, forbidden_actions_json,
		        bound_generation_id, created_at, updated_at
		   FROM task_plans
		  WHERE run_id = $1
		  ORDER BY version DESC
		  LIMIT 1`,
		runID,
	).Scan(
		&plan.ID,
		&plan.SchemaVersion,
		&plan.RunID,
		&plan.ActorUserID,
		&plan.ProjectID,
		&plan.Version,
		&plan.Goal,
		&plan.Status,
		&plan.PlanSHA256,
		&plan.MaxSideEffect,
		&forbidden,
		&plan.BoundGenerationID,
		&plan.CreatedAt,
		&plan.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Plan{}, ErrNotFound
	}
	if err != nil {
		return Plan{}, fmt.Errorf("select task plan: %w", err)
	}
	if err := json.Unmarshal(forbidden, &plan.ForbiddenActions); err != nil {
		return Plan{}, fmt.Errorf("decode forbidden actions: %w", err)
	}
	steps, err := selectSteps(ctx, r.db, plan.ID)
	if err != nil {
		return Plan{}, err
	}
	plan.Steps = steps
	return plan, nil
}

func (r *PostgresRepository) Save(
	ctx context.Context,
	plan Plan,
) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin task plan save: %w", err)
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(
		ctx,
		`UPDATE task_plans
		    SET status = $2, bound_generation_id = $3, updated_at = $4
		  WHERE id = $1 AND run_id = $5 AND version = $6`,
		plan.ID,
		plan.Status,
		plan.BoundGenerationID,
		plan.UpdatedAt,
		plan.RunID,
		plan.Version,
	)
	if err != nil {
		return fmt.Errorf("update task plan: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read task plan update count: %w", err)
	}
	if affected != 1 {
		return ErrNotFound
	}
	if _, err := tx.ExecContext(
		ctx,
		`DELETE FROM task_plan_steps WHERE plan_id = $1`,
		plan.ID,
	); err != nil {
		return fmt.Errorf("replace task plan steps: %w", err)
	}
	if err := insertSteps(ctx, tx, plan); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit task plan save: %w", err)
	}
	return nil
}

type statementExecutor interface {
	ExecContext(
		ctx context.Context,
		query string,
		args ...any,
	) (sql.Result, error)
}

type rowQueryer interface {
	QueryContext(
		ctx context.Context,
		query string,
		args ...any,
	) (*sql.Rows, error)
}

func insertSteps(
	ctx context.Context,
	executor statementExecutor,
	plan Plan,
) error {
	for _, step := range plan.Steps {
		preconditions, _ := json.Marshal(step.Preconditions)
		completion, _ := json.Marshal(step.CompletionConditions)
		evidence, _ := json.Marshal(step.Evidence)
		if _, err := executor.ExecContext(
			ctx,
			`INSERT INTO task_plan_steps (
				plan_id, step_id, position, intent, action, target, value,
				trigger, context_key, timeout_ms,
				expected_occurrences, idempotency, side_effect,
				preconditions_json, completion_conditions_json, status,
				grounding_attempts, evidence_refs_json
			) VALUES (
				$1, $2, $3, $4, $5, NULLIF($6, ''), NULLIF($7, ''),
				NULLIF($8, ''), NULLIF($9, ''), NULLIF($10, 0),
				$11, $12, $13, $14, $15, $16, $17, $18
			)`,
			plan.ID,
			step.ID,
			step.Position,
			step.Intent,
			step.Action,
			step.Target,
			step.Value,
			step.Trigger,
			step.ContextKey,
			step.TimeoutMS,
			step.ExpectedOccurrences,
			step.Idempotency,
			step.SideEffect,
			string(preconditions),
			string(completion),
			step.Status,
			step.GroundingAttempts,
			string(evidence),
		); err != nil {
			return fmt.Errorf("insert task plan step %q: %w", step.ID, err)
		}
	}
	return nil
}

func selectSteps(
	ctx context.Context,
	queryer rowQueryer,
	planID string,
) ([]Step, error) {
	rows, err := queryer.QueryContext(
		ctx,
		`SELECT step_id, position, intent, action,
		        COALESCE(target, ''), COALESCE(value, ''),
		        COALESCE(trigger, ''), COALESCE(context_key, ''),
		        COALESCE(timeout_ms, 0),
		        expected_occurrences, idempotency, side_effect,
		        preconditions_json, completion_conditions_json,
		        status, grounding_attempts, evidence_refs_json
		   FROM task_plan_steps
		  WHERE plan_id = $1
		  ORDER BY position`,
		planID,
	)
	if err != nil {
		return nil, fmt.Errorf("select task plan steps: %w", err)
	}
	defer rows.Close()
	var steps []Step
	for rows.Next() {
		var step Step
		var preconditions, completion, evidence []byte
		if err := rows.Scan(
			&step.ID,
			&step.Position,
			&step.Intent,
			&step.Action,
			&step.Target,
			&step.Value,
			&step.Trigger,
			&step.ContextKey,
			&step.TimeoutMS,
			&step.ExpectedOccurrences,
			&step.Idempotency,
			&step.SideEffect,
			&preconditions,
			&completion,
			&step.Status,
			&step.GroundingAttempts,
			&evidence,
		); err != nil {
			return nil, fmt.Errorf("scan task plan step: %w", err)
		}
		if err := json.Unmarshal(preconditions, &step.Preconditions); err != nil {
			return nil, fmt.Errorf("decode task plan preconditions: %w", err)
		}
		if err := json.Unmarshal(completion, &step.CompletionConditions); err != nil {
			return nil, fmt.Errorf("decode task plan completion conditions: %w", err)
		}
		if err := json.Unmarshal(evidence, &step.Evidence); err != nil {
			return nil, fmt.Errorf("decode task plan evidence: %w", err)
		}
		steps = append(steps, step)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate task plan steps: %w", err)
	}
	return steps, nil
}

var _ Repository = (*PostgresRepository)(nil)
