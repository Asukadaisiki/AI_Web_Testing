package agent

import (
	"context"
	"errors"
	"time"
)

const (
	SystemPromptVersion = "agentcore.system.v1"

	UsageAvailable   ModelUsageStatus = "available"
	UsagePartial     ModelUsageStatus = "partial"
	UsageUnavailable ModelUsageStatus = "unavailable"

	ToolResultSchemaV1       = "agent.tool_result.v1"
	ModelToolSummarySchemaV1 = "agent.model_tool_summary.v1"
	ToolSummaryPolicyV1      = "deterministic.exploration.v1"
)

type PromptSpec struct {
	Version       string                     `json:"version"`
	RequestSHA256 string                     `json:"request_sha256"`
	PromptSHA256  string                     `json:"prompt_sha256"`
	ToolsetSHA256 string                     `json:"toolset_sha256"`
	RequestBudget RequestSerializationBudget `json:"request_budget"`
}

type RequestSerializationBudget struct {
	RequestBytes            int `json:"request_bytes"`
	MessageBytes            int `json:"message_bytes"`
	ToolDefinitionBytes     int `json:"tool_definition_bytes"`
	ExplorationSummaryBytes int `json:"exploration_summary_bytes"`
	ExplorationSummaryCount int `json:"exploration_summary_count"`
}

type ModelUsageStatus string

type ModelUsage struct {
	Status       ModelUsageStatus `json:"status"`
	InputTokens  *int64           `json:"input_tokens,omitempty"`
	OutputTokens *int64           `json:"output_tokens,omitempty"`
	TotalTokens  *int64           `json:"total_tokens,omitempty"`
}

type ModelAttempt struct {
	Attempt           int         `json:"attempt"`
	Status            string      `json:"status"`
	StartedAt         time.Time   `json:"started_at"`
	LatencyMS         int64       `json:"latency_ms"`
	HTTPStatus        *int        `json:"http_status,omitempty"`
	ProviderRequestID string      `json:"provider_request_id,omitempty"`
	Error             *ModelError `json:"error,omitempty"`
}

type Telemetry struct {
	Provider       string         `json:"provider"`
	RequestedModel string         `json:"requested_model"`
	ResolvedModel  string         `json:"resolved_model,omitempty"`
	FinishReason   string         `json:"finish_reason,omitempty"`
	Prompt         PromptSpec     `json:"prompt"`
	Usage          ModelUsage     `json:"usage"`
	Attempts       []ModelAttempt `json:"attempts"`
	TotalLatencyMS int64          `json:"total_latency_ms"`
}

type Error struct {
	Category  string `json:"category"`
	Code      string `json:"code,omitempty"`
	Message   string `json:"message,omitempty"`
	Retryable bool   `json:"retryable"`
	cause     error
}

type ModelTelemetry = Telemetry
type ModelError = Error

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if e.Message != "" {
		return e.Message
	}
	return e.Category
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

func NewModelError(category, code, message string, retryable bool, cause error) *ModelError {
	return &Error{
		Category: category, Code: code, Message: message, Retryable: retryable,
		cause: cause,
	}
}

type TelemetryRecord struct {
	LogicalCallID string
	StepID        string
	ToolCallIDs   []string
	Telemetry     ModelTelemetry
}

type TelemetrySink func(context.Context, TelemetryRecord) error

type telemetryContextKey struct{}

func EmitTelemetry(ctx context.Context, telemetry ModelTelemetry, toolCallIDs []string) error {
	record, ok := ctx.Value(telemetryContextKey{}).(TelemetryRecord)
	if !ok {
		return nil
	}
	sink, ok := ctx.Value(telemetrySinkContextKey{}).(TelemetrySink)
	if !ok || sink == nil {
		return nil
	}
	record.Telemetry = telemetry
	record.ToolCallIDs = append([]string(nil), toolCallIDs...)
	return sink(context.WithoutCancel(ctx), record)
}

type telemetrySinkContextKey struct{}

func WithTelemetryRecorder(ctx context.Context, sink TelemetrySink, logicalCallID, stepID string) context.Context {
	if sink == nil {
		return ctx
	}
	ctx = context.WithValue(ctx, telemetrySinkContextKey{}, sink)
	return context.WithValue(ctx, telemetryContextKey{}, TelemetryRecord{
		LogicalCallID: logicalCallID,
		StepID:        stepID,
	})
}

func AsModelError(err error) *ModelError {
	var modelErr *ModelError
	if errors.As(err, &modelErr) {
		return modelErr
	}
	return nil
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
	Telemetry ModelTelemetry
}

type Model interface {
	Complete(ctx context.Context, messages []Message, tools []ToolDefinition) (ModelResponse, error)
}

type ToolDefinition struct {
	Name        string
	Description string
	InputSchema []byte
}
