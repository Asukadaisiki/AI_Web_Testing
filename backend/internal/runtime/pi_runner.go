package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/domain"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/event"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/piharness"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/postgres"
)

const cancellationPollInterval = 25 * time.Millisecond

// HarnessProcessRunner starts a pi-browser-harness subprocess and streams
// validated events to a callback. *piharness.ProcessRunner satisfies it.
type HarnessProcessRunner interface {
	Run(ctx context.Context, request piharness.Request, onEvent func(piharness.Event)) error
}

// PiRunner drives one Pi browser-harness run: it claims the run, spawns the
// harness, persists each event as EvalHub trace evidence, and derives the
// objective terminal status from the harness verdict.
type PiRunner struct {
	Process                HarnessProcessRunner
	Store                  PiTraceStore
	WorkerURL              string
	DefaultReasoningEffort string
}

// Run executes the harness for command and persists its trace. A replay of an
// already terminal run spawns no child process and returns nil.
func (r PiRunner) Run(ctx context.Context, command event.RunCommand) error {
	if r.Process == nil {
		return errors.New("pi runner process is required")
	}
	if r.Store == nil {
		return errors.New("pi runner store is required")
	}
	if strings.TrimSpace(command.RunID) == "" {
		return errors.New("pi runner command run id is required")
	}

	ref, err := r.Store.StartRun(ctx, command)
	if errors.Is(err, postgres.ErrRunAlreadyTerminal) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("start pi run: %w", err)
	}

	request := buildHarnessRequest(
		command, r.WorkerURL, ref.ProviderAttemptID, r.DefaultReasoningEffort,
	)

	runState := &piRunState{}
	processCtx, cancelProcess := context.WithCancel(ctx)
	watchDone := make(chan struct{})
	watchExited := make(chan struct{})
	go r.watchCancellation(ctx, ref.RunID, cancelProcess, watchDone, watchExited)

	processErr := r.Process.Run(processCtx, request, func(ev piharness.Event) {
		runState.observe(ev)
		if aerr := r.Store.AppendHarnessEvent(ctx, ref, ev); aerr != nil {
			runState.appendErr = aerr
		}
	})
	close(watchDone)
	<-watchExited
	cancelProcess()

	if runState.appendErr != nil && processErr == nil {
		processErr = runState.appendErr
	}

	cancelRequested, cancelErr := r.Store.CancellationRequested(ctx, ref.RunID)
	if cancelErr != nil && processErr == nil {
		processErr = fmt.Errorf("read cancellation state before completion: %w", cancelErr)
	}
	outcome := r.deriveOutcome(processErr, runState, ref.CancelRequested || cancelRequested)
	status, completeErr := r.Store.CompleteHarnessRun(ctx, ref, outcome)
	if completeErr != nil {
		return fmt.Errorf("complete pi run: %w", completeErr)
	}
	if status != "" {
		outcome.Status = status
	}
	if outcome.Status == domain.RunSucceeded {
		return nil
	}
	if outcome.Status == domain.RunCancelled {
		outcome.Error = "run cancelled"
	}
	if outcome.Error == "" {
		outcome.Error = "pi run terminated unsuccessfully"
	}
	return &terminalRunError{cause: errors.New(outcome.Error)}
}

func (r PiRunner) watchCancellation(
	ctx context.Context,
	runID string,
	cancelProcess context.CancelFunc,
	done <-chan struct{},
	exited chan<- struct{},
) {
	defer close(exited)

	check := func() bool {
		cancelRequested, err := r.Store.CancellationRequested(ctx, runID)
		if err != nil || !cancelRequested {
			return false
		}
		cancelProcess()
		return true
	}
	if check() {
		return
	}

	ticker := time.NewTicker(cancellationPollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			return
		case <-ctx.Done():
			cancelProcess()
			return
		case <-ticker.C:
			if check() {
				return
			}
		}
	}
}

type piRunState struct {
	latestCaseVerdict string
	assistantText     string
	failureError      string
	appendErr         error
}

func (s *piRunState) observe(ev piharness.Event) {
	switch ev.Type {
	case piharness.EventCaseCompleted:
		var payload piharness.CaseCompletedPayload
		_ = json.Unmarshal(ev.Payload, &payload)
		s.latestCaseVerdict = payload.Verdict
	case piharness.EventSettled:
		var payload piharness.SettledPayload
		_ = json.Unmarshal(ev.Payload, &payload)
		s.assistantText = payload.AssistantText
	case piharness.EventFailed:
		var payload piharness.FailedPayload
		_ = json.Unmarshal(ev.Payload, &payload)
		s.failureError = payload.Error
	}
}

func (r PiRunner) deriveOutcome(processErr error, state *piRunState, cancelRequested bool) PiRunOutcome {
	// A durable cancel request overrides every other verdict: the run was
	// explicitly cancelled before or during execution, so its terminal state
	// must be RunCancelled regardless of whether the harness happened to pass.
	if cancelRequested {
		return PiRunOutcome{Status: domain.RunCancelled, Error: "run cancelled by durable cancel request", Assistant: state.assistantText}
	}
	if processErr == nil {
		if state.latestCaseVerdict == "passed" {
			return PiRunOutcome{Status: domain.RunSucceeded, Assistant: state.assistantText}
		}
		return PiRunOutcome{
			Status:    domain.RunFailed,
			Error:     "harness settled without a passing latest browser case",
			Assistant: state.assistantText,
		}
	}
	if errors.Is(processErr, context.Canceled) || errors.Is(processErr, context.DeadlineExceeded) {
		return PiRunOutcome{Status: domain.RunCancelled, Error: processErr.Error()}
	}
	var failure *piharness.HarnessFailure
	if errors.As(processErr, &failure) {
		msg := state.failureError
		if msg == "" {
			msg = failure.Message
		}
		return PiRunOutcome{Status: domain.RunFailed, Error: msg}
	}
	if errors.Is(processErr, piharness.ErrTimeout) {
		return PiRunOutcome{Status: domain.RunCancelled, Error: processErr.Error()}
	}
	return PiRunOutcome{Status: domain.RunFailed, Error: processErr.Error()}
}

func buildHarnessRequest(
	command event.RunCommand,
	workerURL, providerAttemptID, defaultReasoningEffort string,
) piharness.Request {
	goal := ""
	for i := len(command.Messages) - 1; i >= 0; i-- {
		if command.Messages[i].Role == "user" {
			goal = command.Messages[i].Content
			break
		}
	}
	if workerURL == "" {
		workerURL = command.WorkerURL
	}
	artifactScope := command.ArtifactScope
	if strings.TrimSpace(artifactScope) == "" {
		artifactScope = command.RunID
	}
	reasoningEffort := command.Model.ReasoningEffort
	if strings.TrimSpace(reasoningEffort) == "" {
		reasoningEffort = defaultReasoningEffort
	}
	return piharness.Request{
		RunID:     command.RunID,
		AttemptID: providerAttemptID,
		Goal:      goal,
		Model: piharness.ModelRequest{
			Provider:        command.Model.Provider,
			Name:            command.Model.Name,
			ReasoningEffort: reasoningEffort,
		},
		WorkerURL:     workerURL,
		ArtifactScope: artifactScope,
	}
}
