package config

import (
	"strings"
	"testing"
	"time"
)

func TestLoadBridgeConfiguration(t *testing.T) {
	t.Setenv("AGENTTRACE_POSTGRES_DSN", "postgres://localhost/agenttrace")
	t.Setenv("AGENTTRACE_KAFKA_BROKERS", "localhost:9092")
	for mask := 0; mask < 8; mask++ {
		t.Run(string(rune('0'+mask)), func(t *testing.T) {
			for index, key := range []string{"AGENTTRACE_BRIDGE_TOKEN", "AGENTTRACE_PROJECT_ID", "AGENTTRACE_CONVERSATION_ID"} {
				value := ""
				if mask&(1<<index) != 0 {
					value = []string{"test-bridge-token", "project_1", "conv_1"}[index]
				}
				t.Setenv(key, value)
			}
			cfg, err := Load()
			if mask != 0 && mask != 7 {
				if err == nil {
					t.Fatal("partial bridge configuration accepted")
				}
				if strings.Contains(err.Error(), "test-bridge-token") {
					t.Fatal("configuration error leaked token")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			for index, value := range []string{cfg.BridgeToken, cfg.BridgeProjectID, cfg.BridgeConversationID} {
				want := ""
				if mask == 7 {
					want = []string{"test-bridge-token", "project_1", "conv_1"}[index]
				}
				if value != want {
					t.Errorf("bridge field %d was not loaded", index)
				}
			}
		})
	}
}

func TestLoadExporterProjectFilter(t *testing.T) {
	setValidExporterEnvironment(t)
	for _, value := range []string{"", " project_1 "} {
		t.Setenv("AGENTTRACE_EXPORT_PROJECT_ID", value)
		cfg, err := LoadExporter()
		if err != nil {
			t.Fatal(err)
		}
		if cfg.ExportProjectID != strings.TrimSpace(value) {
			t.Fatal("export project scope is missing or not loaded")
		}
	}
}

func TestLoadRequiresPostgresAndKafka(t *testing.T) {
	t.Setenv("AGENTTRACE_POSTGRES_DSN", "")
	t.Setenv("AGENTTRACE_KAFKA_BROKERS", "")
	if _, err := Load(); err == nil {
		t.Fatal("expected missing dependency configuration error")
	}
}

func TestLoadAllowsRedisToBeDisabled(t *testing.T) {
	t.Setenv("AGENTTRACE_POSTGRES_DSN", "postgres://agenttrace:agenttrace@localhost:5432/agenttrace?sslmode=disable")
	t.Setenv("AGENTTRACE_KAFKA_BROKERS", "localhost:9092")
	t.Setenv("AGENTTRACE_REDIS_ADDR", "")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Redis.Enabled {
		t.Fatal("redis must be optional")
	}
}

func TestLoadUsesRunTopicDefaults(t *testing.T) {
	t.Setenv("AGENTTRACE_POSTGRES_DSN", "postgres://agenttrace:agenttrace@localhost:5432/agenttrace?sslmode=disable")
	t.Setenv("AGENTTRACE_KAFKA_BROKERS", "localhost:9092")
	t.Setenv("AGENTTRACE_KAFKA_COMMAND_TOPIC", "")
	t.Setenv("AGENTTRACE_KAFKA_EVENT_TOPIC", "")
	t.Setenv("AGENTTRACE_KAFKA_DLQ_TOPIC", "")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}

	if cfg.Kafka.CommandTopic != "agenttrace.run.commands.v1" {
		t.Fatalf("command topic = %q, want %q", cfg.Kafka.CommandTopic, "agenttrace.run.commands.v1")
	}
	if cfg.Kafka.EventTopic != "agenttrace.run.events.v1" {
		t.Fatalf("event topic = %q, want %q", cfg.Kafka.EventTopic, "agenttrace.run.events.v1")
	}
	if cfg.Kafka.DLQTopic != "agenttrace.run.dlq.v1" {
		t.Fatalf("DLQ topic = %q, want %q", cfg.Kafka.DLQTopic, "agenttrace.run.dlq.v1")
	}
}

func TestLoadUsesConfiguredOpenAICompatibleModelDefaults(t *testing.T) {
	t.Setenv("AGENTTRACE_POSTGRES_DSN", "postgres://agenttrace:agenttrace@localhost:5432/agenttrace?sslmode=disable")
	t.Setenv("AGENTTRACE_KAFKA_BROKERS", "localhost:9092")
	t.Setenv("AGENTTRACE_MODEL_BASE_URL", "")
	t.Setenv("AGENTTRACE_MODEL_NAME", "")
	t.Setenv("AGENTTRACE_MODEL_REASONING_EFFORT", "")
	t.Setenv("AGENTTRACE_MODEL_API_KEY", "runtime-only-secret")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Model.BaseURL != "https://api.unself.cn/v1" ||
		cfg.Model.Name != "glm-5.3-flash" ||
		cfg.Model.ReasoningEffort != "high" ||
		cfg.Model.APIKey != "runtime-only-secret" {
		t.Fatalf("model configuration = %#v", cfg.Model)
	}
}

func TestLoadModelProviderIsExplicitAndIndependentOfCredentials(t *testing.T) {
	t.Setenv("AGENTTRACE_POSTGRES_DSN", "postgres://localhost/agenttrace")
	t.Setenv("AGENTTRACE_KAFKA_BROKERS", "localhost:9092")
	t.Setenv("AGENTTRACE_MODEL_API_KEY", "")
	t.Setenv("AGENTTRACE_MODEL_PROVIDER", "")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Model.Provider != "openai-compatible" {
		t.Fatalf("provider = %q, want real model without API-side secrets", cfg.Model.Provider)
	}
	t.Setenv("AGENTTRACE_MODEL_PROVIDER", "scripted")
	cfg, err = Load()
	if err != nil || cfg.Model.Provider != "scripted" {
		t.Fatalf("explicit fixture provider = %q, error = %v", cfg.Model.Provider, err)
	}
	t.Setenv("AGENTTRACE_MODEL_PROVIDER", "typo")
	if _, err := Load(); err == nil {
		t.Fatal("invalid provider accepted")
	}
}

func TestLoadExporterDisabledRequiresNoDependenciesOrCredentials(t *testing.T) {
	t.Setenv("AGENTTRACE_LANGFUSE_ENABLED", "false")
	t.Setenv("AGENTTRACE_POSTGRES_DSN", "")
	t.Setenv("AGENTTRACE_KAFKA_BROKERS", "")
	t.Setenv("AGENTTRACE_LANGFUSE_BASE_URL", "not a URL")
	t.Setenv("AGENTTRACE_LANGFUSE_PUBLIC_KEY", "")
	t.Setenv("AGENTTRACE_LANGFUSE_SECRET_KEY", "")

	cfg, err := LoadExporter()
	if err != nil {
		t.Fatalf("load disabled exporter: %v", err)
	}
	if cfg.Enabled {
		t.Fatal("exporter must be disabled")
	}
}

func TestLoadExporterEnabledValidatesProviderConfiguration(t *testing.T) {
	setValidExporterEnvironment(t)

	for _, test := range []struct {
		name  string
		key   string
		value string
	}{
		{name: "base URL", key: "AGENTTRACE_LANGFUSE_BASE_URL", value: "file:///tmp/langfuse"},
		{name: "public key", key: "AGENTTRACE_LANGFUSE_PUBLIC_KEY", value: ""},
		{name: "secret key", key: "AGENTTRACE_LANGFUSE_SECRET_KEY", value: ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv(test.key, test.value)
			if _, err := LoadExporter(); err == nil {
				t.Fatalf("accepted invalid %s", test.name)
			}
		})
	}
}

func TestLoadExporterEnabledUsesExactTopicsAndRedactionFields(t *testing.T) {
	setValidExporterEnvironment(t)
	t.Setenv("AGENTTRACE_LANGFUSE_REDACT_FIELDS", "authorization, password,api_key")
	t.Setenv("AGENTTRACE_LANGFUSE_MAX_ATTEMPTS", "4")
	t.Setenv("AGENTTRACE_LANGFUSE_MAX_REQUEST_BYTES", "2048")

	cfg, err := LoadExporter()
	if err != nil {
		t.Fatalf("load enabled exporter: %v", err)
	}
	if !cfg.Enabled {
		t.Fatal("exporter must be enabled")
	}
	if cfg.Kafka.EventTopic != "agenttrace.run.events.v1" ||
		cfg.Kafka.DLQTopic != "agenttrace.run.dlq.v1" {
		t.Fatalf("exporter topics = (%q, %q)", cfg.Kafka.EventTopic, cfg.Kafka.DLQTopic)
	}
	if cfg.Langfuse.MaxAttempts != 4 {
		t.Fatalf("max attempts = %d, want 4", cfg.Langfuse.MaxAttempts)
	}
	if cfg.Langfuse.MaxRequestBytes != 2048 {
		t.Fatalf("max request bytes = %d, want 2048", cfg.Langfuse.MaxRequestBytes)
	}
	if len(cfg.Langfuse.SecretFields) != 3 {
		t.Fatalf("redaction fields = %#v", cfg.Langfuse.SecretFields)
	}
}

func TestLoadExporterDefaultsAndValidatesMaximumRequestBytes(t *testing.T) {
	setValidExporterEnvironment(t)
	t.Setenv("AGENTTRACE_LANGFUSE_MAX_REQUEST_BYTES", "")

	cfg, err := LoadExporter()
	if err != nil {
		t.Fatalf("load enabled exporter: %v", err)
	}
	if cfg.Langfuse.MaxRequestBytes != 1<<20 {
		t.Fatalf("default max request bytes = %d, want %d", cfg.Langfuse.MaxRequestBytes, 1<<20)
	}

	t.Setenv("AGENTTRACE_LANGFUSE_MAX_REQUEST_BYTES", "0")
	if _, err := LoadExporter(); err == nil {
		t.Fatal("accepted zero maximum request bytes")
	}
}

func TestLoadUsesPiHarnessDefaults(t *testing.T) {
	t.Setenv("AGENTTRACE_POSTGRES_DSN", "postgres://agenttrace:agenttrace@localhost:5432/agenttrace?sslmode=disable")
	t.Setenv("AGENTTRACE_KAFKA_BROKERS", "localhost:9092")
	t.Setenv("AGENTTRACE_PI_HARNESS_COMMAND", "")
	t.Setenv("AGENTTRACE_PI_HARNESS_ENTRY", "")
	t.Setenv("AGENTTRACE_PI_HARNESS_DIR", "")
	t.Setenv("AGENTTRACE_PI_HARNESS_TIMEOUT", "")
	t.Setenv("AGENTTRACE_PI_HARNESS_TERMINATION_GRACE", "")
	t.Setenv("AGENTTRACE_PI_HARNESS_MAX_LINE_BYTES", "")
	t.Setenv("AGENTTRACE_PI_HARNESS_MAX_STDERR_BYTES", "")
	t.Setenv("AGENTTRACE_MODEL_BASE_URL", "")
	t.Setenv("AGENTTRACE_MODEL_API_KEY", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.Harness.Command != "node" {
		t.Fatalf("harness command = %q, want node", cfg.Harness.Command)
	}
	if cfg.Harness.Entry != "/app/pi-browser-harness/dist/main.js" {
		t.Fatalf("harness entry = %q, want /app/pi-browser-harness/dist/main.js", cfg.Harness.Entry)
	}
	if cfg.Harness.Directory != "/app" {
		t.Fatalf("harness directory = %q, want /app", cfg.Harness.Directory)
	}
	if cfg.Harness.Timeout != 20*time.Minute {
		t.Fatalf("harness timeout = %v, want 20m", cfg.Harness.Timeout)
	}
	if cfg.Harness.TerminationGrace != 5*time.Second {
		t.Fatalf("harness termination grace = %v, want 5s", cfg.Harness.TerminationGrace)
	}
	if cfg.Harness.MaxLineBytes != 4194304 {
		t.Fatalf("harness max line bytes = %d, want 4194304", cfg.Harness.MaxLineBytes)
	}
	if cfg.Harness.MaxStderrBytes != 65536 {
		t.Fatalf("harness max stderr bytes = %d, want 65536", cfg.Harness.MaxStderrBytes)
	}
	if cfg.Harness.ProviderBaseURL != "https://api.unself.cn/v1" {
		t.Fatalf("harness provider base URL = %q, want https://api.unself.cn/v1", cfg.Harness.ProviderBaseURL)
	}
}

func setValidExporterEnvironment(t *testing.T) {
	t.Helper()
	t.Setenv("AGENTTRACE_LANGFUSE_ENABLED", "true")
	t.Setenv("AGENTTRACE_POSTGRES_DSN", "postgres://agenttrace:agenttrace@localhost:5432/agenttrace?sslmode=disable")
	t.Setenv("AGENTTRACE_KAFKA_BROKERS", "localhost:9092")
	t.Setenv("AGENTTRACE_LANGFUSE_BASE_URL", "https://langfuse.example")
	t.Setenv("AGENTTRACE_LANGFUSE_PUBLIC_KEY", "public-test")
	t.Setenv("AGENTTRACE_LANGFUSE_SECRET_KEY", "secret-test")
}
