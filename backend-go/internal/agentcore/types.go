package agentcore

import (
	"context"
	"time"
)

type RunStatus string

const (
	RunStatusRunning     RunStatus = "running"
	RunStatusWaitingUser RunStatus = "waiting_user"
	RunStatusCompleted   RunStatus = "completed"
	RunStatusFailed      RunStatus = "failed"
	RunStatusCancelled   RunStatus = "cancelled"
)

type EventType string

const (
	EventRunStarted      EventType = "run.started"
	EventRunFinished     EventType = "run.finished"
	EventRunFailed       EventType = "run.failed"
	EventMessageStarted  EventType = "message.started"
	EventMessageDelta    EventType = "message.delta"
	EventMessageFinished EventType = "message.finished"
	EventToolStarted     EventType = "tool.started"
	EventToolArgsDelta   EventType = "tool.args.delta"
	EventToolPending     EventType = "tool.pending"
	EventToolResult      EventType = "tool.result"
	EventToolFinished    EventType = "tool.finished"
	EventToolFailed      EventType = "tool.failed"
	EventArtifact        EventType = "artifact.published"
)

type AgentRun struct {
	ID                string    `json:"id"`
	ConversationID    string    `json:"conversation_id"`
	ProjectID         int64     `json:"project_id"`
	Status            RunStatus `json:"status"`
	Input             string    `json:"input"`
	PendingToolCallID *string   `json:"pending_tool_call_id,omitempty"`
	PendingStepID     *string   `json:"pending_step_id,omitempty"`
	Transcript        []Message `json:"-"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

type Event struct {
	Seq            int64          `json:"seq"`
	Type           EventType      `json:"type"`
	ConversationID string         `json:"conversation_id"`
	RunID          string         `json:"run_id"`
	StepID         string         `json:"step_id,omitempty"`
	ToolCallID     string         `json:"tool_call_id,omitempty"`
	ParentID       string         `json:"parent_id,omitempty"`
	CheckpointID   string         `json:"checkpoint_id,omitempty"`
	Timestamp      time.Time      `json:"timestamp"`
	Payload        map[string]any `json:"payload"`
}

type QuestionType string

const (
	QuestionSingleSelect QuestionType = "single_select"
	QuestionMultiSelect  QuestionType = "multi_select"
	QuestionText         QuestionType = "text"
	QuestionConfirm      QuestionType = "confirm"
)

type QuestionOption struct {
	Value       string `json:"value"`
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}

type Question struct {
	ID       string           `json:"id"`
	Prompt   string           `json:"question"`
	Type     QuestionType     `json:"type"`
	Required bool             `json:"required"`
	Options  []QuestionOption `json:"options,omitempty"`
}

type AskUserRequest struct {
	Questions []Question `json:"questions"`
}

type ResumeToolCallRequest struct {
	Answers  map[string]any `json:"answers"`
	NextStep string         `json:"next_step,omitempty"`
}

type Message struct {
	Role       string      `json:"role"`
	Content    string      `json:"content,omitempty"`
	ToolCallID string      `json:"tool_call_id,omitempty"`
	ToolCalls  []ModelTool `json:"tool_calls,omitempty"`
}

type ModelTool struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type ModelResponse struct {
	Content   string
	ToolCalls []ModelTool
}

type Model interface {
	Complete(ctx context.Context, messages []Message, tools []ToolDefinition) (ModelResponse, error)
}

type ToolDefinition struct {
	Name        string
	Description string
	InputSchema []byte
}
