package config

import "testing"

func TestManagementRequiresServiceAuthenticationAndAllowsDynamicScope(t *testing.T) {
	t.Setenv("AGENTTRACE_POSTGRES_DSN", "postgres://localhost/agenttrace")
	t.Setenv("AGENTTRACE_KAFKA_BROKERS", "localhost:9092")
	t.Setenv("AGENTTRACE_BROWSER_PROVIDER_TOKEN", "provider-token")
	t.Setenv("AGENTTRACE_MANAGEMENT_ENABLED", "true")
	t.Setenv("AGENTTRACE_PROJECT_ID", "")
	t.Setenv("AGENTTRACE_CONVERSATION_ID", "")
	t.Setenv("AGENTTRACE_BRIDGE_TOKEN", "")
	if _, err := Load(); err == nil {
		t.Fatal("unauthenticated management accepted")
	}
	t.Setenv("AGENTTRACE_BRIDGE_TOKEN", "service-token")
	_, err := Load()
	if err != nil {
		t.Fatalf("dynamic management rejected: %v", err)
	}
}
