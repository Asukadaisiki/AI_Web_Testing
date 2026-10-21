// Package config loads the v3 process configuration from the environment.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	HTTPAddr           string
	Postgres           Postgres
	Kafka              Kafka
	Redis              Redis
	WorkerURL          string
	ArtifactDir        string
	RuntimeConcurrency int
}

type Postgres struct{ DSN string }

type Kafka struct {
	Brokers      []string
	CommandTopic string
	EventTopic   string
	DLQTopic     string
	GroupID      string
}

type Redis struct {
	Enabled  bool
	Addr     string
	Password string
	DB       int
}

func Load() (Config, error) {
	postgresDSN := strings.TrimSpace(os.Getenv("AGENTTRACE_POSTGRES_DSN"))
	brokers := commaSeparated(os.Getenv("AGENTTRACE_KAFKA_BROKERS"))
	if postgresDSN == "" || len(brokers) == 0 {
		return Config{}, fmt.Errorf(
			"missing required configuration: set AGENTTRACE_POSTGRES_DSN and AGENTTRACE_KAFKA_BROKERS",
		)
	}

	redisDB, err := intEnv("AGENTTRACE_REDIS_DB", 0)
	if err != nil {
		return Config{}, err
	}
	runtimeConcurrency, err := positiveIntEnv("AGENTTRACE_RUNTIME_CONCURRENCY", 1)
	if err != nil {
		return Config{}, err
	}

	redisAddr := strings.TrimSpace(os.Getenv("AGENTTRACE_REDIS_ADDR"))
	return Config{
		HTTPAddr: envOr("AGENTTRACE_HTTP_ADDR", ":8080"),
		Postgres: Postgres{DSN: postgresDSN},
		Kafka: Kafka{
			Brokers:      brokers,
			CommandTopic: envOr("AGENTTRACE_KAFKA_COMMAND_TOPIC", "agenttrace.run-command.v1"),
			EventTopic:   envOr("AGENTTRACE_KAFKA_EVENT_TOPIC", "agenttrace.events.v1"),
			DLQTopic:     envOr("AGENTTRACE_KAFKA_DLQ_TOPIC", "agenttrace.dlq.v1"),
			GroupID:      envOr("AGENTTRACE_KAFKA_GROUP_ID", "agenttrace-runtime"),
		},
		Redis: Redis{
			Enabled:  redisAddr != "",
			Addr:     redisAddr,
			Password: os.Getenv("AGENTTRACE_REDIS_PASSWORD"),
			DB:       redisDB,
		},
		WorkerURL:          envOr("AGENTTRACE_WORKER_URL", "http://worker:8100"),
		ArtifactDir:        envOr("AGENTTRACE_ARTIFACT_DIR", "/artifacts"),
		RuntimeConcurrency: runtimeConcurrency,
	}, nil
}

func commaSeparated(value string) []string {
	var result []string
	for _, item := range strings.Split(value, ",") {
		if item = strings.TrimSpace(item); item != "" {
			result = append(result, item)
		}
	}
	return result
}

func intEnv(key string, fallback int) (int, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer: %w", key, err)
	}
	return parsed, nil
}

func positiveIntEnv(key string, fallback int) (int, error) {
	value, err := intEnv(key, fallback)
	if err != nil {
		return 0, err
	}
	if value < 1 {
		return 0, fmt.Errorf("%s must be at least 1", key)
	}
	return value, nil
}

func envOr(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}
