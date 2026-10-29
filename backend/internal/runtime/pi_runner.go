package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/consumer"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/domain"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/event"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/piharness"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/postgres"
)

// A durable cancellation is checked at most four times per second per run.
const cancellationPollInterval = 250 * time.Millisecond

// HarnessProcessRunner submits an isolated execution session and streams
// normalized events. Its transport can host any compatible Harness.
type HarnessProcessRunner interface {
	Run(ctx context.Context, request piharness.Request, onEvent func(piharness.Event)) error
}

type releaseAwareHarnessProcess interface {
	RunWithRelease(context.Context, piharness.Request, func(piharness.Event)) (bool, error)
}

// PiRunner claims a run, submits its session, persists normalized trace
// evidence, and records the selected Harness's objective verdict.
type PiRunner struct {
	Process                HarnessProcessRunner
	Store                  PiTraceStore
	DefaultReasoningEffort string
	LeaseTTL               time.Duration
}

// Run executes the harness for command and persists its trace. A replay of an
// already terminal run creates no session and returns nil.
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
	if errors.Is(err, postgres.ErrCapacityBusy) {
		return consumer.DeferDelivery(err)
	}
	if err != nil {
		return fmt.Errorf("start pi run: %w", err)
	}

	runState := &piRunState{}
	processCtx, cancelProcess := context.WithCancel(ctx)
	defer cancelProcess()
	if ref.Fence.FencingToken <= 0 || ref.Fence.ProjectID == "" || ref.Fence.EnvironmentID == "" ||
		ref.Fence.ProfileGeneration <= 0 || ref.Fence.RuntimeWorkerID == "" || ref.Fence.BrowserWorkerID == "" {
		return errors.New("harness attempt requires a current managed fence")
	}
	leases, ok := r.Store.(PiLeaseStore)
	if !ok {
		return errors.New("managed pi store requires lease lifecycle support")
	}
	releaseProcess, ok := r.Process.(releaseAwareHarnessProcess)
	if !ok {
		return errors.New("managed harness transport requires explicit release acknowledgement")
	}
	leaseErrors := make(chan error, 1)
	leaseDone := make(chan struct{})
	leaseExited := make(chan struct{})
	command.ProjectID = ref.Fence.ProjectID
	command.EnvironmentID = ref.Fence.EnvironmentID
	command.ProfileGeneration = ref.Fence.ProfileGeneration
	command.FencingToken = ref.Fence.FencingToken
	go r.watchLease(ctx, ref, leases, cancelProcess, leaseDone, leaseExited, leaseErrors)
	defer func() {
		close(leaseDone)
		<-leaseExited
	}()
	workerURL := ref.WorkerURL
	if strings.TrimSpace(workerURL) == "" {
		return errors.New("managed harness browser assignment endpoint is required")
	}
	request := buildHarnessRequest(command, workerURL, ref.ProviderAttemptID, r.DefaultReasoningEffort)
	watchDone := make(chan struct{})
	watchExited := make(chan struct{})
	go r.watchCancellation(ctx, ref.RunID, cancelProcess, watchDone, watchExited)

	var processErr error
	released := false
	if !ref.CancelRequested {
		onEvent := func(ev piharness.Event) {
			if runState.appendErr != nil {
				return
			}
			if aerr := r.Store.AppendHarnessEvent(ctx, ref, ev); aerr != nil {
				runState.appendErr = aerr
				cancelProcess()
				return
			}
			runState.observe(ev)
		}
		processErr = leases.MarkRunStarted(ctx, ref)
		if processErr == nil {
			released, processErr = releaseProcess.RunWithRelease(processCtx, request, onEvent)
		}
	}
	close(watchDone)
	<-watchExited
	cancelProcess()

	select {
	case leaseErr := <-leaseErrors:
		return consumer.DeferDelivery(leaseErr)
	default:
	}
	if errors.Is(runState.appendErr, postgres.ErrStaleFence) || errors.Is(processErr, postgres.ErrStaleFence) {
		return consumer.DeferDelivery(postgres.ErrStaleFence)
	}
	if runState.appendErr != nil {
		// Targeted cancellation waits for the host's cleanup acknowledgement.
		// The underlying write failure, rather than our stop signal, determines
		// the verdict and prevents unsafe automatic task replay.
		processErr = fmt.Errorf("persist harness evidence: %w", runState.appendErr)
	}

	cancelRequested, cancelErr := r.Store.CancellationRequested(ctx, ref.RunID)
	if cancelErr != nil && processErr == nil {
		processErr = fmt.Errorf("read cancellation state before completion: %w", cancelErr)
	}
	outcome := r.deriveOutcome(processErr, runState, ref.CancelRequested || cancelRequested)
	if !released && !ref.CancelRequested {
		outcome.Status = domain.RunFailed
		outcome.Uncertain = true
		if outcome.Error == "" {
			outcome.Error = postgres.ErrReleaseUnacknowledged.Error()
		}
	}
	// Retry only the terminal write, keeping the original verdict and assistant
	// output. Even a cancelled delivery gets a bounded chance to persist it.
	// If all writes fail, inbox redelivery reconciles the existing attempt
	// instead of submitting another session.
	completeCtx, cancelComplete := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancelComplete()
	var status domain.RunStatus
	var completeErr error
	for range 3 {
		status, completeErr = r.Store.CompleteHarnessRun(completeCtx, ref, outcome)
		if completeErr == nil || completeCtx.Err() != nil {
			break
		}
	}
	if completeErr != nil {
		return fmt.Errorf("complete pi run: %w", completeErr)
	}
	if status != "" {
		outcome.Status = status
	}
	if ref.CancelRequested {
		if err := leases.AbandonUnstartedRun(completeCtx, ref); err != nil {
			return fmt.Errorf("abandon unstarted pi lease: %w", err)
		}
	} else if !released {
		return &terminalRunError{cause: errors.New(outcome.Error)}
	} else {
		if err := leases.ReleaseRunLease(completeCtx, ref, released); err != nil {
			return fmt.Errorf("release pi lease: %w", err)
		}
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

func (r PiRunner) watchLease(ctx context.Context, ref HarnessAttemptRef, leases PiLeaseStore,
	cancel context.CancelFunc, done <-chan struct{}, exited chan<- struct{}, errs chan<- error) {
	defer close(exited)
	ttl := r.LeaseTTL
	if ttl == 0 {
		ttl = postgres.DefaultHarnessLeaseTTL
	}
	interval := ttl / 3
	if interval < time.Millisecond {
		interval = time.Millisecond
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			return
		case <-ctx.Done():
			cancel()
			return
		case <-ticker.C:
		}
		renewCtx, stop := context.WithTimeout(context.WithoutCancel(ctx), interval)
		err := leases.RenewRunLease(renewCtx, ref, ttl)
		stop()
		if err != nil {
			errs <- errors.Join(postgres.ErrStaleFence, err)
			cancel()
			return
		}
	}
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
	harnessVerdict string
	assistantText  string
	failureError   string
	appendErr      error
}

func (s *piRunState) observe(ev piharness.Event) {
	switch ev.Type {
	case piharness.EventSettled:
		var payload piharness.SettledPayload
		_ = json.Unmarshal(ev.Payload, &payload)
		s.assistantText = payload.AssistantText
		s.harnessVerdict = payload.Verdict
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
		if state.harnessVerdict == "succeeded" {
			return PiRunOutcome{Status: domain.RunSucceeded, Assistant: state.assistantText}
		}
		if state.harnessVerdict == "failed" {
			return PiRunOutcome{
				Status: domain.RunFailed, Assistant: state.assistantText,
				Error: "selected harness completion policy failed",
			}
		}
		return PiRunOutcome{
			Status:    domain.RunFailed,
			Error:     "harness settled without an explicit completion verdict",
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
	for _, message := range command.Messages {
		if message.Role != "user" {
			continue
		}
		var metadata struct {
			ResponseKind string `json:"response_kind"`
		}
		_ = json.Unmarshal(message.Metadata, &metadata)
		if metadata.ResponseKind != "" {
			goal += fmt.Sprintf("\n\nUser response (%s):\n%s", metadata.ResponseKind, message.Content)
		} else {
			goal = message.Content
		}
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
		WorkerURL:         workerURL,
		ArtifactScope:     artifactScope,
		Execution:         command.Execution.Clone(),
		ProjectID:         command.ProjectID,
		EnvironmentID:     command.EnvironmentID,
		ProfileGeneration: command.ProfileGeneration,
		FencingToken:      command.FencingToken,
	}
}
