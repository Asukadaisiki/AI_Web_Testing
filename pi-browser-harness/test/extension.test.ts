import { describe, expect, it, vi } from "vitest";

import { createSequentialBrowserMcpToolsExtension } from "../src/extension.js";

describe("createSequentialBrowserMcpToolsExtension", () => {
  it("registers every browser MCP tool with Pi's sequential execution mode", () => {
    const registered: Array<Record<string, unknown>> = [];
    const pi = {
      registerTool: vi.fn((tool: Record<string, unknown>) => {
        registered.push(tool);
      }),
    };

    createSequentialBrowserMcpToolsExtension()(pi as never);
    pi.registerTool({ name: "mcp__browser_harness__click" });
    pi.registerTool({ name: "mcp__other__lookup" });
    pi.registerTool({
      name: "mcp__browser_harness__click",
      exposure: "hidden",
    });

    expect(registered).toEqual([
      {
        name: "mcp__browser_harness__click",
        executionMode: "sequential",
      },
      { name: "mcp__other__lookup" },
      {
        name: "mcp__browser_harness__click",
        exposure: "hidden",
        executionMode: "sequential",
      },
    ]);
  });
});
