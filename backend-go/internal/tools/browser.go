package tools

import (
	"context"
	"encoding/json"
)

type BrowserCapabilityClient interface {
	ExecuteBrowserCapability(
		ctx context.Context,
		capability string,
		actorUserID int64,
		projectID int64,
		conversationID string,
		arguments json.RawMessage,
	) (json.RawMessage, error)
}

type BrowserTool struct {
	name        string
	description string
	inputSchema json.RawMessage
	client      BrowserCapabilityClient
}

func NewBrowserTools(client BrowserCapabilityClient) []Handler {
	return []Handler{
		BrowserTool{
			name: "explore_page",
			description: "Open one known URL and return its accessibility elements and candidate links. " +
				"Use this first when the user has only provided one entry URL. " +
				"Bind the probe to the next pending task plan steps with plan_step_ids. " +
				"Each result reports per-plan and run-wide exploration_budget counters. " +
				"Do not repeat the same URL and plan_step_ids signature.",
			inputSchema: json.RawMessage(`{
				"type":"object",
				"properties":{
					"url":{"type":"string","description":"Absolute page URL"},
					"core_user_flow_text":{"type":"string","description":"User flow used to prioritize relevant elements"},
						"observation_schema_version":{"type":"string","enum":["v1","v2"],"default":"v2"},
					"plan_step_ids":{"type":"array","minItems":1,"items":{"type":"string"}}
				},
				"required":["url","plan_step_ids"]
			}`),
			client: client,
		},
		BrowserTool{
			name: "explore_flow",
			description: "Explore multiple page states in one browser session. " +
				"Use discovered links and a bounded sequence of click, input, and wait_for actions. " +
				"For search workflows, explore the actual input and search-control click; do not jump directly to a constructed search URL. " +
				"Each call runs in an isolated disposable probe context, so its browser and business state are never reused by another call or by official execution. " +
				"Express intended multiplicity such as quantity 2 in the flow actions and final DSL, not by relying on state accumulated across calls. " +
				"Each result reports per-plan and run-wide exploration_budget counters. " +
				"Do not repeat a completed flow signature; change its page, actions, or bound plan steps. " +
				"Set action.plan_step_id when an action is intended to ground a specific step; omit it only for supporting observation actions. " +
				"For a control value check, use wait_for with a semantic locator and condition value_equals instead of treating its label as text.",
			inputSchema: json.RawMessage(`{
				"type":"object",
				"properties":{
					"base_url":{"type":"string"},
					"flow_description":{"type":"string"},
						"observation_schema_version":{"type":"string","enum":["v1","v2"],"default":"v2"},
					"plan_step_ids":{"type":"array","minItems":1,"items":{"type":"string"}},
					"steps":{
						"type":"array",
						"minItems":1,
						"items":{
							"type":"object",
							"properties":{
								"url":{"type":"string"},
								"description":{"type":"string"},
								"actions":{
									"type":"array",
									"minItems":0,
									"items":{
										"type":"object",
										"properties":{
											"action":{"type":"string","enum":["click","input","wait_for"]},
												"plan_step_id":{"type":"string","minLength":1,"maxLength":64},
											"target":{"type":"string","minLength":1},
												"locator":{
													"oneOf":[
														{"type":"object","properties":{"kind":{"const":"role"},"role":{"type":"string","minLength":1},"name":{"type":"string"},"exact":{"type":"boolean","default":true}},"required":["kind","role"],"additionalProperties":false},
														{"type":"object","properties":{"kind":{"enum":["label","placeholder","text"]},"value":{"type":"string","minLength":1},"exact":{"type":"boolean","default":true}},"required":["kind","value"],"additionalProperties":false}
													]
												},
												"condition":{
													"type":"object",
													"properties":{
														"type":{"type":"string","enum":["visible","value_equals"]},
														"expected":{"type":"string"}
													},
													"required":["type"],
													"allOf":[{"if":{"properties":{"type":{"const":"value_equals"}}},"then":{"required":["expected"]}}],
													"additionalProperties":false
												},
											"value":{"type":"string"},
											"timeout_ms":{"type":"integer","minimum":1,"maximum":60000}
										},
											"required":["action"],
											"oneOf":[{"required":["target"]},{"required":["locator"]}],
											"allOf":[{"if":{"required":["locator"]},"then":{"properties":{"action":{"const":"wait_for"}}}}]
									}
								}
							}
						}
					}
				},
				"required":["steps","plan_step_ids"]
			}`),
			client: client,
		},
		BrowserTool{
			name: "validate_page_elements",
			description: "Advisory check for whether explored accessibility elements cover required user actions and assertions. " +
				"This does not authorize generation; generate_dsl performs bound case validation internally.",
			inputSchema: json.RawMessage(`{
				"type":"object",
				"properties":{
					"required_elements":{
						"type":"array",
						"items":{
							"type":"object",
							"properties":{
								"id":{"type":"string"},
								"description":{"type":"string"},
								"keywords":{"type":"array","items":{"type":"string"}},
								"roles":{"type":"array","items":{"type":"string"}}
							},
							"required":["id","description","keywords"]
						}
					},
					"a11y_nodes":{"type":"array","items":{"type":"object"}}
				},
				"required":["required_elements","a11y_nodes"]
			}`),
			client: client,
		},
	}
}

func (t BrowserTool) Definition() Definition {
	return Definition{
		Name:        t.name,
		Description: t.description,
		InputSchema: t.inputSchema,
	}
}

func (t BrowserTool) Execute(ctx context.Context, call Call) (Result, error) {
	arguments := call.Arguments
	observationVersion := ""
	if t.name == "explore_page" || t.name == "explore_flow" {
		var payload map[string]any
		if err := json.Unmarshal(call.Arguments, &payload); err != nil {
			return Result{}, err
		}
		delete(payload, "plan_step_ids")
		if _, exists := payload["observation_schema_version"]; !exists {
			payload["observation_schema_version"] = "v2"
		}
		observationVersion, _ = payload["observation_schema_version"].(string)
		var err error
		arguments, err = json.Marshal(payload)
		if err != nil {
			return Result{}, err
		}
	}
	content, err := t.client.ExecuteBrowserCapability(
		ctx,
		t.name,
		call.ActorUserID,
		call.ProjectID,
		call.ConversationID,
		arguments,
	)
	if err != nil {
		return Result{}, err
	}
	if observationVersion == "v2" {
		content, err = compactV2ExplorationResult(content)
		if err != nil {
			return Result{}, err
		}
	}
	return Result{Content: content}, nil
}

func compactV2ExplorationResult(raw json.RawMessage) (json.RawMessage, error) {
	var result map[string]any
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, err
	}
	if _, exists := result["observation_v2"]; exists {
		delete(result, "a11y_nodes")
	}
	if pages, ok := result["pages"].([]any); ok {
		for _, rawPage := range pages {
			page, _ := rawPage.(map[string]any)
			if page == nil {
				continue
			}
			if _, exists := page["observation_v2"]; exists {
				delete(page, "a11y_nodes")
			}
		}
	}
	return json.Marshal(result)
}
