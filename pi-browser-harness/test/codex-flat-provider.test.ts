import { access, readFile } from "node:fs/promises";
import { createServer, type IncomingMessage, type ServerResponse } from "node:http";
import { join } from "node:path";
import { setTimeout as delay } from "node:timers/promises";
import { fileURLToPath } from "node:url";
import { afterEach, describe, expect, it } from "vitest";
import { createCodexSession } from "../src/codex-session.js";
import { prepareHarnessResources as prepareResources } from "../src/harness-resources.js";
import { openRuntimeMcp } from "../src/runtime-mcp.js";
import { HarnessEventWriter, RUN_PROTOCOL_VERSION, type HarnessEvent, type RunRequest } from "../src/run-protocol.js";

const fixture = fileURLToPath(new URL("./fixtures/codex-session-mcp.mjs", import.meta.url));
const prepareHarnessResources = (req: RunRequest) => prepareResources(req, {
  capabilities: new Map([["fixture", { command: process.execPath, args: [fixture], config: {}, secrets: {} }]]),
});
const request: RunRequest = {
  protocol_version: RUN_PROTOCOL_VERSION, run_id: "flat-run", attempt_id: "flat-attempt",
  goal: "Call emit_receipt with label streamed.", model: {
    provider: "openai-responses", name: "flat-only-model", reasoning_effort: "off",
  },
  execution: { runtime_id: "codex", harness: {
    id: "generic", name: "Generic", revision: 1, system_prompt: "Use only selected tools.",
    skills: [], mcp: { selected: { capability: "fixture", config: {}, secret_refs: {} } },
  } },
};
const cleanup: (() => Promise<void>)[] = [];
afterEach(async () => {
  for (const close of cleanup.splice(0).reverse()) await close();
});
async function listen(handler: (req: IncomingMessage, res: ServerResponse) => Promise<void>) {
  const server = createServer((req, res) => {
    void handler(req, res).catch(() => { if (!res.headersSent) res.writeHead(500); res.end(); });
  });
  await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
  cleanup.push(() => new Promise<void>((resolve, reject) => {
    server.close((error) => error ? reject(error) : resolve());
    server.closeAllConnections();
  }));
  const address = server.address();
  if (!address || typeof address === "string") throw new Error("Listener did not start");
  return `http://127.0.0.1:${address.port}/v1`;
}
function observed(req = request) {
  const events: HarnessEvent[] = [];
  return { events, eventWriter: new HarnessEventWriter(req, {
    write(line) { events.push(JSON.parse(String(line))); return true; },
  }) };
}
function emit(res: ServerResponse, event: unknown) {
  res.write(`data: ${JSON.stringify(event)}\r\n\r\n`);
}
function response(id: string, output: unknown[]) {
  return { id, object: "response", status: "completed", output, usage: {
    input_tokens: 20, input_tokens_details: { cached_tokens: 3 },
    output_tokens: 5, output_tokens_details: { reasoning_tokens: 0 }, total_tokens: 25,
  } };
}
async function flatProvider() {
  const requests: Record<string, any>[] = [];
  const headers: IncomingMessage["headers"][] = [];
  const baseUrl = await listen(async (req, res) => {
    let body = "";
    for await (const chunk of req) body += chunk;
    const parsed = JSON.parse(body);
    requests.push(parsed);
    headers.push(req.headers);
    if (parsed.tools.some((tool: any) => tool.type === "namespace")) {
      res.writeHead(422, { "content-type": "application/json" });
      res.end('{"error":{"message":"Only flat function declarations accepted"}}');
      return;
    }
    const tool = parsed.tools.find((tool: any) => tool.name === "mcp__selected__emit_receipt");
    if (!tool) { res.writeHead(422).end("Receipt tool missing"); return; }
    const call = {
      type: "function_call", id: "streamed-item", call_id: "streamed-call",
      name: tool.name, arguments: '{"label":"streamed"}',
    };
    const message = {
      type: "message", id: "answer", role: "assistant", status: "completed",
      content: [{ type: "output_text", text: "Actual receipt received.", annotations: [] }],
    };
    const item = requests.length === 1 ? call : message;
    res.writeHead(200, { "content-type": "text/event-stream" });
    emit(res, { type: "response.created", response: { ...response("response", []), status: "in_progress" } });
    emit(res, { type: "response.output_item.added", output_index: 0,
      item: requests.length === 1 ? { ...call, arguments: "" } : item });
    if (requests.length === 1) {
      for (const delta of ['{"label":', '"streamed"', "}"]) {
        emit(res, { type: "response.function_call_arguments.delta", item_id: call.id,
          output_index: 0, name: tool.name, delta });
        await delay(5);
      }
      emit(res, { type: "response.function_call_arguments.done", item_id: call.id,
        output_index: 0, name: tool.name, arguments: call.arguments });
    }
    emit(res, { type: "response.output_item.done", output_index: 0, item });
    emit(res, { type: "response.completed", response: response("response", [item]) });
    res.end();
  });
  return { baseUrl, requests, headers };
}

describe("optional Codex flat-functions provider (official SDK and native CLI)", () => {
  it("executes real selected MCP from streamed flat calls and forwards the actual tool output", async () => {
    const provider = await flatProvider();
    const trace = observed();
    let root = "";
    const session = await createCodexSession(request, {
      ...trace, env: {
        AGENTTRACE_CODEX_BASE_URL: provider.baseUrl, AGENTTRACE_CODEX_API_KEY: "private-upstream-key",
        AGENTTRACE_CODEX_HTTP_HEADERS: '{"X-Private":"parent-only-header"}',
        AGENTTRACE_CODEX_TOOL_FORMAT: "flat-functions", PATH: process.env.PATH,
      },
      signal: AbortSignal.timeout(20000),
      prepareHarnessResources: async (req) => {
        const resources = await prepareHarnessResources(req); root = resources.cwd; return resources;
      },
    });
    cleanup.push(() => session.close());
    await session.prompt(request.goal);
    expect(provider.requests).toHaveLength(2);
    expect(provider.headers[0]!.authorization).toBe("Bearer private-upstream-key");
    expect(provider.headers[0]!["x-private"]).toBe("parent-only-header");
    expect(provider.requests[0]!.tools.find((tool: any) => tool.name === "mcp__selected__emit_receipt"))
      .toMatchObject({ type: "function", parameters: { required: ["label"] } });
    const input = provider.requests[1]!.input;
    expect(input.find((item: any) => item.type === "function_call")).toMatchObject({
      name: "mcp__selected__emit_receipt", arguments: '{"label":"streamed"}',
    });
    expect(input.find((item: any) => item.type === "function_call")).not.toHaveProperty("namespace");
    expect(JSON.stringify(input.find((item: any) => item.type === "function_call_output")?.output))
      .toContain("receipt:streamed");
    expect(trace.events.filter((event) => event.type === "pi.tool.completed").map((event) => event.payload))
      .toEqual([expect.objectContaining({
        tool_name: "mcp__selected__emit_receipt", is_error: false,
        structured_result: { receipt: "receipt:streamed" },
      })]);
    expect(trace.events.find((event) => event.type === "pi.message.completed")?.payload.text)
      .toBe("Actual receipt received.");
    expect(trace.events.find((event) => event.type === "pi.usage")?.payload).toMatchObject({
      input_tokens: 40, output_tokens: 10, cache_read_tokens: 6,
    });
    const config = await readFile(join(root, ".codex-home", "config.toml"), "utf8");
    expect(config).not.toContain(provider.baseUrl);
    expect(config).not.toContain("private-upstream-key");
    expect(config).not.toContain("parent-only-header");
    await session.close();
    await expect(access(root)).rejects.toThrow();
  }, 30000);

  it("keeps concurrent official SDK attempts and real MCP receipts isolated", async () => {
    const providers = await Promise.all([flatProvider(), flatProvider()]);
    const traces = [observed(), observed()];
    const sessions = await Promise.all(providers.map(async (provider, index) => {
      const session = await createCodexSession(request, {
        prepareHarnessResources,
        ...traces[index]!, env: {
          AGENTTRACE_CODEX_BASE_URL: provider.baseUrl, AGENTTRACE_CODEX_API_KEY: "private-upstream-key",
          AGENTTRACE_CODEX_TOOL_FORMAT: "flat-functions", PATH: process.env.PATH,
        },
        signal: AbortSignal.timeout(20000),
      });
      cleanup.push(() => session.close());
      return session;
    }));
    await Promise.all(sessions.map((session) => session.prompt(request.goal)));
    expect(providers.map((provider) => provider.requests.length)).toEqual([2, 2]);
    expect(traces.map((trace) => trace.events.filter((event) => event.type === "pi.tool.completed").length))
      .toEqual([1, 1]);
    expect(traces[0]!.events.find((event) => event.type === "pi.tool.completed")!.payload.tool_call_id)
      .not.toBe(traces[1]!.events.find((event) => event.type === "pi.tool.completed")!.payload.tool_call_id);
    await Promise.all(sessions.map((session) => session.close()));
  }, 30000);

  it("joins official CLI cancellation and closes the bridge before real MCP and private resources", async () => {
    let ready!: () => void;
    let upstreamClosed!: () => void;
    const started = new Promise<void>((resolve) => { ready = resolve; });
    const disconnected = new Promise<void>((resolve) => { upstreamClosed = resolve; });
    const baseUrl = await listen(async (req, res) => {
      for await (const _chunk of req) { /* Drain the actual CLI request. */ }
      res.on("close", upstreamClosed);
      res.writeHead(200, { "content-type": "text/event-stream" });
      emit(res, { type: "response.created", response: { ...response("cancel", []), status: "in_progress" } });
      ready();
    });
    const trace = observed();
    const controller = new AbortController();
    let root = "";
    let bridgeClosedBeforeMcp = false;
    let childJoinedBeforeMcp = false;
    const session = await createCodexSession(request, {
      ...trace, signal: controller.signal, env: {
        AGENTTRACE_CODEX_BASE_URL: baseUrl, AGENTTRACE_CODEX_API_KEY: "private-upstream-key",
        AGENTTRACE_CODEX_TOOL_FORMAT: "flat-functions", PATH: process.env.PATH,
      },
      prepareHarnessResources: async (req) => {
        const resources = await prepareHarnessResources(req); root = resources.cwd; return resources;
      },
      openRuntimeMcp: async (resources, writer) => {
        const mcp = await openRuntimeMcp(resources, writer);
        return { ...mcp, close: async () => {
          const home = join(root, ".codex-home");
          const state = JSON.parse(await readFile(join(home, "exec-state.json"), "utf8"));
          childJoinedBeforeMcp = state.closed === true;
          expect(() => process.kill(state.supervisor, 0)).toThrow();
          const config = await readFile(join(home, "config.toml"), "utf8");
          const url = JSON.parse(/^base_url = (.+)$/m.exec(config)![1]!);
          try { await fetch(`${url}/responses`); } catch { bridgeClosedBeforeMcp = true; }
          await mcp.close();
        } };
      },
    });
    cleanup.push(() => session.close());
    const running = session.prompt(request.goal);
    const rejected = expect(running).rejects.toThrow();
    await started;
    controller.abort();
    await Promise.all([rejected, session.abort(), session.close(), disconnected]);
    expect(childJoinedBeforeMcp).toBe(true);
    expect(bridgeClosedBeforeMcp).toBe(true);
    expect(trace.events.some((event) => event.type === "harness.settled")).toBe(false);
    await expect(access(root)).rejects.toThrow();
  }, 30000);

  it("preserves a real provider rejection as failure and cleans the actual SDK and MCP without fallback", async () => {
    let calls = 0;
    const baseUrl = await listen(async (req, res) => {
      for await (const _chunk of req) { /* Drain the actual CLI request. */ }
      calls++;
      res.writeHead(403, { "content-type": "application/json" });
      res.end('{"error":{"message":"Actual provider authorization denied"}}');
    });
    const trace = observed();
    let root = "";
    const session = await createCodexSession(request, {
      ...trace, env: {
        AGENTTRACE_CODEX_BASE_URL: baseUrl, AGENTTRACE_CODEX_API_KEY: "private-upstream-key",
        AGENTTRACE_CODEX_TOOL_FORMAT: "flat-functions", PATH: process.env.PATH,
      },
      prepareHarnessResources: async (req) => {
        const resources = await prepareHarnessResources(req); root = resources.cwd; return resources;
      },
      signal: AbortSignal.timeout(20000),
    });
    cleanup.push(() => session.close());
    await expect(session.prompt(request.goal)).rejects.toThrow();
    expect(calls).toBeGreaterThan(0);
    expect(trace.events.some((event) => event.type === "harness.settled")).toBe(false);
    expect(trace.events.some((event) => event.type === "pi.tool.completed")).toBe(false);
    expect(trace.events.at(-1)?.type).toBe("harness.failed");
    await session.close();
    await expect(access(root)).rejects.toThrow();
  }, 30000);
});
