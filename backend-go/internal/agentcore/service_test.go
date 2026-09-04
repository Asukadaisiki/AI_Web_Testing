package agentcore

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestAskUserQuestionPauseAndResume(t *testing.T) {
	repository := NewMemoryRepository()
	now := time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC)
	nextID := 0
	service := NewServiceWithDependencies(
		repository,
		func() time.Time { return now },
		func(prefix string) string {
			nextID++
			return fmt.Sprintf("%s-%d", prefix, nextID)
		},
	)

	run, err := service.StartRun(context.Background(), "conversation-1", "测试登录")
	if err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}

	run, pending, err := service.RequestUserInput(
		context.Background(),
		run.ID,
		AskUserRequest{
			Questions: []Question{{
				ID:       "login_mode",
				Prompt:   "选择登录方式",
				Type:     QuestionSingleSelect,
				Required: true,
				Options: []QuestionOption{
					{Value: "account", Label: "账号密码"},
					{Value: "cookie", Label: "登录态"},
				},
			}},
		},
	)
	if err != nil {
		t.Fatalf("RequestUserInput() error = %v", err)
	}
	if run.Status != RunStatusWaitingUser {
		t.Fatalf("status = %q, want %q", run.Status, RunStatusWaitingUser)
	}
	if run.PendingToolCallID == nil || *run.PendingToolCallID != pending.ToolCallID {
		t.Fatal("pending tool call was not persisted")
	}
	if pending.Type != EventToolPending || pending.CheckpointID == "" {
		t.Fatalf("pending event = %#v", pending)
	}

	run, err = service.ResumeToolCall(
		context.Background(),
		run.ID,
		pending.ToolCallID,
		ResumeToolCallRequest{
			Answers:  map[string]any{"login_mode": "account"},
			NextStep: "continue",
		},
	)
	if err != nil {
		t.Fatalf("ResumeToolCall() error = %v", err)
	}
	if run.Status != RunStatusRunning || run.PendingToolCallID != nil {
		t.Fatalf("resumed run = %#v", run)
	}

	events, err := service.ListEvents(context.Background(), run.ID, 0)
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}
	wantTypes := []EventType{
		EventRunStarted,
		EventToolStarted,
		EventToolPending,
		EventToolResult,
		EventToolFinished,
	}
	if len(events) != len(wantTypes) {
		t.Fatalf("len(events) = %d, want %d", len(events), len(wantTypes))
	}
	for index, event := range events {
		if event.Seq != int64(index+1) {
			t.Fatalf("events[%d].Seq = %d, want %d", index, event.Seq, index+1)
		}
		if event.Type != wantTypes[index] {
			t.Fatalf("events[%d].Type = %q, want %q", index, event.Type, wantTypes[index])
		}
	}
}

func TestResumeToolCallRejectsWrongCall(t *testing.T) {
	repository := NewMemoryRepository()
	service := NewService(repository)
	run, err := service.StartRun(context.Background(), "conversation-1", "测试登录")
	if err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}
	_, _, err = service.RequestUserInput(
		context.Background(),
		run.ID,
		AskUserRequest{Questions: []Question{{
			ID:       "confirm",
			Prompt:   "继续吗？",
			Type:     QuestionConfirm,
			Required: true,
		}}},
	)
	if err != nil {
		t.Fatalf("RequestUserInput() error = %v", err)
	}

	_, err = service.ResumeToolCall(
		context.Background(),
		run.ID,
		"wrong-tool-call",
		ResumeToolCallRequest{Answers: map[string]any{"confirm": true}},
	)
	if !errors.Is(err, ErrToolCallMismatch) {
		t.Fatalf("ResumeToolCall() error = %v, want ErrToolCallMismatch", err)
	}
}

func TestQuestionValidation(t *testing.T) {
	repository := NewMemoryRepository()
	service := NewService(repository)
	run, err := service.StartRun(context.Background(), "conversation-1", "测试登录")
	if err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}

	_, _, err = service.RequestUserInput(context.Background(), run.ID, AskUserRequest{})
	if err == nil {
		t.Fatal("RequestUserInput() error = nil, want validation error")
	}
}
