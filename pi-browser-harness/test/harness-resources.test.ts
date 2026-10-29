import { access, readFile, stat } from "node:fs/promises";
import { isAbsolute, join, relative } from "node:path";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { ExecutionSpec, HarnessSnapshot } from "../src/execution-spec.js";
import { prepareHarnessResources } from "../src/harness-resources.js";
import type { RunRequest } from "../src/run-protocol.js";

const request: RunRequest = {
  protocol_version: "pi-browser-harness.v1", run_id: "run", attempt_id: "attempt",
  goal: "verify checkout", worker_url: "http://127.0.0.1:8080", artifact_scope: "run/attempt",
  model: { provider: "scripted", name: "test", reasoning_effort: "off" },
};

function snapshot(overrides: Partial<HarnessSnapshot> = {}): HarnessSnapshot {
  return {
    id: "custom", name: "Custom", revision: 2, system_prompt: "Use the supplied skill.",
    skills: [{ name: "checkout", content: "# Checkout\nFollow instructions.",
      files: [{ path: "references/details.md", content: "Detailed instructions." }] }],
    mcp: {}, ...overrides,
  };
}

function managed(harness: HarnessSnapshot): RunRequest {
  return { ...request, execution: { runtime_id: "codex", harness } satisfies ExecutionSpec } as RunRequest;
}

describe("attempt harness resources", () => {
  const disposals: (() => Promise<void>)[] = [];
  afterEach(async () => {
    await Promise.all(disposals.splice(0).map((dispose) => dispose()));
    vi.unstubAllEnvs();
  });

  it("materializes snapshot skills and relative files privately outside the repository", async () => {
    const resources = await prepareHarnessResources(managed(snapshot()));
    disposals.push(resources.dispose);
    expect(isAbsolute(resources.cwd)).toBe(true);
    expect(relative(process.cwd(), resources.cwd).startsWith("..")).toBe(true);
    expect((await stat(resources.cwd)).mode & 0o777).toBe(0o700);
    expect(resources.systemPrompt).toBe("Use the supplied skill.");
    expect(await readFile(join(resources.skillPaths[0]!, "checkout/SKILL.md"), "utf8"))
      .toBe("# Checkout\nFollow instructions.");
    expect(await readFile(join(resources.skillPaths[0]!, "checkout/references/details.md"), "utf8"))
      .toBe("Detailed instructions.");
    expect((await stat(join(resources.skillPaths[0]!, "checkout/SKILL.md"))).mode & 0o777).toBe(0o600);
    expect(resources.definition.skillPaths).toEqual(resources.skillPaths);
    expect(resources.definition.completionPolicy({
      assistant_settled: true, aborted: false, assistant_text: "done", events: [],
    })).toEqual({ verdict: "succeeded" });
    expect(resources.definition.completionPolicy({
      assistant_settled: true, aborted: true, assistant_text: "done", events: [],
    }).verdict).toBe("failed");
    await Promise.all([resources.dispose(), resources.dispose()]);
    await expect(access(resources.cwd)).rejects.toThrow();
  });

  it("isolates concurrent attempts and does not retain mutable snapshot arrays", async () => {
    const original = snapshot();
    const first = await prepareHarnessResources(managed(original));
    const second = await prepareHarnessResources(managed(original));
    disposals.push(first.dispose, second.dispose);
    expect(first.cwd).not.toBe(second.cwd);
    original.skills[0]!.content = "mutated";
    await first.dispose();
    expect(await readFile(join(second.skillPaths[0]!, "checkout/SKILL.md"), "utf8"))
      .toBe("# Checkout\nFollow instructions.");
  });

  it("keeps resources outside the repository even when TMPDIR points inside it", async () => {
    vi.stubEnv("TMPDIR", process.cwd());
    const resources = await prepareHarnessResources(managed(snapshot()));
    disposals.push(resources.dispose);
    expect(relative(process.cwd(), resources.cwd).startsWith("..")).toBe(true);
  });

  it("supports dotted skill and MCP names while translating MCP names for Pi", async () => {
    const resources = await prepareHarnessResources(managed(snapshot({
      skills: [{ name: "review.notes", content: "Follow review notes" }],
      mcp: { "my.server": { capability: "reviewed-tools", config: {}, secret_refs: {} } },
    })), { capabilities: new Map([["reviewed-tools", {
      command: process.execPath, config: {}, secrets: {},
    }]]) });
    disposals.push(resources.dispose);
    expect(resources.servers[0]?.name).toBe("my_server");
    expect(resources.definition.toolAllowlist).toEqual(["mcp__my_server__*"]);
    expect(await readFile(join(resources.skillPaths[0]!, "review.notes/SKILL.md"), "utf8"))
      .toBe("Follow review notes");
  });

  it("does not expand placeholder-like text inside approved JSON config", async () => {
    const resources = await prepareHarnessResources({
      ...managed(snapshot({ mcp: { custom: {
        capability: "reviewed-tools", config: { text: "{{goal}}" }, secret_refs: {},
      } } })), goal: "Discuss {{unknown}} syntax",
    }, { capabilities: new Map([["reviewed-tools", {
      command: process.execPath, config: { text: { type: "string", env: "TEXT" } }, secrets: {},
    }]]) });
    disposals.push(resources.dispose);
    expect(resources.servers[0]?.env).toEqual({ TEXT: "{{goal}}" });
  });

  it("copies legacy browser skills and uses browser completion and host-only cleanup", async () => {
    const resources = await prepareHarnessResources(request);
    disposals.push(resources.dispose);
    expect(await readFile(join(resources.skillPaths[0]!, "browser-case-authoring/SKILL.md"), "utf8"))
      .toContain("browser-case-authoring");
    expect(resources.servers[0]).toMatchObject({
      name: "browser-harness", hostOnlyTools: ["close_session"],
      env: { AGENTTRACE_HARNESS_WORKER_URL: request.worker_url },
    });
    expect(resources.definition.completionPolicy({
      assistant_settled: true, aborted: false, assistant_text: "claimed success", events: [],
    }).verdict).toBe("failed");
    expect(resources.definition.completionEventTypes).toEqual(["browser.case.completed"]);
  });

  it("uses browser host context and rejects environment references", async () => {
    const resources = await prepareHarnessResources(managed(snapshot({ mcp: {
      "browser-harness": { capability: "browser-harness", config: {}, secret_refs: {} },
    } })));
    disposals.push(resources.dispose);
    expect(resources.servers[0]).toMatchObject({
      name: "browser-harness", env: {
        AGENTTRACE_HARNESS_WORKER_URL: request.worker_url,
        AGENTTRACE_HARNESS_ARTIFACT_SCOPE: request.artifact_scope,
        AGENTTRACE_HARNESS_GOAL: request.goal,
      },
    });
    expect(resources.definition.completionPolicy({
      assistant_settled: true, aborted: false, assistant_text: "done", events: [],
    }).verdict).toBe("failed");
    for (const value of ["env:HARNESS_TEST_MISSING", "env:HARNESS_TEST_SECRET/suffix", "prefix-env:HARNESS_TEST_SECRET"]) {
      await expect(prepareHarnessResources(managed(snapshot({
        mcp: { custom: { capability: "reviewed-tools", config: {}, secret_refs: { access: value } } },
      })))).rejects.toThrow(/reference/);
    }
  });

  it.each(["../escape", "/absolute", "a/../escape", "a\\escape", ".", "C:/escape"])(
    "rejects unsafe skill file path %s", async (path) => {
      await expect(prepareHarnessResources(managed(snapshot({
        skills: [{ name: "safe", content: "skill", files: [{ path, content: "escape" }] }],
      })))).rejects.toThrow(/path/);
    },
  );

  it("rejects duplicate skills, case-insensitive file collisions and normalized server collisions", async () => {
    for (const skills of [
      [{ name: "../escape", content: "x" }],
      [{ name: "safe", content: "x" }, { name: "SAFE", content: "y" }],
      [{ name: "safe", content: "x", files: [{ path: "skill.md", content: "overwrite" }] }],
      [{ name: "safe", content: "x", files: [
        { path: "ref.md", content: "x" }, { path: "REF.md", content: "y" },
      ] }],
    ]) {
      await expect(prepareHarnessResources(managed(snapshot({ skills }))))
        .rejects.toThrow(/path|duplicate/);
    }
    await expect(prepareHarnessResources(managed(snapshot({ mcp: {
      "my-server": { capability: "tools", config: {}, secret_refs: {} },
      my_server: { capability: "tools", config: {}, secret_refs: {} },
    } })))).rejects.toThrow(/duplicate/);
  });
});
