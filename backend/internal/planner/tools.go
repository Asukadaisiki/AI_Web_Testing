package planner

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/contract"
)

// Tool 是暴露给模型的工具定义（OpenAI 兼容 function schema）。
type Tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

// ToolNames 是除 ask_user（由 agentruntime 处理）之外的全部工具名。
const (
	ToolOpenPage     = "open_page"
	ToolClick        = "click"
	ToolInput        = "input"
	ToolAssertText   = "assert_text"
	ToolAssertURL    = "assert_url"
	ToolDropLastStep = "drop_last_step"
	ToolFinishCase   = "finish_case"
	ToolAskUser      = "ask_user"
)

// Tools 返回工具定义。取值清单来自 contract，避免提示词与代码各写一份。
func Tools() []Tool {
	expectProperties := map[string]any{
		"expect_text": map[string]any{
			"type":        "string",
			"description": "text that must become visible after the action",
		},
		"expect_gone": map[string]any{
			"type":        "string",
			"description": "text that must no longer be visible after the action",
		},
		"expect_url": map[string]any{
			"type":        "string",
			"description": "substring the page url must contain after the action",
		},
		"expect_value": map[string]any{
			"type":        "string",
			"description": "value the target element must hold after the action",
		},
	}
	withExpects := func(base map[string]any, required []string) map[string]any {
		properties := map[string]any{}
		for key, value := range base {
			properties[key] = value
		}
		for key, value := range expectProperties {
			properties[key] = value
		}
		return map[string]any{
			"type":       "object",
			"properties": properties,
			"required":   required,
		}
	}
	return []Tool{
		{
			Name:        ToolOpenPage,
			Description: "Navigate the authoring browser to an absolute url and observe the page. Must be the first tool you call. Every later target is resolved against the latest observation.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"url":    map[string]any{"type": "string", "description": "absolute url"},
					"intent": map[string]any{"type": "string", "description": "why this page is opened, in the user's language"},
				},
				"required": []string{"url", "intent"},
			},
		},
		{
			Name: ToolClick,
			Description: "Click an element on the current page. The hint is matched against the latest observation; it must resolve to exactly one visible, enabled element. " +
				"You must declare at least one expectation, otherwise the step cannot be verified.",
			Parameters: withExpects(map[string]any{
				"hint":   map[string]any{"type": "string", "description": "visible text or accessible name of the element"},
				"intent": map[string]any{"type": "string", "description": "why this click is needed"},
			}, []string{"hint", "intent"}),
		},
		{
			Name:        ToolInput,
			Description: "Type a value into an element on the current page. Same matching and expectation rules as click.",
			Parameters: withExpects(map[string]any{
				"hint":   map[string]any{"type": "string", "description": "visible text or accessible name of the input"},
				"value":  map[string]any{"type": "string", "description": "value to type (empty string is allowed)"},
				"intent": map[string]any{"type": "string", "description": "why this input is needed"},
			}, []string{"hint", "value", "intent"}),
		},
		{
			Name:        ToolAssertText,
			Description: "Assert that text is visible on the current page. Checked on the page as it is now, so it must already hold (or hold after the previous step).",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"text":   map[string]any{"type": "string", "description": "text that must be visible"},
					"intent": map[string]any{"type": "string", "description": "what this assertion proves"},
				},
				"required": []string{"text", "intent"},
			},
		},
		{
			Name:        ToolAssertURL,
			Description: "Assert that the current page url contains a substring.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"contains": map[string]any{"type": "string", "description": "substring the url must contain"},
					"intent":   map[string]any{"type": "string", "description": "what this assertion proves"},
				},
				"required": []string{"contains", "intent"},
			},
		},
		{
			Name:        ToolDropLastStep,
			Description: "Remove the most recently recorded step. Use it when a step was wrong, or to rebuild the tail of the case after a failed dry run.",
			Parameters: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
		{
			Name: ToolFinishCase,
			Description: "Finish the case. The backend validates it and runs it once in a fresh browser; only a case that passes that dry run is stored and sent to the user for approval. " +
				"If the dry run fails you get the failing steps back and must fix them.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name": map[string]any{"type": "string", "description": "short case name"},
				},
				"required": []string{"name"},
			},
		},
		{
			Name:        ToolAskUser,
			Description: "Ask the user a question when the goal is ambiguous or a required value is missing. The run pauses until the user answers.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"question": map[string]any{"type": "string", "description": "question to the user"},
				},
				"required": []string{"question"},
			},
		},
	}
}

// CallOutcome 是一次工具调用的结果。
type CallOutcome struct {
	Result Result
	Case   *contract.Case
	DryRun *contract.ExecutionResult
}

type openPageArgs struct {
	URL    string `json:"url"`
	Intent string `json:"intent"`
}

type actionArgs struct {
	Hint        string  `json:"hint"`
	Value       string  `json:"value"`
	Intent      string  `json:"intent"`
	ExpectText  *string `json:"expect_text"`
	ExpectGone  *string `json:"expect_gone"`
	ExpectURL   *string `json:"expect_url"`
	ExpectValue *string `json:"expect_value"`
}

func (a actionArgs) expects() contract.Expects {
	return contract.Expects{
		Text:  a.ExpectText,
		Gone:  a.ExpectGone,
		URL:   a.ExpectURL,
		Value: a.ExpectValue,
	}
}

type assertTextArgs struct {
	Text   string `json:"text"`
	Intent string `json:"intent"`
}

type assertURLArgs struct {
	Contains string `json:"contains"`
	Intent   string `json:"intent"`
}

type finishArgs struct {
	Name string `json:"name"`
}

// Call 分派一次工具调用。返回的 error 只表示参数不是合法 JSON 之类的内部问题。
func (p *Planner) Call(ctx context.Context, name string, args json.RawMessage) (CallOutcome, error) {
	switch name {
	case ToolOpenPage:
		var parsed openPageArgs
		if err := decodeArgs(args, &parsed); err != nil {
			return CallOutcome{}, err
		}
		result, err := p.openPage(ctx, parsed.URL, parsed.Intent)
		return CallOutcome{Result: result}, err
	case ToolClick:
		var parsed actionArgs
		if err := decodeArgs(args, &parsed); err != nil {
			return CallOutcome{}, err
		}
		result, err := p.click(ctx, parsed.Hint, parsed.Intent, parsed.expects())
		return CallOutcome{Result: result}, err
	case ToolInput:
		var parsed actionArgs
		if err := decodeArgs(args, &parsed); err != nil {
			return CallOutcome{}, err
		}
		result, err := p.input(ctx, parsed.Hint, parsed.Value, parsed.Intent, parsed.expects())
		return CallOutcome{Result: result}, err
	case ToolAssertText:
		var parsed assertTextArgs
		if err := decodeArgs(args, &parsed); err != nil {
			return CallOutcome{}, err
		}
		result, err := p.assertText(parsed.Text, parsed.Intent)
		return CallOutcome{Result: result}, err
	case ToolAssertURL:
		var parsed assertURLArgs
		if err := decodeArgs(args, &parsed); err != nil {
			return CallOutcome{}, err
		}
		result, err := p.assertURL(parsed.Contains, parsed.Intent)
		return CallOutcome{Result: result}, err
	case ToolDropLastStep:
		result, err := p.dropLastStep()
		return CallOutcome{Result: result}, err
	case ToolFinishCase:
		var parsed finishArgs
		if err := decodeArgs(args, &parsed); err != nil {
			return CallOutcome{}, err
		}
		artifact, dryRun, result, err := p.Finish(ctx, parsed.Name)
		if err != nil {
			return CallOutcome{}, err
		}
		outcome := CallOutcome{Result: result, DryRun: &dryRun}
		if result.OK {
			outcome.Case = &artifact
		}
		return outcome, nil
	default:
		return CallOutcome{Result: failure("unknown_tool", fmt.Sprintf("unknown tool %q", name))}, nil
	}
}

func decodeArgs(raw json.RawMessage, target any) error {
	if len(raw) == 0 {
		raw = json.RawMessage("{}")
	}
	if err := json.Unmarshal(raw, target); err != nil {
		return fmt.Errorf("invalid tool arguments: %w", err)
	}
	return nil
}
