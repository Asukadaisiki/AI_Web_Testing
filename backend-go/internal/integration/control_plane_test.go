package integration_test

import (
	"context"
	"database/sql"
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
	generatedRaw, err := controlPlane.GenerateDSL(
		ctx,
		actorID,
		projectID,
		"",
		json.RawMessage(`{
			"case":{"name":"tool generated","steps":[{"action":"goto","value":"https://example.com"}]},
			"a11y_nodes_by_state":{"S0":[]}
		}`),
	)
	if err != nil {
		t.Fatalf("generate DSL through Go control plane: %v", err)
	}
	var generated struct {
		GenerationID int64 `json:"generation_id"`
	}
	if err := json.Unmarshal(generatedRaw, &generated); err != nil {
		t.Fatal(err)
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
		Case json.RawMessage `json:"dsl_case"`
	}
	if err := json.Unmarshal(arguments, &request); err != nil {
		return nil, err
	}
	return json.Marshal(map[string]any{
		"dsl_case": request.Case,
		"valid":    true,
		"warnings": []string{},
	})
}
