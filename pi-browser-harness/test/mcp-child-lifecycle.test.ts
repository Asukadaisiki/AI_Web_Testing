import { execFileSync } from "node:child_process";
import { mkdtempSync, rmSync } from "node:fs";
import { createServer, type ServerResponse } from "node:http";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import { McpClient, StdioTransport } from "@earendil-works/pi-mcp";
import { expect, it, vi } from "vitest";

const root = fileURLToPath(new URL("..", import.meta.url));

it.each([false, true])("an actual MCP child awaits browser DELETE and reports uncertainty (failed DELETE: %s)", async (failedDelete) => {
  const built = mkdtempSync(join(root, "test/.mcp-child-build-"));
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
  let client: McpClient | undefined;
  let closing: Promise<void> | undefined;
  try {
    // A private build avoids racing the existing compiled-module regression's dist cleanup.
    execFileSync(process.execPath, [
      join(root, "node_modules/typescript/bin/tsc"), "--project", "tsconfig.build.json",
      "--outDir", built,
    ], { cwd: root });
    await new Promise<void>((resolve) => worker.listen(0, "127.0.0.1", resolve));
    const address = worker.address();
    if (!address || typeof address === "string") throw new Error("worker did not listen");
    const transport = new StdioTransport({
      command: process.execPath, args: [join(built, "mcp/server.js")],
      env: {
        AGENTTRACE_HARNESS_WORKER_URL: `http://127.0.0.1:${address.port}`,
        AGENTTRACE_HARNESS_ARTIFACT_SCOPE: "run", AGENTTRACE_HARNESS_GOAL: "open example",
      },
    });
    client = new McpClient({ name: "cleanup-regression", version: "1.0.0" });
    await client.connect(transport);
    const result = await client.callTool("open_page", {
      url: "https://example.test", intent: "open example",
    });
    expect(result.isError).not.toBe(true);
    expect(requests).toContain("POST /sessions");
    const pid = transport.pid!;
    let returned = false;
    const acknowledgement = failedDelete
      ? client.request("browser/close").then(
        () => "released",
        (error: unknown) => error instanceof Error ? error.message : String(error),
      ) : undefined;
    if (!failedDelete) closing = client.close().then(() => { returned = true; });
    await vi.waitFor(() => expect(deletion).toBeDefined());
    expect(returned).toBe(false);
    expect(() => process.kill(pid, 0)).not.toThrow();
    if (failedDelete) {
      deletion!.statusCode = 503;
      deletion!.end(JSON.stringify({ error: "worker_error", detail: "cleanup uncertain" }));
      expect(await acknowledgement).toContain("cleanup uncertain");
      await expect(client.request("browser/close")).rejects.toThrow("cleanup uncertain");
      closing = client.close();
    } else {
      deletion!.end(JSON.stringify({ closed: true }));
    }
    await closing;
    expect(requests.filter((entry) => entry === "DELETE /sessions/browser-1")).toHaveLength(1);
    expect(() => process.kill(pid, 0)).toThrow(/ESRCH/);
  } finally {
    if (deletion && !deletion.writableEnded) deletion.end(JSON.stringify({ closed: true }));
    await (closing ?? client?.close());
    await new Promise<void>((resolve) => worker.close(() => resolve()));
    rmSync(built, { recursive: true, force: true });
  }
}, 60_000);
