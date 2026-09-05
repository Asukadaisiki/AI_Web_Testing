package execution

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

var (
	ErrNotFound     = errors.New("execution resource not found")
	ErrAccessDenied = errors.New("execution access denied")
	ErrConflict     = errors.New("execution conflict")
)

type BatchCreateRequest struct {
	ProjectID       int64             `json:"project_id" vd:"$>0"`
	CaseIDs         []int64           `json:"case_ids" vd:"len($)>0"`
	PlanningSession *int64            `json:"planning_session_id,omitempty"`
	IdempotencyKey  *string           `json:"idempotency_key,omitempty"`
	Concurrency     int               `json:"concurrency_limit"`
	InputValues     map[string]string `json:"input_values"`
}

type CaseExecutionRequest struct {
	InputValues map[string]string `json:"input_values"`
}

type ListRequest struct {
	ProjectID          *int64
	CaseID             *int64
	Status             string
	FailureCategory    string
	FailureFingerprint string
	WindowDays         int
	Limit              int
	Offset             int
}

type OverviewRequest struct {
	ScopeType          string
	ProjectID          *int64
	CaseID             *int64
	WindowDays         int
	FailureFingerprint string
}

type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

func (s *Store) CreateBatch(
	ctx context.Context,
	actorUserID int64,
	request BatchCreateRequest,
) (map[string]any, error) {
	if request.Concurrency < 1 || request.Concurrency > 16 {
		request.Concurrency = 1
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err := requireMembership(ctx, tx, request.ProjectID, actorUserID); err != nil {
		return nil, err
	}
	if request.IdempotencyKey != nil {
		var existingBatchID, existingProjectID int64
		err := tx.QueryRowContext(ctx, `
			SELECT id, project_id
			FROM execution_batches
			WHERE triggered_by = $1 AND idempotency_key = $2`,
			actorUserID, request.IdempotencyKey,
		).Scan(&existingBatchID, &existingProjectID)
		if err == nil {
			if existingProjectID != request.ProjectID {
				return nil, ErrConflict
			}
			if err := tx.Rollback(); err != nil {
				return nil, err
			}
			return s.BatchDetail(ctx, actorUserID, existingBatchID)
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
	}
	if request.PlanningSession != nil {
		var valid bool
		if err := tx.QueryRowContext(ctx, `
			SELECT EXISTS(
				SELECT 1
				FROM ai_planning_sessions s
				JOIN session_projects sp ON sp.session_id = s.id
				WHERE s.id = $1 AND s.actor_user_id = $2 AND sp.project_id = $3
			)`,
			request.PlanningSession, actorUserID, request.ProjectID,
		).Scan(&valid); err != nil {
			return nil, err
		}
		if !valid {
			return nil, ErrNotFound
		}
		var activeBatchID int64
		err := tx.QueryRowContext(ctx, `
			SELECT id FROM execution_batches
			WHERE planning_session_id = $1 AND status IN ('pending', 'running')
			LIMIT 1`,
			request.PlanningSession,
		).Scan(&activeBatchID)
		if err == nil {
			return nil, ErrConflict
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
	}
	request.CaseIDs = uniqueInt64s(request.CaseIDs)
	var count int
	if err := tx.QueryRowContext(ctx, `
		SELECT count(*) FROM test_cases
		WHERE project_id = $1 AND id = ANY($2::bigint[])`,
		request.ProjectID, request.CaseIDs,
	).Scan(&count); err != nil {
		return nil, err
	}
	if count != len(request.CaseIDs) {
		return nil, ErrNotFound
	}
	inputValues, _ := json.Marshal(request.InputValues)
	var batchID int64
	err = tx.QueryRowContext(ctx, `
		INSERT INTO execution_batches (
			project_id, planning_session_id, triggered_by, status,
			idempotency_key, concurrency_limit, input_values_json
		) VALUES ($1, $2, $3, 'pending', $4, $5, $6)
		ON CONFLICT (triggered_by, idempotency_key) DO NOTHING
		RETURNING id`,
		request.ProjectID, request.PlanningSession, actorUserID,
		request.IdempotencyKey, request.Concurrency, string(inputValues),
	).Scan(&batchID)
	if errors.Is(err, sql.ErrNoRows) && request.IdempotencyKey != nil {
		var existingProjectID int64
		if err := tx.QueryRowContext(ctx, `
			SELECT id, project_id
			FROM execution_batches
			WHERE triggered_by = $1 AND idempotency_key = $2`,
			actorUserID, request.IdempotencyKey,
		).Scan(&batchID, &existingProjectID); err != nil {
			return nil, err
		}
		if existingProjectID != request.ProjectID {
			return nil, ErrConflict
		}
		if err := tx.Rollback(); err != nil {
			return nil, err
		}
		return s.BatchDetail(ctx, actorUserID, batchID)
	}
	if err != nil {
		return nil, fmt.Errorf("create execution batch: %w", err)
	}
	for index, caseID := range request.CaseIDs {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO execution_jobs (
				batch_id, project_id, case_id, order_index, status,
				attempt_count, max_attempts, cancel_requested
			)
			VALUES ($1, $2, $3, $4, 'pending', 0, 2, false)
			ON CONFLICT (batch_id, case_id) DO NOTHING`,
			batchID, request.ProjectID, caseID, index,
		); err != nil {
			return nil, fmt.Errorf("create execution job: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.BatchDetail(ctx, actorUserID, batchID)
}

func uniqueInt64s(values []int64) []int64 {
	seen := make(map[int64]struct{}, len(values))
	result := make([]int64, 0, len(values))
	for _, value := range values {
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func (s *Store) ExecuteCase(
	ctx context.Context,
	actorUserID, caseID int64,
	request CaseExecutionRequest,
) (map[string]any, error) {
	var projectID int64
	if err := s.db.QueryRowContext(ctx, `
		SELECT tc.project_id
		FROM test_cases tc
		JOIN project_members pm ON pm.project_id = tc.project_id
		WHERE tc.id = $1 AND pm.user_id = $2`,
		caseID, actorUserID,
	).Scan(&projectID); errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	} else if err != nil {
		return nil, err
	}
	batch, err := s.CreateBatch(ctx, actorUserID, BatchCreateRequest{
		ProjectID:   projectID,
		CaseIDs:     []int64{caseID},
		Concurrency: 1,
		InputValues: request.InputValues,
	})
	if err != nil {
		return nil, err
	}
	batchID := batch["id"].(int64)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		report, err := s.BatchReport(ctx, actorUserID, batchID)
		if err != nil {
			return nil, err
		}
		switch report["status"] {
		case "passed", "failed", "needs_intervention", "cancelled":
			jobs := report["jobs"].([]map[string]any)
			if len(jobs) == 0 || jobs[0]["latest_execution"] == nil {
				return nil, ErrNotFound
			}
			return jobs[0]["latest_execution"].(map[string]any), nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
		}
	}
}

func (s *Store) ListExecutions(
	ctx context.Context,
	actorUserID int64,
	request ListRequest,
) ([]map[string]any, error) {
	if request.Limit < 1 || request.Limit > 100 {
		request.Limit = 20
	}
	query := `
		SELECT r.id, r.case_id, tc.name, r.project_id, r.batch_id, r.job_id,
		       r.attempt_number, r.dsl_sha256, r.report_schema_version, r.triggered_by,
		       r.status, r.error_message, r.started_at, r.finished_at, r.dsl_snapshot,
		       r.report, r.failure_signal_json, r.analysis_status, r.analysis_json
		FROM test_case_runs r
		JOIN test_cases tc ON tc.id = r.case_id
		JOIN project_members pm ON pm.project_id = r.project_id
		WHERE pm.user_id = $1
		  AND ($2::bigint IS NULL OR r.project_id = $2)
		  AND ($3::bigint IS NULL OR r.case_id = $3)
		  AND ($4 = '' OR r.status = $4)
		  AND ($5 = '' OR r.failure_signal_json->>'category' = $5)
		  AND ($6 = '' OR r.failure_signal_json->>'fingerprint' = $6)
		  AND ($7 = 0 OR r.started_at >= now() - ($7 * interval '1 day'))
		ORDER BY r.started_at DESC, r.id DESC
		LIMIT $8 OFFSET $9`
	rows, err := s.db.QueryContext(
		ctx, query, actorUserID, request.ProjectID, request.CaseID, request.Status,
		request.FailureCategory, request.FailureFingerprint, request.WindowDays,
		request.Limit, request.Offset,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]map[string]any, 0)
	for rows.Next() {
		item, err := scanExecution(rows)
		if err != nil {
			return nil, err
		}
		delete(item, "dsl_snapshot")
		delete(item, "report")
		delete(item, "analysis_status")
		delete(item, "analysis")
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *Store) GetExecution(ctx context.Context, actorUserID, executionID int64) (map[string]any, error) {
	return scanExecution(s.db.QueryRowContext(ctx, `
		SELECT r.id, r.case_id, tc.name, r.project_id, r.batch_id, r.job_id,
		       r.attempt_number, r.dsl_sha256, r.report_schema_version, r.triggered_by,
		       r.status, r.error_message, r.started_at, r.finished_at, r.dsl_snapshot,
		       r.report, r.failure_signal_json, r.analysis_status,
		       COALESCE(r.analysis_json, b.analysis_json)
		FROM test_case_runs r
		JOIN test_cases tc ON tc.id = r.case_id
		JOIN project_members pm ON pm.project_id = r.project_id
		LEFT JOIN execution_batches b ON b.id = r.batch_id
		WHERE r.id = $1 AND pm.user_id = $2`, executionID, actorUserID))
}

func (s *Store) DeleteExecution(ctx context.Context, actorUserID, executionID int64) error {
	result, err := s.db.ExecContext(ctx, `
		DELETE FROM test_case_runs r
		WHERE r.id = $1
		  AND EXISTS (
		    SELECT 1 FROM project_members pm
		    WHERE pm.project_id = r.project_id AND pm.user_id = $2
		  )`, executionID, actorUserID)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) Overview(
	ctx context.Context,
	actorUserID int64,
	request OverviewRequest,
) (map[string]any, error) {
	request = normalizeOverviewRequest(request)
	current, previous := overviewWindows(time.Now().UTC(), request.WindowDays)
	rows, err := s.listOverviewExecutions(
		ctx,
		actorUserID,
		request,
		previous.Start,
		current.End.AddDate(0, 0, 1),
	)
	if err != nil {
		return nil, err
	}
	return buildOverview(request, rows, current, previous), nil
}

func (s *Store) ListBatches(
	ctx context.Context,
	actorUserID, projectID int64,
	limit int,
) ([]map[string]any, error) {
	if limit < 1 || limit > 100 {
		limit = 50
	}
	if err := requireMembership(ctx, s.db, projectID, actorUserID); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id FROM execution_batches
		WHERE project_id = $1 ORDER BY created_at DESC, id DESC LIMIT $2`,
		projectID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := make([]int64, 0)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	result := make([]map[string]any, 0, len(ids))
	for _, id := range ids {
		detail, err := s.batch(ctx, actorUserID, id, false)
		if err != nil {
			return nil, err
		}
		result = append(result, detail)
	}
	return result, rows.Err()
}

func (s *Store) BatchDetail(ctx context.Context, actorUserID, batchID int64) (map[string]any, error) {
	return s.batch(ctx, actorUserID, batchID, true)
}

func (s *Store) BatchReport(ctx context.Context, actorUserID, batchID int64) (map[string]any, error) {
	result, err := s.batch(ctx, actorUserID, batchID, true)
	if err != nil {
		return nil, err
	}
	passed := result["passed_jobs"].(int64)
	failed := result["failed_jobs"].(int64)
	intervention := result["intervention_jobs"].(int64)
	cancelled := result["cancelled_jobs"].(int64)
	decisive := passed + failed + intervention
	result["completed_jobs"] = passed + failed + intervention + cancelled
	result["pass_rate"] = float64(0)
	if decisive > 0 {
		result["pass_rate"] = float64(passed) / float64(decisive)
	}
	return result, nil
}

func (s *Store) BatchByIdempotency(
	ctx context.Context,
	actorUserID int64,
	idempotencyKey string,
) (map[string]any, bool, error) {
	var batchID int64
	err := s.db.QueryRowContext(ctx, `
		SELECT id FROM execution_batches
		WHERE triggered_by = $1 AND idempotency_key = $2`,
		actorUserID, idempotencyKey,
	).Scan(&batchID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	batch, err := s.BatchDetail(ctx, actorUserID, batchID)
	return batch, err == nil, err
}

func (s *Store) CancelBatch(ctx context.Context, actorUserID, batchID int64) (map[string]any, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err := requireBatchAccess(ctx, tx, batchID, actorUserID); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE execution_jobs
		SET cancel_requested = true,
		    status = CASE WHEN status = 'pending' THEN 'cancelled' ELSE status END,
		    finished_at = CASE WHEN status = 'pending' THEN now() ELSE finished_at END
		WHERE batch_id = $1 AND status IN ('pending', 'running')`, batchID); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE execution_batches
		SET status = CASE
			WHEN EXISTS(SELECT 1 FROM execution_jobs WHERE batch_id = $1 AND status = 'running')
				THEN 'running'
			ELSE 'cancelled'
		END,
		finished_at = CASE
			WHEN EXISTS(SELECT 1 FROM execution_jobs WHERE batch_id = $1 AND status = 'running')
				THEN finished_at
			ELSE now()
		END
		WHERE id = $1`, batchID); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.BatchDetail(ctx, actorUserID, batchID)
}

func (s *Store) batch(
	ctx context.Context,
	actorUserID, batchID int64,
	includeJobs bool,
) (map[string]any, error) {
	var (
		projectID, triggeredBy int64
		status, analysisStatus string
		planningSession        sql.NullInt64
		idempotencyKey         sql.NullString
		concurrency            int
		analysis               []byte
		createdAt              time.Time
		startedAt, finishedAt  sql.NullTime
	)
	err := s.db.QueryRowContext(ctx, `
		SELECT b.project_id, b.planning_session_id, b.triggered_by, b.status,
		       b.idempotency_key, b.concurrency_limit, b.analysis_status,
		       b.analysis_json, b.created_at, b.started_at, b.finished_at
		FROM execution_batches b
		JOIN project_members pm ON pm.project_id = b.project_id
		WHERE b.id = $1 AND pm.user_id = $2`, batchID, actorUserID,
	).Scan(
		&projectID, &planningSession, &triggeredBy, &status,
		&idempotencyKey, &concurrency, &analysisStatus, &analysis,
		&createdAt, &startedAt, &finishedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	jobs, counts, err := s.jobs(ctx, batchID)
	if err != nil {
		return nil, err
	}
	result := map[string]any{
		"id": batchID, "project_id": projectID, "planning_session_id": nullableInt(planningSession),
		"triggered_by": triggeredBy, "status": status, "idempotency_key": nullableString(idempotencyKey),
		"concurrency_limit": concurrency, "total_jobs": counts["total"],
		"pending_jobs": counts["pending"], "running_jobs": counts["running"],
		"passed_jobs": counts["passed"], "failed_jobs": counts["failed"],
		"intervention_jobs": counts["needs_intervention"], "cancelled_jobs": counts["cancelled"],
		"analysis_status": analysisStatus, "analysis": rawJSON(analysis),
		"created_at": createdAt, "started_at": nullableTime(startedAt), "finished_at": nullableTime(finishedAt),
	}
	if includeJobs {
		result["jobs"] = jobs
	}
	return result, nil
}

func (s *Store) jobs(ctx context.Context, batchID int64) ([]map[string]any, map[string]int64, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT j.id, j.project_id, j.case_id, tc.name, j.order_index, j.status,
		       j.attempt_count, j.max_attempts, j.cancel_requested, j.last_error_message,
		       j.created_at, j.started_at, j.heartbeat_at, j.finished_at,
		       r.id, r.dsl_snapshot, r.report, r.failure_signal_json, r.analysis_status,
		       r.analysis_json, r.status, r.error_message, r.started_at, r.finished_at,
		       r.attempt_number, r.dsl_sha256, r.report_schema_version, r.triggered_by
		FROM execution_jobs j
		JOIN test_cases tc ON tc.id = j.case_id
		LEFT JOIN LATERAL (
			SELECT * FROM test_case_runs
			WHERE job_id = j.id ORDER BY attempt_number DESC, id DESC LIMIT 1
		) r ON true
		WHERE j.batch_id = $1 ORDER BY j.order_index, j.id`, batchID)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	items := make([]map[string]any, 0)
	counts := map[string]int64{"total": 0}
	for rows.Next() {
		var id, projectID, caseID int64
		var name, status string
		var orderIndex, attempts, maxAttempts int
		var cancel bool
		var lastError sql.NullString
		var created time.Time
		var started, heartbeat, finished sql.NullTime
		var runID, runAttempt, runTriggered sql.NullInt64
		var dsl, report, failure, runAnalysis []byte
		var analysisStatus, runStatus, runError, hash, version sql.NullString
		var runStarted, runFinished sql.NullTime
		if err := rows.Scan(
			&id, &projectID, &caseID, &name, &orderIndex, &status,
			&attempts, &maxAttempts, &cancel, &lastError,
			&created, &started, &heartbeat, &finished,
			&runID, &dsl, &report, &failure, &analysisStatus,
			&runAnalysis, &runStatus, &runError, &runStarted, &runFinished,
			&runAttempt, &hash, &version, &runTriggered,
		); err != nil {
			return nil, nil, err
		}
		counts["total"]++
		counts[status]++
		var latest any
		if runID.Valid {
			latest = executionDetail(
				runID.Int64, caseID, name, projectID, batchID, id,
				runAttempt.Int64, hash.String, version.String, runTriggered.Int64,
				runStatus.String, nullableString(runError), runStarted.Time,
				nullableTime(runFinished), rawJSON(dsl), rawJSON(report),
				rawJSON(failure), analysisStatus.String, rawJSON(runAnalysis),
			)
		}
		items = append(items, map[string]any{
			"id": id, "batch_id": batchID, "project_id": projectID, "case_id": caseID,
			"case_name": name, "order_index": orderIndex, "status": status,
			"attempt_count": attempts, "max_attempts": maxAttempts,
			"cancel_requested": cancel, "last_error_message": nullableString(lastError),
			"created_at": created, "started_at": nullableTime(started),
			"heartbeat_at": nullableTime(heartbeat), "finished_at": nullableTime(finished),
			"latest_execution": latest,
		})
	}
	return items, counts, rows.Err()
}

func executionDetail(
	id, caseID int64, caseName string, projectID int64, batchID, jobID any, attempt int64,
	hash, version string, triggeredBy int64, status string, errorMessage any,
	startedAt time.Time, finishedAt any, dsl, report, failure any,
	analysisStatus string, analysis any,
) map[string]any {
	steps := []any{}
	if reportMap, ok := report.(map[string]any); ok {
		if value, ok := reportMap["steps"].([]any); ok {
			steps = value
		}
	}
	result := map[string]any{
		"id": id, "case_id": caseID, "case_name": caseName, "project_id": projectID,
		"batch_id": batchID, "job_id": jobID, "attempt_number": attempt,
		"dsl_sha256": hash, "report_schema_version": version, "triggered_by": triggeredBy,
		"status": status, "error_message": errorMessage, "started_at": startedAt,
		"finished_at": finishedAt, "total_steps": len(steps), "report": report,
		"dsl_snapshot": dsl, "failure_signal": failure,
		"analysis_status": analysisStatus, "analysis": analysis,
	}
	if finished, ok := finishedAt.(time.Time); ok {
		result["duration_ms"] = finished.Sub(startedAt).Milliseconds()
	} else {
		result["duration_ms"] = nil
	}
	for _, raw := range steps {
		step, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if url, ok := step["url"].(string); ok {
			result["latest_url"] = url
		}
		if screenshot, ok := step["screenshot_path"].(string); ok {
			result["latest_screenshot_url"] = "/" + screenshot
		}
		if step["status"] == "failed" {
			result["failed_step_index"] = step["step_index"]
			result["failure_step_action"] = step["action"]
			break
		}
	}
	if signal, ok := failure.(map[string]any); ok {
		result["failure_category"] = signal["category"]
	}
	return result
}

func scanExecution(row rowScanner) (map[string]any, error) {
	var (
		id, caseID, projectID, attempt, triggeredBy int64
		caseName, status, analysisStatus            string
		batchID, jobID                              sql.NullInt64
		hash, version, errorMessage                 sql.NullString
		startedAt                                   time.Time
		finishedAt                                  sql.NullTime
		dsl, report, failure, analysis              []byte
	)
	err := row.Scan(
		&id, &caseID, &caseName, &projectID, &batchID, &jobID,
		&attempt, &hash, &version, &triggeredBy, &status, &errorMessage,
		&startedAt, &finishedAt, &dsl, &report, &failure, &analysisStatus, &analysis,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan execution: %w", err)
	}
	return executionDetail(
		id, caseID, caseName, projectID, nullableInt(batchID), nullableInt(jobID),
		attempt, hash.String, version.String, triggeredBy, status,
		nullableString(errorMessage), startedAt, nullableTime(finishedAt),
		rawJSON(dsl), rawJSON(report), rawJSON(failure), analysisStatus, rawJSON(analysis),
	), nil
}

type rowScanner interface {
	Scan(...any) error
}

type queryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func requireMembership(ctx context.Context, query queryer, projectID, actorUserID int64) error {
	var exists bool
	if err := query.QueryRowContext(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM project_members WHERE project_id = $1 AND user_id = $2
		)`, projectID, actorUserID).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return ErrAccessDenied
	}
	return nil
}

func requireBatchAccess(ctx context.Context, query queryer, batchID, actorUserID int64) error {
	var exists bool
	if err := query.QueryRowContext(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM execution_batches b
			JOIN project_members pm ON pm.project_id = b.project_id
			WHERE b.id = $1 AND pm.user_id = $2
		)`, batchID, actorUserID).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return ErrNotFound
	}
	return nil
}

func rawJSON(value []byte) any {
	if len(value) == 0 {
		return nil
	}
	var decoded any
	if json.Unmarshal(value, &decoded) != nil {
		return nil
	}
	return decoded
}

func nullableInt(value sql.NullInt64) any {
	if value.Valid {
		return value.Int64
	}
	return nil
}

func nullableString(value sql.NullString) any {
	if value.Valid {
		return value.String
	}
	return nil
}

func nullableTime(value sql.NullTime) any {
	if value.Valid {
		return value.Time
	}
	return nil
}
