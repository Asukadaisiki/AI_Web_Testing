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
)

type PromptSpec struct {
	Version       string `json:"version"`
	RequestSHA256 string `json:"request_sha256"`
	PromptSHA256  string `json:"prompt_sha256"`
	ToolsetSHA256 string `json:"toolset_sha256"`
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
