package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

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
