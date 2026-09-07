package research

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

const MetricSourceSchemaVersion = "research.metric_source.v1"

type MetricSource struct {
	SchemaVersion string               `json:"schema_version"`
	ResearchRunID string               `json:"research_run_id"`
	Snapshot      SourceSnapshot       `json:"-"`
	Transitions   []Transition         `json:"-"`
	FinalRunLinks RunLinks             `json:"final_run_links"`
	Oracle        Slot[OracleDecision] `json:"oracle"`
	SourceSHA256  string               `json:"source_sha256"`
}

type metricTransitionSource struct {
	Ordinal       int64         `json:"ordinal"`
	AppendKey     string        `json:"append_key"`
	ContentSHA256 string        `json:"content_sha256"`
	SchemaVersion string        `json:"schema_version"`
	ArtifactRefs  []ArtifactRef `json:"artifact_refs"`
}

func NewMetricSource(
	snapshot SourceSnapshot,
	transitions []Transition,
	oracle Slot[OracleDecision],
) (MetricSource, error) {
	source := MetricSource{
		SchemaVersion: MetricSourceSchemaVersion,
		ResearchRunID: snapshot.ResearchRunID,
		Snapshot:      snapshot,
		Transitions:   append([]Transition(nil), transitions...),
		FinalRunLinks: snapshot.FinalRunLinks,
		Oracle:        oracle,
	}
	if err := source.normalize(false); err != nil {
		return MetricSource{}, err
	}
	hash, err := metricSourceHash(source)
	if err != nil {
		return MetricSource{}, err
	}
	source.SourceSHA256 = hash
	return source, nil
}

func (s *MetricSource) NormalizeAndValidate() error {
	if err := s.normalize(true); err != nil {
		return err
	}
	hash, err := metricSourceHash(*s)
	if err != nil {
		return err
	}
	if hash != s.SourceSHA256 {
		return fmt.Errorf("%w: metric source hash", ErrSourceChanged)
	}
	return nil
}

func (s *MetricSource) normalize(requireHash bool) error {
	s.SchemaVersion = strings.TrimSpace(s.SchemaVersion)
	s.ResearchRunID = strings.TrimSpace(s.ResearchRunID)
	s.SourceSHA256 = strings.ToLower(strings.TrimSpace(s.SourceSHA256))
	if s.SchemaVersion != MetricSourceSchemaVersion ||
		s.ResearchRunID == "" || s.ResearchRunID != s.Snapshot.ResearchRunID {
		return fmt.Errorf("%w: metric source envelope", ErrInvalid)
	}
	if requireHash && !sha256Pattern.MatchString(s.SourceSHA256) {
		return fmt.Errorf("%w: metric source hash", ErrInvalid)
	}
	if err := validateSourceSnapshot(s.Snapshot); err != nil {
		return err
	}
	if err := validateMetricRunLinks(s.Snapshot, &s.FinalRunLinks); err != nil {
		return err
	}
	if err := s.Oracle.Validate("metric_source.oracle"); err != nil {
		return err
	}
	if s.Oracle.Status != SlotAvailable || s.Oracle.Value == nil {
		return fmt.Errorf("%w: metric oracle is not immutable", ErrSourceChanged)
	}
	if err := s.Oracle.Value.NormalizeAndValidate(); err != nil {
		return err
	}
	if err := validateImmutableMetricInputs(*s); err != nil {
		return err
	}
	expected, _, err := NewProjector().Project(s.Snapshot)
	if err != nil {
		return err
	}
	if len(expected) != len(s.Transitions) {
		return fmt.Errorf("%w: metric transition count", ErrSourceChanged)
	}
	for index := range expected {
		if err := s.Transitions[index].NormalizeAndValidate(); err != nil {
			return err
		}
		if !sameMetricTransition(expected[index], s.Transitions[index]) {
			return fmt.Errorf(
				"%w: metric transition ordinal %d",
				ErrSourceChanged, index,
			)
		}
	}
	return nil
}

func validateImmutableMetricInputs(source MetricSource) error {
	if source.Snapshot.AgentRunStatus != "completed" {
		return fmt.Errorf("%w: agent run is not completed", ErrSourceChanged)
	}
	if !completeRunLinks(source.FinalRunLinks) {
		return fmt.Errorf("%w: metric source requires complete run links", ErrBrokenLink)
	}
	execution := finalMetricExecution(source)
	if execution == nil {
		return fmt.Errorf("%w: final execution is absent", ErrBrokenLink)
	}
	if !isTerminalExecutionStatus(execution.Status) {
		return fmt.Errorf("%w: final execution is not terminal", ErrSourceChanged)
	}
	if execution.Report.Status != SlotAvailable || execution.Report.Value == nil ||
		execution.ReportRef.Status != SlotAvailable || execution.ReportRef.Value == nil {
		return fmt.Errorf("%w: final execution report is not terminal", ErrSourceChanged)
	}
	canonical, err := validateExecutionReport(*execution.Report.Value)
	if err != nil {
		return err
	}
	var report struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(canonical, &report); err != nil ||
		report.Status != execution.Status ||
		!isTerminalExecutionStatus(report.Status) {
		return fmt.Errorf("%w: final execution and report status disagree", ErrSourceChanged)
	}
	expectedRef, err := sourceRef(
		SourceReport,
		strconv.FormatInt(execution.ID, 10),
		Available(execution.Attempt),
		Available(execution.ReportSchemaVersion),
		json.RawMessage(canonical),
	)
	if err != nil {
		return err
	}
	if !sameSourceRef(*execution.ReportRef.Value, expectedRef) {
		return fmt.Errorf("%w: final execution report reference", ErrSourceChanged)
	}
	return nil
}

func completeRunLinks(links RunLinks) bool {
	return links.AgentRunID != nil &&
		links.GenerationID != nil &&
		links.BatchID != nil &&
		links.ExecutionID != nil &&
		links.DSLSHA256 != nil
}

func metricSourceHash(source MetricSource) (string, error) {
	transitions := make([]metricTransitionSource, 0, len(source.Transitions))
	for _, transition := range source.Transitions {
		artifacts := append([]ArtifactRef(nil), transition.ArtifactRefs...)
		sortArtifactRefs(artifacts)
		transitions = append(transitions, metricTransitionSource{
			Ordinal: transition.Ordinal, AppendKey: transition.AppendKey,
			ContentSHA256: transition.ContentSHA256,
			SchemaVersion: transition.SchemaVersion,
			ArtifactRefs:  artifacts,
		})
	}
	envelope := struct {
		SchemaVersion        string                   `json:"schema_version"`
		ResearchRunID        string                   `json:"research_run_id"`
		SnapshotSourceSHA256 string                   `json:"snapshot_source_sha256"`
		Transitions          []metricTransitionSource `json:"transitions"`
		FinalRunLinks        RunLinks                 `json:"final_run_links"`
		Oracle               Slot[OracleDecision]     `json:"oracle"`
	}{
		SchemaVersion: source.SchemaVersion, ResearchRunID: source.ResearchRunID,
		SnapshotSourceSHA256: source.Snapshot.SourceSHA256,
		Transitions:          transitions, FinalRunLinks: source.FinalRunLinks,
		Oracle: source.Oracle,
	}
	return CanonicalSHA256(envelope)
}

func validateMetricRunLinks(snapshot SourceSnapshot, links *RunLinks) error {
	if err := links.NormalizeAndValidate(); err != nil {
		return err
	}
	if links.AgentRunID == nil || *links.AgentRunID != snapshot.AgentRunID {
		return fmt.Errorf("%w: metric source agent run link", ErrBrokenLink)
	}

	var linkedGeneration *GenerationSnapshot
	if links.GenerationID != nil {
		for index := range snapshot.Generations {
			if snapshot.Generations[index].ID == *links.GenerationID {
				linkedGeneration = &snapshot.Generations[index]
				break
			}
		}
		if linkedGeneration == nil || links.DSLSHA256 == nil ||
			linkedGeneration.DSLSHA256 != *links.DSLSHA256 {
			return fmt.Errorf("%w: metric source generation link", ErrBrokenLink)
		}
	}

	var linkedBatch *BatchSnapshot
	if links.BatchID != nil {
		for index := range snapshot.Batches {
			if snapshot.Batches[index].ID == *links.BatchID {
				linkedBatch = &snapshot.Batches[index]
				break
			}
		}
		if linkedBatch == nil || linkedGeneration == nil ||
			linkedBatch.GenerationID != linkedGeneration.ID {
			return fmt.Errorf("%w: metric source batch link", ErrBrokenLink)
		}
	}

	if links.ExecutionID != nil {
		if linkedBatch == nil {
			return fmt.Errorf("%w: metric source execution link", ErrBrokenLink)
		}
		for _, job := range linkedBatch.Jobs {
			for _, execution := range job.Executions {
				if execution.ID == *links.ExecutionID &&
					links.DSLSHA256 != nil &&
					execution.DSLSHA256 == *links.DSLSHA256 {
					return nil
				}
			}
		}
		return fmt.Errorf("%w: metric source execution link", ErrBrokenLink)
	}
	return nil
}

func sameMetricTransition(left, right Transition) bool {
	if left.ResearchRunID != right.ResearchRunID ||
		left.Ordinal != right.Ordinal ||
		left.AppendKey != right.AppendKey ||
		left.ContentSHA256 != right.ContentSHA256 ||
		left.SchemaVersion != right.SchemaVersion ||
		!bytes.Equal(left.PayloadJSON, right.PayloadJSON) {
		return false
	}
	leftArtifacts := append([]ArtifactRef(nil), left.ArtifactRefs...)
	rightArtifacts := append([]ArtifactRef(nil), right.ArtifactRefs...)
	sortArtifactRefs(leftArtifacts)
	sortArtifactRefs(rightArtifacts)
	leftRaw, leftErr := json.Marshal(leftArtifacts)
	rightRaw, rightErr := json.Marshal(rightArtifacts)
	return leftErr == nil && rightErr == nil && bytes.Equal(leftRaw, rightRaw)
}
