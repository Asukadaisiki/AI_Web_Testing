package research

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

type OracleResult struct {
	ResearchRunID string         `json:"research_run_id"`
	ExecutionID   int64          `json:"execution_id"`
	Decision      OracleDecision `json:"decision"`
	CreatedAt     time.Time      `json:"created_at"`
	UpdatedAt     time.Time      `json:"updated_at"`
}

func (r *OracleResult) NormalizeAndValidate() error {
	r.ResearchRunID = strings.TrimSpace(r.ResearchRunID)
	if r.ResearchRunID == "" || len(r.ResearchRunID) > 64 || r.ExecutionID <= 0 {
		return fmt.Errorf("%w: oracle result identity", ErrInvalid)
	}
	if err := r.Decision.NormalizeAndValidate(); err != nil {
		return err
	}
	r.CreatedAt = utc(r.CreatedAt)
	r.UpdatedAt = utc(r.UpdatedAt)
	return nil
}

type OracleRepository interface {
	PutOracle(context.Context, OracleResult) (OracleResult, error)
	GetOracle(context.Context, string) (OracleResult, error)
}

func (r *PostgresRepository) PutOracle(
	ctx context.Context,
	result OracleResult,
) (OracleResult, error) {
	now := r.now().UTC()
	if result.CreatedAt.IsZero() {
		result.CreatedAt = now
	}
	if result.UpdatedAt.IsZero() {
		result.UpdatedAt = now
	}
	if err := result.NormalizeAndValidate(); err != nil {
		return OracleResult{}, err
	}
	decisionJSON, err := json.Marshal(result.Decision)
	if err != nil {
		return OracleResult{}, fmt.Errorf("encode research oracle: %w", err)
	}

	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return OracleResult{}, fmt.Errorf("begin research oracle transaction: %w", err)
	}
	defer tx.Rollback()

	var runExecutionID, executionProjectID sql.NullInt64
	var runProjectID int64
	if err := tx.QueryRowContext(ctx, `
		SELECT rr.execution_id, rr.project_id, tr.project_id
		FROM research_runs rr
		LEFT JOIN test_case_runs tr ON tr.id = rr.execution_id
		WHERE rr.id = $1
		FOR UPDATE OF rr`,
		result.ResearchRunID,
	).Scan(
		&runExecutionID,
		&runProjectID,
		&executionProjectID,
	); errors.Is(err, sql.ErrNoRows) {
		return OracleResult{}, ErrNotFound
	} else if err != nil {
		return OracleResult{}, fmt.Errorf("lock research oracle run: %w", err)
	}
	if !runExecutionID.Valid ||
		runExecutionID.Int64 != result.ExecutionID ||
		!executionProjectID.Valid ||
		runProjectID != executionProjectID.Int64 {
		return OracleResult{}, ErrBrokenLink
	}

	current, err := getOracle(ctx, tx, result.ResearchRunID)
	if err == nil {
		if current.ExecutionID != result.ExecutionID ||
			current.Decision.ContentSHA256 != result.Decision.ContentSHA256 {
			return OracleResult{}, ErrConflict
		}
		if err := tx.Commit(); err != nil {
			return OracleResult{}, fmt.Errorf("commit research oracle replay: %w", err)
		}
		return current, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return OracleResult{}, fmt.Errorf("read research oracle: %w", err)
	}

	persisted, err := scanOracle(tx.QueryRowContext(ctx, `
		INSERT INTO research_oracle_results (
			research_run_id, execution_id, schema_version, evaluator, passed,
			reason_code, decision_json, content_sha256, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING research_run_id, execution_id, schema_version, evaluator,
		          passed, reason_code, decision_json, content_sha256,
		          created_at, updated_at`,
		result.ResearchRunID,
		result.ExecutionID,
		result.Decision.SchemaVersion,
		result.Decision.Evaluator,
		result.Decision.Passed,
		result.Decision.ReasonCode,
		string(decisionJSON),
		result.Decision.ContentSHA256,
		result.CreatedAt,
		result.UpdatedAt,
	))
	if err != nil {
		return OracleResult{}, fmt.Errorf(
			"put research oracle: %w",
			classifyPersistenceError(err),
		)
	}
	if err := tx.Commit(); err != nil {
		return OracleResult{}, fmt.Errorf("commit research oracle: %w", err)
	}
	return persisted, nil
}

func (r *PostgresRepository) GetOracle(
	ctx context.Context,
	researchRunID string,
) (OracleResult, error) {
	result, err := getOracle(ctx, r.db, strings.TrimSpace(researchRunID))
	if errors.Is(err, sql.ErrNoRows) {
		return OracleResult{}, ErrNotFound
	}
	if err != nil {
		return OracleResult{}, fmt.Errorf("get research oracle: %w", err)
	}
	return result, nil
}

type oracleQueryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func getOracle(
	ctx context.Context,
	query oracleQueryer,
	researchRunID string,
) (OracleResult, error) {
	return scanOracle(query.QueryRowContext(ctx, `
		SELECT research_run_id, execution_id, schema_version, evaluator,
		       passed, reason_code, decision_json, content_sha256,
		       created_at, updated_at
		FROM research_oracle_results
		WHERE research_run_id = $1`,
		researchRunID,
	))
}

func scanOracle(row rowScanner) (OracleResult, error) {
	var result OracleResult
	var decisionJSON []byte
	var schemaVersion, evaluator, reasonCode, contentSHA256 string
	var passed bool
	if err := row.Scan(
		&result.ResearchRunID,
		&result.ExecutionID,
		&schemaVersion,
		&evaluator,
		&passed,
		&reasonCode,
		&decisionJSON,
		&contentSHA256,
		&result.CreatedAt,
		&result.UpdatedAt,
	); err != nil {
		return OracleResult{}, err
	}
	if err := json.Unmarshal(decisionJSON, &result.Decision); err != nil {
		return OracleResult{}, fmt.Errorf("decode research oracle: %w", err)
	}
	if err := result.NormalizeAndValidate(); err != nil {
		return OracleResult{}, fmt.Errorf("validate persisted research oracle: %w", err)
	}
	if schemaVersion != result.Decision.SchemaVersion ||
		evaluator != result.Decision.Evaluator ||
		passed != result.Decision.Passed ||
		reasonCode != result.Decision.ReasonCode ||
		contentSHA256 != result.Decision.ContentSHA256 {
		return OracleResult{}, fmt.Errorf("%w: persisted oracle columns disagree", ErrSourceChanged)
	}
	return result, nil
}
