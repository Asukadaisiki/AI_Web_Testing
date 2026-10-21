// Package config loads the v3 process configuration from the environment.
package config

import (
	"fmt"
	"net/url"
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
	Model              Model
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

// Model contains runtime-only OpenAI-compatible provider configuration.
// APIKey is intentionally loaded only from the environment and is never logged.
type Model struct {
	BaseURL         string
	Name            string
	ReasoningEffort string
	APIKey          string
	ScriptPath      string
}

// Exporter contains only the dependencies used by the optional Langfuse process.
type Exporter struct {
	Enabled  bool
	Postgres Postgres
	Kafka    Kafka
	Langfuse Langfuse
}

// Langfuse contains exporter-only provider settings.
type Langfuse struct {
	BaseURL      string
	PublicKey    string
	SecretKey    string
	GroupID      string
	SecretFields []string
	MaxAttempts  int
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
			CommandTopic: envOr("AGENTTRACE_KAFKA_COMMAND_TOPIC", "agenttrace.run.commands.v1"),
			EventTopic:   envOr("AGENTTRACE_KAFKA_EVENT_TOPIC", "agenttrace.run.events.v1"),
			DLQTopic:     envOr("AGENTTRACE_KAFKA_DLQ_TOPIC", "agenttrace.run.dlq.v1"),
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
		Model: Model{
			BaseURL:         envOr("AGENTTRACE_MODEL_BASE_URL", "https://api.unself.cn/v1"),
			Name:            envOr("AGENTTRACE_MODEL_NAME", "glm-5.3-flash"),
			ReasoningEffort: envOr("AGENTTRACE_MODEL_REASONING_EFFORT", "high"),
			APIKey:          os.Getenv("AGENTTRACE_MODEL_API_KEY"),
			ScriptPath:      strings.TrimSpace(os.Getenv("AGENTTRACE_MODEL_SCRIPT")),
		},
	}, nil
}

// LoadExporter validates no dependencies while the optional process is disabled.
func LoadExporter() (Exporter, error) {
	enabled, err := boolEnv("AGENTTRACE_LANGFUSE_ENABLED", false)
	if err != nil {
		return Exporter{}, err
	}
	if !enabled {
		return Exporter{Enabled: false}, nil
	}

	postgresDSN := strings.TrimSpace(os.Getenv("AGENTTRACE_POSTGRES_DSN"))
	brokers := commaSeparated(os.Getenv("AGENTTRACE_KAFKA_BROKERS"))
	if postgresDSN == "" || len(brokers) == 0 {
		return Exporter{}, fmt.Errorf(
			"missing required exporter configuration: set AGENTTRACE_POSTGRES_DSN and AGENTTRACE_KAFKA_BROKERS",
		)
	}
	baseURL := strings.TrimSpace(os.Getenv("AGENTTRACE_LANGFUSE_BASE_URL"))
	parsedURL, err := url.Parse(baseURL)
	if err != nil || parsedURL.Scheme == "" || parsedURL.Host == "" ||
		(parsedURL.Scheme != "http" && parsedURL.Scheme != "https") ||
		parsedURL.User != nil {
		return Exporter{}, fmt.Errorf("AGENTTRACE_LANGFUSE_BASE_URL must be an absolute HTTP URL without credentials")
	}
	publicKey := strings.TrimSpace(os.Getenv("AGENTTRACE_LANGFUSE_PUBLIC_KEY"))
	secretKey := strings.TrimSpace(os.Getenv("AGENTTRACE_LANGFUSE_SECRET_KEY"))
	if publicKey == "" || secretKey == "" {
		return Exporter{}, fmt.Errorf(
			"missing required Langfuse credentials: set AGENTTRACE_LANGFUSE_PUBLIC_KEY and AGENTTRACE_LANGFUSE_SECRET_KEY",
		)
	}
	maxAttempts, err := positiveIntEnv("AGENTTRACE_LANGFUSE_MAX_ATTEMPTS", 3)
	if err != nil {
		return Exporter{}, err
	}
	secretFields := commaSeparated(envOr(
		"AGENTTRACE_LANGFUSE_REDACT_FIELDS",
		"authorization,cookie,password,token,api_key,secret",
	))
	return Exporter{
		Enabled:  true,
		Postgres: Postgres{DSN: postgresDSN},
		Kafka: Kafka{
			Brokers:    brokers,
			EventTopic: "agenttrace.run.events.v1",
			DLQTopic:   "agenttrace.run.dlq.v1",
		},
		Langfuse: Langfuse{
			BaseURL:      baseURL,
			PublicKey:    publicKey,
			SecretKey:    secretKey,
			GroupID:      envOr("AGENTTRACE_LANGFUSE_GROUP_ID", "agenttrace-langfuse-exporter"),
			SecretFields: secretFields,
			MaxAttempts:  maxAttempts,
		},
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

func boolEnv(key string, fallback bool) (bool, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return false, fmt.Errorf("%s must be a boolean: %w", key, err)
	}
	return parsed, nil
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
