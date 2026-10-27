package traceprojection

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/domain"
)

func TestProjectBrowserSteps(t *testing.T) {
	parent := domain.Span{
		ID:        "operation-1",
		SpanID:    "operation-1",
		Kind:      domain.SpanOperation,
		Name:      "fallback-name",
		Input:     json.RawMessage(`{"name":"browser_execute_case"}`),
		StartedAt: time.Date(2026, 10, 23, 9, 59, 0, 0, time.UTC),
	}
	evidence := Evidence{
		ID:     "evidence-1",
		SpanID: parent.SpanID,
		Status: "succeeded",
		Result: json.RawMessage(`{
			"steps":[{
				"index":0,
				"action":"goto",
				"status":"passed",
				"started_at":"2026-10-23T10:00:00Z",
				"duration_ms":540,
				"url_before":"about:blank",
				"url_after":"https://example.test/",
				"evidence":{"screenshot_path":"session/exec_0.png"},
				"custom_secret":"preserved"
			}]
		}`),
	}

	nodes := Project([]domain.Span{parent}, []Evidence{evidence})
	if len(nodes) != 1 {
		t.Fatalf("node count = %d, want 1", len(nodes))
	}
	node := nodes[0]
	if got := node.ID; got != "derived:operation-1:step:0" {
		t.Fatal(got)
	}
	if got := node.ParentSpanID; got != parent.SpanID {
		t.Fatalf("parent span ID = %q, want %q", got, parent.SpanID)
	}
	if got := node.SourceEvidenceID; got != evidence.ID {
		t.Fatalf("source evidence ID = %q, want %q", got, evidence.ID)
	}
	if got := node.Kind; got != "step" {
		t.Fatalf("kind = %q, want step", got)
	}
	if got := node.Name; got != "Step 1: goto" {
		t.Fatal(got)
	}
	if got := node.Status; got != "passed" {
		t.Fatalf("status = %q, want passed", got)
	}
	if got := node.StartedAt.Format(time.RFC3339Nano); got != "2026-10-23T10:00:00Z" {
		t.Fatalf("started at = %q", got)
	}
	if node.EndedAt == nil {
		t.Fatal("ended at is nil")
	}
	if got := node.EndedAt.Format(time.RFC3339Nano); got != "2026-10-23T10:00:00.54Z" {
		t.Fatal(got)
	}

	var data map[string]any
	if err := json.Unmarshal(node.Data, &data); err != nil {
		t.Fatalf("decode data: %v", err)
	}
	if got := data["custom_secret"]; got != "preserved" {
		t.Fatalf("custom_secret = %#v, want preserved", got)
	}
	for _, key := range []string{"evidence", "started_at", "status"} {
		if _, exists := data[key]; exists {
			t.Fatalf("data unexpectedly contains %q", key)
		}
	}
	if got := data["url_after"]; got != "https://example.test/" {
		t.Fatalf("url_after = %#v", got)
	}

	var projectedEvidence map[string]any
	if err := json.Unmarshal(node.Evidence, &projectedEvidence); err != nil {
		t.Fatalf("decode evidence: %v", err)
	}
	if got := projectedEvidence["screenshot_path"]; got != "session/exec_0.png" {
		t.Fatalf("screenshot_path = %#v", got)
	}
}

func TestProjectPiFinishCaseBrowserSteps(t *testing.T) {
	parent := operationSpan("finish-operation", "mcp__browser_harness__finish_case")
	evidence := Evidence{
		ID:     "finish-evidence",
		SpanID: parent.SpanID,
		Status: "passed",
		Result: browserResult("verify"),
	}

	nodes := Project([]domain.Span{parent}, []Evidence{evidence})
	if len(nodes) != 1 {
		t.Fatalf("node count = %d, want 1", len(nodes))
	}
	if nodes[0].Kind != "step" {
		t.Fatalf("node kind = %q, want step", nodes[0].Kind)
	}
	if nodes[0].ParentSpanID != parent.SpanID {
		t.Fatalf("parent span ID = %q, want %q", nodes[0].ParentSpanID, parent.SpanID)
	}
	if nodes[0].Name != "Step 1: verify" {
		t.Fatalf("node name = %q, want Step 1: verify", nodes[0].Name)
	}
}

func TestProjectMalformedBrowserResultFallsBack(t *testing.T) {
	tests := []struct {
		name   string
		result string
	}{
		{name: "malformed document", result: `{"steps":`},
		{name: "trailing document", result: `{"steps":[]} {}`},
		{name: "missing steps", result: `{}`},
		{name: "step is not object", result: `{"steps":[false]}`},
		{name: "duplicate index", result: `{"steps":[
			{"index":0,"action":"goto","status":"passed","started_at":"2026-10-23T10:00:00Z","duration_ms":1,"evidence":{}},
			{"index":0,"action":"click","status":"passed","started_at":"2026-10-23T10:00:01Z","duration_ms":1,"evidence":{}}
		]}`},
		{name: "negative index", result: `{"steps":[
			{"index":-1,"action":"goto","status":"passed","started_at":"2026-10-23T10:00:00Z","duration_ms":1,"evidence":{}}
		]}`},
		{name: "first index differs from position", result: `{"steps":[
			{"index":1,"action":"goto","status":"passed","started_at":"2026-10-23T10:00:00Z","duration_ms":1,"evidence":{}}
		]}`},
		{name: "fractional duration", result: `{"steps":[
			{"index":0,"action":"goto","status":"passed","started_at":"2026-10-23T10:00:00Z","duration_ms":1.5,"evidence":{}}
		]}`},
		{name: "negative duration", result: `{"steps":[
			{"index":0,"action":"goto","status":"passed","started_at":"2026-10-23T10:00:00Z","duration_ms":-1,"evidence":{}}
		]}`},
		{name: "duration overflows time", result: `{"steps":[
			{"index":0,"action":"goto","status":"passed","started_at":"2026-10-23T10:00:00Z","duration_ms":9223372036855,"evidence":{}}
		]}`},
		{name: "invalid time", result: `{"steps":[
			{"index":0,"action":"goto","status":"passed","started_at":"not-a-time","duration_ms":1,"evidence":{}}
		]}`},
		{name: "missing evidence", result: `{"steps":[
			{"index":0,"action":"goto","status":"passed","started_at":"2026-10-23T10:00:00Z","duration_ms":1}
		]}`},
	}

	parent := operationSpan("operation-1", "browser_execute_case")
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			nodes := Project([]domain.Span{parent}, []Evidence{{
				ID:     "evidence-1",
				SpanID: parent.SpanID,
				Status: "failed",
				Result: json.RawMessage(test.result),
			}})
			assertUnknownTool(t, nodes, "operation-1", "evidence-1")
		})
	}
}

func TestProjectFallbackPreservesMalformedPayloadsAsStrings(t *testing.T) {
	parent := operationSpan("operation-1", "browser_execute_case")
	source := Evidence{
		ID:        "evidence-1",
		SpanID:    parent.SpanID,
		Status:    "failed",
		Result:    json.RawMessage(`{"steps":`),
		Artifacts: json.RawMessage(`{"log_path":`),
		Error:     json.RawMessage(`not-json`),
	}

	nodes := Project([]domain.Span{parent}, []Evidence{source})
	assertUnknownTool(t, nodes, parent.SpanID, source.ID)

	var data map[string]any
	if err := json.Unmarshal(nodes[0].Data, &data); err != nil {
		t.Fatalf("decode fallback data: %v", err)
	}
	if got := data["result"]; got != string(source.Result) {
		t.Fatalf("result = %#v, want %q", got, source.Result)
	}

	var projectedEvidence map[string]any
	if err := json.Unmarshal(nodes[0].Evidence, &projectedEvidence); err != nil {
		t.Fatalf("decode fallback evidence: %v", err)
	}
	if got := projectedEvidence["artifacts"]; got != string(source.Artifacts) {
		t.Fatalf("artifacts = %#v, want %q", got, source.Artifacts)
	}
	if got := projectedEvidence["error"]; got != string(source.Error) {
		t.Fatalf("error = %#v, want %q", got, source.Error)
	}
}

func TestProjectUnknownOperationPreservesFallbackPayload(t *testing.T) {
	startedAt := time.Date(2026, 10, 23, 10, 0, 0, 0, time.UTC)
	updatedAt := startedAt.Add(time.Second)
	parent := operationSpan("operation-1", "custom_operation")
	parent.StartedAt = time.Time{}
	source := Evidence{
		ID:        "evidence-1",
		SpanID:    parent.SpanID,
		Status:    "failed",
		Result:    json.RawMessage(`{"custom_secret":"preserved"}`),
		Artifacts: json.RawMessage(`{"log_path":"session/log.txt"}`),
		Error:     json.RawMessage(`{"message":"failed"}`),
		CreatedAt: startedAt,
		UpdatedAt: updatedAt,
	}

	nodes := Project([]domain.Span{parent}, []Evidence{source})
	assertUnknownTool(t, nodes, parent.SpanID, source.ID)
	node := nodes[0]
	if node.Name != "unknown_tool" {
		t.Fatalf("name = %q, want unknown_tool", node.Name)
	}
	if node.Status != source.Status {
		t.Fatalf("status = %q, want %q", node.Status, source.Status)
	}
	if !node.StartedAt.Equal(startedAt) {
		t.Fatalf("started at = %s, want %s", node.StartedAt, startedAt)
	}
	if node.EndedAt == nil || !node.EndedAt.Equal(updatedAt) {
		t.Fatalf("ended at = %v, want %s", node.EndedAt, updatedAt)
	}

	var data map[string]json.RawMessage
	if err := json.Unmarshal(node.Data, &data); err != nil {
		t.Fatalf("decode fallback data: %v", err)
	}
	if got := string(data["result"]); got != `{"custom_secret":"preserved"}` {
		t.Fatalf("result = %s", got)
	}
	var projectedEvidence map[string]json.RawMessage
	if err := json.Unmarshal(node.Evidence, &projectedEvidence); err != nil {
		t.Fatalf("decode fallback evidence: %v", err)
	}
	if got := string(projectedEvidence["artifacts"]); got != `{"log_path":"session/log.txt"}` {
		t.Fatalf("artifacts = %s", got)
	}
	if got := string(projectedEvidence["error"]); got != `{"message":"failed"}` {
		t.Fatalf("error = %s", got)
	}
}

func TestProjectEmptyBrowserStepsProducesNoNodes(t *testing.T) {
	parent := operationSpan("operation-1", "browser_execute_case")
	nodes := Project([]domain.Span{parent}, []Evidence{{
		ID:     "evidence-1",
		SpanID: parent.SpanID,
		Result: json.RawMessage(`{"steps":[]}`),
	}})
	if len(nodes) != 0 {
		t.Fatalf("node count = %d, want 0", len(nodes))
	}
	encoded, err := json.Marshal(nodes)
	if err != nil {
		t.Fatalf("marshal nodes: %v", err)
	}
	if got := string(encoded); got != "[]" {
		t.Fatalf("encoded nodes = %s, want []", got)
	}
}

func TestProjectIgnoresEvidenceWithoutOperationParent(t *testing.T) {
	nonOperation := operationSpan("model-1", "browser_execute_case")
	nonOperation.Kind = domain.SpanModel
	evidence := []Evidence{
		{ID: "missing", SpanID: "missing-parent", Result: json.RawMessage(`{"steps":[]}`)},
		{ID: "wrong-kind", SpanID: nonOperation.SpanID, Result: json.RawMessage(`{"steps":[]}`)},
	}
	if nodes := Project([]domain.Span{nonOperation}, evidence); len(nodes) != 0 {
		t.Fatalf("node count = %d, want 0", len(nodes))
	}
}

func TestProjectUsesFirstEvidenceAndCanonicalSpanOrderDeterministically(t *testing.T) {
	firstParent := operationSpan("operation-2", "browser_execute_case")
	secondParent := operationSpan("operation-1", "browser_execute_case")
	firstEvidence := Evidence{
		ID:     "evidence-first",
		SpanID: secondParent.SpanID,
		Result: browserResult("first"),
	}
	evidence := []Evidence{
		firstEvidence,
		{ID: "evidence-ignored", SpanID: secondParent.SpanID, Result: browserResult("ignored")},
		{ID: "evidence-second-parent", SpanID: firstParent.SpanID, Result: browserResult("second")},
	}
	spans := []domain.Span{firstParent, secondParent}

	first := Project(spans, evidence)
	second := Project(spans, evidence)
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("repeated projection differs:\nfirst:  %#v\nsecond: %#v", first, second)
	}
	if len(first) != 2 {
		t.Fatalf("node count = %d, want 2", len(first))
	}
	if got := first[0].ID; got != "derived:operation-2:step:0" {
		t.Fatalf("first node ID = %q", got)
	}
	if got := first[1].SourceEvidenceID; got != firstEvidence.ID {
		t.Fatalf("second source evidence ID = %q, want %q", got, firstEvidence.ID)
	}
	if got := first[1].Name; got != "Step 1: first" {
		t.Fatalf("second node name = %q", got)
	}
}

func TestNodeJSONUsesTraceResponseFieldNames(t *testing.T) {
	encoded, err := json.Marshal(Node{
		ID:               "derived:operation-1:unknown_tool",
		ParentSpanID:     "operation-1",
		SourceEvidenceID: "evidence-1",
		Kind:             "unknown_tool",
		Name:             "unknown_tool",
		Status:           "failed",
		StartedAt:        time.Date(2026, 10, 23, 10, 0, 0, 0, time.UTC),
		Data:             json.RawMessage(`{"result":null}`),
		Evidence:         json.RawMessage(`{"artifacts":null,"error":null}`),
	})
	if err != nil {
		t.Fatalf("marshal node: %v", err)
	}
	body := string(encoded)
	for _, field := range []string{
		`"id"`,
		`"parent_span_id"`,
		`"source_evidence_id"`,
		`"kind"`,
		`"name"`,
		`"status"`,
		`"started_at"`,
		`"ended_at":null`,
		`"data"`,
		`"evidence"`,
	} {
		if !strings.Contains(body, field) {
			t.Fatalf("encoded node %s does not contain %s", body, field)
		}
	}
}

func operationSpan(spanID, name string) domain.Span {
	return domain.Span{
		ID:        spanID,
		SpanID:    spanID,
		Kind:      domain.SpanOperation,
		Name:      name,
		StartedAt: time.Date(2026, 10, 23, 9, 59, 0, 0, time.UTC),
	}
}

func browserResult(action string) json.RawMessage {
	return json.RawMessage(`{"steps":[{
		"index":0,
		"action":"` + action + `",
		"status":"passed",
		"started_at":"2026-10-23T10:00:00Z",
		"duration_ms":1,
		"evidence":{}
	}]}`)
}

func assertUnknownTool(t *testing.T, nodes []Node, parentSpanID, evidenceID string) {
	t.Helper()
	if len(nodes) != 1 {
		t.Fatalf("node count = %d, want 1", len(nodes))
	}
	if got := nodes[0].Kind; got != "unknown_tool" {
		t.Fatal(got)
	}
	if got := nodes[0].ID; got != "derived:"+parentSpanID+":unknown_tool" {
		t.Fatal(got)
	}
	if got := nodes[0].ParentSpanID; got != parentSpanID {
		t.Fatalf("parent span ID = %q, want %q", got, parentSpanID)
	}
	if got := nodes[0].SourceEvidenceID; got != evidenceID {
		t.Fatalf("source evidence ID = %q, want %q", got, evidenceID)
	}
}
