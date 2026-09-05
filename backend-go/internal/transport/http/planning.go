package httptransport

import (
	"context"
	"errors"
	"strconv"

	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/planning"
	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

func (h *Handler) createPlanningSession(ctx context.Context, c *app.RequestContext) {
	identity, err := currentActor(c)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	var request planning.CreateSessionRequest
	if err := c.BindAndValidate(&request); err != nil {
		writeError(c, consts.StatusBadRequest, err)
		return
	}
	detail, err := h.planning.CreateSession(ctx, identity.UserID, request)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.Header("Location", "/api/v2/planning/sessions/"+strconv.FormatInt(detail.Session.ID, 10))
	c.JSON(consts.StatusCreated, detail)
}

func (h *Handler) listPlanningSessions(ctx context.Context, c *app.RequestContext) {
	identity, err := currentActor(c)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	sessions, err := h.planning.ListSessions(ctx, identity.UserID)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(consts.StatusOK, sessions)
}

func (h *Handler) getPlanningSession(ctx context.Context, c *app.RequestContext) {
	identity, sessionID, err := planningRequestContext(c)
	if err != nil {
		writeError(c, consts.StatusBadRequest, err)
		return
	}
	detail, err := h.planning.GetSession(ctx, identity.UserID, sessionID)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(consts.StatusOK, detail)
}

func (h *Handler) updatePlanningSession(ctx context.Context, c *app.RequestContext) {
	identity, sessionID, err := planningRequestContext(c)
	if err != nil {
		writeError(c, consts.StatusBadRequest, err)
		return
	}
	var request planning.UpdateSessionRequest
	if err := c.BindAndValidate(&request); err != nil {
		writeError(c, consts.StatusBadRequest, err)
		return
	}
	detail, err := h.planning.UpdateSession(
		ctx,
		identity.UserID,
		sessionID,
		request,
	)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(consts.StatusOK, detail)
}

func (h *Handler) deletePlanningSession(ctx context.Context, c *app.RequestContext) {
	identity, sessionID, err := planningRequestContext(c)
	if err != nil {
		writeError(c, consts.StatusBadRequest, err)
		return
	}
	if err := h.planning.DeleteSession(ctx, identity.UserID, sessionID); err != nil {
		writeServiceError(c, err)
		return
	}
	c.Status(consts.StatusNoContent)
}

func (h *Handler) listPlanningProjects(ctx context.Context, c *app.RequestContext) {
	identity, sessionID, err := planningRequestContext(c)
	if err != nil {
		writeError(c, consts.StatusBadRequest, err)
		return
	}
	projects, err := h.planning.ListProjects(ctx, identity.UserID, sessionID)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(consts.StatusOK, projects)
}

func (h *Handler) linkPlanningProject(ctx context.Context, c *app.RequestContext) {
	identity, sessionID, err := planningRequestContext(c)
	if err != nil {
		writeError(c, consts.StatusBadRequest, err)
		return
	}
	var request planning.LinkProjectRequest
	if err := c.BindAndValidate(&request); err != nil {
		writeError(c, consts.StatusBadRequest, err)
		return
	}
	project, err := h.planning.LinkProject(
		ctx,
		identity.UserID,
		sessionID,
		request.ProjectID,
	)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(consts.StatusCreated, project)
}

func (h *Handler) unlinkPlanningProject(ctx context.Context, c *app.RequestContext) {
	identity, sessionID, err := planningRequestContext(c)
	if err != nil {
		writeError(c, consts.StatusBadRequest, err)
		return
	}
	projectID, err := positivePathID(c.Param("project_id"))
	if err != nil {
		writeError(c, consts.StatusBadRequest, err)
		return
	}
	if err := h.planning.UnlinkProject(
		ctx,
		identity.UserID,
		sessionID,
		projectID,
	); err != nil {
		writeServiceError(c, err)
		return
	}
	c.Status(consts.StatusNoContent)
}

func (h *Handler) createPlanningProject(ctx context.Context, c *app.RequestContext) {
	identity, sessionID, err := planningRequestContext(c)
	if err != nil {
		writeError(c, consts.StatusBadRequest, err)
		return
	}
	var request planning.CreateProjectRequest
	if err := c.BindAndValidate(&request); err != nil {
		writeError(c, consts.StatusBadRequest, err)
		return
	}
	project, err := h.planning.CreateProject(ctx, identity.UserID, sessionID, request)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(consts.StatusCreated, project)
}

func planningRequestContext(c *app.RequestContext) (actorContext, int64, error) {
	identity, err := currentActor(c)
	if err != nil {
		return actorContext{}, 0, err
	}
	sessionID, err := positivePathID(c.Param("session_id"))
	return actorContext{UserID: identity.UserID}, sessionID, err
}

type actorContext struct {
	UserID int64
}

func positivePathID(raw string) (int64, error) {
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value < 1 {
		return 0, errors.New("path id must be a positive integer")
	}
	return value, nil
}
