package domain

// RunStatus describes the durable lifecycle state of a run.
type RunStatus string

const (
	RunQueued          RunStatus = "queued"
	RunRunning         RunStatus = "running"
	RunWaitingInput    RunStatus = "waiting_input"
	RunWaitingApproval RunStatus = "waiting_approval"
	RunCancelling      RunStatus = "cancelling"
	RunSucceeded       RunStatus = "succeeded"
	RunFailed          RunStatus = "failed"
	RunCancelled       RunStatus = "cancelled"
)

// SpanKind identifies a trace-node category.
type SpanKind string

const (
	SpanRun       SpanKind = "run"
	SpanModel     SpanKind = "model"
	SpanReasoning SpanKind = "reasoning"
	SpanTool      SpanKind = "tool"
	SpanOperation SpanKind = "operation"
	SpanReport    SpanKind = "report"
)

// CanTransition reports whether a durable run lifecycle transition is valid.
func CanTransition(from, to RunStatus) bool {
	switch from {
	case RunQueued:
		return to == RunRunning || to == RunCancelling || to == RunFailed || to == RunCancelled
	case RunRunning:
		return to == RunWaitingInput ||
			to == RunWaitingApproval ||
			to == RunCancelling ||
			to == RunSucceeded ||
			to == RunFailed ||
			to == RunCancelled
	case RunWaitingInput, RunWaitingApproval:
		return to == RunRunning || to == RunCancelling || to == RunFailed || to == RunCancelled
	case RunCancelling:
		return to == RunCancelled || to == RunFailed
	default:
		return false
	}
}
