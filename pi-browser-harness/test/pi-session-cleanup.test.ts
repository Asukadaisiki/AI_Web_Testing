import { execFileSync } from "node:child_process";
import { mkdtempSync, rmSync } from "node:fs";
import { createServer, type ServerResponse } from "node:http";
import { join } from "node:path";
import { Writable } from "node:stream";
import { fileURLToPath } from "node:url";
import { createAgentSession, ModelRuntime, type AgentSession } from "@earendil-works/pi-coding-agent";
import { createAssistantMessageEventStream, type AssistantMessage } from "@earendil-works/pi-ai";
import { expect, it, vi } from "vitest";
import { createBrowserSession, type BrowserSession } from "../src/pi-session.js";
import { HarnessEventWriter, type RunRequest } from "../src/run-protocol.js";

const root = fileURLToPath(new URL("..", import.meta.url));

it.each([200, 500])("Pi session close awaits the real MCP child's DELETE acknowledgement (HTTP %s)", async (status) => {
  const built = mkdtempSync(join(root, "test/.pi-cleanup-build-"));
  let deletion: ServerResponse | undefined;
  const requests: string[] = [];
  const worker = createServer((request, reply) => {
    requests.push(`${request.method} ${request.url}`);
    reply.setHeader("content-type", "application/json");
    if (request.method === "POST" && request.url === "/sessions") {
      reply.end(JSON.stringify({ session_id: "browser-1" }));
    } else if (request.url === "/sessions/browser-1/navigate") {
      reply.end(JSON.stringify({
        observation_id: "observation-1", page_state_id: "page-1", browser_session_id: "browser-1",
        url: "https://example.test", title: "Example", elements: [], structures: [],
        blockers: [], action_candidates: [], truncated: false, truncation_reason: null,
        screenshot_path: null,
      }));
    } else if (request.method === "DELETE" && request.url === "/sessions/browser-1") {
      deletion = reply;
    } else {
      reply.statusCode = 404;
      reply.end("{}");
    }
  });
  let session: BrowserSession | undefined;
  let sdk: AgentSession | undefined;
  let closing: Promise<unknown> | undefined;
  let prompt: Promise<void> | undefined;
  let streamSignal: AbortSignal | undefined;
  let release = () => {};
  try {
    execFileSync(process.execPath, [
      join(root, "node_modules/typescript/bin/tsc"), "--project", "tsconfig.build.json",
      "--outDir", built,
    ], { cwd: root });
    await new Promise<void>((resolve) => worker.listen(0, "127.0.0.1", resolve));
    const address = worker.address();
    if (!address || typeof address === "string") throw new Error("worker did not listen");
    const request: RunRequest = {
      protocol_version: "pi-browser-harness.v1", run_id: "cleanup", attempt_id: "cleanup-attempt",
      goal: "open example", model: { provider: "scripted", name: "fixture", reasoning_effort: "off" },
      worker_url: `http://127.0.0.1:${address.port}`, artifact_scope: "cleanup",
    };
    session = await createBrowserSession(request, {
      cwd: built, mcpChildPath: join(built, "mcp/server.js"),
      modelRuntime: await ModelRuntime.create({ refreshOnCreate: false }),
      eventWriter: new HarnessEventWriter(request, new Writable({
        write(_chunk, _encoding, done) { done(); },
      })),
      createAgentSession: async (options) => {
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
        return result;
      },
    });
    await sdk!.prompt("/mcp");
    const result = await sdk!.getToolDefinition("mcp__browser_harness__open_page")!.execute(
      "fixture-open", { url: "https://example.test", intent: "open example" },
      new AbortController().signal, undefined, {} as never,
    );
    expect(result.isError).not.toBe(true);
    expect(requests).toContain("POST /sessions");
    const teardown = vi.spyOn(sdk!.extensionRunner, "emit");
    const disposal = vi.spyOn(sdk!, "dispose");
    const abort = vi.spyOn(sdk!, "abort");
    let promptJoined = false;
    prompt = session.prompt(request.goal).then(() => { promptJoined = true; });
    await vi.waitFor(() => expect(streamSignal).toBeDefined());
    let returned = false;
    const firstClose = session.close();
    expect(session.close()).toBe(firstClose);
    closing = firstClose.then(
      () => { returned = true; return "released"; },
      (error: unknown) => { returned = true; return error; },
    );
    await vi.waitFor(() => expect(streamSignal!.aborted).toBe(true));
    await new Promise((resolve) => setTimeout(resolve, 50));
    expect(abort).toHaveBeenCalledTimes(1);
    expect(sdk!.isIdle).toBe(false);
    expect(promptJoined).toBe(false);
    expect(deletion).toBeUndefined();
    expect(teardown).not.toHaveBeenCalledWith({ type: "session_shutdown", reason: "quit" });
    expect(disposal).not.toHaveBeenCalled();
    release();
    await vi.waitFor(() => expect(deletion).toBeDefined());
    expect(sdk!.isIdle).toBe(true);
    expect(promptJoined).toBe(true);
    expect(returned).toBe(false);
    expect(teardown).not.toHaveBeenCalledWith({ type: "session_shutdown", reason: "quit" });
    expect(disposal).not.toHaveBeenCalled();
    deletion!.statusCode = status;
    deletion!.end(JSON.stringify(status === 200
      ? { closed: true } : { error: "worker_error", detail: "cleanup uncertain" }));
    const outcome = await closing;
    if (status === 500) {
      expect(outcome).toBeInstanceOf(Error);
      expect((outcome as Error).message).toContain("cleanup uncertain");
      await expect(session.close()).rejects.toThrow("cleanup uncertain");
    } else {
      expect(outcome).toBe("released");
      await expect(session.close()).resolves.toBeUndefined();
    }
    expect(teardown).toHaveBeenCalledWith({ type: "session_shutdown", reason: "quit" });
    expect(disposal).toHaveBeenCalledTimes(1);
    expect(requests.filter((entry) => entry === "DELETE /sessions/browser-1")).toHaveLength(1);
  } finally {
    release();
    await prompt;
    if (deletion && !deletion.writableEnded) deletion.end(JSON.stringify({ closed: true }));
    await (closing ?? session?.close().catch(() => {}));
    await new Promise<void>((resolve) => worker.close(() => resolve()));
    rmSync(built, { recursive: true, force: true });
  }
}, 60_000);
