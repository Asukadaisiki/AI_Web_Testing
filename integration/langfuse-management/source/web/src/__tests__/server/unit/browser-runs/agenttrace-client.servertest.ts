import { TRPCError } from "@trpc/server";
import {
  AgentTraceClient,
  assertRunProject,
} from "@/src/features/browser-runs/server/agenttrace-client";
import type { BridgeConfig } from "@/src/features/browser-runs/contracts";

const config: BridgeConfig = {
  apiUrl: "https://agenttrace.test",
  bridgeToken: "server-owned-token",
  langfuseProjectId: "lf",
  agenttraceProjectId: "at",
  actorId: "user-1",
  role: "member",
};
const run = {
  id: "run_1",
  project_id: "at",
  conversation_id: "conv",
  status: "failed",
  checkpoint_version: 1,
  created_at: "2026-10-28T00:00:00Z",
  updated_at: "2026-10-28T00:00:00Z",
};

describe("AgentTraceClient failures and scope", () => {
  it.each([
    [400, "BAD_REQUEST"],
    [401, "PRECONDITION_FAILED"],
    [403, "PRECONDITION_FAILED"],
    [404, "NOT_FOUND"],
    [409, "CONFLICT"],
    [500, "INTERNAL_SERVER_ERROR"],
  ] as const)(
    "maps HTTP %i to %s without upstream detail",
    async (status, code) => {
      const fetcher = vi
        .fn<typeof fetch>()
        .mockResolvedValue(
          new Response(JSON.stringify({ secret: "not exposed" }), { status }),
        );
      await expect(
        new AgentTraceClient(config, fetcher).getRun("run_1"),
      ).rejects.toMatchObject({ code });
    },
  );
  it("sanitizes network and malformed-response failures", async () => {
    const fetcher = vi
      .fn<typeof fetch>()
      .mockRejectedValueOnce(new Error("secret transport detail"))
      .mockResolvedValueOnce(new Response("{}"));
    const client = new AgentTraceClient(config, fetcher);
    await expect(client.listRuntimes()).rejects.toMatchObject({
      code: "INTERNAL_SERVER_ERROR",
    });
    await expect(client.getTrace("run_1")).rejects.toMatchObject({
      code: "INTERNAL_SERVER_ERROR",
    });
  });
  it("rejects a mismatched Run id before trace reads", async () => {
    const fetcher = vi
      .fn<typeof fetch>()
      .mockResolvedValue(Response.json({ ...run, id: "run_other" }));
    await expect(
      new AgentTraceClient(config, fetcher).getTrace("run_1"),
    ).rejects.toMatchObject({ code: "NOT_FOUND" });
    expect(fetcher).toHaveBeenCalledTimes(1);
  });
  it("rejects foreign snapshot provenance before returning a run or reading its trace", async () => {
    const fetcher = vi.fn<typeof fetch>().mockResolvedValue(Response.json({
      ...run,
      harness_snapshot: {
        harness_id: "h1", harness_version_id: "hv1", project_id: "foreign", content_digest: "digest",
        skill_version_ids: ["sv1"], mcp_binding_ids: ["b1"], runtime_id: "pi", environment_id: "e1",
      },
    }));
    await expect(new AgentTraceClient(config, fetcher).getTrace("run_1")).rejects.toMatchObject({ code: "NOT_FOUND" });
    expect(fetcher).toHaveBeenCalledTimes(1);
  });
  it("preflights foreign Runs before rerunning and preserves a retry key", async () => {
    const fetcher = vi
      .fn<typeof fetch>()
      .mockResolvedValueOnce(Response.json({ ...run, project_id: "other" }));
    const client = new AgentTraceClient(config, fetcher);
    await expect(client.rerun("run_1", "key")).rejects.toMatchObject({
      code: "NOT_FOUND",
    });
    expect(fetcher).toHaveBeenCalledTimes(1);
    fetcher
      .mockResolvedValueOnce(Response.json(run))
      .mockResolvedValueOnce(
        Response.json({ run_id: "run_2", status: "queued" }),
      );
    await client.rerun("run_1", "stable-key");
    expect(fetcher.mock.calls[2]?.[1]?.body).toBe(
      JSON.stringify({ idempotency_key: "stable-key" }),
    );
  });
  it.each(["id", "project_id"])(
    "validates the configured Conversation %s before listing or submitting",
    async (field) => {
      const fetcher = vi.fn<typeof fetch>().mockResolvedValue(
        Response.json({
          conversation: { id: "conv", project_id: "at", [field]: "other" },
          messages: [],
          runs: [],
        }),
      );
      await expect(
        new AgentTraceClient(config, fetcher).listRuns("conv"),
      ).rejects.toMatchObject({ code: "NOT_FOUND" });
    },
  );
  it("hides a foreign Project", () => {
    expect(() => assertRunProject({ project_id: "other" }, "at")).toThrowError(
      TRPCError,
    );
  });
});
