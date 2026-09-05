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
	DatabaseURL      string
	BrowserWorkerURL string
	SessionSecret    string
	SessionCookie    string
	SessionMaxAge    int
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
		DatabaseURL:      normalizeDatabaseURL(os.Getenv("DATABASE_URL")),
		BrowserWorkerURL: browserWorkerURL,
		SessionSecret:    os.Getenv("AUTH_SESSION_SECRET"),
		SessionCookie:    envOrDefault("AUTH_SESSION_COOKIE_NAME", "session"),
		SessionMaxAge:    positiveIntOrDefault("AUTH_SESSION_MAX_AGE_SECONDS", 43200),
	}
}

func envOrDefault(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
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
