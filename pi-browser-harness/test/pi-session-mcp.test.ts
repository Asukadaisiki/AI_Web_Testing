import { mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { Writable } from "node:stream";
import { fileURLToPath, pathToFileURL } from "node:url";
import {
  createAgentSession, ModelRuntime, type AgentSession, type InlineExtension,
} from "@earendil-works/pi-coding-agent";
import { Type } from "typebox";
import { expect, it, vi } from "vitest";
import { createBrowserHarnessDefinition } from "../src/browser-harness.js";
import type { HarnessDefinition, HarnessMcpServer } from "../src/harness-definition.js";
import { createPiSession, type PiSession } from "../src/pi-session.js";
import { HarnessEventWriter, type HarnessEvent, type RunRequest } from "../src/run-protocol.js";

const fixture = fileURLToPath(new URL("./fixtures/runtime-mcp-server.mjs", import.meta.url));
const request: RunRequest = {
  protocol_version: "pi-browser-harness.v1", run_id: "pi-mcp", attempt_id: "pi-mcp-attempt",
  goal: "complete the request",
  model: { provider: "scripted", name: "offline-fixture", reasoning_effort: "off" },
};
const generic: HarnessDefinition = {
  systemPrompt: "Use selected tools", skillPaths: [], toolAllowlist: [], mcpServers: [],
  completionPolicy: ({ assistant_settled, aborted }) => ({
    verdict: assistant_settled && !aborted ? "succeeded" : "failed",
  }),
};
function server(name: string, env: Record<string, string> = {}): HarnessMcpServer {
  return {
    name, command: process.execPath, args: [fixture], env, exposure: "direct",
    hostOnlyTools: ["close_session", "release"],
  };
}

async function withSession(
  definition: HarnessDefinition,
  check: (session: PiSession, sdk: AgentSession, cwd: string, events: HarnessEvent[]) => Promise<void>,
  extensionFactories?: InlineExtension[],
) {
  const cwd = await mkdtemp(join(tmpdir(), "pi-mcp-regression-"));
  let session: PiSession | undefined;
  let sdk: AgentSession | undefined;
  const events: HarnessEvent[] = [];
  try {
    session = await createPiSession(request, {
      cwd, modelRuntime: await ModelRuntime.create({ refreshOnCreate: false }),
      eventWriter: new HarnessEventWriter(request, new Writable({
        write(chunk, _encoding, done) { events.push(JSON.parse(chunk.toString())); done(); },
      })),
      ...(extensionFactories ? { extensionFactories } : {}),
      createAgentSession: async (options) => {
        const result = await createAgentSession(options);
        sdk = result.session;
        return result;
      },
    }, definition);
    await check(session, sdk!, cwd, events);
  } finally {
    // Always unblock the real child, including when an ordering assertion fails.
    await writeFile(join(cwd, "allow-cleanup"), "");
    await session?.close().catch(() => {});
    await rm(cwd, { recursive: true, force: true });
  }
}
const log = (cwd: string) => readFile(join(cwd, "lifecycle.log"), "utf8").catch(() => "");

it.each(["generic", "browser plus supplementary"])(
  "awaits every host close_session acknowledgement exactly once for %s", async (kind) => {
    const definition: HarnessDefinition = kind === "generic" ? {
      ...generic, mcpServers: [server("selected-tools", { WAIT_FOR_CLEANUP: "yes" })],
    } : {
      ...createBrowserHarnessDefinition(), skillPaths: [],
      mcpServers: [server("browser-harness"), server("supplementary", { WAIT_FOR_CLEANUP: "yes" })],
    };
    await withSession(definition, async (session, sdk, cwd, events) => {
      // Close must also discover hidden tools when no model prompt ever ran.
      let returned = false;
      const closing = session.close();
      void closing.then(() => { returned = true; }, () => { returned = true; });
      expect(session.close()).toBe(closing);
      const expectedCalls = kind === "generic" ? 1 : 2;
      try {
        await vi.waitFor(async () => expect((await log(cwd)).match(/call:close_session/g))
          .toHaveLength(expectedCalls));
        expect(returned).toBe(false);
        expect(await log(cwd)).not.toContain("disconnected");
        await writeFile(join(cwd, "allow-cleanup"), "");
        await closing;
        const lifecycle = await log(cwd);
        expect(lifecycle.match(/call:close_session/g)).toHaveLength(expectedCalls);
        expect(lifecycle.match(/acknowledged/g)).toHaveLength(expectedCalls);
        expect(lifecycle.match(/disconnected/g)).toHaveLength(expectedCalls);
        expect(lifecycle.lastIndexOf("acknowledged")).toBeLessThan(lifecycle.indexOf("disconnected"));
        await expect(session.close()).resolves.toBeUndefined();
        expect(events).toEqual([]);
        expect(sdk.getActiveToolNames()).not.toContain("mcp__selected_tools__close_session");
      } finally {
        await writeFile(join(cwd, "allow-cleanup"), "");
        await closing.catch(() => {});
      }
    });
  },
);

it.each([{ CLEANUP_FAIL: "yes" }, { CLEANUP_MISSING: "yes" }])(
  "rejects missing/failed release acknowledgement and still cleans other servers: %j", async (env) => {
    await withSession({
      ...generic, mcpServers: [server("first", env), server("second")],
    }, async (session, _sdk, cwd) => {
      const closing = session.close();
      await expect(closing).rejects.toThrow(/cleanup|acknowledgement/);
      expect(session.close()).toBe(closing);
      await expect(session.close()).rejects.toThrow();
      const lifecycle = await log(cwd);
      expect(lifecycle.match(/call:close_session/g)).toHaveLength(2);
      expect(lifecycle).toContain("acknowledged");
      expect(lifecycle.match(/disconnected/g)).toHaveLength(2);
    });
  },
);

it("preserves custom cleanup, deduplicates repeated host calls, and cleans supplementary servers", async () => {
  await withSession({
    ...generic, mcpServers: [server("a"), server("a_")],
    cleanup: async ({ callMcpTool }) => {
      for (let i = 0; i < 2; i++) {
        expect(await callMcpTool("mcp__a___release", {}))
          .toMatchObject({ structuredContent: { closed: true } });
      }
    },
  }, async (session, _sdk, cwd, events) => {
    await session.close();
    const lifecycle = await log(cwd);
    expect(lifecycle.match(/call:release/g)).toHaveLength(1);
    expect(lifecycle.match(/call:close_session/g)).toHaveLength(1);
    expect(lifecycle.match(/acknowledged/g)).toHaveLength(2);
    expect(events).toEqual([]);
  });
});

it("continues supplementary cleanup after the custom Harness hook rejects", async () => {
  await withSession({
    ...generic, mcpServers: [server("custom"), server("supplementary")],
    cleanup: async ({ callMcpTool }) => {
      await callMcpTool("mcp__custom__release", {});
      throw new Error("custom cleanup rejected");
    },
  }, async (session, _sdk, cwd) => {
    await expect(session.close()).rejects.toThrow("custom cleanup rejected");
    const lifecycle = await log(cwd);
    expect(lifecycle.match(/call:release/g)).toHaveLength(1);
    expect(lifecycle.match(/call:close_session/g)).toHaveLength(1);
    expect(lifecycle.match(/disconnected/g)).toHaveLength(2);
  });
});

it("preserves injected legacy tool definitions and their custom cleanup result shape", async () => {
  let releases = 0;
  const lifecycle: string[] = [];
  await withSession({
    ...generic,
    cleanup: async ({ callMcpTool }) => {
      expect(await callMcpTool("mcp__legacy__release", {})).toEqual({ closed: true });
      expect(await callMcpTool("mcp__legacy__release", {})).toEqual({ closed: true });
      lifecycle.push("acknowledged");
    },
  }, async (session) => {
    await session.close();
    expect(releases).toBe(1);
    expect(lifecycle).toEqual(["acknowledged", "shutdown"]);
  }, [(pi) => {
    pi.registerTool({
      name: "mcp__legacy__release", label: "Release", description: "Legacy release",
      parameters: Type.Object({}), exposure: "hidden",
      async execute() {
        releases++;
        return { content: [], details: {}, structuredContent: { closed: true } };
      },
    });
    pi.on("session_shutdown", () => { lifecycle.push("shutdown"); });
  }]);
});

it("retains real generic MCP content and structured data in completed tool evidence", async () => {
  await withSession({
    ...generic, mcpServers: [server("generic-tools")], toolAllowlist: ["mcp__generic_tools__*"],
    scriptedResponse: (_goal, prior) => prior ? { text: "done" } : {
      tool: { name: "mcp__generic_tools__echo_tool", arguments: { answer: 42 } },
    },
  }, async (session, _sdk, _cwd, events) => {
    await session.prompt(request.goal);
    expect(events.find((event) => event.type === "pi.tool.completed")?.payload).toMatchObject({
      tool_name: "mcp__generic_tools__echo_tool", is_error: false,
      structured_result: { args: { answer: 42 } },
      result: {
        content: [{ type: "text", text: '{"answer":42}' }],
        structuredContent: { args: { answer: 42 } },
      },
    });
    expect(events.some((event) => event.type.startsWith("browser."))).toBe(false);
  });
});

it("retains real supplementary MCP results under a browser Harness mapper", async () => {
  await withSession({
    ...createBrowserHarnessDefinition(), skillPaths: [], formatPrompt: (goal) => goal,
    mcpServers: [server("browser-harness"), server("generic-tools")],
    toolAllowlist: ["mcp__browser_harness__*", "mcp__generic_tools__*"],
    scriptedResponse: (_goal, prior) => prior ? { text: "done" } : {
      tool: { name: "mcp__generic_tools__echo_tool", arguments: { answer: 42 } },
    },
  }, async (session, _sdk, _cwd, events) => {
    await session.prompt(request.goal);
    expect(events.find((event) => event.type === "pi.tool.completed")?.payload).toMatchObject({
      tool_name: "mcp__generic_tools__echo_tool",
      result: {
        content: [{ type: "text", text: '{"answer":42}' }],
        structuredContent: { args: { answer: 42 } },
      },
    });
    expect(events.some((event) => event.type.startsWith("browser."))).toBe(false);
  });
});

it("retains the actual MCP error content and status through the SDK tool pipeline", async () => {
  await withSession({
    ...generic, mcpServers: [server("generic-tools")], toolAllowlist: ["mcp__generic_tools__*"],
    scriptedResponse: (_goal, prior) => prior ? { text: "done" } : {
      tool: { name: "mcp__generic_tools__fail", arguments: {} },
    },
  }, async (session, _sdk, _cwd, events) => {
    await session.prompt(request.goal);
    expect(events.find((event) => event.type === "pi.tool.completed")?.payload).toMatchObject({
      tool_name: "mcp__generic_tools__fail", is_error: true,
      structured_result: { ok: false },
      result: {
        content: [{ type: "text", text: "fixture tool failed" }],
        structuredContent: { ok: false }, isError: true,
      },
    });
  });
});

it("joins the actual Pi SDK stdio child exit after SIGKILL escalation", async () => {
  // Keep only this fixture child alive after EOF and SIGTERM so the real SDK must escalate.
  const script = `
    process.on("SIGTERM", () => {});
    process.exit = () => {};
    setInterval(() => {}, 1000);
    await import(${JSON.stringify(pathToFileURL(fixture).href)});
  `;
  await withSession({
    ...generic, mcpServers: [{
      ...server("stubborn"), args: ["--input-type=module", "-e", script],
    }],
  }, async (session, sdk, cwd) => {
    await sdk.prompt("/mcp");
    const tool = sdk.extensionRunner.getToolDefinition("mcp__stubborn__echo_tool")!;
    const result = await tool.execute(
      "get-child-pid", {}, undefined, undefined,
      sdk.extensionRunner.createToolContext("get-child-pid", undefined),
    );
    const pid = (result.structuredContent as { structuredContent: { pid: number } }).structuredContent.pid;
    let returned = false;
    const closing = session.close();
    void closing.then(() => { returned = true; }, () => { returned = true; });
    await vi.waitFor(async () => expect(await log(cwd)).toContain("disconnected"));
    expect(returned).toBe(false);
    expect(() => process.kill(pid, 0)).not.toThrow();
    await closing;
    expect(() => process.kill(pid, 0)).toThrow(/ESRCH/);
    expect((await log(cwd)).match(/call:close_session/g)).toHaveLength(1);
  });
}, 10_000);
