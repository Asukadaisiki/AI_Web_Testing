import { describe, expect, it } from "vitest";
import { parseExecutionSpec } from "../src/execution-spec.js";

const valid = {
  runtime_id: "codex",
  harness: {
    id: "harness-1", name: "Generic Harness", revision: 1,
    system_prompt: "Use selected tools.",
    skills: [{ name: "review", content: "# Review", files: [
      { path: "references/rules.md", content: "Rules" },
    ] }],
    mcp: { "my-server": { capability: "reviewed-tools", config: { endpoint: "https://service.test" },
      secret_refs: { access: "secret://project-1/access" } } },
  },
};

describe("execution specification parser", () => {
  it.each(["pi", "claude-code", "codex"])("accepts and copies the exact snapshot for %s", (runtime_id) => {
    const input = structuredClone({ ...valid, runtime_id });
    const parsed = parseExecutionSpec(input);
    expect(parsed).toEqual(input);
    input.harness.skills[0]!.files[0]!.content = "changed after parse";
    expect(parsed.harness.skills[0]!.files![0]!.content).toBe("Rules");
  });

  it.each([null, [], {}, { ...valid, runtime_id: "fallback" },
    { ...valid, unexpected: true }, { ...valid, harness: { ...valid.harness, extra: true } },
    { ...valid, harness: { ...valid.harness, id: "../bad" } },
    { ...valid, harness: { ...valid.harness, revision: 0 } },
    { ...valid, harness: { ...valid.harness, revision: 1.5 } },
    { ...valid, harness: { ...valid.harness, revision: Number.MAX_SAFE_INTEGER + 1 } },
    { ...valid, harness: { ...valid.harness, skills: [{ name: "a", content: "b", extra: true }] } },
    { ...valid, harness: { ...valid.harness, skills: [
      { name: "a", content: "b", files: [{ path: "a/../bad", content: "x" }] },
    ] } },
    { ...valid, harness: { ...valid.harness, mcp: { "bad/name": { command: "node" } } } },
    { ...valid, harness: { ...valid.harness, mcp: { safe: { command: "node", url: "http://x" } } } },
    { ...valid, harness: { ...valid.harness, mcp: { safe: { command: "node", args: [1] } } } },
    { ...valid, harness: { ...valid.harness, mcp: { safe: { command: "node", env: { TOKEN: 1 } } } } },
    { ...valid, harness: { ...valid.harness, mcp: {
      safe: { command: "node", env: { TOKEN: "env:API_TOKEN/suffix" } },
    } } },
    { ...valid, harness: { ...valid.harness, mcp: {
      safe: { command: "node", host_only_tools: ["bad/name"] },
    } } },
  ])("rejects invalid nested shapes and unsafe identities: %#", (input) => {
    expect(() => parseExecutionSpec(input)).toThrow();
  });

  it("does not resolve or print secret values in the pure parser", () => {
    expect(parseExecutionSpec(valid).harness.mcp["my-server"]!.secret_refs.access).toBe("secret://project-1/access");
    expect(() => parseExecutionSpec({ ...valid, runtime_id: "a-private-secret" }))
      .toThrow("invalid execution specification");
  });
});
