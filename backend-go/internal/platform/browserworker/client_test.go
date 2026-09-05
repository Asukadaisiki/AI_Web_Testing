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
