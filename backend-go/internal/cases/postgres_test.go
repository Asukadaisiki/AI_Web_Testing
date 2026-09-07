package cases

import (
	"encoding/json"
	"testing"
)

func TestValidateMutationRejectsUnsupportedAction(t *testing.T) {
	err := validateMutation(Mutation{
		ProjectID: 1,
		Name:      "invalid",
		Steps:     json.RawMessage(`[{"action":"eval"}]`),
	})
	if err == nil {
		t.Fatal("validateMutation() error = nil")
	}
}

func TestValidateMutationAcceptsSupportedActions(t *testing.T) {
	err := validateMutation(Mutation{
		ProjectID: 1,
		Name:      "valid",
		Steps:     json.RawMessage(`[{"action":"goto"},{"action":"assert_text"}]`),
	})
	if err != nil {
		t.Fatalf("validateMutation() error = %v", err)
	}
}

func TestEncodeDSLPreservesProfile(t *testing.T) {
	profile := "research-v1"
	raw, err := encodeDSL(Mutation{
		ProjectID: 1,
		Profile:   &profile,
		Name:      "research",
		Steps:     json.RawMessage(`[{"action":"goto"}]`),
	})
	if err != nil {
		t.Fatal(err)
	}
	var stored map[string]any
	if err := json.Unmarshal(raw, &stored); err != nil {
		t.Fatal(err)
	}
	if stored["profile"] != profile {
		t.Fatalf("stored profile = %#v, want %q", stored["profile"], profile)
	}

	legacy, err := encodeDSL(Mutation{
		ProjectID: 1,
		Name:      "legacy",
		Steps:     json.RawMessage(`[{"action":"goto"}]`),
	})
	if err != nil {
		t.Fatal(err)
	}
	stored = nil
	if err := json.Unmarshal(legacy, &stored); err != nil {
		t.Fatal(err)
	}
	if _, exists := stored["profile"]; exists {
		t.Fatalf("legacy DSL unexpectedly stored profile: %s", legacy)
	}
}
