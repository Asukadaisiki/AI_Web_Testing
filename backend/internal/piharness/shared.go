package piharness

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

var (
	ErrSharedClosed         = errors.New("piharness: shared runner is closed")
	ErrDuplicateAttempt     = errors.New("piharness: duplicate active attempt")
	ErrEventQueueOverflow   = errors.New("piharness: attempt event queue overflow")
	ErrCommandQueueOverflow = errors.New("piharness: command queue overflow")
	ErrHarnessProtocol      = errors.New("piharness: shared harness protocol error")
	ErrReadyTimeout         = errors.New("piharness: shared harness ready handshake timed out")
	ErrCancellationGrace    = errors.New("piharness: attempt cleanup grace expired")
	ErrShutdownTimeout      = errors.New("piharness: shared harness shutdown timed out")
)

const (
	sharedEventQueueSize   = 128
	sharedCommandQueueSize = 64
)

// SharedProcessRunner supervises a lazy, reusable JSONL daemon. Command and Args
// are used verbatim; the caller supplies the server entrypoint and flags.
// Configure fields before use and do not mutate them concurrently with Run.
// Callbacks run sequentially in their own Run goroutine, never in the reader.
type SharedProcessRunner struct {
	Command          string
	Args             []string
	WorkingDirectory string
	Environment      []string
	MaxLineBytes     int
	MaxStderrBytes   int
	TerminationGrace time.Duration
	Timeout          time.Duration
	Clock            func() time.Time

	mu         sync.Mutex
	process    *sharedProcess
	closed     bool
	closeDone  chan struct{}
	closeErr   error
	lastStderr []byte
}

type sharedAttempt struct {
	decoder   *Decoder
	events    chan Event
	release   chan struct{}
	watchDone chan struct{}
	released  bool
	terminal  *Event
	cancelErr error
	runID     string
	attemptID string
}

type sharedAttemptKey struct {
	runID     string
	attemptID string
}

// mu owns all protocol and attempt state. The writer alone owns stdin writes.
// stop announces termination; done means the child and all pipe workers reaped.
type sharedProcess struct {
	mu        sync.Mutex
	cmd       *exec.Cmd
	stdin     *os.File
	stdout    *os.File
	stderr    *os.File
	stderrBuf *boundedBuffer
	commands  chan []byte
	attempts  map[sharedAttemptKey]*sharedAttempt
	ready     chan struct{}
	stop      chan struct{}
	done      chan struct{}
	isReady   bool
	stopped   bool
	closing   bool
	err       error
	grace     time.Duration
	workers   sync.WaitGroup
}

func (r *SharedProcessRunner) grace() time.Duration {
	if r.TerminationGrace > 0 {
		return r.TerminationGrace
	}
	return DefaultGrace
}

func sharedContextError(ctx context.Context) error {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return ErrTimeout
	}
	return ctx.Err()
}

func validateSharedRequest(request Request) error {
	if request.ProtocolVersion != "" && request.ProtocolVersion != ProtocolVersion {
		return fmt.Errorf("piharness: invalid shared request: unsupported protocol_version %q", request.ProtocolVersion)
	}
	for _, field := range []struct {
		name  string
		value string
	}{
		{"run_id", request.RunID},
		{"attempt_id", request.AttemptID},
		{"goal", request.Goal},
		{"model.provider", request.Model.Provider},
		{"model.name", request.Model.Name},
		{"model.reasoning_effort", request.Model.ReasoningEffort},
	} {
		if strings.TrimSpace(field.value) == "" {
			return fmt.Errorf("piharness: invalid shared request: %s must be non-blank", field.name)
		}
	}
	if request.ArtifactScope != "" && strings.TrimSpace(request.ArtifactScope) == "" {
		return errors.New("piharness: invalid shared request: artifact_scope must be non-blank")
	}
	if request.WorkerURL == "" {
		return nil
	}
	u, err := url.Parse(strings.TrimSpace(request.WorkerURL))
	if err != nil || u.Hostname() == "" ||
		(!strings.EqualFold(u.Scheme, "http") && !strings.EqualFold(u.Scheme, "https")) {
		return errors.New("piharness: invalid shared request: worker_url must be an absolute HTTP URL")
	}
	if u.Port() != "" {
		port, err := strconv.Atoi(u.Port())
		if err != nil || port > 65535 {
			return errors.New("piharness: invalid shared request: worker_url has an invalid port")
		}
	}
	return nil
}

// Run holds the attempt until harness.released, including during cancellation.
// A transport failure ends all active attempts without replaying requests.
func (r *SharedProcessRunner) Run(ctx context.Context, request Request, onEvent func(Event)) error {
	return r.run(ctx, request, onEvent, nil)
}

// RunWithRelease distinguishes a host cleanup acknowledgement from transport
// loss, including when the acknowledged outcome is cancellation or failure.
func (r *SharedProcessRunner) RunWithRelease(ctx context.Context, request Request, onEvent func(Event)) (bool, error) {
	released := false
	err := r.run(ctx, request, onEvent, func() { released = true })
	return released, err
}

func (r *SharedProcessRunner) run(ctx context.Context, request Request, onEvent func(Event), onReleased func()) error {
	if ctx.Err() != nil {
		return sharedContextError(ctx)
	}
	if err := validateSharedRequest(request); err != nil {
		return err
	}
	var timeout <-chan time.Time
	if r.Timeout > 0 {
		clock := r.Clock
		if clock == nil {
			clock = time.Now
		}
		timer := time.NewTimer(time.Until(clock().Add(r.Timeout)))
		defer timer.Stop()
		timeout = timer.C
	}
	data, err := json.Marshal(struct {
		Type    string  `json:"type"`
		Request Request `json:"request"`
	}{Type: "run", Request: request})
	if err != nil {
		return fmt.Errorf("piharness: marshal shared request: %w", err)
	}
	s, err := r.getProcess(ctx, timeout)
	if err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return sharedContextError(ctx)
	case <-timeout:
		return ErrTimeout
	case <-s.stop:
		return s.failure()
	case <-s.ready:
	}

	// Close and registration are ordered together; shutdown cannot overtake a
	// run command that the host has accepted.
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return ErrSharedClosed
	}
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		r.mu.Unlock()
		return s.failure()
	}
	if ctx.Err() != nil {
		s.mu.Unlock()
		r.mu.Unlock()
		return sharedContextError(ctx)
	}
	select {
	case <-timeout:
		s.mu.Unlock()
		r.mu.Unlock()
		return ErrTimeout
	default:
	}
	key := sharedAttemptKey{request.RunID, request.AttemptID}
	if s.attempts[key] != nil {
		s.mu.Unlock()
		r.mu.Unlock()
		return fmt.Errorf("%w: (%s, %s)", ErrDuplicateAttempt, request.RunID, request.AttemptID)
	}
	a := &sharedAttempt{
		decoder: NewDecoder(request.RunID, request.AttemptID),
		events:  make(chan Event, sharedEventQueueSize), release: make(chan struct{}),
		watchDone: make(chan struct{}), runID: request.RunID, attemptID: request.AttemptID,
	}
	s.attempts[key] = a
	err = s.enqueueLocked(data)
	s.mu.Unlock()
	r.mu.Unlock()
	if err != nil {
		s.abort(err)
	}
	go s.watch(ctx, timeout, a)
	defer func() {
		// Panic and Goexit in a callback must not strand the watchdog or child.
		s.mu.Lock()
		if !a.released && !s.stopped {
			s.abortLocked(fmt.Errorf("%w: callback exited before release", ErrHarnessProtocol))
		}
		stopped := s.stopped
		s.mu.Unlock()
		<-a.watchDone
		if stopped {
			<-s.done
		}
		s.mu.Lock()
		delete(s.attempts, key)
		s.mu.Unlock()
	}()

	deliver := func(e Event) {
		if onEvent != nil {
			onEvent(e)
		}
	}
	for {
		select {
		case e := <-a.events:
			deliver(e)
		case <-a.release:
			// The reader queued every event before acknowledging release.
			for {
				select {
				case e := <-a.events:
					deliver(e)
				default:
					if onReleased != nil {
						onReleased()
					}
					return s.result(a)
				}
			}
		case <-s.stop:
			s.mu.Lock()
			released, failure := a.released, s.err
			s.mu.Unlock()
			if released && failure == nil {
				// A graceful shutdown may race the Run's queue drain.
				for {
					select {
					case e := <-a.events:
						deliver(e)
					default:
						if onReleased != nil {
							onReleased()
						}
						return s.result(a)
					}
				}
			}
			return s.attemptFailure(a)
		}
	}
}

func (r *SharedProcessRunner) getProcess(ctx context.Context, timeout <-chan time.Time) (*sharedProcess, error) {
	for {
		r.mu.Lock()
		if r.closed {
			r.mu.Unlock()
			return nil, ErrSharedClosed
		}
		s := r.process
		if s == nil {
			var err error
			s, err = r.startLocked()
			if err == nil {
				r.process = s
			}
			r.mu.Unlock()
			return s, err
		}
		select {
		case <-s.stop:
			r.mu.Unlock()
			select {
			case <-ctx.Done():
				return nil, sharedContextError(ctx)
			case <-timeout:
				return nil, ErrTimeout
			case <-s.done:
			}
			r.mu.Lock()
			if r.process == s {
				r.process = nil
			}
			r.mu.Unlock()
		default:
			r.mu.Unlock()
			return s, nil
		}
	}
}

func (r *SharedProcessRunner) startLocked() (*sharedProcess, error) {
	if r.Command == "" {
		return nil, errors.New("piharness: SharedProcessRunner.Command is not set")
	}
	maxLine, maxStderr := r.MaxLineBytes, r.MaxStderrBytes
	if maxLine <= 0 {
		maxLine = DefaultMaxLineBytes
	}
	if maxStderr <= 0 {
		maxStderr = DefaultMaxStderrBytes
	}
	// Own the pipe endpoints instead of Cmd.StdoutPipe: Wait must not close a
	// pipe before its reader has consumed the last release acknowledgement.
	var pipes []*os.File
	pipe := func() (*os.File, *os.File, error) {
		read, write, err := os.Pipe()
		if err == nil {
			pipes = append(pipes, read, write)
		}
		return read, write, err
	}
	success := false
	defer func() {
		if !success {
			for _, file := range pipes {
				_ = file.Close()
			}
		}
	}()
	inRead, inWrite, err := pipe()
	if err != nil {
		return nil, fmt.Errorf("piharness: shared stdin pipe: %w", err)
	}
	outRead, outWrite, err := pipe()
	if err != nil {
		return nil, fmt.Errorf("piharness: shared stdout pipe: %w", err)
	}
	errRead, errWrite, err := pipe()
	if err != nil {
		return nil, fmt.Errorf("piharness: shared stderr pipe: %w", err)
	}
	cmd := exec.Command(r.Command, r.Args...)
	cmd.Dir, cmd.Env = r.WorkingDirectory, r.Environment
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Stdin, cmd.Stdout, cmd.Stderr = inRead, outWrite, errWrite
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("piharness: start shared process: %w", err)
	}
	success = true
	_ = inRead.Close()
	_ = outWrite.Close()
	_ = errWrite.Close()
	s := &sharedProcess{
		cmd: cmd, stdin: inWrite, stdout: outRead, stderr: errRead,
		stderrBuf: &boundedBuffer{max: maxStderr},
		commands:  make(chan []byte, sharedCommandQueueSize),
		attempts:  make(map[sharedAttemptKey]*sharedAttempt),
		ready:     make(chan struct{}), stop: make(chan struct{}), done: make(chan struct{}),
		grace: r.grace(),
	}
	s.workers.Add(4)
	go s.readStdout(maxLine)
	go s.readStderr()
	go s.writeCommands()
	go func() {
		defer s.workers.Done()
		// A short cleanup grace must not become an equally short cold-start
		// budget for the executable. The Run's walltime still applies.
		startupGrace := s.grace
		if startupGrace < DefaultGrace {
			startupGrace = DefaultGrace
		}
		timer := time.NewTimer(startupGrace)
		defer timer.Stop()
		select {
		case <-s.ready:
		case <-s.stop:
		case <-timer.C:
			s.mu.Lock()
			if !s.isReady {
				s.abortLocked(ErrReadyTimeout)
			}
			s.mu.Unlock()
		}
	}()
	go func() {
		// Termination is decided before Wait reaps the PID. This prevents
		// delayed signals from targeting a subsequently reused process group.
		<-s.stop
		drained := make(chan struct{})
		go func() {
			s.workers.Wait()
			close(drained)
		}()
		timer := time.NewTimer(s.grace)
		select {
		case <-drained:
		case <-timer.C:
		}
		timer.Stop()
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		_ = s.stdout.Close()
		_ = s.stderr.Close()
		_ = cmd.Wait()
		<-drained
		r.mu.Lock()
		if r.process == s {
			r.lastStderr = s.stderrBuf.bytes()
		}
		r.mu.Unlock()
		close(s.done)
	}()
	return s, nil
}

func (s *sharedProcess) enqueueLocked(data []byte) error {
	if s.stopped {
		return ErrSharedClosed
	}
	select {
	case s.commands <- append(data, '\n'):
		return nil
	default:
		return ErrCommandQueueOverflow
	}
}

func (s *sharedProcess) writeCommands() {
	defer s.workers.Done()
	for {
		select {
		case <-s.stop:
			return
		case data := <-s.commands:
			if n, err := s.stdin.Write(data); err != nil || n != len(data) {
				if err == nil {
					err = io.ErrShortWrite
				}
				s.abort(fmt.Errorf("%w: write shared command: %v", ErrPrematureExit, err))
				return
			}
		}
	}
}

func (s *sharedProcess) readStdout(maxLine int) {
	defer s.workers.Done()
	scanner := bufio.NewScanner(s.stdout)
	initial := maxLine
	if initial > 4096 {
		initial = 4096
	}
	scanner.Buffer(make([]byte, 0, initial), maxLine)
	for scanner.Scan() {
		if len(bytes.TrimSpace(scanner.Bytes())) == 0 {
			continue
		}
		if err := s.route(scanner.Bytes()); err != nil {
			s.abort(err)
			_, _ = io.Copy(io.Discard, s.stdout)
			return
		}
	}
	if err := scanner.Err(); err != nil {
		if errors.Is(err, bufio.ErrTooLong) {
			s.abort(ErrLineTooLong)
		} else {
			s.abort(fmt.Errorf("%w: read shared stdout: %v", ErrMalformedJSONL, err))
		}
		_, _ = io.Copy(io.Discard, s.stdout)
		return
	}
	s.mu.Lock()
	graceful := s.closing
	for _, a := range s.attempts {
		graceful = graceful && a.released
	}
	s.mu.Unlock()
	if graceful {
		s.abort(nil)
	} else {
		s.abort(ErrPrematureExit)
	}
}

func (s *sharedProcess) readStderr() {
	defer s.workers.Done()
	buffer := make([]byte, 4096)
	for {
		n, err := s.stderr.Read(buffer)
		if n > 0 {
			_, _ = s.stderrBuf.Write(buffer[:n])
			s.stderrBuf.mu.Lock()
			overflow := s.stderrBuf.overflow
			s.stderrBuf.mu.Unlock()
			if overflow {
				s.abort(ErrStderrOverflow)
				_, _ = io.Copy(io.Discard, s.stderr)
				return
			}
		}
		if err != nil {
			if !errors.Is(err, io.EOF) && !errors.Is(err, os.ErrClosed) {
				s.abort(fmt.Errorf("%w: read shared stderr: %v", ErrPrematureExit, err))
			}
			return
		}
	}
}

func (s *sharedProcess) route(line []byte) error {
	value, err := objectPayload(line)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrMalformedJSONL, err)
	}
	typ, err := requiredString(value, "type")
	if err != nil {
		return fmt.Errorf("%w: %v", ErrMalformedJSONL, err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped {
		return nil
	}
	switch typ {
	case "harness.ready":
		if s.isReady || validateExactKeys(value, []string{"type"}) != nil {
			return fmt.Errorf("%w: invalid or repeated ready", ErrHarnessProtocol)
		}
		s.isReady = true
		close(s.ready)
		return nil
	case "harness.protocol_error":
		if validateExactKeys(value, []string{"type", "error"}) != nil ||
			requireNonBlankString(value, "error") != nil {
			return fmt.Errorf("%w: invalid protocol_error control", ErrHarnessProtocol)
		}
		message, _ := requiredString(value, "error")
		return fmt.Errorf("%w: %s", ErrHarnessProtocol, message)
	}
	if !s.isReady {
		return fmt.Errorf("%w: record before ready handshake", ErrHarnessProtocol)
	}
	attemptID, err := requiredString(value, "attempt_id")
	if err != nil {
		return fmt.Errorf("%w: %v", ErrAttemptIDMismatch, err)
	}
	runID, err := requiredString(value, "run_id")
	if err != nil {
		return fmt.Errorf("%w: %v", ErrRunIDMismatch, err)
	}
	a := s.attempts[sharedAttemptKey{runID, attemptID}]
	if a == nil {
		for key := range s.attempts {
			if key.attemptID == attemptID {
				return fmt.Errorf("%w: unknown tuple (%q, %q)", ErrRunIDMismatch, runID, attemptID)
			}
		}
	}
	if a == nil || a.released {
		return fmt.Errorf("%w: unknown or released tuple (%q, %q)", ErrAttemptIDMismatch, runID, attemptID)
	}
	if typ == "harness.released" {
		if validateExactKeys(value, []string{"type", "run_id", "attempt_id"}) != nil {
			return fmt.Errorf("%w: invalid release control", ErrHarnessProtocol)
		}
		if !a.decoder.TerminalSeen() {
			return ErrMissingTerminal
		}
		a.released = true
		close(a.release)
		return nil
	}
	e, err := a.decoder.Decode(line)
	if err != nil {
		return err
	}
	if isTerminal(e.Type) {
		a.terminal = e
	}
	select {
	case a.events <- *e:
		return nil
	default:
		return fmt.Errorf("%w: %s", ErrEventQueueOverflow, attemptID)
	}
}

func (s *sharedProcess) watch(ctx context.Context, timeout <-chan time.Time, a *sharedAttempt) {
	defer close(a.watchDone)
	var reason error
	select {
	case <-a.release:
		return
	case <-s.stop:
		return
	case <-ctx.Done():
		reason = sharedContextError(ctx)
	case <-timeout:
		reason = ErrTimeout
	}
	s.mu.Lock()
	if a.released || s.stopped {
		s.mu.Unlock()
		return
	}
	a.cancelErr = reason
	data, _ := json.Marshal(struct {
		Type      string `json:"type"`
		RunID     string `json:"run_id"`
		AttemptID string `json:"attempt_id"`
	}{"cancel", a.runID, a.attemptID})
	err := s.enqueueLocked(data)
	s.mu.Unlock()
	if err != nil {
		s.abort(err)
		return
	}
	timer := time.NewTimer(s.grace)
	defer timer.Stop()
	select {
	case <-a.release:
	case <-s.stop:
	case <-timer.C:
		s.expireCancellation(a)
	}
}

func (s *sharedProcess) expireCancellation(a *sharedAttempt) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !a.released {
		s.abortLocked(ErrCancellationGrace)
	}
}

func (s *sharedProcess) abort(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.abortLocked(err)
}

func (s *sharedProcess) abortLocked(err error) {
	if s.stopped {
		return
	}
	s.stopped, s.err = true, err
	// Give the host a bounded opportunity to await its resource cleanup,
	// including detached MCP children outside our process group. Keep output
	// pipes draining; closing stdin unblocks any stuck command writer.
	_ = syscall.Kill(-s.cmd.Process.Pid, syscall.SIGTERM)
	_ = s.stdin.Close()
	close(s.stop)
}

func (s *sharedProcess) failure() error {
	<-s.done
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return s.err
	}
	return ErrSharedClosed
}

func (s *sharedProcess) result(a *sharedAttempt) error {
	s.mu.Lock()
	fatal, cancelErr, terminal := s.err, a.cancelErr, a.terminal
	s.mu.Unlock()
	if fatal != nil {
		return s.attemptFailure(a)
	}
	if cancelErr != nil {
		return cancelErr
	}
	if terminal == nil {
		return ErrMissingTerminal
	}
	if terminal.Type == EventFailed {
		var payload FailedPayload
		_ = json.Unmarshal(terminal.Payload, &payload)
		return &HarnessFailure{Message: payload.Error, Event: *terminal}
	}
	return nil
}

func (s *sharedProcess) attemptFailure(a *sharedAttempt) error {
	fatal := s.failure()
	s.mu.Lock()
	cancelErr := a.cancelErr
	s.mu.Unlock()
	if cancelErr != nil {
		return errors.Join(cancelErr, fatal)
	}
	return fatal
}

// Close prevents new work, requests shutdown and awaits bounded cleanup. It is
// idempotent and safe to call concurrently with Run or another Close.
func (r *SharedProcessRunner) Close() error {
	r.mu.Lock()
	if r.closeDone != nil {
		done := r.closeDone
		r.mu.Unlock()
		<-done
		r.mu.Lock()
		defer r.mu.Unlock()
		return r.closeErr
	}
	r.closed = true
	r.closeDone = make(chan struct{})
	s := r.process
	r.mu.Unlock()
	var closeErr error
	if s != nil {
		s.mu.Lock()
		s.closing = true
		s.mu.Unlock()
		timer := time.NewTimer(s.grace)
		defer timer.Stop()
		select {
		case <-s.stop:
		case <-timer.C:
			s.abort(ErrShutdownTimeout)
		case <-s.ready:
			s.mu.Lock()
			err := s.enqueueLocked([]byte(`{"type":"shutdown"}`))
			s.mu.Unlock()
			if err != nil {
				s.abort(err)
			}
		}
		select {
		case <-s.done:
		case <-timer.C:
			s.abort(ErrShutdownTimeout)
			<-s.done
		}
		s.mu.Lock()
		closeErr = s.err
		s.mu.Unlock()
	}
	r.mu.Lock()
	r.closeErr = closeErr
	close(r.closeDone)
	r.mu.Unlock()
	return closeErr
}
