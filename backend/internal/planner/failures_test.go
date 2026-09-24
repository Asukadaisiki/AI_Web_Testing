package planner

import (
	"fmt"
	"testing"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/contract"
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
			{Ref: "search", Tag: "input", Role: "searchbox", Name: "Search", Visible: true, Value: &searchValue},
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
			{Ref: "search", Tag: "input", Role: "searchbox", Name: "Search", Visible: true, Value: &searchValue},
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
