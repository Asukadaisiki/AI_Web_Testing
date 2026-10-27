package agentprofile_test

import (
	"testing"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/agentprofile"
)

func TestLoadBrowserEvalRetainsModelDefaultsAndSafetyWithoutOperations(t *testing.T) {
	profile, err := agentprofile.LoadBrowserEval()
	if err != nil {
		t.Fatalf("load browser profile: %v", err)
	}
	if profile.ID != "browser-eval" {
		t.Fatalf("profile id = %q, want browser-eval", profile.ID)
	}
	if profile.ProductName != "AgentTrace Eval Hub" {
		t.Fatalf("product name = %q", profile.ProductName)
	}
	if profile.Model.Provider != "openai-compatible" ||
		profile.Model.Name != "glm-5.3-flash" ||
		profile.Model.ReasoningEffort != "high" {
		t.Fatalf("model defaults = %#v", profile.Model)
	}
	if profile.Model.Endpoint != "" || profile.Model.APIKey != "" {
		t.Fatalf("profile must not contain model credentials or endpoint: %#v", profile.Model)
	}
	if !profile.Safety.FreshContext ||
		!profile.Safety.DisposableFixtureData ||
		profile.Safety.IrreversibleProductionOperations != "unsupported" {
		t.Fatalf("browser safety = %#v", profile.Safety)
	}
}
