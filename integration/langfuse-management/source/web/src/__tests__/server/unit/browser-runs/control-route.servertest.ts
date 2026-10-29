import { createMocks } from "node-mocks-http";
import type { NextApiRequest, NextApiResponse } from "next";
import { TRPCError } from "@trpc/server";
import handler from "@/src/pages/api/agenttrace/control/[controlSessionId]";

const mocks = vi.hoisted(() => ({
  client: vi.fn(),
  context: { session: {} },
  headers: {
    Authorization: "Bearer server-secret",
    "X-Agenttrace-Project-Id": "at",
    "X-Langfuse-Project-Id": "lf",
    "X-Agenttrace-Role": "member",
    "X-Agenttrace-Actor-Id": "user-1",
  },
}));
vi.mock("@/src/server/api/trpc", () => ({
  createTRPCContext: () => mocks.context,
}));
vi.mock("@/src/features/browser-runs/server/project-client", () => ({
  clientForProject: mocks.client,
}));
vi.mock("@/src/features/rbac/utils/checkProjectAccess", () => ({
  throwIfNoProjectAccess: vi.fn(),
}));

describe("authenticated control gateway", () => {
  beforeEach(() => {
    mocks.client
      .mockReset()
      .mockResolvedValue({
        apiUrl: "https://control.test",
        headers: mocks.headers,
      });
    vi.stubGlobal(
      "fetch",
      vi
        .fn()
        .mockImplementation(
          async () =>
            new Response(new Uint8Array([137, 80, 78, 71, 13, 10, 26, 10]), {
              headers: { "Content-Type": "image/png" },
            }),
        ),
    );
  });
  afterEach(() => vi.unstubAllGlobals());
  async function request(method: "GET" | "POST" = "GET", body?: unknown) {
    const { req, res } = createMocks<NextApiRequest, NextApiResponse>({
      method,
      body: body as NextApiRequest["body"],
      query: { projectId: "lf", controlSessionId: "control-1" },
      headers:
        method === "POST"
          ? { origin: "http://localhost", host: "localhost" }
          : {},
    });
    await handler(req, res);
    return res;
  }
  it("forwards only to the API origin with project credentials and returns a validated frame", async () => {
    const res = await request();
    expect(res._getStatusCode()).toBe(200);
    expect(res._getHeaders()["content-type"]).toBe("image/png");
    expect(fetch).toHaveBeenCalledWith(
      new URL(
        "https://control.test/api/v1/control-sessions/control-1/screenshot",
      ),
      expect.objectContaining({
        redirect: "error",
        headers: expect.objectContaining(mocks.headers),
      }),
    );
    expect(res._getHeaders()["cache-control"]).toBe("private, no-store");
  });
  it("rejects foreign ownership before fetching any control frame", async () => {
    mocks.client.mockRejectedValue(new TRPCError({ code: "NOT_FOUND" }));
    const res = await request();
    expect(res._getStatusCode()).toBe(404);
    expect(fetch).not.toHaveBeenCalled();
  });
  it.each([403, 404, 409, 410])(
    "preserves control refusal status %i without upstream secrets",
    async (status) => {
      vi.mocked(fetch).mockResolvedValue(
        Response.json({ token: "secret" }, { status }),
      );
      const res = await request();
      expect(res._getStatusCode()).toBe(status);
      expect(res._getJSONData()).toEqual({
        message: "Control session unavailable",
      });
    },
  );
  it("forwards a bounded click but rejects command execution and extra input fields", async () => {
    vi.mocked(fetch).mockResolvedValue(Response.json({ accepted: true }));
    const res = await request("POST", { action: "click", x: 120, y: 40 });
    expect(res._getStatusCode()).toBe(200);
    expect(vi.mocked(fetch).mock.calls[0]?.[1]?.body).toBe(
      '{"action":"click","x":120,"y":40}',
    );
    vi.mocked(fetch).mockClear();
    expect(
      (
        await request("POST", { action: "command", command: "sh" })
      )._getStatusCode(),
    ).toBe(400);
    expect(
      (
        await request("POST", {
          action: "click",
          x: 0,
          y: 0,
          url: "http://worker",
        })
      )._getStatusCode(),
    ).toBe(400);
    expect(fetch).not.toHaveBeenCalled();
  });
  it("rejects cross-origin input, invalid navigation and non-PNG frames", async () => {
    const { req, res } = createMocks<NextApiRequest, NextApiResponse>({
      method: "POST",
      body: { action: "key", key: "Enter" },
      query: { projectId: "lf", controlSessionId: "control-1" },
      headers: { origin: "https://foreign.test", host: "localhost" },
    });
    await handler(req, res);
    expect(res._getStatusCode()).toBe(403);
    expect(fetch).not.toHaveBeenCalled();
    expect(
      (
        await request("POST", { action: "navigate", url: "file:///etc/passwd" })
      )._getStatusCode(),
    ).toBe(400);
    vi.mocked(fetch).mockResolvedValue(Response.json({ token: "private" }));
    expect((await request())._getStatusCode()).toBe(500);
  });
});
