package redis

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/live"
	goredis "github.com/redis/go-redis/v9"
)

var errStopRedisCommand = errors.New("stop redis command")

func TestAppendUsesOneServerSideCommandForAddAndExpiry(t *testing.T) {
	raw := goredis.NewClient(&goredis.Options{Addr: "127.0.0.1:1"})
	t.Cleanup(func() { _ = raw.Close() })
	hook := &recordingHook{}
	raw.AddHook(hook)

	err := NewClient(raw).Append(context.Background(), "run-atomic", live.LiveEvent{
		Sequence: 9, Type: "span.completed", Data: json.RawMessage(`{"span":"plan"}`),
	})
	if !errors.Is(err, errStopRedisCommand) {
		t.Fatalf("append error = %v, want command interception error", err)
	}

	commands := hook.Commands()
	if len(commands) != 1 {
		t.Fatalf("server commands = %v, want one atomic command", commands)
	}
	command := commands[0]
	if command[0] != "eval" {
		t.Fatalf("server command = %v, want EVAL for atomic append and expiry", command)
	}
	if command[2] != 1 || command[3] != liveKey("run-atomic") {
		t.Fatalf("EVAL keys = %v, want one live stream key", command[2:4])
	}
	script, ok := command[1].(string)
	if !ok || !strings.Contains(script, "XADD") ||
		!strings.Contains(script, "MAXLEN") ||
		!strings.Contains(script, `"~"`) ||
		!strings.Contains(script, "EXPIRE") {
		t.Fatalf("EVAL script = %q, want XADD MAXLEN ~ and EXPIRE", script)
	}
	if command[4] != live.MaxStreamLength || command[6] != int64(live.StreamTTL.Seconds()) {
		t.Fatalf("EVAL args = %v, want max length %d and expiry %d", command[4:],
			live.MaxStreamLength, int64(live.StreamTTL.Seconds()))
	}
}

type recordingHook struct {
	mu       sync.Mutex
	commands [][]any
}

func (h *recordingHook) DialHook(next goredis.DialHook) goredis.DialHook {
	return next
}

func (h *recordingHook) ProcessHook(goredis.ProcessHook) goredis.ProcessHook {
	return func(_ context.Context, command goredis.Cmder) error {
		h.mu.Lock()
		h.commands = append(h.commands, append([]any(nil), command.Args()...))
		h.mu.Unlock()
		return errStopRedisCommand
	}
}

func (h *recordingHook) ProcessPipelineHook(next goredis.ProcessPipelineHook) goredis.ProcessPipelineHook {
	return next
}

func (h *recordingHook) Commands() [][]any {
	h.mu.Lock()
	defer h.mu.Unlock()

	commands := make([][]any, len(h.commands))
	for index, command := range h.commands {
		commands[index] = append([]any(nil), command...)
	}
	return commands
}

var _ goredis.Hook = (*recordingHook)(nil)
