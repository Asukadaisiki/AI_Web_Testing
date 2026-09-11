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
