package research_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/dsl"
	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/research"
	"github.com/jackc/pgx/v5/pgconn"
	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestPostgresRepositoryCRUDCASAndFullIDChain(t *testing.T) {
	fixture := newPostgresFixture(t, true)
	repository := research.NewPostgresRepository(fixture.db)
	experiment := fixture.createExperiment(t, repository, "lifecycle", 20)

	listed, err := repository.ListExperiments(
		fixture.ctx,
		research.ExperimentFilter{ProjectID: &fixture.projectID},
	)
	if err != nil || len(listed) != 1 || listed[0].ID != experiment.ID {
		t.Fatalf("ListExperiments() = %#v, %v", listed, err)
	}
	if _, err := repository.CreateExperiment(fixture.ctx, experiment); !errors.Is(err, research.ErrConflict) {
		t.Fatalf("duplicate CreateExperiment() error = %v, want ErrConflict", err)
	}

	run := fixture.newRun(experiment.ID, "lifecycle-run", 0)
	persisted, err := repository.CreateRun(fixture.ctx, run)
	if err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}
	if persisted.Status != research.RunStatusPending {
		t.Fatalf("CreateRun() status = %s, want pending", persisted.Status)
	}

	badLinks := fixture.links
	badLinks.BatchID = &fixture.mismatchedBatchID
	if _, err := repository.UpdateRunLinks(
		fixture.ctx, run.ID, badLinks, time.Time{},
	); !errors.Is(err, research.ErrBrokenLink) {
		t.Fatalf("mismatched UpdateRunLinks() error = %v, want ErrBrokenLink", err)
	}
	unchanged, err := repository.GetRun(fixture.ctx, run.ID)
	if err != nil || unchanged.Links.AgentRunID != nil {
		t.Fatalf("failed link update was not rolled back: %#v, %v", unchanged.Links, err)
	}

	linked, err := repository.UpdateRunLinks(
		fixture.ctx, run.ID, fixture.links, time.Time{},
	)
	if err != nil {
		t.Fatalf("UpdateRunLinks() error = %v", err)
	}
	if linked.Links.ExecutionID == nil || *linked.Links.ExecutionID != fixture.executionID {
		t.Fatalf("linked run = %#v", linked.Links)
	}

	running, err := repository.CompareAndSwapRunStatus(
		fixture.ctx,
		run.ID,
		research.RunStatusPending,
		research.RunStatusRunning,
		time.Time{},
	)
	if err != nil || running.StartedAt == nil || running.FinishedAt != nil {
		t.Fatalf("start run = %#v, %v", running, err)
	}
	if _, err := repository.CompareAndSwapRunStatus(
		fixture.ctx,
		run.ID,
		research.RunStatusPending,
		research.RunStatusCancelled,
		time.Time{},
	); !errors.Is(err, research.ErrConflict) {
		t.Fatalf("stale run CAS error = %v, want ErrConflict", err)
	}
	completed, err := repository.CompareAndSwapRunStatus(
		fixture.ctx,
		run.ID,
		research.RunStatusRunning,
		research.RunStatusCompleted,
		time.Time{},
	)
	if err != nil || completed.FinishedAt == nil {
		t.Fatalf("complete run = %#v, %v", completed, err)
	}
	replayedCompletion, err := repository.CompareAndSwapRunStatus(
		fixture.ctx,
		run.ID,
		research.RunStatusCompleted,
		research.RunStatusCompleted,
		time.Now().UTC().Add(time.Hour),
	)
	if err != nil || replayedCompletion.FinishedAt == nil ||
		!replayedCompletion.FinishedAt.Equal(*completed.FinishedAt) ||
		!replayedCompletion.UpdatedAt.Equal(completed.UpdatedAt) {
		t.Fatalf("idempotent completion changed timestamps: %#v, %v", replayedCompletion, err)
	}
	if _, err := repository.CompareAndSwapRunStatus(
		fixture.ctx,
		run.ID,
		research.RunStatusRunning,
		research.RunStatusFailed,
		time.Time{},
	); !errors.Is(err, research.ErrTerminalStatus) {
		t.Fatalf("terminal run CAS error = %v, want ErrTerminalStatus", err)
	}

	metrics := unavailableMetrics()
	withMetrics, err := repository.PutRunMetrics(fixture.ctx, run.ID, metrics, time.Time{})
	if err != nil || withMetrics.Metrics == nil {
		t.Fatalf("PutRunMetrics() = %#v, %v", withMetrics.Metrics, err)
	}
	cancelledRun := fixture.newRun(experiment.ID, "cancel-before-start", 1)
	if _, err := repository.CreateRun(fixture.ctx, cancelledRun); err != nil {
		t.Fatal(err)
	}
	cancelled, err := repository.CompareAndSwapRunStatus(
		fixture.ctx,
		cancelledRun.ID,
		research.RunStatusPending,
		research.RunStatusCancelled,
		time.Time{},
	)
	if err != nil || cancelled.StartedAt != nil || cancelled.FinishedAt == nil {
		t.Fatalf("cancel pending run = %#v, %v", cancelled, err)
	}
	if err := repository.DeleteRun(fixture.ctx, cancelledRun.ID); err != nil {
		t.Fatalf("DeleteRun() error = %v", err)
	}
	if _, err := repository.GetRun(fixture.ctx, cancelledRun.ID); !errors.Is(err, research.ErrNotFound) {
		t.Fatalf("deleted GetRun() error = %v, want ErrNotFound", err)
	}
	runs, err := repository.ListRuns(
		fixture.ctx,
		research.RunFilter{ExperimentID: &experiment.ID},
	)
	if err != nil || len(runs) != 1 || runs[0].ID != run.ID {
		t.Fatalf("ListRuns() = %#v, %v", runs, err)
	}

	active, err := repository.CompareAndSwapExperimentStatus(
		fixture.ctx,
		experiment.ID,
		research.ExperimentStatusDraft,
		research.ExperimentStatusActive,
		time.Time{},
	)
	if err != nil || active.Status != research.ExperimentStatusActive {
		t.Fatalf("activate experiment = %#v, %v", active, err)
	}
	if _, err := repository.CompareAndSwapExperimentStatus(
		fixture.ctx,
		experiment.ID,
		research.ExperimentStatusDraft,
		research.ExperimentStatusCancelled,
		time.Time{},
	); !errors.Is(err, research.ErrConflict) {
		t.Fatalf("stale experiment CAS error = %v, want ErrConflict", err)
	}
	if err := repository.DeleteExperiment(fixture.ctx, experiment.ID); err != nil {
		t.Fatalf("DeleteExperiment() error = %v", err)
	}
	if _, err := repository.GetExperiment(
		fixture.ctx,
		experiment.ID,
	); !errors.Is(err, research.ErrNotFound) {
		t.Fatalf("deleted GetExperiment() error = %v, want ErrNotFound", err)
	}
}

func TestPostgresRepositoryConcurrentCreateRun(t *testing.T) {
	fixture := newPostgresFixture(t, false)
	repository := research.NewPostgresRepository(fixture.db)
	const (
		rounds  = 20
		workers = 16
	)
	experiment := fixture.createExperiment(t, repository, "concurrent-create", rounds+4)
	for round := range rounds {
		run := fixture.newRun(
			experiment.ID,
			fmt.Sprintf("same-request-%d", round),
			round,
		)
		results, failures := concurrentCreateRuns(
			repository,
			fixture.ctx,
			repeatRun(run, workers),
		)
		if len(failures) != 0 {
			t.Fatalf("round %d concurrent CreateRun() errors = %v", round, failures)
		}
		for _, result := range results {
			if result.ID != run.ID {
				t.Fatalf(
					"round %d concurrent CreateRun() ID = %s, want %s",
					round, result.ID, run.ID,
				)
			}
		}
	}
	var count int
	if err := fixture.db.QueryRowContext(
		fixture.ctx,
		`SELECT count(*) FROM research_runs WHERE experiment_id = $1`,
		experiment.ID,
	).Scan(&count); err != nil || count != rounds {
		t.Fatalf("persisted run count = %d, want %d: %v", count, rounds, err)
	}

	baseline := fixture.newRun(experiment.ID, "mixed-payload", rounds)
	if _, err := repository.CreateRun(fixture.ctx, baseline); err != nil {
		t.Fatalf("create mixed payload baseline: %v", err)
	}
	if _, err := repository.CompareAndSwapRunStatus(
		fixture.ctx,
		baseline.ID,
		research.RunStatusPending,
		research.RunStatusRunning,
		time.Time{},
	); err != nil {
		t.Fatalf("advance mixed payload baseline: %v", err)
	}
	for round := range 10 {
		requests := make([]research.ResearchRun, workers)
		for worker := range workers {
			requests[worker] = baseline
			if worker%2 == 1 {
				switch worker % 6 {
				case 1:
					requests[worker].ID = fmt.Sprintf(
						"research-run-mismatch-%d-%d-%s",
						round, worker, fixture.suffix,
					)
				case 3:
					requests[worker].RepetitionIndex = rounds + 2
				case 5:
					requests[worker].Warmup = true
				}
			}
		}
		results, failures := concurrentCreateRuns(repository, fixture.ctx, requests)
		if len(results) != workers/2 {
			t.Fatalf(
				"mixed round %d successful creates = %d, want %d; errors = %v",
				round, len(results), workers/2, failures,
			)
		}
		for _, result := range results {
			if result.ID != baseline.ID || result.Status != research.RunStatusRunning {
				t.Fatalf("mixed round %d returned run = %#v", round, result)
			}
		}
		if len(failures) != workers/2 {
			t.Fatalf(
				"mixed round %d conflicts = %d, want %d",
				round, len(failures), workers/2,
			)
		}
		for _, err := range failures {
			if !errors.Is(err, research.ErrConflict) ||
				!strings.Contains(err.Error(), "uq_research_runs_experiment_idempotency") {
				t.Fatalf("mixed round %d error = %v, want identity conflict", round, err)
			}
		}
	}

	retry := fixture.newRun(experiment.ID, "primary-key-retry", rounds+1)
	colliding := retry
	colliding.ID = baseline.ID
	if _, err := repository.CreateRun(fixture.ctx, colliding); !errors.Is(err, research.ErrConflict) ||
		!strings.Contains(err.Error(), "pk_research_runs") {
		t.Fatalf("primary key collision error = %v, want pk ErrConflict", err)
	}
	if persisted, err := repository.CreateRun(fixture.ctx, retry); err != nil {
		t.Fatalf("retry after primary key conflict error = %v", err)
	} else if persisted.ID != retry.ID {
		t.Fatalf("retry after primary key conflict ID = %s, want %s", persisted.ID, retry.ID)
	}

	repetitionConflict := fixture.newRun(experiment.ID, "repetition-conflict", 0)
	if _, err := repository.CreateRun(
		fixture.ctx,
		repetitionConflict,
	); !errors.Is(err, research.ErrConflict) ||
		!strings.Contains(err.Error(), "uq_research_runs_experiment_repetition_warmup") {
		t.Fatalf("repetition conflict error = %v, want repetition ErrConflict", err)
	}

	var runIDSequence sql.NullString
	if err := fixture.db.QueryRowContext(
		fixture.ctx,
		`SELECT pg_get_serial_sequence('research_runs', 'id')`,
	).Scan(&runIDSequence); err != nil {
		t.Fatalf("read research run ID sequence: %v", err)
	}
	if runIDSequence.Valid {
		t.Fatalf("research_runs.id unexpectedly uses sequence %s", runIDSequence.String)
	}
}

func TestPostgresRepositoryCreateRunIsolationAndDatabaseErrors(t *testing.T) {
	fixture := newPostgresFixture(t, false)
	repository := research.NewPostgresRepository(fixture.db)
	experiment := fixture.createExperiment(t, repository, "create-isolation", 4)

	const isolationConstraint = "ck_research_runs_test_read_committed"
	if _, err := fixture.db.ExecContext(fixture.ctx, `
		ALTER TABLE research_runs
		ADD CONSTRAINT `+isolationConstraint+`
		CHECK (current_setting('transaction_isolation') = 'read committed')
		NOT VALID`); err != nil {
		t.Fatalf("add transaction isolation check: %v", err)
	}
	t.Cleanup(func() {
		_, _ = fixture.db.ExecContext(context.Background(), `
			ALTER TABLE research_runs
			DROP CONSTRAINT IF EXISTS `+isolationConstraint)
	})

	fixture.db.SetMaxOpenConns(1)
	fixture.db.SetMaxIdleConns(1)
	if _, err := fixture.db.ExecContext(fixture.ctx, `
		SET SESSION CHARACTERISTICS AS TRANSACTION
		ISOLATION LEVEL REPEATABLE READ`); err != nil {
		t.Fatalf("set session transaction isolation: %v", err)
	}
	var sessionIsolation string
	if err := fixture.db.QueryRowContext(
		fixture.ctx,
		`SHOW default_transaction_isolation`,
	).Scan(&sessionIsolation); err != nil {
		t.Fatalf("read session transaction isolation: %v", err)
	}
	if sessionIsolation != "repeatable read" {
		t.Fatalf("session isolation = %q, want repeatable read", sessionIsolation)
	}
	isolationRun := fixture.newRun(experiment.ID, "read-committed", 0)
	if _, err := repository.CreateRun(fixture.ctx, isolationRun); err != nil {
		t.Fatalf("CreateRun() did not enforce read committed: %v", err)
	}

	lockerDB, err := sql.Open("pgx", os.Getenv("TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer lockerDB.Close()
	locker, err := lockerDB.BeginTx(fixture.ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer locker.Rollback()
	if _, err := locker.ExecContext(
		fixture.ctx,
		`LOCK TABLE research_runs IN ACCESS EXCLUSIVE MODE`,
	); err != nil {
		t.Fatalf("lock research_runs: %v", err)
	}

	blockedRun := fixture.newRun(experiment.ID, "database-error", 1)
	blockedContext, cancel := context.WithTimeout(fixture.ctx, 250*time.Millisecond)
	defer cancel()
	if _, err := repository.CreateRun(
		blockedContext,
		blockedRun,
	); !errors.Is(err, context.DeadlineExceeded) || errors.Is(err, research.ErrConflict) {
		t.Fatalf("blocked CreateRun() error = %v, want context deadline only", err)
	}
	if err := locker.Rollback(); err != nil {
		t.Fatalf("release research_runs lock: %v", err)
	}
	if _, err := repository.CreateRun(fixture.ctx, blockedRun); err != nil {
		t.Fatalf("retry after database error: %v", err)
	}

	unknownFirst := fixture.newRun(experiment.ID, "database-unique-first", 2)
	if _, err := repository.CreateRun(fixture.ctx, unknownFirst); err != nil {
		t.Fatalf("create unknown unique baseline: %v", err)
	}
	const unknownUniqueIndex = "uq_research_runs_test_unknown"
	if _, err := fixture.db.ExecContext(fixture.ctx, `
		CREATE UNIQUE INDEX `+unknownUniqueIndex+`
		ON research_runs ((project_id))
		WHERE idempotency_key LIKE 'database-unique-%'`); err != nil {
		t.Fatalf("create unknown unique index: %v", err)
	}
	t.Cleanup(func() {
		_, _ = fixture.db.ExecContext(
			context.Background(),
			`DROP INDEX IF EXISTS `+unknownUniqueIndex,
		)
	})
	unknownSecond := fixture.newRun(experiment.ID, "database-unique-second", 3)
	_, err = repository.CreateRun(fixture.ctx, unknownSecond)
	var postgresError *pgconn.PgError
	if !errors.As(err, &postgresError) ||
		postgresError.ConstraintName != unknownUniqueIndex ||
		errors.Is(err, research.ErrConflict) {
		t.Fatalf("unknown unique error = %v, want unclassified PostgreSQL error", err)
	}
	if _, err := fixture.db.ExecContext(
		fixture.ctx,
		`DROP INDEX `+unknownUniqueIndex,
	); err != nil {
		t.Fatalf("drop unknown unique index: %v", err)
	}
	if _, err := repository.CreateRun(fixture.ctx, unknownSecond); err != nil {
		t.Fatalf("retry after unknown unique error: %v", err)
	}
}

func repeatRun(run research.ResearchRun, count int) []research.ResearchRun {
	result := make([]research.ResearchRun, count)
	for index := range result {
		result[index] = run
	}
	return result
}

func concurrentCreateRuns(
	repository *research.PostgresRepository,
	ctx context.Context,
	runs []research.ResearchRun,
) ([]research.ResearchRun, []error) {
	results := make(chan research.ResearchRun, len(runs))
	failures := make(chan error, len(runs))
	start := make(chan struct{})
	var waitGroup sync.WaitGroup
	for _, run := range runs {
		waitGroup.Add(1)
		go func(run research.ResearchRun) {
			defer waitGroup.Done()
			<-start
			persisted, err := repository.CreateRun(ctx, run)
			if err != nil {
				failures <- err
				return
			}
			results <- persisted
		}(run)
	}
	close(start)
	waitGroup.Wait()
	close(results)
	close(failures)
	persisted := make([]research.ResearchRun, 0, len(results))
	for result := range results {
		persisted = append(persisted, result)
	}
	errs := make([]error, 0, len(failures))
	for err := range failures {
		errs = append(errs, err)
	}
	return persisted, errs
}

func TestPostgresRepositoryAppendIdempotencyConflictsAndRollback(t *testing.T) {
	fixture := newPostgresFixture(t, false)
	repository := research.NewPostgresRepository(fixture.db)
	experiment := fixture.createExperiment(t, repository, "append", 10)
	run := fixture.newRun(experiment.ID, "append-run", 0)
	if _, err := repository.CreateRun(fixture.ctx, run); err != nil {
		t.Fatal(err)
	}

	first := transition(t, run.ID, 0, "source-0", `{"state_summary":"start"}`)
	appended, err := repository.AppendTransitions(
		fixture.ctx, run.ID, []research.Transition{first},
	)
	if err != nil || len(appended) != 1 {
		t.Fatalf("AppendTransitions() = %#v, %v", appended, err)
	}
	replayed, err := repository.AppendTransitions(
		fixture.ctx, run.ID, []research.Transition{first},
	)
	if err != nil || replayed[0].ID != appended[0].ID {
		t.Fatalf("idempotent AppendTransitions() = %#v, %v", replayed, err)
	}

	hashConflict := transition(t, run.ID, 0, "source-0", `{"state_summary":"changed"}`)
	if _, err := repository.AppendTransitions(
		fixture.ctx, run.ID, []research.Transition{hashConflict},
	); !errors.Is(err, research.ErrConflict) {
		t.Fatalf("hash conflict error = %v, want ErrConflict", err)
	}
	ordinalConflict := transition(t, run.ID, 0, "different-source", `{"state_summary":"start"}`)
	if _, err := repository.AppendTransitions(
		fixture.ctx, run.ID, []research.Transition{ordinalConflict},
	); !errors.Is(err, research.ErrConflict) {
		t.Fatalf("ordinal conflict error = %v, want ErrConflict", err)
	}

	rolledBack := transition(t, run.ID, 1, "new-source", `{"state_summary":"new"}`)
	lateConflict := transition(t, run.ID, 2, "source-0", `{"state_summary":"late"}`)
	if _, err := repository.AppendTransitions(
		fixture.ctx,
		run.ID,
		[]research.Transition{rolledBack, lateConflict},
	); !errors.Is(err, research.ErrConflict) {
		t.Fatalf("transaction conflict error = %v, want ErrConflict", err)
	}
	transitions, err := repository.ListTransitions(
		fixture.ctx,
		research.TransitionFilter{ResearchRunID: run.ID},
	)
	if err != nil || len(transitions) != 1 {
		t.Fatalf("rollback transition count = %d, %v", len(transitions), err)
	}

	concurrentRun := fixture.newRun(experiment.ID, "append-concurrent", 1)
	if _, err := repository.CreateRun(fixture.ctx, concurrentRun); err != nil {
		t.Fatal(err)
	}
	same := transition(t, concurrentRun.ID, 0, "same-source", `{"state_summary":"same"}`)
	appendResults := make(chan []research.Transition, 2)
	appendErrors := make(chan error, 2)
	var waitGroup sync.WaitGroup
	for range 2 {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			result, appendErr := repository.AppendTransitions(
				fixture.ctx,
				concurrentRun.ID,
				[]research.Transition{same},
			)
			if appendErr != nil {
				appendErrors <- appendErr
				return
			}
			appendResults <- result
		}()
	}
	waitGroup.Wait()
	close(appendResults)
	close(appendErrors)
	for err := range appendErrors {
		t.Errorf("concurrent idempotent append error = %v", err)
	}
	var persistedID int64
	for result := range appendResults {
		if len(result) != 1 {
			t.Fatalf("concurrent append result = %#v", result)
		}
		if persistedID == 0 {
			persistedID = result[0].ID
		} else if result[0].ID != persistedID {
			t.Errorf("concurrent append IDs = %d and %d", persistedID, result[0].ID)
		}
	}
	if err := repository.DeleteTransitions(fixture.ctx, concurrentRun.ID); err != nil {
		t.Fatalf("DeleteTransitions() error = %v", err)
	}
	remaining, err := repository.ListTransitions(
		fixture.ctx,
		research.TransitionFilter{ResearchRunID: concurrentRun.ID},
	)
	if err != nil || len(remaining) != 0 {
		t.Fatalf("transitions after delete = %#v, %v", remaining, err)
	}
}

type postgresFixture struct {
	t                 *testing.T
	ctx               context.Context
	db                *sql.DB
	suffix            string
	userID            int64
	projectID         int64
	generationID      int64
	batchID           int64
	mismatchedBatchID int64
	executionID       int64
	dslSHA256         string
	agentRunID        string
	links             research.RunLinks
	experimentIDs     []string
}

func newPostgresFixture(t *testing.T, withChain bool) *postgresFixture {
	t.Helper()
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := db.PingContext(ctx); err != nil {
		t.Fatal(err)
	}
	var researchTable sql.NullString
	if err := db.QueryRowContext(
		ctx,
		`SELECT to_regclass('public.research_runs')::text`,
	).Scan(&researchTable); err != nil || !researchTable.Valid {
		t.Fatalf("research migration is not applied: %v", err)
	}
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	fixture := &postgresFixture{t: t, ctx: ctx, db: db, suffix: suffix}
	if err := db.QueryRowContext(ctx, `
		INSERT INTO users (email, display_name)
		VALUES ($1, 'Research Integration')
		RETURNING id`,
		"research-"+suffix+"@example.com",
	).Scan(&fixture.userID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `
		INSERT INTO projects (name, description)
		VALUES ($1, 'research repository integration')
		RETURNING id`,
		"research-"+suffix,
	).Scan(&fixture.projectID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO project_members (project_id, user_id, role)
		VALUES ($1, $2, 'owner')`,
		fixture.projectID, fixture.userID,
	); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(fixture.cleanup)
	if withChain {
		fixture.createExecutionChain()
	}
	return fixture
}

func (f *postgresFixture) createExperiment(
	t *testing.T,
	repository *research.PostgresRepository,
	name string,
	repetitions int,
) research.Experiment {
	t.Helper()
	experiment := research.Experiment{
		ID:                 "experiment-" + name + "-" + f.suffix,
		ProjectID:          f.projectID,
		Name:               name,
		Goal:               "complete a deterministic research task",
		DatasetVersion:     "dataset.v1",
		ModelProvider:      "openai-compatible",
		ModelName:          "model",
		ModelVersion:       "model.v1",
		PromptVersion:      "prompt.v1",
		BrowserName:        "chromium",
		BrowserVersion:     "1",
		ViewportJSON:       json.RawMessage(`{"width":1280,"height":720}`),
		CodeSHA256:         strings.Repeat("a", 64),
		PolicyVersion:      research.PolicyVersion,
		ObservationProfile: "a11y-dom",
		DSLProfile:         "legacy",
		Seed:               42,
		Variant:            "dsl-verification",
		Repetitions:        repetitions,
		Status:             research.ExperimentStatusDraft,
		ConfigJSON:         json.RawMessage(`{}`),
	}
	persisted, err := repository.CreateExperiment(f.ctx, experiment)
	if err != nil {
		t.Fatalf("CreateExperiment() error = %v", err)
	}
	f.experimentIDs = append(f.experimentIDs, persisted.ID)
	return persisted
}

func (f *postgresFixture) newRun(
	experimentID string,
	key string,
	repetition int,
) research.ResearchRun {
	return research.ResearchRun{
		ID:              "research-run-" + key + "-" + f.suffix,
		ExperimentID:    experimentID,
		ProjectID:       f.projectID,
		IdempotencyKey:  key,
		RepetitionIndex: repetition,
		Status:          research.RunStatusPending,
		Versions:        research.DefaultVersionSnapshot(),
	}
}

func (f *postgresFixture) createExecutionChain() {
	f.t.Helper()
	generation, err := dsl.NewStore(f.db).CreateGeneration(
		f.ctx,
		f.userID,
		f.projectID,
		json.RawMessage(`{
			"name":"research fixture",
			"base_url":"https://example.com",
			"steps":[{"action":"goto","value":"https://example.com"}]
		}`),
		nil,
	)
	if err != nil {
		f.t.Fatalf("create generation fixture: %v", err)
	}
	f.generationID = generation.ID
	f.dslSHA256 = generation.DSLHash
	f.agentRunID = "agent-run-" + f.suffix
	if _, err := f.db.ExecContext(f.ctx, `
		INSERT INTO agent_runs (
			id, actor_user_id, conversation_id, project_id, status, input,
			latest_generation_id, approved_generation_id, transcript_json,
			last_event_seq
		) VALUES ($1, $2, $3, $4, 'completed', 'research fixture', $5, $5, '[]'::json, 0)`,
		f.agentRunID,
		f.userID,
		"conversation-"+f.suffix,
		f.projectID,
		f.generationID,
	); err != nil {
		f.t.Fatalf("create agent run fixture: %v", err)
	}
	var caseID int64
	if err := f.db.QueryRowContext(f.ctx, `
		INSERT INTO test_cases (project_id, created_by, updated_by, name, dsl)
		VALUES ($1, $2, $2, 'research fixture', $3::json)
		RETURNING id`,
		f.projectID, f.userID, string(generation.Case),
	).Scan(&caseID); err != nil {
		f.t.Fatalf("create case fixture: %v", err)
	}
	f.batchID, f.executionID = f.createBatchAndExecution(caseID, generation.DSLHash)
	f.mismatchedBatchID, _ = f.createBatchAndExecution(caseID, strings.Repeat("b", 64))
	f.links = research.RunLinks{
		AgentRunID:   &f.agentRunID,
		GenerationID: &f.generationID,
		BatchID:      &f.batchID,
		ExecutionID:  &f.executionID,
		DSLSHA256:    &f.dslSHA256,
	}
}

func (f *postgresFixture) createBatchAndExecution(caseID int64, dslSHA256 string) (int64, int64) {
	f.t.Helper()
	var batchID int64
	if err := f.db.QueryRowContext(f.ctx, `
		INSERT INTO execution_batches (
			project_id, triggered_by, status, idempotency_key,
			concurrency_limit, input_values_json
		) VALUES ($1, $2, 'passed', $3, 1, '{}'::json)
		RETURNING id`,
		f.projectID, f.userID, "batch-"+dslSHA256[:8]+"-"+f.suffix,
	).Scan(&batchID); err != nil {
		f.t.Fatalf("create batch fixture: %v", err)
	}
	var jobID int64
	if err := f.db.QueryRowContext(f.ctx, `
		INSERT INTO execution_jobs (
			batch_id, project_id, case_id, order_index, status,
			attempt_count, max_attempts, cancel_requested, dsl_sha256
		) VALUES ($1, $2, $3, 0, 'passed', 1, 1, false, $4)
		RETURNING id`,
		batchID, f.projectID, caseID, dslSHA256,
	).Scan(&jobID); err != nil {
		f.t.Fatalf("create job fixture: %v", err)
	}
	var executionID int64
	if err := f.db.QueryRowContext(f.ctx, `
		INSERT INTO test_case_runs (
			case_id, project_id, batch_id, job_id, triggered_by, status,
			attempt_number, dsl_sha256, report_schema_version, analysis_status
		) VALUES ($1, $2, $3, $4, $5, 'passed', 1, $6, 'execution.report.v2', 'skipped')
		RETURNING id`,
		caseID, f.projectID, batchID, jobID, f.userID, dslSHA256,
	).Scan(&executionID); err != nil {
		f.t.Fatalf("create execution fixture: %v", err)
	}
	return batchID, executionID
}

func (f *postgresFixture) cleanup() {
	for _, experimentID := range f.experimentIDs {
		_, _ = f.db.ExecContext(
			f.ctx,
			`DELETE FROM research_experiments WHERE id = $1`,
			experimentID,
		)
	}
	_, _ = f.db.ExecContext(f.ctx, `DELETE FROM test_case_runs WHERE project_id = $1`, f.projectID)
	_, _ = f.db.ExecContext(f.ctx, `DELETE FROM execution_batches WHERE project_id = $1`, f.projectID)
	_, _ = f.db.ExecContext(f.ctx, `DELETE FROM agent_runs WHERE project_id = $1`, f.projectID)
	_, _ = f.db.ExecContext(f.ctx, `DELETE FROM dsl_generation_runs WHERE project_id = $1`, f.projectID)
	_, _ = f.db.ExecContext(f.ctx, `DELETE FROM test_cases WHERE project_id = $1`, f.projectID)
	_, _ = f.db.ExecContext(f.ctx, `DELETE FROM projects WHERE id = $1`, f.projectID)
	_, _ = f.db.ExecContext(f.ctx, `DELETE FROM users WHERE id = $1`, f.userID)
	_ = f.db.Close()
}

func transition(
	t *testing.T,
	runID string,
	ordinal int64,
	appendKey string,
	payload string,
) research.Transition {
	t.Helper()
	raw := json.RawMessage(payload)
	hash, err := research.TransitionContentSHA256(research.SchemaVersion, raw, nil)
	if err != nil {
		t.Fatal(err)
	}
	return research.Transition{
		ResearchRunID: runID,
		Ordinal:       ordinal,
		AppendKey:     appendKey,
		ContentSHA256: hash,
		SchemaVersion: research.SchemaVersion,
		PayloadJSON:   raw,
	}
}

func unavailableMetrics() research.RunMetrics {
	reason := "source fact unavailable"
	return research.RunMetrics{
		SchemaVersion:       research.MetricVersion,
		TaskSuccess:         research.NullableValue[bool]{UnavailableReason: &reason},
		GroundingAccuracy:   research.NullableValue[float64]{UnavailableReason: &reason},
		InvalidActionRate:   research.NullableValue[float64]{UnavailableReason: &reason},
		ExecutionSuccess:    research.NullableValue[bool]{UnavailableReason: &reason},
		VerificationSuccess: research.NullableValue[bool]{UnavailableReason: &reason},
		RecoveryRate:        research.NullableValue[float64]{UnavailableReason: &reason},
		Steps:               research.NullableValue[int64]{UnavailableReason: &reason},
		Retries:             research.NullableValue[int64]{UnavailableReason: &reason},
		LLMCalls:            research.NullableValue[int64]{UnavailableReason: &reason},
		InputTokens:         research.NullableValue[int64]{UnavailableReason: &reason},
		OutputTokens:        research.NullableValue[int64]{UnavailableReason: &reason},
		TotalTokens:         research.NullableValue[int64]{UnavailableReason: &reason},
		LatencyMS:           research.NullableValue[int64]{UnavailableReason: &reason},
		VisionCalls:         research.NullableValue[int64]{UnavailableReason: &reason},
	}
}
