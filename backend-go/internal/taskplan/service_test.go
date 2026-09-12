package taskplan

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/browsercontract"
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
			"plan_step_id":"search_product",
			"action":"input",
			"locator":{"kind":"role","role":"textbox","name":"Search","exact":true},
			"value":"Blue Top"
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
			"pages":[{
				"status":"success",
				"observation_v2":{
					"schema_version":"browser.observation.v2",
					"probe_id":"probe-search",
					"observation_id":"obs-search",
					"page_state":{
						"state_id":"products",
						"state_sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
					},
					"elements":[{
						"element_ref":"products:7",
						"context_path":{"frames":[],"shadow_hosts":[]},
						"a11y":{"role":"textbox","name":"Search"},
						"dom":{"tag":"input","attrs":{"id":"search"},"text":""},
						"runtime":{"visible":true,"enabled":true,"editable":true},
						"locators":[{
							"candidate_id":"candidate-search",
							"locator":{"kind":"role","role":"textbox","name":"Search","exact":true},
							"provenance":"a11y_exact","observed_count":1
						}]
					}]
				},
				"actions":[
					{
						"step_index":0,"action_index":0,
						"plan_step_id":"search_product",
						"action":"input","target":"role=textbox, name=Search","value":"Blue Top",
						"phase":"before","status":"success",
						"resolved_target":{
							"schema_version":"browser.resolved-target.v1",
							"probe_id":"probe-search",
							"plan_step_id":"search_product",
							"step_index":0,
							"action_index":0,
							"action":"input",
							"observation_id":"obs-search",
							"page_state_id":"products",
							"page_state_sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
							"element_ref":"products:7",
							"candidate_id":"candidate-search",
							"locator":{"kind":"role","role":"textbox","name":"Search","exact":true},
							"context_path":{"frames":[],"shadow_hosts":[]},
							"provenance":"a11y_exact",
							"runtime_match_count":1,
							"visible":true,
							"enabled":true,
							"editable":true,
							"score":0.95,
							"action_status":"succeeded"
						}
					},
					{
						"step_index":0,"action_index":0,
						"plan_step_id":"search_product",
						"action":"input","target":"role=textbox, name=Search","value":"Blue Top",
						"phase":"after","status":"success"
					}
				]
			}]
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
				"plan_step_id":"open_detail",
				"action":"click",
				"locator":{"kind":"text","value":"Add to cart","exact":true}
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
				"plan_step_id":"open_detail",
				"action":"input",
				"locator":{"kind":"placeholder","value":"Different action","exact":true},
				"value":"unexpected"
			}]}]
		}`),
	)
	if err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("error = %v, want unplanned action rejection", err)
	}
}

func TestExploreFlowCanObserveExternalStepWithoutExecutingIt(t *testing.T) {
	ctx := context.Background()
	service := NewService(NewMemoryRepository())
	plan, err := service.CreateVersion(ctx, CreateRequest{
		RunID: "run-observe-external",
		Definition: Definition{
			Goal:             "Verify the delete control",
			MaxSideEffect:    SideEffectExternal,
			ForbiddenActions: []string{},
			Steps: []StepDefinition{{
				ID: "delete", Intent: "Delete account", Action: "click",
				Target: "Delete account", ExpectedOccurrences: 1,
				Idempotency:          "non_idempotent",
				SideEffect:           SideEffectExternal,
				Preconditions:        []string{"account visible"},
				CompletionConditions: []string{"account deleted"},
			}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	observe := json.RawMessage(`{
		"plan_step_ids":["delete"],
		"steps":[{"actions":[{
			"plan_step_id":"delete",
			"action":"wait_for",
			"locator":{"kind":"text","value":"Delete account","exact":true}
		}]}]
	}`)
	if err := service.Authorize(
		ctx,
		plan.RunID,
		"explore_flow",
		observe,
	); err != nil {
		t.Fatalf("safe observation rejected: %v", err)
	}

	execute := json.RawMessage(`{
		"plan_step_ids":["delete"],
		"steps":[{"actions":[{
			"plan_step_id":"delete",
			"action":"click",
			"locator":{"kind":"text","value":"Delete account","exact":true}
		}]}]
	}`)
	err = service.Authorize(ctx, plan.RunID, "explore_flow", execute)
	if err == nil || !strings.Contains(err.Error(), "cannot be probed") {
		t.Fatalf("external click error = %v", err)
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
		"plan_step_ids":["verify_result"],
		"steps":[{"actions":[
			{"action":"wait_for","locator":{"kind":"text","value":"All Products","exact":true}},
			{"plan_step_id":"search_input","action":"input","locator":{"kind":"placeholder","value":"Search Product","exact":true},"value":"Blue Top"},
			{"plan_step_id":"search_submit","action":"click","locator":{"kind":"role","role":"button","name":"Search","exact":true}},
			{"plan_step_id":"verify_result","action":"wait_for","locator":{"kind":"text","value":"Blue Top","exact":true}}
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
			{"plan_step_id":"future","action":"click","locator":{"kind":"role","role":"link","name":"View Product","exact":true}}
		]}]
	}`)
	err = service.Authorize(
		ctx,
		plan.RunID,
		"explore_flow",
		future,
	)
	if err == nil || !strings.Contains(err.Error(), "unknown plan step") {
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
			{"plan_step_id":"submit","action":"click","locator":{"kind":"role","role":"button","name":"Search","exact":true}}
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

func TestExploreFlowDoesNotTextRebindWithoutResolvedTarget(t *testing.T) {
	ctx := context.Background()
	repository := NewMemoryRepository()
	service := NewService(repository)
	plan, err := service.CreateVersion(ctx, CreateRequest{
		RunID: "run-target-binding",
		Definition: Definition{
			Goal:             "Submit a form",
			MaxSideEffect:    SideEffectBrowserState,
			ForbiddenActions: []string{},
			Steps: []StepDefinition{
				{
					ID: "open", Intent: "Open form", Action: "goto",
					Target: "Form", Value: "https://example.test/form",
					ExpectedOccurrences: 1, Idempotency: "idempotent",
					SideEffect:           SideEffectBrowserState,
					Preconditions:        []string{},
					CompletionConditions: []string{"form visible"},
				},
				{
					ID: "submit", Intent: "Submit form", Action: "click",
					Target:              "Submit",
					ExpectedOccurrences: 1, Idempotency: "idempotent",
					SideEffect:           SideEffectBrowserState,
					Preconditions:        []string{"form visible"},
					CompletionConditions: []string{"submitted"},
				},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	plan.Steps[0].Status = StepGrounded
	if err := repository.Save(ctx, plan); err != nil {
		t.Fatal(err)
	}
	arguments := json.RawMessage(`{
		"plan_step_ids":["submit"],
		"steps":[{"actions":[{
			"plan_step_id":"submit",
			"action":"click",
			"locator":{"kind":"role","role":"button","name":"Submit","exact":true}
		}]}]
	}`)
	result := json.RawMessage(`{
		"success":true,
		"pages":[{
			"status":"success",
			"observation_v2":{
				"schema_version":"browser.observation.v2",
				"probe_id":"probe-1",
				"observation_id":"obs-1",
				"page_state":{
					"state_id":"form",
					"state_sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
				},
				"elements":[{
					"element_ref":"form:7",
					"a11y":{"role":"button","name":"Submit"},
					"dom":{"tag":"button","attrs":{"id":"submit"},"text":"Submit"},
					"runtime":{"visible":true,"enabled":true,"editable":false},
					"locators":[
						{
							"candidate_id":"candidate-role",
							"locator":{"kind":"role","role":"button","name":"Submit","exact":true},
							"provenance":"a11y_exact",
							"observed_count":1
						},
						{
							"candidate_id":"candidate-css",
							"locator":{"kind":"css","value":"#submit","exact":true},
							"provenance":"a11y_backend_dom_node",
							"observed_count":1
						}
					]
				}]
			},
			"actions":[
				{
					"step_index":0,"action_index":0,
					"action":"click","target":"Submit",
					"phase":"before","status":"success"
				},
				{
					"step_index":0,"action_index":0,
					"action":"click","target":"Submit",
					"phase":"after","status":"success"
				}
			]
		}]
	}`)
	if err := service.RecordToolResult(
		ctx,
		plan.RunID,
		"explore_flow",
		arguments,
		result,
		11,
	); err != nil {
		t.Fatal(err)
	}
	current, err := service.GetCurrent(ctx, plan.RunID)
	if err != nil {
		t.Fatal(err)
	}
	binding := current.Steps[1].TargetBinding
	if binding != nil || current.Steps[1].Status != StepPending {
		t.Fatalf("legacy flow evidence unexpectedly bound = %#v", binding)
	}
}

func TestExploreFlowUsesResolvedTargetWithoutTextRebinding(t *testing.T) {
	ctx := context.Background()
	repository := NewMemoryRepository()
	service := NewService(repository)
	plan, err := service.CreateVersion(ctx, CreateRequest{
		RunID: "run-resolved-target",
		Definition: Definition{
			Goal:             "Submit a form",
			MaxSideEffect:    SideEffectBrowserState,
			ForbiddenActions: []string{},
			Steps: []StepDefinition{{
				ID: "submit", Intent: "Submit the form", Action: "click",
				Target:              "Primary form submission control",
				ExpectedOccurrences: 1, Idempotency: "idempotent",
				SideEffect:           SideEffectBrowserState,
				Preconditions:        []string{"form visible"},
				CompletionConditions: []string{"submitted"},
			}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	arguments := json.RawMessage(`{
		"plan_step_ids":["submit"],
		"steps":[{"actions":[{
			"plan_step_id":"submit",
			"action":"click",
			"locator":{"kind":"role","role":"button","name":"Submit","exact":true}
		}]}]
	}`)
	result := json.RawMessage(`{
		"success":true,
		"pages":[{
			"status":"success",
			"observation_v2":{
				"schema_version":"browser.observation.v2",
				"probe_id":"probe-resolved",
				"observation_id":"obs-resolved",
				"page_state":{
					"state_id":"form",
					"state_sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
				},
				"elements":[{
					"element_ref":"form:7",
					"context_path":{"frames":[],"shadow_hosts":[]},
					"a11y":{"role":"button","name":"Submit"},
					"dom":{"tag":"button","attrs":{"id":"submit"},"text":"Submit"},
					"runtime":{"visible":true,"enabled":true,"editable":false},
					"locators":[{
						"candidate_id":"candidate-resolved",
						"locator":{"kind":"role","role":"button","name":"Submit","exact":true},
						"provenance":"grounding_query",
						"observed_count":1
					}]
				}]
			},
			"actions":[
				{
					"step_index":0,"action_index":0,
					"plan_step_id":"submit",
					"action":"click","target":"role=button, name=Submit",
					"phase":"before","status":"success",
					"resolved_target":{
						"schema_version":"browser.resolved-target.v1",
						"probe_id":"probe-resolved",
						"plan_step_id":"submit",
						"step_index":0,
						"action_index":0,
						"action":"click",
						"observation_id":"obs-resolved",
						"page_state_id":"form",
						"page_state_sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
						"element_ref":"form:7",
						"candidate_id":"candidate-resolved",
						"locator":{"kind":"role","role":"button","name":"Submit","exact":true},
						"context_path":{"frames":[],"shadow_hosts":[]},
						"provenance":"grounding_query",
						"runtime_match_count":1,
						"visible":true,
						"enabled":true,
						"editable":false,
						"score":0.95,
						"action_status":"succeeded"
					}
				},
				{
					"step_index":0,"action_index":0,
					"plan_step_id":"submit",
					"action":"click","target":"role=button, name=Submit",
					"phase":"after","status":"success"
				}
			]
		}]
	}`)
	if err := service.RecordToolResult(
		ctx,
		plan.RunID,
		"explore_flow",
		arguments,
		result,
		17,
	); err != nil {
		t.Fatal(err)
	}
	current, err := service.GetCurrent(ctx, plan.RunID)
	if err != nil {
		t.Fatal(err)
	}
	binding := current.Steps[0].TargetBinding
	if binding == nil ||
		binding.SelectedCandidateID != "candidate-resolved" ||
		binding.Candidates[0].ElementRef != "form:7" ||
		binding.SemanticTarget != "Primary form submission control" {
		t.Fatalf("binding = %#v", binding)
	}
}

func TestExploreFlowRequiresResolvedTargetForBinding(t *testing.T) {
	ctx := context.Background()
	repository := NewMemoryRepository()
	service := NewService(repository)
	plan, err := service.CreateVersion(ctx, CreateRequest{
		RunID: "run-container-filter",
		Definition: Definition{
			Goal:             "Open product details",
			MaxSideEffect:    SideEffectBrowserState,
			ForbiddenActions: []string{},
			Steps: []StepDefinition{{
				ID: "details", Intent: "Open details", Action: "click",
				Target: "Blue Top product details link", ExpectedOccurrences: 1,
				Idempotency:          "idempotent",
				SideEffect:           SideEffectBrowserState,
				Preconditions:        []string{"result visible"},
				CompletionConditions: []string{"details visible"},
			}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	arguments := json.RawMessage(`{
		"plan_step_ids":["details"],
		"steps":[{"actions":[{
			"plan_step_id":"details",
			"action":"click",
			"locator":{"kind":"role","role":"link","name":"View Product","exact":true}
		}]}]
	}`)
	result := json.RawMessage(`{
		"success":true,
		"pages":[{
			"status":"success",
			"observation_v2":{
				"schema_version":"browser.observation.v2",
				"probe_id":"probe-details",
				"observation_id":"obs-details",
				"page_state":{
					"state_id":"results",
					"state_sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
				},
				"elements":[
					{
						"element_ref":"results:list",
						"a11y":{"role":"list","name":"View Product"},
						"dom":{"tag":"ul","attrs":{},"text":"View Product"},
						"runtime":{"visible":true,"enabled":true,"editable":false},
						"locators":[{
							"candidate_id":"candidate-list",
							"locator":{"kind":"role","role":"list","name":"View Product","exact":true},
							"provenance":"a11y_exact",
							"observed_count":0
						}]
					},
					{
						"element_ref":"results:item",
						"a11y":{"role":"listitem","name":"View Product"},
						"dom":{"tag":"li","attrs":{},"text":"View Product"},
						"runtime":{"visible":true,"enabled":true,"editable":false},
						"locators":[]
					},
					{
						"element_ref":"results:link",
						"a11y":{"role":"link","name":"View Product"},
						"dom":{"tag":"a","attrs":{"href":"/details/1"},"text":"View Product"},
						"runtime":{"visible":true,"enabled":true,"editable":false},
						"locators":[{
							"candidate_id":"candidate-link",
							"locator":{"kind":"css","value":"a[href=\"/details/1\"]","exact":true},
							"provenance":"a11y_backend_dom_node",
							"observed_count":1
						}]
					}
				]
			},
			"actions":[
				{"step_index":0,"action_index":0,"action":"click","target":"View Product","phase":"before","status":"success"},
				{"step_index":0,"action_index":0,"action":"click","target":"View Product","phase":"after","status":"success"}
			]
		}]
	}`)
	if err := service.RecordToolResult(
		ctx,
		plan.RunID,
		"explore_flow",
		arguments,
		result,
		17,
	); err != nil {
		t.Fatal(err)
	}
	current, err := service.GetCurrent(ctx, plan.RunID)
	if err != nil {
		t.Fatal(err)
	}
	binding := current.Steps[0].TargetBinding
	if binding != nil || current.Steps[0].Status != StepPending {
		t.Fatalf("legacy flow evidence unexpectedly grounded = %#v", current.Steps[0])
	}
}

func TestCompileResearchV2DraftInjectsPersistedTargetBinding(t *testing.T) {
	ctx := context.Background()
	repository := NewMemoryRepository()
	service := NewService(repository)
	plan, err := service.CreateVersion(ctx, CreateRequest{
		RunID: "run-compile-v2",
		Definition: Definition{
			Goal:             "Submit a form",
			MaxSideEffect:    SideEffectBrowserState,
			ForbiddenActions: []string{},
			Steps: []StepDefinition{{
				ID: "submit", Intent: "Submit form", Action: "click",
				Target: "Submit", ExpectedOccurrences: 1,
				Idempotency:          "idempotent",
				SideEffect:           SideEffectBrowserState,
				Preconditions:        []string{"form visible"},
				CompletionConditions: []string{"saved"},
			}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	name := "Submit"
	targetBinding, err := browsercontract.NewTargetBinding(
		browsercontract.TargetBinding{
			PlanID: plan.ID, PlanVersion: plan.Version,
			PlanStepID: "submit", SemanticTarget: "Submit",
			ProbeID: "probe-1", Action: "click", PageStateID: "form",
			ObservationID: "obs-1", ObservationSHA256: strings.Repeat("a", 64),
			ElementRefs: []string{"form:7"},
			Candidates: []browsercontract.LocatorCandidate{{
				CandidateID: "candidate-1", ElementRef: "form:7",
				Locator: browsercontract.LocatorSpec{
					Kind: "role", Role: "button", Name: &name, Exact: true,
				},
				Provenance: "a11y_exact", ObservedCount: 1,
				Visible: true, Enabled: true, Score: 0.95,
			}},
			SelectedCandidateID: "candidate-1",
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	plan.Steps[0].Status = StepGrounded
	plan.Steps[0].TargetBinding = &targetBinding
	plan.Status = StatusReadyForGeneration
	if err := repository.Save(ctx, plan); err != nil {
		t.Fatal(err)
	}

	compiled, _, err := service.CompileDraftCase(
		ctx,
		plan.RunID,
		plan.Binding(),
		json.RawMessage(`{
			"profile":"research-v2",
			"name":"Submit form",
			"steps":[{
				"plan_step_id":"submit",
				"action":"click",
				"intent":"Submit form",
				"target_binding_id":"`+targetBinding.BindingID+`",
				"preconditions":[{"type":"text_visible","value":"Submit"}],
				"postconditions":[{"type":"text_visible","value":"Saved"}],
				"idempotency":"idempotent",
				"side_effect":"browser_state"
			}]
		}`),
	)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(compiled, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["profile"] != "research-v2" {
		t.Fatalf("compiled profile = %#v", payload["profile"])
	}
	if !strings.Contains(
		string(compiled),
		targetBinding.BindingSHA256,
	) {
		t.Fatalf("compiled DSL lacks target binding: %s", compiled)
	}
	steps := payload["steps"].([]any)
	step := steps[0].(map[string]any)
	if step["probe_id"] != "probe-1" ||
		step["observation_id"] != "obs-1" ||
		step["page_state_id"] != "form" ||
		step["selected_candidate_id"] != "candidate-1" {
		t.Fatalf("compiled lineage = %#v", step)
	}
}
