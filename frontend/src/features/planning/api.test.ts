import { afterEach, describe, expect, it, vi } from "vitest";

import { getPlanningSession, listPlanningSessions } from "./api";

describe("planning metadata API", () => {
  afterEach(() => vi.unstubAllGlobals());

  it("使用 Go /api/v2/planning 路径", async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(
        new Response(JSON.stringify([]), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        }),
      )
      .mockResolvedValueOnce(
        new Response(
          JSON.stringify({ session: {}, messages: [], drafts: [] }),
          { status: 200, headers: { "Content-Type": "application/json" } },
        ),
      );
    vi.stubGlobal("fetch", fetchMock);

    await listPlanningSessions();
    await getPlanningSession(12);

    expect(fetchMock).toHaveBeenNthCalledWith(
      1,
      "/api/v2/planning/sessions",
      expect.objectContaining({
        headers: { "Content-Type": "application/json" },
      }),
    );
    expect(fetchMock).toHaveBeenNthCalledWith(
      2,
      "/api/v2/planning/sessions/12",
      expect.objectContaining({
        headers: { "Content-Type": "application/json" },
      }),
    );
  });
});
