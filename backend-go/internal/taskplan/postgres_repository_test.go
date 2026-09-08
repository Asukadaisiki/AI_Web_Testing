package taskplan

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/dbschema"
	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestPostgresRepositoryPersistsVersionedTaskPlan(t *testing.T) {
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
	if _, err := db.ExecContext(ctx, dbschema.TaskPlanMigrationSQL); err != nil {
		t.Fatalf("apply task plan migration: %v", err)
	}

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	var actorID, projectID int64
	if err := db.QueryRowContext(
		ctx,
		`INSERT INTO users (email, display_name)
		 VALUES ($1, 'TaskPlan Test') RETURNING id`,
		"taskplan-"+suffix+"@example.com",
	).Scan(&actorID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(
		ctx,
		`INSERT INTO projects (name, description)
		 VALUES ($1, 'TaskPlan Test') RETURNING id`,
		"taskplan-"+suffix,
	).Scan(&projectID); err != nil {
		t.Fatal(err)
	}
	runID := "run_taskplan_" + suffix
	if _, err := db.ExecContext(
		ctx,
		`INSERT INTO agent_runs (
			id, actor_user_id, conversation_id, project_id, status, input,
			transcript_json, last_event_seq, created_at, updated_at
		) VALUES ($1, $2, $3, $4, 'running', 'goal', '[]'::json, 0, now(), now())`,
		runID,
		actorID,
		"conversation-"+suffix,
		projectID,
	); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM agent_runs WHERE id = $1`, runID)
		_, _ = db.ExecContext(ctx, `DELETE FROM projects WHERE id = $1`, projectID)
		_, _ = db.ExecContext(ctx, `DELETE FROM users WHERE id = $1`, actorID)
	})

	service := NewService(NewPostgresRepository(db))
	request := CreateRequest{
		RunID: runID, ActorUserID: actorID, ProjectID: projectID,
		Definition: Definition{
			Goal: "goal", MaxSideEffect: SideEffectBrowserState,
			ForbiddenActions: []string{"checkout"},
			Steps: []StepDefinition{{
				ID: "open", Intent: "Open page", Action: "goto",
				Target: "Page", Value: "https://example.test",
				ExpectedOccurrences: 1, Idempotency: "idempotent",
				SideEffect:           SideEffectBrowserState,
				Preconditions:        []string{},
				CompletionConditions: []string{"page visible"},
			}},
		},
	}
	first, err := service.CreateVersion(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	request.Definition.Steps[0].Value = "https://example.test/next"
	second, err := service.CreateVersion(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	current, err := service.GetCurrent(ctx, runID)
	if err != nil {
		t.Fatal(err)
	}
	if first.Version != 1 || second.Version != 2 ||
		current.ID != second.ID ||
		current.Steps[0].Value != "https://example.test/next" {
		t.Fatalf(
			"versions first=%#v second=%#v current=%#v",
			first,
			second,
			current,
		)
	}
}
