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
