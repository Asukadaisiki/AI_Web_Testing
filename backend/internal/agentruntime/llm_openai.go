package agentruntime

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Asukadaisiki/AI_Web_Testing/v2/backend/internal/planner"
	"github.com/Asukadaisiki/AI_Web_Testing/v2/backend/internal/usage"
)

// OpenAIConfig 是 OpenAI 兼容端点（DeepSeek 等）的配置。
type OpenAIConfig struct {
	BaseURL     string
	APIKey      string
	Model       string
	Temperature float64
	MaxTokens   int
	Timeout     time.Duration
}

// OpenAILLM 是 OpenAI 兼容的 chat completions 客户端，带 function calling。
type OpenAILLM struct {
	config OpenAIConfig
	tools  []planner.Tool
	http   *http.Client
}

// NewOpenAILLM 创建真实模型客户端。
func NewOpenAILLM(config OpenAIConfig, tools []planner.Tool) *OpenAILLM {
	if config.BaseURL == "" {
		config.BaseURL = "https://api.deepseek.com"
	}
	if config.Timeout <= 0 {
		config.Timeout = 5 * time.Minute
	}
	return &OpenAILLM{
		config: config,
		tools:  tools,
		http:   &http.Client{Timeout: config.Timeout},
	}
}

// Label 实现 LLM。
func (c *OpenAILLM) Label() string {
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
	Model       string        `json:"model"`
	Messages    []wireMessage `json:"messages"`
	Tools       []wireTool    `json:"tools,omitempty"`
	ToolChoice  string        `json:"tool_choice,omitempty"`
	Temperature float64       `json:"temperature"`
	MaxTokens   int           `json:"max_tokens,omitempty"`
}

type wireResponse struct {
	Choices []struct {
		Message      wireMessage `json:"message"`
		FinishReason string      `json:"finish_reason"`
	} `json:"choices"`
	Usage *wireUsage `json:"usage"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
}

// wireUsage 是 OpenAI 兼容端点返回的 usage。
//
// 方舟（Ark）会把思考 token 放在 completion_tokens_details.reasoning_tokens、
// 缓存命中放在 prompt_tokens_details.cached_tokens；两者都只是"看得见的成本构成"，
// 已经包含在 prompt/completion 里，不能再加一遍。
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

// Next 实现 LLM。
func (c *OpenAILLM) Next(ctx context.Context, messages []Message) (Message, usage.Usage, error) {
	request := wireRequest{
		Model:       c.config.Model,
		Messages:    toWireMessages(messages),
		Tools:       c.wireTools(),
		ToolChoice:  "auto",
		Temperature: c.config.Temperature,
		MaxTokens:   c.config.MaxTokens,
	}
	raw, err := json.Marshal(request)
	if err != nil {
		return Message{}, usage.Usage{}, fmt.Errorf("marshal model request: %w", err)
	}

	var lastErr error
	for attempt := 1; attempt <= 2; attempt++ {
		message, spent, err := c.call(ctx, raw)
		if err != nil {
			lastErr = err
			// 网络抖动与限流值得重试一次；其余错误直接返回。
			if !retryable(err) {
				return Message{}, usage.Usage{}, err
			}
			select {
			case <-ctx.Done():
				return Message{}, usage.Usage{}, ctx.Err()
			case <-time.After(time.Duration(attempt) * time.Second):
			}
			continue
		}
		return message, spent, nil
	}
	return Message{}, usage.Usage{}, lastErr
}

func (c *OpenAILLM) call(ctx context.Context, raw []byte) (Message, usage.Usage, error) {
	url := strings.TrimRight(c.config.BaseURL, "/") + "/chat/completions"
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return Message{}, usage.Usage{}, fmt.Errorf("build model request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	if c.config.APIKey != "" {
		request.Header.Set("Authorization", "Bearer "+c.config.APIKey)
	}
	response, err := c.http.Do(request)
	if err != nil {
		return Message{}, usage.Usage{}, fmt.Errorf("call model: %w", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return Message{}, usage.Usage{}, fmt.Errorf("read model response: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return Message{}, usage.Usage{}, &httpError{status: response.StatusCode, body: truncate(string(body), 500)}
	}
	var parsed wireResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return Message{}, usage.Usage{}, fmt.Errorf("decode model response: %w (body: %s)", err, truncate(string(body), 300))
	}
	if parsed.Error != nil {
		return Message{}, usage.Usage{}, fmt.Errorf("model error: %s", parsed.Error.Message)
	}
	if len(parsed.Choices) == 0 {
		return Message{}, usage.Usage{}, fmt.Errorf("model returned no choices")
	}
	return fromWireMessage(parsed.Choices[0].Message), parsed.Usage.toUsage(), nil
}

type httpError struct {
	status int
	body   string
}

func (e *httpError) Error() string {
	return fmt.Sprintf("model http %d: %s", e.status, e.body)
}

func retryable(err error) bool {
	var httpErr *httpError
	if ok := asHTTPError(err, &httpErr); ok {
		return httpErr.status == 429 || httpErr.status >= 500
	}
	// 传输层错误（超时、连接重置）值得重试一次。
	return true
}

func asHTTPError(err error, target **httpError) bool {
	for err != nil {
		if typed, ok := err.(*httpError); ok {
			*target = typed
			return true
		}
		unwrapper, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = unwrapper.Unwrap()
	}
	return false
}

func (c *OpenAILLM) wireTools() []wireTool {
	out := make([]wireTool, 0, len(c.tools))
	for _, tool := range c.tools {
		out = append(out, wireTool{
			Type: "function",
			Function: wireToolDefinition{
				Name:        tool.Name,
				Description: tool.Description,
				Parameters:  tool.Parameters,
			},
		})
	}
	return out
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
				ID:   call.ID,
				Type: "function",
				Function: wireFunction{
					Name:      call.Name,
					Arguments: string(call.Arguments),
				},
			})
		}
		out = append(out, wire)
	}
	return out
}

func fromWireMessage(wire wireMessage) Message {
	message := Message{
		Role:             Role(wire.Role),
		Content:          wire.Content,
		ReasoningContent: wire.ReasoningContent,
		ToolCallID:       wire.ToolCallID,
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
			ID:        call.ID,
			Name:      call.Function.Name,
			Arguments: arguments,
		})
	}
	return message
}

func truncate(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit] + "…"
}
