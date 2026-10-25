package v3

import "encoding/json"

const decisionContentLimit = 2000

type publicDecisionMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type publicSelectedOperation struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

type publicDecisionContext struct {
	Objective          *publicDecisionMessage   `json:"objective,omitempty"`
	PriorObservation   *publicDecisionMessage   `json:"prior_observation,omitempty"`
	SelectedOperation  *publicSelectedOperation `json:"selected_operation,omitempty"`
	VisibleExplanation string                   `json:"visible_explanation,omitempty"`
}

func projectDecisionContext(input, output json.RawMessage) *publicDecisionContext {
	context := &publicDecisionContext{}

	var request struct {
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(input, &request); err == nil {
		latestUser := -1
		for index, message := range request.Messages {
			if message.Role != "user" {
				continue
			}
			latestUser = index
			context.Objective = nil
			if message.Content != "" {
				context.Objective = &publicDecisionMessage{
					Role:    message.Role,
					Content: truncateDecisionContent(message.Content),
				}
			}
		}
		if latestUser >= 0 {
			for _, message := range request.Messages[latestUser+1:] {
				if message.Role != "tool" {
					continue
				}
				context.PriorObservation = nil
				if message.Content != "" {
					context.PriorObservation = &publicDecisionMessage{
						Role:    message.Role,
						Content: truncateDecisionContent(message.Content),
					}
				}
			}
		}
	}

	var response struct {
		Message struct {
			Content   string `json:"content"`
			ToolCalls []struct {
				Name      string          `json:"name"`
				Arguments json.RawMessage `json:"arguments"`
			} `json:"tool_calls"`
		} `json:"message"`
	}
	if err := json.Unmarshal(output, &response); err == nil {
		if response.Message.Content != "" {
			context.VisibleExplanation = truncateDecisionContent(response.Message.Content)
		}
		if len(response.Message.ToolCalls) > 0 {
			first := response.Message.ToolCalls[0]
			if first.Name != "" && len(first.Arguments) > 0 {
				context.SelectedOperation = &publicSelectedOperation{
					Name:      first.Name,
					Arguments: append(json.RawMessage(nil), first.Arguments...),
				}
			}
		}
	}

	if context.Objective == nil &&
		context.PriorObservation == nil &&
		context.SelectedOperation == nil &&
		context.VisibleExplanation == "" {
		return nil
	}
	return context
}

func truncateDecisionContent(content string) string {
	runes := []rune(content)
	if len(runes) <= decisionContentLimit {
		return content
	}
	return string(runes[:decisionContentLimit-3]) + "..."
}
