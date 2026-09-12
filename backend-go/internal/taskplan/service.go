package taskplan

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/browsercontract"
)

var stepIDPattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]{0,63}$`)

type Service struct {
	repository Repository
	now        func() time.Time
}

func NewService(repository Repository) *Service {
	return &Service{repository: repository, now: time.Now}
}

func (s *Service) CreateVersion(ctx context.Context, request CreateRequest) (Plan, error) {
	if strings.TrimSpace(request.RunID) == "" {
		return Plan{}, errors.New("task plan run_id is required")
	}
	definition, err := validateDefinition(request.Definition)
	if err != nil {
		return Plan{}, err
	}
	hash, err := semanticHash(definition)
	if err != nil {
		return Plan{}, err
	}
	steps := make([]Step, len(definition.Steps))
	for index, step := range definition.Steps {
		steps[index] = Step{
			ID: step.ID, Position: index, Intent: step.Intent, Action: step.Action,
			Target: step.Target, Value: step.Value, Trigger: step.Trigger,
			ContextKey: step.ContextKey, TimeoutMS: step.TimeoutMS,
			ExpectedOccurrences: step.ExpectedOccurrences,
			Idempotency:         step.Idempotency, SideEffect: step.SideEffect,
			Preconditions:        step.Preconditions,
			CompletionConditions: step.CompletionConditions,
			Status:               StepPending, Evidence: []EvidenceRef{},
		}
	}
	now := s.now().UTC()
	plan := Plan{
		ID: planID(request.RunID, hash, now), SchemaVersion: SchemaVersion,
		RunID: request.RunID, ActorUserID: request.ActorUserID,
		ProjectID: request.ProjectID, Goal: definition.Goal,
		Status: StatusGrounding, PlanSHA256: hash,
		MaxSideEffect:    definition.MaxSideEffect,
		ForbiddenActions: definition.ForbiddenActions,
		Steps:            steps, CreatedAt: now, UpdatedAt: now,
	}
	previous, err := s.repository.GetCurrent(ctx, request.RunID)
	if err == nil {
		carryForwardGrounding(&plan, previous)
	} else if !errors.Is(err, ErrNotFound) {
		return Plan{}, err
	}
	created, err := s.repository.CreateVersion(ctx, plan)
	if err != nil {
		return Plan{}, err
	}
	if rebindCarriedTargetBindings(&created) {
		if err := s.repository.Save(ctx, created); err != nil {
			return Plan{}, err
		}
	}
	return created, nil
}

func (s *Service) GetCurrent(ctx context.Context, runID string) (Plan, error) {
	return s.repository.GetCurrent(ctx, runID)
}

func (s *Service) Authorize(
	ctx context.Context,
	runID string,
	tool string,
	arguments json.RawMessage,
) error {
	if tool == "set_task_plan" {
		current, err := s.repository.GetCurrent(ctx, runID)
		if errors.Is(err, ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		switch current.Status {
		case StatusApproved, StatusExecuting, StatusCompleted:
			return fmt.Errorf(
				"task plan status %q does not allow semantic revision",
				current.Status,
			)
		}
		return nil
	}
	plan, err := s.repository.GetCurrent(ctx, runID)
	if err != nil {
		if errors.Is(err, ErrNotFound) && tool == "ask_user_question" {
			return nil
		}
		return fmt.Errorf("task plan required before %s: %w", tool, err)
	}
	switch tool {
	case "explore_page", "explore_flow":
		return authorizeExploration(plan, tool, arguments)
	case "generate_dsl":
		if plan.Status != StatusReadyForGeneration {
			return fmt.Errorf(
				"generate_dsl requires task plan status %q, got %q",
				StatusReadyForGeneration,
				plan.Status,
			)
		}
		return validateGenerationArguments(plan, arguments)
	case "execute_dsl":
		if plan.Status != StatusApproved || plan.BoundGenerationID == nil {
			return errors.New("execute_dsl requires an approved task plan with a bound generation")
		}
		var request struct {
			GenerationID int64 `json:"generation_id"`
		}
		if json.Unmarshal(arguments, &request) != nil ||
			request.GenerationID != *plan.BoundGenerationID {
			return errors.New("execute_dsl generation_id does not match the task plan")
		}
	case "get_report":
		if plan.Status != StatusExecuting {
			return fmt.Errorf("get_report requires task plan status %q", StatusExecuting)
		}
	case "fix_and_retry":
		if plan.Status != StatusFailed {
			return fmt.Errorf("fix_and_retry requires task plan status %q", StatusFailed)
		}
	}
	return nil
}

func (s *Service) RecordToolResult(
	ctx context.Context,
	runID string,
	tool string,
	arguments json.RawMessage,
	result json.RawMessage,
	eventSeq int64,
) error {
	if tool == "set_task_plan" ||
		tool == "ask_user_question" ||
		tool == "validate_page_elements" {
		return nil
	}
	plan, err := s.repository.GetCurrent(ctx, runID)
	if err != nil {
		return err
	}
	switch tool {
	case "explore_page", "explore_flow":
		ids, err := planStepIDs(arguments)
		if err != nil {
			return err
		}
		pendingSteps, err := contiguousPendingSteps(plan, ids)
		if err != nil {
			return err
		}
		hash := sha256.Sum256(result)
		evidence := successfulExplorationEvidence(result)
		groundedActions := map[string]bool{}
		targetBindings := map[string]*browsercontract.TargetBinding{}
		if tool == "explore_flow" {
			actionOwners, mapErr := mapProbeActionOwners(
				plan,
				pendingSteps,
				arguments,
			)
			if mapErr != nil {
				return mapErr
			}
			for key := range successfulFlowActions(result) {
				if owner := actionOwners[key]; owner != "" {
					groundedActions[owner] = true
				}
			}
			targetBindings = deriveTargetBindings(
				plan,
				actionOwners,
				result,
			)
		} else {
			targetBindings = derivePageTargetBindings(
				plan,
				pendingSteps,
				result,
			)
		}
		for _, pending := range pendingSteps {
			index := planStepIndex(plan.Steps, pending.ID)
			if index < 0 {
				return fmt.Errorf("task plan step %q disappeared", pending.ID)
			}
			if tool == "explore_flow" &&
				(plan.Steps[index].Action == "click" ||
					plan.Steps[index].Action == "input") &&
				!groundedActions[plan.Steps[index].ID] {
				break
			}
			if !resultContainsStepEvidence(evidence, plan.Steps[index]) &&
				!groundedActions[plan.Steps[index].ID] {
				break
			}
			if requiresTargetBinding(plan.Steps[index].Action) &&
				targetBindings[plan.Steps[index].ID] == nil {
				break
			}
			plan.Steps[index].Status = StepGrounded
			plan.Steps[index].GroundingAttempts++
			if binding := targetBindings[plan.Steps[index].ID]; binding != nil {
				plan.Steps[index].TargetBinding = binding
			}
			plan.Steps[index].Evidence = append(
				plan.Steps[index].Evidence,
				EvidenceRef{
					Tool: tool, EventSeq: eventSeq,
					ContentSHA256: hex.EncodeToString(hash[:]),
				},
			)
		}
		if allGrounded(plan.Steps) {
			plan.Status = StatusReadyForGeneration
		}
	case "generate_dsl":
		var value struct {
			GenerationID int64 `json:"generation_id"`
		}
		if json.Unmarshal(result, &value) != nil || value.GenerationID < 1 {
			return errors.New("generate_dsl result has no generation_id")
		}
		plan.BoundGenerationID = &value.GenerationID
		plan.Status = StatusAwaitingApproval
	case "execute_dsl":
		plan.Status = StatusExecuting
	case "get_report":
		var value struct {
			Status string `json:"status"`
		}
		if json.Unmarshal(result, &value) != nil {
			return errors.New("get_report result has no status")
		}
		switch value.Status {
		case "passed":
			plan.Status = StatusCompleted
		case "failed", "cancelled", "needs_intervention":
			plan.Status = StatusFailed
		}
	case "fix_and_retry":
		var value struct {
			Strategy string `json:"strategy"`
		}
		if json.Unmarshal(result, &value) != nil {
			return errors.New("fix_and_retry result has no strategy")
		}
		plan.BoundGenerationID = nil
		switch value.Strategy {
		case "re_explore":
			plan.Status = StatusGrounding
			for index := range plan.Steps {
				plan.Steps[index].Status = StepPending
			}
		case "regenerate_dsl":
			plan.Status = StatusReadyForGeneration
		case "wait_execution":
			plan.Status = StatusExecuting
		default:
			plan.Status = StatusBlocked
		}
	}
	plan.UpdatedAt = s.now().UTC()
	return s.repository.Save(ctx, plan)
}

func (s *Service) RecordToolFailure(
	ctx context.Context,
	runID string,
	tool string,
	message string,
) error {
	plan, err := s.repository.GetCurrent(ctx, runID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil
		}
		return err
	}
	switch tool {
	case "generate_dsl":
		normalized := normalize(message)
		if strings.Contains(normalized, "locator preflight") ||
			strings.Contains(normalized, "matched 0 elements") ||
			strings.Contains(normalized, "evidence") {
			plan.Status = StatusGrounding
			matched := false
			for index := range plan.Steps {
				if plan.Steps[index].Target != "" &&
					strings.Contains(
						normalized,
						normalize(plan.Steps[index].Target),
					) {
					plan.Steps[index].Status = StepPending
					matched = true
				}
			}
			if !matched {
				for index := range plan.Steps {
					plan.Steps[index].Status = StepPending
				}
			}
		}
	case "execute_dsl":
		plan.Status = StatusFailed
	}
	plan.UpdatedAt = s.now().UTC()
	return s.repository.Save(ctx, plan)
}

func (s *Service) Approve(
	ctx context.Context,
	runID string,
	generationID int64,
) error {
	plan, err := s.repository.GetCurrent(ctx, runID)
	if err != nil {
		return err
	}
	if plan.Status != StatusAwaitingApproval ||
		plan.BoundGenerationID == nil ||
		*plan.BoundGenerationID != generationID {
		return errors.New("approval does not match the current task plan generation")
	}
	plan.Status = StatusApproved
	plan.UpdatedAt = s.now().UTC()
	return s.repository.Save(ctx, plan)
}

func (s *Service) MarkFailed(
	ctx context.Context,
	runID string,
) error {
	return s.markTerminal(ctx, runID, StatusFailed)
}

func (s *Service) MarkBlocked(
	ctx context.Context,
	runID string,
) error {
	return s.markTerminal(ctx, runID, StatusBlocked)
}

func (s *Service) markTerminal(
	ctx context.Context,
	runID string,
	status Status,
) error {
	plan, err := s.repository.GetCurrent(ctx, runID)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if plan.Status == StatusCompleted ||
		plan.Status == StatusSuperseded {
		return nil
	}
	plan.Status = status
	plan.UpdatedAt = s.now().UTC()
	return s.repository.Save(ctx, plan)
}

func (s *Service) ValidateGenerationBinding(
	ctx context.Context,
	runID string,
	binding Binding,
	caseJSON json.RawMessage,
) (Plan, error) {
	plan, err := s.repository.GetCurrent(ctx, runID)
	if err != nil {
		return Plan{}, err
	}
	if binding != plan.Binding() {
		return Plan{}, errors.New("plan binding does not match the current task plan")
	}
	if plan.Status != StatusReadyForGeneration &&
		plan.Status != StatusAwaitingApproval &&
		plan.Status != StatusApproved &&
		plan.Status != StatusExecuting {
		return Plan{}, fmt.Errorf(
			"task plan status %q does not allow DSL binding",
			plan.Status,
		)
	}
	var envelope struct {
		Profile string `json:"profile"`
	}
	if json.Unmarshal(caseJSON, &envelope) == nil &&
		envelope.Profile == "research-v2" {
		if err := validateCompiledCaseSemantics(plan, caseJSON); err != nil {
			return Plan{}, err
		}
		return plan, nil
	}
	if err := validateCaseSemantics(plan, caseJSON); err != nil {
		return Plan{}, err
	}
	return plan, nil
}

func validateDefinition(source Definition) (Definition, error) {
	source.Goal = strings.TrimSpace(source.Goal)
	if source.Goal == "" {
		return Definition{}, errors.New("task plan goal is required")
	}
	if !validSideEffect(source.MaxSideEffect) {
		return Definition{}, errors.New("task plan max_side_effect is invalid")
	}
	if len(source.Steps) == 0 {
		return Definition{}, errors.New("task plan requires at least one step")
	}
	seen := make(map[string]bool)
	forbiddenSeen := make(map[string]bool)
	for index := range source.ForbiddenActions {
		source.ForbiddenActions[index] = normalize(source.ForbiddenActions[index])
		if source.ForbiddenActions[index] == "" {
			return Definition{}, errors.New("forbidden_actions must not contain empty values")
		}
		if forbiddenSeen[source.ForbiddenActions[index]] {
			return Definition{}, fmt.Errorf(
				"duplicate forbidden action %q",
				source.ForbiddenActions[index],
			)
		}
		forbiddenSeen[source.ForbiddenActions[index]] = true
	}
	sort.Strings(source.ForbiddenActions)
	for index := range source.Steps {
		step := &source.Steps[index]
		step.ID = strings.TrimSpace(step.ID)
		step.Intent = strings.TrimSpace(step.Intent)
		step.Action = normalize(step.Action)
		step.Target = strings.TrimSpace(step.Target)
		step.Value = strings.TrimSpace(step.Value)
		step.Trigger = strings.TrimSpace(step.Trigger)
		step.ContextKey = strings.TrimSpace(step.ContextKey)
		if !stepIDPattern.MatchString(step.ID) || seen[step.ID] {
			return Definition{}, fmt.Errorf(
				"task plan step %d has an invalid or duplicate id",
				index,
			)
		}
		seen[step.ID] = true
		if step.Intent == "" ||
			!validAction(step.Action) ||
			!validSideEffect(step.SideEffect) {
			return Definition{}, fmt.Errorf(
				"task plan step %q has invalid semantics",
				step.ID,
			)
		}
		if sideEffectRank(step.SideEffect) >
			sideEffectRank(source.MaxSideEffect) {
			return Definition{}, fmt.Errorf(
				"task plan step %q exceeds max_side_effect",
				step.ID,
			)
		}
		if step.ExpectedOccurrences == 0 {
			step.ExpectedOccurrences = 1
		}
		if step.ExpectedOccurrences < 1 ||
			(step.Idempotency != "idempotent" &&
				step.Idempotency != "non_idempotent") {
			return Definition{}, fmt.Errorf(
				"task plan step %q has invalid occurrence or idempotency",
				step.ID,
			)
		}
		if err := validateActionParameters(*step); err != nil {
			return Definition{}, fmt.Errorf(
				"task plan step %q: %w",
				step.ID,
				err,
			)
		}
		if forbidden(
			source.ForbiddenActions,
			step.Action+" "+step.Target+" "+step.Intent,
		) {
			return Definition{}, fmt.Errorf(
				"task plan step %q violates forbidden_actions",
				step.ID,
			)
		}
		step.Preconditions = cleanStrings(step.Preconditions)
		step.CompletionConditions = cleanStrings(step.CompletionConditions)
	}
	return source, nil
}

func semanticHash(definition Definition) (string, error) {
	raw, err := json.Marshal(struct {
		SchemaVersion string     `json:"schema_version"`
		Definition    Definition `json:"definition"`
	}{SchemaVersion: SchemaVersion, Definition: definition})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func carryForwardGrounding(next *Plan, previous Plan) {
	previousByID := make(map[string]Step, len(previous.Steps))
	for _, step := range previous.Steps {
		previousByID[step.ID] = step
	}
	for index := range next.Steps {
		previousStep, exists := previousByID[next.Steps[index].ID]
		if !exists ||
			previousStep.Status != StepGrounded ||
			!sameStepSemantics(previousStep, next.Steps[index]) {
			continue
		}
		next.Steps[index].Status = StepGrounded
		next.Steps[index].GroundingAttempts =
			previousStep.GroundingAttempts
		next.Steps[index].Evidence =
			append([]EvidenceRef(nil), previousStep.Evidence...)
		if previousStep.TargetBinding != nil {
			binding := *previousStep.TargetBinding
			next.Steps[index].TargetBinding = &binding
		}
	}
	if allGrounded(next.Steps) {
		next.Status = StatusReadyForGeneration
	}
}

func rebindCarriedTargetBindings(plan *Plan) bool {
	changed := false
	for index := range plan.Steps {
		if plan.Steps[index].TargetBinding == nil {
			continue
		}
		binding := *plan.Steps[index].TargetBinding
		binding.PlanID = plan.ID
		binding.PlanVersion = plan.Version
		binding.PlanStepID = plan.Steps[index].ID
		rebound, err := browsercontract.NewTargetBinding(binding)
		if err != nil {
			plan.Steps[index].TargetBinding = nil
			plan.Steps[index].Status = StepPending
			continue
		}
		plan.Steps[index].TargetBinding = &rebound
		changed = true
	}
	return changed
}

func sameStepSemantics(left Step, right Step) bool {
	return left.Intent == right.Intent &&
		left.Action == right.Action &&
		left.Target == right.Target &&
		left.Value == right.Value &&
		left.Trigger == right.Trigger &&
		left.ContextKey == right.ContextKey &&
		left.TimeoutMS == right.TimeoutMS &&
		left.ExpectedOccurrences == right.ExpectedOccurrences &&
		left.Idempotency == right.Idempotency &&
		left.SideEffect == right.SideEffect &&
		equalStrings(left.Preconditions, right.Preconditions) &&
		equalStrings(
			left.CompletionConditions,
			right.CompletionConditions,
		)
}

func equalStrings(left []string, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func planID(runID string, hash string, now time.Time) string {
	sum := sha256.Sum256([]byte(
		runID + "\x00" + hash + "\x00" + now.Format(time.RFC3339Nano),
	))
	return "plan_" + hex.EncodeToString(sum[:16])
}

func authorizeExploration(
	plan Plan,
	tool string,
	arguments json.RawMessage,
) error {
	if plan.Status != StatusGrounding {
		return fmt.Errorf(
			"%s requires task plan status %q, got %q",
			tool,
			StatusGrounding,
			plan.Status,
		)
	}
	ids, err := planStepIDs(arguments)
	if err != nil {
		return err
	}
	steps, err := contiguousPendingSteps(plan, ids)
	if err != nil {
		return err
	}
	if tool != "explore_flow" {
		return nil
	}
	_, err = mapProbeActionOwners(plan, steps, arguments)
	return err
}

type probeFlowRequest struct {
	Steps []struct {
		Description string `json:"description"`
		Actions     []struct {
			PlanStepID string `json:"plan_step_id"`
			Action     string `json:"action"`
			Locator    any    `json:"locator"`
			Value      string `json:"value"`
			Condition  struct {
				Type     string `json:"type"`
				Expected string `json:"expected"`
			} `json:"condition"`
		} `json:"actions"`
	} `json:"steps"`
}

func mapProbeActionOwners(
	plan Plan,
	pending []Step,
	arguments json.RawMessage,
) (map[string]string, error) {
	var request probeFlowRequest
	if json.Unmarshal(arguments, &request) != nil {
		return nil, errors.New("invalid explore_flow arguments")
	}
	owners := make(map[string]string)
	for stepIndex, group := range request.Steps {
		for actionIndex, action := range group.Actions {
			if action.Locator == nil {
				return nil, errors.New(
					"explore_flow action requires structured locator",
				)
			}
			value := action.Value
			if value == "" &&
				strings.EqualFold(action.Condition.Type, "value_equals") {
				value = action.Condition.Expected
			}
			locatorJSON, _ := json.Marshal(action.Locator)
			haystack := action.Action + " " + string(locatorJSON) + " " +
				value + " " + group.Description
			if forbidden(plan.ForbiddenActions, haystack) {
				return nil, errors.New("explore_flow contains a forbidden action")
			}
			if action.PlanStepID == "" {
				if normalize(action.Action) != "wait_for" {
					return nil, fmt.Errorf(
						"explore_flow %s action requires plan_step_id",
						action.Action,
					)
				}
				continue
			}
			matched, ok := stepByID(plan.Steps, action.PlanStepID)
			if !ok {
				return nil, fmt.Errorf(
					"explore_flow action references unknown plan step %q",
					action.PlanStepID,
				)
			}
			if !probeActionMatches(matched.Action, action.Action) ||
				!probeValueMatches(matched, action.Action, value) {
				return nil, fmt.Errorf(
					"explore_flow action %q does not match plan step %q",
					action.Action,
					action.PlanStepID,
				)
			}
			if normalize(action.Action) != "wait_for" &&
				(matched.SideEffect == SideEffectExternal ||
					matched.SideEffect == SideEffectUnknown) {
				return nil, fmt.Errorf(
					"plan step %q cannot be probed because side_effect is %q",
					matched.ID,
					matched.SideEffect,
				)
			}
			if matched.Status == StepGrounded {
				continue
			}
			if _, ok := stepByID(pending, matched.ID); !ok {
				return nil, fmt.Errorf(
					"explore_flow action references unbound plan step %q",
					action.PlanStepID,
				)
			}
			owners[probeActionKey(stepIndex, actionIndex)] =
				matched.ID
		}
	}
	return owners, nil
}

func stepByID(steps []Step, id string) (Step, bool) {
	for _, step := range steps {
		if step.ID == id {
			return step, true
		}
	}
	return Step{}, false
}

func validateGenerationArguments(
	plan Plan,
	arguments json.RawMessage,
) error {
	var request struct {
		PlanBinding Binding         `json:"plan_binding"`
		Case        json.RawMessage `json:"case"`
	}
	if json.Unmarshal(arguments, &request) != nil {
		return errors.New("invalid generate_dsl arguments")
	}
	if request.PlanBinding != plan.Binding() {
		return errors.New(
			"generate_dsl plan_binding does not match the current task plan",
		)
	}
	var envelope struct {
		Profile string `json:"profile"`
	}
	if json.Unmarshal(request.Case, &envelope) == nil &&
		envelope.Profile == "research-v2" {
		return validateDraftCaseSemantics(plan, request.Case)
	}
	return validateCaseSemantics(plan, request.Case)
}

func validateCaseSemantics(plan Plan, raw json.RawMessage) error {
	var candidate struct {
		Steps []struct {
			Action      string     `json:"action"`
			Intent      string     `json:"intent"`
			Target      string     `json:"target"`
			Value       string     `json:"value"`
			Trigger     string     `json:"trigger"`
			ContextKey  string     `json:"context_key"`
			TimeoutMS   int        `json:"timeout_ms"`
			Idempotency string     `json:"idempotency"`
			SideEffect  SideEffect `json:"side_effect"`
		} `json:"steps"`
	}
	if json.Unmarshal(raw, &candidate) != nil {
		return errors.New("DSL steps do not preserve the task plan")
	}
	expectedCount := 0
	for _, step := range plan.Steps {
		expectedCount += step.ExpectedOccurrences
	}
	if len(candidate.Steps) != expectedCount {
		return fmt.Errorf(
			"DSL has %d steps, task plan requires %d occurrences",
			len(candidate.Steps),
			expectedCount,
		)
	}
	dslIndex := 0
	for _, planned := range plan.Steps {
		for occurrence := 0; occurrence < planned.ExpectedOccurrences; occurrence++ {
			step := candidate.Steps[dslIndex]
			if normalize(step.Action) != planned.Action ||
				normalize(step.Intent) != normalize(planned.Intent) ||
				normalize(step.Target) != normalize(planned.Target) ||
				strings.TrimSpace(step.Value) != planned.Value ||
				step.Trigger != planned.Trigger ||
				step.ContextKey != planned.ContextKey ||
				step.TimeoutMS != planned.TimeoutMS ||
				step.Idempotency != planned.Idempotency ||
				step.SideEffect != planned.SideEffect {
				return fmt.Errorf(
					"DSL step %d does not preserve plan step %q semantics",
					dslIndex,
					planned.ID,
				)
			}
			if forbidden(
				plan.ForbiddenActions,
				step.Action+" "+step.Target+" "+step.Intent,
			) {
				return fmt.Errorf(
					"DSL step %d violates forbidden_actions",
					dslIndex,
				)
			}
			dslIndex++
		}
	}
	return nil
}

func planStepIDs(arguments json.RawMessage) ([]string, error) {
	var request struct {
		PlanStepIDs []string `json:"plan_step_ids"`
	}
	if json.Unmarshal(arguments, &request) != nil ||
		len(request.PlanStepIDs) == 0 {
		return nil, errors.New("plan_step_ids is required")
	}
	return request.PlanStepIDs, nil
}

func contiguousPendingSteps(plan Plan, ids []string) ([]Step, error) {
	first := -1
	for index, step := range plan.Steps {
		if step.Status != StepGrounded {
			first = index
			break
		}
	}
	if first < 0 {
		return nil, errors.New("all task plan steps are already grounded")
	}
	firstSubmitted := -1
	for index, step := range plan.Steps {
		if step.ID == ids[0] {
			firstSubmitted = index
			break
		}
	}
	if firstSubmitted != first {
		return nil, fmt.Errorf(
			"expected next plan step %q, got %q",
			plan.Steps[first].ID,
			ids[0],
		)
	}
	if firstSubmitted+len(ids) > len(plan.Steps) {
		return nil, errors.New("plan_step_ids exceed task plan steps")
	}
	for offset, id := range ids {
		step := plan.Steps[firstSubmitted+offset]
		if step.ID != id {
			return nil, fmt.Errorf(
				"plan_step_ids must be contiguous: expected %q, got %q",
				step.ID,
				id,
			)
		}
	}
	return append(
		[]Step(nil),
		plan.Steps[firstSubmitted:firstSubmitted+len(ids)]...,
	), nil
}

func isSelectorTarget(target string) bool {
	target = strings.TrimSpace(target)
	return strings.HasPrefix(target, "#") ||
		strings.HasPrefix(target, ".") ||
		strings.HasPrefix(target, "[") ||
		strings.HasPrefix(target, "//") ||
		strings.HasPrefix(target, "css=") ||
		strings.HasPrefix(target, "xpath=")
}

func probeActionMatches(planned string, actual string) bool {
	planned = normalize(planned)
	actual = normalize(actual)
	if planned == actual {
		return true
	}
	return actual == "wait_for" &&
		(requiresTargetBinding(planned) ||
			planned == "assert_text" ||
			planned == "assert_url_contains" ||
			planned == "capture_text")
}

func semanticTargetMatches(planned string, actual string) bool {
	planned = normalize(planned)
	actual = normalize(actual)
	if planned == actual {
		return true
	}
	if len([]rune(planned)) < 3 || len([]rune(actual)) < 3 {
		return false
	}
	return strings.Contains(planned, actual) ||
		strings.Contains(actual, planned)
}

func probeValueMatches(step Step, action string, value string) bool {
	if normalize(action) == "wait_for" &&
		normalize(step.Action) != "wait_for" {
		return true
	}
	return step.Value == "" ||
		strings.TrimSpace(step.Value) == strings.TrimSpace(value)
}

func resultContainsStepEvidence(decoded any, step Step) bool {
	for _, expected := range []string{step.Target, step.Value} {
		if candidate := normalize(expected); candidate != "" &&
			jsonContainsText(decoded, candidate) {
			return true
		}
	}
	return step.Action == "goto" &&
		jsonContainsText(decoded, normalize(step.Intent))
}

func successfulExplorationEvidence(raw json.RawMessage) []any {
	var result struct {
		URL         string `json:"url"`
		Status      string `json:"status"`
		A11yNodes   []any  `json:"a11y_nodes"`
		Observation any    `json:"observation_v2"`
		Pages       []struct {
			URL         string `json:"url"`
			Status      string `json:"status"`
			A11yNodes   []any  `json:"a11y_nodes"`
			Observation any    `json:"observation_v2"`
			Actions     []struct {
				Action         string `json:"action"`
				Target         string `json:"target"`
				Status         string `json:"status"`
				Phase          string `json:"phase"`
				TargetEvidence []any  `json:"target_evidence"`
			} `json:"actions"`
		} `json:"pages"`
	}
	if json.Unmarshal(raw, &result) != nil {
		return nil
	}
	evidence := make([]any, 0, len(result.Pages)*3+2)
	if !strings.EqualFold(result.Status, "error") {
		evidence = append(
			evidence,
			result.URL,
			result.A11yNodes,
			result.Observation,
		)
	}
	for _, page := range result.Pages {
		if !strings.EqualFold(page.Status, "error") {
			evidence = append(
				evidence,
				page.URL,
				page.A11yNodes,
				page.Observation,
			)
		}
		for _, action := range page.Actions {
			if strings.EqualFold(action.Status, "success") &&
				strings.EqualFold(action.Phase, "after") {
				evidence = append(
					evidence,
					action.Action,
					action.Target,
					action.TargetEvidence,
				)
			}
		}
	}
	return evidence
}

func successfulFlowActions(raw json.RawMessage) map[string]bool {
	var result struct {
		Pages []struct {
			Actions []struct {
				StepIndex   int    `json:"step_index"`
				ActionIndex int    `json:"action_index"`
				Status      string `json:"status"`
				Phase       string `json:"phase"`
			} `json:"actions"`
		} `json:"pages"`
	}
	if json.Unmarshal(raw, &result) != nil {
		return nil
	}
	successful := make(map[string]bool)
	for _, page := range result.Pages {
		for _, action := range page.Actions {
			if strings.EqualFold(action.Status, "success") &&
				strings.EqualFold(action.Phase, "after") {
				successful[probeActionKey(
					action.StepIndex,
					action.ActionIndex,
				)] = true
			}
		}
	}
	return successful
}

type observedLocator struct {
	CandidateID   string                      `json:"candidate_id"`
	Locator       browsercontract.LocatorSpec `json:"locator"`
	Provenance    string                      `json:"provenance"`
	ObservedCount int                         `json:"observed_count"`
}

type observedElement struct {
	ElementRef  string                      `json:"element_ref"`
	ContextPath browsercontract.ContextPath `json:"context_path"`
	A11y        *struct {
		Role string `json:"role"`
		Name string `json:"name"`
	} `json:"a11y"`
	DOM *struct {
		Tag   string            `json:"tag"`
		Attrs map[string]string `json:"attrs"`
		Text  string            `json:"text"`
	} `json:"dom"`
	Runtime struct {
		Visible  bool `json:"visible"`
		Enabled  bool `json:"enabled"`
		Editable bool `json:"editable"`
	} `json:"runtime"`
	Locators []observedLocator `json:"locators"`
}

type observedSnapshot struct {
	SchemaVersion string `json:"schema_version"`
	ProbeID       string `json:"probe_id"`
	ObservationID string `json:"observation_id"`
	PageState     struct {
		StateID string `json:"state_id"`
		SHA256  string `json:"state_sha256"`
	} `json:"page_state"`
	Elements []observedElement `json:"elements"`
}

type observedFlowResult struct {
	Observation observedSnapshot `json:"observation_v2"`
	Pages       []struct {
		Observation observedSnapshot `json:"observation_v2"`
		Actions     []struct {
			StepIndex      int                                     `json:"step_index"`
			ActionIndex    int                                     `json:"action_index"`
			Action         string                                  `json:"action"`
			Target         string                                  `json:"target"`
			Status         string                                  `json:"status"`
			Phase          string                                  `json:"phase"`
			ResolvedTarget *browsercontract.ResolvedTargetEvidence `json:"resolved_target"`
		} `json:"actions"`
	} `json:"pages"`
}

func deriveTargetBindings(
	plan Plan,
	owners map[string]string,
	raw json.RawMessage,
) map[string]*browsercontract.TargetBinding {
	var result observedFlowResult
	if json.Unmarshal(raw, &result) != nil {
		return nil
	}
	successful := successfulFlowActions(raw)
	bindings := make(map[string]*browsercontract.TargetBinding)
	for _, page := range result.Pages {
		for _, action := range page.Actions {
			key := probeActionKey(action.StepIndex, action.ActionIndex)
			if !successful[key] || !strings.EqualFold(action.Phase, "before") {
				continue
			}
			owner := owners[key]
			stepIndex := planStepIndex(plan.Steps, owner)
			if stepIndex < 0 {
				continue
			}
			if action.ResolvedTarget == nil {
				continue
			}
			binding := buildResolvedTargetBinding(
				plan,
				plan.Steps[stepIndex],
				*action.ResolvedTarget,
				page.Observation,
			)
			if binding != nil {
				bindings[owner] = binding
			}
		}
	}
	return bindings
}

func buildResolvedTargetBinding(
	plan Plan,
	step Step,
	resolved browsercontract.ResolvedTargetEvidence,
	observation observedSnapshot,
) *browsercontract.TargetBinding {
	if resolved.Validate() != nil ||
		resolved.ActionStatus != "succeeded" ||
		resolved.PlanStepID != step.ID ||
		!probeActionMatches(step.Action, resolved.Action) ||
		!resolvedTargetMatchesObservation(resolved, observation) {
		return nil
	}
	binding, err := browsercontract.NewTargetBinding(
		browsercontract.TargetBinding{
			PlanID: plan.ID, PlanVersion: plan.Version,
			PlanStepID: step.ID, SemanticTarget: step.Target,
			ProbeID: resolved.ProbeID, Action: step.Action,
			PageStateID:       resolved.PageStateID,
			ObservationID:     resolved.ObservationID,
			ObservationSHA256: resolved.PageStateSHA256,
			ElementRefs:       []string{resolved.ElementRef},
			Candidates: []browsercontract.LocatorCandidate{{
				CandidateID:   resolved.CandidateID,
				ElementRef:    resolved.ElementRef,
				ContextPath:   resolved.ContextPath,
				Locator:       resolved.Locator,
				Provenance:    resolved.Provenance,
				ObservedCount: resolved.RuntimeMatchCount,
				Visible:       resolved.Visible,
				Enabled:       resolved.Enabled,
				Score:         resolved.Score,
			}},
			SelectedCandidateID: resolved.CandidateID,
		},
	)
	if err != nil {
		return nil
	}
	return &binding
}

func resolvedTargetMatchesObservation(
	resolved browsercontract.ResolvedTargetEvidence,
	observation observedSnapshot,
) bool {
	if observation.SchemaVersion != browsercontract.ObservationSchemaVersion ||
		observation.ProbeID != resolved.ProbeID ||
		observation.ObservationID != resolved.ObservationID ||
		observation.PageState.StateID != resolved.PageStateID ||
		observation.PageState.SHA256 != resolved.PageStateSHA256 {
		return false
	}
	for _, element := range observation.Elements {
		if element.ElementRef != resolved.ElementRef ||
			!reflect.DeepEqual(element.ContextPath, resolved.ContextPath) ||
			element.Runtime.Visible != resolved.Visible ||
			element.Runtime.Enabled != resolved.Enabled ||
			element.Runtime.Editable != resolved.Editable {
			continue
		}
		for _, candidate := range element.Locators {
			if candidate.CandidateID == resolved.CandidateID &&
				candidate.Provenance == resolved.Provenance &&
				candidate.ObservedCount == resolved.RuntimeMatchCount &&
				reflect.DeepEqual(candidate.Locator, resolved.Locator) {
				return true
			}
		}
	}
	return false
}

func derivePageTargetBindings(
	plan Plan,
	steps []Step,
	raw json.RawMessage,
) map[string]*browsercontract.TargetBinding {
	var result observedFlowResult
	if json.Unmarshal(raw, &result) != nil {
		return nil
	}
	bindings := make(map[string]*browsercontract.TargetBinding)
	for _, step := range steps {
		binding := buildTargetBinding(plan, step, step.Target, result.Observation)
		if binding != nil {
			bindings[step.ID] = binding
		}
	}
	return bindings
}

func buildTargetBinding(
	plan Plan,
	step Step,
	actualTarget string,
	observation observedSnapshot,
) *browsercontract.TargetBinding {
	if observation.SchemaVersion != browsercontract.ObservationSchemaVersion ||
		observation.ProbeID == "" ||
		observation.ObservationID == "" ||
		len(observation.PageState.SHA256) != 64 {
		return nil
	}
	explicit := isSelectorTarget(actualTarget)
	matched := make([]observedElement, 0, 1)
	for _, element := range observation.Elements {
		if !element.Runtime.Visible ||
			!element.Runtime.Enabled ||
			!elementHasExecutableLocator(element) {
			continue
		}
		if step.Action == "input" && !element.Runtime.Editable {
			continue
		}
		if explicit {
			if elementHasLocatorValue(element, actualTarget) {
				matched = append(matched, element)
			}
			continue
		}
		if elementMatchesSemanticTarget(element, actualTarget) ||
			elementMatchesSemanticTarget(element, step.Target) {
			matched = append(matched, element)
		}
	}
	if len(matched) != 1 {
		return nil
	}
	element := matched[0]
	candidates := make([]browsercontract.LocatorCandidate, 0, len(element.Locators))
	for _, observed := range element.Locators {
		if observed.CandidateID == "" || observed.ObservedCount != 1 {
			continue
		}
		candidates = append(candidates, browsercontract.LocatorCandidate{
			CandidateID: observed.CandidateID,
			ElementRef:  element.ElementRef,
			ContextPath: element.ContextPath,
			Locator:     observed.Locator, Provenance: observed.Provenance,
			ObservedCount: observed.ObservedCount,
			Visible:       element.Runtime.Visible, Enabled: element.Runtime.Enabled,
			Score: locatorScore(observed.Locator.Kind),
		})
	}
	if len(candidates) == 0 {
		return nil
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].Score != candidates[j].Score {
			return candidates[i].Score > candidates[j].Score
		}
		return candidates[i].CandidateID < candidates[j].CandidateID
	})
	binding, err := browsercontract.NewTargetBinding(
		browsercontract.TargetBinding{
			PlanID: plan.ID, PlanVersion: plan.Version,
			PlanStepID: step.ID, SemanticTarget: step.Target,
			ProbeID: observation.ProbeID, Action: step.Action,
			PageStateID:       observation.PageState.StateID,
			ObservationID:     observation.ObservationID,
			ObservationSHA256: observation.PageState.SHA256,
			ElementRefs:       []string{element.ElementRef},
			Candidates:        candidates, SelectedCandidateID: candidates[0].CandidateID,
		},
	)
	if err != nil {
		return nil
	}
	return &binding
}

func elementHasExecutableLocator(element observedElement) bool {
	for _, observed := range element.Locators {
		if observed.ObservedCount == 1 && observed.Locator.Validate() == nil {
			return true
		}
	}
	return false
}

func elementHasLocatorValue(element observedElement, target string) bool {
	target = strings.TrimPrefix(strings.TrimSpace(target), "css=")
	target = strings.TrimPrefix(target, "xpath=")
	for _, observed := range element.Locators {
		value := strings.TrimPrefix(
			strings.TrimSpace(observed.Locator.Value),
			"css=",
		)
		value = strings.TrimPrefix(value, "xpath=")
		if value == target {
			return true
		}
	}
	return false
}

func elementMatchesSemanticTarget(element observedElement, target string) bool {
	target = normalize(target)
	if target == "" {
		return false
	}
	values := make([]string, 0, 6)
	if element.A11y != nil {
		values = append(values, element.A11y.Role, element.A11y.Name)
	}
	if element.DOM != nil {
		values = append(values, element.DOM.Tag, element.DOM.Text)
		for _, key := range []string{"aria-label", "name", "placeholder", "title"} {
			values = append(values, element.DOM.Attrs[key])
		}
	}
	for _, value := range values {
		if semanticTargetMatches(target, value) {
			return true
		}
	}
	return false
}

func locatorScore(kind string) float64 {
	switch kind {
	case "role":
		return 0.95
	case "label":
		return 0.93
	case "test_id":
		return 0.92
	case "placeholder":
		return 0.9
	case "text":
		return 0.85
	case "css":
		return 0.8
	case "xpath":
		return 0.6
	default:
		return 0.5
	}
}

func probeActionKey(stepIndex int, actionIndex int) string {
	return fmt.Sprintf("%d:%d", stepIndex, actionIndex)
}

func planStepIndex(steps []Step, id string) int {
	for index := range steps {
		if steps[index].ID == id {
			return index
		}
	}
	return -1
}

func jsonContainsText(value any, target string) bool {
	switch typed := value.(type) {
	case string:
		return strings.Contains(normalize(typed), target)
	case map[string]any:
		for _, nested := range typed {
			if jsonContainsText(nested, target) {
				return true
			}
		}
	case []any:
		for _, nested := range typed {
			if jsonContainsText(nested, target) {
				return true
			}
		}
	}
	return false
}

func allGrounded(steps []Step) bool {
	for _, step := range steps {
		if step.Status != StepGrounded ||
			(requiresTargetBinding(step.Action) && step.TargetBinding == nil) {
			return false
		}
	}
	return true
}

func forbidden(rules []string, value string) bool {
	value = normalize(value)
	for _, rule := range rules {
		if strings.Contains(value, rule) {
			return true
		}
	}
	return false
}

func normalize(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(value)), " "))
}

func cleanStrings(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			result = append(result, value)
		}
	}
	return result
}

func validAction(value string) bool {
	switch value {
	case "goto", "click", "input", "wait_for",
		"assert_text", "assert_url_contains", "capture_text":
		return true
	}
	return false
}

func validateActionParameters(step StepDefinition) error {
	switch step.Action {
	case "goto":
		if step.Value == "" {
			return errors.New("goto requires value")
		}
		if step.Idempotency != "idempotent" ||
			step.SideEffect != SideEffectBrowserState {
			return errors.New(
				"goto must be idempotent with browser_state side effect",
			)
		}
	case "click":
		if step.Target == "" {
			return errors.New("click requires target")
		}
	case "input":
		if step.Target == "" {
			return errors.New("input requires target")
		}
		if step.Trigger != "" &&
			step.Trigger != "Enter" &&
			step.Trigger != "Tab" {
			return errors.New("input trigger must be Enter or Tab")
		}
		if step.Idempotency != "idempotent" ||
			step.SideEffect != SideEffectBrowserState {
			return errors.New(
				"input must be idempotent with browser_state side effect",
			)
		}
	case "wait_for":
		if step.Target == "" {
			return fmt.Errorf("%s requires target", step.Action)
		}
		if step.Idempotency != "idempotent" ||
			step.SideEffect != SideEffectNone {
			return errors.New(
				"wait_for must be idempotent with no side effect",
			)
		}
	case "assert_text":
		if step.Target == "" || step.Value == "" {
			return errors.New("assert_text requires target and value")
		}
		if step.Idempotency != "idempotent" ||
			step.SideEffect != SideEffectNone {
			return errors.New(
				"assert_text must be idempotent with no side effect",
			)
		}
	case "assert_url_contains":
		if step.Target == "" || step.Value == "" {
			return errors.New(
				"assert_url_contains requires target and value",
			)
		}
		if step.Idempotency != "idempotent" ||
			step.SideEffect != SideEffectNone {
			return errors.New(
				"assert_url_contains must be idempotent with no side effect",
			)
		}
	case "capture_text":
		if step.Target == "" || step.ContextKey == "" {
			return errors.New("capture_text requires target and context_key")
		}
		if step.Idempotency != "idempotent" ||
			step.SideEffect != SideEffectNone {
			return errors.New(
				"capture_text must be idempotent with no side effect",
			)
		}
	}
	if step.TimeoutMS < 0 {
		return errors.New("timeout_ms must not be negative")
	}
	return nil
}

func validSideEffect(value SideEffect) bool {
	return sideEffectRank(value) >= 0
}

func sideEffectRank(value SideEffect) int {
	switch value {
	case SideEffectNone:
		return 0
	case SideEffectBrowserState:
		return 1
	case SideEffectExternal:
		return 2
	case SideEffectUnknown:
		return 3
	}
	return -1
}
