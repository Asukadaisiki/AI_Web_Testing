import type { Session } from "next-auth";
import { AgentTraceClient } from "@/src/features/browser-runs/server/agenttrace-client";
import { browserRunsRouter } from "@/src/features/browser-runs/server/router";
import { createInnerTRPCContext } from "@/src/server/api/trpc";

vi.mock("@/src/server/auth", () => ({ getServerAuthSession: vi.fn() }));
vi.mock("@/src/features/browser-runs/server/config", () => ({
  readBridgeConfig: (projectId: string) =>
    projectId === "lf"
      ? {
          apiUrl: "https://control.test",
          bridgeToken: "server-secret",
          langfuseProjectId: "lf",
          agenttraceProjectId: "at",
        }
      : null,
}));

const config = {
  apiUrl: "https://control.test",
  bridgeToken: "server-secret",
  langfuseProjectId: "lf",
  agenttraceProjectId: "at",
  actorId: "user-1",
  role: "admin" as const,
};
const conversation = {
  id: "conv-2",
  project_id: "at",
  name: "Checkout",
  status: "active",
};
const environment = {
  id: "env-2",
  project_id: "at",
  conversation_id: "conv-2",
  state: "ready",
  profile_generation: 1,
};
const session = (role = "MEMBER", admin = false) =>
  ({
    expires: "1",
    user: {
      id: "user-1",
      admin,
      organizations: [
        {
          id: "org",
          name: "Org",
          role: "OWNER",
          plan: "cloud:hobby",
          projects: [{ id: "lf", name: "Project", role, deletedAt: null }],
        },
      ],
      featureFlags: {},
    },
    environment: { enableExperimentalFeatures: false },
  }) as Session;

describe("management BFF security and lifecycle contracts", () => {
  let requests: { path: string; init: RequestInit }[];
  let responses: Record<string, unknown>;
  beforeEach(() => {
    requests = [];
    responses = {
      "/api/v1/projects/at/binding": {
        project_id: "at",
        langfuse_project_id: "lf",
        status: "active",
      },
      "/api/v1/projects/at/conversations": { conversations: [conversation] },
      "/api/v1/conversations/conv-2": { conversation, messages: [], runs: [] },
      "/api/v1/conversations/conv-2/environment": environment,
      "/api/v1/environments/env-2/reset": {
        ...environment,
        state: "resetting",
      },
      "/api/v1/projects/at/services": {
        workers: [],
        metrics: {},
        components: [],
      },
      "/api/v1/projects/at/skills": { skills: [] },
    };
    vi.stubGlobal(
      "fetch",
      vi.fn(async (url: URL, init: RequestInit = {}) => {
        requests.push({ path: url.pathname, init });
        return Response.json(responses[url.pathname] ?? {});
      }),
    );
  });
  afterEach(() => vi.unstubAllGlobals());
  const caller = (role = "MEMBER", admin = false) =>
    browserRunsRouter.createCaller(
      createInnerTRPCContext({ session: session(role, admin), headers: {} }),
    );

  it("verifies the binding before returning dynamic conversations, with server-derived actor and role", async () => {
    await expect(caller().conversations({ projectId: "lf" })).resolves.toEqual([
      conversation,
    ]);
    expect(requests.map((r) => r.path)).toEqual([
      "/api/v1/projects/at/binding",
      "/api/v1/projects/at/conversations",
    ]);
    for (const { init } of requests) {
      expect(init.headers).toMatchObject({
        Authorization: "Bearer server-secret",
        "X-Agenttrace-Project-Id": "at",
        "X-Langfuse-Project-Id": "lf",
        "X-Agenttrace-Role": "member",
        "X-Agenttrace-Actor-Id": "user-1",
      });
      expect(init.redirect).toBe("error");
    }
  });
  it("uses actual Go title/state conversation wire fields and normalizes omitted titles", async () => {
    const actual = {
      id: "conv-2",
      project_id: "at",
      title: "Checkout",
      state: "active",
    };
    responses["/api/v1/projects/at/conversations"] = {
      conversations: [
        actual,
        { id: "untitled", project_id: "at", state: "archived" },
      ],
    };
    expect(await caller().conversations({ projectId: "lf" })).toEqual([
      conversation,
      {
        id: "untitled",
        project_id: "at",
        name: "Untitled conversation",
        status: "archived",
      },
    ]);
    const client = new AgentTraceClient(
      config,
      vi.fn(async (url: URL | RequestInfo, init?: RequestInit) => {
        const creating = String(url).endsWith("/conversations");
        expect(JSON.parse(String(init?.body))).toEqual(
          creating ? { title: "Checkout" } : { state: "archived" },
        );
        return Response.json({
          ...actual,
          state: creating ? "active" : "archived",
        });
      }) as typeof fetch,
    );
    expect(await client.createConversation("Checkout")).toEqual(conversation);
    expect(await client.updateConversation("conv-2", "archived")).toEqual({
      ...conversation,
      status: "archived",
    });
  });
  it("strips persisted metadata from MCP binding and Harness output without weakening request schemas", async () => {
    responses["/api/v1/projects/at/mcp-bindings"] = {
      mcp_bindings: [
        {
          id: "b1",
          project_id: "at",
          mcp_version_id: "m1",
          config: {},
          secret_refs: {},
          enabled: true,
          revision: 1,
          created_by: "actor",
        },
      ],
    };
    responses["/api/v1/projects/at/harnesses"] = {
      harnesses: [
        {
          id: "hv1",
          harness_id: "h1",
          project_id: "at",
          name: "Generic",
          version: 1,
          system_prompt: "",
          skill_version_ids: ["sv1"],
          mcp_binding_ids: ["b1"],
          runtime_ids: ["pi"],
          content_digest: "digest",
          created_by: "actor",
          created_at: "2026-10-28T12:00:00Z",
        },
      ],
    };
    const bindings = await caller("ADMIN").mcpBindings({ projectId: "lf" });
    const harnesses = await caller("ADMIN").harnesses({ projectId: "lf" });
    expect(bindings[0]).toMatchObject({ id: "b1", enabled: true });
    expect(bindings[0]).not.toHaveProperty("revision");
    expect(bindings[0]).not.toHaveProperty("created_by");
    expect(harnesses[0]).toMatchObject({ id: "hv1", version: 1 });
    expect(harnesses[0]).not.toHaveProperty("created_at");
    expect(harnesses[0]).not.toHaveProperty("created_by");
  });
  it.each([
    { project_id: "foreign", langfuse_project_id: "lf", status: "active" },
    { project_id: "at", langfuse_project_id: "foreign", status: "active" },
    { project_id: "at", langfuse_project_id: "lf", status: "suspended" },
  ])("fails closed for an unverified or suspended binding", async (binding) => {
    responses["/api/v1/projects/at/binding"] = binding;
    await expect(
      caller().conversations({ projectId: "lf" }),
    ).rejects.toMatchObject({ code: "NOT_FOUND" });
    expect(requests).toHaveLength(1);
  });
  it("rejects unauthenticated, foreign and non-member callers before transport", async () => {
    const anonymous = browserRunsRouter.createCaller(
      createInnerTRPCContext({ session: null, headers: {} }),
    );
    await expect(
      anonymous.conversations({ projectId: "lf" }),
    ).rejects.toMatchObject({ code: "UNAUTHORIZED" });
    await expect(
      caller().conversations({ projectId: "foreign" }),
    ).rejects.toMatchObject({ code: "UNAUTHORIZED" });
    expect(requests).toEqual([]);
  });
  it.each(["VIEWER", "MEMBER"])(
    "denies %s service and registry administration before upstream calls",
    async (role) => {
      await expect(
        caller(role).services({ projectId: "lf" }),
      ).rejects.toMatchObject({ code: "FORBIDDEN" });
      await expect(
        caller(role).drainWorker({
          projectId: "lf",
          workerId: "w",
          drain: true,
        }),
      ).rejects.toMatchObject({ code: "FORBIDDEN" });
      await expect(
        caller(role).publishSkill({
          projectId: "lf",
          skillId: "s",
          versionId: "u",
        }),
      ).rejects.toMatchObject({ code: "FORBIDDEN" });
      expect(requests).toEqual([]);
    },
  );
  it("allows project admin services and forwards admin identity", async () => {
    await expect(
      caller("ADMIN").services({ projectId: "lf" }),
    ).resolves.toEqual({ workers: [], metrics: {}, components: [] });
    expect(requests[1]?.init.headers).toMatchObject({
      "X-Agenttrace-Role": "admin",
    });
  });
  it.each(["invalid name", " padded ", "a".repeat(129)])(
    "rejects an invalid Skill name before binding or upload transport: %j",
    async (name) => {
      await expect(
        caller("ADMIN").uploadSkill({
          projectId: "lf",
          upload: {
            name,
            files: [{ path: "SKILL.md", content: "# Instructions" }],
          },
        }),
      ).rejects.toMatchObject({ code: "BAD_REQUEST" });
      expect(requests).toEqual([]);
    },
  );
  it("returns measured Services and nullable observability through authenticated BFF", async () => {
    responses["/api/v1/projects/at/services"] = {
      workers: [],
      runs: {},
      environments: {},
      active_leases: 0,
      expired_leases: 0,
      lost_attempts: 0,
      pending_outbox: 1,
      oldest_outbox_seconds: null,
      dlq_count: 3,
      dlq_scope: "project_retained_outbox_handoffs",
      dependencies: {
        outbox_relay: { status: "degraded", reason: "Pending queue is aging." },
      },
      observability: {
        kafka_consumer_lag: {
          value: null,
          status: "unmeasured",
          scope: "deployment",
          reason: "Offset probe is not configured.",
        },
      },
      recent_registry_audit: [
        {
          id: 1,
          action: "skill.revoke",
          resource_id: "sv1",
          occurred_at: "2026-10-28T12:00:00Z",
          actor_id: "private-actor",
        },
      ],
    };
    const view = await caller("ADMIN").services({ projectId: "lf" });
    expect(view).toMatchObject({
      metrics: { dlq_count: 3, oldest_outbox_seconds: null },
      components: [{ name: "outbox_relay", reason: "Pending queue is aging." }],
      observability: { kafka_consumer_lag: { value: null } },
      recent_registry_audit: [{ action: "skill.revoke", resource_id: "sv1" }],
    });
    expect(JSON.stringify(view)).not.toContain("private-actor");
    expect(requests.map((request) => request.path)).toEqual([
      "/api/v1/projects/at/binding",
      "/api/v1/projects/at/services",
    ]);
  });
  it("keeps Viewer writes blocked even though backend read identity is member", async () => {
    await expect(
      caller("VIEWER").createConversation({ projectId: "lf", name: "New" }),
    ).rejects.toMatchObject({ code: "FORBIDDEN" });
    await expect(
      caller("VIEWER").resetEnvironment({
        projectId: "lf",
        conversationId: "conv-2",
        environmentId: "env-2",
      }),
    ).rejects.toMatchObject({ code: "FORBIDDEN" });
    expect(requests).toEqual([]);
  });
  it("checks environment ownership and identity before reset", async () => {
    responses["/api/v1/conversations/conv-2/environment"] = {
      ...environment,
      id: "foreign",
    };
    await expect(
      caller("ADMIN").resetEnvironment({
        projectId: "lf",
        conversationId: "conv-2",
        environmentId: "env-2",
      }),
    ).rejects.toMatchObject({ code: "NOT_FOUND" });
    expect(requests.some((r) => r.init.method === "POST")).toBe(false);
  });
  it("routes listRuns to the explicitly selected conversation and validates contained run ownership", async () => {
    const client = new AgentTraceClient(config);
    await expect(client.listRuns("conv-2")).resolves.toEqual([]);
    expect(requests[0]?.path).toBe("/api/v1/conversations/conv-2");
    responses["/api/v1/conversations/conv-2"] = {
      conversation: { ...conversation, project_id: "foreign" },
      messages: [],
      runs: [],
    };
    await expect(client.listRuns("conv-2")).rejects.toMatchObject({
      code: "NOT_FOUND",
    });
  });
  it("does not expose worker/control URLs or tokens from takeover creation", async () => {
    responses["/api/v1/environments/env-2/control-session"] = {
      id: "control-2",
      project_id: "at",
      environment_id: "env-2",
      expires_at: "2026-10-28T12:00:00Z",
      control_url: "/api/v1/control-sessions/control-2/consume?token=secret",
      state: "pending",
    };
    responses["/api/v1/control-sessions/control-2/consume"] = {
      id: "control-2",
      environment_id: "env-2",
      expires_at: "2026-10-28T12:00:00Z",
      state: "active",
    };
    const result = await caller().startControl({
      projectId: "lf",
      conversationId: "conv-2",
      environmentId: "env-2",
    });
    expect(result).toEqual({
      id: "control-2",
      expires_at: "2026-10-28T12:00:00Z",
      url: "/api/agenttrace/control/control-2?projectId=lf",
    });
    expect(JSON.stringify(result)).not.toMatch(/worker|secret|Bearer/);
    expect(requests.at(-1)).toMatchObject({
      path: "/api/v1/control-sessions/control-2/consume",
      init: {
        method: "POST",
        body: '{"token":"secret"}',
        headers: { "X-Agenttrace-Actor-Id": "user-1" },
      },
    });
  });
  it("consumes the provider's explicit token field server-side with the same identity", async () => {
    responses["/api/v1/environments/env-2/control-session"] = {
      id: "control-2",
      environment_id: "env-2",
      expires_at: "2099-01-01T00:00:00Z",
      control_url: "/api/v1/control-sessions/control-2",
      state: "pending",
      token: "one-time-private",
    };
    responses["/api/v1/control-sessions/control-2/consume"] = {
      id: "control-2",
      environment_id: "env-2",
      expires_at: "2099-01-01T00:00:00Z",
      state: "active",
    };
    const result = await caller().startControl({
      projectId: "lf",
      conversationId: "conv-2",
      environmentId: "env-2",
    });
    expect(result.url).toBe("/api/agenttrace/control/control-2?projectId=lf");
    expect(JSON.stringify(result)).not.toContain("one-time-private");
    expect(requests.at(-1)?.init.body).toBe('{"token":"one-time-private"}');
  });
  it("normalizes the implemented binding, flat skill versions, MCP wrapper and worker services contract", async () => {
    responses["/api/v1/projects/at/binding"] = {
      agenttrace_project_id: "at",
      langfuse_project_id: "lf",
      status: "active",
    };
    responses["/api/v1/projects/at/skills"] = {
      skills: [
        {
          id: "sv1",
          skill_id: "s1",
          project_id: "at",
          name: "Login",
          version: 1,
          status: "draft",
          files: [{ path: "SKILL.md", content: "# Login" }],
          content_digest: "digest",
        },
      ],
    };
    responses["/api/v1/projects/at/mcp-bindings"] = { mcp_bindings: [] };
    responses["/api/v1/projects/at/services"] = {
      workers: [
        {
          id: "w1",
          role: "browser",
          version: "1",
          capacity: 2,
          in_use: 1,
          draining: false,
          alive: true,
          last_seen_at: "2026-10-28T12:00:00Z",
        },
      ],
      runs: { queued: 3 },
      environments: { ready: 1 },
      active_leases: 1,
      expired_leases: 0,
      lost_attempts: 0,
      pending_outbox: 2,
      oldest_outbox_seconds: null,
      dependencies: { redis: { status: "degraded", mode: "postgres_poll" } },
    };
    expect(
      (await caller("ADMIN").skills({ projectId: "lf" }))[0],
    ).toMatchObject({ id: "s1", versions: [{ id: "sv1" }] });
    expect(await caller("ADMIN").mcpBindings({ projectId: "lf" })).toEqual([]);
    expect(await caller("ADMIN").services({ projectId: "lf" })).toMatchObject({
      workers: [{ id: "w1", active: 1, drain: false, alive: true }],
      metrics: {
        "runs.queued": 3,
        "environments.ready": 1,
        active_leases: 1,
        pending_outbox: 2,
      },
      components: [{ name: "redis", status: "degraded (postgres_poll)" }],
    });
  });
  it("handles 204 skill revocation and forwards the worker draining property", async () => {
    const client = new AgentTraceClient(
      config,
      vi.fn(async (url: URL | RequestInfo, init?: RequestInit) => {
        if (String(url).endsWith("/revoke"))
          return new Response(null, { status: 204 });
        expect(init?.body).toBe('{"draining":true}');
        return Response.json({
          id: "w1",
          role: "runtime",
          version: "1",
          capacity: 2,
          in_use: 0,
          draining: true,
          alive: true,
          last_seen_at: "2026-10-28T12:00:00Z",
        });
      }) as typeof fetch,
    );
    await expect(client.revokeSkill("sv1")).resolves.toBeUndefined();
    await expect(client.drainWorker("w1", true)).resolves.toMatchObject({
      drain: true,
    });
  });
});
