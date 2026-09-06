package research

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

type PostgresRepository struct {
	db  *sql.DB
	now func() time.Time
}

var _ Repository = (*PostgresRepository)(nil)

func NewPostgresRepository(db *sql.DB) *PostgresRepository {
	return &PostgresRepository{db: db, now: time.Now}
}

func (r *PostgresRepository) CreateExperiment(
	ctx context.Context,
	experiment Experiment,
) (Experiment, error) {
	now := r.now().UTC()
	if experiment.CreatedAt.IsZero() {
		experiment.CreatedAt = now
	}
	if experiment.UpdatedAt.IsZero() {
		experiment.UpdatedAt = now
	}
	if err := experiment.NormalizeAndValidate(); err != nil {
		return Experiment{}, err
	}
	row := r.db.QueryRowContext(ctx, `
		INSERT INTO research_experiments (
			id, project_id, name, goal, dataset_version, model_provider,
			model_name, model_version, prompt_version, browser_name,
			browser_version, viewport_json, code_sha256, policy_version,
			observation_profile, dsl_profile, seed, variant, repetitions,
			status, config_json, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13,
			$14, $15, $16, $17, $18, $19, $20, $21, $22, $23
		)
		RETURNING id, project_id, name, goal, dataset_version, model_provider,
		          model_name, model_version, prompt_version, browser_name,
		          browser_version, viewport_json, code_sha256, policy_version,
		          observation_profile, dsl_profile, seed, variant, repetitions,
		          status, config_json, created_at, updated_at`,
		experiment.ID, experiment.ProjectID, experiment.Name, experiment.Goal,
		experiment.DatasetVersion, experiment.ModelProvider, experiment.ModelName,
		experiment.ModelVersion, experiment.PromptVersion, experiment.BrowserName,
		experiment.BrowserVersion, string(experiment.ViewportJSON),
		experiment.CodeSHA256, experiment.PolicyVersion,
		experiment.ObservationProfile, experiment.DSLProfile, experiment.Seed,
		experiment.Variant, experiment.Repetitions, experiment.Status,
		string(experiment.ConfigJSON), experiment.CreatedAt, experiment.UpdatedAt,
	)
	result, err := scanExperiment(row)
	if err != nil {
		return Experiment{}, fmt.Errorf(
			"create research experiment: %w",
			classifyPersistenceError(err),
		)
	}
	return result, nil
}

func (r *PostgresRepository) GetExperiment(
	ctx context.Context,
	experimentID string,
) (Experiment, error) {
	result, err := scanExperiment(r.db.QueryRowContext(ctx, `
		SELECT id, project_id, name, goal, dataset_version, model_provider,
		       model_name, model_version, prompt_version, browser_name,
		       browser_version, viewport_json, code_sha256, policy_version,
		       observation_profile, dsl_profile, seed, variant, repetitions,
		       status, config_json, created_at, updated_at
		FROM research_experiments
		WHERE id = $1`, experimentID))
	if errors.Is(err, sql.ErrNoRows) {
		return Experiment{}, ErrNotFound
	}
	if err != nil {
		return Experiment{}, fmt.Errorf("get research experiment: %w", err)
	}
	return result, nil
}

func (r *PostgresRepository) ListExperiments(
	ctx context.Context,
	filter ExperimentFilter,
) ([]Experiment, error) {
	limit, offset := pagination(filter.Limit, filter.Offset)
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, project_id, name, goal, dataset_version, model_provider,
		       model_name, model_version, prompt_version, browser_name,
		       browser_version, viewport_json, code_sha256, policy_version,
		       observation_profile, dsl_profile, seed, variant, repetitions,
		       status, config_json, created_at, updated_at
		FROM research_experiments
		WHERE ($1::bigint IS NULL OR project_id = $1)
		  AND ($2::text IS NULL OR status = $2)
		  AND ($3::text IS NULL OR variant = $3)
		ORDER BY created_at DESC, id
		LIMIT $4 OFFSET $5`,
		filter.ProjectID, filter.Status, filter.Variant, limit, offset,
	)
	if err != nil {
		return nil, fmt.Errorf("list research experiments: %w", err)
	}
	defer rows.Close()
	result := make([]Experiment, 0)
	for rows.Next() {
		item, err := scanExperiment(rows)
		if err != nil {
			return nil, fmt.Errorf("scan research experiment: %w", err)
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (r *PostgresRepository) CompareAndSwapExperimentStatus(
	ctx context.Context,
	experimentID string,
	from, to ExperimentStatus,
	updatedAt time.Time,
) (Experiment, error) {
	if !validExperimentStatus(from) || !validExperimentStatus(to) || !from.CanTransition(to) {
		return Experiment{}, ErrInvalidStatus
	}
	updatedAt = r.resolveTime(updatedAt)
	result, err := scanExperiment(r.db.QueryRowContext(ctx, `
		UPDATE research_experiments
		SET status = $3::text,
		    updated_at = CASE
		      WHEN $2::text <> $3::text THEN $4
		      ELSE updated_at
		    END
		WHERE id = $1 AND status = $2::text
		RETURNING id, project_id, name, goal, dataset_version, model_provider,
		          model_name, model_version, prompt_version, browser_name,
		          browser_version, viewport_json, code_sha256, policy_version,
		          observation_profile, dsl_profile, seed, variant, repetitions,
		          status, config_json, created_at, updated_at`,
		experimentID, from, to, updatedAt,
	))
	if errors.Is(err, sql.ErrNoRows) {
		return Experiment{}, r.classifyExperimentCAS(ctx, experimentID, from, to)
	}
	if err != nil {
		return Experiment{}, fmt.Errorf("update research experiment status: %w", err)
	}
	return result, nil
}

func (r *PostgresRepository) classifyExperimentCAS(
	ctx context.Context,
	id string,
	from, to ExperimentStatus,
) error {
	current, err := r.GetExperiment(ctx, id)
	if err != nil {
		return err
	}
	if current.Status.Terminal() && current.Status != to {
		return ErrTerminalStatus
	}
	if current.Status != from {
		return ErrConflict
	}
	return ErrInvalidStatus
}

func (r *PostgresRepository) DeleteExperiment(ctx context.Context, id string) error {
	result, err := r.db.ExecContext(ctx, `DELETE FROM research_experiments WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete research experiment: %w", classifyPersistenceError(err))
	}
	return requireAffected(result)
}

func (r *PostgresRepository) CreateRun(
	ctx context.Context,
	run ResearchRun,
) (ResearchRun, error) {
	now := r.now().UTC()
	if run.CreatedAt.IsZero() {
		run.CreatedAt = now
	}
	if run.UpdatedAt.IsZero() {
		run.UpdatedAt = now
	}
	if err := run.NormalizeAndValidate(); err != nil {
		return ResearchRun{}, err
	}
	metrics, err := marshalMetrics(run.Metrics)
	if err != nil {
		return ResearchRun{}, err
	}
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return ResearchRun{}, fmt.Errorf("begin research run transaction: %w", err)
	}
	defer tx.Rollback()
	if err := lockRunCreateKeys(ctx, tx, run); err != nil {
		return ResearchRun{}, fmt.Errorf("lock research run create keys: %w", err)
	}
	persisted, err := getRunByIdempotencyKey(
		ctx, tx, run.ExperimentID, run.IdempotencyKey,
	)
	if err == nil {
		if !sameRunImmutablePayload(persisted, run) {
			return ResearchRun{}, fmt.Errorf(
				"create research run: %w: %s",
				ErrConflict,
				"uq_research_runs_experiment_idempotency",
			)
		}
		if err := tx.Commit(); err != nil {
			return ResearchRun{}, fmt.Errorf("commit research run replay: %w", err)
		}
		return persisted, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return ResearchRun{}, fmt.Errorf("read research run identity: %w", err)
	}
	if err := validateExperimentProject(ctx, tx, run.ExperimentID, run.ProjectID); err != nil {
		return ResearchRun{}, err
	}
	if err := validateRunLinks(ctx, tx, run.ProjectID, run.Links); err != nil {
		return ResearchRun{}, err
	}
	if err := ensureRunCreateKeysAvailable(ctx, tx, run); err != nil {
		return ResearchRun{}, err
	}
	row := tx.QueryRowContext(ctx, `
		INSERT INTO research_runs (
			id, experiment_id, project_id, idempotency_key, repetition_index,
			warmup, status, schema_version, projector_version, metric_version,
			policy_version, agent_run_id, generation_id, batch_id, execution_id,
			dsl_sha256, metrics_json, started_at, finished_at, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11,
			$12, $13, $14, $15, $16, $17, $18, $19, $20, $21
		)
		RETURNING `+runColumns, runArgs(run, metrics)...)
	persisted, err = scanRun(row)
	if err != nil {
		return ResearchRun{}, fmt.Errorf(
			"create research run: %w",
			classifyRunCreateError(err),
		)
	}
	if err := tx.Commit(); err != nil {
		return ResearchRun{}, fmt.Errorf("commit research run: %w", err)
	}
	return persisted, nil
}

func (r *PostgresRepository) GetRun(ctx context.Context, runID string) (ResearchRun, error) {
	result, err := scanRun(r.db.QueryRowContext(ctx,
		`SELECT `+runColumns+` FROM research_runs WHERE id = $1`, runID))
	if errors.Is(err, sql.ErrNoRows) {
		return ResearchRun{}, ErrNotFound
	}
	if err != nil {
		return ResearchRun{}, fmt.Errorf("get research run: %w", err)
	}
	return result, nil
}

func (r *PostgresRepository) ListRuns(
	ctx context.Context,
	filter RunFilter,
) ([]ResearchRun, error) {
	limit, offset := pagination(filter.Limit, filter.Offset)
	rows, err := r.db.QueryContext(ctx, `
		SELECT `+runColumns+`
		FROM research_runs
		WHERE ($1::text IS NULL OR experiment_id = $1)
		  AND ($2::bigint IS NULL OR project_id = $2)
		  AND ($3::text IS NULL OR status = $3)
		  AND ($4::text IS NULL OR agent_run_id = $4)
		ORDER BY created_at DESC, id
		LIMIT $5 OFFSET $6`,
		filter.ExperimentID, filter.ProjectID, filter.Status, filter.AgentRunID,
		limit, offset,
	)
	if err != nil {
		return nil, fmt.Errorf("list research runs: %w", err)
	}
	defer rows.Close()
	result := make([]ResearchRun, 0)
	for rows.Next() {
		item, err := scanRun(rows)
		if err != nil {
			return nil, fmt.Errorf("scan research run: %w", err)
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (r *PostgresRepository) CompareAndSwapRunStatus(
	ctx context.Context,
	runID string,
	from, to RunStatus,
	changedAt time.Time,
) (ResearchRun, error) {
	if !validRunStatus(from) || !validRunStatus(to) || !from.CanTransition(to) {
		return ResearchRun{}, ErrInvalidStatus
	}
	changedAt = r.resolveTime(changedAt)
	result, err := scanRun(r.db.QueryRowContext(ctx, `
		UPDATE research_runs
		SET status = $3::text,
		    started_at = CASE
		      WHEN $2::text <> $3::text AND $3::text = 'running'
		        THEN COALESCE(started_at, $4)
		      ELSE started_at
		    END,
		    finished_at = CASE
		      WHEN $2::text <> $3::text
		        AND $3::text IN ('completed', 'failed', 'cancelled') THEN $4
		      ELSE finished_at
		    END,
		    updated_at = CASE
		      WHEN $2::text <> $3::text THEN $4
		      ELSE updated_at
		    END
		WHERE id = $1 AND status = $2::text
		RETURNING `+runColumns,
		runID, from, to, changedAt,
	))
	if errors.Is(err, sql.ErrNoRows) {
		return ResearchRun{}, r.classifyRunCAS(ctx, runID, from, to)
	}
	if err != nil {
		return ResearchRun{}, fmt.Errorf("update research run status: %w", err)
	}
	return result, nil
}

func (r *PostgresRepository) classifyRunCAS(
	ctx context.Context,
	id string,
	from, to RunStatus,
) error {
	current, err := r.GetRun(ctx, id)
	if err != nil {
		return err
	}
	if current.Status.Terminal() && current.Status != to {
		return ErrTerminalStatus
	}
	if current.Status != from {
		return ErrConflict
	}
	return ErrInvalidStatus
}

func (r *PostgresRepository) UpdateRunLinks(
	ctx context.Context,
	runID string,
	links RunLinks,
	updatedAt time.Time,
) (ResearchRun, error) {
	if err := links.NormalizeAndValidate(); err != nil {
		return ResearchRun{}, err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return ResearchRun{}, fmt.Errorf("begin research link transaction: %w", err)
	}
	defer tx.Rollback()
	current, err := getRunForUpdate(ctx, tx, runID)
	if errors.Is(err, sql.ErrNoRows) {
		return ResearchRun{}, ErrNotFound
	}
	if err != nil {
		return ResearchRun{}, fmt.Errorf("lock research run: %w", err)
	}
	if !linksExtend(current.Links, links) {
		return ResearchRun{}, ErrConflict
	}
	if err := validateRunLinks(ctx, tx, current.ProjectID, links); err != nil {
		return ResearchRun{}, err
	}
	updatedAt = r.resolveTime(updatedAt)
	result, err := scanRun(tx.QueryRowContext(ctx, `
		UPDATE research_runs
		SET agent_run_id = $2, generation_id = $3, batch_id = $4,
		    execution_id = $5, dsl_sha256 = $6, updated_at = $7
		WHERE id = $1
		RETURNING `+runColumns,
		runID, links.AgentRunID, links.GenerationID, links.BatchID,
		links.ExecutionID, links.DSLSHA256, updatedAt,
	))
	if err != nil {
		return ResearchRun{}, fmt.Errorf("update research run links: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return ResearchRun{}, fmt.Errorf("commit research run links: %w", err)
	}
	return result, nil
}

func (r *PostgresRepository) PutRunMetrics(
	ctx context.Context,
	runID string,
	metrics RunMetrics,
	updatedAt time.Time,
) (ResearchRun, error) {
	if err := metrics.Validate(); err != nil {
		return ResearchRun{}, err
	}
	raw, err := json.Marshal(metrics)
	if err != nil {
		return ResearchRun{}, fmt.Errorf("encode research metrics: %w", err)
	}
	updatedAt = r.resolveTime(updatedAt)
	result, err := scanRun(r.db.QueryRowContext(ctx, `
		UPDATE research_runs
		SET metrics_json = $2, updated_at = $3
		WHERE id = $1
		RETURNING `+runColumns,
		runID, string(raw), updatedAt,
	))
	if errors.Is(err, sql.ErrNoRows) {
		return ResearchRun{}, ErrNotFound
	}
	if err != nil {
		return ResearchRun{}, fmt.Errorf("put research run metrics: %w", err)
	}
	return result, nil
}

func (r *PostgresRepository) DeleteRun(ctx context.Context, id string) error {
	result, err := r.db.ExecContext(ctx, `DELETE FROM research_runs WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete research run: %w", classifyPersistenceError(err))
	}
	return requireAffected(result)
}

func (r *PostgresRepository) AppendTransitions(
	ctx context.Context,
	runID string,
	transitions []Transition,
) ([]Transition, error) {
	if len(transitions) == 0 {
		return []Transition{}, nil
	}
	for index := range transitions {
		if transitions[index].ResearchRunID == "" {
			transitions[index].ResearchRunID = runID
		}
		if transitions[index].ResearchRunID != runID {
			return nil, fmt.Errorf("%w: transition run mismatch", ErrInvalid)
		}
		if transitions[index].CreatedAt.IsZero() {
			transitions[index].CreatedAt = r.now().UTC()
		}
		if err := transitions[index].NormalizeAndValidate(); err != nil {
			return nil, err
		}
		if index > 0 && transitions[index].Ordinal != transitions[index-1].Ordinal+1 {
			return nil, fmt.Errorf("%w: transition batch is not contiguous", ErrConflict)
		}
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin transition transaction: %w", err)
	}
	defer tx.Rollback()
	var exists bool
	if err := tx.QueryRowContext(ctx,
		`SELECT true FROM research_runs WHERE id = $1 FOR UPDATE`, runID,
	).Scan(&exists); errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	} else if err != nil {
		return nil, fmt.Errorf("lock research run: %w", err)
	}
	var lastOrdinal int64
	if err := tx.QueryRowContext(ctx, `
		SELECT COALESCE(MAX(ordinal), -1)
		FROM research_transitions
		WHERE research_run_id = $1`, runID,
	).Scan(&lastOrdinal); err != nil {
		return nil, fmt.Errorf("read transition ordinal: %w", err)
	}
	result := make([]Transition, 0, len(transitions))
	for _, transition := range transitions {
		existing, found, err := findTransitionByAppendKey(
			ctx, tx, runID, transition.AppendKey,
		)
		if err != nil {
			return nil, err
		}
		if found {
			if existing.Ordinal != transition.Ordinal ||
				existing.ContentSHA256 != transition.ContentSHA256 {
				return nil, fmt.Errorf("%w: append key reused with different content", ErrConflict)
			}
			result = append(result, existing)
			continue
		}
		if transition.Ordinal != lastOrdinal+1 {
			return nil, fmt.Errorf(
				"%w: transition ordinal %d must follow %d",
				ErrConflict, transition.Ordinal, lastOrdinal,
			)
		}
		artifacts, err := json.Marshal(transition.ArtifactRefs)
		if err != nil {
			return nil, fmt.Errorf("encode artifact references: %w", err)
		}
		persisted, err := scanTransition(tx.QueryRowContext(ctx, `
			INSERT INTO research_transitions (
				research_run_id, ordinal, append_key, content_sha256,
				schema_version, transition_json, artifact_refs_json, created_at
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
			RETURNING `+transitionColumns,
			runID, transition.Ordinal, transition.AppendKey,
			transition.ContentSHA256, transition.SchemaVersion,
			string(transition.PayloadJSON), string(artifacts), transition.CreatedAt,
		))
		if err != nil {
			return nil, fmt.Errorf(
				"append research transition: %w",
				classifyPersistenceError(err),
			)
		}
		lastOrdinal = transition.Ordinal
		result = append(result, persisted)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit research transitions: %w", err)
	}
	return result, nil
}

func (r *PostgresRepository) ListTransitions(
	ctx context.Context,
	filter TransitionFilter,
) ([]Transition, error) {
	if strings.TrimSpace(filter.ResearchRunID) == "" {
		return nil, fmt.Errorf("%w: research_run_id", ErrInvalid)
	}
	limit, _ := pagination(filter.Limit, 0)
	rows, err := r.db.QueryContext(ctx, `
		SELECT `+transitionColumns+`
		FROM research_transitions
		WHERE research_run_id = $1
		  AND ($2::bigint IS NULL OR ordinal > $2)
		ORDER BY ordinal
		LIMIT $3`,
		filter.ResearchRunID, filter.AfterOrdinal, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list research transitions: %w", err)
	}
	defer rows.Close()
	result := make([]Transition, 0)
	for rows.Next() {
		item, err := scanTransition(rows)
		if err != nil {
			return nil, fmt.Errorf("scan research transition: %w", err)
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (r *PostgresRepository) DeleteTransitions(ctx context.Context, runID string) error {
	if _, err := r.GetRun(ctx, runID); err != nil {
		return err
	}
	if _, err := r.db.ExecContext(ctx,
		`DELETE FROM research_transitions WHERE research_run_id = $1`, runID,
	); err != nil {
		return fmt.Errorf("delete research transitions: %w", err)
	}
	return nil
}

const runColumns = `
	id, experiment_id, project_id, idempotency_key, repetition_index,
	warmup, status, schema_version, projector_version, metric_version,
	policy_version, agent_run_id, generation_id, batch_id, execution_id,
	dsl_sha256, metrics_json, started_at, finished_at, created_at, updated_at`

const transitionColumns = `
	id, research_run_id, ordinal, append_key, content_sha256, schema_version,
	transition_json, artifact_refs_json, created_at`

type rowScanner interface {
	Scan(...any) error
}

func scanExperiment(row rowScanner) (Experiment, error) {
	var result Experiment
	var viewport, config []byte
	err := row.Scan(
		&result.ID, &result.ProjectID, &result.Name, &result.Goal,
		&result.DatasetVersion, &result.ModelProvider, &result.ModelName,
		&result.ModelVersion, &result.PromptVersion, &result.BrowserName,
		&result.BrowserVersion, &viewport, &result.CodeSHA256,
		&result.PolicyVersion, &result.ObservationProfile, &result.DSLProfile,
		&result.Seed, &result.Variant, &result.Repetitions, &result.Status,
		&config, &result.CreatedAt, &result.UpdatedAt,
	)
	if err != nil {
		return Experiment{}, err
	}
	result.ViewportJSON = json.RawMessage(viewport)
	result.ConfigJSON = json.RawMessage(config)
	if err := result.NormalizeAndValidate(); err != nil {
		return Experiment{}, fmt.Errorf("validate persisted experiment: %w", err)
	}
	return result, nil
}

func scanRun(row rowScanner) (ResearchRun, error) {
	var result ResearchRun
	var agentRunID, dslSHA sql.NullString
	var generationID, batchID, executionID sql.NullInt64
	var metrics []byte
	var startedAt, finishedAt sql.NullTime
	err := row.Scan(
		&result.ID, &result.ExperimentID, &result.ProjectID,
		&result.IdempotencyKey, &result.RepetitionIndex, &result.Warmup,
		&result.Status, &result.Versions.SchemaVersion,
		&result.Versions.ProjectorVersion, &result.Versions.MetricVersion,
		&result.Versions.PolicyVersion, &agentRunID, &generationID, &batchID,
		&executionID, &dslSHA, &metrics, &startedAt, &finishedAt,
		&result.CreatedAt, &result.UpdatedAt,
	)
	if err != nil {
		return ResearchRun{}, err
	}
	result.Links.AgentRunID = stringPtr(agentRunID)
	result.Links.GenerationID = int64Ptr(generationID)
	result.Links.BatchID = int64Ptr(batchID)
	result.Links.ExecutionID = int64Ptr(executionID)
	result.Links.DSLSHA256 = stringPtr(dslSHA)
	result.StartedAt = timePtr(startedAt)
	result.FinishedAt = timePtr(finishedAt)
	result.CreatedAt = result.CreatedAt.UTC()
	result.UpdatedAt = result.UpdatedAt.UTC()
	if len(metrics) > 0 {
		var decoded RunMetrics
		if err := json.Unmarshal(metrics, &decoded); err != nil {
			return ResearchRun{}, fmt.Errorf("decode metrics: %w", err)
		}
		result.Metrics = &decoded
	}
	if err := result.NormalizeAndValidate(); err != nil {
		return ResearchRun{}, fmt.Errorf("validate persisted research run: %w", err)
	}
	return result, nil
}

func scanTransition(row rowScanner) (Transition, error) {
	var result Transition
	var payload, artifacts []byte
	err := row.Scan(
		&result.ID, &result.ResearchRunID, &result.Ordinal, &result.AppendKey,
		&result.ContentSHA256, &result.SchemaVersion, &payload, &artifacts,
		&result.CreatedAt,
	)
	if err != nil {
		return Transition{}, err
	}
	result.PayloadJSON = json.RawMessage(payload)
	if err := json.Unmarshal(artifacts, &result.ArtifactRefs); err != nil {
		return Transition{}, fmt.Errorf("decode artifact references: %w", err)
	}
	if err := result.NormalizeAndValidate(); err != nil {
		return Transition{}, fmt.Errorf("validate persisted transition: %w", err)
	}
	return result, nil
}

func getRunForUpdate(
	ctx context.Context,
	tx *sql.Tx,
	runID string,
) (ResearchRun, error) {
	return scanRun(tx.QueryRowContext(ctx,
		`SELECT `+runColumns+` FROM research_runs WHERE id = $1 FOR UPDATE`,
		runID,
	))
}

func getRunByIdempotencyKey(
	ctx context.Context,
	tx *sql.Tx,
	experimentID, key string,
) (ResearchRun, error) {
	return scanRun(tx.QueryRowContext(ctx, `
		SELECT `+runColumns+`
		FROM research_runs
		WHERE experiment_id = $1 AND idempotency_key = $2`,
		experimentID, key,
	))
}

func lockRunCreateKeys(ctx context.Context, tx *sql.Tx, run ResearchRun) error {
	keyNames := []string{
		fmt.Sprintf(
			"research-run/identity/%d:%s/%d:%s",
			len(run.ExperimentID), run.ExperimentID,
			len(run.IdempotencyKey), run.IdempotencyKey,
		),
		fmt.Sprintf("research-run/id/%d:%s", len(run.ID), run.ID),
		fmt.Sprintf(
			"research-run/repetition/%d:%s/%d/%t",
			len(run.ExperimentID), run.ExperimentID,
			run.RepetitionIndex, run.Warmup,
		),
	}
	keys := make([]int64, 0, len(keyNames))
	for _, name := range keyNames {
		digest := sha256.Sum256([]byte(name))
		keys = append(keys, int64(binary.BigEndian.Uint64(digest[:8])))
	}
	slices.Sort(keys)
	for index, key := range keys {
		if index > 0 && key == keys[index-1] {
			continue
		}
		if _, err := tx.ExecContext(ctx, `
			SELECT pg_advisory_xact_lock($1::bigint)`,
			key,
		); err != nil {
			return err
		}
	}
	return nil
}

func ensureRunCreateKeysAvailable(
	ctx context.Context,
	tx *sql.Tx,
	run ResearchRun,
) error {
	var conflictingID string
	err := tx.QueryRowContext(ctx,
		`SELECT id FROM research_runs WHERE id = $1`,
		run.ID,
	).Scan(&conflictingID)
	switch {
	case err == nil:
		return fmt.Errorf("%w: pk_research_runs", ErrConflict)
	case !errors.Is(err, sql.ErrNoRows):
		return fmt.Errorf("check research run primary key: %w", err)
	}
	err = tx.QueryRowContext(ctx, `
		SELECT id
		FROM research_runs
		WHERE experiment_id = $1 AND repetition_index = $2 AND warmup = $3`,
		run.ExperimentID, run.RepetitionIndex, run.Warmup,
	).Scan(&conflictingID)
	switch {
	case err == nil:
		return fmt.Errorf(
			"%w: uq_research_runs_experiment_repetition_warmup",
			ErrConflict,
		)
	case !errors.Is(err, sql.ErrNoRows):
		return fmt.Errorf("check research run repetition: %w", err)
	default:
		return nil
	}
}

func findTransitionByAppendKey(
	ctx context.Context,
	tx *sql.Tx,
	runID, appendKey string,
) (Transition, bool, error) {
	result, err := scanTransition(tx.QueryRowContext(ctx, `
		SELECT `+transitionColumns+`
		FROM research_transitions
		WHERE research_run_id = $1 AND append_key = $2`,
		runID, appendKey,
	))
	if errors.Is(err, sql.ErrNoRows) {
		return Transition{}, false, nil
	}
	if err != nil {
		return Transition{}, false, fmt.Errorf("find research transition: %w", err)
	}
	return result, true, nil
}

func validateExperimentProject(
	ctx context.Context,
	tx *sql.Tx,
	experimentID string,
	projectID int64,
) error {
	var persistedProjectID int64
	if err := tx.QueryRowContext(ctx, `
		SELECT project_id
		FROM research_experiments
		WHERE id = $1
		FOR SHARE`, experimentID,
	).Scan(&persistedProjectID); errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	} else if err != nil {
		return fmt.Errorf("validate research experiment: %w", err)
	}
	if persistedProjectID != projectID {
		return ErrBrokenLink
	}
	return nil
}

func validateRunLinks(
	ctx context.Context,
	tx *sql.Tx,
	projectID int64,
	links RunLinks,
) error {
	if links.AgentRunID == nil {
		return nil
	}
	var agentProjectID sql.NullInt64
	var latestGenerationID, approvedGenerationID sql.NullInt64
	if err := tx.QueryRowContext(ctx, `
		SELECT project_id, latest_generation_id, approved_generation_id
		FROM agent_runs
		WHERE id = $1
		FOR SHARE`, *links.AgentRunID,
	).Scan(&agentProjectID, &latestGenerationID, &approvedGenerationID); errors.Is(err, sql.ErrNoRows) {
		return ErrBrokenLink
	} else if err != nil {
		return fmt.Errorf("validate agent run link: %w", err)
	}
	if !agentProjectID.Valid || agentProjectID.Int64 != projectID {
		return ErrBrokenLink
	}
	if links.GenerationID == nil {
		return nil
	}
	var generationProjectID sql.NullInt64
	var generationSHA sql.NullString
	if err := tx.QueryRowContext(ctx, `
		SELECT project_id, dsl_sha256
		FROM dsl_generation_runs
		WHERE id = $1
		FOR SHARE`, *links.GenerationID,
	).Scan(&generationProjectID, &generationSHA); errors.Is(err, sql.ErrNoRows) {
		return ErrBrokenLink
	} else if err != nil {
		return fmt.Errorf("validate generation link: %w", err)
	}
	if !generationProjectID.Valid || generationProjectID.Int64 != projectID ||
		(!latestGenerationID.Valid || latestGenerationID.Int64 != *links.GenerationID) &&
			(!approvedGenerationID.Valid || approvedGenerationID.Int64 != *links.GenerationID) {
		return ErrBrokenLink
	}
	if !generationSHA.Valid || !sha256Pattern.MatchString(generationSHA.String) {
		return ErrBrokenLink
	}
	if links.DSLSHA256 != nil && generationSHA.String != *links.DSLSHA256 {
		return ErrBrokenLink
	}
	if links.BatchID == nil {
		return nil
	}
	var batchProjectID int64
	var batchCarriesGeneration bool
	if err := tx.QueryRowContext(ctx, `
		SELECT b.project_id, EXISTS (
			SELECT 1
			FROM execution_jobs j
			WHERE j.batch_id = b.id
			  AND j.project_id = b.project_id
			  AND j.dsl_sha256 = $2
		)
		FROM execution_batches b
		WHERE b.id = $1
		FOR SHARE`, *links.BatchID, generationSHA.String,
	).Scan(&batchProjectID, &batchCarriesGeneration); errors.Is(err, sql.ErrNoRows) {
		return ErrBrokenLink
	} else if err != nil {
		return fmt.Errorf("validate batch link: %w", err)
	}
	if batchProjectID != projectID || !batchCarriesGeneration {
		return ErrBrokenLink
	}
	if links.ExecutionID == nil {
		return nil
	}
	var executionProjectID, executionBatchID, executionJobID int64
	var executionSHA, jobSHA sql.NullString
	if err := tx.QueryRowContext(ctx, `
		SELECT r.project_id, r.batch_id, r.job_id, r.dsl_sha256, j.dsl_sha256
		FROM test_case_runs r
		JOIN execution_jobs j ON j.id = r.job_id
		WHERE r.id = $1
		  AND j.batch_id = r.batch_id
		  AND j.project_id = r.project_id
		FOR SHARE OF r, j`, *links.ExecutionID,
	).Scan(
		&executionProjectID, &executionBatchID, &executionJobID,
		&executionSHA, &jobSHA,
	); errors.Is(err, sql.ErrNoRows) {
		return ErrBrokenLink
	} else if err != nil {
		return fmt.Errorf("validate execution link: %w", err)
	}
	if executionProjectID != projectID || executionBatchID != *links.BatchID ||
		executionJobID <= 0 ||
		!executionSHA.Valid || executionSHA.String != generationSHA.String ||
		!jobSHA.Valid || jobSHA.String != generationSHA.String {
		return ErrBrokenLink
	}
	return nil
}

func runArgs(run ResearchRun, metrics any) []any {
	return []any{
		run.ID, run.ExperimentID, run.ProjectID, run.IdempotencyKey,
		run.RepetitionIndex, run.Warmup, run.Status, run.Versions.SchemaVersion,
		run.Versions.ProjectorVersion, run.Versions.MetricVersion,
		run.Versions.PolicyVersion, run.Links.AgentRunID, run.Links.GenerationID,
		run.Links.BatchID, run.Links.ExecutionID, run.Links.DSLSHA256, metrics,
		run.StartedAt, run.FinishedAt, run.CreatedAt, run.UpdatedAt,
	}
}

func marshalMetrics(metrics *RunMetrics) (any, error) {
	if metrics == nil {
		return nil, nil
	}
	if err := metrics.Validate(); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(metrics)
	if err != nil {
		return nil, fmt.Errorf("encode research metrics: %w", err)
	}
	return string(raw), nil
}

func sameRunImmutablePayload(left, right ResearchRun) bool {
	return left.ID == right.ID &&
		left.ExperimentID == right.ExperimentID &&
		left.ProjectID == right.ProjectID &&
		left.IdempotencyKey == right.IdempotencyKey &&
		left.RepetitionIndex == right.RepetitionIndex &&
		left.Warmup == right.Warmup &&
		left.Versions == right.Versions
}

func linksExtend(current, next RunLinks) bool {
	return pointerExtends(current.AgentRunID, next.AgentRunID) &&
		pointerExtends(current.GenerationID, next.GenerationID) &&
		pointerExtends(current.BatchID, next.BatchID) &&
		pointerExtends(current.ExecutionID, next.ExecutionID) &&
		pointerExtends(current.DSLSHA256, next.DSLSHA256)
}

func pointerExtends[T comparable](current, next *T) bool {
	return current == nil || next != nil && *current == *next
}

func (r *PostgresRepository) resolveTime(value time.Time) time.Time {
	if value.IsZero() {
		return r.now().UTC()
	}
	return value.UTC()
}

func classifyPersistenceError(err error) error {
	var postgresError *pgconn.PgError
	if !errors.As(err, &postgresError) {
		return err
	}
	switch postgresError.Code {
	case "23505":
		return fmt.Errorf("%w: %s", ErrConflict, postgresError.ConstraintName)
	case "23503":
		return fmt.Errorf("%w: %s", ErrBrokenLink, postgresError.ConstraintName)
	case "23514", "22001":
		return fmt.Errorf("%w: %s", ErrInvalid, postgresError.ConstraintName)
	default:
		return err
	}
}

func classifyRunCreateError(err error) error {
	var postgresError *pgconn.PgError
	if !errors.As(err, &postgresError) || postgresError.Code != "23505" {
		return classifyPersistenceError(err)
	}
	switch postgresError.ConstraintName {
	case "pk_research_runs",
		"uq_research_runs_experiment_idempotency",
		"uq_research_runs_experiment_repetition_warmup":
		return fmt.Errorf("%w: %s", ErrConflict, postgresError.ConstraintName)
	default:
		return err
	}
}

func pagination(limit, offset int) (int, int) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}
	return limit, offset
}

func requireAffected(result sql.Result) error {
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

func stringPtr(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	return &value.String
}

func int64Ptr(value sql.NullInt64) *int64 {
	if !value.Valid {
		return nil
	}
	return &value.Int64
}

func timePtr(value sql.NullTime) *time.Time {
	if !value.Valid {
		return nil
	}
	normalized := value.Time.UTC()
	return &normalized
}
