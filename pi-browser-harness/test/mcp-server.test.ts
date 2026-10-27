import { afterEach, describe, expect, it } from "vitest";

import { Client } from "@modelcontextprotocol/sdk/client/index.js";
import { InMemoryTransport } from "@modelcontextprotocol/sdk/inMemory.js";
import type { CallToolResult } from "@modelcontextprotocol/sdk/types.js";
import type { Transport } from "@modelcontextprotocol/sdk/shared/transport.js";

import {
  createBrowserMcpServer,
  type BrowserMcpBuilder,
} from "../src/mcp/server.js";
import { BROWSER_TOOL_NAMES } from "../src/mcp/schemas.js";
import {
  FatalCaseBuilderError,
  CODE_COMMITTED_PREFIX_REPLAY_FAILED,
  type ToolOutcome,
} from "../src/browser/case-builder.js";
import type { Case, ExecutionResult } from "../src/browser/contract.js";

interface RecordingBuilder extends BrowserMcpBuilder {
  calls: Array<{ name: string; args: Record<string, unknown> }>;
  closeCount: number;
  maxConcurrent: number;
  setOutcome(name: string, outcome: ToolOutcome): void;
  setFatal(name: string, error: FatalCaseBuilderError): void;
}

function makeBuilder(delayMs = 0): RecordingBuilder {
  const outcomes = new Map<string, ToolOutcome>();
  const fatals = new Map<string, FatalCaseBuilderError>();
  const state = { concurrent: 0, maxConcurrent: 0 };
  const calls: Array<{ name: string; args: Record<string, unknown> }> = [];
  const builder = {
    calls,
    closeCount: 0,
    setOutcome(name: string, outcome: ToolOutcome) {
      outcomes.set(name, outcome);
    },
    setFatal(name: string, error: FatalCaseBuilderError) {
      fatals.set(name, error);
    },
    async call(name: string, args: Record<string, unknown>) {
      state.concurrent += 1;
      state.maxConcurrent = Math.max(state.maxConcurrent, state.concurrent);
      if (delayMs > 0) {
        await new Promise((resolve) => setTimeout(resolve, delayMs));
      }
      calls.push({ name, args });
      state.concurrent -= 1;
      const fatal = fatals.get(name);
      if (fatal !== undefined) {
        throw fatal;
      }
      const outcome = outcomes.get(name);
      if (outcome !== undefined) {
        return outcome;
      }
      return { result: { ok: true, summary: `${name} ok` } };
    },
    async close() {
      builder.closeCount += 1;
    },
    get maxConcurrent() {
      return state.maxConcurrent;
    },
  };
  return builder as RecordingBuilder;
}

async function connectPair(builder: BrowserMcpBuilder): Promise<{
  server: ReturnType<typeof createBrowserMcpServer>;
  serverTransport: Transport;
  client: Client;
  clientTransport: InMemoryTransport;
}> {
  const server = createBrowserMcpServer(builder);
  const [serverTransport, clientTransport] =
    InMemoryTransport.createLinkedPair();
  await server.connect(serverTransport);
  const client = new Client({ name: "test-client", version: "1.0.0" });
  await client.connect(clientTransport);
  return { server, serverTransport, client, clientTransport };
}

describe("browser MCP server", () => {
  const transports: Transport[] = [];

  afterEach(async () => {
    for (const transport of transports) {
      try {
        await transport.close();
      } catch {
        // best effort
      }
    }
    transports.length = 0;
  });

  it("advertises the sixteen stable tools in order and no ask_user", async () => {
    const builder = makeBuilder();
    const server = createBrowserMcpServer(builder);
    const [serverTransport, clientTransport] =
      InMemoryTransport.createLinkedPair();
    transports.push(serverTransport, clientTransport);
    await server.connect(serverTransport);

    const client = new Client({ name: "test-client", version: "1.0.0" });
    await client.connect(clientTransport);

    const result = await client.listTools();
    expect(result.tools.map((tool) => tool.name)).toEqual([
      ...BROWSER_TOOL_NAMES,
    ]);
    expect(result.tools.map((tool) => tool.name)).not.toContain("ask_user");

    await client.close();
    await server.close();
  });

  it("serializes concurrent tool calls so only one runs at a time", async () => {
    const builder = makeBuilder(30);
    const server = createBrowserMcpServer(builder);
    const [serverTransport, clientTransport] =
      InMemoryTransport.createLinkedPair();
    transports.push(serverTransport, clientTransport);
    await server.connect(serverTransport);

    const client = new Client({ name: "test-client", version: "1.0.0" });
    await client.connect(clientTransport);

    const calls = Promise.all([
      client.callTool({
        name: "open_page",
        arguments: { url: "https://a.test", intent: "open a" },
      }),
      client.callTool({
        name: "click",
        arguments: { hint: "x", intent: "click x", expect_text: "y" },
      }),
      client.callTool({
        name: "input",
        arguments: { hint: "q", value: "v", intent: "type" },
      }),
    ]);
    await calls;

    expect(builder.maxConcurrent).toBe(1);
    expect(builder.calls).toHaveLength(3);

    await client.close();
    await server.close();
  });

  it("returns model-facing text plus structured content for every result", async () => {
    const builder = makeBuilder();
    builder.setOutcome("open_page", {
      result: {
        ok: true,
        summary: "navigated to https://a.test and recorded goto step 0",
        page: {
          url: "https://a.test",
          title: "A",
          screenshot_path: null,
          truncated: false,
          truncation_reason: null,
          blockers: [],
          scopes: [],
          action_candidates: [],
          elements: [],
        },
        steps: [
          {
            index: 0,
            action: "goto",
            intent: "open a",
            preconditions: [],
            postconditions: ["url_contains:a.test"],
          },
        ],
      },
    });
    const server = createBrowserMcpServer(builder);
    const [serverTransport, clientTransport] =
      InMemoryTransport.createLinkedPair();
    transports.push(serverTransport, clientTransport);
    await server.connect(serverTransport);

    const client = new Client({ name: "test-client", version: "1.0.0" });
    await client.connect(clientTransport);

    const result = (await client.callTool({
      name: "open_page",
      arguments: { url: "https://a.test", intent: "open a" },
    })) as CallToolResult;

    const text = result.content
      .filter((item) => item.type === "text")
      .map((item) => item.text)
      .join("\n");
    expect(text).not.toBe("");
    expect(result.structuredContent).toBeDefined();
    const structured = result.structuredContent as Record<string, unknown>;
    expect(structured.ok).toBe(true);
    expect(structured.summary).toContain("navigated to");
    expect(structured.page).toBeDefined();
    expect(Array.isArray(structured.steps)).toBe(true);

    await client.close();
    await server.close();
  });

  it("exposes stable error code and committed steps on recoverable failures", async () => {
    const builder = makeBuilder();
    builder.setOutcome("click", {
      result: {
        ok: false,
        error: "target_not_found",
        detail: "no element matched",
      },
    });
    const server = createBrowserMcpServer(builder);
    const [serverTransport, clientTransport] =
      InMemoryTransport.createLinkedPair();
    transports.push(serverTransport, clientTransport);
    await server.connect(serverTransport);

    const client = new Client({ name: "test-client", version: "1.0.0" });
    await client.connect(clientTransport);

    const result = (await client.callTool({
      name: "click",
      arguments: { hint: "missing", intent: "click", expect_text: "x" },
    })) as CallToolResult;

    const structured = result.structuredContent as Record<string, unknown>;
    expect(structured.ok).toBe(false);
    expect(structured.error).toBe("target_not_found");
    expect(structured.steps).toEqual([]);

    await client.close();
    await server.close();
  });

  it("finish_case carries the case and dry-run execution", async () => {
    const builder = makeBuilder();
    const artifact: Case = {
      case_version: "loop.case.v1",
      name: "checkout",
      goal: "buy a widget",
      base_url: "https://shop.test",
      steps: [],
    };
    const execution: ExecutionResult = {
      execution_id: "exec_1",
      status: "passed",
      started_at: "2026-10-26T00:00:00.000Z",
      finished_at: "2026-10-26T00:00:01.000Z",
      final_url: "https://shop.test/cart",
      steps: [],
      error: null,
    };
    builder.setOutcome("finish_case", {
      result: {
        ok: true,
        summary: "the case passed a full dry run (0 steps); ready for approval",
        steps: [],
      },
      case: artifact,
      dryRun: execution,
    });
    const server = createBrowserMcpServer(builder);
    const [serverTransport, clientTransport] =
      InMemoryTransport.createLinkedPair();
    transports.push(serverTransport, clientTransport);
    await server.connect(serverTransport);

    const client = new Client({ name: "test-client", version: "1.0.0" });
    await client.connect(clientTransport);

    const result = (await client.callTool({
      name: "finish_case",
      arguments: { name: "checkout" },
    })) as CallToolResult;

    const structured = result.structuredContent as Record<string, unknown>;
    expect(structured.ok).toBe(true);
    expect(structured.case).toEqual(artifact);
    expect(structured.dry_run).toEqual(execution);

    await client.close();
    await server.close();
  });

  it("close() closes the builder exactly once even when called repeatedly", async () => {
    const builder = makeBuilder();
    const server = createBrowserMcpServer(builder);
    const [serverTransport, clientTransport] =
      InMemoryTransport.createLinkedPair();
    transports.push(serverTransport, clientTransport);
    await server.connect(serverTransport);

    await server.close();
    await server.close();
    await server.close();

    expect(builder.closeCount).toBe(1);
  });

  it("fatal builder error closes the builder, invokes onFatal, and surfaces the error to the client without closing the transport", async () => {
    const builder = makeBuilder();
    const fatal = new FatalCaseBuilderError(
      CODE_COMMITTED_PREFIX_REPLAY_FAILED,
      "replay failed",
    );
    builder.setFatal("open_page", fatal);

    const onFatalCalls: FatalCaseBuilderError[] = [];
    const server = createBrowserMcpServer(builder, {
      onFatal: (error) => onFatalCalls.push(error),
    });
    const [serverTransport, clientTransport] =
      InMemoryTransport.createLinkedPair();
    transports.push(serverTransport, clientTransport);
    await server.connect(serverTransport);

    const client = new Client({ name: "test-client", version: "1.0.0" });
    await client.connect(clientTransport);

    const callPromise = client.callTool({
      name: "open_page",
      arguments: { url: "https://a.test", intent: "open a" },
    });

    // The SDK surfaces tool-handler throws as a CallToolResult with isError,
    // not a transport-level rejection. The message must be the fatal error's.
    const result = (await callPromise) as CallToolResult;
    expect(result.isError).toBe(true);
    const text = result.content
      .filter((item) => item.type === "text")
      .map((item) => item.text)
      .join("\n");
    expect(text).toContain("replay failed");

    // builder is closed exactly once
    expect(builder.closeCount).toBe(1);
    // onFatal received the original error
    expect(onFatalCalls).toHaveLength(1);
    expect(onFatalCalls[0]).toBe(fatal);

    // transport is still usable: the SDK wrote an error response rather than
    // dropping the connection. The client can still issue a follow-up call.
    const followUp = await client.callTool({
      name: "click",
      arguments: { hint: "x", intent: "click x", expect_text: "y" },
    });
    expect(followUp).toBeDefined();

    await client.close();
    await server.close();
  });

  it("connect chains an existing transport.onclose handler so process exit still runs", async () => {
    const builder = makeBuilder();
    const server = createBrowserMcpServer(builder);
    const [serverTransport] = InMemoryTransport.createLinkedPair();
    transports.push(serverTransport);

    let exitCalled = false;
    const existingHandler = (): void => {
      exitCalled = true;
    };
    serverTransport.onclose = existingHandler;

    await server.connect(serverTransport);

    // The transport's onclose should now run both the server's closeOnce and
    // the pre-existing handler (which the stdio entry uses for process.exit).
    serverTransport.onclose?.();
    // closeOnce is fire-and-forget; let the microtask flush.
    await new Promise((resolve) => setTimeout(resolve, 0));

    expect(builder.closeCount).toBe(1);
    expect(exitCalled).toBe(true);
  });
});
