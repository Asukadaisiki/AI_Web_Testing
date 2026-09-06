package integration_test

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/cases"
	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/corrections"
	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/dsl"
	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/execution"
	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/projects"
	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/tools"
	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestPostgresControlPlaneLifecycle(t *testing.T) {
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
	actorID := insertUser(t, db, "owner-"+suffix+"@example.com")
	otherID := insertUser(t, db, "other-"+suffix+"@example.com")
	var projectID int64
	t.Cleanup(func() {
		if projectID > 0 {
			_, _ = db.ExecContext(ctx, `
				DELETE FROM locator_corrections
				WHERE source_execution_id IN (
					SELECT id FROM test_case_runs WHERE project_id = $1
				);
				DELETE FROM test_case_runs WHERE project_id = $1;
				DELETE FROM execution_batches WHERE project_id = $1;
				DELETE FROM projects WHERE id = $1`, projectID)
		}
		_, _ = db.ExecContext(ctx, `DELETE FROM users WHERE id = ANY($1)`, []int64{actorID, otherID})
	})

	projectStore := projects.NewPostgresStore(db)
	caseStore := cases.NewPostgresStore(db)
	dslStore := dsl.NewStore(db)
	executionStore := execution.NewStore(db)
	correctionStore := corrections.NewStore(db)
	controlPlane := tools.NewControlPlaneCapabilities(
		dslStore, caseStore, executionStore, passthroughValidator{},
	)

	description := "integration project"
	project, err := projectStore.Create(ctx, actorID, projects.CreateRequest{
		Name:        "go-control-plane-" + suffix,
		Description: &description,
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	projectID = project.ID
	if _, err := projectStore.Get(ctx, projectID, otherID); !errors.Is(err, projects.ErrNotFound) {
		t.Fatalf("unauthorized project get error = %v, want ErrNotFound", err)
	}

	baseURL := "https://example.com"
	generation, err := dslStore.CreateGeneration(
		ctx,
		actorID,
		projectID,
		json.RawMessage(`{
			"name":"generated checkout",
			"base_url":"https://example.com",
			"steps":[{"action":"goto","value":"/checkout"},{"action":"click","target":"Pay"}]
		}`),
		[]string{"verified by integration test"},
	)
	if err != nil {
		t.Fatalf("create DSL generation: %v", err)
	}
	if _, err := dslStore.GetGeneration(ctx, otherID, projectID, generation.ID); !errors.Is(err, dsl.ErrNotFound) {
		t.Fatalf("unauthorized DSL generation get error = %v, want ErrNotFound", err)
	}
	if _, err := db.ExecContext(ctx, `
		UPDATE dsl_generation_runs
		SET generated_case_json = $2::json, dsl_sha256 = NULL, dsl_canonical_version = NULL
		WHERE id = $1`,
		generation.ID,
		`{"name":"generated checkout","base_url":"https://example.com","steps":[{"action":"goto","value":"/checkout"},{"action":"click","target":"Pay"}]}`,
	); err != nil {
		t.Fatalf("prepare legacy DSL generation: %v", err)
	}
	backfilledGeneration, err := dslStore.GetGeneration(ctx, actorID, projectID, generation.ID)
	if err != nil {
		t.Fatalf("backfill legacy DSL generation: %v", err)
	}
	var backfilledHash, backfilledVersion string
	var backfilledCase string
	if err := db.QueryRowContext(ctx, `
		SELECT generated_case_json, dsl_sha256, dsl_canonical_version
		FROM dsl_generation_runs WHERE id = $1`,
		generation.ID,
	).Scan(&backfilledCase, &backfilledHash, &backfilledVersion); err != nil {
		t.Fatalf("read backfilled DSL generation: %v", err)
	}
	if backfilledHash != backfilledGeneration.DSLHash ||
		backfilledVersion != dsl.CanonicalVersion ||
		backfilledCase != string(backfilledGeneration.Case) {
		t.Fatalf("legacy DSL generation was not canonically backfilled")
	}
	generatedRaw, err := controlPlane.GenerateDSL(
		ctx,
		actorID,
		projectID,
		"",
		json.RawMessage(`{
			"case":{"name":"tool generated","steps":[{"action":"click","target":"Pay"}]},
			"a11y_nodes_by_state":{"S0":[]}
		}`),
	)
	if err != nil {
		t.Fatalf("generate DSL through Go control plane: %v", err)
	}
	var generated struct {
		GenerationID     int64           `json:"generation_id"`
		Case             json.RawMessage `json:"case"`
		DSLHash          string          `json:"dsl_sha256"`
		CanonicalVersion string          `json:"dsl_canonical_version"`
	}
	if err := json.Unmarshal(generatedRaw, &generated); err != nil {
		t.Fatal(err)
	}
	var generatedCase struct {
		Steps []struct {
			TargetStrategy    *string `json:"target_strategy"`
			LocatorConfidence *string `json:"locator_confidence"`
		} `json:"steps"`
	}
	if err := json.Unmarshal(generated.Case, &generatedCase); err != nil {
		t.Fatal(err)
	}
	if len(generatedCase.Steps) != 1 ||
		generatedCase.Steps[0].TargetStrategy != nil ||
		generatedCase.Steps[0].LocatorConfidence != nil {
		t.Fatalf("semantic generation optional locator fields = %#v", generatedCase.Steps)
	}
	executedRaw, err := controlPlane.ExecuteDSL(
		ctx,
		actorID,
		"run-"+suffix,
		projectID,
		"",
		json.RawMessage(fmt.Sprintf(`{"generation_id":%d}`, generated.GenerationID)),
	)
	if err != nil {
		t.Fatalf("execute DSL through Go control plane: %v", err)
	}
	var executed map[string]any
	if err := json.Unmarshal(executedRaw, &executed); err != nil {
		t.Fatal(err)
	}
	if executed["report_api_url"] != fmt.Sprintf("/api/v2/execution-batches/%v/report", executed["batch_id"]) {
		t.Fatalf("execution result = %#v", executed)
	}
	approvedBatchID := int64(executed["batch_id"].(float64))
	var approvedJobID, approvedCaseID int64
	var generationCase, persistedCase, jobSnapshot, jobCanonicalJSON, jobHash, jobVersion string
	if err := db.QueryRowContext(ctx, `
		SELECT j.id, tc.id, g.generated_case_json, tc.dsl, j.dsl_snapshot,
		       j.dsl_canonical_json, j.dsl_sha256, j.dsl_canonical_version
		FROM dsl_generation_runs g
		JOIN execution_batches b ON b.id = $2
		JOIN execution_jobs j ON j.batch_id = b.id
		JOIN test_cases tc ON tc.id = j.case_id
		WHERE g.id = $1`,
		generated.GenerationID, approvedBatchID,
	).Scan(
		&approvedJobID, &approvedCaseID,
		&generationCase, &persistedCase, &jobSnapshot,
		&jobCanonicalJSON, &jobHash, &jobVersion,
	); err != nil {
		t.Fatalf("read canonical DSL binding: %v", err)
	}
	if jobHash != generated.DSLHash || jobCanonicalJSON != string(generated.Case) ||
		jobVersion != generated.CanonicalVersion {
		t.Fatalf("job canonical binding = (%s, %s, %s), want (%s, %s, %s)",
			jobVersion, jobHash, jobCanonicalJSON,
			generated.CanonicalVersion, generated.DSLHash, generated.Case)
	}
	var sameSemantics bool
	if err := db.QueryRowContext(ctx, `
		SELECT $1::jsonb = $2::jsonb AND $2::jsonb = $3::jsonb`,
		generationCase, persistedCase, jobSnapshot,
	).Scan(&sameSemantics); err != nil || !sameSemantics {
		t.Fatalf("generation/case/job DSL mismatch: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO test_case_runs (
			case_id, project_id, batch_id, job_id, triggered_by, status,
			attempt_number, dsl_snapshot, dsl_sha256, report_schema_version,
			report, analysis_status, started_at, finished_at
		) VALUES (
			$1, $2, $3, $4, $5, 'passed',
			1, $6::json, $7, 'execution.report.v1',
			'{"status":"passed","steps":[]}'::json, 'skipped', now(), now()
		)`,
		approvedCaseID, projectID, approvedBatchID, approvedJobID, actorID,
		jobSnapshot, jobHash,
	); err != nil {
		t.Fatalf("persist canonical execution report: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		UPDATE execution_jobs SET status = 'passed', attempt_count = 1, finished_at = now()
		WHERE id = $1`, approvedJobID,
	); err != nil {
		t.Fatalf("finalize canonical execution job: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		UPDATE execution_batches SET status = 'passed', finished_at = now()
		WHERE id = $1`, approvedBatchID,
	); err != nil {
		t.Fatalf("finalize canonical execution batch: %v", err)
	}
	approvedReport, err := executionStore.BatchReport(ctx, actorID, approvedBatchID)
	if err != nil {
		t.Fatalf("read canonical execution report: %v", err)
	}
	approvedJobs := approvedReport["jobs"].([]map[string]any)
	approvedExecution := approvedJobs[0]["latest_execution"].(map[string]any)
	if approvedExecution["dsl_sha256"] != generated.DSLHash {
		t.Fatalf("report DSL SHA = %v, want %s", approvedExecution["dsl_sha256"], generated.DSLHash)
	}
	reportSnapshot, err := json.Marshal(approvedExecution["dsl_snapshot"])
	if err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT $1::jsonb = $2::jsonb`,
		string(generated.Case), string(reportSnapshot),
	).Scan(&sameSemantics); err != nil || !sameSemantics {
		t.Fatalf("generation/report DSL mismatch: %v", err)
	}

	testCase, err := caseStore.Create(ctx, actorID, cases.Mutation{
		ProjectID: projectID,
		Name:      "checkout",
		BaseURL:   &baseURL,
		Steps:     json.RawMessage(`[{"action":"goto","value":"/checkout"},{"action":"click","target":"Pay"}]`),
	})
	if err != nil {
		t.Fatalf("create case: %v", err)
	}
	page, err := caseStore.List(ctx, actorID, &projectID, "check", 1, 20)
	if err != nil || page.Total != 1 {
		t.Fatalf("list cases = %#v, %v", page, err)
	}

	idempotencyKey := "batch-" + suffix
	batch, err := executionStore.CreateBatch(ctx, actorID, execution.BatchCreateRequest{
		ProjectID:      projectID,
		CaseIDs:        []int64{testCase.ID, testCase.ID},
		IdempotencyKey: &idempotencyKey,
		Concurrency:    2,
	})
	if err != nil {
		t.Fatalf("create batch: %v", err)
	}
	batchID := batch["id"].(int64)
	var storedInputValues string
	if err := db.QueryRowContext(
		ctx,
		`SELECT input_values_json FROM execution_batches WHERE id = $1`,
		batchID,
	).Scan(&storedInputValues); err != nil || storedInputValues != "{}" {
		t.Fatalf("stored input values = %q, %v", storedInputValues, err)
	}
	jobs := batch["jobs"].([]map[string]any)
	if len(jobs) != 1 {
		t.Fatalf("batch job count = %d, want 1", len(jobs))
	}
	jobID := jobs[0]["id"].(int64)

	replayed, err := executionStore.CreateBatch(ctx, actorID, execution.BatchCreateRequest{
		ProjectID:      projectID,
		CaseIDs:        []int64{testCase.ID},
		IdempotencyKey: &idempotencyKey,
		Concurrency:    1,
	})
	if err != nil || replayed["id"] != batchID {
		t.Fatalf("idempotent batch = %#v, %v", replayed, err)
	}

	report := `{"status":"failed","steps":[{"step_index":0,"action":"click","status":"failed","duration_ms":50,"error_message":"Element not found","url":"https://example.com/orders/123","screenshot_path":"artifacts/run.png"}]}`
	failure := `{"category":"locator","fingerprint":"locator-test","title":"Element not found"}`
	finishedAt := time.Now().UTC()
	startedAt := finishedAt.Add(-2 * time.Second)
	var executionID int64
	err = db.QueryRowContext(ctx, `
		INSERT INTO test_case_runs (
			case_id, project_id, batch_id, job_id, triggered_by, status,
			attempt_number, dsl_snapshot, dsl_sha256, report_schema_version,
			error_message, report, failure_signal_json, analysis_status,
			started_at, finished_at
		) VALUES (
			$1, $2, $3, $4, $5, 'failed',
			1, '{}'::json, 'sha', 'execution.report.v1',
			'Element not found', $6::json, $7::json, 'completed',
			$8, $9
		)
		RETURNING id`,
		testCase.ID, projectID, batchID, jobID, actorID, report, failure, startedAt, finishedAt,
	).Scan(&executionID)
	if err != nil {
		t.Fatalf("insert execution: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		UPDATE execution_jobs
		SET status = 'failed', attempt_count = 1, started_at = now() - interval '2 seconds', finished_at = now()
		WHERE id = $1`, jobID); err != nil {
		t.Fatalf("finalize job: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		UPDATE execution_batches
		SET status = 'failed', started_at = now() - interval '2 seconds', finished_at = now()
		WHERE id = $1`, batchID); err != nil {
		t.Fatalf("finalize batch: %v", err)
	}

	detail, err := executionStore.GetExecution(ctx, actorID, executionID)
	if err != nil || detail["failure_category"] != "locator" {
		t.Fatalf("get execution = %#v, %v", detail, err)
	}
	overview, err := executionStore.Overview(ctx, actorID, execution.OverviewRequest{
		ScopeType:  "project",
		ProjectID:  &projectID,
		WindowDays: 7,
	})
	if err != nil {
		t.Fatalf("execution overview: %v", err)
	}
	if overview["failed_count"] != 1 || len(overview["trend_points"].([]map[string]any)) != 7 {
		t.Fatalf("unexpected overview: %#v", overview)
	}
	reportArguments := json.RawMessage(fmt.Sprintf(`{"batch_id":%d}`, batchID))
	reportRaw, err := controlPlane.GetReport(ctx, actorID, projectID, "", reportArguments)
	if err != nil {
		t.Fatalf("get report through Go control plane: %v", err)
	}
	var batchReport map[string]any
	if err := json.Unmarshal(reportRaw, &batchReport); err != nil || batchReport["status"] != "failed" {
		t.Fatalf("batch report = %#v, %v", batchReport, err)
	}
	repairRaw, err := controlPlane.PrepareFixAndRetry(
		ctx, actorID, projectID, "", reportArguments,
	)
	if err != nil {
		t.Fatalf("prepare repair through Go control plane: %v", err)
	}
	var repair map[string]any
	if err := json.Unmarshal(repairRaw, &repair); err != nil || repair["strategy"] != "re_explore" {
		t.Fatalf("repair result = %#v, %v", repair, err)
	}

	correction, err := correctionStore.Create(ctx, actorID, corrections.CreateRequest{
		PageURL:           "https://example.com/orders/123",
		TargetDescription: " Pay button ",
		CorrectionType:    "css",
		CorrectionValue:   "#pay",
		SourceExecutionID: executionID,
	})
	if err != nil {
		t.Fatalf("create correction: %v", err)
	}
	if correction["page_url_pattern"] != "https://example.com/orders/*" {
		t.Fatalf("correction pattern = %v", correction["page_url_pattern"])
	}

	if err := projectStore.Delete(ctx, projectID, actorID); err != nil {
		t.Fatalf("delete project with execution history: %v", err)
	}
	projectID = 0
	if _, err := projectStore.Get(ctx, project.ID, actorID); !errors.Is(err, projects.ErrNotFound) {
		t.Fatalf("deleted project get error = %v, want ErrNotFound", err)
	}
}

func insertUser(t *testing.T, db *sql.DB, email string) int64 {
	t.Helper()
	var id int64
	if err := db.QueryRow(`
		INSERT INTO users (email, display_name)
		VALUES ($1, 'Integration Test')
		RETURNING id`, email).Scan(&id); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	return id
}

type passthroughValidator struct{}

func (passthroughValidator) ExecuteBrowserCapability(
	_ context.Context,
	_ string,
	_ int64,
	_ int64,
	_ string,
	arguments json.RawMessage,
) (json.RawMessage, error) {
	var request struct {
		Case     json.RawMessage `json:"dsl_case"`
		Evidence json.RawMessage `json:"a11y_nodes_by_state"`
	}
	if err := json.Unmarshal(arguments, &request); err != nil {
		return nil, err
	}
	caseHash := sha256.Sum256(request.Case)
	evidenceHash := sha256.Sum256(request.Evidence)
	return json.Marshal(map[string]any{
		"dsl_case":        request.Case,
		"valid":           true,
		"validation_mode": "dsl_case",
		"case_digest":     hex.EncodeToString(caseHash[:]),
		"evidence_digest": hex.EncodeToString(evidenceHash[:]),
		"warnings":        []string{},
	})
}
