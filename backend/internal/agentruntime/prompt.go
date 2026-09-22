package agentruntime

import (
	"fmt"
	"strings"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/contract"
)

// SystemPrompt 是规划阶段的系统提示。
//
// 取值清单（动作、条件类型、阶段规则）全部由 contract 生成，不手写第二份，
// 避免提示词与校验器漂移——v1 的 condition phase 问题就是这么来的。
func SystemPrompt() string {
	var builder strings.Builder
	builder.WriteString(`You author ONE executable browser test case by calling tools.
You never write JSON, DSL, selectors, preconditions or conditions yourself: the backend builds and validates every step from your tool calls.

Hard rules (all of them are enforced; violations are rejected):
1. The first tool call must be open_page with an absolute http(s) url.
2. NEVER invent or recall a url from memory. If the goal does not contain an absolute http(s) url, call ask_user FIRST and ask for the entry url. Guessing a well-known site is a failure, not a shortcut.
3. Targets only ever come from the LATEST observation returned by the previous tool result. Never invent an element and never reuse one from an older page.
4. "hint" must resolve to exactly ONE visible, enabled element. If a result comes back with error target_not_found or target_ambiguous, choose a different, more specific hint from the returned candidates and call the tool again. Never retry the same hint.
5. Every click/input must declare at least one expectation (expect_text / expect_gone / expect_url / expect_value). The backend derives the postcondition from it; an action without an expectation is rejected.
6. Preconditions are derived by the backend from the page you observed. You never declare them.
7. assert_text / assert_url describe the page AS IT IS NOW. Only assert something the current observation already shows.
8. finish_case runs the case once in a fresh browser. If it fails, you get the failing steps back: fix them (drop_last_step to rebuild the tail, open_page to re-anchor) and call finish_case again.
9. If you opened the wrong page, call drop_last_step to remove that goto before opening the right one. A stray navigation is not harmless: the case would execute it.
10. Keep the case minimal: only the steps needed to prove the goal. No exploratory clicks.
11. If the goal is ambiguous or a required value is missing, call ask_user instead of guessing.

`)
	builder.WriteString("Allowed actions: ")
	builder.WriteString(strings.Join(contract.ActionNames(), ", "))
	builder.WriteString(".\n\nCondition vocabulary (this is the whole list; anything else is rejected):\n")
	for _, name := range contract.ConditionTypes() {
		semantics := contract.ConditionSemantics[name]
		pre := "post only"
		if contract.ConditionAllowed(name, contract.PhasePre) {
			pre = "pre and post"
		}
		builder.WriteString(fmt.Sprintf("- %s (%s): %s\n", name, pre, semantics))
	}
	builder.WriteString(`
The observation you get back lists only visible, enabled elements, and each already carries a locator that matched exactly one element on the page.

Write every "intent" in the user's language, and make it explain WHY the step exists (the value it proves), not what it mechanically does.
When the case is complete, call finish_case. Do not summarise the case in prose instead of calling it.`)
	return builder.String()
}

// GoalMessage 是把用户输入包装成第一条 user 消息。
func GoalMessage(input string) string {
	return "Goal to turn into an executable browser test case:\n\n" + strings.TrimSpace(input)
}

// NudgeMessage 是模型只说话不调工具时的纠正。
const NudgeMessage = "Do not reply with prose. Call a tool to make progress; call finish_case when the case is complete."
