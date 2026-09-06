package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/agent"
	dslstore "github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/dsl"
)

type preflightFailureBrowser struct{}

func (preflightFailureBrowser) ExecuteBrowserCapability(
	context.Context,
	string,
	int64,
	int64,
	string,
	json.RawMessage,
) (json.RawMessage, error) {
	return json.RawMessage(`{
		"dsl_case":{"name":"Example","steps":[{"action":"click","target":"Missing"}]},
		"valid":false,
		"warnings":["Step 0: match_count=0"]
	}`), nil
}

type unboundPreflightBrowser struct{}

type evidenceCapturingBrowser struct {
	evidence map[string][]json.RawMessage
}

func (unboundPreflightBrowser) ExecuteBrowserCapability(
	context.Context,
	string,
	int64,
	int64,
	string,
	json.RawMessage,
) (json.RawMessage, error) {
	return json.RawMessage(`{
		"dsl_case":{"name":"Example","steps":[{"action":"click","target":"Login"}]},
		"valid":true,
		"validation_mode":"dsl_case",
		"case_digest":"wrong",
		"evidence_digest":"wrong"
	}`), nil
}

func (browser *evidenceCapturingBrowser) ExecuteBrowserCapability(
	_ context.Context,
	capability string,
	_ int64,
	_ int64,
	_ string,
	arguments json.RawMessage,
) (json.RawMessage, error) {
	if capability != "validate_page_elements" {
		return nil, nil
	}
	var request struct {
		Evidence map[string][]json.RawMessage `json:"a11y_nodes_by_state"`
	}
	if err := json.Unmarshal(arguments, &request); err != nil {
		return nil, err
	}
	browser.evidence = request.Evidence
	return json.RawMessage(`{
		"dsl_case":{"name":"Summary Evidence","steps":[{"action":"click","target":"#login"}]},
		"valid":false,
		"warnings":["stop after evidence capture"]
	}`), nil
}

func TestGenerateDSLReturnsPreflightWarnings(t *testing.T) {
	capabilities := NewControlPlaneCapabilities(
		dslstore.NewStore(nil),
		nil,
		nil,
		preflightFailureBrowser{},
	)

	_, err := capabilities.GenerateDSL(
		context.Background(),
		1,
		1,
		"1",
		json.RawMessage(`{
			"case":{"name":"Example","steps":[{"action":"click","target":"Missing"}]},
			"a11y_nodes_by_state":{"S0":[]}
		}`),
	)

	if err == nil || !strings.Contains(err.Error(), "Step 0: match_count=0") {
		t.Fatalf("GenerateDSL() error = %v, want preflight warning", err)
	}
}

func TestGenerateDSLRejectsUnboundPreflightResult(t *testing.T) {
	capabilities := NewControlPlaneCapabilities(
		dslstore.NewStore(nil),
		nil,
		nil,
		unboundPreflightBrowser{},
	)

	_, err := capabilities.GenerateDSL(
		context.Background(),
		1,
		1,
		"1",
		json.RawMessage(`{
			"case":{"name":"Example","steps":[{"action":"click","target":"Login"}]},
			"a11y_nodes_by_state":{"login":[{"role":"button","name":"Login"}]}
		}`),
	)

	if err == nil || !strings.Contains(err.Error(), "not bound") {
		t.Fatalf("GenerateDSL() error = %v, want binding error", err)
	}
}

func TestGenerateDSLPreflightReceivesModelSubmittedSummaryEvidence(t *testing.T) {
	raw := json.RawMessage(`{
		"url":"https://example.com/login",
		"page_state":"S0",
		"element_count":2,
		"a11y_nodes":[
			{"node_id":"e1","role":"button","name":"Login","page_state":"S0","focusable":true,
			 "verified_selectors":[{"strategy":"css","selector":"#login","source":"dom"}]},
			{"node_id":"raw-only","role":"generic","name":"raw-only-node","page_state":"S0"}
		]
	}`)
	modelContent, err := agent.BuildModelToolSummary("explore_page", raw, 12)
	if err != nil {
		t.Fatal(err)
	}
	var summary agent.ModelToolSummary
	if err := json.Unmarshal([]byte(modelContent), &summary); err != nil {
		t.Fatal(err)
	}
	submitted, err := json.Marshal(map[string]any{
		"case": map[string]any{
			"name": "Summary Evidence",
			"steps": []map[string]any{{
				"action": "click", "target": "#login",
			}},
		},
		"a11y_nodes_by_state": map[string]any{
			"S0": summary.Pages[0].A11yNodes,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	browser := &evidenceCapturingBrowser{}
	capabilities := NewControlPlaneCapabilities(
		dslstore.NewStore(nil),
		nil,
		nil,
		browser,
	)
	_, err = capabilities.GenerateDSL(context.Background(), 1, 1, "1", submitted)
	if err == nil || !strings.Contains(err.Error(), "stop after evidence capture") {
		t.Fatalf("GenerateDSL() error = %v", err)
	}
	nodes := browser.evidence["S0"]
	if len(nodes) != 1 ||
		!strings.Contains(string(nodes[0]), `"selector":"#login"`) ||
		strings.Contains(string(nodes[0]), "raw-only-node") {
		t.Fatalf("preflight evidence = %s", nodes)
	}
}

func TestCanonicalJSONDigestMatchesWorkerContract(t *testing.T) {
	value := map[string][]json.RawMessage{
		"S0": {
			json.RawMessage(`{"value":"<x>","score":1.0,"name":"登录"}`),
		},
	}
	digest, err := canonicalJSONDigest(value)
	if err != nil {
		t.Fatalf("canonicalJSONDigest() error = %v", err)
	}
	const want = "0a9d522ffd466c74c4ea5c86801e3a77be30d1300b0957602e09dec292242f1b"
	if digest != want {
		t.Fatalf("digest = %s, want %s", digest, want)
	}
}

func TestRepairDecisionAllowsOnlyExplicitlyUncommittedV2Signal(t *testing.T) {
	signal := map[string]any{
		"schema_version":        "failure.signal.v2",
		"category":              "locator",
		"fingerprint":           "locator-test",
		"title":                 "Element not found",
		"stage":                 "locator",
		"code":                  "locator.no_match",
		"retryable":             true,
		"side_effect_committed": false,
		"source_reference": map[string]any{
			"type": "execution_report", "execution_id": 42,
			"step_index": 1, "json_pointer": "/steps/1/action_outcome",
		},
	}

	status, strategy, _, replayAllowed := repairDecision([]map[string]any{signal})

	if status != "repair_ready" || strategy != "re_explore" || !replayAllowed {
		t.Fatalf(
			"decision = status %q, strategy %q, replay %t",
			status, strategy, replayAllowed,
		)
	}
}

func TestRepairDecisionForbidsCommittedUnknownAndV1Replay(t *testing.T) {
	for name, signal := range map[string]map[string]any{
		"committed": {
			"schema_version": "failure.signal.v2",
			"category":       "assertion", "fingerprint": "assertion-test",
			"title": "Postcondition failed", "stage": "postcondition",
			"code": "condition.postcondition.text_visible.failed", "retryable": false,
			"side_effect_committed": true,
			"source_reference": map[string]any{
				"type": "execution_report", "execution_id": 42,
				"step_index": 1, "json_pointer": "/steps/1/condition_results/0",
			},
		},
		"unknown": {
			"schema_version": "failure.signal.v2",
			"category":       "runner", "fingerprint": "runner-test",
			"title": "Dispatch result unknown", "stage": "action",
			"code": "action.unknown", "retryable": false,
			"side_effect_committed": nil,
			"source_reference": map[string]any{
				"type": "execution_report", "execution_id": 43,
				"step_index": 2, "json_pointer": "/steps/2/action_outcome",
			},
		},
		"legacy_v1": {
			"category": "locator", "fingerprint": "legacy-test",
			"title": "Element not found",
		},
	} {
		t.Run(name, func(t *testing.T) {
			status, strategy, reason, replayAllowed := repairDecision([]map[string]any{signal})
			if status != "manual_required" || strategy != "manual_reconcile" ||
				replayAllowed || !strings.Contains(reason, "must not be replayed") {
				t.Fatalf(
					"decision = status %q, strategy %q, reason %q, replay %t",
					status, strategy, reason, replayAllowed,
				)
			}
		})
	}
}
