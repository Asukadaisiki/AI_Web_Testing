package tools

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/taskplan"
)

type SetTaskPlanTool struct {
	plans *taskplan.Service
}

func NewSetTaskPlanTool(plans *taskplan.Service) SetTaskPlanTool {
	return SetTaskPlanTool{plans: plans}
}

func (t SetTaskPlanTool) Definition() Definition {
	return Definition{
		Name: "set_task_plan",
		Description: "Create a persisted, versioned task plan before browser exploration. " +
			"The plan owns business action order, exact occurrence counts, side-effect boundaries, " +
			"forbidden actions, preconditions, and completion conditions. " +
			"Exploration may ground these steps but must never rewrite their semantics.",
		InputSchema: json.RawMessage(`{
			"type":"object",
			"additionalProperties":false,
			"properties":{
				"goal":{"type":"string","minLength":1},
				"max_side_effect":{"type":"string","enum":["none","browser_state","external_state","unknown"]},
				"forbidden_actions":{"type":"array","items":{"type":"string","minLength":1}},
				"steps":{
					"type":"array",
					"minItems":1,
					"items":{
						"type":"object",
						"additionalProperties":false,
						"properties":{
							"id":{"type":"string","pattern":"^[A-Za-z][A-Za-z0-9_-]{0,63}$"},
							"intent":{"type":"string","minLength":1},
							"action":{"type":"string","enum":["goto","click","input","wait_for","assert_text","assert_url_contains","capture_text"]},
							"target":{"type":"string"},
							"value":{"type":"string"},
							"trigger":{"type":"string","enum":["Enter","Tab"]},
							"context_key":{"type":"string","pattern":"^[A-Za-z_][A-Za-z0-9_]*$"},
							"timeout_ms":{"type":"integer","minimum":1,"maximum":60000},
							"expected_occurrences":{"type":"integer","minimum":1},
							"idempotency":{"type":"string","enum":["idempotent","non_idempotent"]},
							"side_effect":{"type":"string","enum":["none","browser_state","external_state","unknown"]},
							"preconditions":{"type":"array","items":{"type":"string","minLength":1}},
							"completion_conditions":{"type":"array","items":{"type":"string","minLength":1}}
						},
						"required":["id","intent","action","expected_occurrences","idempotency","side_effect","preconditions","completion_conditions"]
					}
				}
			},
			"required":["goal","max_side_effect","forbidden_actions","steps"]
		}`),
	}
}

func (t SetTaskPlanTool) Execute(
	ctx context.Context,
	call Call,
) (Result, error) {
	if t.plans == nil {
		return Result{}, errors.New("task plan service is unavailable")
	}
	var definition taskplan.Definition
	if err := json.Unmarshal(call.Arguments, &definition); err != nil {
		return Result{}, err
	}
	if definition.Goal != call.RunInput {
		return Result{}, errors.New(
			"task plan goal must exactly match the agent run input",
		)
	}
	plan, err := t.plans.CreateVersion(ctx, taskplan.CreateRequest{
		RunID:       call.RunID,
		ActorUserID: call.ActorUserID,
		ProjectID:   call.ProjectID,
		Definition:  definition,
	})
	if err != nil {
		return Result{}, err
	}
	content, err := json.Marshal(map[string]any{
		"status":         plan.Status,
		"schema_version": plan.SchemaVersion,
		"plan_id":        plan.ID,
		"version":        plan.Version,
		"plan_sha256":    plan.PlanSHA256,
		"binding":        plan.Binding(),
		"steps":          plan.Steps,
	})
	if err != nil {
		return Result{}, err
	}
	return Result{
		Content: content,
		Artifact: &Artifact{
			Type: "task_plan",
			ID:   plan.ID,
		},
	}, nil
}
