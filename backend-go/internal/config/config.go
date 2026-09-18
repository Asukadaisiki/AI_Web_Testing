package config

import (
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

type Config struct {
	Address                     string
	LLMProvider                 string
	LLMBaseURL                  string
	LLMAPIKey                   string
	LLMModel                    string
	LLMThinkMode                bool
	LLMReasoningEffort          string
	AgentMaxTurns               int
	AgentMaxWallTimeSeconds     int
	AgentExploreReserveSeconds  int
	AgentMaxModelCalls          int
	AgentMaxTotalTokens         int64
	AgentMaxTranscriptBytes     int
	AgentMaxExplorePageCalls    int
	AgentMaxExploreFlowCalls    int
	AgentMaxRunExplorePageCalls int
	AgentMaxRunExploreFlowCalls int
	// AgentPerTurnTranscriptCompaction rewrites historical tool summaries
	// inside a turn to cap request size. That invalidates the provider's
	// prefix cache, so the default is false and growth is bounded by phase
	// boundaries instead.
	AgentPerTurnTranscriptCompaction bool
	DefaultActorID                   int64
	DatabaseURL                      string
	BrowserWorkerURL                 string
}

func Load() Config {
	_ = godotenv.Load("../browser-worker/.env", "browser-worker/.env", ".env")

	address := strings.TrimSpace(os.Getenv("AGENTSERVICE_HTTP_ADDR"))
	if address == "" {
		address = "127.0.0.1:8081"
	}
	// Default floor for agent turns. The harness scales the effective budget
	// with TaskPlan step count (BUG-195), so this value is a lower bound for
	// multi-step plans rather than a hard cap.
	maxTurns := 24
	if parsed, err := strconv.Atoi(os.Getenv("AGENTSERVICE_MAX_TURNS")); err == nil && parsed > 0 {
		maxTurns = parsed
	}
	browserWorkerURL := strings.TrimRight(strings.TrimSpace(os.Getenv("BROWSER_WORKER_URL")), "/")
	if browserWorkerURL == "" {
		browserWorkerURL = "http://127.0.0.1:8000/api/v1"
	}
	return Config{
		Address:                    address,
		LLMProvider:                strings.TrimSpace(os.Getenv("AI_PLANNING_PROVIDER")),
		LLMBaseURL:                 strings.TrimRight(os.Getenv("AI_PLANNING_BASE_URL"), "/"),
		LLMAPIKey:                  os.Getenv("AI_PLANNING_API_KEY"),
		LLMModel:                   os.Getenv("AI_PLANNING_MODEL"),
		LLMThinkMode:               boolFromEnv("AI_PLANNING_THINK_MODE"),
		LLMReasoningEffort:         strings.TrimSpace(os.Getenv("AI_PLANNING_REASONING_EFFORT")),
		AgentMaxTurns:              maxTurns,
		AgentMaxWallTimeSeconds:    nonNegativeIntOrDefault("AGENTSERVICE_MAX_WALL_TIME_SECONDS", 0),
		AgentExploreReserveSeconds: positiveIntOrDefault("AGENTSERVICE_EXPLORE_RESERVE_SECONDS", 90),
		AgentMaxModelCalls:         nonNegativeIntOrDefault("AGENTSERVICE_MAX_MODEL_CALLS", 40),
		AgentMaxTotalTokens:        nonNegativeInt64OrDefault("AGENTSERVICE_MAX_TOTAL_TOKENS", 3_000_000),
		AgentMaxTranscriptBytes:    nonNegativeIntOrDefault("AGENTSERVICE_MAX_TRANSCRIPT_BYTES", 1_000_000),
		// Exploration budget. Per-plan values bound how many page/flow probes a
		// single TaskPlan version may spend; run-wide values are hard limits
		// across the whole run. Both count failed calls too (a failure returns
		// evidence the model must learn from), so defaults are sized generously
		// to leave room for legitimate retry-after-failure.
		AgentMaxExplorePageCalls:    positiveIntOrDefault("AGENTSERVICE_MAX_EXPLORE_PAGE_CALLS", 10),
		AgentMaxExploreFlowCalls:    positiveIntOrDefault("AGENTSERVICE_MAX_EXPLORE_FLOW_CALLS", 10),
		AgentMaxRunExplorePageCalls: positiveIntOrDefault("AGENTSERVICE_MAX_RUN_EXPLORE_PAGE_CALLS", 12),
		AgentMaxRunExploreFlowCalls: positiveIntOrDefault("AGENTSERVICE_MAX_RUN_EXPLORE_FLOW_CALLS", 12),
		AgentPerTurnTranscriptCompaction: boolFromEnv(
			"AGENTSERVICE_PER_TURN_TRANSCRIPT_COMPACTION",
		),
		DefaultActorID:   int64(positiveIntOrDefault("DEFAULT_ACTOR_USER_ID", 1)),
		DatabaseURL:      normalizeDatabaseURL(os.Getenv("DATABASE_URL")),
		BrowserWorkerURL: browserWorkerURL,
	}
}

func (c Config) AgentMaxWallTime() time.Duration {
	return time.Duration(c.AgentMaxWallTimeSeconds) * time.Second
}

func (c Config) AgentExploreReserve() time.Duration {
	return time.Duration(c.AgentExploreReserveSeconds) * time.Second
}

func boolFromEnv(name string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(name))) {
	case "1", "true", "yes", "on", "enabled":
		return true
	default:
		return false
	}
}

func positiveIntOrDefault(name string, fallback int) int {
	if value, err := strconv.Atoi(os.Getenv(name)); err == nil && value > 0 {
		return value
	}
	return fallback
}

func nonNegativeInt64OrDefault(name string, fallback int64) int64 {
	if value, err := strconv.ParseInt(os.Getenv(name), 10, 64); err == nil && value >= 0 {
		return value
	}
	return fallback
}

func nonNegativeIntOrDefault(name string, fallback int) int {
	if value, err := strconv.Atoi(os.Getenv(name)); err == nil && value >= 0 {
		return value
	}
	return fallback
}

func normalizeDatabaseURL(value string) string {
	value = strings.TrimSpace(value)
	value = strings.Replace(value, "postgresql+psycopg://", "postgres://", 1)
	value = strings.Replace(value, "postgresql://", "postgres://", 1)
	// BUG-177: force a UTC session so `now()`-written timestamp columns and
	// UTC window queries (Overview, agent deadlines) share one calendar.
	// pgx v5 passes unrecognized connection parameters as runtime parameters.
	if strings.HasPrefix(value, "postgres://") {
		parsed, err := url.Parse(value)
		if err == nil && parsed.Query().Get("timezone") == "" {
			query := parsed.Query()
			query.Set("timezone", "UTC")
			parsed.RawQuery = query.Encode()
			value = parsed.String()
		}
	}
	return value
}
