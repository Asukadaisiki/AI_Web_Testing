package agent

import (
	"context"
	"fmt"
)

type TurnHandler func(context.Context, ModelResponse) (continueLoop bool, err error)

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
	for range l.maxTurns {
		response, err := l.model.Complete(
			ctx,
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
	return fmt.Errorf("agent exceeded maximum turns: %d", l.maxTurns)
}
