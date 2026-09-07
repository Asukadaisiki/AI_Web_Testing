package research

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestOracleDecisionIsStructuredDeterministicAndTamperEvident(t *testing.T) {
	source := testSourceRef(
		t, SourceOracle, "oracle-artifact-1",
		Unavailable[int64]("oracle_has_no_global_sequence"),
	)
	decision, err := NewOracleDecision(OracleDecision{
		ID: "oracle-1", Evaluator: "cart-oracle.v1", Passed: false,
		ReasonCode: "cart.total_mismatch",
		DecisionFacts: []OracleDecisionFact{
			{
				Name: "total_price", Passed: false,
				Actual:   json.RawMessage(`{"currency":"USD","value":12}`),
				Expected: json.RawMessage(`{"currency":"USD","value":10}`),
				Sources:  []SourceRef{source},
			},
			{Name: "product_name", Passed: true, Sources: []SourceRef{source}},
		},
		Sources: []SourceRef{source},
	})
	if err != nil {
		t.Fatal(err)
	}
	if decision.SchemaVersion != OracleSchemaVersion ||
		decision.DecisionFacts[0].Name != "product_name" ||
		!sha256Pattern.MatchString(decision.ContentSHA256) {
		t.Fatalf("oracle decision = %#v", decision)
	}
	second, err := NewOracleDecision(OracleDecision{
		ID: "oracle-1", Evaluator: "cart-oracle.v1", Passed: false,
		ReasonCode: "cart.total_mismatch",
		DecisionFacts: []OracleDecisionFact{
			{Name: "product_name", Passed: true, Sources: []SourceRef{source}},
			{
				Name: "total_price", Passed: false,
				Actual:   json.RawMessage(`{ "value": 12, "currency": "USD" }`),
				Expected: json.RawMessage(`{"value":10,"currency":"USD"}`),
				Sources:  []SourceRef{source},
			},
		},
		Sources: []SourceRef{source},
	})
	if err != nil || second.ContentSHA256 != decision.ContentSHA256 {
		t.Fatalf("deterministic oracle hash = %s / %s, err = %v",
			decision.ContentSHA256, second.ContentSHA256, err)
	}
	decision.Passed = true
	if err := decision.NormalizeAndValidate(); !errors.Is(err, ErrInvalid) {
		t.Fatalf("tampered oracle error = %v, want ErrInvalid", err)
	}
}

func TestOracleDecisionRejectsInconsistentFactsAndSources(t *testing.T) {
	source := testSourceRef(
		t, SourceOracle, "oracle-artifact-1",
		Unavailable[int64]("oracle_has_no_global_sequence"),
	)
	orphan := testSourceRef(
		t, SourceOracle, "oracle-artifact-2",
		Unavailable[int64]("oracle_has_no_global_sequence"),
	)
	report := testSourceRef(t, SourceReport, "execution-1", Available(int64(1)))
	tests := []struct {
		name     string
		decision OracleDecision
	}{
		{
			name: "top-level true with false fact",
			decision: OracleDecision{
				ID: "oracle-1", Evaluator: "cart-oracle.v1", Passed: true,
				ReasonCode: "passed",
				DecisionFacts: []OracleDecisionFact{{
					Name: "cart_state", Passed: false, Sources: []SourceRef{source},
				}},
				Sources: []SourceRef{source},
			},
		},
		{
			name: "empty facts",
			decision: OracleDecision{
				ID: "oracle-1", Evaluator: "cart-oracle.v1", Passed: true,
				ReasonCode: "passed", Sources: []SourceRef{source},
			},
		},
		{
			name: "fact source outside top-level sources",
			decision: OracleDecision{
				ID: "oracle-1", Evaluator: "cart-oracle.v1", Passed: true,
				ReasonCode: "passed",
				DecisionFacts: []OracleDecisionFact{{
					Name: "cart_state", Passed: true, Sources: []SourceRef{orphan},
				}},
				Sources: []SourceRef{source},
			},
		},
		{
			name: "fact has no independent source",
			decision: OracleDecision{
				ID: "oracle-1", Evaluator: "cart-oracle.v1", Passed: true,
				ReasonCode: "passed",
				DecisionFacts: []OracleDecisionFact{{
					Name: "cart_state", Passed: true, Sources: []SourceRef{report},
				}},
				Sources: []SourceRef{source, report},
			},
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			if _, err := NewOracleDecision(testCase.decision); !errors.Is(err, ErrInvalid) {
				t.Fatalf("NewOracleDecision() error = %v, want ErrInvalid", err)
			}
		})
	}
}

func TestOracleDecisionFactValuesAreBoundedAndHashed(t *testing.T) {
	source := testSourceRef(
		t, SourceOracle, "oracle-artifact-1",
		Unavailable[int64]("oracle_has_no_global_sequence"),
	)
	decision, err := NewOracleDecision(OracleDecision{
		ID: "oracle-1", Evaluator: "cart-oracle.v1", Passed: false,
		ReasonCode: "cart.total_mismatch",
		DecisionFacts: []OracleDecisionFact{{
			Name: "total_price", Passed: false,
			Actual:   json.RawMessage(`{"value":12}`),
			Expected: json.RawMessage(`{"value":10}`),
			Sources:  []SourceRef{source},
		}},
		Sources: []SourceRef{source},
	})
	if err != nil {
		t.Fatal(err)
	}
	decision.DecisionFacts[0].Actual = json.RawMessage(`{"value":13}`)
	if err := decision.NormalizeAndValidate(); !errors.Is(err, ErrInvalid) {
		t.Fatalf("tampered fact value error = %v, want ErrInvalid", err)
	}

	_, err = NewOracleDecision(OracleDecision{
		ID: "oracle-2", Evaluator: "cart-oracle.v1", Passed: true,
		ReasonCode: "passed",
		DecisionFacts: []OracleDecisionFact{{
			Name: "cart_state", Passed: true,
			Actual:  json.RawMessage(`{"reasoning_content":"private"}`),
			Sources: []SourceRef{source},
		}},
		Sources: []SourceRef{source},
	})
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("sensitive fact value error = %v, want ErrInvalid", err)
	}
}

func TestControlledResearchWritesRejectRawReasoningFields(t *testing.T) {
	tests := []string{"reasoning_content", "thought", "scratchpad"}
	for _, key := range tests {
		t.Run(key, func(t *testing.T) {
			raw := `{
				"schema_version":"research.oracle.v1",
				"id":"oracle-1",
				"evaluator":"oracle.v1",
				"passed":true,
				"reason_code":"passed",
				"decision_facts":[],
				"sources":[],
				"content_sha256":"` + strings.Repeat("a", 64) + `",
				"` + key + `":"private"
			}`
			var decision OracleDecision
			if err := json.Unmarshal([]byte(raw), &decision); !errors.Is(err, ErrInvalid) {
				t.Fatalf("Unmarshal() error = %v, want ErrInvalid", err)
			}

			if _, err := TransitionContentSHA256(
				SchemaVersion,
				json.RawMessage(`{"nested":{"`+key+`":"private"}}`),
				nil,
			); !errors.Is(err, ErrInvalid) {
				t.Fatalf("TransitionContentSHA256() error = %v, want ErrInvalid", err)
			}
		})
	}
}

func TestHistoricalAgentEventsDoNotApplyReasoningFieldPolicy(t *testing.T) {
	for _, key := range []string{"reasoning_content", "thought", "scratchpad"} {
		t.Run(key, func(t *testing.T) {
			event := AgentEventSnapshot{
				Seq: 1, Type: "future.event",
				Payload: json.RawMessage(`{"nested":{"` + key + `":"private"}}`),
			}
			if err := validateAgentEventSchema(event); err != nil {
				t.Fatalf("validateAgentEventSchema() error = %v", err)
			}
		})
	}
}
