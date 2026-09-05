package dsl

import (
	"encoding/json"
	"testing"
)

func TestValidateCaseAcceptsRunnableContract(t *testing.T) {
	raw := json.RawMessage(`{
		"name":"Checkout",
		"base_url":"https://example.com",
		"input_contract":[
			{"name":"Account","context_key":"account","value_type":"string","required":true}
		],
		"output_contract":[
			{"name":"Order ID","context_key":"order_id","value_type":"string","source":"last_step_value"}
		],
		"steps":[
			{"action":"goto","value":"/checkout"},
			{
				"action":"click",
				"target":"button \"Pay\"",
				"locator_confidence":"high",
				"candidates":[{"strategy":"role","semantic_value":"Pay","pre_score":0.9}],
				"postconditions":[{"type":"url_changes","timeout_ms":3000}]
			}
		],
		"_preflight":{"locator_confidence":"high"}
	}`)
	normalized, baseURL, err := ValidateCase(raw)
	if err != nil {
		t.Fatalf("ValidateCase() error = %v", err)
	}
	if baseURL != "https://example.com" {
		t.Fatalf("baseURL = %q", baseURL)
	}
	var decoded map[string]any
	if err := json.Unmarshal(normalized, &decoded); err != nil {
		t.Fatal(err)
	}
	if _, exists := decoded["_preflight"]; exists {
		t.Fatal("normalized DSL retained _preflight metadata")
	}
}

func TestValidateCaseRejectsInvalidNestedContracts(t *testing.T) {
	tests := []string{
		`{"name":"x","steps":[{"action":"wait_for","target":"x","timeout_ms":60001}]}`,
		`{"name":"x","steps":[{"action":"capture_text","target":"x","context_key":"bad-key"}]}`,
		`{"name":"x","steps":[{"action":"click","target":"x","locator_confidence":"certain"}]}`,
		`{"name":"x","steps":[{"action":"click","target":"x","candidates":[{"strategy":"role","pre_score":2}]}]}`,
		`{"name":"x","steps":[{"action":"click","target":"x","postconditions":[{"type":"unknown"}]}]}`,
		`{"name":"x","input_contract":[{"name":"A","context_key":"a","value_type":"binary"}],"steps":[{"action":"goto","value":"/"}]}`,
	}
	for _, raw := range tests {
		if _, _, err := ValidateCase(json.RawMessage(raw)); err == nil {
			t.Fatalf("ValidateCase(%s) error = nil", raw)
		}
	}
}
