package httptransport

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"strings"

	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/research"
	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
	"github.com/cloudwego/hertz/pkg/route"
)

type ResearchAPI interface {
	CreateExperiment(context.Context, int64, research.Experiment) (research.Experiment, error)
	GetExperiment(context.Context, int64, int64, string) (research.Experiment, error)
	ListExperiments(
		context.Context,
		int64,
		research.ExperimentFilter,
	) ([]research.Experiment, error)
	StartExperiment(context.Context, int64, int64, string) (research.ExperimentStart, error)
	ListExperimentRuns(context.Context, int64, int64, string) ([]research.ScheduledRun, error)
	GetRun(context.Context, int64, int64, string) (research.ResearchRun, error)
	StartRun(context.Context, int64, int64, string) (research.RunStart, error)
	UpdateRunLinks(
		context.Context,
		int64,
		int64,
		string,
		research.RunLinks,
	) (research.ResearchRun, error)
	PutOracle(
		context.Context,
		int64,
		int64,
		string,
		int64,
		research.OracleDecision,
	) (research.OracleResult, error)
	GetOracle(context.Context, int64, int64, string) (research.OracleResult, error)
	ProjectMetrics(context.Context, int64, int64, string) (research.ResearchRun, error)
	FinishRun(
		context.Context,
		int64,
		int64,
		string,
		research.RunStatus,
	) (research.ResearchRun, error)
	CancelRun(context.Context, int64, int64, string) (research.ResearchRun, error)
}

func registerResearchRoutes(group *route.RouterGroup, handler *Handler) {
	group.POST("/research/experiments", handler.createResearchExperiment)
	group.GET("/research/experiments", handler.listResearchExperiments)
	group.GET("/research/experiments/:experiment_id", handler.getResearchExperiment)
	group.POST("/research/experiments/:experiment_id/start", handler.startResearchExperiment)
	group.GET("/research/experiments/:experiment_id/runs", handler.listResearchRuns)
	group.GET("/research/runs/:run_id", handler.getResearchRun)
	group.POST("/research/runs/:run_id/start", handler.startResearchRun)
	group.PUT("/research/runs/:run_id/links", handler.putResearchRunLinks)
	group.PUT("/research/runs/:run_id/oracle", handler.putResearchRunOracle)
	group.GET("/research/runs/:run_id/oracle", handler.getResearchRunOracle)
	group.POST("/research/runs/:run_id/project-metrics", handler.projectResearchRunMetrics)
	group.POST("/research/runs/:run_id/finish", handler.finishResearchRun)
	group.POST("/research/runs/:run_id/cancel", handler.cancelResearchRun)
}

func (h *Handler) createResearchExperiment(
	ctx context.Context,
	c *app.RequestContext,
) {
	identity, ok := researchActor(c)
	if !ok {
		return
	}
	var request research.Experiment
	if err := bindStrictJSON(c, &request); err != nil {
		writeError(c, consts.StatusBadRequest, err)
		return
	}
	result, err := h.research.CreateExperiment(ctx, identity.UserID, request)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(consts.StatusCreated, result)
}

func (h *Handler) listResearchExperiments(
	ctx context.Context,
	c *app.RequestContext,
) {
	identity, projectID, ok := researchQueryContext(c)
	if !ok {
		return
	}
	filter := research.ExperimentFilter{
		ProjectID: &projectID,
		Limit:     positiveQueryDefault(c.Query("limit"), 100),
		Offset:    nonNegativeQueryDefault(c.Query("offset"), 0),
	}
	if filter.Limit < 0 || filter.Offset < 0 {
		writeError(c, consts.StatusBadRequest, errors.New("invalid pagination"))
		return
	}
	if value := strings.TrimSpace(c.Query("status")); value != "" {
		status := research.ExperimentStatus(value)
		filter.Status = &status
	}
	if value := strings.TrimSpace(c.Query("variant")); value != "" {
		filter.Variant = &value
	}
	result, err := h.research.ListExperiments(ctx, identity.UserID, filter)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(consts.StatusOK, result)
}

func (h *Handler) getResearchExperiment(
	ctx context.Context,
	c *app.RequestContext,
) {
	identity, projectID, ok := researchQueryContext(c)
	if !ok {
		return
	}
	result, err := h.research.GetExperiment(
		ctx, identity.UserID, projectID, c.Param("experiment_id"),
	)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(consts.StatusOK, result)
}

func (h *Handler) startResearchExperiment(
	ctx context.Context,
	c *app.RequestContext,
) {
	identity, projectID, ok := researchBodyContext(c)
	if !ok {
		return
	}
	result, err := h.research.StartExperiment(
		ctx, identity.UserID, projectID, c.Param("experiment_id"),
	)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(consts.StatusOK, result)
}

func (h *Handler) listResearchRuns(
	ctx context.Context,
	c *app.RequestContext,
) {
	identity, projectID, ok := researchQueryContext(c)
	if !ok {
		return
	}
	result, err := h.research.ListExperimentRuns(
		ctx, identity.UserID, projectID, c.Param("experiment_id"),
	)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(consts.StatusOK, result)
}

func (h *Handler) getResearchRun(ctx context.Context, c *app.RequestContext) {
	identity, projectID, ok := researchQueryContext(c)
	if !ok {
		return
	}
	result, err := h.research.GetRun(
		ctx, identity.UserID, projectID, c.Param("run_id"),
	)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(consts.StatusOK, result)
}

func (h *Handler) startResearchRun(ctx context.Context, c *app.RequestContext) {
	identity, projectID, ok := researchBodyContext(c)
	if !ok {
		return
	}
	result, err := h.research.StartRun(
		ctx, identity.UserID, projectID, c.Param("run_id"),
	)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(consts.StatusOK, result)
}

type researchLinksRequest struct {
	ProjectID int64             `json:"project_id"`
	Links     research.RunLinks `json:"links"`
}

func (h *Handler) putResearchRunLinks(
	ctx context.Context,
	c *app.RequestContext,
) {
	identity, ok := researchActor(c)
	if !ok {
		return
	}
	var request researchLinksRequest
	if err := bindStrictJSON(c, &request); err != nil || request.ProjectID <= 0 {
		if err == nil {
			err = errors.New("project_id must be positive")
		}
		writeError(c, consts.StatusBadRequest, err)
		return
	}
	result, err := h.research.UpdateRunLinks(
		ctx,
		identity.UserID,
		request.ProjectID,
		c.Param("run_id"),
		request.Links,
	)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(consts.StatusOK, result)
}

type researchOracleRequest struct {
	ProjectID   int64                   `json:"project_id"`
	ExecutionID int64                   `json:"execution_id"`
	Decision    research.OracleDecision `json:"decision"`
}

func (h *Handler) putResearchRunOracle(
	ctx context.Context,
	c *app.RequestContext,
) {
	identity, ok := researchActor(c)
	if !ok {
		return
	}
	var request researchOracleRequest
	if err := bindStrictJSON(c, &request); err != nil ||
		request.ProjectID <= 0 ||
		request.ExecutionID <= 0 {
		if err == nil {
			err = errors.New("project_id and execution_id must be positive")
		}
		writeError(c, consts.StatusBadRequest, err)
		return
	}
	result, err := h.research.PutOracle(
		ctx,
		identity.UserID,
		request.ProjectID,
		c.Param("run_id"),
		request.ExecutionID,
		request.Decision,
	)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(consts.StatusOK, result)
}

func (h *Handler) getResearchRunOracle(
	ctx context.Context,
	c *app.RequestContext,
) {
	identity, projectID, ok := researchQueryContext(c)
	if !ok {
		return
	}
	result, err := h.research.GetOracle(
		ctx, identity.UserID, projectID, c.Param("run_id"),
	)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(consts.StatusOK, result)
}

func (h *Handler) projectResearchRunMetrics(
	ctx context.Context,
	c *app.RequestContext,
) {
	identity, projectID, ok := researchBodyContext(c)
	if !ok {
		return
	}
	result, err := h.research.ProjectMetrics(
		ctx, identity.UserID, projectID, c.Param("run_id"),
	)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(consts.StatusOK, result)
}

type researchFinishRequest struct {
	ProjectID int64              `json:"project_id"`
	Status    research.RunStatus `json:"status"`
}

func (h *Handler) finishResearchRun(
	ctx context.Context,
	c *app.RequestContext,
) {
	identity, ok := researchActor(c)
	if !ok {
		return
	}
	var request researchFinishRequest
	if err := bindStrictJSON(c, &request); err != nil || request.ProjectID <= 0 {
		if err == nil {
			err = errors.New("project_id must be positive")
		}
		writeError(c, consts.StatusBadRequest, err)
		return
	}
	result, err := h.research.FinishRun(
		ctx,
		identity.UserID,
		request.ProjectID,
		c.Param("run_id"),
		request.Status,
	)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(consts.StatusOK, result)
}

func (h *Handler) cancelResearchRun(
	ctx context.Context,
	c *app.RequestContext,
) {
	identity, projectID, ok := researchBodyContext(c)
	if !ok {
		return
	}
	result, err := h.research.CancelRun(
		ctx, identity.UserID, projectID, c.Param("run_id"),
	)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(consts.StatusOK, result)
}

type researchProjectRequest struct {
	ProjectID int64 `json:"project_id"`
}

func researchBodyContext(
	c *app.RequestContext,
) (actorContext, int64, bool) {
	identity, ok := researchActor(c)
	if !ok {
		return actorContext{}, 0, false
	}
	var request researchProjectRequest
	if err := bindStrictJSON(c, &request); err != nil {
		writeError(c, consts.StatusBadRequest, err)
		return actorContext{}, 0, false
	}
	if request.ProjectID <= 0 {
		writeError(c, consts.StatusBadRequest, errors.New("project_id must be positive"))
		return actorContext{}, 0, false
	}
	return identity, request.ProjectID, true
}

func researchQueryContext(
	c *app.RequestContext,
) (actorContext, int64, bool) {
	identity, ok := researchActor(c)
	if !ok {
		return actorContext{}, 0, false
	}
	projectID, err := strconv.ParseInt(strings.TrimSpace(c.Query("project_id")), 10, 64)
	if err != nil || projectID <= 0 {
		writeError(c, consts.StatusBadRequest, errors.New("project_id must be positive"))
		return actorContext{}, 0, false
	}
	return identity, projectID, true
}

func researchActor(c *app.RequestContext) (actorContext, bool) {
	identity, err := currentActor(c)
	if err != nil {
		writeServiceError(c, err)
		return actorContext{}, false
	}
	return identity, true
}

func bindStrictJSON(c *app.RequestContext, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(c.Request.Body()))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("request body must contain one JSON value")
	}
	return nil
}

func positiveQueryDefault(raw string, fallback int) int {
	if strings.TrimSpace(raw) == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return -1
	}
	return value
}

func nonNegativeQueryDefault(raw string, fallback int) int {
	if strings.TrimSpace(raw) == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 0 {
		return -1
	}
	return value
}
