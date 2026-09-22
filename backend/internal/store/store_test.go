package store

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/Asukadaisiki/AI_Web_Testing/v2/backend/internal/contract"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "loop.db")
	store, err := Open(path)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

func TestRunLifecycleAndEventSequence(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)

	run, err := store.CreateRun(ctx, "check the cart", nil)
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	if run.Status != StatusPlanning {
		t.Fatalf("status = %q", run.Status)
	}

	for index := 1; index <= 3; index++ {
		event, err := store.AppendEvent(ctx, run.ID, "tool_call", map[string]any{"index": index})
		if err != nil {
			t.Fatalf("append event: %v", err)
		}
		if event.Seq != int64(index) {
			t.Fatalf("seq = %d, want %d", event.Seq, index)
		}
	}

	replayed, err := store.ListEvents(ctx, run.ID, 1)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if len(replayed) != 2 || replayed[0].Seq != 2 || replayed[1].Seq != 3 {
		t.Fatalf("replayed = %#v", replayed)
	}

	if err := store.UpdateRunStatus(ctx, run.ID, StatusAwaitingApproval, nil); err != nil {
		t.Fatalf("update status: %v", err)
	}
	reloaded, err := store.GetRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	if reloaded.Status != StatusAwaitingApproval {
		t.Fatalf("status = %q", reloaded.Status)
	}

	runs, err := store.ListRuns(ctx, 10)
	if err != nil {
		t.Fatalf("list runs: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("runs = %#v", runs)
	}
}

func TestCaseArtifactApprovalAndExecution(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)

	run, err := store.CreateRun(ctx, "check the cart", nil)
	if err != nil {
		t.Fatalf("create run: %v", err)
	}

	caseValue, err := contract.DeriveGotoStep(0, "open the list", "https://shop.test/products")
	if err != nil {
		t.Fatalf("derive goto: %v", err)
	}
	artifact := contract.Case{
		CaseVersion: contract.CaseVersion,
		Name:        "list",
		Goal:        "check the cart",
		BaseURL:     "https://shop.test",
		Steps:       []contract.Step{caseValue},
	}
	if err := artifact.Validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}

	saved, err := store.SaveCase(ctx, run.ID, artifact)
	if err != nil {
		t.Fatalf("save case: %v", err)
	}
	if saved.ContentHash != artifact.ContentHash() {
		t.Fatalf("hash = %q, want %q", saved.ContentHash, artifact.ContentHash())
	}

	// 落库形态必须能直接过契约校验：不存在"第二副身子"。
	if _, err := contract.Validate(saved.Payload); err != nil {
		t.Fatalf("persisted payload must be executable: %v", err)
	}

	approved, err := store.IsCaseApproved(ctx, saved.ID)
	if err != nil {
		t.Fatalf("is approved: %v", err)
	}
	if approved {
		t.Fatal("case must not be approved before approval")
	}
	if err := store.ApproveCase(ctx, saved.ID, "owner"); err != nil {
		t.Fatalf("approve: %v", err)
	}
	approved, err = store.IsCaseApproved(ctx, saved.ID)
	if err != nil {
		t.Fatalf("is approved: %v", err)
	}
	if !approved {
		t.Fatal("case must be approved")
	}

	result := contract.ExecutionResult{
		ExecutionID: "exec_test",
		Status:      contract.ExecutionPassed,
		Steps: []contract.StepResult{{
			Index:  0,
			Action: contract.ActionGoto,
			Status: "passed",
			Evidence: contract.Evidence{
				ScreenshotPath: "data/artifacts/exec_test_0.png",
			},
		}},
	}
	execution, err := store.SaveExecution(ctx, run.ID, saved.ID, result)
	if err != nil {
		t.Fatalf("save execution: %v", err)
	}
	if execution.Status != string(contract.ExecutionPassed) {
		t.Fatalf("execution status = %q", execution.Status)
	}
	var decoded contract.ExecutionResult
	if err := json.Unmarshal(execution.Result, &decoded); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if len(decoded.Steps) != 1 || decoded.Steps[0].Evidence.ScreenshotPath == "" {
		t.Fatalf("decoded = %#v", decoded)
	}
}

func TestSignalsAndFeedbackRoundTrip(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)

	run, err := store.CreateRun(ctx, "check the cart", nil)
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	signals := []Signal{
		{RunID: run.ID, ExecutionID: "exec_test", StepIndex: 2, Kind: string(contract.SignalTargetNotFound), Message: "not found"},
		{RunID: run.ID, ExecutionID: "exec_test", StepIndex: 3, Kind: string(contract.SignalConditionUnmet), Message: "unmet"},
	}
	if err := store.ReplaceSignals(ctx, run.ID, "exec_test", signals); err != nil {
		t.Fatalf("replace signals: %v", err)
	}
	stored, err := store.ListSignals(ctx, run.ID)
	if err != nil {
		t.Fatalf("list signals: %v", err)
	}
	if len(stored) != 2 || stored[0].StepIndex != 2 {
		t.Fatalf("signals = %#v", stored)
	}

	if err := store.ReplaceFeedback(ctx, run.ID, []FeedbackCandidate{
		{RunID: run.ID, SignalKind: string(contract.SignalTargetNotFound), ProposedInput: "retry"},
	}); err != nil {
		t.Fatalf("replace feedback: %v", err)
	}
	candidates, err := store.ListFeedback(ctx, run.ID)
	if err != nil {
		t.Fatalf("list feedback: %v", err)
	}
	if len(candidates) != 1 || candidates[0].Status != "pending" {
		t.Fatalf("candidates = %#v", candidates)
	}
	single, err := store.GetFeedback(ctx, candidates[0].ID)
	if err != nil {
		t.Fatalf("get feedback: %v", err)
	}
	if single.ProposedInput != "retry" {
		t.Fatalf("candidate = %#v", single)
	}
	if err := store.MarkFeedbackUsed(ctx, single.ID); err != nil {
		t.Fatalf("mark used: %v", err)
	}
	reloaded, err := store.GetFeedback(ctx, single.ID)
	if err != nil {
		t.Fatalf("get feedback: %v", err)
	}
	if reloaded.Status != "used" {
		t.Fatalf("status = %q", reloaded.Status)
	}
}

func TestGetMissingRecordsReturnsNotFound(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	if _, err := store.GetRun(ctx, "run_missing"); err == nil {
		t.Fatal("expected not found")
	}
	if _, err := store.GetCase(ctx, "run_missing"); err == nil {
		t.Fatal("expected not found")
	}
	if _, err := store.GetExecution(ctx, "run_missing"); err == nil {
		t.Fatal("expected not found")
	}
}
