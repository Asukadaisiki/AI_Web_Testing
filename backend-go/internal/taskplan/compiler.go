package taskplan

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/browsercontract"
)

func (s *Service) CompileDraftCase(
	ctx context.Context,
	runID string,
	binding Binding,
	raw json.RawMessage,
) (json.RawMessage, Plan, error) {
	plan, err := s.repository.GetCurrent(ctx, runID)
	if err != nil {
		return nil, Plan{}, err
	}
	if binding != plan.Binding() {
		return nil, Plan{}, errors.New(
			"plan binding does not match the current task plan",
		)
	}
	if plan.Status != StatusReadyForGeneration {
		return nil, Plan{}, fmt.Errorf(
			"task plan status %q does not allow DSL compilation",
			plan.Status,
		)
	}
	var candidate map[string]any
	if json.Unmarshal(raw, &candidate) != nil || candidate == nil {
		return nil, Plan{}, errors.New("DSL draft must be an object")
	}
	if candidate["profile"] != "research-v2" {
		return nil, Plan{}, errors.New(
			"task plan compiler requires research-v2",
		)
	}
	steps, ok := candidate["steps"].([]any)
	if !ok {
		return nil, Plan{}, errors.New("DSL draft steps must be an array")
	}
	planned := expandedPlanSteps(plan.Steps)
	if len(steps) != len(planned) {
		return nil, Plan{}, fmt.Errorf(
			"DSL has %d steps, task plan requires %d occurrences",
			len(steps),
			len(planned),
		)
	}
	observationBindings := make([]any, 0)
	seenBinding := make(map[string]bool)
	for index, rawStep := range steps {
		step, ok := rawStep.(map[string]any)
		if !ok {
			return nil, Plan{}, fmt.Errorf(
				"case.steps[%d] must be an object",
				index,
			)
		}
		if err := compileDraftStep(step, planned[index], index); err != nil {
			return nil, Plan{}, err
		}
		if planned[index].Target != "" {
			step["semantic_target"] = planned[index].Target
		}
		binding := planned[index].TargetBinding
		if binding == nil {
			continue
		}
		step["locator_candidates"] = binding.Candidates
		if !seenBinding[binding.BindingID] {
			observationBindings = append(observationBindings, map[string]any{
				"binding_id":         binding.BindingID,
				"binding_sha256":     binding.BindingSHA256,
				"observation_id":     binding.ObservationID,
				"observation_sha256": binding.ObservationSHA256,
			})
			seenBinding[binding.BindingID] = true
		}
	}
	candidate["plan_binding"] = binding
	candidate["observation_bindings"] = observationBindings
	compiled, err := json.Marshal(candidate)
	if err != nil {
		return nil, Plan{}, err
	}
	return compiled, plan, nil
}

func validateDraftCaseSemantics(plan Plan, raw json.RawMessage) error {
	var candidate struct {
		Profile string           `json:"profile"`
		Steps   []map[string]any `json:"steps"`
	}
	if json.Unmarshal(raw, &candidate) != nil ||
		candidate.Profile != "research-v2" {
		return errors.New("research-v2 DSL draft is invalid")
	}
	planned := expandedPlanSteps(plan.Steps)
	if len(candidate.Steps) != len(planned) {
		return fmt.Errorf(
			"DSL has %d steps, task plan requires %d occurrences",
			len(candidate.Steps),
			len(planned),
		)
	}
	for index := range candidate.Steps {
		if err := compileDraftStep(
			candidate.Steps[index],
			planned[index],
			index,
		); err != nil {
			return err
		}
	}
	return nil
}

func compileDraftStep(
	step map[string]any,
	planned Step,
	index int,
) error {
	if stringValue(step["plan_step_id"]) != planned.ID ||
		normalize(stringValue(step["action"])) != planned.Action ||
		normalize(stringValue(step["intent"])) != normalize(planned.Intent) ||
		strings.TrimSpace(stringValue(step["value"])) != planned.Value ||
		stringValue(step["trigger"]) != planned.Trigger ||
		stringValue(step["context_key"]) != planned.ContextKey ||
		intValue(step["timeout_ms"]) != planned.TimeoutMS ||
		stringValue(step["idempotency"]) != planned.Idempotency ||
		SideEffect(stringValue(step["side_effect"])) != planned.SideEffect {
		return fmt.Errorf(
			"DSL step %d does not preserve plan step %q semantics",
			index,
			planned.ID,
		)
	}
	if _, exists := step["locator_candidates"]; exists {
		return fmt.Errorf(
			"case.steps[%d].locator_candidates are compiler-owned",
			index,
		)
	}
	if _, exists := step["semantic_target"]; exists {
		return fmt.Errorf(
			"case.steps[%d].semantic_target is compiler-owned",
			index,
		)
	}
	if requiresTargetBinding(planned.Action) {
		if planned.TargetBinding == nil {
			return fmt.Errorf(
				"plan step %q has no grounded target binding",
				planned.ID,
			)
		}
		if err := planned.TargetBinding.Validate(); err != nil {
			return fmt.Errorf(
				"plan step %q target binding: %w",
				planned.ID,
				err,
			)
		}
		if stringValue(step["target_binding_id"]) !=
			planned.TargetBinding.BindingID {
			return fmt.Errorf(
				"case.steps[%d].target_binding_id does not match plan step %q",
				index,
				planned.ID,
			)
		}
	} else if planned.TargetBinding != nil {
		if value := stringValue(step["target_binding_id"]); value != "" &&
			value != planned.TargetBinding.BindingID {
			return fmt.Errorf(
				"case.steps[%d].target_binding_id does not match plan step %q",
				index,
				planned.ID,
			)
		}
	}
	return nil
}

func expandedPlanSteps(steps []Step) []Step {
	result := make([]Step, 0, len(steps))
	for _, step := range steps {
		for range step.ExpectedOccurrences {
			result = append(result, step)
		}
	}
	return result
}

func requiresTargetBinding(action string) bool {
	switch action {
	case "click", "input", "capture_text":
		return true
	default:
		return false
	}
}

func validateCompiledCaseSemantics(plan Plan, raw json.RawMessage) error {
	var candidate struct {
		Profile     string  `json:"profile"`
		PlanBinding Binding `json:"plan_binding"`
		Steps       []struct {
			PlanStepID        string                             `json:"plan_step_id"`
			Action            string                             `json:"action"`
			Intent            string                             `json:"intent"`
			SemanticTarget    string                             `json:"semantic_target"`
			TargetBindingID   string                             `json:"target_binding_id"`
			LocatorCandidates []browsercontract.LocatorCandidate `json:"locator_candidates"`
			Value             string                             `json:"value"`
			Trigger           string                             `json:"trigger"`
			ContextKey        string                             `json:"context_key"`
			TimeoutMS         int                                `json:"timeout_ms"`
			Idempotency       string                             `json:"idempotency"`
			SideEffect        SideEffect                         `json:"side_effect"`
		} `json:"steps"`
	}
	if json.Unmarshal(raw, &candidate) != nil ||
		candidate.Profile != "research-v2" ||
		candidate.PlanBinding != plan.Binding() {
		return errors.New("compiled DSL does not match the task plan binding")
	}
	planned := expandedPlanSteps(plan.Steps)
	if len(candidate.Steps) != len(planned) {
		return errors.New("compiled DSL step count does not match the task plan")
	}
	for index, step := range candidate.Steps {
		expected := planned[index]
		if step.PlanStepID != expected.ID ||
			normalize(step.Action) != expected.Action ||
			normalize(step.Intent) != normalize(expected.Intent) ||
			step.SemanticTarget != expected.Target ||
			step.Value != expected.Value ||
			step.Trigger != expected.Trigger ||
			step.ContextKey != expected.ContextKey ||
			step.TimeoutMS != expected.TimeoutMS ||
			step.Idempotency != expected.Idempotency ||
			step.SideEffect != expected.SideEffect {
			return fmt.Errorf(
				"compiled DSL step %d does not preserve plan step %q",
				index,
				expected.ID,
			)
		}
		if requiresTargetBinding(expected.Action) {
			if expected.TargetBinding == nil ||
				step.TargetBindingID != expected.TargetBinding.BindingID ||
				!sameCandidates(
					step.LocatorCandidates,
					expected.TargetBinding.Candidates,
				) {
				return fmt.Errorf(
					"compiled DSL step %d has an invalid target binding",
					index,
				)
			}
		}
	}
	return nil
}

func sameCandidates(
	left []browsercontract.LocatorCandidate,
	right []browsercontract.LocatorCandidate,
) bool {
	leftJSON, _ := json.Marshal(left)
	rightJSON, _ := json.Marshal(right)
	return string(leftJSON) == string(rightJSON)
}

func stringValue(value any) string {
	if value == nil {
		return ""
	}
	text, _ := value.(string)
	return strings.TrimSpace(text)
}

func intValue(value any) int {
	switch typed := value.(type) {
	case float64:
		return int(typed)
	case int:
		return typed
	default:
		return 0
	}
}
