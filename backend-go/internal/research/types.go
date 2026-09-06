package research

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
)

const (
	SchemaVersion    = "research.persistence.v1"
	ProjectorVersion = "research.projector.v1"
	MetricVersion    = "research.metrics.v1"
	PolicyVersion    = "research.policy.v1"

	MaxControlJSONBytes    = 64 * 1024
	MaxMetricsJSONBytes    = 64 * 1024
	MaxTransitionJSONBytes = 256 * 1024
	MaxArtifactRefs        = 64
)

var (
	ErrNotFound       = errors.New("research resource not found")
	ErrConflict       = errors.New("research resource conflict")
	ErrInvalid        = errors.New("invalid research resource")
	ErrInvalidStatus  = errors.New("invalid research status transition")
	ErrBrokenLink     = errors.New("research run link chain is invalid")
	ErrTerminalStatus = errors.New("research resource is terminal")

	sha256Pattern           = regexp.MustCompile(`^[0-9a-f]{64}$`)
	forbiddenTransitionKeys = map[string]struct{}{
		"dsl":               {},
		"dsl_json":          {},
		"full_dsl":          {},
		"full_report":       {},
		"image_base64":      {},
		"raw_report":        {},
		"report":            {},
		"report_json":       {},
		"screenshot":        {},
		"screenshot_base64": {},
		"transcript":        {},
		"transcript_json":   {},
	}
)

type ExperimentStatus string

const (
	ExperimentStatusDraft     ExperimentStatus = "draft"
	ExperimentStatusActive    ExperimentStatus = "active"
	ExperimentStatusCompleted ExperimentStatus = "completed"
	ExperimentStatusCancelled ExperimentStatus = "cancelled"
)

func (s ExperimentStatus) Terminal() bool {
	return s == ExperimentStatusCompleted || s == ExperimentStatusCancelled
}

func (s ExperimentStatus) CanTransition(to ExperimentStatus) bool {
	if s == to {
		return true
	}
	switch s {
	case ExperimentStatusDraft:
		return to == ExperimentStatusActive || to == ExperimentStatusCancelled
	case ExperimentStatusActive:
		return to == ExperimentStatusCompleted || to == ExperimentStatusCancelled
	default:
		return false
	}
}

type RunStatus string

const (
	RunStatusPending   RunStatus = "pending"
	RunStatusRunning   RunStatus = "running"
	RunStatusCompleted RunStatus = "completed"
	RunStatusFailed    RunStatus = "failed"
	RunStatusCancelled RunStatus = "cancelled"
)

func (s RunStatus) Terminal() bool {
	return s == RunStatusCompleted || s == RunStatusFailed || s == RunStatusCancelled
}

func (s RunStatus) CanTransition(to RunStatus) bool {
	if s == to {
		return true
	}
	switch s {
	case RunStatusPending:
		return to == RunStatusRunning || to == RunStatusCancelled
	case RunStatusRunning:
		return to == RunStatusCompleted || to == RunStatusFailed || to == RunStatusCancelled
	default:
		return false
	}
}

type Experiment struct {
	ID                 string           `json:"id"`
	ProjectID          int64            `json:"project_id"`
	Name               string           `json:"name"`
	Goal               string           `json:"goal"`
	DatasetVersion     string           `json:"dataset_version"`
	ModelProvider      string           `json:"model_provider"`
	ModelName          string           `json:"model_name"`
	ModelVersion       string           `json:"model_version"`
	PromptVersion      string           `json:"prompt_version"`
	BrowserName        string           `json:"browser_name"`
	BrowserVersion     string           `json:"browser_version"`
	ViewportJSON       json.RawMessage  `json:"viewport"`
	CodeSHA256         string           `json:"code_sha256"`
	PolicyVersion      string           `json:"policy_version"`
	ObservationProfile string           `json:"observation_profile"`
	DSLProfile         string           `json:"dsl_profile"`
	Seed               int64            `json:"seed"`
	Variant            string           `json:"variant"`
	Repetitions        int              `json:"repetitions"`
	Status             ExperimentStatus `json:"status"`
	ConfigJSON         json.RawMessage  `json:"config"`
	CreatedAt          time.Time        `json:"created_at"`
	UpdatedAt          time.Time        `json:"updated_at"`
}

func (e *Experiment) NormalizeAndValidate() error {
	e.ID = strings.TrimSpace(e.ID)
	e.Name = strings.TrimSpace(e.Name)
	e.Goal = strings.TrimSpace(e.Goal)
	if e.ID == "" || len(e.ID) > 64 || e.ProjectID <= 0 || e.Name == "" || len(e.Name) > 200 ||
		e.Goal == "" || len(e.Goal) > 20_000 {
		return fmt.Errorf("%w: experiment identity, project, name, or goal", ErrInvalid)
	}
	required := []*string{
		&e.DatasetVersion, &e.ModelProvider, &e.ModelName, &e.ModelVersion, &e.PromptVersion,
		&e.BrowserName, &e.BrowserVersion, &e.PolicyVersion, &e.ObservationProfile,
		&e.DSLProfile, &e.Variant,
	}
	for _, value := range required {
		*value = strings.TrimSpace(*value)
		if *value == "" || len(*value) > 200 {
			return fmt.Errorf("%w: experiment control variable", ErrInvalid)
		}
	}
	if e.PolicyVersion != PolicyVersion {
		return fmt.Errorf("%w: unsupported policy_version %q", ErrInvalid, e.PolicyVersion)
	}
	e.CodeSHA256 = strings.ToLower(strings.TrimSpace(e.CodeSHA256))
	if !sha256Pattern.MatchString(e.CodeSHA256) {
		return fmt.Errorf("%w: code_sha256", ErrInvalid)
	}
	if e.Repetitions < 1 {
		return fmt.Errorf("%w: repetitions", ErrInvalid)
	}
	if !validExperimentStatus(e.Status) {
		return fmt.Errorf("%w: experiment status", ErrInvalid)
	}
	viewport, err := normalizeJSONObject(e.ViewportJSON, MaxControlJSONBytes, false)
	if err != nil {
		return fmt.Errorf("%w: viewport: %v", ErrInvalid, err)
	}
	config, err := normalizeJSONObject(e.ConfigJSON, MaxControlJSONBytes, true)
	if err != nil {
		return fmt.Errorf("%w: config: %v", ErrInvalid, err)
	}
	e.ViewportJSON = viewport
	e.ConfigJSON = config
	e.CreatedAt = utc(e.CreatedAt)
	e.UpdatedAt = utc(e.UpdatedAt)
	return nil
}

type RunLinks struct {
	AgentRunID   *string `json:"agent_run_id,omitempty"`
	GenerationID *int64  `json:"generation_id,omitempty"`
	BatchID      *int64  `json:"batch_id,omitempty"`
	ExecutionID  *int64  `json:"execution_id,omitempty"`
	DSLSHA256    *string `json:"dsl_sha256,omitempty"`
}

func (l *RunLinks) NormalizeAndValidate() error {
	if l.AgentRunID != nil {
		value := strings.TrimSpace(*l.AgentRunID)
		if value == "" || len(value) > 64 {
			return fmt.Errorf("%w: agent_run_id", ErrInvalid)
		}
		l.AgentRunID = &value
	}
	for name, value := range map[string]*int64{
		"generation_id": l.GenerationID,
		"batch_id":      l.BatchID,
		"execution_id":  l.ExecutionID,
	} {
		if value != nil && *value <= 0 {
			return fmt.Errorf("%w: %s", ErrInvalid, name)
		}
	}
	if l.DSLSHA256 != nil {
		value := strings.ToLower(strings.TrimSpace(*l.DSLSHA256))
		if !sha256Pattern.MatchString(value) {
			return fmt.Errorf("%w: dsl_sha256", ErrInvalid)
		}
		l.DSLSHA256 = &value
	}
	if l.GenerationID != nil && l.AgentRunID == nil ||
		l.BatchID != nil && l.GenerationID == nil ||
		l.ExecutionID != nil && l.BatchID == nil ||
		l.DSLSHA256 != nil && l.GenerationID == nil {
		return fmt.Errorf("%w: links must form an ordered prefix", ErrBrokenLink)
	}
	return nil
}

type VersionSnapshot struct {
	SchemaVersion    string `json:"schema_version"`
	ProjectorVersion string `json:"projector_version"`
	MetricVersion    string `json:"metric_version"`
	PolicyVersion    string `json:"policy_version"`
}

func DefaultVersionSnapshot() VersionSnapshot {
	return VersionSnapshot{
		SchemaVersion:    SchemaVersion,
		ProjectorVersion: ProjectorVersion,
		MetricVersion:    MetricVersion,
		PolicyVersion:    PolicyVersion,
	}
}

func (v *VersionSnapshot) NormalizeAndValidate() error {
	v.SchemaVersion = strings.TrimSpace(v.SchemaVersion)
	v.ProjectorVersion = strings.TrimSpace(v.ProjectorVersion)
	v.MetricVersion = strings.TrimSpace(v.MetricVersion)
	v.PolicyVersion = strings.TrimSpace(v.PolicyVersion)
	expected := map[string]string{
		"schema_version":    SchemaVersion,
		"projector_version": ProjectorVersion,
		"metric_version":    MetricVersion,
		"policy_version":    PolicyVersion,
	}
	actual := map[string]string{
		"schema_version":    v.SchemaVersion,
		"projector_version": v.ProjectorVersion,
		"metric_version":    v.MetricVersion,
		"policy_version":    v.PolicyVersion,
	}
	for name, want := range expected {
		if actual[name] != want {
			return fmt.Errorf("%w: unsupported %s %q", ErrInvalid, name, actual[name])
		}
	}
	return nil
}

type ResearchRun struct {
	ID              string          `json:"id"`
	ExperimentID    string          `json:"experiment_id"`
	ProjectID       int64           `json:"project_id"`
	IdempotencyKey  string          `json:"idempotency_key"`
	RepetitionIndex int             `json:"repetition_index"`
	Warmup          bool            `json:"warmup"`
	Status          RunStatus       `json:"status"`
	Versions        VersionSnapshot `json:"versions"`
	Links           RunLinks        `json:"links"`
	Metrics         *RunMetrics     `json:"metrics,omitempty"`
	StartedAt       *time.Time      `json:"started_at,omitempty"`
	FinishedAt      *time.Time      `json:"finished_at,omitempty"`
	CreatedAt       time.Time       `json:"created_at"`
	UpdatedAt       time.Time       `json:"updated_at"`
}

func (r *ResearchRun) NormalizeAndValidate() error {
	r.ID = strings.TrimSpace(r.ID)
	r.ExperimentID = strings.TrimSpace(r.ExperimentID)
	r.IdempotencyKey = strings.TrimSpace(r.IdempotencyKey)
	if r.ID == "" || len(r.ID) > 64 || r.ExperimentID == "" || len(r.ExperimentID) > 64 ||
		r.ProjectID <= 0 || r.IdempotencyKey == "" || len(r.IdempotencyKey) > 200 ||
		r.RepetitionIndex < 0 || !validRunStatus(r.Status) {
		return fmt.Errorf("%w: research run identity, status, or repetition", ErrInvalid)
	}
	if err := r.Versions.NormalizeAndValidate(); err != nil {
		return err
	}
	if err := r.Links.NormalizeAndValidate(); err != nil {
		return err
	}
	if r.Metrics != nil {
		if err := r.Metrics.Validate(); err != nil {
			return err
		}
	}
	r.StartedAt = utcPtr(r.StartedAt)
	r.FinishedAt = utcPtr(r.FinishedAt)
	r.CreatedAt = utc(r.CreatedAt)
	r.UpdatedAt = utc(r.UpdatedAt)
	switch r.Status {
	case RunStatusPending:
		if r.StartedAt != nil || r.FinishedAt != nil {
			return fmt.Errorf("%w: pending run must not have timestamps", ErrInvalid)
		}
	case RunStatusRunning:
		if r.StartedAt == nil || r.FinishedAt != nil {
			return fmt.Errorf("%w: running run requires only started_at", ErrInvalid)
		}
	case RunStatusCompleted, RunStatusFailed:
		if r.StartedAt == nil || r.FinishedAt == nil {
			return fmt.Errorf("%w: completed or failed run requires both timestamps", ErrInvalid)
		}
	case RunStatusCancelled:
		if r.FinishedAt == nil {
			return fmt.Errorf("%w: cancelled run requires finished_at", ErrInvalid)
		}
	}
	if r.StartedAt != nil && r.FinishedAt != nil && r.FinishedAt.Before(*r.StartedAt) {
		return fmt.Errorf("%w: finished_at precedes started_at", ErrInvalid)
	}
	return nil
}

type NullableValue[T any] struct {
	Value             *T      `json:"value"`
	UnavailableReason *string `json:"unavailable_reason"`
}

func (v NullableValue[T]) validate(name string) error {
	if (v.Value == nil) == (v.UnavailableReason == nil) {
		return fmt.Errorf("%w: metric %s requires exactly one of value or unavailable_reason", ErrInvalid, name)
	}
	if v.UnavailableReason != nil {
		reason := strings.TrimSpace(*v.UnavailableReason)
		if reason == "" || len(reason) > 200 {
			return fmt.Errorf("%w: metric %s unavailable_reason", ErrInvalid, name)
		}
	}
	return nil
}

type RunMetrics struct {
	SchemaVersion       string                 `json:"schema_version"`
	TaskSuccess         NullableValue[bool]    `json:"task_success"`
	GroundingAccuracy   NullableValue[float64] `json:"grounding_accuracy"`
	InvalidActionRate   NullableValue[float64] `json:"invalid_action_rate"`
	ExecutionSuccess    NullableValue[bool]    `json:"execution_success"`
	VerificationSuccess NullableValue[bool]    `json:"verification_success"`
	RecoveryRate        NullableValue[float64] `json:"recovery_rate"`
	Steps               NullableValue[int64]   `json:"steps"`
	Retries             NullableValue[int64]   `json:"retries"`
	LLMCalls            NullableValue[int64]   `json:"llm_calls"`
	InputTokens         NullableValue[int64]   `json:"input_tokens"`
	OutputTokens        NullableValue[int64]   `json:"output_tokens"`
	TotalTokens         NullableValue[int64]   `json:"total_tokens"`
	LatencyMS           NullableValue[int64]   `json:"latency_ms"`
	VisionCalls         NullableValue[int64]   `json:"vision_calls"`
}

func (m RunMetrics) Validate() error {
	if strings.TrimSpace(m.SchemaVersion) != MetricVersion {
		return fmt.Errorf("%w: unsupported metrics schema_version %q", ErrInvalid, m.SchemaVersion)
	}
	values := []struct {
		name string
		err  error
	}{
		{"task_success", m.TaskSuccess.validate("task_success")},
		{"grounding_accuracy", m.GroundingAccuracy.validate("grounding_accuracy")},
		{"invalid_action_rate", m.InvalidActionRate.validate("invalid_action_rate")},
		{"execution_success", m.ExecutionSuccess.validate("execution_success")},
		{"verification_success", m.VerificationSuccess.validate("verification_success")},
		{"recovery_rate", m.RecoveryRate.validate("recovery_rate")},
		{"steps", m.Steps.validate("steps")},
		{"retries", m.Retries.validate("retries")},
		{"llm_calls", m.LLMCalls.validate("llm_calls")},
		{"input_tokens", m.InputTokens.validate("input_tokens")},
		{"output_tokens", m.OutputTokens.validate("output_tokens")},
		{"total_tokens", m.TotalTokens.validate("total_tokens")},
		{"latency_ms", m.LatencyMS.validate("latency_ms")},
		{"vision_calls", m.VisionCalls.validate("vision_calls")},
	}
	for _, item := range values {
		if item.err != nil {
			return item.err
		}
	}
	for name, metric := range map[string]NullableValue[float64]{
		"grounding_accuracy":  m.GroundingAccuracy,
		"invalid_action_rate": m.InvalidActionRate,
		"recovery_rate":       m.RecoveryRate,
	} {
		if metric.Value != nil && (*metric.Value < 0 || *metric.Value > 1) {
			return fmt.Errorf("%w: metric %s must be within [0,1]", ErrInvalid, name)
		}
	}
	for name, metric := range map[string]NullableValue[int64]{
		"steps": m.Steps, "retries": m.Retries, "llm_calls": m.LLMCalls,
		"input_tokens": m.InputTokens, "output_tokens": m.OutputTokens,
		"total_tokens": m.TotalTokens, "latency_ms": m.LatencyMS,
		"vision_calls": m.VisionCalls,
	} {
		if metric.Value != nil && *metric.Value < 0 {
			return fmt.Errorf("%w: metric %s must be non-negative", ErrInvalid, name)
		}
	}
	if m.InputTokens.Value != nil && m.OutputTokens.Value != nil &&
		m.TotalTokens.Value != nil &&
		*m.TotalTokens.Value != *m.InputTokens.Value+*m.OutputTokens.Value {
		return fmt.Errorf("%w: total_tokens must equal input_tokens + output_tokens", ErrInvalid)
	}
	raw, err := json.Marshal(m)
	if err != nil || len(raw) > MaxMetricsJSONBytes {
		return fmt.Errorf("%w: metrics JSON exceeds limit", ErrInvalid)
	}
	return nil
}

type ArtifactRef struct {
	Kind          string `json:"kind"`
	URI           string `json:"uri"`
	SHA256        string `json:"sha256"`
	MediaType     string `json:"media_type"`
	SchemaVersion string `json:"schema_version,omitempty"`
	SizeBytes     *int64 `json:"size_bytes,omitempty"`
}

func (a *ArtifactRef) NormalizeAndValidate() error {
	a.Kind = strings.TrimSpace(a.Kind)
	a.URI = strings.TrimSpace(a.URI)
	a.SHA256 = strings.ToLower(strings.TrimSpace(a.SHA256))
	a.MediaType = strings.TrimSpace(a.MediaType)
	a.SchemaVersion = strings.TrimSpace(a.SchemaVersion)
	if a.Kind == "" || len(a.Kind) > 64 || a.URI == "" || len(a.URI) > 2048 ||
		!sha256Pattern.MatchString(a.SHA256) || a.MediaType == "" || len(a.MediaType) > 200 ||
		len(a.SchemaVersion) > 64 || a.SizeBytes != nil && *a.SizeBytes < 0 {
		return fmt.Errorf("%w: artifact reference", ErrInvalid)
	}
	return nil
}

type Transition struct {
	ID            int64           `json:"id"`
	ResearchRunID string          `json:"research_run_id"`
	Ordinal       int64           `json:"ordinal"`
	AppendKey     string          `json:"append_key"`
	ContentSHA256 string          `json:"content_sha256"`
	SchemaVersion string          `json:"schema_version"`
	PayloadJSON   json.RawMessage `json:"transition"`
	ArtifactRefs  []ArtifactRef   `json:"artifact_refs"`
	CreatedAt     time.Time       `json:"created_at"`
}

func (t *Transition) NormalizeAndValidate() error {
	t.ResearchRunID = strings.TrimSpace(t.ResearchRunID)
	t.AppendKey = strings.TrimSpace(t.AppendKey)
	t.ContentSHA256 = strings.ToLower(strings.TrimSpace(t.ContentSHA256))
	if t.ResearchRunID == "" || len(t.ResearchRunID) > 64 || t.Ordinal < 0 ||
		t.AppendKey == "" || len(t.AppendKey) > 200 ||
		!sha256Pattern.MatchString(t.ContentSHA256) {
		return fmt.Errorf("%w: transition identity or version", ErrInvalid)
	}
	t.SchemaVersion = strings.TrimSpace(t.SchemaVersion)
	if t.SchemaVersion != SchemaVersion {
		return fmt.Errorf("%w: unsupported transition schema_version %q", ErrInvalid, t.SchemaVersion)
	}
	payload, err := normalizeJSONObject(t.PayloadJSON, MaxTransitionJSONBytes, false)
	if err != nil {
		return fmt.Errorf("%w: transition payload: %v", ErrInvalid, err)
	}
	t.PayloadJSON = payload
	if err := rejectEmbeddedLargeObjects(payload); err != nil {
		return err
	}
	if len(t.ArtifactRefs) > MaxArtifactRefs {
		return fmt.Errorf("%w: too many artifact references", ErrInvalid)
	}
	for index := range t.ArtifactRefs {
		if err := t.ArtifactRefs[index].NormalizeAndValidate(); err != nil {
			return err
		}
	}
	sortArtifactRefs(t.ArtifactRefs)
	contentSHA256, err := TransitionContentSHA256(
		t.SchemaVersion,
		t.PayloadJSON,
		t.ArtifactRefs,
	)
	if err != nil || contentSHA256 != t.ContentSHA256 {
		return fmt.Errorf("%w: transition content_sha256 mismatch", ErrInvalid)
	}
	t.CreatedAt = utc(t.CreatedAt)
	return nil
}

func TransitionContentSHA256(
	schemaVersion string,
	payload json.RawMessage,
	artifactRefs []ArtifactRef,
) (string, error) {
	schemaVersion = strings.TrimSpace(schemaVersion)
	if schemaVersion != SchemaVersion {
		return "", fmt.Errorf("%w: unsupported transition schema_version %q", ErrInvalid, schemaVersion)
	}
	var normalizedPayload map[string]any
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.UseNumber()
	if err := decoder.Decode(&normalizedPayload); err != nil || normalizedPayload == nil {
		return "", fmt.Errorf("%w: transition payload", ErrInvalid)
	}
	if err := rejectForbiddenKeys(normalizedPayload); err != nil {
		return "", err
	}
	normalizedArtifacts := append([]ArtifactRef(nil), artifactRefs...)
	for index := range normalizedArtifacts {
		if err := normalizedArtifacts[index].NormalizeAndValidate(); err != nil {
			return "", err
		}
	}
	sortArtifactRefs(normalizedArtifacts)
	envelope := struct {
		SchemaVersion string         `json:"schema_version"`
		Transition    map[string]any `json:"transition"`
		ArtifactRefs  []ArtifactRef  `json:"artifact_refs"`
	}{
		SchemaVersion: schemaVersion,
		Transition:    normalizedPayload,
		ArtifactRefs:  normalizedArtifacts,
	}
	raw, err := json.Marshal(envelope)
	if err != nil {
		return "", fmt.Errorf("encode transition content: %w", err)
	}
	sum := sha256.Sum256(raw)
	return fmt.Sprintf("%x", sum), nil
}

func sortArtifactRefs(refs []ArtifactRef) {
	slices.SortFunc(refs, func(left, right ArtifactRef) int {
		if value := strings.Compare(left.Kind, right.Kind); value != 0 {
			return value
		}
		if value := strings.Compare(left.URI, right.URI); value != 0 {
			return value
		}
		if value := strings.Compare(left.SHA256, right.SHA256); value != 0 {
			return value
		}
		if value := strings.Compare(left.MediaType, right.MediaType); value != 0 {
			return value
		}
		if value := strings.Compare(left.SchemaVersion, right.SchemaVersion); value != 0 {
			return value
		}
		switch {
		case left.SizeBytes == nil && right.SizeBytes != nil:
			return -1
		case left.SizeBytes != nil && right.SizeBytes == nil:
			return 1
		case left.SizeBytes == nil:
			return 0
		case *left.SizeBytes < *right.SizeBytes:
			return -1
		case *left.SizeBytes > *right.SizeBytes:
			return 1
		default:
			return 0
		}
	})
}

type ExperimentFilter struct {
	ProjectID *int64
	Status    *ExperimentStatus
	Variant   *string
	Limit     int
	Offset    int
}

type RunFilter struct {
	ExperimentID *string
	ProjectID    *int64
	Status       *RunStatus
	AgentRunID   *string
	Limit        int
	Offset       int
}

type TransitionFilter struct {
	ResearchRunID string
	AfterOrdinal  *int64
	Limit         int
}

type Repository interface {
	CreateExperiment(context.Context, Experiment) (Experiment, error)
	GetExperiment(context.Context, string) (Experiment, error)
	ListExperiments(context.Context, ExperimentFilter) ([]Experiment, error)
	CompareAndSwapExperimentStatus(context.Context, string, ExperimentStatus, ExperimentStatus, time.Time) (Experiment, error)
	DeleteExperiment(context.Context, string) error

	CreateRun(context.Context, ResearchRun) (ResearchRun, error)
	GetRun(context.Context, string) (ResearchRun, error)
	ListRuns(context.Context, RunFilter) ([]ResearchRun, error)
	CompareAndSwapRunStatus(context.Context, string, RunStatus, RunStatus, time.Time) (ResearchRun, error)
	UpdateRunLinks(context.Context, string, RunLinks, time.Time) (ResearchRun, error)
	PutRunMetrics(context.Context, string, RunMetrics, time.Time) (ResearchRun, error)
	DeleteRun(context.Context, string) error

	AppendTransitions(context.Context, string, []Transition) ([]Transition, error)
	ListTransitions(context.Context, TransitionFilter) ([]Transition, error)
	DeleteTransitions(context.Context, string) error
	GetProjectionState(context.Context, string) (ProjectionState, error)
	ReplaceProjection(
		context.Context,
		string,
		ProjectionState,
		ProjectionManifest,
		[]Transition,
	) ([]Transition, ProjectionState, error)
}

func validExperimentStatus(status ExperimentStatus) bool {
	switch status {
	case ExperimentStatusDraft, ExperimentStatusActive, ExperimentStatusCompleted, ExperimentStatusCancelled:
		return true
	default:
		return false
	}
}

func validRunStatus(status RunStatus) bool {
	switch status {
	case RunStatusPending, RunStatusRunning, RunStatusCompleted, RunStatusFailed, RunStatusCancelled:
		return true
	default:
		return false
	}
}

func normalizeJSONObject(raw json.RawMessage, limit int, allowEmpty bool) (json.RawMessage, error) {
	if len(raw) == 0 && allowEmpty {
		raw = json.RawMessage(`{}`)
	}
	if len(raw) == 0 || len(raw) > limit || !json.Valid(raw) {
		return nil, errors.New("invalid or oversized JSON")
	}
	var object map[string]any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&object); err != nil || object == nil {
		return nil, errors.New("JSON value must be an object")
	}
	normalized, err := json.Marshal(object)
	if err != nil || len(normalized) > limit {
		return nil, errors.New("invalid or oversized JSON")
	}
	return normalized, nil
}

func rejectEmbeddedLargeObjects(raw json.RawMessage) error {
	var value map[string]any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return fmt.Errorf("%w: transition payload", ErrInvalid)
	}
	return rejectForbiddenKeys(value)
}

func rejectForbiddenKeys(value any) error {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			normalizedKey := strings.ToLower(strings.TrimSpace(key))
			if _, forbidden := forbiddenTransitionKeys[normalizedKey]; forbidden {
				return fmt.Errorf(
					"%w: transition payload must reference, not embed, %s",
					ErrInvalid, normalizedKey,
				)
			}
			if err := rejectForbiddenKeys(child); err != nil {
				return err
			}
		}
	case []any:
		for _, child := range typed {
			if err := rejectForbiddenKeys(child); err != nil {
				return err
			}
		}
	}
	return nil
}

func utc(value time.Time) time.Time {
	if value.IsZero() {
		return value
	}
	return value.UTC()
}

func utcPtr(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	normalized := value.UTC()
	return &normalized
}
