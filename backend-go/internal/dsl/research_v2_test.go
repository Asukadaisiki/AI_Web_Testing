package dsl

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestResearchV2DraftAndExecutableContracts(t *testing.T) {
	draft := json.RawMessage(`{
		"profile":"research-v2",
		"name":"Submit form",
		"steps":[{
			"plan_step_id":"submit",
			"action":"click",
			"intent":"Submit form",
			"target_binding_id":"binding-1",
			"preconditions":[{"type":"text_visible","value":"Submit"}],
			"postconditions":[{"type":"text_visible","value":"Saved"}],
			"idempotency":"idempotent",
			"side_effect":"browser_state"
		}]
	}`)
	validated, err := ValidateDraftCase(draft)
	if err != nil {
		t.Fatal(err)
	}
	if validated.Profile != ProfileResearchV2 ||
		validated.CanonicalVersion != CanonicalVersionV3 {
		t.Fatalf("validated = %#v", validated)
	}

	executable := json.RawMessage(`{
		"profile":"research-v2",
		"name":"Submit form",
		"input_contract":[],
		"output_contract":[],
		"plan_binding":{"plan_id":"plan-1","version":1,"sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
		"observation_bindings":[{
			"binding_id":"binding-1",
			"binding_sha256":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			"observation_id":"obs-1",
			"observation_sha256":"cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
		}],
		"steps":[{
			"plan_step_id":"submit",
			"action":"click",
			"intent":"Submit form",
			"target_binding_id":"binding-1",
			"semantic_target":"Submit",
			"locator_candidates":[{
				"candidate_id":"candidate-1",
				"element_ref":"form:7",
				"locator":{"kind":"role","role":"button","name":"Submit","exact":true},
				"provenance":"a11y_exact",
				"observed_count":1,
				"visible":true,
				"enabled":true,
				"score":0.95
			}],
			"preconditions":[{"type":"text_visible","value":"Submit"}],
			"postconditions":[{"type":"text_visible","value":"Saved"}],
			"idempotency":"idempotent",
			"side_effect":"browser_state"
		}]
	}`)
	validated, err = ValidateExecutableCase(executable)
	if err != nil {
		t.Fatal(err)
	}
	if validated.CanonicalVersion != CanonicalVersionV3 {
		t.Fatalf("canonical version = %q", validated.CanonicalVersion)
	}
}

func TestResearchV2RejectsModelAuthoredLocatorCandidates(t *testing.T) {
	_, err := ValidateDraftCase(json.RawMessage(`{
		"profile":"research-v2",
		"name":"Invalid",
		"steps":[{
			"plan_step_id":"submit",
			"action":"click",
			"intent":"Submit form",
			"target_binding_id":"binding-1",
			"semantic_target":"Submit",
			"locator_candidates":[],
			"preconditions":[{"type":"element_visible","value":"Submit"}],
			"postconditions":[{"type":"text_visible","value":"Saved"}],
			"idempotency":"idempotent",
			"side_effect":"browser_state"
		}]
	}`))
	if err == nil {
		t.Fatal("compiler-owned locator fields were accepted in a draft")
	}
}

func TestResearchV2SharedCanonicalGolden(t *testing.T) {
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test source")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(source), "../../.."))
	raw, err := os.ReadFile(
		filepath.Join(root, "testdata/dsl_research_v2_contract.json"),
	)
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		CanonicalVersion string `json:"canonical_version"`
		SHA256           string `json:"sha256"`
		CanonicalJSON    string `json:"canonical_json"`
	}
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	validated, err := ValidateCaseForVersion(
		json.RawMessage(fixture.CanonicalJSON),
		fixture.CanonicalVersion,
	)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(validated.CanonicalJSON)
	if actual := hex.EncodeToString(sum[:]); actual != fixture.SHA256 {
		t.Fatalf("sha256 = %s, want %s", actual, fixture.SHA256)
	}
	if string(validated.CanonicalJSON) != fixture.CanonicalJSON {
		t.Fatalf(
			"canonical bytes changed:\n%s\nwant:\n%s",
			validated.CanonicalJSON,
			fixture.CanonicalJSON,
		)
	}
}
