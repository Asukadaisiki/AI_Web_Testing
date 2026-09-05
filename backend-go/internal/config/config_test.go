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
