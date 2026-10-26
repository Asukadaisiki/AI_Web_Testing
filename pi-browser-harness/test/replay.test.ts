import { createServer, type IncomingMessage, type ServerResponse } from "node:http";

import { afterEach, describe, expect, it } from "vitest";

import { WorkerClient } from "../src/browser/worker-client.js";
import {
  CaseBuilder,
  FatalCaseBuilderError,
  type CaseBuilderDeps,
} from "../src/browser/case-builder.js";
import type {
  ExecutionResult,
  Locator,
  Observation,
  StepResult,
} from "../src/browser/contract.js";
import {
  deriveActionStep,
  deriveAssertTextStep,
  deriveGotoStep,
  type Expects,
  type Target,
} from "../src/browser/contract.js";

const LIST_URL = "https://shop.test/products";
const DETAIL_URL = "https://shop.test/item/1";

function roleLocator(role: string, name: string): Locator {
  return { kind: "role", role, name, exact: true, match_count: 1 };
}

function replayObservation(
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

function actionStep(
  index: number,
  action: Parameters<typeof deriveActionStep>[1],
  intent: string,
  locator: Locator,
  value: string | undefined,
  expects: Expects,
): ReturnType<typeof deriveActionStep> {
  const target: Target = {
    hint: intent,
    locator,
    grounding: {
      observation_id: `obs_${LIST_URL}`,
      page_state_id: `ps_${LIST_URL}`,
      candidate_id: "candidate",
      page_url: LIST_URL,
    },
  };
  return deriveActionStep(
    index,
    action,
    intent,
    target,
    value,
    false,
    LIST_URL,
    expects,
  );
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
    artifactScope: "sess_replay",
    goal: "find a product",
    ...overrides,
  });
}

describe("replay and dry-run repair", () => {
  const servers: Array<{ close: () => void }> = [];
  afterEach(() => {
    for (const server of servers) server.close();
    servers.length = 0;
  });

  it("replay replaces the browser session and restores only the exact committed prefix", async () => {
    const opened: string[] = [];
    const closed: string[] = [];
    const navigated: string[] = [];
    const acts: Array<{ session: string; action: string }> = [];

    const searchLocator = roleLocator("textbox", "Search");
    const widgetLocator: Locator = {
      kind: "text",
      text: "Widget",
      exact: true,
      match_count: 1,
    };
    const cartLocator = roleLocator("button", "Add to cart");

    const listObs = replayObservation(LIST_URL, {
      elements: [
        {
          ref: "search",
          tag: "input",
          role: "textbox",
          name: "Search",
          value: "",
          visible: true,
          enabled: true,
          locators: [searchLocator],
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
          locators: [widgetLocator],
        },
      ],
    });
    const detailObs = replayObservation(DETAIL_URL, {
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
          locators: [cartLocator],
        },
      ],
    });

    const server = startServer((req, res, body) => {
      if (req.method === "POST" && req.url === "/sessions") {
        opened.push((body as { session_id: string }).session_id);
        jsonResponse(res, 200, { session_id: "bsess_new" });
        return;
      }
      if (req.method === "DELETE" && req.url?.startsWith("/sessions/")) {
        closed.push(req.url.split("/")[2]!);
        jsonResponse(res, 200, { closed: true });
        return;
      }
      if (req.method === "POST" && req.url?.endsWith("/navigate")) {
        const sessionId = req.url.split("/")[2]!;
        navigated.push(`${sessionId} ${(body as { url: string }).url}`);
        jsonResponse(res, 200, listObs);
        return;
      }
      if (req.method === "POST" && req.url?.endsWith("/act")) {
        const sessionId = req.url.split("/")[2]!;
        const action = (body as { action: string }).action;
        acts.push({ session: sessionId, action });
        const observation =
          action === "click" || action === "assert_element" ? detailObs : listObs;
        jsonResponse(res, 200, {
          status: "passed",
          observation,
          conditions: [],
          error: null,
        });
        return;
      }
      jsonResponse(res, 500, { error: "unexpected", detail: req.url });
    });
    servers.push(server);

    const goto = deriveGotoStep(0, "open products", LIST_URL);
    const inputStep = actionStep(1, "input", "enter search", searchLocator, "alpha", {
      value: "alpha",
    });
    const textStep = deriveAssertTextStep(2, "products visible", "Widget", LIST_URL);
    const clickStep = actionStep(3, "click", "open widget", widgetLocator, undefined, {
      url: "/item/1",
    });
    const elementStep = actionStep(
      4,
      "assert_element",
      "cart visible",
      cartLocator,
      undefined,
      { element: "visible" },
    );
    const failedStep = deriveAssertTextStep(5, "impossible", "Never", DETAIL_URL);

    const builder = await makeBuilder(server.url);
    builder.seedSteps([goto, inputStep, textStep, clickStep, elementStep, failedStep]);
    builder.seedBrowserSession("bsess_old");

    const execution: ExecutionResult = {
      execution_id: "exec_failed",
      status: "failed",
      started_at: "2026-10-26T12:00:00Z",
      finished_at: "2026-10-26T12:00:01Z",
      final_url: DETAIL_URL,
      steps: [
        stepResult(0, "goto", "passed"),
        stepResult(1, "input", "passed"),
        stepResult(2, "assert_text", "passed"),
        stepResult(3, "click", "passed"),
        stepResult(4, "assert_element", "passed"),
        {
          ...stepResult(5, "assert_text", "failed"),
          url_after: DETAIL_URL,
          error: { kind: "condition_unmet", message: "Never is not visible" },
        },
      ],
      error: null,
    };

    const repair = await builder.prepareDryRunRepair(execution);
    expect(repair.ok).toBe(false);
    expect(repair.error).toBe("dry_run_failed");
    expect(builder.committedSteps).toHaveLength(5);
    expect(builder.browserSessionId).toBe("bsess_new");
    expect(opened).toEqual(["sess_replay"]);
    expect(closed).toEqual(["bsess_old"]);
    expect(navigated).toEqual([`bsess_new ${LIST_URL}`]);
    expect(acts).toEqual([
      { session: "bsess_new", action: "input" },
      { session: "bsess_new", action: "click" },
      { session: "bsess_new", action: "assert_element" },
    ]);
  });

  it("a failed fresh dry run truncates from the first failed step and replays the retained prefix", async () => {
    const server = startServer((req, res) => {
      if (req.method === "POST" && req.url === "/sessions") {
        jsonResponse(res, 200, { session_id: "bsess_new" });
        return;
      }
      if (req.method === "DELETE" && req.url?.startsWith("/sessions/")) {
        jsonResponse(res, 200, { closed: true });
        return;
      }
      if (req.method === "POST" && req.url?.endsWith("/navigate")) {
        jsonResponse(res, 200, replayObservation(LIST_URL));
        return;
      }
      if (req.method === "POST" && req.url?.endsWith("/act")) {
        jsonResponse(res, 200, {
          status: "passed",
          observation: replayObservation(LIST_URL),
          conditions: [],
          error: null,
        });
        return;
      }
      jsonResponse(res, 500, { error: "unexpected", detail: req.url });
    });
    servers.push(server);

    const goto = deriveGotoStep(0, "open products", LIST_URL);
    const searchLocator = roleLocator("textbox", "Search");
    const inputStep = actionStep(1, "input", "enter", searchLocator, "alpha", {
      value: "alpha",
    });
    const failedStep = deriveAssertTextStep(2, "find widget", "Widget", LIST_URL);

    const builder = await makeBuilder(server.url);
    builder.seedSteps([goto, inputStep, failedStep]);

    const execution: ExecutionResult = {
      execution_id: "exec_failed",
      status: "failed",
      started_at: "2026-10-26T12:00:00Z",
      finished_at: "2026-10-26T12:00:01Z",
      final_url: LIST_URL,
      steps: [
        stepResult(0, "goto", "passed"),
        stepResult(1, "input", "passed"),
        {
          ...stepResult(2, "assert_text", "failed"),
          error: { kind: "condition_unmet", message: "Widget is not visible" },
        },
      ],
      error: null,
    };

    const repair = await builder.prepareDryRunRepair(execution);
    expect(repair.ok).toBe(false);
    expect(repair.error).toBe("dry_run_failed");
    expect(builder.committedSteps).toHaveLength(2);
    expect(builder.committedSteps.map((step) => step.action)).toEqual([
      "goto",
      "input",
    ]);
  });

  it("replay failure is fatal with committed_prefix_replay_failed", async () => {
    const server = startServer((req, res) => {
      if (req.method === "POST" && req.url === "/sessions") {
        jsonResponse(res, 200, { session_id: "bsess_new" });
        return;
      }
      if (req.method === "DELETE" && req.url?.startsWith("/sessions/")) {
        jsonResponse(res, 200, { closed: true });
        return;
      }
      if (req.method === "POST" && req.url?.endsWith("/navigate")) {
        jsonResponse(res, 500, {
          error: "worker_error",
          detail: "replay navigation failed",
        });
        return;
      }
      jsonResponse(res, 500, { error: "unexpected", detail: req.url });
    });
    servers.push(server);

    const goto = deriveGotoStep(0, "open products", LIST_URL);
    const failedStep = deriveAssertTextStep(1, "find widget", "Widget", LIST_URL);

    const builder = await makeBuilder(server.url);
    builder.seedSteps([goto, failedStep]);

    const execution: ExecutionResult = {
      execution_id: "exec_failed",
      status: "failed",
      started_at: "2026-10-26T12:00:00Z",
      finished_at: "2026-10-26T12:00:01Z",
      final_url: LIST_URL,
      steps: [
        stepResult(0, "goto", "passed"),
        {
          ...stepResult(1, "assert_text", "failed"),
          error: { kind: "condition_unmet", message: "Widget is not visible" },
        },
      ],
      error: null,
    };

    await expect(builder.prepareDryRunRepair(execution)).rejects.toMatchObject({
      code: "committed_prefix_replay_failed",
    });
  });

  it("a failed authoring action whose replay fails surfaces the original result and a fatal error", async () => {
    const server = startServer((req, res) => {
      if (req.method === "POST" && req.url === "/sessions") {
        jsonResponse(res, 200, { session_id: "bsess_new" });
        return;
      }
      if (req.method === "DELETE" && req.url?.startsWith("/sessions/")) {
        jsonResponse(res, 200, { closed: true });
        return;
      }
      if (req.method === "POST" && req.url?.endsWith("/navigate")) {
        jsonResponse(res, 500, {
          error: "worker_error",
          detail: "replay navigation failed",
        });
        return;
      }
      if (req.method === "POST" && req.url?.endsWith("/act")) {
        jsonResponse(res, 200, {
          status: "failed",
          observation: replayObservation(LIST_URL),
          conditions: [],
          error: { kind: "condition_unmet", message: "did not happen" },
        });
        return;
      }
      jsonResponse(res, 500, { error: "unexpected", detail: req.url });
    });
    servers.push(server);

    const searchLocator = roleLocator("textbox", "Search");
    const goto = deriveGotoStep(0, "open products", LIST_URL);
    const builder = await makeBuilder(server.url);
    builder.seedSteps([goto]);
    builder.seedObservation(
      replayObservation(LIST_URL, {
        elements: [
          {
            ref: "search",
            tag: "input",
            role: "textbox",
            name: "Search",
            value: "",
            visible: true,
            enabled: true,
            locators: [searchLocator],
          },
        ],
      }),
    );

    let fatal: FatalCaseBuilderError | undefined;
    try {
      await builder.call("click", {
        hint: "Search",
        intent: "click search",
        expect_text: "Searched",
      });
    } catch (error) {
      fatal = error as FatalCaseBuilderError;
    }
    expect(fatal).toBeInstanceOf(FatalCaseBuilderError);
    expect(fatal?.code).toBe("committed_prefix_replay_failed");
    expect(fatal?.result?.ok).toBe(false);
    expect(fatal?.result?.error).toBe("condition_unmet");
  });

  it("prepareDryRunRepair rejects an unknown failed step fatally", async () => {
    const server = startServer((req, res) => {
      if (req.method === "POST" && req.url === "/sessions") {
        jsonResponse(res, 200, { session_id: "bsess_new" });
        return;
      }
      jsonResponse(res, 500, { error: "unexpected", detail: req.url });
    });
    servers.push(server);

    const goto = deriveGotoStep(0, "open products", LIST_URL);
    const builder = await makeBuilder(server.url);
    builder.seedSteps([goto]);

    const execution: ExecutionResult = {
      execution_id: "exec_invalid",
      status: "failed",
      started_at: "2026-10-26T12:00:00Z",
      finished_at: "2026-10-26T12:00:01Z",
      final_url: LIST_URL,
      steps: [
        stepResult(0, "goto", "passed"),
        stepResult(1, "click", "failed"),
      ],
      error: null,
    };

    await expect(builder.prepareDryRunRepair(execution)).rejects.toMatchObject({
      code: "committed_prefix_replay_failed",
    });
  });
});

function stepResult(
  index: number,
  action: StepResult["action"],
  status: StepResult["status"],
): StepResult {
  return {
    index,
    action,
    status,
    started_at: "2026-10-26T12:00:00Z",
    duration_ms: 10,
    url_before: LIST_URL,
    url_after: LIST_URL,
    conditions: [],
    evidence: { screenshot_path: null, console: [], network: [] },
    error: null,
  };
}
