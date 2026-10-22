package backend_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultBuildHasNoSQLiteDependency(t *testing.T) {
	command := exec.Command("go", "list", "-deps", "./...")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("list default dependencies: %v\n%s", err, output)
	}
	if strings.Contains(string(output), "modernc.org/sqlite") {
		t.Fatalf("default build still depends on modernc.org/sqlite")
	}
}

func TestRetiredV2PathsAreAbsent(t *testing.T) {
	paths := []string{
		"internal/agentruntime",
		"internal/feedback",
		"internal/planner",
		"internal/report",
		"internal/worker",
		"../docs/execution-log.md",
		"../fixtures/scripts/catalog_alpha.json",
		"../fixtures/scripts/ecommerce_login_cart.json",
		"../integration/ecommerce_closed_loop.py",
		"../run_tests.py",
		"../test.config.json",
		"../web/tests/verify_pages.py",
	}
	for _, path := range paths {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("retired v2 path still exists: %s", path)
		}
	}
}

func TestOperatorFilesDescribePortableV3Startup(t *testing.T) {
	root := filepath.Clean("..")
	read := func(path string) string {
		t.Helper()
		content, err := os.ReadFile(filepath.Join(root, path))
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		return string(content)
	}

	readme := read("README.md")
	startup := strings.SplitN(readme, "## Verify", 2)[0]
	if strings.Contains(startup, "docker compose down -v") {
		t.Error("normal startup documentation must not delete Compose volumes")
	}

	envExample := read(".env.example")
	if !strings.Contains(
		envExample,
		"AGENTTRACE_MODEL_SCRIPT=/app/fixtures/scripts/agenttrace_browser_eval.json",
	) {
		t.Error(".env.example must preserve the canonical scripted browser fixture")
	}

	compose := read("compose.yaml")
	if strings.Contains(compose, "goproxy.cn") {
		t.Error("Compose must not force a region-specific Go proxy")
	}
	dockerfile := read("worker/Dockerfile")
	if strings.Contains(dockerfile, "mirrors.tuna.tsinghua.edu.cn") {
		t.Error("worker build must not force a region-specific Debian mirror")
	}
	for _, buildArg := range []string{"UV_PACKAGE_BASE", "DEBIAN_MIRROR", "DEBIAN_SECURITY_MIRROR"} {
		if !strings.Contains(dockerfile, "ARG "+buildArg+"=") {
			t.Errorf("worker build must expose portable %s override", buildArg)
		}
	}
	for _, path := range []string{"worker/pyproject.toml", "worker/uv.lock"} {
		if strings.Contains(read(path), "pypi.tuna.tsinghua.edu.cn") {
			t.Errorf("%s must not force a region-specific Python index", path)
		}
	}

	contract := read("CONTRACT.md")
	if !strings.Contains(contract, "scripted model fixture supplies the complete browser case JSON") {
		t.Error("contract must describe the scripted v3 browser case source")
	}
	if strings.Contains(contract, "models do not write selectors, conditions, or raw case JSON") {
		t.Error("contract still describes the retired planner behavior")
	}
	if !strings.Contains(contract, "model-supplied grounding metadata") {
		t.Error("contract must not claim model-supplied grounding is observation-verified")
	}

	for _, path := range []string{
		"backend/internal/contract/contract.go",
		"worker/loop_worker/contracts.py",
	} {
		if strings.Contains(read(path), "grounded against a real observation") {
			t.Errorf("%s still claims unverified grounding came from a real observation", path)
		}
	}
	workerPackage := read("worker/loop_worker/__init__.py")
	if strings.Contains(workerPackage, "v2") ||
		strings.Contains(workerPackage, "`v2/CONTRACT.md`") {
		t.Error("worker package documentation still points to the retired v2 contract")
	}
}
