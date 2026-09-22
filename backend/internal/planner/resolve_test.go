package planner

import (
	"strings"
	"testing"

	"github.com/Asukadaisiki/AI_Web_Testing/v2/backend/internal/contract"
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
