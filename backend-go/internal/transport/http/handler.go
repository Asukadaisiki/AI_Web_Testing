package httptransport

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/agentservice"
	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/cases"
	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/corrections"
	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/execution"
	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/planning"
	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/projects"
	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
	"github.com/cloudwego/hertz/pkg/protocol/sse"
)

type Handler struct {
	agent       AgentAPI
	planning    planning.Store
	projects    projects.Store
	cases       cases.Store
	executions  *execution.Store
	corrections *corrections.Store
}

type AgentAPI interface {
	StartOwnedProjectAsync(
		context.Context,
		int64,
		string,
		int64,
		string,
	) (agentservice.AgentRun, error)
	GetOwnedRun(ctx context.Context, runID string, actorUserID int64) (agentservice.AgentRun, error)
	ListOwnedEvents(
		ctx context.Context,
		runID string,
		actorUserID int64,
		afterSeq int64,
	) ([]agentservice.Event, error)
	Subscribe(runID string) agentservice.Subscription
	ResumeOwned(
		ctx context.Context,
		actorUserID int64,
		runID string,
		toolCallID string,
		request agentservice.ResumeToolCallRequest,
	) (agentservice.AgentRun, error)
	CancelOwned(
		ctx context.Context,
		actorUserID int64,
		runID string,
		reason string,
	) (agentservice.AgentRun, error)
}

func NewServer(
	address string,
	agent AgentAPI,
	actorUserID int64,
	planningStore planning.Store,
	projectStore projects.Store,
	caseStore cases.Store,
	executionStore *execution.Store,
	correctionStore *corrections.Store,
) *server.Hertz {
	h := server.New(server.WithHostPorts(address))
	handler := &Handler{
		agent: agent, planning: planningStore,
		projects: projectStore, cases: caseStore, executions: executionStore,
		corrections: correctionStore,
	}

	h.GET("/health", handler.health)
	v2 := h.Group("/api/v2")
	v2.Use(actorMiddleware(actorUserID))
	v2.POST("/agent/runs", handler.startRun)
	v2.GET("/agent/runs/:run_id", handler.getRun)
	v2.GET("/agent/runs/:run_id/events", handler.listEvents)
	v2.GET("/agent/runs/:run_id/events/stream", handler.streamEvents)
	v2.POST("/agent/runs/:run_id/cancel", handler.cancelRun)
	v2.POST("/agent/runs/:run_id/tool-calls/:tool_call_id/resume", handler.resumeToolCall)
	v2.POST("/planning/sessions", handler.createPlanningSession)
	v2.GET("/planning/sessions", handler.listPlanningSessions)
	v2.GET("/planning/sessions/:session_id", handler.getPlanningSession)
	v2.PATCH("/planning/sessions/:session_id", handler.updatePlanningSession)
	v2.DELETE("/planning/sessions/:session_id", handler.deletePlanningSession)
	v2.GET("/planning/sessions/:session_id/projects", handler.listPlanningProjects)
	v2.POST("/planning/sessions/:session_id/projects", handler.linkPlanningProject)
	v2.DELETE(
		"/planning/sessions/:session_id/projects/:project_id",
		handler.unlinkPlanningProject,
	)
	v2.POST(
		"/planning/sessions/:session_id/projects:create",
		handler.createPlanningProject,
	)
	v2.GET("/projects", handler.listProjects)
	v2.POST("/projects", handler.createProject)
	v2.GET("/projects/:project_id", handler.getProject)
	v2.PUT("/projects/:project_id", handler.updateProject)
	v2.DELETE("/projects/:project_id", handler.deleteProject)
	v2.GET("/cases", handler.listCases)
	v2.POST("/cases", handler.createCase)
	v2.DELETE("/cases/batch", handler.deleteCases)
	v2.GET("/cases/:case_id", handler.getCase)
	v2.PUT("/cases/:case_id", handler.updateCase)
	v2.DELETE("/cases/:case_id", handler.deleteCase)
	v2.POST("/cases/:case_id/execute", handler.executeCase)
	v2.POST("/execution-batches", handler.createExecutionBatch)
	v2.GET("/execution-batches", handler.listExecutionBatches)
	v2.GET("/execution-batches/:batch_id", handler.getExecutionBatch)
	v2.GET("/execution-batches/:batch_id/report", handler.getExecutionBatchReport)
	v2.POST("/execution-batches/:batch_id/cancel", handler.cancelExecutionBatch)
	v2.GET("/executions/overview", handler.executionOverview)
	v2.GET("/executions", handler.listExecutions)
	v2.GET("/executions/:execution_id", handler.getExecution)
	v2.DELETE("/executions/:execution_id", handler.deleteExecution)
	v2.POST("/corrections", handler.createCorrection)
	return h
}

func (h *Handler) health(_ context.Context, c *app.RequestContext) {
	c.JSON(consts.StatusOK, map[string]string{"status": "ok"})
}

type startRunRequest struct {
	SessionID      int64  `json:"session_id,omitempty"`
	ConversationID string `json:"conversation_id,omitempty"`
	ProjectID      int64  `json:"project_id,omitempty"`
	Message        string `json:"message" vd:"len($)>0"`
}

func (h *Handler) startRun(ctx context.Context, c *app.RequestContext) {
	var request startRunRequest
	if err := c.BindAndValidate(&request); err != nil {
		writeError(c, consts.StatusBadRequest, err)
		return
	}
	identity, err := currentActor(c)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	sessionID := request.SessionID
	if sessionID < 1 && request.ConversationID != "" {
		sessionID, err = strconv.ParseInt(request.ConversationID, 10, 64)
	}
	if err != nil || sessionID < 1 {
		writeError(c, consts.StatusBadRequest, errors.New("session_id is required"))
		return
	}
	conversationID, projectID, err := h.planning.ResolveRunContext(
		ctx,
		identity.UserID,
		sessionID,
	)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	run, err := h.agent.StartOwnedProjectAsync(
		ctx,
		identity.UserID,
		conversationID,
		projectID,
		request.Message,
	)
	if err != nil {
		writeError(c, consts.StatusBadRequest, err)
		return
	}
	c.JSON(consts.StatusAccepted, run)
}

func (h *Handler) getRun(ctx context.Context, c *app.RequestContext) {
	identity, err := currentActor(c)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	run, err := h.agent.GetOwnedRun(ctx, c.Param("run_id"), identity.UserID)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(consts.StatusOK, run)
}

type cancelRunRequest struct {
	Reason string `json:"reason" vd:"len($)>0"`
}

func (h *Handler) cancelRun(ctx context.Context, c *app.RequestContext) {
	identity, err := currentActor(c)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	var request cancelRunRequest
	if err := c.BindAndValidate(&request); err != nil {
		writeError(c, consts.StatusBadRequest, err)
		return
	}
	request.Reason = strings.TrimSpace(request.Reason)
	if request.Reason == "" {
		writeError(c, consts.StatusBadRequest, errors.New("cancel reason is required"))
		return
	}
	run, err := h.agent.CancelOwned(
		ctx,
		identity.UserID,
		c.Param("run_id"),
		request.Reason,
	)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(consts.StatusOK, run)
}

func (h *Handler) listEvents(ctx context.Context, c *app.RequestContext) {
	identity, err := currentActor(c)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	afterSeq := int64(0)
	if raw := c.Query("after_seq"); raw != "" {
		value, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || value < 0 {
			writeError(c, consts.StatusBadRequest, errors.New("after_seq must be a non-negative integer"))
			return
		}
		afterSeq = value
	}
	events, err := h.agent.ListOwnedEvents(
		ctx,
		c.Param("run_id"),
		identity.UserID,
		afterSeq,
	)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(consts.StatusOK, map[string]any{"events": events})
}

func (h *Handler) resumeToolCall(ctx context.Context, c *app.RequestContext) {
	identity, err := currentActor(c)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	var request agentservice.ResumeToolCallRequest
	if err := c.BindAndValidate(&request); err != nil {
		writeError(c, consts.StatusBadRequest, err)
		return
	}
	run, err := h.agent.ResumeOwned(
		ctx,
		identity.UserID,
		c.Param("run_id"),
		c.Param("tool_call_id"),
		request,
	)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(consts.StatusOK, run)
}

func (h *Handler) streamEvents(ctx context.Context, c *app.RequestContext) {
	identity, err := currentActor(c)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	afterSeq, err := parseAfterSeq(c)
	if err != nil {
		writeError(c, consts.StatusBadRequest, err)
		return
	}
	runID := c.Param("run_id")
	history, err := h.agent.ListOwnedEvents(ctx, runID, identity.UserID, afterSeq)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	subscription := h.agent.Subscribe(runID)
	defer subscription.Cancel()
	writer := sse.NewWriter(c)
	defer writer.Close()

	lastSeq := afterSeq
	for _, event := range history {
		if writeErr := writeSSEEvent(writer, event); writeErr != nil {
			return
		}
		lastSeq = event.Seq
	}
	run, err := h.agent.GetOwnedRun(ctx, runID, identity.UserID)
	if err != nil || isTerminal(run.Status) {
		return
	}

	keepAlive := time.NewTicker(15 * time.Second)
	defer keepAlive.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-keepAlive.C:
			if writeErr := writer.WriteKeepAlive(); writeErr != nil {
				return
			}
		case event, ok := <-subscription.Events:
			if !ok {
				return
			}
			if event.Seq <= lastSeq {
				continue
			}
			if writeErr := writeSSEEvent(writer, event); writeErr != nil {
				return
			}
			lastSeq = event.Seq
			if event.Type == agentservice.EventRunFinished ||
				event.Type == agentservice.EventRunFailed ||
				event.Type == agentservice.EventRunCancelled {
				return
			}
		}
	}
}

func parseAfterSeq(c *app.RequestContext) (int64, error) {
	raw := c.Query("after_seq")
	if raw == "" {
		raw = string(c.GetHeader("Last-Event-ID"))
	}
	if raw == "" {
		return 0, nil
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value < 0 {
		return 0, errors.New("after_seq must be a non-negative integer")
	}
	return value, nil
}

func writeSSEEvent(writer *sse.Writer, event agentservice.Event) error {
	id, eventType, data, err := encodeSSEEvent(event)
	if err != nil {
		return err
	}
	return writer.WriteEvent(id, eventType, data)
}

func encodeSSEEvent(event agentservice.Event) (string, string, []byte, error) {
	data, err := json.Marshal(event)
	if err != nil {
		return "", "", nil, err
	}
	return strconv.FormatInt(event.Seq, 10), string(event.Type), data, nil
}

func isTerminal(status agentservice.RunStatus) bool {
	return status == agentservice.RunStatusCompleted ||
		status == agentservice.RunStatusFailed ||
		status == agentservice.RunStatusCancelled
}

func writeServiceError(c *app.RequestContext, err error) {
	switch {
	case errors.Is(err, agentservice.ErrRunNotFound):
		writeError(c, consts.StatusNotFound, err)
	case errors.Is(err, agentservice.ErrRunAccessDenied),
		errors.Is(err, planning.ErrAccessDenied):
		writeError(c, consts.StatusForbidden, err)
	case errors.Is(err, projects.ErrAccessDenied):
		writeError(c, consts.StatusForbidden, err)
	case errors.Is(err, cases.ErrAccessDenied):
		writeError(c, consts.StatusForbidden, err)
	case errors.Is(err, execution.ErrAccessDenied):
		writeError(c, consts.StatusForbidden, err)
	case errors.Is(err, planning.ErrSessionNotFound),
		errors.Is(err, planning.ErrProjectNotFound),
		errors.Is(err, projects.ErrNotFound),
		errors.Is(err, cases.ErrNotFound),
		errors.Is(err, execution.ErrNotFound):
		writeError(c, consts.StatusNotFound, err)
	case errors.Is(err, corrections.ErrNotFound):
		writeError(c, consts.StatusNotFound, err)
	case errors.Is(err, planning.ErrConflict),
		errors.Is(err, projects.ErrConflict),
		errors.Is(err, execution.ErrConflict):
		writeError(c, consts.StatusConflict, err)
	case errors.Is(err, agentservice.ErrRunNotWaitingForUser),
		errors.Is(err, agentservice.ErrToolCallMismatch):
		writeError(c, consts.StatusConflict, err)
	default:
		writeError(c, consts.StatusInternalServerError, err)
	}
}

func writeError(c *app.RequestContext, status int, err error) {
	c.JSON(status, map[string]string{
		"error":   http.StatusText(status),
		"message": err.Error(),
	})
}
