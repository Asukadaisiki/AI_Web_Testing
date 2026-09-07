package browserworker

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestExecuteBrowserCapability(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/api/v1/internal/browser-capabilities/explore_page" {
			t.Fatalf("path = %q", request.URL.Path)
		}
		var payload capabilityRequest
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if payload.ActorUserID != 5 || payload.ProjectID != 7 || payload.ConversationID != "11" {
			t.Fatalf("payload = %#v", payload)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"result":{"url":"https://example.com","element_count":1}}`))
	}))
	defer server.Close()

	client, err := NewClient(server.URL+"/api/v1", time.Second)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	result, err := client.ExecuteBrowserCapability(
		context.Background(),
		"explore_page",
		5,
		7,
		"11",
		json.RawMessage(`{"url":"https://example.com"}`),
	)
	if err != nil {
		t.Fatalf("ExecuteBrowserCapability() error = %v", err)
	}
	if string(result) != `{"url":"https://example.com","element_count":1}` {
		t.Fatalf("result = %s", result)
	}
}

func TestExecuteBrowserCapabilityRequiresProject(t *testing.T) {
	client, err := NewClient("http://127.0.0.1:8000/api/v1", time.Second)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	_, err = client.ExecuteBrowserCapability(
		context.Background(),
		"explore_page",
		1,
		0,
		"11",
		json.RawMessage(`{"url":"https://example.com"}`),
	)
	if err == nil {
		t.Fatal("ExecuteBrowserCapability() error = nil, want project validation error")
	}
}

func TestExecuteBrowserCapabilitySendsResolvedContext(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var payload capabilityRequest
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if payload.Context["clean_context"] != true {
			t.Fatalf("clean_context = %#v", payload.Context["clean_context"])
		}
		if payload.Context["entry_url_or_page"] != "https://example.com/start" {
			t.Fatalf("entry_url_or_page = %#v", payload.Context["entry_url_or_page"])
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"result":{"ok":true}}`))
	}))
	defer server.Close()

	client, err := NewClient(server.URL+"/api/v1", time.Second)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	client.SetContextResolver(func(
		_ context.Context,
		actorUserID int64,
		projectID int64,
		conversationID string,
	) (map[string]any, error) {
		if actorUserID != 5 || projectID != 7 || conversationID != "11" {
			t.Fatalf("resolver context = %d/%d/%q", actorUserID, projectID, conversationID)
		}
		return map[string]any{
			"clean_context":     true,
			"entry_url_or_page": "https://example.com/start",
		}, nil
	})

	result, err := client.ExecuteBrowserCapability(
		context.Background(),
		"explore_flow",
		5,
		7,
		"11",
		json.RawMessage(`{"steps":[{"url":"/"}]}`),
	)
	if err != nil {
		t.Fatalf("ExecuteBrowserCapability() error = %v", err)
	}
	if string(result) != `{"ok":true}` {
		t.Fatalf("result = %s", result)
	}
}

func TestExecuteBrowserCase(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/api/v1/internal/browser-executions" {
			t.Fatalf("path = %q", request.URL.Path)
		}
		var payload BrowserExecutionRequest
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if payload.ExecutionID != 91 {
			t.Fatalf("execution_id = %d", payload.ExecutionID)
		}
		if string(payload.DSLCase) != `{"name":"case","steps":[{"action":"goto","value":"https://example.com"}]}` {
			t.Fatalf("dsl_case = %s", payload.DSLCase)
		}
		if payload.InputValues["email"] != "test@example.com" {
			t.Fatalf("input_values = %#v", payload.InputValues)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"status":"passed","report":{"status":"passed","steps":[]}}`))
	}))
	defer server.Close()

	client, err := NewClient(server.URL+"/api/v1", time.Second)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	result, err := client.ExecuteBrowserCase(
		context.Background(),
		BrowserExecutionRequest{
			ExecutionID: 91,
			DSLCase:     json.RawMessage(`{"name":"case","steps":[{"action":"goto","value":"https://example.com"}]}`),
			InputValues: map[string]string{"email": "test@example.com"},
		},
	)
	if err != nil {
		t.Fatalf("ExecuteBrowserCase() error = %v", err)
	}
	if string(result) != `{"status":"passed","report":{"status":"passed","steps":[]}}` {
		t.Fatalf("result = %s", result)
	}
}
