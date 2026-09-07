package execution

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/dsl"
	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestPostgresReportCanonicalMetadataUsesExecutionJob(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	ctx := context.Background()
	if err := db.PingContext(ctx); err != nil {
		t.Fatal(err)
	}

	var runCanonicalVersionColumnCount int
	if err := db.QueryRowContext(ctx, `
		SELECT count(*)
		FROM information_schema.columns
		WHERE table_schema = current_schema()
		  AND table_name = 'test_case_runs'
		  AND column_name = 'dsl_canonical_version'`,
	).Scan(&runCanonicalVersionColumnCount); err != nil {
		t.Fatal(err)
	}
	if runCanonicalVersionColumnCount != 0 {
		t.Fatal("test_case_runs.dsl_canonical_version must not exist")
	}

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	var actorID, projectID, caseID, batchID, jobID, executionID int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO users (email, display_name)
		VALUES ($1, 'Execution Integration Test')
		RETURNING id`,
		"execution-"+suffix+"@example.com",
	).Scan(&actorID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if projectID != 0 {
			_, _ = db.ExecContext(ctx, `DELETE FROM test_case_runs WHERE project_id = $1`, projectID)
			_, _ = db.ExecContext(ctx, `DELETE FROM execution_batches WHERE project_id = $1`, projectID)
			_, _ = db.ExecContext(ctx, `DELETE FROM projects WHERE id = $1`, projectID)
		}
		_, _ = db.ExecContext(ctx, `DELETE FROM users WHERE id = $1`, actorID)
	})

	if err := db.QueryRowContext(ctx, `
		INSERT INTO projects (name, description)
		VALUES ($1, 'canonical metadata integration test')
		RETURNING id`,
		"execution-canonical-"+suffix,
	).Scan(&projectID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO project_members (project_id, user_id, role)
		VALUES ($1, $2, 'owner')`,
		projectID, actorID,
	); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `
		INSERT INTO test_cases (project_id, created_by, updated_by, name, dsl)
		VALUES ($1, $2, $2, 'canonical report', '{"name":"canonical report","steps":[]}'::json)
		RETURNING id`,
		projectID, actorID,
	).Scan(&caseID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `
		INSERT INTO execution_batches (
			project_id, triggered_by, status, concurrency_limit, input_values_json,
			started_at, finished_at
		) VALUES ($1, $2, 'passed', 1, '{}'::json, now(), now())
		RETURNING id`,
		projectID, actorID,
	).Scan(&batchID); err != nil {
		t.Fatal(err)
	}

	hash := strings.Repeat("a", 64)
	if err := db.QueryRowContext(ctx, `
		INSERT INTO execution_jobs (
			batch_id, project_id, case_id, order_index, status,
			attempt_count, max_attempts, cancel_requested,
			dsl_snapshot, dsl_canonical_json, dsl_sha256, dsl_canonical_version,
			started_at, finished_at
		) VALUES (
			$1, $2, $3, 0, 'passed',
			1, 2, false,
			'{"name":"canonical report","steps":[]}'::json,
			'{"name":"canonical report","steps":[]}', $4, $5,
			now(), now()
		)
		RETURNING id`,
		batchID, projectID, caseID, hash, dsl.CanonicalVersionV2,
	).Scan(&jobID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `
		INSERT INTO test_case_runs (
			case_id, project_id, batch_id, job_id, triggered_by, status,
			attempt_number, dsl_snapshot, dsl_sha256, report_schema_version,
			report, analysis_status, started_at, finished_at
		) VALUES (
			$1, $2, $3, $4, $5, 'passed',
			1, '{"name":"canonical report","steps":[]}'::json, $6, 'execution.report.v1',
			'{"status":"passed","steps":[],"legacy_field":"preserved"}'::json,
			'skipped', now(), now()
		)
		RETURNING id`,
		caseID, projectID, batchID, jobID, actorID, hash,
	).Scan(&executionID); err != nil {
		t.Fatal(err)
	}

	store := NewStore(db)
	detail, err := store.GetExecution(ctx, actorID, executionID)
	if err != nil {
		t.Fatalf("GetExecution() error = %v", err)
	}
	assertPostgresCanonicalMetadata(t, detail, hash)
	report := detail["report"].(map[string]any)
	assertPostgresCanonicalMetadata(t, report, hash)
	if report["legacy_field"] != "preserved" {
		t.Fatalf("legacy report field = %#v", report["legacy_field"])
	}

	executions, err := store.ListExecutions(ctx, actorID, ListRequest{
		ProjectID: &projectID,
		Limit:     20,
	})
	if err != nil {
		t.Fatalf("ListExecutions() error = %v", err)
	}
	if len(executions) != 1 {
		t.Fatalf("ListExecutions() count = %d, want 1", len(executions))
	}
	assertPostgresCanonicalMetadata(t, executions[0], hash)
	overview, err := store.Overview(ctx, actorID, OverviewRequest{
		ScopeType:  "project",
		ProjectID:  &projectID,
		WindowDays: 7,
	})
	if err != nil {
		t.Fatalf("Overview() error = %v", err)
	}
	if overview["total_count"] != 1 {
		t.Fatalf("Overview() total_count = %v, want 1", overview["total_count"])
	}

	batchReport, err := store.BatchReport(ctx, actorID, batchID)
	if err != nil {
		t.Fatalf("BatchReport() error = %v", err)
	}
	assertPostgresCanonicalMetadata(t, batchReport, hash)
	latest := batchReport["jobs"].([]map[string]any)[0]["latest_execution"].(map[string]any)
	assertPostgresCanonicalMetadata(t, latest, hash)
	assertPostgresCanonicalMetadata(t, latest["report"].(map[string]any), hash)

	var legacyExecutionID int64
	legacyHash := strings.Repeat("c", 64)
	if err := db.QueryRowContext(ctx, `
		INSERT INTO test_case_runs (
			case_id, project_id, triggered_by, status, attempt_number,
			dsl_snapshot, dsl_sha256, report_schema_version,
			report, analysis_status, started_at, finished_at
		) VALUES (
			$1, $2, $3, 'passed', 1,
			'{"name":"legacy","steps":[]}'::json, $4, 'execution.report.v1',
			'{"status":"passed","steps":[]}'::json, 'skipped', now(), now()
		)
		RETURNING id`,
		caseID, projectID, actorID, legacyHash,
	).Scan(&legacyExecutionID); err != nil {
		t.Fatal(err)
	}
	legacy, err := store.GetExecution(ctx, actorID, legacyExecutionID)
	if err != nil {
		t.Fatalf("GetExecution(legacy) error = %v", err)
	}
	if legacy["dsl_sha256"] != legacyHash ||
		legacy["dsl_canonical_version"] != nil ||
		legacy["dsl_profile"] != nil {
		t.Fatalf("legacy canonical metadata = %#v", legacy)
	}

	var legacyBatchID, legacyJobID, legacyJobExecutionID int64
	legacyJobHash := strings.Repeat("d", 64)
	if err := db.QueryRowContext(ctx, `
		INSERT INTO execution_batches (
			project_id, triggered_by, status, concurrency_limit, input_values_json,
			started_at, finished_at
		) VALUES ($1, $2, 'passed', 1, '{}'::json, now(), now())
		RETURNING id`,
		projectID, actorID,
	).Scan(&legacyBatchID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `
		INSERT INTO execution_jobs (
			batch_id, project_id, case_id, order_index, status,
			attempt_count, max_attempts, cancel_requested, dsl_snapshot,
			started_at, finished_at
		) VALUES (
			$1, $2, $3, 1, 'passed',
			1, 2, false, '{"name":"legacy job","steps":[]}'::json,
			now(), now()
		)
		RETURNING id`,
		legacyBatchID, projectID, caseID,
	).Scan(&legacyJobID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `
		INSERT INTO test_case_runs (
			case_id, project_id, batch_id, job_id, triggered_by, status,
			attempt_number, dsl_snapshot, dsl_sha256, report_schema_version,
			report, analysis_status, started_at, finished_at
		) VALUES (
			$1, $2, $3, $4, $5, 'passed',
			1, '{"name":"legacy job","steps":[]}'::json, $6, 'execution.report.v1',
			'{"status":"passed","steps":[]}'::json, 'skipped', now(), now()
		)
		RETURNING id`,
		caseID, projectID, legacyBatchID, legacyJobID, actorID, legacyJobHash,
	).Scan(&legacyJobExecutionID); err != nil {
		t.Fatal(err)
	}
	legacyJob, err := store.GetExecution(ctx, actorID, legacyJobExecutionID)
	if err != nil {
		t.Fatalf("GetExecution(legacy job) error = %v", err)
	}
	if legacyJob["dsl_sha256"] != legacyJobHash ||
		legacyJob["dsl_canonical_version"] != nil ||
		legacyJob["dsl_profile"] != nil {
		t.Fatalf("legacy job canonical metadata = %#v", legacyJob)
	}
	overview, err = store.Overview(ctx, actorID, OverviewRequest{
		ScopeType:  "project",
		ProjectID:  &projectID,
		WindowDays: 7,
	})
	if err != nil {
		t.Fatalf("Overview(legacy job) error = %v", err)
	}
	if overview["total_count"] != 3 {
		t.Fatalf("Overview(legacy job) total_count = %v, want 3", overview["total_count"])
	}

	if _, err := db.ExecContext(ctx, `
		UPDATE test_case_runs SET dsl_sha256 = $2 WHERE id = $1`,
		executionID, strings.Repeat("b", 64),
	); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetExecution(ctx, actorID, executionID); !errors.Is(err, ErrConflict) {
		t.Fatalf("GetExecution() mismatch error = %v, want ErrConflict", err)
	}
	if _, err := store.ListExecutions(ctx, actorID, ListRequest{
		ProjectID: &projectID,
		Limit:     20,
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("ListExecutions() mismatch error = %v, want ErrConflict", err)
	}
	if _, err := store.Overview(ctx, actorID, OverviewRequest{
		ScopeType:  "project",
		ProjectID:  &projectID,
		WindowDays: 7,
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("Overview() mismatch error = %v, want ErrConflict", err)
	}
	if _, err := store.BatchReport(ctx, actorID, batchID); !errors.Is(err, ErrConflict) {
		t.Fatalf("BatchReport() mismatch error = %v, want ErrConflict", err)
	}
}

func TestPostgresClaimStartAndFinishJobRun(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	if err := db.PingContext(ctx); err != nil {
		t.Fatal(err)
	}

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	var actorID, projectID, caseID, batchID int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO users (email, display_name)
		VALUES ($1, 'Execution Worker Test')
		RETURNING id`,
		"execution-worker-"+suffix+"@example.com",
	).Scan(&actorID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if projectID != 0 {
			_, _ = db.ExecContext(ctx, `DELETE FROM test_case_runs WHERE project_id = $1`, projectID)
			_, _ = db.ExecContext(ctx, `DELETE FROM execution_batches WHERE project_id = $1`, projectID)
			_, _ = db.ExecContext(ctx, `DELETE FROM projects WHERE id = $1`, projectID)
		}
		_, _ = db.ExecContext(ctx, `DELETE FROM users WHERE id = $1`, actorID)
	})
	if err := db.QueryRowContext(ctx, `
		INSERT INTO projects (name, description)
		VALUES ($1, 'execution worker integration test')
		RETURNING id`,
		"execution-worker-"+suffix,
	).Scan(&projectID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO project_members (project_id, user_id, role)
		VALUES ($1, $2, 'owner')`,
		projectID, actorID,
	); err != nil {
		t.Fatal(err)
	}
	rawCase := json.RawMessage(`{"name":"worker case","steps":[{"action":"goto","value":"https://example.com"}]}`)
	validated, err := dsl.ValidateExecutableCase(rawCase)
	if err != nil {
		t.Fatal(err)
	}
	hash := dsl.SHA256(validated.CanonicalJSON)
	if err := db.QueryRowContext(ctx, `
		INSERT INTO test_cases (project_id, created_by, updated_by, name, dsl)
		VALUES ($1, $2, $2, 'worker case', $3::json)
		RETURNING id`,
		projectID, actorID, string(validated.CanonicalJSON),
	).Scan(&caseID); err != nil {
		t.Fatal(err)
	}
	store := NewStore(db)
	batch, err := store.CreateBatch(ctx, actorID, BatchCreateRequest{
		ProjectID:   projectID,
		CaseIDs:     []int64{caseID},
		Concurrency: 1,
		InputValues: map[string]string{"email": "worker@example.com"},
		DSLBindings: map[int64]CanonicalDSLBinding{
			caseID: {
				CanonicalJSON: validated.CanonicalJSON,
				SHA256:        hash,
				Version:       validated.CanonicalVersion,
			},
		},
	})
	if err != nil {
		t.Fatalf("CreateBatch() error = %v", err)
	}
	batchID = batch["id"].(int64)

	job, ok, err := store.ClaimNextProjectJob(ctx, "test-worker", 60, projectID)
	if err != nil || !ok {
		t.Fatalf("ClaimNextProjectJob() = (%#v, %v, %v)", job, ok, err)
	}
	started, err := store.StartClaimedJobRun(ctx, job)
	if err != nil {
		t.Fatalf("StartClaimedJobRun() error = %v", err)
	}
	if started.ID < 1 || string(started.DSLCase) != string(validated.CanonicalJSON) {
		t.Fatalf("started run = %#v", started)
	}
	if started.InputValues["email"] != "worker@example.com" {
		t.Fatalf("input values = %#v", started.InputValues)
	}
	result := BrowserExecutionResult{
		Status: "passed",
		Report: json.RawMessage(
			`{"status":"passed","steps":[]}`,
		),
	}
	if err := store.FinishClaimedJobRun(ctx, "test-worker", job, started.ID, result); err != nil {
		t.Fatalf("FinishClaimedJobRun() error = %v", err)
	}
	report, err := store.BatchReport(ctx, actorID, batchID)
	if err != nil {
		t.Fatalf("BatchReport() error = %v", err)
	}
	if report["status"] != "passed" {
		t.Fatalf("batch status = %#v", report["status"])
	}
	jobs := report["jobs"].([]map[string]any)
	if jobs[0]["status"] != "passed" {
		t.Fatalf("job status = %#v", jobs[0]["status"])
	}
	latest := jobs[0]["latest_execution"].(map[string]any)
	if latest["status"] != "passed" || latest["dsl_sha256"] != hash {
		t.Fatalf("latest execution = %#v", latest)
	}
}

func assertPostgresCanonicalMetadata(t *testing.T, value map[string]any, hash string) {
	t.Helper()
	if value["dsl_sha256"] != hash ||
		value["dsl_canonical_version"] != dsl.CanonicalVersionV2 ||
		value["dsl_profile"] != string(dsl.ProfileResearchV1) {
		t.Fatalf("canonical metadata = %#v", value)
	}
}
