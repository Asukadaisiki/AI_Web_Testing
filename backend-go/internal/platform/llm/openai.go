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

type callStage string

const (
	stageRequest         callStage = "request"
	stageRead            callStage = "read"
	stageDecode          callStage = "decode"
	stageInvalidResponse callStage = "invalid_response"
)

var (
	errResponseTooLarge = errors.New("LLM response exceeds size limit")
	errNoChoices        = errors.New("LLM response has no choices")
	errInvalidToolCall  = errors.New("LLM returned an invalid tool call")
	errNoResponseOutput = errors.New("LLM response has no content or tool calls")
)

type callStageError struct {
	stage callStage
	cause error
}

func (e *callStageError) Error() string {
	return fmt.Sprintf("LLM %s stage failed", e.stage)
}

func (e *callStageError) Unwrap() error {
	return e.cause
}

type providerHTTPError struct {
	status int
	cause  error
}

func (e *providerHTTPError) Error() string {
	return fmt.Sprintf("LLM provider returned HTTP %d", e.status)
}

func (e *providerHTTPError) Unwrap() error {
	return e.cause
}

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
			RequestBudget: requestSerializationBudget(payload, body),
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
			lastErr = classifyCallError(callErr)
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
		telemetry.ResolvedModel = decoded.Model
		telemetry.Usage = usageFromResponse(decoded.Usage)
		result, parseErr := parseResponse(decoded)
		if parseErr != nil {
			lastErr = classifyCallError(&callStageError{
				stage: stageInvalidResponse,
				cause: parseErr,
			})
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
		telemetry.FinishReason = decoded.Choices[0].FinishReason
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

func requestSerializationBudget(
	request chatRequest,
	body []byte,
) agent.RequestSerializationBudget {
	messageBytes, _ := json.Marshal(request.Messages)
	toolBytes, _ := json.Marshal(request.Tools)
	budget := agent.RequestSerializationBudget{
		RequestBytes:        len(body),
		MessageBytes:        len(messageBytes),
		ToolDefinitionBytes: len(toolBytes),
	}
	for _, message := range request.Messages {
		if message.Role != "tool" {
			continue
		}
		var envelope struct {
			SchemaVersion string `json:"schema_version"`
		}
		if json.Unmarshal([]byte(message.Content), &envelope) == nil &&
			envelope.SchemaVersion == agent.ModelToolSummarySchemaV1 {
			budget.ExplorationSummaryBytes += len(message.Content)
			budget.ExplorationSummaryCount++
		}
	}
	return budget
}

func (c *OpenAIClient) doRequest(
	ctx context.Context,
	body []byte,
) (chatResponse, *int, string, time.Time, error) {
	started := time.Now()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return chatResponse{}, nil, "", started, &callStageError{stage: stageRequest, cause: err}
	}
	request.Header.Set("Authorization", "Bearer "+c.apiKey)
	request.Header.Set("Content-Type", "application/json")
	response, err := c.httpClient.Do(request)
	if err != nil {
		return chatResponse{}, nil, "", started, &callStageError{stage: stageRequest, cause: err}
	}
	status := response.StatusCode
	requestID := firstNonEmpty(response.Header.Get("x-request-id"), response.Header.Get("request-id"))
	responseBody, readErr := readAndCloseResponse(response.Body)
	if status < http.StatusOK || status >= http.StatusMultipleChoices {
		return chatResponse{}, &status, requestID, started, &providerHTTPError{
			status: status,
			cause:  readErr,
		}
	}
	if readErr != nil {
		stage := stageRead
		if errors.Is(readErr, errResponseTooLarge) {
			stage = stageInvalidResponse
		}
		return chatResponse{}, &status, requestID, started, &callStageError{
			stage: stage,
			cause: readErr,
		}
	}
	var decoded chatResponse
	if err := json.Unmarshal(responseBody, &decoded); err != nil {
		return chatResponse{}, &status, requestID, started, &callStageError{
			stage: stageDecode,
			cause: err,
		}
	}
	if decoded.ID != "" {
		requestID = decoded.ID
	}
	return decoded, &status, requestID, started, nil
}

func readAndCloseResponse(body io.ReadCloser) ([]byte, error) {
	responseBody, readErr := io.ReadAll(io.LimitReader(body, maxResponseBytes+1))
	closeErr := body.Close()
	if len(responseBody) > maxResponseBytes {
		responseBody = nil
		readErr = errors.Join(errResponseTooLarge, readErr)
	}
	return responseBody, errors.Join(readErr, closeErr)
}

func parseResponse(decoded chatResponse) (agent.ModelResponse, error) {
	if len(decoded.Choices) == 0 {
		return agent.ModelResponse{}, errNoChoices
	}
	message := decoded.Choices[0].Message
	result := agent.ModelResponse{Content: message.Content}
	for _, call := range message.ToolCalls {
		if call.ID == "" || call.Function.Name == "" {
			return agent.ModelResponse{}, errInvalidToolCall
		}
		result.ToolCalls = append(result.ToolCalls, agent.ModelTool{
			ID: call.ID, Name: call.Function.Name, Arguments: call.Function.Arguments,
		})
	}
	if strings.TrimSpace(result.Content) == "" && len(result.ToolCalls) == 0 {
		return agent.ModelResponse{}, errNoResponseOutput
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

func classifyCallError(err error) *agent.ModelError {
	var httpErr *providerHTTPError
	if errors.As(err, &httpErr) {
		retryable := httpErr.status == 408 || httpErr.status == 429 || httpErr.status == 500 ||
			httpErr.status == 502 || httpErr.status == 503 || httpErr.status == 504
		return agent.NewModelError(
			"http",
			fmt.Sprintf("http_%d", httpErr.status),
			httpErr.Error(),
			retryable,
			err,
		)
	}
	var stageErr *callStageError
	if !errors.As(err, &stageErr) {
		return classifyTransportError(err)
	}
	switch stageErr.stage {
	case stageRequest:
		return classifyTransportError(err)
	case stageRead:
		return classifyResponseReadError(err)
	case stageDecode:
		return agent.NewModelError(
			"response", "response_decode_failed",
			"LLM response JSON is invalid", false, err,
		)
	case stageInvalidResponse:
		code := "invalid_response"
		message := "LLM response is invalid"
		if errors.Is(err, errResponseTooLarge) {
			code = "response_too_large"
			message = "LLM response exceeds size limit"
		}
		return agent.NewModelError("response", code, message, false, err)
	default:
		return classifyTransportError(err)
	}
}

func classifyResponseReadError(err error) *agent.ModelError {
	if errors.Is(err, context.Canceled) {
		return agent.NewModelError(
			"cancelled", "context_cancelled", "LLM request cancelled", false, err,
		)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return agent.NewModelError(
			"timeout", "response_read_timeout", "LLM response read timed out", true, err,
		)
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return agent.NewModelError(
			"timeout", "response_read_timeout", "LLM response read timed out", true, err,
		)
	}
	retryable := errors.Is(err, io.ErrUnexpectedEOF) ||
		errors.Is(err, syscall.ECONNRESET) ||
		errors.Is(err, syscall.EPIPE)
	return agent.NewModelError(
		"transport", "response_read_failed",
		"LLM response read failed", retryable, err,
	)
}

func classifyTransportError(err error) *agent.ModelError {
	if errors.Is(err, context.Canceled) {
		return agent.NewModelError(
			"cancelled", "context_cancelled", "LLM request cancelled", false, err,
		)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return agent.NewModelError(
			"timeout", "deadline_exceeded", "LLM request timed out", true, err,
		)
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		var dnsErr *net.DNSError
		retryable := netErr.Timeout() ||
			(errors.As(err, &dnsErr) && dnsErr.IsTemporary) ||
			errors.Is(err, syscall.ECONNRESET) ||
			errors.Is(err, syscall.ECONNREFUSED) ||
			errors.Is(err, syscall.EPIPE)
		return agent.NewModelError(
			"transport", "network_error", "LLM network request failed", retryable, err,
		)
	}
	retryable := errors.Is(err, io.ErrUnexpectedEOF)
	return agent.NewModelError(
		"transport", "request_failed", "LLM transport request failed", retryable, err,
	)
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
