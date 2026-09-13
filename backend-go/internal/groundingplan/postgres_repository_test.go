package groundingplan

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/browsercontract"
	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/dbschema"
	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/taskplan"
	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestPostgresRepositoryPersistsImmutableGroundingPlanRevisions(t *testing.T) {
	fixture := newGroundingPostgresFixture(t)
	taskPlan := fixture.createTaskPlan("Submit the form")
	service := NewService(NewPostgresRepository(fixture.db))

	initial, err := service.EnsureForTaskPlan(fixture.ctx, taskPlan)
	if err != nil {
		t.Fatal(err)
	}
	var initialJSON, initialHash string
	if err := fixture.db.QueryRowContext(
		fixture.ctx,
		`SELECT content_json::text, content_sha256
		   FROM grounding_plans
		  WHERE task_plan_id = $1 AND revision = 1`,
		taskPlan.ID,
	).Scan(&initialJSON, &initialHash); err != nil {
		t.Fatal(err)
	}

	ref := testCandidateRef("candidate-postgres")
	updated, err := service.RecordObservationQuery(
		fixture.ctx,
		taskPlan.RunID,
		QueryRecord{
			PlanStepID:     taskPlan.Steps[0].ID,
			SourceEventSeq: ref.SourceEventSeq,
			ObservationID:  ref.ObservationID,
			Action:         taskPlan.Steps[0].Action,
			Query:          "submit",
			Role:           "button",
			Limit:          20,
		},
		[]CandidateOption{{
			CandidateRef: ref,
			ElementRef:   "state-1:7",
			Role:         "button",
			Name:         "Submit",
			Locator: browsercontract.LocatorSpec{
				Kind: "role", Role: "button",
				Name: stringPointer("Submit"), Exact: true,
			},
			Provenance:    "a11y_exact",
			ObservedCount: 1,
		}},
	)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Revision != 2 ||
		updated.Steps[0].Status != StepCandidatesAvailable {
		t.Fatalf("updated plan = %#v", updated)
	}

	var rows int
	if err := fixture.db.QueryRowContext(
		fixture.ctx,
		`SELECT count(*) FROM grounding_plans WHERE task_plan_id = $1`,
		taskPlan.ID,
	).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 2 {
		t.Fatalf("revision rows = %d, want 2", rows)
	}
	var persistedInitialJSON, persistedInitialHash string
	if err := fixture.db.QueryRowContext(
		fixture.ctx,
		`SELECT content_json::text, content_sha256
		   FROM grounding_plans
		  WHERE task_plan_id = $1 AND revision = 1`,
		taskPlan.ID,
	).Scan(&persistedInitialJSON, &persistedInitialHash); err != nil {
		t.Fatal(err)
	}
	if persistedInitialJSON != initialJSON || persistedInitialHash != initialHash {
		t.Fatalf(
			"initial row changed\njson: %t\nhash: %s != %s",
			persistedInitialJSON == initialJSON,
			persistedInitialHash,
			initialHash,
		)
	}
	current, err := service.repository.GetCurrent(fixture.ctx, taskPlan.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.ID != updated.ID ||
		current.ContentSHA256 != updated.ContentSHA256 ||
		current.Revision != 2 {
		t.Fatalf("current plan = %#v, want %#v", current, updated)
	}
	if initial.ID == updated.ID {
		t.Fatal("immutable revisions reused the same primary key")
	}
}

func TestPostgresRepositorySupersedesOldPlanForTaskPlanRevision(t *testing.T) {
	fixture := newGroundingPostgresFixture(t)
	taskPlanService := taskplan.NewService(taskplan.NewPostgresRepository(fixture.db))
	firstTaskPlan := fixture.createTaskPlanWithService(
		taskPlanService,
		"Submit the form",
	)
	service := NewService(NewPostgresRepository(fixture.db))
	first, err := service.EnsureForTaskPlan(fixture.ctx, firstTaskPlan)
	if err != nil {
		t.Fatal(err)
	}

	secondTaskPlan := fixture.createTaskPlanWithService(
		taskPlanService,
		"Submit the revised form",
	)
	second, err := service.EnsureForTaskPlan(fixture.ctx, secondTaskPlan)
	if err != nil {
		t.Fatal(err)
	}
	oldCurrent, err := service.repository.GetCurrent(fixture.ctx, firstTaskPlan.ID)
	if err != nil {
		t.Fatal(err)
	}
	if oldCurrent.Status != StatusSuperseded ||
		oldCurrent.Revision != first.Revision+1 {
		t.Fatalf("old current = %#v", oldCurrent)
	}
	if second.Status != StatusActive ||
		second.Revision != 1 ||
		second.TaskPlanBinding() != secondTaskPlan.Binding() {
		t.Fatalf("new current = %#v", second)
	}

	var oldRows, newRows int
	if err := fixture.db.QueryRowContext(
		fixture.ctx,
		`SELECT
			count(*) FILTER (WHERE task_plan_id = $1),
			count(*) FILTER (WHERE task_plan_id = $2)
		   FROM grounding_plans
		  WHERE run_id = $3`,
		firstTaskPlan.ID,
		secondTaskPlan.ID,
		firstTaskPlan.RunID,
	).Scan(&oldRows, &newRows); err != nil {
		t.Fatal(err)
	}
	if oldRows != 2 || newRows != 1 {
		t.Fatalf("revision row counts old=%d new=%d", oldRows, newRows)
	}
}

func TestPostgresRepositoryRollsBackSupersedeWhenReplacementInsertFails(
	t *testing.T,
) {
	fixture := newGroundingPostgresFixture(t)
	taskPlanService := taskplan.NewService(taskplan.NewPostgresRepository(fixture.db))
	firstTaskPlan := fixture.createTaskPlanWithService(
		taskPlanService,
		"Submit the form",
	)
	service := NewService(NewPostgresRepository(fixture.db))
	first, err := service.EnsureForTaskPlan(fixture.ctx, firstTaskPlan)
	if err != nil {
		t.Fatal(err)
	}
	secondTaskPlan := fixture.createTaskPlanWithService(
		taskPlanService,
		"Submit the revised form",
	)

	fixedNow := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return fixedNow }
	collision := clonePlan(first)
	collision.ID = revisionID(Plan{
		RunID: secondTaskPlan.RunID, TaskPlanID: secondTaskPlan.ID,
		Revision: 1, Status: StatusActive,
	}, fixedNow)
	collision.Revision++
	collision.UpdatedAt = fixedNow.Add(-time.Second)
	collision.ContentSHA256, err = contentHash(collision)
	if err != nil {
		t.Fatal(err)
	}
	if err := insertPlan(fixture.ctx, fixture.db, collision); err != nil {
		t.Fatal(err)
	}

	if _, err := service.EnsureForTaskPlan(
		fixture.ctx,
		secondTaskPlan,
	); err == nil {
		t.Fatal("replacement with colliding revision ID succeeded")
	}

	var revision, rows int
	var status Status
	if err := fixture.db.QueryRowContext(
		fixture.ctx,
		`SELECT max(revision), count(*),
		        (array_agg(status ORDER BY revision DESC))[1]
		   FROM grounding_plans
		  WHERE task_plan_id = $1`,
		firstTaskPlan.ID,
	).Scan(&revision, &rows, &status); err != nil {
		t.Fatal(err)
	}
	if revision != 2 || rows != 2 || status != StatusActive {
		t.Fatalf(
			"old plan after failed replacement: revision=%d rows=%d status=%q",
			revision,
			rows,
			status,
		)
	}
}

func TestPostgresRepositorySerializesConcurrentEnsureForTaskPlan(t *testing.T) {
	fixture := newGroundingPostgresFixture(t)
	taskPlanService := taskplan.NewService(taskplan.NewPostgresRepository(fixture.db))
	firstTaskPlan := fixture.createTaskPlanWithService(
		taskPlanService,
		"Submit the form",
	)
	service := NewService(NewPostgresRepository(fixture.db))
	if _, err := service.EnsureForTaskPlan(fixture.ctx, firstTaskPlan); err != nil {
		t.Fatal(err)
	}
	secondTaskPlan := fixture.createTaskPlanWithService(
		taskPlanService,
		"Submit the revised form",
	)

	const workers = 2
	start := make(chan struct{})
	results := make(chan Plan, workers)
	errs := make(chan error, workers)
	var wait sync.WaitGroup
	for range workers {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			plan, err := service.EnsureForTaskPlan(fixture.ctx, secondTaskPlan)
			results <- plan
			errs <- err
		}()
	}
	close(start)
	wait.Wait()
	close(results)
	close(errs)

	var resultID string
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	for plan := range results {
		if resultID == "" {
			resultID = plan.ID
		}
		if plan.ID != resultID {
			t.Fatalf("concurrent Ensure returned IDs %q and %q", resultID, plan.ID)
		}
	}

	var oldRows, newRows, activeRows int
	if err := fixture.db.QueryRowContext(
		fixture.ctx,
		`SELECT
			count(*) FILTER (WHERE task_plan_id = $1),
			count(*) FILTER (WHERE task_plan_id = $2),
			count(*) FILTER (
				WHERE status <> 'superseded'
				AND revision = (
					SELECT max(current.revision)
					FROM grounding_plans current
					WHERE current.task_plan_id = grounding_plans.task_plan_id
				)
			)
		   FROM grounding_plans
		  WHERE run_id = $3`,
		firstTaskPlan.ID,
		secondTaskPlan.ID,
		firstTaskPlan.RunID,
	).Scan(&oldRows, &newRows, &activeRows); err != nil {
		t.Fatal(err)
	}
	if oldRows != 2 || newRows != 1 || activeRows != 1 {
		t.Fatalf(
			"concurrent rows old=%d new=%d active=%d",
			oldRows,
			newRows,
			activeRows,
		)
	}
}

type groundingPostgresFixture struct {
	t         *testing.T
	ctx       context.Context
	db        *sql.DB
	runID     string
	actorID   int64
	projectID int64
}

func newGroundingPostgresFixture(t *testing.T) *groundingPostgresFixture {
	t.Helper()
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	ctx := context.Background()
	if err := db.PingContext(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, dbschema.TaskPlanMigrationSQL); err != nil {
		t.Fatalf("apply task plan migration: %v", err)
	}
	if _, err := db.ExecContext(ctx, dbschema.GroundingPlanMigrationSQL); err != nil {
		t.Fatalf("apply grounding plan migration: %v", err)
	}

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	fixture := &groundingPostgresFixture{
		t: t, ctx: ctx, db: db, runID: "run_grounding_" + suffix,
	}
	if err := db.QueryRowContext(
		ctx,
		`INSERT INTO users (email, display_name)
		 VALUES ($1, 'GroundingPlan Test') RETURNING id`,
		"groundingplan-"+suffix+"@example.com",
	).Scan(&fixture.actorID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(
		ctx,
		`INSERT INTO projects (name, description)
		 VALUES ($1, 'GroundingPlan Test') RETURNING id`,
		"groundingplan-"+suffix,
	).Scan(&fixture.projectID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(
		ctx,
		`INSERT INTO agent_runs (
			id, actor_user_id, conversation_id, project_id, status, input,
			transcript_json, last_event_seq, created_at, updated_at
		) VALUES ($1, $2, $3, $4, 'running', 'goal', '[]'::json, 0, now(), now())`,
		fixture.runID,
		fixture.actorID,
		"conversation-"+suffix,
		fixture.projectID,
	); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM agent_runs WHERE id = $1`, fixture.runID)
		_, _ = db.ExecContext(ctx, `DELETE FROM projects WHERE id = $1`, fixture.projectID)
		_, _ = db.ExecContext(ctx, `DELETE FROM users WHERE id = $1`, fixture.actorID)
	})
	return fixture
}

func (f *groundingPostgresFixture) createTaskPlan(goal string) taskplan.Plan {
	f.t.Helper()
	return f.createTaskPlanWithService(
		taskplan.NewService(taskplan.NewPostgresRepository(f.db)),
		goal,
	)
}

func (f *groundingPostgresFixture) createTaskPlanWithService(
	service *taskplan.Service,
	goal string,
) taskplan.Plan {
	f.t.Helper()
	plan, err := service.CreateVersion(f.ctx, taskplan.CreateRequest{
		RunID: f.runID, ActorUserID: f.actorID, ProjectID: f.projectID,
		Definition: taskplan.Definition{
			Goal: goal, MaxSideEffect: taskplan.SideEffectBrowserState,
			ForbiddenActions: []string{},
			Steps: []taskplan.StepDefinition{{
				ID: "submit", Intent: goal, Action: "click", Target: goal,
				ExpectedOccurrences: 1, Idempotency: "idempotent",
				SideEffect:           taskplan.SideEffectBrowserState,
				Preconditions:        []string{},
				CompletionConditions: []string{"submitted"},
			}},
		},
	})
	if err != nil {
		f.t.Fatal(err)
	}
	if len(plan.PlanSHA256) != 64 ||
		plan.PlanSHA256 != strings.ToLower(plan.PlanSHA256) {
		f.t.Fatalf("task plan hash = %q", plan.PlanSHA256)
	}
	return plan
}
