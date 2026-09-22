package api

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Asukadaisiki/AI_Web_Testing/v2/backend/internal/agentruntime"
	"github.com/Asukadaisiki/AI_Web_Testing/v2/backend/internal/contract"
	"github.com/Asukadaisiki/AI_Web_Testing/v2/backend/internal/store"
	"github.com/Asukadaisiki/AI_Web_Testing/v2/backend/internal/worker"
)

const (
	testListURL = "https://shop.test/products"
	testNextURL = "https://shop.test/products/next"
)

// fakeExecutor 是控制面测试用的最小执行器：一页、一个链接、一步到位。
type fakeExecutor struct {
	server       *httptest.Server
	artifactsDir string
}

func newFakeExecutor(artifactsDir string) *fakeExecutor {
	fake := &fakeExecutor{artifactsDir: artifactsDir}
	mux := http.NewServeMux()
	write := func(w http.ResponseWriter, status int, payload any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(payload)
	}
	observe := func(url string) contract.Observation {
		return contract.Observation{
			ObservationID: "obs_1",
			PageStateID:   "ps_1",
			URL:           url,
			Title:         "Products",
			Elements: []contract.Element{{
				Ref: "e1", Tag: "a", Role: "link", Name: "Widget", Text: "Widget",
				Visible: true, Enabled: true,
				Locators: []contract.Locator{{
					Kind: "role", Role: "link", Name: "Widget", Exact: true, MatchCount: 1,
				}},
			}},
		}
	}
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		write(w, http.StatusOK, map[string]string{
			"status":        "ok",
			"artifacts_dir": fake.artifactsDir,
		})
	})
	mux.HandleFunc("POST /sessions", func(w http.ResponseWriter, r *http.Request) {
		write(w, http.StatusOK, contract.Session{SessionID: "sess_1"})
	})
	mux.HandleFunc("DELETE /sessions/{id}", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST /sessions/{id}/navigate", func(w http.ResponseWriter, r *http.Request) {
		var body contract.NavigateRequest
		_ = json.NewDecoder(r.Body).Decode(&body)
		write(w, http.StatusOK, observe(body.URL))
	})
	mux.HandleFunc("POST /sessions/{id}/act", func(w http.ResponseWriter, r *http.Request) {
		write(w, http.StatusOK, observe(testNextURL))
	})
	mux.HandleFunc("POST /execute", func(w http.ResponseWriter, r *http.Request) {
		now := time.Now().UTC()
		write(w, http.StatusOK, contract.ExecutionResult{
			ExecutionID: "exec_1",
			Status:      contract.ExecutionPassed,
			StartedAt:   now,
			FinishedAt:  now.Add(1200 * time.Millisecond),
			FinalURL:    testNextURL,
			Steps: []contract.StepResult{
				{Index: 0, Action: contract.ActionGoto, Status: "passed", StartedAt: now,
					URLBefore: "about:blank", URLAfter: testListURL,
					Evidence: contract.Evidence{ScreenshotPath: "exec_1_0.png"}},
				{Index: 1, Action: contract.ActionClick, Status: "passed", StartedAt: now,
					URLBefore: testListURL, URLAfter: testNextURL,
					Evidence: contract.Evidence{ScreenshotPath: "exec_1_1.png"}},
			},
		})
	})
	fake.server = httptest.NewServer(mux)
	return fake
}

func (f *fakeExecutor) Close() { f.server.Close() }

func testScript() []agentruntime.ScriptedStep {
	return []agentruntime.ScriptedStep{
		{Tool: "open_page", Arguments: json.RawMessage(
			`{"url":"` + testListURL + `","intent":"打开列表"}`)},
		{Tool: "click", Arguments: json.RawMessage(
			`{"hint":"Widget","intent":"打开详情","expect_url":"/next"}`)},
		{Tool: "finish_case", Arguments: json.RawMessage(`{"name":"打开详情"}`)},
	}
}

type testServer struct {
	server *Server
	http   *httptest.Server
	store  *store.Store
}

func newTestServer(t *testing.T) *testServer {
	t.Helper()
	// 控制面与执行器默认必须指向同一个证据目录，否则截图会静默 404。
	artifactsDir := t.TempDir()
	return newTestServerWith(t, newFakeExecutor(artifactsDir), artifactsDir)
}

func newTestServerWith(t *testing.T, executor *fakeExecutor, artifactsDir string) *testServer {
	t.Helper()
	t.Cleanup(executor.Close)

	database, err := store.Open(filepath.Join(t.TempDir(), "loop.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { database.Close() })

	runtime := agentruntime.New(agentruntime.Config{
		Store:         database,
		Worker:        worker.New(executor.server.URL),
		LLM:           agentruntime.NewScriptedLLM(testScript()),
		MaxModelCalls: 20,
		AnswerTimeout: 5 * time.Second,
	})
	server := New(database, runtime, worker.New(executor.server.URL), artifactsDir)
	t.Cleanup(server.Close)
	httpServer := httptest.NewServer(server.Handler())
	t.Cleanup(httpServer.Close)
	return &testServer{server: server, http: httpServer, store: database}
}

func (ts *testServer) do(t *testing.T, method, path string, body any) (int, []byte) {
	t.Helper()
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		reader = bytes.NewReader(raw)
	}
	request, err := http.NewRequest(method, ts.http.URL+path, reader)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := ts.http.Client().Do(request)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return response.StatusCode, raw
}

func decode[T any](t *testing.T, raw []byte) T {
	t.Helper()
	var parsed T
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("decode %s: %v", string(raw), err)
	}
	return parsed
}

func (ts *testServer) waitForStatus(t *testing.T, runID, want string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		status, raw := ts.do(t, http.MethodGet, "/api/runs/"+runID, nil)
		if status != http.StatusOK {
			t.Fatalf("get run: %d %s", status, raw)
		}
		run := decode[store.Run](t, raw)
		if run.Status == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("run %s stuck at %q, want %q", runID, run.Status, want)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestControlPlaneEndToEnd 走一遍 4 个页面用到的全部端点。
func TestControlPlaneEndToEnd(t *testing.T) {
	ts := newTestServer(t)

	if status, raw := ts.do(t, http.MethodGet, "/api/health", nil); status != http.StatusOK {
		t.Fatalf("health: %d %s", status, raw)
	}

	status, raw := ts.do(t, http.MethodPost, "/api/runs", map[string]string{"input": "打开第一个商品"})
	if status != http.StatusAccepted {
		t.Fatalf("create run: %d %s", status, raw)
	}
	created := decode[map[string]string](t, raw)
	runID := created["run_id"]
	if runID == "" {
		t.Fatal("create run must return run_id")
	}

	ts.waitForStatus(t, runID, store.StatusAwaitingApproval)

	// case 页：case_id 必须是字符串（前端 requireString）。
	status, raw = ts.do(t, http.MethodGet, "/api/runs/"+runID+"/case", nil)
	if status != http.StatusOK {
		t.Fatalf("get case: %d %s", status, raw)
	}
	caseResponse := decode[struct {
		CaseID string          `json:"case_id"`
		Case   json.RawMessage `json:"case"`
	}](t, raw)
	if caseResponse.CaseID == "" {
		t.Fatalf("case_id must be a non-empty string, got %q", caseResponse.CaseID)
	}
	if _, err := contract.Validate(caseResponse.Case); err != nil {
		t.Fatalf("the API must serve an executable case: %v", err)
	}

	// SSE：历史事件必须能重放。
	events := ts.readEvents(t, runID, 0)
	types := map[string]int{}
	for _, event := range events {
		types[event.Type]++
	}
	for _, want := range []string{agentruntime.EventRunStatus, agentruntime.EventToolCall, agentruntime.EventCaseReady} {
		if types[want] == 0 {
			t.Fatalf("missing %q in SSE replay: %v", want, types)
		}
	}

	// 审批 → 执行 → 报告。
	status, raw = ts.do(t, http.MethodPost, "/api/runs/"+runID+"/approve", map[string]any{})
	if status != http.StatusAccepted {
		t.Fatalf("approve: %d %s", status, raw)
	}
	ts.waitForStatus(t, runID, store.StatusCompleted)

	status, raw = ts.do(t, http.MethodGet, "/api/runs/"+runID+"/execution", nil)
	if status != http.StatusOK {
		t.Fatalf("get execution: %d %s", status, raw)
	}
	execution := decode[struct {
		ExecutionID string          `json:"execution_id"`
		Status      string          `json:"status"`
		Result      json.RawMessage `json:"result"`
	}](t, raw)
	if execution.ExecutionID != "exec_1" || execution.Status != "passed" || len(execution.Result) == 0 {
		t.Fatalf("execution response = %+v", execution)
	}

	status, raw = ts.do(t, http.MethodGet, "/api/runs/"+runID+"/report", nil)
	if status != http.StatusOK {
		t.Fatalf("get report: %d %s", status, raw)
	}
	reportValue := decode[struct {
		RunID       string `json:"run_id"`
		Status      string `json:"status"`
		StepsTotal  int    `json:"steps_total"`
		StepsPassed int    `json:"steps_passed"`
		StepsFailed int    `json:"steps_failed"`
		DurationMS  int64  `json:"duration_ms"`
		Signals     []any  `json:"signals"`
		ExecutionID string `json:"execution_id"`
	}](t, raw)
	if reportValue.RunID != runID || reportValue.StepsTotal != 2 || reportValue.StepsPassed != 2 {
		t.Fatalf("report = %+v", reportValue)
	}
	if reportValue.StepsFailed != 0 || len(reportValue.Signals) != 0 {
		t.Fatalf("a passing run must report no failures: %+v", reportValue)
	}
	if reportValue.DurationMS != 1200 {
		t.Fatalf("duration_ms = %d, want 1200", reportValue.DurationMS)
	}
	if reportValue.ExecutionID != "exec_1" {
		t.Fatalf("execution_id = %q", reportValue.ExecutionID)
	}

	status, raw = ts.do(t, http.MethodGet, "/api/runs/"+runID+"/feedback", nil)
	if status != http.StatusOK {
		t.Fatalf("get feedback: %d %s", status, raw)
	}
	feedbackValue := decode[struct {
		Candidates []any `json:"candidates"`
	}](t, raw)
	if len(feedbackValue.Candidates) != 0 {
		t.Fatalf("a passing run must not propose feedback: %+v", feedbackValue)
	}

	status, raw = ts.do(t, http.MethodGet, "/api/runs?limit=10", nil)
	if status != http.StatusOK {
		t.Fatalf("list runs: %d %s", status, raw)
	}
	list := decode[struct {
		Runs []store.Run `json:"runs"`
	}](t, raw)
	if len(list.Runs) != 1 || list.Runs[0].ID != runID {
		t.Fatalf("runs = %+v", list.Runs)
	}
}

type sseEvent struct {
	Seq     int64           `json:"seq"`
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

func (ts *testServer) readEvents(t *testing.T, runID string, from int64) []sseEvent {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(
		ctx, http.MethodGet,
		fmt.Sprintf("%s/api/runs/%s/events?from=%d", ts.http.URL, runID, from), nil,
	)
	if err != nil {
		t.Fatalf("build sse request: %v", err)
	}
	response, err := ts.http.Client().Do(request)
	if err != nil {
		t.Fatalf("sse connect: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("sse status = %d", response.StatusCode)
	}
	if contentType := response.Header.Get("Content-Type"); !strings.HasPrefix(contentType, "text/event-stream") {
		t.Fatalf("sse content-type = %q", contentType)
	}
	events := make([]sseEvent, 0, 8)
	scanner := bufio.NewScanner(response.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var event sseEvent
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &event); err != nil {
			t.Fatalf("decode sse event: %v (%s)", err, line)
		}
		events = append(events, event)
		if event.Type == agentruntime.EventCaseReady {
			return events
		}
	}
	return events
}

// TestApiRejectsBadRequests 确认错误路径也是结构化的，前端能直接显示。
func TestApiRejectsBadRequests(t *testing.T) {
	ts := newTestServer(t)

	if status, raw := ts.do(t, http.MethodPost, "/api/runs", map[string]string{"input": "  "}); status != http.StatusBadRequest {
		t.Fatalf("empty input must be rejected: %d %s", status, raw)
	} else if body := decode[map[string]string](t, raw); body["error"] != "input_required" {
		t.Fatalf("error code = %q", body["error"])
	}

	if status, _ := ts.do(t, http.MethodGet, "/api/runs/run_missing", nil); status != http.StatusNotFound {
		t.Fatalf("missing run must be 404, got %d", status)
	}
	if status, _ := ts.do(t, http.MethodGet, "/api/runs/run_missing/case", nil); status != http.StatusNotFound {
		t.Fatalf("missing case must be 404, got %d", status)
	}
	if status, _ := ts.do(t, http.MethodPost, "/api/runs/run_missing/approve", map[string]any{}); status != http.StatusNotFound {
		t.Fatalf("approving a missing run must be 404, got %d", status)
	}
	if status, _ := ts.do(t, http.MethodPost, "/api/runs/run_missing/answer", map[string]string{"text": "x"}); status != http.StatusNotFound {
		t.Fatalf("answering a missing run must be 404, got %d", status)
	}
}

// TestHealthReportsArtifactsDirMismatch 覆盖一个曾经静默的坑：
// 执行器把截图写到 A，控制面从 B 提供 /artifacts/，图片全部 404 却没人发现。
func TestHealthReportsArtifactsDirMismatch(t *testing.T) {
	workerDir := filepath.Join(t.TempDir(), "worker-artifacts")
	loopdDir := filepath.Join(t.TempDir(), "loopd-artifacts")
	ts := newTestServerWith(t, newFakeExecutor(workerDir), loopdDir)

	_, raw := ts.do(t, http.MethodGet, "/api/health", nil)
	health := decode[struct {
		Status         string `json:"status"`
		Artifacts      string `json:"artifacts"`
		WorkerArtifact string `json:"worker_artifacts"`
		ArtifactsMatch bool   `json:"artifacts_match"`
		WorkerDetail   string `json:"worker_detail"`
	}](t, raw)
	if health.Status != "ok" {
		t.Fatalf("the worker is reachable, so status must stay ok: %+v", health)
	}
	if health.ArtifactsMatch {
		t.Fatal("a mismatched artifacts dir must be reported")
	}
	if health.WorkerArtifact != workerDir {
		t.Fatalf("worker_artifacts = %q, want %q", health.WorkerArtifact, workerDir)
	}
	if !strings.Contains(health.WorkerDetail, "mismatch") {
		t.Fatalf("worker_detail must explain the mismatch: %q", health.WorkerDetail)
	}
}

func TestHealthConfirmsMatchingArtifactsDir(t *testing.T) {
	ts := newTestServer(t)
	_, raw := ts.do(t, http.MethodGet, "/api/health", nil)
	health := decode[struct {
		ArtifactsMatch bool   `json:"artifacts_match"`
		WorkerDetail   string `json:"worker_detail"`
	}](t, raw)
	if !health.ArtifactsMatch {
		t.Fatal("matching dirs must report artifacts_match = true")
	}
	if health.WorkerDetail != "" {
		t.Fatalf("no mismatch means no detail: %q", health.WorkerDetail)
	}
}

// TestApproveRequiresAwaitingApproval 确认不能跳过审批直接执行。
func TestApproveRequiresAwaitingApproval(t *testing.T) {
	ts := newTestServer(t)
	_, raw := ts.do(t, http.MethodPost, "/api/runs", map[string]string{"input": "打开第一个商品"})
	runID := decode[map[string]string](t, raw)["run_id"]

	status, body := ts.do(t, http.MethodPost, "/api/runs/"+runID+"/approve", map[string]any{})
	if status != http.StatusConflict {
		t.Fatalf("approve during planning must be 409, got %d %s", status, body)
	}
}
