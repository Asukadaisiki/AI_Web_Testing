package main

import "testing"

func TestParseRunID(t *testing.T) {
	runID, err := parseRunID([]string{"--run-id", " run-1 "})
	if err != nil {
		t.Fatal(err)
	}
	if runID != "run-1" {
		t.Fatalf("run id = %q", runID)
	}
}

func TestParseRunIDRejectsMissingAndExtraArguments(t *testing.T) {
	if _, err := parseRunID(nil); err == nil {
		t.Fatal("missing run id was accepted")
	}
	if _, err := parseRunID([]string{"--run-id", "run-1", "extra"}); err == nil {
		t.Fatal("extra argument was accepted")
	}
}
