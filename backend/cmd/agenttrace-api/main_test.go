package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestStartupRejectsPartialBridgeBeforeOpeningDependencies(t *testing.T) {
	t.Setenv("AGENTTRACE_POSTGRES_DSN", "invalid-dsn")
	t.Setenv("AGENTTRACE_KAFKA_BROKERS", "localhost:9092")
	t.Setenv("AGENTTRACE_BRIDGE_TOKEN", "")
	t.Setenv("AGENTTRACE_PROJECT_ID", "")
	t.Setenv("AGENTTRACE_CONVERSATION_ID", "")
	err := run()
	if err == nil || !strings.Contains(err.Error(), "agenttrace-api configuration") ||
		!strings.Contains(err.Error(), "AGENTTRACE_BRIDGE_TOKEN") {
		t.Fatalf("partial bridge reached dependency startup: %v", err)
	}
	if strings.Contains(err.Error(), "test-startup-token") {
		t.Fatal("startup error leaked bridge token")
	}
}

func TestWorkerHealthCheckUsesHealthEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			t.Fatalf("path = %q, want /health", r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	if err := workerHealthCheck(server.URL)(context.Background()); err != nil {
		t.Fatalf("worker health check: %v", err)
	}
}

func TestWorkerHealthCheckRejectsUnavailableWorker(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	if err := workerHealthCheck(server.URL)(context.Background()); err == nil {
		t.Fatal("expected unavailable worker health check to fail")
	}
}
