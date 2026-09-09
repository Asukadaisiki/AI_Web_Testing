package dsl

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/browsercontract"
)

func validateResearchV2Case(
	raw json.RawMessage,
	phase ValidationPhase,
) (ValidatedCase, error) {
	if phase != ValidationPhaseDraft && phase != ValidationPhaseExecutable {
		return ValidatedCase{}, fmt.Errorf(
			"unsupported DSL validation phase: %s",
			phase,
		)
	}
	var candidate map[string]any
	if err := json.Unmarshal(raw, &candidate); err != nil || candidate == nil {
		return ValidatedCase{}, errors.New("case must be an object")
	}
	if err := rejectUnknownFields("case", candidate, stringSet(
		"profile", "name", "description", "base_url", "input_contract",
		"output_contract", "steps", "plan_binding", "observation_bindings",
	)); err != nil {
		return ValidatedCase{}, err
	}
	if candidate["profile"] != string(ProfileResearchV2) {
		return ValidatedCase{}, errors.New("case.profile must be research-v2")
	}
	name, _ := candidate["name"].(string)
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > 200 {
		return ValidatedCase{}, errors.New(
			"case.name must contain 1 to 200 characters",
		)
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
	if phase == ValidationPhaseDraft {
		if _, exists := candidate["plan_binding"]; exists {
			return ValidatedCase{}, errors.New(
				"research-v2 draft must not contain plan_binding",
			)
		}
		if _, exists := candidate["observation_bindings"]; exists {
			return ValidatedCase{}, errors.New(
				"research-v2 draft must not contain observation_bindings",
			)
		}
	}
	steps, ok := candidate["steps"].([]any)
	if !ok || len(steps) == 0 {
		return ValidatedCase{}, errors.New("case.steps must be a non-empty array")
	}
	for index, rawStep := range steps {
		step, ok := rawStep.(map[string]any)
		if !ok {
			return ValidatedCase{}, fmt.Errorf(
				"case.steps[%d] must be an object",
				index,
			)
		}
		if err := validateResearchV2Step(index, step, phase); err != nil {
			return ValidatedCase{}, err
		}
		materializeResearchV2Defaults(step)
	}
	if phase == ValidationPhaseExecutable {
		if _, ok := candidate["plan_binding"].(map[string]any); !ok {
			return ValidatedCase{}, errors.New(
				"research-v2 executable case requires plan_binding",
			)
		}
		if _, ok := candidate["observation_bindings"].([]any); !ok {
			return ValidatedCase{}, errors.New(
				"research-v2 executable case requires observation_bindings",
			)
		}
	}
	normalized, err := json.Marshal(candidate)
	if err != nil {
		return ValidatedCase{}, err
	}
	baseURL, _ := candidate["base_url"].(string)
	return ValidatedCase{
		CanonicalJSON: normalized,
		BaseURL:       baseURL,
		Profile:       ProfileResearchV2, CanonicalVersion: CanonicalVersionV3,
		Phase: phase,
	}, nil
}

func materializeResearchV2Defaults(step map[string]any) {
	for _, field := range []string{"preconditions", "postconditions"} {
		conditions, _ := step[field].([]any)
		for _, rawCondition := range conditions {
			condition, _ := rawCondition.(map[string]any)
			if condition == nil {
				continue
			}
			if _, exists := condition["timeout_ms"]; !exists {
				condition["timeout_ms"] = float64(3000)
			}
		}
	}
	if step["action"] == "wait_for" {
		if _, exists := step["timeout_ms"]; !exists {
			step["timeout_ms"] = float64(5000)
		}
	}
}

func validateResearchV2Step(
	index int,
	step map[string]any,
	phase ValidationPhase,
) error {
	action, _ := step["action"].(string)
	if _, ok := supportedActions[action]; !ok {
		return fmt.Errorf("unsupported DSL action: %s", action)
	}
	allowed := stringSet(
		"plan_step_id", "action", "intent", "target_binding_id",
		"semantic_target", "locator_candidates", "value", "trigger",
		"context_key", "timeout_ms", "preconditions", "postconditions",
		"idempotency", "side_effect",
	)
	if err := rejectUnknownFields(
		fmt.Sprintf("case.steps[%d]", index),
		step,
		allowed,
	); err != nil {
		return err
	}
	if err := requireBoundedText(step, "plan_step_id", index, 64); err != nil {
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
	for _, conditions := range [][]any{preconditions, postconditions} {
		for _, rawCondition := range conditions {
			condition, _ := rawCondition.(map[string]any)
			conditionType, _ := condition["type"].(string)
			if conditionType == "element_visible" ||
				conditionType == "element_gone" {
				return fmt.Errorf(
					"case.steps[%d] research-v2 conditions must not use unbound %s",
					index,
					conditionType,
				)
			}
		}
	}
	if !validRequiredEnum(step, "idempotency", idempotencies) ||
		!validRequiredEnum(step, "side_effect", sideEffects) {
		return fmt.Errorf("case.steps[%d] has invalid action metadata", index)
	}
	if err := validateResearchV2ActionFields(index, action, step); err != nil {
		return err
	}
	if err := validateResearchV2ActionSemantics(
		index,
		action,
		step,
		preconditions,
		postconditions,
	); err != nil {
		return err
	}
	if phase == ValidationPhaseDraft {
		if _, exists := step["locator_candidates"]; exists {
			return fmt.Errorf(
				"case.steps[%d].locator_candidates must be added by the compiler",
				index,
			)
		}
		if _, exists := step["semantic_target"]; exists {
			return fmt.Errorf(
				"case.steps[%d].semantic_target must be added by the compiler",
				index,
			)
		}
		return nil
	}
	if hasV2Target(action) {
		if err := requireBoundedText(
			step,
			"semantic_target",
			index,
			500,
		); err != nil {
			return err
		}
	}
	if requiresV2TargetBinding(action) {
		if err := requireBoundedText(
			step,
			"target_binding_id",
			index,
			64,
		); err != nil {
			return err
		}
		if err := requireBoundedText(
			step,
			"semantic_target",
			index,
			500,
		); err != nil {
			return err
		}
		if err := validateV2Candidates(index, step["locator_candidates"]); err != nil {
			return err
		}
	}
	return nil
}

func validateResearchV2ActionSemantics(
	index int,
	action string,
	step map[string]any,
	preconditions, postconditions []any,
) error {
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
		return fmt.Errorf(
			"case.steps[%d] requires a postcondition for its action risk",
			index,
		)
	}
	return nil
}

func validateResearchV2ActionFields(
	index int,
	action string,
	step map[string]any,
) error {
	switch action {
	case "goto":
		if err := requireBoundedText(step, "value", index, 2000); err != nil {
			return err
		}
	case "click":
	case "input":
		if _, ok := step["value"].(string); !ok {
			return fmt.Errorf("case.steps[%d].value must be a string", index)
		}
		if trigger, exists := step["trigger"]; exists && trigger != nil {
			value, ok := trigger.(string)
			if !ok || (value != "Enter" && value != "Tab") {
				return fmt.Errorf("case.steps[%d].trigger is invalid", index)
			}
		}
	case "wait_for":
		if timeout, exists := step["timeout_ms"]; exists {
			value, ok := timeout.(float64)
			if !ok || value < 1 || value > 60000 {
				return fmt.Errorf("case.steps[%d].timeout_ms is invalid", index)
			}
		}
	case "assert_text":
		if err := requireBoundedText(step, "value", index, 10000); err != nil {
			return err
		}
	case "assert_url_contains":
		if err := requireBoundedText(step, "value", index, 2000); err != nil {
			return err
		}
	case "capture_text":
		if err := requireBoundedText(step, "context_key", index, 100); err != nil {
			return err
		}
	}
	if requiresV2TargetBinding(action) {
		if err := requireBoundedText(
			step,
			"target_binding_id",
			index,
			64,
		); err != nil {
			return err
		}
	}
	return nil
}

func requiresV2TargetBinding(action string) bool {
	switch action {
	case "click", "input", "capture_text":
		return true
	default:
		return false
	}
}

func hasV2Target(action string) bool {
	switch action {
	case "click", "input", "wait_for", "assert_text", "capture_text":
		return true
	default:
		return false
	}
}

func validateV2Candidates(index int, raw any) error {
	values, ok := raw.([]any)
	if !ok || len(values) == 0 {
		return fmt.Errorf(
			"case.steps[%d].locator_candidates must be a non-empty array",
			index,
		)
	}
	for candidateIndex, value := range values {
		encoded, err := json.Marshal(value)
		if err != nil {
			return err
		}
		var candidate browsercontract.LocatorCandidate
		if err := json.Unmarshal(encoded, &candidate); err != nil {
			return fmt.Errorf(
				"case.steps[%d].locator_candidates[%d] is invalid",
				index,
				candidateIndex,
			)
		}
		if strings.TrimSpace(candidate.CandidateID) == "" ||
			strings.TrimSpace(candidate.ElementRef) == "" ||
			strings.TrimSpace(candidate.Provenance) == "" ||
			candidate.ObservedCount != 1 ||
			!candidate.Visible {
			return fmt.Errorf(
				"case.steps[%d].locator_candidates[%d] is not executable",
				index,
				candidateIndex,
			)
		}
		if err := candidate.Locator.Validate(); err != nil {
			return fmt.Errorf(
				"case.steps[%d].locator_candidates[%d]: %w",
				index,
				candidateIndex,
				err,
			)
		}
	}
	return nil
}
