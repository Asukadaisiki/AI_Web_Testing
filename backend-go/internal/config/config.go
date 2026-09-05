package config

import (
	"os"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

type Config struct {
	Address          string
	LLMBaseURL       string
	LLMAPIKey        string
	LLMModel         string
	AgentMaxTurns    int
	DefaultActorID   int64
	DatabaseURL      string
	BrowserWorkerURL string
}

func Load() Config {
	_ = godotenv.Load("../browser-worker/.env", "browser-worker/.env", ".env")

	address := strings.TrimSpace(os.Getenv("AGENTSERVICE_HTTP_ADDR"))
	if address == "" {
		address = "127.0.0.1:8081"
	}
	maxTurns := 12
	if parsed, err := strconv.Atoi(os.Getenv("AGENTSERVICE_MAX_TURNS")); err == nil && parsed > 0 {
		maxTurns = parsed
	}
	browserWorkerURL := strings.TrimRight(strings.TrimSpace(os.Getenv("BROWSER_WORKER_URL")), "/")
	if browserWorkerURL == "" {
		browserWorkerURL = "http://127.0.0.1:8000/api/v1"
	}
	return Config{
		Address:          address,
		LLMBaseURL:       strings.TrimRight(os.Getenv("AI_PLANNING_BASE_URL"), "/"),
		LLMAPIKey:        os.Getenv("AI_PLANNING_API_KEY"),
		LLMModel:         os.Getenv("AI_PLANNING_MODEL"),
		AgentMaxTurns:    maxTurns,
		DefaultActorID:   int64(positiveIntOrDefault("DEFAULT_ACTOR_USER_ID", 1)),
		DatabaseURL:      normalizeDatabaseURL(os.Getenv("DATABASE_URL")),
		BrowserWorkerURL: browserWorkerURL,
	}
}

func positiveIntOrDefault(name string, fallback int) int {
	if value, err := strconv.Atoi(os.Getenv(name)); err == nil && value > 0 {
		return value
	}
	return fallback
}

func normalizeDatabaseURL(value string) string {
	value = strings.TrimSpace(value)
	value = strings.Replace(value, "postgresql+psycopg://", "postgres://", 1)
	value = strings.Replace(value, "postgresql://", "postgres://", 1)
	return value
}
