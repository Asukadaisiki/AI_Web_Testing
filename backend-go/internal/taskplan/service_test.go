package taskplan

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestTaskPlanLifecycleBindsGroundingGenerationAndExecution(t *testing.T) {
	ctx := context.Background()
	service := NewService(NewMemoryRepository())
	plan, err := service.CreateVersion(ctx, CreateRequest{
		RunID:       "run-1",
		ActorUserID: 1,
		ProjectID:   2,
		Definition: Definition{
			Goal:             "Search for a product",
			MaxSideEffect:    SideEffectBrowserState,
			ForbiddenActions: []string{"add to cart", "checkout"},
			Steps: []StepDefinition{
				{
					ID: "open_products", Intent: "Open products",
					Action: "goto", Target: "Products page",
					Value:               "https://example.test/products",
					ExpectedOccurrences: 1, Idempotency: "idempotent",
					SideEffect:           SideEffectBrowserState,
					Preconditions:        []string{},
					CompletionConditions: []string{"products page visible"},
				},
				{
					ID: "search_product", Intent: "Search for a product",
					Action: "input", Target: "Search", Value: "Blue Top",
					ExpectedOccurrences: 1, Idempotency: "idempotent",
					SideEffect:           SideEffectBrowserState,
					Preconditions:        []string{"products page visible"},
					CompletionConditions: []string{"search value entered"},
				},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Version != 1 || len(plan.PlanSHA256) != 64 ||
		plan.Status != StatusGrounding {
		t.Fatalf("created plan = %#v", plan)
	}

	pageArgs := json.RawMessage(`{
		"url":"https://example.test/products",
		"plan_step_ids":["open_products"]
	}`)
	if err := service.Authorize(
		ctx,
		plan.RunID,
		"explore_page",
		pageArgs,
	); err != nil {
		t.Fatal(err)
	}
	if err := service.RecordToolResult(
		ctx,
		plan.RunID,
		"explore_page",
		pageArgs,
		json.RawMessage(`{
			"url":"https://example.test/products",
			"status":"success"
		}`),
		8,
	); err != nil {
		t.Fatal(err)
	}

	flowArgs := json.RawMessage(`{
		"plan_step_ids":["search_product"],
		"steps":[{"actions":[{
			"action":"input","target":"Search","value":"Blue Top"
		}]}]
	}`)
	if err := service.Authorize(
		ctx,
		plan.RunID,
		"explore_flow",
		flowArgs,
	); err != nil {
		t.Fatal(err)
	}
	if err := service.RecordToolResult(
		ctx,
		plan.RunID,
		"explore_flow",
		flowArgs,
		json.RawMessage(`{
			"success":true,
			"pages":[{"status":"success","actions":[{
				"step_index":0,"action_index":0,
				"action":"input","target":"Search","value":"Blue Top",
				"phase":"after","status":"success"
			}]}]
		}`),
		16,
	); err != nil {
		t.Fatal(err)
	}

	plan, err = service.GetCurrent(ctx, plan.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Status != StatusReadyForGeneration ||
		plan.Steps[0].Status != StepGrounded ||
		plan.Steps[1].Status != StepGrounded {
		t.Fatalf("grounded plan = %#v", plan)
	}
	caseJSON := json.RawMessage(`{
		"steps":[
			{
				"action":"goto","intent":"Open products",
				"target":"Products page",
				"value":"https://example.test/products",
				"idempotency":"idempotent","side_effect":"browser_state"
			},
			{
				"action":"input","intent":"Search for a product",
				"target":"Search","value":"Blue Top",
				"idempotency":"idempotent","side_effect":"browser_state"
			}
		]
	}`)
	generateArgs, err := json.Marshal(map[string]any{
		"plan_binding": plan.Binding(),
		"case":         json.RawMessage(caseJSON),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Authorize(
		ctx,
		plan.RunID,
		"generate_dsl",
		generateArgs,
	); err != nil {
		t.Fatal(err)
	}
	if err := service.RecordToolResult(
		ctx,
		plan.RunID,
		"generate_dsl",
		generateArgs,
		json.RawMessage(`{"generation_id":41}`),
		24,
	); err != nil {
		t.Fatal(err)
	}
	if err := service.Approve(ctx, plan.RunID, 41); err != nil {
		t.Fatal(err)
	}
	if err := service.Authorize(
		ctx,
		plan.RunID,
		"execute_dsl",
		json.RawMessage(`{"generation_id":41}`),
	); err != nil {
		t.Fatal(err)
	}
	if err := service.RecordToolResult(
		ctx,
		plan.RunID,
		"execute_dsl",
		json.RawMessage(`{"generation_id":41}`),
		json.RawMessage(`{"batch_id":7,"status":"pending"}`),
		32,
	); err != nil {
		t.Fatal(err)
	}
	if err := service.Authorize(
		ctx,
		plan.RunID,
		"get_report",
		json.RawMessage(`{"batch_id":7}`),
	); err != nil {
		t.Fatal(err)
	}
	if err := service.RecordToolResult(
		ctx,
		plan.RunID,
		"get_report",
		json.RawMessage(`{"batch_id":7}`),
		json.RawMessage(`{"id":7,"status":"passed"}`),
		40,
	); err != nil {
		t.Fatal(err)
	}
	plan, err = service.GetCurrent(ctx, plan.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Status != StatusCompleted ||
		plan.BoundGenerationID == nil ||
		*plan.BoundGenerationID != 41 {
		t.Fatalf("completed plan = %#v", plan)
	}
}

func TestTaskPlanRejectsForbiddenAndUnplannedProbeActions(t *testing.T) {
	ctx := context.Background()
	service := NewService(NewMemoryRepository())
	plan, err := service.CreateVersion(ctx, CreateRequest{
		RunID: "run-2",
		Definition: Definition{
			Goal:             "Inspect a product",
			MaxSideEffect:    SideEffectBrowserState,
			ForbiddenActions: []string{"add to cart"},
			Steps: []StepDefinition{{
				ID: "open_detail", Intent: "Open details",
				Action: "click", Target: "View Product",
				ExpectedOccurrences: 1, Idempotency: "idempotent",
				SideEffect:           SideEffectBrowserState,
				Preconditions:        []string{},
				CompletionConditions: []string{"details visible"},
			}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	err = service.Authorize(
		ctx,
		plan.RunID,
		"explore_flow",
		json.RawMessage(`{
			"plan_step_ids":["open_detail"],
			"steps":[{"actions":[{
				"action":"click","target":"Add to cart"
			}]}]
		}`),
	)
	if err == nil || !strings.Contains(err.Error(), "forbidden") {
		t.Fatalf("error = %v, want forbidden action rejection", err)
	}
	err = service.Authorize(
		ctx,
		plan.RunID,
		"explore_flow",
		json.RawMessage(`{
			"plan_step_ids":["open_detail"],
			"steps":[{"actions":[{
				"action":"input","target":"Different action","value":"unexpected"
			}]}]
		}`),
	)
	if err == nil || !strings.Contains(err.Error(), "not owned") {
		t.Fatalf("error = %v, want unplanned action rejection", err)
	}
}

func TestExploreFlowAllowsGroundedPrerequisiteReplay(t *testing.T) {
	ctx := context.Background()
	repository := NewMemoryRepository()
	service := NewService(repository)
	plan, err := service.CreateVersion(ctx, CreateRequest{
		RunID: "run-prerequisite-replay",
		Definition: Definition{
			Goal:             "Search and open a product",
			MaxSideEffect:    SideEffectBrowserState,
			ForbiddenActions: []string{"checkout"},
			Steps: []StepDefinition{
				{
					ID: "search_input", Intent: "Enter search",
					Action: "input",
					Target: "Product search field",
					Value:  "Blue Top", ExpectedOccurrences: 1,
					Idempotency:          "idempotent",
					SideEffect:           SideEffectBrowserState,
					Preconditions:        []string{},
					CompletionConditions: []string{"search entered"},
				},
				{
					ID: "search_submit", Intent: "Submit search",
					Action: "click", Target: "Search button",
					ExpectedOccurrences: 1, Idempotency: "idempotent",
					SideEffect:           SideEffectBrowserState,
					Preconditions:        []string{"search entered"},
					CompletionConditions: []string{"results visible"},
				},
				{
					ID: "verify_result", Intent: "Verify result",
					Action: "assert_text", Target: "Blue Top",
					Value: "Blue Top", ExpectedOccurrences: 1,
					Idempotency: "idempotent", SideEffect: SideEffectNone,
					Preconditions:        []string{"results visible"},
					CompletionConditions: []string{"Blue Top visible"},
				},
				{
					ID: "open_detail", Intent: "Open detail",
					Action: "click", Target: "View Product",
					ExpectedOccurrences: 1, Idempotency: "idempotent",
					SideEffect:           SideEffectBrowserState,
					Preconditions:        []string{"Blue Top visible"},
					CompletionConditions: []string{"detail visible"},
				},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	plan.Steps[0].Status = StepGrounded
	plan.Steps[1].Status = StepGrounded
	if err := repository.Save(ctx, plan); err != nil {
		t.Fatal(err)
	}

	allowed := json.RawMessage(`{
		"plan_step_ids":["search_input","search_submit","verify_result"],
		"steps":[{"actions":[
			{"action":"wait_for","target":"All Products"},
			{"action":"input","target":"Search Product","value":"Blue Top"},
			{"action":"click","target":"#submit_search"},
			{"action":"wait_for","target":"Blue Top"}
		]}]
	}`)
	if err := service.Authorize(
		ctx,
		plan.RunID,
		"explore_flow",
		allowed,
	); err != nil {
		t.Fatalf("prerequisite replay rejected: %v", err)
	}

	future := json.RawMessage(`{
		"plan_step_ids":["verify_result"],
		"steps":[{"actions":[
			{"action":"click","target":"View Product"}
		]}]
	}`)
	err = service.Authorize(
		ctx,
		plan.RunID,
		"explore_flow",
		future,
	)
	if err == nil || !strings.Contains(err.Error(), "not owned") {
		t.Fatalf("future unbound action error = %v", err)
	}
}

func TestExplorationOnlyGroundsStepsWithEvidence(t *testing.T) {
	ctx := context.Background()
	service := NewService(NewMemoryRepository())
	plan, err := service.CreateVersion(ctx, CreateRequest{
		RunID: "run-partial-grounding",
		Definition: Definition{
			Goal:             "Open and search",
			MaxSideEffect:    SideEffectBrowserState,
			ForbiddenActions: []string{},
			Steps: []StepDefinition{
				{
					ID: "open", Intent: "Open products", Action: "goto",
					Target: "Products", Value: "https://example.test/products",
					ExpectedOccurrences: 1, Idempotency: "idempotent",
					SideEffect:           SideEffectBrowserState,
					Preconditions:        []string{},
					CompletionConditions: []string{"products visible"},
				},
				{
					ID: "submit", Intent: "Submit search", Action: "click",
					Target: "Search button", ExpectedOccurrences: 1,
					Idempotency:          "idempotent",
					SideEffect:           SideEffectBrowserState,
					Preconditions:        []string{"products visible"},
					CompletionConditions: []string{"results visible"},
				},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	arguments := json.RawMessage(`{
		"plan_step_ids":["open","submit"],
		"steps":[{"actions":[
			{"action":"click","target":"Search button"}
		]}]
	}`)
	if err := service.RecordToolResult(
		ctx,
		plan.RunID,
		"explore_flow",
		arguments,
		json.RawMessage(`{
			"success":false,
			"pages":[
				{
					"url":"https://example.test/products",
					"status":"success",
					"a11y_nodes":[]
				},
				{
					"url":"https://example.test/products",
					"status":"error",
					"actions":[{
						"action":"click",
						"target":"Search button",
						"phase":"after",
						"status":"error"
					}],
					"failure":{"code":"flow_action_failed"}
				}
			]
		}`),
		9,
	); err != nil {
		t.Fatal(err)
	}
	current, err := service.GetCurrent(ctx, plan.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if current.Steps[0].Status != StepGrounded ||
		current.Steps[1].Status != StepPending ||
		current.Status != StatusGrounding {
		t.Fatalf("partially grounded plan = %#v", current)
	}
}

func TestTaskPlanVersionsAreImmutableAndLatestWins(t *testing.T) {
	ctx := context.Background()
	repository := NewMemoryRepository()
	service := NewService(repository)
	request := CreateRequest{
		RunID: "run-3",
		Definition: Definition{
			Goal:             "Open a page",
			MaxSideEffect:    SideEffectBrowserState,
			ForbiddenActions: []string{},
			Steps: []StepDefinition{{
				ID: "open", Intent: "Open page", Action: "goto",
				Value:               "https://example.test",
				ExpectedOccurrences: 1, Idempotency: "idempotent",
				SideEffect:           SideEffectBrowserState,
				Preconditions:        []string{},
				CompletionConditions: []string{"page visible"},
			}},
		},
	}
	first, err := service.CreateVersion(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	request.Definition.Steps[0].Value = "https://example.test/next"
	second, err := service.CreateVersion(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	current, err := service.GetCurrent(ctx, request.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if first.Version != 1 || second.Version != 2 ||
		current.ID != second.ID ||
		first.PlanSHA256 == second.PlanSHA256 {
		t.Fatalf(
			"versions first=%#v second=%#v current=%#v",
			first,
			second,
			current,
		)
	}
}

func TestTaskPlanRevisionCarriesEquivalentGroundedSteps(t *testing.T) {
	ctx := context.Background()
	repository := NewMemoryRepository()
	service := NewService(repository)
	request := CreateRequest{
		RunID: "run-carry-forward",
		Definition: Definition{
			Goal:             "Open and verify a page",
			MaxSideEffect:    SideEffectBrowserState,
			ForbiddenActions: []string{},
			Steps: []StepDefinition{
				{
					ID: "open", Intent: "Open page", Action: "goto",
					Target: "Page", Value: "https://example.test",
					ExpectedOccurrences: 1, Idempotency: "idempotent",
					SideEffect:           SideEffectBrowserState,
					Preconditions:        []string{},
					CompletionConditions: []string{"page visible"},
				},
				{
					ID: "verify", Intent: "Verify text",
					Action: "assert_text", Target: "Old",
					Value: "Old", ExpectedOccurrences: 1,
					Idempotency:          "idempotent",
					SideEffect:           SideEffectNone,
					Preconditions:        []string{"page visible"},
					CompletionConditions: []string{"text visible"},
				},
			},
		},
	}
	first, err := service.CreateVersion(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	first.Steps[0].Status = StepGrounded
	first.Steps[0].GroundingAttempts = 1
	first.Steps[0].Evidence = []EvidenceRef{{
		Tool: "explore_page", EventSeq: 7,
		ContentSHA256: strings.Repeat("a", 64),
	}}
	if err := repository.Save(ctx, first); err != nil {
		t.Fatal(err)
	}

	request.Definition.Steps[1].Target = "New"
	request.Definition.Steps[1].Value = "New"
	second, err := service.CreateVersion(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if second.Steps[0].Status != StepGrounded ||
		second.Steps[0].GroundingAttempts != 1 ||
		len(second.Steps[0].Evidence) != 1 ||
		second.Steps[1].Status != StepPending {
		t.Fatalf("carried revision = %#v", second.Steps)
	}
}

func TestTaskPlanRequiresPlanBeforeExecutionTools(t *testing.T) {
	service := NewService(NewMemoryRepository())
	err := service.Authorize(
		context.Background(),
		"missing",
		"explore_page",
		json.RawMessage(`{"plan_step_ids":["open"]}`),
	)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("error = %v, want ErrNotFound", err)
	}
}

func TestTaskPlanExpectedOccurrencesMustBePreservedByDSL(t *testing.T) {
	ctx := context.Background()
	repository := NewMemoryRepository()
	service := NewService(repository)
	plan, err := service.CreateVersion(ctx, CreateRequest{
		RunID: "run-4",
		Definition: Definition{
			Goal:             "Click twice",
			MaxSideEffect:    SideEffectBrowserState,
			ForbiddenActions: []string{},
			Steps: []StepDefinition{{
				ID: "increment", Intent: "Increment quantity",
				Action: "click", Target: "Increase",
				ExpectedOccurrences: 2, Idempotency: "non_idempotent",
				SideEffect:           SideEffectBrowserState,
				Preconditions:        []string{},
				CompletionConditions: []string{"quantity increased twice"},
			}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	plan.Steps[0].Status = StepGrounded
	plan.Status = StatusReadyForGeneration
	if err := repository.Save(ctx, plan); err != nil {
		t.Fatal(err)
	}
	oneStep := json.RawMessage(`{"steps":[{
		"action":"click","intent":"Increment quantity","target":"Increase",
		"idempotency":"non_idempotent","side_effect":"browser_state"
	}]}`)
	if _, err := service.ValidateGenerationBinding(
		ctx,
		plan.RunID,
		plan.Binding(),
		oneStep,
	); err == nil || !strings.Contains(err.Error(), "requires 2 occurrences") {
		t.Fatalf("error = %v, want occurrence mismatch", err)
	}
}
