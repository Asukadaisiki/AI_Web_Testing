package taskplan

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
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
	return s.repository.CreateVersion(ctx, plan)
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
		if resultFailed(result) {
			return nil
		}
		ids, err := planStepIDs(arguments)
		if err != nil {
			return err
		}
		hash := sha256.Sum256(result)
		for index := range plan.Steps {
			if !contains(ids, plan.Steps[index].ID) {
				continue
			}
			if !resultContainsStepEvidence(result, plan.Steps[index]) {
				return fmt.Errorf(
					"exploration result has no evidence for plan step %q",
					plan.Steps[index].ID,
				)
			}
			plan.Steps[index].Status = StepGrounded
			plan.Steps[index].GroundingAttempts++
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
	var request struct {
		Steps []struct {
			Description string `json:"description"`
			Actions     []struct {
				Action string `json:"action"`
				Target string `json:"target"`
				Value  string `json:"value"`
			} `json:"actions"`
		} `json:"steps"`
	}
	if json.Unmarshal(arguments, &request) != nil {
		return errors.New("invalid explore_flow arguments")
	}
	for _, group := range request.Steps {
		for _, action := range group.Actions {
			haystack := action.Action + " " + action.Target + " " +
				action.Value + " " + group.Description
			if forbidden(plan.ForbiddenActions, haystack) {
				return errors.New("explore_flow contains a forbidden action")
			}
			if !matchesAnyStep(
				steps,
				action.Action,
				action.Target,
				action.Value,
			) {
				return fmt.Errorf(
					"explore_flow action %q target %q is not owned by the bound plan steps",
					action.Action,
					action.Target,
				)
			}
			for _, step := range steps {
				if matchesStep(
					step,
					action.Action,
					action.Target,
					action.Value,
				) &&
					(step.SideEffect == SideEffectExternal ||
						step.SideEffect == SideEffectUnknown) {
					return fmt.Errorf(
						"plan step %q cannot be probed because side_effect is %q",
						step.ID,
						step.SideEffect,
					)
				}
			}
		}
	}
	return nil
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
	if len(ids) > len(plan.Steps)-first {
		return nil, errors.New(
			"plan_step_ids exceed remaining task plan steps",
		)
	}
	result := make([]Step, len(ids))
	for offset, id := range ids {
		step := plan.Steps[first+offset]
		if step.ID != id {
			return nil, fmt.Errorf(
				"expected next plan step %q, got %q",
				step.ID,
				id,
			)
		}
		result[offset] = step
	}
	return result, nil
}

func resultFailed(raw json.RawMessage) bool {
	var result struct {
		Success  *bool  `json:"success"`
		Status   string `json:"status"`
		Failures []any  `json:"failures"`
	}
	if json.Unmarshal(raw, &result) != nil {
		return true
	}
	if (result.Success != nil && !*result.Success) ||
		strings.EqualFold(result.Status, "error") ||
		len(result.Failures) > 0 {
		return true
	}
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return true
	}
	return containsFailure(value)
}

func containsFailure(value any) bool {
	switch typed := value.(type) {
	case map[string]any:
		for key, nested := range typed {
			if key == "failure" && nested != nil {
				return true
			}
			if key == "status" &&
				strings.EqualFold(fmt.Sprint(nested), "error") {
				return true
			}
			if containsFailure(nested) {
				return true
			}
		}
	case []any:
		for _, nested := range typed {
			if containsFailure(nested) {
				return true
			}
		}
	}
	return false
}

func matchesAnyStep(
	steps []Step,
	action string,
	target string,
	value string,
) bool {
	for _, step := range steps {
		if matchesStep(step, action, target, value) {
			return true
		}
	}
	return false
}

func matchesStep(
	step Step,
	action string,
	target string,
	value string,
) bool {
	return normalize(step.Action) == normalize(action) &&
		normalize(step.Target) == normalize(target) &&
		(step.Value == "" ||
			strings.TrimSpace(step.Value) == strings.TrimSpace(value))
}

func resultContainsStepEvidence(raw json.RawMessage, step Step) bool {
	var decoded any
	if json.Unmarshal(raw, &decoded) != nil {
		return false
	}
	for _, expected := range []string{step.Target, step.Value} {
		if candidate := normalize(expected); candidate != "" &&
			jsonContainsText(decoded, candidate) {
			return true
		}
	}
	return step.Action == "goto" &&
		jsonContainsText(decoded, normalize(step.Intent))
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
		if step.Status != StepGrounded {
			return false
		}
	}
	return true
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
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
