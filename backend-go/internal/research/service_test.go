package research

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestBuildRunScheduleIsStableAndSeparatesWarmups(t *testing.T) {
	experiment := serviceExperimentFixture(19, 4)
	config := serviceConfigFixture()
	config.WarmupRepetitions = 2

	first, err := BuildRunSchedule(experiment, config)
	if err != nil {
		t.Fatal(err)
	}
	second, err := BuildRunSchedule(experiment, config)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("same seed produced different schedules:\n%#v\n%#v", first, second)
	}
	if len(first) != 6 {
		t.Fatalf("schedule length = %d, want 6", len(first))
	}
	seen := make(map[string]struct{}, len(first))
	for index, item := range first {
		if item.Order != index {
			t.Fatalf("schedule[%d].Order = %d", index, item.Order)
		}
		if _, exists := seen[item.Run.ID]; exists {
			t.Fatalf("duplicate scheduled run ID %q", item.Run.ID)
		}
		seen[item.Run.ID] = struct{}{}
		if index < config.WarmupRepetitions && !item.Run.Warmup {
			t.Fatalf("schedule[%d] is not a warmup", index)
		}
		if index >= config.WarmupRepetitions && item.Run.Warmup {
			t.Fatalf("schedule[%d] unexpectedly is a warmup", index)
		}
	}

	otherSeed := serviceExperimentFixture(20, 4)
	third, err := BuildRunSchedule(otherSeed, config)
	if err != nil {
		t.Fatal(err)
	}
	if reflect.DeepEqual(first, third) {
		t.Fatal("different seeds produced identical schedule identities")
	}
}

func TestExperimentConfigValidation(t *testing.T) {
	valid := serviceConfigFixture()
	raw, err := json.Marshal(valid)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParseExperimentConfig(raw); err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}

	tests := []struct {
		name   string
		mutate func(*ExperimentConfig)
	}{
		{"request timeout zero", func(c *ExperimentConfig) { c.RequestTimeoutSeconds = 0 }},
		{"run timeout low", func(c *ExperimentConfig) { c.RunTimeoutSeconds = 59 }},
		{"run timeout high", func(c *ExperimentConfig) { c.RunTimeoutSeconds = 3601 }},
		{"cancel grace negative", func(c *ExperimentConfig) { c.CancelGraceSeconds = -1 }},
		{"cancel grace high", func(c *ExperimentConfig) { c.CancelGraceSeconds = 301 }},
		{"warmup negative", func(c *ExperimentConfig) { c.WarmupRepetitions = -1 }},
		{"dirty context", func(c *ExperimentConfig) { c.CleanContext = false }},
		{"schedule version", func(c *ExperimentConfig) { c.ScheduleVersion = "research.schedule.v2" }},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			config := serviceConfigFixture()
			testCase.mutate(&config)
			if err := config.NormalizeAndValidate(); err == nil {
				t.Fatal("invalid config was accepted")
			}
		})
	}
	if _, err := ParseExperimentConfig(json.RawMessage(`{
		"schema_version":"research.experiment_config.v1",
		"request_timeout_seconds":30,
		"run_timeout_seconds":60,
		"cancel_grace_seconds":5,
		"warmup_repetitions":0,
		"clean_context":true,
		"schedule_version":"research.schedule.v1",
		"unknown":true
	}`)); err == nil {
		t.Fatal("unknown config field was accepted")
	}
}

func serviceConfigFixture() ExperimentConfig {
	return ExperimentConfig{
		SchemaVersion:         ExperimentConfigSchemaVersion,
		RequestTimeoutSeconds: 30,
		RunTimeoutSeconds:     120,
		CancelGraceSeconds:    5,
		WarmupRepetitions:     0,
		CleanContext:          true,
		ScheduleVersion:       ScheduleVersion,
	}
}

func serviceExperimentFixture(seed int64, repetitions int) Experiment {
	config, _ := json.Marshal(serviceConfigFixture())
	return Experiment{
		ID:                 "experiment-service",
		ProjectID:          42,
		Name:               "service",
		Goal:               "verify deterministic research control",
		DatasetVersion:     "dataset.v1",
		ModelProvider:      "provider",
		ModelName:          "model",
		ModelVersion:       "model.v1",
		PromptVersion:      "prompt.v1",
		BrowserName:        "chromium",
		BrowserVersion:     "1",
		ViewportJSON:       json.RawMessage(`{"width":1280,"height":720}`),
		CodeSHA256:         strings.Repeat("a", 64),
		PolicyVersion:      PolicyVersion,
		ObservationProfile: "a11y-dom",
		DSLProfile:         "dsl.v1",
		Seed:               seed,
		Variant:            SupportedVariant,
		Repetitions:        repetitions,
		Status:             ExperimentStatusDraft,
		ConfigJSON:         config,
	}
}

func TestProjectMetricsReplayAndFinishRunCompletesExperiment(t *testing.T) {
	source := metricSourceFixture(t, nil, Available(metricOracleFixture(t, true)))
	startedAt := time.Date(2026, 9, 7, 4, 0, 0, 0, time.UTC)
	experiment := serviceExperimentFixture(19, 1)
	experiment.ID = "experiment-metrics"
	experiment.ProjectID = source.Snapshot.ProjectID
	experiment.Status = ExperimentStatusActive
	repository := &controlRepositoryStub{
		experiment: experiment,
		run: ResearchRun{
			ID: source.ResearchRunID, ExperimentID: experiment.ID,
			ProjectID: source.Snapshot.ProjectID, IdempotencyKey: "run-1",
			Status: RunStatusRunning, Versions: DefaultVersionSnapshot(),
			Links: source.FinalRunLinks, StartedAt: &startedAt,
		},
		oracle: &OracleResult{
			ResearchRunID: source.ResearchRunID,
			ExecutionID:   *source.FinalRunLinks.ExecutionID,
			Decision:      *source.Oracle.Value,
		},
		transitions: source.Transitions,
	}
	service := NewServiceWithClock(
		repository,
		sourceReaderStub{snapshot: source.Snapshot},
		func() time.Time { return startedAt.Add(time.Minute) },
	)

	first, err := service.ProjectMetrics(
		context.Background(), 1, source.Snapshot.ProjectID, source.ResearchRunID,
	)
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := service.ProjectMetrics(
		context.Background(), 1, source.Snapshot.ProjectID, source.ResearchRunID,
	)
	if err != nil {
		t.Fatal(err)
	}
	if repository.metricsWrites != 1 ||
		first.Metrics == nil ||
		replayed.Metrics == nil ||
		first.Metrics.MetricsSHA256 != replayed.Metrics.MetricsSHA256 {
		t.Fatalf(
			"metrics replay writes/hash = %d/%#v/%#v",
			repository.metricsWrites, first.Metrics, replayed.Metrics,
		)
	}

	finished, err := service.FinishRun(
		context.Background(),
		1,
		source.Snapshot.ProjectID,
		source.ResearchRunID,
		RunStatusCompleted,
	)
	if err != nil {
		t.Fatal(err)
	}
	if finished.Status != RunStatusCompleted ||
		repository.experiment.Status != ExperimentStatusCompleted ||
		repository.experimentCAS != 1 {
		t.Fatalf(
			"finished run/experiment = %s/%s, CAS = %d",
			finished.Status, repository.experiment.Status, repository.experimentCAS,
		)
	}
	if _, err := service.FinishRun(
		context.Background(),
		1,
		source.Snapshot.ProjectID,
		source.ResearchRunID,
		RunStatusCompleted,
	); err != nil || repository.experimentCAS != 1 {
		t.Fatalf("FinishRun() replay error/CAS = %v/%d", err, repository.experimentCAS)
	}
}

func TestProjectMetricsRejectsNonTerminalSource(t *testing.T) {
	source := metricSourceFixture(t, nil, Available(metricOracleFixture(t, true)))
	source.Snapshot.AgentRunStatus = "running"
	source.Snapshot.SourceSHA256 = ""
	var err error
	source.Snapshot.SourceSHA256, err = sourceSnapshotHash(source.Snapshot)
	if err != nil {
		t.Fatal(err)
	}
	startedAt := time.Now().UTC()
	experiment := serviceExperimentFixture(19, 1)
	experiment.ProjectID = source.Snapshot.ProjectID
	experiment.Status = ExperimentStatusActive
	repository := &controlRepositoryStub{
		experiment: experiment,
		run: ResearchRun{
			ID: source.ResearchRunID, ExperimentID: experiment.ID,
			ProjectID: source.Snapshot.ProjectID, IdempotencyKey: "run-1",
			Status: RunStatusRunning, Versions: DefaultVersionSnapshot(),
			Links: source.FinalRunLinks, StartedAt: &startedAt,
		},
		oracle: &OracleResult{
			ResearchRunID: source.ResearchRunID,
			ExecutionID:   *source.FinalRunLinks.ExecutionID,
			Decision:      *source.Oracle.Value,
		},
		transitions: source.Transitions,
	}
	service := NewService(
		repository,
		sourceReaderStub{snapshot: source.Snapshot},
	)
	if _, err := service.ProjectMetrics(
		context.Background(), 1, source.Snapshot.ProjectID, source.ResearchRunID,
	); !errors.Is(err, ErrSourceChanged) {
		t.Fatalf("ProjectMetrics() error = %v, want ErrSourceChanged", err)
	}
	if repository.metricsWrites != 0 {
		t.Fatalf("metrics writes = %d, want 0", repository.metricsWrites)
	}
}

func TestFinishRunRequiresCompletionEvidenceAndFailedRunStart(t *testing.T) {
	source := metricSourceFixture(t, nil, Available(metricOracleFixture(t, true)))
	startedAt := time.Now().UTC()
	experiment := serviceExperimentFixture(19, 1)
	experiment.ProjectID = source.Snapshot.ProjectID
	experiment.Status = ExperimentStatusActive
	repository := &controlRepositoryStub{
		experiment: experiment,
		run: ResearchRun{
			ID: source.ResearchRunID, ExperimentID: experiment.ID,
			ProjectID: source.Snapshot.ProjectID, IdempotencyKey: "run-1",
			Status: RunStatusRunning, Versions: DefaultVersionSnapshot(),
			Links: source.FinalRunLinks, StartedAt: &startedAt,
		},
		oracle: &OracleResult{
			ResearchRunID: source.ResearchRunID,
			ExecutionID:   *source.FinalRunLinks.ExecutionID,
			Decision:      *source.Oracle.Value,
		},
		transitions: source.Transitions,
	}
	service := NewService(
		repository,
		sourceReaderStub{snapshot: source.Snapshot},
	)
	if _, err := service.FinishRun(
		context.Background(),
		1,
		source.Snapshot.ProjectID,
		source.ResearchRunID,
		RunStatusCompleted,
	); !errors.Is(err, ErrConflict) {
		t.Fatalf("completed without metrics error = %v, want ErrConflict", err)
	}

	metrics, err := NewMetricProjector().Project(source)
	if err != nil {
		t.Fatal(err)
	}
	repository.run.Metrics = &metrics
	oracle := repository.oracle
	repository.oracle = nil
	if _, err := service.FinishRun(
		context.Background(),
		1,
		source.Snapshot.ProjectID,
		source.ResearchRunID,
		RunStatusCompleted,
	); !errors.Is(err, ErrNotFound) {
		t.Fatalf("completed without oracle error = %v, want ErrNotFound", err)
	}
	repository.oracle = oracle
	changedMetrics := metrics
	changedMetrics.SourceSHA256 = strings.Repeat("f", 64)
	changedMetrics.MetricsSHA256 = ""
	changedMetrics.MetricsSHA256, err = runMetricsHash(changedMetrics)
	if err != nil {
		t.Fatal(err)
	}
	repository.run.Metrics = &changedMetrics
	if _, err := service.FinishRun(
		context.Background(),
		1,
		source.Snapshot.ProjectID,
		source.ResearchRunID,
		RunStatusCompleted,
	); !errors.Is(err, ErrSourceChanged) {
		t.Fatalf("completed with stale metric source error = %v, want ErrSourceChanged", err)
	}

	repository.run.StartedAt = nil
	if _, err := service.FinishRun(
		context.Background(),
		1,
		source.Snapshot.ProjectID,
		source.ResearchRunID,
		RunStatusFailed,
	); !errors.Is(err, ErrConflict) {
		t.Fatalf("failed without start error = %v, want ErrConflict", err)
	}
	repository.run.StartedAt = &startedAt
	failed, err := service.FinishRun(
		context.Background(),
		1,
		source.Snapshot.ProjectID,
		source.ResearchRunID,
		RunStatusFailed,
	)
	if err != nil || failed.Status != RunStatusFailed ||
		repository.experiment.Status != ExperimentStatusCompleted {
		t.Fatalf(
			"failed run/experiment = %#v/%s, error = %v",
			failed, repository.experiment.Status, err,
		)
	}
}

type sourceReaderStub struct {
	snapshot SourceSnapshot
	err      error
}

func (s sourceReaderStub) Read(context.Context, string) (SourceSnapshot, error) {
	return s.snapshot, s.err
}

type controlRepositoryStub struct {
	experiment    Experiment
	run           ResearchRun
	oracle        *OracleResult
	transitions   []Transition
	metricsWrites int
	experimentCAS int
}

func (r *controlRepositoryStub) OwnsProject(context.Context, int64, int64) (bool, error) {
	return true, nil
}

func (r *controlRepositoryStub) CreateExperiment(
	context.Context,
	Experiment,
) (Experiment, error) {
	return Experiment{}, errors.New("unexpected CreateExperiment")
}

func (r *controlRepositoryStub) GetExperiment(
	_ context.Context,
	id string,
) (Experiment, error) {
	if id != r.experiment.ID {
		return Experiment{}, ErrNotFound
	}
	return r.experiment, nil
}

func (r *controlRepositoryStub) ListExperiments(
	context.Context,
	ExperimentFilter,
) ([]Experiment, error) {
	return nil, errors.New("unexpected ListExperiments")
}

func (r *controlRepositoryStub) CompareAndSwapExperimentStatus(
	_ context.Context,
	_ string,
	from ExperimentStatus,
	to ExperimentStatus,
	_ time.Time,
) (Experiment, error) {
	if r.experiment.Status != from {
		return Experiment{}, ErrConflict
	}
	r.experiment.Status = to
	r.experimentCAS++
	return r.experiment, nil
}

func (r *controlRepositoryStub) CreateRun(
	context.Context,
	ResearchRun,
) (ResearchRun, error) {
	return ResearchRun{}, errors.New("unexpected CreateRun")
}

func (r *controlRepositoryStub) GetRun(
	_ context.Context,
	id string,
) (ResearchRun, error) {
	if id != r.run.ID {
		return ResearchRun{}, ErrNotFound
	}
	return r.run, nil
}

func (r *controlRepositoryStub) ListRuns(
	_ context.Context,
	filter RunFilter,
) ([]ResearchRun, error) {
	if filter.ExperimentID == nil || *filter.ExperimentID != r.experiment.ID ||
		filter.Offset > 0 {
		return []ResearchRun{}, nil
	}
	return []ResearchRun{r.run}, nil
}

func (r *controlRepositoryStub) CompareAndSwapRunStatus(
	_ context.Context,
	_ string,
	from RunStatus,
	to RunStatus,
	changedAt time.Time,
) (ResearchRun, error) {
	if r.run.Status != from {
		return ResearchRun{}, ErrConflict
	}
	r.run.Status = to
	finishedAt := changedAt.UTC()
	r.run.FinishedAt = &finishedAt
	return r.run, nil
}

func (r *controlRepositoryStub) UpdateRunLinks(
	context.Context,
	string,
	RunLinks,
	time.Time,
) (ResearchRun, error) {
	return ResearchRun{}, errors.New("unexpected UpdateRunLinks")
}

func (r *controlRepositoryStub) PutOracle(
	context.Context,
	OracleResult,
) (OracleResult, error) {
	return OracleResult{}, errors.New("unexpected PutOracle")
}

func (r *controlRepositoryStub) GetOracle(
	_ context.Context,
	runID string,
) (OracleResult, error) {
	if r.oracle == nil || r.oracle.ResearchRunID != runID {
		return OracleResult{}, ErrNotFound
	}
	return *r.oracle, nil
}

func (r *controlRepositoryStub) ListTransitions(
	_ context.Context,
	filter TransitionFilter,
) ([]Transition, error) {
	if filter.AfterOrdinal != nil {
		return []Transition{}, nil
	}
	return append([]Transition(nil), r.transitions...), nil
}

func (r *controlRepositoryStub) GetProjectionState(
	context.Context,
	string,
) (ProjectionState, error) {
	return ProjectionState{TransitionCount: int64(len(r.transitions))}, nil
}

func (r *controlRepositoryStub) ReplaceProjection(
	context.Context,
	string,
	ProjectionState,
	ProjectionManifest,
	[]Transition,
) ([]Transition, ProjectionState, error) {
	return nil, ProjectionState{}, errors.New("unexpected ReplaceProjection")
}

func (r *controlRepositoryStub) CompareAndSwapRunMetrics(
	_ context.Context,
	_ string,
	_ string,
	metrics RunMetrics,
	_ time.Time,
) (ResearchRun, error) {
	if r.run.Metrics != nil {
		if r.run.Metrics.SourceSHA256 == metrics.SourceSHA256 &&
			r.run.Metrics.MetricsSHA256 == metrics.MetricsSHA256 {
			return r.run, nil
		}
		return ResearchRun{}, ErrConflict
	}
	r.run.Metrics = &metrics
	r.metricsWrites++
	return r.run, nil
}
