package model

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/usage"
)

// OpenAIConfig configures an OpenAI-compatible chat-completions endpoint.
type OpenAIConfig struct {
	BaseURL         string
	APIKey          string
	Model           string
	Temperature     float64
	MaxTokens       int
	ReasoningEffort string
	Timeout         time.Duration
}

// OpenAI is an OpenAI-compatible Model implementation.
type OpenAI struct {
	config OpenAIConfig
	http   *http.Client
}

// NewOpenAI creates an OpenAI-compatible model client.
func NewOpenAI(config OpenAIConfig) *OpenAI {
	if config.BaseURL == "" {
		config.BaseURL = "https://api.deepseek.com"
	}
	if config.Timeout <= 0 {
		config.Timeout = 5 * time.Minute
	}
	return &OpenAI{config: config, http: &http.Client{Timeout: config.Timeout}}
}

// Label identifies the selected model endpoint.
func (c *OpenAI) Label() string {
	return fmt.Sprintf("%s@%s", c.config.Model, c.config.BaseURL)
}

type wireMessage struct {
	Role             string         `json:"role"`
	Content          string         `json:"content"`
	ReasoningContent string         `json:"reasoning_content,omitempty"`
	ToolCalls        []wireToolCall `json:"tool_calls,omitempty"`
	ToolCallID       string         `json:"tool_call_id,omitempty"`
}

type wireToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function wireFunction `json:"function"`
}

type wireFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type wireTool struct {
	Type     string             `json:"type"`
	Function wireToolDefinition `json:"function"`
}

type wireToolDefinition struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

type wireRequest struct {
	Model           string        `json:"model"`
	Messages        []wireMessage `json:"messages"`
	Tools           []wireTool    `json:"tools,omitempty"`
	ToolChoice      string        `json:"tool_choice,omitempty"`
	Temperature     float64       `json:"temperature"`
	MaxTokens       int           `json:"max_tokens,omitempty"`
	ReasoningEffort string        `json:"reasoning_effort,omitempty"`
}

type wireResponse struct {
	Choices []struct {
		Message wireMessage `json:"message"`
	} `json:"choices"`
	Usage *wireUsage `json:"usage"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

type wireUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
	PromptDetails    *struct {
		CachedTokens int `json:"cached_tokens"`
	} `json:"prompt_tokens_details"`
	CompletionDetails *struct {
		ReasoningTokens int `json:"reasoning_tokens"`
	} `json:"completion_tokens_details"`
}

func (w *wireUsage) toUsage() usage.Usage {
	if w == nil {
		return usage.Usage{}
	}
	var reasoning, cached int
	if w.CompletionDetails != nil {
		reasoning = w.CompletionDetails.ReasoningTokens
	}
	if w.PromptDetails != nil {
		cached = w.PromptDetails.CachedTokens
	}
	value := usage.Call(w.PromptTokens, w.CompletionTokens, reasoning, cached)
	if w.TotalTokens > 0 {
		value.TotalTokens = w.TotalTokens
	}
	return value
}

// Complete sends one request, retrying transient provider failures once.
func (c *OpenAI) Complete(ctx context.Context, request Request) (Response, error) {
	raw, err := c.marshalRequest(request)
	if err != nil {
		return Response{}, err
	}
	var lastErr error
	for attempt := 1; attempt <= 2; attempt++ {
		response, err := c.call(ctx, raw)
		if err == nil {
			return response, nil
		}
		lastErr = err
		if !retryable(err) {
			return Response{}, err
		}
		select {
		case <-ctx.Done():
			return Response{}, ctx.Err()
		case <-time.After(time.Duration(attempt) * time.Second):
		}
	}
	return Response{}, lastErr
}

// RequestSize returns the exact number of bytes posted by Complete.
func (c *OpenAI) RequestSize(request Request) (int, error) {
	raw, err := c.marshalRequest(request)
	if err != nil {
		return 0, err
	}
	return len(raw), nil
}

func (c *OpenAI) marshalRequest(request Request) ([]byte, error) {
	modelName := request.Model
	if modelName == "" {
		modelName = c.config.Model
	}
	reasoningEffort := request.ReasoningEffort
	if reasoningEffort == "" {
		reasoningEffort = c.config.ReasoningEffort
	}
	raw, err := json.Marshal(wireRequest{
		Model:           modelName,
		Messages:        toWireMessages(request.Messages),
		Tools:           toWireTools(request.Tools),
		ToolChoice:      "auto",
		Temperature:     c.config.Temperature,
		MaxTokens:       c.config.MaxTokens,
		ReasoningEffort: reasoningEffort,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal model request: %w", err)
	}
	return raw, nil
}

func (c *OpenAI) call(ctx context.Context, raw []byte) (Response, error) {
	url := strings.TrimRight(c.config.BaseURL, "/") + "/chat/completions"
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return Response{}, fmt.Errorf("build model request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	if c.config.APIKey != "" {
		request.Header.Set("Authorization", "Bearer "+c.config.APIKey)
	}
	response, err := c.http.Do(request)
	if err != nil {
		return Response{}, fmt.Errorf("call model: %w", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return Response{}, fmt.Errorf("read model response: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return Response{}, &httpError{status: response.StatusCode, body: truncate(string(body), 500)}
	}
	var parsed wireResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return Response{}, fmt.Errorf("decode model response: %w (body: %s)", err, truncate(string(body), 300))
	}
	if parsed.Error != nil {
		return Response{}, fmt.Errorf("model error: %s", parsed.Error.Message)
	}
	if len(parsed.Choices) == 0 {
		return Response{}, fmt.Errorf("model returned no choices")
	}
	return Response{Message: fromWireMessage(parsed.Choices[0].Message), Usage: parsed.Usage.toUsage()}, nil
}

type httpError struct {
	status int
	body   string
}

func (e *httpError) Error() string { return fmt.Sprintf("model http %d: %s", e.status, e.body) }

func retryable(err error) bool {
	var typed *httpError
	for err != nil {
		if current, ok := err.(*httpError); ok {
			typed = current
			break
		}
		unwrapper, ok := err.(interface{ Unwrap() error })
		if !ok {
			break
		}
		err = unwrapper.Unwrap()
	}
	return typed == nil || typed.status == http.StatusTooManyRequests || typed.status >= http.StatusInternalServerError
}

func toWireMessages(messages []Message) []wireMessage {
	out := make([]wireMessage, 0, len(messages))
	for _, message := range messages {
		wire := wireMessage{
			Role:             string(message.Role),
			Content:          message.Content,
			ReasoningContent: message.ReasoningContent,
			ToolCallID:       message.ToolCallID,
		}
		for _, call := range message.ToolCalls {
			wire.ToolCalls = append(wire.ToolCalls, wireToolCall{
				ID: call.ID, Type: "function",
				Function: wireFunction{Name: call.Name, Arguments: string(call.Arguments)},
			})
		}
		out = append(out, wire)
	}
	return out
}

func toWireTools(tools []Tool) []wireTool {
	out := make([]wireTool, 0, len(tools))
	for _, tool := range tools {
		out = append(out, wireTool{Type: "function", Function: wireToolDefinition{
			Name: tool.Name, Description: tool.Description, Parameters: tool.Parameters,
		}})
	}
	return out
}

func fromWireMessage(wire wireMessage) Message {
	message := Message{
		Role: Role(wire.Role), Content: wire.Content,
		ReasoningContent: wire.ReasoningContent, ToolCallID: wire.ToolCallID,
	}
	if message.Role == "" {
		message.Role = RoleAssistant
	}
	for _, call := range wire.ToolCalls {
		arguments := json.RawMessage(call.Function.Arguments)
		if len(arguments) == 0 {
			arguments = json.RawMessage("{}")
		}
		message.ToolCalls = append(message.ToolCalls, ToolCall{
			ID: call.ID, Name: call.Function.Name, Arguments: arguments,
		})
	}
	return message
}

func truncate(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit] + "..."
}
