package httptransport

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/agentcore"
	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/authn"
	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/planning"
	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
	"github.com/cloudwego/hertz/pkg/protocol/sse"
)

type Handler struct {
	agent    AgentAPI
	planning planning.Store
}

type AgentAPI interface {
	StartOwnedProjectAsync(
		context.Context,
		int64,
		string,
		int64,
		string,
	) (agentcore.AgentRun, error)
	GetOwnedRun(ctx context.Context, runID string, actorUserID int64) (agentcore.AgentRun, error)
	ListOwnedEvents(
		ctx context.Context,
		runID string,
		actorUserID int64,
		afterSeq int64,
	) ([]agentcore.Event, error)
	Subscribe(runID string) agentcore.Subscription
	ResumeOwned(
		ctx context.Context,
		actorUserID int64,
		runID string,
		toolCallID string,
		request agentcore.ResumeToolCallRequest,
	) (agentcore.AgentRun, error)
}

func NewServer(
	address string,
	agent AgentAPI,
	authenticator authn.Authenticator,
	planningStore planning.Store,
) *server.Hertz {
	h := server.New(server.WithHostPorts(address))
	handler := &Handler{agent: agent, planning: planningStore}

	h.GET("/health", handler.health)
	v2 := h.Group("/api/v2")
	v2.Use(authenticationMiddleware(authenticator))
	v2.POST("/agent/runs", handler.startRun)
	v2.GET("/agent/runs/:run_id", handler.getRun)
	v2.GET("/agent/runs/:run_id/events", handler.listEvents)
	v2.GET("/agent/runs/:run_id/events/stream", handler.streamEvents)
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
	identity, err := currentIdentity(c)
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
		authn.WithIdentity(ctx, identity),
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
	identity, err := currentIdentity(c)
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

func (h *Handler) listEvents(ctx context.Context, c *app.RequestContext) {
	identity, err := currentIdentity(c)
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
	identity, err := currentIdentity(c)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	var request agentcore.ResumeToolCallRequest
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
	identity, err := currentIdentity(c)
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
			if event.Type == agentcore.EventRunFinished || event.Type == agentcore.EventRunFailed {
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

func writeSSEEvent(writer *sse.Writer, event agentcore.Event) error {
	id, eventType, data, err := encodeSSEEvent(event)
	if err != nil {
		return err
	}
	return writer.WriteEvent(id, eventType, data)
}

func encodeSSEEvent(event agentcore.Event) (string, string, []byte, error) {
	data, err := json.Marshal(event)
	if err != nil {
		return "", "", nil, err
	}
	return strconv.FormatInt(event.Seq, 10), string(event.Type), data, nil
}

func isTerminal(status agentcore.RunStatus) bool {
	return status == agentcore.RunStatusCompleted ||
		status == agentcore.RunStatusFailed ||
		status == agentcore.RunStatusCancelled
}

func writeServiceError(c *app.RequestContext, err error) {
	switch {
	case errors.Is(err, authn.ErrUnauthenticated):
		writeError(c, consts.StatusUnauthorized, err)
	case errors.Is(err, agentcore.ErrRunNotFound):
		writeError(c, consts.StatusNotFound, err)
	case errors.Is(err, agentcore.ErrRunAccessDenied),
		errors.Is(err, planning.ErrAccessDenied):
		writeError(c, consts.StatusForbidden, err)
	case errors.Is(err, planning.ErrSessionNotFound),
		errors.Is(err, planning.ErrProjectNotFound):
		writeError(c, consts.StatusNotFound, err)
	case errors.Is(err, planning.ErrConflict):
		writeError(c, consts.StatusConflict, err)
	case errors.Is(err, agentcore.ErrRunNotWaitingForUser),
		errors.Is(err, agentcore.ErrToolCallMismatch):
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
