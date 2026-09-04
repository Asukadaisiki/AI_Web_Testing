package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/agentcore"
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

	client, err := NewOpenAIClient(server.URL, "secret", "test-model", time.Second)
	if err != nil {
		t.Fatalf("NewOpenAIClient() error = %v", err)
	}
	response, err := client.Complete(
		context.Background(),
		[]agentcore.Message{{Role: "user", Content: "test login"}},
		[]agentcore.ToolDefinition{{
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
}

func TestCompleteRejectsInvalidToolArguments(t *testing.T) {
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

	client, err := NewOpenAIClient(server.URL, "secret", "test-model", time.Second)
	if err != nil {
		t.Fatalf("NewOpenAIClient() error = %v", err)
	}
	_, err = client.Complete(context.Background(), []agentcore.Message{{Role: "user", Content: "test"}}, nil)
	if err == nil {
		t.Fatal("Complete() error = nil, want invalid tool call error")
	}
}
