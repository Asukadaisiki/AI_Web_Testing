package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/agentprofile"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/config"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/event"
)

func TestStartupRejectsPartialBridgeBeforeOpeningDependencies(t *testing.T) {
	t.Setenv("AGENTTRACE_POSTGRES_DSN", "invalid-dsn")
	t.Setenv("AGENTTRACE_KAFKA_BROKERS", "localhost:9092")
	t.Setenv("AGENTTRACE_BRIDGE_TOKEN", "test-startup-token")
	t.Setenv("AGENTTRACE_PROJECT_ID", "")
	t.Setenv("AGENTTRACE_CONVERSATION_ID", "")
	err := run()
	if err == nil || !strings.Contains(err.Error(), "agenttrace-api configuration") ||
		!strings.Contains(err.Error(), "must be configured together") {
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

func TestConfiguredModelUsesServerModeIdentity(t *testing.T) {
	profile, err := agentprofile.LoadBrowserEval()
	if err != nil {
		t.Fatalf("load browser profile: %v", err)
	}
	reasoningEffort := profile.Model.ReasoningEffort
	for _, test := range []struct {
		name string
		cfg  config.Model
		want event.ModelRef
	}{
		{
			name: "scripted",
			cfg:  config.Model{Provider: "scripted", APIKey: "irrelevant", Name: "ignored", ReasoningEffort: reasoningEffort},
			want: event.ModelRef{Provider: "scripted", Name: "browser-eval-fixture", ReasoningEffort: reasoningEffort},
		},
		{
			name: "external",
			cfg:  config.Model{Provider: "openai-compatible", APIKey: "", Name: "glm-5.3-flash", ReasoningEffort: "low"},
			want: event.ModelRef{Provider: "openai-compatible", Name: "glm-5.3-flash", ReasoningEffort: "low"},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := configuredModel(test.cfg, profile); got != test.want {
				t.Fatalf("configured model = %#v, want %#v", got, test.want)
			}
		})
	}
}
