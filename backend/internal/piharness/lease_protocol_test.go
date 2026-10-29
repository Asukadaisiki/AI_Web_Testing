package piharness

import (
	"context"
	"encoding/json"
	"testing"
)

func TestManagedRequestRejectsPartialAndUnsafeLeaseIdentity(t *testing.T) {
	r := sampleRequest()
	r.ProjectID, r.EnvironmentID, r.ProfileGeneration, r.FencingToken = "p", "env", 1, 2
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	var decoded Request
	if err := json.Unmarshal(data, &decoded); err != nil || decoded.FencingToken != 2 {
		t.Fatalf("managed metadata round trip: %#v %v", decoded, err)
	}
	for _, invalid := range []Request{
		{ProjectID: "p"},
		{ProjectID: "p", EnvironmentID: "env", ProfileGeneration: 1, FencingToken: -1},
		{ProjectID: "p", EnvironmentID: "env", ProfileGeneration: 1, FencingToken: 9007199254740992},
	} {
		r.ProjectID, r.EnvironmentID, r.ProfileGeneration, r.FencingToken = invalid.ProjectID, invalid.EnvironmentID, invalid.ProfileGeneration, invalid.FencingToken
		if _, err := json.Marshal(r); err == nil {
			t.Fatalf("invalid lease metadata accepted: %#v", invalid)
		}
	}
}

func TestSharedTransportReleaseAcknowledgementDistinguishesCrash(t *testing.T) {
	for _, tc := range []struct {
		goal string
		ack  bool
	}{
		{"ok", true}, {"failed", true}, {"crash", false},
	} {
		t.Run(tc.goal, func(t *testing.T) {
			r := sharedTestRunner(t, "normal")
			ack, err := r.RunWithRelease(context.Background(), sharedRequest(tc.goal, tc.goal), nil)
			if ack != tc.ack || tc.goal == "ok" && err != nil || tc.goal != "ok" && err == nil {
				t.Fatalf("release acknowledgement=%v err=%v", ack, err)
			}
		})
	}
}
