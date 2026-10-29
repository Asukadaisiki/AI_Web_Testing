package config

import "testing"

func TestRetiredManagementSwitchCannotDisableAuthentication(t *testing.T) {
	t.Setenv("AGENTTRACE_POSTGRES_DSN", "postgres://localhost/agenttrace")
	t.Setenv("AGENTTRACE_KAFKA_BROKERS", "localhost:9092")
	t.Setenv("AGENTTRACE_MANAGEMENT_ENABLED", "false")
	t.Setenv("AGENTTRACE_BRIDGE_TOKEN", "")
	t.Setenv("AGENTTRACE_BROWSER_PROVIDER_TOKEN", "browser-token")
	if _, err := Load(); err == nil {
		t.Fatal("retired switch disabled authentication")
	}
}

func TestBrowserProviderAuthenticationRequiredAtStartup(t *testing.T) {
	t.Setenv("AGENTTRACE_POSTGRES_DSN", "postgres://localhost/agenttrace")
	t.Setenv("AGENTTRACE_KAFKA_BROKERS", "localhost:9092")
	t.Setenv("AGENTTRACE_MANAGEMENT_ENABLED", "")
	t.Setenv("AGENTTRACE_BRIDGE_TOKEN", "service-token")
	t.Setenv("AGENTTRACE_BROWSER_PROVIDER_TOKEN", "")
	if _, err := Load(); err == nil {
		t.Fatal("startup accepted missing browser provider authentication")
	}
}
