package httptransport

import (
	"context"
	"strconv"
	"strings"

	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/cases"
	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

func (h *Handler) listCases(ctx context.Context, c *app.RequestContext) {
	identity, err := currentActor(c)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	var projectID *int64
	if raw := strings.TrimSpace(c.Query("project_id")); raw != "" {
		value, parseErr := strconv.ParseInt(raw, 10, 64)
		if parseErr != nil || value < 1 {
			writeError(c, consts.StatusBadRequest, cases.ErrAccessDenied)
			return
		}
		projectID = &value
	}
	page := positiveQueryInt(c.Query("page"), 1)
	pageSize := positiveQueryInt(c.Query("page_size"), 20)
	if pageSize > 200 {
		pageSize = 200
	}
	result, err := h.cases.List(
		ctx, identity.UserID, projectID, strings.TrimSpace(c.Query("search")), page, pageSize,
	)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(consts.StatusOK, result)
}

func (h *Handler) getCase(ctx context.Context, c *app.RequestContext) {
	identity, caseID, err := ownedPathContext(c, "case_id")
	if err != nil {
		writeError(c, consts.StatusBadRequest, err)
		return
	}
	item, err := h.cases.Get(ctx, caseID, identity.UserID)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(consts.StatusOK, item)
}

func (h *Handler) createCase(ctx context.Context, c *app.RequestContext) {
	identity, err := currentActor(c)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	var request cases.Mutation
	if err := c.BindAndValidate(&request); err != nil {
		writeError(c, consts.StatusBadRequest, err)
		return
	}
	item, err := h.cases.Create(ctx, identity.UserID, request)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.Header("Location", "/api/v2/cases/"+strconv.FormatInt(item.ID, 10))
	c.JSON(consts.StatusCreated, item)
}

func (h *Handler) updateCase(ctx context.Context, c *app.RequestContext) {
	identity, caseID, err := ownedPathContext(c, "case_id")
	if err != nil {
		writeError(c, consts.StatusBadRequest, err)
		return
	}
	var request cases.Mutation
	if err := c.BindAndValidate(&request); err != nil {
		writeError(c, consts.StatusBadRequest, err)
		return
	}
	item, err := h.cases.Update(ctx, caseID, identity.UserID, request)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(consts.StatusOK, item)
}

func (h *Handler) deleteCase(ctx context.Context, c *app.RequestContext) {
	identity, caseID, err := ownedPathContext(c, "case_id")
	if err != nil {
		writeError(c, consts.StatusBadRequest, err)
		return
	}
	if err := h.cases.Delete(ctx, caseID, identity.UserID); err != nil {
		writeServiceError(c, err)
		return
	}
	c.Status(consts.StatusNoContent)
}

func (h *Handler) deleteCases(ctx context.Context, c *app.RequestContext) {
	identity, err := currentActor(c)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	var request struct {
		CaseIDs []int64 `json:"case_ids" vd:"len($)>0"`
	}
	if err := c.BindAndValidate(&request); err != nil {
		writeError(c, consts.StatusBadRequest, err)
		return
	}
	count, err := h.cases.DeleteBatch(ctx, identity.UserID, request.CaseIDs)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	if count == 0 {
		writeServiceError(c, cases.ErrNotFound)
		return
	}
	c.Status(consts.StatusNoContent)
}

func positiveQueryInt(raw string, fallback int) int {
	value, err := strconv.Atoi(raw)
	if err != nil || value < 1 {
		return fallback
	}
	return value
}
