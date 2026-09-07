package httptransport

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/research"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

type researchAPIStub struct {
	err       error
	operation string
	actorID   int64
	projectID int64
}

func (s *researchAPIStub) record(operation string, actorID, projectID int64) {
	s.operation = operation
	s.actorID = actorID
	s.projectID = projectID
}

func (s *researchAPIStub) CreateExperiment(
	_ context.Context,
	actorID int64,
	experiment research.Experiment,
) (research.Experiment, error) {
	s.record("create_experiment", actorID, experiment.ProjectID)
	return experiment, s.err
}

func (s *researchAPIStub) GetExperiment(
	_ context.Context,
	actorID int64,
	projectID int64,
	experimentID string,
) (research.Experiment, error) {
	s.record("get_experiment", actorID, projectID)
	return research.Experiment{ID: experimentID, ProjectID: projectID}, s.err
}

func (s *researchAPIStub) ListExperiments(
	_ context.Context,
	actorID int64,
	filter research.ExperimentFilter,
) ([]research.Experiment, error) {
	s.record("list_experiments", actorID, *filter.ProjectID)
	return []research.Experiment{}, s.err
}

func (s *researchAPIStub) StartExperiment(
	_ context.Context,
	actorID int64,
	projectID int64,
	experimentID string,
) (research.ExperimentStart, error) {
	s.record("start_experiment", actorID, projectID)
	return research.ExperimentStart{
		Experiment: research.Experiment{ID: experimentID, ProjectID: projectID},
		Schedule:   []research.ScheduledRun{},
	}, s.err
}

func (s *researchAPIStub) ListExperimentRuns(
	_ context.Context,
	actorID int64,
	projectID int64,
	_ string,
) ([]research.ScheduledRun, error) {
	s.record("list_runs", actorID, projectID)
	return []research.ScheduledRun{}, s.err
}

func (s *researchAPIStub) GetRun(
	_ context.Context,
	actorID int64,
	projectID int64,
	runID string,
) (research.ResearchRun, error) {
	s.record("get_run", actorID, projectID)
	return research.ResearchRun{ID: runID, ProjectID: projectID}, s.err
}

func (s *researchAPIStub) StartRun(
	_ context.Context,
	actorID int64,
	projectID int64,
	runID string,
) (research.RunStart, error) {
	s.record("start_run", actorID, projectID)
	return research.RunStart{
		Run:      research.ResearchRun{ID: runID, ProjectID: projectID},
		Deadline: time.Unix(120, 0).UTC(),
	}, s.err
}

func (s *researchAPIStub) UpdateRunLinks(
	_ context.Context,
	actorID int64,
	projectID int64,
	runID string,
	_ research.RunLinks,
) (research.ResearchRun, error) {
	s.record("put_links", actorID, projectID)
	return research.ResearchRun{ID: runID, ProjectID: projectID}, s.err
}

func (s *researchAPIStub) PutOracle(
	_ context.Context,
	actorID int64,
	projectID int64,
	runID string,
	executionID int64,
	decision research.OracleDecision,
) (research.OracleResult, error) {
	s.record("put_oracle", actorID, projectID)
	return research.OracleResult{
		ResearchRunID: runID,
		ExecutionID:   executionID,
		Decision:      decision,
	}, s.err
}

func (s *researchAPIStub) GetOracle(
	_ context.Context,
	actorID int64,
	projectID int64,
	runID string,
) (research.OracleResult, error) {
	s.record("get_oracle", actorID, projectID)
	return research.OracleResult{ResearchRunID: runID}, s.err
}

func (s *researchAPIStub) ProjectMetrics(
	_ context.Context,
	actorID int64,
	projectID int64,
	runID string,
) (research.ResearchRun, error) {
	s.record("project_metrics", actorID, projectID)
	return research.ResearchRun{ID: runID, ProjectID: projectID}, s.err
}

func (s *researchAPIStub) FinishRun(
	_ context.Context,
	actorID int64,
	projectID int64,
	runID string,
	_ research.RunStatus,
) (research.ResearchRun, error) {
	s.record("finish_run", actorID, projectID)
	return research.ResearchRun{ID: runID, ProjectID: projectID}, s.err
}

func (s *researchAPIStub) CancelRun(
	_ context.Context,
	actorID int64,
	projectID int64,
	runID string,
) (research.ResearchRun, error) {
	s.record("cancel_run", actorID, projectID)
	return research.ResearchRun{ID: runID, ProjectID: projectID}, s.err
}

func newResearchHTTPTestServer(
	t *testing.T,
	api ResearchAPI,
) *server.Hertz {
	t.Helper()
	return NewServer(
		"127.0.0.1:0",
		newTestServer(t),
		7,
		staticPlanningStore{},
		nil,
		nil,
		nil,
		nil,
		api,
	)
}

func TestResearchRoutesExposeControlPlaneContract(t *testing.T) {
	tests := []struct {
		name      string
		method    string
		path      string
		body      string
		operation string
		status    int
	}{
		{
			"create experiment",
			"POST",
			"/api/v2/research/experiments",
			`{"project_id":42}`,
			"create_experiment",
			consts.StatusCreated,
		},
		{
			"list experiments",
			"GET",
			"/api/v2/research/experiments?project_id=42",
			"",
			"list_experiments",
			consts.StatusOK,
		},
		{
			"get experiment",
			"GET",
			"/api/v2/research/experiments/exp-1?project_id=42",
			"",
			"get_experiment",
			consts.StatusOK,
		},
		{
			"start experiment",
			"POST",
			"/api/v2/research/experiments/exp-1/start",
			`{"project_id":42}`,
			"start_experiment",
			consts.StatusOK,
		},
		{
			"list runs",
			"GET",
			"/api/v2/research/experiments/exp-1/runs?project_id=42",
			"",
			"list_runs",
			consts.StatusOK,
		},
		{
			"get run",
			"GET",
			"/api/v2/research/runs/run-1?project_id=42",
			"",
			"get_run",
			consts.StatusOK,
		},
		{
			"start run",
			"POST",
			"/api/v2/research/runs/run-1/start",
			`{"project_id":42}`,
			"start_run",
			consts.StatusOK,
		},
		{
			"put links",
			"PUT",
			"/api/v2/research/runs/run-1/links",
			`{"project_id":42,"links":{}}`,
			"put_links",
			consts.StatusOK,
		},
		{
			"put oracle",
			"PUT",
			"/api/v2/research/runs/run-1/oracle",
			`{"project_id":42,"execution_id":9,"decision":{}}`,
			"put_oracle",
			consts.StatusOK,
		},
		{
			"get oracle",
			"GET",
			"/api/v2/research/runs/run-1/oracle?project_id=42",
			"",
			"get_oracle",
			consts.StatusOK,
		},
		{
			"project metrics",
			"POST",
			"/api/v2/research/runs/run-1/project-metrics",
			`{"project_id":42}`,
			"project_metrics",
			consts.StatusOK,
		},
		{
			"finish run",
			"POST",
			"/api/v2/research/runs/run-1/finish",
			`{"project_id":42,"status":"completed"}`,
			"finish_run",
			consts.StatusOK,
		},
		{
			"cancel run",
			"POST",
			"/api/v2/research/runs/run-1/cancel",
			`{"project_id":42}`,
			"cancel_run",
			consts.StatusOK,
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			api := &researchAPIStub{}
			server := newResearchHTTPTestServer(t, api)
			var body *ut.Body
			if testCase.body != "" {
				raw := []byte(testCase.body)
				body = &ut.Body{Body: bytes.NewReader(raw), Len: len(raw)}
			}
			response := ut.PerformRequest(
				server.Engine,
				testCase.method,
				testCase.path,
				body,
				ut.Header{Key: "Content-Type", Value: "application/json"},
			).Result()
			if response.StatusCode() != testCase.status {
				t.Fatalf(
					"status = %d, want %d; body = %s",
					response.StatusCode(), testCase.status, response.Body(),
				)
			}
			if api.operation != testCase.operation ||
				api.actorID != 7 ||
				api.projectID != 42 {
				t.Fatalf(
					"call = %s actor=%d project=%d",
					api.operation, api.actorID, api.projectID,
				)
			}
		})
	}
}

func TestResearchRoutesValidateInputAndMapErrors(t *testing.T) {
	api := &researchAPIStub{}
	server := newResearchHTTPTestServer(t, api)
	response := ut.PerformRequest(
		server.Engine,
		"POST",
		"/api/v2/research/runs/run-1/start",
		&ut.Body{Body: bytes.NewReader([]byte(`{"project_id":0}`)), Len: 16},
		ut.Header{Key: "Content-Type", Value: "application/json"},
	).Result()
	if response.StatusCode() != consts.StatusBadRequest {
		t.Fatalf("invalid request status = %d, body = %s", response.StatusCode(), response.Body())
	}

	for _, testCase := range []struct {
		err    error
		status int
	}{
		{research.ErrInvalid, consts.StatusBadRequest},
		{research.ErrNotFound, consts.StatusNotFound},
		{research.ErrConflict, consts.StatusConflict},
		{research.ErrBrokenLink, consts.StatusConflict},
	} {
		api = &researchAPIStub{err: testCase.err}
		server = newResearchHTTPTestServer(t, api)
		response = ut.PerformRequest(
			server.Engine,
			"GET",
			"/api/v2/research/runs/run-1?project_id=42",
			nil,
		).Result()
		if response.StatusCode() != testCase.status {
			t.Fatalf(
				"error %v status = %d, want %d; body = %s",
				testCase.err, response.StatusCode(), testCase.status, response.Body(),
			)
		}
	}
}

func TestResearchRoutesRejectUnknownFields(t *testing.T) {
	api := &researchAPIStub{}
	server := newResearchHTTPTestServer(t, api)
	raw := []byte(`{"project_id":42,"unknown":true}`)
	response := ut.PerformRequest(
		server.Engine,
		"POST",
		"/api/v2/research/runs/run-1/start",
		&ut.Body{Body: bytes.NewReader(raw), Len: len(raw)},
		ut.Header{Key: "Content-Type", Value: "application/json"},
	).Result()
	if response.StatusCode() != consts.StatusBadRequest ||
		!errors.Is(api.err, nil) ||
		api.operation != "" {
		t.Fatalf("response = %d %s, operation = %q", response.StatusCode(), response.Body(), api.operation)
	}
}
