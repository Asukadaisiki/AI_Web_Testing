package agentcore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/tools"
)

const defaultSystemPrompt = `You are AgentCore for a web UI testing platform.
Understand the user's goal, plan the work, and call the available tools.
Use ask_user_question only when required information or explicit approval is missing.
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
	run, err := e.runs.StartProjectRun(context.Background(), conversationID, projectID, input)
	if err != nil {
		return AgentRun{}, err
	}
	go func() {
		_, _ = e.Continue(context.Background(), run.ID)
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
			_, _ = e.runs.FailRun(ctx, run, modelErr)
			return AgentRun{}, modelErr
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
			result, executeErr := e.tools.Execute(ctx, tools.Call{
				RunID:      run.ID,
				ProjectID:  run.ProjectID,
				ToolCallID: call.ID,
				Name:       call.Name,
				Arguments:  json.RawMessage(call.Arguments),
			})
			if executeErr != nil {
				_, _ = e.runs.FailRun(ctx, run, executeErr)
				return AgentRun{}, executeErr
			}
			if result.Pending != nil {
				var request AskUserRequest
				if err := json.Unmarshal(result.Pending.Payload, &request); err != nil {
					_, _ = e.runs.FailRun(ctx, run, err)
					return AgentRun{}, err
				}
				if err := e.runs.SaveRun(ctx, run); err != nil {
					return AgentRun{}, err
				}
				waitingRun, _, err := e.runs.RequestUserInputForCall(
					ctx,
					run.ID,
					call.ID,
					e.runs.newID("step"),
					request,
				)
				return waitingRun, err
			}
			if err := e.recordToolResult(ctx, run, call, result); err != nil {
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
	_, _ = e.runs.FailRun(ctx, run, maxStepsErr)
	return AgentRun{}, maxStepsErr
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
	if err := e.runs.SaveRun(ctx, run); err != nil {
		return AgentRun{}, err
	}
	return e.Continue(ctx, run.ID)
}

func (e *Engine) GetRun(ctx context.Context, runID string) (AgentRun, error) {
	return e.runs.GetRun(ctx, runID)
}

func (e *Engine) ListEvents(ctx context.Context, runID string, afterSeq int64) ([]Event, error) {
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
	call ModelTool,
	result tools.Result,
) error {
	if len(result.Content) == 0 || !json.Valid(result.Content) {
		return errors.New("tool result content must be valid JSON")
	}
	stepID := e.runs.newID("step")
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
	for _, event := range events {
		if _, err := e.runs.RecordEvent(ctx, run, event); err != nil {
			return err
		}
	}
	return nil
}
