package dsl

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"
)

type Profile string

const (
	ProfileLegacyV1   Profile = "legacy-v1"
	ProfileResearchV1 Profile = "research-v1"

	CanonicalVersionV1 = "dsl.canonical.v1"
	CanonicalVersionV2 = "dsl.canonical.v2"
)

type ValidationPhase string

const (
	ValidationPhaseDraft      ValidationPhase = "draft"
	ValidationPhaseExecutable ValidationPhase = "executable"
)

type ValidatedCase struct {
	CanonicalJSON    json.RawMessage
	BaseURL          string
	Profile          Profile
	CanonicalVersion string
	Phase            ValidationPhase
}

var (
	idempotencies                 = stringSet("idempotent", "non_idempotent")
	sideEffects                   = stringSet("none", "browser_state", "external_state", "unknown")
	executableCandidateStrategies = stringSet(
		"css", "css_selector", "xpath", "data-testid", "data_testid",
		"role", "text", "label", "placeholder", "element_id", "tag", "semantic",
		"verified_role", "verified_role_fuzzy", "verified_css", "verified_xpath",
		"verified_placeholder", "verified_placeholder_fuzzy", "verified_label",
		"verified_label_fuzzy", "verified_text", "verified_data-testid",
		"verified_element_id", "a11y_scoped_role_exact",
		"a11y_scoped_role_fuzzy", "a11y_scoped_text_exact",
		"a11y_scoped_text_fuzzy",
	)
)

func ValidateDraftCase(raw json.RawMessage) (ValidatedCase, error) {
	return validateCase(raw, ValidationPhaseDraft)
}

func ValidateExecutableCase(raw json.RawMessage) (ValidatedCase, error) {
	return validateCase(raw, ValidationPhaseExecutable)
}

func ValidateCase(raw json.RawMessage) (json.RawMessage, string, error) {
	validated, err := ValidateExecutableCase(raw)
	if err != nil {
		return nil, "", err
	}
	return validated.CanonicalJSON, validated.BaseURL, nil
}

func ValidateCaseForVersion(raw json.RawMessage, version string) (ValidatedCase, error) {
	validated, err := ValidateExecutableCase(raw)
	if err != nil {
		return ValidatedCase{}, err
	}
	if validated.CanonicalVersion != version {
		return ValidatedCase{}, fmt.Errorf(
			"DSL profile %q uses %s, not %s",
			validated.Profile,
			validated.CanonicalVersion,
			version,
		)
	}
	return validated, nil
}

func IsCanonicalVersion(version string) bool {
	return version == CanonicalVersionV1 || version == CanonicalVersionV2
}

func validateCase(raw json.RawMessage, phase ValidationPhase) (ValidatedCase, error) {
	profile, normalizedRaw, err := caseProfile(raw)
	if err != nil {
		return ValidatedCase{}, err
	}
	if profile == ProfileLegacyV1 {
		canonical, baseURL, err := validateLegacyCase(normalizedRaw)
		if err != nil {
			return ValidatedCase{}, err
		}
		return ValidatedCase{
			CanonicalJSON: canonical, BaseURL: baseURL, Profile: profile,
			CanonicalVersion: CanonicalVersionV1, Phase: phase,
		}, nil
	}
	return validateResearchCase(normalizedRaw, phase)
}

func caseProfile(raw json.RawMessage) (Profile, json.RawMessage, error) {
	var candidate map[string]any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if err := decoder.Decode(&candidate); err != nil || candidate == nil {
		return "", nil, errors.New("case must be an object")
	}
	value, exists := candidate["profile"]
	if !exists {
		return ProfileLegacyV1, raw, nil
	}
	profile, ok := value.(string)
	if !ok {
		return "", nil, errors.New("case.profile must be legacy-v1 or research-v1")
	}
	switch Profile(strings.TrimSpace(profile)) {
	case ProfileLegacyV1:
		delete(candidate, "profile")
		normalized, err := json.Marshal(candidate)
		return ProfileLegacyV1, normalized, err
	case ProfileResearchV1:
		candidate["profile"] = string(ProfileResearchV1)
		normalized, err := json.Marshal(candidate)
		return ProfileResearchV1, normalized, err
	default:
		return "", nil, errors.New("case.profile must be legacy-v1 or research-v1")
	}
}

func validateResearchCase(raw json.RawMessage, phase ValidationPhase) (ValidatedCase, error) {
	if phase != ValidationPhaseDraft && phase != ValidationPhaseExecutable {
		return ValidatedCase{}, fmt.Errorf("unsupported DSL validation phase: %s", phase)
	}
	var candidate map[string]any
	if err := json.Unmarshal(raw, &candidate); err != nil || candidate == nil {
		return ValidatedCase{}, errors.New("case must be an object")
	}
	if err := rejectUnknownFields("case", candidate, stringSet(
		"profile", "name", "description", "base_url", "input_contract",
		"output_contract", "steps",
	)); err != nil {
		return ValidatedCase{}, err
	}
	if candidate["profile"] != string(ProfileResearchV1) {
		return ValidatedCase{}, errors.New("case.profile must be research-v1")
	}
	name, _ := candidate["name"].(string)
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > 200 {
		return ValidatedCase{}, errors.New("case.name must contain 1 to 200 characters")
	}
	candidate["name"] = name
	if err := validateOptionalText(candidate, "description", 1000); err != nil {
		return ValidatedCase{}, err
	}
	if err := validateOptionalText(candidate, "base_url", 500); err != nil {
		return ValidatedCase{}, err
	}
	for _, field := range []string{"input_contract", "output_contract"} {
		value, exists := candidate[field]
		if !exists {
			candidate[field] = []any{}
			continue
		}
		contracts, ok := value.([]any)
		if !ok {
			return ValidatedCase{}, fmt.Errorf("case.%s must be an array", field)
		}
		if err := validateResearchContracts(field, contracts); err != nil {
			return ValidatedCase{}, err
		}
	}
	steps, ok := candidate["steps"].([]any)
	if !ok || len(steps) == 0 {
		return ValidatedCase{}, errors.New("case.steps must be a non-empty array")
	}
	for index, rawStep := range steps {
		step, ok := rawStep.(map[string]any)
		if !ok {
			return ValidatedCase{}, fmt.Errorf("case.steps[%d] must be an object", index)
		}
		if err := validateResearchStep(index, step, phase); err != nil {
			return ValidatedCase{}, err
		}
	}

	canonical := canonicalResearchCase(candidate)
	normalized, err := json.Marshal(canonical)
	if err != nil {
		return ValidatedCase{}, err
	}
	baseURL, _ := canonical["base_url"].(string)
	return ValidatedCase{
		CanonicalJSON: normalized, BaseURL: baseURL,
		Profile: ProfileResearchV1, CanonicalVersion: CanonicalVersionV2,
		Phase: phase,
	}, nil
}

func validateResearchContracts(field string, contracts []any) error {
	allowed := stringSet("name", "context_key", "value_type", "description")
	if field == "input_contract" {
		allowed["required"] = true
		allowed["value"] = true
	} else {
		allowed["source"] = true
	}
	for index, raw := range contracts {
		contract, ok := raw.(map[string]any)
		if !ok {
			return fmt.Errorf("case.%s[%d] must be an object", field, index)
		}
		if err := rejectUnknownFields(
			fmt.Sprintf("case.%s[%d]", field, index),
			contract,
			allowed,
		); err != nil {
			return err
		}
	}
	return validateContracts(field, contracts)
}

func validateResearchStep(index int, step map[string]any, phase ValidationPhase) error {
	action, _ := step["action"].(string)
	if _, ok := supportedActions[action]; !ok {
		return fmt.Errorf("unsupported DSL action: %s", action)
	}
	allowed := researchStepFields(action)
	if err := rejectUnknownFields(fmt.Sprintf("case.steps[%d]", index), step, allowed); err != nil {
		return err
	}
	if err := requireBoundedText(step, "intent", index, 500); err != nil {
		return err
	}
	preconditions, err := requiredConditionArray(index, "preconditions", step)
	if err != nil {
		return err
	}
	postconditions, err := requiredConditionArray(index, "postconditions", step)
	if err != nil {
		return err
	}
	if err := validateResearchConditions(index, "preconditions", preconditions); err != nil {
		return err
	}
	if err := validateResearchConditions(index, "postconditions", postconditions); err != nil {
		return err
	}
	if !validRequiredEnum(step, "idempotency", idempotencies) {
		return fmt.Errorf("case.steps[%d].idempotency is invalid", index)
	}
	if !validRequiredEnum(step, "side_effect", sideEffects) {
		return fmt.Errorf("case.steps[%d].side_effect is invalid", index)
	}
	if err := validateResearchActionSemantics(index, action, step, preconditions, postconditions); err != nil {
		return err
	}
	if err := validateStep(index, action, step); err != nil {
		return err
	}
	if phase == ValidationPhaseDraft {
		if _, exists := step["candidates"]; exists {
			return fmt.Errorf("case.steps[%d].candidates must be added by locator preflight", index)
		}
		if _, exists := step["locator_confidence"]; exists {
			return fmt.Errorf("case.steps[%d].locator_confidence must be added by locator preflight", index)
		}
		return nil
	}
	if requiresPreflightCandidates(action, step) || hasPreflightCandidates(step) {
		if err := validateExecutableLocator(index, step); err != nil {
			return err
		}
	}
	return nil
}

func researchStepFields(action string) map[string]bool {
	fields := stringSet(
		"action", "intent", "preconditions", "postconditions",
		"idempotency", "side_effect",
	)
	switch action {
	case "goto", "assert_url_contains":
		fields["target"] = true
		fields["value"] = true
	case "click":
		addLocatorStepFields(fields)
	case "input":
		addLocatorStepFields(fields)
		fields["value"] = true
		fields["trigger"] = true
	case "wait_for":
		addLocatorStepFields(fields)
		fields["timeout_ms"] = true
	case "assert_text":
		addLocatorStepFields(fields)
		fields["value"] = true
	case "capture_text":
		addLocatorStepFields(fields)
		fields["context_key"] = true
	}
	return fields
}

func addLocatorStepFields(fields map[string]bool) {
	fields["target"] = true
	fields["page_state"] = true
	fields["target_strategy"] = true
	fields["locator_confidence"] = true
	fields["candidates"] = true
}

func validateResearchActionSemantics(
	index int,
	action string,
	step map[string]any,
	preconditions, postconditions []any,
) error {
	if action == "goto" || action == "assert_url_contains" {
		if err := requireText(step, "target", index); err != nil {
			return err
		}
	}
	idempotency := strings.TrimSpace(step["idempotency"].(string))
	sideEffect := strings.TrimSpace(step["side_effect"].(string))
	switch action {
	case "goto", "input":
		if idempotency != "idempotent" || sideEffect != "browser_state" {
			return fmt.Errorf(
				"case.steps[%d] %s must be idempotent with browser_state side effect",
				index,
				action,
			)
		}
	case "wait_for", "assert_text", "assert_url_contains", "capture_text":
		if idempotency != "idempotent" || sideEffect != "none" {
			return fmt.Errorf(
				"case.steps[%d] %s must be idempotent with no side effect",
				index,
				action,
			)
		}
	}
	if action == "goto" && len(postconditions) == 0 {
		return fmt.Errorf(
			"case.steps[%d] goto requires at least one postcondition",
			index,
		)
	}
	if action == "input" || action == "click" {
		if len(preconditions) == 0 || len(postconditions) == 0 {
			return fmt.Errorf(
				"case.steps[%d] %s requires at least one precondition and postcondition",
				index,
				action,
			)
		}
	}
	if (idempotency == "non_idempotent" ||
		sideEffect == "external_state" ||
		sideEffect == "unknown") &&
		len(postconditions) == 0 {
		return fmt.Errorf("case.steps[%d] requires a postcondition for its action risk", index)
	}
	return nil
}

func requiredConditionArray(
	stepIndex int,
	field string,
	step map[string]any,
) ([]any, error) {
	raw, exists := step[field]
	if !exists {
		return nil, fmt.Errorf("case.steps[%d].%s is required", stepIndex, field)
	}
	conditions, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("case.steps[%d].%s must be an array", stepIndex, field)
	}
	return conditions, nil
}

func validateResearchConditions(stepIndex int, field string, conditions []any) error {
	for index, raw := range conditions {
		condition, ok := raw.(map[string]any)
		if !ok {
			return fmt.Errorf("case.steps[%d].%s[%d] must be an object", stepIndex, field, index)
		}
		if err := rejectUnknownFields(
			fmt.Sprintf("case.steps[%d].%s[%d]", stepIndex, field, index),
			condition,
			stringSet("type", "value", "method", "status", "timeout_ms"),
		); err != nil {
			return err
		}
	}
	return validateConditions(stepIndex, field, conditions)
}

func validateExecutableLocator(index int, step map[string]any) error {
	confidence, ok := step["locator_confidence"].(string)
	if !ok || (strings.TrimSpace(confidence) != "high" && strings.TrimSpace(confidence) != "medium") {
		return fmt.Errorf("case.steps[%d].locator_confidence must be high or medium", index)
	}
	candidates, ok := step["candidates"].([]any)
	if !ok || len(candidates) == 0 {
		return fmt.Errorf("case.steps[%d].candidates must be a non-empty array", index)
	}
	hasVerifiedEvidence := false
	for candidateIndex, raw := range candidates {
		candidate, ok := raw.(map[string]any)
		if !ok {
			return fmt.Errorf("case.steps[%d].candidates[%d] must be an object", index, candidateIndex)
		}
		if err := rejectUnknownFields(
			fmt.Sprintf("case.steps[%d].candidates[%d]", index, candidateIndex),
			candidate,
			stringSet("strategy", "selector", "semantic_value", "pre_score", "pre_features"),
		); err != nil {
			return err
		}
		strategy, _ := candidate["strategy"].(string)
		strategy = strings.TrimSpace(strategy)
		if !executableCandidateStrategies[strategy] {
			return fmt.Errorf(
				"case.steps[%d].candidates[%d].strategy is not supported by the runner",
				index,
				candidateIndex,
			)
		}
		selector, _ := candidate["selector"].(string)
		if strings.TrimSpace(selector) == "" {
			return fmt.Errorf(
				"case.steps[%d].candidates[%d].selector is required",
				index,
				candidateIndex,
			)
		}
		features, ok := candidate["pre_features"].(map[string]any)
		hasVerifiedEvidence = hasVerifiedEvidence ||
			(ok && hasVerifiedPreflightProvenance(strategy, features))
	}
	if !hasVerifiedEvidence {
		return fmt.Errorf("case.steps[%d].candidates lack verified preflight evidence", index)
	}
	return nil
}

func hasVerifiedPreflightProvenance(strategy string, features map[string]any) bool {
	verified, _ := features["verified"].(bool)
	source, _ := features["source"].(string)
	if !verified {
		return false
	}
	source = strings.TrimSpace(source)
	switch {
	case strings.HasPrefix(strategy, "verified_"):
		return source == "a11y_backend_dom_node" ||
			source == "dom_verified_interactive_control" ||
			source == strategy
	case strategy == "role":
		return source == "a11y_role_exact"
	case strategy == "text":
		return source == "a11y_text_exact"
	case strings.HasPrefix(strategy, "a11y_scoped_"):
		return source == strategy
	default:
		return false
	}
}

func canonicalResearchCase(candidate map[string]any) map[string]any {
	return map[string]any{
		"profile":         string(ProfileResearchV1),
		"name":            trimmedString(candidate["name"]),
		"description":     optionalTrimmedString(candidate["description"]),
		"base_url":        optionalTrimmedString(candidate["base_url"]),
		"input_contract":  canonicalContracts(candidate["input_contract"], true),
		"output_contract": canonicalContracts(candidate["output_contract"], false),
		"steps":           canonicalResearchSteps(candidate["steps"].([]any)),
	}
}

func canonicalResearchSteps(steps []any) []any {
	result := make([]any, 0, len(steps))
	for _, raw := range steps {
		step := raw.(map[string]any)
		action := trimmedString(step["action"])
		canonical := map[string]any{
			"action":         action,
			"intent":         trimmedString(step["intent"]),
			"idempotency":    trimmedString(step["idempotency"]),
			"side_effect":    trimmedString(step["side_effect"]),
			"preconditions":  canonicalConditions(step["preconditions"]),
			"postconditions": canonicalConditions(step["postconditions"]),
		}
		switch action {
		case "goto", "assert_url_contains":
			canonical["target"] = trimmedString(step["target"])
			canonical["value"] = trimmedString(step["value"])
		case "click":
			canonicalResearchLocatorFields(canonical, step)
		case "input":
			canonicalResearchLocatorFields(canonical, step)
			canonical["value"] = trimmedString(step["value"])
			canonical["trigger"] = optionalTrimmedString(step["trigger"])
		case "wait_for":
			canonicalResearchLocatorFields(canonical, step)
			canonical["timeout_ms"] = valueOrDefault(step, "timeout_ms", float64(5000))
		case "assert_text":
			canonicalResearchLocatorFields(canonical, step)
			canonical["value"] = trimmedString(step["value"])
		case "capture_text":
			canonicalResearchLocatorFields(canonical, step)
			canonical["context_key"] = trimmedString(step["context_key"])
		}
		result = append(result, canonical)
	}
	return result
}

func canonicalResearchLocatorFields(canonical, step map[string]any) {
	canonical["target"] = trimmedString(step["target"])
	canonical["page_state"] = optionalTrimmedString(step["page_state"])
	canonical["target_strategy"] = optionalTrimmedString(step["target_strategy"])
	if confidence, exists := step["locator_confidence"]; exists {
		canonical["locator_confidence"] = optionalTrimmedString(confidence)
	}
	if candidates, exists := step["candidates"]; exists {
		canonical["candidates"] = canonicalCandidates(candidates)
	}
}

func requiresPreflightCandidates(action string, step map[string]any) bool {
	switch action {
	case "click", "input", "capture_text":
		return true
	}
	if strategy, _ := step["target_strategy"].(string); strings.TrimSpace(strategy) != "" {
		return true
	}
	target := strings.TrimSpace(trimmedString(step["target"]))
	return strings.HasPrefix(target, "css=") ||
		strings.HasPrefix(target, "xpath=") ||
		strings.HasPrefix(target, "#") ||
		strings.HasPrefix(target, ".") ||
		strings.HasPrefix(target, "//")
}

func hasPreflightCandidates(step map[string]any) bool {
	candidates, ok := step["candidates"].([]any)
	return ok && len(candidates) > 0
}

func requireBoundedText(object map[string]any, field string, index, maxLength int) error {
	value, ok := object[field].(string)
	if !ok || strings.TrimSpace(value) == "" ||
		utf8.RuneCountInString(strings.TrimSpace(value)) > maxLength {
		return fmt.Errorf(
			"case.steps[%d].%s must contain 1 to %d characters",
			index,
			field,
			maxLength,
		)
	}
	return nil
}

func validRequiredEnum(object map[string]any, field string, allowed map[string]bool) bool {
	value, ok := object[field].(string)
	return ok && allowed[strings.TrimSpace(value)]
}

func rejectUnknownFields(path string, object map[string]any, allowed map[string]bool) error {
	unknown := make([]string, 0)
	for field := range object {
		if !allowed[field] {
			unknown = append(unknown, field)
		}
	}
	if len(unknown) == 0 {
		return nil
	}
	sort.Strings(unknown)
	return fmt.Errorf("%s contains unknown field %q", path, unknown[0])
}
