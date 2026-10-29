package piharness

import (
	"errors"
	"sync"
	"time"
)

const (
	DefaultMaxLineBytes   = 1 << 20
	DefaultMaxStderrBytes = 64 << 10
	DefaultGrace          = 5 * time.Second
)

var (
	ErrLineTooLong     = errors.New("piharness: stdout line exceeds maximum size")
	ErrStderrOverflow  = errors.New("piharness: stderr output exceeds maximum size")
	ErrPrematureExit   = errors.New("piharness: process exited before emitting a terminal event")
	ErrMissingTerminal = errors.New("piharness: process exited cleanly without a terminal event")
	ErrTimeout         = errors.New("piharness: run timed out")
)

type HarnessFailure struct {
	Message string
	Event   Event
}

func (h *HarnessFailure) Error() string {
	return "piharness: harness failed: " + h.Message
}

// boundedBuffer drains child diagnostics while retaining a fixed-size prefix.
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
	return append([]byte(nil), b.buf...)
}
