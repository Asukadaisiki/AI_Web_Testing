package dsl

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestCanonicalContractGolden(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "dsl_canonical_contract.json"))
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Input            json.RawMessage `json:"input"`
		CanonicalVersion string          `json:"canonical_version"`
		CanonicalJSON    string          `json:"canonical_json"`
		SHA256           string          `json:"sha256"`
	}
	if decodeErr := json.Unmarshal(raw, &fixture); decodeErr != nil {
		t.Fatal(decodeErr)
	}
	canonical, _, err := ValidateCase(fixture.Input)
	if err != nil {
		t.Fatalf("ValidateCase() error = %v", err)
	}
	if string(canonical) != fixture.CanonicalJSON {
		t.Fatalf("canonical JSON mismatch\n got: %s\nwant: %s", canonical, fixture.CanonicalJSON)
	}
	if hash := SHA256(canonical); hash != fixture.SHA256 {
		t.Fatalf("SHA256() = %s, want %s", hash, fixture.SHA256)
	}
	if fixture.CanonicalVersion != CanonicalVersion {
		t.Fatalf("canonical version = %s, want %s", fixture.CanonicalVersion, CanonicalVersion)
	}
}

func TestValidateCaseIsIdempotentForCanonicalOptionalNulls(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "dsl_canonical_contract.json"))
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Input         json.RawMessage `json:"input"`
		CanonicalJSON string          `json:"canonical_json"`
		SHA256        string          `json:"sha256"`
	}
	if decodeErr := json.Unmarshal(raw, &fixture); decodeErr != nil {
		t.Fatal(decodeErr)
	}
	first, _, err := ValidateCase(fixture.Input)
	if err != nil {
		t.Fatalf("first ValidateCase() error = %v", err)
	}
	second, _, err := ValidateCase(first)
	if err != nil {
		t.Fatalf("second ValidateCase() error = %v", err)
	}
	if string(second) != string(first) || string(second) != fixture.CanonicalJSON {
		t.Fatalf("canonicalization is not idempotent\nfirst:  %s\nsecond: %s", first, second)
	}
	if hash := SHA256(second); hash != fixture.SHA256 {
		t.Fatalf("idempotent SHA256() = %s, want %s", hash, fixture.SHA256)
	}
}

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

func TestValidateCasePreservesConditionsAndNetworkFilters(t *testing.T) {
	raw := json.RawMessage(`{
		"name":"Cart",
		"steps":[{
			"action":"click",
			"target":"Add to cart",
			"preconditions":[{"type":"element_visible","value":"button.cart"}],
			"postconditions":[{
				"type":"network_request",
				"value":"/api/cart",
				"method":"post",
				"status":201
			}]
		}]
	}`)

	normalized, _, err := ValidateCase(raw)
	if err != nil {
		t.Fatalf("ValidateCase() error = %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(normalized, &payload); err != nil {
		t.Fatal(err)
	}
	step := payload["steps"].([]any)[0].(map[string]any)
	precondition := step["preconditions"].([]any)[0].(map[string]any)
	postcondition := step["postconditions"].([]any)[0].(map[string]any)
	if precondition["type"] != "element_visible" {
		t.Fatalf("precondition = %#v", precondition)
	}
	if postcondition["method"] != "POST" || postcondition["status"] != float64(201) {
		t.Fatalf("postcondition = %#v", postcondition)
	}
}

func TestValidateCaseAcceptsAbsentAndNullOptionalLocatorEnums(t *testing.T) {
	tests := []string{
		`{"name":"absent","steps":[{"action":"click","target":"Login"}]}`,
		`{"name":"null","steps":[{"action":"click","target":"Login","target_strategy":null,"locator_confidence":null}]}`,
	}
	for _, raw := range tests {
		if _, _, err := ValidateCase(json.RawMessage(raw)); err != nil {
			t.Fatalf("ValidateCase(%s) error = %v", raw, err)
		}
	}
}

func TestValidateCaseInputTriggerContract(t *testing.T) {
	accepted := []string{
		`{"name":"absent","steps":[{"action":"input","target":"Search","value":"Blue Top"}]}`,
		`{"name":"null","steps":[{"action":"input","target":"Search","value":"Blue Top","trigger":null}]}`,
		`{"name":"enter","steps":[{"action":"input","target":"Search","value":"Blue Top","trigger":"Enter"}]}`,
		`{"name":"tab","steps":[{"action":"input","target":"Search","value":"Blue Top","trigger":"Tab"}]}`,
	}
	for _, raw := range accepted {
		if _, _, err := ValidateCase(json.RawMessage(raw)); err != nil {
			t.Fatalf("ValidateCase(%s) error = %v", raw, err)
		}
	}

	rejected := []string{
		`{"name":"empty","steps":[{"action":"input","target":"Search","value":"Blue Top","trigger":""}]}`,
		`{"name":"semantic text","steps":[{"action":"input","target":"Search","value":"Blue Top","trigger":"Search Product textbox"}]}`,
		`{"name":"unsupported key","steps":[{"action":"input","target":"Search","value":"Blue Top","trigger":"Escape"}]}`,
	}
	for _, raw := range rejected {
		if _, _, err := ValidateCase(json.RawMessage(raw)); err == nil {
			t.Fatalf("ValidateCase(%s) error = nil", raw)
		}
	}
}

func TestValidateCaseRejectsInvalidNestedContracts(t *testing.T) {
	tests := []string{
		`{"name":"x","steps":[{"action":"wait_for","target":"x","timeout_ms":60001}]}`,
		`{"name":"x","steps":[{"action":"capture_text","target":"x","context_key":"bad-key"}]}`,
		`{"name":"x","steps":[{"action":"click","target":"x","locator_confidence":""}]}`,
		`{"name":"x","steps":[{"action":"click","target":"x","locator_confidence":"certain"}]}`,
		`{"name":"x","steps":[{"action":"click","target":"x","target_strategy":""}]}`,
		`{"name":"x","steps":[{"action":"click","target":"x","target_strategy":"semantic"}]}`,
		`{"name":"x","steps":[{"action":"click","target":"x","target_strategy":"text"}]}`,
		`{"name":"x","steps":[{"action":"click","target":"x","target_strategy":"unknown"}]}`,
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

func TestValidateCaseRequiresTargetURLForVerifiedCrossPageAnchor(t *testing.T) {
	tests := []string{
		`{"name":"missing","steps":[{"action":"click","target":"Details","candidates":[{"strategy":"css","selector":"a[href='/details/1']","pre_score":1,"pre_features":{"verified_href":"/details/1"}}]}]}`,
		`{"name":"changes-only","steps":[{"action":"click","target":"Details","candidates":[{"strategy":"css","selector":"a[href='/details/1']","pre_score":1,"pre_features":{"verified_href":"/details/1"}}],"postconditions":[{"type":"url_changes"}]}]}`,
		`{"name":"wrong-target","steps":[{"action":"click","target":"Details","candidates":[{"strategy":"css","selector":"a[href='/details/1']","pre_score":1,"pre_features":{"verified_href":"/details/1"}}],"postconditions":[{"type":"url_contains","value":"/cart"}]}]}`,
	}
	for _, raw := range tests {
		if _, _, err := ValidateCase(json.RawMessage(raw)); err == nil {
			t.Fatalf("ValidateCase(%s) error = nil", raw)
		}
	}

	valid := json.RawMessage(`{"name":"valid","steps":[{"action":"click","target":"Details","candidates":[{"strategy":"css","selector":"a[href='/details/1']","pre_score":1,"pre_features":{"verified_href":"/details/1"}}],"postconditions":[{"type":"url_contains","value":"/details/1","timeout_ms":5000}]}]}`)
	if _, _, err := ValidateCase(valid); err != nil {
		t.Fatalf("ValidateCase(valid) error = %v", err)
	}
}

func TestResearchCanonicalContractGolden(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "dsl_research_v1_contract.json"))
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Input            json.RawMessage `json:"input"`
		CanonicalVersion string          `json:"canonical_version"`
		Profile          Profile         `json:"profile"`
		CanonicalJSON    string          `json:"canonical_json"`
		SHA256           string          `json:"sha256"`
	}
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	validated, err := ValidateExecutableCase(fixture.Input)
	if err != nil {
		t.Fatalf("ValidateExecutableCase() error = %v", err)
	}
	if string(validated.CanonicalJSON) != fixture.CanonicalJSON {
		t.Fatalf(
			"canonical JSON mismatch\n got: %s\nwant: %s",
			validated.CanonicalJSON,
			fixture.CanonicalJSON,
		)
	}
	if hash := SHA256(validated.CanonicalJSON); hash != fixture.SHA256 {
		t.Fatalf("SHA256() = %s, want %s", hash, fixture.SHA256)
	}
	if validated.CanonicalVersion != fixture.CanonicalVersion ||
		validated.Profile != fixture.Profile {
		t.Fatalf(
			"contract = (%s, %s), want (%s, %s)",
			validated.Profile,
			validated.CanonicalVersion,
			fixture.Profile,
			fixture.CanonicalVersion,
		)
	}
	revalidated, err := ValidateExecutableCase(validated.CanonicalJSON)
	if err != nil {
		t.Fatalf("revalidate canonical research case: %v", err)
	}
	if string(revalidated.CanonicalJSON) != string(validated.CanonicalJSON) {
		t.Fatalf(
			"research canonicalization is not idempotent\nfirst:  %s\nsecond: %s",
			validated.CanonicalJSON,
			revalidated.CanonicalJSON,
		)
	}
}

func TestResearchDraftRejectsMissingUnknownAndSelectorFields(t *testing.T) {
	tests := []string{
		`{"profile":"research-v1","name":"missing intent","steps":[{"action":"wait_for","target":"Ready","preconditions":[],"postconditions":[],"idempotency":"idempotent","side_effect":"none"}]}`,
		`{"profile":"research-v1","name":"missing conditions","steps":[{"action":"assert_url_contains","intent":"Check URL","target":"Current URL","value":"/done","idempotency":"idempotent","side_effect":"none"}]}`,
		`{"profile":"research-v1","name":"unknown case","unexpected":true,"steps":[{"action":"assert_url_contains","intent":"Check URL","target":"Current URL","value":"/done","preconditions":[],"postconditions":[],"idempotency":"idempotent","side_effect":"none"}]}`,
		`{"profile":"research-v1","name":"unknown condition","steps":[{"action":"assert_url_contains","intent":"Check URL","target":"Current URL","value":"/done","preconditions":[{"type":"url_contains","value":"/","unexpected":true}],"postconditions":[],"idempotency":"idempotent","side_effect":"none"}]}`,
		`{"profile":"research-v1","name":"selector","steps":[{"action":"click","intent":"Click login","target":"Login button","selector":"#login","preconditions":[{"type":"element_visible","value":"Login button"}],"postconditions":[{"type":"url_changes"}],"idempotency":"idempotent","side_effect":"browser_state"}]}`,
		`{"profile":"research-v1","name":"candidates","steps":[{"action":"click","intent":"Click login","target":"Login button","candidates":[],"preconditions":[{"type":"element_visible","value":"Login button"}],"postconditions":[{"type":"url_changes"}],"idempotency":"idempotent","side_effect":"browser_state"}]}`,
	}
	for _, raw := range tests {
		if _, err := ValidateDraftCase(json.RawMessage(raw)); err == nil {
			t.Fatalf("ValidateDraftCase(%s) error = nil", raw)
		}
	}
}

func TestResearchDraftAndExecutablePhases(t *testing.T) {
	draft := json.RawMessage(`{
		"profile":"research-v1",
		"name":"Login",
		"steps":[{
			"action":"click",
			"intent":"Submit credentials",
			"target":"Login button",
			"page_state":"login",
			"preconditions":[{"type":"element_visible","value":"Login button"}],
			"postconditions":[{"type":"url_contains","value":"/home"}],
			"idempotency":"non_idempotent",
			"side_effect":"browser_state"
		}]
	}`)
	validatedDraft, err := ValidateDraftCase(draft)
	if err != nil {
		t.Fatalf("ValidateDraftCase() error = %v", err)
	}
	if validatedDraft.Profile != ProfileResearchV1 ||
		validatedDraft.CanonicalVersion != CanonicalVersionV2 {
		t.Fatalf("draft contract = %#v", validatedDraft)
	}
	if _, err := ValidateExecutableCase(validatedDraft.CanonicalJSON); err == nil {
		t.Fatal("ValidateExecutableCase(draft) error = nil")
	}

	executable := json.RawMessage(`{
		"profile":"research-v1",
		"name":"Login",
		"steps":[{
			"action":"click",
			"intent":"Submit credentials",
			"target":"Login button",
			"page_state":"login",
			"preconditions":[{"type":"element_visible","value":"Login button"}],
			"postconditions":[{"type":"url_contains","value":"/home"}],
			"idempotency":"non_idempotent",
			"side_effect":"browser_state",
			"locator_confidence":"high",
			"candidates":[{
				"strategy":"verified_css",
				"selector":"#login",
				"semantic_value":"Login",
				"pre_score":1,
				"pre_features":{"verified":true,"source":"a11y_backend_dom_node"}
			}]
		}]
	}`)
	if _, err := ValidateExecutableCase(executable); err != nil {
		t.Fatalf("ValidateExecutableCase() error = %v", err)
	}
}

func TestResearchDraftAllowsExplicitQuantityTwo(t *testing.T) {
	draft := json.RawMessage(`{
		"profile":"research-v1",
		"name":"Add two Blue Tops",
		"steps":[
			{
				"action":"input",
				"intent":"Set the requested product quantity",
				"target":"Quantity",
				"value":"2",
				"preconditions":[{"type":"element_visible","value":"#quantity"}],
				"postconditions":[{"type":"value_changed","value":"2"}],
				"idempotency":"idempotent",
				"side_effect":"browser_state"
			},
			{
				"action":"click",
				"intent":"Add the requested quantity to the cart",
				"target":"Add to cart",
				"preconditions":[{"type":"element_visible","value":"button.cart"}],
				"postconditions":[{"type":"text_visible","value":"Added!"}],
				"idempotency":"non_idempotent",
				"side_effect":"external_state"
			}
		]
	}`)

	if _, err := ValidateDraftCase(draft); err != nil {
		t.Fatalf("ValidateDraftCase(quantity=2) error = %v", err)
	}
}

func TestResearchExecutableRejectsUnverifiedCandidate(t *testing.T) {
	tests := []string{
		`{
			"profile":"research-v1","name":"Login","steps":[{
				"action":"wait_for","intent":"Wait for login","target":"Login button",
				"preconditions":[],"postconditions":[],
				"idempotency":"idempotent","side_effect":"none",
				"locator_confidence":"medium",
				"candidates":[{
					"strategy":"role_fuzzy","selector":"button","semantic_value":"Login",
					"pre_score":0.75,"pre_features":{"source":"a11y_role_fuzzy"}
				}]
			}]
		}`,
		`{
			"profile":"research-v1","name":"Login","steps":[{
				"action":"wait_for","intent":"Wait for login","target":"Login button",
				"preconditions":[],"postconditions":[],
				"idempotency":"idempotent","side_effect":"none",
				"locator_confidence":"high",
				"candidates":[{
					"strategy":"verified_css","selector":"#login","semantic_value":"Login",
					"pre_score":1,"pre_features":{"verified":true},"unexpected":true
				}]
			}]
		}`,
		`{
			"profile":"research-v1","name":"Login","steps":[{
				"action":"wait_for","intent":"Wait for login","target":"Login button",
				"preconditions":[],"postconditions":[],
				"idempotency":"idempotent","side_effect":"none",
				"locator_confidence":"high",
				"candidates":[{
					"strategy":"verified_css","selector":"#login","semantic_value":"Login",
					"pre_score":1,
					"pre_features":{"verified":true,"source":"user_claim"}
				}]
			}]
		}`,
		`{
			"profile":"research-v1","name":"Login","steps":[{
				"action":"wait_for","intent":"Wait for login","target":"Login button",
				"preconditions":[],"postconditions":[],
				"idempotency":"idempotent","side_effect":"none",
				"locator_confidence":"high",
				"candidates":[{
					"strategy":"invented_selector","selector":"#login","semantic_value":"Login",
					"pre_score":1,
					"pre_features":{"verified":true,"source":"a11y_backend_dom_node"}
				}]
			}]
		}`,
	}
	for _, raw := range tests {
		if _, err := ValidateExecutableCase(json.RawMessage(raw)); err == nil {
			t.Fatalf("ValidateExecutableCase(%s) error = nil", raw)
		}
	}
}

func TestResearchExecutableAcceptsRealPreflightCandidateShape(t *testing.T) {
	raw := json.RawMessage(`{
		"profile":"research-v1","name":"Login","steps":[{
			"action":"click","intent":"Click login","target":"Login button",
			"page_state":"S0",
			"preconditions":[{"type":"element_visible","value":"Login button"}],
			"postconditions":[{"type":"url_changes"}],
			"idempotency":"idempotent","side_effect":"browser_state",
			"locator_confidence":"high",
			"candidates":[
				{
					"strategy":"verified_css","selector":"#login",
					"semantic_value":"Login","pre_score":1,
					"pre_features":{
						"verified":true,"source":"a11y_backend_dom_node",
						"verified_href":null
					}
				},
				{
					"strategy":"role","selector":"button",
					"semantic_value":"Login","pre_score":0.9,
					"pre_features":{"verified":true,"source":"a11y_role_exact"}
				}
			]
		}]
	}`)
	if _, err := ValidateExecutableCase(raw); err != nil {
		t.Fatalf("ValidateExecutableCase() error = %v", err)
	}
}

func TestResearchExecutableAllowsRuntimeTextVerificationWithoutCandidates(t *testing.T) {
	raw := json.RawMessage(`{
		"profile":"research-v1","name":"Verify text","steps":[{
			"action":"wait_for","intent":"Wait for runtime text","target":"Rs. 400",
			"preconditions":[],"postconditions":[],
			"idempotency":"idempotent","side_effect":"none",
			"locator_confidence":"medium","candidates":[]
		}]
	}`)
	if _, err := ValidateExecutableCase(raw); err != nil {
		t.Fatalf("ValidateExecutableCase(wait_for runtime text) error = %v", err)
	}
}

func TestResearchExecutableStillRequiresCandidatesForInteractiveSteps(t *testing.T) {
	raw := json.RawMessage(`{
		"profile":"research-v1","name":"Click","steps":[{
			"action":"click","intent":"Click submit","target":"Submit",
			"preconditions":[{"type":"url_contains","value":"/form"}],
			"postconditions":[{"type":"url_changes"}],
			"idempotency":"idempotent","side_effect":"browser_state",
			"locator_confidence":"medium","candidates":[]
		}]
	}`)
	if _, err := ValidateExecutableCase(raw); err == nil {
		t.Fatal("ValidateExecutableCase(click without candidates) error = nil")
	}
}

func TestResearchActionSemantics(t *testing.T) {
	rejected := []string{
		`{"profile":"research-v1","name":"goto","steps":[{"action":"goto","intent":"Open checkout","target":"Checkout page","value":"/checkout","preconditions":[],"postconditions":[],"idempotency":"idempotent","side_effect":"browser_state"}]}`,
		`{"profile":"research-v1","name":"input","steps":[{"action":"input","intent":"Enter query","target":"Search field","value":"dress","preconditions":[{"type":"element_visible","value":"Search field"}],"postconditions":[{"type":"value_changed","value":"dress"}],"idempotency":"non_idempotent","side_effect":"browser_state"}]}`,
		`{"profile":"research-v1","name":"wait","steps":[{"action":"wait_for","intent":"Wait for result","target":"Result","preconditions":[],"postconditions":[],"idempotency":"non_idempotent","side_effect":"none"}]}`,
		`{"profile":"research-v1","name":"url","steps":[{"action":"assert_url_contains","intent":"Check URL","value":"/done","preconditions":[],"postconditions":[],"idempotency":"idempotent","side_effect":"none"}]}`,
	}
	for _, raw := range rejected {
		if _, err := ValidateDraftCase(json.RawMessage(raw)); err == nil {
			t.Fatalf("ValidateDraftCase(%s) error = nil", raw)
		}
	}

	gotoStep := json.RawMessage(`{
		"profile":"research-v1","name":"goto","steps":[{
			"action":"goto","intent":"Open checkout","target":"Checkout page",
			"value":"/checkout","preconditions":[],
			"postconditions":[{"type":"url_contains","value":"/checkout"}],
			"idempotency":"idempotent","side_effect":"browser_state"
		}]
	}`)
	if _, err := ValidateDraftCase(gotoStep); err != nil {
		t.Fatalf("ValidateDraftCase(goto) error = %v", err)
	}

	click := json.RawMessage(`{
		"profile":"research-v1","name":"purchase","steps":[{
			"action":"click","intent":"Place order","target":"Place order button",
			"preconditions":[{"type":"element_visible","value":"Place order button"}],
			"postconditions":[{"type":"text_visible","value":"Confirmation"}],
			"idempotency":"non_idempotent","side_effect":"external_state"
		}]
	}`)
	if _, err := ValidateDraftCase(click); err != nil {
		t.Fatalf("ValidateDraftCase(click) error = %v", err)
	}
}
