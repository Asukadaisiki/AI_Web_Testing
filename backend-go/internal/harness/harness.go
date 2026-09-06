package harness

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/agent"
	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/agentservice"
	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/tools"
)

const defaultSystemPrompt = `You are AgentCore for a web UI testing platform.
Understand the user's goal, plan the work, and call the available tools.
Use ask_user_question only when required information or explicit approval is missing.
For a new test, use explore_page for the first known URL, then explore_flow when later page states require interaction.
Exploration tool results use agent.model_tool_summary.v1. Use pages[].a11y_nodes as the exact evidence submitted in generate_dsl.a11y_nodes_by_state; source.event_seq and hashes reference the complete persisted tool.result event.
Never invent omitted nodes or selectors. Re-explore when the retained evidence is insufficient.
You may call validate_page_elements with required_elements to find exploration gaps, but that advisory result does not authorize generation.
As soon as the evidence is sufficient, call generate_dsl. It validates the exact final case against a11y_nodes_by_state and persists it atomically.
Author the complete structured DSL in generate_dsl.case and include collected nodes grouped by their actual page state. Never flatten states.
	DSL steps may only use goto, click, input, wait_for, assert_text, assert_url_contains, and capture_text. Use wait_for or postconditions for visibility checks; assert_visible is not supported.
	goto and assert_url_contains store their URL in value. assert_text requires both target and expected value. input requires target and value. capture_text requires target and context_key.
	Every click on an anchor that should move to another page must declare a url_contains postcondition whose value identifies the concrete destination URL or path. url_changes alone is insufficient because hash or interstitial navigation is not the intended destination.
	Only set target_strategy to css, xpath, data-testid, element_id, or tag. For semantic targets, omit it; null is accepted only as the equivalent optional boundary value.
	Explicit CSS targets must exactly match a selector from verified_selectors. Never compose a new descendant CSS selector from separately observed nodes.
	When the user asks to search through page controls, the final DSL must contain the real input step followed by the real search-control click. Never replace those actions with goto to a constructed search-result URL.
	Input trigger is optional and only accepts Enter or Tab. Omit trigger for ordinary semantic input, and represent a search-button action as a separate click step.
	Do not include candidates, match_count, or locator_confidence in generate_dsl.case; locator preflight derives those fields from a11y_nodes_by_state.
After generate_dsl, use ask_user_question with a required confirm question whose id is approve_dsl.
Never call execute_dsl until that approval tool result is true for the latest generation.
When execute_dsl returns a batch_id, use get_report to read its current result.
The get_report tool waits for a terminal result by default; call it once instead of polling repeatedly.
For a failed batch, call fix_and_retry first and follow its strategy. Never skip DSL validation or approval during repair.
Never claim that a tool ran unless its result is present.
Never invent page elements, execution results, or report data.
When the task is complete, answer concisely in the user's language.`

type Harness struct {
	runs   *agentservice.Service
	loop   *agent.Loop
	tools  *tools.Registry
	policy ToolPolicy

	activeMu   sync.Mutex
	activeRuns map[string]*activeRun
}

type activeRun struct {
	cancel context.CancelFunc
}

func New(runs *agentservice.Service, model agent.Model, registry *tools.Registry, maxSteps int) *Harness {
	return &Harness{
		runs:       runs,
		loop:       agent.NewLoop(model, toolDefinitions(registry), defaultSystemPrompt, maxSteps),
		tools:      registry,
		policy:     DefaultToolPolicy{},
		activeRuns: make(map[string]*activeRun),
	}
}

func (e *Harness) Start(ctx context.Context, conversationID string, input string) (agentservice.AgentRun, error) {
	run, err := e.runs.StartProjectRun(ctx, conversationID, 0, input)
	if err != nil {
		return agentservice.AgentRun{}, err
	}
	return e.Continue(ctx, run.ID)
}

func (e *Harness) StartAsync(conversationID string, input string) (agentservice.AgentRun, error) {
	return e.StartProjectAsync(conversationID, 0, input)
}

func (e *Harness) StartProjectAsync(
	conversationID string,
	projectID int64,
	input string,
) (agentservice.AgentRun, error) {
	return e.StartOwnedProjectAsync(context.Background(), 0, conversationID, projectID, input)
}

func (e *Harness) StartOwnedProjectAsync(
	ctx context.Context,
	actorUserID int64,
	conversationID string,
	projectID int64,
	input string,
) (agentservice.AgentRun, error) {
	runContext := context.WithoutCancel(ctx)
	run, err := e.runs.StartOwnedProjectRun(
		runContext,
		actorUserID,
		conversationID,
		projectID,
		input,
	)
	if err != nil {
		return agentservice.AgentRun{}, err
	}
	go func() {
		_, _ = e.Continue(runContext, run.ID)
	}()
	return run, nil
}

func (e *Harness) Continue(ctx context.Context, runID string) (agentservice.AgentRun, error) {
	runContext, cancel := context.WithCancel(ctx)
	active := &activeRun{cancel: cancel}
	e.activeMu.Lock()
	previous := e.activeRuns[runID]
	e.activeRuns[runID] = active
	e.activeMu.Unlock()
	if previous != nil {
		previous.cancel()
	}
	defer func() {
		e.activeMu.Lock()
		if e.activeRuns[runID] == active {
			delete(e.activeRuns, runID)
		}
		e.activeMu.Unlock()
		cancel()
	}()

	return e.continueRun(runContext, runID)
}

func (e *Harness) continueRun(ctx context.Context, runID string) (agentservice.AgentRun, error) {
	run, err := e.runs.GetRun(ctx, runID)
	if err != nil {
		return agentservice.AgentRun{}, err
	}
	if run.Status != agentservice.RunStatusRunning {
		return run, nil
	}

	loopErr := e.loop.RunWithModelContext(
		ctx,
		&run.Transcript,
		func(callContext context.Context) context.Context {
			logicalCallID := e.runs.NewID("llm")
			stepID := e.runs.NewID("step")
			return agent.WithTelemetryRecorder(
				callContext,
				func(recordContext context.Context, record agent.TelemetryRecord) error {
					return e.runs.RecordModelTelemetry(recordContext, run, record)
				},
				logicalCallID,
				stepID,
			)
		},
		func(ctx context.Context, response agent.ModelResponse) (bool, error) {
			if strings.TrimSpace(response.Content) != "" {
				if err := e.recordMessage(ctx, run, response.Content); err != nil {
					return false, err
				}
			}
			if len(response.ToolCalls) == 0 {
				if err := e.runs.SaveRun(ctx, run); err != nil {
					return false, err
				}
				run, err = e.runs.CompleteRun(ctx, run)
				return false, err
			}

			for _, call := range response.ToolCalls {
				stepID := e.runs.NewID("step")
				if err := e.recordToolStart(ctx, run, stepID, call); err != nil {
					return false, err
				}
				if err := e.policy.BeforeToolCall(run, call); err != nil {
					if recordErr := e.recordRecoverableToolFailure(ctx, &run, stepID, call, err); recordErr != nil {
						return false, recordErr
					}
					continue
				}
				result, executeErr := e.tools.Execute(ctx, tools.Call{
					RunID:                run.ID,
					ActorUserID:          run.ActorUserID,
					ConversationID:       run.ConversationID,
					ProjectID:            run.ProjectID,
					LatestGenerationID:   run.LatestGenerationID,
					ApprovedGenerationID: run.ApprovedGenerationID,
					ToolCallID:           call.ID,
					Name:                 call.Name,
					Arguments:            json.RawMessage(call.Arguments),
				})
				if executeErr != nil {
					if errors.Is(executeErr, context.Canceled) {
						return false, executeErr
					}
					if recordErr := e.recordRecoverableToolFailure(ctx, &run, stepID, call, executeErr); recordErr != nil {
						return false, recordErr
					}
					continue
				}
				if result.Pending != nil {
					var request agentservice.AskUserRequest
					if err := json.Unmarshal(result.Pending.Payload, &request); err != nil {
						return false, err
					}
					if err := e.runs.SaveRun(ctx, run); err != nil {
						return false, err
					}
					run, _, err = e.runs.RequestUserInputForCall(
						ctx,
						run.ID,
						call.ID,
						stepID,
						request,
					)
					return false, err
				}
				if result.Artifact != nil && result.Artifact.Type == "dsl_generation" {
					generationID, parseErr := strconv.ParseInt(result.Artifact.ID, 10, 64)
					if parseErr != nil {
						_ = e.recordToolFailure(ctx, run, stepID, call, parseErr)
						return false, parseErr
					}
					run.LatestGenerationID = &generationID
					run.ApprovedGenerationID = nil
				}
				sourceEventSeq, err := e.recordToolResult(ctx, run, stepID, call, result)
				if err != nil {
					return false, err
				}
				modelContent, err := agent.BuildModelToolSummary(
					call.Name,
					result.Content,
					sourceEventSeq,
				)
				if err != nil {
					return false, fmt.Errorf("summarize tool result: %w", err)
				}
				run.Transcript = append(run.Transcript, agent.Message{
					Role:       "tool",
					Content:    modelContent,
					ToolCallID: call.ID,
				})
				run.Transcript = agent.CompactExplorationTranscript(run.Transcript)
				if err := e.runs.SaveRun(ctx, run); err != nil {
					return false, err
				}
			}
			return true, nil
		},
	)
	if loopErr != nil {
		current, getErr := e.runs.GetRun(context.WithoutCancel(ctx), run.ID)
		if getErr == nil && current.Status == agentservice.RunStatusCancelled {
			return current, nil
		}
		if errors.Is(loopErr, context.Canceled) ||
			errors.Is(loopErr, agentservice.ErrRunCancelled) {
			if getErr == nil {
				return current, loopErr
			}
			return run, loopErr
		}
		failedRun, _ := e.runs.FailRun(ctx, run, loopErr)
		return failedRun, loopErr
	}
	return run, nil
}

func (e *Harness) Resume(
	ctx context.Context,
	runID string,
	toolCallID string,
	request agentservice.ResumeToolCallRequest,
) (agentservice.AgentRun, error) {
	run, err := e.runs.ResumeToolCall(ctx, runID, toolCallID, request)
	if err != nil {
		return agentservice.AgentRun{}, err
	}
	result, err := json.Marshal(request)
	if err != nil {
		return agentservice.AgentRun{}, fmt.Errorf("encode tool resume result: %w", err)
	}
	run.Transcript = append(run.Transcript, agent.Message{
		Role:       "tool",
		Content:    string(result),
		ToolCallID: toolCallID,
	})
	if approved, ok := request.Answers["approve_dsl"].(bool); ok && approved {
		run.ApprovedGenerationID = run.LatestGenerationID
	}
	if err := e.runs.SaveRun(ctx, run); err != nil {
		return agentservice.AgentRun{}, err
	}
	return e.Continue(ctx, run.ID)
}

func (e *Harness) ResumeOwned(
	ctx context.Context,
	actorUserID int64,
	runID string,
	toolCallID string,
	request agentservice.ResumeToolCallRequest,
) (agentservice.AgentRun, error) {
	if _, err := e.runs.GetOwnedRun(ctx, runID, actorUserID); err != nil {
		return agentservice.AgentRun{}, err
	}
	return e.Resume(ctx, runID, toolCallID, request)
}

func (e *Harness) GetRun(ctx context.Context, runID string) (agentservice.AgentRun, error) {
	return e.runs.GetRun(ctx, runID)
}

func (e *Harness) GetOwnedRun(
	ctx context.Context,
	runID string,
	actorUserID int64,
) (agentservice.AgentRun, error) {
	return e.runs.GetOwnedRun(ctx, runID, actorUserID)
}

func (e *Harness) CancelOwned(
	ctx context.Context,
	actorUserID int64,
	runID string,
	reason string,
) (agentservice.AgentRun, error) {
	run, err := e.runs.CancelOwnedRun(ctx, runID, actorUserID, reason)
	if err != nil {
		return agentservice.AgentRun{}, err
	}
	if run.Status == agentservice.RunStatusCancelled {
		e.activeMu.Lock()
		active := e.activeRuns[runID]
		e.activeMu.Unlock()
		if active != nil {
			active.cancel()
		}
	}
	return run, nil
}

func (e *Harness) ListEvents(ctx context.Context, runID string, afterSeq int64) ([]agentservice.Event, error) {
	return e.runs.ListEvents(ctx, runID, afterSeq)
}

func (e *Harness) ListOwnedEvents(
	ctx context.Context,
	runID string,
	actorUserID int64,
	afterSeq int64,
) ([]agentservice.Event, error) {
	if _, err := e.runs.GetOwnedRun(ctx, runID, actorUserID); err != nil {
		return nil, err
	}
	return e.runs.ListEvents(ctx, runID, afterSeq)
}

func (e *Harness) Subscribe(runID string) agentservice.Subscription {
	return e.runs.Subscribe(runID)
}

func toolDefinitions(registry *tools.Registry) []agent.ToolDefinition {
	definitions := registry.Definitions()
	result := make([]agent.ToolDefinition, 0, len(definitions))
	for _, definition := range definitions {
		result = append(result, agent.ToolDefinition{
			Name:        definition.Name,
			Description: definition.Description,
			InputSchema: definition.InputSchema,
		})
	}
	return result
}

func (e *Harness) recordMessage(ctx context.Context, run agentservice.AgentRun, content string) error {
	stepID := e.runs.NewID("step")
	events := []agentservice.Event{
		{Type: agentservice.EventMessageStarted, StepID: stepID},
		{Type: agentservice.EventMessageDelta, StepID: stepID, Payload: map[string]any{"delta": content}},
		{Type: agentservice.EventMessageFinished, StepID: stepID, Payload: map[string]any{"content": content}},
	}
	for _, event := range events {
		if _, err := e.runs.RecordEvent(ctx, run, event); err != nil {
			return err
		}
	}
	return nil
}

func (e *Harness) recordToolResult(
	ctx context.Context,
	run agentservice.AgentRun,
	stepID string,
	call agent.ModelTool,
	result tools.Result,
) (int64, error) {
	payload, err := agent.NewToolResultEventPayload(call.Name, result.Content)
	if err != nil {
		return 0, err
	}
	encodedPayload, err := json.Marshal(payload)
	if err != nil {
		return 0, fmt.Errorf("encode tool result event: %w", err)
	}
	var eventPayload map[string]any
	if err := json.Unmarshal(encodedPayload, &eventPayload); err != nil {
		return 0, fmt.Errorf("normalize tool result event: %w", err)
	}
	persisted, err := e.runs.RecordEvent(ctx, run, agentservice.Event{
		Type:       agentservice.EventToolResult,
		StepID:     stepID,
		ToolCallID: call.ID,
		Payload:    eventPayload,
	})
	if err != nil {
		return 0, err
	}
	events := []agentservice.Event{
		{
			Type:       agentservice.EventToolFinished,
			StepID:     stepID,
			ToolCallID: call.ID,
			Payload:    map[string]any{"tool": call.Name},
		},
	}
	if result.Artifact != nil {
		events = append(events, agentservice.Event{
			Type:       agentservice.EventArtifact,
			StepID:     stepID,
			ToolCallID: call.ID,
			Payload: map[string]any{
				"type": result.Artifact.Type,
				"id":   result.Artifact.ID,
			},
		})
	}
	for _, event := range events {
		if _, err := e.runs.RecordEvent(ctx, run, event); err != nil {
			return 0, err
		}
	}
	return persisted.Seq, nil
}

func (e *Harness) recordToolStart(
	ctx context.Context,
	run agentservice.AgentRun,
	stepID string,
	call agent.ModelTool,
) error {
	events := []agentservice.Event{
		{
			Type:       agentservice.EventToolStarted,
			StepID:     stepID,
			ToolCallID: call.ID,
			Payload:    map[string]any{"tool": call.Name},
		},
		{
			Type:       agentservice.EventToolArgsDelta,
			StepID:     stepID,
			ToolCallID: call.ID,
			Payload:    map[string]any{"arguments": call.Arguments},
		},
	}
	for _, event := range events {
		if _, err := e.runs.RecordEvent(ctx, run, event); err != nil {
			return err
		}
	}
	return nil
}

func (e *Harness) recordToolFailure(
	ctx context.Context,
	run agentservice.AgentRun,
	stepID string,
	call agent.ModelTool,
	cause error,
) error {
	_, err := e.runs.RecordEvent(ctx, run, agentservice.Event{
		Type:       agentservice.EventToolFailed,
		StepID:     stepID,
		ToolCallID: call.ID,
		Payload: map[string]any{
			"tool":    call.Name,
			"message": cause.Error(),
		},
	})
	return err
}

func (e *Harness) recordRecoverableToolFailure(
	ctx context.Context,
	run *agentservice.AgentRun,
	stepID string,
	call agent.ModelTool,
	cause error,
) error {
	if err := e.recordToolFailure(ctx, *run, stepID, call, cause); err != nil {
		return err
	}
	content, err := json.Marshal(map[string]any{
		"status":  "error",
		"tool":    call.Name,
		"message": cause.Error(),
	})
	if err != nil {
		return fmt.Errorf("encode tool failure result: %w", err)
	}
	run.Transcript = append(run.Transcript, agent.Message{
		Role:       "tool",
		Content:    string(content),
		ToolCallID: call.ID,
	})
	if err := e.runs.SaveRun(ctx, *run); err != nil {
		return fmt.Errorf("save recoverable tool failure: %w", err)
	}
	return nil
}
