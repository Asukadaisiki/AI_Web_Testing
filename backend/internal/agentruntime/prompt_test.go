package agentruntime

import (
	"strings"
	"testing"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/contract"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/planner"
)

// 提示词与校验器不能漂移：模型看到的取值清单必须就是校验器接受的清单。
func TestSystemPromptListsEveryAction(t *testing.T) {
	prompt := SystemPrompt()
	for _, action := range contract.ActionNames() {
		if !strings.Contains(prompt, action) {
			t.Fatalf("prompt does not mention action %q", action)
		}
	}
}

func TestSystemPromptListsEveryConditionWithItsPhase(t *testing.T) {
	prompt := SystemPrompt()
	for _, name := range contract.ConditionTypes() {
		if !strings.Contains(prompt, string(name)) {
			t.Fatalf("prompt does not mention condition %q", name)
		}
		want := "post only"
		if contract.ConditionAllowed(name, contract.PhasePre) {
			want = "pre and post"
		}
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt does not state the %q phase rule", want)
		}
	}
}

// 真实 LLM 跑出来的第一个问题：目标里没有网址时，模型凭记忆猜了 saucedemo.com。
// 这条规则必须留在提示词里。
func TestSystemPromptForbidsGuessingUrls(t *testing.T) {
	prompt := SystemPrompt()
	if !strings.Contains(prompt, "NEVER invent or recall a url from memory") {
		t.Fatal("prompt must forbid inventing urls")
	}
	if !strings.Contains(prompt, "ask_user FIRST") {
		t.Fatal("prompt must tell the model to ask for the entry url first")
	}
}

func TestSystemPromptRequiresOneActionCompatibleToolCallPerTurn(t *testing.T) {
	prompt := SystemPrompt()
	for _, want := range []string{
		"exactly ONE tool call per model turn",
		"Never batch or parallelize tool calls",
		"rejected in full",
		"no call executes and no progress is made",
		"candidate_id may only be used with the exact action advertised by that candidate",
		"Never reuse a click or input candidate for an assertion or another tool",
		"If no compatible assertion candidate exists",
		"semantic target or hint grounded in the current elements",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt must state the tool-call contract with %q", want)
		}
	}
	if singleCall := strings.Index(prompt, "exactly ONE tool call per model turn"); singleCall < 0 ||
		singleCall > strings.Index(prompt, "The first tool call must be open_page") {
		t.Fatal("the one-tool-call contract must be the first numbered hard rule")
	}
}

func TestCandidateIDToolMetadataRequiresCurrentToolAction(t *testing.T) {
	actionTools := []string{
		planner.ToolClick,
		planner.ToolInput,
		planner.ToolSelect,
		planner.ToolCheck,
		planner.ToolUncheck,
		planner.ToolScrollIntoView,
		planner.ToolHover,
		planner.ToolDismissDialog,
		planner.ToolUploadFile,
		planner.ToolAssertElement,
		planner.ToolAssertAttribute,
		planner.ToolAssertCount,
	}
	toolsByName := make(map[string]planner.Tool)
	for _, tool := range planner.Tools() {
		toolsByName[tool.Name] = tool
	}
	for _, toolName := range actionTools {
		tool, ok := toolsByName[toolName]
		if !ok {
			t.Fatalf("tool definition %q is missing", toolName)
		}
		properties, ok := tool.Parameters["properties"].(map[string]any)
		if !ok {
			t.Fatalf("%s properties schema = %#v", toolName, tool.Parameters["properties"])
		}
		candidate, ok := properties["candidate_id"].(map[string]any)
		if !ok {
			t.Fatalf("%s candidate_id schema = %#v", toolName, properties["candidate_id"])
		}
		description, _ := candidate["description"].(string)
		for _, want := range []string{
			"latest page view",
			"advertised action is exactly " + toolName,
			"matching this tool",
			"never use a candidate advertised for another action",
		} {
			if !strings.Contains(description, want) {
				t.Errorf("%s candidate_id description must contain %q: %q", toolName, want, description)
			}
		}
		if strings.HasPrefix(toolName, "assert_") {
			for _, want := range []string{
				"never reuse a click or input candidate",
				"use hint or target grounded in current elements",
			} {
				if !strings.Contains(description, want) {
					t.Errorf("%s candidate_id description must contain %q: %q", toolName, want, description)
				}
			}
		}
	}
}

func TestSystemPromptProtectsCommittedSteps(t *testing.T) {
	prompt := SystemPrompt()
	if strings.Contains(prompt, "drop_last_step") {
		t.Fatal("prompt must not offer a tool that deletes committed steps")
	}
	if !strings.Contains(prompt, "committed") || !strings.Contains(prompt, "backend") {
		t.Fatal("prompt must explain that the backend repairs failed tails while committed steps remain protected")
	}
}

func TestSystemPromptRequiresCommittedFinalStateProofBeforeFinish(t *testing.T) {
	prompt := SystemPrompt()
	for _, want := range []string{
		"before calling finish_case",
		"every explicit expected outcome",
		"committed proof",
		"relevant final state",
		"Navigation or current visibility alone is not proof",
		"assertion or an action postcondition",
		"Setting an input earlier does not prove",
		"persisted on a later or final page",
		"Final requested values and counts must be asserted there",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt must require final-state proof with %q", want)
		}
	}
}

func TestFinishCaseToolRequiresCommittedFinalStateProofBeforeFinish(t *testing.T) {
	finishDescription := ""
	for _, tool := range planner.Tools() {
		if tool.Name == planner.ToolFinishCase {
			finishDescription = tool.Description
			break
		}
	}
	if finishDescription == "" {
		t.Fatal("finish_case tool definition is missing")
	}
	for _, want := range []string{
		"every explicit expected outcome",
		"committed proof",
		"relevant final state",
		"Navigation or current visibility alone is not proof",
		"assertion or an action postcondition",
		"Setting an input earlier does not prove",
		"persisted on a later or final page",
		"Final requested values and counts must be asserted there",
	} {
		if !strings.Contains(finishDescription, want) {
			t.Fatalf("finish_case tool must require final-state proof with %q", want)
		}
	}
}

func TestGoalMessageCarriesTheInputVerbatim(t *testing.T) {
	message := GoalMessage("  把商品加入购物车  ")
	if !strings.Contains(message, "把商品加入购物车") {
		t.Fatalf("goal message = %q", message)
	}
	if strings.HasSuffix(message, "\n") || strings.HasSuffix(message, " ") {
		t.Fatalf("goal message must be trimmed: %q", message)
	}
}
