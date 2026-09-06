package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

type TurnHandler func(context.Context, ModelResponse) (continueLoop bool, err error)
type ModelContextFactory func(context.Context) context.Context

type Loop struct {
	model        Model
	definitions  []ToolDefinition
	systemPrompt string
	maxTurns     int
}

func NewLoop(
	model Model,
	definitions []ToolDefinition,
	systemPrompt string,
	maxTurns int,
) *Loop {
	if maxTurns < 1 {
		maxTurns = 1
	}
	return &Loop{
		model:        model,
		definitions:  append([]ToolDefinition(nil), definitions...),
		systemPrompt: systemPrompt,
		maxTurns:     maxTurns,
	}
}

func (l *Loop) Run(
	ctx context.Context,
	transcript *[]Message,
	handle TurnHandler,
) error {
	return l.RunWithModelContext(ctx, transcript, nil, handle)
}

func (l *Loop) RunWithModelContext(
	ctx context.Context,
	transcript *[]Message,
	modelContext ModelContextFactory,
	handle TurnHandler,
) error {
	for range l.maxTurns {
		callContext := ctx
		if modelContext != nil {
			callContext = modelContext(ctx)
		}
		response, err := l.model.Complete(
			callContext,
			append([]Message{{Role: "system", Content: l.systemPrompt}}, (*transcript)...),
			l.definitions,
		)
		if err != nil {
			return err
		}
		*transcript = append(*transcript, Message{
			Role:      "assistant",
			Content:   response.Content,
			ToolCalls: response.ToolCalls,
		})
		continueLoop, err := handle(ctx, response)
		if err != nil {
			return err
		}
		if !continueLoop {
			return nil
		}
	}
	message := fmt.Sprintf("agent exceeded maximum turns: %d", l.maxTurns)
	if lastError := latestToolError(*transcript); lastError != "" {
		message += "; last tool error: " + lastError
	}
	return fmt.Errorf("%s", message)
}

func latestToolError(transcript []Message) string {
	for index := len(transcript) - 1; index >= 0; index-- {
		message := transcript[index]
		if message.Role != "tool" {
			continue
		}
		var result struct {
			Status  string `json:"status"`
			Message string `json:"message"`
		}
		if json.Unmarshal([]byte(message.Content), &result) != nil ||
			result.Status != "error" {
			return ""
		}
		return strings.TrimSpace(result.Message)
	}
	return ""
}
