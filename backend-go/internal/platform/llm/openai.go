package llm

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/agent"
)

const (
	defaultMaxAttempts = 3
	maxResponseBytes   = 4 << 20
	localCacheStatus   = "not_configured"

	// BUG-167 resilience defaults.
	defaultStreamWatchdog     = 60 * time.Second
	defaultMaxRetryAfter      = 10 * time.Second
	defaultBreakerMaxFailures = 3
	defaultBreakerCooldown    = 30 * time.Second
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
	errStreamStalled    = errors.New("LLM response stream stalled")
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
	provider              string
	chatCompletionsURL    string
	apiKey                string
	model                 string
	thinkingMode          string
	reasoningEffort       string
	endpointScheme        string
	endpointHost          string
	credentialFingerprint string
	httpClient            *http.Client
	maxAttempts           int
	retryDelay            time.Duration

	// BUG-167 resilience knobs (override in tests).
	attemptTimeout time.Duration // per-attempt deadline; 0 = inherit request ctx
	streamWatchdog time.Duration // no-progress stall watchdog for response read
	maxRetryAfter  time.Duration // cap for Retry-After honored on 429
	breaker        *callBreaker  // call-level circuit breaker
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
	apiKey = strings.TrimSpace(apiKey)
	if provider == "" {
		return nil, errors.New("LLM provider is required")
	}
	if baseURL == "" {
		return nil, errors.New("LLM base URL is required")
	}
	endpoint, err := url.Parse(baseURL)
	if err != nil || endpoint == nil {
		return nil, errors.New("LLM base URL must be an absolute HTTP(S) URL without user info")
	}
	endpointScheme := strings.ToLower(endpoint.Scheme)
	if endpoint.Hostname() == "" ||
		(endpointScheme != "http" && endpointScheme != "https") ||
		endpoint.User != nil {
		return nil, errors.New("LLM base URL must be an absolute HTTP(S) URL without user info")
	}
	if apiKey == "" {
		return nil, errors.New("LLM API key is required")
	}
	if strings.TrimSpace(model) == "" {
		return nil, errors.New("LLM model is required")
	}
	endpointHost := strings.ToLower(endpoint.Host)
	endpoint.Path = strings.TrimRight(endpoint.Path, "/") + "/chat/completions"
	endpoint.RawPath = ""
	return &OpenAIClient{
		provider: provider, chatCompletionsURL: endpoint.String(),
		apiKey: apiKey, model: model,
		endpointScheme: endpointScheme,
		endpointHost:   endpointHost,
		credentialFingerprint: fingerprintCredential(
			provider,
			endpointHost,
			apiKey,
		),
		httpClient:     &http.Client{Timeout: timeout},
		maxAttempts:    defaultMaxAttempts,
		retryDelay:     100 * time.Millisecond,
		attemptTimeout: timeout,
		streamWatchdog: defaultStreamWatchdog,
		maxRetryAfter:  defaultMaxRetryAfter,
		breaker:        newCallBreaker(defaultBreakerMaxFailures, defaultBreakerCooldown),
	}, nil
}

func (c *OpenAIClient) EnableThinking(reasoningEffort string) {
	c.thinkingMode = "enabled"
	c.reasoningEffort = normalizeReasoningEffort(reasoningEffort)
}

type chatRequest struct {
	Model           string           `json:"model"`
	Messages        []chatMessage    `json:"messages"`
	Tools           []chatTool       `json:"tools,omitempty"`
	ToolChoice      string           `json:"tool_choice,omitempty"`
	UserID          string           `json:"user_id"`
	Thinking        *thinkingControl `json:"thinking,omitempty"`
	ReasoningEffort string           `json:"reasoning_effort,omitempty"`
}

type thinkingControl struct {
	Type string `json:"type"`
}

type chatMessage struct {
	Role             string     `json:"role"`
	Content          string     `json:"content,omitempty"`
	ReasoningContent string     `json:"reasoning_content,omitempty"`
	ToolCallID       string     `json:"tool_call_id,omitempty"`
	ToolCalls        []toolCall `json:"tool_calls,omitempty"`
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
		PromptTokens          *int64 `json:"prompt_tokens"`
		CompletionTokens      *int64 `json:"completion_tokens"`
		TotalTokens           *int64 `json:"total_tokens"`
		PromptCacheHitTokens  *int64 `json:"prompt_cache_hit_tokens"`
		PromptCacheMissTokens *int64 `json:"prompt_cache_miss_tokens"`
	} `json:"usage,omitempty"`
}

type providerResponseEvidence struct {
	responseID      string
	headerRequestID string
	headerName      string
	retryAfter      time.Duration
}

func (e providerResponseEvidence) legacyRequestID() string {
	return firstNonEmpty(e.responseID, e.headerRequestID)
}

func (c *OpenAIClient) Complete(
	ctx context.Context,
	messages []agent.Message,
	definitions []agent.ToolDefinition,
) (agent.ModelResponse, error) {
	clientRequestID, err := newClientRequestID()
	if err != nil {
		return agent.ModelResponse{}, fmt.Errorf("generate LLM client request ID: %w", err)
	}
	payload := c.buildRequest(clientRequestID, cacheUserID(ctx, clientRequestID), messages, definitions)
	body, err := json.Marshal(payload)
	if err != nil {
		return agent.ModelResponse{}, fmt.Errorf("encode LLM request: %w", err)
	}
	telemetry := agent.ModelTelemetry{
		Provider:              c.provider,
		RequestedModel:        c.model,
		ClientRequestID:       clientRequestID,
		EndpointScheme:        c.endpointScheme,
		EndpointHost:          c.endpointHost,
		CredentialFingerprint: c.credentialFingerprint,
		LocalResponseCache:    localCacheStatus,
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
	if !c.breaker.allow() {
		lastErr = agent.NewModelError(
			"circuit", "circuit_open",
			"LLM calls suspended by circuit breaker after repeated failures",
			false, nil,
		)
		telemetry.TotalLatencyMS = elapsedMillis(totalStarted)
		if emitErr := agent.EmitTelemetry(ctx, telemetry, nil); emitErr != nil {
			return agent.ModelResponse{}, emitErr
		}
		return agent.ModelResponse{}, lastErr
	}
	var pendingRetryAfter time.Duration
	for attempt := 1; attempt <= c.maxAttempts; attempt++ {
		if attempt > 1 {
			delay := c.retryDelay * time.Duration(attempt-1)
			if pendingRetryAfter > delay {
				delay = pendingRetryAfter
			}
			if c.maxRetryAfter > 0 && delay > c.maxRetryAfter {
				delay = c.maxRetryAfter
			}
			timer := time.NewTimer(delay)
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
		attemptCtx := ctx
		attemptCancel := func() {}
		if c.attemptTimeout > 0 {
			attemptCtx, attemptCancel = context.WithTimeout(ctx, c.attemptTimeout)
		}
		decoded, status, evidence, started, callErr := c.doRequest(
			attemptCtx,
			body,
			clientRequestID,
		)
		attemptCancel()
		if callErr != nil {
			lastErr = classifyCallError(callErr)
			pendingRetryAfter = evidence.retryAfter
			telemetry.Attempts = append(
				telemetry.Attempts,
				failedAttempt(attempt, started, status, evidence, lastErr),
			)
			if !lastErr.Retryable || attempt == c.maxAttempts {
				telemetry.TotalLatencyMS = elapsedMillis(totalStarted)
				if emitErr := agent.EmitTelemetry(ctx, telemetry, nil); emitErr != nil {
					return agent.ModelResponse{}, emitErr
				}
				if errors.Is(callErr, context.Canceled) {
					return agent.ModelResponse{}, context.Canceled
				}
				if !errors.Is(callErr, context.Canceled) {
					c.breaker.recordFailure()
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
				failedAttempt(attempt, started, status, evidence, lastErr),
			)
			telemetry.TotalLatencyMS = elapsedMillis(totalStarted)
			if emitErr := agent.EmitTelemetry(ctx, telemetry, nil); emitErr != nil {
				return agent.ModelResponse{}, emitErr
			}
			c.breaker.recordFailure()
			return agent.ModelResponse{}, lastErr
		}
		telemetry.FinishReason = decoded.Choices[0].FinishReason
		telemetry.Reasoning = reasoningAudit(
			c.thinkingMode,
			c.reasoningEffort,
			result.ReasoningContent,
		)
		telemetry.Attempts = append(telemetry.Attempts, agent.ModelAttempt{
			Attempt: attempt, Status: "succeeded", StartedAt: started.UTC(),
			LatencyMS: elapsedMillis(started), HTTPStatus: status,
			ProviderResponseID:            truncate(evidence.responseID, 128),
			ProviderHeaderRequestID:       truncate(evidence.headerRequestID, 128),
			ProviderHeaderRequestIDHeader: truncate(evidence.headerName, 64),
			ProviderRequestID:             truncate(evidence.legacyRequestID(), 128),
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
		c.breaker.recordSuccess()
		return result, nil
	}
	c.breaker.recordFailure()
	return agent.ModelResponse{}, lastErr
}

// cacheUserID selects the identity the provider partitions its prompt cache by.
// It must stay stable across the calls of one run: a per-call identity makes
// every request a cache miss, which was measured against the DeepSeek API
// (identical body, new identity: 0 of 369 prompt tokens hit). The per-call
// request ID remains the fallback for callers that pin no identity.
func cacheUserID(ctx context.Context, clientRequestID string) string {
	if identity := agent.CacheIdentity(ctx); identity != "" {
		return identity
	}
	return clientRequestID
}

func (c *OpenAIClient) buildRequest(
	clientRequestID string,
	userID string,
	messages []agent.Message,
	definitions []agent.ToolDefinition,
) chatRequest {
	request := chatRequest{
		Model: c.model, ToolChoice: "auto", UserID: userID,
	}
	if c.thinkingMode != "" {
		request.Thinking = &thinkingControl{Type: c.thinkingMode}
		request.ReasoningEffort = c.reasoningEffort
	}
	for _, message := range messages {
		converted := chatMessage{
			Role:       message.Role,
			Content:    message.Content,
			ToolCallID: message.ToolCallID,
		}
		if c.thinkingMode != "" {
			converted.ReasoningContent = message.ReasoningContent
		}
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
		MessageCount:        len(request.Messages),
	}
	for _, message := range request.Messages {
		switch message.Role {
		case "system":
			budget.SystemContentBytes += len(message.Content)
		case "user":
			budget.UserContentBytes += len(message.Content)
		case "assistant":
			budget.AssistantContentBytes += len(message.Content)
			budget.AssistantReasoningBytes += len(message.ReasoningContent)
			for _, call := range message.ToolCalls {
				budget.AssistantToolArgumentBytes += len(call.Function.Arguments)
			}
		case "tool":
			budget.ToolContentBytes += len(message.Content)
		}
		if message.Role != "tool" {
			continue
		}
		if summary, ok := agent.DecodeModelToolSummary(message.Content); ok {
			if agent.IsExplorationTool(summary.Tool) {
				budget.ExplorationSummaryBytes += len(message.Content)
				budget.ExplorationSummaryCount++
			} else {
				budget.NonExplorationSummaryBytes += len(message.Content)
			}
			continue
		}
		var envelope struct {
			Status string `json:"status"`
		}
		if json.Unmarshal([]byte(message.Content), &envelope) == nil &&
			envelope.Status == "error" {
			budget.RecoverableToolErrorBytes += len(message.Content)
		}
	}
	return budget
}

func (c *OpenAIClient) doRequest(
	ctx context.Context,
	body []byte,
	clientRequestID string,
) (chatResponse, *int, providerResponseEvidence, time.Time, error) {
	started := time.Now()
	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		c.chatCompletionsURL,
		bytes.NewReader(body),
	)
	if err != nil {
		return chatResponse{}, nil, providerResponseEvidence{}, started, &callStageError{stage: stageRequest, cause: err}
	}
	request.Header.Set("Authorization", "Bearer "+c.apiKey)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Client-Request-ID", clientRequestID)
	response, err := c.httpClient.Do(request)
	if err != nil {
		return chatResponse{}, nil, providerResponseEvidence{}, started, &callStageError{stage: stageRequest, cause: err}
	}
	status := response.StatusCode
	headerRequestID, headerName := providerRequestIDFromHeader(response.Header)
	evidence := providerResponseEvidence{
		headerRequestID: headerRequestID,
		headerName:      headerName,
		retryAfter:      retryAfterFromHeader(response.Header),
	}
	if c.maxRetryAfter > 0 && evidence.retryAfter > c.maxRetryAfter {
		evidence.retryAfter = c.maxRetryAfter
	}
	responseBody, readErr := c.readAndCloseResponse(response.Body)
	if status < http.StatusOK || status >= http.StatusMultipleChoices {
		return chatResponse{}, &status, evidence, started, &providerHTTPError{
			status: status,
			cause:  readErr,
		}
	}
	if readErr != nil {
		stage := stageRead
		if errors.Is(readErr, errResponseTooLarge) {
			stage = stageInvalidResponse
		}
		return chatResponse{}, &status, evidence, started, &callStageError{
			stage: stage,
			cause: readErr,
		}
	}
	var decoded chatResponse
	if err := json.Unmarshal(responseBody, &decoded); err != nil {
		return chatResponse{}, &status, evidence, started, &callStageError{
			stage: stageDecode,
			cause: err,
		}
	}
	evidence.responseID = decoded.ID
	return decoded, &status, evidence, started, nil
}

func (c *OpenAIClient) readAndCloseResponse(body io.ReadCloser) ([]byte, error) {
	if c.streamWatchdog <= 0 {
		return readAndCloseResponse(body)
	}
	responseBody, readErr := readWithWatchdog(body, maxResponseBytes+1, c.streamWatchdog)
	closeErr := body.Close()
	if len(responseBody) > maxResponseBytes {
		responseBody = nil
		readErr = errors.Join(errResponseTooLarge, readErr)
	}
	return responseBody, errors.Join(readErr, closeErr)
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
	result := agent.ModelResponse{
		Content:          message.Content,
		ReasoningContent: message.ReasoningContent,
	}
	for _, call := range message.ToolCalls {
		if call.ID == "" || call.Function.Name == "" {
			return agent.ModelResponse{}, errInvalidToolCall
		}
		result.ToolCalls = append(result.ToolCalls, agent.ModelTool{
			ID: call.ID, Name: call.Function.Name, Arguments: call.Function.Arguments,
		})
	}
	if strings.TrimSpace(result.Content) == "" &&
		strings.TrimSpace(result.ReasoningContent) == "" &&
		len(result.ToolCalls) == 0 {
		return agent.ModelResponse{}, errNoResponseOutput
	}
	return result, nil
}

func reasoningAudit(
	thinkingMode string,
	reasoningEffort string,
	reasoningContent string,
) *agent.ReasoningAudit {
	if thinkingMode == "" && strings.TrimSpace(reasoningContent) == "" {
		return nil
	}
	audit := &agent.ReasoningAudit{
		ThinkingMode:    thinkingMode,
		ReasoningEffort: reasoningEffort,
	}
	if reasoningContent == "" {
		return audit
	}
	sum := sha256.Sum256([]byte(reasoningContent))
	audit.ContentAvailable = true
	audit.ContentBytes = len([]byte(reasoningContent))
	audit.ContentSHA256 = hex.EncodeToString(sum[:])
	return audit
}

func normalizeReasoningEffort(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "low", "high", "max":
		return strings.ToLower(strings.TrimSpace(value))
	case "":
		return "max"
	default:
		return "max"
	}
}

func usageFromResponse(usage *struct {
	PromptTokens          *int64 `json:"prompt_tokens"`
	CompletionTokens      *int64 `json:"completion_tokens"`
	TotalTokens           *int64 `json:"total_tokens"`
	PromptCacheHitTokens  *int64 `json:"prompt_cache_hit_tokens"`
	PromptCacheMissTokens *int64 `json:"prompt_cache_miss_tokens"`
}) agent.ModelUsage {
	result := agent.ModelUsage{Status: agent.UsageUnavailable}
	if usage == nil {
		return result
	}
	result.InputTokens = usage.PromptTokens
	result.OutputTokens = usage.CompletionTokens
	result.TotalTokens = usage.TotalTokens
	result.PromptCacheHitTokens = usage.PromptCacheHitTokens
	result.PromptCacheMissTokens = usage.PromptCacheMissTokens
	switch {
	case result.InputTokens != nil && result.OutputTokens != nil && result.TotalTokens != nil:
		result.Status = agent.UsageAvailable
	case result.InputTokens != nil || result.OutputTokens != nil || result.TotalTokens != nil ||
		result.PromptCacheHitTokens != nil || result.PromptCacheMissTokens != nil:
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
	if errors.Is(err, errStreamStalled) {
		return agent.NewModelError(
			"timeout", "stream_stalled", "LLM response stream stalled", true, err,
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
		errors.Is(err, syscall.ECONNABORTED) ||
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
			errors.Is(err, syscall.ECONNABORTED) ||
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
	evidence providerResponseEvidence,
	modelErr *agent.ModelError,
) agent.ModelAttempt {
	return agent.ModelAttempt{
		Attempt: attempt, Status: "failed", StartedAt: started.UTC(),
		LatencyMS: elapsedMillis(started), HTTPStatus: status,
		ProviderResponseID:            truncate(evidence.responseID, 128),
		ProviderHeaderRequestID:       truncate(evidence.headerRequestID, 128),
		ProviderHeaderRequestIDHeader: truncate(evidence.headerName, 64),
		ProviderRequestID:             truncate(evidence.legacyRequestID(), 128),
		Error:                         modelErr,
	}
}

// callBreaker is a call-level circuit breaker: after maxFailures consecutive
// failed Complete calls it refuses new calls for cooldown, then lets a probe
// through and resets on success.
type callBreaker struct {
	mu          sync.Mutex
	maxFailures int
	cooldown    time.Duration
	failures    int
	openUntil   time.Time
}

func newCallBreaker(maxFailures int, cooldown time.Duration) *callBreaker {
	return &callBreaker{maxFailures: maxFailures, cooldown: cooldown}
}

func (b *callBreaker) allow() bool {
	if b == nil || b.maxFailures <= 0 {
		return true
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.failures >= b.maxFailures {
		if time.Now().Before(b.openUntil) {
			return false
		}
		b.failures = 0
	}
	return true
}

func (b *callBreaker) recordFailure() {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.failures++
	if b.failures >= b.maxFailures {
		b.openUntil = time.Now().Add(b.cooldown)
	}
}

func (b *callBreaker) recordSuccess() {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.failures = 0
	b.openUntil = time.Time{}
}

// readProgress tracks the last moment bytes arrived so a stalled stream can be
// distinguished from a slow-but-alive one.
type readProgress struct {
	mu   sync.Mutex
	last time.Time
}

func (p *readProgress) touch() {
	p.mu.Lock()
	p.last = time.Now()
	p.mu.Unlock()
}

func (p *readProgress) stalled(idle time.Duration) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return time.Since(p.last) >= idle
}

type progressReader struct {
	body     io.Reader
	progress *readProgress
}

func (r *progressReader) Read(p []byte) (int, error) {
	n, err := r.body.Read(p)
	if n > 0 {
		r.progress.touch()
	}
	return n, err
}

// readWithWatchdog reads up to limit bytes, aborting with errStreamStalled when
// no progress is made for idle. The caller must close the underlying body after
// a watchdog abort so the blocked read goroutine can unwind.
func readWithWatchdog(body io.Reader, limit int64, idle time.Duration) ([]byte, error) {
	progress := &readProgress{last: time.Now()}
	type readResult struct {
		data []byte
		err  error
	}
	done := make(chan readResult, 1)
	go func() {
		data, err := io.ReadAll(io.LimitReader(&progressReader{body: body, progress: progress}, limit))
		done <- readResult{data: data, err: err}
	}()
	ticker := time.NewTicker(idle)
	defer ticker.Stop()
	for {
		select {
		case result := <-done:
			return result.data, result.err
		case <-ticker.C:
			if progress.stalled(idle) {
				return nil, errStreamStalled
			}
		}
	}
}

func retryAfterFromHeader(header http.Header) time.Duration {
	raw := strings.TrimSpace(header.Get("Retry-After"))
	if raw == "" {
		return 0
	}
	if seconds, err := strconv.Atoi(raw); err == nil && seconds >= 0 {
		return time.Duration(seconds) * time.Second
	}
	if date, err := http.ParseTime(raw); err == nil {
		if delta := time.Until(date); delta > 0 {
			return delta
		}
	}
	return 0
}

func newClientRequestID() (string, error) {
	random := make([]byte, 16)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	return "e2e_" + hex.EncodeToString(random), nil
}

func fingerprintCredential(provider, host, apiKey string) string {
	sum := sha256.Sum256([]byte(provider + "\x00" + host + "\x00" + apiKey))
	return "sha256:v1:" + hex.EncodeToString(sum[:])
}

func providerRequestIDFromHeader(header http.Header) (string, string) {
	for _, name := range []string{
		"x-request-id",
		"request-id",
		"x-ds-trace-id",
	} {
		if value := strings.TrimSpace(header.Get(name)); value != "" {
			return value, name
		}
	}
	return "", ""
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
