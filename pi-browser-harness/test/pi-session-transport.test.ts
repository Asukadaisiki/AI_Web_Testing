import { genericExecution } from "./fixtures/selected-harness.js";
import { mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { Writable } from "node:stream";
import { fileURLToPath, pathToFileURL } from "node:url";
import {
  createAgentSession, ModelRuntime, type AgentSession, type McpTransportFactory,
} from "@earendil-works/pi-coding-agent";
import { StdioTransport } from "@earendil-works/pi-mcp";
import { expect, it, vi } from "vitest";
import type { HarnessDefinition } from "../src/harness-definition.js";
import { createPiSession, type PiSession } from "../src/pi-session.js";
import { HarnessEventWriter, type RunRequest } from "../src/run-protocol.js";
import { SessionCleanupError } from "../src/server.js";

const fixture = fileURLToPath(new URL("./fixtures/runtime-mcp-server.mjs", import.meta.url));
const request: RunRequest = {
  execution: genericExecution,
  protocol_version: "pi-browser-harness.v1", run_id: "transport", attempt_id: "transport-attempt",
  goal: "complete the request",
  model: { provider: "scripted", name: "offline-fixture", reasoning_effort: "off" },
};
const definition: HarnessDefinition = {
  systemPrompt: "Complete the request", skillPaths: [], toolAllowlist: [],
  mcpServers: [{
    name: "transport", command: process.execPath, args: [fixture], exposure: "direct",
  }],
  completionPolicy: () => ({ verdict: "succeeded" }),
};

async function withSession(
  createTransport: McpTransportFactory | undefined,
  check: (session: PiSession, sdk: AgentSession, cwd: string) => Promise<void>,
  transportCloseTimeoutMs = 5_000,
  selectedDefinition = definition,
) {
  const cwd = await mkdtemp(join(tmpdir(), "pi-transport-"));
  let session: PiSession | undefined;
  let sdk: AgentSession | undefined;
  try {
    session = await createPiSession(request, {
      cwd, ...(createTransport ? { createTransport } : {}), transportCloseTimeoutMs,
      modelRuntime: await ModelRuntime.create({ refreshOnCreate: false }),
      eventWriter: new HarnessEventWriter(request, new Writable({
        write(_chunk, _encoding, done) { done(); },
      })),
      createAgentSession: async (options) => {
        const result = await createAgentSession(options);
        sdk = result.session;
        return result;
      },
    }, selectedDefinition);
    await sdk!.prompt("/mcp");
    await check(session, sdk!, cwd);
  } finally {
    await writeFile(join(cwd, "release-pipes"), "");
    await session?.close().catch(() => {});
    await rm(cwd, { recursive: true, force: true });
  }
}

it.each(["already-exited", "repeat-close"])(
  "retains the actual Pi SDK transport close event across its %s early return", async (race) => {
    let transport: StdioTransport | undefined;
    let nativeClose: (() => Promise<void>) | undefined;
    let acknowledged = false;
    const holdingPipes = `
      import { access } from "node:fs/promises";
      import { join } from "node:path";
      for (;;) {
        try { await access(join(process.cwd(), "release-pipes")); break; }
        catch { await new Promise(resolve => setTimeout(resolve, 10)); }
      }
    `;
    const script = `
      import { spawn } from "node:child_process";
      import { writeFileSync } from "node:fs";
      const child = spawn(process.execPath, ["--input-type=module", "-e", ${JSON.stringify(holdingPipes)}],
        { stdio: ["ignore", 1, 2] });
      writeFileSync("pipe-holder.pid", String(child.pid));
      child.unref();
      await import(${JSON.stringify(pathToFileURL(fixture).href)});
    `;
    await withSession((_entry, cwd) => {
      transport = new StdioTransport({
        command: process.execPath, args: ["--input-type=module", "-e", script], cwd,
      });
      nativeClose = transport.close.bind(transport);
      transport.onClose(() => { acknowledged = true; });
      return transport;
    }, async (session, sdk, cwd) => {
      expect(transport).toBeDefined();
      const pid = transport!.pid!;
      const holder = Number(await readFile(join(cwd, "pipe-holder.pid"), "utf8"));
      let first: Promise<void> | undefined;
      let closing: Promise<void> | undefined;
      const disposal = vi.spyOn(sdk, "dispose");
      try {
        if (race === "already-exited") {
          process.kill(pid, "SIGTERM");
        } else {
          // Bypass only our wrapper to characterize the pinned SDK's active close.
          first = nativeClose!();
        }
        await vi.waitFor(() => expect(() => process.kill(pid, 0)).toThrow(/ESRCH/));
        expect(acknowledged).toBe(false);
        expect(() => process.kill(holder, 0)).not.toThrow();
        // The real SDK resolves although inherited pipes still prevent child 'close'.
        await nativeClose!();
        expect(acknowledged).toBe(false);
        let returned = false;
        closing = session.close();
        void closing.then(() => { returned = true; }, () => { returned = true; });
        expect(session.close()).toBe(closing);
        await new Promise((resolve) => setTimeout(resolve, 50));
        expect(returned).toBe(false);
        expect(disposal).not.toHaveBeenCalled();
        await writeFile(join(cwd, "release-pipes"), "");
        await closing;
        expect(acknowledged).toBe(true);
        expect(disposal).toHaveBeenCalledTimes(1);
        await vi.waitFor(() => expect(() => process.kill(holder, 0)).toThrow(/ESRCH/));
      } finally {
        await writeFile(join(cwd, "release-pipes"), "");
        await closing?.catch(() => {});
        await first;
      }
    });
  },
);

it.each(["missing acknowledgement", "suppressed close error"])(
  "fails with SessionCleanupError on %s through the supported transport factory", async (failure) => {
    let pid: number | undefined;
    await withSession((_entry, cwd) => {
      const transport = new StdioTransport({ command: process.execPath, args: [fixture], cwd });
      const start = transport.start.bind(transport);
      transport.start = async () => { await start(); pid = transport.pid; };
      if (failure === "missing acknowledgement") {
        transport.onClose = () => () => {};
      } else {
        const close = transport.close.bind(transport);
        transport.close = async () => { await close(); throw new Error("injected native close error"); };
      }
      return transport;
    }, async (session, sdk) => {
      const disposal = vi.spyOn(sdk, "dispose");
      const closing = session.close();
      await expect(closing).rejects.toBeInstanceOf(SessionCleanupError);
      await expect(closing).rejects.toThrow(
        failure === "missing acknowledgement" ? /acknowledgement/ : /injected native close error/,
      );
      expect(session.close()).toBe(closing);
      expect(disposal).toHaveBeenCalledTimes(1);
      expect(() => process.kill(pid!, 0)).toThrow(/ESRCH/);
    }, 200);
  },
);

it.each([
  "${PI_TRANSPORT_INSTANCE}",
  "$PI_TRANSPORT_INSTANCE",
  "!printf resolved-command",
  "$$PI_TRANSPORT_INSTANCE $!literal",
])("passes resolved MCP environment values literally without shell or host-env expansion: %s", async (value) => {
  vi.stubEnv("PI_TRANSPORT_INSTANCE", "resolved-environment");
  try {
    await withSession(undefined, async (_session, sdk) => {
      const tool = sdk.extensionRunner.getToolDefinition("mcp__transport__echo_tool")!;
      const result = await tool.execute(
        "config-resolution", {}, undefined, undefined,
        sdk.extensionRunner.createToolContext("config-resolution", undefined),
      );
      expect(result.structuredContent).toMatchObject({
        structuredContent: { instance: value },
      });
    }, 5_000, {
      ...definition, mcpServers: [{
        name: "transport", command: process.execPath, args: [fixture],
        env: { INSTANCE: value! }, exposure: "direct",
      }],
    });
  } finally {
    vi.unstubAllEnvs();
  }
});

it("retains an unacknowledged old transport after Pi reconnects and closes the new child", async () => {
  const transports: StdioTransport[] = [];
  let firstClose: (() => Promise<void>) | undefined;
  await withSession((_entry, cwd) => {
    const transport = new StdioTransport({ command: process.execPath, args: [fixture], cwd });
    if (!transports.length) {
      firstClose = transport.close.bind(transport);
      const onClose = transport.onClose.bind(transport);
      let subscriptions = 0;
      // Only the retained observer loses its acknowledgement; Pi can still reconnect.
      transport.onClose = (listener) => ++subscriptions === 1 ? () => {} : onClose(listener);
    }
    transports.push(transport);
    return transport;
  }, async (session, sdk) => {
    await firstClose!();
    const tool = sdk.extensionRunner.getToolDefinition("mcp__transport__echo_tool")!;
    const result = await tool.execute(
      "reconnect", {}, undefined, undefined,
      sdk.extensionRunner.createToolContext("reconnect", undefined),
    );
    const pid = (result.structuredContent as { structuredContent: { pid: number } }).structuredContent.pid;
    expect(transports).toHaveLength(2);
    await expect(session.close()).rejects.toBeInstanceOf(SessionCleanupError);
    expect(() => process.kill(pid, 0)).toThrow(/ESRCH/);
  }, 200);
});
