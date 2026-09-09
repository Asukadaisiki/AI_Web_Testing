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

func TestBuildModelToolSummaryMergesRepeatedPageStateActions(t *testing.T) {
	raw := json.RawMessage(`{
		"success": false,
		"failures": [{
			"code": "flow_action_failed",
			"step_index": 0,
			"action_index": 2,
			"action": "wait_for",
			"target": "Category: Men > Tshirts"
		}],
		"pages": [
			{
				"url": "https://example.com/product_details/2",
				"page_state": "S0",
				"revision": 1,
				"status": "success",
				"element_count": 3,
				"actions": [{
					"step_index": 0,
					"action_index": 0,
					"action": "wait_for",
					"target": "Men Tshirt",
					"phase": "after",
					"status": "success",
					"url": "https://example.com/product_details/2",
					"page_state": "S0",
					"evidence_count": 1
				}]
			},
			{
				"url": "https://example.com/product_details/2",
				"page_state": "S0",
				"revision": 2,
				"status": "success",
				"element_count": 3,
				"actions": [{
					"step_index": 0,
					"action_index": 1,
					"action": "wait_for",
					"target": "Rs. 400",
					"phase": "after",
					"status": "success",
					"url": "https://example.com/product_details/2",
					"page_state": "S0",
					"evidence_count": 0
				}]
			},
			{
				"url": "https://example.com/product_details/2",
				"page_state": "S0",
				"revision": 3,
				"status": "error",
				"element_count": 0,
				"actions": [{
					"step_index": 0,
					"action_index": 2,
					"action": "wait_for",
					"target": "Category: Men > Tshirts",
					"phase": "after",
					"status": "error",
					"url": "https://example.com/product_details/2",
					"page_state": "S0",
					"evidence_count": 0,
					"failure": {
						"code": "flow_action_failed",
						"step_index": 0,
						"action_index": 2,
						"action": "wait_for",
						"target": "Category: Men > Tshirts"
					}
				}]
			}
		]
	}`)

	content, err := BuildModelToolSummary("explore_flow", raw, 23)
	if err != nil {
		t.Fatal(err)
	}
	var summary ModelToolSummary
	if err := json.Unmarshal([]byte(content), &summary); err != nil {
		t.Fatal(err)
	}
	if len(summary.Pages) != 1 {
		t.Fatalf("pages = %d, want merged single page", len(summary.Pages))
	}
	actions := summary.Pages[0].Actions
	if len(actions) != 3 {
		t.Fatalf("actions = %#v, want successful and failed actions preserved", actions)
	}
	if actions[0].Target != "Men Tshirt" ||
		actions[1].Target != "Rs. 400" ||
		actions[2].Target != "Category: Men > Tshirts" {
		t.Fatalf("merged actions = %#v", actions)
	}
	if summary.Observation == nil || len(summary.Observation.ActionOptions) != 3 {
		t.Fatalf("observation action options = %#v", summary.Observation)
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

func TestExecuteDSLToolResultExposesBatchID(t *testing.T) {
	raw := json.RawMessage(`{
		"batch_id":562,
		"case_id":400,
		"report_api_url":"/api/v2/execution-batches/562/report",
		"status":"pending"
	}`)
	content, err := BuildModelToolSummary("execute_dsl", raw, 11)
	if err != nil {
		t.Fatal(err)
	}
	if content == string(raw) {
		t.Fatalf("raw execute_dsl result was not summarized: %s", content)
	}
	var summary ModelToolSummary
	if err := json.Unmarshal([]byte(content), &summary); err != nil {
		t.Fatal(err)
	}
	if summary.Tool != "execute_dsl" ||
		summary.Execution == nil ||
		summary.Execution.BatchID != float64(562) ||
		summary.Execution.CaseID != float64(400) ||
		summary.Execution.Status != "pending" ||
		summary.Execution.ReportAPIURL != "/api/v2/execution-batches/562/report" {
		t.Fatalf("execution summary = %#v", summary.Execution)
	}
}

func TestTaskPlanToolResultExposesBindingAndSteps(t *testing.T) {
	raw := json.RawMessage(`{
		"status":"grounding",
		"plan_id":"plan-1",
		"version":2,
		"plan_sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"steps":[{"id":"open"},{"id":"verify"}]
	}`)
	content, err := BuildModelToolSummary("set_task_plan", raw, 4)
	if err != nil {
		t.Fatal(err)
	}
	var summary ModelToolSummary
	if err := json.Unmarshal([]byte(content), &summary); err != nil {
		t.Fatal(err)
	}
	if summary.TaskPlan == nil ||
		summary.TaskPlan.PlanID != "plan-1" ||
		summary.TaskPlan.Version != 2 ||
		len(summary.TaskPlan.StepIDs) != 2 {
		t.Fatalf("task plan summary = %#v", summary.TaskPlan)
	}
}

func TestTaskPlanToolResultCanExposeFreshExplorationBudget(t *testing.T) {
	budget := &ToolResultExplorationBudgetSummary{
		Scope:       "plan_version",
		PlanID:      "plan-2",
		PlanVersion: 2,
		ExplorePage: ToolResultBudgetCounter{
			Used: 0, Limit: 5, Remaining: 5,
		},
		ExploreFlow: ToolResultBudgetCounter{
			Used: 0, Limit: 4, Remaining: 4,
		},
		RunExplorePage: ToolResultBudgetCounter{
			Used: 1, Limit: 10, Remaining: 9,
		},
		RunExploreFlow: ToolResultBudgetCounter{
			Used: 4, Limit: 8, Remaining: 4,
		},
		RepeatedExplorePageLimit: 1,
		RepeatedExploreFlowLimit: 1,
	}
	content, err := BuildModelToolSummary(
		"set_task_plan",
		json.RawMessage(`{
			"status":"grounding",
			"plan_id":"plan-2",
			"version":2,
			"plan_sha256":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			"steps":[{"id":"search"}]
		}`),
		8,
		&ToolResultTaskPlanSummary{
			PlanID:            "plan-2",
			Version:           2,
			PlanSHA256:        strings.Repeat("b", 64),
			Status:            "grounding",
			StepIDs:           []string{"search"},
			PendingStepIDs:    []string{"search"},
			ExplorationBudget: budget,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	summary, ok := DecodeModelToolSummary(content)
	if !ok ||
		summary.TaskPlan == nil ||
		summary.TaskPlan.ExplorationBudget == nil ||
		summary.TaskPlan.ExplorationBudget.ExploreFlow.Remaining != 4 ||
		summary.TaskPlan.ExplorationBudget.RunExploreFlow.Used != 4 {
		t.Fatalf("summary = %s", content)
	}
}

func TestExplorationSummaryExposesPersistedTargetBindings(t *testing.T) {
	content, err := BuildModelToolSummary(
		"explore_page",
		json.RawMessage(`{
			"url":"https://example.test/form",
			"status":"success",
			"a11y_nodes":[]
		}`),
		17,
		&ToolResultTaskPlanSummary{
			PlanID: "plan-1", Version: 2,
			PlanSHA256: strings.Repeat("a", 64),
			Status:     "grounding", StepIDs: []string{"open", "submit"},
			GroundedStepIDs: []string{"open"},
			PendingStepIDs:  []string{"submit"},
			StepBindings:    map[string]string{"submit": "binding-1"},
			ExplorationBudget: &ToolResultExplorationBudgetSummary{
				Scope:       "plan_version",
				PlanID:      "plan-1",
				PlanVersion: 2,
				ExplorePage: ToolResultBudgetCounter{
					Used: 1, Limit: 5, Remaining: 4,
				},
				ExploreFlow: ToolResultBudgetCounter{
					Used: 2, Limit: 4, Remaining: 2,
				},
				RunExplorePage: ToolResultBudgetCounter{
					Used: 1, Limit: 10, Remaining: 9,
				},
				RunExploreFlow: ToolResultBudgetCounter{
					Used: 2, Limit: 8, Remaining: 6,
				},
				RepeatedExplorePageLimit:  1,
				RepeatedExploreFlowLimit:  1,
				CurrentTool:               "explore_flow",
				CurrentSignatureUses:      intPointer(1),
				CurrentSignatureRemaining: intPointer(0),
			},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	summary, ok := DecodeModelToolSummary(content)
	if !ok || summary.TaskPlan == nil {
		t.Fatalf("summary = %s", content)
	}
	if summary.TaskPlan.StepBindings["submit"] != "binding-1" {
		t.Fatalf("task plan summary = %#v", summary.TaskPlan)
	}
	if len(summary.TaskPlan.GroundedStepIDs) != 1 ||
		len(summary.TaskPlan.PendingStepIDs) != 1 ||
		summary.TaskPlan.ExplorationBudget == nil ||
		summary.TaskPlan.ExplorationBudget.ExploreFlow.Remaining != 2 {
		t.Fatalf("task plan summary = %#v", summary.TaskPlan)
	}
}

func intPointer(value int) *int {
	return &value
}

func TestExplorationSummaryReadsObservationV2WithoutLegacyNodes(t *testing.T) {
	content, err := BuildModelToolSummary(
		"explore_page",
		json.RawMessage(`{
			"url":"https://example.test/form",
			"element_count":1,
			"observation_v2":{
				"schema_version":"browser.observation.v2",
				"observation_id":"obs-1",
				"page_state":{
					"state_id":"form",
					"state_sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
				},
				"elements":[{
					"element_ref":"form:7",
					"a11y":{"role":"button","name":"Submit"},
					"dom":{"tag":"button","attrs":{"id":"submit"}},
					"runtime":{"visible":true,"enabled":true},
					"locators":[{
						"locator":{"kind":"css","value":"#submit"},
						"provenance":"a11y_backend_dom_node",
						"observed_count":1
					}]
				}]
			}
		}`),
		21,
	)
	if err != nil {
		t.Fatal(err)
	}
	summary, ok := DecodeModelToolSummary(content)
	if !ok || len(summary.Pages) != 1 ||
		len(summary.Pages[0].A11yNodes) != 1 {
		t.Fatalf("summary = %s", content)
	}
	node := summary.Pages[0].A11yNodes[0]
	if node.Name != "Submit" ||
		len(node.VerifiedSelectors) != 1 ||
		node.VerifiedSelectors[0].Selector != "#submit" {
		t.Fatalf("node = %#v", node)
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
