package agentcore

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/tools"
)

var askUserInputSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "questions": {
      "type": "array",
      "minItems": 1,
      "maxItems": 3,
      "items": {
        "type": "object",
        "properties": {
          "id": {"type": "string"},
          "question": {"type": "string"},
          "type": {"type": "string", "enum": ["single_select", "multi_select", "text", "confirm"]},
          "required": {"type": "boolean"},
          "options": {
            "type": "array",
            "items": {
              "type": "object",
              "properties": {
                "value": {"type": "string"},
                "label": {"type": "string"},
                "description": {"type": "string"}
              },
              "required": ["value", "label"]
            }
          }
        },
        "required": ["id", "question", "type", "required"]
      }
    }
  },
  "required": ["questions"]
}`)

type AskUserTool struct{}

func (AskUserTool) Definition() tools.Definition {
	return tools.Definition{
		Name:        "ask_user_question",
		Description: "Ask the user for missing information or approval. Use only when progress cannot continue safely without the answer.",
		InputSchema: askUserInputSchema,
	}
}

func (AskUserTool) Execute(_ context.Context, call tools.Call) (tools.Result, error) {
	var request AskUserRequest
	if err := json.Unmarshal(call.Arguments, &request); err != nil {
		return tools.Result{}, fmt.Errorf("decode ask_user_question arguments: %w", err)
	}
	if err := validateQuestions(request.Questions); err != nil {
		return tools.Result{}, err
	}
	payload, err := json.Marshal(request)
	if err != nil {
		return tools.Result{}, fmt.Errorf("encode ask_user_question payload: %w", err)
	}
	return tools.Result{
		Pending: &tools.Pending{
			Kind:    "user_input",
			Payload: payload,
		},
	}, nil
}
