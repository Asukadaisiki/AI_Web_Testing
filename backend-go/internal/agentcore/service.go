package agentcore

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
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
		ConversationID: conversationID,
		Status:         RunStatusRunning,
		Input:          input,
		Transcript:     []Message{{Role: "user", Content: input}},
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

func (s *Service) ListEvents(ctx context.Context, runID string, afterSeq int64) ([]Event, error) {
	if afterSeq < 0 {
		return nil, errors.New("after_seq must not be negative")
	}
	return s.repository.ListEvents(ctx, runID, afterSeq)
}

func (s *Service) Subscribe(runID string) Subscription {
	return s.broker.Subscribe(runID)
}

func (s *Service) RequestUserInput(
	ctx context.Context,
	runID string,
	request AskUserRequest,
) (AgentRun, Event, error) {
	return s.RequestUserInputForCall(
		ctx,
		runID,
		s.newID("tool"),
		s.newID("step"),
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

	if _, eventErr := s.appendEvent(ctx, run, Event{
		Type:       EventToolStarted,
		StepID:     stepID,
		ToolCallID: toolCallID,
		Payload:    map[string]any{"tool": "ask_user_question"},
	}); eventErr != nil {
		return AgentRun{}, Event{}, eventErr
	}
	if _, eventErr := s.appendEvent(ctx, run, Event{
		Type:       EventToolArgsDelta,
		StepID:     stepID,
		ToolCallID: toolCallID,
		Payload:    map[string]any{"arguments": request},
	}); eventErr != nil {
		return AgentRun{}, Event{}, eventErr
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

func (s *Service) RecordEvent(ctx context.Context, run AgentRun, event Event) (Event, error) {
	return s.appendEvent(ctx, run, event)
}

func (s *Service) CompleteRun(ctx context.Context, run AgentRun) (AgentRun, error) {
	run.Status = RunStatusCompleted
	run.PendingToolCallID = nil
	run.PendingStepID = nil
	run.UpdatedAt = s.now().UTC()
	if err := s.repository.SaveRun(ctx, run); err != nil {
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
	s.broker.Publish(persisted)
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
