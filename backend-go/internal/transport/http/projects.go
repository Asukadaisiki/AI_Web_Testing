package httptransport

import (
	"context"
	"strconv"

	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/projects"
	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

func (h *Handler) listProjects(ctx context.Context, c *app.RequestContext) {
	identity, err := currentActor(c)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	items, err := h.projects.List(ctx, identity.UserID)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(consts.StatusOK, items)
}

func (h *Handler) createProject(ctx context.Context, c *app.RequestContext) {
	identity, err := currentActor(c)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	var request projects.CreateRequest
	if err := c.BindAndValidate(&request); err != nil {
		writeError(c, consts.StatusBadRequest, err)
		return
	}
	project, err := h.projects.Create(ctx, identity.UserID, request)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.Header("Location", "/api/v2/projects/"+strconv.FormatInt(project.ID, 10))
	c.JSON(consts.StatusCreated, project)
}

func (h *Handler) getProject(ctx context.Context, c *app.RequestContext) {
	identity, projectID, err := ownedPathContext(c, "project_id")
	if err != nil {
		writeError(c, consts.StatusBadRequest, err)
		return
	}
	project, err := h.projects.Get(ctx, projectID, identity.UserID)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(consts.StatusOK, project)
}

func (h *Handler) updateProject(ctx context.Context, c *app.RequestContext) {
	identity, projectID, err := ownedPathContext(c, "project_id")
	if err != nil {
		writeError(c, consts.StatusBadRequest, err)
		return
	}
	var request projects.UpdateRequest
	if err := c.BindAndValidate(&request); err != nil {
		writeError(c, consts.StatusBadRequest, err)
		return
	}
	project, err := h.projects.Update(ctx, projectID, identity.UserID, request)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(consts.StatusOK, project)
}

func (h *Handler) deleteProject(ctx context.Context, c *app.RequestContext) {
	identity, projectID, err := ownedPathContext(c, "project_id")
	if err != nil {
		writeError(c, consts.StatusBadRequest, err)
		return
	}
	if err := h.projects.Delete(ctx, projectID, identity.UserID); err != nil {
		writeServiceError(c, err)
		return
	}
	c.Status(consts.StatusNoContent)
}

func ownedPathContext(c *app.RequestContext, name string) (actorContext, int64, error) {
	identity, err := currentActor(c)
	if err != nil {
		return actorContext{}, 0, err
	}
	value, err := positivePathID(c.Param(name))
	return actorContext{UserID: identity.UserID}, value, err
}
