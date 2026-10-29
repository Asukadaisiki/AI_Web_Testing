package main

import (
	"bytes"
	"testing"
)

func TestSeedCLIRejectsIncompleteInputsWithoutConnecting(t *testing.T) {
	t.Setenv("AGENTTRACE_LANGFUSE_PROJECT_ID", "")
	t.Setenv("AGENTTRACE_PROJECT_ID", "")
	t.Setenv("AGENTTRACE_CONVERSATION_ID", "")
	t.Setenv("AGENTTRACE_BUILTIN_HARNESS_DIR", "")
	t.Setenv("AGENTTRACE_POSTGRES_DSN", "")
	for _, args := range [][]string{
		{}, {"--langfuse-project-id", "lf"}, {"--unexpected"}, {"positional"},
		{"--langfuse-project-id", "lf", "--resources-dir", ".", "--timeout", "0s"},
	} {
		var output bytes.Buffer
		if err := run(args, &output); err == nil || output.Len() != 0 {
			t.Fatalf("invalid CLI accepted: %v", args)
		}
	}
}
