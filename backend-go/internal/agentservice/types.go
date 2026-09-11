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
	EventTaskPlanUpdated EventType = "task_plan.updated"
	EventPipelineTrace   EventType = "agent.pipeline.trace"
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
const PipelineTraceSchemaV1 = "agent.pipeline.trace.v1"

type PipelineTraceKind string

const (
	PipelineTraceModelRequest PipelineTraceKind = "model_request"
	PipelineTraceToolCall     PipelineTraceKind = "tool_call"
)

type PipelinePlanRef struct {
	PlanID  string `json:"plan_id"`
	Version int    `json:"version"`
	SHA256  string `json:"sha256"`
	Status  string `json:"status"`
}

type PipelineCumulativeUsage struct {
	LogicalCalls             int   `json:"logical_calls"`
	PhysicalAttempts         int   `json:"physical_attempts"`
	InputTokens              int64 `json:"input_tokens"`
	OutputTokens             int64 `json:"output_tokens"`
	TotalTokens              int64 `json:"total_tokens"`
	UsageUnavailableAttempts int   `json:"usage_unavailable_attempts"`
}

type PipelineModelRequestTrace struct {
	LogicalCallID string                           `json:"logical_call_id"`
	Attempt       int                              `json:"attempt"`
	AttemptStatus string                           `json:"attempt_status"`
	RequestBudget agent.RequestSerializationBudget `json:"request_budget"`
	Cumulative    PipelineCumulativeUsage          `json:"cumulative"`
	ToolCallIDs   []string                         `json:"tool_call_ids"`
}

type PipelineToolCallTrace struct {
	Name              string               `json:"name"`
	Signature         string               `json:"signature"`
	Status            string               `json:"status"`
	Attempt           int                  `json:"attempt"`
	RetryOfToolCallID string               `json:"retry_of_tool_call_id,omitempty"`
	ReasonCode        string               `json:"reason_code,omitempty"`
	PlanStepIDs       []string             `json:"plan_step_ids"`
	Lineage           []PipelineLineageRef `json:"lineage,omitempty"`
}

type PipelineLineageRef struct {
	Stage               string   `json:"stage"`
	PlanID              string   `json:"plan_id"`
	PlanVersion         int      `json:"plan_version"`
	PlanStepID          string   `json:"plan_step_id"`
	ProbeID             string   `json:"probe_id,omitempty"`
	ObservationID       string   `json:"observation_id,omitempty"`
	ObservationSHA256   string   `json:"observation_sha256,omitempty"`
	PageStateID         string   `json:"page_state_id,omitempty"`
	ElementRefs         []string `json:"element_refs,omitempty"`
	TargetBindingID     string   `json:"target_binding_id,omitempty"`
	PlannedCandidateID  string   `json:"planned_candidate_id,omitempty"`
	ResolvedCandidateID string   `json:"resolved_candidate_id,omitempty"`
	ResolvedElementRef  string   `json:"resolved_element_ref,omitempty"`
	GenerationID        int64    `json:"generation_id,omitempty"`
	BatchID             int64    `json:"batch_id,omitempty"`
	CaseID              int64    `json:"case_id,omitempty"`
	ExecutionID         int64    `json:"execution_id,omitempty"`
	ReportStatus        string   `json:"report_status,omitempty"`
}

type PipelineTracePayload struct {
	SchemaVersion string                     `json:"schema_version"`
	Kind          PipelineTraceKind          `json:"kind"`
	StateEpoch    string                     `json:"state_epoch"`
	Plan          *PipelinePlanRef           `json:"plan,omitempty"`
	ModelRequest  *PipelineModelRequestTrace `json:"model_request,omitempty"`
	ToolCall      *PipelineToolCallTrace     `json:"tool_call,omitempty"`
}

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
	SchemaVersion                 string                    `json:"schema_version"`
	LogicalCallID                 string                    `json:"logical_call_id"`
	Provider                      string                    `json:"provider"`
	RequestedModel                string                    `json:"requested_model"`
	ResolvedModel                 string                    `json:"resolved_model,omitempty"`
	ClientRequestID               string                    `json:"client_request_id,omitempty"`
	EndpointScheme                string                    `json:"endpoint_scheme,omitempty"`
	EndpointHost                  string                    `json:"endpoint_host,omitempty"`
	CredentialFingerprint         string                    `json:"credential_fingerprint,omitempty"`
	ProviderResponseID            string                    `json:"provider_response_id,omitempty"`
	ProviderHeaderRequestID       string                    `json:"provider_header_request_id,omitempty"`
	ProviderHeaderRequestIDHeader string                    `json:"provider_header_request_id_header,omitempty"`
	LocalResponseCache            string                    `json:"local_response_cache,omitempty"`
	Prompt                        agent.PromptSpec          `json:"prompt_spec"`
	Usage                         agent.ModelUsage          `json:"usage"`
	Reasoning                     *agent.ReasoningAudit     `json:"reasoning,omitempty"`
	FinishReason                  string                    `json:"finish_reason,omitempty"`
	Attempt                       int                       `json:"attempt"`
	AttemptStatus                 string                    `json:"attempt_status"`
	AttemptStartedAt              time.Time                 `json:"attempt_started_at"`
	AttemptLatencyMS              int64                     `json:"attempt_latency_ms"`
	TotalLatencyMS                int64                     `json:"total_latency_ms"`
	HTTPStatus                    *int                      `json:"http_status,omitempty"`
	ProviderRequestID             string                    `json:"provider_request_id,omitempty"`
	RetryCount                    int                       `json:"retry_count"`
	ToolCallStatus                ToolCallStatus            `json:"tool_call_status"`
	ToolCallUnavailableReason     ToolCallUnavailableReason `json:"tool_call_unavailable_reason,omitempty"`
	ToolCallIDs                   []string                  `json:"tool_call_ids,omitempty"`
	Error                         *agent.ModelError         `json:"error,omitempty"`
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
