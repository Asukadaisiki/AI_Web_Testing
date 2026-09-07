package research

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"
)

const (
	ExperimentConfigSchemaVersion = "research.experiment_config.v1"
	ScheduleVersion               = "research.schedule.v1"
	SupportedVariant              = "dsl_verification"
)

type ExperimentConfig struct {
	SchemaVersion         string `json:"schema_version"`
	RequestTimeoutSeconds int    `json:"request_timeout_seconds"`
	RunTimeoutSeconds     int    `json:"run_timeout_seconds"`
	CancelGraceSeconds    int    `json:"cancel_grace_seconds"`
	WarmupRepetitions     int    `json:"warmup_repetitions"`
	CleanContext          bool   `json:"clean_context"`
	ScheduleVersion       string `json:"schedule_version"`
}

func (c *ExperimentConfig) NormalizeAndValidate() error {
	c.SchemaVersion = strings.TrimSpace(c.SchemaVersion)
	c.ScheduleVersion = strings.TrimSpace(c.ScheduleVersion)
	if c.SchemaVersion != ExperimentConfigSchemaVersion ||
		c.ScheduleVersion != ScheduleVersion ||
		c.RequestTimeoutSeconds <= 0 ||
		c.RequestTimeoutSeconds > 3600 ||
		c.RunTimeoutSeconds < 60 ||
		c.RunTimeoutSeconds > 3600 ||
		c.CancelGraceSeconds < 0 ||
		c.CancelGraceSeconds > 300 ||
		c.WarmupRepetitions < 0 ||
		!c.CleanContext {
		return fmt.Errorf("%w: experiment config", ErrInvalid)
	}
	return nil
}

func ParseExperimentConfig(raw json.RawMessage) (ExperimentConfig, error) {
	var config ExperimentConfig
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&config); err != nil {
		return ExperimentConfig{}, fmt.Errorf("%w: experiment config: %v", ErrInvalid, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return ExperimentConfig{}, fmt.Errorf("%w: trailing experiment config", ErrInvalid)
	}
	if err := config.NormalizeAndValidate(); err != nil {
		return ExperimentConfig{}, err
	}
	return config, nil
}

type ScheduledRun struct {
	Order int         `json:"order"`
	Run   ResearchRun `json:"run"`
}

type ExperimentStart struct {
	Experiment Experiment     `json:"experiment"`
	Schedule   []ScheduledRun `json:"schedule"`
}

type RunStart struct {
	Run      ResearchRun `json:"run"`
	Deadline time.Time   `json:"deadline"`
}

type ControlRepository interface {
	OwnsProject(context.Context, int64, int64) (bool, error)
	CreateExperiment(context.Context, Experiment) (Experiment, error)
	GetExperiment(context.Context, string) (Experiment, error)
	ListExperiments(context.Context, ExperimentFilter) ([]Experiment, error)
	CompareAndSwapExperimentStatus(
		context.Context,
		string,
		ExperimentStatus,
		ExperimentStatus,
		time.Time,
	) (Experiment, error)
	CreateRun(context.Context, ResearchRun) (ResearchRun, error)
	GetRun(context.Context, string) (ResearchRun, error)
	ListRuns(context.Context, RunFilter) ([]ResearchRun, error)
	CompareAndSwapRunStatus(
		context.Context,
		string,
		RunStatus,
		RunStatus,
		time.Time,
	) (ResearchRun, error)
	UpdateRunLinks(context.Context, string, RunLinks, time.Time) (ResearchRun, error)
	PutOracle(context.Context, OracleResult) (OracleResult, error)
	GetOracle(context.Context, string) (OracleResult, error)
	ListTransitions(context.Context, TransitionFilter) ([]Transition, error)
	GetProjectionState(context.Context, string) (ProjectionState, error)
	ReplaceProjection(
		context.Context,
		string,
		ProjectionState,
		ProjectionManifest,
		[]Transition,
	) ([]Transition, ProjectionState, error)
	CompareAndSwapRunMetrics(
		context.Context,
		string,
		string,
		RunMetrics,
		time.Time,
	) (ResearchRun, error)
}

type Service struct {
	repository ControlRepository
	sources    SourceReader
	now        func() time.Time
}

func NewService(repository ControlRepository, sources SourceReader) *Service {
	return NewServiceWithClock(repository, sources, time.Now)
}

func NewServiceWithClock(
	repository ControlRepository,
	sources SourceReader,
	now func() time.Time,
) *Service {
	return &Service{repository: repository, sources: sources, now: now}
}

func (s *Service) CreateExperiment(
	ctx context.Context,
	actorUserID int64,
	experiment Experiment,
) (Experiment, error) {
	generateID := strings.TrimSpace(experiment.ID) == ""
	if generateID {
		experiment.ID = "pending"
	}
	experiment.Status = ExperimentStatusDraft
	if err := experiment.NormalizeAndValidate(); err != nil {
		return Experiment{}, err
	}
	if generateID {
		experiment.ID = ""
	}
	if err := s.requireProject(ctx, actorUserID, experiment.ProjectID); err != nil {
		return Experiment{}, err
	}
	if _, err := ParseExperimentConfig(experiment.ConfigJSON); err != nil {
		return Experiment{}, err
	}
	if experiment.Variant != SupportedVariant {
		return Experiment{}, fmt.Errorf("%w: unsupported variant", ErrInvalid)
	}
	if generateID {
		id, err := experimentIdentity(experiment)
		if err != nil {
			return Experiment{}, err
		}
		experiment.ID = id
	}
	persisted, err := s.repository.CreateExperiment(ctx, experiment)
	if err == nil {
		return persisted, nil
	}
	if !errors.Is(err, ErrConflict) {
		return Experiment{}, err
	}
	current, getErr := s.repository.GetExperiment(ctx, experiment.ID)
	if getErr != nil {
		return Experiment{}, err
	}
	experiment.CreatedAt = current.CreatedAt
	experiment.UpdatedAt = current.UpdatedAt
	experiment.Status = current.Status
	if !sameExperimentDefinition(current, experiment) {
		return Experiment{}, ErrConflict
	}
	return current, nil
}

func (s *Service) GetExperiment(
	ctx context.Context,
	actorUserID int64,
	projectID int64,
	experimentID string,
) (Experiment, error) {
	if err := s.requireProject(ctx, actorUserID, projectID); err != nil {
		return Experiment{}, err
	}
	experiment, err := s.repository.GetExperiment(ctx, experimentID)
	if err != nil {
		return Experiment{}, err
	}
	if experiment.ProjectID != projectID {
		return Experiment{}, ErrNotFound
	}
	return experiment, nil
}

func (s *Service) ListExperiments(
	ctx context.Context,
	actorUserID int64,
	filter ExperimentFilter,
) ([]Experiment, error) {
	if filter.ProjectID == nil {
		return nil, fmt.Errorf("%w: project_id", ErrInvalid)
	}
	if err := s.requireProject(ctx, actorUserID, *filter.ProjectID); err != nil {
		return nil, err
	}
	if filter.Status != nil && !validExperimentStatus(*filter.Status) {
		return nil, fmt.Errorf("%w: experiment status filter", ErrInvalid)
	}
	if filter.Variant != nil && strings.TrimSpace(*filter.Variant) != SupportedVariant {
		return nil, fmt.Errorf("%w: experiment variant filter", ErrInvalid)
	}
	return s.repository.ListExperiments(ctx, filter)
}

func (s *Service) StartExperiment(
	ctx context.Context,
	actorUserID int64,
	projectID int64,
	experimentID string,
) (ExperimentStart, error) {
	experiment, err := s.GetExperiment(
		ctx, actorUserID, projectID, experimentID,
	)
	if err != nil {
		return ExperimentStart{}, err
	}
	config, err := ParseExperimentConfig(experiment.ConfigJSON)
	if err != nil {
		return ExperimentStart{}, err
	}
	if experiment.Status.Terminal() {
		return ExperimentStart{}, ErrTerminalStatus
	}
	schedule, err := BuildRunSchedule(experiment, config)
	if err != nil {
		return ExperimentStart{}, err
	}
	for index := range schedule {
		persisted, createErr := s.repository.CreateRun(ctx, schedule[index].Run)
		if createErr != nil {
			return ExperimentStart{}, createErr
		}
		schedule[index].Run = persisted
	}
	if experiment.Status == ExperimentStatusDraft {
		experiment, err = s.repository.CompareAndSwapExperimentStatus(
			ctx,
			experiment.ID,
			ExperimentStatusDraft,
			ExperimentStatusActive,
			s.now().UTC(),
		)
		if errors.Is(err, ErrConflict) {
			experiment, err = s.repository.GetExperiment(ctx, experiment.ID)
			if err == nil && experiment.Status != ExperimentStatusActive {
				err = ErrConflict
			}
		}
		if err != nil {
			return ExperimentStart{}, err
		}
	}
	return ExperimentStart{Experiment: experiment, Schedule: schedule}, nil
}

func (s *Service) ListExperimentRuns(
	ctx context.Context,
	actorUserID int64,
	projectID int64,
	experimentID string,
) ([]ScheduledRun, error) {
	experiment, err := s.GetExperiment(
		ctx, actorUserID, projectID, experimentID,
	)
	if err != nil {
		return nil, err
	}
	config, err := ParseExperimentConfig(experiment.ConfigJSON)
	if err != nil {
		return nil, err
	}
	expected, err := BuildRunSchedule(experiment, config)
	if err != nil {
		return nil, err
	}
	runs, err := s.repository.ListRuns(ctx, RunFilter{
		ExperimentID: &experimentID,
		ProjectID:    &projectID,
		Limit:        500,
	})
	if err != nil {
		return nil, err
	}
	byID := make(map[string]ResearchRun, len(runs))
	for _, run := range runs {
		byID[run.ID] = run
	}
	result := make([]ScheduledRun, 0, len(runs))
	for _, item := range expected {
		if run, exists := byID[item.Run.ID]; exists {
			item.Run = run
			result = append(result, item)
			delete(byID, run.ID)
		}
	}
	if len(byID) != 0 {
		return nil, fmt.Errorf("%w: run is outside stable schedule", ErrConflict)
	}
	return result, nil
}

func (s *Service) GetRun(
	ctx context.Context,
	actorUserID int64,
	projectID int64,
	runID string,
) (ResearchRun, error) {
	if err := s.requireProject(ctx, actorUserID, projectID); err != nil {
		return ResearchRun{}, err
	}
	run, err := s.repository.GetRun(ctx, runID)
	if err != nil {
		return ResearchRun{}, err
	}
	if run.ProjectID != projectID {
		return ResearchRun{}, ErrNotFound
	}
	return run, nil
}

func (s *Service) StartRun(
	ctx context.Context,
	actorUserID int64,
	projectID int64,
	runID string,
) (RunStart, error) {
	run, experiment, config, err := s.runContext(
		ctx, actorUserID, projectID, runID,
	)
	if err != nil {
		return RunStart{}, err
	}
	if experiment.Status != ExperimentStatusActive {
		return RunStart{}, ErrConflict
	}
	switch run.Status {
	case RunStatusPending:
		run, err = s.repository.CompareAndSwapRunStatus(
			ctx, run.ID, RunStatusPending, RunStatusRunning, s.now().UTC(),
		)
		if errors.Is(err, ErrConflict) {
			run, err = s.repository.GetRun(ctx, run.ID)
		}
		if err != nil {
			return RunStart{}, err
		}
	case RunStatusRunning:
	default:
		return RunStart{}, ErrTerminalStatus
	}
	if run.StartedAt == nil {
		return RunStart{}, fmt.Errorf("%w: running run has no started_at", ErrConflict)
	}
	return RunStart{
		Run:      run,
		Deadline: run.StartedAt.Add(time.Duration(config.RunTimeoutSeconds) * time.Second),
	}, nil
}

func (s *Service) UpdateRunLinks(
	ctx context.Context,
	actorUserID int64,
	projectID int64,
	runID string,
	links RunLinks,
) (ResearchRun, error) {
	if _, err := s.GetRun(ctx, actorUserID, projectID, runID); err != nil {
		return ResearchRun{}, err
	}
	return s.repository.UpdateRunLinks(ctx, runID, links, s.now().UTC())
}

func (s *Service) PutOracle(
	ctx context.Context,
	actorUserID int64,
	projectID int64,
	runID string,
	executionID int64,
	decision OracleDecision,
) (OracleResult, error) {
	run, err := s.GetRun(ctx, actorUserID, projectID, runID)
	if err != nil {
		return OracleResult{}, err
	}
	if run.Links.ExecutionID == nil || *run.Links.ExecutionID != executionID {
		return OracleResult{}, ErrBrokenLink
	}
	if strings.TrimSpace(decision.ContentSHA256) == "" {
		decision, err = NewOracleDecision(decision)
	} else {
		err = decision.NormalizeAndValidate()
	}
	if err != nil {
		return OracleResult{}, err
	}
	if !oracleHasIndependentSource(decision) {
		return OracleResult{}, fmt.Errorf("%w: oracle requires independent source", ErrInvalid)
	}
	return s.repository.PutOracle(ctx, OracleResult{
		ResearchRunID: runID,
		ExecutionID:   executionID,
		Decision:      decision,
	})
}

func (s *Service) GetOracle(
	ctx context.Context,
	actorUserID int64,
	projectID int64,
	runID string,
) (OracleResult, error) {
	run, err := s.GetRun(ctx, actorUserID, projectID, runID)
	if err != nil {
		return OracleResult{}, err
	}
	result, err := s.repository.GetOracle(ctx, runID)
	if err != nil {
		return OracleResult{}, err
	}
	if run.Links.ExecutionID == nil ||
		*run.Links.ExecutionID != result.ExecutionID {
		return OracleResult{}, ErrBrokenLink
	}
	return result, nil
}

func (s *Service) ProjectMetrics(
	ctx context.Context,
	actorUserID int64,
	projectID int64,
	runID string,
) (ResearchRun, error) {
	run, err := s.GetRun(ctx, actorUserID, projectID, runID)
	if err != nil {
		return ResearchRun{}, err
	}
	source, err := s.immutableMetricSource(ctx, run)
	if err != nil {
		return ResearchRun{}, err
	}
	metrics, err := NewMetricProjector().Project(source)
	if err != nil {
		return ResearchRun{}, err
	}
	return s.repository.CompareAndSwapRunMetrics(
		ctx, runID, "", metrics, s.now().UTC(),
	)
}

func (s *Service) FinishRun(
	ctx context.Context,
	actorUserID int64,
	projectID int64,
	runID string,
	status RunStatus,
) (ResearchRun, error) {
	if status != RunStatusCompleted && status != RunStatusFailed {
		return ResearchRun{}, fmt.Errorf("%w: finish status", ErrInvalid)
	}
	run, experiment, config, err := s.runContext(
		ctx, actorUserID, projectID, runID,
	)
	if err != nil {
		return ResearchRun{}, err
	}
	if run.StartedAt == nil {
		return ResearchRun{}, fmt.Errorf("%w: finished run was not started", ErrConflict)
	}
	if status == RunStatusCompleted {
		if err := s.validateCompletedRun(ctx, run); err != nil {
			return ResearchRun{}, err
		}
	}
	if run.Status == status {
		if err := s.completeExperimentIfAllRunsTerminal(
			ctx, experiment, config,
		); err != nil {
			return ResearchRun{}, err
		}
		return run, nil
	}
	if run.Status != RunStatusRunning {
		return ResearchRun{}, ErrConflict
	}
	run, err = s.repository.CompareAndSwapRunStatus(
		ctx, runID, RunStatusRunning, status, s.now().UTC(),
	)
	if err != nil {
		return ResearchRun{}, err
	}
	if err := s.completeExperimentIfAllRunsTerminal(
		ctx, experiment, config,
	); err != nil {
		return ResearchRun{}, err
	}
	return run, nil
}

func (s *Service) CancelRun(
	ctx context.Context,
	actorUserID int64,
	projectID int64,
	runID string,
) (ResearchRun, error) {
	run, experiment, config, err := s.runContext(
		ctx, actorUserID, projectID, runID,
	)
	if err != nil {
		return ResearchRun{}, err
	}
	if run.Status == RunStatusCancelled {
		if err := s.completeExperimentIfAllRunsTerminal(
			ctx, experiment, config,
		); err != nil {
			return ResearchRun{}, err
		}
		return run, nil
	}
	if run.Status.Terminal() {
		return ResearchRun{}, ErrTerminalStatus
	}
	run, err = s.repository.CompareAndSwapRunStatus(
		ctx, runID, run.Status, RunStatusCancelled, s.now().UTC(),
	)
	if err != nil {
		return ResearchRun{}, err
	}
	if err := s.completeExperimentIfAllRunsTerminal(
		ctx, experiment, config,
	); err != nil {
		return ResearchRun{}, err
	}
	return run, nil
}

func BuildRunSchedule(
	experiment Experiment,
	config ExperimentConfig,
) ([]ScheduledRun, error) {
	if err := experiment.NormalizeAndValidate(); err != nil {
		return nil, err
	}
	if err := config.NormalizeAndValidate(); err != nil {
		return nil, err
	}
	if experiment.Variant != SupportedVariant {
		return nil, fmt.Errorf("%w: unsupported variant", ErrInvalid)
	}
	warmups := scheduleGroup(experiment, true, config.WarmupRepetitions)
	measured := scheduleGroup(experiment, false, experiment.Repetitions)
	runs := append(warmups, measured...)
	result := make([]ScheduledRun, len(runs))
	for index, run := range runs {
		result[index] = ScheduledRun{Order: index, Run: run}
	}
	return result, nil
}

func scheduleGroup(
	experiment Experiment,
	warmup bool,
	repetitions int,
) []ResearchRun {
	result := make([]ResearchRun, 0, repetitions)
	for repetition := range repetitions {
		group := "measured"
		if warmup {
			group = "warmup"
		}
		key := fmt.Sprintf(
			"%s:%d:%s:%d",
			ScheduleVersion, experiment.Seed, group, repetition,
		)
		digest := sha256.Sum256([]byte(experiment.ID + ":" + key))
		result = append(result, ResearchRun{
			ID:              "research-run-" + hex.EncodeToString(digest[:16]),
			ExperimentID:    experiment.ID,
			ProjectID:       experiment.ProjectID,
			IdempotencyKey:  key,
			RepetitionIndex: repetition,
			Warmup:          warmup,
			Status:          RunStatusPending,
			Versions:        DefaultVersionSnapshot(),
		})
	}
	slices.SortFunc(result, func(left, right ResearchRun) int {
		leftHash := sha256.Sum256([]byte(
			fmt.Sprintf("%d:%s", experiment.Seed, left.IdempotencyKey),
		))
		rightHash := sha256.Sum256([]byte(
			fmt.Sprintf("%d:%s", experiment.Seed, right.IdempotencyKey),
		))
		return bytes.Compare(leftHash[:], rightHash[:])
	})
	return result
}

func (s *Service) requireProject(
	ctx context.Context,
	actorUserID int64,
	projectID int64,
) error {
	owned, err := s.repository.OwnsProject(ctx, actorUserID, projectID)
	if err != nil {
		return err
	}
	if !owned {
		return ErrNotFound
	}
	return nil
}

func (s *Service) runContext(
	ctx context.Context,
	actorUserID int64,
	projectID int64,
	runID string,
) (ResearchRun, Experiment, ExperimentConfig, error) {
	run, err := s.GetRun(ctx, actorUserID, projectID, runID)
	if err != nil {
		return ResearchRun{}, Experiment{}, ExperimentConfig{}, err
	}
	experiment, err := s.GetExperiment(
		ctx, actorUserID, projectID, run.ExperimentID,
	)
	if err != nil {
		return ResearchRun{}, Experiment{}, ExperimentConfig{}, err
	}
	config, err := ParseExperimentConfig(experiment.ConfigJSON)
	return run, experiment, config, err
}

func (s *Service) loadOrRebuildTransitions(
	ctx context.Context,
	runID string,
	snapshot SourceSnapshot,
) ([]Transition, error) {
	state, err := s.repository.GetProjectionState(ctx, runID)
	if err != nil {
		return nil, err
	}
	if state.TransitionCount == 0 {
		projected, manifest, projectErr := NewProjector().Project(snapshot)
		if projectErr != nil {
			return nil, projectErr
		}
		persisted, _, replaceErr := s.repository.ReplaceProjection(
			ctx, runID, state, manifest, projected,
		)
		if replaceErr == nil {
			return persisted, nil
		}
		if !errors.Is(replaceErr, ErrConflict) {
			return nil, replaceErr
		}
	}
	result := make([]Transition, 0, state.TransitionCount)
	var after *int64
	for {
		page, listErr := s.repository.ListTransitions(ctx, TransitionFilter{
			ResearchRunID: runID,
			AfterOrdinal:  after,
			Limit:         500,
		})
		if listErr != nil {
			return nil, listErr
		}
		result = append(result, page...)
		if len(page) < 500 {
			break
		}
		value := page[len(page)-1].Ordinal
		after = &value
	}
	return result, nil
}

func (s *Service) immutableMetricSource(
	ctx context.Context,
	run ResearchRun,
) (MetricSource, error) {
	if !completeRunLinks(run.Links) {
		return MetricSource{}, ErrBrokenLink
	}
	if s.sources == nil {
		return MetricSource{}, errors.New("research source reader is unavailable")
	}
	snapshot, err := s.sources.Read(ctx, run.ID)
	if err != nil {
		return MetricSource{}, err
	}
	if snapshot.ProjectID != run.ProjectID ||
		snapshot.ResearchRunID != run.ID ||
		!sameRunLinks(snapshot.FinalRunLinks, run.Links) {
		return MetricSource{}, ErrBrokenLink
	}
	persistedOracle, err := s.repository.GetOracle(ctx, run.ID)
	if err != nil {
		return MetricSource{}, err
	}
	if persistedOracle.ExecutionID != *snapshot.FinalRunLinks.ExecutionID {
		return MetricSource{}, ErrBrokenLink
	}
	transitions, err := s.loadOrRebuildTransitions(ctx, run.ID, snapshot)
	if err != nil {
		return MetricSource{}, err
	}
	return NewMetricSource(
		snapshot,
		transitions,
		Available(persistedOracle.Decision),
	)
}

func (s *Service) validateCompletedRun(
	ctx context.Context,
	run ResearchRun,
) error {
	if !completeRunLinks(run.Links) {
		return ErrBrokenLink
	}
	if run.Metrics == nil {
		return fmt.Errorf("%w: completed run requires metrics", ErrConflict)
	}
	if err := validateProjectedMetrics(*run.Metrics); err != nil {
		return err
	}
	source, err := s.immutableMetricSource(ctx, run)
	if err != nil {
		return err
	}
	expected, err := NewMetricProjector().Project(source)
	if err != nil {
		return err
	}
	if run.Metrics.SourceSHA256 != source.SourceSHA256 ||
		run.Metrics.MetricsSHA256 != expected.MetricsSHA256 {
		return fmt.Errorf("%w: completed run metrics provenance", ErrSourceChanged)
	}
	return nil
}

func validateProjectedMetrics(metrics RunMetrics) error {
	if err := metrics.Validate(); err != nil {
		return err
	}
	if metrics.ProjectorVersion != MetricProjectorVersion ||
		!sha256Pattern.MatchString(metrics.SourceSHA256) ||
		!sha256Pattern.MatchString(metrics.MetricsSHA256) {
		return fmt.Errorf("%w: completed run metrics provenance", ErrInvalid)
	}
	return nil
}

func (s *Service) completeExperimentIfAllRunsTerminal(
	ctx context.Context,
	experiment Experiment,
	config ExperimentConfig,
) error {
	if experiment.Status != ExperimentStatusActive {
		return nil
	}
	expectedRuns := experiment.Repetitions + config.WarmupRepetitions
	terminalRuns := 0
	for offset := 0; ; offset += 500 {
		runs, err := s.repository.ListRuns(ctx, RunFilter{
			ExperimentID: &experiment.ID,
			ProjectID:    &experiment.ProjectID,
			Limit:        500,
			Offset:       offset,
		})
		if err != nil {
			return err
		}
		for _, run := range runs {
			if !run.Status.Terminal() {
				return nil
			}
			terminalRuns++
		}
		if len(runs) < 500 {
			break
		}
	}
	if terminalRuns != expectedRuns {
		return nil
	}
	_, err := s.repository.CompareAndSwapExperimentStatus(
		ctx,
		experiment.ID,
		ExperimentStatusActive,
		ExperimentStatusCompleted,
		s.now().UTC(),
	)
	if err == nil {
		return nil
	}
	if !errors.Is(err, ErrConflict) && !errors.Is(err, ErrTerminalStatus) {
		return err
	}
	current, getErr := s.repository.GetExperiment(ctx, experiment.ID)
	if getErr != nil {
		return getErr
	}
	if current.Status.Terminal() {
		return nil
	}
	return err
}

func experimentIdentity(experiment Experiment) (string, error) {
	copy := experiment
	copy.ID = ""
	copy.Status = ExperimentStatusDraft
	copy.CreatedAt = time.Time{}
	copy.UpdatedAt = time.Time{}
	hash, err := CanonicalSHA256(copy)
	if err != nil {
		return "", err
	}
	return "research-experiment-" + hash[:32], nil
}

func sameExperimentDefinition(left, right Experiment) bool {
	left.CreatedAt, left.UpdatedAt = time.Time{}, time.Time{}
	right.CreatedAt, right.UpdatedAt = time.Time{}, time.Time{}
	left.Status, right.Status = ExperimentStatusDraft, ExperimentStatusDraft
	leftJSON, leftErr := CanonicalSHA256(left)
	rightJSON, rightErr := CanonicalSHA256(right)
	return leftErr == nil && rightErr == nil && leftJSON == rightJSON
}

func sameRunLinks(left, right RunLinks) bool {
	leftHash, leftErr := CanonicalSHA256(left)
	rightHash, rightErr := CanonicalSHA256(right)
	return leftErr == nil && rightErr == nil && leftHash == rightHash
}

func oracleHasIndependentSource(decision OracleDecision) bool {
	for _, source := range decision.Sources {
		if source.Kind == SourceOracle {
			return true
		}
	}
	return false
}
