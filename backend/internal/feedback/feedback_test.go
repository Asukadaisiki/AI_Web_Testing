package feedback

import (
	"strings"
	"testing"

	"github.com/Asukadaisiki/AI_Web_Testing/v2/backend/internal/contract"
	"github.com/Asukadaisiki/AI_Web_Testing/v2/backend/internal/store"
)

func steps() []contract.Step {
	return []contract.Step{
		{Index: 0, Action: contract.ActionGoto, Intent: "打开列表"},
		{Index: 1, Action: contract.ActionClick, Intent: "打开第一个商品"},
		{Index: 2, Action: contract.ActionAssertText, Intent: "确认已加入购物车"},
	}
}

func TestCandidatesKeepOriginalInputAndMentionTheFailure(t *testing.T) {
	signals := []store.Signal{
		{StepIndex: 1, Kind: string(contract.SignalTargetNotFound), Message: "locator matched 0 elements"},
	}
	candidates := Candidates("把商品加入购物车", steps(), signals)
	if len(candidates) != 1 {
		t.Fatalf("candidates = %#v", candidates)
	}
	candidate := candidates[0]
	if candidate.SignalKind != string(contract.SignalTargetNotFound) {
		t.Fatalf("signal_kind = %q", candidate.SignalKind)
	}
	if !strings.Contains(candidate.ProposedInput, "把商品加入购物车") {
		t.Fatalf("the original goal must survive: %q", candidate.ProposedInput)
	}
	if !strings.Contains(candidate.ProposedInput, string(contract.SignalTargetNotFound)) {
		t.Fatalf("the failure kind must be visible: %q", candidate.ProposedInput)
	}
	// 失败步骤的 intent 必须带上，人才能看懂上一轮死在哪。
	if !strings.Contains(candidate.ProposedInput, "打开第一个商品") {
		t.Fatalf("the failing step intent must be included: %q", candidate.ProposedInput)
	}
	if !strings.Contains(candidate.ProposedInput, "locator matched 0 elements") {
		t.Fatalf("the executor message must be included: %q", candidate.ProposedInput)
	}
}

func TestCandidatesDedupeByKindAndCapAtThree(t *testing.T) {
	signals := []store.Signal{
		{StepIndex: 1, Kind: string(contract.SignalTargetNotFound), Message: "a"},
		{StepIndex: 2, Kind: string(contract.SignalTargetNotFound), Message: "b"},
		{StepIndex: 3, Kind: string(contract.SignalConditionUnmet), Message: "c"},
		{StepIndex: 4, Kind: string(contract.SignalStepTimeout), Message: "d"},
		{StepIndex: 5, Kind: string(contract.SignalWorkerError), Message: "e"},
	}
	candidates := Candidates("目标", steps(), signals)
	if len(candidates) != MaxCandidates {
		t.Fatalf("candidates = %d, want %d", len(candidates), MaxCandidates)
	}
	seen := map[string]bool{}
	for _, candidate := range candidates {
		if seen[candidate.SignalKind] {
			t.Fatalf("kinds must be deduped: %#v", candidates)
		}
		seen[candidate.SignalKind] = true
	}
	if !seen[string(contract.SignalTargetNotFound)] || !seen[string(contract.SignalConditionUnmet)] {
		t.Fatalf("the first distinct kinds must win: %#v", candidates)
	}
}

func TestCandidatesWithoutSignalsIsEmpty(t *testing.T) {
	if candidates := Candidates("目标", steps(), nil); len(candidates) != 0 {
		t.Fatalf("candidates = %#v", candidates)
	}
}

func TestCandidatesTolerateMissingStep(t *testing.T) {
	// 执行器层的失败没有对应步骤，不能因此崩掉或丢掉候选。
	signals := []store.Signal{
		{StepIndex: -1, Kind: string(contract.SignalWorkerError), Message: "browser launch failed"},
	}
	candidates := Candidates("目标", steps(), signals)
	if len(candidates) != 1 {
		t.Fatalf("candidates = %#v", candidates)
	}
	if !strings.Contains(candidates[0].ProposedInput, "browser launch failed") {
		t.Fatalf("message missing: %q", candidates[0].ProposedInput)
	}
}
