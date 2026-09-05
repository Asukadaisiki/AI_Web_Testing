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
	if got := Load().DefaultActorID; got != 9 {
		t.Fatalf("DefaultActorID = %d, want 9", got)
	}
}
