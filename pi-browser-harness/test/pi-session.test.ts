import { Writable } from "node:stream";

import { describe, expect, it, vi } from "vitest";

import {
  ModelRuntime,
  type CreateAgentSessionOptions,
  type ResourceLoader,
} from "@earendil-works/pi-coding-agent";

import { createBrowserSession } from "../src/pi-session.js";
import { HarnessEventWriter, type RunRequest } from "../src/run-protocol.js";

const baseRequest: RunRequest = {
  protocol_version: "pi-browser-harness.v1",
  run_id: "run_1",
  attempt_id: "attempt_1",
  goal: "verify the cart",
  model: {
    provider: "scripted",
    name: "browser-fixture",
    reasoning_effort: "high",
  },
  worker_url: "http://worker:8100",
  artifact_scope: "run_1",
};

function makeWriter() {
  const chunks: string[] = [];
  const output = new Writable({
    write(chunk, _encoding, callback) {
      chunks.push(chunk.toString());
      callback();
    },
  });
  return { writer: new HarnessEventWriter(baseRequest, output), chunks };
}

interface FakeSession {
  prompt: ReturnType<typeof vi.fn>;
  abort: ReturnType<typeof vi.fn>;
  dispose: ReturnType<typeof vi.fn>;
  bindExtensions: ReturnType<typeof vi.fn>;
  subscribe: ReturnType<typeof vi.fn>;
  getActiveToolNames: ReturnType<typeof vi.fn>;
}

function makeFakeSession(activeTools: string[] = []): FakeSession {
  return {
    prompt: vi.fn(),
    abort: vi.fn().mockResolvedValue(undefined),
    dispose: vi.fn(),
    bindExtensions: vi.fn().mockResolvedValue(undefined),
    subscribe: vi.fn(() => () => {}),
    getActiveToolNames: vi.fn(() => activeTools),
  };
}

describe("createBrowserSession", () => {
  it("forces the checked-in system prompt and an empty append override", async () => {
    const runtime = await ModelRuntime.create({ refreshOnCreate: false });
    const fake = makeFakeSession();
    let captured: CreateAgentSessionOptions | undefined;
    const { writer } = makeWriter();

    await createBrowserSession(baseRequest, {
      createAgentSession: async (options) => {
        captured = options;
        return { session: fake as never, extensionsResult: {} as never };
      },
      modelRuntime: runtime,
      eventWriter: writer,
    });

    const loader = captured!.resourceLoader as ResourceLoader;
    await loader.reload();

    expect(loader.getSystemPrompt()).toContain(
      "Author exactly one executable browser case",
    );
    expect(loader.getAppendSystemPrompt()).toEqual([]);
  });

  it("loads only the checked-in browser-case-authoring skill", async () => {
    const runtime = await ModelRuntime.create({ refreshOnCreate: false });
    const fake = makeFakeSession();
    let captured: CreateAgentSessionOptions | undefined;
    const { writer } = makeWriter();

    await createBrowserSession(baseRequest, {
      createAgentSession: async (options) => {
        captured = options;
        return { session: fake as never, extensionsResult: {} as never };
      },
      modelRuntime: runtime,
      eventWriter: writer,
    });

    const loader = captured!.resourceLoader as ResourceLoader;
    await loader.reload();

    const { skills } = loader.getSkills();
    expect(skills.map((s) => s.name)).toEqual(["browser-case-authoring"]);
  });

  it("uses in-memory session and settings managers", async () => {
    const runtime = await ModelRuntime.create({ refreshOnCreate: false });
    const fake = makeFakeSession();
    let captured: CreateAgentSessionOptions | undefined;
    const { writer } = makeWriter();

    await createBrowserSession(baseRequest, {
      createAgentSession: async (options) => {
        captured = options;
        return { session: fake as never, extensionsResult: {} as never };
      },
      modelRuntime: runtime,
      eventWriter: writer,
    });

    expect(captured!.sessionManager).toBeDefined();
    expect(captured!.sessionManager!.getSessionFile()).toBeUndefined();
    expect(captured!.settingsManager).toBeDefined();
  });

  it("restricts active tools to the browser MCP namespace", async () => {
    const runtime = await ModelRuntime.create({ refreshOnCreate: false });
    const fake = makeFakeSession(["mcp__browser_harness__open_page"]);
    const { writer } = makeWriter();

    const session = await createBrowserSession(baseRequest, {
      createAgentSession: async () => ({
        session: fake as never,
        extensionsResult: {} as never,
      }),
      modelRuntime: runtime,
      eventWriter: writer,
    });

    const active = session.getActiveToolNames();
    expect(active.every((name) => name.startsWith("mcp__browser_harness__"))).toBe(true);
  });

  it("binds extensions before the first prompt and sends the skill command", async () => {
    const runtime = await ModelRuntime.create({ refreshOnCreate: false });
    const fake = makeFakeSession();
    const order: string[] = [];
    fake.bindExtensions.mockImplementation(async () => {
      order.push("bindExtensions");
    });
    fake.prompt.mockImplementation(async (text: string) => {
      order.push(`prompt:${text}`);
    });
    const { writer } = makeWriter();

    const session = await createBrowserSession(baseRequest, {
      createAgentSession: async () => ({
        session: fake as never,
        extensionsResult: {} as never,
      }),
      modelRuntime: runtime,
      eventWriter: writer,
    });

    await session.prompt("verify the cart");

    expect(order).toEqual([
      "bindExtensions",
      "prompt:/skill:browser-case-authoring verify the cart",
    ]);
  });

  it("dispose() releases the session", async () => {
    const runtime = await ModelRuntime.create({ refreshOnCreate: false });
    const fake = makeFakeSession();
    const { writer } = makeWriter();

    const session = await createBrowserSession(baseRequest, {
      createAgentSession: async () => ({
        session: fake as never,
        extensionsResult: {} as never,
      }),
      modelRuntime: runtime,
      eventWriter: writer,
    });

    session.dispose();

    expect(fake.dispose).toHaveBeenCalledTimes(1);
  });
});
