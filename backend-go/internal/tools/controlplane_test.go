package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	dslstore "github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/dsl"
)

type preflightFailureBrowser struct{}

func (preflightFailureBrowser) ExecuteBrowserCapability(
	context.Context,
	string,
	int64,
	int64,
	string,
	json.RawMessage,
) (json.RawMessage, error) {
	return json.RawMessage(`{
		"dsl_case":{"name":"Example","steps":[{"action":"click","target":"Missing"}]},
		"valid":false,
		"warnings":["Step 0: match_count=0"]
	}`), nil
}

func TestGenerateDSLReturnsPreflightWarnings(t *testing.T) {
	capabilities := NewControlPlaneCapabilities(
		dslstore.NewStore(nil),
		nil,
		nil,
		preflightFailureBrowser{},
	)

	_, err := capabilities.GenerateDSL(
		context.Background(),
		1,
		1,
		"1",
		json.RawMessage(`{
			"case":{"name":"Example","steps":[{"action":"click","target":"Missing"}]},
			"a11y_nodes_by_state":{"S0":[]}
		}`),
	)

	if err == nil || !strings.Contains(err.Error(), "Step 0: match_count=0") {
		t.Fatalf("GenerateDSL() error = %v, want preflight warning", err)
	}
}
