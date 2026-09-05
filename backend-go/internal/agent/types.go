package agent

import "context"

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
