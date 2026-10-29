import { access, readFile, realpath, writeFile } from "node:fs/promises";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import { Writable } from "node:stream";
import { Client } from "@modelcontextprotocol/sdk/client/index.js";
import { StreamableHTTPClientTransport } from "@modelcontextprotocol/sdk/client/streamableHttp.js";
import type { Transport } from "@modelcontextprotocol/sdk/shared/transport.js";
import type { CallToolResult } from "@modelcontextprotocol/sdk/types.js";
import { afterEach, describe, expect, it, vi } from "vitest";
import { prepareHarnessResources, type HarnessResources } from "../src/harness-resources.js";
import { openRuntimeMcp } from "../src/runtime-mcp.js";
import { HarnessEventWriter, type HarnessEvent, type RunRequest } from "../src/run-protocol.js";
import { SessionCleanupError } from "../src/server.js";
import { BrowserEvidenceGuard } from "../src/browser/evidence-guard.js";

const request: RunRequest = {
  protocol_version: "pi-browser-harness.v1", run_id: "http-run", attempt_id: "http-attempt",
  goal: "Verify the page", model: { provider: "scripted", name: "test", reasoning_effort: "off" },
};
const fixture = fileURLToPath(new URL("./fixtures/runtime-mcp-server.mjs", import.meta.url));
const edgeFixture = fileURLToPath(new URL("./fixtures/runtime-mcp-edge-server.mjs", import.meta.url));

describe("attempt-local real MCP HTTP bridge", () => {
  const disposals: (() => Promise<void>)[] = [];
  afterEach(async () => {
    for (const dispose of disposals.splice(0).reverse()) await dispose();
  });

  async function resources(env: Record<string, string> = {}, name = "browser-harness"): Promise<HarnessResources> {
    const result = await prepareHarnessResources({
      ...request, worker_url: "http://127.0.0.1:8080", artifact_scope: "http-run/http-attempt",
      execution: { runtime_id: "claude-code", harness: {
        id: "test", name: "Test", revision: 1, system_prompt: "Use tools", skills: [],
        mcp: { [name]: { capability: name === "browser-harness" ? "browser-harness" : "fixture",
          config: {}, secret_refs: {} } },
      } },
    } as RunRequest, { capabilities: new Map([["fixture", {
      command: process.execPath, args: [fixture], env, config: {}, secrets: {},
      hostOnlyTools: ["close_session", "release"],
    }]]) });
    // Trusted transport fixture only; public browser snapshots never specify launch fields.
    if (name === "browser-harness") Object.assign(result.servers[0]!, {
      command: process.execPath, args: [fixture], env, hostOnlyTools: ["close_session", "release"],
    });
    disposals.push(result.dispose);
    return result;
  }

  async function connect(resource: HarnessResources, guard?: BrowserEvidenceGuard) {
    const written: HarnessEvent[] = [];
    const rawWriter = new HarnessEventWriter(request, new Writable({
      write(chunk, _encoding, done) { written.push(JSON.parse(chunk.toString())); done(); },
    }));
    const eventWriter = guard?.publicWriter(rawWriter) ?? rawWriter;
    const bridge = await openRuntimeMcp(resource, eventWriter, guard);
    disposals.push(() => bridge.close().catch(() => {}));
    const config = Object.values(bridge.mcpServers)[0]!;
    const client = new Client({ name: "http-test-client", version: "1.0.0" });
    await client.connect(new StreamableHTTPClientTransport(new URL(config.url)) as Transport);
    disposals.push(() => client.close());
    return { bridge, client, written, eventWriter };
  }

  async function edgeResources(env: Record<string, string> = {}) {
    const resource = await resources(env, "custom-tools");
    resource.servers[0]!.args = [edgeFixture];
    // The holder deliberately survives its parent to exercise real stdio settlement.
    disposals.push(async () => {
      await writeFile(join(resource.cwd, `${env.INSTANCE ?? "edge"}-allow-close`), "");
      const log = await readFile(join(resource.cwd, `${env.INSTANCE ?? "edge"}-lifecycle.log`), "utf8")
        .catch(() => "");
      for (const match of log.matchAll(/(?:server|holder):(\d+)/g)) {
        try { process.kill(Number(match[1]), "SIGKILL"); }
        catch (error) { if ((error as NodeJS.ErrnoException).code !== "ESRCH") throw error; }
      }
    });
    return resource;
  }

  it("forwards real tools with normalized identities, actual results, and one upstream child", async () => {
    const resource = await resources();
    const { bridge, client, written } = await connect(resource);
    expect(Object.keys(bridge.mcpServers)).toEqual(["browser_harness"]);
    const endpoint = new URL(bridge.mcpServers.browser_harness!.url);
    expect(endpoint.hostname).toBe("127.0.0.1");
    const listed = (await client.listTools()).tools.map((tool) => tool.name);
    expect(listed).toContain("echo_tool");
    expect(listed).not.toContain("close_session");
    expect(listed).not.toContain("release");
    const args = { nested: { number: 42 }, text: "actual forwarding" };
    const first = await client.callTool({ name: "echo_tool", arguments: args }) as CallToolResult;
    const actualCwd = await realpath(resource.cwd);
    expect(first.structuredContent).toMatchObject({ args, cwd: actualCwd });
    const secondClient = new Client({ name: "second-http-client", version: "1.0.0" });
    await secondClient.connect(new StreamableHTTPClientTransport(endpoint) as Transport);
    try {
      const second = await secondClient.callTool({ name: "echo_tool", arguments: {} }) as CallToolResult;
      expect(second.structuredContent?.pid).toBe(first.structuredContent?.pid);
    } finally { await secondClient.close(); }
    expect((await readFile(join(resource.cwd, "lifecycle.log"), "utf8")).match(/initialized/g))
      .toHaveLength(1);
    expect(written.map((event) => event.type)).toEqual([
      "pi.tool.started", "pi.tool.completed", "pi.tool.started", "pi.tool.completed",
    ]);
    expect(written[1]?.payload).toMatchObject({
      tool_name: "mcp__browser_harness__echo_tool", is_error: false,
      structured_result: { args, cwd: actualCwd },
      result: {
        content: [{ type: "text", text: JSON.stringify(args) }],
        structuredContent: { args, cwd: actualCwd },
      },
    });
    const starts = written.filter((event) => event.type === "pi.tool.started");
    const ends = written.filter((event) => event.type === "pi.tool.completed");
    expect(starts[0]?.payload.tool_call_id).toBe(ends[0]?.payload.tool_call_id);
    expect(starts[1]?.payload.tool_call_id).not.toBe(starts[0]?.payload.tool_call_id);
    expect(bridge.events).toEqual([]);
    await expect(client.callTool({ name: "close_session", arguments: {} })).rejects.toThrow();
    expect(written).toHaveLength(4);
  });

  it("classifies actual private MCP arguments before live evidence and deferred model publication", async () => {
    const canary = "native-mcp-password-canary";
    const { client, written, eventWriter } = await connect(
      await resources({ PASSWORD_CANARY: canary }), new BrowserEvidenceGuard(),
    );
    eventWriter.write("pi.message.completed", { text: `I will enter ${canary}`,
      input: { messages: [{ role: "user", content: `Goal ${canary}` }] } });
    expect(written).toEqual([]);
    const reply = await client.callTool({ name: "password_input", arguments: { value: canary } }) as CallToolResult;
    expect(reply.structuredContent?.accepted).toBe(true);
    expect(written.map((event) => event.type)).toEqual(["pi.tool.started", "pi.tool.completed"]);
    eventWriter.write("harness.settled", { assistant_text: `Entered ${canary}`, verdict: "succeeded" });
    expect(JSON.stringify(written)).not.toContain(canary);
    expect(written.find((event) => event.type === "pi.message.completed")?.payload.text)
      .toBe("I will enter [redacted]");
  });

  it("emits browser evidence from real results, retains only latest selected completion, and rejects empty proof", async () => {
    const resource = await resources();
    const { bridge, client, written } = await connect(resource);
    await client.callTool({ name: "open_page", arguments: {} });
    expect(written.find((event) => event.type === "browser.observation")?.payload).toMatchObject({
      observation: { url: "https://example.test", title: "Example" },
    });
    for (const args of [{ empty: true }, { inconsistent: true }]) {
      await client.callTool({ name: "finish_case", arguments: args });
      expect(bridge.events).toEqual([]);
      expect(resource.definition.completionPolicy({
        assistant_settled: true, aborted: false, assistant_text: "success", events: bridge.events,
      }).verdict).toBe("failed");
    }
    await client.callTool({ name: "finish_case", arguments: {} });
    expect(bridge.events).toHaveLength(1);
    expect(bridge.events[0]?.payload).toMatchObject({ verdict: "passed" });
    expect(resource.definition.completionPolicy({
      assistant_settled: true, aborted: false, assistant_text: "done", events: bridge.events,
    }).verdict).toBe("succeeded");
    await client.callTool({ name: "finish_case", arguments: { status: "failed" } });
    expect(bridge.events).toHaveLength(1);
    expect(bridge.events[0]?.payload).toMatchObject({ verdict: "failed" });
    expect(resource.definition.completionPolicy({
      assistant_settled: true, aborted: false, assistant_text: "success", events: bridge.events,
    }).verdict).toBe("failed");
  });

  it("preserves tool errors and emits a failed completion for protocol errors", async () => {
    const { client, written } = await connect(await resources());
    expect((await client.callTool({ name: "fail", arguments: {} })).isError).toBe(true);
    expect((await client.callTool({ name: "throw", arguments: {} })).isError).toBe(true);
    expect(written.filter((event) => event.type === "pi.tool.completed").map((event) => event.payload.is_error))
      .toEqual([true, true]);
    const completed = written.filter((event) => event.type === "pi.tool.completed");
    expect(completed[0]?.payload.result).toEqual({
      content: [{ type: "text", text: "fixture tool failed" }], isError: true,
      structuredContent: { ok: false },
    });
    expect(completed[1]?.payload.result).toEqual({
      content: [{ type: "text", text: "MCP tool execution failed" }], isError: true,
    });
  });

  it.each([
    { tool: "text_success", result: { content: [{ type: "text", text: "actual text observation" }] } },
    { tool: "text_error", result: {
      content: [{ type: "text", text: "actual text failure details" }], isError: true,
    } },
    { tool: "text_false", result: {
      content: [{ type: "text", text: "explicit successful observation" }], isError: false,
    } },
  ])("captures canonical content-only evidence for $tool without MCP configuration secrets", async ({ tool, result }) => {
    const resource = await edgeResources();
    resource.servers[0]!.env = {
      MCP_SECRET_OPTION: "private-mcp-option-must-not-be-emitted",
    };
    const { client, written } = await connect(resource);
    const reply = await client.callTool({ name: tool, arguments: {} });
    expect(reply).toEqual(result);
    const completed = written.find((event) => event.type === "pi.tool.completed")!;
    expect(completed.payload.result).toEqual(result);
    expect(completed.payload.is_error).toBe("isError" in result && result.isError === true);
    expect(completed.payload).not.toHaveProperty("structured_result");
    expect(JSON.stringify(written)).not.toContain("private-mcp-option-must-not-be-emitted");
    expect(JSON.stringify(written)).not.toContain("MCP_SECRET_OPTION");
  });

  it("awaits delayed SIGTERM server exit after resource acknowledgement", async () => {
    const resource = await edgeResources({ SHUTDOWN_MODE: "delayed" });
    const { bridge, client } = await connect(resource);
    const reply = await client.callTool({ name: "inspect", arguments: {} }) as CallToolResult;
    const pid = reply.structuredContent!.pid as number;
    let settled = false;
    const closing = bridge.close();
    void closing.then(() => { settled = true; }, () => { settled = true; });
    await vi.waitFor(async () => expect(await readFile(join(resource.cwd, "edge-lifecycle.log"), "utf8"))
      .toContain("sigterm"), { timeout: 5000 });
    expect(settled).toBe(false);
    expect(() => process.kill(pid, 0)).not.toThrow();
    expect(await readFile(join(resource.cwd, "edge-lifecycle.log"), "utf8")).toContain("acknowledged");
    await writeFile(join(resource.cwd, "edge-allow-exit"), "");
    await closing;
    expect(() => process.kill(pid, 0)).toThrow(/ESRCH/);
  }, 10_000);

  it("joins real child close after SIGKILL without closing a sibling Attempt", async () => {
    const resource = await edgeResources({ SHUTDOWN_MODE: "ignore", HOLD_STDOUT: "yes" });
    const { bridge, client } = await connect(resource);
    const sibling = await connect(await resources({ INSTANCE: "sibling" }, "custom-tools"));
    const reply = await client.callTool({ name: "inspect", arguments: {} }) as CallToolResult;
    const pid = reply.structuredContent!.pid as number;
    await vi.waitFor(async () => expect(await readFile(join(resource.cwd, "edge-lifecycle.log"), "utf8"))
      .toMatch(/holder:\d+/));
    let settled = false;
    const closing = bridge.close();
    expect(bridge.close()).toBe(closing);
    void closing.then(() => { settled = true; }, () => { settled = true; });
    await vi.waitFor(() => expect(() => process.kill(pid, 0)).toThrow(/ESRCH/), { timeout: 7000 });
    expect(settled).toBe(false);
    expect((await sibling.client.callTool({ name: "echo_tool", arguments: {} }) as CallToolResult)
      .structuredContent?.instance).toBe("sibling");
    await writeFile(join(resource.cwd, "edge-allow-close"), "");
    await closing;
    const log = await readFile(join(resource.cwd, "edge-lifecycle.log"), "utf8");
    expect(log).toContain("holder-exited");
    const holder = Number(log.match(/holder:(\d+)/)![1]);
    expect(() => process.kill(holder, 0)).toThrow(/ESRCH/);
    await expect(fetch(bridge.mcpServers.custom_tools!.url)).rejects.toThrow();
  }, 12_000);

  it("joins the failing handshake's child before rejecting startup and releasing earlier servers", async () => {
    const resource = await edgeResources({
      INSTANCE: "startup", SHUTDOWN_MODE: "ignore", HOLD_STDOUT: "yes", FAIL_INITIALIZE: "yes",
    });
    resource.servers.unshift({ name: "healthy", command: process.execPath, args: [fixture] });
    let settled = false;
    const startup = openRuntimeMcp(resource, { write() { return undefined; } });
    void startup.then(() => { settled = true; }, () => { settled = true; });
    await vi.waitFor(async () => expect(await readFile(join(resource.cwd, "startup-lifecycle.log"), "utf8"))
      .toMatch(/holder:\d+/));
    const log = await readFile(join(resource.cwd, "startup-lifecycle.log"), "utf8");
    const pid = Number(log.match(/server:(\d+)/)![1]);
    await vi.waitFor(() => expect(() => process.kill(pid, 0)).toThrow(/ESRCH/), { timeout: 7000 });
    expect(settled).toBe(false);
    await writeFile(join(resource.cwd, "startup-allow-close"), "");
    await expect(startup).rejects.toThrow(/startup/);
    expect(await readFile(join(resource.cwd, "startup-lifecycle.log"), "utf8")).toContain("holder-exited");
    expect(await readFile(join(resource.cwd, "lifecycle.log"), "utf8")).toContain("acknowledged");
  }, 12_000);

  it("rejects unacknowledged post-SIGKILL settlement rather than reporting successful cleanup", async () => {
    const resource = await edgeResources({ SHUTDOWN_MODE: "ignore", HOLD_STDOUT: "yes" });
    const { bridge } = await connect(resource);
    const closing = bridge.close();
    await expect(closing).rejects.toBeInstanceOf(SessionCleanupError);
    expect(bridge.close()).toBe(closing);
    const log = await readFile(join(resource.cwd, "edge-lifecycle.log"), "utf8");
    expect(log).toContain("acknowledged");
    expect(log).toContain("sigterm");
    expect(log).not.toContain("holder-exited");
    await expect(fetch(bridge.mcpServers.custom_tools!.url)).rejects.toThrow();
  }, 12_000);

  it("does not reuse previous passing proof after a malformed finish result", async () => {
    const resource = await resources();
    const { bridge, client } = await connect(resource);
    await client.callTool({ name: "finish_case", arguments: {} });
    await client.callTool({ name: "finish_case", arguments: { empty: true } });
    expect(bridge.events).toEqual([]);
    expect(resource.definition.completionPolicy({
      assistant_settled: true, aborted: false, assistant_text: "success", events: bridge.events,
    }).verdict).toBe("failed");
  });

  it("drains an actual in-flight call before cleanup", async () => {
    const resource = await resources();
    const { bridge, client } = await connect(resource);
    const calling = client.callTool({ name: "echo_tool", arguments: { wait: true } });
    await vi.waitFor(async () => expect(await readFile(join(resource.cwd, "lifecycle.log"), "utf8"))
      .toContain("call:echo-tool"));
    const closing = bridge.close();
    expect(await readFile(join(resource.cwd, "lifecycle.log"), "utf8")).not.toContain("call:close_session");
    await writeFile(join(resource.cwd, "allow-tool"), "");
    expect((await calling as CallToolResult).structuredContent).toMatchObject({ args: { wait: true } });
    await closing;
    expect(await readFile(join(resource.cwd, "lifecycle.log"), "utf8")).toContain("acknowledged");
  });

  it("isolates concurrent Attempts and keeps sibling HTTP/stdio ownership independent", async () => {
    const first = await connect(await resources({ INSTANCE: "first" }));
    const second = await connect(await resources({ INSTANCE: "second" }));
    const one = await first.client.callTool({ name: "echo_tool", arguments: {} }) as CallToolResult;
    const two = await second.client.callTool({ name: "echo_tool", arguments: {} }) as CallToolResult;
    expect(one.structuredContent?.pid).not.toBe(two.structuredContent?.pid);
    expect(one.structuredContent?.instance).toBe("first");
    expect(two.structuredContent?.instance).toBe("second");
    await first.bridge.close();
    expect((await second.client.callTool({ name: "echo_tool", arguments: {} }) as CallToolResult)
      .structuredContent?.instance).toBe("second");
  });

  it("attempts another server's cleanup even when configured cleanup rejects", async () => {
    const resource = await resources({ CLEANUP_FAIL: "yes" });
    resource.servers.push({ name: "other", command: process.execPath, args: [fixture] });
    const { bridge } = await connect(resource);
    await expect(bridge.close()).rejects.toThrow(/cleanup/);
    const log = await readFile(join(resource.cwd, "lifecycle.log"), "utf8");
    expect(log.match(/call:close_session/g)).toHaveLength(2);
    expect(log).toContain("acknowledged");
    expect(log.match(/disconnected/g)).toHaveLength(2);
  });

  it("cleans already-connected servers if later stdio startup fails", async () => {
    const resource = await resources();
    resource.servers.push({ name: "invalid", command: "/missing/agenttrace-mcp-command" });
    await expect(openRuntimeMcp(resource, new HarnessEventWriter(request, {
      write() { return true; },
    }))).rejects.toThrow(/startup/);
    const log = await readFile(join(resource.cwd, "lifecycle.log"), "utf8");
    expect(log.match(/call:close_session/g)).toHaveLength(1);
    expect(log.indexOf("acknowledged")).toBeLessThan(log.indexOf("disconnected"));
  });

  it("addresses cleanup by exact canonical tool identity rather than overlapping server prefixes", async () => {
    const resource = await resources({ INSTANCE: "first" }, "a");
    resource.servers.push({ name: "a_", command: process.execPath, args: [fixture],
      env: { INSTANCE: "second" }, hostOnlyTools: ["release"] });
    let acknowledgement: unknown;
    resource.definition.cleanup = async ({ callMcpTool }) => {
      acknowledgement = await callMcpTool("mcp__a___release", {});
    };
    const { bridge } = await connect(resource);
    await bridge.close();
    expect(acknowledgement).toMatchObject({ structuredContent: { closed: true, instance: "second" } });
    const log = await readFile(join(resource.cwd, "lifecycle.log"), "utf8");
    expect(log.match(/call:release/g)).toHaveLength(1);
    expect(log.match(/call:close_session/g)).toHaveLength(1);
    expect(log.match(/acknowledged/g)).toHaveLength(2);
  });

  it("awaits host cleanup acknowledgement before disconnecting and closing HTTP exactly once", async () => {
    const resource = await resources({ WAIT_FOR_CLEANUP: "yes" });
    const { bridge, client } = await connect(resource);
    const reply = await client.callTool({ name: "echo_tool", arguments: {} }) as CallToolResult;
    const pid = reply.structuredContent!.pid as number;
    const closing = bridge.close();
    expect(bridge.close()).toBe(closing);
    let returned = false;
    void closing.then(() => { returned = true; });
    await vi.waitFor(async () => expect(await readFile(join(resource.cwd, "lifecycle.log"), "utf8"))
      .toContain("call:close_session"));
    expect(returned).toBe(false);
    expect(() => process.kill(pid, 0)).not.toThrow();
    expect(await readFile(join(resource.cwd, "lifecycle.log"), "utf8")).not.toContain("disconnected");
    await writeFile(join(resource.cwd, "allow-cleanup"), "");
    await closing;
    const log = await readFile(join(resource.cwd, "lifecycle.log"), "utf8");
    expect(log.match(/call:close_session/g)).toHaveLength(1);
    expect(log.indexOf("acknowledged")).toBeLessThan(log.indexOf("disconnected"));
    expect(() => process.kill(pid, 0)).toThrow(/ESRCH/);
    await expect(fetch(bridge.mcpServers.browser_harness!.url)).rejects.toThrow();
  });

  it.each([{ CLEANUP_FAIL: "yes" }, { CLEANUP_MISSING: "yes" }])(
    "rejects uncertain cleanup without repeating it or leaving transports alive: %j", async (env) => {
      const resource = await resources(env);
      const { bridge } = await connect(resource);
      const closing = bridge.close();
      await expect(closing).rejects.toThrow(/cleanup|acknowledgement/);
      expect(bridge.close()).toBe(closing);
      await expect(bridge.close()).rejects.toThrow();
      const log = await readFile(join(resource.cwd, "lifecycle.log"), "utf8");
      expect(log.match(/call:close_session/g)).toHaveLength(1);
      expect(log).toContain("disconnected");
      await expect(fetch(bridge.mcpServers.browser_harness!.url)).rejects.toThrow();
    },
  );

  it("uses configured generic resource cleanup with hidden host tools", async () => {
    const resource = await resources({}, "custom-tools");
    resource.definition.cleanup = async ({ callMcpTool }) => {
      const result = await callMcpTool("mcp__custom_tools__release", {});
      expect(result).toMatchObject({ structuredContent: { closed: true } });
    };
    const { bridge, client, written } = await connect(resource);
    const result = await client.callTool({ name: "open_page", arguments: {} }) as CallToolResult;
    expect(result.structuredContent?.page).toBeDefined();
    expect(written.some((event) => event.type.startsWith("browser."))).toBe(false);
    await bridge.close();
    const log = await readFile(join(resource.cwd, "lifecycle.log"), "utf8");
    expect(log.match(/call:release/g)).toHaveLength(1);
    expect(log).not.toContain("call:close_session");
    expect(written.some((event) => event.type === "pi.tool.completed" &&
      event.payload.tool_name === "mcp__custom_tools__release")).toBe(false);
  });

  it("starts no HTTP service for an empty generic harness", async () => {
    const resource = await resources();
    resource.servers = [];
    delete resource.definition.cleanup;
    const written: HarnessEvent[] = [];
    const bridge = await openRuntimeMcp(resource, new HarnessEventWriter(request, {
      write(chunk) { written.push(JSON.parse(String(chunk))); return true; },
    }));
    expect(bridge.mcpServers).toEqual({});
    expect(bridge.events).toEqual([]);
    await bridge.close();
    expect(written).toEqual([]);
    await expect(access(join(resource.cwd, "lifecycle.log"))).rejects.toThrow();
  });
});
