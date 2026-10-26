package piharness

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"syscall"
	"time"
)

// Default command used when ProcessRunner is created via Default.
const (
	DefaultCommand = "node"
	DefaultMain    = "/app/pi-browser-harness/dist/main.js"
)

// Default limits.
const (
	DefaultMaxLineBytes   = 1 << 20  // 1 MiB
	DefaultMaxStderrBytes = 64 << 10 // 64 KiB
	DefaultGrace          = 5 * time.Second
)

// Stable process errors. Callers match these with errors.Is.
var (
	ErrLineTooLong     = errors.New("piharness: stdout line exceeds maximum size")
	ErrStderrOverflow  = errors.New("piharness: stderr output exceeds maximum size")
	ErrPrematureExit   = errors.New("piharness: process exited before emitting a terminal event")
	ErrMissingTerminal = errors.New("piharness: process exited cleanly without a terminal event")
	ErrTimeout         = errors.New("piharness: run timed out")
)

// HarnessFailure is returned when the harness emits a harness.failed terminal
// event. The Message carries the error string from the event payload.
type HarnessFailure struct {
	Message string
	Event   Event
}

func (h *HarnessFailure) Error() string {
	return "piharness: harness failed: " + h.Message
}

// ProcessRunner starts a pi-browser-harness child process, writes a single
// request to its stdin, and streams validated JSONL events from stdout to a
// callback. All fields are injectable; zero values fall back to defaults for
// limits but the command must be set explicitly (see Default).
type ProcessRunner struct {
	// Command and Args form the child argv. No shell is invoked.
	Command string
	Args    []string

	WorkingDirectory string
	Environment      []string

	// MaxLineBytes caps a single stdout JSONL line. Lines longer than this
	// cause Run to return ErrLineTooLong.
	MaxLineBytes int

	// MaxStderrBytes caps retained stderr diagnostics. Stderr beyond this
	// bound causes Run to return ErrStderrOverflow.
	MaxStderrBytes int

	// TerminationGrace is how long Run waits after sending SIGINT before
	// escalating to SIGKILL.
	TerminationGrace time.Duration

	// Timeout, if positive, bounds the total run wall-clock. When exceeded Run
	// returns ErrTimeout. Zero means no internal timeout (the caller's ctx
	// still applies).
	Timeout time.Duration

	// Clock returns the current time. It is used for timeout accounting.
	Clock func() time.Time

	// lastStderr retains the bounded stderr diagnostics from the most recent
	// Run. Tests inspect it; production callers that need diagnostics should
	// capture the error wrapping.
	lastStderr []byte
}

// Default returns a ProcessRunner configured with the production default
// command and limits.
func Default() *ProcessRunner {
	return &ProcessRunner{
		Command:          DefaultCommand,
		Args:             []string{DefaultMain},
		MaxLineBytes:     DefaultMaxLineBytes,
		MaxStderrBytes:   DefaultMaxStderrBytes,
		TerminationGrace: DefaultGrace,
		Clock:            time.Now,
	}
}

// Run executes the harness with the given request. onEvent is invoked
// synchronously, in sequence order, for every valid event. Run returns once the
// child process exits and stdout is fully consumed.
//
// The returned error is nil only when the child emits a harness.settled
// terminal event and exits successfully. A harness.failed terminal event yields
// a *HarnessFailure. Protocol or process failures yield the matching sentinel
// error.
func (r *ProcessRunner) Run(ctx context.Context, request Request, onEvent func(Event)) error {
	if r.Command == "" {
		return errors.New("piharness: ProcessRunner.Command is not set")
	}

	maxLine := r.MaxLineBytes
	if maxLine <= 0 {
		maxLine = DefaultMaxLineBytes
	}
	maxStderr := r.MaxStderrBytes
	if maxStderr <= 0 {
		maxStderr = DefaultMaxStderrBytes
	}
	grace := r.TerminationGrace
	if grace <= 0 {
		grace = DefaultGrace
	}
	clock := r.Clock
	if clock == nil {
		clock = time.Now
	}

	reqBytes, err := json.Marshal(request)
	if err != nil {
		return fmt.Errorf("piharness: marshal request: %w", err)
	}

	cmd := exec.Command(r.Command, r.Args...)
	cmd.Dir = r.WorkingDirectory
	cmd.Env = r.Environment
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("piharness: stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("piharness: stdout pipe: %w", err)
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("piharness: stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("piharness: start process: %w", err)
	}

	// Stderr is captured into a bounded buffer in a dedicated goroutine.
	stderrBuf := &boundedBuffer{max: maxStderr}
	stderrDone := make(chan struct{})
	go func() {
		io.Copy(stderrBuf, stderrPipe)
		close(stderrDone)
	}()

	// Context / timeout cancellation drives interrupt-then-kill.
	cancelErr := make(chan error, 1)
	go func() {
		var timeoutCh <-chan time.Time
		if r.Timeout > 0 {
			deadline := clock().Add(r.Timeout)
			delay := time.Until(deadline)
			if delay <= 0 {
				cancelErr <- ErrTimeout
				r.signalInterrupt(cmd)
				return
			}
			timer := time.NewTimer(delay)
			defer timer.Stop()
			timeoutCh = timer.C
		}
		select {
		case <-ctx.Done():
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				cancelErr <- ErrTimeout
			} else {
				cancelErr <- ctx.Err()
			}
		case <-timeoutCh:
			cancelErr <- ErrTimeout
		case <-stderrDone:
			// Process already finished; nothing to cancel. Return so we do
			// not fall through to signalInterrupt, which would send SIGINT to
			// a (possibly reused) process group after a clean exit.
			return
		}
		r.signalInterrupt(cmd)
	}()

	// Write the request and close stdin so the harness sees EOF after the
	// single request.
	if _, werr := stdin.Write(reqBytes); werr != nil {
		r.killProcess(cmd)
		<-stderrDone
		return fmt.Errorf("piharness: write request: %w", werr)
	}
	if cerr := stdin.Close(); cerr != nil {
		r.killProcess(cmd)
		<-stderrDone
		return fmt.Errorf("piharness: close stdin: %w", cerr)
	}

	// Read stdout line by line with a bounded scanner.
	dec := NewDecoder(request.RunID, request.AttemptID)
	scanner := bufio.NewScanner(stdout)
	// The effective max token size is max(cap(buf), max); size the buffer to
	// maxLine so the bound is honored.
	scanner.Buffer(make([]byte, 0, maxLine), maxLine)

	var decodeErr error
	var lastTerminal *Event
	for scanner.Scan() {
		line := scanner.Bytes()
		// Skip blank lines defensively; the protocol emits one record per line.
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		ev, derr := dec.Decode(line)
		if derr != nil {
			decodeErr = derr
			break
		}
		if isTerminal(ev.Type) {
			lastTerminal = ev
		}
		onEvent(*ev)
	}
	if decodeErr == nil {
		if serr := scanner.Err(); serr != nil {
			if errors.Is(serr, bufio.ErrTooLong) {
				decodeErr = ErrLineTooLong
			} else {
				decodeErr = fmt.Errorf("%w: %v", ErrMalformedJSONL, serr)
			}
		}
	}

	// Stop the child if we bailed out before natural completion.
	if decodeErr != nil {
		r.killProcess(cmd)
	}

	// Wait for the process and the stderr reader.
	waitErr := cmd.Wait()
	<-stderrDone
	r.lastStderr = stderrBuf.bytes()

	// Stderr overflow wins: it indicates the child produced unbounded
	// diagnostics and must not be treated as a successful run.
	if stderrBuf.overflow {
		return ErrStderrOverflow
	}

	// Protocol decode errors propagate as-is.
	if decodeErr != nil {
		return decodeErr
	}

	// Cancellation / timeout errors take precedence over exit-code analysis.
	select {
	case cerr := <-cancelErr:
		if cerr != nil {
			return cerr
		}
	default:
	}

	if lastTerminal != nil {
		if lastTerminal.Type == EventFailed {
			var fp FailedPayload
			_ = json.Unmarshal(lastTerminal.Payload, &fp)
			return &HarnessFailure{Message: fp.Error, Event: *lastTerminal}
		}
		// harness.settled: the run completed; exit code is informational.
		return nil
	}

	// No terminal event was emitted before the process exited. Distinguish a
	// clean-but-incomplete exit (zero status, no terminal) from a crash
	// (non-zero status, no terminal).
	if waitErr != nil {
		return fmt.Errorf("%w: %v", ErrPrematureExit, waitErr)
	}
	return fmt.Errorf("%w: process exited with status 0 but no terminal event", ErrMissingTerminal)
}

// signalInterrupt sends SIGINT to the child's process group, then waits up to
// the termination grace period before escalating to SIGKILL.
func (r *ProcessRunner) signalInterrupt(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	grace := r.TerminationGrace
	if grace <= 0 {
		grace = DefaultGrace
	}
	// Negative PID signals the entire process group.
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGINT)

	deadline := time.Now().Add(grace)
	for time.Now().Before(deadline) {
		if err := cmd.Process.Signal(syscall.Signal(0)); err != nil {
			return // process has exited
		}
		time.Sleep(5 * time.Millisecond)
	}
	r.killProcess(cmd)
}

// killProcess sends SIGKILL to the child's process group.
func (r *ProcessRunner) killProcess(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
}

// boundedBuffer retains at most max bytes. Writes beyond max set overflow and
// are discarded (but reported as fully consumed so the writer does not block).
type boundedBuffer struct {
	mu       sync.Mutex
	buf      []byte
	max      int
	overflow bool
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.overflow {
		return len(p), nil
	}
	remaining := b.max - len(b.buf)
	if len(p) > remaining {
		b.buf = append(b.buf, p[:remaining]...)
		b.overflow = true
		return len(p), nil
	}
	b.buf = append(b.buf, p...)
	return len(p), nil
}

func (b *boundedBuffer) bytes() []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]byte, len(b.buf))
	copy(out, b.buf)
	return out
}
