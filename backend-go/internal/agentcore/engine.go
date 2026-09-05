package agentcore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/tools"
)

const defaultSystemPrompt = `You are AgentCore for a web UI testing platform.
Understand the user's goal, plan the work, and call the available tools.
Use ask_user_question only when required information or explicit approval is missing.
For a new test, use explore_page for the first known URL, then explore_flow when later page states require interaction.
Before generate_dsl, call validate_page_elements with required_elements derived from the user's flow and the collected a11y_nodes.
If validation reports missing requirements, explore again instead of inventing elements.
Only call generate_dsl after validation allows it.
After generate_dsl, use ask_user_question with a required confirm question whose id is approve_dsl.
Never call execute_dsl until that approval tool result is true for the latest generation.
When execute_dsl returns a batch_id, use get_report to read its current result.
The get_report tool waits for a terminal result by default; call it once instead of polling repeatedly.
For a failed batch, call fix_and_retry first and follow its strategy. Never skip DSL validation or approval during repair.
Never claim that a tool ran unless its result is present.
Never invent page elements, execution results, or report data.
When the task is complete, answer concisely in the user's language.`

type Engine struct {
	runs         *Service
	model        Model
	tools        *tools.Registry
	systemPrompt string
	maxSteps     int
}

func NewEngine(runs *Service, model Model, registry *tools.Registry, maxSteps int) *Engine {
	if maxSteps < 1 {
		maxSteps = 1
	}
	return &Engine{
		runs:         runs,
		model:        model,
		tools:        registry,
		systemPrompt: defaultSystemPrompt,
		maxSteps:     maxSteps,
	}
}

func (e *Engine) Start(ctx context.Context, conversationID string, input string) (AgentRun, error) {
	run, err := e.runs.StartProjectRun(ctx, conversationID, 0, input)
	if err != nil {
		return AgentRun{}, err
	}
	return e.Continue(ctx, run.ID)
}

func (e *Engine) StartAsync(conversationID string, input string) (AgentRun, error) {
	return e.StartProjectAsync(conversationID, 0, input)
}

func (e *Engine) StartProjectAsync(
	conversationID string,
	projectID int64,
	input string,
) (AgentRun, error) {
	return e.StartOwnedProjectAsync(context.Background(), 0, conversationID, projectID, input)
}

func (e *Engine) StartOwnedProjectAsync(
	ctx context.Context,
	actorUserID int64,
	conversationID string,
	projectID int64,
	input string,
) (AgentRun, error) {
	runContext := context.WithoutCancel(ctx)
	run, err := e.runs.StartOwnedProjectRun(
		runContext,
		actorUserID,
		conversationID,
		projectID,
		input,
	)
	if err != nil {
		return AgentRun{}, err
	}
	go func() {
		_, _ = e.Continue(runContext, run.ID)
	}()
	return run, nil
}

func (e *Engine) Continue(ctx context.Context, runID string) (AgentRun, error) {
	run, err := e.runs.GetRun(ctx, runID)
	if err != nil {
		return AgentRun{}, err
	}
	if run.Status != RunStatusRunning {
		return run, nil
	}

	for range e.maxSteps {
		response, modelErr := e.model.Complete(
			ctx,
			append([]Message{{Role: "system", Content: e.systemPrompt}}, run.Transcript...),
			e.toolDefinitions(),
		)
		if modelErr != nil {
			failedRun, _ := e.runs.FailRun(ctx, run, modelErr)
			return failedRun, modelErr
		}

		assistantMessage := Message{
			Role:      "assistant",
			Content:   response.Content,
			ToolCalls: response.ToolCalls,
		}
		run.Transcript = append(run.Transcript, assistantMessage)
		if strings.TrimSpace(response.Content) != "" {
			if err := e.recordMessage(ctx, run, response.Content); err != nil {
				return AgentRun{}, err
			}
		}
		if len(response.ToolCalls) == 0 {
			if err := e.runs.SaveRun(ctx, run); err != nil {
				return AgentRun{}, err
			}
			return e.runs.CompleteRun(ctx, run)
		}

		for _, call := range response.ToolCalls {
			stepID := e.runs.newID("step")
			if err := e.recordToolStart(ctx, run, stepID, call); err != nil {
				return AgentRun{}, err
			}
			result, executeErr := e.tools.Execute(ctx, tools.Call{
				RunID:                run.ID,
				ConversationID:       run.ConversationID,
				ProjectID:            run.ProjectID,
				LatestGenerationID:   run.LatestGenerationID,
				ApprovedGenerationID: run.ApprovedGenerationID,
				ToolCallID:           call.ID,
				Name:                 call.Name,
				Arguments:            json.RawMessage(call.Arguments),
			})
			if executeErr != nil {
				_ = e.recordToolFailure(ctx, run, stepID, call, executeErr)
				failedRun, _ := e.runs.FailRun(ctx, run, executeErr)
				return failedRun, executeErr
			}
			if result.Pending != nil {
				var request AskUserRequest
				if err := json.Unmarshal(result.Pending.Payload, &request); err != nil {
					failedRun, _ := e.runs.FailRun(ctx, run, err)
					return failedRun, err
				}
				if err := e.runs.SaveRun(ctx, run); err != nil {
					return AgentRun{}, err
				}
				waitingRun, _, err := e.runs.RequestUserInputForCall(
					ctx,
					run.ID,
					call.ID,
					stepID,
					request,
				)
				return waitingRun, err
			}
			if result.Artifact != nil && result.Artifact.Type == "dsl_generation" {
				generationID, parseErr := strconv.ParseInt(result.Artifact.ID, 10, 64)
				if parseErr != nil {
					_ = e.recordToolFailure(ctx, run, stepID, call, parseErr)
					failedRun, _ := e.runs.FailRun(ctx, run, parseErr)
					return failedRun, parseErr
				}
				run.LatestGenerationID = &generationID
				run.ApprovedGenerationID = nil
			}
			if err := e.recordToolResult(ctx, run, stepID, call, result); err != nil {
				return AgentRun{}, err
			}
			run.Transcript = append(run.Transcript, Message{
				Role:       "tool",
				Content:    string(result.Content),
				ToolCallID: call.ID,
			})
			if err := e.runs.SaveRun(ctx, run); err != nil {
				return AgentRun{}, err
			}
		}
	}

	maxStepsErr := fmt.Errorf("agent exceeded maximum tool steps: %d", e.maxSteps)
	failedRun, _ := e.runs.FailRun(ctx, run, maxStepsErr)
	return failedRun, maxStepsErr
}

func (e *Engine) Resume(
	ctx context.Context,
	runID string,
	toolCallID string,
	request ResumeToolCallRequest,
) (AgentRun, error) {
	run, err := e.runs.ResumeToolCall(ctx, runID, toolCallID, request)
	if err != nil {
		return AgentRun{}, err
	}
	result, err := json.Marshal(request)
	if err != nil {
		return AgentRun{}, fmt.Errorf("encode tool resume result: %w", err)
	}
	run.Transcript = append(run.Transcript, Message{
		Role:       "tool",
		Content:    string(result),
		ToolCallID: toolCallID,
	})
	if approved, ok := request.Answers["approve_dsl"].(bool); ok && approved {
		run.ApprovedGenerationID = run.LatestGenerationID
	}
	if err := e.runs.SaveRun(ctx, run); err != nil {
		return AgentRun{}, err
	}
	return e.Continue(ctx, run.ID)
}

func (e *Engine) ResumeOwned(
	ctx context.Context,
	actorUserID int64,
	runID string,
	toolCallID string,
	request ResumeToolCallRequest,
) (AgentRun, error) {
	if _, err := e.runs.GetOwnedRun(ctx, runID, actorUserID); err != nil {
		return AgentRun{}, err
	}
	return e.Resume(ctx, runID, toolCallID, request)
}

func (e *Engine) GetRun(ctx context.Context, runID string) (AgentRun, error) {
	return e.runs.GetRun(ctx, runID)
}

func (e *Engine) GetOwnedRun(
	ctx context.Context,
	runID string,
	actorUserID int64,
) (AgentRun, error) {
	return e.runs.GetOwnedRun(ctx, runID, actorUserID)
}

func (e *Engine) ListEvents(ctx context.Context, runID string, afterSeq int64) ([]Event, error) {
	return e.runs.ListEvents(ctx, runID, afterSeq)
}

func (e *Engine) ListOwnedEvents(
	ctx context.Context,
	runID string,
	actorUserID int64,
	afterSeq int64,
) ([]Event, error) {
	if _, err := e.runs.GetOwnedRun(ctx, runID, actorUserID); err != nil {
		return nil, err
	}
	return e.runs.ListEvents(ctx, runID, afterSeq)
}

func (e *Engine) Subscribe(runID string) Subscription {
	return e.runs.Subscribe(runID)
}

func (e *Engine) toolDefinitions() []ToolDefinition {
	definitions := e.tools.Definitions()
	result := make([]ToolDefinition, 0, len(definitions))
	for _, definition := range definitions {
		result = append(result, ToolDefinition{
			Name:        definition.Name,
			Description: definition.Description,
			InputSchema: definition.InputSchema,
		})
	}
	return result
}

func (e *Engine) recordMessage(ctx context.Context, run AgentRun, content string) error {
	stepID := e.runs.newID("step")
	events := []Event{
		{Type: EventMessageStarted, StepID: stepID},
		{Type: EventMessageDelta, StepID: stepID, Payload: map[string]any{"delta": content}},
		{Type: EventMessageFinished, StepID: stepID, Payload: map[string]any{"content": content}},
	}
	for _, event := range events {
		if _, err := e.runs.RecordEvent(ctx, run, event); err != nil {
			return err
		}
	}
	return nil
}

func (e *Engine) recordToolResult(
	ctx context.Context,
	run AgentRun,
	stepID string,
	call ModelTool,
	result tools.Result,
) error {
	if len(result.Content) == 0 || !json.Valid(result.Content) {
		return errors.New("tool result content must be valid JSON")
	}
	events := []Event{
		{
			Type:       EventToolResult,
			StepID:     stepID,
			ToolCallID: call.ID,
			Payload:    map[string]any{"tool": call.Name, "content": json.RawMessage(result.Content)},
		},
		{
			Type:       EventToolFinished,
			StepID:     stepID,
			ToolCallID: call.ID,
			Payload:    map[string]any{"tool": call.Name},
		},
	}
	if result.Artifact != nil {
		events = append(events, Event{
			Type:       EventArtifact,
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
			return err
		}
	}
	return nil
}

func (e *Engine) recordToolStart(
	ctx context.Context,
	run AgentRun,
	stepID string,
	call ModelTool,
) error {
	events := []Event{
		{
			Type:       EventToolStarted,
			StepID:     stepID,
			ToolCallID: call.ID,
			Payload:    map[string]any{"tool": call.Name},
		},
		{
			Type:       EventToolArgsDelta,
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

func (e *Engine) recordToolFailure(
	ctx context.Context,
	run AgentRun,
	stepID string,
	call ModelTool,
	cause error,
) error {
	_, err := e.runs.RecordEvent(ctx, run, Event{
		Type:       EventToolFailed,
		StepID:     stepID,
		ToolCallID: call.ID,
		Payload: map[string]any{
			"tool":    call.Name,
			"message": cause.Error(),
		},
	})
	return err
}
