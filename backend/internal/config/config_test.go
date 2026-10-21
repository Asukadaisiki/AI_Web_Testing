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
