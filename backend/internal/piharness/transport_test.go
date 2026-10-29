package piharness

func sampleRequest() Request {
	return Request{
		ProtocolVersion: ProtocolVersion,
		RunID:           "run_test",
		AttemptID:       "attempt_test",
		Goal:            "verify",
		Model: ModelRequest{
			Provider: "scripted", Name: "fixture", ReasoningEffort: "high",
		},
		WorkerURL: "http://worker:8100", ArtifactScope: "run_test",
	}
}
