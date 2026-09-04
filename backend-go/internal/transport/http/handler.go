package httptransport

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/agentcore"
	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
	"github.com/cloudwego/hertz/pkg/protocol/sse"
)

type Handler struct {
	agent AgentAPI
}

type AgentAPI interface {
	StartAsync(conversationID string, input string) (agentcore.AgentRun, error)
	GetRun(ctx context.Context, runID string) (agentcore.AgentRun, error)
	ListEvents(ctx context.Context, runID string, afterSeq int64) ([]agentcore.Event, error)
	Subscribe(runID string) agentcore.Subscription
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
	v2.GET("/agent/runs/:run_id/events/stream", handler.streamEvents)
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

func (h *Handler) startRun(_ context.Context, c *app.RequestContext) {
	var request startRunRequest
	if err := c.BindAndValidate(&request); err != nil {
		writeError(c, consts.StatusBadRequest, err)
		return
	}
	run, err := h.agent.StartAsync(request.ConversationID, request.Message)
	if err != nil {
		writeError(c, consts.StatusBadRequest, err)
		return
	}
	c.JSON(consts.StatusAccepted, run)
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

func (h *Handler) streamEvents(ctx context.Context, c *app.RequestContext) {
	afterSeq, err := parseAfterSeq(c)
	if err != nil {
		writeError(c, consts.StatusBadRequest, err)
		return
	}
	runID := c.Param("run_id")
	subscription := h.agent.Subscribe(runID)
	defer subscription.Cancel()

	history, err := h.agent.ListEvents(ctx, runID, afterSeq)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	writer := sse.NewWriter(c)
	defer writer.Close()

	lastSeq := afterSeq
	for _, event := range history {
		if writeErr := writeSSEEvent(writer, event); writeErr != nil {
			return
		}
		lastSeq = event.Seq
	}
	run, err := h.agent.GetRun(ctx, runID)
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
