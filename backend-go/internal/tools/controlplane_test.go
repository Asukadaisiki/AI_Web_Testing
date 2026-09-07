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
			"case":{"profile":"research-v1","name":"Example","steps":[{
				"action":"click","intent":"Click missing element","target":"Missing",
				"preconditions":[{"type":"element_visible","value":"Missing"}],
				"postconditions":[{"type":"url_changes"}],
				"idempotency":"idempotent","side_effect":"browser_state"
			}]},
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
			"case":{"profile":"research-v1","name":"Example","steps":[{
				"action":"click","intent":"Click login","target":"Login",
				"preconditions":[{"type":"element_visible","value":"Login"}],
				"postconditions":[{"type":"url_changes"}],
				"idempotency":"idempotent","side_effect":"browser_state"
			}]},
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
			"profile": "research-v1",
			"name":    "Summary Evidence",
			"steps": []map[string]any{{
				"action": "click", "intent": "Click login", "target": "Login",
				"preconditions": []map[string]any{{
					"type": "element_visible", "value": "Login",
				}},
				"postconditions": []map[string]any{{"type": "url_changes"}},
				"idempotency":    "idempotent",
				"side_effect":    "browser_state",
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

func TestStripPreflightMetadataPreservesExecutableEvidence(t *testing.T) {
	draft := json.RawMessage(`{
		"profile":"research-v1",
		"name":"Login",
		"steps":[{
			"action":"wait_for","intent":"Wait for login","target":"Login",
			"page_state":"draft-state",
			"preconditions":[],"postconditions":[],
			"idempotency":"idempotent","side_effect":"none"
		}]
	}`)
	raw := json.RawMessage(`{
		"profile":"research-v1",
		"name":"Login",
		"_preflight":{"locator_confidence":"high"},
		"steps":[{
			"action":"wait_for","intent":"Wait for login","target":"Login",
			"page_state":"S0",
			"preconditions":[],"postconditions":[],
			"idempotency":"idempotent","side_effect":"none",
			"match_count":1,"locator_confidence":"high",
			"candidates":[{
				"strategy":"verified_css","selector":"#login","semantic_value":"Login",
				"pre_score":1,
				"pre_features":{"verified":true,"source":"a11y_backend_dom_node"}
			}]
		}]
	}`)
	sanitized, err := stripPreflightMetadata(raw, draft)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(sanitized), "_preflight") ||
		strings.Contains(string(sanitized), "match_count") {
		t.Fatalf("sanitized case = %s", sanitized)
	}
	if !strings.Contains(string(sanitized), `"page_state":"S0"`) {
		t.Fatalf("sanitized case did not merge preflight page state: %s", sanitized)
	}
	if _, err := dslstore.ValidateExecutableCase(sanitized); err != nil {
		t.Fatalf("ValidateExecutableCase() error = %v", err)
	}
}

func TestPreparePreflightCaseOmitsNonLocatorSemanticTargets(t *testing.T) {
	raw := json.RawMessage(`{
		"profile":"research-v1","name":"Navigation","steps":[
			{"action":"goto","intent":"Open cart","target":"Cart page","value":"/cart","preconditions":[],"postconditions":[],"idempotency":"idempotent","side_effect":"browser_state"},
			{"action":"wait_for","intent":"Wait for cart","target":"Cart heading","preconditions":[],"postconditions":[],"idempotency":"idempotent","side_effect":"none"}
		]
	}`)
	preflight, err := preparePreflightCase(raw)
	if err != nil {
		t.Fatal(err)
	}
	var candidate map[string]any
	if err := json.Unmarshal(preflight, &candidate); err != nil {
		t.Fatal(err)
	}
	steps := candidate["steps"].([]any)
	if steps[0].(map[string]any)["target"] != "Cart page" {
		t.Fatalf("goto preflight target changed: %s", preflight)
	}
	if steps[1].(map[string]any)["target"] != "Cart heading" {
		t.Fatalf("locator preflight target changed: %s", preflight)
	}
}

func TestCaseMutationPreservesResearchProfile(t *testing.T) {
	raw := json.RawMessage(`{
		"profile":"research-v1","name":"Navigation",
		"input_contract":[],"output_contract":[],
		"steps":[{
			"action":"goto","intent":"Open cart","target":"Cart page","value":"/cart",
			"preconditions":[],"postconditions":[{"type":"url_contains","value":"/cart"}],
			"idempotency":"idempotent","side_effect":"browser_state"
		}]
	}`)
	mutation, err := caseMutation(7, raw)
	if err != nil {
		t.Fatal(err)
	}
	if mutation.Profile == nil || *mutation.Profile != "research-v1" {
		t.Fatalf("case mutation profile = %#v", mutation.Profile)
	}
}

func TestStripPreflightMetadataRejectsBusinessSemanticChanges(t *testing.T) {
	draft := json.RawMessage(`{
		"profile":"research-v1","name":"Search","steps":[{
			"action":"input","intent":"Enter query","target":"Search field","value":"dress",
			"preconditions":[{"type":"element_visible","value":"Search field"}],
			"postconditions":[{"type":"value_changed","value":"dress"}],
			"idempotency":"idempotent","side_effect":"browser_state"
		}]
	}`)
	var base map[string]any
	if err := json.Unmarshal(draft, &base); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name   string
		mutate func(map[string]any)
	}{
		{"intent", func(step map[string]any) { step["intent"] = "Rewrite intent" }},
		{"target", func(step map[string]any) { step["target"] = "#search" }},
		{"value", func(step map[string]any) { step["value"] = "shoes" }},
		{"conditions", func(step map[string]any) { step["preconditions"] = []any{} }},
		{"idempotency", func(step map[string]any) { step["idempotency"] = "non_idempotent" }},
		{"side_effect", func(step map[string]any) { step["side_effect"] = "external_state" }},
		{"action", func(step map[string]any) { step["action"] = "click" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			clonedRaw, err := json.Marshal(base)
			if err != nil {
				t.Fatal(err)
			}
			var returned map[string]any
			if err := json.Unmarshal(clonedRaw, &returned); err != nil {
				t.Fatal(err)
			}
			step := returned["steps"].([]any)[0].(map[string]any)
			test.mutate(step)
			step["page_state"] = "S0"
			step["locator_confidence"] = "high"
			step["candidates"] = []any{map[string]any{
				"strategy": "verified_css", "selector": "#search",
				"semantic_value": "Search field", "pre_score": 1.0,
				"pre_features": map[string]any{
					"verified": true, "source": "a11y_backend_dom_node",
				},
			}}
			returnedRaw, err := json.Marshal(returned)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := stripPreflightMetadata(returnedRaw, draft); err == nil {
				t.Fatal("stripPreflightMetadata() error = nil")
			}
		})
	}
}

func TestStripPreflightMetadataRejectsStepCountChange(t *testing.T) {
	draft := json.RawMessage(`{
		"profile":"research-v1","name":"Navigation","steps":[{
			"action":"goto","intent":"Open cart","target":"Cart page","value":"/cart",
			"preconditions":[],"postconditions":[{"type":"url_contains","value":"/cart"}],
			"idempotency":"idempotent","side_effect":"browser_state"
		}]
	}`)
	returned := json.RawMessage(`{
		"profile":"research-v1","name":"Navigation","steps":[]
	}`)
	if _, err := stripPreflightMetadata(returned, draft); err == nil {
		t.Fatal("stripPreflightMetadata() error = nil")
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
