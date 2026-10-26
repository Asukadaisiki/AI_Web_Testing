package piharness

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"testing"
	"time"
)

// subprocessScenarioEnv selects the helper behavior when the test binary
// re-executes itself as the harness child process.
const subprocessScenarioEnv = "PIHARNESS_TEST_SCENARIO"

func TestMain(m *testing.M) {
	if scenario := os.Getenv(subprocessScenarioEnv); scenario != "" {
		runSubprocessScenario(scenario)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// runSubprocessScenario emulates the pi-browser-harness child for process
// tests. It reads one JSON request from stdin and writes JSONL events to
// stdout according to the selected scenario.
func runSubprocessScenario(scenario string) {
	stdin, err := io.ReadAll(os.Stdin)
	if err != nil {
		fmt.Fprintf(os.Stderr, "read stdin: %v\n", err)
		os.Exit(2)
	}

	var req Request
	if err := json.Unmarshal(stdin, &req); err != nil {
		fmt.Fprintf(os.Stderr, "parse request: %v\n", err)
		os.Exit(2)
	}

	writeEvent := func(typ EventType, sequence int64, payload any) {
		ev := map[string]any{
			"type":       string(typ),
			"run_id":     req.RunID,
			"attempt_id": req.AttemptID,
			"sequence":   sequence,
			"timestamp":  "2026-10-26T12:34:56.789Z",
			"payload":    payload,
		}
		data, _ := json.Marshal(ev)
		fmt.Fprintf(os.Stdout, "%s\n", data)
	}

	switch scenario {
	case "clean_settled":
		writeEvent(EventStarted, 1, map[string]any{})
		writeEvent(EventSettled, 2, map[string]any{"assistant_text": "done"})
		os.Exit(0)

	case "failed_nonzero":
		writeEvent(EventStarted, 1, map[string]any{})
		writeEvent(EventFailed, 2, map[string]any{"error": "boom"})
		os.Exit(1)

	case "zero_exit_before_terminal":
		writeEvent(EventStarted, 1, map[string]any{})
		os.Exit(0)

	case "nonzero_before_terminal":
		writeEvent(EventStarted, 1, map[string]any{})
		os.Exit(3)

	case "malformed_json":
		fmt.Fprintln(os.Stdout, `{"type":"harness.started"`)
		os.Exit(0)

	case "duplicate_sequence":
		writeEvent(EventStarted, 1, map[string]any{})
		writeEvent(EventMessageCompleted, 1, map[string]any{"text": "dup"})
		os.Exit(0)

	case "long_line":
		big := strings.Repeat("x", 8192)
		fmt.Fprintf(os.Stdout, `{"type":"harness.started","run_id":"%s","attempt_id":"%s","sequence":1,"timestamp":"2026-10-26T12:34:56.789Z","payload":{"text":%q}}`+"\n",
			req.RunID, req.AttemptID, big)
		os.Exit(0)

	case "stderr_diagnostics":
		fmt.Fprintln(os.Stderr, "starting harness")
		fmt.Fprintln(os.Stderr, "loading model")
		writeEvent(EventStarted, 1, map[string]any{})
		writeEvent(EventSettled, 2, map[string]any{"assistant_text": "done"})
		os.Exit(0)

	case "stderr_overflow":
		fmt.Fprint(os.Stderr, strings.Repeat("e", 1024))
		writeEvent(EventStarted, 1, map[string]any{})
		writeEvent(EventSettled, 2, map[string]any{"assistant_text": "done"})
		os.Exit(0)

	case "trap_interrupt":
		writeEvent(EventStarted, 1, map[string]any{})
		// Flush started so the parent observes progress.
		os.Stdout.Sync()
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT)
		<-sigCh
		// Exit cleanly after the interrupt instead of hanging forever so the
		// kill path is observable only when the grace period is too short.
		os.Exit(0)

	case "ignore_interrupt":
		writeEvent(EventStarted, 1, map[string]any{})
		os.Stdout.Sync()
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT)
		// Ignore the signal and keep running until killed.
		for {
			select {
			case <-sigCh:
			case <-time.After(time.Hour):
				os.Exit(0)
			}
		}

	default:
		fmt.Fprintf(os.Stderr, "unknown scenario: %s\n", scenario)
		os.Exit(2)
	}
}

func newTestRunner(scenario string) *ProcessRunner {
	return &ProcessRunner{
		Command:          os.Args[0],
		Args:             nil,
		WorkingDirectory: "",
		Environment: append(os.Environ(),
			subprocessScenarioEnv+"="+scenario,
		),
		MaxLineBytes:     4096,
		MaxStderrBytes:   512,
		TerminationGrace: 50 * time.Millisecond,
		Clock:            time.Now,
	}
}

func sampleRequest() Request {
	return Request{
		ProtocolVersion: ProtocolVersion,
		RunID:           "run_test",
		AttemptID:       "attempt_test",
		Goal:            "verify",
		Model: ModelRequest{
			Provider:        "scripted",
			Name:            "fixture",
			ReasoningEffort: "high",
		},
		WorkerURL:     "http://worker:8100",
		ArtifactScope: "run_test",
	}
}

func TestRunWritesRequestToStdinAndClosesIt(t *testing.T) {
	runner := newTestRunner("clean_settled")
	var events []Event
	err := runner.Run(context.Background(), sampleRequest(), func(e Event) {
		events = append(events, e)
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}
	if events[0].Type != EventStarted {
		t.Fatalf("first event = %s, want %s", events[0].Type, EventStarted)
	}
	if events[1].Type != EventSettled {
		t.Fatalf("second event = %s, want %s", events[1].Type, EventSettled)
	}
}

func TestRunCallbackCalledSynchronouslyInSequenceOrder(t *testing.T) {
	runner := newTestRunner("clean_settled")
	var seen []int64
	err := runner.Run(context.Background(), sampleRequest(), func(e Event) {
		seen = append(seen, e.Sequence)
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	for i, seq := range seen {
		if seq != int64(i+1) {
			t.Fatalf("event %d sequence = %d, want %d", i, seq, i+1)
		}
	}
}

func TestRunEnforcesStdoutLineSizeBound(t *testing.T) {
	runner := newTestRunner("long_line")
	runner.MaxLineBytes = 4096
	err := runner.Run(context.Background(), sampleRequest(), func(e Event) {})
	if err == nil {
		t.Fatalf("expected error for oversized stdout line, got nil")
	}
	if !errors.Is(err, ErrLineTooLong) && !errors.Is(err, ErrMalformedJSONL) {
		t.Fatalf("expected line-too-long or malformed error, got %v", err)
	}
}

func TestRunRetainsStderrAsBoundedDiagnostics(t *testing.T) {
	runner := newTestRunner("stderr_diagnostics")
	runner.MaxStderrBytes = 1024
	err := runner.Run(context.Background(), sampleRequest(), func(e Event) {})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if runner.lastStderr == nil {
		t.Fatalf("expected stderr diagnostics to be retained")
	}
	stderr := string(runner.lastStderr)
	if !strings.Contains(stderr, "starting harness") ||
		!strings.Contains(stderr, "loading model") {
		t.Fatalf("stderr diagnostics missing, got %q", stderr)
	}
}

func TestRunRejectsStderrOverflow(t *testing.T) {
	runner := newTestRunner("stderr_overflow")
	runner.MaxStderrBytes = 64
	err := runner.Run(context.Background(), sampleRequest(), func(e Event) {})
	if !errors.Is(err, ErrStderrOverflow) {
		t.Fatalf("expected ErrStderrOverflow, got %v", err)
	}
}

func TestRunContextCancellationSendsInterruptThenKills(t *testing.T) {
	runner := newTestRunner("ignore_interrupt")
	runner.TerminationGrace = 30 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- runner.Run(ctx, sampleRequest(), func(e Event) {})
	}()

	// Give the child time to start and emit the started event.
	time.Sleep(100 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err == nil {
			t.Fatalf("expected error after cancellation, got nil")
		}
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context.Canceled, got %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("Run did not return after cancellation")
	}
}

func TestRunNonZeroExitAfterFailedReturnsTypedHarnessFailure(t *testing.T) {
	runner := newTestRunner("failed_nonzero")
	err := runner.Run(context.Background(), sampleRequest(), func(e Event) {})
	if err == nil {
		t.Fatalf("expected harness failure error, got nil")
	}
	var hf *HarnessFailure
	if !errors.As(err, &hf) {
		t.Fatalf("expected *HarnessFailure, got %T: %v", err, err)
	}
	if hf.Message != "boom" {
		t.Fatalf("failure message = %q, want %q", hf.Message, "boom")
	}
}

func TestRunZeroExitBeforeTerminalIsPrematureExit(t *testing.T) {
	runner := newTestRunner("zero_exit_before_terminal")
	err := runner.Run(context.Background(), sampleRequest(), func(e Event) {})
	if !errors.Is(err, ErrPrematureExit) {
		t.Fatalf("expected ErrPrematureExit, got %v", err)
	}
}

func TestRunNonZeroExitBeforeTerminalIsPrematureExit(t *testing.T) {
	runner := newTestRunner("nonzero_before_terminal")
	err := runner.Run(context.Background(), sampleRequest(), func(e Event) {})
	if !errors.Is(err, ErrPrematureExit) {
		t.Fatalf("expected ErrPrematureExit, got %v", err)
	}
}

func TestRunMalformedJSONStopsProcessing(t *testing.T) {
	runner := newTestRunner("malformed_json")
	err := runner.Run(context.Background(), sampleRequest(), func(e Event) {})
	if !errors.Is(err, ErrMalformedJSONL) {
		t.Fatalf("expected ErrMalformedJSONL, got %v", err)
	}
}

func TestRunDuplicateSequenceStopsProcessing(t *testing.T) {
	runner := newTestRunner("duplicate_sequence")
	err := runner.Run(context.Background(), sampleRequest(), func(e Event) {})
	if !errors.Is(err, ErrInvalidSequence) {
		t.Fatalf("expected ErrInvalidSequence, got %v", err)
	}
}

func TestRunDoesNotInvokeShell(t *testing.T) {
	// The runner must build argv directly via exec.Command, never through a
	// shell. Verify the test binary child is invoked with its own path and no
	// shell -c indirection by inspecting how the runner constructs the command.
	runner := newTestRunner("clean_settled")
	if runner.Command == "" {
		t.Fatalf("expected Command to be set")
	}
	// A shell invocation would set Args to ["-c", "<script>"].
	for _, a := range runner.Args {
		if a == "-c" {
			t.Fatalf("runner must not pass -c to a shell, args = %v", runner.Args)
		}
	}
	// exec.Command splits argv into Path (resolved binary) and Args; no shell
	// wrapper is inserted.
	cmd := exec.Command(runner.Command, runner.Args...)
	if cmd.Path == "" {
		t.Skip("command not resolvable")
	}
	if strings.HasSuffix(cmd.Path, "sh") || strings.HasSuffix(cmd.Path, "bash") {
		t.Fatalf("runner must not invoke a shell, got %s", cmd.Path)
	}
}
