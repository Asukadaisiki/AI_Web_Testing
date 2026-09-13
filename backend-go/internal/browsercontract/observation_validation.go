package browsercontract

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"
)

type strictObservation struct {
	SchemaVersion *string            `json:"schema_version"`
	ProbeID       json.RawMessage    `json:"probe_id"`
	ObservationID *string            `json:"observation_id"`
	PageState     json.RawMessage    `json:"page_state"`
	Elements      *[]json.RawMessage `json:"elements"`
	Relations     *[]json.RawMessage `json:"relations"`
	Artifact      json.RawMessage    `json:"artifact"`
}

type strictPageState struct {
	StateID             *string         `json:"state_id"`
	Revision            *int            `json:"revision"`
	URL                 *string         `json:"url"`
	Title               *string         `json:"title"`
	StateSHA256         *string         `json:"state_sha256"`
	PreviousStateSHA256 json.RawMessage `json:"previous_state_sha256"`
}

type strictElement struct {
	ElementRef  *string            `json:"element_ref"`
	ContextPath json.RawMessage    `json:"context_path"`
	A11y        json.RawMessage    `json:"a11y"`
	DOM         json.RawMessage    `json:"dom"`
	Runtime     json.RawMessage    `json:"runtime"`
	Locators    *[]json.RawMessage `json:"locators"`
}

type strictContextPath struct {
	Frames      *[]string `json:"frames"`
	ShadowHosts *[]string `json:"shadow_hosts"`
}

type strictA11y struct {
	Role        *string                     `json:"role"`
	Name        *string                     `json:"name"`
	Description json.RawMessage             `json:"description"`
	Value       json.RawMessage             `json:"value"`
	States      *map[string]json.RawMessage `json:"states"`
	Relations   *map[string]json.RawMessage `json:"relations"`
}

type strictDOM struct {
	BackendNodeID json.RawMessage    `json:"backend_node_id"`
	Tag           *string            `json:"tag"`
	Attrs         *map[string]string `json:"attrs"`
	Text          *string            `json:"text"`
}

type strictRuntime struct {
	Connected *bool `json:"connected"`
	Visible   *bool `json:"visible"`
	Enabled   *bool `json:"enabled"`
	Editable  *bool `json:"editable"`
}

type strictObservedLocator struct {
	CandidateID   json.RawMessage `json:"candidate_id"`
	Locator       json.RawMessage `json:"locator"`
	Provenance    *string         `json:"provenance"`
	ObservedCount *int            `json:"observed_count"`
}

type strictRoleLocator struct {
	Kind  *string         `json:"kind"`
	Role  *string         `json:"role"`
	Name  json.RawMessage `json:"name"`
	Exact *bool           `json:"exact"`
}

type strictValueLocator struct {
	Kind  *string `json:"kind"`
	Value *string `json:"value"`
	Exact *bool   `json:"exact"`
}

type strictScopedLocator struct {
	Kind   *string         `json:"kind"`
	Scope  json.RawMessage `json:"scope"`
	Target json.RawMessage `json:"target"`
}

type strictRelation struct {
	Kind   *string `json:"kind"`
	Source *string `json:"source"`
	Target *string `json:"target"`
}

type strictArtifact struct {
	URI          *string `json:"uri"`
	SHA256       *string `json:"sha256"`
	ContentBytes *int64  `json:"content_bytes"`
}

func validateObservationJSON(raw json.RawMessage) error {
	var observation strictObservation
	if err := decodeStrictJSON(raw, &observation); err != nil {
		return fmt.Errorf("browser observation: %w", err)
	}
	if observation.SchemaVersion == nil ||
		*observation.SchemaVersion != ObservationSchemaVersion ||
		!nonEmpty(observation.ObservationID) ||
		len(observation.PageState) == 0 ||
		observation.Elements == nil ||
		observation.Relations == nil {
		return errors.New("browser observation is incomplete")
	}
	if err := validateOptionalNonEmptyString(observation.ProbeID); err != nil {
		return fmt.Errorf("browser observation probe_id: %w", err)
	}
	if err := validatePageState(observation.PageState); err != nil {
		return fmt.Errorf("browser observation page_state: %w", err)
	}
	for index, element := range *observation.Elements {
		if err := validateObservationElement(element); err != nil {
			return fmt.Errorf("browser observation elements[%d]: %w", index, err)
		}
	}
	for index, relation := range *observation.Relations {
		if err := validateObservationRelation(relation); err != nil {
			return fmt.Errorf("browser observation relations[%d]: %w", index, err)
		}
	}
	if len(observation.Artifact) > 0 && !isJSONNull(observation.Artifact) {
		if err := validateObservationArtifact(observation.Artifact); err != nil {
			return fmt.Errorf("browser observation artifact: %w", err)
		}
	}
	return nil
}

func validatePageState(raw json.RawMessage) error {
	var page strictPageState
	if isJSONNull(raw) {
		return errors.New("must be an object")
	}
	if err := decodeStrictJSON(raw, &page); err != nil {
		return err
	}
	if !nonEmpty(page.StateID) ||
		page.Revision == nil ||
		*page.Revision < 1 ||
		page.URL == nil ||
		page.Title == nil ||
		page.StateSHA256 == nil ||
		!validSHA256(*page.StateSHA256) {
		return errors.New("is incomplete")
	}
	if len(page.PreviousStateSHA256) > 0 &&
		!isJSONNull(page.PreviousStateSHA256) {
		var previous string
		if err := json.Unmarshal(page.PreviousStateSHA256, &previous); err != nil ||
			!validSHA256(previous) {
			return errors.New("previous_state_sha256 is invalid")
		}
	}
	return nil
}

func validateObservationElement(raw json.RawMessage) error {
	var element strictElement
	if isJSONNull(raw) {
		return errors.New("must be an object")
	}
	if err := decodeStrictJSON(raw, &element); err != nil {
		return err
	}
	if !nonEmpty(element.ElementRef) ||
		len(element.ContextPath) == 0 ||
		len(element.A11y) == 0 ||
		len(element.DOM) == 0 ||
		len(element.Runtime) == 0 ||
		element.Locators == nil {
		return errors.New("is incomplete")
	}
	if err := validateContextPath(element.ContextPath); err != nil {
		return fmt.Errorf("context_path: %w", err)
	}
	if !isJSONNull(element.A11y) {
		if err := validateA11y(element.A11y); err != nil {
			return fmt.Errorf("a11y: %w", err)
		}
	}
	if !isJSONNull(element.DOM) {
		if err := validateDOM(element.DOM); err != nil {
			return fmt.Errorf("dom: %w", err)
		}
	}
	if err := validateRuntime(element.Runtime); err != nil {
		return fmt.Errorf("runtime: %w", err)
	}
	for index, locator := range *element.Locators {
		if err := validateObservedLocator(locator); err != nil {
			return fmt.Errorf("locators[%d]: %w", index, err)
		}
	}
	return nil
}

func validateContextPath(raw json.RawMessage) error {
	var path strictContextPath
	if isJSONNull(raw) {
		return errors.New("must be an object")
	}
	if err := decodeStrictJSON(raw, &path); err != nil {
		return err
	}
	if path.Frames == nil || path.ShadowHosts == nil {
		return errors.New("is incomplete")
	}
	return nil
}

func validateA11y(raw json.RawMessage) error {
	var fact strictA11y
	if err := decodeStrictJSON(raw, &fact); err != nil {
		return err
	}
	if fact.Role == nil ||
		fact.Name == nil ||
		fact.States == nil ||
		fact.Relations == nil {
		return errors.New("is incomplete")
	}
	if err := validateOptionalString(fact.Description); err != nil {
		return fmt.Errorf("description: %w", err)
	}
	return nil
}

func validateDOM(raw json.RawMessage) error {
	var fact strictDOM
	if err := decodeStrictJSON(raw, &fact); err != nil {
		return err
	}
	if fact.Tag == nil || fact.Attrs == nil || fact.Text == nil {
		return errors.New("is incomplete")
	}
	if utf8.RuneCountInString(*fact.Text) > 256 {
		return errors.New("text exceeds 256 characters")
	}
	if len(fact.BackendNodeID) > 0 && !isJSONNull(fact.BackendNodeID) {
		var nodeID int64
		if err := json.Unmarshal(fact.BackendNodeID, &nodeID); err != nil {
			return errors.New("backend_node_id must be an integer or null")
		}
	}
	return nil
}

func validateRuntime(raw json.RawMessage) error {
	var runtime strictRuntime
	if isJSONNull(raw) {
		return errors.New("must be an object")
	}
	if err := decodeStrictJSON(raw, &runtime); err != nil {
		return err
	}
	if runtime.Connected == nil ||
		runtime.Visible == nil ||
		runtime.Enabled == nil ||
		runtime.Editable == nil {
		return errors.New("is incomplete")
	}
	return nil
}

func validateObservedLocator(raw json.RawMessage) error {
	var candidate strictObservedLocator
	if isJSONNull(raw) {
		return errors.New("must be an object")
	}
	if err := decodeStrictJSON(raw, &candidate); err != nil {
		return err
	}
	if err := validateOptionalNonEmptyString(candidate.CandidateID); err != nil {
		return fmt.Errorf("candidate_id: %w", err)
	}
	if len(candidate.Locator) == 0 ||
		!nonEmpty(candidate.Provenance) ||
		candidate.ObservedCount == nil ||
		*candidate.ObservedCount < 0 {
		return errors.New("is incomplete")
	}
	if err := validateLocatorJSON(candidate.Locator, true); err != nil {
		return fmt.Errorf("locator: %w", err)
	}
	return nil
}

func validateLocatorJSON(raw json.RawMessage, allowScoped bool) error {
	var discriminator struct {
		Kind *string `json:"kind"`
	}
	if isJSONNull(raw) {
		return errors.New("must be an object")
	}
	if err := json.Unmarshal(raw, &discriminator); err != nil {
		return err
	}
	if discriminator.Kind == nil {
		return errors.New("kind is required")
	}
	switch *discriminator.Kind {
	case "role":
		var locator strictRoleLocator
		if err := decodeStrictJSON(raw, &locator); err != nil {
			return err
		}
		if !nonEmpty(locator.Role) || locator.Exact == nil {
			return errors.New("role locator is incomplete")
		}
		return validateOptionalString(locator.Name)
	case "label", "placeholder", "text", "test_id", "css", "xpath":
		var locator strictValueLocator
		if err := decodeStrictJSON(raw, &locator); err != nil {
			return err
		}
		if !nonEmpty(locator.Value) || locator.Exact == nil {
			return errors.New("value locator is incomplete")
		}
		return nil
	case "scoped":
		if !allowScoped {
			return errors.New("nested scoped locators are not supported")
		}
		var locator strictScopedLocator
		if err := decodeStrictJSON(raw, &locator); err != nil {
			return err
		}
		if len(locator.Scope) == 0 || len(locator.Target) == 0 {
			return errors.New("scoped locator is incomplete")
		}
		if err := validateLocatorJSON(locator.Scope, false); err != nil {
			return fmt.Errorf("scope: %w", err)
		}
		if err := validateLocatorJSON(locator.Target, false); err != nil {
			return fmt.Errorf("target: %w", err)
		}
		return nil
	default:
		return fmt.Errorf("unsupported locator kind %q", *discriminator.Kind)
	}
}

func validateObservationRelation(raw json.RawMessage) error {
	var relation strictRelation
	if isJSONNull(raw) {
		return errors.New("must be an object")
	}
	if err := decodeStrictJSON(raw, &relation); err != nil {
		return err
	}
	if !nonEmpty(relation.Kind) ||
		!nonEmpty(relation.Source) ||
		!nonEmpty(relation.Target) {
		return errors.New("is incomplete")
	}
	return nil
}

func validateObservationArtifact(raw json.RawMessage) error {
	var artifact strictArtifact
	if err := decodeStrictJSON(raw, &artifact); err != nil {
		return err
	}
	if !nonEmpty(artifact.URI) ||
		artifact.SHA256 == nil ||
		!validSHA256(*artifact.SHA256) ||
		artifact.ContentBytes == nil ||
		*artifact.ContentBytes < 0 {
		return errors.New("is incomplete")
	}
	return nil
}

func decodeStrictJSON(raw json.RawMessage, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("contains trailing JSON")
	}
	return nil
}

func validateOptionalNonEmptyString(raw json.RawMessage) error {
	if len(raw) == 0 || isJSONNull(raw) {
		return nil
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return errors.New("must be a string or null")
	}
	if strings.TrimSpace(value) == "" {
		return errors.New("must not be empty")
	}
	return nil
}

func validateOptionalString(raw json.RawMessage) error {
	if len(raw) == 0 || isJSONNull(raw) {
		return nil
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return errors.New("must be a string or null")
	}
	return nil
}

func nonEmpty(value *string) bool {
	return value != nil && strings.TrimSpace(*value) != ""
}

func validSHA256(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, char := range value {
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') {
			return false
		}
	}
	return true
}

func isJSONNull(raw json.RawMessage) bool {
	return bytes.Equal(bytes.TrimSpace(raw), []byte("null"))
}
