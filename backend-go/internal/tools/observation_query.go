package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/groundingplan"
	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/taskplan"
)

type ObservationQueryTool struct {
	reader         *groundingplan.ObservationReader
	taskPlans      *taskplan.Service
	groundingPlans *groundingplan.Service
}

func NewObservationQueryTool(
	reader *groundingplan.ObservationReader,
	taskPlans *taskplan.Service,
	groundingPlans *groundingplan.Service,
) ObservationQueryTool {
	return ObservationQueryTool{
		reader:         reader,
		taskPlans:      taskPlans,
		groundingPlans: groundingPlans,
	}
}

func (t ObservationQueryTool) Definition() Definition {
	return Definition{
		Name: "query_observation",
		Description: "Search candidates from one complete persisted exploration observation in the current run. " +
			"The plan_step_id and action must match the next pending TaskPlan step. " +
			"Use query, role, observation_id, page_state_id, or candidate_id as filters; " +
			"the result returns reusable candidate references and never accepts selectors.",
		InputSchema: json.RawMessage(`{
			"type":"object",
			"additionalProperties":false,
			"properties":{
				"schema_version":{"const":"grounding.observation-query.v1"},
				"plan_step_id":{"type":"string","minLength":1},
				"source_event_seq":{"type":"integer","minimum":1},
				"observation_id":{"type":"string","minLength":1},
				"page_state_id":{"type":"string","minLength":1},
				"candidate_id":{"type":"string","minLength":1},
				"action":{"type":"string","enum":["click","input","wait_for"]},
				"query":{"type":"string","minLength":1},
				"role":{"type":"string","minLength":1},
				"limit":{"type":"integer","minimum":1,"maximum":100,"default":20}
			},
			"required":["schema_version","plan_step_id","source_event_seq","action"]
		}`),
	}
}

func (t ObservationQueryTool) Execute(
	ctx context.Context,
	call Call,
) (Result, error) {
	if t.reader == nil || t.taskPlans == nil || t.groundingPlans == nil {
		return Result{}, errors.New("observation query dependencies are unavailable")
	}
	var query groundingplan.ObservationQuery
	decoder := json.NewDecoder(bytes.NewReader(call.Arguments))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&query); err != nil {
		return Result{}, fmt.Errorf("decode observation query: %w", err)
	}
	if err := ensureJSONEnd(decoder); err != nil {
		return Result{}, err
	}

	plan, err := t.taskPlans.GetCurrent(ctx, call.RunID)
	if err != nil {
		return Result{}, err
	}
	step, err := currentPendingTaskPlanStep(plan)
	if err != nil {
		return Result{}, err
	}
	if query.PlanStepID != step.ID || query.Action != step.Action {
		return Result{}, errors.New(
			"observation query does not match the current pending task plan step",
		)
	}
	queryResult, err := t.reader.Query(ctx, call.RunID, query)
	if err != nil {
		return Result{}, err
	}
	if len(queryResult.Matches) == 0 {
		return Result{}, errors.New("observation query returned no candidates")
	}
	if _, ensureErr := t.groundingPlans.EnsureForTaskPlan(ctx, plan); ensureErr != nil {
		return Result{}, ensureErr
	}
	if _, recordErr := t.groundingPlans.RecordObservationQuery(
		ctx,
		call.RunID,
		groundingplan.QueryRecord{
			PlanStepID:     query.PlanStepID,
			SourceEventSeq: query.SourceEventSeq,
			ObservationID:  query.ObservationID,
			Action:         query.Action,
			Query:          query.Query,
			Role:           query.Role,
			Limit:          query.Limit,
		},
		groundingCandidates(queryResult.Matches),
	); recordErr != nil {
		return Result{}, recordErr
	}
	content, err := json.Marshal(queryResult)
	if err != nil {
		return Result{}, err
	}
	return Result{Content: content}, nil
}

func groundingCandidates(
	matches []groundingplan.ObservationQueryMatch,
) []groundingplan.CandidateOption {
	result := make([]groundingplan.CandidateOption, len(matches))
	for index, match := range matches {
		result[index] = groundingplan.CandidateOption{
			CandidateRef: match.CandidateRef,
			ElementRef:   match.ElementRef,
			Role:         match.Role,
			Name:         match.Name,
			DOM: groundingplan.CandidateDOM{
				Tag:   match.DOM.Tag,
				Attrs: match.DOM.Attrs,
			},
			Locator:       match.Locator,
			Provenance:    match.Provenance,
			ObservedCount: match.ObservedCount,
		}
	}
	return result
}

func currentPendingTaskPlanStep(plan taskplan.Plan) (taskplan.Step, error) {
	if plan.Status != taskplan.StatusGrounding {
		return taskplan.Step{}, fmt.Errorf(
			"query_observation requires task plan status %q, got %q",
			taskplan.StatusGrounding,
			plan.Status,
		)
	}
	for _, step := range plan.Steps {
		if step.Status == taskplan.StepGrounded {
			continue
		}
		if step.Status != taskplan.StepPending {
			return taskplan.Step{}, errors.New(
				"next task plan step is not pending",
			)
		}
		return step, nil
	}
	return taskplan.Step{}, errors.New("task plan has no pending step")
}

func ensureJSONEnd(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); errors.Is(err, io.EOF) {
		return nil
	} else if err != nil {
		return fmt.Errorf("decode observation query: %w", err)
	}
	return errors.New("observation query contains multiple JSON values")
}
