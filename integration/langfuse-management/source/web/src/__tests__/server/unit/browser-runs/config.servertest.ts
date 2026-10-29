const bridgeEnv = vi.hoisted(() => ({
  AGENTTRACE_API_URL: "http://agenttrace.test",
  AGENTTRACE_BRIDGE_TOKEN: "bridge-token",
  AGENTTRACE_LANGFUSE_PROJECT_ID: "lf-project",
  AGENTTRACE_PROJECT_ID: "at-project",
  AGENTTRACE_PROJECT_BINDINGS: "",
}));

vi.mock("@/src/env.mjs", () => ({ env: bridgeEnv }));

import { readBridgeConfig } from "@/src/features/browser-runs/server/config";

describe("readBridgeConfig", () => {
  it("treats whitespace-only bridge values as absent", () => {
    bridgeEnv.AGENTTRACE_BRIDGE_TOKEN = "   ";

    expect(readBridgeConfig("lf-project")).toBeNull();
    bridgeEnv.AGENTTRACE_BRIDGE_TOKEN = "bridge-token";
  });
  it("uses server-only mapping and no fixed conversation config", () => {
    expect(readBridgeConfig("lf-project")).toEqual({
      apiUrl: "http://agenttrace.test",
      bridgeToken: "bridge-token",
      langfuseProjectId: "lf-project",
      agenttraceProjectId: "at-project",
    });
    expect(readBridgeConfig("foreign")).toBeNull();
    bridgeEnv.AGENTTRACE_PROJECT_BINDINGS = '{"second-lf":"second-at"}';
    expect(readBridgeConfig("second-lf")?.agenttraceProjectId).toBe(
      "second-at",
    );
    bridgeEnv.AGENTTRACE_PROJECT_BINDINGS = '{"second-lf":123}';
    expect(readBridgeConfig("second-lf")).toBeNull();
    bridgeEnv.AGENTTRACE_PROJECT_BINDINGS = "invalid";
    expect(readBridgeConfig("lf-project")).toBeNull();
    bridgeEnv.AGENTTRACE_PROJECT_BINDINGS = "";
  });
});
