import { describe, expect, it } from "vitest";
import { createBrowserHarnessDefinition } from "../src/browser-harness.js";
import { parseRunRequest } from "../src/run-protocol.js";

const generic = {
  protocol_version: "pi-browser-harness.v1", run_id: "run", attempt_id: "attempt",
  goal: "answer", model: { provider: "scripted", name: "fixture", reasoning_effort: "off" },
};

describe("browser request boundary", () => {
  it.each([
    [generic, "worker_url"],
    [{ ...generic, worker_url: "http://worker:8100" }, "artifact_scope"],
    [{ ...generic, artifact_scope: "scope" }, "worker_url"],
  ])("rejects missing %s browser configuration before launching MCP", (input, field) => {
    const definition = createBrowserHarnessDefinition();
    const servers = definition.mcpServers;
    expect(typeof servers).toBe("function");
    expect(() => (servers as Exclude<typeof servers, readonly unknown[]>)(parseRunRequest(JSON.stringify(input))))
      .toThrow(`browser harness requires ${field}`);
  });

  it("passes validated browser fields to the MCP child", () => {
    const definition = createBrowserHarnessDefinition();
    const servers = definition.mcpServers;
    const request = parseRunRequest(JSON.stringify({
      ...generic, worker_url: "http://worker:8100", artifact_scope: "scope",
    }));
    expect(typeof servers === "function" ? servers(request)[0]?.env : undefined).toMatchObject({
      AGENTTRACE_HARNESS_WORKER_URL: "http://worker:8100",
      AGENTTRACE_HARNESS_ARTIFACT_SCOPE: "scope",
    });
  });

  it.each(["/worker", "ftp://worker", " "])("rejects invalid worker URL %s at the browser boundary", (worker_url) => {
    const servers = createBrowserHarnessDefinition().mcpServers;
    expect(() => typeof servers === "function" && servers({
      ...generic, protocol_version: "pi-browser-harness.v1", worker_url, artifact_scope: "scope",
    })).toThrow("worker_url");
  });
});
