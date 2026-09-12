package browsercontract

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v5"
)

func TestCapabilityManifestMatchesLocatorContract(t *testing.T) {
	var manifest struct {
		SchemaVersion string   `json:"schema_version"`
		LocatorKinds  []string `json:"locator_kinds"`
	}
	if err := json.Unmarshal(readRepositoryFile(t, "contracts/browser-capabilities.v1.json"), &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.SchemaVersion != "browser.capabilities.v1" {
		t.Fatalf("schema version = %q", manifest.SchemaVersion)
	}
	for _, kind := range manifest.LocatorKinds {
		spec := LocatorSpec{Kind: kind, Value: "value", Exact: true}
		if kind == "role" {
			spec.Value = ""
			spec.Role = "button"
		}
		if kind == "scoped" {
			spec.Value = ""
			spec.Scope = &LocatorSpec{Kind: "text", Value: "row", Exact: true}
			spec.Target = &LocatorSpec{Kind: "role", Role: "button", Exact: true}
		}
		if err := spec.Validate(); err != nil {
			t.Fatalf("manifest locator kind %q is unsupported: %v", kind, err)
		}
	}
}

func TestTargetBindingValidatesHashAndSelectedCandidate(t *testing.T) {
	name := "Submit"
	binding := TargetBinding{
		SchemaVersion:     TargetBindingVersion,
		BindingID:         "binding-1",
		PlanID:            "plan-1",
		PlanVersion:       1,
		PlanStepID:        "step-1",
		ProbeID:           "probe-1",
		SemanticTarget:    "Submit form",
		Action:            "click",
		PageStateID:       "state-1",
		ObservationID:     "obs-1",
		ObservationSHA256: repeat("a", 64),
		ElementRefs:       []string{"el-1"},
		Candidates: []LocatorCandidate{{
			CandidateID: "candidate-1", ElementRef: "el-1",
			Locator: LocatorSpec{
				Kind: "role", Role: "button", Name: &name, Exact: true,
			},
			Provenance: "a11y_exact", ObservedCount: 1,
			Visible: true, Enabled: true, Score: 0.95,
		}},
		SelectedCandidateID: "candidate-1",
	}
	hash, err := binding.CalculateSHA256()
	if err != nil {
		t.Fatal(err)
	}
	binding.BindingSHA256 = hash
	if err := binding.Validate(); err != nil {
		t.Fatal(err)
	}
	binding.SelectedCandidateID = "missing"
	if err := binding.Validate(); err == nil {
		t.Fatal("missing selected candidate was accepted")
	}
}

func TestCrossSiteFixtureCoversDistinctPageKinds(t *testing.T) {
	var fixture struct {
		SchemaVersion string `json:"schema_version"`
		Scenarios     []struct {
			ID       string `json:"id"`
			PageKind string `json:"page_kind"`
		} `json:"scenarios"`
	}
	if err := json.Unmarshal(
		readRepositoryFile(t, "research/fixtures/browser-contract-cross-site.v1.json"),
		&fixture,
	); err != nil {
		t.Fatal(err)
	}
	if fixture.SchemaVersion != "browser-contract-cross-site.v1" ||
		len(fixture.Scenarios) != 7 {
		t.Fatalf("fixture = %#v", fixture)
	}
	kinds := make(map[string]bool)
	for _, scenario := range fixture.Scenarios {
		kinds[scenario.PageKind] = true
	}
	if len(kinds) < 6 {
		t.Fatalf("page kinds = %#v", kinds)
	}
}

func TestSharedSchemasCompileAndValidateTargetBinding(t *testing.T) {
	compiler := jsonschema.NewCompiler()
	for _, name := range []string{
		"locator-spec.v1.schema.json",
		"browser-observation.v2.schema.json",
		"browser-resolved-target.v1.schema.json",
		"grounding-query.v1.schema.json",
		"target-binding.v1.schema.json",
	} {
		content := readRepositoryFile(t, "contracts/"+name)
		if err := compiler.AddResource(
			"https://ai-web-testing.local/contracts/"+name,
			bytes.NewReader(content),
		); err != nil {
			t.Fatal(err)
		}
	}
	schema, err := compiler.Compile(
		"https://ai-web-testing.local/contracts/target-binding.v1.schema.json",
	)
	if err != nil {
		t.Fatal(err)
	}
	name := "Submit"
	binding, err := NewTargetBinding(TargetBinding{
		PlanID: "plan-1", PlanVersion: 1, PlanStepID: "submit",
		ProbeID:        "probe-1",
		SemanticTarget: "Submit form", Action: "click",
		PageStateID: "state-1", ObservationID: "obs-1",
		ObservationSHA256: repeat("a", 64),
		ElementRefs:       []string{"form:7"},
		Candidates: []LocatorCandidate{{
			CandidateID: "candidate-1", ElementRef: "form:7",
			Locator: LocatorSpec{
				Kind: "role", Role: "button", Name: &name, Exact: true,
			},
			Provenance: "a11y_exact", ObservedCount: 1,
			Visible: true, Enabled: true, Score: 0.95,
		}},
		SelectedCandidateID: "candidate-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(binding)
	if err != nil {
		t.Fatal(err)
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatal(err)
	}
	if err := schema.Validate(value); err != nil {
		t.Fatal(err)
	}
	resolvedSchema, err := compiler.Compile(
		"https://ai-web-testing.local/contracts/browser-resolved-target.v1.schema.json",
	)
	if err != nil {
		t.Fatal(err)
	}
	resolved := ResolvedTargetEvidence{
		SchemaVersion: ResolvedTargetVersion,
		ProbeID:       "probe-1", PlanStepID: "submit",
		StepIndex: 0, ActionIndex: 0, Action: "click",
		ObservationID: "obs-1", PageStateID: "state-1",
		PageStateSHA256: repeat("a", 64),
		ElementRef:      "form:7", CandidateID: "candidate-1",
		Locator: LocatorSpec{
			Kind: "role", Role: "button", Name: &name, Exact: true,
		},
		ContextPath: ContextPath{Frames: []string{}, ShadowHosts: []string{}},
		Provenance:  "grounding_query", RuntimeMatchCount: 1,
		Visible: true, Enabled: true, Score: 0.95,
		ActionStatus: "succeeded",
	}
	if err := resolved.Validate(); err != nil {
		t.Fatal(err)
	}
	raw, err = json.Marshal(resolved)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatal(err)
	}
	if err := resolvedSchema.Validate(value); err != nil {
		t.Fatal(err)
	}
	groundingSchema, err := compiler.Compile(
		"https://ai-web-testing.local/contracts/grounding-query.v1.schema.json",
	)
	if err != nil {
		t.Fatal(err)
	}
	groundingQuery := map[string]any{
		"schema_version": "grounding.query.v1",
		"plan_step_ids":  []any{"submit"},
		"steps": []any{map[string]any{
			"actions": []any{map[string]any{
				"action":       "click",
				"plan_step_id": "submit",
				"locator": map[string]any{
					"kind": "role", "role": "button", "name": "Submit",
				},
			}},
		}},
	}
	if err := groundingSchema.Validate(groundingQuery); err != nil {
		t.Fatal(err)
	}
}

func TestSharedSchemaValidatesSerializedBrowserObservation(t *testing.T) {
	compiler := jsonschema.NewCompiler()
	for _, name := range []string{
		"locator-spec.v1.schema.json",
		"browser-observation.v2.schema.json",
	} {
		content := readRepositoryFile(t, "contracts/"+name)
		if err := compiler.AddResource(
			"https://ai-web-testing.local/contracts/"+name,
			bytes.NewReader(content),
		); err != nil {
			t.Fatal(err)
		}
	}
	schema, err := compiler.Compile(
		"https://ai-web-testing.local/contracts/browser-observation.v2.schema.json",
	)
	if err != nil {
		t.Fatal(err)
	}
	raw := readRepositoryFile(t, "testdata/browser_observation_v2_contract.json")
	var observation any
	if err := json.Unmarshal(raw, &observation); err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeObservation(raw); err != nil {
		t.Fatalf("Go observation contract rejected shared fixture: %v", err)
	}
	if err := schema.Validate(observation); err != nil {
		t.Fatal(err)
	}
}

func TestTargetBindingRejectsXPathInsideShadowRoot(t *testing.T) {
	binding := TargetBinding{
		SchemaVersion:     TargetBindingVersion,
		BindingID:         "binding-1",
		PlanID:            "plan-1",
		PlanVersion:       1,
		PlanStepID:        "step-1",
		ProbeID:           "probe-1",
		SemanticTarget:    "Shadow action",
		Action:            "click",
		PageStateID:       "state-1",
		ObservationID:     "obs-1",
		ObservationSHA256: repeat("a", 64),
		ElementRefs:       []string{"el-1"},
		Candidates: []LocatorCandidate{{
			CandidateID: "candidate-1", ElementRef: "el-1",
			ContextPath: ContextPath{ShadowHosts: []string{"popup-info"}},
			Locator: LocatorSpec{
				Kind: "xpath", Value: "//button", Exact: true,
			},
			Provenance: "dom_verified", ObservedCount: 1,
			Visible: true, Enabled: true, Score: 0.5,
		}},
		SelectedCandidateID: "candidate-1",
		BindingSHA256:       repeat("b", 64),
	}
	if err := binding.Validate(); err == nil {
		t.Fatal("xpath inside shadow root was accepted")
	}
}

func readRepositoryFile(t *testing.T, path string) []byte {
	t.Helper()
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test source")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(source), "../../.."))
	content, err := os.ReadFile(filepath.Join(root, path))
	if err != nil {
		t.Fatal(err)
	}
	return content
}

func repeat(value string, count int) string {
	result := ""
	for range count {
		result += value
	}
	return result
}
