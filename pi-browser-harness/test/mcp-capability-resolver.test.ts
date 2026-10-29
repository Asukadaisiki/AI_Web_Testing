import { afterEach, expect, it, vi } from "vitest";
import { prepareHarnessResources } from "../src/harness-resources.js";
import type { RunRequest } from "../src/run-protocol.js";
import type { McpCapabilitySnapshot } from "../src/execution-spec.js";

const request = (server: McpCapabilitySnapshot): RunRequest => ({
  protocol_version: "pi-browser-harness.v1", run_id: "run", attempt_id: "attempt",
  goal: "answer", model: { provider: "scripted", name: "offline", reasoning_effort: "off" },
  execution: { runtime_id: "pi", harness: {
    id: "generic", name: "Generic", revision: 1, system_prompt: "", skills: [], mcp: { "my.server": server },
  } },
});
const descriptor: McpCapabilitySnapshot = {
  capability: "reviewed-tools", config: { endpoint: "https://service.test", limit: 3, options: [true, null] },
  secret_refs: { access: "secret://project-1/access" },
};
const options = {
  capabilities: new Map([["reviewed-tools", {
    command: process.execPath, args: ["/deployed/reviewed.js"],
    config: {
      endpoint: { type: "string" as const, env: "SERVICE_URL", required: true },
      limit: { type: "number" as const, env: "SERVICE_LIMIT" },
      options: { type: "json" as const, env: "SERVICE_OPTIONS" },
    },
    secrets: { access: "SERVICE_ACCESS_TOKEN" }, hostOnlyTools: ["release"],
  }]]),
  secrets: { scope: "project-1", resolve: async () => "private-value" },
};

afterEach(() => vi.unstubAllEnvs());

it("rejects undeployed capabilities instead of falling back to snapshot commands", async () => {
  await expect(prepareHarnessResources(request(descriptor))).rejects.toThrow(/capability/);
});

it("uses only deployed config mappings and scoped injected secrets without mutating the snapshot", async () => {
  vi.stubEnv("HOST_TOKEN", "ambient-secret");
  const input = request(descriptor);
  const before = JSON.stringify(input);
  const resources = await prepareHarnessResources(input, options);
  try {
    expect(resources.servers).toEqual([{
      name: "my_server", command: process.execPath, args: ["/deployed/reviewed.js"],
      env: { SERVICE_URL: "https://service.test", SERVICE_LIMIT: "3", SERVICE_OPTIONS: "[true,null]",
        SERVICE_ACCESS_TOKEN: "private-value" },
      exposure: "direct", executionMode: "parallel", hostOnlyTools: ["release"],
    }]);
    expect(resources.definition.toolAllowlist).toEqual(["mcp__my_server__*"]);
    expect(JSON.stringify(input)).toBe(before);
    expect(JSON.stringify(input)).not.toContain("private-value");
  } finally { await resources.dispose(); }
});

it.each([
  { ...descriptor, config: { ...descriptor.config, unlisted: "x" } },
  { ...descriptor, config: { ...descriptor.config, endpoint: 3 } },
  { ...descriptor, config: {} },
  { ...descriptor, secret_refs: { unlisted: "secret://project-1/access" } },
  { ...descriptor, secret_refs: {} },
  { ...descriptor, secret_refs: { access: "secret://project-2/access" } },
])("fails closed on config/secret whitelist or scope violations %#", async (server) => {
  let resolutions = 0;
  await expect(prepareHarnessResources(request(server), {
    ...options, secrets: { scope: "project-1", resolve: () => { resolutions++; return "private-value"; } },
  })).rejects.toThrow();
  expect(resolutions).toBe(0);
});

it("does not resolve from host env or disclose resolver exceptions", async () => {
  vi.stubEnv("SERVICE_ACCESS_TOKEN", "private-value");
  await expect(prepareHarnessResources(request(descriptor), { capabilities: options.capabilities }))
    .rejects.toThrow(/resolver/);
  const error = await prepareHarnessResources(request(descriptor), {
    ...options, secrets: { scope: "project-1", resolve: () => { throw new Error("private-value"); } },
  }).catch((error: unknown) => error);
  expect(error).toBeInstanceOf(Error);
  expect((error as Error).message).toBe("MCP secret resolution failed");
  expect((error as Error).cause).toBeUndefined();
});

it("does not allow a deployed mapping to override the builtin browser capability", async () => {
  const input = request({ capability: "browser-harness", config: {}, secret_refs: {} });
  input.worker_url = "https://worker.test";
  input.artifact_scope = "run/attempt";
  const resources = await prepareHarnessResources(input, {
    capabilities: new Map([["browser-harness", { command: "/malicious", config: {}, secrets: {} }]]),
  });
  try {
    expect(resources.servers[0]?.command).toBe(process.execPath);
    expect(resources.servers[0]?.args?.[0]).toMatch(/\/mcp\/server\.js$/);
  } finally { await resources.dispose(); }
});
