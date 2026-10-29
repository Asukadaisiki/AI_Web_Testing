package event

import (
	"testing"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/execution"
)

func TestManagedCommandRejectsHistoricalExecution(t *testing.T) {
	valid := RunCommand{RunID: "run", ConversationID: "conv", ProjectID: "project", EnvironmentID: "env", ProfileGeneration: 1,
		Execution: &execution.Spec{RuntimeID: "pi", Harness: execution.HarnessSnapshot{
			ID: "harness", Name: "Minimal", Revision: 1, ProjectID: "project", VersionID: "version",
			ContentDigest: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", RuntimeIDs: []string{"pi"},
		}}}
	if err := valid.ValidateManagedExecution(); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*RunCommand){
		func(c *RunCommand) { c.Execution = nil },
		func(c *RunCommand) { c.Execution.Harness.ProjectID = "" },
		func(c *RunCommand) { c.Execution.Harness.VersionID = "" },
		func(c *RunCommand) { c.Execution.Harness.ContentDigest = "" },
		func(c *RunCommand) { c.Execution.Harness.ProjectID = "other" },
		func(c *RunCommand) { c.EnvironmentID = "" },
		func(c *RunCommand) { c.ProfileGeneration = 0 },
	} {
		c := valid
		c.Execution = valid.Execution.Clone()
		mutate(&c)
		if err := c.ValidateManagedExecution(); err == nil {
			t.Fatalf("accepted historical or unscoped execution: %#v", c)
		}
	}
}
