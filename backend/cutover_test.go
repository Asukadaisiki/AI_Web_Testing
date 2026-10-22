package backend_test

import (
	"os/exec"
	"strings"
	"testing"
)

func TestDefaultBuildHasNoSQLiteDependency(t *testing.T) {
	command := exec.Command(
		"go",
		"list",
		"-deps",
		"./cmd/agenttrace-api",
		"./cmd/agenttrace-runtime",
		"./cmd/agenttrace-exporter",
	)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("list default dependencies: %v\n%s", err, output)
	}
	if strings.Contains(string(output), "modernc.org/sqlite") {
		t.Fatalf("default build still depends on modernc.org/sqlite")
	}
}
