import type { Session } from "next-auth";
import { TRPCError } from "@trpc/server";
import { browserRunsRouter } from "@/src/features/browser-runs/server/router";
import { createInnerTRPCContext } from "@/src/server/api/trpc";

vi.mock("@/src/server/auth", () => ({
  getServerAuthSession: vi.fn(),
}));

const bridgeMocks = vi.hoisted(() => ({
  config: {
    apiUrl: "https://agenttrace.test",
    bridgeToken: "server-owned-token",
    langfuseProjectId: "lf-project",
    agenttraceProjectId: "at-project",
    conversationId: "conv-fixed",
  } as {
    apiUrl: string;
    bridgeToken: string;
    langfuseProjectId: string;
    agenttraceProjectId: string;
    conversationId: string;
  } | null,
  client: {
    verifyBinding: vi.fn().mockResolvedValue(undefined),
    listRuns: vi.fn(),
    createRun: vi.fn(),
    getRun: vi.fn(),
    getTrace: vi.fn(),
    listRuntimes: vi.fn(),
    listHarnesses: vi.fn(),
    createHarness: vi.fn(),
    updateHarness: vi.fn(),
    deleteHarness: vi.fn(),
    cancelRun: vi.fn(),
    rerun: vi.fn(),
    authorizeEvents: vi.fn(),
  },
}));

vi.mock("@/src/features/browser-runs/server/config", () => ({
  readBridgeConfig: () => bridgeMocks.config,
}));

vi.mock("@/src/features/browser-runs/server/agenttrace-client", () => ({
  createAgentTraceClient: () => bridgeMocks.client,
}));

const session: Session = {
  expires: "1",
  user: {
    id: "browser-runs-user",
    name: "Browser Runs User",
    email: "browser-runs@example.com",
    canCreateOrganizations: true,
    organizations: [
      {
        id: "org-browser-runs",
        name: "Browser Runs Org",
        role: "OWNER",
        plan: "cloud:hobby",
        cloudConfig: undefined,
        metadata: {},
        aiFeaturesEnabled: true,
        aiTelemetryEnabled: true,
        projects: [
          {
            id: "lf-project",
            name: "Browser Runs Project",
            role: "MEMBER",
            deletedAt: null,
            retentionDays: null,
            hasTraces: false,
            metadata: {},
            createdAt: new Date(0).toISOString(),
          },
          {
            id: "other-project",
            name: "Other Project",
            role: "MEMBER",
            deletedAt: null,
            retentionDays: null,
            hasTraces: false,
            metadata: {},
            createdAt: new Date(0).toISOString(),
          },
        ],
      },
    ],
    featureFlags: {
      excludeClickhouseRead: false,
      observationEvals: false,
      templateFlag: false,
      searchBar: false,
      v4BetaToggleVisible: false,
      experimentsV4Enabled: false,
    },
    admin: false,
  },
  environment: {
    enableExperimentalFeatures: false,
    selfHostedInstancePlan: "cloud:hobby",
  },
};

const withProjectRole = (
  role: NonNullable<
    Session["user"]
  >["organizations"][number]["projects"][number]["role"],
): Session => ({
  ...session,
  user: {
    ...session.user!,
    organizations: session.user!.organizations.map((organization) => ({
      ...organization,
      projects: organization.projects.map((project) =>
        project.id === "lf-project" ? { ...project, role } : project,
      ),
    })),
  },
});

const caller = (callerSession = session) =>
  browserRunsRouter.createCaller(
    createInnerTRPCContext({ session: callerSession, headers: {} }),
  );

describe("browserRunsRouter", () => {
  beforeEach(() => {
    bridgeMocks.config = {
      apiUrl: "https://agenttrace.test",
      bridgeToken: "server-owned-token",
      langfuseProjectId: "lf-project",
      agenttraceProjectId: "at-project",
      conversationId: "conv-fixed",
    };
    bridgeMocks.client.listRuns.mockResolvedValue([{ id: "run-1" }]);
    bridgeMocks.client.createRun.mockResolvedValue({ run_id: "run-1" });
    bridgeMocks.client.getRun.mockResolvedValue({ id: "run-1" });
    bridgeMocks.client.getTrace.mockResolvedValue({
      spans: [],
      attempts: [],
      usage: [],
      operation_evidence: [],
      derived_nodes: [],
    });
    bridgeMocks.client.listRuntimes.mockResolvedValue([]);
    bridgeMocks.client.listHarnesses.mockResolvedValue([]);
    bridgeMocks.client.createHarness.mockResolvedValue({
      id: "har_1",
      revision: 1,
    });
    bridgeMocks.client.updateHarness.mockResolvedValue({
      id: "har_1",
      revision: 2,
    });
    bridgeMocks.client.deleteHarness.mockResolvedValue(undefined);
    bridgeMocks.client.cancelRun.mockResolvedValue({
      run_id: "run-1",
      status: "cancelling",
    });
    bridgeMocks.client.rerun.mockResolvedValue({ run_id: "run-2" });
    bridgeMocks.client.authorizeEvents.mockResolvedValue({ runId: "run-1" });
  });

  afterEach(() => {
    vi.clearAllMocks();
  });

  it("returns PRECONDITION_FAILED when the bridge is not fully configured", async () => {
    bridgeMocks.config = null;

    await expect(
      caller().list({ projectId: "lf-project", conversationId: "conv-2" }),
    ).rejects.toMatchObject({
      code: "PRECONDITION_FAILED",
    });
  });

  it("hides the bridge from a different Langfuse project", async () => {
    await expect(
      caller().list({ projectId: "other-project", conversationId: "conv-2" }),
    ).rejects.toMatchObject({ code: "NOT_FOUND" });
  });

  it("allows VIEWER reads through the real protected project procedure", async () => {
    const viewer = caller(withProjectRole("VIEWER"));

    await expect(
      viewer.list({ projectId: "lf-project", conversationId: "conv-2" }),
    ).resolves.toEqual([{ id: "run-1" }]);
    await expect(
      viewer.get({ projectId: "lf-project", runId: "run-1" }),
    ).resolves.toEqual({ id: "run-1" });
    await expect(
      viewer.trace({ projectId: "lf-project", runId: "run-1" }),
    ).resolves.toMatchObject({ spans: [], operation_evidence: [] });
    await expect(viewer.runtimes({ projectId: "lf-project" })).resolves.toEqual(
      [],
    );
    await expect(
      viewer.harnesses({ projectId: "lf-project" }),
    ).resolves.toEqual([]);
    await expect(
      viewer.authorizeEvents({ projectId: "lf-project", runId: "run-1" }),
    ).resolves.toEqual({ runId: "run-1" });
  });

  it.each([
    [
      "create",
      () =>
        caller(withProjectRole("VIEWER")).create({
          projectId: "lf-project",
          conversationId: "conv-2",
          url: "https://shop.test",
          goal: "Verify cart",
          runtimeId: "codex",
          harnessId: "har_1",
          idempotencyKey: "stable-key",
        }),
    ],
    [
      "cancel",
      () =>
        caller(withProjectRole("VIEWER")).cancel({
          projectId: "lf-project",
          runId: "run-1",
        }),
    ],
    [
      "rerun",
      () =>
        caller(withProjectRole("VIEWER")).rerun({
          projectId: "lf-project",
          runId: "run-1",
          idempotencyKey: "stable-rerun-key",
        }),
    ],
  ])("denies VIEWER %s without playground:execute", async (_name, invoke) => {
    await expect(invoke()).rejects.toMatchObject({ code: "FORBIDDEN" });
  });

  it("allows MEMBER create, cancel, and rerun", async () => {
    const member = caller(withProjectRole("MEMBER"));

    await expect(
      member.create({
        projectId: "lf-project",
        conversationId: "conv-2",
        url: "https://shop.test",
        goal: "Verify cart",
        runtimeId: "codex",
        harnessId: "har_1",
        idempotencyKey: "stable-key",
      }),
    ).resolves.toEqual({ run_id: "run-1" });
    await expect(
      member.cancel({ projectId: "lf-project", runId: "run-1" }),
    ).resolves.toEqual({ run_id: "run-1", status: "cancelling" });
    await expect(
      member.rerun({
        projectId: "lf-project",
        runId: "run-1",
        idempotencyKey: "stable-rerun-key",
      }),
    ).resolves.toEqual({ run_id: "run-2" });
  });

  it("uses a tRPC error for bridge preconditions", () => {
    const error = new TRPCError({ code: "PRECONDITION_FAILED" });
    expect(error.code).toBe("PRECONDITION_FAILED");
  });

  const definition = {
    name: "Generic",
    system_prompt: "",
    skill_version_ids: [],
    mcp_binding_ids: [],
    runtime_ids: ["pi", "claude-code", "codex"] as (
      | "pi"
      | "claude-code"
      | "codex"
    )[],
  };
  it.each(["VIEWER", "MEMBER"] as const)(
    "denies %s Harness CRUD without project:update",
    async (role) => {
      const member = caller(withProjectRole(role));
      await expect(
        member.createHarness({ projectId: "lf-project", definition }),
      ).rejects.toMatchObject({ code: "FORBIDDEN" });
      await expect(
        member.updateHarness({
          projectId: "lf-project",
          harnessId: "har_1",
          definition,
        }),
      ).rejects.toMatchObject({ code: "FORBIDDEN" });
      expect(bridgeMocks.client.createHarness).not.toHaveBeenCalled();
      expect(bridgeMocks.client.updateHarness).not.toHaveBeenCalled();
      expect(bridgeMocks.client.deleteHarness).not.toHaveBeenCalled();
    },
  );
  it("allows ADMIN Harness CRUD", async () => {
    const admin = caller(withProjectRole("ADMIN"));
    await expect(
      admin.createHarness({ projectId: "lf-project", definition }),
    ).resolves.toEqual({ id: "har_1", revision: 1 });
    await expect(
      admin.updateHarness({
        projectId: "lf-project",
        harnessId: "har_1",
        definition,
      }),
    ).resolves.toEqual({ id: "har_1", revision: 2 });
  });
  it("rejects unauthenticated and non-member reads", async () => {
    const anonymous = browserRunsRouter.createCaller(
      createInnerTRPCContext({ session: null, headers: {} }),
    );
    await expect(
      anonymous.harnesses({ projectId: "lf-project" }),
    ).rejects.toMatchObject({ code: "UNAUTHORIZED" });
    await expect(
      caller().runtimes({ projectId: "not-a-member" }),
    ).rejects.toMatchObject({ code: "UNAUTHORIZED" });
  });
  it("gates all catalog, evidence and write procedures to the configured LF project", async () => {
    const admin = caller(withProjectRole("ADMIN"));
    const foreign = {
      projectId: "other-project",
      conversationId: "conv-2",
      runId: "run_1",
      harnessId: "har_1",
    };
    const invocations = [
      () => admin.runtimes(foreign),
      () => admin.harnesses(foreign),
      () => admin.get(foreign),
      () => admin.trace(foreign),
      () => admin.authorizeEvents(foreign),
      () =>
        admin.create({
          ...foreign,
          goal: "goal",
          runtimeId: "pi",
          idempotencyKey: "key",
        }),
      () => admin.cancel(foreign),
      () => admin.rerun({ ...foreign, idempotencyKey: "key" }),
      () => admin.createHarness({ ...foreign, definition }),
      () => admin.updateHarness({ ...foreign, definition }),
    ];
    // A configured-project mismatch must not reach the upstream client even if RBAC rejects a foreign write first.
    for (const invoke of invocations)
      await expect(invoke()).rejects.toBeInstanceOf(TRPCError);
    for (const method of Object.values(bridgeMocks.client))
      expect(method).not.toHaveBeenCalled();
  });
});
