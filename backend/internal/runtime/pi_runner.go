package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/domain"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/event"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/piharness"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/postgres"
)

// HarnessProcessRunner starts a pi-browser-harness subprocess and streams
// validated events to a callback. *piharness.ProcessRunner satisfies it.
type HarnessProcessRunner interface {
	Run(ctx context.Context, request piharness.Request, onEvent func(piharness.Event)) error
}

// PiRunner drives one Pi browser-harness run: it claims the run, spawns the
// harness, persists each event as EvalHub trace evidence, and derives the
// objective terminal status from the harness verdict.
type PiRunner struct {
	Process HarnessProcessRunner
	Store   PiTraceStore
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

	request := buildHarnessRequest(command, ref.ProviderAttemptID)

	runState := &piRunState{}
	processErr := r.Process.Run(ctx, request, func(ev piharness.Event) {
		runState.observe(ev)
		if aerr := r.Store.AppendHarnessEvent(ctx, ref, ev); aerr != nil {
			runState.appendErr = aerr
		}
	})

	if runState.appendErr != nil && processErr == nil {
		processErr = runState.appendErr
	}

	outcome := r.deriveOutcome(processErr, runState)
	if cerr := r.Store.CompleteHarnessRun(ctx, ref, outcome); cerr != nil {
		if outcome.Status == domain.RunSucceeded {
			return fmt.Errorf("complete pi run: %w", cerr)
		}
		return &terminalRunError{cause: fmt.Errorf("complete pi run: %w", cerr)}
	}
	if outcome.Status == domain.RunSucceeded {
		return nil
	}
	if outcome.Error == "" {
		outcome.Error = "pi run terminated unsuccessfully"
	}
	return &terminalRunError{cause: errors.New(outcome.Error)}
}

type piRunState struct {
	casePassed    bool
	assistantText string
	failureError  string
	appendErr     error
}

func (s *piRunState) observe(ev piharness.Event) {
	switch ev.Type {
	case piharness.EventCaseCompleted:
		var payload piharness.CaseCompletedPayload
		_ = json.Unmarshal(ev.Payload, &payload)
		if payload.Verdict == "passed" {
			s.casePassed = true
		}
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

func (r PiRunner) deriveOutcome(processErr error, state *piRunState) PiRunOutcome {
	if processErr == nil {
		if state.casePassed {
			return PiRunOutcome{Status: domain.RunSucceeded, Assistant: state.assistantText}
		}
		return PiRunOutcome{
			Status:    domain.RunFailed,
			Error:     "harness settled without a passing browser case",
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

func buildHarnessRequest(command event.RunCommand, providerAttemptID string) piharness.Request {
	goal := ""
	for i := len(command.Messages) - 1; i >= 0; i-- {
		if command.Messages[i].Role == "user" {
			goal = command.Messages[i].Content
			break
		}
	}
	return piharness.Request{
		RunID:     command.RunID,
		AttemptID: providerAttemptID,
		Goal:      goal,
		Model: piharness.ModelRequest{
			Provider:        command.Model.Provider,
			Name:            command.Model.Name,
			ReasoningEffort: command.Model.ReasoningEffort,
		},
		WorkerURL:     command.WorkerURL,
		ArtifactScope: command.ArtifactScope,
	}
}
