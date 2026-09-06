package execution

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestFailureSignalGoldenContract(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "failure_signal_contract.json"))
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		ContractVersion string `json:"contract_version"`
		Cases           []struct {
			Name     string          `json:"name"`
			Expected json.RawMessage `json:"expected"`
		} `json:"cases"`
		LegacyV1 json.RawMessage `json:"legacy_v1"`
	}
	if decodeErr := json.Unmarshal(raw, &fixture); decodeErr != nil {
		t.Fatal(decodeErr)
	}
	if fixture.ContractVersion != FailureSignalV2 {
		t.Fatalf("contract version = %q, want %q", fixture.ContractVersion, FailureSignalV2)
	}

	categories := make(map[string]bool)
	for _, testCase := range fixture.Cases {
		t.Run(testCase.Name, func(t *testing.T) {
			signal, signalErr := DecodeFailureSignal(testCase.Expected)
			if signalErr != nil {
				t.Fatalf("DecodeFailureSignal() error = %v", signalErr)
			}
			categories[signal.Category] = true
			if signal.SourceReference == nil || signal.SourceReference.ExecutionID < 1 {
				t.Fatalf("source reference = %#v", signal.SourceReference)
			}
			if signal.AgentEventReference != nil {
				t.Fatalf("fixture fabricated agent event reference = %#v", signal.AgentEventReference)
			}
		})
	}
	for _, category := range []string{
		"configuration", "locator", "assertion", "navigation", "network", "runner",
	} {
		if !categories[category] {
			t.Errorf("golden fixture does not cover category %q", category)
		}
	}

	legacy, err := DecodeFailureSignal(fixture.LegacyV1)
	if err != nil {
		t.Fatalf("decode legacy v1: %v", err)
	}
	if legacy.SchemaVersion != "" || legacy.SideEffectCommitted != nil {
		t.Fatalf("legacy signal was assigned v2 facts: %#v", legacy)
	}
	if legacy.AllowsOriginalActionReplay() {
		t.Fatal("legacy signal must not allow original action replay")
	}
}

func TestFailureSignalReplayRequiresExplicitNotCommitted(t *testing.T) {
	notCommitted := false
	committed := true
	base := FailureSignal{SchemaVersion: FailureSignalV2}

	base.SideEffectCommitted = &notCommitted
	if !base.AllowsOriginalActionReplay() {
		t.Fatal("explicit not-committed v2 signal should allow replay")
	}
	base.SideEffectCommitted = &committed
	if base.AllowsOriginalActionReplay() {
		t.Fatal("committed side effect must forbid replay")
	}
	base.SideEffectCommitted = nil
	if base.AllowsOriginalActionReplay() {
		t.Fatal("unknown side effect must forbid replay")
	}
}
