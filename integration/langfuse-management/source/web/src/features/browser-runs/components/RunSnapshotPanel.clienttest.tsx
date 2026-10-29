import { fireEvent, render, screen } from "@testing-library/react";
import { RunSnapshotPanel } from "./RunSnapshotPanel";
import type { AgentTraceRun } from "../contracts";

const run: AgentTraceRun = {
  id: "run-1", project_id: "p1", conversation_id: "c1", status: "failed", checkpoint_version: 1,
  created_at: "2026-10-28T00:00:00Z", updated_at: "2026-10-28T00:00:00Z",
  runtime_id: "pi", environment_id: "env-1", profile_generation: 2,
  model: { provider: "provider", name: "recorded-model", reasoning_effort: "high" },
  harness_snapshot: {
    harness_id: "h1", harness_version_id: "hv1", revision: 2, name: "Checkout", project_id: "p1",
    content_digest: "digest", runtime_ids: ["pi"], skill_version_ids: ["sv1"], mcp_binding_ids: ["b1"],
    runtime_id: "pi", environment_id: "env-1",
    skills: [{ name: "Login", version_id: "sv1", content_digest: "skill-digest" }],
    mcp: { browser: { capability: "browser-harness", version_id: "m1", content_digest: "mcp-digest" } },
  },
};
it("renders persisted version, model and environment provenance without a registry lookup", () => {
  render(<RunSnapshotPanel run={run} />);
  fireEvent.click(screen.getByText("Immutable execution snapshot"));
  expect(screen.getByText("hv1")).toBeInTheDocument();
  expect(screen.getByText("provider / recorded-model / high")).toBeInTheDocument();
  expect(screen.getByText("Profile generation")).toBeInTheDocument();
  expect(screen.getByText("Login")).toBeInTheDocument();
  expect(screen.getByText("b1")).toBeInTheDocument();
  expect(screen.getByText("m1")).toBeInTheDocument();
  expect(screen.getByText(/Persisted at run creation/)).toBeInTheDocument();
});
it("keeps historical missing provenance explicit rather than inferring current registry values", () => {
  render(<RunSnapshotPanel run={{ ...run, harness_snapshot: undefined, model: undefined }} />);
  expect(screen.getByText(/Snapshot provenance was not recorded/)).toBeInTheDocument();
});
