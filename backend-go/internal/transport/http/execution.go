package httptransport

import (
	"context"
	"strconv"

	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/execution"
	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

func (h *Handler) executeCase(ctx context.Context, c *app.RequestContext) {
	identity, caseID, err := ownedPathContext(c, "case_id")
	if err != nil {
		writeError(c, consts.StatusBadRequest, err)
		return
	}
	var request execution.CaseExecutionRequest
	if err := c.BindAndValidate(&request); err != nil {
		writeError(c, consts.StatusBadRequest, err)
		return
	}
	result, err := h.executions.ExecuteCase(ctx, identity.UserID, caseID, request)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(consts.StatusOK, result)
}

func (h *Handler) createExecutionBatch(ctx context.Context, c *app.RequestContext) {
	identity, err := currentActor(c)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	var request execution.BatchCreateRequest
	if err := c.BindAndValidate(&request); err != nil {
		writeError(c, consts.StatusBadRequest, err)
		return
	}
	result, err := h.executions.CreateBatch(ctx, identity.UserID, request)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.Header("Location", "/api/v2/execution-batches/"+strconv.FormatInt(result["id"].(int64), 10))
	c.JSON(consts.StatusCreated, result)
}

func (h *Handler) listExecutionBatches(ctx context.Context, c *app.RequestContext) {
	identity, err := currentActor(c)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	projectID, err := positivePathID(c.Query("project_id"))
	if err != nil {
		writeError(c, consts.StatusBadRequest, err)
		return
	}
	result, err := h.executions.ListBatches(
		ctx, identity.UserID, projectID, positiveQueryInt(c.Query("limit"), 50),
	)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(consts.StatusOK, result)
}

func (h *Handler) getExecutionBatch(ctx context.Context, c *app.RequestContext) {
	identity, batchID, err := ownedPathContext(c, "batch_id")
	if err != nil {
		writeError(c, consts.StatusBadRequest, err)
		return
	}
	result, err := h.executions.BatchDetail(ctx, identity.UserID, batchID)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(consts.StatusOK, result)
}

func (h *Handler) getExecutionBatchReport(ctx context.Context, c *app.RequestContext) {
	identity, batchID, err := ownedPathContext(c, "batch_id")
	if err != nil {
		writeError(c, consts.StatusBadRequest, err)
		return
	}
	result, err := h.executions.BatchReport(ctx, identity.UserID, batchID)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(consts.StatusOK, result)
}

func (h *Handler) cancelExecutionBatch(ctx context.Context, c *app.RequestContext) {
	identity, batchID, err := ownedPathContext(c, "batch_id")
	if err != nil {
		writeError(c, consts.StatusBadRequest, err)
		return
	}
	result, err := h.executions.CancelBatch(ctx, identity.UserID, batchID)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(consts.StatusOK, result)
}

func (h *Handler) listExecutions(ctx context.Context, c *app.RequestContext) {
	identity, err := currentActor(c)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	projectID, err := optionalPositiveQueryID(c.Query("project_id"))
	if err != nil {
		writeError(c, consts.StatusBadRequest, err)
		return
	}
	caseID, err := optionalPositiveQueryID(c.Query("case_id"))
	if err != nil {
		writeError(c, consts.StatusBadRequest, err)
		return
	}
	result, err := h.executions.ListExecutions(ctx, identity.UserID, execution.ListRequest{
		ProjectID: projectID, CaseID: caseID, Status: c.Query("status"),
		FailureCategory:    c.Query("failure_category"),
		FailureFingerprint: c.Query("failure_fingerprint"),
		WindowDays:         positiveQueryInt(c.Query("window_days"), 0),
		Limit:              positiveQueryInt(c.Query("limit"), 20),
		Offset:             nonNegativeQueryInt(c.Query("offset")),
	})
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(consts.StatusOK, result)
}

func (h *Handler) getExecution(ctx context.Context, c *app.RequestContext) {
	identity, executionID, err := ownedPathContext(c, "execution_id")
	if err != nil {
		writeError(c, consts.StatusBadRequest, err)
		return
	}
	result, err := h.executions.GetExecution(ctx, identity.UserID, executionID)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(consts.StatusOK, result)
}

func (h *Handler) deleteExecution(ctx context.Context, c *app.RequestContext) {
	identity, executionID, err := ownedPathContext(c, "execution_id")
	if err != nil {
		writeError(c, consts.StatusBadRequest, err)
		return
	}
	if err := h.executions.DeleteExecution(ctx, identity.UserID, executionID); err != nil {
		writeServiceError(c, err)
		return
	}
	c.Status(consts.StatusNoContent)
}

func (h *Handler) executionOverview(ctx context.Context, c *app.RequestContext) {
	identity, err := currentActor(c)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	projectID, err := optionalPositiveQueryID(c.Query("project_id"))
	if err != nil {
		writeError(c, consts.StatusBadRequest, err)
		return
	}
	caseID, err := optionalPositiveQueryID(c.Query("case_id"))
	if err != nil {
		writeError(c, consts.StatusBadRequest, err)
		return
	}
	scope := c.Query("scope_type")
	if scope != "" && scope != "global" && scope != "project" && scope != "case" {
		writeError(c, consts.StatusBadRequest, execution.ErrAccessDenied)
		return
	}
	result, err := h.executions.Overview(ctx, identity.UserID, execution.OverviewRequest{
		ScopeType: scope, ProjectID: projectID, CaseID: caseID,
		WindowDays:         positiveQueryInt(c.Query("window_days"), 7),
		FailureFingerprint: c.Query("failure_fingerprint"),
	})
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(consts.StatusOK, result)
}

func optionalPositiveQueryID(raw string) (*int64, error) {
	if raw == "" {
		return nil, nil
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value < 1 {
		return nil, execution.ErrNotFound
	}
	return &value, nil
}

func nonNegativeQueryInt(raw string) int {
	value, err := strconv.Atoi(raw)
	if err != nil || value < 0 {
		return 0
	}
	return value
}
