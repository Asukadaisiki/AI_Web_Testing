package v3

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/postgres"
)

type managementRepository struct {
	bridgeRepository
	binding     postgres.ProjectBinding
	environment postgres.Environment
	actor       string
	action      string
}

func ownedManagementRepository() *managementRepository {
	return &managementRepository{
		bridgeRepository: *ownedBridgeRepository(),
		binding:          postgres.ProjectBinding{AgenttraceProjectID: "project_1", LangfuseProjectID: "lf_1", Status: "active"},
		environment:      postgres.Environment{ID: "env_1", ProjectID: "project_1", ConversationID: "conv_1", State: "ready", ProfileRef: "private/reference"},
	}
}

func (r *managementRepository) GetProjectBinding(context.Context, string) (postgres.ProjectBinding, error) {
	return r.binding, r.lookupErr
}
func (r *managementRepository) GetEnvironment(context.Context, string) (postgres.Environment, error) {
	return r.environment, r.lookupErr
}
func (r *managementRepository) GetConversationEnvironment(context.Context, string) (postgres.Environment, error) {
	return r.environment, r.lookupErr
}
func (r *managementRepository) ResetEnvironment(_ context.Context, _ string, actor string) (postgres.Environment, error) {
	r.action, r.actor = "reset", actor
	r.environment.State = "resetting"
	return r.environment, nil
}
func (r *managementRepository) ReviewEnvironment(_ context.Context, _ string, actor string) (postgres.Environment, error) {
	r.action, r.actor = "review", actor
	r.environment.State = "ready"
	return r.environment, nil
}
func (r *managementRepository) DeleteEnvironment(_ context.Context, _ string, actor string) error {
	r.action, r.actor = "delete", actor
	return nil
}
func (r *managementRepository) ArchiveConversation(_ context.Context, _ string, actor string) (postgres.Conversation, error) {
	r.action, r.actor = "archive", actor
	r.conversation.State = "archived"
	return r.conversation, nil
}
func (r *managementRepository) DeleteConversationAs(_ context.Context, _ string, actor string) error {
	r.action, r.actor = "delete", actor
	return nil
}
func (r *managementRepository) ControlSessionOwner(context.Context, string) (string, string, error) {
	return r.environment.ProjectID, r.environment.ID, r.lookupErr
}

func dynamicManagementHandler(repository Repository, next http.Handler) http.Handler {
	server := &Server{repository: repository}
	if next == nil {
		mux := http.NewServeMux()
		server.registerEnvironmentRoutes(mux)
		mux.HandleFunc("GET /api/v1/projects/{id}/conversations", server.handleListConversations)
		mux.HandleFunc("DELETE /api/v1/conversations/{id}", server.handleDeleteConversation)
		next = mux
	}
	return server.bridgeGuard(next, ServerConfig{BridgeToken: testBridgeToken})
}

func managementRequest(handler http.Handler, method, path, body, role string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+testBridgeToken)
	req.Header.Set("X-Agenttrace-Project-Id", "project_1")
	req.Header.Set("X-Langfuse-Project-Id", "lf_1")
	req.Header.Set("X-Agenttrace-Role", role)
	req.Header.Set("X-Agenttrace-Actor-Id", "actor_1")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	return recorder
}

func TestDynamicBridgeTrustedHeadersBindingAndOwnership(t *testing.T) {
	for _, test := range []struct {
		name, header, value string
		want                int
	}{
		{"owned", "", "", 204},
		{"wrong bearer", "Authorization", "Bearer other", 401},
		{"missing project", "X-Agenttrace-Project-Id", "", 403},
		{"missing actor", "X-Agenttrace-Actor-Id", "", 403},
		{"missing role", "X-Agenttrace-Role", "", 403},
		{"unknown role", "X-Agenttrace-Role", "superuser", 403},
		{"missing langfuse", "X-Langfuse-Project-Id", "", 403},
		{"wrong langfuse", "X-Langfuse-Project-Id", "lf_other", 403},
		{"other project", "X-Agenttrace-Project-Id", "other", 403},
	} {
		t.Run(test.name, func(t *testing.T) {
			repository := ownedManagementRepository()
			next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				identity, ok := BridgeIdentityFromContext(r.Context())
				if !ok || identity.ProjectID != "project_1" || identity.ActorID != "actor_1" {
					t.Fatalf("trusted identity=%+v ok=%v", identity, ok)
				}
				w.WriteHeader(204)
			})
			req := httptest.NewRequest("GET", "/api/v1/conversations/conv_1", nil)
			req.Header.Set("Authorization", "Bearer "+testBridgeToken)
			req.Header.Set("X-Agenttrace-Project-Id", "project_1")
			req.Header.Set("X-Langfuse-Project-Id", "lf_1")
			req.Header.Set("X-Agenttrace-Role", "member")
			req.Header.Set("X-Agenttrace-Actor-Id", "actor_1")
			if test.header != "" {
				req.Header.Set(test.header, test.value)
			}
			recorder := httptest.NewRecorder()
			dynamicManagementHandler(repository, next).ServeHTTP(recorder, req)
			if recorder.Code != test.want {
				t.Fatalf("status=%d want=%d body=%s", recorder.Code, test.want, recorder.Body)
			}
		})
	}
}

func TestDynamicBridgeRegistryExactRoutesAndRoles(t *testing.T) {
	handler := dynamicManagementHandler(ownedManagementRepository(), http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) }))
	for _, route := range []struct{ method, path, role string }{
		{"POST", "/projects/project_1/skills/uploads", "member"},
		{"POST", "/projects/project_1/skills/skill_1/versions", "project_admin"},
		{"POST", "/projects/project_1/skill-versions/version_1/publish", "project_admin"},
		{"POST", "/projects/project_1/skill-versions/version_1/revoke", "project_admin"},
		{"GET", "/projects/project_1/mcp-bindings", "member"},
		{"POST", "/projects/project_1/mcp-bindings", "project_admin"},
		{"PATCH", "/projects/project_1/mcp-bindings/binding_1", "project_admin"},
		{"GET", "/projects/project_1/harnesses", "member"},
		{"POST", "/projects/project_1/harnesses", "project_admin"},
		{"POST", "/projects/project_1/harnesses/harness_1/versions", "project_admin"},
		{"POST", "/projects/project_1/harness-versions/version_1/revoke", "project_admin"},
		{"GET", "/mcp-definitions", "system_admin"},
		{"GET", "/mcp-definitions", "project_admin"},
		{"GET", "/mcp-definitions", "admin"},
		{"POST", "/mcp-definitions", "system_admin"},
		{"POST", "/mcp-versions/version_1/revoke", "system_admin"},
	} {
		got := managementRequest(handler, route.method, "/api/v1"+route.path, "{}", route.role)
		if got.Code != 204 {
			t.Errorf("%s %s role=%s status=%d body=%s", route.method, route.path, route.role, got.Code, got.Body)
		}
	}
	for _, route := range []struct{ method, path, role string }{
		{"POST", "/projects/project_1/mcp-bindings", "member"},
		{"POST", "/projects/project_1/skill-versions/v/publish", "member"},
		{"GET", "/projects/project_1/services", "member"},
		{"GET", "/mcp-definitions", "member"},
		{"POST", "/mcp-definitions", "project_admin"},
		{"POST", "/mcp-versions/v/revoke", "project_admin"},
		{"GET", "/projects/foreign/skills", "system_admin"},
		{"POST", "/projects/project_1/skills/uploads/extra", "project_admin"},
		{"DELETE", "/projects/project_1", "system_admin"},
		{"GET", "/harnesses", "system_admin"},
		{"GET", "/projects", "system_admin"},
		{"GET", "/projects/project_1/../../mcp-definitions", "system_admin"},
	} {
		if got := managementRequest(handler, route.method, "/api/v1"+route.path, "{}", route.role); got.Code != 403 {
			t.Errorf("unexpected allowed route %s %s status=%d", route.method, route.path, got.Code)
		}
	}
}

func TestEnvironmentHandlersOwnershipAsyncActionsAndProfilePrivacy(t *testing.T) {
	repository := ownedManagementRepository()
	handler := dynamicManagementHandler(repository, nil)
	for _, path := range []string{"/api/v1/environments/env_1", "/api/v1/conversations/conv_1/environment"} {
		got := managementRequest(handler, "GET", path, "", "member")
		if got.Code != 200 || strings.Contains(got.Body.String(), "private/reference") {
			t.Fatalf("read=%d body=%s", got.Code, got.Body)
		}
	}
	for _, route := range []struct{ method, path, body, action string }{
		{"POST", "/api/v1/environments/env_1/reset", "{}", "reset"},
		{"PATCH", "/api/v1/conversations/conv_1", `{"state":"archived"}`, "archive"},
		{"DELETE", "/api/v1/environments/env_1", "", "delete"},
		{"DELETE", "/api/v1/conversations/conv_1", "", "delete"},
	} {
		got := managementRequest(handler, route.method, route.path, route.body, "member")
		want := 202
		if route.action == "archive" {
			want = 200
		}
		if got.Code != want || repository.action != route.action || repository.actor != "actor_1" {
			t.Fatalf("%s %s status=%d action=%s actor=%s body=%s", route.method, route.path, got.Code, repository.action, repository.actor, got.Body)
		}
	}
	repository.environment.ProjectID = "foreign"
	repository.action = ""
	if got := managementRequest(handler, "POST", "/api/v1/environments/env_1/reset", "{}", "member"); got.Code != 403 || repository.action != "" {
		t.Fatalf("foreign reset=%d action=%s", got.Code, repository.action)
	}
}

func TestDynamicBridgeDuplicateScopeAndForeignRunFailClosed(t *testing.T) {
	repository := ownedManagementRepository()
	handler := dynamicManagementHandler(repository, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) }))
	for _, header := range []string{"X-Agenttrace-Project-Id", "X-Langfuse-Project-Id", "X-Agenttrace-Role", "X-Agenttrace-Actor-Id"} {
		req := httptest.NewRequest("GET", "/api/v1/runs/run_1/trace", nil)
		req.Header.Set("Authorization", "Bearer "+testBridgeToken)
		req.Header.Set("X-Agenttrace-Project-Id", "project_1")
		req.Header.Set("X-Langfuse-Project-Id", "lf_1")
		req.Header.Set("X-Agenttrace-Role", "member")
		req.Header.Set("X-Agenttrace-Actor-Id", "actor_1")
		req.Header.Add(header, "duplicate")
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, req)
		if recorder.Code != 403 {
			t.Fatalf("duplicate %s=%d", header, recorder.Code)
		}
	}
	repository.run.ProjectID = "foreign"
	for _, path := range []string{"/api/v1/runs/run_1/trace", "/api/v1/runs/run_1/events", "/artifacts/run_1/shot.png"} {
		if got := managementRequest(handler, "GET", path, "", "member"); got.Code != 403 || repository.traceReads != 0 {
			t.Fatalf("foreign run path=%s status=%d reads=%d", path, got.Code, repository.traceReads)
		}
	}
}

func TestManagementBridgeIgnoresLegacyConversationPin(t *testing.T) {
	repository := ownedManagementRepository()
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) })
	server := &Server{repository: repository}
	handler := server.bridgeGuard(next, ServerConfig{
		BridgeToken: testBridgeToken, BridgeProjectID: "project_1",
	})
	for _, path := range []string{"/api/v1/conversations/conv_1", "/api/v1/runs/run_1", "/api/v1/projects/project_1/conversations"} {
		if got := managementRequest(handler, "GET", path, "", "member"); got.Code != 204 {
			t.Fatalf("legacy pin restricted management: %s status=%d body=%s", path, got.Code, got.Body)
		}
	}
}

func TestManagementFutureControlRoutesRepeatEnvironmentOwnership(t *testing.T) {
	repository := ownedManagementRepository()
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) })
	handler := dynamicManagementHandler(repository, next)
	for _, route := range []struct{ method, path string }{
		{"POST", "/api/v1/environments/env_1/review"},
		{"GET", "/api/v1/control-sessions/control_1/screenshot"},
		{"POST", "/api/v1/control-sessions/control_1/input"},
	} {
		got := managementRequest(handler, route.method, route.path, "{}", "member")
		if got.Code != 204 {
			t.Fatalf("route=%s status=%d body=%s", route.path, got.Code, got.Body)
		}
		repository.environment.ProjectID = "foreign"
		got = managementRequest(handler, route.method, route.path, "{}", "member")
		if got.Code != 403 {
			t.Fatalf("foreign route=%s status=%d", route.path, got.Code)
		}
		repository.environment.ProjectID = "project_1"
	}
	if got := managementRequest(handler, "POST", "/api/v1/control-sessions/control_1/input", "{}", "viewer"); got.Code != 403 {
		t.Fatalf("viewer control input=%d", got.Code)
	}
	for _, path := range []string{"/api/v1/control-sessions/c/input/extra", "/api/v1/environments/e/review/extra"} {
		if got := managementRequest(handler, "POST", path, "{}", "member"); got.Code != 403 {
			t.Fatalf("extra route=%s status=%d", path, got.Code)
		}
	}
}

func TestEnvironmentReviewRouteRecordsActorAndHidesProfile(t *testing.T) {
	repository := ownedManagementRepository()
	repository.environment.State = "needs_review"
	handler := dynamicManagementHandler(repository, nil)
	got := managementRequest(handler, "POST", "/api/v1/environments/env_1/review", "{}", "member")
	if got.Code != 200 || repository.action != "review" || repository.actor != "actor_1" ||
		strings.Contains(got.Body.String(), "private/reference") {
		t.Fatalf("review status=%d action=%s actor=%s body=%s", got.Code, repository.action, repository.actor, got.Body)
	}
	repository.action = ""
	got = managementRequest(handler, "POST", "/api/v1/environments/env_1/review", "{}", "viewer")
	if got.Code != 403 || repository.action != "" {
		t.Fatalf("viewer review status=%d action=%s", got.Code, repository.action)
	}
}
