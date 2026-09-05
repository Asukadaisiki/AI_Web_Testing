package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
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

type askUserRequest struct {
	Questions []askUserQuestion `json:"questions"`
}

type askUserQuestion struct {
	ID      string          `json:"id"`
	Prompt  string          `json:"question"`
	Type    string          `json:"type"`
	Options []askUserOption `json:"options,omitempty"`
}

type askUserOption struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

type AskUserTool struct{}

func (AskUserTool) Definition() Definition {
	return Definition{
		Name:        "ask_user_question",
		Description: "Ask the user for missing information or approval. Use only when progress cannot continue safely without the answer.",
		InputSchema: askUserInputSchema,
	}
}

func (AskUserTool) Execute(_ context.Context, call Call) (Result, error) {
	var request askUserRequest
	if err := json.Unmarshal(call.Arguments, &request); err != nil {
		return Result{}, fmt.Errorf("decode ask_user_question arguments: %w", err)
	}
	if err := validateAskUserQuestions(request.Questions); err != nil {
		return Result{}, err
	}
	payload, err := json.Marshal(request)
	if err != nil {
		return Result{}, fmt.Errorf("encode ask_user_question payload: %w", err)
	}
	return Result{
		Pending: &Pending{
			Kind:    "user_input",
			Payload: payload,
		},
	}, nil
}

func validateAskUserQuestions(questions []askUserQuestion) error {
	if len(questions) == 0 || len(questions) > 3 {
		return errors.New("questions must contain between 1 and 3 items")
	}
	seen := make(map[string]struct{}, len(questions))
	for _, question := range questions {
		if strings.TrimSpace(question.ID) == "" || strings.TrimSpace(question.Prompt) == "" {
			return errors.New("question id and question are required")
		}
		if _, exists := seen[question.ID]; exists {
			return fmt.Errorf("duplicate question id %q", question.ID)
		}
		seen[question.ID] = struct{}{}
		if question.Type == "single_select" && len(question.Options) < 2 {
			return fmt.Errorf("single_select question %q requires at least two options", question.ID)
		}
	}
	return nil
}
