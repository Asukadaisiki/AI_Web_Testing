import { expect, it } from "vitest";
import { BrowserEvidenceGuard } from "../src/browser/evidence-guard.js";
import { HarnessEventWriter, type HarnessEvent } from "../src/run-protocol.js";

it("preserves a user-authored canonical password goal but scrubs tool transcript copies", () => {
  const guard = new BrowserEvidenceGuard();
  const secret = "canonical-password-fixture";
  guard.observe({
    type: "tool_execution_start", toolCallId: "password",
    toolName: "mcp__browser_harness__input", args: { value: secret },
  });
  guard.observe({
    type: "tool_execution_end", toolCallId: "password", toolName: "mcp__browser_harness__input",
    result: { structuredContent: { redacted_args: ["value"] } },
  });
  const context = guard.modelContext({ messages: [
    { role: "user", content: `User-authored goal with ${secret}` },
    { role: "assistant", content: [{ type: "toolCall", name: "mcp__browser_harness__input",
      arguments: { value: secret } }] },
  ] });
  expect(context.messages).toEqual([
    { role: "user", content: "User-authored goal with canonical-password-fixture" },
    { role: "assistant", content: [{ type: "toolCall", name: "mcp__browser_harness__input",
      arguments: { value: "[redacted]" } }] },
  ]);
});

it("redacts escaped tool output copies and only learns fields marked by the browser builder", () => {
  const guard = new BrowserEvidenceGuard();
  const secret = 'quoted"\\password-canary';
  const args = { value: secret };
  const observe = (toolName: string, redacted_args?: string[]) => {
    guard.observe({ type: "tool_execution_start", toolCallId: "call", toolName, args });
    guard.observe({ type: "tool_execution_end", toolCallId: "call", toolName,
      result: { structuredContent: { redacted_args } } });
  };
  observe("mcp__generic__input", ["value"]);
  observe("mcp__browser_harness__input");
  expect(guard.redact(args)).toEqual({ value: secret });
  observe("mcp__browser_harness__input", ["value"]);
  expect(guard.redact({ text: JSON.stringify(args), url: `https://test.invalid/?q=${encodeURIComponent(secret)}` }))
    .toEqual({ text: '{"value":"[redacted]"}', url: "https://test.invalid/?q=[redacted]" });
});

it("streams value-free tool starts and grounded observations without waiting for settlement", () => {
  const guard = new BrowserEvidenceGuard();
  const events: HarnessEvent[] = [];
  const writer = guard.publicWriter(new HarnessEventWriter({ run_id: "live", attempt_id: "live" }, {
    write(line) { events.push(JSON.parse(String(line))); return true; },
  }));
  writer.write("pi.tool.started", {
    tool_call_id: "password", tool_name: "mcp__browser_harness__input",
    args: { value: "unclassified-canary" }, prompt: "unclassified-canary",
  });
  expect(JSON.stringify(events)).not.toContain("unclassified-canary");
  guard.observe({ type: "tool_execution_start", toolCallId: "password",
    toolName: "mcp__browser_harness__input", args: { value: "unclassified-canary" } });
  guard.observe({ type: "tool_execution_end", toolCallId: "password",
    toolName: "mcp__browser_harness__input", result: { structuredContent: { redacted_args: ["value"] } } });
  writer.write("browser.observation", {
    tool_call_id: "password", observation: { title: "unclassified-canary", screenshot: "live.png" },
  });
  expect(events.map((event) => event.type)).toEqual(["pi.tool.started", "browser.observation"]);
  expect(events[1]?.payload).toEqual({
    tool_call_id: "password", observation: { title: "[redacted]", screenshot: "live.png" },
  });
});

it.each([
  { maxPendingEvents: 1, maxPendingBytes: 4096 },
  { maxPendingEvents: 256, maxPendingBytes: 128 },
])("bounds pending telemetry and omits unsafe model content on overflow: %j", (limits) => {
  const guard = new BrowserEvidenceGuard(limits);
  const events: HarnessEvent[] = [];
  const writer = guard.publicWriter(new HarnessEventWriter({ run_id: "bound", attempt_id: "bound" }, {
    write(line) { events.push(JSON.parse(String(line))); return true; },
  }));
  const canary = "overflow-password-canary";
  writer.write("pi.message.completed", {
    text: canary.repeat(20), provider: "scripted", model: "fixture",
    input: { messages: [{ role: "user", content: canary }] }, private_note: canary,
  });
  writer.write("pi.usage", { provider: "scripted", model: "fixture", input_tokens: 3, output_tokens: 2 });
  expect(events.length).toBeGreaterThan(0);
  expect(JSON.stringify(events)).not.toContain(canary);
  writer.write("pi.message.completed", { text: canary, provider: "scripted", model: "fixture" });
  writer.write("harness.settled", { assistant_text: canary, verdict: "succeeded" });
  expect(events.map((event) => event.type)).toEqual([
    "pi.message.completed", "pi.usage", "pi.message.completed", "harness.settled",
  ]);
  expect(JSON.stringify(events)).not.toContain(canary);
  expect(events[0]?.payload).toEqual({ text: "", provider: "scripted", model: "fixture" });
  expect(events[1]?.payload).toMatchObject({ input_tokens: 3, output_tokens: 2 });
  expect(events.find((event) => event.type === "harness.settled")?.payload.assistant_text).toBe("");
});

it.each(["failed", "succeeded"] as const)(
  "omits model content after an unclassified browser error even when settlement is %s",
  (verdict) => {
    const guard = new BrowserEvidenceGuard();
    const events: HarnessEvent[] = [];
    const writer = guard.publicWriter(new HarnessEventWriter({ run_id: "error", attempt_id: "error" }, {
      write(line) { events.push(JSON.parse(String(line))); return true; },
    }));
    const canary = "unclassified-error-password-canary";
    writer.write("pi.message.completed", {
      text: `I will enter ${canary}`,
      input: { messages: [{ role: "user", content: `Private goal ${canary}` }] },
    });
    writer.write("pi.usage", { input_tokens: 3, output_tokens: 2 });
    guard.observe({ type: "tool_execution_start", toolCallId: "password",
      toolName: "mcp__browser_harness__input", args: { value: canary } });
    guard.observe({ type: "tool_execution_end", toolCallId: "password",
      toolName: "mcp__browser_harness__input",
      result: { isError: true, content: [{ type: "text", text: "MCP tool execution failed" }] } });
    writer.write("pi.tool.completed", {
      tool_call_id: "password", tool_name: "mcp__browser_harness__input", is_error: true,
    });
    expect(events.map((event) => event.type)).toEqual(["pi.tool.completed"]);
    writer.write("harness.settled", { assistant_text: `Could not enter ${canary}`, verdict });
    expect(JSON.stringify(events)).not.toContain(canary);
    expect(events[2]?.payload).toMatchObject({ input_tokens: 3, output_tokens: 2 });
  },
);
