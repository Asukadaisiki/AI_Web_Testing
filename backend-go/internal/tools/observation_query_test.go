package tools

import (
	"context"
	"encoding/json"
	"strconv"
	"testing"

	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/agentservice"
	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/groundingplan"
	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/taskplan"
)

func TestObservationQueryToolReturnsReferencesAndRecordsGroundingCandidates(
	t *testing.T,
) {
	ctx := context.Background()
	runService := agentservice.NewService(agentservice.NewMemoryRepository())
	run, eventSeq := observationQueryRun(t, runService)
	taskPlans := taskplan.NewService(taskplan.NewMemoryRepository())
	plan := createObservationTaskPlan(t, taskPlans, run.ID)
	groundingPlans := groundingplan.NewService(groundingplan.NewMemoryRepository())
	tool := NewObservationQueryTool(
		groundingplan.NewObservationReader(runService),
		taskPlans,
		groundingPlans,
	)

	result, err := tool.Execute(ctx, Call{
		RunID: run.ID,
		Name:  "query_observation",
		Arguments: json.RawMessage(`{
			"schema_version":"grounding.observation-query.v1",
			"plan_step_id":"submit",
			"source_event_seq":` + jsonInt(eventSeq) + `,
			"observation_id":"obs-query",
			"action":"click",
			"query":"submit_search",
			"role":"button",
			"limit":20
		}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	var queryResult groundingplan.ObservationQueryResult
	if err := json.Unmarshal(result.Content, &queryResult); err != nil {
		t.Fatal(err)
	}
	if queryResult.SchemaVersion != groundingplan.ObservationQueryResultVersion ||
		queryResult.PlanStepID != "submit" ||
		queryResult.SourceEventSeq != eventSeq ||
		len(queryResult.Matches) != 1 ||
		queryResult.Matches[0].CandidateRef.CandidateID != "candidate-submit" ||
		queryResult.Matches[0].DOM.Attrs["id"] != "submit_search" {
		t.Fatalf("query result = %#v", queryResult)
	}

	current, err := groundingPlans.EnsureForTaskPlan(ctx, plan)
	if err != nil {
		t.Fatal(err)
	}
	if current.Revision != 2 ||
		current.CurrentPlanStepID != "submit" ||
		current.Steps[0].Status != groundingplan.StepCandidatesAvailable ||
		len(current.Steps[0].ObservationQueries) != 1 ||
		len(current.Steps[0].CandidateRefs) != 1 ||
		current.Steps[0].CandidateRefs[0].CandidateRef.CandidateID !=
			"candidate-submit" {
		t.Fatalf("grounding plan = %#v", current)
	}
}

func TestObservationQueryToolAuthorizesCurrentPendingStepAndAction(t *testing.T) {
	ctx := context.Background()
	runService := agentservice.NewService(agentservice.NewMemoryRepository())
	run, eventSeq := observationQueryRun(t, runService)
	taskPlans := taskplan.NewService(taskplan.NewMemoryRepository())
	createObservationTaskPlan(t, taskPlans, run.ID)

	tests := []struct {
		name      string
		arguments string
	}{
		{
			name: "later plan step",
			arguments: `{
				"schema_version":"grounding.observation-query.v1",
				"plan_step_id":"enter_email",
				"source_event_seq":` + jsonInt(eventSeq) + `,
				"action":"input"
			}`,
		},
		{
			name: "mismatched action",
			arguments: `{
				"schema_version":"grounding.observation-query.v1",
				"plan_step_id":"submit",
				"source_event_seq":` + jsonInt(eventSeq) + `,
				"action":"input"
			}`,
		},
		{
			name: "model supplied selector",
			arguments: `{
				"schema_version":"grounding.observation-query.v1",
				"plan_step_id":"submit",
				"source_event_seq":` + jsonInt(eventSeq) + `,
				"action":"click",
				"selector":"#submit_search"
			}`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			taskPlan := mustCurrentTaskPlan(t, taskPlans, run.ID)
			groundingPlans := groundingplan.NewService(
				groundingplan.NewMemoryRepository(),
			)
			if _, err := groundingPlans.EnsureForTaskPlan(ctx, taskPlan); err != nil {
				t.Fatal(err)
			}
			tool := NewObservationQueryTool(
				groundingplan.NewObservationReader(runService),
				taskPlans,
				groundingPlans,
			)
			if _, err := tool.Execute(ctx, Call{
				RunID:     run.ID,
				Name:      "query_observation",
				Arguments: json.RawMessage(test.arguments),
			}); err == nil {
				t.Fatal("unauthorized observation query succeeded")
			}
			current, err := groundingPlans.EnsureForTaskPlan(ctx, taskPlan)
			if err != nil {
				t.Fatal(err)
			}
			if current.Revision != 1 ||
				current.Steps[0].Status != groundingplan.StepPending {
				t.Fatalf("grounding plan mutated after rejection: %#v", current)
			}
		})
	}
}

func TestObservationQueryToolDefinitionIsStructuredAndHasNoSelector(t *testing.T) {
	definition := NewObservationQueryTool(nil, nil, nil).Definition()
	if definition.Name != "query_observation" {
		t.Fatalf("tool name = %q", definition.Name)
	}
	var schema struct {
		AdditionalProperties bool                       `json:"additionalProperties"`
		Properties           map[string]json.RawMessage `json:"properties"`
		Required             []string                   `json:"required"`
	}
	if err := json.Unmarshal(definition.InputSchema, &schema); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{
		"schema_version",
		"plan_step_id",
		"source_event_seq",
		"action",
	} {
		if !containsString(schema.Required, field) {
			t.Fatalf("required fields = %#v, missing %q", schema.Required, field)
		}
	}
	if schema.AdditionalProperties ||
		schema.Properties["selector"] != nil ||
		schema.Properties["locator"] != nil {
		t.Fatalf("unsafe observation query schema = %s", definition.InputSchema)
	}
}

func observationQueryRun(
	t *testing.T,
	service *agentservice.Service,
) (agentservice.AgentRun, int64) {
	t.Helper()
	run, err := service.StartRun(
		context.Background(),
		"conversation-query",
		"Submit the form",
	)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(
		[]byte(observationQueryToolResultFixture),
		&payload,
	); err != nil {
		t.Fatal(err)
	}
	event, err := service.RecordEvent(
		context.Background(),
		run,
		agentservice.Event{
			Type:    agentservice.EventToolResult,
			Payload: payload,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	return run, event.Seq
}

func createObservationTaskPlan(
	t *testing.T,
	service *taskplan.Service,
	runID string,
) taskplan.Plan {
	t.Helper()
	plan, err := service.CreateVersion(context.Background(), taskplan.CreateRequest{
		RunID: runID,
		Definition: taskplan.Definition{
			Goal:             "Submit the form",
			MaxSideEffect:    taskplan.SideEffectBrowserState,
			ForbiddenActions: []string{},
			Steps: []taskplan.StepDefinition{
				{
					ID: "submit", Intent: "Submit the form", Action: "click",
					Target: "Submit", ExpectedOccurrences: 1,
					Idempotency:   "idempotent",
					SideEffect:    taskplan.SideEffectBrowserState,
					Preconditions: []string{},
					CompletionConditions: []string{
						"form submitted",
					},
				},
				{
					ID: "enter_email", Intent: "Enter email", Action: "input",
					Target: "Email", Value: "qa@example.test",
					ExpectedOccurrences: 1, Idempotency: "idempotent",
					SideEffect: taskplan.SideEffectBrowserState,
					Preconditions: []string{
						"form visible",
					},
					CompletionConditions: []string{
						"email entered",
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

func mustCurrentTaskPlan(
	t *testing.T,
	service *taskplan.Service,
	runID string,
) taskplan.Plan {
	t.Helper()
	plan, err := service.GetCurrent(context.Background(), runID)
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

func jsonInt(value int64) string {
	return strconv.FormatInt(value, 10)
}

func containsString(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

const observationQueryToolResultFixture = `{
	"schema_version":"agent.tool_result.v1",
	"tool":"explore_page",
	"content":{
		"probe_id":"probe-query",
		"url":"https://example.test/form",
		"element_count":1,
		"observation_v2":{
			"schema_version":"browser.observation.v2",
			"probe_id":"probe-query",
			"observation_id":"obs-query",
			"page_state":{
				"state_id":"form",
				"revision":1,
				"url":"https://example.test/form",
				"title":"Form",
				"state_sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
				"previous_state_sha256":null
			},
			"elements":[{
				"element_ref":"form:7",
				"context_path":{"frames":[],"shadow_hosts":[]},
				"a11y":{"role":"button","name":"","states":{"focusable":true,"disabled":false},"relations":{}},
				"dom":{"backend_node_id":7,"tag":"button","attrs":{"id":"submit_search","type":"button"},"text":""},
				"runtime":{"connected":true,"visible":true,"enabled":true,"editable":false},
				"locators":[{
					"candidate_id":"candidate-submit",
					"locator":{"kind":"css","value":"#submit_search","exact":true},
					"provenance":"a11y_backend_dom_node",
					"observed_count":1
				}]
			}],
			"relations":[]
		}
	},
	"content_sha256":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
	"content_bytes":1024
}`
