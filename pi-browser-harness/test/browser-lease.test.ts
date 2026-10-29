import { browserExecution, environmentLease } from "./fixtures/selected-harness.js";
import { expect, it, vi } from "vitest";
import { prepareHarnessResources } from "../src/harness-resources.js";
import type { RunRequest } from "../src/run-protocol.js";
import { WorkerClient } from "../src/browser/worker-client.js";

it("materializes environment routing and provider auth only in the host-owned MCP descriptor", async () => {
  vi.stubEnv("AGENTTRACE_BROWSER_PROVIDER_TOKEN", "host-only-token");
  const request: RunRequest = {
  execution: browserExecution, ...environmentLease,
    protocol_version: "pi-browser-harness.v1", run_id: "r", attempt_id: "a", goal: "verify",
    model: { provider: "scripted", name: "fixture", reasoning_effort: "off" },
    worker_url: "http://assigned:8100", artifact_scope: "r",
    project_id: "p", environment_id: "env", profile_generation: 2, fencing_token: 7,
  };
  try {
    const resources = await prepareHarnessResources(request);
    try {
      expect(resources.servers[0]?.env).toMatchObject({
        AGENTTRACE_HARNESS_PROJECT_ID: "p",
        AGENTTRACE_HARNESS_ENVIRONMENT_ID: "env",
        AGENTTRACE_HARNESS_PROFILE_GENERATION: "2",
        AGENTTRACE_HARNESS_FENCING_TOKEN: "7",
        AGENTTRACE_BROWSER_PROVIDER_TOKEN: "host-only-token",
      });
      expect(JSON.stringify(request)).not.toContain("host-only-token");
    } finally {
      await resources.dispose();
    }
  } finally {
    vi.unstubAllEnvs();
  }
});

it("fails closed when a managed browser has no credential or no held dryrun context", async () => {
  const environmentLease = { project_id: "p", environment_id: "env", profile_generation: 1, fencing_token: 2 };
  expect(() => new WorkerClient("http://localhost:8100", { environmentLease })).toThrow("host provider token");
  const client = new WorkerClient("http://localhost:8100", { environmentLease, providerToken: "host-only" });
  await expect(client.execute("scope", {
    case_version: "loop.case.v1", name: "test", goal: "test", base_url: "https://example.test", steps: [],
  })).rejects.toThrow("held browser session");
});
