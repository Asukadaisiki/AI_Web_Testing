package httptransport

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/agentcore"
	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

type Handler struct {
	agent AgentAPI
}

type AgentAPI interface {
	Start(ctx context.Context, conversationID string, input string) (agentcore.AgentRun, error)
	GetRun(ctx context.Context, runID string) (agentcore.AgentRun, error)
	ListEvents(ctx context.Context, runID string, afterSeq int64) ([]agentcore.Event, error)
	Resume(
		ctx context.Context,
		runID string,
		toolCallID string,
		request agentcore.ResumeToolCallRequest,
	) (agentcore.AgentRun, error)
}

func NewServer(address string, agent AgentAPI) *server.Hertz {
	h := server.New(server.WithHostPorts(address))
	handler := &Handler{agent: agent}

	h.GET("/health", handler.health)
	v2 := h.Group("/api/v2")
	v2.POST("/agent/runs", handler.startRun)
	v2.GET("/agent/runs/:run_id", handler.getRun)
	v2.GET("/agent/runs/:run_id/events", handler.listEvents)
	v2.POST("/agent/runs/:run_id/tool-calls/:tool_call_id/resume", handler.resumeToolCall)
	return h
}

func (h *Handler) health(_ context.Context, c *app.RequestContext) {
	c.JSON(consts.StatusOK, map[string]string{"status": "ok"})
}

type startRunRequest struct {
	ConversationID string `json:"conversation_id" vd:"len($)>0"`
	Message        string `json:"message" vd:"len($)>0"`
}

func (h *Handler) startRun(ctx context.Context, c *app.RequestContext) {
	var request startRunRequest
	if err := c.BindAndValidate(&request); err != nil {
		writeError(c, consts.StatusBadRequest, err)
		return
	}
	run, err := h.agent.Start(ctx, request.ConversationID, request.Message)
	if err != nil {
		writeError(c, consts.StatusBadRequest, err)
		return
	}
	c.JSON(consts.StatusCreated, run)
}

func (h *Handler) getRun(ctx context.Context, c *app.RequestContext) {
	run, err := h.agent.GetRun(ctx, c.Param("run_id"))
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(consts.StatusOK, run)
}

func (h *Handler) listEvents(ctx context.Context, c *app.RequestContext) {
	afterSeq := int64(0)
	if raw := c.Query("after_seq"); raw != "" {
		value, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || value < 0 {
			writeError(c, consts.StatusBadRequest, errors.New("after_seq must be a non-negative integer"))
			return
		}
		afterSeq = value
	}
	events, err := h.agent.ListEvents(ctx, c.Param("run_id"), afterSeq)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(consts.StatusOK, map[string]any{"events": events})
}

func (h *Handler) resumeToolCall(ctx context.Context, c *app.RequestContext) {
	var request agentcore.ResumeToolCallRequest
	if err := c.BindAndValidate(&request); err != nil {
		writeError(c, consts.StatusBadRequest, err)
		return
	}
	run, err := h.agent.Resume(
		ctx,
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

func writeServiceError(c *app.RequestContext, err error) {
	switch {
	case errors.Is(err, agentcore.ErrRunNotFound):
		writeError(c, consts.StatusNotFound, err)
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
