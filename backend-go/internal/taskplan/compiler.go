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
		// The plan owns the semantic target and the compiler injects it below, so
		// drop any descriptive target the author carried over from the plan
		// rather than letting it leak into the executable case.
		delete(step, "target")
		if planned[index].Target != "" {
			step["semantic_target"] = planned[index].Target
		}
		binding := planned[index].TargetBinding
		if binding == nil {
			continue
		}
		if binding.ProbeID != "" {
			step["probe_id"] = binding.ProbeID
			step["observation_id"] = binding.ObservationID
			step["observation_sha256"] = binding.ObservationSHA256
			step["page_state_id"] = binding.PageStateID
			step["selected_candidate_id"] = binding.SelectedCandidateID
		}
		step["locator_candidates"] = binding.Candidates
		if !seenBinding[binding.BindingID] {
			observationBinding := map[string]any{
				"binding_id":         binding.BindingID,
				"binding_sha256":     binding.BindingSHA256,
				"observation_id":     binding.ObservationID,
				"observation_sha256": binding.ObservationSHA256,
			}
			if binding.ProbeID != "" {
				observationBinding["probe_id"] = binding.ProbeID
				observationBinding["page_state_id"] = binding.PageStateID
			}
			observationBindings = append(
				observationBindings,
				observationBinding,
			)
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
	if mismatch := firstPlanStepMismatch(planStepSemanticChecks(
		stringValue(step["plan_step_id"]),
		stringValue(step["action"]),
		stringValue(step["value"]),
		stringValue(step["trigger"]),
		stringValue(step["context_key"]),
		intValue(step["timeout_ms"]),
		stringValue(step["idempotency"]),
		stringValue(step["side_effect"]),
		planned,
	)); mismatch != "" {
		return fmt.Errorf(
			"DSL step %d does not preserve plan step %q semantics: %s",
			index,
			planned.ID,
			mismatch,
		)
	}
	// `intent` is descriptive free text, so only its presence is required
	// here; see planStepSemanticChecks for why it is not compared verbatim.
	if stringValue(step["intent"]) == "" {
		return fmt.Errorf(
			"DSL step %d for plan step %q must carry a non-empty intent",
			index,
			planned.ID,
		)
	}
	for _, field := range []string{
		"semantic_target",
		"locator_candidates",
		"probe_id",
		"observation_id",
		"observation_sha256",
		"page_state_id",
		"selected_candidate_id",
	} {
		if _, exists := step[field]; exists {
			return fmt.Errorf(
				"case.steps[%d].%s is compiler-owned",
				index,
				field,
			)
		}
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
	if err := validateConditionPreservation(step, planned, index); err != nil {
		return err
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
			PlanStepID          string                             `json:"plan_step_id"`
			Action              string                             `json:"action"`
			Intent              string                             `json:"intent"`
			SemanticTarget      string                             `json:"semantic_target"`
			TargetBindingID     string                             `json:"target_binding_id"`
			ProbeID             string                             `json:"probe_id"`
			ObservationID       string                             `json:"observation_id"`
			ObservationSHA256   string                             `json:"observation_sha256"`
			PageStateID         string                             `json:"page_state_id"`
			SelectedCandidateID string                             `json:"selected_candidate_id"`
			LocatorCandidates   []browsercontract.LocatorCandidate `json:"locator_candidates"`
			Value               string                             `json:"value"`
			Trigger             string                             `json:"trigger"`
			ContextKey          string                             `json:"context_key"`
			TimeoutMS           int                                `json:"timeout_ms"`
			Idempotency         string                             `json:"idempotency"`
			SideEffect          SideEffect                         `json:"side_effect"`
			Preconditions       []map[string]any                   `json:"preconditions"`
			Postconditions      []map[string]any                   `json:"postconditions"`
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
		checks := planStepSemanticChecks(
			step.PlanStepID,
			step.Action,
			step.Value,
			step.Trigger,
			step.ContextKey,
			step.TimeoutMS,
			step.Idempotency,
			string(step.SideEffect),
			expected,
		)
		checks = append(checks, planStepCheck{
			Field:    "semantic_target",
			Actual:   step.SemanticTarget,
			Expected: expected.Target,
		})
		if mismatch := firstPlanStepMismatch(checks); mismatch != "" {
			return fmt.Errorf(
				"compiled DSL step %d does not preserve plan step %q: %s",
				index,
				expected.ID,
				mismatch,
			)
		}
		if strings.TrimSpace(step.Intent) == "" {
			return fmt.Errorf(
				"compiled DSL step %d for plan step %q must carry a non-empty intent",
				index,
				expected.ID,
			)
		}
		if err := validateCompiledConditions(
			index,
			step.Preconditions,
			step.Postconditions,
			expected,
		); err != nil {
			return err
		}
		if requiresTargetBinding(expected.Action) {
			if expected.TargetBinding == nil ||
				step.TargetBindingID != expected.TargetBinding.BindingID ||
				step.ProbeID != expected.TargetBinding.ProbeID ||
				step.ObservationID != expected.TargetBinding.ObservationID ||
				step.ObservationSHA256 !=
					expected.TargetBinding.ObservationSHA256 ||
				step.PageStateID != expected.TargetBinding.PageStateID ||
				step.SelectedCandidateID !=
					expected.TargetBinding.SelectedCandidateID ||
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

func validateCompiledConditions(
	index int,
	preconditions, postconditions []map[string]any,
	expected Step,
) error {
	dslPre := make([]ConditionIntent, 0, len(preconditions))
	for _, condition := range preconditions {
		dslPre = append(dslPre, dslConditionIntent(condition))
	}
	dslPost := make([]ConditionIntent, 0, len(postconditions))
	for _, condition := range postconditions {
		dslPost = append(dslPost, dslConditionIntent(condition))
	}
	if len(expected.Preconditions) > 0 && len(dslPre) == 0 {
		return fmt.Errorf(
			"compiled DSL step %d drops plan step %q preconditions",
			index,
			expected.ID,
		)
	}
	if len(expected.CompletionConditions) > 0 && len(dslPost) == 0 {
		return fmt.Errorf(
			"compiled DSL step %d drops plan step %q completion conditions",
			index,
			expected.ID,
		)
	}
	for _, raw := range expected.Preconditions {
		intent := parseConditionIntent(raw)
		if intent.Kind == "free_text" {
			continue
		}
		if !anyMatches(dslPre, intent) {
			return fmt.Errorf(
				"compiled DSL step %d does not preserve plan step %q precondition %q",
				index,
				expected.ID,
				raw,
			)
		}
	}
	for _, raw := range expected.CompletionConditions {
		intent := parseConditionIntent(raw)
		if intent.Kind == "free_text" {
			continue
		}
		if !anyMatches(dslPost, intent) {
			return fmt.Errorf(
				"compiled DSL step %d does not preserve plan step %q completion condition %q",
				index,
				expected.ID,
				raw,
			)
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

// planStepCheck is one field-level comparison between a DSL step and the plan
// step it claims to implement.
type planStepCheck struct {
	Field    string
	Actual   string
	Expected string
}

// planStepSemanticChecks lists the plan-owned fields that a DSL step must
// reproduce exactly.
//
// `intent` is deliberately NOT among them. It is descriptive free text: the
// DSL validators only require it to be non-empty and bounded, and neither the
// compiler nor the runner consumes it. Comparing it verbatim rejected any
// faithful paraphrase of the plan's own wording -- including the case where the
// plan's intent carried an implementation note in parentheses -- and stalled
// generation until the wall clock expired (BUG-201).
func planStepSemanticChecks(
	planStepID, action, value, trigger, contextKey string,
	timeoutMS int,
	idempotency, sideEffect string,
	expected Step,
) []planStepCheck {
	checks := []planStepCheck{
		{Field: "plan_step_id", Actual: planStepID, Expected: expected.ID},
		{Field: "action", Actual: normalize(action), Expected: expected.Action},
		{Field: "value", Actual: strings.TrimSpace(value), Expected: expected.Value},
		{Field: "trigger", Actual: trigger, Expected: expected.Trigger},
		{Field: "context_key", Actual: contextKey, Expected: expected.ContextKey},
		{Field: "idempotency", Actual: idempotency, Expected: expected.Idempotency},
		{
			Field:    "side_effect",
			Actual:   sideEffect,
			Expected: string(expected.SideEffect),
		},
	}
	// A plan that leaves the timeout unset (0) must not constrain the DSL. The
	// research-v2 validation materializes a default timeout for wait_for steps
	// and rejects an explicit zero, so demanding the plan's 0 made every
	// unset-timeout step uncompilable and deadlocked DSL generation: the model
	// could neither omit the field nor set it to zero.
	if expected.TimeoutMS > 0 {
		checks = append(checks, planStepCheck{
			Field:    "timeout_ms",
			Actual:   fmt.Sprintf("%d", timeoutMS),
			Expected: fmt.Sprintf("%d", expected.TimeoutMS),
		})
	}
	return checks
}

// firstPlanStepMismatch describes the first field that disagrees with the plan
// and returns "" when every field agrees. The message names the field and both
// values so a caller can repair the draft directly: the previous opaque
// "does not preserve semantics" error left the model guessing which field was
// wrong, and every guess cost a full turn (BUG-201).
func firstPlanStepMismatch(checks []planStepCheck) string {
	for _, check := range checks {
		if check.Actual != check.Expected {
			return fmt.Sprintf(
				"field %q is %q but the plan requires %q",
				check.Field,
				check.Actual,
				check.Expected,
			)
		}
	}
	return ""
}
