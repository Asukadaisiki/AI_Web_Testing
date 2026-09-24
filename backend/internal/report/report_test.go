package report

import (
	"strings"
	"testing"
	"time"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/contract"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/store"
)

func failedStep(index int, kind contract.SignalKind, message string, unmet ...string) contract.StepResult {
	step := contract.StepResult{
		Index:  index,
		Action: contract.ActionClick,
		Status: "failed",
		Error:  &contract.StepError{Kind: kind, Message: message},
	}
	for _, value := range unmet {
		detail := "detail for " + value
		step.Conditions = append(step.Conditions, contract.ConditionResult{
			Phase: contract.PhasePost, Type: contract.CondURLContains, Value: value,
			Satisfied: false, Detail: &detail,
		})
	}
	return step
}

func TestSignalsFromStepErrorsAndUnmetConditions(t *testing.T) {
	started := time.Date(2026, 9, 21, 10, 0, 0, 0, time.UTC)
	result := contract.ExecutionResult{
		ExecutionID: "exec_1",
		Status:      contract.ExecutionFailed,
		StartedAt:   started,
		FinishedAt:  started.Add(4 * time.Second),
		FinalURL:    "https://shop.test/products",
		Steps: []contract.StepResult{
			{Index: 0, Action: contract.ActionGoto, Status: "passed"},
			failedStep(1, contract.SignalTargetNotFound, "matched 0 elements", "/view_cart"),
		},
	}
	signals := Signals("run_1", result)
	if len(signals) != 2 {
		t.Fatalf("signals = %#v", signals)
	}
	if signals[0].Kind != string(contract.SignalTargetNotFound) || signals[0].StepIndex != 1 {
		t.Fatalf("first signal = %#v", signals[0])
	}
	if signals[1].Kind != string(contract.SignalConditionUnmet) {
		t.Fatalf("second signal = %#v", signals[1])
	}
	if !strings.Contains(signals[1].Message, "/view_cart") {
		t.Fatalf("condition signal must name the unmet condition: %q", signals[1].Message)
	}
	for _, signal := range signals {
		if signal.RunID != "run_1" || signal.ExecutionID != "exec_1" {
			t.Fatalf("signal must be tied to the run and execution: %#v", signal)
		}
	}
}

func TestSignalsCoerceUnknownKinds(t *testing.T) {
	result := contract.ExecutionResult{
		ExecutionID: "exec_1",
		Status:      contract.ExecutionFailed,
		Steps: []contract.StepResult{
			failedStep(0, contract.SignalKind("something_new"), "boom"),
		},
	}
	signals := Signals("run_1", result)
	if len(signals) != 1 || signals[0].Kind != string(contract.SignalWorkerError) {
		t.Fatalf("an unknown kind must be coerced to worker_error: %#v", signals)
	}
}

func TestSignalsKeepBlockerKinds(t *testing.T) {
	result := contract.ExecutionResult{
		ExecutionID: "exec_1",
		Status:      contract.ExecutionFailed,
		Steps: []contract.StepResult{
			failedStep(2, contract.SignalBlockedByAuth, "sign in required"),
		},
	}
	signals := Signals("run_1", result)
	if len(signals) != 1 {
		t.Fatalf("signals = %#v", signals)
	}
	if signals[0].Kind != string(contract.SignalBlockedByAuth) || signals[0].StepIndex != 2 {
		t.Fatalf("blocker signal was not preserved: %#v", signals[0])
	}
}

func TestSignalsForExecutorLevelError(t *testing.T) {
	result := contract.ExecutionResult{
		ExecutionID: "exec_1",
		Status:      contract.ExecutionError,
		Error:       &contract.StepError{Kind: contract.SignalWorkerError, Message: "browser launch failed"},
	}
	signals := Signals("run_1", result)
	if len(signals) != 1 || signals[0].StepIndex != -1 {
		t.Fatalf("signals = %#v", signals)
	}
}

func TestSignalsForErrorStatusWithoutDetail(t *testing.T) {
	// 执行器只报 error 不说原因时，也必须给出一条信号，不能变成死路。
	signals := Signals("run_1", contract.ExecutionResult{
		ExecutionID: "exec_1", Status: contract.ExecutionError,
	})
	if len(signals) != 1 || signals[0].Kind != string(contract.SignalWorkerError) {
		t.Fatalf("signals = %#v", signals)
	}
}

func TestPassingRunHasNoSignals(t *testing.T) {
	signals := Signals("run_1", contract.ExecutionResult{
		ExecutionID: "exec_1", Status: contract.ExecutionPassed,
		Steps: []contract.StepResult{{Index: 0, Action: contract.ActionGoto, Status: "passed"}},
	})
	if len(signals) != 0 {
		t.Fatalf("signals = %#v", signals)
	}
}

func TestBuildCountsAndDuration(t *testing.T) {
	started := time.Date(2026, 9, 21, 10, 0, 0, 0, time.UTC)
	result := contract.ExecutionResult{
		ExecutionID: "exec_1",
		Status:      contract.ExecutionFailed,
		StartedAt:   started,
		FinishedAt:  started.Add(2500 * time.Millisecond),
		Steps: []contract.StepResult{
			{Index: 0, Action: contract.ActionGoto, Status: "passed"},
			{Index: 1, Action: contract.ActionClick, Status: "passed"},
			failedStep(2, contract.SignalTargetNotFound, "matched 0 elements"),
		},
	}
	value := Build("run_1", "completed", result, nil)
	if value.StepsTotal != 3 || value.StepsPassed != 2 || value.StepsFailed != 1 {
		t.Fatalf("report counts = %+v", value)
	}
	if value.DurationMS != 2500 {
		t.Fatalf("duration_ms = %d", value.DurationMS)
	}
	if value.Status != "completed" || value.RunID != "run_1" || value.ExecutionID != "exec_1" {
		t.Fatalf("report identity = %+v", value)
	}
	if len(value.Signals) != 1 || value.Signals[0].StepIndex == nil || *value.Signals[0].StepIndex != 2 {
		t.Fatalf("report signals = %+v", value.Signals)
	}
}

func TestBuildWithoutExecutionIsStillWellFormed(t *testing.T) {
	value := Build("run_1", "failed", contract.ExecutionResult{}, []store.Signal{})
	if value.StepsTotal != 0 || value.DurationMS != 0 {
		t.Fatalf("empty report = %+v", value)
	}
	if value.Signals == nil {
		t.Fatal("signals must serialise as [] and not null")
	}
}
