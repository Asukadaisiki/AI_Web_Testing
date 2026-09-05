package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/agent"
)

type OpenAIClient struct {
	baseURL    string
	apiKey     string
	model      string
	httpClient *http.Client
}

func NewOpenAIClient(baseURL string, apiKey string, model string, timeout time.Duration) (*OpenAIClient, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return nil, errors.New("LLM base URL is required")
	}
	if strings.TrimSpace(apiKey) == "" {
		return nil, errors.New("LLM API key is required")
	}
	if strings.TrimSpace(model) == "" {
		return nil, errors.New("LLM model is required")
	}
	return &OpenAIClient{
		baseURL: baseURL,
		apiKey:  apiKey,
		model:   model,
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}, nil
}

type chatRequest struct {
	Model      string        `json:"model"`
	Messages   []chatMessage `json:"messages"`
	Tools      []chatTool    `json:"tools,omitempty"`
	ToolChoice string        `json:"tool_choice,omitempty"`
}

type chatMessage struct {
	Role       string     `json:"role"`
	Content    string     `json:"content,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	ToolCalls  []toolCall `json:"tool_calls,omitempty"`
}

type chatTool struct {
	Type     string       `json:"type"`
	Function toolFunction `json:"function"`
}

type toolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function toolFunction `json:"function"`
}

type toolFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
	Arguments   string          `json:"arguments,omitempty"`
}

type chatResponse struct {
	Choices []struct {
		Message chatMessage `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func (c *OpenAIClient) Complete(
	ctx context.Context,
	messages []agent.Message,
	definitions []agent.ToolDefinition,
) (agent.ModelResponse, error) {
	requestPayload := chatRequest{
		Model:      c.model,
		Messages:   make([]chatMessage, 0, len(messages)),
		Tools:      make([]chatTool, 0, len(definitions)),
		ToolChoice: "auto",
	}
	for _, message := range messages {
		converted := chatMessage{
			Role:       message.Role,
			Content:    message.Content,
			ToolCallID: message.ToolCallID,
			ToolCalls:  make([]toolCall, 0, len(message.ToolCalls)),
		}
		for _, call := range message.ToolCalls {
			converted.ToolCalls = append(converted.ToolCalls, toolCall{
				ID:   call.ID,
				Type: "function",
				Function: toolFunction{
					Name:      call.Name,
					Arguments: call.Arguments,
				},
			})
		}
		requestPayload.Messages = append(requestPayload.Messages, converted)
	}
	for _, definition := range definitions {
		requestPayload.Tools = append(requestPayload.Tools, chatTool{
			Type: "function",
			Function: toolFunction{
				Name:        definition.Name,
				Description: definition.Description,
				Parameters:  definition.InputSchema,
			},
		})
	}

	body, err := json.Marshal(requestPayload)
	if err != nil {
		return agent.ModelResponse{}, fmt.Errorf("encode LLM request: %w", err)
	}
	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		c.baseURL+"/chat/completions",
		bytes.NewReader(body),
	)
	if err != nil {
		return agent.ModelResponse{}, fmt.Errorf("create LLM request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+c.apiKey)
	request.Header.Set("Content-Type", "application/json")

	response, err := c.httpClient.Do(request)
	if err != nil {
		return agent.ModelResponse{}, fmt.Errorf("call LLM: %w", err)
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, 4<<20))
	if err != nil {
		return agent.ModelResponse{}, fmt.Errorf("read LLM response: %w", err)
	}

	var decoded chatResponse
	if err := json.Unmarshal(responseBody, &decoded); err != nil {
		return agent.ModelResponse{}, fmt.Errorf("decode LLM response: %w", err)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		message := http.StatusText(response.StatusCode)
		if decoded.Error != nil && decoded.Error.Message != "" {
			message = decoded.Error.Message
		}
		return agent.ModelResponse{}, fmt.Errorf("LLM returned HTTP %d: %s", response.StatusCode, message)
	}
	if len(decoded.Choices) == 0 {
		return agent.ModelResponse{}, errors.New("LLM response has no choices")
	}

	message := decoded.Choices[0].Message
	result := agent.ModelResponse{
		Content:   message.Content,
		ToolCalls: make([]agent.ModelTool, 0, len(message.ToolCalls)),
	}
	for _, call := range message.ToolCalls {
		if call.ID == "" || call.Function.Name == "" {
			return agent.ModelResponse{}, errors.New("LLM returned an invalid tool call")
		}
		result.ToolCalls = append(result.ToolCalls, agent.ModelTool{
			ID:        call.ID,
			Name:      call.Function.Name,
			Arguments: call.Function.Arguments,
		})
	}
	if strings.TrimSpace(result.Content) == "" && len(result.ToolCalls) == 0 {
		return agent.ModelResponse{}, errors.New("LLM response has no content or tool calls")
	}
	return result, nil
}
