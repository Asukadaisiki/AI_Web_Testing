package event

import "errors"

// ValidateManagedExecution is admission policy, not historical decoding.
func (c RunCommand) ValidateManagedExecution() error {
	if c.RunID == "" || c.ConversationID == "" || c.ProjectID == "" || c.EnvironmentID == "" ||
		c.ProfileGeneration < 1 || c.Execution == nil {
		return errors.New("managed command requires execution, project, conversation, environment and generation")
	}
	h := c.Execution.Harness
	if h.ProjectID != c.ProjectID || h.VersionID == "" || h.ContentDigest == "" || len(h.RuntimeIDs) == 0 {
		return errors.New("managed command requires a project registry snapshot")
	}
	return c.Execution.Validate()
}
