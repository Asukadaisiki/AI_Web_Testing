package v3

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestManagementOnlyRejectsRetiredRoutesWithoutModeFlag(t *testing.T) {
	handler := New(ServerConfig{Repository: ownedManagementRepository(), BridgeToken: testBridgeToken}).Handler()
	for _, route := range []struct{ method, path string }{
		{"GET", "/api/v1/agents"},
		{"GET", "/api/v1/harnesses"},
		{"POST", "/api/v1/harnesses"},
		{"PUT", "/api/v1/harnesses/global"},
		{"DELETE", "/api/v1/harnesses/global"},
		{"GET", "/api/v1/projects"},
		{"POST", "/api/v1/projects"},
		{"DELETE", "/api/v1/projects/project_1"},
		{"POST", "/api/v1/runs/run_1/responses"},
	} {
		got := managementRequest(handler, route.method, route.path, "{}", "system_admin")
		if got.Code != http.StatusForbidden {
			t.Errorf("%s %s operated: %d %s", route.method, route.path, got.Code, got.Body)
		}
	}
}

func TestManagementOnlyEmptyAuthenticationFailsClosed(t *testing.T) {
	handler := New(ServerConfig{Repository: ownedManagementRepository()}).Handler()
	got := httptest.NewRecorder()
	handler.ServeHTTP(got, httptest.NewRequest(http.MethodGet, "/api/v1/runtimes", nil))
	if got.Code != http.StatusServiceUnavailable {
		t.Fatalf("empty authentication operated: %d %s", got.Code, got.Body)
	}
}

func TestManagementOnlyRejectsAgentModelSubmission(t *testing.T) {
	for _, body := range []string{
		`{"content":"goal","agent_id":"browser-eval","model":{"provider":"scripted","name":"browser-eval-fixture"},"idempotency_key":"old"}`,
		`{"content":"goal","model":{"provider":"scripted","name":"browser-eval-fixture"},"idempotency_key":"old"}`,
		`{"content":"goal","agent_id":"pi","runtime_id":"pi","harness_id":"version","idempotency_key":"old"}`,
	} {
		repository := ownedManagementRepository()
		handler := New(ServerConfig{Repository: repository, BridgeToken: testBridgeToken}).Handler()
		got := managementRequest(handler, "POST", "/api/v1/conversations/conv_1/messages", body, "member")
		if got.Code != http.StatusBadRequest || repository.message.ConversationID != "" {
			t.Fatalf("old submission operated: %d %s", got.Code, got.Body)
		}
	}
}
