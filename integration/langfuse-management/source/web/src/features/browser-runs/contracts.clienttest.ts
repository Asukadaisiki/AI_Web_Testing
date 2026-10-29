import {
  TraceSchema,
  RunSchema,
  HarnessDefinitionSchema,
  CreateRunSchema,
  readAgentTraceRunId,
  shouldShowAgentTracePanel,
} from "./contracts";

describe("readAgentTraceRunId", () => {
  it.each([
    [{ agenttrace_run_id: "run_123" }, "run_123"],
    ['{"agenttrace_run_id":"run_456"}', "run_456"],
    ['{"agenttrace_run_id":"other"}', null],
    ["not json", null],
    [null, null],
    [[], null],
  ])("reads only a valid AgentTrace run id from %o", (metadata, expected) => {
    expect(readAgentTraceRunId(metadata)).toBe(expected);
  });
});

describe("shouldShowAgentTracePanel", () => {
  it("prevents extension requests for a public shared Trace viewer without project access", () => {
    expect(shouldShowAgentTracePanel("run_123", false)).toBe(false);
    expect(shouldShowAgentTracePanel(null, true)).toBe(false);
    expect(shouldShowAgentTracePanel("run_123", true)).toBe(true);
  });
});

describe("Runtime/Harness and canonical contracts", () => {
  it("retains sanitized immutable run provenance without executable or credential content", () => {
    const run = RunSchema.parse({
      id: "run-1", project_id: "p1", conversation_id: "c1", status: "succeeded",
      checkpoint_version: 1, created_at: "2026-10-28T00:00:00Z", updated_at: "2026-10-28T00:00:00Z",
      runtime_id: "pi", environment_id: "env-1", profile_generation: 2,
      model: { provider: "configured", name: "test-model", reasoning_effort: "high", api_key: "hidden" },
      harness_snapshot: {
        harness_id: "h1", harness_version_id: "hv1", revision: 2, name: "Checkout", project_id: "p1",
        content_digest: "digest", runtime_ids: ["pi"], skill_version_ids: ["sv1"], mcp_binding_ids: ["b1"],
        runtime_id: "pi", environment_id: "env-1",
        skills: [{ name: "Login", version_id: "sv1", content_digest: "skill-digest", content: "private executable bytes" }],
        mcp: { browser: { capability: "browser-harness", version_id: "m1", content_digest: "mcp-digest", config: { credential: "hidden" }, secret_refs: { token: "secret://hidden" } } },
        system_prompt: "private executable content",
      },
    });
    expect(run).toMatchObject({
      model: { provider: "configured", name: "test-model", reasoning_effort: "high" },
      profile_generation: 2,
      harness_snapshot: { name: "Checkout", revision: 2, runtime_ids: ["pi"], skills: [{ name: "Login", version_id: "sv1" }], mcp: { browser: { capability: "browser-harness", version_id: "m1" } } },
    });
    expect(JSON.stringify(run)).not.toMatch(/hidden|private executable|secret_refs|system_prompt|api_key/);
  });
  it("accepts canonical missing evidence without invented fields", () => {
    expect(
      TraceSchema.parse({
        spans: [],
        attempts: [],
        usage: [],
        operation_evidence: [],
        derived_nodes: [],
      }),
    ).toEqual({
      spans: [],
      attempts: [],
      usage: [],
      operation_evidence: [],
      derived_nodes: [],
    });
  });
  it("normalizes empty URL to optional and rejects non-HTTP URL", () => {
    const input = {
      projectId: "lf",
      conversationId: "c1",
      goal: " goal ",
      runtimeId: "pi",
      harnessId: "har_1",
      idempotencyKey: "key",
      url: "",
    };
    expect(CreateRunSchema.parse(input)).toMatchObject({
      goal: "goal",
      url: undefined,
    });
    expect(
      CreateRunSchema.safeParse({ ...input, url: "file:///tmp/test" }).success,
    ).toBe(false);
  });
  it.each([
    {
      skills: [
        { name: "duplicate", content: "" },
        { name: "duplicate", content: "" },
      ],
    },
    {
      skills: [
        {
          name: "skill",
          content: "",
          files: [{ path: "../secret", content: "" }],
        },
      ],
    },
    {
      mcp: {
        browser: { command: "tool", env: { API_KEY: "literal-credential" } },
      },
    },
  ])("rejects unsafe Harness resources: %o", (override) => {
    expect(
      HarnessDefinitionSchema.safeParse({
        name: "Generic",
        system_prompt: "",
        skills: [],
        mcp: {},
        ...override,
      }).success,
    ).toBe(false);
  });
  it("accepts only published version and project binding references", () => {
    expect(
      HarnessDefinitionSchema.safeParse({
        name: "Generic",
        system_prompt: "",
        skill_version_ids: ["skill-v1"],
        mcp_binding_ids: ["binding-1"],
        runtime_ids: ["pi"],
      }).success,
    ).toBe(true);
  });
});
