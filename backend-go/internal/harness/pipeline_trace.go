package harness

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strings"

	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/agent"
	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/agentservice"
	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/taskplan"
)

type toolTraceIdentity struct {
	stateEpoch        string
	plan              *agentservice.PipelinePlanRef
	signature         string
	attempt           int
	retryOfToolCallID string
	planStepIDs       []string
	lineage           []agentservice.PipelineLineageRef
}

func (e *Harness) currentPipelineState(
	ctx context.Context,
	run agentservice.AgentRun,
) (agent.PipelineState, *taskplan.Plan, error) {
	var plan *taskplan.Plan
	if e.plans != nil {
		current, err := e.plans.GetCurrent(ctx, run.ID)
		if err != nil && !errors.Is(err, taskplan.ErrNotFound) {
			return agent.PipelineState{}, nil, err
		}
		if err == nil {
			plan = &current
		}
	}
	state := agent.PipelineState{}
	if plan != nil {
		state.PlanID = plan.ID
		state.Version = plan.Version
		state.PlanSHA256 = plan.PlanSHA256
		state.Status = string(plan.Status)
	}
	state.Epoch = pipelineStateEpoch(run, plan)
	return state, plan, nil
}

func (e *Harness) newToolTraceIdentity(
	ctx context.Context,
	run agentservice.AgentRun,
	call agent.ModelTool,
	plan *taskplan.Plan,
) (toolTraceIdentity, error) {
	identity := toolTraceIdentity{
		stateEpoch:  pipelineStateEpoch(run, plan),
		plan:        pipelinePlanRef(plan),
		signature:   normalizedToolCallSignature(call),
		attempt:     1,
		planStepIDs: toolPlanStepIDs(call.Arguments),
	}
	events, err := e.runs.ListEvents(ctx, run.ID, 0)
	if err != nil {
		return toolTraceIdentity{}, err
	}
	seenCalls := make(map[string]struct{})
	for _, event := range events {
		if event.Type != agentservice.EventPipelineTrace ||
			event.ToolCallID == "" ||
			event.Payload["kind"] != string(agentservice.PipelineTraceToolCall) ||
			event.Payload["state_epoch"] != identity.stateEpoch {
			continue
		}
		detail, _ := event.Payload["tool_call"].(map[string]any)
		if detail == nil ||
			detail["signature"] != identity.signature ||
			detail["name"] != call.Name {
			continue
		}
		if _, exists := seenCalls[event.ToolCallID]; exists {
			continue
		}
		seenCalls[event.ToolCallID] = struct{}{}
		identity.retryOfToolCallID = event.ToolCallID
	}
	identity.attempt += len(seenCalls)
	return identity, nil
}

func (e *Harness) pendingToolTraceIdentity(
	ctx context.Context,
	run agentservice.AgentRun,
	toolCallID string,
) (agent.ModelTool, toolTraceIdentity, bool, error) {
	var call agent.ModelTool
	for messageIndex := len(run.Transcript) - 1; messageIndex >= 0; messageIndex-- {
		for _, candidate := range run.Transcript[messageIndex].ToolCalls {
			if candidate.ID == toolCallID {
				call = candidate
				break
			}
		}
		if call.ID != "" {
			break
		}
	}
	if call.ID == "" {
		return agent.ModelTool{}, toolTraceIdentity{}, false, nil
	}
	events, err := e.runs.ListEvents(ctx, run.ID, 0)
	if err != nil {
		return agent.ModelTool{}, toolTraceIdentity{}, false, err
	}
	for index := len(events) - 1; index >= 0; index-- {
		event := events[index]
		if event.Type != agentservice.EventPipelineTrace ||
			event.ToolCallID != toolCallID ||
			event.Payload["kind"] != string(agentservice.PipelineTraceToolCall) {
			continue
		}
		detail, _ := event.Payload["tool_call"].(map[string]any)
		if detail == nil || detail["status"] != "pending" {
			continue
		}
		identity := toolTraceIdentity{
			stateEpoch:        stringFromAny(event.Payload["state_epoch"]),
			signature:         stringFromAny(detail["signature"]),
			attempt:           intFromAny(detail["attempt"]),
			retryOfToolCallID: stringFromAny(detail["retry_of_tool_call_id"]),
			planStepIDs:       stringsFromAny(detail["plan_step_ids"]),
		}
		if rawPlan, _ := event.Payload["plan"].(map[string]any); rawPlan != nil {
			identity.plan = &agentservice.PipelinePlanRef{
				PlanID:  stringFromAny(rawPlan["plan_id"]),
				Version: intFromAny(rawPlan["version"]),
				SHA256:  stringFromAny(rawPlan["sha256"]),
				Status:  stringFromAny(rawPlan["status"]),
			}
		}
		return call, identity, true, nil
	}
	return agent.ModelTool{}, toolTraceIdentity{}, false, nil
}

func (e *Harness) recordPipelineToolTrace(
	ctx context.Context,
	run agentservice.AgentRun,
	stepID string,
	call agent.ModelTool,
	identity toolTraceIdentity,
	status string,
	reasonCode string,
) error {
	return e.runs.RecordPipelineTrace(
		ctx,
		run,
		stepID,
		call.ID,
		agentservice.PipelineTracePayload{
			Kind:       agentservice.PipelineTraceToolCall,
			StateEpoch: identity.stateEpoch,
			Plan:       identity.plan,
			ToolCall: &agentservice.PipelineToolCallTrace{
				Name:              call.Name,
				Signature:         identity.signature,
				Status:            status,
				Attempt:           identity.attempt,
				RetryOfToolCallID: identity.retryOfToolCallID,
				ReasonCode:        reasonCode,
				PlanStepIDs:       identity.planStepIDs,
				Lineage:           identity.lineage,
			},
		},
	)
}

func pipelineStateEpoch(
	run agentservice.AgentRun,
	plan *taskplan.Plan,
) string {
	type state struct {
		RunStatus            agentservice.RunStatus `json:"run_status"`
		PendingToolCallID    *string                `json:"pending_tool_call_id,omitempty"`
		LatestGenerationID   *int64                 `json:"latest_generation_id,omitempty"`
		ApprovedGenerationID *int64                 `json:"approved_generation_id,omitempty"`
		PlanID               string                 `json:"plan_id,omitempty"`
		PlanVersion          int                    `json:"plan_version,omitempty"`
		PlanSHA256           string                 `json:"plan_sha256,omitempty"`
		PlanStatus           taskplan.Status        `json:"plan_status,omitempty"`
	}
	value := state{
		RunStatus:            run.Status,
		PendingToolCallID:    run.PendingToolCallID,
		LatestGenerationID:   run.LatestGenerationID,
		ApprovedGenerationID: run.ApprovedGenerationID,
	}
	if plan != nil {
		value.PlanID = plan.ID
		value.PlanVersion = plan.Version
		value.PlanSHA256 = plan.PlanSHA256
		value.PlanStatus = plan.Status
	}
	encoded, _ := json.Marshal(value)
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}

func pipelinePlanRef(plan *taskplan.Plan) *agentservice.PipelinePlanRef {
	if plan == nil {
		return nil
	}
	return &agentservice.PipelinePlanRef{
		PlanID:  plan.ID,
		Version: plan.Version,
		SHA256:  plan.PlanSHA256,
		Status:  string(plan.Status),
	}
}

func (e *Harness) pipelineLineage(
	ctx context.Context,
	run agentservice.AgentRun,
	tool string,
	result json.RawMessage,
	planStepIDs []string,
) ([]agentservice.PipelineLineageRef, error) {
	if e.plans == nil {
		return nil, nil
	}
	plan, err := e.plans.GetCurrent(ctx, run.ID)
	if errors.Is(err, taskplan.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	stage := pipelineLineageStage(tool)
	if stage == "" {
		return nil, nil
	}
	selectedSteps := make(map[string]struct{}, len(planStepIDs))
	for _, id := range planStepIDs {
		selectedSteps[id] = struct{}{}
	}
	var value map[string]any
	_ = json.Unmarshal(result, &value)
	batchID := int64FromAny(value["batch_id"])
	if batchID == 0 && (tool == "get_report" || tool == "fix_and_retry") {
		batchID = int64FromAny(firstNonNil(value["id"], value["source_batch_id"]))
	}
	caseID := int64FromAny(value["case_id"])
	reportByStep := reportLineageByPlanStep(value)
	lineage := make([]agentservice.PipelineLineageRef, 0, len(plan.Steps))
	for _, step := range plan.Steps {
		if len(selectedSteps) > 0 {
			if _, exists := selectedSteps[step.ID]; !exists &&
				stage == "grounding" {
				continue
			}
		}
		item := agentservice.PipelineLineageRef{
			Stage: stage, PlanID: plan.ID, PlanVersion: plan.Version,
			PlanStepID: step.ID, BatchID: batchID, CaseID: caseID,
		}
		if plan.BoundGenerationID != nil {
			item.GenerationID = *plan.BoundGenerationID
		}
		if binding := step.TargetBinding; binding != nil {
			item.ProbeID = binding.ProbeID
			item.ObservationID = binding.ObservationID
			item.ObservationSHA256 = binding.ObservationSHA256
			item.PageStateID = binding.PageStateID
			item.ElementRefs = append([]string(nil), binding.ElementRefs...)
			item.TargetBindingID = binding.BindingID
			item.PlannedCandidateID = binding.SelectedCandidateID
		}
		if runtime, exists := reportByStep[step.ID]; exists {
			item.ExecutionID = runtime.executionID
			if runtime.caseID > 0 {
				item.CaseID = runtime.caseID
			}
			item.ReportStatus = runtime.reportStatus
			item.ResolvedCandidateID = runtime.candidateID
			item.ResolvedElementRef = runtime.elementRef
		}
		lineage = append(lineage, item)
	}
	return lineage, nil
}

type runtimeLineage struct {
	executionID  int64
	caseID       int64
	reportStatus string
	candidateID  string
	elementRef   string
}

func reportLineageByPlanStep(value map[string]any) map[string]runtimeLineage {
	result := make(map[string]runtimeLineage)
	if nested, _ := value["report"].(map[string]any); nested != nil {
		for stepID, lineage := range reportLineageByPlanStep(nested) {
			result[stepID] = lineage
		}
	}
	jobs, _ := value["jobs"].([]any)
	for _, rawJob := range jobs {
		job, _ := rawJob.(map[string]any)
		execution, _ := job["latest_execution"].(map[string]any)
		report, _ := execution["report"].(map[string]any)
		steps, _ := report["steps"].([]any)
		for _, rawStep := range steps {
			step, _ := rawStep.(map[string]any)
			planStepID := stringFromAny(step["plan_step_id"])
			if planStepID == "" {
				continue
			}
			result[planStepID] = runtimeLineage{
				executionID: int64FromAny(execution["id"]),
				caseID:      int64FromAny(job["case_id"]),
				reportStatus: stringFromAny(firstNonNil(
					step["status"],
					execution["status"],
				)),
				candidateID: stringFromAny(step["candidate_id"]),
				elementRef:  stringFromAny(step["element_ref"]),
			}
		}
	}
	return result
}

func pipelineLineageStage(tool string) string {
	switch tool {
	case "explore_page", "explore_flow":
		return "grounding"
	case "generate_dsl":
		return "generation"
	case "execute_dsl":
		return "execution"
	case "get_report":
		return "report"
	case "fix_and_retry":
		return "repair"
	default:
		return ""
	}
}

func firstNonNil(values ...any) any {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}

func normalizedToolCallSignature(call agent.ModelTool) string {
	var arguments any
	if json.Unmarshal([]byte(call.Arguments), &arguments) != nil {
		arguments = strings.TrimSpace(call.Arguments)
	} else if agent.IsExplorationTool(call.Name) {
		arguments = normalizeJSONForSignature(arguments)
	}
	encoded, _ := json.Marshal(struct {
		Tool      string `json:"tool"`
		Arguments any    `json:"arguments"`
	}{
		Tool:      call.Name,
		Arguments: arguments,
	})
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}

func toolPlanStepIDs(arguments string) []string {
	var value any
	if json.Unmarshal([]byte(arguments), &value) != nil {
		return []string{}
	}
	rawIDs := make([]string, 0)
	collectPlanStepIDs(value, &rawIDs)
	seen := make(map[string]struct{}, len(rawIDs))
	result := make([]string, 0, len(rawIDs))
	for _, id := range rawIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		result = append(result, id)
	}
	return result
}

func collectPlanStepIDs(value any, result *[]string) {
	switch typed := value.(type) {
	case map[string]any:
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			nested := typed[key]
			switch key {
			case "plan_step_id":
				if id, ok := nested.(string); ok {
					*result = append(*result, id)
				}
			case "plan_step_ids":
				if ids, ok := nested.([]any); ok {
					for _, rawID := range ids {
						if id, ok := rawID.(string); ok {
							*result = append(*result, id)
						}
					}
				}
			default:
				collectPlanStepIDs(nested, result)
			}
		}
	case []any:
		for _, nested := range typed {
			collectPlanStepIDs(nested, result)
		}
	}
}

func pipelineReasonCode(err error, fallback string) string {
	var gateError *ExplorationGateError
	if errors.As(err, &gateError) && gateError.Code != "" {
		return gateError.Code
	}
	return fallback
}

func stringFromAny(value any) string {
	result, _ := value.(string)
	return result
}

func intFromAny(value any) int {
	switch typed := value.(type) {
	case float64:
		return int(typed)
	case int:
		return typed
	default:
		return 0
	}
}

func int64FromAny(value any) int64 {
	switch typed := value.(type) {
	case float64:
		return int64(typed)
	case int:
		return int64(typed)
	case int64:
		return typed
	default:
		return 0
	}
}

func stringsFromAny(value any) []string {
	raw, _ := value.([]any)
	result := make([]string, 0, len(raw))
	for _, item := range raw {
		if text := stringFromAny(item); text != "" {
			result = append(result, text)
		}
	}
	return result
}
