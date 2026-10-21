package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/agentprofile"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/config"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/event"
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

func TestConfiguredModelUsesServerModeIdentity(t *testing.T) {
	profile, err := agentprofile.LoadBrowserEval("https://worker.internal")
	if err != nil {
		t.Fatalf("load browser profile: %v", err)
	}
	for _, test := range []struct {
		name string
		cfg  config.Model
		want event.ModelRef
	}{
		{
			name: "scripted",
			cfg:  config.Model{ScriptPath: "testdata/scripted-model.json", Name: "ignored"},
			want: event.ModelRef{Provider: "scripted", Name: "browser-eval-fixture"},
		},
		{
			name: "external",
			cfg:  config.Model{Name: "glm-5.3-flash"},
			want: event.ModelRef{Provider: "openai-compatible", Name: "glm-5.3-flash"},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := configuredModel(test.cfg, profile); got != test.want {
				t.Fatalf("configured model = %#v, want %#v", got, test.want)
			}
		})
	}
}
