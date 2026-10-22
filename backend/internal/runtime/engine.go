// Package runtime executes checkpointed ReAct runs.
package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/event"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/live"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/model"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/operation"
)

// Engine coordinates durable model turns and operation calls.
type Engine struct {
	Model         model.Model
	Operations    operation.Dispatcher
	Store         CheckpointStore
	Live          live.LiveStore
	MaxModelCalls int
}

// Run processes command from its latest durable checkpoint.
func (e Engine) Run(ctx context.Context, command event.RunCommand) error {
	if e.Store == nil {
		return errors.New("runtime checkpoint store is required")
	}
	if e.Model == nil {
		return errors.New("runtime model is required")
	}
	if strings.TrimSpace(command.RunID) == "" {
		return errors.New("runtime run id is required")
	}

	for {
		checkpoint, err := e.Store.Load(ctx, command.RunID)
		if err != nil {
			return fmt.Errorf("load run checkpoint: %w", err)
		}
		if checkpoint.Completed {
			return nil
		}
		if checkpoint.CancelRequested {
			return e.complete(ctx, checkpoint, CompletionCancelled, "", ErrCancelled)
		}
		if checkpoint.PendingOperation != nil {
			if err := e.runOperation(ctx, checkpoint); err != nil {
				return err
			}
			continue
		}
		if e.maxModelCalls() > 0 && checkpoint.ModelCalls >= e.maxModelCalls() {
			return e.complete(ctx, checkpoint, CompletionFailed, "", ErrModelCallBudget)
		}
		if err := e.runModel(ctx, checkpoint); err != nil {
			return err
		}
	}
}

func (e Engine) runModel(ctx context.Context, checkpoint Checkpoint) error {
	request := model.Request{
		Model:    checkpoint.Command.Model.Name,
		Messages: initialMessages(checkpoint),
		Tools:    modelTools(checkpoint.Command.Operations),
	}
	step := StepResult{
		RunID:         checkpoint.RunID,
		Checkpoint:    checkpoint.NextCheckpoint,
		Kind:          StepModel,
		LogicalSpanID: LogicalSpanID(checkpoint.RunID, checkpoint.NextCheckpoint, StepModel),
		ModelRequest:  request,
	}
	step.AttemptID = AttemptID(step.LogicalSpanID, 1)
	if err := e.Store.StartAttempt(ctx, step); err != nil {
		return fmt.Errorf("start model attempt: %w", err)
	}
	response, err := completeModel(ctx, e.Model, checkpoint, request)
	if err != nil {
		return e.complete(ctx, checkpoint, CompletionFailed, "", fmt.Errorf("model completion: %w", err))
	}
	step.Model = response
	if response.Report != "" {
		step.Kind = StepReport
	}
	step.SpanName = modelSpanName(step.Kind, response.Stage)
	if len(response.Message.ToolCalls) > 1 {
		return e.complete(ctx, checkpoint, CompletionFailed, "", errors.New("multiple tool calls are not supported"))
	}
	if len(response.Message.ToolCalls) == 1 {
		call := response.Message.ToolCalls[0]
		step.OperationCall = &OperationCall{
			ToolCallID:  call.ID,
			Name:        call.Name,
			Arguments:   append(json.RawMessage(nil), call.Arguments...),
			OperationID: OperationID(checkpoint.RunID, checkpoint.NextCheckpoint+1),
		}
	}
	if err := e.Store.CommitStep(ctx, step); err != nil {
		return fmt.Errorf("commit model checkpoint: %w", err)
	}
	e.appendLive(ctx, checkpoint.RunID, step)
	if len(response.Message.ToolCalls) == 0 {
		checkpoint.NextCheckpoint++
		if response.Report != "" {
			return nil
		}
		return e.complete(ctx, checkpoint, CompletionSucceeded, response.Message.Content, nil)
	}
	return nil
}

func completeModel(
	ctx context.Context,
	selected model.Model,
	checkpoint Checkpoint,
	request model.Request,
) (model.Response, error) {
	if checkpointed, ok := selected.(model.CheckpointedModel); ok {
		return checkpointed.CompleteForRun(ctx, checkpoint.RunID, checkpoint.ModelCalls, request)
	}
	return selected.Complete(ctx, request)
}

func (e Engine) runOperation(ctx context.Context, checkpoint Checkpoint) error {
	if e.Operations == nil {
		return e.complete(ctx, checkpoint, CompletionFailed, "", errors.New("runtime operation dispatcher is required"))
	}
	pending := checkpoint.PendingOperation
	spanID := LogicalSpanID(checkpoint.RunID, checkpoint.NextCheckpoint, StepOperation)
	call := operation.Call{
		RunID:       checkpoint.RunID,
		SpanID:      spanID,
		OperationID: pending.OperationID,
		ToolCallID:  pending.ToolCallID,
		Name:        pending.Name,
		Arguments:   pending.Arguments,
	}
	step := StepResult{
		RunID:         checkpoint.RunID,
		Checkpoint:    checkpoint.NextCheckpoint,
		Kind:          StepOperation,
		LogicalSpanID: spanID,
		AttemptID:     AttemptID(spanID, 1),
		OperationCall: pending,
	}
	if err := e.Store.StartAttempt(ctx, step); err != nil {
		return fmt.Errorf("start operation attempt: %w", err)
	}
	result, err := e.Operations.Dispatch(ctx, call)
	if err != nil {
		return e.complete(ctx, checkpoint, CompletionFailed, "", fmt.Errorf("dispatch operation: %w", err))
	}
	step.OperationResult = result
	if err := e.Store.CommitStep(ctx, step); err != nil {
		return fmt.Errorf("commit operation checkpoint: %w", err)
	}
	e.appendLive(ctx, checkpoint.RunID, step)
	return nil
}

func (e Engine) complete(
	ctx context.Context,
	checkpoint Checkpoint,
	status CompletionStatus,
	assistant string,
	runErr error,
) error {
	completion := Completion{
		RunID: checkpoint.RunID, Status: status, Assistant: assistant,
		NextCheckpoint: checkpoint.NextCheckpoint,
	}
	if runErr != nil {
		completion.Error = runErr.Error()
	}
	if err := e.Store.CompleteRun(ctx, completion); err != nil {
		return fmt.Errorf("complete run: %w", err)
	}
	if runErr != nil {
		return runErr
	}
	return nil
}

func (e Engine) appendLive(ctx context.Context, runID string, step StepResult) {
	if e.Live == nil {
		return
	}
	_ = e.Live.Append(ctx, runID, live.LiveEvent{
		Sequence: step.Checkpoint + 1,
		Type:     "runtime.step.completed",
		Data:     stepEventData(step),
	})
}

func (e Engine) maxModelCalls() int {
	if e.MaxModelCalls == 0 {
		return 25
	}
	return e.MaxModelCalls
}

func modelTools(operations []event.Operation) []model.Tool {
	tools := make([]model.Tool, 0, len(operations))
	for _, operation := range operations {
		var parameters map[string]any
		_ = json.Unmarshal(operation.InputSchema, &parameters)
		tools = append(tools, model.Tool{
			Name: operation.Name, Description: operation.Description, Parameters: parameters,
		})
	}
	return tools
}

func modelSpanName(kind StepKind, stage model.Stage) string {
	if kind == StepReport {
		return string(StepReport)
	}
	switch stage {
	case model.StagePlan, model.StageFinal:
		return string(stage)
	default:
		return string(StepModel)
	}
}
