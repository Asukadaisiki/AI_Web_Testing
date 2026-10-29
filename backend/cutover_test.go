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

func TestOperatorFilesDescribePortableManagedStartup(t *testing.T) {
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
	if strings.Contains(envExample, "AGENTTRACE_MODEL_SCRIPT=") {
		t.Error(".env.example must not set AGENTTRACE_MODEL_SCRIPT; the Pi harness is the production path")
	}
	if !strings.Contains(envExample, "AGENTTRACE_PI_HARNESS_ENTRY=") {
		t.Error(".env.example must configure the Pi harness entry point")
	}

	hostDockerfile := read("pi-browser-harness/Dockerfile")
	if strings.Contains(hostDockerfile, "goproxy.cn") {
		t.Error("host build must not force a region-specific Go proxy")
	}
	for _, entry := range []string{"AGENTTRACE_PI_HARNESS_ENTRY=", "AGENTTRACE_BUILTIN_HARNESS_DIR=", "AGENTTRACE_BUILTIN_MCP_ENTRY=", `CMD ["go", "run", "./cmd/agenttrace-runtime"]`} {
		if !strings.Contains(hostDockerfile, entry) {
			t.Errorf("host image must configure managed runtime entry/resource: %s", entry)
		}
	}
	dockerfile := read("worker/Dockerfile")
	if !strings.Contains(dockerfile, `CMD ["uv", "run", "--no-sync", "uvicorn", "loop_worker.main:app"`) {
		t.Error("worker image must define its explicit service command")
	}
	for _, retired := range []string{"AGENTTRACE_MANAGEMENT_ENABLED=", "AGENTTRACE_HARNESS_MODULE=", "AGENTTRACE_LOCAL_WEB_PORT="} {
		if strings.Contains(envExample, retired) {
			t.Errorf("active environment example configures retired mode: %s", retired)
		}
	}
	for _, required := range []string{"AGENTTRACE_BRIDGE_TOKEN=", "AGENTTRACE_BROWSER_PROVIDER_TOKEN=", "AGENTTRACE_LANGFUSE_PROJECT_ID=", "AGENTTRACE_REDIS_DB=3"} {
		if !strings.Contains(envExample, required) {
			t.Errorf("native managed example must include %s", required)
		}
	}
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
	if !strings.Contains(contract, "Pi browser harness") {
		t.Error("contract must describe the Pi browser harness production path")
	}
	if strings.Contains(contract, "model writes complete case JSON") {
		t.Error("contract must not claim the model writes complete case JSON")
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

func TestNativeManagedStartupEnablesParallelRuntimeDelivery(t *testing.T) {
	content, err := os.ReadFile(filepath.Join("..", "integration/local/run.py"))
	if err != nil {
		t.Fatalf("read native launcher: %v", err)
	}
	launcher := string(content)
	for _, required := range []string{`"--partitions", "8"`, "PartitionCount:", "configure_kafka_topic(topic, env)", `"agenttrace.environment.commands.v1"`, `"AGENTTRACE_RUNTIME_CONCURRENCY": "4"`, "seed_management(env)"} {
		if !strings.Contains(launcher, required) {
			t.Errorf("native startup lacks managed delivery contract: %s", required)
		}
	}
	if strings.Contains(launcher, "management_enabled") || strings.Contains(launcher, `ROOT / "web"`) {
		t.Error("native startup must have one mandatory managed plan")
	}
}

func TestRetiredProductPathsAreAbsent(t *testing.T) {
	for _, path := range []string{"web", "compose.yaml", "integration/v3", "run_v3_tests.py", "run_v3_tests_test.py",
		"integration/local/acceptance.py", "integration/local/composition.py", "integration/local/composition_test.py",
		"integration/local/faults.py", "integration/local/worker_audit.py"} {
		if _, err := os.Stat(filepath.Join("..", path)); !os.IsNotExist(err) {
			t.Errorf("retired product path still exists: %s", path)
		}
	}
}
