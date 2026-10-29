package v3

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/browserprovider"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/event"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/postgres"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/testpostgres"
	"github.com/jackc/pgx/v5/pgxpool"
)

type controlGatewayFixture struct {
	ctx      context.Context
	pool     *pgxpool.Pool
	store    *postgres.Store
	env      postgres.Environment
	handler  http.Handler
	mu       sync.Mutex
	calls    []string
	fail     string
	block    string
	entered  chan struct{}
	resume   chan struct{}
	provider *browserprovider.Client
}

func controlGateway(t *testing.T) *controlGatewayFixture {
	return controlGatewayWithProvider(t, "")
}

func controlGatewayWithProvider(t *testing.T, providerAddress string) *controlGatewayFixture {
	t.Helper()
	dsn := os.Getenv("AGENTTRACE_CONTROL_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("AGENTTRACE_CONTROL_TEST_POSTGRES_DSN is not configured")
	}
	name, err := testpostgres.DatabaseName(dsn)
	if err != nil || !strings.HasPrefix(name, "agenttrace_test_") {
		t.Fatalf("control tests require isolated agenttrace_test_* database: %v", err)
	}
	for _, key := range []string{"AGENTTRACE_TEST_POSTGRES_DSN", "AGENTTRACE_MANAGEMENT_TEST_POSTGRES_DSN", "AGENTTRACE_REGISTRY_TEST_POSTGRES_DSN", "AGENTTRACE_LEASE_TEST_POSTGRES_DSN"} {
		if other := os.Getenv(key); other != "" {
			otherName, _ := testpostgres.DatabaseName(other)
			if otherName == name {
				t.Fatalf("control database aliases %s", key)
			}
		}
	}
	f := &controlGatewayFixture{ctx: context.Background(), entered: make(chan struct{}, 1), resume: make(chan struct{})}
	f.pool, err = pgxpool.New(f.ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(f.pool.Close)
	if _, err := f.pool.Exec(f.ctx, `DROP SCHEMA public CASCADE; CREATE SCHEMA public`); err != nil {
		t.Fatal(err)
	}
	if err := postgres.Migrate(f.ctx, f.pool); err != nil {
		t.Fatal(err)
	}
	f.store = postgres.NewStore(f.pool)

	if err := f.store.SetEnvironmentDefaultPolicy([]string{"127.0.0.1"}); err != nil {
		t.Fatal(err)
	}
	prj, err := f.store.CreateProject(f.ctx, postgres.CreateProjectParams{Name: "Control", LangfuseProjectID: "lf_control"})
	if err != nil {
		t.Fatal(err)
	}
	conversation, err := f.store.CreateConversation(f.ctx, postgres.CreateConversationParams{ProjectID: prj.ID, CreatedBy: "actor"})
	if err != nil {
		t.Fatal(err)
	}
	f.env, err = f.store.GetConversationEnvironment(f.ctx, conversation.ID)
	if err != nil {
		t.Fatal(err)
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer provider-private-token" {
			t.Error("provider request lost internal authentication")
			w.WriteHeader(401)
			return
		}
		f.mu.Lock()
		f.calls = append(f.calls, r.Method+" "+r.URL.Path)
		fail, block := f.fail, f.block
		f.mu.Unlock()
		var transactions int
		if err := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND state='idle in transaction'`).Scan(&transactions); err != nil || transactions != 0 {
			t.Errorf("provider invoked with open transaction: %d err=%v", transactions, err)
		}
		if strings.Contains(r.URL.Path, block) && block != "" {
			f.entered <- struct{}{}
			<-f.resume
		}
		if strings.Contains(r.URL.Path, fail) && fail != "" {
			w.WriteHeader(503)
			_, _ = w.Write([]byte("PRIVATE_PROVIDER_ERROR"))
			return
		}
		var body map[string]any
		if r.Method == "POST" {
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("provider invalid JSON: %v", err)
			}
		}
		switch {
		case strings.HasSuffix(r.URL.Path, "/provision"), strings.HasSuffix(r.URL.Path, "/reset"):
			writeJSON(w, 200, map[string]any{"environment_id": f.env.ID,
				"profile_generation": body["profile_generation"], "fencing_token": body["fencing_token"], "state": "ready"})
		case r.URL.Path == "/sessions" && r.Method == "POST":
			var state string
			if err := f.pool.QueryRow(f.ctx, `SELECT state FROM control_sessions WHERE id=$1`, body["session_id"]).Scan(&state); err != nil || state != "opening" && state != "needs_review" {
				t.Errorf("Open before committed consume: state=%s err=%v", state, err)
			}
			if body["environment_id"] != f.env.ID || body["profile_generation"] != float64(f.env.ProfileGeneration) {
				t.Errorf("Open identity=%v", body)
			}
			writeJSON(w, 200, map[string]string{"session_id": "bsess_PRIVATE"})
		case strings.HasSuffix(r.URL.Path, "/screenshot"):
			if r.Header.Get("X-Profile-Generation") == "" || r.Header.Get("X-Fencing-Token") == "" {
				t.Error("screenshot missing fences")
			}
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write([]byte("\x89PNG\r\n\x1a\nscreenshot"))
		case strings.HasSuffix(r.URL.Path, "/input"):
			if body["profile_generation"] == nil || body["fencing_token"] == nil {
				t.Error("input missing canonical fences")
			}
			writeJSON(w, 200, map[string]bool{"ok": true})
		case strings.HasPrefix(r.URL.Path, "/sessions/") && r.Method == "DELETE":
			var state string
			if err := f.pool.QueryRow(f.ctx, `SELECT state FROM environments WHERE id=$1`, f.env.ID).Scan(&state); err != nil || state != "leased" && state != "needs_review" {
				t.Errorf("Close with prematurely ready environment=%s err=%v", state, err)
			}
			writeJSON(w, 200, map[string]bool{"persisted": true})
		default:
			t.Errorf("unexpected provider path=%s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(upstream.Close)
	if providerAddress == "" {
		providerAddress = upstream.URL
	}
	f.provider, err = browserprovider.New(providerAddress, "provider-private-token")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.RegisterWorker(f.ctx, postgres.WorkerRegistration{ID: f.provider.WorkerID(), Role: "browser",
		Endpoint: providerAddress, Capacity: 1}); err != nil {
		t.Fatal(err)
	}
	if err := f.store.BindEnvironmentWorker(f.ctx, f.env.ID, f.provider.WorkerID()); err != nil {
		t.Fatal(err)
	}
	var raw []byte
	if err := f.pool.QueryRow(f.ctx, `SELECT payload FROM outbox_events WHERE key=$1`, f.env.ID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var command event.Envelope[postgres.EnvironmentCommand]
	if err := json.Unmarshal(raw, &command); err != nil {
		t.Fatal(err)
	}
	if err := f.store.ProcessEnvironmentCommand(f.ctx, command, f.provider); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(f.ctx, `UPDATE outbox_events SET published_at=now() WHERE event_id=$1`, command.EventID); err != nil {
		t.Fatal(err)
	}
	f.env, _ = f.store.GetEnvironment(f.ctx, f.env.ID)
	server := &Server{repository: f.store, browserProvider: f.provider}
	mux := http.NewServeMux()
	server.registerControlSessionRoutes(mux)
	server.registerEnvironmentRoutes(mux)
	f.handler = server.bridgeGuard(mux, ServerConfig{BridgeToken: testBridgeToken})
	return f
}

func (f *controlGatewayFixture) request(method, path, body, actor string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+testBridgeToken)
	req.Header.Set("X-Agenttrace-Project-Id", f.env.ProjectID)
	req.Header.Set("X-Langfuse-Project-Id", "lf_control")
	req.Header.Set("X-Agenttrace-Role", "member")
	req.Header.Set("X-Agenttrace-Actor-Id", actor)
	recorder := httptest.NewRecorder()
	f.handler.ServeHTTP(recorder, req)
	return recorder
}

func (f *controlGatewayFixture) create(t *testing.T) (postgres.ControlSession, string) {
	t.Helper()
	response := f.request("POST", "/api/v1/environments/"+f.env.ID+"/control-session", "{}", "actor")
	if response.Code != 201 {
		t.Fatalf("create=%d body=%s", response.Code, response.Body)
	}
	var result struct {
		postgres.ControlSession
		Token string `json:"token"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.ID == "" || result.Token == "" || result.State != "pending" {
		t.Fatalf("create=%+v", result)
	}
	return result.ControlSession, result.Token
}

func TestControlGatewayFullLifecycleRealPostgresAndProviderHTTP(t *testing.T) {
	f := controlGateway(t)
	c, token := f.create(t)
	if got := f.request("POST", c.ControlURL+"/consume", `{"token":"wrong"}`, "actor"); got.Code != 409 {
		t.Fatalf("wrong token=%d", got.Code)
	}
	if got := f.request("POST", c.ControlURL+"/consume", `{"token":"`+token+`"}`, "other"); got.Code != 409 {
		t.Fatalf("foreign actor=%d", got.Code)
	}
	body := `{"token":"` + token + `"}`
	got := f.request("POST", c.ControlURL+"/consume", body, "actor")
	if got.Code != 200 || !strings.Contains(got.Body.String(), `"state":"leased"`) || strings.Contains(got.Body.String(), "PRIVATE") {
		t.Fatalf("consume=%d body=%s", got.Code, got.Body)
	}
	if got := f.request("POST", c.ControlURL+"/consume", body, "actor"); got.Code != 409 {
		t.Fatalf("replay=%d", got.Code)
	}
	got = f.request("GET", c.ControlURL+"/screenshot", "", "actor")
	if got.Code != 200 || got.Body.String() != "\x89PNG\r\n\x1a\nscreenshot" || got.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("screenshot=%d body=%s", got.Code, got.Body)
	}
	for _, action := range []string{
		`{"action":"click","x":23,"y":40}`, `{"action":"key","key":"Tab"}`,
		`{"action":"type","text":"user@example.test"}`, `{"action":"navigate","url":"https://example.test/login"}`,
	} {
		if got := f.request("POST", c.ControlURL+"/input", action, "actor"); got.Code != 200 {
			t.Fatalf("input=%d body=%s", got.Code, got.Body)
		}
	}
	if got := f.request("POST", "/api/v1/environments/"+f.env.ID+"/reset", "{}", "actor"); got.Code != 409 {
		t.Fatalf("live reset=%d", got.Code)
	}
	if got := f.request("DELETE", c.ControlURL, "", "actor"); got.Code != 200 || !strings.Contains(got.Body.String(), `"state":"closed"`) {
		t.Fatalf("stop=%d body=%s", got.Code, got.Body)
	}
	env, _ := f.store.GetEnvironment(f.ctx, f.env.ID)
	if env.State != "ready" {
		t.Fatalf("stop not released=%+v", env)
	}
	if got := f.request("GET", c.ControlURL+"/screenshot", "", "actor"); got.Code != 409 {
		t.Fatalf("old gateway=%d", got.Code)
	}
	next, _ := f.create(t)
	stored, err := f.store.GetControlSession(f.ctx, next.ID, f.env.ProjectID, "actor")
	if err != nil || stored.FencingToken != env.FencingToken+1 {
		t.Fatalf("new takeover fence=%+v err=%v", stored, err)
	}
}

func TestControlGatewayRejectsArbitraryJSIdentityOverridesAndActor(t *testing.T) {
	f := controlGateway(t)
	c, token := f.create(t)
	if got := f.request("POST", c.ControlURL+"/consume", `{"token":"`+token+`"}`, "actor"); got.Code != 200 {
		t.Fatalf("consume=%d body=%s", got.Code, got.Body)
	}
	for _, action := range []string{
		`{"action":"evaluate","javascript":"fetch('/delete')"}`, `{"action":"click","x":1,"y":2,"fencing_token":99}`,
		`{"action":"click","x":1}`, `{"action":"click","x":-1,"y":2}`, `{"action":"click","x":1.5,"y":2}`,
		`{"action":"type","text":"ok","key":"Enter"}`, `{"action":"key","key":"Control+L"}`,
		`{"action":"navigate","url":"javascript:alert(1)"}`, `{"action":"navigate","url":"file:///etc/passwd"}`,
		`{"action":"navigate","url":"https://user:pass@example.test"}`, `{"action":"type","text":"bad\ntext"}`,
	} {
		if got := f.request("POST", c.ControlURL+"/input", action, "actor"); got.Code != 400 {
			t.Fatalf("unsafe action accepted=%d action=%s body=%s", got.Code, action, got.Body)
		}
	}
	for _, route := range []struct{ method, path, body string }{
		{"GET", c.ControlURL, ""}, {"GET", c.ControlURL + "/screenshot", ""},
		{"POST", c.ControlURL + "/input", `{"action":"key","key":"Enter"}`}, {"DELETE", c.ControlURL, ""},
	} {
		if got := f.request(route.method, route.path, route.body, "other"); got.Code != 409 {
			t.Fatalf("actor gateway=%d path=%s", got.Code, route.path)
		}
	}
	unauth := httptest.NewRecorder()
	f.handler.ServeHTTP(unauth, httptest.NewRequest("GET", c.ControlURL+"/screenshot", nil))
	if unauth.Code != 401 {
		t.Fatalf("unauth=%d", unauth.Code)
	}
}

func TestControlGatewayProviderFailuresRemainNeedsReviewAndNeverLeak(t *testing.T) {
	for _, failure := range []string{"open", "screenshot", "input", "close"} {
		t.Run(failure, func(t *testing.T) {
			f := controlGateway(t)
			c, token := f.create(t)
			if failure == "open" {
				f.fail = "/sessions"
			}
			consume := f.request("POST", c.ControlURL+"/consume", `{"token":"`+token+`"}`, "actor")
			var got *httptest.ResponseRecorder
			switch failure {
			case "open":
				got = consume
			case "screenshot":
				f.fail = "/screenshot"
				got = f.request("GET", c.ControlURL+"/screenshot", "", "actor")
			case "input":
				f.fail = "/input"
				got = f.request("POST", c.ControlURL+"/input", `{"action":"key","key":"Enter"}`, "actor")
			case "close":
				f.fail = "/sessions"
				got = f.request("DELETE", c.ControlURL, "", "actor")
			}
			if got.Code != 502 || strings.Contains(got.Body.String(), "PRIVATE") || strings.Contains(got.Body.String(), "bsess_") {
				t.Fatalf("failure response=%d body=%s", got.Code, got.Body)
			}
			env, _ := f.store.GetEnvironment(f.ctx, f.env.ID)
			if env.State != "needs_review" {
				t.Fatalf("unsafe failure environment=%+v", env)
			}
			if replay := f.request("POST", c.ControlURL+"/consume", `{"token":"`+token+`"}`, "actor"); replay.Code != 409 {
				t.Fatalf("failed provider replayed=%d", replay.Code)
			}
		})
	}
}

func TestControlGatewayInflightInputExcludesStopAndReset(t *testing.T) {
	f := controlGateway(t)
	c, token := f.create(t)
	if got := f.request("POST", c.ControlURL+"/consume", `{"token":"`+token+`"}`, "actor"); got.Code != 200 {
		t.Fatal(got.Body)
	}
	f.block = "/input"
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() { done <- f.request("POST", c.ControlURL+"/input", `{"action":"key","key":"Enter"}`, "actor") }()
	<-f.entered
	if got := f.request("DELETE", c.ControlURL, "", "actor"); got.Code != 409 {
		t.Errorf("inflight stop=%d", got.Code)
	}
	if got := f.request("POST", "/api/v1/environments/"+f.env.ID+"/reset", "{}", "actor"); got.Code != 409 {
		t.Errorf("inflight reset=%d", got.Code)
	}
	close(f.resume)
	if got := <-done; got.Code != 200 {
		t.Fatalf("input=%d body=%s", got.Code, got.Body)
	}
}

func TestControlGatewayServerConstructorRegistersManagementOnly(t *testing.T) {
	f := controlGateway(t)
	f.handler = New(ServerConfig{Repository: f.store,
		BridgeToken: testBridgeToken, BrowserProvider: f.provider}).Handler()
	f.create(t)
	f.handler = New(ServerConfig{Repository: f.store, BrowserProvider: f.provider}).Handler()
	if got := f.request("GET", "/api/v1/control-sessions/unknown", "", "actor"); got.Code != 503 {
		t.Fatalf("unconfigured bridge must fail closed, gateway=%d", got.Code)
	}
}

func TestControlGatewayExpiryRecoverAndPendingStopWithoutOpeningProvider(t *testing.T) {
	f := controlGateway(t)
	c, token := f.create(t)
	before := len(f.calls)
	if got := f.request("GET", c.ControlURL+"/screenshot", "", "actor"); got.Code != 409 {
		t.Fatalf("pending screenshot=%d", got.Code)
	}
	if got := f.request("DELETE", c.ControlURL, "", "actor"); got.Code != 200 {
		t.Fatalf("pending stop=%d body=%s", got.Code, got.Body)
	}
	if len(f.calls) != before {
		t.Fatal("pending stop invoked the browser provider")
	}
	if got := f.request("POST", c.ControlURL+"/consume", `{"token":"`+token+`"}`, "actor"); got.Code != 409 {
		t.Fatalf("cancelled token replay=%d", got.Code)
	}
	c, token = f.create(t)
	if got := f.request("POST", c.ControlURL+"/consume", `{"token":"`+token+`"}`, "actor"); got.Code != 200 {
		t.Fatalf("consume=%d body=%s", got.Code, got.Body)
	}
	if _, err := f.pool.Exec(f.ctx, `UPDATE control_sessions SET expires_at=now()-interval '1 second' WHERE id=$1`, c.ID); err != nil {
		t.Fatal(err)
	}
	if got := f.request("GET", c.ControlURL+"/screenshot", "", "actor"); got.Code != 409 {
		t.Fatalf("expired screenshot=%d", got.Code)
	}
	env, _ := f.store.GetEnvironment(f.ctx, f.env.ID)
	if env.State != "needs_review" {
		t.Fatalf("expired gateway left environment=%s", env.State)
	}
	if got := f.request("DELETE", c.ControlURL, "", "actor"); got.Code != 200 {
		t.Fatalf("expired stop=%d body=%s", got.Code, got.Body)
	}
	env, _ = f.store.GetEnvironment(f.ctx, f.env.ID)
	if env.State != "needs_review" {
		t.Fatalf("acknowledged stop bypassed required review=%s", env.State)
	}
	if got := f.request("POST", "/api/v1/environments/"+env.ID+"/review", "{}", "actor"); got.Code != 200 {
		t.Fatalf("explicit review after stop=%d body=%s", got.Code, got.Body)
	}
}

func TestControlGatewayCancellationDuringOpenFailsClosed(t *testing.T) {
	f := controlGateway(t)
	c, token := f.create(t)
	f.block = "/sessions"
	ctx, cancel := context.WithCancel(f.ctx)
	req := httptest.NewRequest("POST", c.ControlURL+"/consume", strings.NewReader(`{"token":"`+token+`"}`)).WithContext(ctx)
	req.Header.Set("Authorization", "Bearer "+testBridgeToken)
	req.Header.Set("X-Agenttrace-Project-Id", f.env.ProjectID)
	req.Header.Set("X-Langfuse-Project-Id", "lf_control")
	req.Header.Set("X-Agenttrace-Role", "member")
	req.Header.Set("X-Agenttrace-Actor-Id", "actor")
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		got := httptest.NewRecorder()
		f.handler.ServeHTTP(got, req)
		done <- got
	}()
	<-f.entered
	cancel()
	got := <-done
	close(f.resume)
	if got.Code != 502 {
		t.Fatalf("cancelled Open=%d body=%s", got.Code, got.Body)
	}
	env, _ := f.store.GetEnvironment(f.ctx, f.env.ID)
	if env.State != "needs_review" {
		t.Fatalf("cancelled Open prematurely ready=%s", env.State)
	}
	if got := f.request("POST", c.ControlURL+"/consume", `{"token":"`+token+`"}`, "actor"); got.Code != 409 {
		t.Fatalf("cancelled Open replayed=%d", got.Code)
	}
}

func TestControlGatewayRealProjectBindingOwnershipAndPrivacy(t *testing.T) {
	f := controlGateway(t)
	c, token := f.create(t)
	other, err := f.store.CreateProject(f.ctx, postgres.CreateProjectParams{Name: "Other", LangfuseProjectID: "lf_other"})
	if err != nil {
		t.Fatal(err)
	}
	for _, route := range []struct{ method, path, body string }{
		{"POST", "/api/v1/environments/" + f.env.ID + "/control-session", "{}"},
		{"POST", c.ControlURL + "/consume", `{"token":"` + token + `"}`},
		{"GET", c.ControlURL + "/screenshot", ""}, {"DELETE", c.ControlURL, ""},
	} {
		req := httptest.NewRequest(route.method, route.path, strings.NewReader(route.body))
		req.Header.Set("Authorization", "Bearer "+testBridgeToken)
		req.Header.Set("X-Agenttrace-Project-Id", other.ID)
		req.Header.Set("X-Langfuse-Project-Id", "lf_other")
		req.Header.Set("X-Agenttrace-Role", "member")
		req.Header.Set("X-Agenttrace-Actor-Id", "actor")
		got := httptest.NewRecorder()
		f.handler.ServeHTTP(got, req)
		if got.Code != 403 {
			t.Fatalf("cross-project route=%s status=%d body=%s", route.path, got.Code, got.Body)
		}
	}
	got := f.request("GET", c.ControlURL, "", "actor")
	if got.Code != 200 || strings.Contains(got.Body.String(), token) ||
		strings.Contains(got.Body.String(), "token_hash") || strings.Contains(got.Body.String(), "provider") ||
		got.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("session privacy=%d body=%s headers=%v", got.Code, got.Body, got.Header())
	}
}
