// Package report 把一次执行结果聚合成报告与失败信号。
//
// 信号是"报告 → 失败回灌"的唯一接口：回灌只看 signals，不看原始执行结果。
package report

import (
	"fmt"
	"strings"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/contract"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/store"
)

// Report 是"报告"页的数据形态，字段名与 web/src/api.ts 的 RunReport 一一对应。
type Report struct {
	RunID       string       `json:"run_id"`
	Status      string       `json:"status"`
	ExecutionID string       `json:"execution_id"`
	StepsTotal  int          `json:"steps_total"`
	StepsPassed int          `json:"steps_passed"`
	StepsFailed int          `json:"steps_failed"`
	DurationMS  int64        `json:"duration_ms"`
	Signals     []SignalView `json:"signals"`
}

// SignalView 是报告里的一条失败信号。
type SignalView struct {
	StepIndex *int   `json:"step_index"`
	Kind      string `json:"kind"`
	Message   string `json:"message"`
}

// knownKinds 是合法的信号种类。执行器报上来的 kind 不在其中时收敛为 worker_error，
// 避免脏数据一路流进回灌候选。
var knownKinds = map[contract.SignalKind]bool{
	contract.SignalTargetNotFound: true,
	contract.SignalConditionUnmet: true,
	contract.SignalStepTimeout:    true,
	contract.SignalWorkerError:    true,
	contract.SignalCaseInvalid:    true,
}

// Signals 从执行结果派生失败信号（CONTRACT §4.1）。
//
// 它是纯函数：同样的执行结果永远得到同样的信号，便于离线断言。
func Signals(runID string, result contract.ExecutionResult) []store.Signal {
	signals := make([]store.Signal, 0, 4)
	add := func(stepIndex int, kind contract.SignalKind, message string) {
		if !knownKinds[kind] {
			kind = contract.SignalWorkerError
		}
		signals = append(signals, store.Signal{
			RunID:       runID,
			ExecutionID: result.ExecutionID,
			StepIndex:   stepIndex,
			Kind:        string(kind),
			Message:     message,
		})
	}

	if result.Error != nil {
		add(-1, result.Error.Kind, result.Error.Message)
	}
	for _, step := range result.Steps {
		if step.Error != nil {
			add(step.Index, step.Error.Kind, fmt.Sprintf(
				"第 %d 步（%s）：%s", step.Index, step.Action, step.Error.Message,
			))
		}
		unsatisfied := make([]string, 0, len(step.Conditions))
		for _, condition := range step.Conditions {
			if condition.Satisfied {
				continue
			}
			detail := ""
			if condition.Detail != nil {
				detail = "，" + *condition.Detail
			}
			unsatisfied = append(unsatisfied, fmt.Sprintf(
				"%s（%s）= %q%s", condition.Type, condition.Phase, condition.Value, detail,
			))
		}
		if len(unsatisfied) > 0 {
			add(step.Index, contract.SignalConditionUnmet, fmt.Sprintf(
				"第 %d 步（%s）条件未满足：%s", step.Index, step.Action, strings.Join(unsatisfied, "；"),
			))
		}
	}
	if result.Status == contract.ExecutionError && len(signals) == 0 {
		add(-1, contract.SignalWorkerError, "执行器返回 error 但没有给出原因")
	}
	return signals
}

// Build 汇总一次执行。signals 为 nil 时会从 result 现算，保证报告与信号永远一致。
func Build(runID string, status string, result contract.ExecutionResult, signals []store.Signal) Report {
	if signals == nil {
		signals = Signals(runID, result)
	}
	report := Report{
		RunID:       runID,
		Status:      status,
		ExecutionID: result.ExecutionID,
		StepsTotal:  len(result.Steps),
		Signals:     make([]SignalView, 0, len(signals)),
	}
	for _, step := range result.Steps {
		if step.Status == "passed" {
			report.StepsPassed++
			continue
		}
		report.StepsFailed++
	}
	if !result.StartedAt.IsZero() && !result.FinishedAt.IsZero() {
		report.DurationMS = result.FinishedAt.Sub(result.StartedAt).Milliseconds()
		if report.DurationMS < 0 {
			report.DurationMS = 0
		}
	}
	for _, signal := range signals {
		view := SignalView{Kind: signal.Kind, Message: signal.Message}
		if signal.StepIndex >= 0 {
			index := signal.StepIndex
			view.StepIndex = &index
		}
		report.Signals = append(report.Signals, view)
	}
	return report
}
