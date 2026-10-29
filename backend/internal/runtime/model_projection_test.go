package runtime

import (
	"encoding/json"
	"testing"
)

func TestModelUsageIdentityProjection(t *testing.T) {
	value, err := modelUsage(json.RawMessage(`{"provider":"openai-compatible","model":"glm-4.7","input_tokens":10,"output_tokens":5}`), "attempt")
	if err != nil {
		t.Fatal(err)
	}
	if value == nil || value.Provider != "openai-compatible" || value.Model != "glm-4.7" {
		t.Fatalf("model identity lost: %#v", value)
	}
}

func TestModelMessageIdentityProjection(t *testing.T) {
	value, err := decodeModelMessage(json.RawMessage(`{"text":"answer","provider":"openai-compatible","model":"glm-4.7"}`))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	if fields["provider"] != "openai-compatible" || fields["model"] != "glm-4.7" {
		t.Fatalf("message projection lost identity: %s", raw)
	}
}

func TestModelIdentityRejectsInvalidEvidence(t *testing.T) {
	for _, raw := range []string{
		`{"provider":null}`, `{"model":null}`, `{"provider":" "}`, `{"model":42}`,
	} {
		t.Run(raw, func(t *testing.T) {
			if _, err := decodeModelMessage(json.RawMessage(raw)); err == nil {
				t.Fatal("accepted invalid message identity")
			}
			var fields map[string]json.RawMessage
			_ = json.Unmarshal([]byte(raw), &fields)
			fields["input_tokens"] = json.RawMessage(`1`)
			fields["output_tokens"] = json.RawMessage(`2`)
			usageRaw, _ := json.Marshal(fields)
			if _, err := modelUsage(usageRaw, "attempt"); err == nil {
				t.Fatal("accepted invalid usage identity")
			}
		})
	}
}
