import { Readable, Writable } from "node:stream";
import { afterEach, expect, it, vi } from "vitest";
import { main } from "../src/main.js";
import { parseRunRequest, HarnessEventWriter, type RunRequest } from "../src/run-protocol.js";
import { prepareHarnessResources } from "../src/harness-resources.js";
import { createExecutionSessionFactory } from "../src/session-factory.js";
import { createBrowserHarnessDefinition } from "../src/browser-harness.js";
import { WorkerClient } from "../src/browser/worker-client.js";
import { runStdioServer } from "../src/mcp/server.js";

const generic: RunRequest = {
  protocol_version: "pi-browser-harness.v1", run_id: "r", attempt_id: "a", goal: "answer",
  model: { provider: "scripted", name: "fixture", reasoning_effort: "off" },
  execution: { runtime_id: "pi", harness: {
    id: "selected", name: "Selected", revision: 1, system_prompt: "Answer directly",
    skills: [], mcp: {},
  } },
};
const browser: RunRequest = {
  ...generic, worker_url: "http://localhost:8100", artifact_scope: "r",
  project_id: "p", environment_id: "env", profile_generation: 1, fencing_token: 2,
  execution: { ...generic.execution!, harness: { ...generic.execution!.harness,
    mcp: { browser: { capability: "browser-harness", config: {}, secret_refs: {} } },
  } },
};

afterEach(() => vi.unstubAllEnvs());

it.each(["missing snapshot", "missing runtime", "unsupported runtime"])(
  "rejects %s at parser, resource and factory entrypoints", async (mode) => {
    const invalid = { ...generic } as Record<string, unknown>;
    if (mode === "missing snapshot") delete invalid.execution;
    else invalid.execution = { harness: generic.execution!.harness,
      ...(mode === "unsupported runtime" ? { runtime_id: "unsupported" } : {}) };
    expect(() => parseRunRequest(JSON.stringify(invalid))).toThrow(/execution/);
    await expect(prepareHarnessResources(invalid as unknown as RunRequest)).rejects.toThrow(/execution/);
    const createAgentSession = vi.fn();
    const factory = await createExecutionSessionFactory({ createAgentSession });
    await expect(factory(invalid as unknown as RunRequest, {
      eventWriter: new HarnessEventWriter(generic, { write: () => true }),
      signal: new AbortController().signal,
    })).rejects.toThrow(/execution|runtime/);
    expect(createAgentSession).not.toHaveBeenCalled();
  },
);

it("rejects snapshotless resource preparation instead of disk Skill fallback", async () => {
  const { execution: _execution, ...invalid } = generic;
  await expect(prepareHarnessResources(invalid as RunRequest)).rejects.toThrow(/execution/);
});

it("rejects snapshotless factory requests instead of startup-module/default Pi selection", async () => {
  vi.stubEnv("AGENTTRACE_HARNESS_MODULE", "/not-a-startup-module.mjs");
  const { execution: _execution, ...invalid } = generic;
  const factory = await createExecutionSessionFactory();
  await expect(factory(invalid as RunRequest, {
    eventWriter: new HarnessEventWriter(generic, { write: () => true }),
    signal: new AbortController().signal,
  })).rejects.toThrow(/execution/);
});

it("requires private browser provider credentials in resource preparation", async () => {
  vi.stubEnv("AGENTTRACE_BROWSER_PROVIDER_TOKEN", "");
  await expect(prepareHarnessResources(browser)).rejects.toThrow(/provider token/);
});

it("rejects an unmanaged WorkerClient at construction", () => {
  expect(() => new WorkerClient("http://localhost:8100")).toThrow(/lease/);
  expect(() => new WorkerClient("http://localhost:8100", { providerToken: "private" })).toThrow(/lease/);
});

it("rejects the obsolete one-shot CLI before reading stdin or constructing a session", async () => {
  const sessionFactory = vi.fn();
  const input = new Readable({ read() { throw new Error("must not read one-shot stdin"); } });
  await expect(main({ argv: [], input, sessionFactory })).rejects.toThrow(/--server/);
  expect(sessionFactory).not.toHaveBeenCalled();
});

it("does not load a startup module even when a selected server run succeeds", async () => {
  vi.stubEnv("AGENTTRACE_HARNESS_MODULE", "/not-a-startup-module.mjs");
  const input = new Readable({ read() {} });
  const events: { type: string; payload?: { verdict?: string } }[] = [];
  const output = new Writable({ write(chunk, _encoding, done) {
    const event = JSON.parse(chunk.toString());
    events.push(event);
    if (event.type === "harness.released") input.push(null);
    done();
  } });
  const running = main({ argv: ["--server"], input, output });
  input.push(`${JSON.stringify({ type: "run", request: generic })}\n`);
  expect(await running).toBe(0);
  expect(events.find((event) => event.type === "harness.settled")?.payload?.verdict).toBe("succeeded");
});

it.each(["project_id", "environment_id", "profile_generation", "fencing_token"])(
  "rejects browser selection without %s at parser and descriptor boundaries", (field) => {
    vi.stubEnv("AGENTTRACE_BROWSER_PROVIDER_TOKEN", "private-fixture-token");
    const invalid = { ...browser } as Record<string, unknown>;
    delete invalid[field];
    expect(() => parseRunRequest(JSON.stringify(invalid))).toThrow(/lease/);
    const servers = createBrowserHarnessDefinition().mcpServers;
    expect(() => typeof servers === "function" && servers(invalid as unknown as RunRequest)).toThrow(/lease/);
  },
);

it("rejects browser selection with no lease and no private provider token", async () => {
  const { project_id: _p, environment_id: _e, profile_generation: _g, fencing_token: _f, ...unmanaged } = browser;
  expect(() => parseRunRequest(JSON.stringify(unmanaged))).toThrow(/lease/);
  vi.stubEnv("AGENTTRACE_BROWSER_PROVIDER_TOKEN", "");
  await expect(prepareHarnessResources(browser)).rejects.toThrow(/provider token/);
  expect(() => new WorkerClient("http://localhost:8100")).toThrow(/lease/);
  expect(() => new WorkerClient("http://localhost:8100", { providerToken: "private" })).toThrow(/lease/);
});

it("rejects browser stdio bootstrap with missing managed metadata before HTTP", async () => {
  vi.stubEnv("AGENTTRACE_HARNESS_WORKER_URL", "http://localhost:8100");
  vi.stubEnv("AGENTTRACE_HARNESS_ARTIFACT_SCOPE", "r");
  vi.stubEnv("AGENTTRACE_HARNESS_GOAL", "answer");
  vi.stubEnv("AGENTTRACE_BROWSER_PROVIDER_TOKEN", "private");
  for (const key of ["PROJECT_ID", "ENVIRONMENT_ID", "PROFILE_GENERATION", "FENCING_TOKEN"]) {
    vi.stubEnv(`AGENTTRACE_HARNESS_${key}`, "");
  }
  await expect(runStdioServer()).rejects.toThrow(/required/);
});
