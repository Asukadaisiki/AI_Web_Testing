package execution

import (
	"encoding/json"
	"errors"
	"fmt"
)

const FailureSignalV2 = "failure.signal.v2"

type FailureSourceReference struct {
	Type        string `json:"type"`
	ExecutionID int64  `json:"execution_id"`
	StepIndex   *int   `json:"step_index"`
	JSONPointer string `json:"json_pointer"`
}

type AgentEventReference struct {
	RunID string `json:"run_id"`
	Seq   int64  `json:"seq"`
}

type FailureSignal struct {
	SchemaVersion       string                  `json:"schema_version"`
	Category            string                  `json:"category"`
	Fingerprint         string                  `json:"fingerprint"`
	Title               string                  `json:"title"`
	Stage               string                  `json:"stage"`
	Code                string                  `json:"code"`
	Retryable           *bool                   `json:"retryable"`
	SideEffectCommitted *bool                   `json:"side_effect_committed"`
	SourceReference     *FailureSourceReference `json:"source_reference"`
	AgentEventReference *AgentEventReference    `json:"agent_event_reference"`
	StepIndex           *int                    `json:"step_index"`
	Action              *string                 `json:"action"`
	Target              *string                 `json:"target"`
	ErrorMessage        *string                 `json:"error_message"`
}

func DecodeFailureSignal(raw json.RawMessage) (FailureSignal, error) {
	var signal FailureSignal
	if err := json.Unmarshal(raw, &signal); err != nil {
		return FailureSignal{}, fmt.Errorf("decode failure signal: %w", err)
	}
	if !validFailureCategory(signal.Category) {
		return FailureSignal{}, fmt.Errorf("invalid failure category %q", signal.Category)
	}
	if signal.Fingerprint == "" || signal.Title == "" {
		return FailureSignal{}, errors.New("failure signal requires fingerprint and title")
	}
	if signal.SchemaVersion == "" {
		return signal, nil
	}
	if signal.SchemaVersion != FailureSignalV2 {
		return FailureSignal{}, fmt.Errorf("unsupported failure signal schema %q", signal.SchemaVersion)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return FailureSignal{}, fmt.Errorf("decode failure signal fields: %w", err)
	}
	if !validFailureStage(signal.Stage) || signal.Code == "" || signal.Retryable == nil ||
		signal.SourceReference == nil {
		return FailureSignal{}, errors.New("failure.signal.v2 is missing required fields")
	}
	if _, exists := fields["side_effect_committed"]; !exists {
		return FailureSignal{}, errors.New("failure.signal.v2 is missing side_effect_committed")
	}
	if (signal.SourceReference.Type != "execution_report" &&
		signal.SourceReference.Type != "execution_error") ||
		signal.SourceReference.ExecutionID < 1 ||
		signal.SourceReference.JSONPointer == "" {
		return FailureSignal{}, errors.New("failure.signal.v2 has invalid source_reference")
	}
	if signal.AgentEventReference != nil &&
		(signal.AgentEventReference.RunID == "" || signal.AgentEventReference.Seq < 1) {
		return FailureSignal{}, errors.New("failure.signal.v2 has invalid agent_event_reference")
	}
	return signal, nil
}

func DecodeFailureSignalValue(value any) (FailureSignal, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return FailureSignal{}, fmt.Errorf("encode failure signal: %w", err)
	}
	return DecodeFailureSignal(raw)
}

func (s FailureSignal) AllowsOriginalActionReplay() bool {
	return s.SchemaVersion == FailureSignalV2 &&
		s.SideEffectCommitted != nil &&
		!*s.SideEffectCommitted
}

func validFailureCategory(category string) bool {
	switch category {
	case "configuration", "locator", "assertion", "navigation", "network", "runner":
		return true
	default:
		return false
	}
}

func validFailureStage(stage string) bool {
	switch stage {
	case "configuration", "precondition", "locator", "action", "postcondition", "network", "runner":
		return true
	default:
		return false
	}
}
