package v3

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/domain"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/postgres"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/stream"
)

const testBridgeToken = "test-bridge-token"

func bridgeTestConfig(repository Repository, dir, token, project, conversation string) ServerConfig {
	return ServerConfig{
		Repository: repository, ArtifactDir: dir,
		BridgeToken: token, BridgeProjectID: project, BridgeConversationID: conversation,
		Health: HealthDependencies{Postgres: func(context.Context) error { return nil }},
	}
}

func bridgeRequest(t *testing.T, handler http.Handler, method, path, auth, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	return recorder
}

func ownedBridgeRepository() *bridgeRepository {
	return &bridgeRepository{fakeRepository: fakeRepository{
		run:          domain.Run{ID: "run_1", ProjectID: "project_1", ConversationID: "conv_1"},
		conversation: postgres.Conversation{ID: "conv_1", ProjectID: "project_1"},
	}}
}

func protectedBridge(repository Repository, dir string) http.Handler {
	return New(bridgeTestConfig(repository, dir, testBridgeToken, "project_1", "conv_1")).Handler()
}

func TestBridgeAuthenticationAndExactHealthException(t *testing.T) {
	handler := protectedBridge(ownedBridgeRepository(), "")
	response := request(t, handler, http.MethodGet, "/api/v1/runtimes", "")
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("unprotected runtimes: status = %d, want 401", response.Code)
	}
	health := request(t, handler, http.MethodGet, "/api/v1/health", "")
	if health.Code != http.StatusOK {
		t.Fatalf("health = %d, want 200", health.Code)
	}
	for _, test := range []struct {
		name, auth string
		want       int
	}{
		{"correct", "Bearer " + testBridgeToken, 200},
		{"wrong", "Bearer wrong-token", 401},
		{"prefix", "Bearer " + testBridgeToken + "-extra", 401},
		{"basic", "Basic " + testBridgeToken, 401},
		{"empty", "Bearer ", 401},
		{"extra whitespace", "Bearer  " + testBridgeToken, 401},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := bridgeRequest(t, handler, "GET", "/api/v1/runtimes", test.auth, "")
			if got.Code != test.want {
				t.Fatalf("status = %d, want %d", got.Code, test.want)
			}
			if strings.Contains(got.Body.String(), testBridgeToken) {
				t.Fatal("response leaked bridge token")
			}
		})
	}
	for _, path := range []string{"/api/v1/health/", "/api/v1//health", "/unknown", "/artifacts/run_1/x.png"} {
		if got := request(t, handler, "GET", path, ""); got.Code != 401 {
			t.Errorf("health exception leaked to %s: %d", path, got.Code)
		}
	}
	if got := request(t, handler, "HEAD", "/api/v1/health", ""); got.Code != 401 {
		t.Errorf("HEAD health bypassed authentication: %d", got.Code)
	}
}

func TestBridgeLegacyEmptyConfigurationIsUnchanged(t *testing.T) {
	handler := New(ServerConfig{Repository: ownedBridgeRepository()}).Handler()
	for _, path := range []string{"/api/v1/runtimes", "/api/v1/projects", "/api/v1/agents"} {
		if got := request(t, handler, "GET", path, ""); got.Code != 200 {
			t.Errorf("legacy %s = %d, want 200", path, got.Code)
		}
	}
}

func TestBridgeDuplicateAuthorizationIsRejectedBeforeOwnership(t *testing.T) {
	repository := ownedBridgeRepository()
	req := httptest.NewRequest("GET", "/api/v1/runs/run_1/trace", nil)
	req.Header.Add("Authorization", "Bearer "+testBridgeToken)
	req.Header.Add("Authorization", "Bearer wrong")
	recorder := httptest.NewRecorder()
	protectedBridge(repository, "").ServeHTTP(recorder, req)
	if recorder.Code != 401 || repository.traceReads != 0 {
		t.Fatalf("ambiguous authentication = %d, trace reads = %d", recorder.Code, repository.traceReads)
	}
}

func TestBridgePartialConfigurationFailsClosed(t *testing.T) {
	for mask := 1; mask < 7; mask++ {
		t.Run(string(rune('0'+mask)), func(t *testing.T) {
			values := []string{"", "", ""}
			for index, value := range []string{testBridgeToken, "project_1", "conv_1"} {
				if mask&(1<<index) != 0 {
					values[index] = value
				}
			}
			handler := New(bridgeTestConfig(ownedBridgeRepository(), "", values[0], values[1], values[2])).Handler()
			for _, auth := range []string{"", "Bearer " + testBridgeToken} {
				got := bridgeRequest(t, handler, "GET", "/api/v1/runtimes", auth, "")
				if got.Code != 503 {
					t.Fatalf("partial configuration exposed routes: %d, want 503", got.Code)
				}
				if strings.Contains(got.Body.String(), testBridgeToken) {
					t.Fatal("partial configuration response leaked token")
				}
			}
			if got := request(t, handler, "GET", "/api/v1/health", ""); got.Code != 200 {
				t.Fatalf("partial configuration health = %d, want 200", got.Code)
			}
		})
	}
}

func TestBridgeRouteAllowlist(t *testing.T) {
	repository := ownedBridgeRepository()
	handler := protectedBridge(repository, "")
	for _, test := range []struct {
		method, path string
	}{
		{"GET", "/api/v1/projects"}, {"POST", "/api/v1/projects"},
		{"DELETE", "/api/v1/projects/project_1"},
		{"GET", "/api/v1/projects/project_1/conversations"},
		{"POST", "/api/v1/projects/project_1/conversations"},
		{"DELETE", "/api/v1/conversations/conv_1"},
		{"GET", "/api/v1/agents"}, {"POST", "/api/v1/runs/run_1/responses"},
		{"GET", "/api/v1/runs/run_1/evaluation"}, {"GET", "/unknown"},
		{"POST", "/api/v1/runtimes"}, {"PATCH", "/api/v1/harnesses/harness_1"},
		{"GET", "/api/v1//projects"}, {"GET", "/api/v1/runs/run_1/../run_1/trace"},
		{"GET", "/api/v1/runs/run_1%2Ftrace"}, {"GET", "/api%2fv1/runtimes"},
	} {
		if got := bridgeRequest(t, handler, test.method, test.path, "Bearer "+testBridgeToken, "{}"); got.Code != 403 {
			t.Errorf("%s %s = %d, want 403", test.method, test.path, got.Code)
		}
	}
	if repository.response.RunID != "" || len(repository.deletedConversationIDs) != 0 || len(repository.deletedProjectIDs) != 0 {
		t.Fatal("denied route mutated repository")
	}
	for _, test := range []struct {
		method, path string
		want         int
	}{
		{"GET", "/api/v1/runtimes", 200}, {"GET", "/api/v1/harnesses", 503},
		{"GET", "/api/v1/harnesses/harness_1", 503},
		{"POST", "/api/v1/harnesses", 503}, {"PUT", "/api/v1/harnesses/harness_1", 503},
		{"DELETE", "/api/v1/harnesses/harness_1", 503},
		{"GET", "/api/v1/conversations/conv_1", 200}, {"POST", "/api/v1/conversations/conv_1/messages", 400},
		{"GET", "/api/v1/runs/run_1", 200}, {"GET", "/api/v1/runs/run_1/trace", 200},
		{"GET", "/api/v1/runs/run_1/events", 503},
		{"POST", "/api/v1/runs/run_1/cancel", 202}, {"POST", "/api/v1/runs/run_1/reruns", 400},
	} {
		if got := bridgeRequest(t, handler, test.method, test.path, "Bearer "+testBridgeToken, "{}"); got.Code != test.want {
			t.Errorf("allowed %s %s = %d, want %d", test.method, test.path, got.Code, test.want)
		}
	}
}

func TestBridgeRunOwnershipBeforeTraceSSEAndActions(t *testing.T) {
	for _, test := range []struct {
		name, project, conversation, id string
	}{
		{"foreign project", "foreign", "conv_1", "run_1"},
		{"foreign conversation", "project_1", "foreign", "run_1"},
		{"incorrect canonical id", "project_1", "conv_1", "different"},
	} {
		t.Run(test.name, func(t *testing.T) {
			repository := ownedBridgeRepository()
			repository.run.ProjectID, repository.run.ConversationID, repository.run.ID = test.project, test.conversation, test.id
			handler := protectedBridge(repository, "")
			for _, route := range []struct{ method, suffix, body string }{
				{"GET", "", ""}, {"GET", "/trace", ""}, {"GET", "/events", ""},
				{"POST", "/cancel", "{}"}, {"POST", "/reruns", `{"idempotency_key":"retry-1"}`},
			} {
				got := bridgeRequest(t, handler, route.method, "/api/v1/runs/run_1"+route.suffix, "Bearer "+testBridgeToken, route.body)
				if got.Code != 403 || strings.Contains(got.Header().Get("Content-Type"), "text/event-stream") {
					t.Errorf("%s ownership bypass: status = %d", route.suffix, got.Code)
				}
			}
			if repository.traceReads != 0 || repository.cancelledRunID != "" || repository.rerunInput.RunID != "" {
				t.Fatal("foreign Run reached evidence or mutation handler")
			}
		})
	}
}

func TestBridgeEventsOwnershipBeforeSSEHeaders(t *testing.T) {
	for _, test := range []struct {
		name, project, conversation string
		want                        int
	}{
		{"owned", "project_1", "conv_1", 200},
		{"foreign project", "other", "conv_1", 403},
		{"foreign conversation", "project_1", "other", 403},
	} {
		t.Run(test.name, func(t *testing.T) {
			repository := ownedBridgeRepository()
			repository.run.ProjectID, repository.run.ConversationID = test.project, test.conversation
			cfg := bridgeTestConfig(repository, "", testBridgeToken, "project_1", "conv_1")
			cfg.Stream = stream.NewService(&fakeEventReader{}, nil, stream.Config{})
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			req := httptest.NewRequest("GET", "/api/v1/runs/run_1/events", nil).WithContext(ctx)
			req.Header.Set("Authorization", "Bearer "+testBridgeToken)
			recorder := httptest.NewRecorder()
			New(cfg).Handler().ServeHTTP(recorder, req)
			if recorder.Code != test.want {
				t.Fatalf("SSE status = %d, want %d", recorder.Code, test.want)
			}
			isSSE := strings.Contains(recorder.Header().Get("Content-Type"), "text/event-stream")
			if isSSE != (test.want == 200) {
				t.Fatalf("SSE headers set for foreign Run: %v", isSSE)
			}
		})
	}
}

func TestBridgeConversationOwnershipBeforeReadAndSubmission(t *testing.T) {
	for _, test := range []struct{ name, pathID, actualID, project string }{
		{"foreign path", "other", "conv_1", "project_1"},
		{"foreign actual project", "conv_1", "conv_1", "other"},
		{"incorrect canonical id", "conv_1", "other", "project_1"},
	} {
		t.Run(test.name, func(t *testing.T) {
			repository := ownedBridgeRepository()
			repository.conversation.ID, repository.conversation.ProjectID = test.actualID, test.project
			handler := protectedBridge(repository, "")
			for _, route := range []struct{ method, suffix string }{{"GET", ""}, {"POST", "/messages"}} {
				got := bridgeRequest(t, handler, route.method, "/api/v1/conversations/"+test.pathID+route.suffix,
					"Bearer "+testBridgeToken, `{"content":"goal","idempotency_key":"submit-1"}`)
				if got.Code != 403 {
					t.Errorf("foreign Conversation %s = %d, want 403", route.suffix, got.Code)
				}
			}
			if repository.message.ConversationID != "" {
				t.Fatal("foreign Conversation submission reached repository")
			}
		})
	}
}

func TestBridgeOwnershipLookupErrorsFailClosed(t *testing.T) {
	for _, err := range []error{postgres.ErrNotFound, errors.New("storage failure")} {
		repository := ownedBridgeRepository()
		repository.lookupErr = err
		handler := protectedBridge(repository, "")
		want := 503
		if errors.Is(err, postgres.ErrNotFound) {
			want = 404
		}
		for _, path := range []string{"/api/v1/runs/run_1/events", "/api/v1/runs/run_1/trace", "/api/v1/conversations/conv_1"} {
			if got := bridgeRequest(t, handler, "GET", path, "Bearer "+testBridgeToken, ""); got.Code != want {
				t.Errorf("lookup failure %s = %d, want %d", path, got.Code, want)
			}
		}
		if repository.traceReads != 0 {
			t.Fatal("failed ownership lookup reached trace reader")
		}
	}
}

func TestBridgeArtifactsRequireCanonicalRegularContainedScreenshot(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"run_1/shot.png", "run_1/unlisted.png", "run_1/only-result.png", "other/foreign.png"} {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("screenshot bytes"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(dir, "run_1", "directory"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(filepath.Join(dir, "run_1", "pipe.png"), 0600); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside.png")
	if err := os.WriteFile(outside, []byte("private outside"), 0600); err != nil {
		t.Fatal(err)
	}
	for name, target := range map[string]string{
		"escape.png": outside, "cross-run.png": filepath.Join(dir, "other", "foreign.png"),
		"internal.png": "shot.png",
	} {
		if err := os.Symlink(target, filepath.Join(dir, "run_1", name)); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(dir, filepath.Join(dir, "run_1", "escape-dir")); err != nil {
		t.Fatal(err)
	}
	repository := ownedBridgeRepository()
	repository.trace.Operations = []postgres.TraceOperationDetail{{
		Artifacts: json.RawMessage(`{"screenshot_paths":["run_1/shot.png","run_1/directory","run_1/pipe.png","run_1/missing.png","run_1/escape.png","run_1/cross-run.png","run_1/internal.png","run_1/escape-dir/other/foreign.png","other/foreign.png"]}`),
		Result:    json.RawMessage(`{"screenshot_paths":["run_1/only-result.png"]}`),
	}}
	handler := protectedBridge(repository, dir)
	if got := request(t, handler, "GET", "/artifacts/run_1/shot.png", ""); got.Code != 401 {
		t.Fatalf("unauthenticated artifact = %d, want 401", got.Code)
	}
	if got := bridgeRequest(t, handler, "GET", "/artifacts/run_1/shot.png", "Bearer wrong", ""); got.Code != 401 {
		t.Fatalf("wrong-token artifact = %d, want 401", got.Code)
	}
	for _, path := range []string{"run_1/shot.png", "run_1/internal.png"} {
		got := bridgeRequest(t, handler, "GET", "/artifacts/"+path, "Bearer "+testBridgeToken, "")
		if got.Code != 200 || got.Body.String() != "screenshot bytes" {
			t.Errorf("canonical artifact %s = %d %q", path, got.Code, got.Body.String())
		}
	}
	for _, path := range []string{
		"", "run_1/", "run_1/unlisted.png", "run_1/only-result.png", "run_1/directory",
		"run_1/missing.png", "run_1/pipe.png", "run_1/escape.png", "run_1/cross-run.png",
		"run_1/escape-dir/other/foreign.png", "other/foreign.png",
		"run_1/../other/foreign.png", "run_1/%2e%2e/other/foreign.png",
	} {
		got := bridgeRequest(t, handler, "GET", "/artifacts/"+path, "Bearer "+testBridgeToken, "")
		if got.Code < 400 || strings.Contains(got.Body.String(), "screenshot bytes") || strings.Contains(got.Body.String(), "private outside") {
			t.Errorf("unsafe artifact %s = %d", path, got.Code)
		}
	}
	repository.run.ProjectID = "other"
	got := bridgeRequest(t, handler, "GET", "/artifacts/run_1/shot.png", "Bearer "+testBridgeToken, "")
	if got.Code != 403 {
		t.Errorf("foreign project artifact = %d, want 403", got.Code)
	}
	repository.run.ProjectID, repository.run.ConversationID = "project_1", "other"
	got = bridgeRequest(t, handler, "GET", "/artifacts/run_1/shot.png", "Bearer "+testBridgeToken, "")
	if got.Code != 403 {
		t.Errorf("foreign conversation artifact = %d, want 403", got.Code)
	}
}

func TestBridgeArtifactsRejectSymlinkRunNamespace(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "other"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "other", "shot.png"), []byte("foreign"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("other", filepath.Join(dir, "run_1")); err != nil {
		t.Fatal(err)
	}
	repository := ownedBridgeRepository()
	repository.trace.Operations = []postgres.TraceOperationDetail{{Artifacts: json.RawMessage(`{"screenshot_paths":["run_1/shot.png"]}`)}}
	if got := bridgeRequest(t, protectedBridge(repository, dir), "GET", "/artifacts/run_1/shot.png", "Bearer "+testBridgeToken, ""); got.Code < 400 {
		t.Fatalf("symlink namespace = %d, want rejection", got.Code)
	}
}

type bridgeRepository struct {
	fakeRepository
	traceReads int
	lookupErr  error
}

func (r *bridgeRepository) GetRun(ctx context.Context, id string) (domain.Run, error) {
	if r.lookupErr != nil {
		return domain.Run{}, r.lookupErr
	}
	return r.fakeRepository.GetRun(ctx, id)
}

func (r *bridgeRepository) GetConversation(ctx context.Context, id string) (postgres.Conversation, []domain.Message, []domain.Run, error) {
	if r.lookupErr != nil {
		return postgres.Conversation{}, nil, nil, r.lookupErr
	}
	return r.fakeRepository.GetConversation(ctx, id)
}

func (r *bridgeRepository) GetTraceDetails(ctx context.Context, id string) (postgres.TraceDetails, error) {
	r.traceReads++
	return r.fakeRepository.GetTraceDetails(ctx, id)
}
