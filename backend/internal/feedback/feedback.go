// Package feedback 把报告里的失败信号回灌成下一轮输入候选（半自动：人确认后才开下一轮）。
package feedback

import (
	"fmt"
	"strings"

	"github.com/Asukadaisiki/AI_Web_Testing/v2/backend/internal/contract"
	"github.com/Asukadaisiki/AI_Web_Testing/v2/backend/internal/store"
)

// MaxCandidates 是一轮最多给出的候选数。
const MaxCandidates = 3

// Candidates 由失败信号生成候选输入。
//
// 规则（CONTRACT §7，全部通用，不含任何站点/业务专有词）：
//   - 按信号 kind 去重，只取每种 kind 的第一条；
//   - 最多 MaxCandidates 条；
//   - 每条 = 原输入 + 结构化的失败摘要，人可在"错误注入"页上改。
func Candidates(
	originalInput string, steps []contract.Step, signals []store.Signal,
) []store.FeedbackCandidate {
	candidates := make([]store.FeedbackCandidate, 0, MaxCandidates)
	seen := map[string]bool{}
	for _, signal := range signals {
		if seen[signal.Kind] {
			continue
		}
		seen[signal.Kind] = true
		candidates = append(candidates, store.FeedbackCandidate{
			SignalKind:    signal.Kind,
			ProposedInput: compose(originalInput, steps, signal),
		})
		if len(candidates) >= MaxCandidates {
			break
		}
	}
	return candidates
}

// compose 拼出候选输入：保留原目标，追加一段结构化失败摘要。
func compose(originalInput string, steps []contract.Step, signal store.Signal) string {
	var builder strings.Builder
	builder.WriteString(strings.TrimSpace(originalInput))
	builder.WriteString("\n\n上一轮失败，请重新规划时避开：\n")
	builder.WriteString(fmt.Sprintf("- 失败类型：%s（%s）\n", signal.Kind, contract.SignalKind(signal.Kind).Message()))
	if step, ok := stepAt(steps, signal.StepIndex); ok {
		builder.WriteString(fmt.Sprintf("- 失败步骤：第 %d 步 %s", step.Index, step.Action))
		if step.Intent != "" {
			builder.WriteString(fmt.Sprintf("（%s）", step.Intent))
		}
		builder.WriteString("\n")
	}
	builder.WriteString(fmt.Sprintf("- 执行器反馈：%s\n", signal.Message))
	builder.WriteString("- 要求：换一条更稳的路径或更稳的目标，不要重复上一轮的定位方式。")
	return builder.String()
}

func stepAt(steps []contract.Step, index int) (contract.Step, bool) {
	for _, step := range steps {
		if step.Index == index {
			return step, true
		}
	}
	return contract.Step{}, false
}
