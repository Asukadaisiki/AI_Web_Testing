package agent

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestBuildModelToolSummaryIsDeterministicAndTraceable(t *testing.T) {
	first := json.RawMessage(`{
		"success":true,
			"context_evidence":{
				"execution_scope":"isolated_probe",
				"state_persisted":false,
				"clean_context_requested":true,
				"storage_state_loaded":false,
				"planning_session_id":63
			},
		"pages":[
			{
				"url":"https://example.com/products",
				"page_state":"S1",
				"revision":2,
				"status":"success",
				"element_count":4,
				"actions":[{
					"step_index":1,
					"action_index":0,
					"action":"click",
					"target":"Add to cart",
						"url":"https://example.com/products",
					"phase":"before",
					"status":"success",
					"target_evidence":[{
						"node_id":"e2",
						"parent_id":"e1",
						"role":"button",
						"name":"Add to cart",
						"page_state":"S1",
						"focusable":true,
						"verified_selectors":[
							{"strategy":"css","selector":"#add","source":"dom"},
							{"strategy":"css","selector":"#add","source":"dom"}
						]
					}]
				}],
				"a11y_nodes":[
					{"node_id":"e2","parent_id":"e1","role":"button","name":"Add to cart","page_state":"S1","focusable":true,
					 "verified_selectors":[{"strategy":"css","selector":"#add","source":"dom"}]},
					{"node_id":"e1","role":"product","name":"Blue Top","page_state":"S1"},
					{"node_id":"e2","parent_id":"e1","role":"button","name":"Add to cart","page_state":"S1","focusable":true,
					 "verified_selectors":[{"strategy":"css","selector":"#add","source":"dom"}]},
					{"node_id":"ignored","role":"generic","name":"","page_state":"S1"}
				]
			}
		]
	}`)
	second := json.RawMessage(`{
		"pages":[{
			"a11y_nodes":[
				{"node_id":"ignored","role":"generic","name":"","page_state":"S1"},
				{"node_id":"e1","role":"product","name":"Blue Top","page_state":"S1"},
				{"node_id":"e2","parent_id":"e1","role":"button","name":"Add to cart","page_state":"S1","focusable":true,
				 "verified_selectors":[{"source":"dom","selector":"#add","strategy":"css"}]}
			],
			"actions":[{
				"target_evidence":[{
					"verified_selectors":[
						{"source":"dom","selector":"#add","strategy":"css"},
						{"source":"dom","selector":"#add","strategy":"css"}
					],
					"focusable":true,"page_state":"S1","name":"Add to cart","role":"button","parent_id":"e1","node_id":"e2"
				}],
					"status":"success","phase":"before","target":"Add to cart","url":"https://example.com/products","action":"click","action_index":0,"step_index":1
			}],
			"element_count":4,"status":"success","revision":2,"page_state":"S1","url":"https://example.com/products"
		}],
		"success":true
	}`)

	firstSummary, err := BuildModelToolSummary("explore_flow", first, 17)
	if err != nil {
		t.Fatal(err)
	}
	repeatedSummary, err := BuildModelToolSummary("explore_flow", first, 17)
	if err != nil {
		t.Fatal(err)
	}
	if firstSummary != repeatedSummary {
		t.Fatalf("same source bytes produced different summaries:\n%s\n%s", firstSummary, repeatedSummary)
	}
	secondSummary, err := BuildModelToolSummary("explore_flow", second, 17)
	if err != nil {
		t.Fatal(err)
	}
	var summary, reordered ModelToolSummary
	if err := json.Unmarshal([]byte(firstSummary), &summary); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(secondSummary), &reordered); err != nil {
		t.Fatal(err)
	}
	if summary.SchemaVersion != ModelToolSummarySchemaV1 ||
		summary.PolicyVersion != ToolSummaryPolicyV1 ||
		summary.Source.EventSeq != 17 ||
		summary.Source.ContentBytes != len(first) ||
		summary.Source.ContentSHA256 != sha256HexBytes(first) {
		t.Fatalf("summary source = %#v", summary)
	}
	wantHash := summary.SummarySHA256
	unsigned, err := marshalUnsignedSummary(summary)
	if err != nil {
		t.Fatal(err)
	}
	if wantHash == "" || wantHash != sha256HexBytes(unsigned) {
		t.Fatalf("summary hash = %q, want %q", wantHash, sha256HexBytes(unsigned))
	}
	for index := range summary.Pages {
		summary.Pages[index].Omitted = ToolResultOmissionCounters{}
		for actionIndex := range summary.Pages[index].Actions {
			summary.Pages[index].Actions[actionIndex].OmittedSelectors = 0
		}
	}
	for index := range reordered.Pages {
		reordered.Pages[index].Omitted = ToolResultOmissionCounters{}
		for actionIndex := range reordered.Pages[index].Actions {
			reordered.Pages[index].Actions[actionIndex].OmittedSelectors = 0
		}
	}
	firstPages, _ := json.Marshal(summary.Pages)
	secondPages, _ := json.Marshal(reordered.Pages)
	if string(firstPages) != string(secondPages) {
		t.Fatalf("semantic summary is not stable:\n%s\n%s", firstPages, secondPages)
	}
	if summary.Source.ContentSHA256 == reordered.Source.ContentSHA256 {
		t.Fatal("different raw source bytes produced the same source hash")
	}
	page := summary.Pages[0]
	if len(page.A11yNodes) != 2 ||
		page.A11yNodes[0].NodeID != "e2" ||
		page.A11yNodes[1].NodeID != "e1" ||
		len(page.A11yNodes[0].VerifiedSelectors) != 1 {
		t.Fatalf("deduplicated nodes = %#v", page.A11yNodes)
	}
	action := page.Actions[0]
	if action.Action != "click" || action.Target != "Add to cart" ||
		len(action.TargetEvidence) != 1 {
		t.Fatalf("action = %#v", action)
	}
	if summary.Observation == nil ||
		summary.Observation.SchemaVersion != StructuredObservationV1 ||
		len(summary.Observation.PageStates) != 1 {
		t.Fatalf("observation = %#v", summary.Observation)
	}
	if summary.Context == nil ||
		summary.Context.ExecutionScope != "isolated_probe" ||
		summary.Context.StatePersisted ||
		summary.Context.PlanningSessionID != 63 {
		t.Fatalf("context = %#v", summary.Context)
	}
	if summary.Observation.PageStates[0].PageKind != "products" {
		t.Fatalf("page kind = %#v", summary.Observation.PageStates[0])
	}
	if !hasElementGroup(summary.Observation.ElementGroups, "S1", "button", 1, 1) ||
		!hasElementGroup(summary.Observation.ElementGroups, "S1", "product", 1, 0) {
		t.Fatalf("element groups = %#v", summary.Observation.ElementGroups)
	}
	if !hasCandidate(
		summary.Observation.CandidateCoverage,
		"S1", "button", "Add to cart", "#add", true,
	) {
		t.Fatalf("candidate coverage = %#v", summary.Observation.CandidateCoverage)
	}
	if len(summary.Observation.ActionOptions) != 1 ||
		summary.Observation.ActionOptions[0].SideEffect != "external_or_business_state" ||
		summary.Observation.ActionOptions[0].IdempotencyHint != "non_idempotent" {
		t.Fatalf("action options = %#v", summary.Observation.ActionOptions)
	}
	if len(summary.ExecutedEffects) != 1 ||
		summary.ExecutedEffects[0].Action != "click" ||
		summary.ExecutedEffects[0].Target != "Add to cart" ||
		summary.ExecutedEffects[0].URL != "https://example.com/products" {
		t.Fatalf("executed effects = %#v", summary.ExecutedEffects)
	}
}

func TestBuildModelToolSummaryBoundsUTF8AndReportsOmissions(t *testing.T) {
	nodes := make([]map[string]any, 0, 1200)
	for index := 0; index < 1200; index++ {
		nodes = append(nodes, map[string]any{
			"node_id":    fmt.Sprintf("node-%04d", index),
			"parent_id":  "root",
			"role":       "button",
			"name":       strings.Repeat("界", 400) + fmt.Sprintf("-%04d", index),
			"page_state": "S0",
			"focusable":  true,
			"verified_selectors": []map[string]any{{
				"strategy": "css",
				"selector": fmt.Sprintf(`#item-%04d[data-label="%s"]`, index, strings.Repeat("值", 400)),
			}},
		})
	}
	raw, err := json.Marshal(map[string]any{
		"url":           "https://example.com/" + strings.Repeat("路径", 2000),
		"page_state":    "S0",
		"element_count": len(nodes),
		"a11y_nodes":    nodes,
	})
	if err != nil {
		t.Fatal(err)
	}
	content, err := BuildModelToolSummary("explore_page", raw, 9)
	if err != nil {
		t.Fatal(err)
	}
	if len(content) > ModelToolSummaryTargetBytes ||
		len(content) > ModelToolSummaryHardLimitBytes ||
		!utf8.ValidString(content) {
		t.Fatalf("summary bytes=%d utf8=%v", len(content), utf8.ValidString(content))
	}
	var summary ModelToolSummary
	if err := json.Unmarshal([]byte(content), &summary); err != nil {
		t.Fatal(err)
	}
	if !summary.Truncation.Truncated ||
		summary.Truncation.Omitted.Nodes == 0 ||
		summary.Truncation.SummaryBytes != len(content) {
		t.Fatalf("truncation = %#v, bytes=%d", summary.Truncation, len(content))
	}
	if summary.Source.ContentBytes != len(raw) ||
		summary.Source.ContentSHA256 != sha256HexBytes(raw) {
		t.Fatalf("source = %#v", summary.Source)
	}
}

func TestCompactExplorationTranscriptReferencesSupersededState(t *testing.T) {
	transcript := []Message{{Role: "user", Content: "keep me"}}
	for index := 1; index <= 8; index++ {
		nodes := make([]map[string]any, 0, 180)
		for nodeIndex := 0; nodeIndex < 180; nodeIndex++ {
			nodes = append(nodes, map[string]any{
				"node_id": fmt.Sprintf("%d-%d", index, nodeIndex),
				"role":    "button", "name": strings.Repeat("x", 120),
				"page_state": "S0", "focusable": true,
			})
		}
		raw, _ := json.Marshal(map[string]any{
			"url": "https://example.com/products", "page_state": "S0",
			"revision": index, "element_count": len(nodes), "a11y_nodes": nodes,
		})
		summary, err := BuildModelToolSummary("explore_page", raw, int64(index))
		if err != nil {
			t.Fatal(err)
		}
		transcript = append(transcript, Message{
			Role: "tool", ToolCallID: fmt.Sprintf("call-%d", index), Content: summary,
		})
	}
	transcript = CompactExplorationTranscript(transcript)
	if explorationSummaryBytes(transcript) > ModelExplorationBudgetBytes {
		t.Fatalf("exploration transcript bytes = %d", explorationSummaryBytes(transcript))
	}
	var first, last ModelToolSummary
	if err := json.Unmarshal([]byte(transcript[1].Content), &first); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(transcript[len(transcript)-1].Content), &last); err != nil {
		t.Fatal(err)
	}
	if !first.Pages[0].ReferenceOnly || len(first.Pages[0].A11yNodes) != 0 ||
		first.Source.EventSeq != 1 {
		t.Fatalf("old summary = %#v", first)
	}
	if last.Pages[0].ReferenceOnly || last.Source.EventSeq != 8 {
		t.Fatalf("latest summary = %#v", last)
	}
	if transcript[0].Content != "keep me" {
		t.Fatal("non-exploration message was modified")
	}
}

func TestReferenceOnlyExplorationSummaryPreservesExecutedEffects(t *testing.T) {
	raw := json.RawMessage(`{
		"success":true,
		"pages":[{
			"url":"https://example.com/view_cart",
			"page_state":"S1",
			"status":"success",
			"actions":[{
				"action":"click",
				"target":"Add to cart",
				"status":"success"
			}]
		}]
	}`)
	content, err := BuildModelToolSummary("explore_flow", raw, 19)
	if err != nil {
		t.Fatal(err)
	}
	var summary ModelToolSummary
	if err := json.Unmarshal([]byte(content), &summary); err != nil {
		t.Fatal(err)
	}
	makeSummaryReferenceOnly(&summary, "test")
	encoded, err := encodeSummary(&summary)
	if err != nil {
		t.Fatal(err)
	}
	var compacted ModelToolSummary
	if err := json.Unmarshal(encoded, &compacted); err != nil {
		t.Fatal(err)
	}
	if !compacted.ReferenceOnly ||
		len(compacted.ExecutedEffects) != 1 ||
		compacted.ExecutedEffects[0].Target != "Add to cart" {
		t.Fatalf("compacted executed effects = %#v", compacted.ExecutedEffects)
	}
}

func TestNonExplorationToolResultUsesStructuredModelSummary(t *testing.T) {
	raw := json.RawMessage(`{
		"id":3,
		"status":"failed",
		"analysis":{
			"recommended_action":"targeted_retest",
			"case_results":[{
				"case_id":7,
				"case_name":"Blue Top cart",
				"status":"failed",
				"passed_steps":8,
				"total_steps":10,
				"failure_summary":"Postcondition failed with a very long internal report that should be bounded."
			}],
			"failure_signals":[{
				"category":"postcondition",
				"stage":"postcondition",
				"code":"condition.postcondition.text_visible.failed",
				"title":"Cart item missing",
				"retryable":false,
				"side_effect_committed":true
			}]
		},
		"report":{"large":"this full nested report must not be echoed to the model"}
	}`)
	content, err := BuildModelToolSummary("get_report", raw, 5)
	if err != nil {
		t.Fatal(err)
	}
	if content == string(raw) || strings.Contains(content, `"large"`) {
		t.Fatalf("raw report leaked into model summary: %s", content)
	}
	var summary ModelToolSummary
	if err := json.Unmarshal([]byte(content), &summary); err != nil {
		t.Fatal(err)
	}
	if summary.Tool != "get_report" ||
		summary.Report == nil ||
		summary.Report.Status != "failed" ||
		summary.Report.RecommendedAction != "targeted_retest" ||
		len(summary.Report.CaseResults) != 1 ||
		len(summary.Report.FailureSignals) != 1 {
		t.Fatalf("report summary = %#v", summary.Report)
	}
	if summary.Report.FailureSignals[0].Category != "postcondition" ||
		summary.Report.FailureSignals[0].SideEffectCommitted != true {
		t.Fatalf("failure brief = %#v", summary.Report.FailureSignals[0])
	}
}

func TestGenerateAndRepairToolResultsUseDecisionSummaries(t *testing.T) {
	generated := json.RawMessage(`{
		"generation_id":8,
		"case":{
			"profile":"research-v1",
			"name":"Blue Top cart",
			"steps":[
				{"action":"goto","target":"Products","value":"/products"},
				{"action":"input","target":"Search Product","value":"Blue Top"},
				{"action":"click","target":"Add to cart"}
			]
		}
	}`)
	content, err := BuildModelToolSummary("generate_dsl", generated, 6)
	if err != nil {
		t.Fatal(err)
	}
	var generation ModelToolSummary
	if err := json.Unmarshal([]byte(content), &generation); err != nil {
		t.Fatal(err)
	}
	if generation.DSL == nil ||
		generation.DSL.GenerationID != float64(8) ||
		generation.DSL.StepCount != 3 ||
		!containsString(generation.DSL.Actions, "click") ||
		!containsString(generation.DSL.Targets, "Search Product") {
		t.Fatalf("dsl summary = %#v", generation.DSL)
	}

	repair := json.RawMessage(`{
		"source_batch_id":3,
		"source_execution_id":9,
		"status":"manual_required",
		"strategy":"manual_reconcile",
		"reason":"The original action must not be replayed.",
		"original_action_replay_allowed":false,
		"failure_signals":[{"category":"assertion","title":"Cart quantity mismatch"}],
		"source_dsl":{"steps":[{"action":"click","target":"Add to cart"}]}
	}`)
	content, err = BuildModelToolSummary("fix_and_retry", repair, 7)
	if err != nil {
		t.Fatal(err)
	}
	var repairSummary ModelToolSummary
	if err := json.Unmarshal([]byte(content), &repairSummary); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(content, "source_dsl") ||
		repairSummary.Repair == nil ||
		repairSummary.Repair.Strategy != "manual_reconcile" ||
		repairSummary.Repair.OriginalActionReplayAllowed == nil ||
		*repairSummary.Repair.OriginalActionReplayAllowed {
		t.Fatalf("repair summary = %s", content)
	}
}

func hasElementGroup(
	groups []ObservedElementGroup,
	pageState string,
	category string,
	count int,
	verified int,
) bool {
	for _, group := range groups {
		if group.PageState == pageState &&
			group.Category == category &&
			group.Count == count &&
			group.VerifiedCount == verified {
			return true
		}
	}
	return false
}

func hasCandidate(
	candidates []ObservedCandidateCoverage,
	pageState string,
	category string,
	label string,
	selector string,
	executable bool,
) bool {
	for _, candidate := range candidates {
		if candidate.PageState == pageState &&
			candidate.Category == category &&
			candidate.Label == label &&
			candidate.PrimarySelector == selector &&
			candidate.Executable == executable {
			return true
		}
	}
	return false
}

func TestNewToolResultEventPayloadRejectsInvalidUTF8(t *testing.T) {
	raw := json.RawMessage{'"', 0xff, '"'}
	if _, err := NewToolResultEventPayload("explore_page", raw); err == nil {
		t.Fatal("invalid UTF-8 tool result was accepted")
	}
}
