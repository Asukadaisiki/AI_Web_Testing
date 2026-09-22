// Package api 是控制面 HTTP 接口：4 个页面所需的一切都在这里。
//
// 端点与字段名严格对应 web/src/api.ts，不自创第二个形态。
package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/agentruntime"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/contract"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/report"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/store"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/usage"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/worker"
)

// Server 是控制面服务。
type Server struct {
	store        *store.Store
	runtime      *agentruntime.Runtime
	client       *worker.Client
	artifactsDir string
	baseCtx      context.Context
	cancel       context.CancelFunc
	mux          *http.ServeMux
}

// New 创建服务。baseCtx 决定后台规划/执行任务的生命周期。
func New(
	store *store.Store,
	runtime *agentruntime.Runtime,
	client *worker.Client,
	artifactsDir string,
) *Server {
	baseCtx, cancel := context.WithCancel(context.Background())
	server := &Server{
		store:        store,
		runtime:      runtime,
		client:       client,
		artifactsDir: artifactsDir,
		baseCtx:      baseCtx,
		cancel:       cancel,
	}
	server.mux = server.routes()
	return server
}

// Close 取消所有后台任务。
func (s *Server) Close() { s.cancel() }

// Handler 返回可挂到 http.Server 的 handler。
func (s *Server) Handler() http.Handler { return s.mux }

func (s *Server) routes() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", s.handleHealth)
	mux.HandleFunc("GET /api/sessions", s.handleListSessions)
	mux.HandleFunc("POST /api/sessions", s.handleCreateSession)
	mux.HandleFunc("GET /api/sessions/{id}", s.handleGetSession)
	mux.HandleFunc("GET /api/runs", s.handleListRuns)
	mux.HandleFunc("POST /api/runs", s.handleCreateRun)
	mux.HandleFunc("GET /api/runs/{id}", s.handleGetRun)
	mux.HandleFunc("GET /api/runs/{id}/case", s.handleGetCase)
	mux.HandleFunc("POST /api/runs/{id}/approve", s.handleApprove)
	mux.HandleFunc("POST /api/runs/{id}/answer", s.handleAnswer)
	mux.HandleFunc("GET /api/runs/{id}/execution", s.handleGetExecution)
	mux.HandleFunc("GET /api/runs/{id}/report", s.handleGetReport)
	mux.HandleFunc("GET /api/runs/{id}/feedback", s.handleGetFeedback)
	mux.HandleFunc("POST /api/runs/{id}/feedback/confirm", s.handleConfirmFeedback)
	mux.HandleFunc("GET /api/runs/{id}/events", s.handleEvents)
	mux.Handle("/artifacts/", http.StripPrefix("/artifacts/", http.FileServer(http.Dir(s.artifactsDir))))
	return mux
}

/* ------------------------------- 通用工具 ------------------------------- */

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if payload == nil {
		return
	}
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		log.Printf("api: write response: %v", err)
	}
}

func writeError(w http.ResponseWriter, status int, code, detail string) {
	writeJSON(w, status, map[string]string{"error": code, "detail": detail})
}

func (s *Server) loadRun(w http.ResponseWriter, r *http.Request) (store.Run, bool) {
	run, err := s.store.GetRun(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "run_not_found", "找不到该 run")
		return store.Run{}, false
	}
	return run, true
}

/* -------------------------------- 端点 --------------------------------- */

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	workerStatus := "ok"
	workerDetail := ""
	workerArtifacts := ""
	if health, err := s.client.Health(ctx); err != nil {
		workerStatus = "unreachable"
		workerDetail = err.Error()
	} else {
		workerArtifacts = health.ArtifactsDir
		if workerArtifacts != "" && !samePath(workerArtifacts, s.artifactsDir) {
			// 不是致命故障，但截图一定 404，必须让操作者一眼看到。
			workerDetail = fmt.Sprintf(
				"artifacts dir mismatch: loopd serves %s but the worker writes to %s; screenshots will 404. Set LOOP_ARTIFACTS_DIR to the same directory for both.",
				s.artifactsDir, workerArtifacts,
			)
		}
	}
	status := http.StatusOK
	if workerStatus != "ok" {
		status = http.StatusServiceUnavailable
	}
	writeJSON(w, status, map[string]any{
		"status":           workerStatus,
		"worker":           s.client.BaseURL(),
		"worker_detail":    workerDetail,
		"model":            s.runtime.LLMLabel(),
		"artifacts":        s.artifactsDir,
		"worker_artifacts": workerArtifacts,
		"artifacts_match":  workerArtifacts == "" || samePath(workerArtifacts, s.artifactsDir),
	})
}

// samePath 比较两个目录是否指向同一处（忽略大小写、分隔符与结尾斜杠）。
func samePath(a, b string) bool {
	normalize := func(value string) string {
		value = strings.TrimRight(strings.ReplaceAll(value, "\\", "/"), "/")
		return strings.ToLower(filepath.ToSlash(value))
	}
	return normalize(a) == normalize(b)
}

func (s *Server) handleListRuns(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	runs, err := s.store.ListRuns(r.Context(), limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list_runs_failed", err.Error())
		return
	}
	// 列表也带用量：run 对象在列表与详情里必须是同一个形状，
	// 否则前端要为"同一个东西的两种 JSON"写两套解析。
	out := make([]runResponse, 0, len(runs))
	for _, run := range runs {
		spent, err := s.store.GetUsage(r.Context(), run.ID)
		if err != nil {
			log.Printf("api: get usage for %s: %v", run.ID, err)
		}
		out = append(out, runResponse{Run: run, Usage: spent})
	}
	writeJSON(w, http.StatusOK, map[string]any{"runs": out})
}

// handleCreateSession 建会话并跑它的第 1 轮。
//
// 这是"人输入一个目标"的唯一入口：目标属于会话，不属于某一轮（CONTRACT §9.1）。
func (s *Server) handleCreateSession(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Goal string `json:"goal"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	goal := strings.TrimSpace(body.Goal)
	if goal == "" {
		writeError(w, http.StatusBadRequest, "goal_required", "goal 不能为空")
		return
	}
	session, run, err := s.store.CreateSession(r.Context(), goal)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "create_session_failed", err.Error())
		return
	}
	s.startPlanning(run)
	writeJSON(w, http.StatusAccepted, map[string]string{
		"session_id": session.ID,
		"run_id":     run.ID,
	})
}

// handleListSessions 列出会话（含轮次数、最新一轮状态、累计用量）。
func (s *Server) handleListSessions(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	sessions, err := s.store.ListSessions(r.Context(), limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list_sessions_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"sessions": sessions})
}

// handleGetSession 返回会话本身 + 它的全部轮次（第 1 轮在前）。
func (s *Server) handleGetSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	session, err := s.store.GetSession(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "session_not_found", "会话不存在")
			return
		}
		writeError(w, http.StatusInternalServerError, "get_session_failed", err.Error())
		return
	}
	runs, err := s.store.ListRunsBySession(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list_runs_failed", err.Error())
		return
	}
	// 轮次用与 /api/runs 完全相同的形状，前端只写一套解析。
	out := make([]runResponse, 0, len(runs))
	for _, run := range runs {
		spent, err := s.store.GetUsage(r.Context(), run.ID)
		if err != nil {
			log.Printf("api: get usage for %s: %v", run.ID, err)
		}
		out = append(out, runResponse{Run: run, Usage: spent})
	}
	writeJSON(w, http.StatusOK, sessionResponse{Session: session, Runs: out})
}

// handleCreateRun 在既有会话里再开一轮（回灌链条上的下一轮）。
func (s *Server) handleCreateRun(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Input       string  `json:"input"`
		SessionID   string  `json:"session_id"`
		ParentRunID *string `json:"parent_run_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	input := strings.TrimSpace(body.Input)
	if input == "" {
		writeError(w, http.StatusBadRequest, "input_required", "input 不能为空")
		return
	}
	if strings.TrimSpace(body.SessionID) == "" {
		writeError(w, http.StatusBadRequest, "session_required", "session_id 不能为空；新建目标请用 POST /api/sessions")
		return
	}
	run, err := s.store.CreateRun(r.Context(), body.SessionID, input, body.ParentRunID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "session_not_found", "会话不存在")
			return
		}
		writeError(w, http.StatusInternalServerError, "create_run_failed", err.Error())
		return
	}
	s.startPlanning(run)
	writeJSON(w, http.StatusAccepted, map[string]string{
		"session_id": body.SessionID,
		"run_id":     run.ID,
	})
}

func (s *Server) startPlanning(run store.Run) {
	go func() {
		if err := s.runtime.Plan(s.baseCtx, run); err != nil {
			log.Printf("api: run %s planning ended with: %v", run.ID, err)
		}
	}()
}

func (s *Server) handleGetRun(w http.ResponseWriter, r *http.Request) {
	run, ok := s.loadRun(w, r)
	if !ok {
		return
	}
	// 用量附在 run 上：页面 1 要在规划过程中就能看到钱在涨，所以不能只放在报告里。
	spent, err := s.store.GetUsage(r.Context(), run.ID)
	if err != nil {
		log.Printf("api: get usage for %s: %v", run.ID, err)
	}
	writeJSON(w, http.StatusOK, runResponse{Run: run, Usage: spent})
}

// runResponse 是 run + 它的累计模型用量。列表与详情用同一个形状。
type runResponse struct {
	store.Run
	Usage usage.Usage `json:"usage"`
}

// sessionResponse 是会话 + 它的全部轮次。
type sessionResponse struct {
	store.Session
	Runs []runResponse `json:"runs"`
}

func (s *Server) handleGetCase(w http.ResponseWriter, r *http.Request) {
	run, ok := s.loadRun(w, r)
	if !ok {
		return
	}
	record, err := s.store.GetCase(r.Context(), run.ID)
	if err != nil {
		writeError(w, http.StatusNotFound, "case_not_ready", "该 run 还没有 case 工件")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"case_id": strconv.FormatInt(record.ID, 10),
		"case":    json.RawMessage(record.Payload),
	})
}

func (s *Server) handleApprove(w http.ResponseWriter, r *http.Request) {
	run, ok := s.loadRun(w, r)
	if !ok {
		return
	}
	if run.Status != store.StatusAwaitingApproval {
		writeError(w, http.StatusConflict, "run_not_awaiting_approval",
			fmt.Sprintf("当前状态是 %s，只有 awaiting_approval 可以审批", run.Status))
		return
	}
	record, err := s.store.GetCase(r.Context(), run.ID)
	if err != nil {
		writeError(w, http.StatusConflict, "case_not_ready", "该 run 还没有 case 工件")
		return
	}
	if err := s.store.ApproveCase(r.Context(), record.ID, "owner"); err != nil {
		writeError(w, http.StatusInternalServerError, "approve_failed", err.Error())
		return
	}
	go func() {
		if err := s.runtime.Execute(s.baseCtx, run); err != nil {
			log.Printf("api: run %s execution ended with: %v", run.ID, err)
		}
	}()
	writeJSON(w, http.StatusAccepted, map[string]string{"status": store.StatusExecuting})
}

func (s *Server) handleAnswer(w http.ResponseWriter, r *http.Request) {
	run, ok := s.loadRun(w, r)
	if !ok {
		return
	}
	var body struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	if strings.TrimSpace(body.Text) == "" {
		writeError(w, http.StatusBadRequest, "answer_required", "回答不能为空")
		return
	}
	if !s.runtime.Answer(run.ID, body.Text) {
		writeError(w, http.StatusConflict, "run_not_awaiting_input",
			fmt.Sprintf("run %s 当前不在等待回答（状态 %s）", run.ID, run.Status))
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": store.StatusPlanning})
}

func (s *Server) handleGetExecution(w http.ResponseWriter, r *http.Request) {
	run, ok := s.loadRun(w, r)
	if !ok {
		return
	}
	record, err := s.store.GetExecution(r.Context(), run.ID)
	if err != nil {
		// 还没执行：返回空壳而不是 404，前端少一个分支。
		writeJSON(w, http.StatusOK, map[string]any{
			"execution_id": "", "status": run.Status, "result": nil,
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"execution_id": record.ID,
		"status":       record.Status,
		"result":       json.RawMessage(record.Result),
	})
}

func (s *Server) handleGetReport(w http.ResponseWriter, r *http.Request) {
	run, ok := s.loadRun(w, r)
	if !ok {
		return
	}
	var result contract.ExecutionResult
	if record, err := s.store.GetExecution(r.Context(), run.ID); err == nil {
		if err := json.Unmarshal(record.Result, &result); err != nil {
			writeError(w, http.StatusInternalServerError, "execution_decode_failed", err.Error())
			return
		}
	}
	signals, err := s.store.ListSignals(r.Context(), run.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list_signals_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, report.Build(run.ID, run.Status, result, signals))
}

func (s *Server) handleGetFeedback(w http.ResponseWriter, r *http.Request) {
	run, ok := s.loadRun(w, r)
	if !ok {
		return
	}
	candidates, err := s.store.ListFeedback(r.Context(), run.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list_feedback_failed", err.Error())
		return
	}
	if candidates == nil {
		candidates = []store.FeedbackCandidate{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"candidates": candidates})
}

// handleConfirmFeedback 是"半自动回灌"的人工确认口：人改完输入，开下一轮 run。
func (s *Server) handleConfirmFeedback(w http.ResponseWriter, r *http.Request) {
	run, ok := s.loadRun(w, r)
	if !ok {
		return
	}
	var body struct {
		CandidateID int64  `json:"candidate_id"`
		Input       string `json:"input"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	input := strings.TrimSpace(body.Input)
	if input == "" {
		writeError(w, http.StatusBadRequest, "input_required", "下一轮输入不能为空")
		return
	}
	candidate, err := s.store.GetFeedback(r.Context(), body.CandidateID)
	if err != nil {
		writeError(w, http.StatusNotFound, "candidate_not_found", "找不到该回灌候选")
		return
	}
	if candidate.RunID != run.ID {
		writeError(w, http.StatusBadRequest, "candidate_run_mismatch", "该候选不属于这个 run")
		return
	}
	// 候选是一次性的：确认过（used）就不能再开新一轮，否则同一个失败会被重复回灌。
	if candidate.Status != "pending" {
		writeError(w, http.StatusConflict, "candidate_already_used",
			fmt.Sprintf("该候选已被确认过（status=%s），一条候选只能开一轮", candidate.Status))
		return
	}
	if err := s.store.MarkFeedbackUsed(r.Context(), candidate.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "mark_feedback_failed", err.Error())
		return
	}
	// 回灌的下一轮属于**同一个会话**：会话是目标 + 它的全部轮次（CONTRACT §9.1）。
	sessionID, err := sessionIDOfRun(run)
	if err != nil {
		writeError(w, http.StatusConflict, "run_without_session", err.Error())
		return
	}
	parentRunID := run.ID
	child, err := s.store.CreateRun(r.Context(), sessionID, input, &parentRunID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "create_run_failed", err.Error())
		return
	}
	s.startPlanning(child)
	writeJSON(w, http.StatusAccepted, map[string]string{
		"session_id": sessionID,
		"run_id":     child.ID,
	})
}

// sessionIDOfRun 取 run 的会话 id。
func sessionIDOfRun(run store.Run) (string, error) {
	if run.SessionID == nil || *run.SessionID == "" {
		return "", fmt.Errorf("run %s 没有关联会话", run.ID)
	}
	return *run.SessionID, nil
}

// handleEvents 是 SSE：先按 seq 重放，再实时推送；断线重连带 from 即可补齐。
func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	runID := r.PathValue("id")
	if _, err := s.store.GetRun(r.Context(), runID); err != nil {
		writeError(w, http.StatusNotFound, "run_not_found", "找不到该 run")
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming_unsupported", "当前 server 不支持流式响应")
		return
	}
	from, _ := strconv.ParseInt(r.URL.Query().Get("from"), 10, 64)

	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	// 先订阅再重放：这样重放期间产生的事件也不会丢（靠 seq 去重）。
	channel, cancel := s.store.Broker().Subscribe(runID)
	defer cancel()

	lastSeq := from
	events, err := s.store.ListEvents(r.Context(), runID, from)
	if err != nil {
		log.Printf("api: replay events for %s: %v", runID, err)
		return
	}
	for _, event := range events {
		if event.Seq <= lastSeq {
			continue
		}
		if !writeEvent(w, event) {
			return
		}
		lastSeq = event.Seq
	}
	flusher.Flush()

	heartbeat := time.NewTicker(20 * time.Second)
	defer heartbeat.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case event, open := <-channel:
			if !open {
				return
			}
			if event.Seq <= lastSeq {
				continue
			}
			if !writeEvent(w, event) {
				return
			}
			lastSeq = event.Seq
			flusher.Flush()
		case <-heartbeat.C:
			if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

func writeEvent(w http.ResponseWriter, event store.Event) bool {
	payload, err := json.Marshal(map[string]any{
		"seq":     event.Seq,
		"type":    event.Type,
		"payload": event.Payload,
	})
	if err != nil {
		log.Printf("api: marshal event %d: %v", event.Seq, err)
		return true
	}
	if _, err := fmt.Fprintf(w, "data: %s\n\n", payload); err != nil {
		return false
	}
	return true
}

// Serve 是便捷入口，给 cmd/loopd 用。
func Serve(addr string, handler http.Handler) error {
	server := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}
	log.Printf("loopd listening on http://%s", addr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

// EnsureArtifactsDir 保证证据目录存在。
func EnsureArtifactsDir(dir string) error {
	if dir == "" {
		return nil
	}
	return os.MkdirAll(dir, 0o755)
}
