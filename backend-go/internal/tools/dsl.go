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
			"The candidate must use only supported actions and grounded semantic targets.",
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
							"steps":{"type":"array","items":{"type":"object"}}
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
