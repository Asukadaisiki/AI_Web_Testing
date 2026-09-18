package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

type TurnHandler func(context.Context, ModelResponse) (continueLoop bool, err error)
type ModelContextFactory func(context.Context) context.Context

// TurnBudget returns the effective maximum number of turns for the current
// loop run. A value <= 0 keeps the fixed NewLoop budget. The loop re-queries
// the budget before every turn so a harness can scale it with TaskPlan size.
type TurnBudget func(used int) int

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
	return l.RunWithTurnBudget(ctx, transcript, modelContext, handle, nil)
}

// RunWithTurnBudget runs the loop like RunWithModelContext but consults the
// optional budget before every turn. The budget may grow as the TaskPlan
// grows; a nil budget keeps the fixed NewLoop limit.
func (l *Loop) RunWithTurnBudget(
	ctx context.Context,
	transcript *[]Message,
	modelContext ModelContextFactory,
	handle TurnHandler,
	turnBudget TurnBudget,
) error {
	for used := 0; ; used++ {
		limit := l.maxTurns
		if turnBudget != nil {
			if dynamic := turnBudget(used); dynamic > limit {
				limit = dynamic
			}
		}
		if used >= limit {
			message := fmt.Sprintf("agent exceeded maximum turns: %d", limit)
			if lastError := latestToolError(*transcript); lastError != "" {
				message += "; last tool error: " + lastError
			}
			return fmt.Errorf("%s", message)
		}
		callContext := ctx
		if modelContext != nil {
			callContext = modelContext(ctx)
		}
		response, err := l.model.Complete(
			callContext,
			append(
				[]Message{{Role: "system", Content: l.systemPrompt}},
				ModelContext(*transcript)...,
			),
			l.definitions,
		)
		if err != nil {
			return err
		}
		*transcript = append(*transcript, Message{
			Role:             "assistant",
			Content:          response.Content,
			ReasoningContent: response.ReasoningContent,
			ToolCalls:        response.ToolCalls,
		})
		continueLoop, err := handle(ctx, response)
		if err != nil {
			return err
		}
		if !continueLoop {
			return nil
		}
	}
}

// ModelContext returns the messages a model call should see: the tail of the
// transcript starting at the most recent segment boundary.
//
// A tool-carrying request must replay the reasoning of every assistant turn it
// still contains (BUG-203), so replaying the whole run would grow the request
// without bound. Segmenting closes a finished phase: the durable transcript
// keeps every turn for tool governance and audit, while the model starts the
// next phase from a compact handoff instead of the full exploration history.
// See docs/plan/2026-09-18-context-budget-design.md.
func ModelContext(transcript []Message) []Message {
	for index := len(transcript) - 1; index >= 0; index-- {
		if transcript[index].SegmentBoundary {
			return transcript[index:]
		}
	}
	return transcript
}

// latestToolError returns the failure message of the most recent tool result
// that is itself a failure. A successful tool result (success=true) clears
// the stale error even if an earlier turn failed: max-turn terminal state
// must not misattribute a recovered failure. When the latest tool result is
// a success, "" is returned.
func latestToolError(transcript []Message) string {
	for index := len(transcript) - 1; index >= 0; index-- {
		message := transcript[index]
		if message.Role != "tool" {
			continue
		}
		if summary, ok := DecodeModelToolSummary(message.Content); ok {
			if summary.Success != nil && *summary.Success {
				return ""
			}
			for _, failure := range summary.Failures {
				if value := strings.TrimSpace(failure.Message); value != "" {
					return value
				}
			}
			for _, page := range summary.Pages {
				if page.Failure != nil {
					if value := strings.TrimSpace(page.Failure.Message); value != "" {
						return value
					}
				}
				for _, action := range page.Actions {
					if action.Failure != nil {
						if value := strings.TrimSpace(action.Failure.Message); value != "" {
							return value
						}
					}
				}
			}
			return ""
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
