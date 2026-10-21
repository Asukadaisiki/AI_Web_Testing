package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/config"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/event"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/model"
)

func TestSelectModelUsesScriptedFixtureWhenConfigured(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fixture.json")
	if err := os.WriteFile(path, []byte(`{"steps":[{"final":"done"}]}`), 0o600); err != nil {
		t.Fatalf("write scripted fixture: %v", err)
	}
	selected, err := selectModel(config.Model{ScriptPath: path})
	if err != nil {
		t.Fatalf("select model: %v", err)
	}
	if _, ok := selected.(*model.Scripted); !ok {
		t.Fatalf("model = %T, want scripted", selected)
	}
}

func TestBoundedRunnerHonorsConcurrencyLimit(t *testing.T) {
	runner := &blockingRunner{started: make(chan struct{}, 2), release: make(chan struct{})}
	bounded, err := newBoundedRunner(runner, 1)
	if err != nil {
		t.Fatalf("create bounded runner: %v", err)
	}
	done := make(chan error, 2)
	for range 2 {
		go func() { done <- bounded.Run(context.Background(), event.RunCommand{RunID: "run"}) }()
	}
	select {
	case <-runner.started:
	case <-time.After(time.Second):
		t.Fatal("first run did not start")
	}
	select {
	case <-runner.started:
		t.Fatal("second run started before concurrency slot was released")
	case <-time.After(25 * time.Millisecond):
	}
	close(runner.release)
	for range 2 {
		if err := <-done; err != nil {
			t.Fatalf("run: %v", err)
		}
	}
}

type blockingRunner struct {
	started chan struct{}
	release chan struct{}
}

func (r *blockingRunner) Run(context.Context, event.RunCommand) error {
	r.started <- struct{}{}
	<-r.release
	return nil
}
