package tools

import (
	"context"
	"encoding/json"
	"strconv"
)

type DSLCapabilityClient interface {
	GenerateDSL(
		ctx context.Context,
		actorUserID int64,
		runID string,
		projectID int64,
		conversationID string,
		arguments json.RawMessage,
	) (json.RawMessage, error)
}

type GenerateDSLTool struct {
	client DSLCapabilityClient
}

func NewGenerateDSLTool(client DSLCapabilityClient) GenerateDSLTool {
	return GenerateDSLTool{client: client}
}

func (t GenerateDSLTool) Definition() Definition {
	return Definition{
		Name: "generate_dsl",
		Description: "Validate and persist a research-v1 Action IR case authored from the user's goal and verified page elements. " +
			"The plan_binding must match the current persisted task plan. " +
			"Every step action must be one of: goto, click, input, wait_for, assert_text, " +
			"assert_url_contains, capture_text. Every step must declare intent, preconditions, postconditions, idempotency, and side_effect. " +
			"Author only semantic target and optional page_state/target_strategy fields; never author selector, candidates, or locator_confidence. " +
			"Locator preflight derives executable candidates from the submitted accessibility evidence. " +
			"Use preconditions for required pre-action state and postconditions for outcomes. " +
			"network_request conditions may match URL substring, method, and status on one observed event. " +
			"Express standalone visibility checks as wait_for, " +
			"and give every cross-page anchor click a url_contains postcondition with the expected destination URL or path. " +
			"never as assert_visible. Targets must be grounded in verified page elements. " +
			"When a goal requires search through page controls, preserve the verified input then click steps; " +
			"do not replace them with goto to a constructed search-result URL. " +
			"Input trigger is optional and only accepts Enter or Tab; omit it for ordinary semantic input, " +
			"and use a separate click step for a search button.",
		InputSchema: json.RawMessage(`{
			"type":"object",
			"additionalProperties":false,
			"properties":{
				"plan_binding":{
					"type":"object",
					"additionalProperties":false,
					"properties":{
						"plan_id":{"type":"string","minLength":1},
						"version":{"type":"integer","minimum":1},
						"sha256":{"type":"string","pattern":"^[0-9a-f]{64}$"}
					},
					"required":["plan_id","version","sha256"]
				},
				"case":{"$ref":"#/$defs/case"},
				"a11y_nodes_by_state":{
					"type":"object",
					"patternProperties":{"^.+$":{"type":"array","items":{"$ref":"#/$defs/a11y_node"}}},
					"additionalProperties":false
				}
			},
			"required":["plan_binding","case","a11y_nodes_by_state"],
			"$defs":{
				"condition":{
					"type":"object",
					"additionalProperties":false,
					"properties":{
						"type":{"type":"string","enum":["url_contains","url_changes","text_visible","text_gone","element_visible","element_gone","network_request","dom_changed","value_changed"]},
						"value":{"type":["string","null"]},
						"method":{"type":["string","null"],"minLength":1},
						"status":{"type":["integer","null"],"minimum":100,"maximum":599},
						"timeout_ms":{"type":"integer","minimum":100,"maximum":30000}
					},
					"required":["type"],
					"allOf":[
						{
							"if":{"properties":{"type":{"const":"url_contains"}},"required":["type"]},
							"then":{"properties":{"value":{"type":"string","minLength":1}},"required":["value"]}
						},
						{
							"if":{"properties":{"type":{"const":"network_request"}},"required":["type"]},
							"then":{"anyOf":[
								{"properties":{"value":{"type":"string","minLength":1}},"required":["value"]},
								{"properties":{"method":{"type":"string","minLength":1}},"required":["method"]},
								{"properties":{"status":{"type":"integer","minimum":100,"maximum":599}},"required":["status"]}
							]}
						}
					]
				},
				"input_contract":{
					"type":"object",
					"additionalProperties":false,
					"properties":{
						"name":{"type":"string","minLength":1,"maxLength":100},
						"context_key":{"type":"string","minLength":1,"maxLength":100,"pattern":"^[A-Za-z_][A-Za-z0-9_]*$"},
						"value_type":{"type":"string","enum":["string","number","boolean","object","array"]},
						"description":{"type":["string","null"]},
						"required":{"type":"boolean"},
						"value":{"type":["string","null"]}
					},
					"required":["name","context_key","value_type"]
				},
				"output_contract":{
					"type":"object",
					"additionalProperties":false,
					"properties":{
						"name":{"type":"string","minLength":1,"maxLength":100},
						"context_key":{"type":"string","minLength":1,"maxLength":100,"pattern":"^[A-Za-z_][A-Za-z0-9_]*$"},
						"value_type":{"type":"string","enum":["string","number","boolean","object","array"]},
						"description":{"type":["string","null"]},
						"source":{"type":["string","null"],"enum":["latest_url","error_message","status","last_step_url","last_step_page_title","last_step_target","last_step_value","last_step_error_message",null]}
					},
					"required":["name","context_key","value_type"]
				},
				"case":{
					"type":"object",
					"additionalProperties":false,
					"properties":{
						"profile":{"type":"string","const":"research-v1","default":"research-v1"},
						"name":{"type":"string","minLength":1,"maxLength":200},
						"description":{"type":["string","null"],"minLength":1,"maxLength":1000},
						"base_url":{"type":["string","null"],"minLength":1,"maxLength":500},
						"input_contract":{"type":"array","items":{"$ref":"#/$defs/input_contract"}},
						"output_contract":{"type":"array","items":{"$ref":"#/$defs/output_contract"}},
						"steps":{"type":"array","minItems":1,"items":{"oneOf":[
							{"$ref":"#/$defs/goto_step"},
							{"$ref":"#/$defs/click_step"},
							{"$ref":"#/$defs/input_step"},
							{"$ref":"#/$defs/wait_for_step"},
							{"$ref":"#/$defs/assert_text_step"},
							{"$ref":"#/$defs/assert_url_step"},
							{"$ref":"#/$defs/capture_text_step"}
						]}}
					},
					"required":["profile","name","steps"]
				},
				"goto_step":{
					"type":"object","additionalProperties":false,
					"properties":{
						"action":{"const":"goto"},"intent":{"type":"string","minLength":1,"maxLength":500},
						"target":{"type":"string","minLength":1},"value":{"type":"string","minLength":1},
						"preconditions":{"type":"array","items":{"$ref":"#/$defs/condition"}},
						"postconditions":{"type":"array","minItems":1,"items":{"$ref":"#/$defs/condition"}},
						"idempotency":{"const":"idempotent"},"side_effect":{"const":"browser_state"}
					},
					"required":["action","intent","target","value","preconditions","postconditions","idempotency","side_effect"]
				},
				"click_step":{
					"type":"object","additionalProperties":false,
					"properties":{
						"action":{"const":"click"},"intent":{"type":"string","minLength":1,"maxLength":500},
						"target":{"type":"string","minLength":1},"page_state":{"type":["string","null"]},
						"target_strategy":{"$ref":"#/$defs/target_strategy"},
						"preconditions":{"type":"array","minItems":1,"items":{"$ref":"#/$defs/condition"}},
						"postconditions":{"type":"array","minItems":1,"items":{"$ref":"#/$defs/condition"}},
						"idempotency":{"type":"string","enum":["idempotent","non_idempotent"]},
						"side_effect":{"type":"string","enum":["none","browser_state","external_state","unknown"]}
					},
					"required":["action","intent","target","preconditions","postconditions","idempotency","side_effect"]
				},
				"input_step":{
					"type":"object","additionalProperties":false,
					"properties":{
						"action":{"const":"input"},"intent":{"type":"string","minLength":1,"maxLength":500},
						"target":{"type":"string","minLength":1},"value":{"type":"string"},
						"trigger":{"type":["string","null"],"enum":["Enter","Tab",null]},
						"page_state":{"type":["string","null"]},"target_strategy":{"$ref":"#/$defs/target_strategy"},
						"preconditions":{"type":"array","minItems":1,"items":{"$ref":"#/$defs/condition"}},
						"postconditions":{"type":"array","minItems":1,"items":{"$ref":"#/$defs/condition"}},
						"idempotency":{"const":"idempotent"},"side_effect":{"const":"browser_state"}
					},
					"required":["action","intent","target","value","preconditions","postconditions","idempotency","side_effect"]
				},
				"wait_for_step":{
					"type":"object","additionalProperties":false,
					"properties":{
						"action":{"const":"wait_for"},"intent":{"type":"string","minLength":1,"maxLength":500},
						"target":{"type":"string","minLength":1},"timeout_ms":{"type":"integer","minimum":1,"maximum":60000},
						"page_state":{"type":["string","null"]},"target_strategy":{"$ref":"#/$defs/target_strategy"},
						"preconditions":{"type":"array","items":{"$ref":"#/$defs/condition"}},
						"postconditions":{"type":"array","items":{"$ref":"#/$defs/condition"}},
						"idempotency":{"const":"idempotent"},"side_effect":{"const":"none"}
					},
					"required":["action","intent","target","preconditions","postconditions","idempotency","side_effect"]
				},
				"assert_text_step":{
					"type":"object","additionalProperties":false,
					"properties":{
						"action":{"const":"assert_text"},"intent":{"type":"string","minLength":1,"maxLength":500},
						"target":{"type":"string","minLength":1},"value":{"type":"string","minLength":1},
						"page_state":{"type":["string","null"]},"target_strategy":{"$ref":"#/$defs/target_strategy"},
						"preconditions":{"type":"array","items":{"$ref":"#/$defs/condition"}},
						"postconditions":{"type":"array","items":{"$ref":"#/$defs/condition"}},
						"idempotency":{"const":"idempotent"},"side_effect":{"const":"none"}
					},
					"required":["action","intent","target","value","preconditions","postconditions","idempotency","side_effect"]
				},
				"assert_url_step":{
					"type":"object","additionalProperties":false,
					"properties":{
						"action":{"const":"assert_url_contains"},"intent":{"type":"string","minLength":1,"maxLength":500},
						"target":{"type":"string","minLength":1},"value":{"type":"string","minLength":1},
						"preconditions":{"type":"array","items":{"$ref":"#/$defs/condition"}},
						"postconditions":{"type":"array","items":{"$ref":"#/$defs/condition"}},
						"idempotency":{"const":"idempotent"},"side_effect":{"const":"none"}
					},
					"required":["action","intent","target","value","preconditions","postconditions","idempotency","side_effect"]
				},
				"capture_text_step":{
					"type":"object","additionalProperties":false,
					"properties":{
						"action":{"const":"capture_text"},"intent":{"type":"string","minLength":1,"maxLength":500},
						"target":{"type":"string","minLength":1},"context_key":{"type":"string","minLength":1},
						"page_state":{"type":["string","null"]},"target_strategy":{"$ref":"#/$defs/target_strategy"},
						"preconditions":{"type":"array","items":{"$ref":"#/$defs/condition"}},
						"postconditions":{"type":"array","items":{"$ref":"#/$defs/condition"}},
						"idempotency":{"const":"idempotent"},"side_effect":{"const":"none"}
					},
					"required":["action","intent","target","context_key","preconditions","postconditions","idempotency","side_effect"]
				},
				"target_strategy":{"type":["string","null"],"enum":["css","xpath","data-testid","element_id","tag",null]},
				"selector_evidence":{
					"type":"object","additionalProperties":false,
					"properties":{"strategy":{"type":"string"},"selector":{"type":"string"},"name":{"type":"string"},"source":{"type":"string"}},
					"required":["selector"]
				},
				"dom":{
					"type":"object","additionalProperties":false,
					"properties":{
						"tag":{"type":"string"},
						"attrs":{"type":"object","patternProperties":{"^.*$":{"type":"string"}},"additionalProperties":false}
					}
				},
				"a11y_node":{
					"type":"object","additionalProperties":false,
					"properties":{
						"node_id":{"type":"string"},"backend_dom_node_id":{},
						"parent_id":{"type":"string"},"role":{"type":"string"},"name":{"type":"string"},
						"page_state":{"type":"string"},"source":{"type":"string"},
						"focusable":{"type":"boolean"},"disabled":{"type":"boolean"},
						"dom":{"$ref":"#/$defs/dom"},
						"verified_selectors":{"type":"array","items":{"$ref":"#/$defs/selector_evidence"}}
					}
				}
			}
		}`),
	}
}

func (t GenerateDSLTool) Execute(ctx context.Context, call Call) (Result, error) {
	content, err := t.client.GenerateDSL(
		ctx,
		call.ActorUserID,
		call.RunID,
		call.ProjectID,
		call.ConversationID,
		call.Arguments,
	)
	if err != nil {
		return Result{}, err
	}
	var generated struct {
		GenerationID int64 `json:"generation_id"`
	}
	if err := json.Unmarshal(content, &generated); err != nil {
		return Result{}, err
	}
	result := Result{Content: content}
	if generated.GenerationID > 0 {
		result.Artifact = &Artifact{
			Type: "dsl_generation",
			ID:   strconv.FormatInt(generated.GenerationID, 10),
		}
	}
	return result, nil
}
