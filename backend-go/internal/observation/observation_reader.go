package observation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"strings"

	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/agent"
	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/agentservice"
	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/browsercontract"
)

type observationEventReader interface {
	GetEvent(context.Context, string, int64) (agentservice.Event, error)
}

// ObservationReader resolves opaque candidate references against persisted
// exploration tool results. It is read-only: it never mutates any plan state.
type ObservationReader struct {
	events observationEventReader
}

func NewObservationReader(events observationEventReader) *ObservationReader {
	return &ObservationReader{events: events}
}

// ResolveCandidate looks up a trusted resolved candidate for the opaque
// candidate reference produced by an earlier exploration tool result.
func (r *ObservationReader) ResolveCandidate(
	ctx context.Context,
	runID string,
	action string,
	ref browsercontract.CandidateRef,
) (browsercontract.TrustedResolvedCandidate, error) {
	if err := ref.Validate(); err != nil {
		return browsercontract.TrustedResolvedCandidate{}, err
	}
	if !validObservationAction(action) {
		return browsercontract.TrustedResolvedCandidate{}, errors.New(
			"candidate action is invalid",
		)
	}
	observations, err := r.readObservations(
		ctx,
		runID,
		ref.SourceEventSeq,
	)
	if err != nil {
		return browsercontract.TrustedResolvedCandidate{}, err
	}
	var found *resolvedObservationCandidate
	observationCount := 0
	for _, observation := range observations {
		if observation.ProbeID != ref.ProbeID ||
			observation.ObservationID != ref.ObservationID {
			continue
		}
		observationCount++
		for _, element := range observation.Elements {
			for _, candidate := range element.Locators {
				if candidate.CandidateID != ref.CandidateID {
					continue
				}
				if found != nil {
					return browsercontract.TrustedResolvedCandidate{}, errors.New(
						"candidate_id is not unique in the source observation",
					)
				}
				found = &resolvedObservationCandidate{
					observation: observation,
					element:     element,
					candidate:   candidate,
				}
			}
		}
	}
	if observationCount != 1 {
		return browsercontract.TrustedResolvedCandidate{}, errors.New(
			"candidate reference does not identify exactly one observation",
		)
	}
	if found == nil {
		return browsercontract.TrustedResolvedCandidate{}, errors.New(
			"candidate reference was not found",
		)
	}
	if !sourceActionable(found.element, found.candidate, action) {
		return browsercontract.TrustedResolvedCandidate{}, errors.New(
			"candidate is not source-actionable",
		)
	}
	if err := found.candidate.Locator.Validate(); err != nil {
		return browsercontract.TrustedResolvedCandidate{}, fmt.Errorf(
			"candidate locator: %w",
			err,
		)
	}
	if len(found.element.ContextPath.Frames) > 0 {
		return browsercontract.TrustedResolvedCandidate{}, errors.New(
			"candidate frame context is unsupported",
		)
	}
	return browsercontract.TrustedResolvedCandidate{
		Source:          ref,
		PageStateID:     found.observation.PageState.StateID,
		PageStateSHA256: found.observation.PageState.SHA256,
		ElementRef:      found.element.ElementRef,
		Locator:         found.candidate.Locator,
		ContextPath:     found.element.ContextPath,
		Provenance:      found.candidate.Provenance,
		Metadata: browsercontract.CandidateElementMetadata{
			Role:        found.element.A11y.Role,
			Name:        found.element.A11y.Name,
			Description: found.element.A11y.Description,
			Value:       found.element.A11y.Value,
			DOMTag:      found.element.DOM.Tag,
			DOMText:     found.element.DOM.Text,
			DOMAttrs:    maps.Clone(found.element.DOM.Attrs),
		},
	}, nil
}

func (r *ObservationReader) readObservations(
	ctx context.Context,
	runID string,
	sourceEventSeq int64,
) ([]persistedObservation, error) {
	if r == nil || r.events == nil {
		return nil, errors.New("observation event reader is unavailable")
	}
	event, err := r.events.GetEvent(ctx, runID, sourceEventSeq)
	if err != nil {
		return nil, err
	}
	if event.RunID != runID || event.Seq != sourceEventSeq {
		return nil, errors.New("source event does not belong to the current run")
	}
	if event.Type != agentservice.EventToolResult {
		return nil, errors.New("source event is not a tool.result")
	}
	encoded, err := json.Marshal(event.Payload)
	if err != nil {
		return nil, fmt.Errorf("encode persisted tool result: %w", err)
	}
	var payload agent.ToolResultEventPayload
	if err := json.Unmarshal(encoded, &payload); err != nil {
		return nil, fmt.Errorf("decode persisted tool result: %w", err)
	}
	if payload.SchemaVersion != agent.ToolResultSchemaV1 {
		return nil, errors.New("source event has an unsupported tool result schema")
	}
	if !agent.IsExplorationTool(payload.Tool) {
		return nil, errors.New("source event is not an exploration result")
	}
	var content persistedExplorationResult
	if err := json.Unmarshal(payload.Content, &content); err != nil {
		return nil, fmt.Errorf("decode persisted exploration content: %w", err)
	}
	if strings.TrimSpace(content.ProbeID) == "" {
		return nil, errors.New("source exploration probe_id is required")
	}
	observations := make([]persistedObservation, 0, len(content.Pages)+1)
	if payload.Tool == "explore_page" {
		if !hasObservation(content.Observation) {
			return nil, errors.New("explore_page source has no observation")
		}
		observation, err := decodePersistedObservation(content.Observation)
		if err != nil {
			return nil, err
		}
		observations = append(observations, observation)
	} else {
		for index, page := range content.Pages {
			if hasObservation(page.Observation) {
				observation, err := decodePersistedObservation(page.Observation)
				if err != nil {
					return nil, fmt.Errorf(
						"decode source page %d observation: %w",
						index,
						err,
					)
				}
				observations = append(observations, observation)
			}
		}
	}
	if len(observations) == 0 {
		return nil, errors.New("source exploration has no observations")
	}
	seenObservations := make(map[string]bool, len(observations))
	for _, observation := range observations {
		if err := validatePersistedObservation(
			content.ProbeID,
			observation,
		); err != nil {
			return nil, err
		}
		key := observation.ProbeID + "\x00" + observation.ObservationID
		if seenObservations[key] {
			return nil, errors.New("source contains duplicate observations")
		}
		seenObservations[key] = true
	}
	return observations, nil
}

func hasObservation(raw json.RawMessage) bool {
	value := strings.TrimSpace(string(raw))
	return value != "" && value != "null"
}

func decodePersistedObservation(
	raw json.RawMessage,
) (persistedObservation, error) {
	if _, err := browsercontract.DecodeObservation(raw); err != nil {
		return persistedObservation{}, fmt.Errorf(
			"validate persisted observation: %w",
			err,
		)
	}
	var observation persistedObservation
	if err := json.Unmarshal(raw, &observation); err != nil {
		return persistedObservation{}, fmt.Errorf(
			"decode persisted observation: %w",
			err,
		)
	}
	return observation, nil
}

func validObservationAction(action string) bool {
	return action == "click" || action == "input" || action == "wait_for"
}

func validatePersistedObservation(
	probeID string,
	observation persistedObservation,
) error {
	if observation.SchemaVersion != browsercontract.ObservationSchemaVersion ||
		observation.ProbeID != probeID ||
		strings.TrimSpace(observation.ObservationID) == "" ||
		strings.TrimSpace(observation.PageState.StateID) == "" ||
		len(observation.PageState.SHA256) != 64 {
		return errors.New("source observation is incomplete or mismatched")
	}
	seenCandidates := make(map[string]bool)
	for _, element := range observation.Elements {
		for _, candidate := range element.Locators {
			if strings.TrimSpace(candidate.CandidateID) == "" {
				return errors.New("source observation contains an empty candidate_id")
			}
			if seenCandidates[candidate.CandidateID] {
				return errors.New("source observation contains a duplicate candidate_id")
			}
			seenCandidates[candidate.CandidateID] = true
		}
	}
	return nil
}

func sourceActionable(
	element persistedElement,
	candidate persistedCandidate,
	action string,
) bool {
	if candidate.ObservedCount != 1 ||
		!element.Runtime.Connected ||
		!element.Runtime.Visible {
		return false
	}
	if (action == "click" || action == "input") && !element.Runtime.Enabled {
		return false
	}
	return action != "input" || element.Runtime.Editable
}

type persistedExplorationResult struct {
	ProbeID     string          `json:"probe_id"`
	Observation json.RawMessage `json:"observation_v2"`
	Pages       []struct {
		Observation json.RawMessage `json:"observation_v2"`
	} `json:"pages"`
}

type persistedObservation struct {
	SchemaVersion string `json:"schema_version"`
	ProbeID       string `json:"probe_id"`
	ObservationID string `json:"observation_id"`
	PageState     struct {
		StateID string `json:"state_id"`
		SHA256  string `json:"state_sha256"`
	} `json:"page_state"`
	Elements []persistedElement `json:"elements"`
}

type persistedElement struct {
	ElementRef  string                      `json:"element_ref"`
	ContextPath browsercontract.ContextPath `json:"context_path"`
	A11y        struct {
		Role        string `json:"role"`
		Name        string `json:"name"`
		Description string `json:"description"`
		Value       any    `json:"value"`
	} `json:"a11y"`
	DOM struct {
		Tag   string            `json:"tag"`
		Attrs map[string]string `json:"attrs"`
		Text  string            `json:"text"`
	} `json:"dom"`
	Runtime struct {
		Connected bool `json:"connected"`
		Visible   bool `json:"visible"`
		Enabled   bool `json:"enabled"`
		Editable  bool `json:"editable"`
	} `json:"runtime"`
	Locators []persistedCandidate `json:"locators"`
}

type persistedCandidate struct {
	CandidateID   string                      `json:"candidate_id"`
	Locator       browsercontract.LocatorSpec `json:"locator"`
	Provenance    string                      `json:"provenance"`
	ObservedCount int                         `json:"observed_count"`
}

type resolvedObservationCandidate struct {
	observation persistedObservation
	element     persistedElement
	candidate   persistedCandidate
}
