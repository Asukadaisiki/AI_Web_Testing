package tools

import (
	"context"
	"encoding/json"
	"strconv"
)

type DSLCapabilityClient interface {
	GenerateDSL(
		ctx context.Context,
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
		Description: "Generate a candidate structured DSL after page element coverage has been validated. " +
			"The result is a draft and must not be described as executed.",
		InputSchema: json.RawMessage(`{
			"type":"object",
			"properties":{
				"prompt":{"type":"string","description":"Complete test intent and expected assertions"},
				"base_url":{"type":"string"},
				"flow_steps":{"type":"array","items":{"type":"object"}},
				"a11y_nodes_by_state":{
					"type":"object",
					"additionalProperties":{"type":"array","items":{"type":"object"}}
				},
				"scenario_variables":{"type":"array","items":{"type":"object"}},
				"user_context":{"type":"string"}
			},
			"required":["prompt","base_url","a11y_nodes_by_state"]
		}`),
	}
}

func (t GenerateDSLTool) Execute(ctx context.Context, call Call) (Result, error) {
	content, err := t.client.GenerateDSL(
		ctx,
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
