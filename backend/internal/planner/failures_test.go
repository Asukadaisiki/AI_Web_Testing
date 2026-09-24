package planner

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/contract"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/worker"
)

func TestEquivalentReplayPageHasSameFingerprint(t *testing.T) {
	searchValue := "widget"
	first := contract.Observation{
		ObservationID: "obs_random_1",
		PageStateID:   "page_random_1",
		URL:           " HTTPS://SHOP.TEST:443/products?b=2&a=1#results ",
		Title:         "  All   Products ",
		ActionCandidates: []contract.ActionCandidate{
			{CandidateID: "candidate-b"},
			{CandidateID: "candidate-a"},
		},
		Elements: []contract.Element{
			{Ref: "hidden", Visible: false, Value: stringPointer("ignored")},
			{
				Ref: "control-17", Tag: "input", Role: "searchbox", Name: "Search",
				Visible: true, Value: &searchValue,
				Locators: []contract.Locator{{
					Kind: "role", Role: "searchbox", Name: "Search", Exact: true, MatchCount: 1,
				}},
			},
		},
		Blockers: []contract.Blocker{
			{Kind: "blocked_by_overlay"},
			{Kind: "blocked_by_dialog"},
		},
	}
	replayed := contract.Observation{
		ObservationID: "obs_random_2",
		PageStateID:   "page_random_2",
		URL:           "https://shop.test/products?a=1&b=2#results",
		Title:         "All Products",
		ActionCandidates: []contract.ActionCandidate{
			{CandidateID: "candidate-a"},
			{CandidateID: "candidate-b"},
		},
		Elements: []contract.Element{
			{
				Ref: "control-93", Tag: "input", Role: "searchbox", Name: "Search",
				Visible: true, Value: &searchValue,
				Locators: []contract.Locator{{
					Kind: "role", Role: "searchbox", Name: "Search", Exact: true, MatchCount: 7,
				}},
			},
			{Ref: "hidden", Visible: false, Value: stringPointer("changed but hidden")},
		},
		Blockers: []contract.Blocker{
			{Kind: "blocked_by_dialog"},
			{Kind: "blocked_by_overlay"},
		},
	}

	firstFingerprint := PageFingerprint(first)
	replayedFingerprint := PageFingerprint(replayed)
	if firstFingerprint == "" {
		t.Fatal("fingerprint must not be empty")
	}
	if firstFingerprint != replayedFingerprint {
		t.Fatalf("equivalent pages differ: %q != %q", firstFingerprint, replayedFingerprint)
	}
}

func TestFailureTargetKeyPrefersExplicitCandidateID(t *testing.T) {
	spec := &contract.TargetSpec{
		Object: contract.TargetObject{Name: "ignored"},
		Scope:  &contract.TargetScope{Ref: "ignored"},
	}

	if got := failureTargetKey("  candidate-7  ", spec, "ignored"); got != "candidate-7" {
		t.Fatalf("target key = %q, want explicit candidate id", got)
	}
}

func TestNormalizedHintDuplicateIsRejectedBeforeWorkerCall(t *testing.T) {
	observation := failureTestObservation("control-17", "widget")
	planner, workerCalls := failureTestPlanner(t, observation)
	planner.failures = []FailureSignature{{
		PageFingerprint: PageFingerprint(observation),
		Action:          contract.ActionClick,
		TargetKey:       "widget",
		ErrorCode:       "condition_unmet",
	}}

	result, err := planner.click(
		context.Background(),
		"  WIDGET \n",
		nil,
		"",
		"open widget",
		contract.Expects{Element: stringPointer("visible")},
	)
	if err != nil {
		t.Fatalf("click: %v", err)
	}
	if result.Error != CodeStrategyRepeated {
		t.Fatalf("click result = %+v, want %s", result, CodeStrategyRepeated)
	}
	if *workerCalls != 0 {
		t.Fatalf("worker calls = %d, want 0", *workerCalls)
	}
}

func TestSemanticallyEquivalentStructuredSpecDuplicateIsRejected(t *testing.T) {
	observation := failureTestObservation("control-17", "buy now")
	observation.Elements[0].Role = "button"
	observation.Elements[0].Name = "Add to cart"
	observation.Elements[0].ContainerRef = "scope-7"
	observation.Elements[0].Locators = []contract.Locator{{
		Kind: "role", Role: "button", Name: "Add to cart", Exact: true, MatchCount: 1,
	}}
	observation.Structures = []contract.StructureNode{{
		Ref: "scope-7", Kind: "card", FullText: "Blue Top Buy now", Visible: true,
	}}
	planner, workerCalls := failureTestPlanner(t, observation)
	planner.failures = []FailureSignature{{
		PageFingerprint: PageFingerprint(observation),
		Action:          contract.ActionClick,
		TargetKey: `{"object":{"role":"button","text":"buy now","name":"add to cart",` +
			`"aliases":["add","buy"]},"scope":{"kind":"card","contains_text":"blue top",` +
			`"ref":"scope-7"},"relation":"within"}`,
		ErrorCode: "condition_unmet",
	}}
	spec := &contract.TargetSpec{
		Object: contract.TargetObject{
			Text:    " Buy   Now ",
			Aliases: []string{" BUY ", "add", "buy"},
		},
		Scope: &contract.TargetScope{
			Kind: "card", ContainsText: "Blue Top", Ref: "scope-7",
		},
		Relation: " WITHIN ",
		Role:     "button",
		Name:     "Add to cart",
	}

	result, err := planner.click(
		context.Background(),
		"",
		spec,
		"",
		"buy item",
		contract.Expects{Element: stringPointer("visible")},
	)
	if err != nil {
		t.Fatalf("click: %v", err)
	}
	if result.Error != CodeStrategyRepeated {
		t.Fatalf("click result = %+v, want %s", result, CodeStrategyRepeated)
	}
	if *workerCalls != 0 {
		t.Fatalf("worker calls = %d, want 0", *workerCalls)
	}
}

func TestRealSemanticPageChangeAllowsHintRetry(t *testing.T) {
	before := failureTestObservation("control-17", "before")
	after := failureTestObservation("control-93", "after")
	planner, workerCalls := failureTestPlanner(t, after)
	planner.failures = []FailureSignature{{
		PageFingerprint: PageFingerprint(before),
		Action:          contract.ActionClick,
		TargetKey:       "widget",
		ErrorCode:       "condition_unmet",
	}}

	result, err := planner.click(
		context.Background(),
		"Widget",
		nil,
		"",
		"retry widget",
		contract.Expects{Element: stringPointer("visible")},
	)
	if err != nil {
		t.Fatalf("click: %v", err)
	}
	if !result.OK {
		t.Fatalf("click result = %+v, want worker retry", result)
	}
	if *workerCalls != 1 {
		t.Fatalf("worker calls = %d, want 1", *workerCalls)
	}
}

func TestChangedURLFragmentChangesFingerprint(t *testing.T) {
	before := contract.Observation{URL: "https://shop.test/products#list", Title: "Products"}
	after := contract.Observation{URL: "https://shop.test/products#details", Title: "Products"}

	if PageFingerprint(before) == PageFingerprint(after) {
		t.Fatal("changing a URL fragment must change the page fingerprint")
	}
}

func TestChangedInputValueChangesFingerprint(t *testing.T) {
	before := contract.Observation{
		URL:   "https://shop.test/products",
		Title: "Products",
		Elements: []contract.Element{{
			Ref: "search", Tag: "input", Role: "searchbox", Name: "Search",
			Visible: true, Value: stringPointer("widget"),
			Locators: []contract.Locator{{
				Kind: "role", Role: "searchbox", Name: "Search", Exact: true, MatchCount: 1,
			}},
		}},
	}
	after := before
	after.Elements = append([]contract.Element(nil), before.Elements...)
	after.Elements[0].Value = stringPointer("blue top")

	if PageFingerprint(before) == PageFingerprint(after) {
		t.Fatal("changing a visible control value must change the page fingerprint")
	}
}

func TestRepeatedFailureLedgerRetainsNewestEightUniqueSignatures(t *testing.T) {
	planner := &Planner{}
	for index := 0; index < 10; index++ {
		planner.rememberFailure(FailureSignature{
			PageFingerprint: fmt.Sprintf("page-%d", index),
			Action:          contract.ActionClick,
			TargetKey:       "candidate",
			ErrorCode:       "condition_unmet",
		})
	}

	if len(planner.failures) != 8 {
		t.Fatalf("ledger length = %d, want 8", len(planner.failures))
	}
	if planner.failures[0].PageFingerprint != "page-2" ||
		planner.failures[7].PageFingerprint != "page-9" {
		t.Fatalf("ledger does not retain the newest entries: %#v", planner.failures)
	}

	newest := planner.failures[3]
	planner.rememberFailure(newest)
	if len(planner.failures) != 8 {
		t.Fatalf("remembering a duplicate changed ledger length to %d", len(planner.failures))
	}
	if planner.failures[7] != newest {
		t.Fatalf("duplicate was not moved to newest position: %#v", planner.failures)
	}
}

func stringPointer(value string) *string {
	return &value
}

func failureTestObservation(ref, controlValue string) contract.Observation {
	return contract.Observation{
		ObservationID: "observation-" + ref,
		PageStateID:   "page-" + ref,
		URL:           "https://shop.test/products",
		Title:         "Products",
		Elements: []contract.Element{{
			Ref: ref, Tag: "a", Role: "link", Name: "Widget", Text: "Widget",
			Value: stringPointer(controlValue), Visible: true, Enabled: true,
			Locators: []contract.Locator{{
				Kind: "role", Role: "link", Name: "Widget", Exact: true, MatchCount: 1,
			}},
		}},
	}
}

func failureTestPlanner(
	t *testing.T, observation contract.Observation,
) (*Planner, *int) {
	t.Helper()
	workerCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		workerCalls++
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(contract.ActResponse{
			Status: "passed", Observation: observation,
		}); err != nil {
			t.Errorf("encode worker response: %v", err)
		}
	}))
	t.Cleanup(server.Close)
	return &Planner{
		client:           worker.New(server.URL),
		sessionID:        "failure-test",
		browserSessionID: "browser-test",
		observation:      observation,
		hasPage:          true,
	}, &workerCalls
}
