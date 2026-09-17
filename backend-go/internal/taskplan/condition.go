package taskplan

import (
	"fmt"
	"regexp"
	"strings"
)

// ConditionIntent is a structured interpretation of a TaskPlan condition
// string that must be preserved by the compiled research-v2 DSL. Conditions
// that cannot be deterministically parsed stay free-text: they are only
// subject to direction-preservation checks (the DSL must still declare a
// condition in the same direction).
type ConditionIntent struct {
	Kind   string // text_visible | text_gone | url_contains | url_changes | page_fact | free_text
	Target string // semantic target (may be empty)
	Value  string // expected value (may be empty)
}

var (
	urlContainsPattern = regexp.MustCompile(`(?i)^url\s+contains\s+(.+)$`)
	urlChangesPattern  = regexp.MustCompile(`(?i)^url\s+changes?$`)
	visiblePattern     = regexp.MustCompile(`(?i)^(.+)\s+(?:is\s+)?visible$`)
	gonePattern        = regexp.MustCompile(`(?i)^(.+)\s+(?:is\s+)?(?:gone|not\s+visible)$`)
	pageLoadsPattern   = regexp.MustCompile(`(?i)^(.+\s+)?page\s+(?:loads?|loaded|ready|appears)$`)
)

// parseConditionIntent deterministically translates a TaskPlan condition
// string into a ConditionIntent. Unrecognized strings yield Kind free_text.
func parseConditionIntent(raw string) ConditionIntent {
	normalized := strings.TrimSpace(raw)
	if normalized == "" {
		return ConditionIntent{Kind: "free_text", Value: normalized}
	}
	if match := urlContainsPattern.FindStringSubmatch(normalized); match != nil {
		return ConditionIntent{
			Kind: "url_contains", Value: strings.TrimSpace(match[1]),
		}
	}
	if urlChangesPattern.MatchString(normalized) {
		return ConditionIntent{Kind: "url_changes"}
	}
	if pageLoadsPattern.MatchString(normalized) {
		return ConditionIntent{Kind: "page_fact"}
	}
	if match := visiblePattern.FindStringSubmatch(normalized); match != nil {
		return ConditionIntent{
			Kind: "text_visible", Target: strings.TrimSpace(match[1]),
		}
	}
	if match := gonePattern.FindStringSubmatch(normalized); match != nil {
		return ConditionIntent{
			Kind: "text_gone", Target: strings.TrimSpace(match[1]),
		}
	}
	return ConditionIntent{Kind: "free_text", Value: normalized}
}

// dslConditionIntent normalizes a structured DSL condition (map) into a
// ConditionIntent so Plan intents can be compared against it.
func dslConditionIntent(condition map[string]any) ConditionIntent {
	conditionType, _ := condition["type"].(string)
	value, _ := condition["value"].(string)
	switch strings.TrimSpace(conditionType) {
	case "text_visible":
		return ConditionIntent{Kind: "text_visible", Target: strings.TrimSpace(value), Value: strings.TrimSpace(value)}
	case "text_gone":
		return ConditionIntent{Kind: "text_gone", Target: strings.TrimSpace(value), Value: strings.TrimSpace(value)}
	case "url_contains":
		return ConditionIntent{Kind: "url_contains", Value: strings.TrimSpace(value)}
	case "url_changes":
		return ConditionIntent{Kind: "url_changes"}
	case "dom_changed", "value_changed", "network_request":
		return ConditionIntent{Kind: "page_fact"}
	default:
		return ConditionIntent{Kind: "free_text", Value: strings.TrimSpace(value)}
	}
}

// matchesIntent reports whether a DSL condition satisfies the Plan intent.
// Element/region facts only need the same kind to be present; page-level
// facts such as url_contains additionally require the expected value to match
// so the DSL cannot swap the asserted destination.
func matchesIntent(dsl ConditionIntent, planned ConditionIntent) bool {
	if planned.Kind == "free_text" {
		// A free-text Plan condition has no deterministic target; direction
		// preservation (non-empty DSL conditions) is enforced separately.
		return true
	}
	if planned.Kind == "page_fact" {
		// Any page-level fact satisfies the intent: URL transition or
		// destination, DOM change, value change, or network activity.
		switch dsl.Kind {
		case "page_fact", "url_changes", "url_contains":
			return true
		}
		return false
	}
	if dsl.Kind != planned.Kind {
		return false
	}
	switch planned.Kind {
	case "url_contains":
		return planned.Value != "" &&
			(strings.Contains(dsl.Value, planned.Value) ||
				strings.Contains(planned.Value, dsl.Value))
	default:
		// text_visible / text_gone: semantic target descriptions are authored
		// by the model and may be paraphrased, so matching the kind is the
		// deterministic contract.
		return true
	}
}

// dslConditionList extracts the condition arrays of a DSL step map as
// ConditionIntents. Missing arrays yield empty results.
func dslConditionList(step map[string]any, field string) []ConditionIntent {
	raw, ok := step[field].([]any)
	if !ok {
		return nil
	}
	result := make([]ConditionIntent, 0, len(raw))
	for _, entry := range raw {
		condition, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		result = append(result, dslConditionIntent(condition))
	}
	return result
}

// validateConditionPreservation enforces the BUG-182 contract: the compiled
// DSL must keep every TaskPlan precondition/completion condition direction and
// must not drop or rewrite page-level facts such as the asserted URL.
func validateConditionPreservation(
	step map[string]any,
	planned Step,
	index int,
) error {
	dslPre := dslConditionList(step, "preconditions")
	dslPost := dslConditionList(step, "postconditions")
	if len(planned.Preconditions) > 0 && len(dslPre) == 0 {
		return fmt.Errorf(
			"case.steps[%d] drops plan step %q preconditions",
			index,
			planned.ID,
		)
	}
	if len(planned.CompletionConditions) > 0 && len(dslPost) == 0 {
		return fmt.Errorf(
			"case.steps[%d] drops plan step %q completion conditions",
			index,
			planned.ID,
		)
	}
	for _, raw := range planned.Preconditions {
		intent := parseConditionIntent(raw)
		if intent.Kind == "free_text" {
			continue
		}
		if !anyMatches(dslPre, intent) {
			return fmt.Errorf(
				"case.steps[%d] does not preserve plan step %q precondition %q",
				index,
				planned.ID,
				raw,
			)
		}
	}
	for _, raw := range planned.CompletionConditions {
		intent := parseConditionIntent(raw)
		if intent.Kind == "free_text" {
			continue
		}
		if !anyMatches(dslPost, intent) {
			return fmt.Errorf(
				"case.steps[%d] does not preserve plan step %q completion condition %q",
				index,
				planned.ID,
				raw,
			)
		}
	}
	return nil
}

func anyMatches(conditions []ConditionIntent, planned ConditionIntent) bool {
	for _, condition := range conditions {
		if matchesIntent(condition, planned) {
			return true
		}
	}
	return false
}
