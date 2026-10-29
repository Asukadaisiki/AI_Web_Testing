import { genericExecution } from "./fixtures/selected-harness.js";
import { spawn, type ChildProcessWithoutNullStreams } from "node:child_process";
import { EventEmitter } from "node:events";
import { access, mkdtemp, readFile, rm } from "node:fs/promises";
import { createServer } from "node:http";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { PassThrough, Writable } from "node:stream";
import { fileURLToPath } from "node:url";
import type {
  SDKAssistantMessage, SDKMessage, SDKResultMessage, SDKResultSuccess, SpawnOptions,
} from "@anthropic-ai/claude-agent-sdk";
import { afterEach, describe, expect, it, vi } from "vitest";
import { createClaudeSession, type ClaudeSessionDeps } from "../src/claude-session.js";
import type { CompletionContext, CompletionResult, ObservedEvent } from "../src/harness-definition.js";
import { HarnessEventWriter, type HarnessEvent, type RunRequest } from "../src/run-protocol.js";
import { serverMain, SessionCleanupError } from "../src/server.js";

afterEach(() => vi.unstubAllEnvs());

const request: RunRequest = {
  execution: genericExecution,
  protocol_version: "pi-browser-harness.v1",
  run_id: "claude-adapter-run", attempt_id: "claude-adapter-attempt", goal: "Verify the selected case",
  model: { provider: "anthropic-compatible", name: "claude-selected", reasoning_effort: "high" },
};

const firstUuid = "00000000-0000-4000-8000-000000000001";
const secondUuid = "00000000-0000-4000-8000-000000000002";

function assistant(text: string, uuid: SDKAssistantMessage["uuid"], extra: Partial<SDKAssistantMessage> = {}): SDKAssistantMessage {
  return {
    type: "assistant", uuid, session_id: "sdk-session", parent_tool_use_id: null,
    message: {
      id: "msg-shared", type: "message", role: "assistant", model: "claude-actual",
      content: [{ type: "text", text, citations: null }], stop_reason: "end_turn", stop_sequence: null,
      usage: {
        input_tokens: 10, output_tokens: 4, cache_read_input_tokens: 2,
        cache_creation_input_tokens: 3,
      },
    } as SDKAssistantMessage["message"],
    ...extra,
  };
}

function result(extra: Partial<SDKResultSuccess> = {}): SDKResultSuccess {
  return {
    type: "result", subtype: "success", is_error: false, uuid: "00000000-0000-4000-8000-000000000003", session_id: "sdk-session",
    result: "SDK final answer", duration_ms: 75, duration_api_ms: 40, num_turns: 2,
    stop_reason: "end_turn", total_cost_usd: 0.012, permission_denials: [],
    usage: {
      input_tokens: 20, output_tokens: 8, cache_read_input_tokens: 4, cache_creation_input_tokens: 6,
    } as SDKResultSuccess["usage"],
    modelUsage: {
      "claude-actual": {
        inputTokens: 20, outputTokens: 8, cacheReadInputTokens: 4, cacheCreationInputTokens: 6,
        webSearchRequests: 0, costUSD: 0.012, contextWindow: 200000, maxOutputTokens: 4096,
        provider: "firstParty",
      },
    },
    ...extra,
  };
}

function deferred() {
  let resolve!: () => void;
  const promise = new Promise<void>((done) => { resolve = done; });
  return { promise, resolve };
}

function setup(messages: SDKMessage[] = [assistant("SDK text", firstUuid), result()]) {
  const written: HarnessEvent[] = [];
  const lifecycle: string[] = [];
  const evidence: ObservedEvent[] = [];
  const completion = vi.fn((_context: CompletionContext): CompletionResult => ({ verdict: "succeeded" }));
  const resources = {
    cwd: fileURLToPath(new URL("./fixtures", import.meta.url)), systemPrompt: "Selected snapshot prompt",
    skillPaths: [fileURLToPath(new URL("./fixtures/claude-adapter-skills", import.meta.url))],
    servers: [],
    definition: {
      systemPrompt: "Selected snapshot prompt", skillPaths: [], toolAllowlist: ["mcp__selected__*"],
      mcpServers: [], completionPolicy: completion,
    },
    dispose: vi.fn(async () => { lifecycle.push("resources.dispose"); }),
  };
  const mcp = {
    mcpServers: { selected: { type: "http" as const, url: "http://127.0.0.1:43210/mcp" } },
    events: evidence,
    close: vi.fn(async () => { lifecycle.push("mcp.close"); }),
  };
  const stream = Object.assign((async function* () {
    try { yield* messages; } finally { lifecycle.push("sdk.joined"); }
  })(), {
    close: vi.fn(() => { lifecycle.push("sdk.close"); }),
  });
  const query = vi.fn<NonNullable<ClaudeSessionDeps["query"]>>(() => stream);
  const eventWriter = new HarnessEventWriter(request, new Writable({
    write(chunk, _encoding, done) { written.push(JSON.parse(String(chunk)) as HarnessEvent); done(); },
  }));
  const deps: ClaudeSessionDeps = {
    eventWriter, query,
    prepareHarnessResources: vi.fn(async () => resources),
    openRuntimeMcp: vi.fn(async () => mcp),
  };
  return { deps, resources, mcp, written, lifecycle, completion, query, stream };
}

describe("official Claude SDK adapter", () => {
  it("defers password-bearing SDK model messages while keeping native tool progress live", async () => {
    const canary = "claude-model-password-canary";
    const fixture = setup([]);
    fixture.resources.servers.push({ name: "browser-harness", command: process.execPath } as never);
    let privateGoal: unknown;
    let classify: () => void = () => {};
    fixture.deps.openRuntimeMcp = async (_resources, writer, guard) => {
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
      return fixture.mcp;
    };
    fixture.deps.query = (parameters) => {
      privateGoal = parameters.prompt;
      return Object.assign((async function* () {
        yield assistant(`I will enter ${canary}`, firstUuid);
        expect(JSON.stringify(fixture.written)).not.toContain(canary);
        classify();
        expect(fixture.written.map((event) => event.type)).toEqual(["pi.tool.started", "pi.tool.completed"]);
        yield assistant(`Entered ${canary}`, secondUuid);
        yield result({ result: `Verified ${canary}` });
      })(), { close() {} });
    };
    const session = await createClaudeSession(request, fixture.deps);
    try {
      await session.prompt(`Goal ${canary}`);
      expect(privateGoal).toBe(`Goal ${canary}`);
      expect(JSON.stringify(fixture.written)).not.toContain(canary);
      expect(fixture.written.filter((event) => event.type === "pi.message.completed").map((event) => event.payload.text))
        .toEqual(["I will enter [redacted]", "Entered [redacted]"]);
      expect(fixture.written.find((event) => event.type === "harness.settled")?.payload.assistant_text)
        .toBe("Verified [redacted]");
    } finally { await session.close(); }
  });

  it("selects only the snapshot prompt, full skills and MCP with isolated SDK settings and credentials", async () => {
    vi.stubEnv("AGENTTRACE_CLAUDE_API_KEY", "fixture-private-key");
    vi.stubEnv("AGENTTRACE_CLAUDE_BASE_URL", "http://127.0.0.1:43211");
    vi.stubEnv("ANTHROPIC_API_KEY", "inherited-wrong-key");
    vi.stubEnv("ANTHROPIC_AUTH_TOKEN", "inherited-oauth-token");
    vi.stubEnv("CLAUDE_CODE_USE_BEDROCK", "1");
    const fixture = setup();
    const session = await createClaudeSession(request, fixture.deps);
    await session.prompt(request.goal);
    const input = fixture.query.mock.calls[0]![0];
    expect(input.prompt).toBe(request.goal);
    expect(input.options).toMatchObject({
      cwd: fixture.resources.cwd, model: "claude-selected", tools: [], skills: [],
      settingSources: [], strictMcpConfig: true, persistSession: false,
      mcpServers: { selected: { type: "http", url: "http://127.0.0.1:43210/mcp" } },
      env: { ANTHROPIC_API_KEY: "fixture-private-key", ANTHROPIC_BASE_URL: "http://127.0.0.1:43211" },
    });
    expect(input.options?.abortController).toBeInstanceOf(AbortController);
    expect(input.options?.systemPrompt).toContain("Selected snapshot prompt");
    expect(input.options?.systemPrompt).toContain("Never claim success without its evidence");
    expect(input.options?.systemPrompt).toContain("not assistant prose, determines completion");
    expect(input.options?.env?.["ANTHROPIC_AUTH_TOKEN"]).toBeUndefined();
    expect(input.options?.env?.["CLAUDE_CODE_USE_BEDROCK"]).toBeUndefined();
    expect(input.options?.hooks).toBeUndefined();
    expect(JSON.stringify(fixture.written)).not.toContain("fixture-private-key");
    await session.close();
  });

  it("preserves SDK block granularity but accounts per-model result usage once without invented call input or timing", async () => {
    const fixture = setup([assistant("First", firstUuid), assistant("Second", secondUuid), result()]);
    const session = await createClaudeSession(request, fixture.deps);
    await session.prompt(request.goal);
    const messages = fixture.written.filter((event) => event.type === "pi.message.completed");
    expect(messages.map((event) => event.payload.text)).toEqual(["First", "Second"]);
    expect(messages[0]?.payload).toMatchObject({
      model: "claude-actual", sdk_message_id: "msg-shared", sdk_message_uuid: firstUuid,
      granularity: "sdk_assistant_message",
    });
    for (const event of messages) {
      expect(event.payload.input).toBeUndefined();
      expect(event.payload.started_at).toBeUndefined();
      expect(event.payload.ended_at).toBeUndefined();
    }
    const usages = fixture.written.filter((event) => event.type === "pi.usage");
    expect(usages).toHaveLength(1);
    expect(usages[0]?.payload).toMatchObject({
      provider: "firstParty", model: "claude-actual", input_tokens: 20, output_tokens: 8,
      cache_read_tokens: 4, cache_write_tokens: 6, granularity: "sdk_query_model_cumulative",
      sdk_duration_ms: 75, sdk_duration_api_ms: 40, timing_scope: "sdk_turn",
    });
    expect(fixture.written.at(-1)?.payload).toMatchObject({ assistant_text: "SDK final answer", verdict: "succeeded" });
    await session.close();
  });

  it("uses actual helper evidence for settlement and never remaps SDK tool events", async () => {
    const toolMessage = assistant("", firstUuid);
    toolMessage.message.content = [{
      type: "tool_use", id: "sdk-tool-id", name: "mcp__selected__verify", input: { case: "selected" },
    } as SDKAssistantMessage["message"]["content"][number]];
    const fixture = setup([toolMessage, result()]);
    fixture.mcp.events.push({
      type: "pi.tool.completed", payload: { tool_call_id: "native-tool-id", tool_name: "mcp__selected__verify", is_error: false },
    });
    fixture.completion.mockImplementation((context) => ({
      verdict: context.events.length ? "succeeded" : "failed",
    }));
    const session = await createClaudeSession(request, fixture.deps);
    await session.prompt(request.goal);
    expect(fixture.completion.mock.calls[0]?.[0]).toEqual({
      assistant_settled: true, aborted: false, assistant_text: "SDK final answer", events: fixture.mcp.events,
    });
    expect(fixture.written.filter((event) => event.type.startsWith("pi.tool."))).toEqual([]);
    await session.close();
  });

  it.each([
    [assistant("Error-looking assistant prose", firstUuid, { error: "authentication_failed" })],
    [result({ is_error: true, result: "API failure in success-shaped result" })],
    [{ ...result(), subtype: "error_max_turns", errors: ["SDK turn limit"] } as SDKResultMessage],
  ])("fails SDK errors instead of passing their prose to completion", async (failure) => {
    const fixture = setup([failure]);
    const session = await createClaudeSession(request, fixture.deps);
    await expect(session.prompt(request.goal)).rejects.toThrow();
    expect(fixture.completion).not.toHaveBeenCalled();
    expect(fixture.written.filter((event) => event.type === "harness.settled")).toEqual([]);
    expect(fixture.written.at(-1)?.type).toBe("harness.failed");
    if (failure.type === "assistant") {
      expect(fixture.written.filter((event) => event.type === "pi.message.completed")).toEqual([]);
    }
    await session.close();
  });

  it("does not settle a stream that throws after a success result, or ends without a result", async () => {
    for (const afterResult of [false, true]) {
      const fixture = setup([]);
      fixture.query.mockImplementation(() => Object.assign((async function* () {
        if (afterResult) yield result();
        throw new Error("SDK transport failed");
      })(), { close() {} }));
      const session = await createClaudeSession(request, fixture.deps);
      await expect(session.prompt(request.goal)).rejects.toThrow("SDK transport failed");
      expect(fixture.completion).not.toHaveBeenCalled();
      await session.close();
    }
    const fixture = setup([]);
    const session = await createClaudeSession(request, fixture.deps);
    await expect(session.prompt(request.goal)).rejects.toThrow(/result/i);
    await session.close();
  });

  it("cancels the SDK and joins in-flight work before idempotent MCP/resource cleanup", async () => {
    const fixture = setup([]);
    const controller = new AbortController();
    const joined = deferred();
    let sdkClosed = false;
    fixture.query.mockImplementation(() => Object.assign((async function* () {
      yield assistant("partial", firstUuid);
      await joined.promise;
      fixture.lifecycle.push("sdk.joined");
      yield result();
    })(), {
      close() { sdkClosed = true; fixture.lifecycle.push("sdk.close"); },
    }));
    const session = await createClaudeSession(request, { ...fixture.deps, signal: controller.signal });
    const running = session.prompt(request.goal);
    const outcome = running.catch((error: unknown) => error);
    await vi.waitFor(() => expect(fixture.query).toHaveBeenCalledTimes(1));
    controller.abort();
    const closing = session.close();
    expect(session.close()).toBe(closing);
    await vi.waitFor(() => expect(sdkClosed).toBe(true));
    expect(fixture.query.mock.calls[0]![0].options?.abortController?.signal.aborted).toBe(true);
    expect(fixture.lifecycle).not.toContain("mcp.close");
    expect(fixture.lifecycle).not.toContain("resources.dispose");
    joined.resolve();
    await closing;
    expect(await outcome).toBeInstanceOf(Error);
    expect(fixture.lifecycle).toEqual(["sdk.close", "sdk.joined", "mcp.close", "resources.dispose"]);
    expect(fixture.completion).not.toHaveBeenCalled();
  });

  it("retains private resources when helper shutdown is not acknowledged", async () => {
    const fixture = setup();
    const cwd = await mkdtemp(join(tmpdir(), "claude-mcp-unjoined-"));
    fixture.resources.cwd = cwd;
    fixture.resources.dispose.mockImplementation(async () => {
      fixture.lifecycle.push("resources.dispose");
      await rm(cwd, { recursive: true, force: true });
    });
    const cleanup = deferred();
    fixture.mcp.close.mockImplementation(async () => {
      fixture.lifecycle.push("mcp.close");
      await cleanup.promise;
      throw new Error("MCP release uncertain");
    });
    const session = await createClaudeSession(request, fixture.deps);
    await session.prompt(request.goal);
    const closing = session.close();
    const outcome = closing.catch((error: unknown) => error);
    await vi.waitFor(() => expect(fixture.lifecycle).toContain("mcp.close"));
    expect(fixture.lifecycle).not.toContain("resources.dispose");
    cleanup.resolve();
    try {
      expect(await outcome).toBeInstanceOf(SessionCleanupError);
      await expect(session.close()).rejects.toThrow("MCP release uncertain");
      expect(fixture.lifecycle.at(-1)).toBe("mcp.close");
      expect(fixture.mcp.close).toHaveBeenCalledTimes(1);
      expect(fixture.resources.dispose).not.toHaveBeenCalled();
      await expect(access(cwd)).resolves.toBeUndefined();
    } finally {
      await rm(cwd, { recursive: true, force: true });
    }
  });

  it("closes the query when cancellation fires synchronously during SDK construction", async () => {
    const fixture = setup([]);
    const controller = new AbortController();
    const gate = deferred();
    const sdkClose = vi.fn();
    fixture.query.mockImplementation(() => {
      controller.abort();
      return Object.assign((async function* () {
        await gate.promise;
        yield result();
      })(), { close: sdkClose });
    });
    const session = await createClaudeSession(request, { ...fixture.deps, signal: controller.signal });
    const running = session.prompt(request.goal).catch((error: unknown) => error);
    try {
      await vi.waitFor(() => expect(sdkClose).toHaveBeenCalledTimes(1));
    } finally {
      gate.resolve();
      await running;
      await session.close();
    }
    expect(fixture.completion).not.toHaveBeenCalled();
  });

  it("cleans materialized resources on MCP initialization failure", async () => {
    const fixture = setup();
    fixture.deps.openRuntimeMcp = async () => { throw new Error("MCP startup failed"); };
    await expect(createClaudeSession(request, fixture.deps)).rejects.toThrow("MCP startup failed");
    expect(fixture.lifecycle).toEqual(["resources.dispose"]);
  });

  it("retains resource directories when MCP initialization reports uncertain shutdown", async () => {
    const fixture = setup();
    fixture.deps.openRuntimeMcp = async () => { throw new SessionCleanupError("MCP startup shutdown uncertain"); };
    await expect(createClaudeSession(request, fixture.deps)).rejects.toBeInstanceOf(SessionCleanupError);
    expect(fixture.resources.dispose).not.toHaveBeenCalled();
  });

  it("retains MCP and resource ownership without harness.released when child close is unacknowledged", async () => {
    const fixture = setup();
    const spawned = deferred();
    const cwd = await mkdtemp(join(tmpdir(), "claude-unjoined-"));
    fixture.resources.cwd = cwd;
    fixture.resources.dispose.mockImplementation(async () => { await rm(cwd, { recursive: true, force: true }); });
    // Even exitCode=0 and an exit event cannot acknowledge the missing close event.
    const child = Object.assign(new EventEmitter(), {
      stdin: new PassThrough(), stdout: new PassThrough(), stderr: new PassThrough(),
      exitCode: 0, signalCode: null, killed: true, kill: () => true,
    }) as unknown as ChildProcessWithoutNullStreams;
    fixture.query.mockImplementation((parameters) => {
      parameters.options?.spawnClaudeCodeProcess?.({
        command: process.execPath, args: [], cwd,
        env: parameters.options.env ?? {}, signal: new AbortController().signal,
      });
      spawned.resolve();
      child.emit("exit", 0, null);
      child.emit("error", new Error("post-exit process error"));
      child.stdin.emit("error", new Error("stdin closed during shutdown"));
      child.stderr.emit("error", new Error("stderr closed during shutdown"));
      return fixture.stream;
    });
    const input = new PassThrough();
    const controls: Record<string, unknown>[] = [];
    let session: Awaited<ReturnType<typeof createClaudeSession>> | undefined;
    vi.useFakeTimers();
    try {
      const serving = serverMain({
        input, process: new EventEmitter(),
        output: { write(line) { controls.push(JSON.parse(String(line))); return true; } },
        sessionFactory: async (_request, context) => {
          session = await createClaudeSession(request, {
            ...fixture.deps, ...context, spawnClaudeCodeProcess: () => child,
          });
          return session;
        },
      });
      input.write(`${JSON.stringify({ type: "run", request })}\n`);
      await spawned.promise;
      await vi.advanceTimersByTimeAsync(0);
      await vi.advanceTimersByTimeAsync(15_001);
      input.end();
      const code = await serving;
      expect(code).toBe(1);
      await expect(session!.close()).rejects.toBeInstanceOf(SessionCleanupError);
      expect(controls.some((event) => event.type === "harness.settled")).toBe(true);
      expect(controls.some((event) => event.type === "harness.protocol_error")).toBe(true);
      expect(controls.some((event) => event.type === "harness.released")).toBe(false);
      expect(fixture.mcp.close).not.toHaveBeenCalled();
      expect(fixture.resources.dispose).not.toHaveBeenCalled();
      await expect(access(cwd)).resolves.toBeUndefined();
    } finally {
      vi.useRealTimers();
      child.emit("close", 0, null);
      await rm(cwd, { recursive: true, force: true });
    }
  });

  it.each(["throw", "spawn-error", "needs-signal", "needs-kill"] as const)("cleans MCP/resources after an acknowledged %s during SDK initialization", async (mode) => {
    const fixture = setup();
    let child: ChildProcessWithoutNullStreams | undefined;
    let closed = false;
    fixture.query.mockImplementation((parameters) => {
      parameters.options?.spawnClaudeCodeProcess?.({
        command: mode === "spawn-error" ? "/missing/claude-fixture-binary" : process.execPath,
        args: ["-e", mode === "needs-signal" || mode === "needs-kill"
          ? `process.on('SIGTERM', () => ${mode === "needs-kill" ? "{}" : "process.exit(0)"}); setInterval(() => {}, 1000); process.stdin.resume()`
          : "process.stdin.resume()"], cwd: fixture.resources.cwd,
        env: parameters.options.env ?? {}, signal: new AbortController().signal,
      });
      throw new Error("SDK initialization failed after spawn");
    });
    const session = await createClaudeSession(request, {
      ...fixture.deps,
      spawnClaudeCodeProcess(options) {
        child = spawn(options.command, options.args, {
          cwd: options.cwd, env: options.env, stdio: "pipe",
        });
        child.once("close", () => { closed = true; });
        return child;
      },
    });
    fixture.mcp.close.mockImplementation(async () => {
      expect(closed).toBe(true);
      fixture.lifecycle.push("mcp.close");
    });
    try {
      await expect(session.prompt(request.goal)).rejects.toThrow("SDK initialization failed after spawn");
      await session.close();
      expect(closed).toBe(true);
      expect(fixture.lifecycle).toEqual(["mcp.close", "resources.dispose"]);
    } finally {
      child?.kill("SIGKILL");
    }
  }, 12_000);

  it("joins an asynchronous spawn failure from the actual SDK before releasing MCP/resources", async () => {
    const fixture = setup([]);
    const { query: _injectedQuery, ...deps } = fixture.deps;
    let closed = false;
    const session = await createClaudeSession(request, {
      ...deps,
      spawnClaudeCodeProcess(options) {
        const child = spawn("/missing/claude-fixture-binary", options.args, {
          cwd: options.cwd, env: options.env, stdio: "pipe",
        });
        child.once("close", () => { closed = true; });
        return child;
      },
    });
    fixture.mcp.close.mockImplementation(async () => {
      expect(closed).toBe(true);
      fixture.lifecycle.push("mcp.close");
    });
    try {
      await expect(session.prompt(request.goal)).rejects.toThrow();
      await session.close();
      expect(fixture.lifecycle).toEqual(["mcp.close", "resources.dispose"]);
      expect(fixture.written.some((event) => event.type === "harness.settled")).toBe(false);
    } finally {
      await session.close().catch(() => {});
    }
  });

  it("cleans resources when the supported spawn hook throws before creating a child", async () => {
    const fixture = setup([]);
    const { query: _injectedQuery, ...deps } = fixture.deps;
    const session = await createClaudeSession(request, {
      ...deps, spawnClaudeCodeProcess() { throw new Error("spawn hook failed"); },
    });
    try {
      await expect(session.prompt(request.goal)).rejects.toThrow("spawn hook failed");
      await session.close();
      expect(fixture.lifecycle).toEqual(["mcp.close", "resources.dispose"]);
    } finally {
      await session.close().catch(() => {});
    }
  });

  it("joins a real child when cancellation fires synchronously inside the official SDK spawn hook", async () => {
    const fixture = setup([]);
    const { query: _injectedQuery, ...deps } = fixture.deps;
    const controller = new AbortController();
    let closed = false;
    const session = await createClaudeSession(request, {
      ...deps, signal: controller.signal,
      spawnClaudeCodeProcess(options) {
        const child = spawn(process.execPath, ["-e", "process.stdin.resume()"], {
          cwd: options.cwd, env: options.env, stdio: "pipe",
        });
        child.once("close", () => { closed = true; });
        controller.abort();
        return child;
      },
    });
    fixture.mcp.close.mockImplementation(async () => {
      expect(closed).toBe(true);
      fixture.lifecycle.push("mcp.close");
    });
    try {
      await expect(session.prompt(request.goal)).rejects.toThrow();
      await Promise.all([session.abort(), session.close()]);
      expect(fixture.lifecycle).toEqual(["mcp.close", "resources.dispose"]);
      expect(fixture.written.some((event) => event.type === "harness.settled")).toBe(false);
    } finally {
      await session.close().catch(() => {});
    }
  });

  it("joins the real official SDK child after stdout EOF and delayed SIGTERM exit before releasing MCP/resources", async () => {
    const fixture = setup([]);
    const cwd = await mkdtemp(join(tmpdir(), "claude-delayed-exit-"));
    fixture.resources.cwd = cwd;
    fixture.mcp.mcpServers = {} as typeof fixture.mcp.mcpServers;
    const controller = new AbortController();
    let child: ChildProcessWithoutNullStreams | undefined;
    let childClosed = false;
    let spawnOptions: SpawnOptions | undefined;
    fixture.mcp.close.mockImplementation(async () => {
      expect(childClosed).toBe(true);
      expect(await readFile(join(cwd, "child-terminated"), "utf8")).toBe("terminated");
      fixture.lifecycle.push("mcp.close");
    });
    fixture.resources.dispose.mockImplementation(async () => {
      expect(childClosed).toBe(true);
      fixture.lifecycle.push("resources.dispose");
      await rm(cwd, { recursive: true, force: true });
    });
    const { query: _injectedQuery, ...deps } = fixture.deps;
    const session = await createClaudeSession(request, {
      ...deps, signal: controller.signal,
      spawnClaudeCodeProcess(options) {
        spawnOptions = options;
        child = spawn(process.execPath, [
          fileURLToPath(new URL("./fixtures/claude-delayed-exit.mjs", import.meta.url)),
        ], { cwd: options.cwd, env: options.env, signal: options.signal, stdio: "pipe" });
        child.once("close", () => { childClosed = true; });
        return child;
      },
    });
    const running = session.prompt(request.goal);
    const outcome = running.catch((error: unknown) => error);
    try {
      await vi.waitFor(async () => expect(await readFile(join(cwd, "child-ready"), "utf8")).toBe("ready"), {
        timeout: 15_000,
      });
      expect(spawnOptions?.command).toBeTruthy();
      expect(spawnOptions?.args).toContain("--output-format");
      controller.abort();
      const closing = session.close();
      expect(session.close()).toBe(closing);
      expect(await outcome).toBeInstanceOf(Error); // The SDK join finished while the child is still alive.
      expect(childClosed).toBe(false);
      expect(fixture.lifecycle).not.toContain("mcp.close");
      await expect(access(cwd)).resolves.toBeUndefined();
      await closing;
      expect(childClosed).toBe(true);
      expect(fixture.lifecycle).toEqual(["mcp.close", "resources.dispose"]);
      await expect(access(cwd)).rejects.toThrow();
    } finally {
      controller.abort();
      child?.kill("SIGKILL");
      await session.close().catch(() => {});
      await outcome;
      await rm(cwd, { recursive: true, force: true });
    }
  }, 25_000);

  it("runs the bundled official SDK against a controlled local Messages provider with a /v1 configuration", async () => {
    const cwd = await mkdtemp(join(tmpdir(), "claude-adapter-offline-"));
    const paths: string[] = [];
    const bodies: Record<string, unknown>[] = [];
    const provider = createServer(async (incoming, response) => {
      paths.push(incoming.url ?? "");
      let body = "";
      for await (const chunk of incoming) body += String(chunk);
      if (incoming.url?.startsWith("/v1/messages/count_tokens")) {
        response.setHeader("content-type", "application/json");
        response.end(JSON.stringify({ input_tokens: 12 }));
        return;
      }
      if (!incoming.url?.startsWith("/v1/messages")) {
        response.writeHead(404, { "content-type": "application/json" });
        response.end(JSON.stringify({ error: { type: "not_found_error", message: "Wrong Messages path" } }));
        return;
      }
      const input = JSON.parse(body) as Record<string, unknown>;
      bodies.push(input);
      const message = {
        id: "msg_controlled_official", type: "message", role: "assistant", model: "claude-sonnet-4-6",
        content: [{ type: "text", text: "Official SDK controlled reply." }],
        stop_reason: "end_turn", stop_sequence: null,
        usage: { input_tokens: 12, output_tokens: 7, cache_read_input_tokens: 0, cache_creation_input_tokens: 0 },
      };
      if (!input.stream) {
        response.setHeader("content-type", "application/json");
        response.end(JSON.stringify(message));
        return;
      }
      response.writeHead(200, { "content-type": "text/event-stream" });
      const frames = [
        { type: "message_start", message: { ...message, content: [], stop_reason: null, usage: { ...message.usage, output_tokens: 0 } } },
        { type: "content_block_start", index: 0, content_block: { type: "text", text: "" } },
        { type: "content_block_delta", index: 0, delta: { type: "text_delta", text: "Official SDK controlled reply." } },
        { type: "content_block_stop", index: 0 },
        { type: "message_delta", delta: { stop_reason: "end_turn", stop_sequence: null }, usage: { output_tokens: 7 } },
        { type: "message_stop" },
      ];
      for (const frame of frames) response.write(`event: ${frame.type}\ndata: ${JSON.stringify(frame)}\n\n`);
      response.end();
    });
    const fixture = setup([]);
    const controller = new AbortController();
    const timeout = setTimeout(() => controller.abort(), 45_000);
    let session: Awaited<ReturnType<typeof createClaudeSession>> | undefined;
    try {
      await new Promise<void>((resolve) => provider.listen(0, "127.0.0.1", resolve));
      const address = provider.address();
      if (!address || typeof address === "string") throw new Error("Offline provider did not listen");
      vi.stubEnv("AGENTTRACE_CLAUDE_API_KEY", "offline-fixture-not-a-real-key");
      vi.stubEnv("AGENTTRACE_CLAUDE_BASE_URL", `http://127.0.0.1:${address.port}/v1`);
      fixture.resources.cwd = cwd;
      fixture.mcp.mcpServers = {} as typeof fixture.mcp.mcpServers;
      fixture.resources.dispose.mockImplementation(async () => { await rm(cwd, { recursive: true, force: true }); });
      const { query: _injectedQuery, ...deps } = fixture.deps;
      session = await createClaudeSession({
        ...request, model: { ...request.model, name: "claude-sonnet-4-6" },
      }, { ...deps, signal: controller.signal });
      await session.prompt(request.goal);
      expect(paths.some((path) => path.startsWith("/v1/messages"))).toBe(true);
      expect(paths.some((path) => path.includes("/v1/v1"))).toBe(false);
      expect(bodies.some((body) => JSON.stringify(body.system).includes("Never claim success without its evidence"))).toBe(true);
      expect(bodies.every((body) => !Array.isArray(body.tools) || body.tools.length === 0)).toBe(true);
      expect(fixture.written.some((event) => event.type === "pi.message.completed" &&
        event.payload.text === "Official SDK controlled reply.")).toBe(true);
      expect(fixture.written.some((event) => event.type === "pi.usage" &&
        event.payload.model === "claude-sonnet-4-6" && event.payload.output_tokens === 7)).toBe(true);
      expect(fixture.written.at(-1)?.payload).toMatchObject({
        verdict: "succeeded", assistant_text: "Official SDK controlled reply.",
      });
    } finally {
      clearTimeout(timeout);
      controller.abort();
      await session?.close();
      provider.closeAllConnections();
      await new Promise<void>((resolve) => provider.close(() => resolve()));
      await rm(cwd, { recursive: true, force: true });
    }
  }, 60_000);
});
