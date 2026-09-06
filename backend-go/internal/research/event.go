package research

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"unicode/utf8"
)

const (
	EventSchemaVersion      = "research.event.v1"
	TransitionSchemaVersion = "research.transition.v1"
	ExportSchemaVersion     = "research.trajectory.jsonl.v1"
	ProjectionManifestV1    = "research.projection_manifest.v1"
	MaxResearchEventBytes   = 128 * 1024
)

type EventKind string

const (
	EventKindObservation  EventKind = "observation"
	EventKindDecision     EventKind = "decision"
	EventKindAction       EventKind = "action"
	EventKindExecution    EventKind = "execution"
	EventKindVerification EventKind = "verification"
	EventKindFailure      EventKind = "failure"
	EventKindRecovery     EventKind = "recovery"
	EventKindReward       EventKind = "reward"
	EventKindUnknown      EventKind = "unknown"
)

func (k EventKind) Valid() bool {
	switch k {
	case EventKindObservation, EventKindDecision, EventKindAction,
		EventKindExecution, EventKindVerification, EventKindFailure,
		EventKindRecovery, EventKindReward, EventKindUnknown:
		return true
	default:
		return false
	}
}

type SlotStatus string

const (
	SlotAvailable     SlotStatus = "available"
	SlotUnavailable   SlotStatus = "unavailable"
	SlotNotApplicable SlotStatus = "not_applicable"
)

type Slot[T any] struct {
	Status SlotStatus `json:"status"`
	Value  *T         `json:"value,omitempty"`
	Reason string     `json:"reason,omitempty"`
}

func Available[T any](value T) Slot[T] {
	return Slot[T]{Status: SlotAvailable, Value: &value}
}

func Unavailable[T any](reason string) Slot[T] {
	return Slot[T]{Status: SlotUnavailable, Reason: strings.TrimSpace(reason)}
}

func NotApplicable[T any](reason string) Slot[T] {
	return Slot[T]{Status: SlotNotApplicable, Reason: strings.TrimSpace(reason)}
}

func (s Slot[T]) Validate(name string) error {
	switch s.Status {
	case SlotAvailable:
		if s.Value == nil || s.Reason != "" {
			return fmt.Errorf("%w: %s available slot", ErrInvalid, name)
		}
	case SlotUnavailable, SlotNotApplicable:
		if s.Value != nil || s.Reason == "" || len(s.Reason) > 200 {
			return fmt.Errorf("%w: %s unavailable slot", ErrInvalid, name)
		}
	default:
		return fmt.Errorf("%w: %s slot status %q", ErrInvalid, name, s.Status)
	}
	return nil
}

type SourceKind string

const (
	SourceAgentEvent SourceKind = "agent_event"
	SourceGeneration SourceKind = "dsl_generation"
	SourceBatch      SourceKind = "execution_batch"
	SourceJob        SourceKind = "execution_job"
	SourceExecution  SourceKind = "execution"
	SourceReport     SourceKind = "execution_report"
	SourceOracle     SourceKind = "independent_oracle"
)

type SourceRef struct {
	Kind          SourceKind   `json:"kind"`
	ID            string       `json:"id"`
	Sequence      Slot[int64]  `json:"sequence"`
	ContentSHA256 string       `json:"content_sha256"`
	SchemaVersion Slot[string] `json:"schema_version"`
}

func (r *SourceRef) NormalizeAndValidate() error {
	r.ID = strings.TrimSpace(r.ID)
	r.ContentSHA256 = strings.ToLower(strings.TrimSpace(r.ContentSHA256))
	if !validSourceKind(r.Kind) || r.ID == "" || len(r.ID) > 200 ||
		!sha256Pattern.MatchString(r.ContentSHA256) {
		return fmt.Errorf("%w: source reference", ErrInvalid)
	}
	if err := r.Sequence.Validate("source.sequence"); err != nil {
		return err
	}
	if r.Sequence.Status == SlotAvailable && *r.Sequence.Value < 0 {
		return fmt.Errorf("%w: source sequence", ErrInvalid)
	}
	if err := r.SchemaVersion.Validate("source.schema_version"); err != nil {
		return err
	}
	if r.SchemaVersion.Status == SlotAvailable {
		value := strings.TrimSpace(*r.SchemaVersion.Value)
		if value == "" || len(value) > 100 {
			return fmt.Errorf("%w: source schema_version", ErrInvalid)
		}
		r.SchemaVersion.Value = &value
	}
	return nil
}

type SourceCursor struct {
	SchemaVersion         string  `json:"schema_version"`
	AgentRunID            string  `json:"agent_run_id"`
	AgentEventSeq         int64   `json:"agent_event_seq"`
	ApprovedGenerationIDs []int64 `json:"approved_generation_ids"`
	BatchIDs              []int64 `json:"batch_ids"`
	ExecutionIDs          []int64 `json:"execution_ids"`
}

func (c *SourceCursor) NormalizeAndValidate() error {
	c.SchemaVersion = strings.TrimSpace(c.SchemaVersion)
	c.AgentRunID = strings.TrimSpace(c.AgentRunID)
	if c.SchemaVersion != EventSchemaVersion || c.AgentRunID == "" ||
		len(c.AgentRunID) > 64 || c.AgentEventSeq < 0 {
		return fmt.Errorf("%w: source cursor", ErrInvalid)
	}
	for name, values := range map[string]*[]int64{
		"approved_generation_ids": &c.ApprovedGenerationIDs,
		"batch_ids":               &c.BatchIDs,
		"execution_ids":           &c.ExecutionIDs,
	} {
		normalized, err := sortedPositiveUnique(*values)
		if err != nil {
			return fmt.Errorf("%w: source cursor %s", ErrInvalid, name)
		}
		*values = normalized
	}
	return nil
}

type ResearchEvent struct {
	SchemaVersion string          `json:"schema_version"`
	ID            string          `json:"id"`
	ContentSHA256 string          `json:"content_sha256"`
	Kind          EventKind       `json:"kind"`
	ResearchRunID string          `json:"research_run_id"`
	CorrelationID Slot[string]    `json:"correlation_id"`
	CausationID   Slot[string]    `json:"causation_id"`
	ToolCallIDs   []string        `json:"tool_call_ids"`
	Attempt       Slot[int64]     `json:"attempt"`
	StepIndex     Slot[int64]     `json:"step_index"`
	Sources       []SourceRef     `json:"sources"`
	Data          json.RawMessage `json:"data"`
}

func NewResearchEvent(event ResearchEvent) (ResearchEvent, error) {
	event.SchemaVersion = EventSchemaVersion
	event.ID = ""
	event.ContentSHA256 = ""
	if err := event.normalize(false); err != nil {
		return ResearchEvent{}, err
	}
	hash, err := researchEventHash(event)
	if err != nil {
		return ResearchEvent{}, err
	}
	event.ContentSHA256 = hash
	event.ID = "rev_" + hash
	return event, nil
}

func (e *ResearchEvent) NormalizeAndValidate() error {
	if err := e.normalize(true); err != nil {
		return err
	}
	hash, err := researchEventHash(*e)
	if err != nil {
		return err
	}
	if e.ContentSHA256 != hash || e.ID != "rev_"+hash {
		return fmt.Errorf("%w: research event identity", ErrInvalid)
	}
	return nil
}

func (e *ResearchEvent) normalize(requireIdentity bool) error {
	e.SchemaVersion = strings.TrimSpace(e.SchemaVersion)
	e.ID = strings.TrimSpace(e.ID)
	e.ContentSHA256 = strings.ToLower(strings.TrimSpace(e.ContentSHA256))
	e.ResearchRunID = strings.TrimSpace(e.ResearchRunID)
	if e.SchemaVersion != EventSchemaVersion || !e.Kind.Valid() ||
		e.ResearchRunID == "" || len(e.ResearchRunID) > 64 {
		return fmt.Errorf("%w: research event envelope", ErrInvalid)
	}
	if requireIdentity && (e.ID == "" || !sha256Pattern.MatchString(e.ContentSHA256)) {
		return fmt.Errorf("%w: research event identity", ErrInvalid)
	}
	for name, slot := range map[string]Slot[string]{
		"correlation_id": e.CorrelationID,
		"causation_id":   e.CausationID,
	} {
		if err := slot.Validate(name); err != nil {
			return err
		}
		if slot.Status == SlotAvailable &&
			(strings.TrimSpace(*slot.Value) == "" || len(*slot.Value) > 200) {
			return fmt.Errorf("%w: research event %s", ErrInvalid, name)
		}
	}
	if err := e.Attempt.Validate("attempt"); err != nil {
		return err
	}
	if e.Attempt.Status == SlotAvailable && *e.Attempt.Value < 1 {
		return fmt.Errorf("%w: research event attempt", ErrInvalid)
	}
	if err := e.StepIndex.Validate("step_index"); err != nil {
		return err
	}
	if e.StepIndex.Status == SlotAvailable && *e.StepIndex.Value < 0 {
		return fmt.Errorf("%w: research event step_index", ErrInvalid)
	}
	e.ToolCallIDs = sortedUniqueStrings(e.ToolCallIDs)
	for _, id := range e.ToolCallIDs {
		if id == "" || len(id) > 200 {
			return fmt.Errorf("%w: research event tool_call_ids", ErrInvalid)
		}
	}
	for index := range e.Sources {
		if err := e.Sources[index].NormalizeAndValidate(); err != nil {
			return err
		}
	}
	if e.Sources == nil {
		e.Sources = []SourceRef{}
	}
	slices.SortFunc(e.Sources, compareSourceRefs)
	data, err := normalizeJSONObject(e.Data, MaxResearchEventBytes, true)
	if err != nil {
		return fmt.Errorf("%w: research event data: %v", ErrInvalid, err)
	}
	e.Data = data
	return nil
}

type ProjectionManifest struct {
	SchemaVersion    string       `json:"schema_version"`
	ProjectorVersion string       `json:"projector_version"`
	SourceCursor     SourceCursor `json:"source_cursor"`
	SourceSHA256     string       `json:"source_sha256"`
	TransitionCount  int64        `json:"transition_count"`
	ManifestSHA256   string       `json:"manifest_sha256"`
}

func NewProjectionManifest(cursor SourceCursor, sourceSHA256 string, count int64) (ProjectionManifest, error) {
	manifest := ProjectionManifest{
		SchemaVersion:    ProjectionManifestV1,
		ProjectorVersion: ProjectorVersion,
		SourceCursor:     cursor,
		SourceSHA256:     strings.ToLower(strings.TrimSpace(sourceSHA256)),
		TransitionCount:  count,
	}
	if err := manifest.normalize(false); err != nil {
		return ProjectionManifest{}, err
	}
	hash, err := projectionManifestHash(manifest)
	if err != nil {
		return ProjectionManifest{}, err
	}
	manifest.ManifestSHA256 = hash
	return manifest, nil
}

func (m *ProjectionManifest) NormalizeAndValidate() error {
	if err := m.normalize(true); err != nil {
		return err
	}
	hash, err := projectionManifestHash(*m)
	if err != nil {
		return err
	}
	if hash != m.ManifestSHA256 {
		return fmt.Errorf("%w: projection manifest hash", ErrInvalid)
	}
	return nil
}

func (m *ProjectionManifest) normalize(requireHash bool) error {
	m.SchemaVersion = strings.TrimSpace(m.SchemaVersion)
	m.ProjectorVersion = strings.TrimSpace(m.ProjectorVersion)
	m.SourceSHA256 = strings.ToLower(strings.TrimSpace(m.SourceSHA256))
	m.ManifestSHA256 = strings.ToLower(strings.TrimSpace(m.ManifestSHA256))
	if m.SchemaVersion != ProjectionManifestV1 ||
		m.ProjectorVersion != ProjectorVersion ||
		!sha256Pattern.MatchString(m.SourceSHA256) ||
		m.TransitionCount < 0 {
		return fmt.Errorf("%w: projection manifest", ErrInvalid)
	}
	if requireHash && !sha256Pattern.MatchString(m.ManifestSHA256) {
		return fmt.Errorf("%w: projection manifest hash", ErrInvalid)
	}
	return m.SourceCursor.NormalizeAndValidate()
}

func researchEventHash(event ResearchEvent) (string, error) {
	event.ID = ""
	event.ContentSHA256 = ""
	return CanonicalSHA256(struct {
		SchemaVersion string          `json:"schema_version"`
		Kind          EventKind       `json:"kind"`
		ResearchRunID string          `json:"research_run_id"`
		CorrelationID Slot[string]    `json:"correlation_id"`
		CausationID   Slot[string]    `json:"causation_id"`
		ToolCallIDs   []string        `json:"tool_call_ids"`
		Attempt       Slot[int64]     `json:"attempt"`
		StepIndex     Slot[int64]     `json:"step_index"`
		Sources       []SourceRef     `json:"sources"`
		Data          json.RawMessage `json:"data"`
	}{
		SchemaVersion: event.SchemaVersion,
		Kind:          event.Kind, ResearchRunID: event.ResearchRunID,
		CorrelationID: event.CorrelationID, CausationID: event.CausationID,
		ToolCallIDs: event.ToolCallIDs, Attempt: event.Attempt,
		StepIndex: event.StepIndex, Sources: event.Sources, Data: event.Data,
	})
}

func projectionManifestHash(manifest ProjectionManifest) (string, error) {
	manifest.ManifestSHA256 = ""
	return CanonicalSHA256(struct {
		SchemaVersion    string       `json:"schema_version"`
		ProjectorVersion string       `json:"projector_version"`
		SourceCursor     SourceCursor `json:"source_cursor"`
		SourceSHA256     string       `json:"source_sha256"`
		TransitionCount  int64        `json:"transition_count"`
	}{
		SchemaVersion: manifest.SchemaVersion, ProjectorVersion: manifest.ProjectorVersion,
		SourceCursor: manifest.SourceCursor, SourceSHA256: manifest.SourceSHA256,
		TransitionCount: manifest.TransitionCount,
	})
}

func CanonicalSHA256(value any) (string, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("encode canonical JSON: %w", err)
	}
	canonical, err := CanonicalJSON(raw)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:]), nil
}

func CanonicalJSON(raw []byte) ([]byte, error) {
	if !utf8.Valid(raw) {
		return nil, fmt.Errorf("%w: JSON is not UTF-8", ErrInvalid)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, fmt.Errorf("%w: invalid JSON: %v", ErrInvalid, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("%w: trailing JSON", ErrInvalid)
	}
	canonical, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("%w: canonical JSON: %v", ErrInvalid, err)
	}
	return canonical, nil
}

func validSourceKind(kind SourceKind) bool {
	switch kind {
	case SourceAgentEvent, SourceGeneration, SourceBatch, SourceJob,
		SourceExecution, SourceReport, SourceOracle:
		return true
	default:
		return false
	}
}

func sortedPositiveUnique(values []int64) ([]int64, error) {
	result := append([]int64(nil), values...)
	slices.Sort(result)
	result = slices.Compact(result)
	for _, value := range result {
		if value <= 0 {
			return nil, errors.New("identifier must be positive")
		}
	}
	if result == nil {
		result = []int64{}
	}
	return result, nil
}

func sortedUniqueStrings(values []string) []string {
	result := append([]string(nil), values...)
	for index := range result {
		result[index] = strings.TrimSpace(result[index])
	}
	slices.Sort(result)
	result = slices.Compact(result)
	if result == nil {
		result = []string{}
	}
	return result
}

func compareSourceRefs(left, right SourceRef) int {
	if value := strings.Compare(string(left.Kind), string(right.Kind)); value != 0 {
		return value
	}
	if value := strings.Compare(left.ID, right.ID); value != 0 {
		return value
	}
	leftSeq, rightSeq := int64(-1), int64(-1)
	if left.Sequence.Value != nil {
		leftSeq = *left.Sequence.Value
	}
	if right.Sequence.Value != nil {
		rightSeq = *right.Sequence.Value
	}
	if leftSeq < rightSeq {
		return -1
	}
	if leftSeq > rightSeq {
		return 1
	}
	return 0
}
