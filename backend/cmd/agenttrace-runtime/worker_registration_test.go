package main

import (
	"testing"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/config"
)

func TestRuntimeWorkerRegistrationUsesTrustedConfig(t *testing.T) {
	t.Setenv("AGENTTRACE_RUNTIME_WORKER_ID", "runtime-1")
	t.Setenv("AGENTTRACE_BROWSER_WORKER_ID", "browser-1")
	t.Setenv("AGENTTRACE_WORKER_PROJECT_ID", "p")
	t.Setenv("AGENTTRACE_BROWSER_WORKER_CAPACITY", "3")
	cfg := config.Config{WorkerURL: "http://deployment:8100", RuntimeConcurrency: 2, BrowserProviderToken: "test-provider"}
	runtimeNode, browserNode, err := configuredWorkers(cfg)
	if err != nil || runtimeNode.ID != "runtime-1" || runtimeNode.Capacity != 2 ||
		browserNode.ID != "browser-1" || browserNode.Capacity != 3 || browserNode.Endpoint != cfg.WorkerURL ||
		runtimeNode.ProjectID != "p" || browserNode.ProjectID != "p" {
		t.Fatalf("registrations: %#v %#v %v", runtimeNode, browserNode, err)
	}
	t.Setenv("AGENTTRACE_BROWSER_WORKER_CAPACITY", "0")
	if _, _, err := configuredWorkers(cfg); err == nil {
		t.Fatal("invalid browser capacity accepted")
	}
	t.Setenv("AGENTTRACE_BROWSER_WORKER_CAPACITY", "1")
	cfg.BrowserProviderToken = ""
	if _, _, err := configuredWorkers(cfg); err == nil {
		t.Fatal("management startup accepted missing browser provider credential")
	}
}
