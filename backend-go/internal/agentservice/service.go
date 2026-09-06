package agentservice

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/agent"
)

type Clock func() time.Time
type IDGenerator func(prefix string) string

type Service struct {
	repository Repository
	broker     *EventBroker
	now        Clock
	newID      IDGenerator
}

func NewService(repository Repository) *Service {
	return NewServiceWithDependencies(repository, NewEventBroker(), time.Now, randomID)
}

func NewServiceWithDependencies(
	repository Repository,
	broker *EventBroker,
	now Clock,
	newID IDGenerator,
) *Service {
	return &Service{
		repository: repository,
		broker:     broker,
		now:        now,
		newID:      newID,
	}
}

func (s *Service) StartRun(ctx context.Context, conversationID string, input string) (AgentRun, error) {
	return s.StartProjectRun(ctx, conversationID, 0, input)
}

func (s *Service) StartProjectRun(
	ctx context.Context,
	conversationID string,
	projectID int64,
	input string,
) (AgentRun, error) {
	return s.StartOwnedProjectRun(ctx, 0, conversationID, projectID, input)
}

func (s *Service) StartOwnedProjectRun(
	ctx context.Context,
	actorUserID int64,
	conversationID string,
	projectID int64,
	input string,
) (AgentRun, error) {
	conversationID = strings.TrimSpace(conversationID)
	input = strings.TrimSpace(input)
	if conversationID == "" {
		return AgentRun{}, errors.New("conversation_id is required")
	}
	if input == "" {
		return AgentRun{}, errors.New("input is required")
	}

	now := s.now().UTC()
	run := AgentRun{
		ID:             s.newID("run"),
		ActorUserID:    actorUserID,
		ConversationID: conversationID,
		ProjectID:      projectID,
		Status:         RunStatusRunning,
		Input:          input,
		Transcript:     []agent.Message{{Role: "user", Content: input}},
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if err := s.repository.CreateRun(ctx, run); err != nil {
		return AgentRun{}, fmt.Errorf("create agent run: %w", err)
	}
	if _, err := s.appendEvent(ctx, run, Event{
		Type:    EventRunStarted,
		StepID:  s.newID("step"),
		Payload: map[string]any{"input": input},
	}); err != nil {
		return AgentRun{}, err
	}
	return run, nil
}

func (s *Service) GetRun(ctx context.Context, runID string) (AgentRun, error) {
	return s.repository.GetRun(ctx, runID)
}

func (s *Service) GetOwnedRun(
	ctx context.Context,
	runID string,
	actorUserID int64,
) (AgentRun, error) {
	run, err := s.repository.GetRun(ctx, runID)
	if err != nil {
		return AgentRun{}, err
	}
	if actorUserID < 1 || run.ActorUserID != actorUserID {
		return AgentRun{}, ErrRunAccessDenied
	}
	return run, nil
}

func (s *Service) ListEvents(ctx context.Context, runID string, afterSeq int64) ([]Event, error) {
	if afterSeq < 0 {
		return nil, errors.New("after_seq must not be negative")
	}
	return s.repository.ListEvents(ctx, runID, afterSeq)
}

func (s *Service) Subscribe(runID string) Subscription {
	return s.broker.Subscribe(runID)
}

func (s *Service) NewID(prefix string) string {
	return s.newID(prefix)
}

func (s *Service) RequestUserInput(
	ctx context.Context,
	runID string,
	request AskUserRequest,
) (AgentRun, Event, error) {
	run, err := s.GetRun(ctx, runID)
	if err != nil {
		return AgentRun{}, Event{}, err
	}
	toolCallID := s.newID("tool")
	stepID := s.newID("step")
	if _, err := s.RecordEvent(ctx, run, Event{
		Type:       EventToolStarted,
		StepID:     stepID,
		ToolCallID: toolCallID,
		Payload:    map[string]any{"tool": "ask_user_question"},
	}); err != nil {
		return AgentRun{}, Event{}, err
	}
	if _, err := s.RecordEvent(ctx, run, Event{
		Type:       EventToolArgsDelta,
		StepID:     stepID,
		ToolCallID: toolCallID,
		Payload:    map[string]any{"arguments": request},
	}); err != nil {
		return AgentRun{}, Event{}, err
	}
	return s.RequestUserInputForCall(
		ctx,
		runID,
		toolCallID,
		stepID,
		request,
	)
}

func (s *Service) RequestUserInputForCall(
	ctx context.Context,
	runID string,
	toolCallID string,
	stepID string,
	request AskUserRequest,
) (AgentRun, Event, error) {
	if err := validateQuestions(request.Questions); err != nil {
		return AgentRun{}, Event{}, err
	}
	run, err := s.repository.GetRun(ctx, runID)
	if err != nil {
		return AgentRun{}, Event{}, err
	}
	if run.Status != RunStatusRunning {
		return AgentRun{}, Event{}, fmt.Errorf("cannot request user input from run status %q", run.Status)
	}

	pendingEvent, err := s.appendEvent(ctx, run, Event{
		Type:         EventToolPending,
		StepID:       stepID,
		ToolCallID:   toolCallID,
		CheckpointID: s.newID("checkpoint"),
		Payload:      map[string]any{"tool": "ask_user_question", "questions": request.Questions},
	})
	if err != nil {
		return AgentRun{}, Event{}, err
	}

	run.Status = RunStatusWaitingUser
	run.PendingToolCallID = &toolCallID
	run.PendingStepID = &stepID
	run.UpdatedAt = s.now().UTC()
	if err := s.repository.SaveRun(ctx, run); err != nil {
		return AgentRun{}, Event{}, fmt.Errorf("save waiting agent run: %w", err)
	}
	return run, pendingEvent, nil
}

func (s *Service) SaveRun(ctx context.Context, run AgentRun) error {
	run.UpdatedAt = s.now().UTC()
	return s.repository.SaveRun(ctx, run)
}

func (s *Service) CancelOwnedRun(
	ctx context.Context,
	runID string,
	actorUserID int64,
	reason string,
) (AgentRun, error) {
	if _, err := s.GetOwnedRun(ctx, runID, actorUserID); err != nil {
		return AgentRun{}, err
	}
	return s.CancelRun(ctx, runID, reason)
}

func (s *Service) CancelRun(
	ctx context.Context,
	runID string,
	reason string,
) (AgentRun, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return AgentRun{}, errors.New("cancel reason is required")
	}
	now := s.now().UTC()
	run, event, transitioned, err := s.repository.CancelRun(ctx, runID, now, Event{
		Type:      EventRunCancelled,
		Timestamp: now,
		Payload:   map[string]any{"reason": reason},
	})
	if err != nil || !transitioned {
		return run, err
	}
	s.broker.Publish(event.RunID)
	return run, nil
}

func (s *Service) RecordEvent(ctx context.Context, run AgentRun, event Event) (Event, error) {
	return s.appendEvent(ctx, run, event)
}

func (s *Service) RecordModelTelemetry(
	ctx context.Context,
	run AgentRun,
	record agent.TelemetryRecord,
) error {
	for index, attempt := range record.Telemetry.Attempts {
		usage := agent.ModelUsage{Status: agent.UsageUnavailable}
		finishReason := ""
		resolvedModel := ""
		toolCallIDs := []string(nil)
		toolCallStatus := ToolCallUnavailable
		unavailableReason := ToolCallUnavailableAttemptFailedNoResponse
		if attempt.Status == "succeeded" {
			usage = record.Telemetry.Usage
			finishReason = record.Telemetry.FinishReason
			resolvedModel = record.Telemetry.ResolvedModel
			toolCallIDs = append(toolCallIDs, record.ToolCallIDs...)
			if len(toolCallIDs) > 0 {
				toolCallStatus = ToolCallAvailable
				unavailableReason = ""
			} else {
				unavailableReason = ToolCallUnavailableModelReturnedFinalText
			}
		}
		var safeError *agent.ModelError
		if attempt.Error != nil {
			safeError = &agent.ModelError{
				Category:  limitString(attempt.Error.Category, 64),
				Code:      limitString(attempt.Error.Code, 64),
				Retryable: attempt.Error.Retryable,
			}
		}
		payload := ResearchLLMCallPayload{
			SchemaVersion:  ResearchLLMCallSchemaV1,
			LogicalCallID:  limitString(record.LogicalCallID, 128),
			Provider:       limitString(record.Telemetry.Provider, 128),
			RequestedModel: limitString(record.Telemetry.RequestedModel, 128),
			ResolvedModel:  limitString(resolvedModel, 128),
			Prompt: agent.PromptSpec{
				Version:       limitString(record.Telemetry.Prompt.Version, 64),
				RequestSHA256: limitString(record.Telemetry.Prompt.RequestSHA256, 64),
				PromptSHA256:  limitString(record.Telemetry.Prompt.PromptSHA256, 64),
				ToolsetSHA256: limitString(record.Telemetry.Prompt.ToolsetSHA256, 64),
				RequestBudget: record.Telemetry.Prompt.RequestBudget,
			},
			Usage:                     usage,
			FinishReason:              limitString(finishReason, 64),
			Attempt:                   attempt.Attempt,
			AttemptStatus:             attempt.Status,
			AttemptStartedAt:          attempt.StartedAt.UTC(),
			AttemptLatencyMS:          attempt.LatencyMS,
			TotalLatencyMS:            record.Telemetry.TotalLatencyMS,
			HTTPStatus:                attempt.HTTPStatus,
			ProviderRequestID:         limitString(attempt.ProviderRequestID, 128),
			RetryCount:                index,
			ToolCallStatus:            toolCallStatus,
			ToolCallUnavailableReason: unavailableReason,
			ToolCallIDs:               toolCallIDs,
			Error:                     safeError,
		}
		encoded, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("encode LLM telemetry: %w", err)
		}
		var safePayload map[string]any
		if err := json.Unmarshal(encoded, &safePayload); err != nil {
			return fmt.Errorf("normalize LLM telemetry: %w", err)
		}
		toolCallID := ""
		if len(toolCallIDs) == 1 {
			toolCallID = toolCallIDs[0]
		}
		if _, err := s.appendEvent(ctx, run, Event{
			Type: EventResearchLLMCall, StepID: record.StepID,
			ToolCallID: toolCallID, Payload: safePayload,
		}); err != nil {
			return err
		}
	}
	return nil
}

func limitString(value string, limit int) string {
	value = strings.TrimSpace(value)
	if len(value) <= limit {
		return value
	}
	return value[:limit]
}

func (s *Service) CompleteRun(ctx context.Context, run AgentRun) (AgentRun, error) {
	run.Status = RunStatusCompleted
	run.PendingToolCallID = nil
	run.PendingStepID = nil
	run.UpdatedAt = s.now().UTC()
	if err := s.repository.SaveRun(ctx, run); err != nil {
		if errors.Is(err, ErrRunCancelled) {
			return s.repository.GetRun(context.WithoutCancel(ctx), run.ID)
		}
		return AgentRun{}, err
	}
	if _, err := s.appendEvent(ctx, run, Event{Type: EventRunFinished}); err != nil {
		return AgentRun{}, err
	}
	return run, nil
}

func (s *Service) FailRun(ctx context.Context, run AgentRun, cause error) (AgentRun, error) {
	run.Status = RunStatusFailed
	run.PendingToolCallID = nil
	run.PendingStepID = nil
	run.UpdatedAt = s.now().UTC()
	if err := s.repository.SaveRun(ctx, run); err != nil {
		if errors.Is(err, ErrRunCancelled) {
			return s.repository.GetRun(context.WithoutCancel(ctx), run.ID)
		}
		return AgentRun{}, err
	}
	payload := map[string]any{}
	if cause != nil {
		payload["message"] = cause.Error()
	}
	if _, err := s.appendEvent(ctx, run, Event{Type: EventRunFailed, Payload: payload}); err != nil {
		return AgentRun{}, err
	}
	return run, nil
}

func (s *Service) ResumeToolCall(
	ctx context.Context,
	runID string,
	toolCallID string,
	request ResumeToolCallRequest,
) (AgentRun, error) {
	run, err := s.repository.GetRun(ctx, runID)
	if err != nil {
		return AgentRun{}, err
	}
	if run.Status != RunStatusWaitingUser || run.PendingToolCallID == nil {
		return AgentRun{}, ErrRunNotWaitingForUser
	}
	if *run.PendingToolCallID != toolCallID {
		return AgentRun{}, ErrToolCallMismatch
	}

	stepID := ""
	if run.PendingStepID != nil {
		stepID = *run.PendingStepID
	}
	if _, err := s.appendEvent(ctx, run, Event{
		Type:       EventToolResult,
		StepID:     stepID,
		ToolCallID: toolCallID,
		Payload: map[string]any{
			"tool":      "ask_user_question",
			"answers":   request.Answers,
			"next_step": request.NextStep,
		},
	}); err != nil {
		return AgentRun{}, err
	}
	if _, err := s.appendEvent(ctx, run, Event{
		Type:       EventToolFinished,
		StepID:     stepID,
		ToolCallID: toolCallID,
		Payload:    map[string]any{"tool": "ask_user_question"},
	}); err != nil {
		return AgentRun{}, err
	}

	run.Status = RunStatusRunning
	run.PendingToolCallID = nil
	run.PendingStepID = nil
	run.UpdatedAt = s.now().UTC()
	if err := s.repository.SaveRun(ctx, run); err != nil {
		return AgentRun{}, fmt.Errorf("save resumed agent run: %w", err)
	}
	return run, nil
}

func (s *Service) appendEvent(ctx context.Context, run AgentRun, event Event) (Event, error) {
	event.ConversationID = run.ConversationID
	event.RunID = run.ID
	event.Timestamp = s.now().UTC()
	if event.Payload == nil {
		event.Payload = map[string]any{}
	}
	persisted, err := s.repository.AppendEvent(ctx, event)
	if err != nil {
		return Event{}, fmt.Errorf("append agent event: %w", err)
	}
	s.broker.Publish(persisted.RunID)
	return persisted, nil
}

func validateQuestions(questions []Question) error {
	if len(questions) == 0 || len(questions) > 3 {
		return errors.New("questions must contain between 1 and 3 items")
	}
	seen := make(map[string]struct{}, len(questions))
	for _, question := range questions {
		if strings.TrimSpace(question.ID) == "" || strings.TrimSpace(question.Prompt) == "" {
			return errors.New("question id and question are required")
		}
		if _, exists := seen[question.ID]; exists {
			return fmt.Errorf("duplicate question id %q", question.ID)
		}
		seen[question.ID] = struct{}{}
		if question.Type == QuestionSingleSelect && len(question.Options) < 2 {
			return fmt.Errorf("single_select question %q requires at least two options", question.ID)
		}
	}
	return nil
}

func randomID(prefix string) string {
	var value [12]byte
	if _, err := rand.Read(value[:]); err != nil {
		panic(fmt.Sprintf("generate id: %v", err))
	}
	return prefix + "_" + hex.EncodeToString(value[:])
}
