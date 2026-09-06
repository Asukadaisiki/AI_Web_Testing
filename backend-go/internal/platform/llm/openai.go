package llm

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"syscall"
	"time"

	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/agent"
)

const (
	defaultMaxAttempts = 3
	maxResponseBytes   = 4 << 20
)

type OpenAIClient struct {
	provider    string
	baseURL     string
	apiKey      string
	model       string
	httpClient  *http.Client
	maxAttempts int
	retryDelay  time.Duration
}

func NewOpenAIClient(
	provider string,
	baseURL string,
	apiKey string,
	model string,
	timeout time.Duration,
) (*OpenAIClient, error) {
	provider = strings.TrimSpace(provider)
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if provider == "" {
		return nil, errors.New("LLM provider is required")
	}
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
		provider: provider, baseURL: baseURL, apiKey: apiKey, model: model,
		httpClient:  &http.Client{Timeout: timeout},
		maxAttempts: defaultMaxAttempts,
		retryDelay:  100 * time.Millisecond,
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
	ID      string `json:"id"`
	Model   string `json:"model"`
	Choices []struct {
		Message      chatMessage `json:"message"`
		FinishReason string      `json:"finish_reason"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens     *int64 `json:"prompt_tokens"`
		CompletionTokens *int64 `json:"completion_tokens"`
		TotalTokens      *int64 `json:"total_tokens"`
	} `json:"usage,omitempty"`
}

func (c *OpenAIClient) Complete(
	ctx context.Context,
	messages []agent.Message,
	definitions []agent.ToolDefinition,
) (agent.ModelResponse, error) {
	payload := buildRequest(c.model, messages, definitions)
	body, err := json.Marshal(payload)
	if err != nil {
		return agent.ModelResponse{}, fmt.Errorf("encode LLM request: %w", err)
	}
	telemetry := agent.ModelTelemetry{
		Provider:       c.provider,
		RequestedModel: c.model,
		Prompt: agent.PromptSpec{
			Version:       agent.SystemPromptVersion,
			RequestSHA256: sha256Hex(body),
			PromptSHA256:  promptSHA(messages),
			ToolsetSHA256: toolsetSHA(definitions),
		},
		Usage: agent.ModelUsage{Status: agent.UsageUnavailable},
	}
	totalStarted := time.Now()
	var lastErr *agent.ModelError
	for attempt := 1; attempt <= c.maxAttempts; attempt++ {
		if attempt > 1 {
			timer := time.NewTimer(c.retryDelay * time.Duration(attempt-1))
			select {
			case <-ctx.Done():
				timer.Stop()
				telemetry.TotalLatencyMS = elapsedMillis(totalStarted)
				if emitErr := agent.EmitTelemetry(ctx, telemetry, nil); emitErr != nil {
					return agent.ModelResponse{}, emitErr
				}
				return agent.ModelResponse{}, ctx.Err()
			case <-timer.C:
			}
		}
		decoded, status, requestID, started, callErr := c.doRequest(ctx, body)
		if callErr != nil {
			lastErr = classifyCallError(callErr, status)
			telemetry.Attempts = append(
				telemetry.Attempts,
				failedAttempt(attempt, started, status, requestID, lastErr),
			)
			if !lastErr.Retryable || attempt == c.maxAttempts {
				telemetry.TotalLatencyMS = elapsedMillis(totalStarted)
				if emitErr := agent.EmitTelemetry(ctx, telemetry, nil); emitErr != nil {
					return agent.ModelResponse{}, emitErr
				}
				if errors.Is(callErr, context.Canceled) {
					return agent.ModelResponse{}, context.Canceled
				}
				return agent.ModelResponse{}, lastErr
			}
			continue
		}
		result, parseErr := parseResponse(decoded)
		if parseErr != nil {
			lastErr = &agent.ModelError{Category: "response", Code: "invalid_response", Message: parseErr.Error()}
			telemetry.Attempts = append(
				telemetry.Attempts,
				failedAttempt(attempt, started, status, requestID, lastErr),
			)
			telemetry.TotalLatencyMS = elapsedMillis(totalStarted)
			if emitErr := agent.EmitTelemetry(ctx, telemetry, nil); emitErr != nil {
				return agent.ModelResponse{}, emitErr
			}
			return agent.ModelResponse{}, lastErr
		}
		telemetry.ResolvedModel = decoded.Model
		telemetry.FinishReason = decoded.Choices[0].FinishReason
		telemetry.Usage = usageFromResponse(decoded.Usage)
		telemetry.Attempts = append(telemetry.Attempts, agent.ModelAttempt{
			Attempt: attempt, Status: "succeeded", StartedAt: started.UTC(),
			LatencyMS: elapsedMillis(started), HTTPStatus: status,
			ProviderRequestID: truncate(requestID, 128),
		})
		telemetry.TotalLatencyMS = elapsedMillis(totalStarted)
		result.Telemetry = telemetry
		toolCallIDs := make([]string, 0, len(result.ToolCalls))
		for _, call := range result.ToolCalls {
			toolCallIDs = append(toolCallIDs, call.ID)
		}
		if err := agent.EmitTelemetry(ctx, telemetry, toolCallIDs); err != nil {
			return agent.ModelResponse{}, err
		}
		return result, nil
	}
	return agent.ModelResponse{}, lastErr
}

func buildRequest(model string, messages []agent.Message, definitions []agent.ToolDefinition) chatRequest {
	request := chatRequest{Model: model, ToolChoice: "auto"}
	for _, message := range messages {
		converted := chatMessage{Role: message.Role, Content: message.Content, ToolCallID: message.ToolCallID}
		for _, call := range message.ToolCalls {
			converted.ToolCalls = append(converted.ToolCalls, toolCall{
				ID: call.ID, Type: "function",
				Function: toolFunction{Name: call.Name, Arguments: call.Arguments},
			})
		}
		request.Messages = append(request.Messages, converted)
	}
	for _, definition := range definitions {
		request.Tools = append(request.Tools, chatTool{
			Type: "function",
			Function: toolFunction{
				Name: definition.Name, Description: definition.Description,
				Parameters: definition.InputSchema,
			},
		})
	}
	return request
}

func (c *OpenAIClient) doRequest(
	ctx context.Context,
	body []byte,
) (chatResponse, *int, string, time.Time, error) {
	started := time.Now()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return chatResponse{}, nil, "", started, fmt.Errorf("create request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+c.apiKey)
	request.Header.Set("Content-Type", "application/json")
	response, err := c.httpClient.Do(request)
	if err != nil {
		return chatResponse{}, nil, "", started, err
	}
	defer response.Body.Close()
	status := response.StatusCode
	requestID := firstNonEmpty(response.Header.Get("x-request-id"), response.Header.Get("request-id"))
	responseBody, readErr := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes))
	if readErr != nil {
		return chatResponse{}, &status, requestID, started, readErr
	}
	if status < http.StatusOK || status >= http.StatusMultipleChoices {
		return chatResponse{}, &status, requestID, started, fmt.Errorf("provider returned HTTP %d", status)
	}
	var decoded chatResponse
	if err := json.Unmarshal(responseBody, &decoded); err != nil {
		return chatResponse{}, &status, requestID, started, err
	}
	if decoded.ID != "" {
		requestID = decoded.ID
	}
	return decoded, &status, requestID, started, nil
}

func parseResponse(decoded chatResponse) (agent.ModelResponse, error) {
	if len(decoded.Choices) == 0 {
		return agent.ModelResponse{}, errors.New("LLM response has no choices")
	}
	message := decoded.Choices[0].Message
	result := agent.ModelResponse{Content: message.Content}
	for _, call := range message.ToolCalls {
		if call.ID == "" || call.Function.Name == "" {
			return agent.ModelResponse{}, errors.New("LLM returned an invalid tool call")
		}
		result.ToolCalls = append(result.ToolCalls, agent.ModelTool{
			ID: call.ID, Name: call.Function.Name, Arguments: call.Function.Arguments,
		})
	}
	if strings.TrimSpace(result.Content) == "" && len(result.ToolCalls) == 0 {
		return agent.ModelResponse{}, errors.New("LLM response has no content or tool calls")
	}
	return result, nil
}

func usageFromResponse(usage *struct {
	PromptTokens     *int64 `json:"prompt_tokens"`
	CompletionTokens *int64 `json:"completion_tokens"`
	TotalTokens      *int64 `json:"total_tokens"`
}) agent.ModelUsage {
	result := agent.ModelUsage{Status: agent.UsageUnavailable}
	if usage == nil {
		return result
	}
	result.InputTokens = usage.PromptTokens
	result.OutputTokens = usage.CompletionTokens
	result.TotalTokens = usage.TotalTokens
	switch {
	case result.InputTokens != nil && result.OutputTokens != nil && result.TotalTokens != nil:
		result.Status = agent.UsageAvailable
	case result.InputTokens != nil || result.OutputTokens != nil || result.TotalTokens != nil:
		result.Status = agent.UsagePartial
	}
	return result
}

func classifyCallError(err error, status *int) *agent.ModelError {
	if status != nil {
		retryable := *status == 408 || *status == 429 || *status == 500 ||
			*status == 502 || *status == 503 || *status == 504
		return &agent.ModelError{
			Category: "http", Code: fmt.Sprintf("http_%d", *status),
			Message: fmt.Sprintf("LLM provider returned HTTP %d", *status), Retryable: retryable,
		}
	}
	return classifyTransportError(err)
}

func classifyTransportError(err error) *agent.ModelError {
	if errors.Is(err, context.Canceled) {
		return &agent.ModelError{Category: "cancelled", Code: "context_cancelled", Message: "LLM request cancelled"}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return &agent.ModelError{Category: "timeout", Code: "deadline_exceeded", Message: "LLM request timed out", Retryable: true}
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		var dnsErr *net.DNSError
		retryable := netErr.Timeout() ||
			(errors.As(err, &dnsErr) && dnsErr.IsTemporary) ||
			errors.Is(err, syscall.ECONNRESET) ||
			errors.Is(err, syscall.ECONNREFUSED) ||
			errors.Is(err, syscall.EPIPE)
		return &agent.ModelError{
			Category: "transport", Code: "network_error",
			Message: "LLM network request failed", Retryable: retryable,
		}
	}
	retryable := errors.Is(err, io.ErrUnexpectedEOF)
	return &agent.ModelError{
		Category: "transport", Code: "request_failed",
		Message: "LLM transport request failed", Retryable: retryable,
	}
}

func failedAttempt(
	attempt int,
	started time.Time,
	status *int,
	requestID string,
	modelErr *agent.ModelError,
) agent.ModelAttempt {
	return agent.ModelAttempt{
		Attempt: attempt, Status: "failed", StartedAt: started.UTC(),
		LatencyMS: elapsedMillis(started), HTTPStatus: status,
		ProviderRequestID: truncate(requestID, 128), Error: modelErr,
	}
}

func promptSHA(messages []agent.Message) string {
	if len(messages) == 0 || messages[0].Role != "system" {
		return sha256Hex(nil)
	}
	return sha256Hex([]byte(messages[0].Content))
}

func toolsetSHA(definitions []agent.ToolDefinition) string {
	body, _ := json.Marshal(definitions)
	return sha256Hex(body)
}

func sha256Hex(value []byte) string {
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}

func elapsedMillis(started time.Time) int64 {
	value := time.Since(started).Milliseconds()
	if value < 0 {
		return 0
	}
	return value
}

func truncate(value string, limit int) string {
	value = strings.TrimSpace(value)
	if len(value) <= limit {
		return value
	}
	return value[:limit]
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
