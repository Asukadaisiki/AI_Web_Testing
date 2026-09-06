package research

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"
)

type TransitionUnit struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

type CostSummary struct {
	InputTokens  Slot[int64] `json:"input_tokens"`
	OutputTokens Slot[int64] `json:"output_tokens"`
	TotalTokens  Slot[int64] `json:"total_tokens"`
	LatencyMS    Slot[int64] `json:"latency_ms"`
	VisionCalls  Slot[int64] `json:"vision_calls"`
	Sources      []SourceRef `json:"sources"`
}

type TransitionPayloadV1 struct {
	SchemaVersion    string              `json:"schema_version"`
	ProjectorVersion string              `json:"projector_version"`
	Unit             TransitionUnit      `json:"unit"`
	State            Slot[ResearchEvent] `json:"state"`
	Observation      Slot[ResearchEvent] `json:"observation"`
	Candidate        Slot[ResearchEvent] `json:"candidate"`
	Decision         Slot[ResearchEvent] `json:"decision"`
	Action           Slot[ResearchEvent] `json:"action"`
	Execution        Slot[ResearchEvent] `json:"execution"`
	Verification     Slot[ResearchEvent] `json:"verification"`
	Failure          Slot[ResearchEvent] `json:"failure"`
	Recovery         Slot[ResearchEvent] `json:"recovery"`
	Reward           Slot[ResearchEvent] `json:"reward"`
	Unknown          Slot[ResearchEvent] `json:"unknown"`
	Cost             Slot[CostSummary]   `json:"cost"`
	Done             bool                `json:"done"`
	Projection       ProjectionManifest  `json:"projection"`
}

type Projector struct{}

func NewProjector() *Projector {
	return &Projector{}
}

func (p *Projector) Project(snapshot SourceSnapshot) ([]Transition, ProjectionManifest, error) {
	if err := validateSourceSnapshot(snapshot); err != nil {
		return nil, ProjectionManifest{}, err
	}
	candidates, err := projectAgentUnits(snapshot)
	if err != nil {
		return nil, ProjectionManifest{}, err
	}
	executionUnits, err := projectExecutionUnits(snapshot)
	if err != nil {
		return nil, ProjectionManifest{}, err
	}
	candidates = append(candidates, executionUnits...)
	terminal, err := projectTerminalUnit(snapshot)
	if err != nil {
		return nil, ProjectionManifest{}, err
	}
	candidates = append(candidates, terminal)
	slices.SortStableFunc(candidates, func(left, right projectedUnit) int {
		if left.order != right.order {
			if left.order < right.order {
				return -1
			}
			return 1
		}
		return strings.Compare(left.payload.Unit.ID, right.payload.Unit.ID)
	})
	manifest, err := NewProjectionManifest(
		snapshot.Cursor, snapshot.SourceSHA256, int64(len(candidates)),
	)
	if err != nil {
		return nil, ProjectionManifest{}, err
	}
	transitions := make([]Transition, 0, len(candidates))
	for index, candidate := range candidates {
		candidate.payload.Projection = manifest
		if err := candidate.payload.Validate(); err != nil {
			return nil, ProjectionManifest{}, err
		}
		payload, err := json.Marshal(candidate.payload)
		if err != nil {
			return nil, ProjectionManifest{}, fmt.Errorf("encode transition: %w", err)
		}
		transition := Transition{
			ResearchRunID: snapshot.ResearchRunID,
			Ordinal:       int64(index),
			AppendKey:     candidate.appendKey,
			SchemaVersion: SchemaVersion,
			PayloadJSON:   payload,
			ArtifactRefs:  candidate.artifacts,
		}
		transition.ContentSHA256, err = TransitionContentSHA256(
			transition.SchemaVersion,
			transition.PayloadJSON,
			transition.ArtifactRefs,
		)
		if err != nil {
			return nil, ProjectionManifest{}, err
		}
		if err := transition.NormalizeAndValidate(); err != nil {
			return nil, ProjectionManifest{}, err
		}
		transitions = append(transitions, transition)
	}
	return transitions, manifest, nil
}

func (p *TransitionPayloadV1) Validate() error {
	if p.SchemaVersion != TransitionSchemaVersion ||
		p.ProjectorVersion != ProjectorVersion ||
		strings.TrimSpace(p.Unit.Type) == "" ||
		strings.TrimSpace(p.Unit.ID) == "" {
		return fmt.Errorf("%w: transition envelope", ErrInvalid)
	}
	eventSlots := []struct {
		name string
		slot Slot[ResearchEvent]
		kind EventKind
	}{
		{"state", p.State, EventKindObservation},
		{"observation", p.Observation, EventKindObservation},
		{"candidate", p.Candidate, EventKindObservation},
		{"decision", p.Decision, EventKindDecision},
		{"action", p.Action, EventKindAction},
		{"execution", p.Execution, EventKindExecution},
		{"verification", p.Verification, EventKindVerification},
		{"failure", p.Failure, EventKindFailure},
		{"recovery", p.Recovery, EventKindRecovery},
		{"reward", p.Reward, EventKindReward},
		{"unknown", p.Unknown, EventKindUnknown},
	}
	for _, item := range eventSlots {
		if err := item.slot.Validate(item.name); err != nil {
			return err
		}
		if item.slot.Value != nil {
			if item.slot.Value.Kind != item.kind {
				return fmt.Errorf("%w: transition %s event kind", ErrInvalid, item.name)
			}
			if err := item.slot.Value.NormalizeAndValidate(); err != nil {
				return err
			}
		}
	}
	if err := p.Cost.Validate("cost"); err != nil {
		return err
	}
	if p.Cost.Value != nil {
		if err := p.Cost.Value.Validate(); err != nil {
			return err
		}
	}
	return p.Projection.NormalizeAndValidate()
}

func (c *CostSummary) Validate() error {
	for name, slot := range map[string]Slot[int64]{
		"input_tokens": c.InputTokens, "output_tokens": c.OutputTokens,
		"total_tokens": c.TotalTokens, "latency_ms": c.LatencyMS,
		"vision_calls": c.VisionCalls,
	} {
		if err := slot.Validate("cost." + name); err != nil {
			return err
		}
		if slot.Value != nil && *slot.Value < 0 {
			return fmt.Errorf("%w: cost %s", ErrInvalid, name)
		}
	}
	for index := range c.Sources {
		if err := c.Sources[index].NormalizeAndValidate(); err != nil {
			return err
		}
	}
	if c.Sources == nil {
		c.Sources = []SourceRef{}
	}
	slices.SortFunc(c.Sources, compareSourceRefs)
	return nil
}

type projectedUnit struct {
	order     int64
	appendKey string
	payload   TransitionPayloadV1
	artifacts []ArtifactRef
}

type toolCallAggregate struct {
	id           string
	tool         string
	firstSeq     int64
	stepID       string
	events       []AgentEventSnapshot
	llmEvents    []AgentEventSnapshot
	args         json.RawMessage
	result       json.RawMessage
	failed       json.RawMessage
	pending      bool
	resumed      bool
	checkpointID string
	artifacts    []map[string]any
}

type modelCallAggregate struct {
	id          string
	firstSeq    int64
	events      []AgentEventSnapshot
	toolCallIDs []string
}

func projectAgentUnits(snapshot SourceSnapshot) ([]projectedUnit, error) {
	tools := make(map[string]*toolCallAggregate)
	unknown := make([]AgentEventSnapshot, 0)
	modelCalls := make(map[string]*modelCallAggregate)
	knownTypes := map[string]bool{
		"run.started": true, "run.finished": true, "run.failed": true,
		"run.cancelled": true, "message.started": true, "message.delta": true,
		"message.finished": true, "tool.started": true, "tool.args.delta": true,
		"tool.pending": true, "tool.result": true, "tool.finished": true,
		"tool.failed": true, "artifact.published": true,
		"research.llm_call": true,
	}
	logicalAttempts := make(map[string]map[int64]bool)
	for _, event := range snapshot.Events {
		if !knownTypes[event.Type] {
			unknown = append(unknown, event)
			continue
		}
		if event.Type == "research.llm_call" {
			var payload struct {
				LogicalCallID string   `json:"logical_call_id"`
				Attempt       int64    `json:"attempt"`
				ToolCallIDs   []string `json:"tool_call_ids"`
			}
			if err := json.Unmarshal(event.Payload, &payload); err != nil {
				return nil, err
			}
			if logicalAttempts[payload.LogicalCallID] == nil {
				logicalAttempts[payload.LogicalCallID] = make(map[int64]bool)
			}
			if logicalAttempts[payload.LogicalCallID][payload.Attempt] {
				return nil, fmt.Errorf(
					"%w: duplicate LLM attempt %s/%d",
					ErrSourceChanged, payload.LogicalCallID, payload.Attempt,
				)
			}
			logicalAttempts[payload.LogicalCallID][payload.Attempt] = true
			modelCall := modelCalls[payload.LogicalCallID]
			if modelCall == nil {
				modelCall = &modelCallAggregate{
					id: payload.LogicalCallID, firstSeq: event.Seq,
				}
				modelCalls[payload.LogicalCallID] = modelCall
			}
			modelCall.events = append(modelCall.events, event)
			modelCall.toolCallIDs = append(
				modelCall.toolCallIDs, payload.ToolCallIDs...,
			)
			continue
		}
		if event.ToolCallID.Value == nil {
			continue
		}
		id := *event.ToolCallID.Value
		aggregate := tools[id]
		if aggregate == nil {
			aggregate = &toolCallAggregate{id: id, firstSeq: event.Seq}
			tools[id] = aggregate
		}
		if event.Seq < aggregate.firstSeq {
			aggregate.firstSeq = event.Seq
		}
		aggregate.events = append(aggregate.events, event)
		if event.StepID.Value != nil {
			if aggregate.stepID != "" && aggregate.stepID != *event.StepID.Value {
				return nil, fmt.Errorf("%w: tool call %s changed step_id", ErrSourceChanged, id)
			}
			aggregate.stepID = *event.StepID.Value
		}
		var payload map[string]json.RawMessage
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			return nil, err
		}
		if raw := payload["tool"]; len(raw) > 0 {
			var tool string
			if err := json.Unmarshal(raw, &tool); err == nil && tool != "" {
				if aggregate.tool != "" && aggregate.tool != tool {
					return nil, fmt.Errorf("%w: tool call %s changed tool", ErrSourceChanged, id)
				}
				aggregate.tool = tool
			}
		}
		switch event.Type {
		case "tool.args.delta":
			arguments, err := canonicalToolArguments(payload["arguments"])
			if err != nil {
				return nil, fmt.Errorf("%w: tool call %s arguments", ErrSourceChanged, id)
			}
			aggregate.args = arguments
		case "tool.result":
			aggregate.result = event.Payload
			if aggregate.pending {
				aggregate.resumed = true
			}
		case "tool.failed":
			aggregate.failed = event.Payload
		case "tool.pending":
			aggregate.pending = true
			if event.CheckpointID.Value != nil {
				aggregate.checkpointID = *event.CheckpointID.Value
			}
		case "artifact.published":
			var artifact map[string]any
			if err := json.Unmarshal(event.Payload, &artifact); err == nil {
				aggregate.artifacts = append(aggregate.artifacts, artifact)
			}
		}
	}
	standaloneModelCalls := make([]modelCallAggregate, 0)
	for _, modelCall := range modelCalls {
		modelCall.toolCallIDs = sortedUniqueStrings(modelCall.toolCallIDs)
		if err := validateModelAttempts(*modelCall); err != nil {
			return nil, err
		}
		linked := false
		for _, id := range modelCall.toolCallIDs {
			if aggregate := tools[id]; aggregate != nil {
				aggregate.llmEvents = append(aggregate.llmEvents, modelCall.events...)
				if modelCall.firstSeq < aggregate.firstSeq {
					aggregate.firstSeq = modelCall.firstSeq
				}
				linked = true
			}
		}
		if !linked {
			standaloneModelCalls = append(standaloneModelCalls, *modelCall)
		}
	}
	aggregates := make([]*toolCallAggregate, 0, len(tools))
	for _, aggregate := range tools {
		aggregates = append(aggregates, aggregate)
	}
	slices.SortFunc(aggregates, func(left, right *toolCallAggregate) int {
		if left.firstSeq < right.firstSeq {
			return -1
		}
		if left.firstSeq > right.firstSeq {
			return 1
		}
		return strings.Compare(left.id, right.id)
	})
	result := make(
		[]projectedUnit, 0,
		len(aggregates)+len(standaloneModelCalls)+len(unknown),
	)
	for _, aggregate := range aggregates {
		unit, err := projectToolCall(snapshot, *aggregate)
		if err != nil {
			return nil, err
		}
		result = append(result, unit)
	}
	slices.SortFunc(standaloneModelCalls, func(left, right modelCallAggregate) int {
		if left.firstSeq < right.firstSeq {
			return -1
		}
		if left.firstSeq > right.firstSeq {
			return 1
		}
		return strings.Compare(left.id, right.id)
	})
	for _, modelCall := range standaloneModelCalls {
		unit, err := projectStandaloneModelCall(snapshot.ResearchRunID, modelCall)
		if err != nil {
			return nil, err
		}
		result = append(result, unit)
	}
	for _, event := range unknown {
		unit, err := projectUnknownEvent(snapshot, event)
		if err != nil {
			return nil, err
		}
		result = append(result, unit)
	}
	return result, nil
}

func validateModelAttempts(call modelCallAggregate) error {
	attempts := make([]int64, 0, len(call.events))
	for _, event := range call.events {
		var payload struct {
			LogicalCallID string `json:"logical_call_id"`
			Attempt       int64  `json:"attempt"`
		}
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			return err
		}
		if payload.LogicalCallID != call.id {
			return fmt.Errorf("%w: logical call identity changed", ErrSourceChanged)
		}
		attempts = append(attempts, payload.Attempt)
	}
	slices.Sort(attempts)
	for index, attempt := range attempts {
		if attempt != int64(index+1) {
			return fmt.Errorf(
				"%w: logical call %s attempt sequence",
				ErrSourceChanged, call.id,
			)
		}
	}
	return nil
}

func projectStandaloneModelCall(
	researchRunID string,
	call modelCallAggregate,
) (projectedUnit, error) {
	payload := newTransitionPayload("model_call", call.id)
	decision, cost, err := projectDecisionAndCost(
		researchRunID,
		toolCallAggregate{
			id: call.id, firstSeq: call.firstSeq, llmEvents: call.events,
		},
		true,
	)
	if err != nil {
		return projectedUnit{}, err
	}
	payload.Decision = decision
	payload.Cost = cost
	return projectedUnit{
		order:     call.firstSeq * 1_000_000,
		appendKey: "model:" + call.id,
		payload:   payload,
		artifacts: []ArtifactRef{},
	}, nil
}

func projectToolCall(
	snapshot SourceSnapshot,
	call toolCallAggregate,
) (projectedUnit, error) {
	base := newTransitionPayload("agent_tool_call", call.id)
	state, err := newEvent(
		snapshot.ResearchRunID, EventKindObservation,
		Available(call.id), NotApplicable[string]("tool_call_has_no_prior_event"),
		[]string{call.id}, NotApplicable[int64]("agent_tool_call"),
		NotApplicable[int64]("agent_tool_call"), refsFromEvents(call.events),
		map[string]any{"phase": "agent", "step_id": call.stepID},
	)
	if err != nil {
		return projectedUnit{}, err
	}
	base.State = Available(state)

	decision, cost, err := projectDecisionAndCost(
		snapshot.ResearchRunID, call, false,
	)
	if err != nil {
		return projectedUnit{}, err
	}
	base.Decision = decision
	base.Cost = cost

	actionData := map[string]any{
		"tool": call.tool, "arguments": summarizedJSON(call.args),
		"pending": call.pending, "resumed": call.resumed,
	}
	if call.checkpointID != "" {
		actionData["checkpoint_id"] = call.checkpointID
	}
	if len(call.artifacts) > 0 {
		actionData["artifacts"] = call.artifacts
	}
	cause := NotApplicable[string]("model_decision_unavailable")
	if decision.Value != nil {
		cause = Available(decision.Value.ID)
	}
	action, err := newEvent(
		snapshot.ResearchRunID, EventKindAction, Available(call.id), cause,
		[]string{call.id}, NotApplicable[int64]("agent_tool_call"),
		NotApplicable[int64]("agent_tool_call"), refsFromEvents(call.events),
		actionData,
	)
	if err != nil {
		return projectedUnit{}, err
	}
	base.Action = Available(action)

	if len(call.result) > 0 {
		execution, err := newEvent(
			snapshot.ResearchRunID, EventKindExecution, Available(call.id),
			Available(action.ID), []string{call.id},
			NotApplicable[int64]("agent_tool_call"),
			NotApplicable[int64]("agent_tool_call"), refsFromEvents(call.events),
			toolResultSummary(call.result),
		)
		if err != nil {
			return projectedUnit{}, err
		}
		base.Execution = Available(execution)
		if isVerificationTool(call.tool) {
			verification, err := newEvent(
				snapshot.ResearchRunID, EventKindVerification, Available(call.id),
				Available(execution.ID), []string{call.id},
				NotApplicable[int64]("agent_tool_call"),
				NotApplicable[int64]("agent_tool_call"), refsFromEvents(call.events),
				map[string]any{
					"tool": call.tool, "result": toolResultSummary(call.result),
				},
			)
			if err != nil {
				return projectedUnit{}, err
			}
			base.Verification = Available(verification)
		}
		if isObservationTool(call.tool) {
			observation, err := newEvent(
				snapshot.ResearchRunID, EventKindObservation, Available(call.id),
				Available(execution.ID), []string{call.id},
				NotApplicable[int64]("agent_tool_call"),
				NotApplicable[int64]("agent_tool_call"), refsFromEvents(call.events),
				map[string]any{
					"tool": call.tool, "result": toolResultSummary(call.result),
				},
			)
			if err != nil {
				return projectedUnit{}, err
			}
			base.Observation = Available(observation)
			base.Candidate = Available(observation)
		}
	} else {
		reason := "tool_result_not_persisted"
		if call.pending {
			reason = "tool_waiting_for_resume"
		}
		base.Execution = Unavailable[ResearchEvent](reason)
	}
	if len(call.failed) > 0 {
		failure, err := newEvent(
			snapshot.ResearchRunID, EventKindFailure, Available(call.id),
			Available(action.ID), []string{call.id},
			NotApplicable[int64]("agent_tool_call"),
			NotApplicable[int64]("agent_tool_call"), refsFromEvents(call.events),
			map[string]any{"tool": call.tool, "failure": summarizedJSON(call.failed)},
		)
		if err != nil {
			return projectedUnit{}, err
		}
		base.Failure = Available(failure)
	}
	if call.tool == "fix_and_retry" {
		recovery, err := newEvent(
			snapshot.ResearchRunID, EventKindRecovery, Available(call.id),
			Available(action.ID), []string{call.id},
			NotApplicable[int64]("agent_tool_call"),
			NotApplicable[int64]("agent_tool_call"), refsFromEvents(call.events),
			map[string]any{"strategy": "fix_and_retry"},
		)
		if err != nil {
			return projectedUnit{}, err
		}
		base.Recovery = Available(recovery)
	}
	return projectedUnit{
		order: call.firstSeq * 1_000_000, appendKey: "tool:" + call.id,
		payload: base, artifacts: []ArtifactRef{},
	}, nil
}

func projectDecisionAndCost(
	researchRunID string,
	call toolCallAggregate,
	forceCostOwner bool,
) (Slot[ResearchEvent], Slot[CostSummary], error) {
	if len(call.llmEvents) == 0 {
		return Unavailable[ResearchEvent]("model_decision_source_not_persisted"),
			Unavailable[CostSummary]("model_cost_source_not_persisted"), nil
	}
	type attemptSummary struct {
		LogicalCallID string `json:"logical_call_id"`
		Attempt       int64  `json:"attempt"`
		Status        string `json:"status"`
		Provider      string `json:"provider"`
		Model         string `json:"model"`
		PromptVersion string `json:"prompt_version"`
		PromptSHA256  string `json:"prompt_sha256"`
		RequestSHA256 string `json:"request_sha256"`
	}
	attempts := make([]attemptSummary, 0, len(call.llmEvents))
	var input, output, total, latency int64
	allUsageAvailable := true
	refs := refsFromEvents(call.llmEvents)
	allToolCallIDs := make([]string, 0)
	for _, event := range call.llmEvents {
		var payload struct {
			LogicalCallID  string   `json:"logical_call_id"`
			Attempt        int64    `json:"attempt"`
			AttemptStatus  string   `json:"attempt_status"`
			Provider       string   `json:"provider"`
			ResolvedModel  string   `json:"resolved_model"`
			RequestedModel string   `json:"requested_model"`
			AttemptLatency int64    `json:"attempt_latency_ms"`
			ToolCallIDs    []string `json:"tool_call_ids"`
			PromptSpec     struct {
				Version       string `json:"version"`
				PromptSHA256  string `json:"prompt_sha256"`
				RequestSHA256 string `json:"request_sha256"`
			} `json:"prompt_spec"`
			Usage struct {
				Status       string `json:"status"`
				InputTokens  int64  `json:"input_tokens"`
				OutputTokens int64  `json:"output_tokens"`
				TotalTokens  int64  `json:"total_tokens"`
			} `json:"usage"`
		}
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			return Slot[ResearchEvent]{}, Slot[CostSummary]{}, err
		}
		model := payload.ResolvedModel
		if model == "" {
			model = payload.RequestedModel
		}
		attempts = append(attempts, attemptSummary{
			LogicalCallID: payload.LogicalCallID, Attempt: payload.Attempt,
			Status: payload.AttemptStatus, Provider: payload.Provider, Model: model,
			PromptVersion: payload.PromptSpec.Version,
			PromptSHA256:  payload.PromptSpec.PromptSHA256,
			RequestSHA256: payload.PromptSpec.RequestSHA256,
		})
		latency += payload.AttemptLatency
		allToolCallIDs = append(allToolCallIDs, payload.ToolCallIDs...)
		if payload.Usage.Status != "available" {
			allUsageAvailable = false
		} else {
			input += payload.Usage.InputTokens
			output += payload.Usage.OutputTokens
			total += payload.Usage.TotalTokens
		}
	}
	slices.SortFunc(attempts, func(left, right attemptSummary) int {
		if left.Attempt < right.Attempt {
			return -1
		}
		if left.Attempt > right.Attempt {
			return 1
		}
		return strings.Compare(left.LogicalCallID, right.LogicalCallID)
	})
	decision, err := newEvent(
		researchRunID, EventKindDecision, Available(call.id),
		NotApplicable[string]("model_call_is_root_decision"),
		sortedUniqueStrings(allToolCallIDs),
		NotApplicable[int64]("decision_aggregates_attempts"),
		NotApplicable[int64]("agent_tool_call"), refs,
		map[string]any{"attempts": attempts},
	)
	if err != nil {
		return Slot[ResearchEvent]{}, Slot[CostSummary]{}, err
	}
	owner := call.id
	for _, id := range sortedUniqueStrings(allToolCallIDs) {
		if id < owner {
			owner = id
		}
	}
	if !forceCostOwner && owner != call.id {
		return Available(decision),
			NotApplicable[CostSummary]("shared_model_cost_owned_by_tool_call:" + owner), nil
	}
	cost := CostSummary{
		LatencyMS:   Available(latency),
		VisionCalls: Available(int64(0)),
		Sources:     refs,
	}
	if allUsageAvailable {
		cost.InputTokens = Available(input)
		cost.OutputTokens = Available(output)
		cost.TotalTokens = Available(total)
	} else {
		cost.InputTokens = Unavailable[int64]("one_or_more_attempt_usage_unavailable")
		cost.OutputTokens = Unavailable[int64]("one_or_more_attempt_usage_unavailable")
		cost.TotalTokens = Unavailable[int64]("one_or_more_attempt_usage_unavailable")
	}
	return Available(decision), Available(cost), nil
}

func projectExecutionUnits(snapshot SourceSnapshot) ([]projectedUnit, error) {
	anchors := executionBatchAnchors(snapshot.Events)
	result := make([]projectedUnit, 0)
	generations := make(map[int64]GenerationSnapshot)
	for _, generation := range snapshot.Generations {
		generations[generation.ID] = generation
	}
	for _, batch := range snapshot.Batches {
		anchor := anchors[batch.ID]
		if anchor == 0 {
			return nil, fmt.Errorf(
				"%w: batch %d has no execution_batch artifact",
				ErrSourceChanged, batch.ID,
			)
		}
		generation := generations[batch.GenerationID]
		for _, job := range batch.Jobs {
			for _, execution := range job.Executions {
				if execution.Report.Value == nil {
					continue
				}
				var report struct {
					Status string            `json:"status"`
					Steps  []json.RawMessage `json:"steps"`
				}
				if err := json.Unmarshal(*execution.Report.Value, &report); err != nil {
					return nil, err
				}
				for _, rawStep := range report.Steps {
					unit, stepIndex, err := projectExecutionStep(
						snapshot, generation, batch, job, execution, rawStep,
					)
					if err != nil {
						return nil, err
					}
					unit.order = anchor*1_000_000 +
						job.OrderIndex*10_000 + execution.Attempt*1_000 + stepIndex
					result = append(result, unit)
				}
			}
		}
	}
	return result, nil
}

func projectExecutionStep(
	snapshot SourceSnapshot,
	generation GenerationSnapshot,
	batch BatchSnapshot,
	job JobSnapshot,
	execution ExecutionSnapshot,
	raw json.RawMessage,
) (projectedUnit, int64, error) {
	var step map[string]any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&step); err != nil {
		return projectedUnit{}, 0, err
	}
	stepIndex, err := int64Value(step["step_index"])
	if err != nil {
		return projectedUnit{}, 0, err
	}
	unitID := fmt.Sprintf(
		"batch:%d/job:%d/attempt:%d/step:%d",
		batch.ID, job.ID, execution.Attempt, stepIndex,
	)
	payload := newTransitionPayload("execution_step", unitID)
	sources := []SourceRef{
		generation.Ref, batch.Ref, job.Ref, execution.Ref, *execution.ReportRef.Value,
	}
	correlation := Available(fmt.Sprintf("batch:%d", batch.ID))
	stateData := map[string]any{
		"execution_id": execution.ID, "attempt": execution.Attempt,
		"step_index": stepIndex,
	}
	if preState, ok := step["pre_state"].(map[string]any); ok {
		stateData["pre_state"] = map[string]any{
			"url":                safeURLSummary(stringValue(preState["url"])),
			"dom_hash":           stringValue(preState["dom_hash"]),
			"visible_text_count": arrayLength(preState["visible_texts"]),
			"input_count":        objectLength(preState["input_values"]),
		}
	}
	state, err := newEvent(
		snapshot.ResearchRunID, EventKindObservation, correlation,
		Available(fmt.Sprintf("execution:%d", execution.ID)), nil,
		Available(execution.Attempt), Available(stepIndex), sources, stateData,
	)
	if err != nil {
		return projectedUnit{}, 0, err
	}
	payload.State = Available(state)

	locatorTrace, _ := step["locator_trace"].(map[string]any)
	observation, err := newEvent(
		snapshot.ResearchRunID, EventKindObservation, correlation,
		Available(state.ID), nil, Available(execution.Attempt), Available(stepIndex),
		sources,
		map[string]any{
			"locator_confidence":     stringValue(step["locator_confidence"]),
			"resolved_by":            stringValue(step["resolved_by"]),
			"candidate_count":        arrayLength(locatorTrace["candidates"]),
			"failure_reason_present": locatorTrace["failure_reason"] != nil,
		},
	)
	if err != nil {
		return projectedUnit{}, 0, err
	}
	payload.Observation = Available(observation)
	candidate, err := newEvent(
		snapshot.ResearchRunID, EventKindObservation, correlation,
		Available(observation.ID), nil, Available(execution.Attempt), Available(stepIndex),
		sources, candidateSummary(locatorTrace),
	)
	if err != nil {
		return projectedUnit{}, 0, err
	}
	payload.Candidate = Available(candidate)
	payload.Decision = NotApplicable[ResearchEvent]("execution_step_uses_approved_dsl")

	action, err := newEvent(
		snapshot.ResearchRunID, EventKindAction, correlation,
		Available(candidate.ID), nil, Available(execution.Attempt), Available(stepIndex),
		sources,
		map[string]any{
			"action":     stringValue(step["action"]),
			"target":     summarizedScalar(step["target"]),
			"value":      summarizedScalar(step["value"]),
			"dsl_sha256": execution.DSLSHA256,
		},
	)
	if err != nil {
		return projectedUnit{}, 0, err
	}
	payload.Action = Available(action)
	outcome, _ := step["action_outcome"].(map[string]any)
	executionEvent, err := newEvent(
		snapshot.ResearchRunID, EventKindExecution, correlation,
		Available(action.ID), nil, Available(execution.Attempt), Available(stepIndex),
		sources,
		map[string]any{
			"execution_id": execution.ID, "attempt": execution.Attempt,
			"status":            stringValue(step["status"]),
			"duration_ms":       int64OrZero(step["duration_ms"]),
			"action_status":     stringValue(outcome["status"]),
			"side_effect_state": stringValue(outcome["side_effect_state"]),
		},
	)
	if err != nil {
		return projectedUnit{}, 0, err
	}
	payload.Execution = Available(executionEvent)
	verification, err := newEvent(
		snapshot.ResearchRunID, EventKindVerification, correlation,
		Available(executionEvent.ID), nil, Available(execution.Attempt), Available(stepIndex),
		sources, verificationSummary(step),
	)
	if err != nil {
		return projectedUnit{}, 0, err
	}
	payload.Verification = Available(verification)
	if stringValue(step["status"]) == "failed" {
		failureData := map[string]any{
			"error":             summarizedScalar(step["error_message"]),
			"side_effect_state": stringValue(outcome["side_effect_state"]),
		}
		if execution.FailureSignal.Value != nil {
			failureData["signal"] = failureSignalSummary(*execution.FailureSignal.Value)
		}
		failure, err := newEvent(
			snapshot.ResearchRunID, EventKindFailure, correlation,
			Available(verification.ID), nil, Available(execution.Attempt),
			Available(stepIndex), sources, failureData,
		)
		if err != nil {
			return projectedUnit{}, 0, err
		}
		payload.Failure = Available(failure)
	}
	if execution.Attempt > 1 {
		recovery, err := newEvent(
			snapshot.ResearchRunID, EventKindRecovery, correlation,
			Available(executionEvent.ID), nil, Available(execution.Attempt),
			Available(stepIndex), sources,
			map[string]any{
				"attempt":          execution.Attempt,
				"previous_attempt": execution.Attempt - 1,
			},
		)
		if err != nil {
			return projectedUnit{}, 0, err
		}
		payload.Recovery = Available(recovery)
	}
	duration := int64OrZero(step["duration_ms"])
	visionCalls := int64(0)
	if used, _ := step["vlm_preverify_used"].(bool); used {
		visionCalls = 1
	}
	payload.Cost = Available(CostSummary{
		InputTokens:  NotApplicable[int64]("execution_step_has_no_llm_usage"),
		OutputTokens: NotApplicable[int64]("execution_step_has_no_llm_usage"),
		TotalTokens:  NotApplicable[int64]("execution_step_has_no_llm_usage"),
		LatencyMS:    Available(duration), VisionCalls: Available(visionCalls),
		Sources: sources,
	})
	return projectedUnit{
		appendKey: "execution:" + strconv.FormatInt(execution.ID, 10) +
			":step:" + strconv.FormatInt(stepIndex, 10),
		payload: payload, artifacts: []ArtifactRef{},
	}, stepIndex, nil
}

func projectTerminalUnit(snapshot SourceSnapshot) (projectedUnit, error) {
	payload := newTransitionPayload("terminal", snapshot.AgentRunID)
	terminalRefs := make([]SourceRef, 0)
	if len(snapshot.Events) > 0 {
		terminalRefs = append(terminalRefs, snapshot.Events[len(snapshot.Events)-1].Ref)
	}
	batchStatuses := make([]map[string]any, 0, len(snapshot.Batches))
	executionStatuses := make([]map[string]any, 0)
	for _, batch := range snapshot.Batches {
		terminalRefs = append(terminalRefs, batch.Ref)
		batchStatuses = append(batchStatuses, map[string]any{
			"id": batch.ID, "status": batch.Status,
			"generation_id": batch.GenerationID,
		})
		for _, job := range batch.Jobs {
			for _, execution := range job.Executions {
				terminalRefs = append(terminalRefs, execution.Ref)
				executionStatuses = append(executionStatuses, map[string]any{
					"id": execution.ID, "attempt": execution.Attempt,
					"status": execution.Status,
				})
			}
		}
	}
	state, err := newEvent(
		snapshot.ResearchRunID, EventKindObservation,
		Available(snapshot.AgentRunID), NotApplicable[string]("terminal_state"),
		nil, NotApplicable[int64]("terminal"), NotApplicable[int64]("terminal"),
		terminalRefs,
		map[string]any{
			"agent_run_id":     snapshot.AgentRunID,
			"agent_run_status": snapshot.AgentRunStatus,
		},
	)
	if err != nil {
		return projectedUnit{}, err
	}
	payload.State = Available(state)
	execution, err := newEvent(
		snapshot.ResearchRunID, EventKindExecution,
		Available(snapshot.AgentRunID), Available(state.ID), nil,
		NotApplicable[int64]("terminal"), NotApplicable[int64]("terminal"),
		terminalRefs,
		map[string]any{
			"agent_run_status": snapshot.AgentRunStatus,
			"batches":          batchStatuses, "executions": executionStatuses,
		},
	)
	if err != nil {
		return projectedUnit{}, err
	}
	payload.Execution = Available(execution)
	verification, err := newEvent(
		snapshot.ResearchRunID, EventKindVerification,
		Available(snapshot.AgentRunID), Available(execution.ID), nil,
		NotApplicable[int64]("terminal"), NotApplicable[int64]("terminal"),
		terminalRefs,
		map[string]any{
			"formal_execution_statuses": executionStatuses,
			"task_success":              "unavailable",
			"reason":                    "independent_oracle_not_persisted",
		},
	)
	if err != nil {
		return projectedUnit{}, err
	}
	payload.Verification = Available(verification)
	payload.Reward = Unavailable[ResearchEvent]("independent_oracle_not_persisted")
	payload.Cost = NotApplicable[CostSummary]("terminal_has_no_direct_cost")
	payload.Done = isTerminalAgentRunStatus(snapshot.AgentRunStatus)
	maxSeq := int64(0)
	if len(snapshot.Events) > 0 {
		maxSeq = snapshot.Events[len(snapshot.Events)-1].Seq
	}
	return projectedUnit{
		order:     maxSeq*1_000_000 + 999_999,
		appendKey: "terminal:" + snapshot.AgentRunID,
		payload:   payload, artifacts: []ArtifactRef{},
	}, nil
}

func projectUnknownEvent(
	snapshot SourceSnapshot,
	event AgentEventSnapshot,
) (projectedUnit, error) {
	payload := newTransitionPayload(
		"unknown_agent_event",
		fmt.Sprintf("%s:%d", snapshot.AgentRunID, event.Seq),
	)
	unknown, err := newEvent(
		snapshot.ResearchRunID, EventKindUnknown,
		Available(snapshot.AgentRunID), NotApplicable[string]("unknown_event"),
		toolCallIDs(event), NotApplicable[int64]("unknown_event"),
		NotApplicable[int64]("unknown_event"), []SourceRef{event.Ref},
		map[string]any{
			"source_event_type": event.Type,
			"payload":           summarizedJSON(event.Payload),
		},
	)
	if err != nil {
		return projectedUnit{}, err
	}
	payload.Unknown = Available(unknown)
	return projectedUnit{
		order:     event.Seq * 1_000_000,
		appendKey: fmt.Sprintf("unknown:%d", event.Seq),
		payload:   payload, artifacts: []ArtifactRef{},
	}, nil
}

func newTransitionPayload(unitType, unitID string) TransitionPayloadV1 {
	return TransitionPayloadV1{
		SchemaVersion: TransitionSchemaVersion, ProjectorVersion: ProjectorVersion,
		Unit:         TransitionUnit{Type: unitType, ID: unitID},
		State:        Unavailable[ResearchEvent]("state_source_unavailable"),
		Observation:  NotApplicable[ResearchEvent]("unit_has_no_observation"),
		Candidate:    NotApplicable[ResearchEvent]("unit_has_no_candidates"),
		Decision:     NotApplicable[ResearchEvent]("unit_has_no_decision"),
		Action:       NotApplicable[ResearchEvent]("unit_has_no_action"),
		Execution:    NotApplicable[ResearchEvent]("unit_has_no_execution"),
		Verification: NotApplicable[ResearchEvent]("unit_has_no_verification"),
		Failure:      NotApplicable[ResearchEvent]("unit_has_no_failure"),
		Recovery:     NotApplicable[ResearchEvent]("unit_has_no_recovery"),
		Reward:       Unavailable[ResearchEvent]("independent_oracle_not_persisted"),
		Unknown:      NotApplicable[ResearchEvent]("unit_has_no_unknown_event"),
		Cost:         NotApplicable[CostSummary]("unit_has_no_direct_cost"),
	}
}

func newEvent(
	researchRunID string,
	kind EventKind,
	correlation, causation Slot[string],
	toolCallIDs []string,
	attempt, stepIndex Slot[int64],
	sources []SourceRef,
	data any,
) (ResearchEvent, error) {
	raw, err := json.Marshal(data)
	if err != nil {
		return ResearchEvent{}, err
	}
	return NewResearchEvent(ResearchEvent{
		SchemaVersion: EventSchemaVersion, Kind: kind,
		ResearchRunID: researchRunID, CorrelationID: correlation,
		CausationID: causation, ToolCallIDs: toolCallIDs,
		Attempt: attempt, StepIndex: stepIndex, Sources: sources, Data: raw,
	})
}

func validateSourceSnapshot(snapshot SourceSnapshot) error {
	if snapshot.SchemaVersion != EventSchemaVersion ||
		snapshot.ResearchRunID == "" || snapshot.ProjectID <= 0 ||
		snapshot.AgentRunID == "" ||
		!validAgentRunStatus(snapshot.AgentRunStatus) {
		return fmt.Errorf("%w: source snapshot", ErrInvalid)
	}
	if len(snapshot.Events) == 0 {
		return fmt.Errorf("%w: source event stream is empty", ErrSourceChanged)
	}
	for index := range snapshot.Events {
		event := snapshot.Events[index]
		expectedSeq := int64(index + 1)
		if event.Seq != expectedSeq || strings.TrimSpace(event.Type) == "" {
			return fmt.Errorf(
				"%w: source event sequence %d",
				ErrSourceChanged, event.Seq,
			)
		}
		for name, slot := range map[string]Slot[string]{
			"step_id":       event.StepID,
			"tool_call_id":  event.ToolCallID,
			"parent_id":     event.ParentID,
			"checkpoint_id": event.CheckpointID,
		} {
			if err := slot.Validate("source event " + name); err != nil {
				return err
			}
			if slot.Value != nil &&
				(strings.TrimSpace(*slot.Value) == "" || len(*slot.Value) > 200) {
				return fmt.Errorf("%w: source event %s", ErrInvalid, name)
			}
		}
		if err := validateAgentEventSchema(event); err != nil {
			return err
		}
		expectedRef, err := sourceRef(
			SourceAgentEvent,
			fmt.Sprintf("%s:%d", snapshot.AgentRunID, event.Seq),
			Available(event.Seq),
			eventSchemaSlot(event),
			struct {
				Seq            int64           `json:"seq"`
				Type           string          `json:"type"`
				ConversationID string          `json:"conversation_id"`
				StepID         Slot[string]    `json:"step_id"`
				ToolCallID     Slot[string]    `json:"tool_call_id"`
				ParentID       Slot[string]    `json:"parent_id"`
				CheckpointID   Slot[string]    `json:"checkpoint_id"`
				Payload        json.RawMessage `json:"payload"`
			}{
				event.Seq, event.Type, event.ConversationID, event.StepID,
				event.ToolCallID, event.ParentID, event.CheckpointID, event.Payload,
			},
		)
		if err != nil {
			return err
		}
		if !sameSourceRef(event.Ref, expectedRef) {
			return fmt.Errorf(
				"%w: source event %d reference",
				ErrSourceChanged, event.Seq,
			)
		}
	}
	if err := snapshot.Reward.Validate("source.reward"); err != nil {
		return err
	}
	if snapshot.Reward.Status != SlotUnavailable ||
		snapshot.Reward.Reason != "independent_oracle_not_persisted" {
		return fmt.Errorf("%w: source reward must not be inferred", ErrInvalid)
	}
	cursor := snapshot.Cursor
	if err := cursor.NormalizeAndValidate(); err != nil {
		return err
	}
	expectedCursor := buildSourceCursor(snapshot)
	if err := expectedCursor.NormalizeAndValidate(); err != nil {
		return err
	}
	if !sameSourceCursor(cursor, expectedCursor) {
		return fmt.Errorf("%w: source cursor does not match source facts", ErrSourceChanged)
	}
	hash, err := sourceSnapshotHash(snapshot)
	if err != nil {
		return err
	}
	if hash != snapshot.SourceSHA256 {
		return fmt.Errorf("%w: source snapshot hash", ErrSourceChanged)
	}
	return nil
}

func sameSourceRef(left, right SourceRef) bool {
	leftRaw, leftErr := json.Marshal(left)
	rightRaw, rightErr := json.Marshal(right)
	return leftErr == nil && rightErr == nil && bytes.Equal(leftRaw, rightRaw)
}

func sameSourceCursor(left, right SourceCursor) bool {
	leftRaw, leftErr := json.Marshal(left)
	rightRaw, rightErr := json.Marshal(right)
	return leftErr == nil && rightErr == nil && bytes.Equal(leftRaw, rightRaw)
}

func validAgentRunStatus(status string) bool {
	switch status {
	case "running", "waiting_user", "completed", "failed", "cancelled":
		return true
	default:
		return false
	}
}

func isTerminalAgentRunStatus(status string) bool {
	return status == "completed" || status == "failed" || status == "cancelled"
}

func refsFromEvents(events []AgentEventSnapshot) []SourceRef {
	result := make([]SourceRef, 0, len(events))
	for _, event := range events {
		result = append(result, event.Ref)
	}
	slices.SortFunc(result, compareSourceRefs)
	return result
}

func toolCallIDs(event AgentEventSnapshot) []string {
	if event.ToolCallID.Value == nil {
		return []string{}
	}
	return []string{*event.ToolCallID.Value}
}

func executionBatchAnchors(events []AgentEventSnapshot) map[int64]int64 {
	result := make(map[int64]int64)
	for _, event := range events {
		if event.Type != "artifact.published" {
			continue
		}
		var payload struct {
			Type string `json:"type"`
			ID   string `json:"id"`
		}
		if json.Unmarshal(event.Payload, &payload) == nil &&
			payload.Type == "execution_batch" {
			if id, err := strconv.ParseInt(payload.ID, 10, 64); err == nil {
				result[id] = event.Seq
			}
		}
	}
	return result
}

func isObservationTool(tool string) bool {
	return tool == "explore_page" || tool == "explore_flow"
}

func isVerificationTool(tool string) bool {
	switch tool {
	case "validate_page_elements", "generate_dsl", "get_report":
		return true
	default:
		return false
	}
}

func toolResultSummary(raw json.RawMessage) map[string]any {
	var payload struct {
		SchemaVersion string          `json:"schema_version"`
		Tool          string          `json:"tool"`
		Content       json.RawMessage `json:"content"`
		ContentSHA256 string          `json:"content_sha256"`
		ContentBytes  int64           `json:"content_bytes"`
	}
	if json.Unmarshal(raw, &payload) != nil {
		return map[string]any{"payload": summarizedJSON(raw)}
	}
	result := map[string]any{
		"schema_version": payload.SchemaVersion, "tool": payload.Tool,
		"content_sha256": payload.ContentSHA256, "content_bytes": payload.ContentBytes,
	}
	if len(payload.Content) > 0 {
		var content map[string]any
		if json.Unmarshal(payload.Content, &content) == nil {
			for _, key := range []string{
				"status", "generation_id", "batch_id", "case_id",
				"dsl_sha256", "dsl_canonical_version",
			} {
				if value, exists := content[key]; exists {
					result[key] = value
				}
			}
			result["top_level_keys"] = sortedMapKeys(content)
		}
	}
	return result
}

func summarizedJSON(raw json.RawMessage) map[string]any {
	if len(raw) == 0 {
		return map[string]any{"present": false}
	}
	canonical, err := CanonicalJSON(raw)
	if err != nil {
		return map[string]any{"present": true, "valid": false}
	}
	hash, _ := CanonicalSHA256(json.RawMessage(canonical))
	var value any
	_ = json.Unmarshal(canonical, &value)
	result := map[string]any{
		"present": true, "valid": true, "sha256": hash, "bytes": len(canonical),
	}
	switch typed := value.(type) {
	case map[string]any:
		result["type"] = "object"
		result["keys"] = sortedMapKeys(typed)
	case []any:
		result["type"] = "array"
		result["items"] = len(typed)
	default:
		result["type"] = fmt.Sprintf("%T", typed)
	}
	return result
}

func summarizedScalar(value any) map[string]any {
	if value == nil {
		return map[string]any{"status": "not_applicable"}
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return map[string]any{"status": "unavailable", "reason": "encode_failed"}
	}
	hash, err := CanonicalSHA256(value)
	if err != nil {
		return map[string]any{"status": "unavailable", "reason": "canonicalize_failed"}
	}
	return map[string]any{
		"status": "available", "sha256": hash, "bytes": len(raw),
		"type": fmt.Sprintf("%T", value),
	}
}

func candidateSummary(trace map[string]any) map[string]any {
	result := map[string]any{
		"candidate_count": arrayLength(trace["candidates"]),
		"match_strategy":  stringValue(trace["match_strategy"]),
		"selected":        trace["selected_candidate"] != nil,
	}
	if selected, ok := trace["selected_candidate"].(map[string]any); ok {
		result["selected_candidate"] = map[string]any{
			"role":             stringValue(selected["role"]),
			"strategy":         stringValue(selected["strategy"]),
			"score":            selected["score"],
			"visible":          selected["visible"],
			"enabled":          selected["enabled"],
			"matched_rules":    selected["matched_rules"],
			"rejected_reasons": selected["rejected_reasons"],
		}
	}
	return result
}

func verificationSummary(step map[string]any) map[string]any {
	result := map[string]any{
		"step_status":     stringValue(step["status"]),
		"condition_count": arrayLength(step["condition_results"]),
	}
	conditions, _ := step["condition_results"].([]any)
	items := make([]map[string]any, 0, len(conditions))
	for _, value := range conditions {
		condition, ok := value.(map[string]any)
		if !ok {
			continue
		}
		items = append(items, map[string]any{
			"type":        stringValue(condition["type"]),
			"phase":       stringValue(condition["phase"]),
			"status":      stringValue(condition["status"]),
			"duration_ms": int64OrZero(condition["duration_ms"]),
			"expected":    summarizedScalar(condition["expected"]),
			"actual":      summarizedScalar(condition["actual"]),
		})
	}
	result["conditions"] = items
	return result
}

func failureSignalSummary(raw json.RawMessage) map[string]any {
	var signal map[string]any
	if json.Unmarshal(raw, &signal) != nil {
		return summarizedJSON(raw)
	}
	result := make(map[string]any)
	for _, key := range []string{
		"schema_version", "category", "stage", "code", "retryable",
		"side_effect_committed", "step_index", "fingerprint",
	} {
		if value, exists := signal[key]; exists {
			result[key] = value
		}
	}
	return result
}

func safeURLSummary(raw string) map[string]any {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return map[string]any{"status": "unavailable", "reason": "url_unavailable"}
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return summarizedScalar(raw)
	}
	return map[string]any{
		"status": "available", "scheme": parsed.Scheme,
		"host": parsed.Hostname(), "path": parsed.EscapedPath(),
	}
}

func sortedMapKeys(value map[string]any) []string {
	keys := make([]string, 0, len(value))
	for key := range value {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}

func arrayLength(value any) int {
	items, _ := value.([]any)
	return len(items)
}

func objectLength(value any) int {
	object, _ := value.(map[string]any)
	return len(object)
}

func int64Value(value any) (int64, error) {
	switch typed := value.(type) {
	case json.Number:
		return typed.Int64()
	case float64:
		return int64(typed), nil
	case int64:
		return typed, nil
	case int:
		return int64(typed), nil
	default:
		return 0, fmt.Errorf("%w: expected integer, got %T", ErrInvalid, value)
	}
}

func int64OrZero(value any) int64 {
	result, _ := int64Value(value)
	return result
}
