import { afterEach, beforeEach, expect, test, vi } from "vitest";

import { getCorrections, getExecutions } from "./api";

const fetchMock = vi.fn();

beforeEach(() => {
  fetchMock.mockResolvedValue({
    ok: true,
    json: async () => [],
  });
  vi.stubGlobal("fetch", fetchMock);
});

afterEach(() => {
  vi.unstubAllGlobals();
  fetchMock.mockReset();
});

test("getCorrections includes offset=0 in query string", async () => {
  await getCorrections({
    target_description: "登录按钮",
    offset: 0,
    limit: 10,
  });

  expect(fetchMock).toHaveBeenCalledWith(
    "/api/v1/corrections?target_description=%E7%99%BB%E5%BD%95%E6%8C%89%E9%92%AE&limit=10&offset=0",
    expect.any(Object),
  );
});

test("getExecutions includes offset=0 in query string", async () => {
  await getExecutions({
    project_id: 1,
    offset: 0,
    limit: 10,
  });

  expect(fetchMock).toHaveBeenCalledWith(
    "/api/v1/executions?project_id=1&limit=10&offset=0",
    expect.any(Object),
  );
});
