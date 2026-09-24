package agentruntime

import (
	"strings"
	"testing"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/contract"
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

func TestSystemPromptProtectsCommittedSteps(t *testing.T) {
	prompt := SystemPrompt()
	if strings.Contains(prompt, "drop_last_step") {
		t.Fatal("prompt must not offer a tool that deletes committed steps")
	}
	if !strings.Contains(prompt, "committed") || !strings.Contains(prompt, "backend") {
		t.Fatal("prompt must explain that the backend repairs failed tails while committed steps remain protected")
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
