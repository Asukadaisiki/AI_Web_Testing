import { mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { Writable } from "node:stream";

import {
  createAgentSession,
  DefaultResourceLoader,
  ModelRuntime,
  type AgentSession,
} from "@earendil-works/pi-coding-agent";
import { describe, expect, it, vi } from "vitest";

import { BROWSER_TOOL_NAMES } from "../src/mcp/schemas.js";
import { createBrowserSession } from "../src/pi-session.js";
import { HarnessEventWriter, type RunRequest } from "../src/run-protocol.js";

// A real stdio child exercises Pi's MCP initialization and tools/list discovery.
// Absolute SDK URLs let the child run in an isolated directory without installs.
const MCP_CHILD = `
import { Server } from ${JSON.stringify(import.meta.resolve("@modelcontextprotocol/sdk/server/index.js"))};
import { StdioServerTransport } from ${JSON.stringify(import.meta.resolve("@modelcontextprotocol/sdk/server/stdio.js"))};
import { ListToolsRequestSchema } from ${JSON.stringify(import.meta.resolve("@modelcontextprotocol/sdk/types.js"))};

const names = JSON.parse(process.env.MCP_TEST_TOOLS ?? ${JSON.stringify(JSON.stringify(BROWSER_TOOL_NAMES))});
const server = new Server(
  { name: "sequential-registration-fixture", version: "1.0.0" },
  { capabilities: { tools: {} } },
);
server.setRequestHandler(ListToolsRequestSchema, async () => ({
  tools: names.map((name) => ({
    name,
    description: "Fixture " + name,
    inputSchema: {
      type: "object",
      properties: { value: { type: "string" } },
      required: ["value"],
    },
    annotations: { readOnlyHint: true, destructiveHint: false },
  })),
}));
const transport = new StdioServerTransport();
transport.onclose = () => process.exit(0);
await server.connect(transport);
`;

describe("sequential browser MCP registration", () => {
  it("uses Pi 1.4.0's real loader and MCP discovery without changing unrelated tools", async () => {
    const cwd = mkdtempSync(join(tmpdir(), "pi-sequential-mcp-"));
    const childPath = join(cwd, "mcp-child.mjs");
    let piSession: AgentSession | undefined;
    let unsubscribeErrors: (() => void) | undefined;
    const errors: unknown[] = [];

    try {
      vi.stubEnv("PI_CODING_AGENT_DIR", cwd);
      writeFileSync(childPath, MCP_CHILD);
      writeFileSync(join(cwd, "mcp.json"), JSON.stringify({
        mcpServers: {
          other: {
            command: process.execPath,
            args: [childPath],
            env: {
              MCP_TEST_TOOLS: JSON.stringify([
                "lookup", "deferred_lookup", "hidden_lookup",
              ]),
            },
            exposure: "direct",
            toolExposure: {
              deferred_lookup: "deferred",
              hidden_lookup: "hidden",
            },
          },
        },
      }));
      const request: RunRequest = {
        protocol_version: "pi-browser-harness.v1",
        run_id: "sequential_run",
        attempt_id: "sequential_attempt",
        goal: "verify sequential registration",
        model: {
          provider: "scripted",
          name: "browser-fixture",
          reasoning_effort: "high",
        },
        worker_url: "http://unused.invalid",
        artifact_scope: "sequential_run",
      };
      const runtime = await ModelRuntime.create({ refreshOnCreate: false });
      const output = new Writable({
        write(_chunk, _encoding, callback) {
          callback();
        },
      });

      await createBrowserSession(request, {
        cwd,
        mcpChildPath: childPath,
        modelRuntime: runtime,
        eventWriter: new HarnessEventWriter(request, output),
        createAgentSession: async (options) => {
          expect(options.resourceLoader).toBeInstanceOf(DefaultResourceLoader);
          expect(options.resourceLoader!.getExtensions().errors).toEqual([]);
          const result = await createAgentSession(options);
          piSession = result.session;
          unsubscribeErrors = piSession.extensionRunner.onError((error) => {
            errors.push(error);
          });
          return result;
        },
      });
      const session = piSession!;

      // /mcp awaits the built-in extension's pending discovery, without a model call.
      await session.prompt("/mcp");
      const browserNames = BROWSER_TOOL_NAMES.map(
        (name) => `mcp__browser_harness__${name}`,
      );
      expect(session.getAllTools()
        .filter((tool) => tool.name.startsWith("mcp__browser_harness__"))
        .map((tool) => tool.name).sort()).toEqual([...browserNames].sort());
      expect(session.getActiveToolNames().sort()).toEqual([...browserNames].sort());
      expect(errors).toEqual([]);

      // MCP and lifecycle registrations belong to different factory APIs.
      const loaded = session.resourceLoader.getExtensions();
      const mcpExtensions = loaded.extensions.filter(
        (extension) => extension.commands.has("mcp"),
      );
      expect(mcpExtensions).toHaveLength(1);
      const mcpExtension = mcpExtensions[0]!;
      const lifecyclePath = loaded.runtime.mcpServers
        .get("browser-harness")!.extensionPath;
      expect(mcpExtension.path).not.toBe(lifecyclePath);

      for (const [name, exposure] of [
        ["lookup", "direct"],
        ["deferred_lookup", "deferred"],
        ["hidden_lookup", "hidden"],
      ] as const) {
        const definition = session.extensionRunner
          .getToolDefinition(`mcp__other__${name}`)!;
        expect(definition).toMatchObject({
          name: `mcp__other__${name}`,
          label: `other/${name}`,
          description: `Fixture ${name}`,
          exposure,
          parameters: {
            type: "object",
            properties: { value: { type: "string" } },
            required: ["value"],
          },
          annotations: { readOnlyHint: true, destructiveHint: false },
        });
        expect(definition.executionMode).toBeUndefined();
      }
      expect(browserNames.map((name) => ({
        name,
        executionMode: session.getToolDefinition(name)!.executionMode,
      }))).toEqual(browserNames.map((name) => ({
        name,
        executionMode: "sequential",
      })));
    } finally {
      if (piSession !== undefined) {
        await piSession.extensionRunner.emit({
          type: "session_shutdown",
          reason: "quit",
        });
        unsubscribeErrors?.();
        piSession.dispose();
      }
      vi.unstubAllEnvs();
      rmSync(cwd, { recursive: true, force: true });
    }
  }, 30_000);
});
