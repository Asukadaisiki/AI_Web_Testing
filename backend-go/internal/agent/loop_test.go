package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

type sequenceModel struct {
	responses []ModelResponse
}

func (m *sequenceModel) Complete(
	context.Context,
	[]Message,
	[]ToolDefinition,
) (ModelResponse, error) {
	response := m.responses[0]
	m.responses = m.responses[1:]
	return response, nil
}

func TestLoopContinuesUntilHandlerStops(t *testing.T) {
	model := &sequenceModel{responses: []ModelResponse{
		{ToolCalls: []ModelTool{{ID: "call-1", Name: "read", Arguments: `{}`}}},
		{Content: "done"},
	}}
	loop := NewLoop(model, nil, "system", 3)
	transcript := []Message{{Role: "user", Content: "start"}}
	turns := 0

	err := loop.Run(context.Background(), &transcript, func(_ context.Context, response ModelResponse) (bool, error) {
		turns++
		if len(response.ToolCalls) > 0 {
			transcript = append(transcript, Message{
				Role:       "tool",
				ToolCallID: response.ToolCalls[0].ID,
				Content:    `{"ok":true}`,
			})
			return true, nil
		}
		return false, nil
	})

	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if turns != 2 {
		t.Fatalf("turns = %d, want 2", turns)
	}
	if len(transcript) != 4 {
		t.Fatalf("transcript length = %d, want 4", len(transcript))
	}
}

func TestLoopEnforcesTurnLimit(t *testing.T) {
	model := &sequenceModel{responses: []ModelResponse{
		{ToolCalls: []ModelTool{{ID: "call-1", Name: "read", Arguments: `{}`}}},
	}}
	loop := NewLoop(model, nil, "system", 1)
	transcript := []Message{{Role: "user", Content: "start"}}

	err := loop.Run(context.Background(), &transcript, func(context.Context, ModelResponse) (bool, error) {
		return true, nil
	})
	if err == nil {
		t.Fatal("Run() error = nil, want turn limit error")
	}
}

func TestLoopTurnLimitPreservesLatestToolError(t *testing.T) {
	model := &sequenceModel{responses: []ModelResponse{
		{ToolCalls: []ModelTool{{ID: "generate-1", Name: "generate_dsl", Arguments: `{}`}}},
	}}
	loop := NewLoop(model, nil, "system", 1)
	transcript := []Message{{Role: "user", Content: "start"}}

	err := loop.Run(context.Background(), &transcript, func(context.Context, ModelResponse) (bool, error) {
		transcript = append(transcript, Message{
			Role:       "tool",
			ToolCallID: "generate-1",
			Content:    `{"status":"error","message":"Step 13: composite CSS was not verified"}`,
		})
		return true, nil
	})
	if err == nil ||
		!strings.Contains(err.Error(), "last tool error: Step 13: composite CSS was not verified") {
		t.Fatalf("Run() error = %v, want preserved tool diagnostic", err)
	}
}

func TestLoopTurnLimitPreservesExplorationSummaryError(t *testing.T) {
	model := &sequenceModel{responses: []ModelResponse{
		{ToolCalls: []ModelTool{{ID: "explore-1", Name: "explore_flow", Arguments: `{}`}}},
	}}
	loop := NewLoop(model, nil, "system", 1)
	transcript := []Message{{Role: "user", Content: "start"}}

	err := loop.Run(context.Background(), &transcript, func(context.Context, ModelResponse) (bool, error) {
		content, summaryErr := BuildModelToolSummary(
			"explore_flow",
			json.RawMessage(`{
				"success":false,
				"failures":[{
					"code":"flow_action_failed",
					"message":"View Cart remained hidden",
					"action":"click",
					"target":"View Cart"
				}],
				"pages":[]
			}`),
			12,
		)
		if summaryErr != nil {
			return false, summaryErr
		}
		transcript = append(transcript, Message{
			Role: "tool", ToolCallID: "explore-1", Content: content,
		})
		return true, nil
	})
	if err == nil ||
		!strings.Contains(err.Error(), "last tool error: View Cart remained hidden") {
		t.Fatalf("Run() error = %v, want exploration diagnostic", err)
	}
}

func TestLoopReturnsModelError(t *testing.T) {
	expected := errors.New("model unavailable")
	model := ModelFunc(func(context.Context, []Message, []ToolDefinition) (ModelResponse, error) {
		return ModelResponse{}, expected
	})
	loop := NewLoop(model, nil, "system", 1)
	transcript := []Message{{Role: "user", Content: "start"}}

	err := loop.Run(context.Background(), &transcript, func(context.Context, ModelResponse) (bool, error) {
		return false, nil
	})
	if !errors.Is(err, expected) {
		t.Fatalf("Run() error = %v, want %v", err, expected)
	}
}

type ModelFunc func(context.Context, []Message, []ToolDefinition) (ModelResponse, error)

func (f ModelFunc) Complete(
	ctx context.Context,
	messages []Message,
	tools []ToolDefinition,
) (ModelResponse, error) {
	return f(ctx, messages, tools)
}
