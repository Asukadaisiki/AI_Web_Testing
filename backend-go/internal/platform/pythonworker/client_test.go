package pythonworker

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/authn"
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
		if payload.ProjectID != 7 || payload.ConversationID != "11" {
			t.Fatalf("payload = %#v", payload)
		}
		if request.Header.Get("Cookie") != "session=signed-cookie" {
			t.Fatalf("cookie = %q", request.Header.Get("Cookie"))
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"result":{"url":"https://example.com","element_count":1}}`))
	}))
	defer server.Close()

	client, err := NewClient(server.URL+"/api/v1", time.Second)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	ctx := authn.WithIdentity(context.Background(), authn.Identity{
		UserID: 7,
		Cookie: "session=signed-cookie",
	})
	result, err := client.ExecuteBrowserCapability(
		ctx,
		"explore_page",
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
		0,
		"11",
		json.RawMessage(`{"url":"https://example.com"}`),
	)
	if err == nil {
		t.Fatal("ExecuteBrowserCapability() error = nil, want project validation error")
	}
}

func TestGenerateDSL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/api/v1/internal/agent-capabilities/generate-dsl" {
			t.Fatalf("path = %q", request.URL.Path)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"result":{"generation_id":9,"case":{"name":"Login","steps":[]}}}`))
	}))
	defer server.Close()

	client, err := NewClient(server.URL+"/api/v1", time.Second)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	result, err := client.GenerateDSL(
		context.Background(),
		1,
		"11",
		json.RawMessage(`{"prompt":"test login","base_url":"https://example.com","a11y_nodes_by_state":{"S0":[]}}`),
	)
	if err != nil {
		t.Fatalf("GenerateDSL() error = %v", err)
	}
	if string(result) != `{"generation_id":9,"case":{"name":"Login","steps":[]}}` {
		t.Fatalf("result = %s", result)
	}
}
