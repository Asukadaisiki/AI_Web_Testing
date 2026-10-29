import { access, readFile, rm } from "node:fs/promises";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import {
  createAgentSession, type AgentSession, type McpTransportFactory,
} from "@earendil-works/pi-coding-agent";
import { createAssistantMessageEventStream, type AssistantMessage } from "@earendil-works/pi-ai";
import { StdioTransport } from "@earendil-works/pi-mcp";
import { expect, it, vi } from "vitest";
import { HarnessEventWriter, type RunRequest } from "../src/run-protocol.js";
import { SessionCleanupError } from "../src/server.js";
import { createExecutionSessionFactory } from "../src/session-factory.js";

const fixture = fileURLToPath(new URL("./fixtures/runtime-mcp-server.mjs", import.meta.url));
const request: RunRequest = {
  protocol_version: "pi-browser-harness.v1", run_id: "factory", attempt_id: "factory-attempt",
  goal: "complete the request",
  model: { provider: "scripted", name: "offline-fixture", reasoning_effort: "off" },
  execution: {
    runtime_id: "pi",
    harness: {
      id: "cleanup", name: "Cleanup", revision: 1, system_prompt: "Use selected tools",
      skills: [{ name: "selected", content: "Retain this skill until shutdown is acknowledged" }],
      mcp: { selected: { capability: "fixture", config: {}, secret_refs: {} } },
    },
  },
};
const mcpResolution = { capabilities: new Map([["fixture", {
  command: process.execPath, args: [fixture], config: {}, secrets: {},
}]]) };
const context = () => ({
  eventWriter: new HarnessEventWriter(request, { write: () => true }),
  signal: new AbortController().signal,
});

it.each(["direct close", "abort then close"] as const)(
  "retains factory resources until a real Pi stream joins on %s", async (mode) => {
    let cwd: string | undefined;
    let sdk: AgentSession | undefined;
    let streamSignal: AbortSignal | undefined;
    let release = () => {};
    let finishPrompt = () => {};
    const promptTail = new Promise<void>((resolve) => { finishPrompt = resolve; });
    let promptJoined = false;
    let closeReturned = false;
    const order: string[] = [];
    const factory = await createExecutionSessionFactory({
      mcpResolution,
      createAgentSession: async (options) => {
        cwd = options.cwd!;
        // Only the provider boundary is controlled; keep the actual SDK run/idle loop.
        options.modelRuntime!.streamSimple = (model, _context, streamOptions) => {
          streamSignal = streamOptions?.signal;
          const stream = createAssistantMessageEventStream();
          const message: AssistantMessage = {
            role: "assistant", content: [], api: model.api, provider: model.provider, model: model.id,
            usage: {
              input: 0, output: 0, cacheRead: 0, cacheWrite: 0, totalTokens: 0,
              cost: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0, total: 0 },
            },
            stopReason: "aborted", timestamp: Date.now(),
          };
          stream.push({ type: "start", partial: message });
          release = () => stream.end(message);
          return stream;
        };
        const result = await createAgentSession(options);
        sdk = result.session;
        const prompt = sdk.prompt.bind(sdk);
        vi.spyOn(sdk, "prompt").mockImplementation(async (text, options) => {
          await prompt(text, options);
          // Keep the prompt pending past actual SDK idle; discovery must remain available.
          if (text !== "/mcp") await promptTail;
        });
        sdk.subscribe((event) => {
          if (event.type === "agent_settled") order.push("settled");
        });
        const emit = sdk.extensionRunner.emit.bind(sdk.extensionRunner);
        vi.spyOn(sdk.extensionRunner, "emit").mockImplementation(async (event) => {
          if (event.type === "session_shutdown") order.push("shutdown");
          return emit(event);
        });
        return result;
      },
    });
    const session = await factory(request, context());
    const disposal = vi.spyOn(sdk!, "dispose");
    const prompt = session.prompt(request.goal).then(() => {
      promptJoined = true;
      order.push("prompt joined");
    });
    let aborting: Promise<void> | undefined;
    let closing: Promise<void> | undefined;
    try {
      await vi.waitFor(() => expect(streamSignal).toBeDefined());
      expect(sdk!.isIdle).toBe(false);
      if (mode === "abort then close") aborting = session.abort();
      closing = session.close();
      void closing.then(() => { closeReturned = true; }, () => { closeReturned = true; });
      expect(session.close()).toBe(closing);
      await vi.waitFor(() => expect(streamSignal!.aborted).toBe(true));
      // Flush cleanup's async steps while the real SDK stream deliberately stays busy.
      await new Promise((resolve) => setTimeout(resolve, 50));
      expect(await readFile(join(cwd!, "skills/selected/SKILL.md"), "utf8"))
        .toBe("Retain this skill until shutdown is acknowledged");
      expect(closeReturned).toBe(false);
      expect(promptJoined).toBe(false);
      expect(sdk!.isIdle).toBe(false);
      expect(order).toEqual([]);
      expect(disposal).not.toHaveBeenCalled();
      release();
      await vi.waitFor(() => expect(sdk!.isIdle).toBe(true));
      expect(promptJoined).toBe(false);
      expect(closeReturned).toBe(false);
      expect(order).toEqual(["settled"]);
      expect(disposal).not.toHaveBeenCalled();
      expect(await readFile(join(cwd!, "skills/selected/SKILL.md"), "utf8"))
        .toBe("Retain this skill until shutdown is acknowledged");
      finishPrompt();
      await closing;
      expect(promptJoined).toBe(true);
      expect(sdk!.isIdle).toBe(true);
      expect(order).toEqual(["settled", "prompt joined", "shutdown"]);
      expect(disposal).toHaveBeenCalledTimes(1);
      expect(session.close()).toBe(closing);
      await expect(access(cwd!)).rejects.toMatchObject({ code: "ENOENT" });
    } finally {
      release();
      finishPrompt();
      await prompt;
      await aborting;
      await (closing ?? session.close()).catch(() => {});
      if (cwd) await rm(cwd, { recursive: true, force: true });
    }
  },
);

it.each(["close", "initialization"] as const)(
  "preserves real attempt files after an unacknowledged transport %s", async (phase) => {
    let cwd: string | undefined;
    const createTransport: McpTransportFactory = (_entry, directory) => {
      const transport = new StdioTransport({
        command: process.execPath, args: [fixture], cwd: directory,
      });
      // An unsupported acknowledgement source must fail closed, not delete resources.
      transport.onClose = () => () => {};
      return transport;
    };
    const factory = await createExecutionSessionFactory({
      mcpResolution,
      createTransport, transportCloseTimeoutMs: 200,
      createAgentSession: async (options) => {
        cwd = options.cwd!;
        const result = await createAgentSession(options);
        if (phase === "initialization") {
          const bind = result.session.bindExtensions.bind(result.session);
          result.session.bindExtensions = async (options) => {
            await bind(options);
            await result.session.prompt("/mcp");
            throw new Error("initialization failed after MCP connected");
          };
        }
        return result;
      },
    });
    try {
      if (phase === "close") {
        const session = await factory(request, context());
        const closing = session.close();
        await expect(closing).rejects.toBeInstanceOf(SessionCleanupError);
        expect(session.close()).toBe(closing);
      } else {
        const error = await factory(request, context()).catch((error: unknown) => error);
        expect(error).toBeInstanceOf(SessionCleanupError);
        expect((error as Error).cause).toMatchObject({
          message: "initialization failed after MCP connected",
        });
      }
      expect(await readFile(join(cwd!, "skills/selected/SKILL.md"), "utf8"))
        .toBe("Retain this skill until shutdown is acknowledged");
    } finally {
      if (cwd) await rm(cwd, { recursive: true, force: true });
    }
  },
);

it.each(["close", "initialization"] as const)(
  "removes real attempt files after acknowledged transport %s", async (phase) => {
    let cwd: string | undefined;
    const factory = await createExecutionSessionFactory({
      mcpResolution,
      createAgentSession: async (options) => {
        cwd = options.cwd!;
        const result = await createAgentSession(options);
        if (phase === "initialization") {
          const bind = result.session.bindExtensions.bind(result.session);
          result.session.bindExtensions = async (options) => {
            await bind(options);
            await result.session.prompt("/mcp");
            throw new Error("initialization failed after MCP connected");
          };
        }
        return result;
      },
    });
    try {
      if (phase === "close") {
        const session = await factory(request, context());
        await session.close();
      } else {
        await expect(factory(request, context())).rejects.toThrow(
          "initialization failed after MCP connected",
        );
      }
      await expect(access(cwd!)).rejects.toMatchObject({ code: "ENOENT" });
    } finally {
      if (cwd) await rm(cwd, { recursive: true, force: true });
    }
  },
);
