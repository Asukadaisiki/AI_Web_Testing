package research

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestExperimentValidationNormalizesUTCAndJSON(t *testing.T) {
	experiment := validExperiment()
	experiment.CreatedAt = time.Date(2026, 9, 6, 12, 0, 0, 0, time.FixedZone("test", 8*60*60))
	if err := experiment.NormalizeAndValidate(); err != nil {
		t.Fatalf("NormalizeAndValidate() error = %v", err)
	}
	if experiment.CreatedAt.Location() != time.UTC {
		t.Fatalf("CreatedAt location = %v, want UTC", experiment.CreatedAt.Location())
	}
	if string(experiment.ConfigJSON) != `{}` {
		t.Fatalf("ConfigJSON = %s, want {}", experiment.ConfigJSON)
	}

	experiment.CodeSHA256 = strings.Repeat("z", 64)
	if err := experiment.NormalizeAndValidate(); !errors.Is(err, ErrInvalid) {
		t.Fatalf("invalid SHA error = %v, want ErrInvalid", err)
	}
}

func TestRunStatusTransitionsAreTerminalAndOneWay(t *testing.T) {
	tests := []struct {
		from RunStatus
		to   RunStatus
		want bool
	}{
		{RunStatusPending, RunStatusRunning, true},
		{RunStatusPending, RunStatusCompleted, false},
		{RunStatusRunning, RunStatusCompleted, true},
		{RunStatusCompleted, RunStatusRunning, false},
		{RunStatusFailed, RunStatusFailed, true},
	}
	for _, test := range tests {
		if got := test.from.CanTransition(test.to); got != test.want {
			t.Errorf("%s.CanTransition(%s) = %v, want %v", test.from, test.to, got, test.want)
		}
	}
}

func TestResearchRunRejectsUnsupportedVersions(t *testing.T) {
	run := validRun()
	run.Versions.SchemaVersion = "research.persistence.v2"
	if err := run.NormalizeAndValidate(); !errors.Is(err, ErrInvalid) {
		t.Fatalf("unsupported schema version error = %v, want ErrInvalid", err)
	}

	experiment := validExperiment()
	experiment.PolicyVersion = "research.policy.v2"
	if err := experiment.NormalizeAndValidate(); !errors.Is(err, ErrInvalid) {
		t.Fatalf("unsupported policy version error = %v, want ErrInvalid", err)
	}

	metrics := unavailableMetrics()
	metrics.SchemaVersion = "research.metrics.v2"
	if err := metrics.Validate(); !errors.Is(err, ErrInvalid) {
		t.Fatalf("unsupported metrics version error = %v, want ErrInvalid", err)
	}

	if _, err := TransitionContentSHA256(
		"research.persistence.v2",
		json.RawMessage(`{"state_summary":"start"}`),
		nil,
	); !errors.Is(err, ErrInvalid) {
		t.Fatalf("unsupported transition version error = %v, want ErrInvalid", err)
	}
}

func TestCancelledPendingRunAllowsMissingStartedAt(t *testing.T) {
	run := validRun()
	finishedAt := time.Now()
	run.Status = RunStatusCancelled
	run.FinishedAt = &finishedAt
	if err := run.NormalizeAndValidate(); err != nil {
		t.Fatalf("cancelled pending run validation error = %v", err)
	}
}

func TestRunLinksRequireOrderedPrefix(t *testing.T) {
	generationID := int64(1)
	links := RunLinks{GenerationID: &generationID}
	if err := links.NormalizeAndValidate(); !errors.Is(err, ErrBrokenLink) {
		t.Fatalf("generation without agent run error = %v, want ErrBrokenLink", err)
	}
}

func TestRunMetricsNeverUseSyntheticZeroForUnavailable(t *testing.T) {
	metrics := unavailableMetrics()
	if err := metrics.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	zero := int64(0)
	reason := "not observed"
	metrics.TotalTokens = NullableValue[int64]{
		Value:             &zero,
		UnavailableReason: &reason,
	}
	if err := metrics.Validate(); !errors.Is(err, ErrInvalid) {
		t.Fatalf("ambiguous zero metric error = %v, want ErrInvalid", err)
	}

	input, output, total := int64(10), int64(5), int64(14)
	metrics = unavailableMetrics()
	metrics.InputTokens = NullableValue[int64]{Value: &input}
	metrics.OutputTokens = NullableValue[int64]{Value: &output}
	metrics.TotalTokens = NullableValue[int64]{Value: &total}
	if err := metrics.Validate(); !errors.Is(err, ErrInvalid) {
		t.Fatalf("inconsistent total tokens error = %v, want ErrInvalid", err)
	}
}

func TestTransitionValidationBindsContentHash(t *testing.T) {
	transition := Transition{
		ResearchRunID: "run-1",
		Ordinal:       0,
		AppendKey:     "event-1",
		SchemaVersion: SchemaVersion,
		PayloadJSON:   json.RawMessage(`{"state":{"url":"https://example.com"}}`),
		ArtifactRefs: []ArtifactRef{{
			Kind:      "screenshot",
			URI:       "artifact://execution/1/step/1",
			SHA256:    strings.Repeat("a", 64),
			MediaType: "image/png",
		}},
	}
	hash, err := TransitionContentSHA256(
		transition.SchemaVersion,
		transition.PayloadJSON,
		transition.ArtifactRefs,
	)
	if err != nil {
		t.Fatal(err)
	}
	transition.ContentSHA256 = hash
	if err := transition.NormalizeAndValidate(); err != nil {
		t.Fatalf("NormalizeAndValidate() error = %v", err)
	}
	transition.PayloadJSON = json.RawMessage(`{"state":{"url":"https://changed.example.com"}}`)
	if err := transition.NormalizeAndValidate(); !errors.Is(err, ErrInvalid) {
		t.Fatalf("content mismatch error = %v, want ErrInvalid", err)
	}
}

func TestTransitionArtifactOrderingUsesEveryDTOField(t *testing.T) {
	small, large := int64(1), int64(2)
	first := ArtifactRef{
		Kind: "evidence", URI: "artifact://run/item",
		SHA256: strings.Repeat("a", 64), MediaType: "application/json",
		SchemaVersion: "evidence.v1", SizeBytes: &small,
	}
	second := first
	second.SchemaVersion = "evidence.v2"
	second.SizeBytes = &large

	left, err := TransitionContentSHA256(
		SchemaVersion,
		json.RawMessage(`{"state":{"ready":true}}`),
		[]ArtifactRef{second, first},
	)
	if err != nil {
		t.Fatal(err)
	}
	right, err := TransitionContentSHA256(
		SchemaVersion,
		json.RawMessage(`{"state":{"ready":true}}`),
		[]ArtifactRef{first, second},
	)
	if err != nil {
		t.Fatal(err)
	}
	if left != right {
		t.Fatalf("artifact order changed content hash: %s != %s", left, right)
	}
}

func TestJSONLimitsRejectOversizedPayload(t *testing.T) {
	experiment := validExperiment()
	experiment.ConfigJSON = json.RawMessage(`{"value":"` +
		strings.Repeat("x", MaxControlJSONBytes) + `"}`)
	if err := experiment.NormalizeAndValidate(); !errors.Is(err, ErrInvalid) {
		t.Fatalf("oversized config error = %v, want ErrInvalid", err)
	}
}

func TestTransitionRejectsEmbeddedLargeSourceObjects(t *testing.T) {
	for _, payload := range []string{
		`{"transcript":[{"role":"user","content":"secret"}]}`,
		`{"execution":{"report":{"status":"passed"}}}`,
		`{"observation":{"screenshot_base64":"encoded"}}`,
		`{"action":{"full_dsl":{"steps":[]}}}`,
	} {
		if _, err := TransitionContentSHA256(
			SchemaVersion,
			json.RawMessage(payload),
			nil,
		); !errors.Is(err, ErrInvalid) {
			t.Fatalf("payload %s error = %v, want ErrInvalid", payload, err)
		}
	}
}

func validExperiment() Experiment {
	return Experiment{
		ID:                 "experiment-1",
		ProjectID:          1,
		Name:               "canonical",
		Goal:               "complete the canonical goal",
		DatasetVersion:     "automation-exercise.v1",
		ModelProvider:      "openai-compatible",
		ModelName:          "model",
		ModelVersion:       "model.v1",
		PromptVersion:      "prompt.v1",
		BrowserName:        "chromium",
		BrowserVersion:     "1",
		ViewportJSON:       json.RawMessage(`{"width":1280,"height":720}`),
		CodeSHA256:         strings.Repeat("a", 64),
		PolicyVersion:      PolicyVersion,
		ObservationProfile: "a11y-dom",
		DSLProfile:         "legacy",
		Seed:               42,
		Variant:            "dsl-verification",
		Repetitions:        20,
		Status:             ExperimentStatusDraft,
		ConfigJSON:         nil,
	}
}

func validRun() ResearchRun {
	return ResearchRun{
		ID:              "research-run-1",
		ExperimentID:    "experiment-1",
		ProjectID:       1,
		IdempotencyKey:  "request-1",
		RepetitionIndex: 0,
		Status:          RunStatusPending,
		Versions:        DefaultVersionSnapshot(),
	}
}

func unavailableMetrics() RunMetrics {
	reason := "source fact unavailable"
	boolMetric := func() NullableValue[bool] {
		return NullableValue[bool]{UnavailableReason: &reason}
	}
	floatMetric := func() NullableValue[float64] {
		return NullableValue[float64]{UnavailableReason: &reason}
	}
	intMetric := func() NullableValue[int64] {
		return NullableValue[int64]{UnavailableReason: &reason}
	}
	return RunMetrics{
		SchemaVersion:       MetricVersion,
		TaskSuccess:         boolMetric(),
		GroundingAccuracy:   floatMetric(),
		InvalidActionRate:   floatMetric(),
		ExecutionSuccess:    boolMetric(),
		VerificationSuccess: boolMetric(),
		RecoveryRate:        floatMetric(),
		Steps:               intMetric(),
		Retries:             intMetric(),
		LLMCalls:            intMetric(),
		InputTokens:         intMetric(),
		OutputTokens:        intMetric(),
		TotalTokens:         intMetric(),
		LatencyMS:           intMetric(),
		VisionCalls:         intMetric(),
	}
}
