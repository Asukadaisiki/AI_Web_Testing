package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/agent"
)

func TestCompleteParsesNativeToolCall(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer secret" {
			t.Fatalf("authorization header was not set")
		}
		var payload chatRequest
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if payload.Model != "test-model" || len(payload.Tools) != 1 {
			t.Fatalf("request payload = %#v", payload)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{
			"choices":[{
				"message":{
					"role":"assistant",
					"tool_calls":[{
						"id":"call-1",
						"type":"function",
						"function":{
							"name":"ask_user_question",
							"arguments":"{\"questions\":[{\"id\":\"url\",\"question\":\"URL?\",\"type\":\"text\",\"required\":true}]}"
						}
					}]
				}
			}]
		}`))
	}))
	defer server.Close()

	client, err := NewOpenAIClient("test-provider", server.URL, "secret", "test-model", time.Second)
	if err != nil {
		t.Fatalf("NewOpenAIClient() error = %v", err)
	}
	response, err := client.Complete(
		context.Background(),
		[]agent.Message{{Role: "user", Content: "test login"}},
		[]agent.ToolDefinition{{
			Name:        "ask_user_question",
			Description: "ask",
			InputSchema: []byte(`{"type":"object"}`),
		}},
	)
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	if len(response.ToolCalls) != 1 {
		t.Fatalf("tool calls = %#v", response.ToolCalls)
	}
	if response.ToolCalls[0].Name != "ask_user_question" {
		t.Fatalf("tool name = %q", response.ToolCalls[0].Name)
	}
	if response.Telemetry.Usage.Status != agent.UsageUnavailable ||
		response.Telemetry.Usage.InputTokens != nil ||
		response.Telemetry.Usage.OutputTokens != nil ||
		response.Telemetry.Usage.TotalTokens != nil {
		t.Fatalf("missing usage was not preserved as unavailable: %#v", response.Telemetry.Usage)
	}
}

func TestCompleteRecordsUsageHashesAndRetryAttempts(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			writer.Header().Set("x-request-id", "failed-request")
			http.Error(writer, "secret provider body", http.StatusTooManyRequests)
			return
		}
		writer.Header().Set("x-request-id", "header-request")
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{
			"id":"provider-request","model":"resolved-model",
			"choices":[{"finish_reason":"stop","message":{"role":"assistant","content":"done"}}],
			"usage":{"prompt_tokens":11,"completion_tokens":3,"total_tokens":14}
		}`))
	}))
	defer server.Close()

	client, err := NewOpenAIClient("gateway", server.URL, "secret", "requested-model", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	client.retryDelay = 0
	var records []agent.TelemetryRecord
	ctx := agent.WithTelemetryRecorder(context.Background(), func(_ context.Context, record agent.TelemetryRecord) error {
		records = append(records, record)
		return nil
	}, "logical-1", "step-1")
	response, err := client.Complete(ctx, []agent.Message{
		{Role: "system", Content: "private prompt"},
		{Role: "user", Content: "private message"},
	}, []agent.ToolDefinition{{Name: "tool", InputSchema: []byte(`{"type":"object"}`)}})
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	if calls.Load() != 2 || len(records) != 1 || len(records[0].Telemetry.Attempts) != 2 {
		t.Fatalf("calls = %d, records = %#v", calls.Load(), records)
	}
	if records[0].Telemetry.Attempts[0].ProviderRequestID != "failed-request" {
		t.Fatalf("failed attempt request id = %#v", records[0].Telemetry.Attempts[0])
	}
	telemetry := response.Telemetry
	if telemetry.Provider != "gateway" || telemetry.RequestedModel != "requested-model" ||
		telemetry.ResolvedModel != "resolved-model" || telemetry.FinishReason != "stop" {
		t.Fatalf("telemetry = %#v", telemetry)
	}
	if telemetry.Usage.Status != agent.UsageAvailable ||
		telemetry.Usage.InputTokens == nil || *telemetry.Usage.InputTokens != 11 {
		t.Fatalf("usage = %#v", telemetry.Usage)
	}
	encoded, _ := json.Marshal(telemetry)
	if strings.Contains(string(encoded), "private prompt") ||
		strings.Contains(string(encoded), "private message") ||
		strings.Contains(string(encoded), "secret provider body") {
		t.Fatalf("telemetry leaked request or provider content: %s", encoded)
	}
	for _, hash := range []string{
		telemetry.Prompt.RequestSHA256,
		telemetry.Prompt.PromptSHA256,
		telemetry.Prompt.ToolsetSHA256,
	} {
		if len(hash) != 64 {
			t.Fatalf("hash = %q", hash)
		}
	}
}

func TestCompleteRecordsRequestSerializationBudgetWithoutRejectingRequiredMessages(t *testing.T) {
	const requiredPayloadBytes = 96 << 10
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var payload chatRequest
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if len(payload.Messages) != 3 ||
			len(payload.Messages[2].Content) != requiredPayloadBytes {
			t.Fatalf("messages = %#v", payload.Messages)
		}
		_, _ = writer.Write([]byte(`{
			"id":"budget-request",
			"model":"resolved",
			"choices":[{"finish_reason":"stop","message":{"content":"done"}}]
		}`))
	}))
	defer server.Close()

	summary, err := agent.BuildModelToolSummary(
		"explore_page",
		json.RawMessage(`{"url":"https://example.com","a11y_nodes":[]}`),
		7,
	)
	if err != nil {
		t.Fatal(err)
	}
	client, _ := NewOpenAIClient("gateway", server.URL, "secret", "model", time.Second)
	response, err := client.Complete(
		context.Background(),
		[]agent.Message{
			{Role: "tool", ToolCallID: "explore", Content: summary},
			{Role: "user", Content: "required"},
			{Role: "tool", ToolCallID: "required-tool", Content: strings.Repeat("x", requiredPayloadBytes)},
		},
		[]agent.ToolDefinition{{
			Name: "required_tool", InputSchema: json.RawMessage(`{"type":"object"}`),
		}},
	)
	if err != nil {
		t.Fatalf("Complete() rejected a required non-exploration message: %v", err)
	}
	budget := response.Telemetry.Prompt.RequestBudget
	if budget.RequestBytes < requiredPayloadBytes ||
		budget.MessageBytes < requiredPayloadBytes ||
		budget.ToolDefinitionBytes == 0 ||
		budget.ExplorationSummaryCount != 1 ||
		budget.ExplorationSummaryBytes != len(summary) {
		t.Fatalf("request budget = %#v", budget)
	}
}

func TestCompleteRetriesTruncatedSuccessResponse(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			connection, _, err := writer.(http.Hijacker).Hijack()
			if err != nil {
				t.Errorf("hijack response: %v", err)
				return
			}
			body := `{"id":"truncated-request","choices":[`
			_, _ = fmt.Fprintf(
				connection,
				"HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nX-Request-Id: truncated-header\r\nContent-Length: %d\r\nConnection: close\r\n\r\n%s",
				len(body)+64,
				body,
			)
			_ = connection.Close()
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{
			"id":"successful-request","model":"resolved-model",
			"choices":[{"finish_reason":"stop","message":{"content":"done"}}],
			"usage":{"prompt_tokens":7,"completion_tokens":2,"total_tokens":9}
		}`))
	}))
	defer server.Close()

	client, _ := NewOpenAIClient("gateway", server.URL, "secret", "model", time.Second)
	client.retryDelay = 0
	var records []agent.TelemetryRecord
	ctx := agent.WithTelemetryRecorder(context.Background(), func(_ context.Context, record agent.TelemetryRecord) error {
		records = append(records, record)
		return nil
	}, "logical", "step")

	response, err := client.Complete(ctx, []agent.Message{{Role: "user", Content: "x"}}, nil)
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	if calls.Load() != 2 || len(records) != 1 || len(response.Telemetry.Attempts) != 2 {
		t.Fatalf("calls = %d, records = %d, telemetry = %#v", calls.Load(), len(records), response.Telemetry)
	}
	failed := response.Telemetry.Attempts[0]
	if failed.HTTPStatus == nil || *failed.HTTPStatus != http.StatusOK ||
		failed.ProviderRequestID != "truncated-header" ||
		failed.Error == nil ||
		failed.Error.Category != "transport" ||
		failed.Error.Code != "response_read_failed" ||
		!failed.Error.Retryable ||
		!errors.Is(failed.Error, io.ErrUnexpectedEOF) {
		t.Fatalf("truncated attempt = %#v", failed)
	}
	succeeded := response.Telemetry.Attempts[1]
	if succeeded.ProviderRequestID != "successful-request" ||
		response.Telemetry.ResolvedModel != "resolved-model" ||
		response.Telemetry.Usage.Status != agent.UsageAvailable ||
		response.Telemetry.Usage.TotalTokens == nil ||
		*response.Telemetry.Usage.TotalTokens != 9 {
		t.Fatalf("successful telemetry = %#v", response.Telemetry)
	}
}

func TestCompleteDoesNotRetryMalformedSuccessJSON(t *testing.T) {
	const rawBody = `{"choices":[{"message":{"content":"private provider body"}}]`
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		writer.Header().Set("x-request-id", "malformed-request")
		_, _ = writer.Write([]byte(rawBody))
	}))
	defer server.Close()

	client, _ := NewOpenAIClient("gateway", server.URL, "secret", "model", time.Second)
	client.retryDelay = 0
	var record agent.TelemetryRecord
	ctx := agent.WithTelemetryRecorder(context.Background(), func(_ context.Context, value agent.TelemetryRecord) error {
		record = value
		return nil
	}, "logical", "step")

	_, err := client.Complete(ctx, []agent.Message{{Role: "user", Content: "x"}}, nil)
	modelErr := agent.AsModelError(err)
	var syntaxErr *json.SyntaxError
	if calls.Load() != 1 ||
		modelErr == nil ||
		modelErr.Category != "response" ||
		modelErr.Code != "response_decode_failed" ||
		modelErr.Retryable ||
		!errors.As(err, &syntaxErr) {
		t.Fatalf("calls = %d, error = %#v", calls.Load(), err)
	}
	if len(record.Telemetry.Attempts) != 1 ||
		record.Telemetry.Attempts[0].HTTPStatus == nil ||
		*record.Telemetry.Attempts[0].HTTPStatus != http.StatusOK ||
		record.Telemetry.Attempts[0].ProviderRequestID != "malformed-request" {
		t.Fatalf("telemetry = %#v", record.Telemetry)
	}
	encoded, _ := json.Marshal(record.Telemetry)
	if strings.Contains(string(encoded), "private provider body") ||
		strings.Contains(string(encoded), "cause") {
		t.Fatalf("telemetry leaked response details: %s", encoded)
	}
}

func TestCompleteDoesNotRetryEmptyChoices(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		_, _ = writer.Write([]byte(`{
			"id":"empty-request","model":"deepseek-compatible","choices":[],
			"usage":{"prompt_tokens":5,"completion_tokens":0,"total_tokens":5}
		}`))
	}))
	defer server.Close()

	client, _ := NewOpenAIClient("gateway", server.URL, "secret", "model", time.Second)
	client.retryDelay = 0
	var record agent.TelemetryRecord
	ctx := agent.WithTelemetryRecorder(context.Background(), func(_ context.Context, value agent.TelemetryRecord) error {
		record = value
		return nil
	}, "logical", "step")

	_, err := client.Complete(ctx, []agent.Message{{Role: "user", Content: "x"}}, nil)
	modelErr := agent.AsModelError(err)
	if calls.Load() != 1 ||
		modelErr == nil ||
		modelErr.Category != "response" ||
		modelErr.Code != "invalid_response" ||
		modelErr.Retryable ||
		!errors.Is(err, errNoChoices) {
		t.Fatalf("calls = %d, error = %#v", calls.Load(), err)
	}
	if record.Telemetry.ResolvedModel != "deepseek-compatible" ||
		record.Telemetry.Usage.Status != agent.UsageAvailable ||
		len(record.Telemetry.Attempts) != 1 ||
		record.Telemetry.Attempts[0].ProviderRequestID != "empty-request" {
		t.Fatalf("telemetry = %#v", record.Telemetry)
	}
}

func TestCompleteDoesNotRetryInvalidToolCallEnvelope(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		_, _ = writer.Write([]byte(`{
			"choices":[{
				"message":{
					"tool_calls":[{
						"id":"",
						"type":"function",
						"function":{"name":"submit_result","arguments":"{}"}
					}]
				}
			}]
		}`))
	}))
	defer server.Close()

	client, _ := NewOpenAIClient("gateway", server.URL, "secret", "model", time.Second)
	client.retryDelay = 0
	_, err := client.Complete(context.Background(), []agent.Message{{Role: "user", Content: "x"}}, nil)
	modelErr := agent.AsModelError(err)
	if calls.Load() != 1 ||
		modelErr == nil ||
		modelErr.Category != "response" ||
		modelErr.Code != "invalid_response" ||
		modelErr.Retryable ||
		!errors.Is(err, errInvalidToolCall) {
		t.Fatalf("calls = %d, error = %#v", calls.Load(), err)
	}
}

func TestCompleteClassifiesNonSuccessResponseWithoutLeakingBody(t *testing.T) {
	tests := []struct {
		name        string
		status      int
		contentType string
		body        string
	}{
		{
			name:        "json",
			status:      http.StatusBadRequest,
			contentType: "application/json",
			body:        `{"error":{"message":"private json provider body"}}`,
		},
		{
			name:        "plaintext",
			status:      http.StatusInternalServerError,
			contentType: "text/plain",
			body:        "private plaintext provider body",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				calls.Add(1)
				writer.Header().Set("Content-Type", test.contentType)
				writer.Header().Set("x-request-id", "failed-request")
				writer.WriteHeader(test.status)
				_, _ = writer.Write([]byte(test.body))
			}))
			defer server.Close()

			client, _ := NewOpenAIClient("gateway", server.URL, "secret", "model", time.Second)
			client.maxAttempts = 1
			var record agent.TelemetryRecord
			ctx := agent.WithTelemetryRecorder(context.Background(), func(_ context.Context, value agent.TelemetryRecord) error {
				record = value
				return nil
			}, "logical", "step")

			_, err := client.Complete(ctx, []agent.Message{{Role: "user", Content: "x"}}, nil)
			modelErr := agent.AsModelError(err)
			if calls.Load() != 1 ||
				modelErr == nil ||
				modelErr.Category != "http" ||
				modelErr.Code != fmt.Sprintf("http_%d", test.status) {
				t.Fatalf("calls = %d, error = %#v", calls.Load(), err)
			}
			encoded, _ := json.Marshal(record.Telemetry)
			if strings.Contains(string(encoded), test.body) || strings.Contains(err.Error(), test.body) {
				t.Fatalf("provider body leaked: error=%v telemetry=%s", err, encoded)
			}
		})
	}
}

func TestCompleteDoesNotRetryOversizedSuccessResponse(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		_, _ = writer.Write([]byte(strings.Repeat("x", maxResponseBytes+1)))
	}))
	defer server.Close()

	client, _ := NewOpenAIClient("gateway", server.URL, "secret", "model", time.Second)
	client.retryDelay = 0
	_, err := client.Complete(context.Background(), []agent.Message{{Role: "user", Content: "x"}}, nil)
	modelErr := agent.AsModelError(err)
	if calls.Load() != 1 ||
		modelErr == nil ||
		modelErr.Category != "response" ||
		modelErr.Code != "response_too_large" ||
		modelErr.Retryable ||
		!errors.Is(err, errResponseTooLarge) {
		t.Fatalf("calls = %d, error = %#v", calls.Load(), err)
	}
}

func TestCompleteRetriesTransientResponseReadFailures(t *testing.T) {
	tests := []struct {
		name         string
		readErr      error
		closeErr     error
		cause        error
		wantCategory string
		wantCode     string
	}{
		{
			name:         "timeout",
			readErr:      timeoutReadError{},
			cause:        timeoutReadError{},
			wantCategory: "timeout",
			wantCode:     "response_read_timeout",
		},
		{
			name:         "connection reset",
			readErr:      syscall.ECONNRESET,
			cause:        syscall.ECONNRESET,
			wantCategory: "transport",
			wantCode:     "response_read_failed",
		},
		{
			name:         "close connection reset",
			closeErr:     syscall.ECONNRESET,
			cause:        syscall.ECONNRESET,
			wantCategory: "transport",
			wantCode:     "response_read_failed",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var calls atomic.Int32
			var failedBody *trackingReadCloser
			client, _ := NewOpenAIClient("gateway", "https://provider.invalid", "secret", "model", time.Second)
			client.retryDelay = 0
			client.httpClient.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
				if calls.Add(1) == 1 {
					var reader io.Reader = strings.NewReader(`{
						"id":"first-request","model":"resolved",
						"choices":[{"message":{"content":"done"}}]
					}`)
					if test.readErr != nil {
						reader = errorReader{err: test.readErr}
					}
					failedBody = &trackingReadCloser{Reader: reader, closeErr: test.closeErr}
					return &http.Response{
						StatusCode: http.StatusOK,
						Header:     http.Header{"X-Request-Id": []string{"failed-request"}},
						Body:       failedBody,
					}, nil
				}
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body: io.NopCloser(strings.NewReader(`{
						"id":"successful-request","model":"resolved",
						"choices":[{"message":{"content":"done"}}]
					}`)),
				}, nil
			})

			response, err := client.Complete(
				context.Background(),
				[]agent.Message{{Role: "user", Content: "x"}},
				nil,
			)
			if err != nil {
				t.Fatalf("Complete() error = %v", err)
			}
			failed := response.Telemetry.Attempts[0]
			if calls.Load() != 2 ||
				failedBody == nil ||
				!failedBody.closed.Load() ||
				failed.Error == nil ||
				failed.Error.Category != test.wantCategory ||
				failed.Error.Code != test.wantCode ||
				!failed.Error.Retryable ||
				!errors.Is(failed.Error, test.cause) {
				t.Fatalf(
					"calls = %d, closed = %v, failed attempt = %#v",
					calls.Load(),
					failedBody != nil && failedBody.closed.Load(),
					failed,
				)
			}
		})
	}
}

func TestCompleteDoesNotRetryOrdinaryClientError(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		http.Error(writer, "contains raw provider details", http.StatusBadRequest)
	}))
	defer server.Close()
	client, _ := NewOpenAIClient("gateway", server.URL, "secret", "model", time.Second)
	client.retryDelay = 0

	_, err := client.Complete(context.Background(), []agent.Message{{Role: "user", Content: "x"}}, nil)
	modelErr := agent.AsModelError(err)
	if calls.Load() != 1 || modelErr == nil || modelErr.Retryable || modelErr.Code != "http_400" {
		t.Fatalf("calls = %d, error = %#v", calls.Load(), err)
	}
	if strings.Contains(err.Error(), "raw provider details") {
		t.Fatalf("error leaked provider body: %v", err)
	}
}

func TestCompleteRetriesOnlyConfiguredHTTPStatuses(t *testing.T) {
	for _, status := range []int{408, 429, 500, 502, 503, 504} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				if calls.Add(1) == 1 {
					http.Error(writer, "temporary", status)
					return
				}
				_, _ = writer.Write([]byte(`{
					"model":"resolved",
					"choices":[{"finish_reason":"stop","message":{"content":"done"}}]
				}`))
			}))
			defer server.Close()
			client, _ := NewOpenAIClient("gateway", server.URL, "secret", "model", time.Second)
			client.retryDelay = 0
			if _, err := client.Complete(context.Background(), []agent.Message{{Role: "user", Content: "x"}}, nil); err != nil {
				t.Fatal(err)
			}
			if calls.Load() != 2 {
				t.Fatalf("HTTP %d calls = %d, want 2", status, calls.Load())
			}
		})
	}
}

func TestCompleteDoesNotRetryCancellation(t *testing.T) {
	var calls atomic.Int32
	started := make(chan struct{})
	client, _ := NewOpenAIClient("gateway", "https://provider.invalid", "secret", "model", time.Minute)
	client.httpClient.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls.Add(1)
		close(started)
		<-request.Context().Done()
		return nil, request.Context().Err()
	})
	client.retryDelay = 0
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-started
		cancel()
	}()

	_, err := client.Complete(ctx, []agent.Message{{Role: "user", Content: "x"}}, nil)
	if !errors.Is(err, context.Canceled) || calls.Load() != 1 {
		t.Fatalf("calls = %d, error = %v", calls.Load(), err)
	}
}

func TestCompleteDoesNotRetryCancellationWhileReadingResponse(t *testing.T) {
	var calls atomic.Int32
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		calls.Add(1)
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusOK)
		writer.(http.Flusher).Flush()
		close(started)
		<-request.Context().Done()
	}))
	defer server.Close()

	client, _ := NewOpenAIClient("gateway", server.URL, "secret", "model", time.Minute)
	client.retryDelay = 0
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-started
		cancel()
	}()

	_, err := client.Complete(ctx, []agent.Message{{Role: "user", Content: "x"}}, nil)
	if !errors.Is(err, context.Canceled) || calls.Load() != 1 {
		t.Fatalf("calls = %d, error = %v", calls.Load(), err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func TestNewOpenAIClientRequiresExplicitProvider(t *testing.T) {
	if _, err := NewOpenAIClient("", "https://example.com", "secret", "model", time.Second); err == nil {
		t.Fatal("NewOpenAIClient() accepted an empty provider")
	}
}

func TestOfficialEndpointCompatibility(t *testing.T) {
	if os.Getenv("RUN_LLM_INTEGRATION") != "1" {
		t.Skip("set RUN_LLM_INTEGRATION=1 to call the configured LLM endpoint")
	}
	client, err := NewOpenAIClient(
		os.Getenv("AI_PLANNING_PROVIDER"),
		os.Getenv("AI_PLANNING_BASE_URL"),
		os.Getenv("AI_PLANNING_API_KEY"),
		os.Getenv("AI_PLANNING_MODEL"),
		2*time.Minute,
	)
	if err != nil {
		t.Fatal("status=0 model= usage=unavailable category=configuration")
	}

	t.Run("minimal", func(t *testing.T) {
		response, callErr := client.Complete(
			context.Background(),
			[]agent.Message{{Role: "user", Content: "Reply exactly OK."}},
			nil,
		)
		logOfficialProbe(t, response, callErr, false)
	})
	t.Run("medium tool call", func(t *testing.T) {
		response, callErr := client.Complete(
			context.Background(),
			[]agent.Message{{
				Role:    "user",
				Content: "Call submit_plan exactly once with a three-step plan for testing a login page. Do not answer in plain text.",
			}},
			[]agent.ToolDefinition{{
				Name:        "submit_plan",
				Description: "Submit a structured web test plan.",
				InputSchema: json.RawMessage(`{
					"type":"object",
					"properties":{
						"title":{"type":"string"},
						"priority":{"type":"string","enum":["low","medium","high"]},
						"steps":{
							"type":"array",
							"items":{
								"type":"object",
								"properties":{
									"action":{"type":"string"},
									"expected":{"type":"string"}
								},
								"required":["action","expected"],
								"additionalProperties":false
							},
							"minItems":3,
							"maxItems":3
						}
					},
					"required":["title","priority","steps"],
					"additionalProperties":false
				}`),
			}},
		)
		logOfficialProbe(t, response, callErr, true)
	})
}

func logOfficialProbe(
	t *testing.T,
	response agent.ModelResponse,
	err error,
	requireToolCall bool,
) {
	t.Helper()
	if err != nil {
		modelErr := agent.AsModelError(err)
		category := "unknown"
		status := 0
		if modelErr != nil {
			category = modelErr.Category
		}
		t.Fatalf(
			"status=%d model=%s usage=%s category=%s",
			status,
			os.Getenv("AI_PLANNING_MODEL"),
			agent.UsageUnavailable,
			category,
		)
	}
	lastAttempt := response.Telemetry.Attempts[len(response.Telemetry.Attempts)-1]
	status := 0
	if lastAttempt.HTTPStatus != nil {
		status = *lastAttempt.HTTPStatus
	}
	category := "success_text"
	if len(response.ToolCalls) > 0 {
		category = "success_tool"
	}
	t.Logf(
		"status=%d model=%s usage=%s category=%s",
		status,
		response.Telemetry.ResolvedModel,
		response.Telemetry.Usage.Status,
		category,
	)
	if requireToolCall {
		if len(response.ToolCalls) != 1 ||
			response.ToolCalls[0].Name != "submit_plan" ||
			!json.Valid([]byte(response.ToolCalls[0].Arguments)) {
			t.Fatal("status=200 model=redacted usage=unavailable category=invalid_response")
		}
	}
}

func TestCompleteReturnsInvalidToolArgumentsForHarnessRecovery(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{
			"choices":[{
				"message":{
					"role":"assistant",
					"tool_calls":[{
						"id":"call-1",
						"type":"function",
						"function":{"name":"broken","arguments":"{"}
					}]
				}
			}]
		}`))
	}))
	defer server.Close()

	client, err := NewOpenAIClient("test-provider", server.URL, "secret", "test-model", time.Second)
	if err != nil {
		t.Fatalf("NewOpenAIClient() error = %v", err)
	}
	response, err := client.Complete(
		context.Background(),
		[]agent.Message{{Role: "user", Content: "test"}},
		nil,
	)
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	if len(response.ToolCalls) != 1 || response.ToolCalls[0].Arguments != "{" {
		t.Fatalf("tool calls = %#v", response.ToolCalls)
	}
}

func TestReadAndCloseResponseReturnsCloseCause(t *testing.T) {
	closeErr := errors.New("close failed")
	body := &trackingReadCloser{
		Reader:   strings.NewReader(`{"choices":[]}`),
		closeErr: closeErr,
	}

	_, err := readAndCloseResponse(body)
	if !body.closed.Load() || !errors.Is(err, closeErr) {
		t.Fatalf("closed = %v, error = %v", body.closed.Load(), err)
	}
}

type trackingReadCloser struct {
	io.Reader
	closeErr error
	closed   atomic.Bool
}

func (body *trackingReadCloser) Close() error {
	body.closed.Store(true)
	return body.closeErr
}

type errorReader struct {
	err error
}

func (reader errorReader) Read([]byte) (int, error) {
	return 0, reader.err
}

type timeoutReadError struct{}

func (timeoutReadError) Error() string   { return "read timeout" }
func (timeoutReadError) Timeout() bool   { return true }
func (timeoutReadError) Temporary() bool { return true }
