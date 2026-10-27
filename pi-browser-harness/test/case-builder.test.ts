import { createServer, type IncomingMessage, type ServerResponse } from "node:http";

import { afterEach, describe, expect, it } from "vitest";

import { WorkerClient } from "../src/browser/worker-client.js";
import {
  CaseBuilder,
  type CaseBuilderDeps,
} from "../src/browser/case-builder.js";
import type {
  ActRequest,
  ActResponse,
  Case,
  ExecutionResult,
  Locator,
  Observation,
} from "../src/browser/contract.js";

const LIST_URL = "https://shop.test/products";
const DETAIL_URL = "https://shop.test/item/1";

function roleLocator(role: string, name: string): Locator {
  return { kind: "role", role, name, exact: true, match_count: 1 };
}

function baseObservation(
  url: string,
  overrides: Partial<Observation> = {},
): Observation {
  return {
    observation_id: `obs_${url}`,
    page_state_id: `ps_${url}`,
    browser_session_id: "browser",
    url,
    title: url,
    elements: [],
    structures: [],
    blockers: [],
    action_candidates: [],
    truncated: false,
    truncation_reason: null,
    screenshot_path: null,
    ...overrides,
  };
}

function listObservation(): Observation {
  return baseObservation(LIST_URL, {
    elements: [
      {
        ref: "search",
        tag: "input",
        role: "textbox",
        name: "Search",
        text: null,
        value: "",
        visible: true,
        enabled: true,
        locators: [roleLocator("textbox", "Search")],
      },
      {
        ref: "widget",
        tag: "a",
        role: "link",
        name: "Widget",
        text: "Widget",
        value: null,
        visible: true,
        enabled: true,
        locators: [{ kind: "text", text: "Widget", exact: true, match_count: 1 }],
      },
    ],
    action_candidates: [
      {
        candidate_id: "candidate_widget",
        kind: "control",
        action: "click",
        target_ref: "widget",
        locator: { kind: "text", text: "Widget", exact: true, match_count: 1 },
        confidence: "high",
      },
    ],
  });
}

function detailObservation(): Observation {
  return baseObservation(DETAIL_URL, {
    elements: [
      {
        ref: "cart",
        tag: "button",
        role: "button",
        name: "Add to cart",
        text: "Add to cart",
        value: null,
        visible: true,
        enabled: true,
        locators: [roleLocator("button", "Add to cart")],
      },
    ],
  });
}

function passingActResponse(observation: Observation): ActResponse {
  return { status: "passed", observation, conditions: [], error: null };
}

function startServer(
  handler: (req: IncomingMessage, res: ServerResponse, body: unknown) => void,
): { url: string; close: () => void } {
  const server = createServer((req, res) => {
    const chunks: Buffer[] = [];
    req.on("data", (chunk) => chunks.push(chunk as Buffer));
    req.on("end", () => {
      const raw = Buffer.concat(chunks).toString("utf8");
      const body = raw === "" ? undefined : JSON.parse(raw);
      handler(req, res, body);
    });
  });
  server.listen(0);
  const address = server.address();
  const port =
    typeof address === "object" && address !== null ? address.port : 0;
  return {
    url: `http://127.0.0.1:${port}`,
    close: () => server.close(),
  };
}

function jsonResponse(res: ServerResponse, status: number, body: unknown): void {
  res.statusCode = status;
  res.setHeader("content-type", "application/json");
  res.end(JSON.stringify(body));
}

async function makeBuilder(
  workerUrl: string,
  overrides: Partial<CaseBuilderDeps> = {},
): Promise<CaseBuilder> {
  return CaseBuilder.create({
    worker: new WorkerClient(workerUrl),
    artifactScope: "sess_test",
    goal: "find a product",
    ...overrides,
  });
}

describe("CaseBuilder transactional authoring", () => {
  const servers: Array<{ close: () => void }> = [];
  afterEach(() => {
    for (const server of servers) server.close();
    servers.length = 0;
  });

  it("open_page must be the first action and use an absolute URL", async () => {
    const server = startServer((req, res) => {
      if (req.method === "POST" && req.url === "/sessions") {
        jsonResponse(res, 200, { session_id: "bsess_1" });
        return;
      }
      jsonResponse(res, 500, { error: "unexpected", detail: req.url });
    });
    servers.push(server);
    const builder = await makeBuilder(server.url);

    const click = await builder.call(
      "click",
      { hint: "Widget", intent: "open", expect_text: "Details" },
    );
    expect(click.result.ok).toBe(false);
    expect(click.result.error).toBe("no_observation");
    expect(builder.committedSteps).toHaveLength(0);

    const relative = await builder.call("open_page", {
      url: "/products",
      intent: "open products",
    });
    expect(relative.result.ok).toBe(false);
    expect(relative.result.error).toBe("url_not_absolute");
    expect(builder.committedSteps).toHaveLength(0);
  });

  it("every target action requires at least one objective expectation", async () => {
    const server = startServer((req, res) => {
      if (req.method === "POST" && req.url === "/sessions") {
        jsonResponse(res, 200, { session_id: "bsess_1" });
        return;
      }
      if (req.method === "POST" && req.url?.startsWith("/sessions/") && req.url.endsWith("/navigate")) {
        jsonResponse(res, 200, listObservation());
        return;
      }
      jsonResponse(res, 500, { error: "unexpected", detail: req.url });
    });
    servers.push(server);
    const builder = await makeBuilder(server.url);

    await builder.call("open_page", { url: LIST_URL, intent: "open products" });

    const noExpectation = await builder.call("click", {
      hint: "Widget",
      intent: "open widget",
    });
    expect(noExpectation.result.ok).toBe(false);
    expect(noExpectation.result.error).toBe("step_rejected");
    expect(builder.committedSteps).toHaveLength(1);
  });

  it("a failed authoring action never commits a step", async () => {
    let acts = 0;
    const server = startServer((req, res, body) => {
      if (req.method === "POST" && req.url === "/sessions") {
        jsonResponse(res, 200, { session_id: "bsess_1" });
        return;
      }
      if (req.method === "POST" && req.url?.endsWith("/navigate")) {
        jsonResponse(res, 200, listObservation());
        return;
      }
      if (req.method === "POST" && req.url?.endsWith("/act")) {
        acts += 1;
        jsonResponse(res, 200, {
          status: "failed",
          observation: listObservation(),
          conditions: [],
          error: { kind: "condition_unmet", message: "did not happen" },
        });
        return;
      }
      if (req.method === "DELETE" && req.url?.startsWith("/sessions/")) {
        res.statusCode = 200;
        res.setHeader("content-type", "application/json");
        res.end(JSON.stringify({ closed: true }));
        return;
      }
      jsonResponse(res, 500, { error: "unexpected", detail: req.url });
    });
    servers.push(server);
    const builder = await makeBuilder(server.url);

    await builder.call("open_page", { url: LIST_URL, intent: "open products" });
    expect(builder.committedSteps).toHaveLength(1);

    const failed = await builder.call("click", {
      hint: "Widget",
      intent: "open widget",
      expect_text: "Never appears",
    });
    expect(failed.result.ok).toBe(false);
    expect(failed.result.error).toBe("condition_unmet");
    expect(acts).toBe(1);
    expect(builder.committedSteps).toHaveLength(1);
  });

  it("a successful action commits only after the worker reports passed", async () => {
    let acts = 0;
    const server = startServer((req, res) => {
      if (req.method === "POST" && req.url === "/sessions") {
        jsonResponse(res, 200, { session_id: "bsess_1" });
        return;
      }
      if (req.method === "POST" && req.url?.endsWith("/navigate")) {
        jsonResponse(res, 200, listObservation());
        return;
      }
      if (req.method === "POST" && req.url?.endsWith("/act")) {
        acts += 1;
        jsonResponse(res, 200, passingActResponse(detailObservation()));
        return;
      }
      jsonResponse(res, 500, { error: "unexpected", detail: req.url });
    });
    servers.push(server);
    const builder = await makeBuilder(server.url);

    await builder.call("open_page", { url: LIST_URL, intent: "open products" });
    const clicked = await builder.call("click", {
      hint: "Widget",
      intent: "open widget",
      expect_url: "/item/1",
    });
    expect(clicked.result.ok).toBe(true);
    expect(acts).toBe(1);
    expect(builder.committedSteps).toHaveLength(2);
    expect(builder.committedSteps[1]?.action).toBe("click");
  });

  it("assert_text and assert_url evaluate the current observation without mutating the page", async () => {
    let navigateCalls = 0;
    let actCalls = 0;
    const server = startServer((req, res) => {
      if (req.method === "POST" && req.url === "/sessions") {
        jsonResponse(res, 200, { session_id: "bsess_1" });
        return;
      }
      if (req.method === "POST" && req.url?.endsWith("/navigate")) {
        navigateCalls += 1;
        jsonResponse(res, 200, listObservation());
        return;
      }
      if (req.method === "POST" && req.url?.endsWith("/act")) {
        actCalls += 1;
        jsonResponse(res, 200, passingActResponse(listObservation()));
        return;
      }
      jsonResponse(res, 500, { error: "unexpected", detail: req.url });
    });
    servers.push(server);
    const builder = await makeBuilder(server.url);

    await builder.call("open_page", { url: LIST_URL, intent: "open products" });
    const navigateBefore = navigateCalls;
    const actBefore = actCalls;

    const textOk = await builder.call("assert_text", {
      text: "Widget",
      intent: "widget is listed",
    });
    expect(textOk.result.ok).toBe(true);
    expect(textOk.result.error).toBeUndefined();

    const urlOk = await builder.call("assert_url", {
      contains: "/products",
      intent: "still on products",
    });
    expect(urlOk.result.ok).toBe(true);

    expect(navigateCalls).toBe(navigateBefore);
    expect(actCalls).toBe(actBefore);
    expect(builder.committedSteps).toHaveLength(3);
    expect(builder.committedSteps[1]?.action).toBe("assert_text");
    expect(builder.committedSteps[2]?.action).toBe("assert_url");
  });

  it("failed assertions retain the current compact page and committed steps", async () => {
    const observed = listObservation();
    observed.screenshot_path = "run_1/obs_failed.png";
    const server = startServer((req, res) => {
      if (req.method === "POST" && req.url === "/sessions") {
        jsonResponse(res, 200, { session_id: "bsess_1" });
        return;
      }
      if (req.method === "POST" && req.url?.endsWith("/navigate")) {
        jsonResponse(res, 200, observed);
        return;
      }
      jsonResponse(res, 500, { error: "unexpected", detail: req.url });
    });
    servers.push(server);
    const builder = await makeBuilder(server.url);

    await builder.call("open_page", { url: LIST_URL, intent: "open products" });
    const text = await builder.call("assert_text", {
      text: "DoesNotExist",
      intent: "verify missing product",
    });
    const url = await builder.call("assert_url", {
      contains: "/missing",
      intent: "verify missing path",
    });

    for (const outcome of [text, url]) {
      expect(outcome.result.ok).toBe(false);
      expect(outcome.result.error).toBe("condition_unmet");
      expect(outcome.result.page?.screenshot_path).toBe("run_1/obs_failed.png");
      expect(outcome.result.steps).toEqual([
        expect.objectContaining({ index: 0, action: "goto" }),
      ]);
    }
  });

  it("navigation retries transient worker failures three times and reports navigation_failed", async () => {
    let navigateCalls = 0;
    const server = startServer((req, res) => {
      if (req.method === "POST" && req.url === "/sessions") {
        jsonResponse(res, 200, { session_id: "bsess_1" });
        return;
      }
      if (req.method === "POST" && req.url?.endsWith("/navigate")) {
        navigateCalls += 1;
        jsonResponse(res, 500, {
          error: "worker_error",
          detail: "transient boom",
        });
        return;
      }
      jsonResponse(res, 500, { error: "unexpected", detail: req.url });
    });
    servers.push(server);
    const builder = await makeBuilder(server.url);

    const result = await builder.call("open_page", {
      url: LIST_URL,
      intent: "open products",
    });
    expect(result.result.ok).toBe(false);
    expect(result.result.error).toBe("navigation_failed");
    expect(navigateCalls).toBe(3);
    expect(builder.committedSteps).toHaveLength(0);
  });

  it("overload pages report site_overloaded", async () => {
    const server = startServer((req, res) => {
      if (req.method === "POST" && req.url === "/sessions") {
        jsonResponse(res, 200, { session_id: "bsess_1" });
        return;
      }
      if (req.method === "POST" && req.url?.endsWith("/navigate")) {
        jsonResponse(
          res,
          200,
          baseObservation(LIST_URL, {
            title: "This website is under heavy load",
            structures: [
              {
                ref: "overload",
                kind: "overlay",
                tag: "div",
                full_text: "This website is under heavy load",
                visible: true,
              },
            ],
          }),
        );
        return;
      }
      jsonResponse(res, 500, { error: "unexpected", detail: req.url });
    });
    servers.push(server);
    const builder = await makeBuilder(server.url);

    const result = await builder.call("open_page", {
      url: LIST_URL,
      intent: "open products",
    });
    expect(result.result.ok).toBe(false);
    expect(result.result.error).toBe("site_overloaded");
    expect(builder.committedSteps).toHaveLength(0);
  });

  it("finish_case with a passing dry run returns case and execution", async () => {
    const server = startServer((req, res, body) => {
      if (req.method === "POST" && req.url === "/sessions") {
        jsonResponse(res, 200, { session_id: "bsess_1" });
        return;
      }
      if (req.method === "POST" && req.url?.endsWith("/navigate")) {
        jsonResponse(res, 200, listObservation());
        return;
      }
      if (req.method === "POST" && req.url?.endsWith("/act")) {
        jsonResponse(res, 200, passingActResponse(detailObservation()));
        return;
      }
      if (req.method === "POST" && req.url === "/execute") {
        const payload = body as { case: Case };
        jsonResponse(res, 200, {
          execution_id: "exec_1",
          status: "passed",
          started_at: "2026-10-26T12:00:00Z",
          finished_at: "2026-10-26T12:00:01Z",
          final_url: DETAIL_URL,
          steps: payload.case.steps.map((step) => ({
            index: step.index,
            action: step.action,
            status: "passed",
            started_at: "2026-10-26T12:00:00Z",
            duration_ms: 10,
            url_before: LIST_URL,
            url_after: DETAIL_URL,
            conditions: [],
            evidence: {
              screenshot_path: null,
              console: [],
              network: [],
            },
            error: null,
          })),
          error: null,
        } satisfies ExecutionResult);
        return;
      }
      jsonResponse(res, 500, { error: "unexpected", detail: req.url });
    });
    servers.push(server);
    const builder = await makeBuilder(server.url);

    await builder.call("open_page", { url: LIST_URL, intent: "open products" });
    await builder.call("click", {
      hint: "Widget",
      intent: "open widget",
      expect_url: "/item/1",
    });

    const finished = await builder.call("finish_case", { name: "open widget" });
    expect(finished.result.ok).toBe(true);
    expect(finished.case).toBeDefined();
    expect(finished.dryRun?.status).toBe("passed");
    expect(finished.case?.steps).toHaveLength(2);
  });

  it("finish_case retains the authored case when its dry run fails", async () => {
    const server = startServer((req, res, body) => {
      if (req.method === "POST" && req.url === "/sessions") {
        jsonResponse(res, 200, { session_id: "bsess_1" });
        return;
      }
      if (req.method === "POST" && req.url?.endsWith("/navigate")) {
        jsonResponse(res, 200, listObservation());
        return;
      }
      if (req.method === "POST" && req.url === "/execute") {
        const payload = body as { case: Case };
        jsonResponse(res, 200, {
          execution_id: "exec_failed",
          status: "failed",
          started_at: "2026-10-26T12:00:00Z",
          finished_at: "2026-10-26T12:00:01Z",
          final_url: LIST_URL,
          steps: payload.case.steps.map((step) => ({
            index: step.index,
            action: step.action,
            status: "failed",
            started_at: "2026-10-26T12:00:00Z",
            duration_ms: 10,
            url_before: LIST_URL,
            url_after: LIST_URL,
            conditions: [],
            evidence: {
              screenshot_path: "run_1/dry_run_failed.png",
              console: [],
              network: [],
            },
            error: { kind: "condition_unmet", message: "missing" },
          })),
          error: { kind: "condition_unmet", message: "missing" },
        } satisfies ExecutionResult);
        return;
      }
      jsonResponse(res, 500, { error: "unexpected", detail: req.url });
    });
    servers.push(server);
    const builder = await makeBuilder(server.url);

    await builder.call("open_page", { url: LIST_URL, intent: "open products" });
    const finished = await builder.call("finish_case", { name: "failing case" });

    expect(finished.result.ok).toBe(false);
    expect(finished.case?.steps).toHaveLength(1);
    expect(finished.dryRun?.status).toBe("failed");
  });
});
