package v3

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image/png"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/browserprovider"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/event"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/postgres"
)

// Opt-in real Chromium test. It uses only the dedicated control-test database
// and temporary provider/profile/upload directories, not the deployed worker.
func TestControlGatewayRealChromiumProfilePersistenceAndReset(t *testing.T) {
	if os.Getenv("AGENTTRACE_CONTROL_REAL_BROWSER_TEST") != "1" {
		t.Skip("AGENTTRACE_CONTROL_REAL_BROWSER_TEST=1 enables real Chromium")
	}
	workerDir, err := filepath.Abs("../../../../worker")
	if err != nil {
		t.Fatal(err)
	}
	executable := filepath.Join(workerDir, ".venv", "bin", "uvicorn")
	if _, err := os.Stat(executable); err != nil {
		t.Fatalf("real worker environment unavailable: %v", err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(executable, "loop_worker.main:app", "--host", "127.0.0.1", "--port", strconv.Itoa(port), "--log-level", "warning")
	cmd.Dir = workerDir
	cmd.Env = append(os.Environ(),
		"LOOP_WORKER_INTERNAL_TOKEN=provider-private-token",
		"LOOP_ENVIRONMENTS_DIR="+filepath.Join(root, "profiles"),
		"LOOP_UPLOADS_DIR="+filepath.Join(root, "uploads"),
		"LOOP_ARTIFACTS_DIR="+filepath.Join(root, "artifacts"),
		"LOOP_ENVIRONMENT_ENCRYPTION_KEY="+base64.URLEncoding.EncodeToString(key))
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Signal(os.Interrupt)
		done := make(chan error, 1)
		go func() { done <- cmd.Wait() }()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			_ = cmd.Process.Kill()
			<-done
		}
		if t.Failed() {
			t.Logf("worker output: %s", output.String())
		}
	})
	address := fmt.Sprintf("http://127.0.0.1:%d", port)
	client := &http.Client{Timeout: time.Second}
	deadline := time.Now().Add(20 * time.Second)
	for {
		response, err := client.Get(address + "/health")
		if err == nil {
			_ = response.Body.Close()
			if response.StatusCode == 200 {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("real worker did not start")
		}
		time.Sleep(50 * time.Millisecond)
	}
	f := controlGatewayWithProvider(t, address)
	f.provider, err = browserprovider.New(address, "provider-private-token")
	if err != nil {
		t.Fatal(err)
	}
	f.env.Policy = json.RawMessage(`{"allowed_domains":["127.0.0.1"]}`)
	if _, err := f.pool.Exec(f.ctx, `UPDATE environments SET policy=$2::jsonb WHERE id=$1`, f.env.ID, f.env.Policy); err != nil {
		t.Fatal(err)
	}
	if _, err := f.provider.ApplyEnvironment(f.ctx, postgres.EnvironmentCommand{ProjectID: f.env.ProjectID, EnvironmentID: f.env.ID,
		Kind: "provision", ProfileGeneration: f.env.ProfileGeneration, FencingToken: f.env.FencingToken, Policy: f.env.Policy}); err != nil {
		t.Fatal(err)
	}
	server := &Server{repository: f.store, browserProvider: f.provider}
	mux := http.NewServeMux()
	server.registerControlSessionRoutes(mux)
	server.registerEnvironmentRoutes(mux)
	f.handler = server.bridgeGuard(mux, ServerConfig{BridgeToken: testBridgeToken})
	checks := make(chan string, 10)
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Logf("real browser site request: %s %s", r.Method, r.URL.Path)
		switch r.URL.Path {
		case "/login":
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte(`<html><body><form action="/save"><input autofocus name="identity" style="position:absolute;left:10px;top:10px;width:200px;height:30px"><button style="position:absolute;left:10px;top:60px;width:100px;height:30px">Login</button></form></body></html>`))
		case "/save":
			identity := r.URL.Query().Get("identity")
			http.SetCookie(w, &http.Cookie{Name: "control_identity", Value: identity, Path: "/", MaxAge: 3600, HttpOnly: true})
			checks <- identity
			_, _ = w.Write([]byte("<html><body>Login saved</body></html>"))
		case "/check":
			cookie, _ := r.Cookie("control_identity")
			identity := ""
			if cookie != nil {
				identity = cookie.Value
			}
			checks <- identity
			_, _ = fmt.Fprintf(w, "<html><body>Identity: %s</body></html>", identity)
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(site.Close)
	consume := func(c postgres.ControlSession, token string) {
		t.Helper()
		if got := f.request("POST", c.ControlURL+"/consume", `{"token":"`+token+`"}`, "actor"); got.Code != 200 {
			t.Fatalf("real consume=%d body=%s", got.Code, got.Body)
		}
	}
	input := func(c postgres.ControlSession, body string) {
		t.Helper()
		if got := f.request("POST", c.ControlURL+"/input", body, "actor"); got.Code != 200 {
			t.Fatalf("real input=%d body=%s", got.Code, got.Body)
		}
	}
	check := func(want string) {
		t.Helper()
		select {
		case got := <-checks:
			if got != want {
				t.Fatalf("browser identity=%q want=%q", got, want)
			}
		case <-time.After(10 * time.Second):
			t.Fatal("browser did not reach identity page")
		}
	}
	first, token := f.create(t)
	consume(first, token)
	input(first, `{"action":"navigate","url":"`+site.URL+`/login"}`)
	input(first, `{"action":"click","x":80,"y":25}`)
	input(first, `{"action":"type","text":"persisted-control-user"}`)
	input(first, `{"action":"key","key":"Enter"}`)
	check("persisted-control-user")
	image := f.request("GET", first.ControlURL+"/screenshot", "", "actor")
	if image.Code != 200 {
		t.Fatalf("real screenshot=%d body=%s", image.Code, image.Body)
	}
	dimensions, err := png.DecodeConfig(bytes.NewReader(image.Body.Bytes()))
	if err != nil || dimensions.Width != 1280 || dimensions.Height != 800 {
		t.Fatalf("real screenshot dimensions=%+v err=%v", dimensions, err)
	}
	if got := f.request("DELETE", first.ControlURL, "", "actor"); got.Code != 200 {
		t.Fatalf("real close=%d body=%s", got.Code, got.Body)
	}
	entries, err := os.ReadDir(filepath.Join(root, "profiles"))
	if err != nil {
		t.Fatal(err)
	}
	encrypted := false
	for _, entry := range entries {
		if !entry.Type().IsRegular() || strings.HasSuffix(entry.Name(), ".lock") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(root, "profiles", entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(raw, []byte("persisted-control-user")) || bytes.Contains(raw, []byte("control_identity")) {
			t.Fatal("provider persisted plaintext browser identity")
		}
		encrypted = encrypted || len(raw) > 100
	}
	if !encrypted {
		t.Fatal("provider did not persist encrypted browser profile")
	}
	second, token := f.create(t)
	consume(second, token)
	input(second, `{"action":"navigate","url":"`+site.URL+`/check"}`)
	check("persisted-control-user")
	stale, err := f.store.GetControlSession(f.ctx, second.ID, f.env.ProjectID, "actor")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(f.ctx, `UPDATE control_sessions SET expires_at=now()-interval '1 second' WHERE id=$1`, second.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.RecoverExpiredControlSessions(f.ctx); err != nil {
		t.Fatal(err)
	}
	if got := f.request("POST", "/api/v1/environments/"+f.env.ID+"/reset", "{}", "actor"); got.Code != 202 {
		t.Fatalf("real reset=%d body=%s", got.Code, got.Body)
	}
	var raw []byte
	if err := f.pool.QueryRow(f.ctx, `SELECT payload FROM outbox_events WHERE key=$1 AND payload->>'event_type'='environment.reset_requested' ORDER BY created_at DESC LIMIT 1`, f.env.ID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var reset event.Envelope[postgres.EnvironmentCommand]
	if err := json.Unmarshal(raw, &reset); err != nil {
		t.Fatal(err)
	}
	if err := f.store.ProcessEnvironmentCommand(f.ctx, reset, f.provider); err != nil {
		t.Fatalf("real reset provider=%v generation=%d fence=%d", err, reset.Payload.ProfileGeneration, reset.Payload.FencingToken)
	}
	if _, err := f.pool.Exec(f.ctx, `UPDATE outbox_events SET published_at=now() WHERE event_id=$1`, reset.EventID); err != nil {
		t.Fatal(err)
	}
	f.env, _ = f.store.GetEnvironment(f.ctx, f.env.ID)
	if err := f.provider.Input(f.ctx, stale.EnvironmentID, stale.ProviderSessionID, stale.ProfileGeneration, stale.FencingToken,
		map[string]any{"action": "type", "text": "stale-write"}, stale.ProjectID); err == nil {
		t.Fatal("old provider generation accepted an input after reset")
	}
	if got := f.request("GET", second.ControlURL+"/screenshot", "", "actor"); got.Code != 409 {
		t.Fatalf("old gateway after reset=%d", got.Code)
	}
	third, token := f.create(t)
	consume(third, token)
	input(third, `{"action":"navigate","url":"`+site.URL+`/check"}`)
	check("")
	if got := f.request("DELETE", third.ControlURL, "", "actor"); got.Code != 200 {
		t.Fatalf("final real close=%d body=%s", got.Code, got.Body)
	}
}
