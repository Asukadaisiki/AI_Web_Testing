package agentservice

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/agent"
)

const PipelineTraceSummarySchemaV1 = "agent.pipeline.trace-summary.v1"

type PipelineContextGrowth struct {
	FirstRequestBytes  int                              `json:"first_request_bytes"`
	LatestRequestBytes int                              `json:"latest_request_bytes"`
	MaxRequestBytes    int                              `json:"max_request_bytes"`
	FirstMessageBytes  int                              `json:"first_message_bytes"`
	LatestMessageBytes int                              `json:"latest_message_bytes"`
	MaxMessageBytes    int                              `json:"max_message_bytes"`
	LatestBreakdown    agent.RequestSerializationBudget `json:"latest_breakdown"`
}

type PipelineRepeatedToolCall struct {
	Name              string `json:"name"`
	Signature         string `json:"signature"`
	StateEpoch        string `json:"state_epoch"`
	Attempt           int    `json:"attempt"`
	RetryOfToolCallID string `json:"retry_of_tool_call_id,omitempty"`
	ToolCallID        string `json:"tool_call_id"`
}

type PipelineTraceSummary struct {
	SchemaVersion        string                     `json:"schema_version"`
	RunID                string                     `json:"run_id"`
	TraceEvents          int                        `json:"trace_events"`
	PlanVersions         []PipelinePlanRef          `json:"plan_versions"`
	ToolCalls            int                        `json:"tool_calls"`
	ToolStateTransitions int                        `json:"tool_state_transitions"`
	RepeatedToolCalls    []PipelineRepeatedToolCall `json:"repeated_tool_calls"`
	ModelRequests        int                        `json:"model_requests"`
	Cumulative           PipelineCumulativeUsage    `json:"cumulative"`
	ContextGrowth        PipelineContextGrowth      `json:"context_growth"`
}

func (s *Service) RecordPipelineTrace(
	ctx context.Context,
	run AgentRun,
	stepID string,
	toolCallID string,
	payload PipelineTracePayload,
) error {
	payload.SchemaVersion = PipelineTraceSchemaV1
	if payload.StateEpoch == "" {
		payload.StateEpoch = runStateEpoch(run)
	}
	if err := validatePipelineTrace(payload); err != nil {
		return err
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode pipeline trace: %w", err)
	}
	var eventPayload map[string]any
	if decodeErr := json.Unmarshal(encoded, &eventPayload); decodeErr != nil {
		return fmt.Errorf("normalize pipeline trace: %w", decodeErr)
	}
	_, err = s.appendEvent(ctx, run, Event{
		Type:       EventPipelineTrace,
		StepID:     stepID,
		ToolCallID: toolCallID,
		Payload:    eventPayload,
	})
	return err
}

func (s *Service) recordPipelineModelTrace(
	ctx context.Context,
	run AgentRun,
	record agent.TelemetryRecord,
	attempt agent.ModelAttempt,
) error {
	cumulative, err := s.pipelineCumulativeUsage(ctx, run.ID)
	if err != nil {
		return err
	}
	stateEpoch := record.State.Epoch
	if stateEpoch == "" {
		stateEpoch = runStateEpoch(run)
	}
	var plan *PipelinePlanRef
	if record.State.PlanID != "" {
		plan = &PipelinePlanRef{
			PlanID:  record.State.PlanID,
			Version: record.State.Version,
			SHA256:  record.State.PlanSHA256,
			Status:  record.State.Status,
		}
	}
	toolCallIDs := []string{}
	if attempt.Status == "succeeded" {
		toolCallIDs = normalizedIDs(record.ToolCallIDs)
	}
	return s.RecordPipelineTrace(
		ctx,
		run,
		record.StepID,
		singleToolCallID(toolCallIDs),
		PipelineTracePayload{
			Kind:       PipelineTraceModelRequest,
			StateEpoch: stateEpoch,
			Plan:       plan,
			ModelRequest: &PipelineModelRequestTrace{
				LogicalCallID: limitString(record.LogicalCallID, 128),
				Attempt:       attempt.Attempt,
				AttemptStatus: limitString(attempt.Status, 64),
				RequestBudget: record.Telemetry.Prompt.RequestBudget,
				Cumulative:    cumulative,
				ToolCallIDs:   toolCallIDs,
			},
		},
	)
}

func (s *Service) pipelineCumulativeUsage(
	ctx context.Context,
	runID string,
) (PipelineCumulativeUsage, error) {
	events, err := s.repository.ListEvents(ctx, runID, 0)
	if err != nil {
		return PipelineCumulativeUsage{}, err
	}
	logicalCalls := make(map[string]struct{})
	result := PipelineCumulativeUsage{}
	for _, event := range events {
		if event.Type != EventResearchLLMCall {
			continue
		}
		result.PhysicalAttempts++
		if logicalID, _ := event.Payload["logical_call_id"].(string); logicalID != "" {
			logicalCalls[logicalID] = struct{}{}
		}
		usage, _ := event.Payload["usage"].(map[string]any)
		if usage == nil || stringValue(usage["status"]) == string(agent.UsageUnavailable) {
			result.UsageUnavailableAttempts++
			continue
		}
		result.InputTokens += int64Value(usage["input_tokens"])
		result.OutputTokens += int64Value(usage["output_tokens"])
		result.TotalTokens += int64Value(usage["total_tokens"])
	}
	result.LogicalCalls = len(logicalCalls)
	return result, nil
}

func SummarizePipelineTrace(
	runID string,
	events []Event,
) (PipelineTraceSummary, error) {
	result := PipelineTraceSummary{
		SchemaVersion:     PipelineTraceSummarySchemaV1,
		RunID:             strings.TrimSpace(runID),
		PlanVersions:      []PipelinePlanRef{},
		RepeatedToolCalls: []PipelineRepeatedToolCall{},
	}
	if result.RunID == "" {
		return PipelineTraceSummary{}, errors.New("pipeline trace summary requires run_id")
	}
	plans := make(map[string]PipelinePlanRef)
	toolCalls := make(map[string]struct{})
	firstModelRequest := true
	for _, event := range events {
		if event.Type == EventTaskPlanUpdated {
			plan := PipelinePlanRef{
				PlanID:  stringValue(event.Payload["plan_id"]),
				Version: int(int64Value(event.Payload["version"])),
				SHA256:  stringValue(event.Payload["plan_sha256"]),
				Status:  stringValue(event.Payload["status"]),
			}
			if plan.PlanID != "" && plan.Version > 0 {
				plans[planKey(plan)] = plan
			}
			continue
		}
		if event.Type != EventPipelineTrace {
			continue
		}
		var trace PipelineTracePayload
		encoded, err := json.Marshal(event.Payload)
		if err != nil {
			return PipelineTraceSummary{}, fmt.Errorf(
				"encode pipeline trace event %d: %w",
				event.Seq,
				err,
			)
		}
		if err := json.Unmarshal(encoded, &trace); err != nil {
			return PipelineTraceSummary{}, fmt.Errorf(
				"decode pipeline trace event %d: %w",
				event.Seq,
				err,
			)
		}
		if err := validatePipelineTrace(trace); err != nil {
			return PipelineTraceSummary{}, fmt.Errorf(
				"validate pipeline trace event %d: %w",
				event.Seq,
				err,
			)
		}
		result.TraceEvents++
		if trace.Plan != nil {
			plans[planKey(*trace.Plan)] = *trace.Plan
		}
		switch trace.Kind {
		case PipelineTraceModelRequest:
			result.ModelRequests++
			budget := trace.ModelRequest.RequestBudget
			if firstModelRequest {
				result.ContextGrowth.FirstRequestBytes = budget.RequestBytes
				result.ContextGrowth.FirstMessageBytes = budget.MessageBytes
				firstModelRequest = false
			}
			result.ContextGrowth.LatestRequestBytes = budget.RequestBytes
			result.ContextGrowth.LatestMessageBytes = budget.MessageBytes
			result.ContextGrowth.MaxRequestBytes = max(
				result.ContextGrowth.MaxRequestBytes,
				budget.RequestBytes,
			)
			result.ContextGrowth.MaxMessageBytes = max(
				result.ContextGrowth.MaxMessageBytes,
				budget.MessageBytes,
			)
			result.ContextGrowth.LatestBreakdown = budget
			result.Cumulative = trace.ModelRequest.Cumulative
		case PipelineTraceToolCall:
			result.ToolStateTransitions++
			if event.ToolCallID != "" {
				toolCalls[event.ToolCallID] = struct{}{}
			}
			if trace.ToolCall.Status == "proposed" &&
				trace.ToolCall.Attempt > 1 {
				result.RepeatedToolCalls = append(
					result.RepeatedToolCalls,
					PipelineRepeatedToolCall{
						Name: trace.ToolCall.Name, Signature: trace.ToolCall.Signature,
						StateEpoch: trace.StateEpoch, Attempt: trace.ToolCall.Attempt,
						RetryOfToolCallID: trace.ToolCall.RetryOfToolCallID,
						ToolCallID:        event.ToolCallID,
					},
				)
			}
		}
	}
	result.ToolCalls = len(toolCalls)
	for _, plan := range plans {
		result.PlanVersions = append(result.PlanVersions, plan)
	}
	sort.Slice(result.PlanVersions, func(i, j int) bool {
		if result.PlanVersions[i].Version != result.PlanVersions[j].Version {
			return result.PlanVersions[i].Version < result.PlanVersions[j].Version
		}
		return result.PlanVersions[i].PlanID < result.PlanVersions[j].PlanID
	})
	sort.Slice(result.RepeatedToolCalls, func(i, j int) bool {
		if result.RepeatedToolCalls[i].Attempt !=
			result.RepeatedToolCalls[j].Attempt {
			return result.RepeatedToolCalls[i].Attempt <
				result.RepeatedToolCalls[j].Attempt
		}
		return result.RepeatedToolCalls[i].ToolCallID <
			result.RepeatedToolCalls[j].ToolCallID
	})
	return result, nil
}

func validatePipelineTrace(payload PipelineTracePayload) error {
	if payload.SchemaVersion != PipelineTraceSchemaV1 {
		return fmt.Errorf("unsupported pipeline trace schema %q", payload.SchemaVersion)
	}
	if !validSHA256(payload.StateEpoch) {
		return errors.New("pipeline trace requires a SHA-256 state_epoch")
	}
	if payload.Plan != nil &&
		(strings.TrimSpace(payload.Plan.PlanID) == "" ||
			payload.Plan.Version < 1 ||
			!validSHA256(payload.Plan.SHA256) ||
			strings.TrimSpace(payload.Plan.Status) == "") {
		return errors.New("pipeline trace plan reference is invalid")
	}
	switch payload.Kind {
	case PipelineTraceModelRequest:
		if payload.ModelRequest == nil || payload.ToolCall != nil {
			return errors.New("model_request trace has invalid detail")
		}
		if strings.TrimSpace(payload.ModelRequest.LogicalCallID) == "" ||
			payload.ModelRequest.Attempt < 1 ||
			strings.TrimSpace(payload.ModelRequest.AttemptStatus) == "" {
			return errors.New("model_request trace is incomplete")
		}
	case PipelineTraceToolCall:
		if payload.ToolCall == nil || payload.ModelRequest != nil {
			return errors.New("tool_call trace has invalid detail")
		}
		if strings.TrimSpace(payload.ToolCall.Name) == "" ||
			!validSHA256(payload.ToolCall.Signature) ||
			payload.ToolCall.Attempt < 1 ||
			!validToolTraceStatus(payload.ToolCall.Status) {
			return errors.New("tool_call trace is incomplete")
		}
	default:
		return fmt.Errorf("unsupported pipeline trace kind %q", payload.Kind)
	}
	return nil
}

func validToolTraceStatus(status string) bool {
	switch status {
	case "proposed", "authorized", "running", "succeeded", "failed", "rejected", "pending":
		return true
	default:
		return false
	}
}

func planKey(plan PipelinePlanRef) string {
	return fmt.Sprintf("%s:%d:%s", plan.PlanID, plan.Version, plan.SHA256)
}

func validSHA256(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func runStateEpoch(run AgentRun) string {
	payload, _ := json.Marshal(struct {
		Status               RunStatus `json:"status"`
		PendingToolCallID    *string   `json:"pending_tool_call_id,omitempty"`
		LatestGenerationID   *int64    `json:"latest_generation_id,omitempty"`
		ApprovedGenerationID *int64    `json:"approved_generation_id,omitempty"`
	}{
		Status:               run.Status,
		PendingToolCallID:    run.PendingToolCallID,
		LatestGenerationID:   run.LatestGenerationID,
		ApprovedGenerationID: run.ApprovedGenerationID,
	})
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

func normalizedIDs(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func singleToolCallID(values []string) string {
	values = normalizedIDs(values)
	if len(values) == 1 {
		return values[0]
	}
	return ""
}

func stringValue(value any) string {
	text, _ := value.(string)
	return text
}

func int64Value(value any) int64 {
	switch typed := value.(type) {
	case float64:
		return int64(typed)
	case int:
		return int64(typed)
	case int64:
		return typed
	case json.Number:
		result, _ := typed.Int64()
		return result
	default:
		return 0
	}
}
