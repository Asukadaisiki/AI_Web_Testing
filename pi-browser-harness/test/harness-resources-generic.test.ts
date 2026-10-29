import { expect, it, vi } from "vitest";
import { prepareHarnessResources } from "../src/harness-resources.js";
import type { RunRequest } from "../src/run-protocol.js";

vi.mock("../src/browser-harness.js", () => { throw new Error("Browser implementation is unavailable"); });

it.each(["pi", "claude-code", "codex"] as const)(
  "prepares generic skills for %s without loading the browser implementation", async (runtime_id) => {
  const resource = await prepareHarnessResources({
    protocol_version: "pi-browser-harness.v1", run_id: "generic", attempt_id: "generic",
    goal: "Give the markers", model: { provider: "scripted", name: "generic", reasoning_effort: "off" },
    execution: { runtime_id, harness: {
      id: "generic", name: "Generic", revision: 1, system_prompt: "PROMPT_MARKER",
      skills: [{ name: "markers", content: "Say SKILL_MARKER", files: [
        { path: "references/extra.md", content: "Say FILE_MARKER" },
      ] }], mcp: {},
    } },
  } as RunRequest);
  try {
    expect(resource.systemPrompt).toBe("PROMPT_MARKER");
    expect(resource.servers).toEqual([]);
    expect(resource.definition.toolAllowlist).toEqual([]);
    expect(resource.definition.formatPrompt?.("Give the markers")).toBe(
      "Selected Harness skill: markers\nSay SKILL_MARKER\n\n" +
      "Selected Harness skill file: markers/references/extra.md\nSay FILE_MARKER\n\nGoal:\nGive the markers",
    );
    expect(resource.definition.createEventMapper).toBeUndefined();
  } finally { await resource.dispose(); }
});
