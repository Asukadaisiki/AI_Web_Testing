package groundingplan

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

type PostgresRepository struct {
	db *sql.DB
}

func NewPostgresRepository(db *sql.DB) *PostgresRepository {
	return &PostgresRepository{db: db}
}

func (r *PostgresRepository) CreateInitial(
	ctx context.Context,
	plan Plan,
) (Plan, error) {
	if err := validatePlan(plan); err != nil {
		return Plan{}, err
	}
	if plan.Revision != 1 {
		return Plan{}, ErrConflict
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return Plan{}, fmt.Errorf("begin grounding plan transaction: %w", err)
	}
	defer tx.Rollback()
	if err := lockRun(ctx, tx, plan.RunID); err != nil {
		return Plan{}, err
	}
	existing, err := selectCurrent(ctx, tx, plan.TaskPlanID, false)
	if err == nil {
		return existing, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return Plan{}, err
	}
	if err := insertPlan(ctx, tx, plan); err != nil {
		return Plan{}, err
	}
	if err := tx.Commit(); err != nil {
		return Plan{}, fmt.Errorf("commit initial grounding plan: %w", err)
	}
	return plan, nil
}

func (r *PostgresRepository) ReplaceForTaskPlan(
	ctx context.Context,
	plan Plan,
) (Plan, error) {
	if err := validatePlan(plan); err != nil {
		return Plan{}, err
	}
	if plan.Revision != 1 {
		return Plan{}, ErrConflict
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return Plan{}, fmt.Errorf("begin grounding plan replacement: %w", err)
	}
	defer tx.Rollback()
	if err := lockRun(ctx, tx, plan.RunID); err != nil {
		return Plan{}, err
	}
	existing, err := selectCurrent(ctx, tx, plan.TaskPlanID, false)
	if err == nil {
		return existing, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return Plan{}, err
	}

	previous, err := selectCurrent(ctx, tx, plan.RunID, true)
	if err == nil {
		if previous.TaskPlanVersion > plan.TaskPlanVersion {
			return Plan{}, ErrConflict
		}
		if previous.TaskPlanID != plan.TaskPlanID &&
			previous.Status != StatusSuperseded {
			superseded, err := supersededRevision(previous, plan.UpdatedAt)
			if err != nil {
				return Plan{}, err
			}
			if err := insertPlan(ctx, tx, superseded); err != nil {
				return Plan{}, err
			}
		}
	} else if !errors.Is(err, ErrNotFound) {
		return Plan{}, err
	}
	if err := insertPlan(ctx, tx, plan); err != nil {
		return Plan{}, err
	}
	if err := tx.Commit(); err != nil {
		return Plan{}, fmt.Errorf("commit grounding plan replacement: %w", err)
	}
	return plan, nil
}

func (r *PostgresRepository) AppendRevision(
	ctx context.Context,
	plan Plan,
) (Plan, error) {
	if err := validatePlan(plan); err != nil {
		return Plan{}, err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return Plan{}, fmt.Errorf("begin grounding plan revision: %w", err)
	}
	defer tx.Rollback()
	if err := lockRun(ctx, tx, plan.RunID); err != nil {
		return Plan{}, err
	}
	current, err := selectCurrent(ctx, tx, plan.TaskPlanID, false)
	if err != nil {
		return Plan{}, err
	}
	if plan.Revision != current.Revision+1 ||
		plan.TaskPlanBinding() != current.TaskPlanBinding() ||
		plan.RunID != current.RunID {
		return Plan{}, ErrConflict
	}
	if err := insertPlan(ctx, tx, plan); err != nil {
		return Plan{}, err
	}
	if err := tx.Commit(); err != nil {
		return Plan{}, fmt.Errorf("commit grounding plan revision: %w", err)
	}
	return plan, nil
}

func (r *PostgresRepository) GetCurrent(
	ctx context.Context,
	key string,
) (Plan, error) {
	plan, err := selectCurrent(ctx, r.db, key, false)
	if !errors.Is(err, ErrNotFound) {
		return plan, err
	}
	return selectCurrent(ctx, r.db, key, true)
}

func (r *PostgresRepository) SupersedeForTaskPlan(
	ctx context.Context,
	taskPlanID string,
	at time.Time,
) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin grounding plan supersede: %w", err)
	}
	defer tx.Rollback()
	var runID string
	if err := tx.QueryRowContext(
		ctx,
		`SELECT run_id FROM task_plans WHERE id = $1`,
		taskPlanID,
	).Scan(&runID); errors.Is(err, sql.ErrNoRows) {
		return nil
	} else if err != nil {
		return fmt.Errorf("select task plan run for supersede: %w", err)
	}
	if err := lockRun(ctx, tx, runID); err != nil {
		return err
	}
	plan, err := selectCurrent(ctx, tx, taskPlanID, false)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if plan.Status != StatusSuperseded {
		plan, err = supersededRevision(plan, at)
		if err != nil {
			return err
		}
		if err := insertPlan(ctx, tx, plan); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit grounding plan supersede: %w", err)
	}
	return nil
}

func supersededRevision(plan Plan, at time.Time) (Plan, error) {
	plan.Revision++
	plan.Status = StatusSuperseded
	plan.UpdatedAt = at.UTC()
	plan.ID = revisionID(plan, plan.UpdatedAt)
	hash, err := contentHash(plan)
	if err != nil {
		return Plan{}, err
	}
	plan.ContentSHA256 = hash
	if err := validatePlan(plan); err != nil {
		return Plan{}, err
	}
	return plan, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

type queryRower interface {
	QueryRowContext(
		context.Context,
		string,
		...any,
	) *sql.Row
}

func selectCurrent(
	ctx context.Context,
	queryer queryRower,
	key string,
	byRun bool,
) (Plan, error) {
	where := "task_plan_id = $1"
	order := "revision DESC"
	if byRun {
		where = "run_id = $1"
		order = "task_plan_version DESC, revision DESC"
	}
	row := queryer.QueryRowContext(
		ctx,
		`SELECT id, schema_version, run_id, task_plan_id,
		        task_plan_version, revision, status,
		        COALESCE(current_plan_step_id, ''), content_json,
		        content_sha256, created_at, updated_at
		   FROM grounding_plans
		  WHERE `+where+`
		  ORDER BY `+order+`
		  LIMIT 1`,
		key,
	)
	return scanPlan(row)
}

func scanPlan(row rowScanner) (Plan, error) {
	var (
		plan              Plan
		raw               []byte
		schemaVersion     string
		runID             string
		taskPlanID        string
		status            string
		currentPlanStepID string
		contentSHA256     string
		taskPlanVersion   int
		revision          int
		createdAt         time.Time
		updatedAt         time.Time
	)
	err := row.Scan(
		&plan.ID,
		&schemaVersion,
		&runID,
		&taskPlanID,
		&taskPlanVersion,
		&revision,
		&status,
		&currentPlanStepID,
		&raw,
		&contentSHA256,
		&createdAt,
		&updatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Plan{}, ErrNotFound
	}
	if err != nil {
		return Plan{}, fmt.Errorf("select grounding plan: %w", err)
	}
	if err := json.Unmarshal(raw, &plan); err != nil {
		return Plan{}, fmt.Errorf("decode grounding plan: %w", err)
	}
	if plan.SchemaVersion != schemaVersion ||
		plan.RunID != runID ||
		plan.TaskPlanID != taskPlanID ||
		plan.TaskPlanVersion != taskPlanVersion ||
		plan.Revision != revision ||
		string(plan.Status) != status ||
		plan.CurrentPlanStepID != currentPlanStepID ||
		plan.ContentSHA256 != contentSHA256 {
		return Plan{}, errors.New("grounding plan columns do not match content_json")
	}
	if err := validatePlan(plan); err != nil {
		return Plan{}, fmt.Errorf("validate persisted grounding plan: %w", err)
	}
	return plan, nil
}

type statementExecutor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func lockRun(
	ctx context.Context,
	executor statementExecutor,
	runID string,
) error {
	if _, err := executor.ExecContext(
		ctx,
		`SELECT pg_advisory_xact_lock(hashtext($1))`,
		runID,
	); err != nil {
		return fmt.Errorf("lock grounding plan run: %w", err)
	}
	return nil
}

func insertPlan(
	ctx context.Context,
	executor statementExecutor,
	plan Plan,
) error {
	raw, err := json.Marshal(plan)
	if err != nil {
		return fmt.Errorf("encode grounding plan: %w", err)
	}
	if _, err := executor.ExecContext(
		ctx,
		`INSERT INTO grounding_plans (
			id, schema_version, run_id, task_plan_id, task_plan_version,
			revision, status, current_plan_step_id, content_json,
			content_sha256, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, NULLIF($8, ''), $9, $10, $11, $12
		)`,
		plan.ID,
		plan.SchemaVersion,
		plan.RunID,
		plan.TaskPlanID,
		plan.TaskPlanVersion,
		plan.Revision,
		plan.Status,
		plan.CurrentPlanStepID,
		string(raw),
		plan.ContentSHA256,
		plan.CreatedAt,
		plan.UpdatedAt,
	); err != nil {
		return fmt.Errorf("insert grounding plan revision: %w", err)
	}
	return nil
}

var _ Repository = (*PostgresRepository)(nil)
