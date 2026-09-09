package execution

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/dsl"
)

type ClaimedJob struct {
	ID               int64
	BatchID          int64
	ProjectID        int64
	CaseID           int64
	AttemptNumber    int64
	TriggeredBy      int64
	DSLCase          json.RawMessage
	DSLHash          string
	CanonicalVersion string
	BaseURL          string
	InputValues      map[string]string
}

type StartedRun struct {
	ID          int64
	DSLCase     json.RawMessage
	BaseURL     string
	InputValues map[string]string
}

type BrowserExecutionResult struct {
	Status        string          `json:"status"`
	ErrorMessage  *string         `json:"error_message"`
	Report        json.RawMessage `json:"report"`
	FailureSignal json.RawMessage `json:"failure_signal"`
}

func DecodeBrowserExecutionResult(raw json.RawMessage) (BrowserExecutionResult, error) {
	var result BrowserExecutionResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return BrowserExecutionResult{}, fmt.Errorf("decode browser execution result: %w", err)
	}
	if !isTerminalStatus(result.Status) {
		return BrowserExecutionResult{}, fmt.Errorf("%w: invalid browser execution status %q", ErrConflict, result.Status)
	}
	if len(result.Report) == 0 || !json.Valid(result.Report) {
		return BrowserExecutionResult{}, errors.New("browser execution result requires a valid report")
	}
	if len(result.FailureSignal) > 0 && !json.Valid(result.FailureSignal) {
		return BrowserExecutionResult{}, errors.New("browser execution result has invalid failure_signal")
	}
	return result, nil
}

func FailedBrowserExecutionResult(err error) BrowserExecutionResult {
	message := fmt.Sprintf("BrowserExecutionError: %v", err)
	return BrowserExecutionResult{
		Status:       "failed",
		ErrorMessage: &message,
		Report:       json.RawMessage(`{"status":"failed","steps":[]}`),
	}
}

func (s *Store) ClaimNextJob(
	ctx context.Context,
	workerID string,
	leaseSeconds int,
) (ClaimedJob, bool, error) {
	return s.claimNextJob(ctx, workerID, leaseSeconds, nil)
}

func (s *Store) ClaimNextProjectJob(
	ctx context.Context,
	workerID string,
	leaseSeconds int,
	projectID int64,
) (ClaimedJob, bool, error) {
	if projectID < 1 {
		return ClaimedJob{}, false, ErrNotFound
	}
	return s.claimNextJob(ctx, workerID, leaseSeconds, &projectID)
}

func (s *Store) claimNextJob(
	ctx context.Context,
	workerID string,
	leaseSeconds int,
	projectID *int64,
) (ClaimedJob, bool, error) {
	if workerID == "" {
		return ClaimedJob{}, false, errors.New("worker_id is required")
	}
	if leaseSeconds < 1 {
		leaseSeconds = 1800
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ClaimedJob{}, false, err
	}
	defer tx.Rollback()
	if err := s.quarantineUnsafeExpiredJobs(ctx, tx); err != nil {
		return ClaimedJob{}, false, err
	}

	var job ClaimedJob
	var inputValues []byte
	var rawDSL string
	err = tx.QueryRowContext(ctx, `
		SELECT j.id, j.batch_id, j.project_id, j.case_id, j.attempt_count + 1,
		       b.triggered_by, b.input_values_json,
		       COALESCE(j.dsl_canonical_json, j.dsl_snapshot::text, tc.dsl::text),
		       COALESCE(j.dsl_sha256, ''), COALESCE(j.dsl_canonical_version, '')
		FROM execution_jobs j
		JOIN execution_batches b ON b.id = j.batch_id
		JOIN test_cases tc ON tc.id = j.case_id
		WHERE b.status IN ('pending', 'running')
		  AND (
		    j.status = 'pending'
		    OR (j.status = 'running' AND j.lease_expires_at < now())
		  )
		  AND j.cancel_requested = false
		  AND j.attempt_count < j.max_attempts
		  AND ($1::bigint IS NULL OR j.project_id = $1)
		  AND (
		    SELECT count(*)
		    FROM execution_jobs running
		    WHERE running.batch_id = b.id
		      AND running.status = 'running'
		      AND running.lease_expires_at >= now()
		  ) < b.concurrency_limit
		ORDER BY b.created_at, b.id, j.order_index, j.id
		FOR UPDATE OF b, j SKIP LOCKED
		LIMIT 1`,
		nullableInt64Param(projectID),
	).Scan(
		&job.ID,
		&job.BatchID,
		&job.ProjectID,
		&job.CaseID,
		&job.AttemptNumber,
		&job.TriggeredBy,
		&inputValues,
		&rawDSL,
		&job.DSLHash,
		&job.CanonicalVersion,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return ClaimedJob{}, false, nil
	}
	if err != nil {
		return ClaimedJob{}, false, err
	}
	job.DSLCase = json.RawMessage(rawDSL)
	if len(inputValues) > 0 {
		if err := json.Unmarshal(inputValues, &job.InputValues); err != nil {
			return ClaimedJob{}, false, fmt.Errorf("decode batch input values: %w", err)
		}
	}
	if job.InputValues == nil {
		job.InputValues = map[string]string{}
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE execution_jobs
		SET status = 'running',
		    attempt_count = attempt_count + 1,
		    lease_owner = $2,
		    lease_expires_at = now() + ($3 * interval '1 second'),
		    heartbeat_at = now(),
		    started_at = COALESCE(started_at, now()),
		    finished_at = NULL
		WHERE id = $1`,
		job.ID, workerID, leaseSeconds,
	); err != nil {
		return ClaimedJob{}, false, err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE execution_batches
		SET status = 'running',
		    started_at = COALESCE(started_at, now()),
		    finished_at = NULL
		WHERE id = $1`,
		job.BatchID,
	); err != nil {
		return ClaimedJob{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return ClaimedJob{}, false, err
	}
	return job, true, nil
}

func (s *Store) quarantineUnsafeExpiredJobs(ctx context.Context, tx *sql.Tx) error {
	rows, err := tx.QueryContext(ctx, `
		SELECT j.id, j.batch_id,
		       COALESCE(j.dsl_canonical_json, j.dsl_snapshot::text, tc.dsl::text),
		       r.status, r.report
		FROM execution_jobs j
		JOIN test_cases tc ON tc.id = j.case_id
		LEFT JOIN LATERAL (
			SELECT status, report
			FROM test_case_runs
			WHERE job_id = j.id
			ORDER BY id DESC
			LIMIT 1
		) r ON true
		WHERE j.status = 'running'
		  AND j.lease_expires_at < now()
		FOR UPDATE OF j SKIP LOCKED`)
	if err != nil {
		return err
	}
	defer rows.Close()

	changedBatches := make(map[int64]struct{})
	for rows.Next() {
		var jobID, batchID int64
		var rawDSL []byte
		var runStatus sql.NullString
		var report []byte
		if err := rows.Scan(&jobID, &batchID, &rawDSL, &runStatus, &report); err != nil {
			return err
		}
		if !requiresManualRecovery(rawDSL, runStatus, report) {
			continue
		}
		message := "Lease expired after a protected action may have been dispatched; automatic whole-case replay is blocked."
		if _, err := tx.ExecContext(ctx, `
			UPDATE execution_jobs
			SET status = 'needs_intervention',
			    last_error_message = $2,
			    finished_at = now(),
			    lease_owner = NULL,
			    lease_expires_at = NULL,
			    heartbeat_at = NULL
			WHERE id = $1`,
			jobID, message,
		); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE test_case_runs
			SET status = 'needs_intervention',
			    error_message = $2,
			    finished_at = COALESCE(finished_at, now())
			WHERE job_id = $1 AND status = 'running'`,
			jobID, message,
		); err != nil {
			return err
		}
		changedBatches[batchID] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for batchID := range changedBatches {
		if err := refreshBatchStatus(ctx, tx, batchID); err != nil {
			return err
		}
	}
	return nil
}

func requiresManualRecovery(
	rawDSL []byte,
	runStatus sql.NullString,
	report []byte,
) bool {
	var dslCase map[string]any
	if json.Unmarshal(rawDSL, &dslCase) != nil {
		return true
	}
	steps, _ := dslCase["steps"].([]any)
	if dslCase["profile"] == string(dsl.ProfileResearchV1) ||
		dslCase["profile"] == string(dsl.ProfileResearchV2) {
		return researchRequiresManualRecovery(steps, runStatus, report)
	}
	hasClick := false
	for _, rawStep := range steps {
		step, _ := rawStep.(map[string]any)
		if step["action"] == "click" {
			hasClick = true
			break
		}
	}
	if !hasClick {
		return false
	}
	if !runStatus.Valid || runStatus.String == "running" || len(report) == 0 {
		return true
	}
	reportSteps := reportSteps(report)
	observedClick := false
	for _, step := range reportSteps {
		if step["action"] != "click" {
			continue
		}
		observedClick = true
		outcome, _ := step["action_outcome"].(map[string]any)
		if len(outcome) == 0 {
			return true
		}
		if outcome["status"] == "succeeded" || outcome["status"] == "unknown" {
			return true
		}
		if outcome["side_effect_state"] == "committed" || outcome["side_effect_state"] == "unknown" {
			return true
		}
	}
	return !observedClick
}

func researchRequiresManualRecovery(
	steps []any,
	runStatus sql.NullString,
	report []byte,
) bool {
	protected := map[int]struct{}{}
	for index, rawStep := range steps {
		step, _ := rawStep.(map[string]any)
		if step["idempotency"] == "non_idempotent" ||
			step["side_effect"] == "external_state" ||
			step["side_effect"] == "unknown" {
			protected[index] = struct{}{}
		}
	}
	if len(protected) == 0 {
		return false
	}
	if !runStatus.Valid || len(report) == 0 {
		return true
	}
	evidenceByIndex := make(map[int]map[string]any)
	for _, step := range reportSteps(report) {
		number, ok := step["step_index"].(float64)
		if !ok {
			continue
		}
		evidenceByIndex[int(number)] = step
	}
	for index := range protected {
		evidence, exists := evidenceByIndex[index]
		if !exists {
			return runStatus.String == "running"
		}
		outcome, _ := evidence["action_outcome"].(map[string]any)
		sideEffectState, _ := outcome["side_effect_state"].(string)
		switch sideEffectState {
		case "committed", "unknown":
			return true
		case "not_committed", "not_applicable":
			continue
		}
		status, _ := outcome["status"].(string)
		if status == "succeeded" || status == "unknown" || status == "" {
			return true
		}
	}
	return false
}

func reportSteps(report []byte) []map[string]any {
	var payload struct {
		Steps []map[string]any `json:"steps"`
	}
	if json.Unmarshal(report, &payload) != nil {
		return nil
	}
	return payload.Steps
}

func (s *Store) StartClaimedJobRun(
	ctx context.Context,
	job ClaimedJob,
) (StartedRun, error) {
	validated, err := validateClaimedJobDSL(job)
	if err != nil {
		return StartedRun{}, err
	}
	hash := job.DSLHash
	if hash == "" {
		hash = dsl.SHA256(validated.CanonicalJSON)
	}
	var runID int64
	if err := s.db.QueryRowContext(ctx, `
		INSERT INTO test_case_runs (
			case_id, project_id, batch_id, job_id, triggered_by, status,
			attempt_number, dsl_snapshot, dsl_sha256, report_schema_version,
			analysis_status, started_at
		) VALUES (
			$1, $2, $3, $4, $5, 'running',
			$6, $7::json, $8, 'execution.report.v2',
			'pending', now()
		)
		RETURNING id`,
		job.CaseID,
		job.ProjectID,
		job.BatchID,
		job.ID,
		job.TriggeredBy,
		job.AttemptNumber,
		string(validated.CanonicalJSON),
		hash,
	).Scan(&runID); err != nil {
		return StartedRun{}, err
	}
	return StartedRun{
		ID:          runID,
		DSLCase:     validated.CanonicalJSON,
		BaseURL:     validated.BaseURL,
		InputValues: job.InputValues,
	}, nil
}

func validateClaimedJobDSL(job ClaimedJob) (dsl.ValidatedCase, error) {
	if len(job.DSLCase) == 0 || !json.Valid(job.DSLCase) {
		return dsl.ValidatedCase{}, fmt.Errorf("%w: execution job has no valid DSL", ErrConflict)
	}
	var (
		validated dsl.ValidatedCase
		err       error
	)
	if job.CanonicalVersion != "" {
		validated, err = dsl.ValidateCaseForVersion(job.DSLCase, job.CanonicalVersion)
	} else {
		validated, err = dsl.ValidateExecutableCase(job.DSLCase)
	}
	if err != nil {
		return dsl.ValidatedCase{}, fmt.Errorf("%w: %v", ErrConflict, err)
	}
	if job.DSLHash != "" && job.DSLHash != dsl.SHA256(validated.CanonicalJSON) {
		return dsl.ValidatedCase{}, fmt.Errorf("%w: execution job DSL SHA mismatch", ErrConflict)
	}
	return validated, nil
}

func (s *Store) FinishClaimedJobRun(
	ctx context.Context,
	workerID string,
	job ClaimedJob,
	runID int64,
	result BrowserExecutionResult,
) error {
	if !isTerminalStatus(result.Status) {
		return fmt.Errorf("%w: invalid terminal status %q", ErrConflict, result.Status)
	}
	if len(result.Report) == 0 || !json.Valid(result.Report) {
		return errors.New("execution report must be valid JSON")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `
		UPDATE test_case_runs
		SET status = $2,
		    error_message = $3,
		    report = $4::json,
		    failure_signal_json = $5::json,
		    finished_at = now()
		WHERE id = $1 AND job_id = $6`,
		runID,
		result.Status,
		nullableStringParam(result.ErrorMessage),
		string(result.Report),
		nullableRawJSONParam(result.FailureSignal),
		job.ID,
	); err != nil {
		return err
	}
	update, err := tx.ExecContext(ctx, `
		UPDATE execution_jobs
		SET status = $2,
		    last_error_message = $3,
		    finished_at = now(),
		    lease_owner = NULL,
		    lease_expires_at = NULL,
		    heartbeat_at = NULL
		WHERE id = $1 AND lease_owner = $4`,
		job.ID,
		result.Status,
		nullableStringParam(result.ErrorMessage),
		workerID,
	)
	if err != nil {
		return err
	}
	if affected, err := update.RowsAffected(); err != nil {
		return err
	} else if affected == 0 {
		return ErrConflict
	}
	if err := refreshBatchStatus(ctx, tx, job.BatchID); err != nil {
		return err
	}
	return tx.Commit()
}

func refreshBatchStatus(ctx context.Context, tx *sql.Tx, batchID int64) error {
	_, err := tx.ExecContext(ctx, `
		UPDATE execution_batches
		SET status = CASE
			WHEN EXISTS(SELECT 1 FROM execution_jobs WHERE batch_id = $1 AND status = 'running')
				THEN 'running'
			WHEN EXISTS(SELECT 1 FROM execution_jobs WHERE batch_id = $1 AND status = 'pending')
				THEN CASE WHEN started_at IS NULL THEN 'pending' ELSE 'running' END
			WHEN EXISTS(SELECT 1 FROM execution_jobs WHERE batch_id = $1 AND status = 'needs_intervention')
				THEN 'needs_intervention'
			WHEN EXISTS(SELECT 1 FROM execution_jobs WHERE batch_id = $1 AND status = 'failed')
				THEN 'failed'
			WHEN NOT EXISTS(SELECT 1 FROM execution_jobs WHERE batch_id = $1 AND status <> 'cancelled')
				THEN 'cancelled'
			ELSE 'passed'
		END,
		finished_at = CASE
			WHEN EXISTS(SELECT 1 FROM execution_jobs WHERE batch_id = $1 AND status IN ('pending', 'running'))
				THEN finished_at
			ELSE COALESCE(finished_at, now())
		END
		WHERE id = $1`,
		batchID,
	)
	return err
}

func isTerminalStatus(status string) bool {
	switch status {
	case "passed", "failed", "needs_intervention", "cancelled":
		return true
	default:
		return false
	}
}

func nullableStringParam(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableRawJSONParam(value json.RawMessage) any {
	if len(value) == 0 {
		return nil
	}
	return string(value)
}

func nullableInt64Param(value *int64) any {
	if value == nil {
		return nil
	}
	return *value
}

func WorkerID(prefix string) string {
	if prefix == "" {
		prefix = "execution-worker"
	}
	return fmt.Sprintf("%s:%d", prefix, time.Now().UnixNano())
}
