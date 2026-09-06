package llm

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
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

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func TestNewOpenAIClientRequiresExplicitProvider(t *testing.T) {
	if _, err := NewOpenAIClient("", "https://example.com", "secret", "model", time.Second); err == nil {
		t.Fatal("NewOpenAIClient() accepted an empty provider")
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
