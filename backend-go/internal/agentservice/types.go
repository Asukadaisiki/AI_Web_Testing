package agentservice

import (
	"time"

	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/agent"
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
	EventRunCancelled    EventType = "run.cancelled"
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
	EventResearchLLMCall EventType = "research.llm_call"
)

type AgentRun struct {
	ID                   string          `json:"id"`
	ActorUserID          int64           `json:"-"`
	ConversationID       string          `json:"conversation_id"`
	ProjectID            int64           `json:"project_id"`
	Status               RunStatus       `json:"status"`
	Input                string          `json:"input"`
	PendingToolCallID    *string         `json:"pending_tool_call_id,omitempty"`
	PendingStepID        *string         `json:"pending_step_id,omitempty"`
	LatestGenerationID   *int64          `json:"latest_generation_id,omitempty"`
	ApprovedGenerationID *int64          `json:"approved_generation_id,omitempty"`
	Transcript           []agent.Message `json:"-"`
	CreatedAt            time.Time       `json:"created_at"`
	UpdatedAt            time.Time       `json:"updated_at"`
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

const ResearchLLMCallSchemaV1 = "research.llm_call.v1"

type ToolCallStatus string

const (
	ToolCallAvailable   ToolCallStatus = "available"
	ToolCallUnavailable ToolCallStatus = "unavailable"
)

type ToolCallUnavailableReason string

const (
	ToolCallUnavailableModelReturnedFinalText  ToolCallUnavailableReason = "model_returned_final_text"
	ToolCallUnavailableAttemptFailedNoResponse ToolCallUnavailableReason = "model_attempt_failed_without_response"
)

type ResearchLLMCallPayload struct {
	SchemaVersion             string                    `json:"schema_version"`
	LogicalCallID             string                    `json:"logical_call_id"`
	Provider                  string                    `json:"provider"`
	RequestedModel            string                    `json:"requested_model"`
	ResolvedModel             string                    `json:"resolved_model,omitempty"`
	Prompt                    agent.PromptSpec          `json:"prompt_spec"`
	Usage                     agent.ModelUsage          `json:"usage"`
	FinishReason              string                    `json:"finish_reason,omitempty"`
	Attempt                   int                       `json:"attempt"`
	AttemptStatus             string                    `json:"attempt_status"`
	AttemptStartedAt          time.Time                 `json:"attempt_started_at"`
	AttemptLatencyMS          int64                     `json:"attempt_latency_ms"`
	TotalLatencyMS            int64                     `json:"total_latency_ms"`
	HTTPStatus                *int                      `json:"http_status,omitempty"`
	ProviderRequestID         string                    `json:"provider_request_id,omitempty"`
	RetryCount                int                       `json:"retry_count"`
	ToolCallStatus            ToolCallStatus            `json:"tool_call_status"`
	ToolCallUnavailableReason ToolCallUnavailableReason `json:"tool_call_unavailable_reason,omitempty"`
	ToolCallIDs               []string                  `json:"tool_call_ids,omitempty"`
	Error                     *agent.ModelError         `json:"error,omitempty"`
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
