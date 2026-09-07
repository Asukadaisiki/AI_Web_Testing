package research_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/research"
)

func TestResearchServiceLifecycleOwnershipScheduleAndDeadline(t *testing.T) {
	fixture := newPostgresFixture(t, false)
	repository := research.NewPostgresRepository(fixture.db)
	now := time.Date(2026, 9, 7, 3, 0, 0, 0, time.UTC)
	service := research.NewServiceWithClock(repository, nil, func() time.Time {
		return now
	})
	experiment := servicePostgresExperiment(fixture, "service-lifecycle")

	created, err := service.CreateExperiment(fixture.ctx, fixture.userID, experiment)
	if err != nil {
		t.Fatal(err)
	}
	fixture.experimentIDs = append(fixture.experimentIDs, created.ID)
	replayed, err := service.CreateExperiment(fixture.ctx, fixture.userID, experiment)
	if err != nil || replayed.ID != created.ID {
		t.Fatalf("idempotent CreateExperiment() = %#v, %v", replayed, err)
	}
	if _, err := service.GetExperiment(
		fixture.ctx,
		fixture.userID+1,
		fixture.projectID,
		created.ID,
	); !errors.Is(err, research.ErrNotFound) {
		t.Fatalf("foreign GetExperiment() error = %v, want ErrNotFound", err)
	}

	started, err := service.StartExperiment(
		fixture.ctx, fixture.userID, fixture.projectID, created.ID,
	)
	if err != nil {
		t.Fatal(err)
	}
	if started.Experiment.Status != research.ExperimentStatusActive ||
		len(started.Schedule) != 5 {
		t.Fatalf("started experiment = %#v", started)
	}
	for index, item := range started.Schedule {
		if item.Order != index {
			t.Fatalf("schedule[%d].Order = %d", index, item.Order)
		}
		if index < 2 && !item.Run.Warmup {
			t.Fatalf("schedule[%d] is not warmup", index)
		}
		if index >= 2 && item.Run.Warmup {
			t.Fatalf("schedule[%d] unexpectedly warmup", index)
		}
	}
	repeated, err := service.StartExperiment(
		fixture.ctx, fixture.userID, fixture.projectID, created.ID,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(repeated.Schedule) != len(started.Schedule) {
		t.Fatalf("repeated schedule length = %d", len(repeated.Schedule))
	}
	for index := range started.Schedule {
		if repeated.Schedule[index].Run.ID != started.Schedule[index].Run.ID {
			t.Fatalf("schedule replay changed at %d", index)
		}
	}

	runID := started.Schedule[0].Run.ID
	runStart, err := service.StartRun(
		fixture.ctx, fixture.userID, fixture.projectID, runID,
	)
	if err != nil {
		t.Fatal(err)
	}
	if runStart.Run.StartedAt == nil ||
		!runStart.Deadline.Equal(now.Add(90*time.Second)) {
		t.Fatalf("run start = %#v", runStart)
	}
	replayedStart, err := service.StartRun(
		fixture.ctx, fixture.userID, fixture.projectID, runID,
	)
	if err != nil ||
		!replayedStart.Deadline.Equal(runStart.Deadline) {
		t.Fatalf("idempotent StartRun() = %#v, %v", replayedStart, err)
	}
}

func TestPostgresOracleIdempotencyBindingAndMetricsCAS(t *testing.T) {
	fixture := newPostgresFixture(t, true)
	var oracleTable *string
	if err := fixture.db.QueryRowContext(
		fixture.ctx,
		`SELECT to_regclass('public.research_oracle_results')::text`,
	).Scan(&oracleTable); err != nil || oracleTable == nil {
		t.Fatalf("research oracle migration is not applied: %v", err)
	}
	repository := research.NewPostgresRepository(fixture.db)
	experiment := fixture.createExperiment(t, repository, "oracle-cas", 1)
	run := fixture.newRun(experiment.ID, "oracle-cas", 0)
	if _, err := repository.CreateRun(fixture.ctx, run); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.UpdateRunLinks(
		fixture.ctx, run.ID, fixture.links, time.Time{},
	); err != nil {
		t.Fatal(err)
	}
	decision := oracleDecision(t, false, "task.oracle_failed")
	input := research.OracleResult{
		ResearchRunID: run.ID,
		ExecutionID:   fixture.executionID,
		Decision:      decision,
	}
	first, err := repository.PutOracle(fixture.ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	second, err := repository.PutOracle(fixture.ctx, input)
	if err != nil || second.Decision.ContentSHA256 != first.Decision.ContentSHA256 {
		t.Fatalf("idempotent PutOracle() = %#v, %v", second, err)
	}

	conflicting := input
	conflicting.Decision = oracleDecision(t, true, "task.passed")
	if _, err := repository.PutOracle(
		fixture.ctx, conflicting,
	); !errors.Is(err, research.ErrConflict) {
		t.Fatalf("conflicting PutOracle() error = %v, want ErrConflict", err)
	}
	crossExecution := input
	crossExecution.ExecutionID = fixture.executionID + 1
	if _, err := repository.PutOracle(
		fixture.ctx, crossExecution,
	); !errors.Is(err, research.ErrBrokenLink) {
		t.Fatalf("cross-execution PutOracle() error = %v, want ErrBrokenLink", err)
	}

	sourceA := strings.Repeat("1", 64)
	sourceB := strings.Repeat("2", 64)
	metricsA := projectedUnavailableMetrics(t, sourceA)
	withA, err := repository.CompareAndSwapRunMetrics(
		fixture.ctx, run.ID, "", metricsA, time.Time{},
	)
	if err != nil || withA.Metrics == nil ||
		withA.Metrics.SourceSHA256 != sourceA {
		t.Fatalf("first metrics CAS = %#v, %v", withA.Metrics, err)
	}
	replayedMetrics, err := repository.CompareAndSwapRunMetrics(
		fixture.ctx, run.ID, "", metricsA, time.Time{},
	)
	if err != nil ||
		replayedMetrics.Metrics.MetricsSHA256 != metricsA.MetricsSHA256 {
		t.Fatalf("idempotent metrics CAS = %#v, %v", replayedMetrics.Metrics, err)
	}
	metricsB := projectedUnavailableMetrics(t, sourceB)
	if _, err := repository.CompareAndSwapRunMetrics(
		fixture.ctx, run.ID, "", metricsB, time.Time{},
	); !errors.Is(err, research.ErrConflict) {
		t.Fatalf("stale metrics CAS error = %v, want ErrConflict", err)
	}
	if _, err := repository.CompareAndSwapRunMetrics(
		fixture.ctx, run.ID, sourceA, metricsB, time.Time{},
	); !errors.Is(err, research.ErrConflict) {
		t.Fatalf("metrics replacement error = %v, want ErrConflict", err)
	}
}

func servicePostgresExperiment(
	fixture *postgresFixture,
	name string,
) research.Experiment {
	config, _ := json.Marshal(research.ExperimentConfig{
		SchemaVersion:         research.ExperimentConfigSchemaVersion,
		RequestTimeoutSeconds: 30,
		RunTimeoutSeconds:     90,
		CancelGraceSeconds:    5,
		WarmupRepetitions:     2,
		CleanContext:          true,
		ScheduleVersion:       research.ScheduleVersion,
	})
	return research.Experiment{
		ID:                 "experiment-" + name + "-" + fixture.suffix,
		ProjectID:          fixture.projectID,
		Name:               name,
		Goal:               "test the research control plane",
		DatasetVersion:     "dataset.v1",
		ModelProvider:      "provider",
		ModelName:          "model",
		ModelVersion:       "model.v1",
		PromptVersion:      "prompt.v1",
		BrowserName:        "chromium",
		BrowserVersion:     "1",
		ViewportJSON:       json.RawMessage(`{"width":1280,"height":720}`),
		CodeSHA256:         strings.Repeat("a", 64),
		PolicyVersion:      research.PolicyVersion,
		ObservationProfile: "a11y-dom",
		DSLProfile:         "dsl.v1",
		Seed:               42,
		Variant:            research.SupportedVariant,
		Repetitions:        3,
		ConfigJSON:         config,
	}
}

func oracleDecision(
	t *testing.T,
	passed bool,
	reason string,
) research.OracleDecision {
	t.Helper()
	source := research.SourceRef{
		Kind:          research.SourceOracle,
		ID:            "oracle-artifact",
		Sequence:      research.Unavailable[int64]("not_sequenced"),
		ContentSHA256: strings.Repeat("a", 64),
		SchemaVersion: research.Available("oracle.fixture.v1"),
	}
	decision, err := research.NewOracleDecision(research.OracleDecision{
		ID:         "oracle-result",
		Evaluator:  "fixture.v1",
		Passed:     passed,
		ReasonCode: reason,
		DecisionFacts: []research.OracleDecisionFact{{
			Name: "task_outcome", Passed: passed, Sources: []research.SourceRef{source},
		}},
		Sources: []research.SourceRef{source},
	})
	if err != nil {
		t.Fatal(err)
	}
	return decision
}

func projectedUnavailableMetrics(
	t *testing.T,
	sourceSHA256 string,
) research.RunMetrics {
	t.Helper()
	metrics := unavailableMetrics()
	metrics.ProjectorVersion = research.MetricProjectorVersion
	metrics.SourceSHA256 = sourceSHA256
	metrics.MetricsSHA256 = ""
	hash, err := research.CanonicalSHA256(metrics)
	if err != nil {
		t.Fatal(err)
	}
	metrics.MetricsSHA256 = hash
	if err := metrics.Validate(); err != nil {
		t.Fatal(err)
	}
	return metrics
}
