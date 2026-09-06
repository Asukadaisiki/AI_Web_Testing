package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

type fakeCapabilityClient struct {
	capability      string
	projectID       int64
	conversationID  string
	arguments       json.RawMessage
	reportResponses []json.RawMessage
	reportCalls     int
	repairResponse  json.RawMessage
}

func (c *fakeCapabilityClient) ExecuteBrowserCapability(
	_ context.Context,
	capability string,
	_ int64,
	projectID int64,
	conversationID string,
	arguments json.RawMessage,
) (json.RawMessage, error) {
	c.capability = capability
	c.projectID = projectID
	c.conversationID = conversationID
	c.arguments = arguments
	return json.RawMessage(`{"ok":true}`), nil
}

func (c *fakeCapabilityClient) GenerateDSL(
	_ context.Context,
	_ int64,
	projectID int64,
	conversationID string,
	arguments json.RawMessage,
) (json.RawMessage, error) {
	c.capability = "generate_dsl"
	c.projectID = projectID
	c.conversationID = conversationID
	c.arguments = arguments
	return json.RawMessage(`{"generation_id":1}`), nil
}

func (c *fakeCapabilityClient) ExecuteDSL(
	_ context.Context,
	_ int64,
	runID string,
	projectID int64,
	conversationID string,
	arguments json.RawMessage,
) (json.RawMessage, error) {
	c.capability = "execute_dsl"
	c.projectID = projectID
	c.conversationID = conversationID
	c.arguments = arguments
	return json.RawMessage(`{"batch_id":3,"status":"pending"}`), nil
}

func (c *fakeCapabilityClient) GetReport(
	_ context.Context,
	_ int64,
	projectID int64,
	conversationID string,
	arguments json.RawMessage,
) (json.RawMessage, error) {
	c.capability = "get_report"
	c.projectID = projectID
	c.conversationID = conversationID
	c.arguments = arguments
	c.reportCalls++
	if len(c.reportResponses) > 0 {
		response := c.reportResponses[0]
		c.reportResponses = c.reportResponses[1:]
		return response, nil
	}
	return json.RawMessage(`{"id":3,"status":"passed"}`), nil
}

func (c *fakeCapabilityClient) PrepareFixAndRetry(
	_ context.Context,
	_ int64,
	projectID int64,
	conversationID string,
	arguments json.RawMessage,
) (json.RawMessage, error) {
	c.capability = "fix_and_retry"
	c.projectID = projectID
	c.conversationID = conversationID
	c.arguments = arguments
	if len(c.repairResponse) > 0 {
		return c.repairResponse, nil
	}
	return json.RawMessage(`{"source_batch_id":3,"status":"repair_ready","strategy":"re_explore"}`), nil
}

func TestGetReportToolWaitsForTerminalStatus(t *testing.T) {
	client := &fakeCapabilityClient{
		reportResponses: []json.RawMessage{
			json.RawMessage(`{"id":3,"status":"running"}`),
			json.RawMessage(`{"id":3,"status":"passed"}`),
		},
	}
	handler := NewGetReportTool(client)
	handler.pollInterval = time.Millisecond
	handler.maxWait = time.Second
	result, err := handler.Execute(context.Background(), Call{
		ProjectID:      7,
		ConversationID: "11",
		Name:           "get_report",
		Arguments:      json.RawMessage(`{"batch_id":3}`),
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if client.reportCalls != 2 {
		t.Fatalf("report calls = %d, want 2", client.reportCalls)
	}
	if string(result.Content) != `{"id":3,"status":"passed"}` {
		t.Fatalf("result = %s", result.Content)
	}
}

func TestFixAndRetryPublishesRepairPlan(t *testing.T) {
	client := &fakeCapabilityClient{}
	handler := NewFixAndRetryTool(client)
	result, err := handler.Execute(context.Background(), Call{
		ProjectID:      7,
		ConversationID: "11",
		Name:           "fix_and_retry",
		Arguments:      json.RawMessage(`{"batch_id":3}`),
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.Artifact == nil || result.Artifact.Type != "repair_plan" || result.Artifact.ID != "3" {
		t.Fatalf("artifact = %#v", result.Artifact)
	}
}

func TestFixAndRetryDoesNotPublishPlanWhenRepairIsNotRequired(t *testing.T) {
	client := &fakeCapabilityClient{
		repairResponse: json.RawMessage(
			`{"source_batch_id":3,"status":"not_required","strategy":"none"}`,
		),
	}
	handler := NewFixAndRetryTool(client)
	result, err := handler.Execute(context.Background(), Call{
		ProjectID:      7,
		ConversationID: "11",
		Name:           "fix_and_retry",
		Arguments:      json.RawMessage(`{"batch_id":3}`),
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.Artifact != nil {
		t.Fatalf("artifact = %#v, want nil", result.Artifact)
	}
}

func TestBrowserToolForwardsRunContext(t *testing.T) {
	client := &fakeCapabilityClient{}
	handler := NewBrowserTools(client)[0]
	_, err := handler.Execute(context.Background(), Call{
		ProjectID:      7,
		ConversationID: "11",
		Name:           "explore_page",
		Arguments:      json.RawMessage(`{"url":"https://example.com"}`),
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if client.capability != "explore_page" ||
		client.projectID != 7 ||
		client.conversationID != "11" {
		t.Fatalf("forwarded context = %#v", client)
	}
}

func TestGenerateDSLToolForwardsRunContext(t *testing.T) {
	client := &fakeCapabilityClient{}
	handler := NewGenerateDSLTool(client)
	result, err := handler.Execute(context.Background(), Call{
		ProjectID:      7,
		ConversationID: "11",
		Name:           "generate_dsl",
		Arguments:      json.RawMessage(`{"prompt":"test"}`),
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if client.capability != "generate_dsl" || string(result.Content) != `{"generation_id":1}` {
		t.Fatalf("result = %s, client = %#v", result.Content, client)
	}
}

func TestGenerateDSLToolSchemaRestrictsSupportedActions(t *testing.T) {
	definition := NewGenerateDSLTool(&fakeCapabilityClient{}).Definition()
	var schema struct {
		Properties struct {
			Case struct {
				Properties struct {
					Steps struct {
						Items struct {
							OneOf []struct {
								Properties struct {
									Action struct {
										Const string `json:"const"`
									} `json:"action"`
								} `json:"properties"`
								Required []string `json:"required"`
							} `json:"oneOf"`
						} `json:"items"`
					} `json:"steps"`
				} `json:"properties"`
			} `json:"case"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(definition.InputSchema, &schema); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	variants := schema.Properties.Case.Properties.Steps.Items.OneOf
	if len(variants) != 7 {
		t.Fatalf("variants = %#v, want 7 supported actions", variants)
	}
	requiredByAction := map[string][]string{
		"goto":                {"action", "value"},
		"click":               {"action", "target"},
		"input":               {"action", "target", "value"},
		"wait_for":            {"action", "target"},
		"assert_text":         {"action", "target", "value"},
		"assert_url_contains": {"action", "value"},
		"capture_text":        {"action", "target", "context_key"},
	}
	for _, variant := range variants {
		action := variant.Properties.Action.Const
		required, ok := requiredByAction[action]
		if !ok {
			t.Fatalf("variant has no action schema: %#v", variant)
		}
		if len(variant.Required) != len(required) {
			t.Fatalf("%s required = %#v, want %#v", action, variant.Required, required)
		}
		for index := range required {
			if variant.Required[index] != required[index] {
				t.Fatalf("%s required = %#v, want %#v", action, variant.Required, required)
			}
		}
	}
}

func TestBrowserToolSchemasAllowStateCaptureAndExposeOnlyAdvisoryValidation(t *testing.T) {
	definitions := NewBrowserTools(&fakeCapabilityClient{})
	if !strings.Contains(definitions[1].Definition().Description, "do not jump directly") {
		t.Fatal("explore_flow contract does not prohibit direct search URL bypass")
	}
	var flowSchema map[string]any
	if err := json.Unmarshal(definitions[1].Definition().InputSchema, &flowSchema); err != nil {
		t.Fatalf("decode explore_flow schema: %v", err)
	}
	steps := flowSchema["properties"].(map[string]any)["steps"].(map[string]any)
	actions := steps["items"].(map[string]any)["properties"].(map[string]any)["actions"].(map[string]any)
	if actions["minItems"] != float64(0) {
		t.Fatalf("actions.minItems = %#v, want 0", actions["minItems"])
	}

	var validationSchema map[string]any
	if err := json.Unmarshal(definitions[2].Definition().InputSchema, &validationSchema); err != nil {
		t.Fatalf("decode validate_page_elements schema: %v", err)
	}
	properties := validationSchema["properties"].(map[string]any)
	if _, exists := properties["dsl_case"]; exists {
		t.Fatal("model-visible validation schema exposes dsl_case")
	}
	if _, exists := properties["a11y_nodes_by_state"]; exists {
		t.Fatal("model-visible validation schema exposes a11y_nodes_by_state")
	}
	required := validationSchema["required"].([]any)
	if len(required) != 2 ||
		required[0] != "required_elements" ||
		required[1] != "a11y_nodes" {
		t.Fatalf("validation required = %#v", required)
	}
}

func TestGenerateDSLToolSchemaUsesRuntimeTargetStrategyEnum(t *testing.T) {
	definition := NewGenerateDSLTool(&fakeCapabilityClient{}).Definition()
	if !strings.Contains(definition.Description, "do not replace them with goto") {
		t.Fatal("generate_dsl contract does not require real search input and click")
	}
	var schema map[string]any
	if err := json.Unmarshal(definition.InputSchema, &schema); err != nil {
		t.Fatalf("decode generate_dsl schema: %v", err)
	}
	caseSchema := schema["properties"].(map[string]any)["case"].(map[string]any)
	steps := caseSchema["properties"].(map[string]any)["steps"].(map[string]any)
	variants := steps["items"].(map[string]any)["oneOf"].([]any)
	want := []any{"css", "xpath", "data-testid", "element_id", "tag"}
	for _, raw := range variants {
		variant := raw.(map[string]any)
		properties := variant["properties"].(map[string]any)
		strategy, ok := properties["target_strategy"].(map[string]any)
		if !ok {
			continue
		}
		if got := strategy["enum"].([]any); len(got) != len(want)+1 || got[len(got)-1] != nil {
			t.Fatalf("target_strategy enum = %#v, want %#v", got, want)
		}
		types := strategy["type"].([]any)
		if len(types) != 2 || types[0] != "string" || types[1] != "null" {
			t.Fatalf("target_strategy type = %#v, want nullable string", types)
		}
	}
}

func TestExecuteDSLToolRequiresMatchingApproval(t *testing.T) {
	client := &fakeCapabilityClient{}
	handler := NewExecuteDSLTool(client)
	generationID := int64(8)
	call := Call{
		RunID:                "run-1",
		ProjectID:            7,
		ConversationID:       "11",
		Name:                 "execute_dsl",
		Arguments:            json.RawMessage(`{"generation_id":8}`),
		LatestGenerationID:   &generationID,
		ApprovedGenerationID: nil,
	}
	if _, err := handler.Execute(context.Background(), call); err == nil {
		t.Fatal("Execute() error = nil, want approval error")
	}

	call.ApprovedGenerationID = &generationID
	result, err := handler.Execute(context.Background(), call)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.Artifact == nil || result.Artifact.Type != "execution_batch" || result.Artifact.ID != "3" {
		t.Fatalf("artifact = %#v", result.Artifact)
	}
}
