package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/domain"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/event"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/model"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/postgres"
)

const (
	runtimeStepEventType       = "runtime.step.completed"
	runtimeCompletionEventType = "runtime.completed"
)

// PostgresCheckpointStore adapts the Task 3 transactional ledger to Engine.
type PostgresCheckpointStore struct {
	Store *postgres.Store
}

// PostgresRetryStore persists consumer failures across Consumer instances.
type PostgresRetryStore struct {
	Store    *postgres.Store
	Consumer string
}

// Increment atomically increments the durable delivery count.
func (s PostgresRetryStore) Increment(ctx context.Context, eventID string, payload []byte) (RetryIncrement, error) {
	increment, err := s.Store.IncrementConsumerRetryUnlessDLQHandoff(
		ctx,
		s.Consumer,
		eventID,
		stableID("consumer-dlq", eventID),
		payload,
	)
	if err != nil {
		return RetryIncrement{}, err
	}
	return RetryIncrement{
		Attempts:     increment.Attempts,
		DLQHandedOff: increment.DLQHandedOff,
	}, nil
}

// Clear removes a completed delivery's retry state.
func (s PostgresRetryStore) Clear(ctx context.Context, eventID string) error {
	return s.Store.ClearConsumerRetry(ctx, s.Consumer, eventID)
}

// HandoffDLQ atomically enqueues the DLQ record and removes retry state.
func (s PostgresRetryStore) HandoffDLQ(ctx context.Context, eventID string, handoff DLQHandoff) error {
	return s.Store.HandoffConsumerRetryToOutbox(
		ctx,
		s.Consumer,
		eventID,
		handoff.EventID,
		handoff.Topic,
		handoff.Key,
		handoff.Payload,
	)
}

// HasDLQHandoff reports whether this source record was already handed to the
// durable outbox during a previous delivery.
func (s PostgresRetryStore) HasDLQHandoff(ctx context.Context, _ string, outboxEventID string) (bool, error) {
	return s.Store.HasConsumerRetryDLQHandoff(ctx, outboxEventID)
}

// Load reconstructs Engine state only from PostgreSQL's immutable request and durable events.
func (s PostgresCheckpointStore) Load(ctx context.Context, runID string) (Checkpoint, error) {
	run, command, err := s.Store.LoadRunCommand(ctx, runID)
	if err != nil {
		return Checkpoint{}, err
	}
	checkpoint := Checkpoint{
		RunID:           runID,
		Command:         command,
		NextCheckpoint:  run.CheckpointVersion,
		CancelRequested: run.CancelRequestedAt != nil,
		Completed: run.Status == domain.RunSucceeded ||
			run.Status == domain.RunFailed || run.Status == domain.RunCancelled,
	}
	events, err := s.Store.ListRunEvents(ctx, runID)
	if err != nil {
		return Checkpoint{}, err
	}
	for _, durableEvent := range events {
		if durableEvent.EventType != runtimeStepEventType {
			continue
		}
		var step StepResult
		if err := json.Unmarshal(durableEvent.Payload, &step); err != nil {
			return Checkpoint{}, fmt.Errorf("decode runtime checkpoint step: %w", err)
		}
		applyStep(&checkpoint, step)
	}
	return checkpoint, nil
}

// StartAttempt records an external call before it is issued without changing
// the completed-step checkpoint.
func (s PostgresCheckpointStore) StartAttempt(ctx context.Context, step StepResult) error {
	now := time.Now().UTC()
	span := domain.Span{
		ID:        step.LogicalSpanID,
		RunID:     step.RunID,
		TraceID:   stableID("trace", step.RunID),
		SpanID:    step.LogicalSpanID,
		Kind:      spanKind(step.Kind),
		Name:      string(step.Kind),
		Status:    "started",
		StartedAt: now,
	}
	attempt := postgres.TraceAttempt{
		ID:            step.AttemptID,
		AttemptNumber: 1,
		Status:        "started",
		StartedAt:     now,
	}
	switch step.Kind {
	case StepModel:
		request, err := json.Marshal(step.ModelRequest)
		if err != nil {
			return fmt.Errorf("marshal started model request: %w", err)
		}
		span.Input = request
		attempt.Provider = "model"
		attempt.Request = request
	case StepOperation:
		request, err := json.Marshal(step.OperationCall)
		if err != nil {
			return fmt.Errorf("marshal started operation request: %w", err)
		}
		span.Input = request
		attempt.OperationID = step.OperationCall.OperationID
		attempt.Request = request
	default:
		return fmt.Errorf("unknown attempt kind %q", step.Kind)
	}
	return s.Store.StartTraceAttempt(ctx, postgres.TraceAttemptStart{
		ExpectedCheckpoint: step.Checkpoint,
		Span:               span,
		Attempt:            attempt,
	})
}

// CommitStep persists a complete trace step, event, checkpoint advance, and outbox notification.
func (s PostgresCheckpointStore) CommitStep(ctx context.Context, step StepResult) error {
	now := time.Now().UTC()
	payload, err := json.Marshal(step)
	if err != nil {
		return fmt.Errorf("marshal runtime step: %w", err)
	}
	eventID := stableID("event", step.RunID, fmt.Sprintf("%d", step.Checkpoint), string(step.Kind))
	durableEvent := event.Envelope[json.RawMessage]{
		SchemaVersion: "agenttrace.event.v1",
		EventID:       eventID,
		EventType:     runtimeStepEventType,
		AggregateID:   step.RunID,
		CorrelationID: step.RunID,
		OccurredAt:    now,
		Payload:       payload,
	}
	span := domain.Span{
		ID:        step.LogicalSpanID,
		RunID:     step.RunID,
		TraceID:   stableID("trace", step.RunID),
		SpanID:    step.LogicalSpanID,
		Kind:      spanKind(step.Kind),
		Name:      string(step.Kind),
		Status:    stepStatus(step),
		StartedAt: now,
		EndedAt:   &now,
	}
	attempt := postgres.TraceAttempt{
		ID:            step.AttemptID,
		AttemptNumber: 1,
		Status:        span.Status,
		StartedAt:     now,
		EndedAt:       &now,
	}
	traceStep := postgres.TraceStep{
		ExpectedCheckpoint: step.Checkpoint,
		Span:               span,
		Attempt:            &attempt,
		Event:              durableEvent,
	}
	switch step.Kind {
	case StepModel:
		request, _ := json.Marshal(step.ModelRequest)
		response, _ := json.Marshal(step.Model)
		attempt.Provider = "model"
		attempt.Request = request
		attempt.Response = response
		traceStep.Usage = &postgres.TraceUsage{
			ID:       stableID("usage", step.AttemptID),
			Provider: "model",
			Usage:    usageFor(step.Model),
		}
	case StepOperation:
		request, _ := json.Marshal(step.OperationCall)
		response, _ := json.Marshal(step.OperationResult)
		attempt.OperationID = step.OperationCall.OperationID
		attempt.Request = request
		attempt.Response = response
		traceStep.Operation = &postgres.TraceOperationResult{
			ID:          stableID("result", step.OperationCall.OperationID),
			OperationID: step.OperationCall.OperationID,
			Status:      step.OperationResult.Status,
			Result:      step.OperationResult.Output,
			Artifacts:   step.OperationResult.Artifacts,
			Error:       step.OperationResult.Error,
		}
	}
	return s.Store.AppendTraceStep(ctx, traceStep)
}

// CompleteRun atomically persists terminal run state, its event, and outbox notification.
func (s PostgresCheckpointStore) CompleteRun(ctx context.Context, completion Completion) error {
	payload, err := json.Marshal(completion)
	if err != nil {
		return fmt.Errorf("marshal completion: %w", err)
	}
	eventID := stableID("event", completion.RunID, "terminal", string(completion.Status))
	envelope := event.Envelope[json.RawMessage]{
		SchemaVersion: "agenttrace.event.v1",
		EventID:       eventID,
		EventType:     runtimeCompletionEventType,
		AggregateID:   completion.RunID,
		CorrelationID: completion.RunID,
		OccurredAt:    time.Now().UTC(),
		Payload:       payload,
	}
	var runError json.RawMessage
	if completion.Error != "" {
		runError, _ = json.Marshal(map[string]string{"message": completion.Error})
	}
	return s.Store.FinishRun(ctx, completion.RunID, completionStatus(completion.Status), runError, envelope)
}

func applyStep(checkpoint *Checkpoint, step StepResult) {
	if step.Kind == StepModel {
		checkpoint.ModelCalls++
		checkpoint.Messages = append(checkpoint.Messages, step.Model.Message)
		checkpoint.PendingOperation = step.OperationCall
		return
	}
	if step.Kind == StepOperation && step.OperationCall != nil {
		checkpoint.Messages = append(checkpoint.Messages, modelToolMessage(step))
		checkpoint.PendingOperation = nil
	}
}

func modelToolMessage(step StepResult) model.Message {
	return model.Message{
		Role:       model.RoleTool,
		Content:    string(step.OperationResult.Output),
		ToolCallID: step.OperationCall.ToolCallID,
	}
}

func spanKind(kind StepKind) domain.SpanKind {
	if kind == StepOperation {
		return domain.SpanOperation
	}
	return domain.SpanModel
}

func stepStatus(step StepResult) string {
	if step.Kind == StepOperation && step.OperationResult.Status != "" {
		return step.OperationResult.Status
	}
	return "succeeded"
}

func completionStatus(status CompletionStatus) domain.RunStatus {
	switch status {
	case CompletionSucceeded:
		return domain.RunSucceeded
	case CompletionCancelled:
		return domain.RunCancelled
	default:
		return domain.RunFailed
	}
}
