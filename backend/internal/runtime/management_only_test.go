package runtime

import (
	"context"
	"testing"
)

func TestPiRunnerRejectsUnleasedAttempt(t *testing.T) {
	s := &fakePiStore{unleased: true}
	p := &fakeHarnessProcess{}
	err := (PiRunner{Store: s, Process: p}).Run(context.Background(), commandFor("unleased"))
	if err == nil || p.calls != 0 || s.completed {
		t.Fatalf("unleased execution operated: err=%v calls=%d completed=%v", err, p.calls, s.completed)
	}
}
