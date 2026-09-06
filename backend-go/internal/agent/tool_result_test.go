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
				"status":"success","phase":"before","target":"Add to cart","action":"click","action_index":0,"step_index":1
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

func TestNonExplorationToolResultKeepsExistingTranscriptSemantics(t *testing.T) {
	raw := json.RawMessage(`{"status":"pending","report":{"required":true}}`)
	content, err := BuildModelToolSummary("get_report", raw, 5)
	if err != nil {
		t.Fatal(err)
	}
	if content != string(raw) {
		t.Fatalf("content = %s", content)
	}
}

func TestNewToolResultEventPayloadRejectsInvalidUTF8(t *testing.T) {
	raw := json.RawMessage{'"', 0xff, '"'}
	if _, err := NewToolResultEventPayload("explore_page", raw); err == nil {
		t.Fatal("invalid UTF-8 tool result was accepted")
	}
}
