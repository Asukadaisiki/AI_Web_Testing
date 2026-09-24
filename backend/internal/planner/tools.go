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
	ToolOpenPage        = "open_page"
	ToolClick           = "click"
	ToolInput           = "input"
	ToolSelect          = "select"
	ToolCheck           = "check"
	ToolUncheck         = "uncheck"
	ToolScrollIntoView  = "scroll_into_view"
	ToolHover           = "hover"
	ToolDismissDialog   = "dismiss_dialog"
	ToolUploadFile      = "upload_file"
	ToolAssertText      = "assert_text"
	ToolAssertURL       = "assert_url"
	ToolAssertElement   = "assert_element"
	ToolAssertAttribute = "assert_attribute"
	ToolAssertCount     = "assert_count"
	ToolFinishCase      = "finish_case"
	ToolAskUser         = "ask_user"
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
		"expect_element": map[string]any{
			"type":        "string",
			"description": "target element state: visible, hidden, enabled, disabled, checked, or unchecked",
		},
		"expect_attribute": map[string]any{
			"type":        "string",
			"description": "target attribute assertion in attr=value form",
		},
		"expect_count": map[string]any{
			"type":        "string",
			"description": "expected count for the grounded target locator",
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
	candidateIDSchema := func(action contract.Action, assertion bool) map[string]any {
		description := fmt.Sprintf(
			"candidate id from the latest page view only when its advertised action is exactly %s, matching this tool; never use a candidate advertised for another action",
			action,
		)
		if assertion {
			description += "; never reuse a click or input candidate. If no compatible assertion candidate exists, use hint or target grounded in current elements"
		}
		return map[string]any{"type": "string", "description": description}
	}
	targetSpecSchema := map[string]any{
		"type":        "object",
		"description": "structured semantic target. It may include object/scope hints, but never CSS or XPath; the system grounds it to a verified locator.",
		"properties": map[string]any{
			"object": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"role":    map[string]any{"type": "string"},
					"text":    map[string]any{"type": "string"},
					"name":    map[string]any{"type": "string"},
					"aliases": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
				},
			},
			"scope": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"kind":          map[string]any{"type": "string", "description": "container kind such as card, form, dialog, table_row, list_item, frame"},
					"contains_text": map[string]any{"type": "string", "description": "text that must appear inside the scope"},
					"ref":           map[string]any{"type": "string", "description": "scope ref from the current page view, if known"},
				},
			},
			"relation": map[string]any{"type": "string", "description": "relation such as within, near, label_for, row_contains"},
		},
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
				"If the page view includes a suitable action candidate, pass candidate_id instead of describing a low-semantics target. You must declare at least one expectation, otherwise the step cannot be verified.",
			Parameters: withExpects(map[string]any{
				"hint":         map[string]any{"type": "string", "description": "visible text or accessible name of the element"},
				"candidate_id": candidateIDSchema(contract.ActionClick, false),
				"target":       targetSpecSchema,
				"intent":       map[string]any{"type": "string", "description": "why this click is needed"},
			}, []string{"intent"}),
		},
		{
			Name: ToolInput,
			Description: "Type a value into an element on the current page. Same matching and expectation rules as click. " +
				"Set submit=true to press Enter after typing — use it for search forms whose submit control has no usable name (for example an icon-only button).",
			Parameters: withExpects(map[string]any{
				"hint":         map[string]any{"type": "string", "description": "visible text or accessible name of the input"},
				"candidate_id": candidateIDSchema(contract.ActionInput, false),
				"target":       targetSpecSchema,
				"value":        map[string]any{"type": "string", "description": "value to type (empty string is allowed)"},
				"intent":       map[string]any{"type": "string", "description": "why this input is needed"},
				"submit": map[string]any{
					"type":        "boolean",
					"description": "press Enter after typing, to submit the form (input only)",
				},
			}, []string{"value", "intent"}),
		},
		{
			Name:        ToolSelect,
			Description: "Select an option in a native select/combobox. Defaults to verifying the selected value.",
			Parameters: withExpects(map[string]any{
				"hint":         map[string]any{"type": "string", "description": "visible text or accessible name of the select"},
				"candidate_id": candidateIDSchema(contract.ActionSelect, false),
				"target":       targetSpecSchema,
				"value":        map[string]any{"type": "string", "description": "option label or value"},
				"intent":       map[string]any{"type": "string", "description": "why this select is needed"},
			}, []string{"value", "intent"}),
		},
		{
			Name:        ToolCheck,
			Description: "Check a checkbox or radio target. Defaults to verifying checked state.",
			Parameters: withExpects(map[string]any{
				"hint":         map[string]any{"type": "string", "description": "visible text or accessible name of the checkbox/radio"},
				"candidate_id": candidateIDSchema(contract.ActionCheck, false),
				"target":       targetSpecSchema,
				"intent":       map[string]any{"type": "string", "description": "why this check is needed"},
			}, []string{"intent"}),
		},
		{
			Name:        ToolUncheck,
			Description: "Uncheck a checkbox target. Defaults to verifying unchecked state.",
			Parameters: withExpects(map[string]any{
				"hint":         map[string]any{"type": "string", "description": "visible text or accessible name of the checkbox"},
				"candidate_id": candidateIDSchema(contract.ActionUncheck, false),
				"target":       targetSpecSchema,
				"intent":       map[string]any{"type": "string", "description": "why this uncheck is needed"},
			}, []string{"intent"}),
		},
		{
			Name:        ToolScrollIntoView,
			Description: "Scroll a target into view before a later action. Declare an expectation such as expect_element=visible.",
			Parameters: withExpects(map[string]any{
				"hint":         map[string]any{"type": "string", "description": "visible text or accessible name of the element"},
				"candidate_id": candidateIDSchema(contract.ActionScrollIntoView, false),
				"target":       targetSpecSchema,
				"intent":       map[string]any{"type": "string", "description": "why this scroll is needed"},
			}, []string{"intent"}),
		},
		{
			Name:        ToolHover,
			Description: "Hover a target, usually to open a menu. Declare the expected menu text, url, attribute, or element state.",
			Parameters: withExpects(map[string]any{
				"hint":         map[string]any{"type": "string", "description": "visible text or accessible name of the element"},
				"candidate_id": candidateIDSchema(contract.ActionHover, false),
				"target":       targetSpecSchema,
				"intent":       map[string]any{"type": "string", "description": "why this hover is needed"},
			}, []string{"intent"}),
		},
		{
			Name:        ToolDismissDialog,
			Description: "Dismiss a visible dialog/modal by clicking a close control inside it. Declare expect_element=hidden when appropriate.",
			Parameters: withExpects(map[string]any{
				"hint":         map[string]any{"type": "string", "description": "visible text or accessible name of the dialog"},
				"candidate_id": candidateIDSchema(contract.ActionDismissDialog, false),
				"target":       targetSpecSchema,
				"intent":       map[string]any{"type": "string", "description": "why this dialog should be dismissed"},
			}, []string{"intent"}),
		},
		{
			Name:        ToolUploadFile,
			Description: "Upload a local file through a file input. The value is the local file path.",
			Parameters: withExpects(map[string]any{
				"hint":         map[string]any{"type": "string", "description": "visible text or accessible name of the file input"},
				"candidate_id": candidateIDSchema(contract.ActionUploadFile, false),
				"target":       targetSpecSchema,
				"value":        map[string]any{"type": "string", "description": "local file path"},
				"intent":       map[string]any{"type": "string", "description": "why this upload is needed"},
			}, []string{"value", "intent"}),
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
			Name:        ToolAssertElement,
			Description: "Assert a target element state: visible, hidden, enabled, disabled, checked, or unchecked.",
			Parameters: withExpects(map[string]any{
				"hint":           map[string]any{"type": "string", "description": "visible text or accessible name of the element"},
				"candidate_id":   candidateIDSchema(contract.ActionAssertElement, true),
				"target":         targetSpecSchema,
				"intent":         map[string]any{"type": "string", "description": "what this assertion proves"},
				"expect_element": map[string]any{"type": "string", "description": "state to assert"},
			}, []string{"intent", "expect_element"}),
		},
		{
			Name:        ToolAssertAttribute,
			Description: "Assert a target element attribute in attr=value form.",
			Parameters: withExpects(map[string]any{
				"hint":             map[string]any{"type": "string", "description": "visible text or accessible name of the element"},
				"candidate_id":     candidateIDSchema(contract.ActionAssertAttribute, true),
				"target":           targetSpecSchema,
				"intent":           map[string]any{"type": "string", "description": "what this assertion proves"},
				"expect_attribute": map[string]any{"type": "string", "description": "attr=value assertion"},
			}, []string{"intent", "expect_attribute"}),
		},
		{
			Name:        ToolAssertCount,
			Description: "Assert the grounded target locator resolves to an expected count.",
			Parameters: withExpects(map[string]any{
				"hint":         map[string]any{"type": "string", "description": "visible text or accessible name of the element set"},
				"candidate_id": candidateIDSchema(contract.ActionAssertCount, true),
				"target":       targetSpecSchema,
				"intent":       map[string]any{"type": "string", "description": "what this assertion proves"},
				"expect_count": map[string]any{"type": "string", "description": "expected integer count"},
			}, []string{"intent", "expect_count"}),
		},
		{
			Name: ToolFinishCase,
			Description: "Finish the case only after inspecting the committed steps and ensuring every explicit expected outcome from the user's goal has committed proof on the relevant final state. " +
				"Navigation or current visibility alone is not proof; use an assertion or an action postcondition that explicitly encodes the expected outcome. " +
				"Setting an input earlier does not prove its value persisted on a later or final page. Final requested values and counts must be asserted there. " +
				"The backend validates the case and runs it once in a fresh browser; only a case that passes that dry run is stored and sent to the user for approval. " +
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
	Hint            string               `json:"hint"`
	Target          *contract.TargetSpec `json:"target"`
	CandidateID     string               `json:"candidate_id"`
	Value           string               `json:"value"`
	Intent          string               `json:"intent"`
	Submit          bool                 `json:"submit"`
	ExpectText      *string              `json:"expect_text"`
	ExpectGone      *string              `json:"expect_gone"`
	ExpectURL       *string              `json:"expect_url"`
	ExpectValue     *string              `json:"expect_value"`
	ExpectElement   *string              `json:"expect_element"`
	ExpectAttribute *string              `json:"expect_attribute"`
	ExpectCount     *string              `json:"expect_count"`
}

func (a actionArgs) expects() contract.Expects {
	return contract.Expects{
		Text:      a.ExpectText,
		Gone:      a.ExpectGone,
		URL:       a.ExpectURL,
		Value:     a.ExpectValue,
		Element:   a.ExpectElement,
		Attribute: a.ExpectAttribute,
		Count:     a.ExpectCount,
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
		result, err := p.click(
			ctx, parsed.Hint, parsed.Target, parsed.CandidateID, parsed.Intent, parsed.expects(),
		)
		return CallOutcome{Result: result}, err
	case ToolInput:
		var parsed actionArgs
		if err := decodeArgs(args, &parsed); err != nil {
			return CallOutcome{}, err
		}
		result, err := p.input(
			ctx, parsed.Hint, parsed.Target, parsed.CandidateID, parsed.Value,
			parsed.Intent, parsed.expects(), parsed.Submit,
		)
		return CallOutcome{Result: result}, err
	case ToolSelect, ToolCheck, ToolUncheck, ToolScrollIntoView, ToolHover, ToolDismissDialog, ToolUploadFile,
		ToolAssertElement, ToolAssertAttribute, ToolAssertCount:
		var parsed actionArgs
		if err := decodeArgs(args, &parsed); err != nil {
			return CallOutcome{}, err
		}
		action, expects := actionForTool(name, parsed)
		result, err := p.targetAction(
			ctx, action, parsed.Hint, parsed.Target, parsed.CandidateID, parsed.Value,
			parsed.Intent, expects,
		)
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

func actionForTool(name string, args actionArgs) (contract.Action, contract.Expects) {
	expects := args.expects()
	switch name {
	case ToolSelect:
		if expects.Value == nil {
			expects.Value = &args.Value
		}
		return contract.ActionSelect, expects
	case ToolCheck:
		if expects.Element == nil {
			value := "checked"
			expects.Element = &value
		}
		return contract.ActionCheck, expects
	case ToolUncheck:
		if expects.Element == nil {
			value := "unchecked"
			expects.Element = &value
		}
		return contract.ActionUncheck, expects
	case ToolScrollIntoView:
		return contract.ActionScrollIntoView, expects
	case ToolHover:
		return contract.ActionHover, expects
	case ToolDismissDialog:
		return contract.ActionDismissDialog, expects
	case ToolUploadFile:
		return contract.ActionUploadFile, expects
	case ToolAssertElement:
		return contract.ActionAssertElement, expects
	case ToolAssertAttribute:
		return contract.ActionAssertAttribute, expects
	case ToolAssertCount:
		return contract.ActionAssertCount, expects
	default:
		return contract.ActionClick, expects
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
