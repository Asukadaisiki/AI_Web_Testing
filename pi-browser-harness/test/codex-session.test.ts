import { execFile } from "node:child_process";
import { readFileSync } from "node:fs";
import { access, readFile, stat, writeFile } from "node:fs/promises";
import { createServer, Server as HttpServer } from "node:http";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import { promisify } from "node:util";
import { PassThrough } from "node:stream";
import { Codex, type CodexOptions, type ThreadEvent, type ThreadOptions, type TurnOptions } from "@openai/codex-sdk";
import { afterEach, describe, expect, it, vi } from "vitest";
import { createCodexSession, type CodexSessionDeps } from "../src/codex-session.js";
import { prepareHarnessResources as prepareResources, type HarnessResources } from "../src/harness-resources.js";
import type { ExecutionSpec } from "../src/execution-spec.js";
import type { ObservedEvent } from "../src/harness-definition.js";
import { HarnessEventWriter, RUN_PROTOCOL_VERSION, type HarnessEvent, type RunRequest } from "../src/run-protocol.js";
import { SessionCleanupError, serverMain } from "../src/server.js";

const prepareHarnessResources = (req: RunRequest) => prepareResources(req, {
  capabilities: new Map([["fixture", {
    command: process.execPath,
    args: [fileURLToPath(new URL("./fixtures/codex-session-mcp.mjs", import.meta.url))],
    config: {}, secrets: {},
  }]]),
});

const request: RunRequest & { execution: ExecutionSpec } = {
  protocol_version: RUN_PROTOCOL_VERSION, run_id: "codex-run", attempt_id: "codex-attempt",
  goal: "Use only the selected harness", model: {
    provider: "openai-responses", name: "request-model", reasoning_effort: "high",
  },
  execution: { runtime_id: "codex", harness: {
    id: "generic", name: "Generic", revision: 1, system_prompt: "Selected harness instructions.",
    skills: [{ name: "selected", content: "---\nname: selected\ndescription: Selected skill\n---\nUse SELECTED_SKILL." }],
    mcp: {},
  } },
};
const env = {
  AGENTTRACE_CODEX_BASE_URL: "http://127.0.0.1:9999/v1?private=secret",
  AGENTTRACE_CODEX_API_KEY: "private-test-key",
  AGENTTRACE_CODEX_MODEL: "configured-model",
  AGENTTRACE_CODEX_HTTP_HEADERS: JSON.stringify({ "X-Private": "private-header", Authorization: "Bearer private-bearer" }),
  HOME: "/personal/home", CODEX_HOME: "/personal/codex", OPENAI_API_KEY: "personal-key",
  CODEX_CONFIG: "personal-config", NODE_OPTIONS: "--require personal-hook",
  PATH: process.env.PATH!,
};
const cleanup: (() => Promise<void>)[] = [];
afterEach(async () => {
  for (const close of cleanup.splice(0).reverse()) await close().catch(() => {});
  vi.restoreAllMocks();
});
function writer() {
  const events: HarnessEvent[] = [];
  return { events, eventWriter: new HarnessEventWriter(request, {
    write(line) { events.push(JSON.parse(String(line))); return true; },
  }) };
}
const completed: ThreadEvent[] = [
  { type: "thread.started", thread_id: "thread-1" },
  { type: "turn.started" },
  { type: "item.completed", item: { type: "agent_message", id: "answer", text: "Selected result" } },
  { type: "turn.completed", usage: {
    input_tokens: 12, cached_input_tokens: 3, cache_write_input_tokens: 2,
    output_tokens: 5, reasoning_output_tokens: 1,
  } },
];
async function setup(options: {
  stream?: (signal: AbortSignal) => AsyncGenerator<ThreadEvent>;
  close?: () => Promise<void>;
  events?: ObservedEvent[];
  resources?: (resources: HarnessResources) => void;
  openRuntimeMcp?: CodexSessionDeps["openRuntimeMcp"];
} = {}) {
  const observed = writer();
  let resources!: HarnessResources;
  let clientOptions!: CodexOptions;
  let threadOptions!: ThreadOptions;
  let promptText = "";
  let turnSignal!: AbortSignal;
  let disposed = false;
  const deps: CodexSessionDeps = {
    ...observed, env,
    prepareHarnessResources: async (req) => {
      resources = await prepareHarnessResources(req);
      options.resources?.(resources);
      const dispose = resources.dispose;
      resources.dispose = async () => { disposed = true; await dispose(); };
      return resources;
    },
    openRuntimeMcp: options.openRuntimeMcp ?? (async () => ({
      mcpServers: { selected: { type: "http", url: "http://127.0.0.1:4321/mcp" } },
      events: options.events ?? [],
      close: options.close ?? (async () => {}),
    })),
    createClient: (opts) => {
      clientOptions = opts;
      return { startThread(opts) {
        threadOptions = opts!;
        return { async runStreamed(input, opts: TurnOptions = {}) {
          promptText = String(input); turnSignal = opts.signal!;
          return { events: options.stream ? options.stream(turnSignal) : (async function* () { yield* completed; })() };
        } };
      } };
    },
  };
  const session = await createCodexSession(request, deps);
  cleanup.push(() => session.close());
  return {
    session, observed, resources, clientOptions,
    get threadOptions() { return threadOptions; },
    get promptText() { return promptText; },
    get turnSignal() { return turnSignal; },
    get disposed() { return disposed; },
  };
}

type ResponsesTool = {
  type: string; name?: string; namespace?: string;
  parameters?: unknown; tools?: ResponsesTool[];
};
type ResponsesRequest = {
  model: string; tools: ResponsesTool[]; input: Record<string, unknown>[];
};
function responseFunctions(tools: ResponsesTool[], namespace?: string): ResponsesTool[] {
  return tools.flatMap((tool) => tool.type === "namespace"
    ? responseFunctions(tool.tools ?? [], tool.name)
    : [{ ...tool, ...(namespace ? { namespace } : {}) }]);
}

async function fakeResponses(upstream?: { baseUrl: string; apiKey: string; headers?: Record<string, string> }) {
  const requests: ResponsesRequest[] = [];
  const server = createServer(async (req, res) => {
    try {
      let body = "";
      for await (const chunk of req) body += chunk.toString();
      const request = JSON.parse(body) as ResponsesRequest;
      requests.push(request);
      if (upstream) {
        const result = await fetch(`${upstream.baseUrl.replace(/\/$/, "")}/responses`, {
          method: "POST", headers: {
            "content-type": "application/json", Authorization: `Bearer ${upstream.apiKey}`, ...upstream.headers,
          },
          body, signal: AbortSignal.timeout(45000),
        });
        res.writeHead(result.status, { "content-type": result.headers.get("content-type") ?? "text/event-stream" });
        if (result.body) for await (const chunk of result.body) res.write(chunk);
        res.end();
        return;
      }
      const tool = responseFunctions(request.tools).find((tool) =>
        tool.name === "mcp__selected__emit_receipt" ||
        (tool.namespace === "mcp__selected" && tool.name === "emit_receipt"));
      const item = requests.length === 1 && tool ? {
        type: "function_call", id: "receipt-item", call_id: "receipt-call",
        name: tool.name, ...(tool.namespace ? { namespace: tool.namespace } : {}),
        arguments: '{"label":"official-sdk"}',
      } : {
        type: "message", id: "answer", role: "assistant", status: "completed",
        content: [{ type: "output_text", text: tool ? "Receipt received" : "Selected tool unavailable", annotations: [] }],
      };
      const response = {
        id: `response-${requests.length}`, object: "response", status: "completed",
        output: [item], usage: {
          input_tokens: 20, input_tokens_details: { cached_tokens: 0 },
          output_tokens: 5, output_tokens_details: { reasoning_tokens: 0 }, total_tokens: 25,
        },
      };
      const events = [
        { type: "response.created", response: { ...response, status: "in_progress", output: [] } },
        { type: "response.output_item.added", output_index: 0, item },
        { type: "response.output_item.done", output_index: 0, item },
        { type: "response.completed", response },
      ];
      res.writeHead(200, { "content-type": "text/event-stream" });
      res.end(events.map((event) => `data: ${JSON.stringify(event)}\n\n`).join(""));
    } catch {
      res.writeHead(400).end("Invalid test Responses request");
    }
  });
  await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
  cleanup.push(() => new Promise<void>((resolve, reject) => {
    server.close((error) => error ? reject(error) : resolve());
    server.closeAllConnections();
  }));
  const address = server.address();
  if (!address || typeof address === "string") throw new Error("Test Responses listener failed");
  return { requests, baseUrl: `http://127.0.0.1:${address.port}/v1` };
}

describe("official Codex session adapter", () => {
  it("redacts deferred native model and settlement text after private password tool classification", async () => {
    const canary = "codex-model-password-canary";
    let classify: () => void = () => {};
    const ctx = await setup({
      resources(resource) { resource.servers.push({ name: "browser-harness", command: process.execPath }); },
      openRuntimeMcp: async (_resource, writer, guard) => {
        classify = () => {
          guard?.observe({ type: "tool_execution_start", toolCallId: "password",
            toolName: "mcp__browser_harness__input", args: { value: canary } });
          writer.write("pi.tool.started", { tool_call_id: "password", tool_name: "mcp__browser_harness__input" });
          guard?.observe({ type: "tool_execution_end", toolCallId: "password",
            toolName: "mcp__browser_harness__input", result: { structuredContent: { redacted_args: ["value"] } } });
          writer.write("pi.tool.completed", {
            tool_call_id: "password", tool_name: "mcp__browser_harness__input", is_error: false,
          });
        };
        return { mcpServers: {}, events: [], close: async () => {} };
      },
      stream: async function* () {
        yield { type: "item.completed", item: { type: "agent_message", id: "pre", text: `I will enter ${canary}` } };
        classify();
        expect(ctx.observed.events.map((event) => event.type)).toEqual(["pi.tool.started", "pi.tool.completed"]);
        yield { type: "item.completed", item: { type: "agent_message", id: "post", text: `Entered ${canary}` } };
        yield completed.at(-1)!;
      },
    });
    await ctx.session.prompt(`Goal ${canary}`);
    expect(ctx.promptText).toBe(`Goal ${canary}`);
    expect(JSON.stringify(ctx.observed.events)).not.toContain(canary);
    expect(ctx.observed.events.find((event) => event.type === "pi.message.completed")?.payload.text)
      .toBe("Entered [redacted]");
    expect(ctx.observed.events.find((event) => event.type === "harness.settled")?.payload.assistant_text)
      .toBe("Entered [redacted]");
  });

  it("scopes instructions, selected skills and HTTP MCP without secrets in SDK arguments", async () => {
    const ctx = await setup();
    const opts = ctx.clientOptions;
    expect(opts.config).toBeUndefined();
    expect(opts.configOverrides).toBeUndefined();
    expect(opts.apiKey).toBeUndefined();
    expect(opts.baseUrl).toBeUndefined();
    expect(opts.env?.HOME).not.toBe(env.HOME);
    expect(opts.env?.CODEX_HOME).not.toBe(env.CODEX_HOME);
    expect(opts.env?.OPENAI_API_KEY).toBeUndefined();
    expect(opts.env?.NODE_OPTIONS).toBeUndefined();
    expect(opts.env?.CODEX_CONFIG).toBeUndefined();
    const configPath = join(opts.env!.CODEX_HOME!, "config.toml");
    expect((await stat(configPath)).mode & 0o777).toBe(0o600);
    expect((await stat(opts.env!.CODEX_HOME!)).mode & 0o777).toBe(0o700);
    const config = await readFile(configPath, "utf8");
    expect(config).toContain('model_provider = "agenttrace"');
    expect(config).toContain('wire_api = "responses"');
    expect(config).toContain("requires_openai_auth = false");
    expect(config).toContain("private-header");
    expect(config).toContain("private-bearer");
    expect(config).toContain('model = "request-model"');
    expect(config).not.toContain("configured-model");
    expect(config).toContain("Selected harness instructions.");
    expect(config).toContain("SELECTED_SKILL");
    expect(config).toContain("http://127.0.0.1:4321/mcp");
    expect(config).not.toContain("/personal/");
    expect(config).toContain("shell_tool = false");
    expect(config).toContain("hooks = false");
    expect(config).toContain("skip_host_skill_discovery = true");
    await ctx.session.prompt(request.goal);
    expect(ctx.threadOptions).toMatchObject({
      sandboxMode: "read-only", networkAccessEnabled: true, webSearchMode: "disabled",
      approvalPolicy: "never", skipGitRepoCheck: true, model: "request-model",
      workingDirectory: ctx.resources.cwd, modelReasoningEffort: "high",
    });
    expect(ctx.promptText).toBe(request.goal);
    await ctx.session.close();
    await expect(access(opts.env!.CODEX_HOME!)).rejects.toThrow();
    expect(ctx.disposed).toBe(true);
  });

  it("emits one turn-level message and aggregate SDK usage without fabricated model calls", async () => {
    const ctx = await setup();
    await ctx.session.prompt(request.goal);
    expect(ctx.observed.events.map((e) => e.type)).toEqual([
      "pi.message.completed", "pi.usage", "harness.settled",
    ]);
    expect(ctx.observed.events[1]!.payload).toMatchObject({
      provider: "openai-responses", model: "request-model", granularity: "turn",
      input_tokens: 12, output_tokens: 5, cache_read_tokens: 3, cache_write_tokens: 2,
      reasoning_output_tokens: 1, total_tokens: 17,
    });
    expect(ctx.observed.events[0]!.payload).toMatchObject({ text: "Selected result", granularity: "turn" });
    expect(ctx.observed.events[0]!.payload).not.toHaveProperty("input");
    expect(ctx.observed.events[2]!.payload).toEqual({ assistant_text: "Selected result", verdict: "succeeded" });
  });

  it("uses helper evidence for completion and does not duplicate SDK MCP item events", async () => {
    const ctx = await setup({
      resources(resources) {
        resources.definition.completionPolicy = ({ events }) => ({
          verdict: events.some((e) => e.type === "pi.tool.completed") ? "succeeded" : "failed",
        });
      },
      events: [{ type: "pi.tool.completed", payload: {
        tool_name: "mcp__selected__check", tool_call_id: "actual-helper-id", is_error: false,
      } }],
      stream: async function* () {
        yield { type: "item.started", item: {
          type: "mcp_tool_call", id: "sdk-id", server: "selected", tool: "check",
          arguments: {}, status: "in_progress",
        } };
        yield* completed;
      },
    });
    await ctx.session.prompt(request.goal);
    expect(ctx.observed.events.filter((e) => e.type.startsWith("pi.tool."))).toEqual([]);
    expect(ctx.observed.events.at(-1)!.payload).toMatchObject({ verdict: "succeeded" });
  });

  it("composes supplementary selected skill text files into the scoped instructions", async () => {
    const observed = writer();
    let home = "";
    const session = await createCodexSession({
      ...request, execution: { ...request.execution, harness: {
        ...request.execution.harness,
        skills: [{ name: "selected", content: "Main selected skill", files: [
          { path: "references/guide.md", content: "SUPPLEMENTARY_SELECTED_REFERENCE" },
          { path: "examples/selected.json", content: '{"selected":"ONLY_SELECTED_EXAMPLE"}' },
        ] }],
      } },
    }, {
      ...observed, env,
      openRuntimeMcp: async () => ({ mcpServers: {}, events: [], close: async () => {} }),
      createClient: (options) => {
        home = options.env!.CODEX_HOME!;
        return { startThread: () => ({ runStreamed: async () => ({
          events: (async function* () { yield* completed; })(),
        }) }) };
      },
    });
    cleanup.push(() => session.close());
    const config = await readFile(join(home, "config.toml"), "utf8");
    expect(config).toContain("SUPPLEMENTARY_SELECTED_REFERENCE");
    expect(config).toContain("ONLY_SELECTED_EXAMPLE");
    expect(config).not.toContain("/personal/");
  });

  it.each(["turn.failed", "error", "throw", "incomplete"] as const)("preserves SDK %s failures without fallback or settlement", async (mode) => {
    const ctx = await setup({ stream: async function* () {
      if (mode === "throw") throw new Error("provider unavailable");
      if (mode === "incomplete") { yield { type: "turn.started" }; return; }
      if (mode === "turn.failed") yield { type: "turn.failed", error: { message: "provider unavailable" } };
      else yield { type: "error", message: "provider unavailable" };
    } });
    await expect(ctx.session.prompt(request.goal)).rejects.toThrow(mode === "incomplete" ? /completed/ : /provider unavailable/);
    expect(ctx.observed.events.at(-1)?.type).toBe("harness.failed");
    expect(ctx.observed.events.some((e) => e.type === "harness.settled")).toBe(false);
    await ctx.session.close();
    expect(ctx.disposed).toBe(true);
  });

  it("awaits SDK cancellation before closing MCP and disposing resources", async () => {
    const order: string[] = [];
    let started!: () => void;
    const ready = new Promise<void>((resolve) => { started = resolve; });
    let finish!: () => void;
    const joined = new Promise<void>((resolve) => { finish = resolve; });
    const ctx = await setup({
      stream: async function* (signal) {
        started();
        await new Promise<void>((resolve) => signal.addEventListener("abort", () => resolve(), { once: true }));
        await joined;
        order.push("sdk-ended");
        throw new Error("aborted");
      },
      close: async () => { order.push("mcp-closed"); },
    });
    const running = ctx.session.prompt(request.goal);
    const rejected = expect(running).rejects.toThrow(/aborted|cancel/i);
    await ready;
    const aborting = ctx.session.abort();
    const closing = ctx.session.close();
    await new Promise((resolve) => setTimeout(resolve, 10));
    expect(ctx.turnSignal.aborted).toBe(true);
    expect(order).toEqual([]);
    expect(ctx.disposed).toBe(false);
    finish();
    await Promise.all([rejected, aborting, closing]);
    expect(order).toEqual(["sdk-ended", "mcp-closed"]);
    expect(ctx.disposed).toBe(true);
  });

  it("keeps generic helper cleanup failure idempotent and retains private resources", async () => {
    let closes = 0;
    const ctx = await setup({ close: async () => { closes++; throw new Error("release rejected"); } });
    cleanup.push(() => ctx.resources.dispose());
    await ctx.session.prompt(request.goal);
    const first = ctx.session.close();
    expect(ctx.session.close()).toBe(first);
    await expect(first).rejects.toThrow("release rejected");
    await expect(ctx.session.close()).rejects.toThrow("release rejected");
    expect(closes).toBe(1);
    expect(ctx.disposed).toBe(false);
    await expect(access(ctx.resources.cwd)).resolves.toBeUndefined();
  });

  it("retains resources after an unacknowledged MCP close", async () => {
    const ctx = await setup({ close: async () => { throw new SessionCleanupError("MCP child still running"); } });
    await ctx.session.prompt(request.goal);
    await expect(ctx.session.close()).rejects.toBeInstanceOf(SessionCleanupError);
    expect(ctx.disposed).toBe(false);
    await expect(access(ctx.resources.cwd)).resolves.toBeUndefined();
    cleanup.push(() => ctx.resources.dispose());
  });

  it("preserves uncertain MCP startup cleanup and the host never emits released", async () => {
    const input = new PassThrough();
    const events: Record<string, any>[] = [];
    let resources!: HarnessResources;
    let initialized!: () => void;
    const startup = new Promise<void>((resolve) => { initialized = resolve; });
    const running = serverMain({
      input, output: { write(line) { events.push(JSON.parse(String(line))); return true; } },
      sessionFactory: (req, context) => createCodexSession(req, {
        ...context, env,
        prepareHarnessResources: async (req) => {
          resources = await prepareHarnessResources(req);
          cleanup.push(() => resources.dispose());
          return resources;
        },
        openRuntimeMcp: async () => { initialized(); throw new SessionCleanupError("MCP startup child still running"); },
      }),
    });
    input.write(`${JSON.stringify({ type: "run", request })}\n`);
    await startup;
    input.end();
    expect(await running).toBe(1);
    expect(events.some((event) => event.type === "harness.released")).toBe(false);
    expect(events.find((event) => event.type === "harness.protocol_error")?.error)
      .toContain("MCP startup child still running");
    await expect(access(resources.cwd)).resolves.toBeUndefined();
  });

  it("rejects malformed private headers before opening MCP", async () => {
    const observed = writer();
    await expect(createCodexSession(request, {
      ...observed, env: { ...env, AGENTTRACE_CODEX_HTTP_HEADERS: '{"Authorization":123}' },
      openRuntimeMcp: async () => { throw new Error("MCP should not open"); },
    })).rejects.toThrow(/headers/i);
  });

  it("rejects an unsupported explicit provider format before preparing resources", async () => {
    await expect(createCodexSession(request, {
      ...writer(), env: { ...env, AGENTTRACE_CODEX_TOOL_FORMAT: "fallback" },
      prepareHarnessResources: async () => { throw new Error("Resources should not be prepared"); },
    })).rejects.toThrow(/AGENTTRACE_CODEX_TOOL_FORMAT/);
  });

  it("cleans actual initialization resources and MCP when the provider listener cannot start", async () => {
    let root = "";
    let closed = false;
    vi.spyOn(HttpServer.prototype, "listen").mockImplementationOnce(() => { throw new Error("Listener failed"); });
    await expect(createCodexSession(request, {
      ...writer(), env: { ...env, AGENTTRACE_CODEX_TOOL_FORMAT: "flat-functions" },
      prepareHarnessResources: async (req) => {
        const resources = await prepareHarnessResources(req); root = resources.cwd; return resources;
      },
      openRuntimeMcp: async () => ({
        mcpServers: {}, events: [], close: async () => { closed = true; },
      }),
    })).rejects.toThrow("Codex provider bridge startup failed");
    expect(closed).toBe(true);
    await expect(access(root)).rejects.toThrow();
  });

  it.each(["native", "flat-functions"])("cleans %s initialization resources when constructing the SDK client fails", async (format) => {
    const observed = writer();
    let root = "";
    let loopback = "";
    await expect(createCodexSession(request, {
      ...observed, env: { ...env, AGENTTRACE_CODEX_TOOL_FORMAT: format },
      prepareHarnessResources: async (req) => {
        const resources = await prepareHarnessResources(req); root = resources.cwd; return resources;
      },
      openRuntimeMcp: async () => ({ mcpServers: {}, events: [], close: async () => {} }),
      createClient: (options) => {
        if (format === "flat-functions") {
          const config = readFileSync(join(options.env!.CODEX_HOME!, "config.toml"), "utf8");
          loopback = JSON.parse(/^base_url = (.+)$/m.exec(config)![1]!);
        }
        throw new Error("SDK initialization failed");
      },
    })).rejects.toThrow("SDK initialization failed");
    await expect(access(root)).rejects.toThrow();
    if (loopback) await expect(fetch(`${loopback}/responses`)).rejects.toThrow();
  });

  it("retains initialization resources when generic helper cleanup fails", async () => {
    let resources!: HarnessResources;
    await expect(createCodexSession(request, {
      ...writer(), env,
      prepareHarnessResources: async (req) => {
        resources = await prepareHarnessResources(req);
        cleanup.push(() => resources.dispose());
        return resources;
      },
      openRuntimeMcp: async () => ({
        mcpServers: {}, events: [], close: async () => { throw new Error("Helper close rejected"); },
      }),
      createClient: () => { throw new Error("SDK initialization failed"); },
    })).rejects.toBeInstanceOf(SessionCleanupError);
    await expect(access(resources.cwd)).resolves.toBeUndefined();
  });

  it("uses separate private homes for concurrent attempts", async () => {
    const [a, b] = await Promise.all([setup(), setup()]);
    expect(a.clientOptions.env!.CODEX_HOME).not.toBe(b.clientOptions.env!.CODEX_HOME);
    await a.session.close();
    await expect(access(b.clientOptions.env!.CODEX_HOME!)).resolves.toBeUndefined();
  });

  it("redacts private provider values from SDK errors and normalized failure events", async () => {
    const ctx = await setup({ stream: async function* () {
      throw new Error(`Provider rejected ${env.AGENTTRACE_CODEX_API_KEY} private-header private-bearer ${env.AGENTTRACE_CODEX_BASE_URL}`);
    } });
    await expect(ctx.session.prompt(request.goal)).rejects.toThrow("Provider rejected [redacted]");
    const output = JSON.stringify(ctx.observed.events);
    expect(output).not.toContain("private-test-key");
    expect(output).not.toContain("private-header");
    expect(output).not.toContain("private-bearer");
    expect(output).not.toContain("private=secret");
  });

  it("rejects a pre-aborted attempt without creating resources", async () => {
    const controller = new AbortController();
    controller.abort(new Error("pre-aborted"));
    await expect(createCodexSession(request, {
      ...writer(), env, signal: controller.signal,
      prepareHarnessResources: async () => { throw new Error("resources must not be prepared"); },
    })).rejects.toThrow("pre-aborted");
  });

  it("drains the SDK stream before preserving an error that follows turn.completed", async () => {
    let drained = false;
    const ctx = await setup({ stream: async function* () {
      yield* completed;
      yield { type: "error", message: "late SDK failure" };
      drained = true;
    } });
    await expect(ctx.session.prompt(request.goal)).rejects.toThrow("late SDK failure");
    expect(drained).toBe(true);
    expect(ctx.observed.events.map((event) => event.type)).toEqual([
      "pi.message.completed", "pi.usage", "harness.failed",
    ]);
    expect(ctx.observed.events.at(-1)!.type).toBe("harness.failed");
    expect(ctx.observed.events.some((event) => event.type === "harness.settled")).toBe(false);
  });

  it("does not emit orphaned failure usage without an SDK assistant message", async () => {
    const ctx = await setup({ stream: async function* () {
      yield { type: "turn.started" };
      yield completed[3]!;
      yield { type: "error", message: "no assistant output" };
    } });
    await expect(ctx.session.prompt(request.goal)).rejects.toThrow("no assistant output");
    expect(ctx.observed.events.map((event) => event.type)).toEqual(["harness.failed"]);
  });

  it("rejects SDK built-in tool execution outside the selected harness", async () => {
    const ctx = await setup({ stream: async function* () {
      yield { type: "item.started", item: {
        id: "shell", type: "command_execution", command: "unexpected command",
        aggregated_output: "", status: "in_progress",
      } };
    } });
    await expect(ctx.session.prompt(request.goal)).rejects.toThrow("outside the selected harness");
    expect(ctx.turnSignal.aborted).toBe(true);
    expect(ctx.observed.events.at(-1)!.type).toBe("harness.failed");
  });

  it.each([
    { type: "mcp_tool_call", id: "foreign", server: "personal", tool: "check", arguments: {}, status: "in_progress" },
    { type: "mcp_tool_call", id: "host-only", server: "selected", tool: "close_session", arguments: {}, status: "in_progress" },
    { type: "todo_list", id: "native-plan", items: [] },
    { type: "unknown_native_tool", id: "future-tool" },
  ])("rejects unselected or native SDK items: $id", async (item) => {
    const ctx = await setup({ stream: async function* () {
      yield { type: "item.started", item } as ThreadEvent;
      yield* completed;
    } });
    await expect(ctx.session.prompt(request.goal)).rejects.toThrow("outside the selected harness");
    expect(ctx.turnSignal.aborted).toBe(true);
    expect(ctx.observed.events.at(-1)!.type).toBe("harness.failed");
    expect(ctx.observed.events.some((event) => event.type === "harness.settled")).toBe(false);
  });

  it("the bundled native CLI accepts the private config and sees only the selected MCP", async () => {
    const ctx = await setup();
    const { stdout } = await promisify(execFile)(ctx.clientOptions.codexPathOverride!, [
      "mcp", "list", "--json",
    ], { env: ctx.clientOptions.env!, timeout: 20000 });
    const servers = JSON.parse(stdout) as { name: string; transport: { url: string } }[];
    expect(servers.map((server) => server.name)).toEqual(["selected"]);
    expect(servers[0]!.transport.url).toBe("http://127.0.0.1:4321/mcp");
  }, 30000);

  it.each(["request-model", "search-model"])(
    "exposes and executes selected MCP schemas in the official SDK %s model request", async (model) => {
      const provider = await fakeResponses();
      const observed = writer();
      let catalog = "";
      const session = await createCodexSession({
        ...request, model: { ...request.model, name: model }, execution: { ...request.execution, harness: {
          ...request.execution.harness, mcp: { selected: {
            capability: "fixture", config: {}, secret_refs: {},
          } },
        } },
      }, {
        ...observed,
        prepareHarnessResources,
        env: { AGENTTRACE_CODEX_BASE_URL: provider.baseUrl, AGENTTRACE_CODEX_API_KEY: "offline-test-key", PATH: process.env.PATH },
        ...(model === "search-model" ? {
          prepareHarnessResources: async (req: RunRequest) => {
            const resources = await prepareHarnessResources(req);
            catalog = join(resources.cwd, "model-catalog.json");
            // A supported native catalog fixture exercises deferral without GPT's built-in patch tool.
            await writeFile(catalog, JSON.stringify({ models: [{
              slug: model, display_name: model, supported_reasoning_levels: [],
              shell_type: "unified_exec", visibility: "none", supported_in_api: true, priority: 1,
              support_verbosity: false, truncation_policy: { mode: "bytes", limit: 10000 },
              experimental_supported_tools: [], supports_search_tool: true,
              model_messages: { instructions_template: "Use only the selected harness." },
            }] }));
            return resources;
          },
          createClient: (options: CodexOptions) => new Codex({ ...options, config: { model_catalog_json: catalog } }),
        } : {}),
      });
      cleanup.push(() => session.close());
      await session.prompt("Call the selected emit_receipt MCP tool with label official-sdk.");
      const tools = responseFunctions(provider.requests[0]!.tools);
      expect(tools.map((tool) => [tool.namespace, tool.name])).toContainEqual(["mcp__selected", "emit_receipt"]);
      expect(tools.find((tool) => tool.namespace === "mcp__selected" && tool.name === "emit_receipt")?.parameters).toMatchObject({
        type: "object", properties: { label: { type: "string" } }, required: ["label"],
      });
      expect(tools.some((tool) => /close_session|shell|exec_command|apply_patch|browser|image|skill/.test(tool.name ?? ""))).toBe(false);
      // CLI 0.165.0 unconditionally adds these resource tools when MCP servers exist.
      expect(tools.map((tool) => tool.name).sort()).toEqual([
        "emit_receipt", "list_mcp_resource_templates", "list_mcp_resources", "read_mcp_resource",
      ]);
      expect(provider.requests).toHaveLength(2);
      expect(provider.requests[0]!.model).toBe(model);
      const output = provider.requests[1]!.input.find((item) =>
        item.type === "function_call_output" && item.call_id === "receipt-call");
      expect(JSON.stringify(output?.output)).toContain("receipt:official-sdk");
      expect(observed.events.filter((event) => event.type === "pi.tool.completed")).toHaveLength(1);
      expect(observed.events.find((event) => event.type === "pi.tool.completed")?.payload).toMatchObject({
        tool_name: "mcp__selected__emit_receipt", is_error: false,
        structured_result: { receipt: "receipt:official-sdk" },
      });
      expect(observed.events.at(-1)?.type).toBe("harness.settled");
      await session.close();
    }, 30000,
  );

  it.runIf(process.env.AGENTTRACE_CODEX_LIVE_MCP_TEST === "1")(
    "calls selected MCP through the configured live Responses provider with the explicit tool format", async () => {
      const format = process.env.AGENTTRACE_CODEX_TOOL_FORMAT || "native";
      const provider = await fakeResponses({
        baseUrl: process.env.AGENTTRACE_CODEX_BASE_URL!, apiKey: process.env.AGENTTRACE_CODEX_API_KEY!,
        headers: JSON.parse(process.env.AGENTTRACE_CODEX_HTTP_HEADERS || "{}"),
      });
      const observed = writer();
      const session = await createCodexSession({
        ...request, model: { ...request.model, name: process.env.AGENTTRACE_CODEX_MODEL!, reasoning_effort: "off" },
        execution: { ...request.execution, harness: {
          ...request.execution.harness, mcp: { selected: {
            capability: "fixture", config: {}, secret_refs: {},
          } },
        } },
      }, {
        ...observed,
        prepareHarnessResources,
        env: {
          AGENTTRACE_CODEX_BASE_URL: provider.baseUrl, AGENTTRACE_CODEX_API_KEY: "offline-test-key",
          AGENTTRACE_CODEX_TOOL_FORMAT: format, PATH: process.env.PATH,
        },
        signal: AbortSignal.timeout(60000),
      });
      cleanup.push(() => session.close());
      await session.prompt("Call the selected emit_receipt MCP tool with label official-sdk. Do not use other tools.");
      expect(responseFunctions(provider.requests[0]!.tools).map((tool) => [tool.namespace, tool.name]))
        .toContainEqual(format === "flat-functions"
          ? [undefined, "mcp__selected__emit_receipt"] : ["mcp__selected", "emit_receipt"]);
      const assistant = observed.events.find((event) => event.type === "pi.message.completed")?.payload.text;
      expect(observed.events.filter((event) => event.type === "pi.tool.completed").map((event) => event.payload),
        `Live provider assistant: ${assistant}`)
        .toContainEqual(expect.objectContaining({
          tool_name: "mcp__selected__emit_receipt", is_error: false,
          structured_result: { receipt: "receipt:official-sdk" },
        }));
      await session.close();
    }, 70000,
  );

  it("keeps provider secrets out of actual SDK subprocess argv", async () => {
    const observed = writer();
    let root = "";
    const session = await createCodexSession(request, {
      ...observed,
      env: { ...env, AGENTTRACE_CODEX_PATH: fileURLToPath(new URL("./fixtures/codex-session-native.mjs", import.meta.url)) },
      prepareHarnessResources: async (req) => {
        const resources = await prepareHarnessResources(req); root = resources.cwd; return resources;
      },
      openRuntimeMcp: async () => ({ mcpServers: {}, events: [], close: async () => {} }),
    });
    cleanup.push(() => session.close());
    await session.prompt("fixture:inspect");
    const capture = JSON.parse(await readFile(join(root, ".codex-home", "fixture-argv.json"), "utf8")) as {
      argv: string[]; environmentKeys: string[];
    };
    expect(capture.argv).toContain("exec");
    expect(capture.argv).toContain("--experimental-json");
    for (const secret of ["private-test-key", "private-header", "private-bearer", "private=secret"]) {
      expect(capture.argv.join(" ")).not.toContain(secret);
    }
    expect(capture.environmentKeys).not.toContain("NODE_OPTIONS");
    expect(capture.environmentKeys).not.toContain("OPENAI_API_KEY");
    expect(capture.environmentKeys).toContain("CODEX_HOME");
  }, 15000);

  it("supervises the real SDK child that closes stdout before delayed SIGTERM exit", async () => {
    const observed = writer();
    const controller = new AbortController();
    let root = "";
    let home = "";
    let cleanupSawChildClosed = false;
    const session = await createCodexSession(request, {
      ...observed, signal: controller.signal,
      env: { ...env, AGENTTRACE_CODEX_PATH: fileURLToPath(new URL("./fixtures/codex-session-native.mjs", import.meta.url)) },
      prepareHarnessResources: async (req) => {
        const resources = await prepareHarnessResources(req); root = resources.cwd; return resources;
      },
      openRuntimeMcp: async () => ({ mcpServers: {}, events: [], close: async () => {
        cleanupSawChildClosed = (await readFile(join(home, "fixture-child.closed"), "utf8")) === "closed";
      } }),
    });
    cleanup.push(() => session.close());
    home = join(root, ".codex-home");
    const running = session.prompt("fixture:cancel");
    const rejected = expect(running).rejects.toThrow();
    for (let i = 0; ; i++) {
      try { await access(join(home, "fixture-child.ready")); break; } catch {
        if (i > 500) throw new Error("fixture child never started");
        await new Promise((resolve) => setTimeout(resolve, 10));
      }
    }
    controller.abort();
    await Promise.all([rejected, session.abort(), session.close()]);
    expect(cleanupSawChildClosed).toBe(true);
    await expect(access(root)).rejects.toThrow();
  }, 15000);
});
