import { EventEmitter } from "node:events";
import { createServer, type ServerResponse } from "node:http";
import { PassThrough } from "node:stream";
import { expect, it, vi } from "vitest";
import { CaseBuilder } from "../src/browser/case-builder.js";
import { WorkerClient } from "../src/browser/worker-client.js";
import { createBrowserMcpServer, runStdioServer } from "../src/mcp/server.js";

it("awaited MCP close preserves the session and rejects when real worker DELETE fails", async () => {
  let deletes = 0;
  const worker = createServer((request, reply) => {
    expect(request.method).toBe("DELETE");
    expect(request.url).toBe("/sessions/browser-1");
    deletes += 1;
    reply.writeHead(503, { "content-type": "application/json" });
    reply.end(JSON.stringify({ error: "worker_error", detail: "cleanup uncertain" }));
  });
  await new Promise<void>((resolve) => worker.listen(0, "127.0.0.1", resolve));
  const address = worker.address();
  if (!address || typeof address === "string") throw new Error("worker failed to listen");
  const builder = await CaseBuilder.create({
    worker: new WorkerClient(`http://127.0.0.1:${address.port}`),
    artifactScope: "run", goal: "close browser",
  });
  builder.seedBrowserSession("browser-1");
  const server = createBrowserMcpServer(builder);
  try {
    const closing = server.close();
    await expect(closing).rejects.toThrow("cleanup uncertain");
    expect(builder.browserSessionId).toBe("browser-1");
    await expect(server.close()).rejects.toThrow("cleanup uncertain");
    await expect(builder.close()).rejects.toThrow("cleanup uncertain");
    expect(deletes).toBe(1);
  } finally {
    await new Promise<void>((resolve) => worker.close(() => resolve()));
  }
});

it("EOF and repeated signals await real worker DELETE before the MCP child exits", async () => {
  let response: ServerResponse | undefined;
  let deleted = false;
  const worker = createServer((request, reply) => {
    expect(request.method).toBe("DELETE");
    expect(request.url).toBe("/sessions/browser-1");
    response = reply;
  });
  await new Promise<void>((resolve) => worker.listen(0, "127.0.0.1", resolve));
  const address = worker.address();
  if (!address || typeof address === "string") throw new Error("worker failed to listen");
  const input = new PassThrough();
  const output = new PassThrough();
  output.resume();
  const signals = new EventEmitter();
  const exits: number[] = [];
  const proc = Object.assign(signals, {
    exit(code: number) { expect(deleted).toBe(true); exits.push(code); },
  });
  const builder = await CaseBuilder.create({
    worker: new WorkerClient(`http://127.0.0.1:${address.port}`),
    artifactScope: "run", goal: "close browser",
  });
  builder.seedBrowserSession("browser-1");
  try {
    await runStdioServer({ input, output, process: proc, builder });
    input.end();
    await vi.waitFor(() => expect(response).toBeDefined());
    signals.emit("SIGTERM");
    signals.emit("SIGINT");
    await Promise.resolve();
    expect(exits).toEqual([]);
    deleted = true;
    response!.setHeader("content-type", "application/json");
    response!.end(JSON.stringify({ closed: true }));
    await vi.waitFor(() => expect(exits).toEqual([0]));
    expect(signals.listenerCount("SIGTERM")).toBe(0);
  } finally {
    if (response && !response.writableEnded) response.end(JSON.stringify({ closed: true }));
    input.destroy();
    output.destroy();
    await new Promise<void>((resolve) => worker.close(() => resolve()));
  }
});

it("stdio shutdown reports failed DELETE as nonzero exit and retains the session", async () => {
  const worker = createServer((_request, reply) => {
    reply.writeHead(503, { "content-type": "application/json" });
    reply.end(JSON.stringify({ error: "worker_error", detail: "cleanup uncertain" }));
  });
  await new Promise<void>((resolve) => worker.listen(0, "127.0.0.1", resolve));
  const address = worker.address();
  if (!address || typeof address === "string") throw new Error("worker failed to listen");
  const builder = await CaseBuilder.create({
    worker: new WorkerClient(`http://127.0.0.1:${address.port}`),
    artifactScope: "run", goal: "close browser",
  });
  builder.seedBrowserSession("browser-1");
  const input = new PassThrough();
  const output = new PassThrough();
  output.resume();
  const signals = new EventEmitter();
  const exits: number[] = [];
  const proc = Object.assign(signals, { exit: (code: number) => { exits.push(code); } });
  const stderr = vi.spyOn(console, "error").mockImplementation(() => {});
  try {
    await runStdioServer({ input, output, process: proc, builder });
    input.end();
    signals.emit("SIGTERM");
    await vi.waitFor(() => expect(exits).toEqual([1]));
    expect(builder.browserSessionId).toBe("browser-1");
    await expect(builder.close()).rejects.toThrow("cleanup uncertain");
  } finally {
    stderr.mockRestore();
    input.destroy();
    output.destroy();
    await new Promise<void>((resolve) => worker.close(() => resolve()));
  }
});
