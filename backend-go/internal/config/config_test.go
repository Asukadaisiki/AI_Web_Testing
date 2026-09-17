package config

import (
	"testing"
	"time"
)

func TestNormalizeDatabaseURL(t *testing.T) {
	tests := map[string]string{
		"postgresql+psycopg://user:pass@localhost/db": "postgres://user:pass@localhost/db?timezone=UTC",
		"postgresql://user:pass@localhost/db":         "postgres://user:pass@localhost/db?timezone=UTC",
		"postgres://user:pass@localhost/db":           "postgres://user:pass@localhost/db?timezone=UTC",
		"postgres://user:pass@localhost/db?sslmode=disable": "postgres://user:pass@localhost/db?sslmode=disable&timezone=UTC",
		"postgres://user:pass@localhost/db?timezone=Asia/Shanghai": "postgres://user:pass@localhost/db?timezone=Asia/Shanghai",
	}
	for input, want := range tests {
		if got := normalizeDatabaseURL(input); got != want {
			t.Fatalf("normalizeDatabaseURL(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestLoadUsesConfiguredDefaultActor(t *testing.T) {
	t.Setenv("DEFAULT_ACTOR_USER_ID", "9")
	t.Setenv("AI_PLANNING_PROVIDER", "gateway")
	t.Setenv("AI_PLANNING_THINK_MODE", "enabled")
	t.Setenv("AI_PLANNING_REASONING_EFFORT", "high")
	loaded := Load()
	if got := loaded.DefaultActorID; got != 9 {
		t.Fatalf("DefaultActorID = %d, want 9", got)
	}
	if loaded.LLMProvider != "gateway" {
		t.Fatalf("LLMProvider = %q, want gateway", loaded.LLMProvider)
	}
	if !loaded.LLMThinkMode || loaded.LLMReasoningEffort != "high" {
		t.Fatalf("thinking config = %#v", loaded)
	}
}

func TestLoadParsesExplorationWallClockBudget(t *testing.T) {
	t.Setenv("AGENTSERVICE_MAX_WALL_TIME_SECONDS", "600")
	t.Setenv("AGENTSERVICE_EXPLORE_RESERVE_SECONDS", "45")
	loaded := Load()
	if loaded.AgentMaxWallTimeSeconds != 600 {
		t.Fatalf("AgentMaxWallTimeSeconds = %d, want 600", loaded.AgentMaxWallTimeSeconds)
	}
	if loaded.AgentExploreReserveSeconds != 45 {
		t.Fatalf("AgentExploreReserveSeconds = %d, want 45", loaded.AgentExploreReserveSeconds)
	}
	if loaded.AgentMaxWallTime() != 600*time.Second {
		t.Fatalf("AgentMaxWallTime() = %v, want 10m", loaded.AgentMaxWallTime())
	}
	if loaded.AgentExploreReserve() != 45*time.Second {
		t.Fatalf("AgentExploreReserve() = %v, want 45s", loaded.AgentExploreReserve())
	}
}

func TestLoadDefaultsExplorationWallClockBudget(t *testing.T) {
	t.Setenv("AGENTSERVICE_MAX_WALL_TIME_SECONDS", "")
	t.Setenv("AGENTSERVICE_EXPLORE_RESERVE_SECONDS", "")
	loaded := Load()
	if loaded.AgentMaxWallTimeSeconds != 0 {
		t.Fatalf("AgentMaxWallTimeSeconds = %d, want 0", loaded.AgentMaxWallTimeSeconds)
	}
	if loaded.AgentExploreReserveSeconds != 90 {
		t.Fatalf("AgentExploreReserveSeconds = %d, want 90", loaded.AgentExploreReserveSeconds)
	}
	if loaded.AgentMaxWallTime() != 0 {
		t.Fatalf("AgentMaxWallTime() = %v, want 0", loaded.AgentMaxWallTime())
	}
	if loaded.AgentExploreReserve() != 90*time.Second {
		t.Fatalf("AgentExploreReserve() = %v, want 90s", loaded.AgentExploreReserve())
	}
}

func TestLoadMaxTurnsDefaultAndOverride(t *testing.T) {
	t.Setenv("AGENTSERVICE_MAX_TURNS", "")
	loaded := Load()
	if loaded.AgentMaxTurns != 24 {
		t.Fatalf("AgentMaxTurns = %d, want default 24", loaded.AgentMaxTurns)
	}

	t.Setenv("AGENTSERVICE_MAX_TURNS", "40")
	loaded = Load()
	if loaded.AgentMaxTurns != 40 {
		t.Fatalf("AgentMaxTurns = %d, want 40", loaded.AgentMaxTurns)
	}

	t.Setenv("AGENTSERVICE_MAX_TURNS", "not-a-number")
	loaded = Load()
	if loaded.AgentMaxTurns != 24 {
		t.Fatalf("AgentMaxTurns = %d, want fallback 24", loaded.AgentMaxTurns)
	}
}
