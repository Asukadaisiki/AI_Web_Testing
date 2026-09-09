package browsercontract

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

const (
	ObservationSchemaVersion = "browser.observation.v2"
	TargetBindingVersion     = "grounding.target-binding.v1"
)

type LocatorSpec struct {
	Kind   string       `json:"kind"`
	Role   string       `json:"role,omitempty"`
	Name   *string      `json:"name,omitempty"`
	Value  string       `json:"value,omitempty"`
	Exact  bool         `json:"exact"`
	Scope  *LocatorSpec `json:"scope,omitempty"`
	Target *LocatorSpec `json:"target,omitempty"`
}

func (s LocatorSpec) Validate() error {
	switch s.Kind {
	case "role":
		if strings.TrimSpace(s.Role) == "" {
			return errors.New("role locator requires role")
		}
		if s.Scope != nil || s.Target != nil || s.Value != "" {
			return errors.New("role locator contains incompatible fields")
		}
	case "label", "placeholder", "text", "test_id", "css", "xpath":
		if strings.TrimSpace(s.Value) == "" {
			return fmt.Errorf("%s locator requires value", s.Kind)
		}
		if s.Scope != nil || s.Target != nil || s.Role != "" || s.Name != nil {
			return fmt.Errorf("%s locator contains incompatible fields", s.Kind)
		}
	case "scoped":
		if s.Scope == nil || s.Target == nil {
			return errors.New("scoped locator requires scope and target")
		}
		if s.Scope.Kind == "scoped" || s.Target.Kind == "scoped" {
			return errors.New("nested scoped locators are not supported")
		}
		if err := s.Scope.Validate(); err != nil {
			return fmt.Errorf("scope: %w", err)
		}
		if err := s.Target.Validate(); err != nil {
			return fmt.Errorf("target: %w", err)
		}
	default:
		return fmt.Errorf("unsupported locator kind %q", s.Kind)
	}
	return nil
}

type LocatorCandidate struct {
	CandidateID   string      `json:"candidate_id"`
	ElementRef    string      `json:"element_ref"`
	ContextPath   ContextPath `json:"context_path"`
	Locator       LocatorSpec `json:"locator"`
	Provenance    string      `json:"provenance"`
	ObservedCount int         `json:"observed_count"`
	Visible       bool        `json:"visible"`
	Enabled       bool        `json:"enabled"`
	Score         float64     `json:"score"`
}

type ContextPath struct {
	Frames      []string `json:"frames"`
	ShadowHosts []string `json:"shadow_hosts"`
}

type TargetBinding struct {
	SchemaVersion       string             `json:"schema_version"`
	BindingID           string             `json:"binding_id"`
	PlanID              string             `json:"plan_id"`
	PlanVersion         int                `json:"plan_version"`
	PlanStepID          string             `json:"plan_step_id"`
	SemanticTarget      string             `json:"semantic_target"`
	Action              string             `json:"action"`
	PageStateID         string             `json:"page_state_id"`
	ObservationID       string             `json:"observation_id"`
	ObservationSHA256   string             `json:"observation_sha256"`
	ElementRefs         []string           `json:"element_refs"`
	Candidates          []LocatorCandidate `json:"candidates"`
	SelectedCandidateID string             `json:"selected_candidate_id"`
	BindingSHA256       string             `json:"binding_sha256"`
}

func NewTargetBinding(binding TargetBinding) (TargetBinding, error) {
	binding.SchemaVersion = TargetBindingVersion
	for index := range binding.Candidates {
		if binding.Candidates[index].ContextPath.Frames == nil {
			binding.Candidates[index].ContextPath.Frames = []string{}
		}
		if binding.Candidates[index].ContextPath.ShadowHosts == nil {
			binding.Candidates[index].ContextPath.ShadowHosts = []string{}
		}
	}
	binding.BindingID = ""
	binding.BindingSHA256 = ""
	seed, err := json.Marshal(binding)
	if err != nil {
		return TargetBinding{}, err
	}
	sum := sha256.Sum256(seed)
	binding.BindingID = "binding_" + hex.EncodeToString(sum[:12])
	binding.BindingSHA256, err = binding.CalculateSHA256()
	if err != nil {
		return TargetBinding{}, err
	}
	if err := binding.Validate(); err != nil {
		return TargetBinding{}, err
	}
	return binding, nil
}

func (b TargetBinding) Validate() error {
	if b.SchemaVersion != TargetBindingVersion {
		return fmt.Errorf("unsupported target binding schema %q", b.SchemaVersion)
	}
	if strings.TrimSpace(b.BindingID) == "" ||
		strings.TrimSpace(b.PlanID) == "" ||
		b.PlanVersion < 1 ||
		strings.TrimSpace(b.PlanStepID) == "" ||
		strings.TrimSpace(b.SemanticTarget) == "" ||
		strings.TrimSpace(b.Action) == "" ||
		strings.TrimSpace(b.PageStateID) == "" ||
		strings.TrimSpace(b.ObservationID) == "" ||
		len(b.ObservationSHA256) != 64 ||
		len(b.BindingSHA256) != 64 ||
		len(b.ElementRefs) == 0 ||
		len(b.Candidates) == 0 {
		return errors.New("target binding is incomplete")
	}
	selected := false
	elementRefs := make(map[string]bool, len(b.ElementRefs))
	for _, elementRef := range b.ElementRefs {
		if strings.TrimSpace(elementRef) == "" || elementRefs[elementRef] {
			return errors.New("target binding element refs must be unique")
		}
		elementRefs[elementRef] = true
	}
	ids := make(map[string]bool, len(b.Candidates))
	for _, candidate := range b.Candidates {
		if strings.TrimSpace(candidate.CandidateID) == "" ||
			strings.TrimSpace(candidate.ElementRef) == "" ||
			strings.TrimSpace(candidate.Provenance) == "" ||
			candidate.ObservedCount < 0 ||
			candidate.Score < 0 ||
			candidate.Score > 1 {
			return errors.New("target binding candidate is invalid")
		}
		if !elementRefs[candidate.ElementRef] {
			return errors.New(
				"target binding candidate references an unknown element",
			)
		}
		if ids[candidate.CandidateID] {
			return errors.New("target binding candidate IDs must be unique")
		}
		ids[candidate.CandidateID] = true
		if err := candidate.Locator.Validate(); err != nil {
			return fmt.Errorf("candidate %q: %w", candidate.CandidateID, err)
		}
		if len(candidate.ContextPath.ShadowHosts) > 0 &&
			candidate.Locator.containsKind("xpath") {
			return fmt.Errorf(
				"candidate %q uses xpath inside shadow DOM",
				candidate.CandidateID,
			)
		}
		if candidate.CandidateID == b.SelectedCandidateID {
			if candidate.ObservedCount != 1 || !candidate.Visible {
				return errors.New(
					"selected candidate must be unique and visible",
				)
			}
			if (b.Action == "click" || b.Action == "input") &&
				!candidate.Enabled {
				return errors.New(
					"selected action candidate must be enabled",
				)
			}
			selected = true
		}
	}
	if !selected {
		return errors.New("selected candidate is absent")
	}
	expected, err := b.CalculateSHA256()
	if err != nil {
		return err
	}
	if expected != b.BindingSHA256 {
		return errors.New("target binding SHA-256 mismatch")
	}
	return nil
}

func (s LocatorSpec) containsKind(kind string) bool {
	if s.Kind == kind {
		return true
	}
	return s.Scope != nil && s.Scope.containsKind(kind) ||
		s.Target != nil && s.Target.containsKind(kind)
}

func (b TargetBinding) CalculateSHA256() (string, error) {
	b.BindingSHA256 = ""
	raw, err := json.Marshal(b)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

type BrowserObservation struct {
	SchemaVersion string          `json:"schema_version"`
	ObservationID string          `json:"observation_id"`
	PageState     json.RawMessage `json:"page_state"`
	Elements      json.RawMessage `json:"elements"`
	Relations     json.RawMessage `json:"relations"`
	Artifact      json.RawMessage `json:"artifact,omitempty"`
}

func DecodeObservation(raw json.RawMessage) (BrowserObservation, error) {
	var observation BrowserObservation
	if err := json.Unmarshal(raw, &observation); err != nil {
		return BrowserObservation{}, err
	}
	if observation.SchemaVersion != ObservationSchemaVersion ||
		strings.TrimSpace(observation.ObservationID) == "" ||
		len(observation.PageState) == 0 ||
		len(observation.Elements) == 0 ||
		len(observation.Relations) == 0 {
		return BrowserObservation{}, errors.New("browser observation is incomplete")
	}
	return observation, nil
}
