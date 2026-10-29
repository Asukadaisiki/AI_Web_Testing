package piharness

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// Re-exec the Go test binary as a real, SDK-free JSONL daemon.
func TestSharedChildProcess(t *testing.T) {
	mode := os.Getenv("PIHARNESS_SHARED_CHILD")
	if mode == "" {
		return
	}
	if mode == "detached-worker" {
		time.Sleep(time.Hour)
		os.Exit(0)
	}
	if mode == "no-ready" {
		time.Sleep(time.Hour)
		os.Exit(0)
	}
	if mode == "ignore-term" {
		signal.Ignore(syscall.SIGTERM)
	}
	var output sync.Mutex
	write := func(value any) {
		output.Lock()
		defer output.Unlock()
		data, _ := json.Marshal(value)
		fmt.Fprintf(os.Stdout, "%s\n", data)
	}
	if mode == "detached-cleanup" {
		child := exec.Command(os.Args[0], "-test.run=^TestSharedChildProcess$")
		child.Env = append(os.Environ(), "PIHARNESS_SHARED_CHILD=detached-worker")
		child.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
		if err := child.Start(); err != nil {
			os.Exit(3)
		}
		dir := os.Getenv("PIHARNESS_SHARED_CLEANUP_DIR")
		if err := os.WriteFile(filepath.Join(dir, "pid"), []byte(strconv.Itoa(child.Process.Pid)), 0600); err != nil {
			_ = child.Process.Kill()
			_ = child.Wait()
			os.Exit(3)
		}
		signals := make(chan os.Signal, 1)
		signal.Notify(signals, syscall.SIGTERM, syscall.SIGINT)
		go func() {
			<-signals
			// Cleanup can write diagnostics beyond the transport's bounds.
			// The parent must keep draining both pipes after a fatal error.
			fmt.Fprintln(os.Stdout, strings.Repeat("d", 128<<10))
			fmt.Fprint(os.Stderr, strings.Repeat("d", 128<<10))
			_ = syscall.Kill(-child.Process.Pid, syscall.SIGTERM)
			_ = child.Wait()
			time.Sleep(50 * time.Millisecond)
			if err := os.WriteFile(filepath.Join(dir, "cleaned"), []byte("done"), 0600); err != nil {
				os.Exit(3)
			}
			os.Exit(0)
		}()
	}
	write(map[string]any{"type": "harness.ready"})
	if mode == "blocked-stdin" {
		time.Sleep(time.Hour)
		os.Exit(0)
	}
	type childAttempt struct {
		cancel chan struct{}
		once   sync.Once
	}
	type childKey struct {
		runID     string
		attemptID string
	}
	var mu sync.Mutex
	active := make(map[childKey]*childAttempt)
	var workers sync.WaitGroup
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 4096), 4<<20)
	for scanner.Scan() {
		var command struct {
			Type      string  `json:"type"`
			Request   Request `json:"request"`
			RunID     string  `json:"run_id"`
			AttemptID string  `json:"attempt_id"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &command); err != nil {
			write(map[string]any{"type": "harness.protocol_error", "error": err.Error()})
			os.Exit(2)
		}
		switch command.Type {
		case "run":
			req := command.Request
			if strings.TrimSpace(req.Goal) == "" {
				write(map[string]any{"type": "harness.protocol_error", "error": "goal must be non-blank"})
				os.Exit(2)
			}
			key := childKey{req.RunID, req.AttemptID}
			a := &childAttempt{cancel: make(chan struct{})}
			mu.Lock()
			if active[key] != nil {
				write(map[string]any{"type": "harness.protocol_error", "error": "duplicate"})
				os.Exit(2)
			}
			active[key] = a
			mu.Unlock()
			workers.Add(1)
			go func() {
				defer workers.Done()
				var seq int64
				event := func(typ EventType, payload any) {
					seq++
					write(map[string]any{
						"type": typ, "run_id": req.RunID, "attempt_id": req.AttemptID,
						"sequence": seq, "timestamp": "2026-10-28T12:00:00Z", "payload": payload,
					})
				}
				event(EventStarted, map[string]any{})
				event(EventMessageCompleted, map[string]any{"text": strconv.Itoa(os.Getpid())})
				switch req.Goal {
				case "crash":
					time.Sleep(80 * time.Millisecond)
					os.Exit(7)
				case "protocol":
					write(map[string]any{"type": "harness.protocol_error", "error": "bad command"})
					return
				case "sequence":
					seq = 1
					event(EventMessageCompleted, map[string]any{"text": "duplicate"})
					return
				case "stale":
					write(map[string]any{
						"type": EventStarted, "run_id": req.RunID, "attempt_id": "stale-attempt",
						"sequence": 1, "timestamp": "2026-10-28T12:00:00Z", "payload": map[string]any{},
					})
					return
				case "wrong-run":
					write(map[string]any{
						"type": EventStarted, "run_id": "wrong-run", "attempt_id": req.AttemptID,
						"sequence": 3, "timestamp": "2026-10-28T12:00:00Z", "payload": map[string]any{},
					})
					return
				case "line":
					event(EventMessageCompleted, map[string]any{"text": strings.Repeat("x", 8192)})
					return
				case "stderr":
					fmt.Fprint(os.Stderr, strings.Repeat("e", 8192))
					<-a.cancel
				case "flood":
					for i := 0; i < 2000; i++ {
						event(EventMessageCompleted, map[string]any{"text": "flood"})
					}
				case "ignore-cancel":
					time.Sleep(time.Hour)
					return
				case "wait":
					<-a.cancel
					event(EventFailed, map[string]any{"error": "cancelled"})
					time.Sleep(150 * time.Millisecond)
				case "failed":
					event(EventFailed, map[string]any{"error": "isolated failure"})
				case "missing-terminal":
				default:
					time.Sleep(30 * time.Millisecond)
					event(EventSettled, map[string]any{"assistant_text": req.Goal, "verdict": "succeeded"})
					if req.Goal == "delay-release" {
						time.Sleep(150 * time.Millisecond)
					}
				}
				if req.Goal == "flood" {
					event(EventSettled, map[string]any{"assistant_text": "flood", "verdict": "succeeded"})
				}
				mu.Lock()
				delete(active, key)
				mu.Unlock()
				write(map[string]any{"type": "harness.released", "run_id": req.RunID, "attempt_id": req.AttemptID})
				if req.Goal == "late-event" {
					event(EventMessageCompleted, map[string]any{"text": "after release"})
				}
			}()
		case "cancel":
			mu.Lock()
			a := active[childKey{command.RunID, command.AttemptID}]
			mu.Unlock()
			if a != nil {
				a.once.Do(func() { close(a.cancel) })
			}
		case "shutdown":
			mu.Lock()
			for _, a := range active {
				a.once.Do(func() { close(a.cancel) })
			}
			mu.Unlock()
			workers.Wait()
			os.Exit(0)
		default:
			write(map[string]any{"type": "harness.protocol_error", "error": "unknown command"})
			os.Exit(2)
		}
	}
	if mode == "detached-cleanup" {
		// EOF must not bypass the in-flight signal handler's awaited cleanup.
		time.Sleep(time.Hour)
	}
	os.Exit(0)
}

func sharedTestRunner(t *testing.T, mode string) *SharedProcessRunner {
	t.Helper()
	r := &SharedProcessRunner{
		Command: os.Args[0],
		Args:    []string{"-test.run=^TestSharedChildProcess$"},
		Environment: append(os.Environ(),
			"PIHARNESS_SHARED_CHILD="+mode,
			"GORACE=atexit_sleep_ms=0",
		),
		MaxLineBytes: 4096, MaxStderrBytes: 512,
		TerminationGrace: 500 * time.Millisecond,
	}
	t.Cleanup(func() { _ = r.Close() })
	return r
}

func sharedRequest(id, goal string) Request {
	req := sampleRequest()
	req.RunID = "run_" + id
	req.AttemptID = "attempt_" + id
	req.Goal = goal
	return req
}

func sharedAwait(t *testing.T, done <-chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("shared runner did not return")
		return nil
	}
}

func sharedStarted(t *testing.T, started <-chan struct{}) {
	t.Helper()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("child did not start attempt")
	}
}

func sharedAsync(r *SharedProcessRunner, ctx context.Context, req Request) (<-chan error, <-chan struct{}) {
	done := make(chan error, 1)
	started := make(chan struct{})
	go func() {
		done <- r.Run(ctx, req, func(e Event) {
			if e.Type == EventStarted {
				close(started)
			}
		})
	}()
	return done, started
}

func TestSharedProcessReuseAndIndependentSequences(t *testing.T) {
	r := sharedTestRunner(t, "normal")
	var pid string
	for _, id := range []string{"first", "second"} {
		var events []Event
		if err := r.Run(context.Background(), sharedRequest(id, "normal"), func(e Event) {
			events = append(events, e)
		}); err != nil {
			t.Fatal(err)
		}
		if len(events) != 3 || events[0].Sequence != 1 || events[2].Sequence != 3 {
			t.Fatalf("events = %+v, want independent sequence 1..3", events)
		}
		var message MessageCompletedPayload
		_ = json.Unmarshal(events[1].Payload, &message)
		if pid != "" && message.Text != pid {
			t.Fatalf("process changed: %s -> %s", pid, message.Text)
		}
		pid = message.Text
		var settled SettledPayload
		_ = json.Unmarshal(events[2].Payload, &settled)
		if settled.AssistantText != "normal" || events[2].RunID != "run_"+id {
			t.Fatalf("request routing failed: %+v", events[2])
		}
	}
	if _, err := strconv.Atoi(pid); err != nil {
		t.Fatalf("child PID = %q", pid)
	}
}

func TestSharedInterleavingDoesNotBlockSiblingCallback(t *testing.T) {
	r := sharedTestRunner(t, "normal")
	block := make(chan struct{})
	var unblock sync.Once
	defer unblock.Do(func() { close(block) })
	started := make(chan struct{})
	done := make(chan error, 1)
	var slowSequence []int64
	go func() {
		done <- r.Run(context.Background(), sharedRequest("slow", "normal"), func(e Event) {
			slowSequence = append(slowSequence, e.Sequence)
			if e.Type == EventStarted {
				close(started)
				<-block
			}
		})
	}()
	sharedStarted(t, started)
	var sequence []int64
	if err := r.Run(context.Background(), sharedRequest("fast", "normal"), func(e Event) {
		sequence = append(sequence, e.Sequence)
	}); err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(sequence) != "[1 2 3]" {
		t.Fatalf("sibling sequence = %v", sequence)
	}
	select {
	case err := <-done:
		t.Fatalf("blocked Run returned early: %v", err)
	default:
	}
	unblock.Do(func() { close(block) })
	if err := sharedAwait(t, done); err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(slowSequence) != "[1 2 3]" {
		t.Fatalf("slow callback sequence = %v", slowSequence)
	}
}

func TestSharedTargetedCancellationWaitsForCleanup(t *testing.T) {
	r := sharedTestRunner(t, "normal")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cancelled, started := sharedAsync(r, ctx, sharedRequest("cancel", "wait"))
	sharedStarted(t, started)
	sibling, siblingStarted := sharedAsync(r, context.Background(), sharedRequest("sibling", "normal"))
	sharedStarted(t, siblingStarted)
	before := time.Now()
	cancel()
	if err := sharedAwait(t, sibling); err != nil {
		t.Fatalf("sibling was cancelled: %v", err)
	}
	if err := sharedAwait(t, cancelled); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel error = %v", err)
	}
	if time.Since(before) < 130*time.Millisecond {
		t.Fatal("Run returned before release acknowledged cleanup")
	}
	if err := r.Run(context.Background(), sharedRequest("reuse", "normal"), nil); err != nil {
		t.Fatalf("daemon not reusable after targeted cancellation: %v", err)
	}
}

func TestSharedSettledWaitsForRelease(t *testing.T) {
	r := sharedTestRunner(t, "normal")
	var terminal time.Time
	err := r.Run(context.Background(), sharedRequest("release", "delay-release"), func(e Event) {
		if e.Type == EventSettled {
			terminal = time.Now()
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if terminal.IsZero() || time.Since(terminal) < 130*time.Millisecond {
		t.Fatal("settled returned without awaiting cleanup")
	}
}

func TestSharedCrashFailsAllAndNextRunRestarts(t *testing.T) {
	r := sharedTestRunner(t, "normal")
	sibling, started := sharedAsync(r, context.Background(), sharedRequest("sibling", "wait"))
	sharedStarted(t, started)
	err := r.Run(context.Background(), sharedRequest("crash", "crash"), nil)
	if !errors.Is(err, ErrPrematureExit) {
		t.Fatalf("crash error = %v", err)
	}
	if err := sharedAwait(t, sibling); !errors.Is(err, ErrPrematureExit) {
		t.Fatalf("sibling crash error = %v", err)
	}
	if err := r.Run(context.Background(), sharedRequest("restart", "normal"), nil); err != nil {
		t.Fatalf("restart failed: %v", err)
	}
}

func TestSharedRejectsCancelledAndDuplicateAttempts(t *testing.T) {
	r := sharedTestRunner(t, "normal")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r.Command = "/no/such/executable"
	if err := r.Run(ctx, sharedRequest("cancelled", "normal"), nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("already cancelled context started child: %v", err)
	}
	r.Command = os.Args[0]
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	req := sharedRequest("duplicate", "wait")
	done, started := sharedAsync(r, ctx, req)
	sharedStarted(t, started)
	if err := r.Run(context.Background(), req, nil); !errors.Is(err, ErrDuplicateAttempt) {
		t.Fatal("duplicate active attempt accepted")
	}
	cancel()
	if err := sharedAwait(t, done); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestSharedGenericRequestValidation(t *testing.T) {
	req := sharedRequest("generic", "normal")
	req.WorkerURL = ""
	req.ArtifactScope = ""
	if err := validateSharedRequest(req); err != nil {
		t.Fatalf("generic request rejected: %v", err)
	}
	req.WorkerURL = "http://worker:8100"
	if err := validateSharedRequest(req); err != nil {
		t.Fatalf("optional worker alone rejected: %v", err)
	}
	req.WorkerURL = ""
	req.ArtifactScope = "generic"
	if err := validateSharedRequest(req); err != nil {
		t.Fatalf("optional scope alone rejected: %v", err)
	}
}

func TestSharedInvalidRequestDoesNotStartChild(t *testing.T) {
	for _, test := range []struct {
		field  string
		mutate func(*Request)
	}{
		{"protocol_version", func(r *Request) { r.ProtocolVersion = "v2" }},
		{"run_id", func(r *Request) { r.RunID = " \t" }},
		{"attempt_id", func(r *Request) { r.AttemptID = "" }},
		{"goal", func(r *Request) { r.Goal = "\n " }},
		{"artifact_scope", func(r *Request) { r.ArtifactScope = " " }},
		{"model.provider", func(r *Request) { r.Model.Provider = "" }},
		{"model.name", func(r *Request) { r.Model.Name = "\t" }},
		{"model.reasoning_effort", func(r *Request) { r.Model.ReasoningEffort = " " }},
		{"worker_url", func(r *Request) { r.WorkerURL = "/relative" }},
		{"worker_url", func(r *Request) { r.WorkerURL = "ftp://worker:8100" }},
		{"worker_url", func(r *Request) { r.WorkerURL = "http://" }},
		{"worker_url", func(r *Request) { r.WorkerURL = "http://worker:%" }},
	} {
		t.Run(test.field, func(t *testing.T) {
			r := sharedTestRunner(t, "normal")
			r.Command = "/no/such/executable"
			req := sharedRequest("invalid", "normal")
			test.mutate(&req)
			err := r.Run(context.Background(), req, func(Event) {
				t.Error("invalid request delivered an event")
			})
			if err == nil || !strings.Contains(err.Error(), "invalid shared request") ||
				!strings.Contains(err.Error(), test.field) {
				t.Fatalf("expected local %s validation error, got %v", test.field, err)
			}
			r.mu.Lock()
			started := r.process != nil
			r.mu.Unlock()
			if started {
				t.Fatal("invalid request started child")
			}
		})
	}
}

func TestSharedInvalidRequestDoesNotPoisonActiveSibling(t *testing.T) {
	r := sharedTestRunner(t, "normal")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done, started := sharedAsync(r, ctx, sharedRequest("sibling", "wait"))
	sharedStarted(t, started)
	r.mu.Lock()
	process := r.process
	r.mu.Unlock()
	if err := r.Run(context.Background(), sharedRequest("invalid", " "), nil); err == nil ||
		!strings.Contains(err.Error(), "invalid shared request") {
		t.Fatalf("invalid request was sent to daemon: %v", err)
	}
	cancel()
	if err := sharedAwait(t, done); !errors.Is(err, context.Canceled) ||
		errors.Is(err, ErrHarnessProtocol) {
		t.Fatalf("invalid sibling poisoned active attempt: %v", err)
	}
	req := sharedRequest("reuse", "normal")
	req.ProtocolVersion = ""
	if err := r.Run(context.Background(), req, nil); err != nil {
		t.Fatalf("valid default protocol request: %v", err)
	}
	r.mu.Lock()
	reused := r.process == process
	r.mu.Unlock()
	if !reused {
		t.Fatal("invalid request forced process restart")
	}
}

func TestSharedSameAttemptIDAcrossRunsCoexistsAndCancelsOnlyTuple(t *testing.T) {
	r := sharedTestRunner(t, "normal")
	first := sharedRequest("first-run", "wait")
	second := sharedRequest("second-run", "wait")
	second.AttemptID = first.AttemptID
	ctx1, cancel1 := context.WithCancel(context.Background())
	defer cancel1()
	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()
	firstDone, firstStarted := sharedAsync(r, ctx1, first)
	sharedStarted(t, firstStarted)
	secondDone, secondStarted := sharedAsync(r, ctx2, second)
	select {
	case err := <-secondDone:
		t.Fatalf("different run with same attempt ID rejected: %v", err)
	case <-secondStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("second tuple did not start")
	}
	cancel1()
	if err := sharedAwait(t, firstDone); !errors.Is(err, context.Canceled) {
		t.Fatalf("first tuple cancellation: %v", err)
	}
	select {
	case err := <-secondDone:
		t.Fatalf("cancel routed to sibling tuple: %v", err)
	default:
	}
	cancel2()
	if err := sharedAwait(t, secondDone); !errors.Is(err, context.Canceled) {
		t.Fatalf("first tuple deletion affected sibling tuple: %v", err)
	}
}

func TestSharedStrictProtocolAndOutputBounds(t *testing.T) {
	for _, test := range []struct {
		goal string
		want error
	}{
		{"sequence", ErrInvalidSequence},
		{"stale", ErrAttemptIDMismatch},
		{"wrong-run", ErrRunIDMismatch},
		{"line", ErrLineTooLong},
		{"stderr", ErrStderrOverflow},
		{"missing-terminal", ErrMissingTerminal},
		{"protocol", nil},
	} {
		t.Run(test.goal, func(t *testing.T) {
			r := sharedTestRunner(t, "normal")
			err := r.Run(context.Background(), sharedRequest("bad", test.goal), nil)
			if err == nil || (test.want != nil && !errors.Is(err, test.want)) {
				t.Fatalf("error = %v, want %v", err, test.want)
			}
			if err := r.Run(context.Background(), sharedRequest("restart", "normal"), nil); err != nil {
				t.Fatalf("restart after protocol failure: %v", err)
			}
		})
	}
}

func TestSharedHarnessFailureIsAttemptScoped(t *testing.T) {
	r := sharedTestRunner(t, "normal")
	var failure *HarnessFailure
	if err := r.Run(context.Background(), sharedRequest("failed", "failed"), nil); !errors.As(err, &failure) {
		t.Fatalf("failure = %v", err)
	}
	if failure.Message != "isolated failure" {
		t.Fatalf("failure message = %q", failure.Message)
	}
	if err := r.Run(context.Background(), sharedRequest("sibling", "normal"), nil); err != nil {
		t.Fatalf("failed attempt poisoned daemon: %v", err)
	}
}

func TestSharedCancellationGraceKillsAndReaps(t *testing.T) {
	r := sharedTestRunner(t, "normal")
	r.TerminationGrace = 50 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done, started := sharedAsync(r, ctx, sharedRequest("ignore", "ignore-cancel"))
	sharedStarted(t, started)
	sibling, siblingStarted := sharedAsync(r, context.Background(), sharedRequest("sibling", "wait"))
	sharedStarted(t, siblingStarted)
	cancel()
	if err := sharedAwait(t, done); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel error = %v", err)
	}
	if err := sharedAwait(t, sibling); !errors.Is(err, ErrCancellationGrace) ||
		errors.Is(err, context.Canceled) || errors.Is(err, ErrTimeout) {
		t.Fatalf("sibling must receive host failure, not target cancellation: %v", err)
	}
	if err := r.Run(context.Background(), sharedRequest("restart", "normal"), nil); err != nil {
		t.Fatal(err)
	}
}

func TestSharedFatalShutdownAllowsDetachedChildCleanup(t *testing.T) {
	for _, test := range []struct {
		goal string
		want error
	}{
		{"protocol", ErrHarnessProtocol},
		{"line", ErrLineTooLong},
		{"stderr", ErrStderrOverflow},
		{"ignore-cancel", ErrCancellationGrace},
	} {
		t.Run(test.goal, func(t *testing.T) {
			dir := t.TempDir()
			r := sharedTestRunner(t, "detached-cleanup")
			r.Environment = append(r.Environment, "PIHARNESS_SHARED_CLEANUP_DIR="+dir)
			t.Cleanup(func() {
				data, _ := os.ReadFile(filepath.Join(dir, "pid"))
				pid, _ := strconv.Atoi(string(data))
				if pid > 0 && syscall.Kill(pid, 0) == nil {
					_ = syscall.Kill(-pid, syscall.SIGKILL)
				}
			})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var err error
			if test.goal == "ignore-cancel" {
				done, started := sharedAsync(r, ctx, sharedRequest("fatal", test.goal))
				sharedStarted(t, started)
				cancel()
				err = sharedAwait(t, done)
			} else {
				err = r.Run(ctx, sharedRequest("fatal", test.goal), nil)
			}
			if !errors.Is(err, test.want) {
				t.Fatalf("fatal error = %v, want %v", err, test.want)
			}
			if _, err := os.Stat(filepath.Join(dir, "cleaned")); err != nil {
				t.Fatalf("Run returned before awaited SIGTERM cleanup: %v", err)
			}
			data, err := os.ReadFile(filepath.Join(dir, "pid"))
			if err != nil {
				t.Fatal(err)
			}
			pid, err := strconv.Atoi(string(data))
			if err != nil {
				t.Fatal(err)
			}
			if err := syscall.Kill(pid, 0); !errors.Is(err, syscall.ESRCH) {
				t.Fatalf("detached child %d not reaped: %v", pid, err)
			}
		})
	}
}

func TestSharedPerRunTimeout(t *testing.T) {
	r := sharedTestRunner(t, "normal")
	r.Timeout = 100 * time.Millisecond
	if err := r.Run(context.Background(), sharedRequest("timeout", "wait"), nil); !errors.Is(err, ErrTimeout) {
		t.Fatalf("timeout error = %v", err)
	}
}

func TestSharedBlockedStdinAndMissingReadyAreBounded(t *testing.T) {
	for _, mode := range []string{"blocked-stdin", "no-ready"} {
		t.Run(mode, func(t *testing.T) {
			r := sharedTestRunner(t, mode)
			r.TerminationGrace = 100 * time.Millisecond
			ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
			defer cancel()
			req := sharedRequest("blocked", strings.Repeat("x", 1<<20))
			done, _ := sharedAsync(r, ctx, req)
			if err := sharedAwait(t, done); err == nil {
				t.Fatal("unresponsive child succeeded")
			}
			if err := r.Close(); err != nil {
				// Transport failure is also permitted to be reported by Close.
				t.Logf("Close: %v", err)
			}
		})
	}
}

func TestSharedCloseDrainsAndRejectsFutureRuns(t *testing.T) {
	r := sharedTestRunner(t, "normal")
	done, started := sharedAsync(r, context.Background(), sharedRequest("close", "wait"))
	sharedStarted(t, started)
	before := time.Now()
	if err := r.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if time.Since(before) < 130*time.Millisecond {
		t.Fatal("Close did not drain cleanup")
	}
	var failure *HarnessFailure
	if err := sharedAwait(t, done); !errors.As(err, &failure) {
		t.Fatalf("shutdown run error = %v", err)
	}
	if err := r.Run(context.Background(), sharedRequest("closed", "normal"), nil); err == nil {
		t.Fatal("Run accepted after Close")
	}
	if err := r.Close(); err != nil {
		t.Fatalf("repeated Close: %v", err)
	}
}

func TestSharedCloseKillsUncooperativeChild(t *testing.T) {
	r := sharedTestRunner(t, "ignore-term")
	r.TerminationGrace = 100 * time.Millisecond
	var pid int
	started := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- r.Run(context.Background(), sharedRequest("close", "ignore-cancel"), func(e Event) {
			if e.Type == EventMessageCompleted {
				var payload MessageCompletedPayload
				_ = json.Unmarshal(e.Payload, &payload)
				pid, _ = strconv.Atoi(payload.Text)
				close(started)
			}
		})
	}()
	sharedStarted(t, started)
	before := time.Now()
	if err := r.Close(); !errors.Is(err, ErrShutdownTimeout) {
		t.Fatalf("Close error = %v", err)
	}
	if elapsed := time.Since(before); elapsed < r.TerminationGrace || elapsed > time.Second {
		t.Fatalf("uncooperative shutdown not bounded by grace: %v", elapsed)
	}
	if err := sharedAwait(t, done); err == nil {
		t.Fatal("unreleased attempt succeeded on Close")
	}
	if err := syscall.Kill(pid, 0); err == nil {
		t.Fatalf("child %d still alive after Close", pid)
	}
}

func TestSharedQueueOverflowFailsAllWithoutBlockingReader(t *testing.T) {
	r := sharedTestRunner(t, "normal")
	sibling, siblingStarted := sharedAsync(r, context.Background(), sharedRequest("sibling", "wait"))
	sharedStarted(t, siblingStarted)
	block := make(chan struct{})
	var unblock sync.Once
	defer unblock.Do(func() { close(block) })
	started := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- r.Run(context.Background(), sharedRequest("flood", "flood"), func(e Event) {
			if e.Type == EventStarted {
				close(started)
				<-block
			}
		})
	}()
	sharedStarted(t, started)
	err := sharedAwait(t, sibling)
	unblock.Do(func() { close(block) })
	if !errors.Is(err, ErrEventQueueOverflow) {
		t.Fatalf("sibling overflow error = %v", err)
	}
	if err := sharedAwait(t, done); !errors.Is(err, ErrEventQueueOverflow) {
		t.Fatalf("flood overflow error = %v", err)
	}
	if err := r.Run(context.Background(), sharedRequest("restart", "normal"), nil); err != nil {
		t.Fatal(err)
	}
}

func TestSharedLateEventsFailActiveSibling(t *testing.T) {
	r := sharedTestRunner(t, "normal")
	sibling, started := sharedAsync(r, context.Background(), sharedRequest("sibling", "wait"))
	sharedStarted(t, started)
	_ = r.Run(context.Background(), sharedRequest("late", "late-event"), nil)
	if err := sharedAwait(t, sibling); err == nil {
		t.Fatal("event after release was accepted")
	}
	if err := r.Run(context.Background(), sharedRequest("restart", "normal"), nil); err != nil {
		t.Fatal(err)
	}
}

func TestSharedBlockedCallbackStillEnforcesCancellationGrace(t *testing.T) {
	r := sharedTestRunner(t, "normal")
	r.TerminationGrace = 100 * time.Millisecond
	sibling, siblingStarted := sharedAsync(r, context.Background(), sharedRequest("sibling", "wait"))
	sharedStarted(t, siblingStarted)
	block := make(chan struct{})
	var unblock sync.Once
	defer unblock.Do(func() { close(block) })
	started := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- r.Run(ctx, sharedRequest("blocked-callback", "ignore-cancel"), func(e Event) {
			if e.Type == EventStarted {
				close(started)
				<-block
			}
		})
	}()
	sharedStarted(t, started)
	cancel()
	if err := sharedAwait(t, sibling); !errors.Is(err, ErrCancellationGrace) {
		t.Fatalf("blocked callback prevented cancellation watchdog: %v", err)
	}
	unblock.Do(func() { close(block) })
	if err := sharedAwait(t, done); !errors.Is(err, context.Canceled) {
		t.Fatalf("blocked callback error = %v", err)
	}
}

func TestSharedConcurrentCloseAndRun(t *testing.T) {
	r := sharedTestRunner(t, "normal")
	const count = 20
	runResults := make(chan error, count)
	started := make(chan struct{}, count)
	for i := 0; i < count; i++ {
		go func(i int) {
			runResults <- r.Run(context.Background(), sharedRequest(strconv.Itoa(i), "wait"), func(e Event) {
				if e.Type == EventStarted {
					started <- struct{}{}
				}
			})
		}(i)
	}
	for i := 0; i < count; i++ {
		sharedStarted(t, started)
	}
	closeResults := make(chan error, count)
	for i := 0; i < count; i++ {
		go func() { closeResults <- r.Close() }()
	}
	for i := 0; i < count; i++ {
		if err := sharedAwait(t, closeResults); err != nil {
			t.Fatalf("concurrent Close: %v", err)
		}
		var failure *HarnessFailure
		if err := sharedAwait(t, runResults); !errors.As(err, &failure) {
			t.Fatalf("concurrent shutdown Run: %v", err)
		}
	}
}

func TestSharedCallbackPanicDoesNotStrandSupervisor(t *testing.T) {
	r := sharedTestRunner(t, "normal")
	done := make(chan any, 1)
	started := make(chan struct{})
	go func() {
		defer func() { done <- recover() }()
		_ = r.Run(context.Background(), sharedRequest("panic", "wait"), func(Event) {
			close(started)
			panic("callback panic")
		})
	}()
	sharedStarted(t, started)
	select {
	case value := <-done:
		if value != "callback panic" {
			t.Fatalf("callback panic = %v", value)
		}
	case <-time.After(300 * time.Millisecond):
		t.Fatal("callback panic stranded the Run watchdog")
	}
	if err := r.Run(context.Background(), sharedRequest("restart", "normal"), nil); err != nil {
		t.Fatalf("restart after callback panic: %v", err)
	}
}

func TestSharedLateGraceTickDoesNotKillReleasedAttemptSibling(t *testing.T) {
	r := sharedTestRunner(t, "normal")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req := sharedRequest("release-race", "wait")
	done, started := sharedAsync(r, ctx, req)
	sharedStarted(t, started)
	r.mu.Lock()
	s := r.process
	r.mu.Unlock()
	s.mu.Lock()
	a := s.attempts[sharedAttemptKey{req.RunID, req.AttemptID}]
	s.mu.Unlock()
	cancel()
	if err := sharedAwait(t, done); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	siblingCtx, cancelSibling := context.WithCancel(context.Background())
	defer cancelSibling()
	sibling, siblingStarted := sharedAsync(r, siblingCtx, sharedRequest("sibling", "wait"))
	sharedStarted(t, siblingStarted)
	// Model an expired timer selected just before the reader applied release.
	s.expireCancellation(a)
	cancelSibling()
	if err := sharedAwait(t, sibling); !errors.Is(err, context.Canceled) || errors.Is(err, ErrCancellationGrace) {
		t.Fatalf("late grace tick killed sibling after release: %v", err)
	}
}
