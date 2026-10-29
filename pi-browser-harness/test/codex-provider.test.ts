import { createServer, request as httpRequest, type RequestListener } from "node:http";
import { setTimeout as delay } from "node:timers/promises";
import { afterEach, describe, expect, it } from "vitest";
import { openCodexProvider } from "../src/codex-provider.js";

const cleanup: (() => Promise<void>)[] = [];
afterEach(async () => {
  for (const close of cleanup.splice(0).reverse()) await close();
});
async function upstream(handler: RequestListener) {
  const server = createServer(handler);
  await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
  cleanup.push(() => new Promise<void>((resolve, reject) => {
    server.close((error) => error ? reject(error) : resolve()); server.closeAllConnections();
  }));
  const address = server.address();
  if (!address || typeof address === "string") throw new Error("Listener did not start");
  return `http://127.0.0.1:${address.port}/v1?private=query`;
}
async function bridge(baseUrl: string, servers = ["selected"]) {
  const controller = new AbortController();
  const provider = await openCodexProvider({
    baseUrl, apiKey: "private-upstream-key", headers: { "X-Private": "private-header" },
    servers, signal: controller.signal,
  });
  cleanup.push(() => provider.close());
  return { ...provider, controller };
}
function namespace(name = "mcp__selected", tool = "check") {
  return { type: "namespace", name, description: "Selected tools.", tools: [{
    type: "function", name: tool, description: "Actual selected function.", strict: false,
    parameters: { type: "object", properties: { text: { type: "string" } } },
  }] };
}
async function post(provider: Awaited<ReturnType<typeof bridge>>, body: unknown) {
  return fetch(`${provider.baseUrl}/responses`, {
    method: "POST", headers: { Authorization: `Bearer ${provider.apiKey}`, "content-type": "application/json" },
    body: JSON.stringify(body),
  });
}

describe("Codex provider wire bridge", () => {
  it("translates input and all choices without changing opaque fields, arguments, outputs or upstream credentials", async () => {
    let received: any;
    let headers: any;
    let path = "";
    const base = await upstream((req, res) => {
      headers = req.headers; path = req.url!;
      let body = "";
      req.on("data", (chunk) => { body += chunk; });
      req.on("end", () => {
        received = JSON.parse(body);
        res.writeHead(200, { "content-type": "application/json" });
        res.end(JSON.stringify({ output: [{
          type: "function_call", name: "mcp__selected__check", arguments: '{"namespace":"unchanged"}',
        }], usage: { opaque_tokens: 19 }, metadata: { namespace: "opaque", name: "opaque" } }));
      });
    });
    const provider = await bridge(base);
    const opaque = { type: "function_call", namespace: "mcp__selected", name: "check" };
    const call = { ...opaque, arguments: JSON.stringify(opaque), call_id: "actual-call" };
    const toolOutput = { type: "function_call_output", call_id: "actual-call", output: opaque };
    const body = {
      model: "same-model", tools: [namespace(), { type: "function", name: "native_function", parameters: {} }],
      input: [call, toolOutput], tool_choice: { type: "allowed_tools", mode: "required", tools: [opaque] },
      metadata: opaque, custom_provider_option: { input: [opaque] }, stream: false,
    };
    const result = await (await post(provider, body)).json();
    expect(path).toBe("/v1/responses?private=query");
    expect(headers.authorization).toBe("Bearer private-upstream-key");
    expect(headers["x-private"]).toBe("private-header");
    expect(received).toEqual({
      ...body, tools: [{ ...namespace().tools[0], name: "mcp__selected__check" }, body.tools[1]],
      input: [{ type: "function_call", name: "mcp__selected__check", arguments: call.arguments, call_id: "actual-call" }, toolOutput],
      tool_choice: { type: "allowed_tools", mode: "required", tools: [{ type: "function_call", name: "mcp__selected__check" }] },
    });
    expect(result).toEqual({
      output: [{ type: "function_call", namespace: "mcp__selected", name: "check", arguments: '{"namespace":"unchanged"}' }],
      usage: { opaque_tokens: 19 }, metadata: { namespace: "opaque", name: "opaque" },
    });
    await post(provider, { ...body, tool_choice: { type: "function", namespace: "mcp__selected", name: "check" } });
    expect(received.tool_choice).toEqual({ type: "function", name: "mcp__selected__check" });
  });

  it("maps every streaming envelope and named delta with fragmented multiline SSE, preserving text and unknown calls", async () => {
    const call = { type: "function_call", id: "call", name: "mcp__selected__check", arguments: '{"text":"real"}' };
    const events = [
      { type: "response.created", response: { output: [call], usage: { total_tokens: 17 } } },
      { type: "response.output_item.added", item: { ...call, arguments: "" }, output_index: 0 },
      { type: "response.function_call_arguments.delta", name: call.name, delta: '{"text":', item_id: "call" },
      { type: "response.function_call_arguments.done", name: call.name, arguments: call.arguments },
      { type: "response.output_item.done", item: call },
      { type: "response.completed", response: { output: [call], usage: { total_tokens: 17 } } },
      { type: "response.output_text.delta", delta: 'Unchanged text: {"name":"mcp__selected__check"} \u00e9' },
      { type: "response.output_item.done", item: { ...call, name: "undeclared_function" } },
    ];
    const base = await upstream((_req, res) => {
      res.writeHead(200, { "content-type": "text/event-stream" });
      void (async () => {
        res.write(": keepalive\r\n\r\n");
        for (const event of events) {
          const raw = Buffer.from(`event: ${event.type}\r\nid: exact-id\r\ndata: ${JSON.stringify(event)}\r\n\r\n`);
          for (let i = 0; i < raw.length; i += 7) {
            res.write(raw.subarray(i, i + 7));
            await delay(0);
          }
        }
        res.end("data: [DONE]\r\n\r\n");
      })();
    });
    const provider = await bridge(base);
    const raw = await (await post(provider, { tools: [namespace()], input: [], stream: true })).text();
    const actual = raw.split("\n\n").map((frame) => frame.split("\n").find((line) => line.startsWith("data: ")))
      .filter((line) => line && line !== "data: [DONE]").map((line) => JSON.parse(line!.slice(6)));
    const mapped = { ...call, name: "check", namespace: "mcp__selected" };
    expect(actual).toEqual([
      { ...events[0], response: { output: [mapped], usage: { total_tokens: 17 } } },
      { ...events[1], item: { ...mapped, arguments: "" } },
      { ...events[2], name: "check", namespace: "mcp__selected" },
      { ...events[3], name: "check", namespace: "mcp__selected" },
      { ...events[4], item: mapped },
      { ...events[5], response: { output: [mapped], usage: { total_tokens: 17 } } },
      events[6], events[7],
    ]);
    expect(raw).toContain("id: exact-id");
    expect(raw).toContain("[DONE]");
    expect(raw).toContain(": keepalive");
  });

  it("bounds aliases and resolves collisions without cross-attempt mappings", async () => {
    const captured: any[] = [];
    const base = await upstream((req, res) => {
      let body = "";
      req.on("data", (chunk) => { body += chunk; });
      req.on("end", () => {
        const parsed = JSON.parse(body); captured.push(parsed);
        res.writeHead(200, { "content-type": "application/json" });
        res.end(JSON.stringify({ output: parsed.tools.map((tool: any) => ({
          type: "function_call", name: tool.name, arguments: "{}",
        })) }));
      });
    });
    const long = "a".repeat(80);
    const a = await bridge(base, ["a", "a__b", long]);
    const b = await bridge(base, ["selected"]);
    expect(a.baseUrl).not.toBe(b.baseUrl);
    expect(a.apiKey).not.toBe(b.apiKey);
    const tools = [
      { type: "function", name: "mcp__a__b__c", parameters: {} },
      namespace("mcp__a", "b__c"), namespace("mcp__a__b", "c"), namespace(`mcp__${long}`, "check"),
    ];
    const [resultA, resultB] = await Promise.all([
      post(a, { tools, input: [] }).then((res) => res.json()),
      post(b, { tools: [namespace()], input: [] }).then((res) => res.json()),
    ]);
    expect(resultA.output).toEqual([
      { type: "function_call", name: "mcp__a__b__c", arguments: "{}" },
      { type: "function_call", name: "b__c", namespace: "mcp__a", arguments: "{}" },
      { type: "function_call", name: "c", namespace: "mcp__a__b", arguments: "{}" },
      { type: "function_call", name: "check", namespace: `mcp__${long}`, arguments: "{}" },
    ]);
    expect(resultB.output).toEqual([{ type: "function_call", name: "check", namespace: "mcp__selected", arguments: "{}" }]);
    const names = captured.find((body) => body.tools.length === 4).tools.map((tool: any) => tool.name);
    expect(new Set(names).size).toBe(4);
    expect(names.every((name: string) => name.length <= 64 && /^[A-Za-z0-9_-]+$/.test(name))).toBe(true);
    await a.close();
    await expect(post(a, { tools: [], input: [] })).rejects.toThrow();
    expect((await post(b, { tools: [namespace()], input: [] })).status).toBe(200);
  });

  it("denies origins, wrong hosts, unknown routes and missing authentication before forwarding", async () => {
    let calls = 0;
    const base = await upstream((_req, res) => { calls++; res.end("{}"); });
    const provider = await bridge(base);
    for (const [suffix, headers, status] of [
      ["/responses", {}, 401],
      ["/responses", { Authorization: "Bearer wrong" }, 401],
      ["/responses", { Authorization: `Bearer ${provider.apiKey}`, Origin: "https://example.com" }, 403],
      ["/wrong", { Authorization: `Bearer ${provider.apiKey}` }, 404],
    ] as const) {
      expect((await fetch(provider.baseUrl + suffix, { method: "POST", headers })).status).toBe(status);
    }
    const wrongHostStatus = await new Promise<number | undefined>((resolve, reject) => {
      const req = httpRequest(`${provider.baseUrl}/responses`, {
        method: "POST", headers: { Authorization: `Bearer ${provider.apiKey}`, Host: "example.com" },
      }, (res) => { res.resume(); resolve(res.statusCode); });
      req.on("error", reject);
      req.end();
    });
    expect(wrongHostStatus).toBe(403);
    expect(calls).toBe(0);
  });

  it.each([403, 429, 503])("forwards actual provider HTTP %s and body without creating a response", async (status) => {
    const base = await upstream((_req, res) => {
      res.writeHead(status, { "content-type": "application/json", "retry-after": "7" });
      res.end('{"error":{"message":"Actual upstream rejection"}}');
    });
    const provider = await bridge(base);
    const result = await post(provider, { tools: [namespace()], input: [] });
    expect(result.status).toBe(status);
    expect(result.headers.get("retry-after")).toBe("7");
    expect(await result.text()).toBe('{"error":{"message":"Actual upstream rejection"}}');
  });

  it("preserves redirect status without forwarding private credentials to a redirected host", async () => {
    let followed = 0;
    const target = await upstream((_req, res) => { followed++; res.end("{}"); });
    const base = await upstream((_req, res) => {
      res.writeHead(307, { location: target });
      res.end("Actual upstream redirect");
    });
    const provider = await bridge(base);
    const result = await fetch(`${provider.baseUrl}/responses`, {
      method: "POST", redirect: "manual", headers: { Authorization: `Bearer ${provider.apiKey}` },
      body: JSON.stringify({ tools: [], input: [] }),
    });
    expect(result.status).toBe(307);
    expect(await result.text()).toBe("Actual upstream redirect");
    expect(followed).toBe(0);
  });

  it("cancels an upstream stream on client disconnect and joins it on close", async () => {
    let ready!: () => void;
    let disconnected!: () => void;
    const started = new Promise<void>((resolve) => { ready = resolve; });
    const ended = new Promise<void>((resolve) => { disconnected = resolve; });
    const base = await upstream((_req, res) => {
      res.on("close", disconnected);
      res.writeHead(200, { "content-type": "text/event-stream" });
      res.write(": waiting\n\n"); ready();
    });
    const provider = await bridge(base);
    const result = await post(provider, { tools: [namespace()], input: [] });
    await started;
    await result.body!.cancel();
    await ended;
    const closing = provider.close();
    expect(provider.close()).toBe(closing);
    await closing;
    await expect(post(provider, {})).rejects.toThrow();
  });

  it("SDK abort terminates incomplete local request bodies rather than retaining pending work", async () => {
    const base = await upstream((_req, res) => res.end("{}"));
    const provider = await bridge(base);
    const request = httpRequest(`${provider.baseUrl}/responses`, {
      method: "POST", headers: { Authorization: `Bearer ${provider.apiKey}`, "content-length": "10000" },
    });
    const ended = new Promise<void>((resolve) => {
      request.on("error", () => resolve()); request.on("close", () => resolve());
    });
    request.write("{");
    await delay(30);
    provider.controller.abort();
    const outcome = await Promise.race([ended.then(() => "closed"), delay(500).then(() => "retained")]);
    request.destroy();
    expect(outcome).toBe("closed");
    await provider.close();
  });
});
