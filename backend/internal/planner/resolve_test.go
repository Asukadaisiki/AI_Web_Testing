package planner

import (
	"fmt"
	"strings"
	"testing"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/contract"
)

func element(ref, role, name, text string, locators ...contract.Locator) contract.Element {
	return contract.Element{
		Ref: ref, Tag: "div", Role: role, Name: name, Text: text,
		Visible: true, Enabled: true, Locators: locators,
	}
}

func roleLocator(role, name string) contract.Locator {
	return contract.Locator{Kind: "role", Role: role, Name: name, Exact: true, MatchCount: 1}
}

func textLocator(text string) contract.Locator {
	return contract.Locator{Kind: "text", Text: text, Exact: true, MatchCount: 1}
}

func cssLocator(css string) contract.Locator {
	return contract.Locator{Kind: "css", CSS: css, MatchCount: 1}
}

func TestResolvePrefersExactAccessibleName(t *testing.T) {
	elements := []contract.Element{
		element("e1", "button", "Add to cart", "Add", roleLocator("button", "Add to cart")),
		element("e2", "button", "Add", "Add", roleLocator("button", "Add")),
	}
	winner, locator, _, err := resolve("Add", elements)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	// "Add" 与 e2 的可访问名完全相等，得分最高。
	if winner.Ref != "e2" {
		t.Fatalf("winner = %q, want e2", winner.Ref)
	}
	if locator != roleLocator("button", "Add") {
		t.Fatalf("locator = %#v", locator)
	}
}

func TestResolveIsCaseAndWhitespaceInsensitive(t *testing.T) {
	elements := []contract.Element{
		element("e1", "button", "Add  to Cart", "Add to Cart", roleLocator("button", "Add to Cart")),
	}
	if _, _, _, err := resolve("  add TO cart ", elements); err != nil {
		t.Fatalf("resolve must normalize case and whitespace: %v", err)
	}
}

func TestResolveRejectsAmbiguousHints(t *testing.T) {
	elements := []contract.Element{
		element("e1", "link", "Widget", "Widget", textLocator("Widget")),
		element("e2", "link", "Widget", "Widget", textLocator("Widget")),
	}
	_, _, candidates, err := resolve("Widget", elements)
	if err == nil {
		t.Fatal("an ambiguous hint must be rejected")
	}
	if code := errorCode(err); code != CodeTargetAmbiguous {
		t.Fatalf("code = %q, want %q", code, CodeTargetAmbiguous)
	}
	if len(candidates) != 2 {
		t.Fatalf("candidates = %#v", candidates)
	}
	if !strings.Contains(err.Error(), "more specific") {
		t.Fatalf("message must tell the model what to do: %v", err)
	}
}

func TestResolveRejectsUnknownHintsAndOffersCandidates(t *testing.T) {
	elements := []contract.Element{
		element("e1", "button", "Add to cart", "Add to cart", roleLocator("button", "Add to cart")),
		element("e2", "button", "Checkout", "Checkout", roleLocator("button", "Checkout")),
	}
	_, _, candidates, err := resolve("Buy now", elements)
	if err == nil {
		t.Fatal("an unknown hint must be rejected")
	}
	if code := errorCode(err); code != CodeTargetNotFound {
		t.Fatalf("code = %q, want %q", code, CodeTargetNotFound)
	}
	// 候选要让模型有东西可选，否则它只能瞎猜。
	if len(candidates) == 0 {
		t.Fatal("a not-found hint must come with candidates")
	}
}

func TestResolveSkipsInvisibleAndDisabledElements(t *testing.T) {
	hidden := element("e1", "button", "Submit", "Submit", roleLocator("button", "Submit"))
	hidden.Visible = false
	disabled := element("e2", "button", "Submit", "Submit", roleLocator("button", "Submit"))
	disabled.Enabled = false
	usable := element("e3", "button", "Submit", "Submit", roleLocator("button", "Submit"))

	winner, _, _, err := resolve("Submit", []contract.Element{hidden, disabled, usable})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if winner.Ref != "e3" {
		t.Fatalf("winner = %q, want e3 (only the usable one)", winner.Ref)
	}

	// 三个都不可用时必须报"没有可点的元素"，而不是随便挑一个。
	_, _, _, err = resolve("Submit", []contract.Element{hidden, disabled})
	if err == nil {
		t.Fatal("expected a rejection")
	}
}

func TestResolveRejectsEmptyHint(t *testing.T) {
	_, _, _, err := resolve("   ", nil)
	if err == nil {
		t.Fatal("an empty hint must be rejected")
	}
	if code := errorCode(err); code != CodeTargetHintEmpty {
		t.Fatalf("code = %q", code)
	}
}

func TestResolveSpecScopesDuplicateLinksToContainingCard(t *testing.T) {
	observation := contract.Observation{
		URL: "https://shop.test/products",
		Structures: []contract.StructureNode{{
			Ref: "s1", Kind: "card", FullText: "Blue Top View Product", Visible: true,
		}, {
			Ref: "s2", Kind: "card", FullText: "Red Top View Product", Visible: true,
		}},
		Elements: []contract.Element{
			element("e1", "link", "", "View Product", cssLocator("#blue-link")),
			element("e2", "link", "", "View Product", cssLocator("#red-link")),
		},
	}
	observation.Elements[0].ContainerRef = "s1"
	observation.Elements[1].ContainerRef = "s2"

	winner, locator, candidates, err := resolveSpec(contract.TargetSpec{
		Object: contract.TargetObject{Text: "View Product", Role: "link"},
		Scope:  &contract.TargetScope{Kind: "card", ContainsText: "Blue Top"},
	}, observation)
	if err != nil {
		t.Fatalf("resolveSpec: %v candidates=%#v", err, candidates)
	}
	if winner.Ref != "e1" {
		t.Fatalf("winner = %q, want e1", winner.Ref)
	}
	if locator != cssLocator("#blue-link") {
		t.Fatalf("locator = %#v", locator)
	}
}

func TestResolveSpecUsesAliasesInsideScopeForIconControls(t *testing.T) {
	observation := contract.Observation{
		URL: "https://shop.test/products",
		Structures: []contract.StructureNode{{
			Ref: "s1", Kind: "form", FullText: "Search items", Visible: true,
		}},
		Elements: []contract.Element{
			element("e1", "searchbox", "Search items", "", roleLocator("searchbox", "Search items")),
			element("e2", "button", "", "\uf002", cssLocator("#submit_search")),
		},
	}
	observation.Elements[0].ContainerRef = "s1"
	observation.Elements[1].ContainerRef = "s1"
	observation.Elements[1].Attributes = map[string]string{"id": "submit_search", "type": "button"}

	winner, _, _, err := resolveSpec(contract.TargetSpec{
		Object: contract.TargetObject{Aliases: []string{"search submit", "submit_search"}},
		Scope:  &contract.TargetScope{Kind: "form", ContainsText: "Search items"},
	}, observation)
	if err != nil {
		t.Fatalf("resolveSpec: %v", err)
	}
	if winner.Ref != "e2" {
		t.Fatalf("winner = %q, want e2", winner.Ref)
	}
}

func TestResolveSpecCanUseDescendantTextAsScopeAndRoleOnlyObject(t *testing.T) {
	observation := contract.Observation{
		URL: "https://shop.test/products",
		Structures: []contract.StructureNode{{
			Ref: "s1", Kind: "form", FullText: "", Visible: true,
		}},
		Elements: []contract.Element{
			element("e1", "searchbox", "Search Product", "", roleLocator("searchbox", "Search Product")),
			element("e2", "button", "", "\uf002", cssLocator("#submit_search")),
		},
	}
	observation.Elements[0].ContainerRef = "s1"
	observation.Elements[1].ContainerRef = "s1"
	observation.Elements[0].Attributes = map[string]string{"placeholder": "Search Product"}
	observation.Elements[1].Role = "button"
	observation.Elements[1].Attributes = map[string]string{"id": "submit_search", "type": "button"}

	winner, _, _, err := resolveSpec(contract.TargetSpec{
		Object: contract.TargetObject{Role: "button"},
		Scope:  &contract.TargetScope{ContainsText: "Search Product"},
	}, observation)
	if err != nil {
		t.Fatalf("resolveSpec: %v", err)
	}
	if winner.Ref != "e2" {
		t.Fatalf("winner = %q, want e2", winner.Ref)
	}
}

func TestResolveSpecIncludesElementsInDescendantScopes(t *testing.T) {
	observation := contract.Observation{
		URL: "https://shop.test/products",
		Structures: []contract.StructureNode{{
			Ref: "s1", Kind: "card", FullText: "Blue Top", Visible: true,
		}, {
			Ref: "s2", Kind: "list_item", ParentRef: "s1", FullText: "View Product", Visible: true,
		}},
		Elements: []contract.Element{
			element("e1", "link", "", "View Product", cssLocator("#nested-view")),
		},
	}
	observation.Elements[0].ContainerRef = "s2"

	winner, _, _, err := resolveSpec(contract.TargetSpec{
		Object: contract.TargetObject{Role: "link", Text: "View Product"},
		Scope:  &contract.TargetScope{Kind: "card", ContainsText: "Blue Top"},
	}, observation)
	if err != nil {
		t.Fatalf("resolveSpec: %v", err)
	}
	if winner.Ref != "e1" {
		t.Fatalf("winner = %q, want e1", winner.Ref)
	}
}

func TestResolveSpecStillRejectsUnscopedDuplicateLinks(t *testing.T) {
	observation := contract.Observation{
		Elements: []contract.Element{
			element("e1", "link", "", "View Product", cssLocator("#blue-link")),
			element("e2", "link", "", "View Product", cssLocator("#red-link")),
			element("e3", "link", "", "View Product", cssLocator("#green-link")),
		},
	}
	_, _, _, err := resolveSpec(contract.TargetSpec{
		Object: contract.TargetObject{Text: "View Product", Role: "link"},
	}, observation)
	if err == nil {
		t.Fatal("unscoped duplicate object must stay ambiguous")
	}
	if code := errorCode(err); code != CodeTargetAmbiguous {
		t.Fatalf("code = %q, want %q", code, CodeTargetAmbiguous)
	}
}

func TestResolveSpecAcceptsTopLevelObjectFieldsFromLooseModelCalls(t *testing.T) {
	observation := contract.Observation{
		Elements: []contract.Element{
			element("e1", "link", "", "View Product", cssLocator("#view")),
		},
	}
	winner, _, _, err := resolveSpec(contract.TargetSpec{
		Role: "link",
		Name: "View Product",
		Text: "View Product",
	}, observation)
	if err != nil {
		t.Fatalf("resolveSpec: %v", err)
	}
	if winner.Ref != "e1" {
		t.Fatalf("winner = %q, want e1", winner.Ref)
	}
}

func TestResolveIgnoresElementsWithoutVerifiedLocators(t *testing.T) {
	// 观测里不该出现没有定位器的元素；真出现了也不能拿来用。
	elements := []contract.Element{
		element("e1", "button", "Submit", "Submit"),
		element("e2", "button", "Submit", "Submit", roleLocator("button", "Submit")),
	}
	winner, _, _, err := resolve("Submit", elements)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if winner.Ref != "e2" {
		t.Fatalf("winner = %q, want e2", winner.Ref)
	}
}

func TestPageAnchorUsesPathThenHost(t *testing.T) {
	if got := contract.PageAnchor("http://127.0.0.1:8123/index.html"); got != "/index.html" {
		t.Fatalf("anchor = %q", got)
	}
	if got := contract.PageAnchor("http://127.0.0.1:8123/"); got != "127.0.0.1:8123" {
		t.Fatalf("root anchor = %q", got)
	}
}

func TestPageViewIncludesStructuresAndTruncationReason(t *testing.T) {
	observation := contract.Observation{
		URL:              "https://shop.test/products",
		Title:            "Products",
		Truncated:        true,
		TruncationReason: "max_elements=2 kept=2 candidates=5",
		Structures: []contract.StructureNode{{
			Ref: "s1", Kind: "card", FullText: "Blue Top View Product", Visible: true,
		}},
		Elements: []contract.Element{
			element("e1", "link", "", "View Product", cssLocator("#blue-view")),
		},
	}
	observation.Elements[0].ContainerRef = "s1"

	view := pageView(observation)
	if view == nil || !view.Truncated || view.TruncationReason == "" {
		t.Fatalf("page view truncation was not preserved: %#v", view)
	}
	if len(view.Scopes) != 1 || view.Scopes[0].Ref != "s1" || view.Scopes[0].Kind != "card" {
		t.Fatalf("scopes = %#v", view.Scopes)
	}
	if len(view.Elements) != 1 || view.Elements[0].ContainerRef != "s1" {
		t.Fatalf("elements = %#v", view.Elements)
	}
}

func TestPageViewIncludesActionCandidatesWithoutExposingLocators(t *testing.T) {
	observation := contract.Observation{
		URL:   "https://shop.test/products",
		Title: "Products",
		ActionCandidates: []contract.ActionCandidate{{
			CandidateID: "act_search_submit",
			Kind:        "form_submit_candidate",
			Action:      string(contract.ActionClick),
			TargetRef:   "e2",
			Role:        "button",
			Aliases:     []string{"form submit", "search submit", "submit_search"},
			Relations: []contract.CandidateRelation{{
				Type: "form_submit_candidate",
				Ref:  "s1",
			}},
			Locator:    cssLocator("#submit_search"),
			Confidence: "high",
		}},
		Elements: []contract.Element{
			element("e2", "button", "", "\uf002", cssLocator("#submit_search")),
		},
	}

	view := pageView(observation)
	if len(view.ActionCandidates) != 1 {
		t.Fatalf("action candidates = %#v", view.ActionCandidates)
	}
	candidate := view.ActionCandidates[0]
	if candidate.CandidateID != "act_search_submit" || candidate.Kind != "form_submit_candidate" {
		t.Fatalf("candidate summary = %#v", candidate)
	}
	if strings.Contains(fmt.Sprintf("%#v", candidate), "#submit_search") {
		t.Fatalf("page view must not expose raw locator details: %#v", candidate)
	}
	if len(candidate.Aliases) != 3 || candidate.Aliases[1] != "search submit" {
		t.Fatalf("aliases = %#v", candidate.Aliases)
	}
}

func TestPageViewKeepsGoalRelevantFormSubmitAheadOfAds(t *testing.T) {
	observation := rankedPageViewObservation(31)
	observation.Structures = append(observation.Structures, contract.StructureNode{
		Ref: "scope-search", Kind: "form", FullText: "Search products", Visible: true,
	}, contract.StructureNode{
		Ref: "scope-parent", Kind: "section", FullText: "Account settings", Visible: true,
	}, contract.StructureNode{
		Ref: "scope-child", Kind: "group", FullText: "Controls",
		ParentRef: "scope-parent", Visible: true,
	})
	observation.Elements = append(observation.Elements, contract.Element{
		Ref: "target-search", Tag: "button", Role: "button",
		Visible: true, Enabled: true, VisibleInViewport: true, ContainerRef: "scope-search",
		Locators: []contract.Locator{cssLocator("[data-control=search-submit]")},
	}, contract.Element{
		Ref: "target-nested", Tag: "button", Role: "button", Name: "Continue",
		Visible: true, Enabled: true, VisibleInViewport: true, ContainerRef: "scope-child",
		Locators: []contract.Locator{roleLocator("button", "Continue")},
	})
	observation.ActionCandidates = append(observation.ActionCandidates, contract.ActionCandidate{
		CandidateID: "candidate-search-submit",
		Kind:        "form_submit_candidate",
		Action:      string(contract.ActionClick),
		TargetRef:   "target-search",
		Role:        "button",
		Aliases:     []string{"form submit", "search submit"},
		Relations: []contract.CandidateRelation{{
			Type: "form_submit_candidate",
			Ref:  "scope-search",
		}},
		Locator:    cssLocator("[data-control=search-submit]"),
		Confidence: "high",
	})

	view := BuildPageView(
		observation,
		"search products, submit the search form, and update account settings",
		nil,
	)
	if len(view.ActionCandidates) != maxPageActionCandidates {
		t.Fatalf("action candidates = %d, want %d", len(view.ActionCandidates), maxPageActionCandidates)
	}
	if view.ActionCandidates[0].CandidateID != "candidate-search-submit" {
		t.Fatalf("first action candidate = %#v, want goal-relevant form submit", view.ActionCandidates[0])
	}
	if !hasActionCandidate(view, "candidate-search-submit") {
		t.Fatalf("goal-relevant form submit was omitted: %#v", view.ActionCandidates)
	}
	if !hasPageElement(view, "target-nested") {
		t.Fatalf("element inside nested goal-relevant scope was omitted: %#v", view.Elements)
	}

	_, _, candidate, err := resolveActionCandidate(
		contract.ActionClick, "candidate-search-submit", observation,
	)
	if err != nil || candidate.CandidateID != "candidate-search-submit" {
		t.Fatalf("full observation no longer resolves omitted candidate data: candidate=%#v err=%v", candidate, err)
	}
}

func TestPageViewKeepsVisibleDialogCandidatesAfterAction(t *testing.T) {
	observation := rankedPageViewObservation(31)
	observation.Structures = append(observation.Structures, contract.StructureNode{
		Ref: "scope-dialog", Kind: "dialog", FullText: "Confirmation", Visible: true,
	})
	observation.Elements = append(observation.Elements, contract.Element{
		Ref: "target-dialog", Tag: "a", Role: "link", Name: "Continue",
		Text: "Continue", Visible: true, Enabled: true, VisibleInViewport: true,
		ContainerRef: "scope-dialog",
		Locators:     []contract.Locator{roleLocator("link", "Continue")},
	})
	observation.ActionCandidates = append(observation.ActionCandidates, contract.ActionCandidate{
		CandidateID: "candidate-dialog-continue",
		Kind:        "element_candidate",
		Action:      string(contract.ActionClick),
		TargetRef:   "target-dialog",
		Role:        "link",
		Name:        "Continue",
		Text:        "Continue",
		Aliases:     []string{"continue"},
		Locator:     roleLocator("link", "Continue"),
		Confidence:  "high",
	})
	observation.Elements = append(observation.Elements, contract.Element{
		Ref: "last-target", Tag: "button", Role: "button", Name: "Retry",
		Text: "Retry", Visible: true, Enabled: true,
		Locators: []contract.Locator{roleLocator("button", "Retry")},
	})
	observation.ActionCandidates = append(observation.ActionCandidates, contract.ActionCandidate{
		CandidateID: "candidate-action-related",
		Kind:        "element_candidate",
		Action:      string(contract.ActionClick),
		TargetRef:   "last-target",
		Role:        "button",
		Name:        "Retry",
		Text:        "Retry",
		Aliases:     []string{"retry"},
		Locator:     roleLocator("button", "Retry"),
		Confidence:  "low",
	})
	last := &CompactResult{
		OK: false,
		Failure: &DryRunFailure{Steps: []DryRunStep{{
			Action: string(contract.ActionClick),
			HitTest: &contract.HitTest{
				TargetRef: "last-target",
			},
		}}},
	}

	view := BuildPageView(observation, "finish the workflow", last)
	if !hasActionCandidate(view, "candidate-dialog-continue") {
		t.Fatalf("visible dialog candidate was omitted after action: %#v", view.ActionCandidates)
	}
	if view.ActionCandidates[0].CandidateID != "candidate-action-related" {
		t.Fatalf("first action candidate = %#v, want latest-action relation", view.ActionCandidates[0])
	}
}

func TestPageViewReportsOmittedCounts(t *testing.T) {
	observation := contract.Observation{URL: "https://app.test/page", Title: "Page"}
	for index := 6; index >= 0; index-- {
		observation.Blockers = append(observation.Blockers, contract.Blocker{
			Kind: "overlay", Ref: fmt.Sprintf("blocker-%02d", index), Confidence: "medium",
			Reason: strings.Repeat("blocker ", 30),
		})
	}
	for index := 10; index >= 0; index-- {
		observation.Structures = append(observation.Structures, contract.StructureNode{
			Ref: fmt.Sprintf("scope-%02d", index), Kind: "section",
			FullText: strings.Repeat("scope ", 35), Visible: true,
		})
	}
	for index := 22; index >= 0; index-- {
		ref := fmt.Sprintf("element-%02d", index)
		observation.Elements = append(observation.Elements, contract.Element{
			Ref: ref, Tag: "button", Role: "button", Text: strings.Repeat("element ", 20),
			Visible: true, Enabled: true,
			VisibleInViewport: true, Locators: []contract.Locator{cssLocator("[data-ref=" + ref + "]")},
		})
	}
	for index := 14; index >= 0; index-- {
		ref := fmt.Sprintf("candidate-%02d", index)
		observation.ActionCandidates = append(observation.ActionCandidates, contract.ActionCandidate{
			CandidateID: ref,
			Kind:        "element_candidate",
			Action:      string(contract.ActionClick),
			TargetRef:   fmt.Sprintf("element-%02d", index),
			Text:        strings.Repeat("candidate ", 20),
			Locator:     cssLocator("[data-candidate=" + ref + "]"),
			Confidence:  "medium",
		})
	}

	view := BuildPageView(observation, "", nil)
	if len(view.Blockers) != maxPageBlockers ||
		len(view.Scopes) != maxPageScopes ||
		len(view.ActionCandidates) != maxPageActionCandidates ||
		len(view.Elements) != maxPageElements {
		t.Fatalf(
			"page view limits = blockers:%d scopes:%d candidates:%d elements:%d",
			len(view.Blockers), len(view.Scopes), len(view.ActionCandidates), len(view.Elements),
		)
	}
	const wantReason = "page_view_omitted blockers=2 action_candidates=3 scopes=3 elements=3"
	if !view.Truncated || view.TruncationReason != wantReason {
		t.Fatalf("truncation = %t %q, want %q", view.Truncated, view.TruncationReason, wantReason)
	}
	if view.Blockers[0].Ref != "blocker-00" ||
		view.Scopes[0].Ref != "scope-00" ||
		view.ActionCandidates[0].CandidateID != "candidate-00" ||
		view.Elements[0].Ref != "element-00" {
		t.Fatalf(
			"stable-ref ordering failed: blocker=%q scope=%q candidate=%q element=%q",
			view.Blockers[0].Ref,
			view.Scopes[0].Ref,
			view.ActionCandidates[0].CandidateID,
			view.Elements[0].Ref,
		)
	}
	if len([]rune(view.Blockers[0].Reason)) > maxPageSummaryText ||
		len([]rune(view.Scopes[0].Text)) > maxPageSummaryText ||
		len([]rune(view.ActionCandidates[0].Text)) > maxPageElementText ||
		len([]rune(view.Elements[0].Text)) > maxPageElementText {
		t.Fatalf(
			"text limits exceeded: blocker=%d scope=%d candidate=%d element=%d",
			len([]rune(view.Blockers[0].Reason)),
			len([]rune(view.Scopes[0].Text)),
			len([]rune(view.ActionCandidates[0].Text)),
			len([]rune(view.Elements[0].Text)),
		)
	}
}

func rankedPageViewObservation(adCount int) contract.Observation {
	observation := contract.Observation{
		URL:   "https://app.test/page",
		Title: "Page",
	}
	for index := 0; index < adCount; index++ {
		ref := fmt.Sprintf("ad-%02d", index)
		observation.Elements = append(observation.Elements, contract.Element{
			Ref: ref, Tag: "a", Role: "link", Name: "Sponsored offer",
			Text: "Sponsored offer", Visible: true, Enabled: true, VisibleInViewport: true,
			Locators: []contract.Locator{cssLocator("[data-ad=" + ref + "]")},
		})
		observation.ActionCandidates = append(observation.ActionCandidates, contract.ActionCandidate{
			CandidateID: "candidate-" + ref,
			Kind:        "element_candidate",
			Action:      string(contract.ActionClick),
			TargetRef:   ref,
			Role:        "link",
			Name:        "Sponsored offer",
			Text:        "Sponsored offer",
			Aliases:     []string{"sponsored offer"},
			Locator:     cssLocator("[data-ad=" + ref + "]"),
			Confidence:  "high",
		})
	}
	return observation
}

func hasActionCandidate(view *PageView, candidateID string) bool {
	for _, candidate := range view.ActionCandidates {
		if candidate.CandidateID == candidateID {
			return true
		}
	}
	return false
}

func hasPageElement(view *PageView, ref string) bool {
	for _, element := range view.Elements {
		if element.Ref == ref {
			return true
		}
	}
	return false
}

func TestResolveActionCandidateGroundsLatestObservationCandidate(t *testing.T) {
	observation := contract.Observation{
		ObservationID: "obs_current",
		PageStateID:   "ps_current",
		URL:           "https://shop.test/products",
		Elements: []contract.Element{
			element("e1", "searchbox", "Search Product", "", roleLocator("searchbox", "Search Product")),
			element("e2", "button", "", "\uf002", cssLocator("#submit_search")),
		},
		ActionCandidates: []contract.ActionCandidate{{
			CandidateID: "act_search_submit",
			Kind:        "form_submit_candidate",
			Action:      string(contract.ActionClick),
			TargetRef:   "e2",
			Role:        "button",
			Aliases:     []string{"form submit", "search submit", "submit_search"},
			Locator:     cssLocator("#submit_search"),
			Confidence:  "high",
		}},
	}

	winner, locator, candidate, err := resolveActionCandidate(
		contract.ActionClick, "act_search_submit", observation,
	)
	if err != nil {
		t.Fatalf("resolveActionCandidate: %v", err)
	}
	if winner.Ref != "e2" {
		t.Fatalf("winner = %q, want e2", winner.Ref)
	}
	if locator != cssLocator("#submit_search") {
		t.Fatalf("locator = %#v", locator)
	}
	if candidate.CandidateID != "act_search_submit" {
		t.Fatalf("candidate = %#v", candidate)
	}
}

func TestResolveActionCandidateRejectsStaleCandidateIDs(t *testing.T) {
	observation := contract.Observation{
		ObservationID: "obs_after_navigation",
		PageStateID:   "ps_after_navigation",
		URL:           "https://shop.test/other",
		Elements: []contract.Element{
			element("e9", "button", "Continue", "Continue", roleLocator("button", "Continue")),
		},
	}

	_, _, _, err := resolveActionCandidate(contract.ActionClick, "act_search_submit", observation)
	if err == nil {
		t.Fatal("a candidate from a previous observation must be rejected")
	}
	if code := errorCode(err); code != CodeCandidateNotFound {
		t.Fatalf("code = %q, want %q", code, CodeCandidateNotFound)
	}
}

func TestResolveActionCandidateRejectsIncompatibleActions(t *testing.T) {
	observation := contract.Observation{
		ObservationID: "obs_current",
		PageStateID:   "ps_current",
		URL:           "https://shop.test/products",
		Elements: []contract.Element{
			element("e2", "button", "", "\uf002", cssLocator("#submit_search")),
		},
		ActionCandidates: []contract.ActionCandidate{{
			CandidateID: "act_search_submit",
			Kind:        "form_submit_candidate",
			Action:      string(contract.ActionClick),
			TargetRef:   "e2",
			Locator:     cssLocator("#submit_search"),
			Confidence:  "high",
		}},
	}

	_, _, _, err := resolveActionCandidate(contract.ActionInput, "act_search_submit", observation)
	if err == nil {
		t.Fatal("an input tool must not reuse a click-only form submit candidate")
	}
	if code := errorCode(err); code != CodeCandidateIncompatible {
		t.Fatalf("code = %q, want %q", code, CodeCandidateIncompatible)
	}
}

func TestDryRunFailureCarriesRepairContext(t *testing.T) {
	detail := "matched 0"
	failure := dryRunFailure(contract.ExecutionResult{
		ExecutionID: "exec_1",
		Status:      contract.ExecutionFailed,
		FinalURL:    "https://shop.test/products",
		Steps: []contract.StepResult{{
			Index:     1,
			Action:    contract.ActionClick,
			Status:    "failed",
			URLBefore: "https://shop.test/products",
			URLAfter:  "https://shop.test/products",
			Error:     &contract.StepError{Kind: contract.SignalTargetNotFound, Message: "locator matched 0 elements"},
			Conditions: []contract.ConditionResult{{
				Phase: contract.PhasePost, Type: contract.CondTextVisible, Value: "Added",
				Satisfied: false, Detail: &detail,
			}},
		}},
	})
	if failure.FailedStep == nil || failure.FailedStep.Index != 1 {
		t.Fatalf("failed step context missing: %#v", failure)
	}
	if failure.CurrentURL != "https://shop.test/products" {
		t.Fatalf("current url = %q", failure.CurrentURL)
	}
	if len(failure.RepairHints) == 0 {
		t.Fatalf("repair hints missing: %#v", failure)
	}
}

func TestDryRunFailureCarriesBlockerRecoveryContext(t *testing.T) {
	failure := dryRunFailure(contract.ExecutionResult{
		ExecutionID: "exec_1",
		Status:      contract.ExecutionFailed,
		FinalURL:    "https://app.test/edit",
		Steps: []contract.StepResult{{
			Index:     3,
			Action:    contract.ActionClick,
			Status:    "failed",
			URLBefore: "https://app.test/edit",
			URLAfter:  "https://app.test/edit",
			Error:     &contract.StepError{Kind: contract.SignalBlockedByAuth, Message: "auth wall covers target"},
			Blocker: &contract.Blocker{
				Kind:            "auth_wall",
				Ref:             "b1",
				Confidence:      "high",
				CoversTargetRef: "e7",
				Reason:          "visible sign in prompt",
			},
			HitTest: &contract.HitTest{
				TargetRef:   "e7",
				HitRef:      "b1",
				HitTag:      "div",
				Covered:     true,
				BlockerKind: "auth_wall",
			},
			Recovery: []contract.RecoveryAttempt{{
				Blocker:               contract.Blocker{Kind: "auth_wall", Ref: "b1", Confidence: "high"},
				Action:                "none",
				Succeeded:             false,
				RetriedOriginalAction: false,
				Reason:                "requires user login",
			}},
		}},
	})
	if failure.FailedStep == nil {
		t.Fatal("failed step missing")
	}
	if failure.FailedStep.Blocker == nil || failure.FailedStep.Blocker.Kind != "auth_wall" {
		t.Fatalf("blocker context missing: %#v", failure.FailedStep)
	}
	if failure.FailedStep.HitTest == nil || !failure.FailedStep.HitTest.Covered {
		t.Fatalf("hit-test context missing: %#v", failure.FailedStep)
	}
	if len(failure.FailedStep.Recovery) != 1 {
		t.Fatalf("recovery attempts missing: %#v", failure.FailedStep)
	}
	if !strings.Contains(strings.Join(failure.RepairHints, "\n"), "ask the user to clear the auth wall") {
		t.Fatalf("auth repair hint missing: %#v", failure.RepairHints)
	}
}
