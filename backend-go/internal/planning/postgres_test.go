package planning

import (
	"encoding/json"
	"testing"
)

func TestInitialRequirementsPersistCleanContext(t *testing.T) {
	for _, test := range []struct {
		name         string
		cleanContext bool
	}{
		{name: "default"},
		{name: "clean", cleanContext: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			raw, err := initialRequirements(test.cleanContext)
			if err != nil {
				t.Fatalf("initialRequirements() error = %v", err)
			}
			var requirements map[string]any
			if err := json.Unmarshal(raw, &requirements); err != nil {
				t.Fatalf("decode requirements: %v", err)
			}
			if got, ok := requirements["clean_context"].(bool); !ok || got != test.cleanContext {
				t.Fatalf(
					"clean_context = %#v, want %t",
					requirements["clean_context"],
					test.cleanContext,
				)
			}
		})
	}
}
