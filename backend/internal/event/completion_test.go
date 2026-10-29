package event

import (
	"encoding/json"
	"testing"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/domain"
)

func TestRunCompletionContract(t *testing.T) {
	for _, status := range []domain.RunStatus{domain.RunSucceeded, domain.RunFailed, domain.RunCancelled} {
		raw, err := json.Marshal(RunCompletion{RunID: "run-1", Status: status})
		if err != nil {
			t.Fatal(err)
		}
		var fields map[string]any
		if err := json.Unmarshal(raw, &fields); err != nil {
			t.Fatal(err)
		}
		if fields["run_id"] != "run-1" || fields["status"] != string(status) || fields["error"] != nil {
			t.Fatalf("completion wire contract = %s", raw)
		}
		if _, err := DecodeRunCompletion(RunCompleted, "run-1", raw); err != nil {
			t.Fatal(err)
		}
	}
}

func TestRunCompletionRejectsMissingOrMismatchedIdentityAndNonterminalStatus(t *testing.T) {
	for _, raw := range []string{
		`{"status":"succeeded"}`,
		`{"run_id":"other-run","status":"succeeded"}`,
		`{"run_id":"run-1","status":"running"}`,
	} {
		if _, err := DecodeRunCompletion(RunCompleted, "run-1", json.RawMessage(raw)); err == nil {
			t.Fatalf("accepted invalid completion %s", raw)
		}
	}
}

func TestQueuedLegacyCompletionsDecodeWithoutWeakeningNewContract(t *testing.T) {
	for kind, raw := range map[string]string{
		"runtime.completed": `{"RunID":"run-1","Status":"succeeded"}`,
		"harness.completed": `{"status":"cancelled"}`,
	} {
		payload, err := DecodeRunCompletion(kind, "run-1", json.RawMessage(raw))
		if err != nil || payload.RunID != "run-1" {
			t.Fatalf("decode queued %s = %#v, %v", kind, payload, err)
		}
	}
	if _, err := DecodeRunCompletion("harness.completed", "run-1", json.RawMessage(`{"run_id":"wrong","status":"succeeded"}`)); err == nil {
		t.Fatal("accepted mismatched legacy identity")
	}
}
