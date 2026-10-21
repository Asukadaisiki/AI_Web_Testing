package domain

import "testing"

func TestRunStatusTransitions(t *testing.T) {
	cases := []struct {
		from, to RunStatus
		allowed  bool
	}{
		{RunQueued, RunRunning, true},
		{RunRunning, RunWaitingInput, true},
		{RunWaitingInput, RunRunning, true},
		{RunRunning, RunSucceeded, true},
		{RunSucceeded, RunRunning, false},
		{RunCancelled, RunRunning, false},
	}
	for _, tc := range cases {
		if got := CanTransition(tc.from, tc.to); got != tc.allowed {
			t.Fatalf("%s -> %s = %v", tc.from, tc.to, got)
		}
	}
}
