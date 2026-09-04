package config

import (
	"os"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

type Config struct {
	Address       string
	LLMBaseURL    string
	LLMAPIKey     string
	LLMModel      string
	AgentMaxSteps int
	DatabaseURL   string
	PythonAPIURL  string
}

func Load() Config {
	_ = godotenv.Load("../backend/.env", "backend/.env", ".env")

	address := strings.TrimSpace(os.Getenv("AGENTCORE_HTTP_ADDR"))
	if address == "" {
		address = "127.0.0.1:8081"
	}
	maxSteps := 12
	if parsed, err := strconv.Atoi(os.Getenv("AGENTCORE_MAX_STEPS")); err == nil && parsed > 0 {
		maxSteps = parsed
	}
	pythonAPIURL := strings.TrimRight(strings.TrimSpace(os.Getenv("PYTHON_API_URL")), "/")
	if pythonAPIURL == "" {
		pythonAPIURL = "http://127.0.0.1:8000/api/v1"
	}
	return Config{
		Address:       address,
		LLMBaseURL:    strings.TrimRight(os.Getenv("AI_PLANNING_BASE_URL"), "/"),
		LLMAPIKey:     os.Getenv("AI_PLANNING_API_KEY"),
		LLMModel:      os.Getenv("AI_PLANNING_MODEL"),
		AgentMaxSteps: maxSteps,
		DatabaseURL:   normalizeDatabaseURL(os.Getenv("DATABASE_URL")),
		PythonAPIURL:  pythonAPIURL,
	}
}

func normalizeDatabaseURL(value string) string {
	value = strings.TrimSpace(value)
	value = strings.Replace(value, "postgresql+psycopg://", "postgres://", 1)
	value = strings.Replace(value, "postgresql://", "postgres://", 1)
	return value
}
