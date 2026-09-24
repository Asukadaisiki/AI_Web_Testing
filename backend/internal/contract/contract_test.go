package contract

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fixtureFile 是 Go 与 Python 共读的契约一致性夹具。
type fixtureFile struct {
	Version         string                 `json:"version"`
	Cases           []fixtureCase          `json:"cases"`
	AuthoringAction fixtureAuthoringAction `json:"authoring_action"`
}

type fixtureAuthoringAction struct {
	Request  json.RawMessage `json:"request"`
	Response json.RawMessage `json:"response"`
}

type fixtureCase struct {
	Name   string         `json:"name"`
	Expect fixtureExpect  `json:"expect"`
	Case   map[string]any `json:"case"`
}

type fixtureExpect struct {
	Accept bool   `json:"accept"`
	Code   string `json:"code"`
}

func loadFixtures(t *testing.T) fixtureFile {
	t.Helper()
	path := filepath.Join("..", "..", "..", "fixtures", "contract", "case_contract.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixtures: %v", err)
	}
	var parsed fixtureFile
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("parse fixtures: %v", err)
	}
	if len(parsed.Cases) == 0 {
		t.Fatal("fixtures must not be empty")
	}
	return parsed
}

// TestContractFixtures 是契约一致性的 Go 侧实现。
// Python 侧读取同一个文件，两侧必须给出相同的 accept 与 code。
func TestContractFixtures(t *testing.T) {
	fixtures := loadFixtures(t)
	if fixtures.Version != CaseVersion {
		t.Fatalf("fixtures version = %q, want %q", fixtures.Version, CaseVersion)
	}
	for _, item := range fixtures.Cases {
		item := item
		t.Run(item.Name, func(t *testing.T) {
			raw, err := json.Marshal(item.Case)
			if err != nil {
				t.Fatalf("marshal fixture: %v", err)
			}
			_, err = Validate(raw)
			if item.Expect.Accept {
				if err != nil {
					t.Fatalf("expected accept, got %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("expected reject, got accept")
			}
			if code := CodeOf(err); code != item.Expect.Code {
				t.Fatalf("code = %q, want %q (error: %v)", code, item.Expect.Code, err)
			}
		})
	}
}

func TestAuthoringActionFixtureMatchesGoContract(t *testing.T) {
	fixture := loadFixtures(t).AuthoringAction
	var request ActRequest
	if err := json.Unmarshal(fixture.Request, &request); err != nil {
		t.Fatalf("decode authoring request: %v", err)
	}
	if request.Action != ActionAssertElement {
		t.Fatalf("request action = %q, want %q", request.Action, ActionAssertElement)
	}
	if len(request.Postconditions) != 1 || request.Postconditions[0].Type != CondTextVisible {
		t.Fatalf("request postconditions = %+v", request.Postconditions)
	}

	var response ActResponse
	if err := json.Unmarshal(fixture.Response, &response); err != nil {
		t.Fatalf("decode authoring response: %v", err)
	}
	if response.Status != "failed" || response.Observation.ObservationID != "obs_authoring" {
		t.Fatalf("authoring response = %+v", response)
	}
	if len(response.Conditions) != 1 || response.Conditions[0].Satisfied {
		t.Fatalf("response conditions = %+v", response.Conditions)
	}
	if response.Error == nil || response.Error.Kind != SignalConditionUnmet {
		t.Fatalf("response error = %+v", response.Error)
	}
}

func TestValidateRejectsTransitionPreconditionWithActionableMessage(t *testing.T) {
	_, err := Validate([]byte(`{
		"case_version":"loop.case.v1","name":"x","base_url":"https://shop.test",
		"steps":[{"index":0,"action":"goto","intent":"open","value":"https://shop.test/a",
			"preconditions":[],"postconditions":[{"type":"url_contains","value":"/a"}],"timeout_ms":5000},
		{"index":1,"action":"click","intent":"click","target":{"hint":"Go",
			"locator":{"kind":"role","role":"button","name":"Go","exact":true,"match_count":1},
			"grounding":{"observation_id":"o","page_state_id":"p","candidate_id":"c","page_url":"https://shop.test/a"}},
			"preconditions":[{"type":"value_equals","value":"1"}],
			"postconditions":[{"type":"text_visible","value":"Done"}],"timeout_ms":5000}]}`))
	if err == nil {
		t.Fatal("expected reject")
	}
	if code := CodeOf(err); code != CodeConditionPhase {
		t.Fatalf("code = %q, want %q", code, CodeConditionPhase)
	}
	message := err.Error()
	for _, want := range []string{"value_equals", "url_contains", "text_visible", "text_gone"} {
		if !strings.Contains(message, want) {
			t.Fatalf("message %q must mention %q", message, want)
		}
	}
}

// TestDerivedStepsAlwaysSatisfyTheContract 证明"构建即校验"：
// 派生函数本身不会产出过不了契约的步骤。
func TestDerivedStepsAlwaysSatisfyTheContract(t *testing.T) {
	pageURL := "https://shop.test/products"
	expectText := "Added"
	expectURL := "/view_cart"
	expectValue := "2"

	gotoStep, err := DeriveGotoStep(0, "open the list", pageURL)
	if err != nil {
		t.Fatalf("goto: %v", err)
	}
	target := Target{
		Hint:    "Add to cart",
		Locator: Locator{Kind: "role", Role: "button", Name: "Add to cart", Exact: true, MatchCount: 1},
		Grounding: Grounding{
			ObservationID: "obs_1",
			PageStateID:   "ps_1",
			CandidateID:   "e5:0",
			PageURL:       pageURL,
		},
	}
	clickStep, err := DeriveActionStep(
		1, ActionClick, "add it", target, nil, false, pageURL,
		Expects{Text: &expectText, URL: &expectURL},
	)
	if err != nil {
		t.Fatalf("click: %v", err)
	}
	inputStep, err := DeriveActionStep(
		2, ActionInput, "set quantity", target, strPtr("2"), false, pageURL,
		Expects{Value: &expectValue},
	)
	if err != nil {
		t.Fatalf("input: %v", err)
	}
	assertStep, err := DeriveAssertTextStep(3, "check it", "Widget", pageURL)
	if err != nil {
		t.Fatalf("assert_text: %v", err)
	}
	assertURLStep, err := DeriveAssertURLStep(4, "check the cart", "/view_cart", pageURL)
	if err != nil {
		t.Fatalf("assert_url: %v", err)
	}

	derived := Case{
		CaseVersion: CaseVersion,
		Name:        "derived",
		Goal:        "derived",
		BaseURL:     "https://shop.test",
		Steps:       []Step{gotoStep, clickStep, inputStep, assertStep, assertURLStep},
	}
	if err := derived.Validate(); err != nil {
		t.Fatalf("derived case must satisfy the contract: %v", err)
	}
	// 首步无前置条件，后续步骤的前置条件来自观测页锚点，天然可满足。
	if len(derived.Steps[0].Preconditions) != 0 {
		t.Fatalf("goto step preconditions = %#v, want empty", derived.Steps[0].Preconditions)
	}
	for index := 1; index < len(derived.Steps); index++ {
		conditions := derived.Steps[index].Preconditions
		if len(conditions) != 1 || conditions[0].Type != CondURLContains || conditions[0].Value != "/products" {
			t.Fatalf("step %d preconditions = %#v", index, conditions)
		}
	}
}

func TestDeriveActionStepRequiresAnExpectation(t *testing.T) {
	target := Target{
		Hint:    "Go",
		Locator: Locator{Kind: "text", Text: "Go", Exact: true, MatchCount: 1},
		Grounding: Grounding{
			ObservationID: "o", PageStateID: "p", CandidateID: "c", PageURL: "https://shop.test/a",
		},
	}
	if _, err := DeriveActionStep(1, ActionClick, "click", target, nil, false, "https://shop.test/a", Expects{}); err == nil {
		t.Fatal("expected an error when no expectation is declared")
	}
}

func TestGenericV2ActionsAndAssertionsSatisfyTheContract(t *testing.T) {
	pageURL := "https://app.test/settings"
	expectText := "Saved"
	expectAttr := "data-state=ready"
	expectCount := "3"
	target := Target{
		Hint:    "Status",
		Locator: Locator{Kind: "role", Role: "combobox", Name: "Status", Exact: true, MatchCount: 1},
		Grounding: Grounding{
			ObservationID: "o", PageStateID: "p", CandidateID: "c", PageURL: pageURL,
		},
	}

	for _, item := range []struct {
		action Action
		value  *string
	}{
		{ActionSelect, strPtr("Active")},
		{ActionCheck, nil},
		{ActionUncheck, nil},
		{ActionScrollIntoView, nil},
		{ActionHover, nil},
		{ActionDismissDialog, nil},
		{ActionUploadFile, strPtr("/tmp/avatar.png")},
	} {
		if _, err := DeriveActionStep(
			1, item.action, "generic action", target, item.value, false, pageURL,
			Expects{Text: &expectText},
		); err != nil {
			t.Fatalf("%s should satisfy contract: %v", item.action, err)
		}
	}
	if _, err := DeriveActionStep(
		1, ActionAssertElement, "element visible", target, nil, false, pageURL,
		Expects{Element: strPtr("visible")},
	); err != nil {
		t.Fatalf("assert_element should satisfy contract: %v", err)
	}
	if _, err := DeriveActionStep(
		1, ActionAssertAttribute, "attribute ready", target, nil, false, pageURL,
		Expects{Attribute: &expectAttr},
	); err != nil {
		t.Fatalf("assert_attribute should satisfy contract: %v", err)
	}
	if _, err := DeriveActionStep(
		1, ActionAssertCount, "three rows", target, nil, false, pageURL,
		Expects{Count: &expectCount},
	); err != nil {
		t.Fatalf("assert_count should satisfy contract: %v", err)
	}
}

func TestSubmitIsOnlyAllowedOnInput(t *testing.T) {
	target := Target{
		Hint:    "Search items",
		Locator: Locator{Kind: "role", Role: "searchbox", Name: "Search items", Exact: true, MatchCount: 1},
		Grounding: Grounding{
			ObservationID: "o", PageStateID: "p", CandidateID: "c", PageURL: "https://shop.test/a",
		},
	}
	expectURL := "/search"

	// input 可以带 submit：填完按回车提交。
	step, err := DeriveActionStep(
		1, ActionInput, "search", target, strPtr("alpha"), true, "https://shop.test/a",
		Expects{URL: &expectURL},
	)
	if err != nil {
		t.Fatalf("input with submit: %v", err)
	}
	if !step.Submit {
		t.Fatal("input step must carry submit")
	}

	// click 带 submit 必须被拒：回车只对"填值"有意义。
	_, err = DeriveActionStep(
		1, ActionClick, "click", target, nil, true, "https://shop.test/a",
		Expects{URL: &expectURL},
	)
	if err == nil {
		t.Fatal("expected click with submit to be rejected")
	}
	if code := CodeOf(err); code != CodeUnexpectedSubmit {
		t.Fatalf("code = %q, want %q", code, CodeUnexpectedSubmit)
	}

	// 契约本身也要挡住：绕过 Derive* 手写一个带 submit 的 click 同样非法。
	manual := Step{
		Index:         0,
		Action:        ActionClick,
		Intent:        "click",
		Target:        &target,
		Submit:        true,
		Preconditions: []Condition{{Type: CondURLContains, Value: "shop.test", TimeoutMS: 1000}},
		Postconditions: []Condition{
			{Type: CondURLContains, Value: "/search", TimeoutMS: 1000},
		},
		TimeoutMS: 5000,
	}
	if code := CodeOf(ValidateStep(0, manual)); code != CodeUnexpectedSubmit {
		t.Fatalf("manual click+submit code = %q, want %q", code, CodeUnexpectedSubmit)
	}
}

func TestPageAnchorFallsBackToHostForRootPath(t *testing.T) {
	if got := PageAnchor("https://shop.test/"); got != "shop.test" {
		t.Fatalf("root anchor = %q", got)
	}
	if got := PageAnchor("https://shop.test/products?search=x"); got != "/products" {
		t.Fatalf("path anchor = %q", got)
	}
}

func strPtr(value string) *string { return &value }
