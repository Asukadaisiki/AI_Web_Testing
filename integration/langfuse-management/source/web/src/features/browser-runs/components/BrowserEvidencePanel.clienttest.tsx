import { render, screen } from "@testing-library/react";
import Image from "next/image";
import { BrowserEvidencePanel } from "./BrowserEvidencePanel";
import type { AgentTrace } from "../contracts";

vi.mock("@/src/components/ui/CodeJsonViewer", () => ({
  JSONView: ({ title, json }: { title: string; json: unknown }) => (
    <section>
      <h3>{title}</h3>
      <pre>{JSON.stringify(json)}</pre>
    </section>
  ),
}));
vi.mock("@/src/components/ui/resizable-image", () => ({
  ResizableImage: ({ src, alt }: { src: string; alt: string }) => (
    <Image src={src} alt={alt} width={100} height={100} unoptimized />
  ),
}));
const time = "2026-10-28T00:00:00Z";
const trace: AgentTrace = {
  spans: [
    {
      id: "span_1",
      span_id: "span_1",
      trace_id: "trace_1",
      run_id: "run_1",
      kind: "operation",
      name: "finish_case",
      status: "succeeded",
      input: {
        case: {
          case_version: "loop.case.v1",
          name: "Checkout",
          goal: "Complete checkout",
          base_url: "https://shop.test",
          steps: [],
        },
      },
      started_at: time,
      created_at: time,
      updated_at: time,
    },
  ],
  operation_evidence: [
    {
      id: "ev_1",
      operation_id: "op_1",
      span_id: "span_1",
      status: "passed",
      result: {
        execution_id: "execution_1",
        status: "passed",
        started_at: time,
        finished_at: time,
        final_url: "https://shop.test/cart",
        steps: [],
        error: null,
      },
      artifacts: { screenshot_paths: ["run_1/shot.png", "other/shot.png"] },
      created_at: time,
      updated_at: time,
    },
  ],
  attempts: [],
  usage: [],
  derived_nodes: [],
};
describe("BrowserEvidencePanel", () => {
  it("renders canonical Case, dry run and operation result with only scoped screenshots", () => {
    render(
      <BrowserEvidencePanel
        projectId="lf-project"
        runId="run_1"
        trace={trace}
      />,
    );
    expect(screen.getByText("Case span_1")).toBeInTheDocument();
    expect(screen.getByText("Dry run execution_1")).toBeInTheDocument();
    expect(screen.getByText("finish_case input")).toBeInTheDocument();
    expect(screen.getByText("finish_case result ev_1")).toBeInTheDocument();
    expect(screen.getAllByRole("img")).toHaveLength(1);
    expect(screen.getByRole("img")).toHaveAttribute(
      "src",
      "/api/agenttrace/artifacts/run_1?projectId=lf-project&path=run_1%2Fshot.png",
    );
    expect(
      screen.queryByText(
        /Latest verdict|Repair attempts|Public assistant explanation/,
      ),
    ).not.toBeInTheDocument();
  });
  it("keeps missing canonical evidence distinct from exporter availability", () => {
    const { rerender } = render(
      <BrowserEvidencePanel
        projectId="lf"
        runId="run_1"
        trace={{
          spans: [],
          attempts: [],
          usage: [],
          operation_evidence: [],
          derived_nodes: [],
        }}
      />,
    );
    expect(
      screen.getByText("Case evidence not available."),
    ).toBeInTheDocument();
    expect(
      screen.getByText("Dry-run evidence not available."),
    ).toBeInTheDocument();
    rerender(
      <BrowserEvidencePanel
        projectId="lf"
        runId="run_1"
        trace={undefined}
        error="Transport unavailable"
      />,
    );
    expect(
      screen.getByText("Canonical evidence unavailable"),
    ).toBeInTheDocument();
    expect(screen.getByText("Transport unavailable")).toBeInTheDocument();
  });
});
