import { execFileSync } from "node:child_process";
import { existsSync, rmSync } from "node:fs";
import { Writable } from "node:stream";
import { fileURLToPath, pathToFileURL } from "node:url";

import { afterEach, describe, expect, it, vi } from "vitest";
import { z } from "zod";

import {
  ModelRuntime,
  type CreateAgentSessionOptions,
  type ResourceLoader,
} from "@earendil-works/pi-coding-agent";

import {
  BROWSER_TOOL_SCHEMAS,
  type BrowserToolName,
} from "../src/mcp/schemas.js";
import { createBrowserSession, createPiSession } from "../src/pi-session.js";
import { HarnessEventWriter, type RunRequest } from "../src/run-protocol.js";
import { SessionCleanupError } from "../src/server.js";

const PACKAGE_ROOT = fileURLToPath(new URL("..", import.meta.url));
const TYPESCRIPT_COMPILER_PATH = fileURLToPath(
  new URL("../node_modules/typescript/bin/tsc", import.meta.url),
);
const BUILT_PI_SESSION_PATH = fileURLToPath(
  new URL("../dist/pi-session.js", import.meta.url),
);
const BUILT_MCP_CHILD_PATH = fileURLToPath(
  new URL("../dist/mcp/server.js", import.meta.url),
);

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
  getToolDefinition: ReturnType<typeof vi.fn>;
  extensionRunner: {
    createToolContext: ReturnType<typeof vi.fn>;
    getToolDefinition: ReturnType<typeof vi.fn>;
  };
}

function makeFakeSession(activeTools: string[] = []): FakeSession {
  return {
    prompt: vi.fn(),
    abort: vi.fn().mockResolvedValue(undefined),
    dispose: vi.fn(),
    bindExtensions: vi.fn().mockResolvedValue(undefined),
    subscribe: vi.fn(() => () => {}),
    getActiveToolNames: vi.fn(() => activeTools),
    getToolDefinition: vi.fn(() => ({
      async execute() { return { structuredContent: { structuredContent: { closed: true } } }; },
    })),
    extensionRunner: {
      createToolContext: vi.fn(() => ({})),
      getToolDefinition: vi.fn(() => ({
        async execute() { return { structuredContent: { structuredContent: { closed: true } } }; },
      })),
    },
  };
}

async function createDefaultResourceLoader(
  mcpChildPath?: string,
): Promise<ResourceLoader> {
  const runtime = await ModelRuntime.create({ refreshOnCreate: false });
  const fake = makeFakeSession();
  const { writer } = makeWriter();
  let loader: ResourceLoader | undefined;

  await createBrowserSession(baseRequest, {
    createAgentSession: async (options) => {
      loader = options.resourceLoader;
      return { session: fake as never, extensionsResult: {} as never };
    },
    modelRuntime: runtime,
    eventWriter: writer,
    ...(mcpChildPath === undefined ? {} : { mcpChildPath }),
  });

  if (loader === undefined) {
    throw new Error("createAgentSession did not receive a resource loader");
  }
  return loader;
}

function browserHarnessMcpChildPath(
  loader: ResourceLoader,
): string | undefined {
  const server = loader
    .getExtensions()
    .runtime.mcpServers.get("browser-harness");
  if (server === undefined || !("command" in server.config)) {
    throw new Error("browser-harness stdio MCP server was not registered");
  }
  return server.config.args?.[0];
}

async function createSessionWithRequest(
  request: RunRequest,
  runtime: ModelRuntime,
): Promise<CreateAgentSessionOptions> {
  const fake = makeFakeSession();
  const { writer } = makeWriter();
  let captured: CreateAgentSessionOptions | undefined;

  await createBrowserSession(request, {
    createAgentSession: async (options) => {
      captured = options;
      return { session: fake as never, extensionsResult: {} as never };
    },
    modelRuntime: runtime,
    eventWriter: writer,
  });

  if (captured === undefined) {
    throw new Error("createAgentSession was not called");
  }
  return captured;
}

async function scriptedResponse(
  runtime: ModelRuntime,
  modelName: string,
  goal: string,
  priorToolResults: number,
) {
  const model = runtime.getModel("scripted", modelName);
  if (model === undefined) {
    throw new Error(`scripted model ${modelName} was not registered`);
  }

  return runtime.streamSimple(model, {
    messages: [
      { role: "user", content: goal, timestamp: 1 },
      ...Array.from({ length: priorToolResults }, (_, index) => ({
        role: "toolResult" as const,
        toolCallId: `scripted-tool-${index + 1}`,
        toolName: "mcp__browser_harness__open_page",
        content: [],
        isError: false,
        timestamp: index + 2,
      })),
    ],
  }).result();
}

function onlyToolCall(message: Awaited<ReturnType<typeof scriptedResponse>>) {
  expect(message.content).toHaveLength(1);
  const content = message.content[0];
  if (content === undefined || content.type !== "toolCall") {
    throw new Error("expected a tool call");
  }
  return content;
}

function expectMcpToolArguments(
  fullName: string,
  arguments_: Record<string, unknown>,
): void {
  const name = fullName.replace(
    "mcp__browser_harness__",
    "",
  ) as BrowserToolName;
  const schema = BROWSER_TOOL_SCHEMAS.find((item) => item.name === name);
  expect(schema, `schema for ${fullName} must exist`).toBeDefined();
  expect(z.object(schema!.inputSchema).safeParse(arguments_).success).toBe(true);
}

describe("createBrowserSession", () => {
  it.each(["model-context-password-canary", "input", "value", "passed"])("sanitizes known password %s tool arguments before model invocation and event capture", async (secret) => {
    const runtime = await ModelRuntime.create({ refreshOnCreate: false });
    const provider = vi.fn(() => ({} as never));
    runtime.streamSimple = provider;
    const fake = makeFakeSession();
    const { writer, chunks } = makeWriter();
    const session = await createBrowserSession(baseRequest, {
      modelRuntime: runtime, eventWriter: writer,
      createAgentSession: async () => ({
        session: { ...fake, extensionRunner: { ...fake.extensionRunner, async emit() {} } } as never,
        extensionsResult: {} as never,
      }),
    });
    try {
      const emit = fake.subscribe.mock.calls[0]![0] as (event: unknown) => void;
      const expected = "model-context-password-expectation";
      const args = { value: secret, expect_value: expected, hint: "Password", intent: "sign in" };
      emit({
        type: "tool_execution_start", toolCallId: "password-1",
        toolName: "mcp__browser_harness__input", args,
      });
      emit({
        type: "tool_execution_end", toolCallId: "password-1",
        toolName: "mcp__browser_harness__input", args, isError: false,
        result: { content: [], structuredContent: { ok: true, redacted_args: ["value", "expect_value"] } },
      });
      const context = {
        messages: [
          { role: "user", content: "Canonical user goal", timestamp: 1 },
          { role: "assistant", content: [
            { type: "toolCall", id: "password-1", name: "mcp__browser_harness__input", arguments: args },
            { type: "toolCall", id: "normal-1", name: "mcp__browser_harness__input",
              arguments: { value: "public-search", expect_value: "public-search" } },
          ], timestamp: 2 },
          { role: "toolResult", toolCallId: "password-1", toolName: "mcp__browser_harness__input",
            content: [{ type: "text", text: `old observation ${secret}` }], timestamp: 3 },
        ],
      };
      runtime.streamSimple(runtime.getModel("scripted", "browser-fixture")!, context as never);
      if (secret === "model-context-password-canary") {
        expect(JSON.stringify(provider.mock.calls[0])).not.toContain(secret);
      }
      expect(JSON.stringify(provider.mock.calls[0])).not.toContain(expected);
      expect(JSON.stringify(provider.mock.calls[0])).toContain("public-search");
      expect(provider.mock.calls[0]).toEqual(expect.arrayContaining([
        expect.objectContaining({ messages: expect.arrayContaining([
          expect.objectContaining({ role: "assistant", content: expect.arrayContaining([
            expect.objectContaining({ name: "mcp__browser_harness__input", arguments: {
              value: "[redacted]", expect_value: "[redacted]", hint: "Password", intent: "sign in",
            } }),
          ]) }),
        ]) }),
      ]));
      expect(context.messages[1]!.content).toEqual(expect.arrayContaining([
        expect.objectContaining({ arguments: args }),
      ]));
      emit({
        type: "message_end", message: {
          role: "assistant", content: [{ type: "text", text: `echo ${secret}` }],
          provider: "scripted", model: "browser-fixture", stopReason: "stop", timestamp: 4,
        },
      });
      if (secret === "model-context-password-canary") expect(chunks.join("")).not.toContain(secret);
      expect(chunks.join("")).not.toContain(expected);
      emit({ type: "agent_settled", aborted: false });
      expect(chunks.join("")).toContain("public-search");
      expect(chunks.join("")).toContain("Canonical user goal");
    } finally {
      await session.close();
    }
  });

  it("reports failed initialization cleanup as a fatal lifecycle error", async () => {
    const runtime = await ModelRuntime.create({ refreshOnCreate: false });
    const fake = makeFakeSession();
    const { writer } = makeWriter();
    fake.bindExtensions.mockRejectedValue(new Error("bind failed"));
    await expect(createBrowserSession(baseRequest, {
      modelRuntime: runtime, eventWriter: writer,
      createAgentSession: async () => ({
        session: {
          ...fake,
          extensionRunner: { ...fake.extensionRunner, async emit() { throw new Error("shutdown failed"); } },
        } as never,
        extensionsResult: {} as never,
      }),
    })).rejects.toBeInstanceOf(SessionCleanupError);
    expect(fake.dispose).toHaveBeenCalledTimes(1);
  });
  it("closes a bound session if the selected mapper cannot initialize", async () => {
    const runtime = await ModelRuntime.create({ refreshOnCreate: false });
    const fake = makeFakeSession();
    const { writer } = makeWriter();
    const order: string[] = [];
    fake.dispose.mockImplementation(() => { order.push("dispose"); });
    await expect(createPiSession(baseRequest, {
      modelRuntime: runtime, eventWriter: writer,
      createAgentSession: async () => ({
        session: { ...fake, extensionRunner: { async emit() { order.push("shutdown"); } } } as never,
        extensionsResult: {} as never,
      }),
    }, {
      systemPrompt: "", skillPaths: [], toolAllowlist: [], mcpServers: [],
      completionPolicy: () => ({ verdict: "succeeded" }),
      createEventMapper() { throw new Error("mapper initialization failed"); },
    })).rejects.toThrow("mapper initialization failed");
    expect(order).toEqual(["shutdown", "dispose"]);
  });
  it("closes a session before rejecting failed extension initialization", async () => {
    const runtime = await ModelRuntime.create({ refreshOnCreate: false });
    const fake = makeFakeSession();
    const { writer } = makeWriter();
    const order: string[] = [];
    let finishShutdown!: () => void;
    const shutdown = new Promise<void>((resolve) => { finishShutdown = resolve; });
    fake.bindExtensions.mockRejectedValue(new Error("bind failed"));
    const withShutdown = {
      ...fake,
      extensionRunner: {
        ...fake.extensionRunner,
        async emit() { order.push("shutdown"); await shutdown; },
      },
    };
    fake.dispose.mockImplementation(() => order.push("dispose"));
    const creating = createBrowserSession(baseRequest, {
      modelRuntime: runtime, eventWriter: writer,
      createAgentSession: async () => ({ session: withShutdown as never, extensionsResult: {} as never }),
    });
    void creating.catch(() => {});
    await vi.waitFor(() => expect(order).toEqual(["shutdown"]));
    finishShutdown();
    await expect(creating).rejects.toThrow("bind failed");
    expect(order).toEqual(["shutdown", "dispose"]);
  });

  it("aborts and awaits cleanup when cancelled during extension binding", async () => {
    const runtime = await ModelRuntime.create({ refreshOnCreate: false });
    const fake = makeFakeSession();
    const { writer } = makeWriter();
    const controller = new AbortController();
    const order: string[] = [];
    let finishBinding!: () => void;
    const binding = new Promise<void>((resolve) => { finishBinding = resolve; });
    fake.bindExtensions.mockImplementation(async () => { order.push("bind"); await binding; });
    fake.abort.mockImplementation(async () => { order.push("abort"); finishBinding(); });
    fake.dispose.mockImplementation(() => { order.push("dispose"); });
    const creating = createBrowserSession(baseRequest, {
      modelRuntime: runtime, eventWriter: writer, signal: controller.signal,
      createAgentSession: async () => ({
        session: { ...fake, extensionRunner: { async emit() { order.push("shutdown"); } } } as never,
        extensionsResult: {} as never,
      }),
    });
    void creating.catch(() => {});
    await vi.waitFor(() => expect(order).toEqual(["bind"]));
    controller.abort();
    await expect(creating).rejects.toThrow();
    expect(order).toEqual(["bind", "abort", "shutdown", "dispose"]);
  });
  it("reloads the selected resource loader before creating the agent session", async () => {
    const runtime = await ModelRuntime.create({ refreshOnCreate: false });
    const fake = makeFakeSession();
    const { writer } = makeWriter();
    const order: string[] = [];
    const resourceLoader = {
      async reload() {
        await Promise.resolve();
        order.push("reload");
      },
    } as ResourceLoader;

    await createBrowserSession(baseRequest, {
      createAgentSession: async () => {
        order.push("createAgentSession");
        return { session: fake as never, extensionsResult: {} as never };
      },
      modelRuntime: runtime,
      eventWriter: writer,
      resourceLoader,
    });

    expect(order).toEqual(["reload", "createAgentSession"]);
  });

  it("passes the injected MCP child path to the default resource loader", async () => {
    const injectedChildPath = "/tmp/injected-browser-harness-mcp.js";
    const loader = await createDefaultResourceLoader(injectedChildPath);

    expect(browserHarnessMcpChildPath(loader)).toBe(injectedChildPath);
  });

  it("registers the emitted MCP child from the compiled session module", async () => {
    rmSync(new URL("../dist", import.meta.url), {
      recursive: true,
      force: true,
    });
    execFileSync(
      process.execPath,
      [TYPESCRIPT_COMPILER_PATH, "--project", "tsconfig.build.json"],
      { cwd: PACKAGE_ROOT },
    );

    const compiledModule = (await import(
      `${pathToFileURL(BUILT_PI_SESSION_PATH).href}?test=${Date.now()}`
    )) as typeof import("../src/pi-session.js");
    const runtime = await ModelRuntime.create({ refreshOnCreate: false });
    const fake = makeFakeSession();
    const { writer } = makeWriter();
    let registeredChildArg: string | undefined;

    await compiledModule.createBrowserSession(baseRequest, {
      createAgentSession: async (options) => {
        registeredChildArg = browserHarnessMcpChildPath(
          options.resourceLoader as ResourceLoader,
        );
        return { session: fake as never, extensionsResult: {} as never };
      },
      modelRuntime: runtime,
      eventWriter: writer,
    });

    expect(registeredChildArg).toBe(BUILT_MCP_CHILD_PATH);
    expect(existsSync(BUILT_MCP_CHILD_PATH)).toBe(true);
  }, 30_000);

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

describe("registered providers", () => {
  afterEach(() => {
    vi.unstubAllEnvs();
  });

  it("resolves the requested scripted model and passes it to createAgentSession", async () => {
    const runtime = await ModelRuntime.create({ refreshOnCreate: false });
    const request: RunRequest = {
      ...baseRequest,
      model: {
        ...baseRequest.model,
        name: "browser-eval-fixture",
      },
    };

    const captured = await createSessionWithRequest(request, runtime);

    expect(captured.model).toMatchObject({
      id: "browser-eval-fixture",
      name: "browser-eval-fixture",
      reasoning: false,
      input: ["text"],
      contextWindow: 128_000,
      maxTokens: 4096,
      cost: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0 },
    });
  });

  it("resolves the requested external model and uses the selected provider API", async () => {
    vi.stubEnv("AGENTTRACE_MODEL_BASE_URL", "https://api.example.com/v1");
    vi.stubEnv("AGENTTRACE_MODEL_API_KEY", "test-key");
    vi.stubEnv("AGENTTRACE_PI_PROVIDER_API", "openai-responses");
    const runtime = await ModelRuntime.create({ refreshOnCreate: false });
    const request: RunRequest = {
      ...baseRequest,
      model: {
        provider: "openai-compatible",
        name: "gpt-test",
        reasoning_effort: "low",
      },
    };

    const captured = await createSessionWithRequest(request, runtime);

    expect(captured.model).toMatchObject({
      id: "gpt-test",
      name: "gpt-test",
      reasoning: true,
      input: ["text"],
      contextWindow: 128_000,
      maxTokens: 4096,
      cost: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0 },
    });
    expect(runtime.getRegisteredProviderConfig("openai-compatible")?.api).toBe(
      "openai-responses",
    );
  });
});

describe("scripted provider", () => {
  it("emits the canonical browser tool sequence from prior tool results", async () => {
    const runtime = await ModelRuntime.create({ refreshOnCreate: false });
    await createSessionWithRequest(
      {
        ...baseRequest,
        model: {
          ...baseRequest.model,
          name: "browser-eval-fixture",
        },
      },
      runtime,
    );
    const expected = [
      {
        name: "mcp__browser_harness__open_page",
        arguments: {
          url: "http://fixture-site/index.html",
          intent: "Open the local catalog",
        },
      },
      {
        name: "mcp__browser_harness__input",
        arguments: {
          target: { object: { text: "Search items" } },
          value: "alpha",
          submit: true,
          intent: "Filter the catalog to Alpha",
          expect_value: "alpha",
          expect_gone: "Beta",
        },
      },
      {
        name: "mcp__browser_harness__click",
        arguments: {
          target: { object: { text: "Alpha" } },
          intent: "Open the Alpha item",
          expect_url: "detail.html",
          expect_gone: "Loading details",
        },
      },
      {
        name: "mcp__browser_harness__input",
        arguments: {
          target: { object: { text: "Quantity" } },
          value: "2",
          intent: "Set quantity to two",
          expect_value: "2",
        },
      },
      {
        name: "mcp__browser_harness__click",
        arguments: {
          target: { object: { text: "Add to cart" } },
          intent: "Add Alpha to the cart",
          expect_text: "Added!",
        },
      },
      {
        name: "mcp__browser_harness__click",
        arguments: {
          target: { object: { text: "View Cart" } },
          intent: "Open the cart",
          expect_url: "cart.html",
        },
      },
      {
        name: "mcp__browser_harness__assert_text",
        arguments: {
          text: "Alpha x 2",
          intent: "Verify the final cart quantity",
        },
      },
      {
        name: "mcp__browser_harness__finish_case",
        arguments: { name: "canonical" },
      },
    ];

    for (const [step, expectedCall] of expected.entries()) {
      const response = await scriptedResponse(
        runtime,
        "browser-eval-fixture",
        "Verify the catalog cart",
        step,
      );
      expect(response.stopReason).toBe("toolUse");
      expect(response.model).toBe("browser-eval-fixture");
      const call = onlyToolCall(response);
      expect(call).toMatchObject({
        id: `scripted-tool-${step + 1}`,
        ...expectedCall,
      });
      expectMcpToolArguments(call.name, call.arguments);
    }

    const finalResponse = await scriptedResponse(
      runtime,
      "browser-eval-fixture",
      "Verify the catalog cart",
      expected.length,
    );
    expect(finalResponse.stopReason).toBe("stop");
    expect(finalResponse.content).toEqual([
      { type: "text", text: "Final: Alpha x 2 is in the cart." },
    ]);
  });

  it("asserts a missing goal text and never emits finish_case", async () => {
    const runtime = await ModelRuntime.create({ refreshOnCreate: false });
    await createSessionWithRequest(
      {
        ...baseRequest,
        model: {
          ...baseRequest.model,
          name: "browser-eval-fixture",
        },
      },
      runtime,
    );

    const first = await scriptedResponse(
      runtime,
      "browser-eval-fixture",
      "Find DoesNotExist",
      0,
    );
    const second = await scriptedResponse(
      runtime,
      "browser-eval-fixture",
      "Find DoesNotExist",
      1,
    );
    const finalResponse = await scriptedResponse(
      runtime,
      "browser-eval-fixture",
      "Find DoesNotExist",
      2,
    );

    expect(onlyToolCall(first)).toMatchObject({
      name: "mcp__browser_harness__open_page",
    });
    const missingTextCall = onlyToolCall(second);
    expect(missingTextCall).toMatchObject({
      name: "mcp__browser_harness__assert_text",
      arguments: {
        text: "DoesNotExist",
        intent: "Verify the missing product is visible",
      },
    });
    expectMcpToolArguments(missingTextCall.name, missingTextCall.arguments);
    expect(finalResponse.stopReason).toBe("stop");
    expect(finalResponse.content).not.toContainEqual(
      expect.objectContaining({
        type: "toolCall",
        name: "mcp__browser_harness__finish_case",
      }),
    );
  });
});

describe("registerOpenAiCompatibleProvider", () => {
  afterEach(() => {
    vi.unstubAllEnvs();
  });

  function makeOpenAiRequest(provider = "openai-compatible"): RunRequest {
    return {
      ...baseRequest,
      model: {
        provider,
        name: "gpt-test",
        reasoning_effort: "low",
      },
    };
  }

  it("defaults the provider api to openai-completions", async () => {
    vi.stubEnv("AGENTTRACE_MODEL_BASE_URL", "https://api.example.com/v1");
    vi.stubEnv("AGENTTRACE_MODEL_API_KEY", "test-key");

    const runtime = await ModelRuntime.create({ refreshOnCreate: false });
    const fake = makeFakeSession();
    const { writer } = makeWriter();

    await createBrowserSession(makeOpenAiRequest(), {
      createAgentSession: async () => ({
        session: fake as never,
        extensionsResult: {} as never,
      }),
      modelRuntime: runtime,
      eventWriter: writer,
    });

    const config = runtime.getRegisteredProviderConfig("openai-compatible");
    expect(config).toBeDefined();
    expect(config!.api).toBe("openai-completions");
  });

  it("honours AGENTTRACE_PI_PROVIDER_API=openai-responses", async () => {
    vi.stubEnv("AGENTTRACE_MODEL_BASE_URL", "https://api.example.com/v1");
    vi.stubEnv("AGENTTRACE_MODEL_API_KEY", "test-key");
    vi.stubEnv("AGENTTRACE_PI_PROVIDER_API", "openai-responses");

    const runtime = await ModelRuntime.create({ refreshOnCreate: false });
    const fake = makeFakeSession();
    const { writer } = makeWriter();

    await createBrowserSession(makeOpenAiRequest(), {
      createAgentSession: async () => ({
        session: fake as never,
        extensionsResult: {} as never,
      }),
      modelRuntime: runtime,
      eventWriter: writer,
    });

    const config = runtime.getRegisteredProviderConfig("openai-compatible");
    expect(config).toBeDefined();
    expect(config!.api).toBe("openai-responses");
  });
});
