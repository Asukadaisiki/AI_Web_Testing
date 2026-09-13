package groundingplan

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

const (
	ObservationQueryVersion       = "grounding.observation-query.v1"
	ObservationQueryResultVersion = "grounding.observation-query-result.v1"
	defaultObservationQueryLimit  = 20
	maxObservationQueryLimit      = 100
)

type ObservationQuery struct {
	SchemaVersion  string `json:"schema_version"`
	PlanStepID     string `json:"plan_step_id"`
	SourceEventSeq int64  `json:"source_event_seq"`
	ObservationID  string `json:"observation_id,omitempty"`
	PageStateID    string `json:"page_state_id,omitempty"`
	CandidateID    string `json:"candidate_id,omitempty"`
	Action         string `json:"action"`
	Query          string `json:"query,omitempty"`
	Role           string `json:"role,omitempty"`
	Limit          int    `json:"limit,omitempty"`
}

type ObservationQueryResult struct {
	SchemaVersion  string                  `json:"schema_version"`
	PlanStepID     string                  `json:"plan_step_id"`
	SourceEventSeq int64                   `json:"source_event_seq"`
	Matches        []ObservationQueryMatch `json:"matches"`
	OmittedCount   int                     `json:"omitted_count"`
}

type ObservationQueryMatch struct {
	CandidateRef  browsercontract.CandidateRef `json:"candidate_ref"`
	ElementRef    string                       `json:"element_ref"`
	Role          string                       `json:"role"`
	Name          string                       `json:"name"`
	DOM           ObservationQueryDOM          `json:"dom"`
	Locator       browsercontract.LocatorSpec  `json:"locator"`
	Provenance    string                       `json:"provenance"`
	ObservedCount int                          `json:"observed_count"`
}

type ObservationQueryDOM struct {
	Tag   string            `json:"tag"`
	Attrs map[string]string `json:"attrs"`
}

type observationEventReader interface {
	GetEvent(context.Context, string, int64) (agentservice.Event, error)
}

type ObservationReader struct {
	events observationEventReader
}

func NewObservationReader(events observationEventReader) *ObservationReader {
	return &ObservationReader{events: events}
}

func (r *ObservationReader) Query(
	ctx context.Context,
	runID string,
	query ObservationQuery,
) (ObservationQueryResult, error) {
	if err := validateObservationQuery(query); err != nil {
		return ObservationQueryResult{}, err
	}
	observations, err := r.readObservations(
		ctx,
		runID,
		query.SourceEventSeq,
	)
	if err != nil {
		return ObservationQueryResult{}, err
	}

	matches := make([]ObservationQueryMatch, 0)
	observationMatched := query.ObservationID == ""
	for _, observation := range observations {
		if query.ObservationID != "" &&
			observation.ObservationID != query.ObservationID {
			continue
		}
		observationMatched = true
		if query.PageStateID != "" &&
			observation.PageState.StateID != query.PageStateID {
			continue
		}
		for _, element := range observation.Elements {
			for _, candidate := range element.Locators {
				if !candidateMatches(element, candidate, query) {
					continue
				}
				matches = append(matches, candidateOption(
					query.SourceEventSeq,
					observation,
					element,
					candidate,
				))
			}
		}
	}
	if !observationMatched {
		return ObservationQueryResult{}, errors.New(
			"observation_id does not match the source event",
		)
	}

	limit := query.Limit
	if limit == 0 {
		limit = defaultObservationQueryLimit
	}
	omitted := 0
	if len(matches) > limit {
		omitted = len(matches) - limit
		matches = matches[:limit]
	}
	return ObservationQueryResult{
		SchemaVersion:  ObservationQueryResultVersion,
		PlanStepID:     query.PlanStepID,
		SourceEventSeq: query.SourceEventSeq,
		Matches:        matches,
		OmittedCount:   omitted,
	}, nil
}

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

func validateObservationQuery(query ObservationQuery) error {
	if query.SchemaVersion != ObservationQueryVersion ||
		strings.TrimSpace(query.PlanStepID) == "" ||
		query.SourceEventSeq < 1 ||
		!validObservationAction(query.Action) ||
		query.Limit < 0 ||
		query.Limit > maxObservationQueryLimit {
		return errors.New("observation query is invalid")
	}
	return nil
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

func candidateMatches(
	element persistedElement,
	candidate persistedCandidate,
	query ObservationQuery,
) bool {
	if !sourceActionable(element, candidate, query.Action) ||
		candidate.Locator.Validate() != nil ||
		len(element.ContextPath.Frames) > 0 {
		return false
	}
	if query.CandidateID != "" && candidate.CandidateID != query.CandidateID {
		return false
	}
	if query.Role != "" &&
		!strings.EqualFold(strings.TrimSpace(element.A11y.Role), strings.TrimSpace(query.Role)) {
		return false
	}
	needle := strings.ToLower(strings.TrimSpace(query.Query))
	if needle == "" {
		return true
	}
	values := []string{
		element.ElementRef,
		element.A11y.Role,
		element.A11y.Name,
		element.DOM.Tag,
		element.DOM.Text,
		candidate.CandidateID,
	}
	values = appendLocatorSearchValues(values, candidate.Locator)
	for key, value := range element.DOM.Attrs {
		values = append(values, key, value)
	}
	for _, value := range values {
		if strings.Contains(strings.ToLower(value), needle) {
			return true
		}
	}
	return false
}

func appendLocatorSearchValues(
	values []string,
	locator browsercontract.LocatorSpec,
) []string {
	values = append(values, locator.Kind, locator.Role, locator.Value)
	if locator.Name != nil {
		values = append(values, *locator.Name)
	}
	if locator.Scope != nil {
		values = appendLocatorSearchValues(values, *locator.Scope)
	}
	if locator.Target != nil {
		values = appendLocatorSearchValues(values, *locator.Target)
	}
	return values
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

func candidateOption(
	sourceEventSeq int64,
	observation persistedObservation,
	element persistedElement,
	candidate persistedCandidate,
) ObservationQueryMatch {
	attrs := maps.Clone(element.DOM.Attrs)
	if attrs == nil {
		attrs = map[string]string{}
	}
	return ObservationQueryMatch{
		CandidateRef: browsercontract.CandidateRef{
			SchemaVersion:  browsercontract.CandidateRefVersion,
			SourceEventSeq: sourceEventSeq,
			ProbeID:        observation.ProbeID,
			ObservationID:  observation.ObservationID,
			CandidateID:    candidate.CandidateID,
		},
		ElementRef: element.ElementRef,
		Role:       element.A11y.Role,
		Name:       element.A11y.Name,
		DOM: ObservationQueryDOM{
			Tag:   element.DOM.Tag,
			Attrs: attrs,
		},
		Locator:       candidate.Locator,
		Provenance:    candidate.Provenance,
		ObservedCount: candidate.ObservedCount,
	}
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
