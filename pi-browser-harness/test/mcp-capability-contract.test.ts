import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";
import { parseExecutionSpec } from "../src/execution-spec.js";
import { prepareHarnessResources } from "../src/harness-resources.js";
import type { RunRequest } from "../src/run-protocol.js";

const spec = (server: unknown) => ({
  runtime_id: "pi",
  harness: { id: "reviewed", name: "Reviewed", revision: 1, system_prompt: "",
    skills: [], mcp: { tools: server } },
});
const descriptor = {
  capability: "browser-harness", config: {}, secret_refs: {},
};

describe("public MCP capability contract", () => {
  it("retains JSON config, scoped references and identity without resolving secrets", () => {
    const input = spec({
      capability: "reviewed-tools", config: { endpoint: "https://service.test", options: [true, null, 3] },
      secret_refs: { access: "secret://project-1/access" },
      version_id: "version-1", content_digest: `sha256:${"a".repeat(64)}`,
    });
    const parsed = parseExecutionSpec(input);
    expect(parsed).toEqual(input);
  });

  it("preserves optional registry metadata and enforces runtime compatibility", () => {
    const input = spec(descriptor);
    const harness = { ...input.harness, project_id: "project-1", version_id: "harness-1",
      content_digest: "a".repeat(64), runtime_ids: ["pi", "codex"],
      skills: [{ name: "notes", content: "notes", version_id: "skill-1",
        content_digest: `sha256:${"b".repeat(64)}` }] };
    expect(parseExecutionSpec({ ...input, harness })).toEqual({ ...input, harness });
    for (const overrides of [
      { project_id: "../project" }, { version_id: "../version" }, { content_digest: "invalid" },
      { runtime_ids: [] }, { runtime_ids: ["pi", "pi"] }, { runtime_ids: ["unknown"] },
      { runtime_ids: ["codex"] }, { skills: [{ ...harness.skills[0], version_id: "../version" }] },
    ]) expect(() => parseExecutionSpec({ ...input, harness: { ...harness, ...overrides } })).toThrow();
  });

  it.each([
    { command: "node", args: ["server.js"], env: { TEXT: "env:HOST_TEXT" } },
    { ...descriptor, command: "node" }, { ...descriptor, args: [] },
    { ...descriptor, env: {} }, { ...descriptor, host_only_tools: [] },
    { ...descriptor, config: { worker_url: "https://evil.test" } },
    { ...descriptor, secret_refs: { access: "secret://project-1/access" } },
    { ...descriptor, capability: "tools", config: { token: "plaintext" } },
    { ...descriptor, capability: "tools", config: { nested: { value: "env:HOST_TOKEN" } } },
    { ...descriptor, capability: "tools", secret_refs: { access: "env:HOST_TOKEN" } },
    { ...descriptor, capability: "tools", secret_refs: { access: "plaintext" } },
    { ...descriptor, capability: "tools", secret_refs: { access: "secret://project-1/../access" } },
    { ...descriptor, config: null }, { ...descriptor, secret_refs: null },
    { ...descriptor, version_id: "" },
  ])("rejects unsafe descriptor %#", (server) => {
    expect(() => parseExecutionSpec(spec(server))).toThrow();
  });

  it("resolves the deployed browser capability from host request, not MCP name or public paths", async () => {
    const request = {
      protocol_version: "pi-browser-harness.v1", run_id: "run", attempt_id: "attempt",
      goal: "Discuss {{unknown}} syntax", worker_url: "http://127.0.0.1:8080", artifact_scope: "run/attempt",
      model: { provider: "scripted", name: "test", reasoning_effort: "off" },
      execution: spec(descriptor),
    } as unknown as RunRequest;
    const resources = await prepareHarnessResources(request);
    try {
      expect(resources.servers).toEqual([{
        name: "browser-harness", command: process.execPath,
        args: [fileURLToPath(new URL("../src/mcp/server.js", import.meta.url))],
        env: {
          AGENTTRACE_HARNESS_WORKER_URL: request.worker_url,
          AGENTTRACE_HARNESS_ARTIFACT_SCOPE: request.artifact_scope,
          AGENTTRACE_HARNESS_GOAL: request.goal,
        },
        exposure: "direct", executionMode: "sequential", hostOnlyTools: ["close_session"],
      }]);
      expect(resources.definition.completionPolicy({
        assistant_settled: true, aborted: false, assistant_text: "done", events: [],
      }).verdict).toBe("failed");
    } finally { await resources.dispose(); }
  });
});
