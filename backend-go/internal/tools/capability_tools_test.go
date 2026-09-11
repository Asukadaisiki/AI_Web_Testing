package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

type fakeCapabilityClient struct {
	capability      string
	runID           string
	projectID       int64
	conversationID  string
	arguments       json.RawMessage
	reportResponses []json.RawMessage
	reportCalls     int
	repairResponse  json.RawMessage
}

type isolatedProbeCapabilityClient struct {
	calls int
}

type observationCapabilityClient struct{}

func (observationCapabilityClient) ExecuteBrowserCapability(
	context.Context,
	string,
	int64,
	int64,
	string,
	json.RawMessage,
) (json.RawMessage, error) {
	return json.RawMessage(`{
		"url":"https://example.test/form",
		"a11y_nodes":[{"role":"button","name":"legacy"}],
		"observation_v2":{
			"schema_version":"browser.observation.v2",
			"observation_id":"obs-1",
			"page_state":{
				"state_id":"form",
				"revision":1,
				"url":"https://example.test/form",
				"title":"Form",
				"state_sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
			},
			"elements":[],
			"relations":[]
		}
	}`), nil
}

func (c *isolatedProbeCapabilityClient) ExecuteBrowserCapability(
	_ context.Context,
	_ string,
	_ int64,
	_ int64,
	_ string,
	arguments json.RawMessage,
) (json.RawMessage, error) {
	if !strings.Contains(string(arguments), `"target":"Add to cart"`) {
		return nil, errors.New("expected Add to cart probe")
	}
	c.calls++
	return json.RawMessage(`{
		"success":true,
		"context_evidence":{
			"execution_scope":"isolated_probe",
			"state_persisted":false
		},
		"pages":[{
			"url":"https://automationexercise.com/view_cart",
			"page_state":"S1",
			"status":"success",
			"actions":[{
				"action":"click",
				"target":"Add to cart",
				"status":"success"
			}],
			"a11y_nodes":[{
				"node_id":"cart-row",
				"role":"row",
				"name":"Blue Top Rs. 500 1 Rs. 500",
				"verified_selectors":[{
					"strategy":"css",
					"selector":"#product-1"
				}]
			}]
		}]
	}`), nil
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
	runID string,
	projectID int64,
	conversationID string,
	arguments json.RawMessage,
) (json.RawMessage, error) {
	c.capability = "generate_dsl"
	c.runID = runID
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
		RunID:          "run-1",
		ToolCallID:     "call-1",
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
	var arguments map[string]any
	if err := json.Unmarshal(client.arguments, &arguments); err != nil {
		t.Fatal(err)
	}
	if arguments["observation_schema_version"] != "v2" {
		t.Fatalf("forwarded arguments = %#v", arguments)
	}
	if arguments["probe_id"] != browserProbeID(Call{
		RunID: "run-1", ToolCallID: "call-1", Name: "explore_page",
	}) {
		t.Fatalf("forwarded probe lineage = %#v", arguments)
	}
}

func TestBrowserToolV2KeepsObservationAndDropsLegacyNodes(t *testing.T) {
	handler := NewBrowserTools(observationCapabilityClient{})[0]
	result, err := handler.Execute(context.Background(), Call{
		ProjectID: 7, ConversationID: "11", Name: "explore_page",
		Arguments: json.RawMessage(`{"url":"https://example.test/form"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(result.Content, &payload); err != nil {
		t.Fatal(err)
	}
	if _, exists := payload["a11y_nodes"]; exists {
		t.Fatalf("legacy a11y_nodes leaked into v2 result: %s", result.Content)
	}
	if _, exists := payload["observation_v2"]; !exists {
		t.Fatalf("observation_v2 missing: %s", result.Content)
	}
}

func TestExploreFlowMockIsolatesRepeatedSideEffectProbes(t *testing.T) {
	client := &isolatedProbeCapabilityClient{}
	handler := NewBrowserTools(client)[1]
	arguments := json.RawMessage(`{
		"base_url":"https://automationexercise.com/product_details/1",
		"steps":[{
			"url":"https://automationexercise.com/product_details/1",
			"actions":[
				{"action":"input","target":"Quantity","value":"1"},
				{"action":"click","target":"Add to cart"},
				{"action":"click","target":"View Cart"}
			]
		}]
	}`)

	for range 3 {
		result, err := handler.Execute(context.Background(), Call{
			ProjectID:      1057,
			ConversationID: "63",
			Name:           "explore_flow",
			Arguments:      arguments,
		})
		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		if !strings.Contains(string(result.Content), `"name":"Blue Top Rs. 500 1 Rs. 500"`) ||
			strings.Contains(string(result.Content), "Rs. 1000") {
			t.Fatalf("isolated probe result = %s", result.Content)
		}
	}
	if client.calls != 3 {
		t.Fatalf("probe calls = %d, want 3", client.calls)
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
	var schema map[string]any
	if err := json.Unmarshal(definition.InputSchema, &schema); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	definitions := schema["$defs"].(map[string]any)
	caseSchema := definitions["case"].(map[string]any)
	steps := caseSchema["properties"].(map[string]any)["steps"].(map[string]any)
	variants := steps["items"].(map[string]any)["oneOf"].([]any)
	if len(variants) != 7 {
		t.Fatalf("variants = %#v, want 7 supported actions", variants)
	}
	wantActions := map[string]bool{
		"goto": true, "click": true, "input": true, "wait_for": true,
		"assert_text": true, "assert_url_contains": true, "capture_text": true,
	}
	for _, rawVariant := range variants {
		ref := rawVariant.(map[string]any)["$ref"].(string)
		name := strings.TrimPrefix(ref, "#/$defs/")
		variant := definitions[name].(map[string]any)
		properties := variant["properties"].(map[string]any)
		action := properties["action"].(map[string]any)["const"].(string)
		if !wantActions[action] {
			t.Fatalf("unexpected action schema: %q", action)
		}
		delete(wantActions, action)
		required := variant["required"].([]any)
		for _, field := range []string{
			"action", "intent", "preconditions", "postconditions",
			"idempotency", "side_effect",
		} {
			if !containsSchemaString(required, field) {
				t.Fatalf("%s does not require %s: %#v", action, field, required)
			}
		}
	}
	if len(wantActions) != 0 {
		t.Fatalf("missing action schemas: %#v", wantActions)
	}
	profile := caseSchema["properties"].(map[string]any)["profile"].(map[string]any)
	if profile["default"] != "research-v2" ||
		!reflect.DeepEqual(
			profile["enum"],
			[]any{"research-v1", "research-v2"},
		) {
		t.Fatalf("profile schema = %#v", profile)
	}
	if !strings.Contains(
		definition.Description,
		"never author selector, locator candidates, or locator confidence",
	) {
		t.Fatal("generate_dsl description does not prohibit model-authored locators")
	}
	assertStrictObjectSchemas(t, schema, "$")
}

func containsSchemaString(values []any, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func assertStrictObjectSchemas(t *testing.T, value any, path string) {
	t.Helper()
	switch typed := value.(type) {
	case map[string]any:
		if typed["type"] == "object" && typed["additionalProperties"] != false {
			t.Fatalf("%s object schema is not strict: %#v", path, typed)
		}
		for key, child := range typed {
			assertStrictObjectSchemas(t, child, path+"."+key)
		}
	case []any:
		for index, child := range typed {
			assertStrictObjectSchemas(t, child, fmt.Sprintf("%s[%d]", path, index))
		}
	}
}

func TestBrowserToolSchemasAllowStateCaptureAndExposeOnlyAdvisoryValidation(t *testing.T) {
	definitions := NewBrowserTools(&fakeCapabilityClient{})
	if !strings.Contains(definitions[1].Definition().Description, "do not jump directly") {
		t.Fatal("explore_flow contract does not prohibit direct search URL bypass")
	}
	if !strings.Contains(definitions[1].Definition().Description, "isolated disposable probe context") ||
		!strings.Contains(definitions[1].Definition().Description, "quantity 2") {
		t.Fatal("explore_flow contract does not separate probe state from task orchestration")
	}
	if !strings.Contains(definitions[1].Definition().Description, "exploration_budget") ||
		!strings.Contains(definitions[1].Definition().Description, "Do not repeat") ||
		!strings.Contains(definitions[1].Definition().Description, "value_equals") {
		t.Fatal("explore_flow contract does not expose budgets and repeat/value guidance")
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
	actionProperties := actions["items"].(map[string]any)["properties"].(map[string]any)
	if _, exists := actionProperties["plan_step_id"]; !exists {
		t.Fatal("explore_flow action schema does not expose plan_step_id")
	}
	if _, exists := actionProperties["locator"]; !exists {
		t.Fatal("explore_flow action schema does not expose structured locator")
	}
	if _, exists := actionProperties["condition"]; !exists {
		t.Fatal("explore_flow action schema does not expose structured condition")
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
	if !strings.Contains(definition.Description, "omit it for ordinary semantic input") ||
		!strings.Contains(definition.Description, "separate click step for a search button") {
		t.Fatal("generate_dsl contract does not explain input trigger semantics")
	}
	var schema map[string]any
	if err := json.Unmarshal(definition.InputSchema, &schema); err != nil {
		t.Fatalf("decode generate_dsl schema: %v", err)
	}
	definitions := schema["$defs"].(map[string]any)
	strategy := definitions["target_strategy"].(map[string]any)
	want := []any{"css", "xpath", "data-testid", "element_id", "tag"}
	if got := strategy["enum"].([]any); len(got) != len(want)+1 || got[len(got)-1] != nil {
		t.Fatalf("target_strategy enum = %#v, want %#v", got, want)
	}
	types := strategy["type"].([]any)
	if len(types) != 2 || types[0] != "string" || types[1] != "null" {
		t.Fatalf("target_strategy type = %#v, want nullable string", types)
	}
}

func TestGenerateDSLToolSchemaRestrictsInputTrigger(t *testing.T) {
	definition := NewGenerateDSLTool(&fakeCapabilityClient{}).Definition()
	var schema map[string]any
	if err := json.Unmarshal(definition.InputSchema, &schema); err != nil {
		t.Fatalf("decode generate_dsl schema: %v", err)
	}
	definitions := schema["$defs"].(map[string]any)
	inputStep := definitions["input_step"].(map[string]any)
	properties := inputStep["properties"].(map[string]any)
	trigger := properties["trigger"].(map[string]any)
	want := []any{"Enter", "Tab", nil}
	if !reflect.DeepEqual(trigger["enum"], want) {
		t.Fatalf("trigger enum = %#v, want %#v", trigger["enum"], want)
	}
	if !reflect.DeepEqual(trigger["type"], []any{"string", "null"}) {
		t.Fatalf("trigger type = %#v, want nullable string", trigger["type"])
	}
}

func TestGenerateDSLToolSchemaMatchesConditionAndActionConstraints(t *testing.T) {
	definition := NewGenerateDSLTool(&fakeCapabilityClient{}).Definition()
	var schema map[string]any
	if err := json.Unmarshal(definition.InputSchema, &schema); err != nil {
		t.Fatalf("decode generate_dsl schema: %v", err)
	}
	definitions := schema["$defs"].(map[string]any)
	gotoProperties := definitions["goto_step"].(map[string]any)["properties"].(map[string]any)
	gotoPreconditions := gotoProperties["preconditions"].(map[string]any)
	gotoPostconditions := gotoProperties["postconditions"].(map[string]any)
	if _, exists := gotoPreconditions["minItems"]; exists {
		t.Fatalf("goto preconditions unexpectedly require an item: %#v", gotoPreconditions)
	}
	if gotoPostconditions["minItems"] != float64(1) {
		t.Fatalf("goto postconditions = %#v, want minItems 1", gotoPostconditions)
	}
	for _, stepName := range []string{"click_step", "input_step"} {
		properties := definitions[stepName].(map[string]any)["properties"].(map[string]any)
		for _, field := range []string{"preconditions", "postconditions"} {
			if properties[field].(map[string]any)["minItems"] != float64(1) {
				t.Fatalf("%s.%s does not require an item", stepName, field)
			}
		}
	}
	for _, contractName := range []string{"input_contract", "output_contract"} {
		properties := definitions[contractName].(map[string]any)["properties"].(map[string]any)
		if properties["name"].(map[string]any)["minLength"] != float64(1) ||
			properties["context_key"].(map[string]any)["pattern"] != "^[A-Za-z_][A-Za-z0-9_]*$" {
			t.Fatalf("%s string constraints = %#v", contractName, properties)
		}
	}
	condition := definitions["condition"].(map[string]any)
	if len(condition["allOf"].([]any)) != 2 {
		t.Fatalf("condition constraints = %#v, want url/network branches", condition)
	}
	networkBranch := condition["allOf"].([]any)[1].(map[string]any)
	then := networkBranch["then"].(map[string]any)
	if len(then["anyOf"].([]any)) != 3 {
		t.Fatalf("network_request constraint = %#v, want three alternatives", then)
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
