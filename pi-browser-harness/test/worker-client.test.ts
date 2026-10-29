import { once } from "node:events";
import { createServer, type IncomingMessage, type ServerResponse } from "node:http";

import { afterEach, describe, expect, it } from "vitest";

import {
  WorkerClient,
  WorkerTransportError,
} from "../src/browser/worker-client.js";
import type {
  ActRequest,
  ActResponse,
  Case,
  ExecutionResult,
  Observation,
} from "../src/browser/contract.js";

interface RecordedRequest {
  method: string;
  path: string;
  body: unknown;
}

const observation: Observation = {
  observation_id: "obs_1",
  page_state_id: "ps_1",
  browser_session_id: "bsess_1",
  url: "https://shop.test/products",
  title: "Products",
  elements: [
    {
      ref: "el_add",
      tag: "button",
      role: "button",
      name: "Add",
      text: "Add",
      value: null,
      visible: true,
      enabled: true,
      locators: [
        {
          kind: "role",
          role: "button",
          name: "Add",
          exact: true,
          match_count: 1,
        },
      ],
      parent_ref: "section_products",
      container_ref: "section_products",
      own_text: "Add",
      full_text: "Add",
      bbox: { x: 10, y: 20, width: 80, height: 30 },
      visible_in_viewport: true,
      z_index: 1,
      attributes: { "data-testid": "add" },
      form: {
        form_ref: "form_cart",
        submit_candidate_ref: "el_submit",
        enter_submittable: true,
      },
    },
  ],
  structures: [
    {
      ref: "section_products",
      kind: "region",
      tag: "section",
      role: "region",
      parent_ref: null,
      full_text: "Products Add",
      visible: true,
      bbox: { x: 0, y: 0, width: 400, height: 300 },
      attributes: { "aria-label": "Products" },
      submit_candidate_ref: "el_submit",
      enter_submittable: true,
    },
  ],
  blockers: [
    {
      kind: "cookie_banner",
      ref: "cookie_banner",
      confidence: "high",
      covers_target_ref: null,
      dismiss_candidates: [
        {
          kind: "text",
          text: "Accept",
          exact: true,
          match_count: 1,
        },
      ],
      reason: "cookie consent is visible",
    },
  ],
  action_candidates: [
    {
      candidate_id: "candidate_add",
      kind: "control",
      action: "click",
      target_ref: "el_add",
      role: "button",
      name: "Add",
      text: "Add",
      aliases: ["add to cart"],
      attributes: { "data-testid": "add" },
      relations: [
        { type: "inside", ref: "section_products", label: "Products" },
      ],
      locator: {
        kind: "role",
        role: "button",
        name: "Add",
        exact: true,
        match_count: 1,
      },
      confidence: "high",
    },
  ],
  truncated: false,
  truncation_reason: null,
  screenshot_path: "scope/obs_1.png",
};

const actRequest: ActRequest = {
  action: "click",
  locator: {
    kind: "role",
    role: "button",
    name: "Add",
    exact: true,
    match_count: 1,
  },
  value: null,
  submit: false,
  postconditions: [
    { type: "text_visible", value: "Added", timeout_ms: 1_000 },
  ],
};

const actResponse: ActResponse = {
  status: "passed",
  observation,
  conditions: [
    {
      phase: "post",
      type: "text_visible",
      value: "Added",
      satisfied: true,
      detail: null,
    },
  ],
  error: null,
};

const artifact: Case = {
  case_version: "loop.case.v1",
  name: "open products",
  goal: "open products",
  base_url: "https://shop.test",
  steps: [
    {
      index: 0,
      action: "goto",
      intent: "open products",
      value: "https://shop.test/products",
      submit: false,
      preconditions: [],
      postconditions: [
        {
          type: "url_contains",
          value: "/products",
          timeout_ms: 1_000,
        },
      ],
      timeout_ms: 5_000,
    },
  ],
};

const executionResult: ExecutionResult = {
  execution_id: "exec_1",
  status: "passed",
  started_at: "2026-10-26T12:00:00Z",
  finished_at: "2026-10-26T12:00:01Z",
  final_url: "https://shop.test/products",
  steps: [
    {
      index: 0,
      action: "click",
      status: "passed",
      started_at: "2026-10-26T12:00:00Z",
      duration_ms: 1000,
      url_before: "https://shop.test/products",
      url_after: "https://shop.test/products",
      conditions: [
        {
          phase: "post",
          type: "text_visible",
          value: "Added",
          satisfied: true,
          detail: null,
        },
      ],
      evidence: {
        screenshot_path: "scope/step-0.png",
        console: [{ level: "info", text: "added" }],
        network: [
          {
            method: "POST",
            url: "https://shop.test/cart",
            status: 200,
          },
        ],
      },
      error: null,
      blocker: {
        kind: "cookie_banner",
        ref: "cookie_banner",
        confidence: "high",
        covers_target_ref: "el_add",
        dismiss_candidates: [
          {
            kind: "text",
            text: "Accept",
            exact: true,
            match_count: 1,
          },
        ],
        reason: "cookie consent covered the target",
      },
      hit_test: {
        target_ref: "el_add",
        x: 50,
        y: 35,
        hit_ref: "cookie_banner",
        hit_tag: "div",
        hit_role: "dialog",
        hit_text: "Accept cookies",
        covered: true,
        blocker_kind: "cookie_banner",
      },
      recovery: [
        {
          blocker: {
            kind: "cookie_banner",
            ref: "cookie_banner",
            confidence: "high",
            covers_target_ref: "el_add",
            dismiss_candidates: [
              {
                kind: "text",
                text: "Accept",
                exact: true,
                match_count: 1,
              },
            ],
            reason: "cookie consent covered the target",
          },
          action: "dismiss",
          succeeded: true,
          reason: "dismissed cookie banner",
          before_screenshot_path: "scope/before.png",
          after_screenshot_path: "scope/after.png",
          url_before: "https://shop.test/products",
          url_after: "https://shop.test/products",
          retried_original_action: true,
        },
      ],
    },
  ],
  error: null,
};

const servers: Array<ReturnType<typeof createServer>> = [];

afterEach(async () => {
  await Promise.all(
    servers.splice(0).map(
      (server) =>
        new Promise<void>((resolve, reject) => {
          server.close((error) => (error === undefined ? resolve() : reject(error)));
          server.closeAllConnections();
        }),
    ),
  );
});

async function readBody(request: IncomingMessage): Promise<unknown> {
  const chunks: Buffer[] = [];
  for await (const chunk of request) {
    chunks.push(Buffer.from(chunk));
  }
  if (chunks.length === 0) {
    return undefined;
  }
  return JSON.parse(Buffer.concat(chunks).toString("utf8")) as unknown;
}

async function startServer(
  handler: (
    request: IncomingMessage,
    response: ServerResponse,
  ) => Promise<void> | void,
): Promise<string> {
  const server = createServer((request, response) => {
    Promise.resolve(handler(request, response)).catch((error: unknown) => {
      response.statusCode = 500;
      response.end(String(error));
    });
  });
  servers.push(server);
  server.listen(0, "127.0.0.1");
  await once(server, "listening");
  const address = server.address();
  if (address === null || typeof address === "string") {
    throw new Error("test server did not bind a TCP port");
  }
  return `http://127.0.0.1:${address.port}`;
}

it("sends lease metadata and host auth on every managed browser request", async () => {
  const seen: { path: string; headers: IncomingMessage["headers"]; body: unknown }[] = [];
  const baseUrl = await startServer(async (request, response) => {
    const chunks: Buffer[] = [];
    for await (const chunk of request) chunks.push(Buffer.from(chunk));
    seen.push({ path: request.url!, headers: request.headers, body: chunks.length ? JSON.parse(Buffer.concat(chunks).toString()) : null });
    if (request.url === "/sessions") sendJson(response, { session_id: "managed_session" });
    else if (request.method === "DELETE") sendJson(response, { closed: true });
    else if (request.url === "/execute") sendJson(response, executionResult);
    else if (request.url?.endsWith("/act")) sendJson(response, actResponse);
    else sendJson(response, observation);
  });
  const environmentLease = { project_id: "p", environment_id: "env", profile_generation: 2, fencing_token: 7 };
  const client = new WorkerClient(baseUrl, { environmentLease, providerToken: "host-only-secret" });
  const id = await client.openSession("scope");
  await client.navigate(id, "https://shop.test");
  await client.act(id, actRequest);
  await client.execute("scope", artifact);
  await client.closeSession(id);
  expect(seen[0]?.body).toEqual({ session_id: "scope", ...environmentLease });
  expect(seen.find((r) => r.path === "/execute")?.body).toEqual({
    session_id: "scope", case: artifact, ...environmentLease, browser_session_id: id,
  });
  expect(seen).toHaveLength(5);
  for (const request of seen) {
    expect(request.headers.authorization).toBe("Bearer host-only-secret");
    expect(request.headers["x-profile-generation"]).toBe("2");
    expect(request.headers["x-fencing-token"]).toBe("7");
    expect(request.headers["x-environment-id"]).toBe("env");
    expect(request.headers["x-project-id"]).toBe("p");
  }
});

function sendJson(response: ServerResponse, value: unknown): void {
  response.setHeader("content-type", "application/json");
  response.end(JSON.stringify(value));
}

describe("WorkerClient", () => {
  it("uses the exact retained worker routes and request bodies", async () => {
    const requests: RecordedRequest[] = [];
    const baseUrl = await startServer(async (request, response) => {
      const record = {
        method: request.method ?? "",
        path: request.url ?? "",
        body: await readBody(request),
      };
      requests.push(record);

      if (record.path === "/sessions" && record.method === "POST") {
        sendJson(response, { session_id: "bsess_1" });
      } else if (record.path === "/sessions/bsess_1/navigate") {
        sendJson(response, observation);
      } else if (record.path === "/sessions/bsess_1/act") {
        sendJson(response, actResponse);
      } else if (record.path === "/execute") {
        sendJson(response, executionResult);
      } else if (
        record.path === "/sessions/bsess_1" &&
        record.method === "DELETE"
      ) {
        sendJson(response, { closed: true });
      } else {
        response.statusCode = 404;
        sendJson(response, { error: "not_found", detail: "unexpected route" });
      }
    });
    const client = new WorkerClient(baseUrl);

    await expect(client.openSession("scope_1")).resolves.toBe("bsess_1");
    await expect(
      client.navigate("bsess_1", "https://shop.test/products"),
    ).resolves.toEqual(observation);
    await expect(client.act("bsess_1", actRequest)).resolves.toEqual(
      actResponse,
    );
    await expect(client.execute("scope_1", artifact)).resolves.toEqual(
      executionResult,
    );
    await expect(client.closeSession("bsess_1")).resolves.toBeUndefined();

    expect(requests).toEqual([
      { method: "POST", path: "/sessions", body: { session_id: "scope_1" } },
      {
        method: "POST",
        path: "/sessions/bsess_1/navigate",
        body: { url: "https://shop.test/products" },
      },
      {
        method: "POST",
        path: "/sessions/bsess_1/act",
        body: actRequest,
      },
      {
        method: "POST",
        path: "/execute",
        body: { session_id: "scope_1", case: artifact },
      },
      {
        method: "DELETE",
        path: "/sessions/bsess_1",
        body: undefined,
      },
    ]);
  });

  it("preserves typed fields from non-2xx worker errors", async () => {
    const baseUrl = await startServer((_request, response) => {
      response.statusCode = 404;
      sendJson(response, {
        error: "session_not_found",
        detail: "browser session bsess_missing does not exist",
      });
    });
    const client = new WorkerClient(baseUrl);

    const promise = client.navigate("bsess_missing", "https://shop.test");
    await expect(promise).rejects.toMatchObject({
      statusCode: 404,
      code: "session_not_found",
      detail: "browser session bsess_missing does not exist",
    });
    await expect(promise).rejects.toBeInstanceOf(WorkerTransportError);
  });

  it.each([
    ["malformed JSON", "{broken"],
    ["an invalid success shape", JSON.stringify({ session_id: "" })],
  ])("maps %s to worker_error", async (_case, body) => {
    const baseUrl = await startServer((_request, response) => {
      response.end(body);
    });
    const client = new WorkerClient(baseUrl);

    await expect(client.openSession("scope_1")).rejects.toMatchObject({
      statusCode: 200,
      code: "worker_error",
    });
  });

  it.each([
    [
      "navigate element",
      "/sessions/bsess_1/navigate",
      { ...observation, elements: [{}] },
      (client: WorkerClient) =>
        client.navigate("bsess_1", "https://shop.test/products"),
    ],
    [
      "act blocker",
      "/sessions/bsess_1/act",
      {
        ...actResponse,
        observation: { ...observation, blockers: [{}] },
      },
      (client: WorkerClient) => client.act("bsess_1", actRequest),
    ],
    [
      "execute blocker",
      "/execute",
      {
        ...executionResult,
        steps: [{ ...executionResult.steps[0], blocker: "invalid" }],
      },
      (client: WorkerClient) => client.execute("scope_1", artifact),
    ],
    [
      "execute recovery",
      "/execute",
      {
        ...executionResult,
        steps: [{ ...executionResult.steps[0], recovery: "invalid" }],
      },
      (client: WorkerClient) => client.execute("scope_1", artifact),
    ],
  ])("maps a malformed nested %s payload to worker_error", async (
    _case,
    expectedPath,
    payload,
    request,
  ) => {
    const baseUrl = await startServer((incoming, response) => {
      expect(incoming.url).toBe(expectedPath);
      sendJson(response, payload);
    });
    const client = new WorkerClient(baseUrl);

    await expect(request(client)).rejects.toMatchObject({
      statusCode: 200,
      code: "worker_error",
    });
  });

  it.each([
    ["timeoutMs", Number.NaN],
    ["timeoutMs", Number.POSITIVE_INFINITY],
    ["timeoutMs", Number.NEGATIVE_INFINITY],
    ["maxResponseBytes", Number.NaN],
    ["maxResponseBytes", Number.POSITIVE_INFINITY],
    ["maxResponseBytes", Number.NEGATIVE_INFINITY],
  ] as const)("rejects non-finite %s option %s", (option, value) => {
    expect(
      () => new WorkerClient("http://worker.test", { [option]: value }),
    ).toThrow(RangeError);
  });

  it("rejects a response larger than the configured byte bound", async () => {
    const baseUrl = await startServer((_request, response) => {
      sendJson(response, {
        session_id: `bsess_${"x".repeat(256)}`,
      });
    });
    const client = new WorkerClient(baseUrl, { maxResponseBytes: 64 });

    await expect(client.openSession("scope_1")).rejects.toMatchObject({
      statusCode: 200,
      code: "worker_error",
      detail: expect.stringContaining("64"),
    });
  });

  it("propagates AbortSignal cancellation to an in-flight fetch", async () => {
    let requestClosed = false;
    const baseUrl = await startServer((request) => {
      request.on("close", () => {
        requestClosed = true;
      });
    });
    const client = new WorkerClient(baseUrl);
    const controller = new AbortController();

    const request = client.openSession("scope_1", controller.signal);
    setTimeout(() => controller.abort(new Error("cancelled by test")), 20);

    await expect(request).rejects.toThrow();
    await expect.poll(() => requestClosed).toBe(true);
  });

  it("enforces its timeout through the same abort path", async () => {
    const baseUrl = await startServer(() => {});
    const client = new WorkerClient(baseUrl, { timeoutMs: 20 });

    await expect(client.openSession("scope_1")).rejects.toMatchObject({
      statusCode: 0,
      code: "worker_error",
      detail: expect.stringContaining("timed out"),
    });
  });

  it("keeps the timeout active while reading the response body", async () => {
    const baseUrl = await startServer((_request, response) => {
      response.write('{"session_id":"bsess_');
    });
    const client = new WorkerClient(baseUrl, { timeoutMs: 20 });

    await expect(client.openSession("scope_1")).rejects.toMatchObject({
      statusCode: 0,
      code: "worker_error",
      detail: expect.stringContaining("timed out"),
    });
  });
});
