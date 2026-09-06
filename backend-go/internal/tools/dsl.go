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
		Description: "Validate and persist a structured DSL candidate authored from the user's goal and verified page elements. " +
			"Every step action must be one of: goto, click, input, wait_for, assert_text, " +
			"assert_url_contains, capture_text. Express visibility checks as wait_for or postconditions, " +
			"and give every cross-page anchor click a url_contains postcondition with the expected destination URL or path. " +
			"never as assert_visible. Targets must be grounded in verified page elements. " +
			"Do not author candidates, match_count, or locator_confidence; locator preflight adds them.",
		InputSchema: json.RawMessage(`{
			"type":"object",
			"properties":{
					"case":{
						"type":"object",
						"properties":{
							"name":{"type":"string"},
							"description":{"type":"string"},
							"base_url":{"type":"string"},
							"input_contract":{"type":"array","items":{"type":"object"}},
							"output_contract":{"type":"array","items":{"type":"object"}},
							"steps":{
								"type":"array",
								"minItems":1,
								"items":{
									"oneOf":[
										{
											"type":"object",
											"properties":{
												"action":{"type":"string","const":"goto"},
												"value":{"type":"string","description":"Absolute URL or path to navigate to."}
											},
											"required":["action","value"]
										},
										{
											"type":"object",
											"properties":{
												"action":{"type":"string","const":"click"},
												"target":{"type":"string"},
												"target_strategy":{"type":["string","null"],"enum":["css","xpath","data-testid","element_id","tag",null]},
												"page_state":{"type":"string"},
												"postconditions":{"type":"array","items":{
													"type":"object",
													"properties":{
														"type":{"type":"string","enum":["url_contains","url_changes","text_visible","text_gone","element_visible","element_gone","network_request","dom_changed","value_changed"]},
														"value":{"type":["string","null"],"description":"Expected destination URL/path, text, selector, or value. url_contains requires a non-empty target."},
														"timeout_ms":{"type":"integer","minimum":100,"maximum":30000}
													},
													"required":["type"]
												}}
											},
											"required":["action","target"]
										},
										{
											"type":"object",
											"properties":{
												"action":{"type":"string","const":"input"},
												"target":{"type":"string"},
												"value":{"type":"string"},
												"trigger":{"type":"string"},
												"target_strategy":{"type":["string","null"],"enum":["css","xpath","data-testid","element_id","tag",null]},
												"page_state":{"type":"string"},
												"postconditions":{"type":"array","items":{
													"type":"object",
													"properties":{
														"type":{"type":"string","enum":["url_contains","url_changes","text_visible","text_gone","element_visible","element_gone","network_request","dom_changed","value_changed"]},
														"value":{"type":["string","null"]},
														"timeout_ms":{"type":"integer","minimum":100,"maximum":30000}
													},
													"required":["type"]
												}}
											},
											"required":["action","target","value"]
										},
										{
											"type":"object",
											"properties":{
												"action":{"type":"string","const":"wait_for"},
												"target":{"type":"string"},
												"target_strategy":{"type":["string","null"],"enum":["css","xpath","data-testid","element_id","tag",null]},
												"page_state":{"type":"string"},
												"timeout_ms":{"type":"integer"}
											},
											"required":["action","target"]
										},
										{
											"type":"object",
											"properties":{
												"action":{"type":"string","const":"assert_text"},
												"target":{"type":"string"},
												"target_strategy":{"type":["string","null"],"enum":["css","xpath","data-testid","element_id","tag",null]},
												"page_state":{"type":"string"},
												"value":{"type":"string","description":"Expected text."}
											},
											"required":["action","target","value"]
										},
										{
											"type":"object",
											"properties":{
												"action":{"type":"string","const":"assert_url_contains"},
												"value":{"type":"string","description":"Expected URL fragment."}
											},
											"required":["action","value"]
										},
										{
											"type":"object",
											"properties":{
												"action":{"type":"string","const":"capture_text"},
												"target":{"type":"string"},
												"target_strategy":{"type":["string","null"],"enum":["css","xpath","data-testid","element_id","tag",null]},
												"page_state":{"type":"string"},
												"context_key":{"type":"string"}
											},
											"required":["action","target","context_key"]
										}
									]
								}
							}
						},
						"required":["name","steps"]
					},
				"a11y_nodes_by_state":{
					"type":"object",
					"additionalProperties":{"type":"array","items":{"type":"object"}}
					}
			},
				"required":["case","a11y_nodes_by_state"]
		}`),
	}
}

func (t GenerateDSLTool) Execute(ctx context.Context, call Call) (Result, error) {
	content, err := t.client.GenerateDSL(
		ctx,
		call.ActorUserID,
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
