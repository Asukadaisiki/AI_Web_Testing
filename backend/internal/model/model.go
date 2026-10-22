// Package model defines provider-independent model requests and responses.
package model

import (
	"context"
	"encoding/json"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/usage"
)

// Role identifies a message participant.
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

// ToolCall is one model-requested operation.
type ToolCall struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// Message is one entry in the model conversation.
type Message struct {
	Role             Role       `json:"role"`
	Content          string     `json:"content,omitempty"`
	ReasoningContent string     `json:"reasoning_content,omitempty"`
	ToolCalls        []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID       string     `json:"tool_call_id,omitempty"`
}

// Tool defines an operation available to the model.
type Tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

// Request is a provider-neutral completion request.
type Request struct {
	Model           string    `json:"model,omitempty"`
	Messages        []Message `json:"messages"`
	Tools           []Tool    `json:"tools,omitempty"`
	ToolChoice      string    `json:"tool_choice,omitempty"`
	ReasoningEffort string    `json:"reasoning_effort,omitempty"`
}

// Response is one completed model turn and its normalized provider usage.
type Response struct {
	Message Message     `json:"message"`
	Report  string      `json:"report,omitempty"`
	Stage   Stage       `json:"stage,omitempty"`
	Usage   usage.Usage `json:"usage"`
}

// Stage optionally names the semantic role of a model turn in a trace.
type Stage string

const (
	StagePlan  Stage = "plan"
	StageFinal Stage = "final"
)

// Model completes requests through a provider or deterministic fixture.
type Model interface {
	Complete(context.Context, Request) (Response, error)
	Label() string
}

// CheckpointedModel can deterministically replay a response selected by durable
// run state instead of mutable process-local call order.
type CheckpointedModel interface {
	Model
	CompleteForRun(context.Context, string, int, Request) (Response, error)
}

// RequestSizer reports the exact serialized transport request size.
type RequestSizer interface {
	RequestSize(Request) (int, error)
}
