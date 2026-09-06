package config

import "testing"

func TestNormalizeDatabaseURL(t *testing.T) {
	tests := map[string]string{
		"postgresql+psycopg://user:pass@localhost/db": "postgres://user:pass@localhost/db",
		"postgresql://user:pass@localhost/db":         "postgres://user:pass@localhost/db",
		"postgres://user:pass@localhost/db":           "postgres://user:pass@localhost/db",
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
	loaded := Load()
	if got := loaded.DefaultActorID; got != 9 {
		t.Fatalf("DefaultActorID = %d, want 9", got)
	}
	if loaded.LLMProvider != "gateway" {
		t.Fatalf("LLMProvider = %q, want gateway", loaded.LLMProvider)
	}
}
