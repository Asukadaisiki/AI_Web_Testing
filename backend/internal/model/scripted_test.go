package model

import (
	"context"
	"testing"
)

func TestScriptedReplaysFixtureIndependentlyForEachRun(t *testing.T) {
	scripted := NewCheckpointedScripted([]ScriptedStep{
		{Tool: "inspect"},
		{Final: "complete"},
	})

	first, err := scripted.CompleteForRun(context.Background(), "run-one", 0, Request{})
	if err != nil {
		t.Fatalf("complete first run step: %v", err)
	}
	if len(first.Message.ToolCalls) != 1 || first.Message.ToolCalls[0].Name != "inspect" {
		t.Fatalf("first run first response = %#v", first.Message)
	}

	second, err := scripted.CompleteForRun(context.Background(), "run-two", 0, Request{})
	if err != nil {
		t.Fatalf("complete second run step: %v", err)
	}
	if len(second.Message.ToolCalls) != 1 || second.Message.ToolCalls[0].Name != "inspect" {
		t.Fatalf("second run first response = %#v", second.Message)
	}

	resumed, err := scripted.CompleteForRun(context.Background(), "run-one", 1, Request{})
	if err != nil {
		t.Fatalf("resume first run: %v", err)
	}
	if resumed.Message.Content != "complete" {
		t.Fatalf("resumed response = %#v, want final fixture response", resumed.Message)
	}
}
