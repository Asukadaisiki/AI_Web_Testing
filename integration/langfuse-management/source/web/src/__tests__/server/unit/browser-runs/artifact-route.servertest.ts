import type { NextApiRequest, NextApiResponse } from "next";
import { createMocks } from "node-mocks-http";
import { TRPCError } from "@trpc/server";
import handler from "@/src/pages/api/agenttrace/artifacts/[runId]";

const mocks = vi.hoisted(() => ({
  trace: vi.fn(),
  config: {
    apiUrl: "https://agenttrace.test",
    bridgeToken: "server-owned-token",
    langfuseProjectId: "lf",
    agenttraceProjectId: "at",
    actorId: "user-1",
    role: "member",
  },
}));
vi.mock("@/src/server/api/trpc", () => ({
  createTRPCContext: vi.fn().mockResolvedValue({}),
}));
vi.mock("@/src/features/browser-runs/server/project-client", () => ({
  clientForProject: () => ({
    getTrace: mocks.trace,
    apiUrl: mocks.config.apiUrl,
    headers: {
      Authorization: `Bearer ${mocks.config.bridgeToken}`,
      "X-Agenttrace-Project-Id": "at",
      "X-Langfuse-Project-Id": "lf",
      "X-Agenttrace-Actor-Id": "user-1",
      "X-Agenttrace-Role": "member",
    },
  }),
}));

const time = "2026-10-28T00:00:00Z";
const evidence = {
  spans: [],
  attempts: [],
  usage: [],
  derived_nodes: [],
  operation_evidence: [
    {
      id: "ev_1",
      operation_id: "op_1",
      status: "passed",
      artifacts: {
        screenshot_paths: [
          "run_1/shot.png",
          "other/shot.png",
          "run_1/../other.png",
          "run_1/nested//shot.png",
        ],
      },
      result: { screenshot_paths: ["run_1/unlisted.png"] },
      created_at: time,
      updated_at: time,
    },
  ],
};

describe("canonical screenshot proxy handler", () => {
  beforeEach(() => {
    mocks.trace.mockReset().mockResolvedValue(evidence);
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        new Response(new Uint8Array([1, 2, 3]), {
          headers: { "content-type": "image/png" },
        }),
      ),
    );
  });
  afterEach(() => vi.unstubAllGlobals());

  async function request(path: string, method = "GET") {
    const { req, res } = createMocks<NextApiRequest, NextApiResponse>({
      method: method as "GET",
      query: { projectId: "lf", runId: "run_1", path },
    });
    await handler(req, res);
    return res;
  }
  it("authorizes via trace and forwards only the exact canonical screenshot with server authentication", async () => {
    const res = await request("run_1/shot.png");
    expect(mocks.trace).toHaveBeenCalledWith("run_1");
    expect(fetch).toHaveBeenCalledWith(
      new URL("https://agenttrace.test/artifacts/run_1/shot.png"),
      expect.objectContaining({
        redirect: "error",
        headers: expect.objectContaining({
          Authorization: "Bearer server-owned-token",
          "X-Agenttrace-Project-Id": "at",
          "X-Langfuse-Project-Id": "lf",
          "X-Agenttrace-Actor-Id": "user-1",
          "X-Agenttrace-Role": "member",
        }),
      }),
    );
    expect(res._getStatusCode()).toBe(200);
    expect(res._getHeaders()["cache-control"]).toBe("private, no-store");
    expect(res._getHeaders()["x-content-type-options"]).toBe("nosniff");
    expect(res._getData()).toBe(String.fromCharCode(1, 2, 3));
  });
  it.each([
    "other/shot.png",
    "run_1/../other.png",
    "run_1/nested//shot.png",
    "run_1/unlisted.png",
    "run_1/shot.png?other",
    "/run_1/shot.png",
  ])(
    "rejects noncanonical or cross-Run path %s before transport",
    async (path) => {
      expect((await request(path))._getStatusCode()).toBe(404);
      expect(fetch).not.toHaveBeenCalled();
    },
  );
  it.each([
    ["FORBIDDEN", 403],
    ["NOT_FOUND", 404],
    ["PRECONDITION_FAILED", 412],
  ] as const)(
    "rejects %s authorization without transporting or exposing upstream details",
    async (code, status) => {
      mocks.trace.mockRejectedValue(
        new TRPCError({ code, message: "not exposed" }),
      );
      const res = await request("run_1/shot.png");
      expect(res._getStatusCode()).toBe(status);
      expect(res._getJSONData()).toEqual({
        message: "Unable to authorize artifact",
      });
      expect(fetch).not.toHaveBeenCalled();
    },
  );
  it("rejects mutations without opening the authorization boundary", async () => {
    expect((await request("run_1/shot.png", "POST"))._getStatusCode()).toBe(
      405,
    );
    expect(mocks.trace).not.toHaveBeenCalled();
    expect(fetch).not.toHaveBeenCalled();
  });
});
