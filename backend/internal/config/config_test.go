package config

import "testing"

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
