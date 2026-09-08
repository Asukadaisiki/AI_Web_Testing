package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/taskplan"
)

func TestSetTaskPlanPersistsVersionedPlan(t *testing.T) {
	service := taskplan.NewService(taskplan.NewMemoryRepository())
	tool := NewSetTaskPlanTool(service)
	result, err := tool.Execute(context.Background(), Call{
		RunID:    "run-tool",
		RunInput: "Open the products page",
		Name:     "set_task_plan",
		Arguments: json.RawMessage(`{
			"goal":"Open the products page",
			"max_side_effect":"browser_state",
			"forbidden_actions":["checkout"],
			"steps":[{
				"id":"open_products",
				"intent":"Open the products page",
				"action":"goto",
				"target":"Products page",
				"value":"https://example.test/products",
				"expected_occurrences":1,
				"idempotency":"idempotent",
				"side_effect":"browser_state",
				"preconditions":[],
				"completion_conditions":["products page visible"]
			}]
		}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Artifact == nil || result.Artifact.Type != "task_plan" {
		t.Fatalf("artifact = %#v", result.Artifact)
	}
	var payload struct {
		PlanID     string           `json:"plan_id"`
		Version    int              `json:"version"`
		PlanSHA256 string           `json:"plan_sha256"`
		Binding    taskplan.Binding `json:"binding"`
	}
	if err := json.Unmarshal(result.Content, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.PlanID == "" || payload.Version != 1 ||
		len(payload.PlanSHA256) != 64 ||
		payload.Binding.PlanID != payload.PlanID ||
		payload.Binding.SHA256 != payload.PlanSHA256 {
		t.Fatalf("payload = %#v", payload)
	}
}

func TestSetTaskPlanRejectsGoalRewrite(t *testing.T) {
	service := taskplan.NewService(taskplan.NewMemoryRepository())
	_, err := NewSetTaskPlanTool(service).Execute(
		context.Background(),
		Call{
			RunID:    "run-tool",
			RunInput: "Original goal",
			Arguments: json.RawMessage(`{
				"goal":"Different goal",
				"max_side_effect":"none",
				"forbidden_actions":[],
				"steps":[{
					"id":"verify",
					"intent":"Verify",
					"action":"wait_for",
					"target":"Done",
					"expected_occurrences":1,
					"idempotency":"idempotent",
					"side_effect":"none",
					"preconditions":[],
					"completion_conditions":["done"]
				}]
			}`),
		},
	)
	if err == nil || !strings.Contains(err.Error(), "exactly match") {
		t.Fatalf("error = %v, want goal mismatch", err)
	}
}

func TestBrowserToolDoesNotForwardPlanMetadata(t *testing.T) {
	client := &fakeCapabilityClient{}
	handler := NewBrowserTools(client)[0]
	_, err := handler.Execute(context.Background(), Call{
		Name: "explore_page",
		Arguments: json.RawMessage(`{
			"url":"https://example.test",
			"plan_step_ids":["open"]
		}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(client.arguments), "plan_step_ids") {
		t.Fatalf("browser arguments leaked plan metadata: %s", client.arguments)
	}
}
