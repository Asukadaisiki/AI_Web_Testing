package model

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOpenAICompletePreservesReasoningEffortToolCallsAndUsage(t *testing.T) {
	var request wireRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read request: %v", err)
		}
		if err := json.Unmarshal(body, &request); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		_, _ = w.Write([]byte(`{
			"choices":[{"message":{"role":"assistant","tool_calls":[{
				"id":"call_1","type":"function",
				"function":{"name":"inspect","arguments":"{\"url\":\"https://example.test\"}"}
			}]}}],
			"usage":{
				"prompt_tokens":10,"completion_tokens":4,"total_tokens":14,
				"prompt_tokens_details":{"cached_tokens":3},
				"completion_tokens_details":{"reasoning_tokens":2}
			}
		}`))
	}))
	defer server.Close()

	client := NewOpenAI(OpenAIConfig{
		BaseURL:         server.URL,
		Model:           "fixture",
		ReasoningEffort: "high",
	})
	response, err := client.Complete(context.Background(), Request{
		Messages: []Message{{Role: RoleUser, Content: "inspect"}},
		Tools: []Tool{{
			Name: "inspect", Parameters: map[string]any{"type": "object"},
		}},
	})
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if request.ReasoningEffort != "high" {
		t.Fatalf("reasoning effort = %q, want high", request.ReasoningEffort)
	}
	if len(response.Message.ToolCalls) != 1 || response.Message.ToolCalls[0].Name != "inspect" {
		t.Fatalf("tool calls = %#v", response.Message.ToolCalls)
	}
	if got, want := string(response.Message.ToolCalls[0].Arguments), `{"url":"https://example.test"}`; got != want {
		t.Fatalf("tool arguments = %s, want %s", got, want)
	}
	if response.Usage.PromptTokens != 10 || response.Usage.CompletionTokens != 4 ||
		response.Usage.TotalTokens != 14 || response.Usage.FreshPromptTokens != 7 ||
		response.Usage.ReasoningTokens != 2 {
		t.Fatalf("normalized usage = %#v", response.Usage)
	}
}

func TestOpenAICompleteUsesConfiguredModelWhenRequestModelDiffers(t *testing.T) {
	var request wireRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read request: %v", err)
		}
		if err := json.Unmarshal(body, &request); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"done"}}]}`))
	}))
	defer server.Close()

	client := NewOpenAI(OpenAIConfig{BaseURL: server.URL, Model: "glm-5.3-flash"})
	if _, err := client.Complete(context.Background(), Request{
		Model:    "unapproved-client-model",
		Messages: []Message{{Role: RoleUser, Content: "inspect"}},
	}); err != nil {
		t.Fatalf("complete: %v", err)
	}
	if request.Model != "glm-5.3-flash" {
		t.Fatalf("wire model = %q, want configured model", request.Model)
	}
}
