import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import fixtureData from "../../../testdata/failure_signal_contract.json";
import type { ExecutionAnalysis, FailureSignal } from "../types/api";
import { ExecutionAnalysisPanel } from "./ExecutionAnalysisPanel";

const fixture = fixtureData as unknown as {
  cases: Array<{ name: string; expected: FailureSignal }>;
  legacy_v1: FailureSignal;
};

function analysisWith(signal: FailureSignal): ExecutionAnalysis {
  return {
    source: "deterministic",
    summary: "执行失败",
    conclusion: "all_failed",
    case_results: [],
    failure_details: [],
    failure_signals: [signal],
    recommended_action: "manual",
  };
}

describe("ExecutionAnalysisPanel", () => {
  it("兼容展示 v1 failure signal", () => {
    render(<ExecutionAnalysisPanel analysis={analysisWith(fixture.legacy_v1)} />);

    expect(screen.getByText("locator")).toBeInTheDocument();
    expect(screen.getByText(/Element not found/)).toBeInTheDocument();
    expect(screen.queryByText(/来源：执行/)).not.toBeInTheDocument();
  });

  it("展示 v2 结构化分类和直接执行来源", () => {
    const signal = fixture.cases.find(({ name }) => name === "postcondition")?.expected;
    if (!signal) {
      throw new Error("postcondition fixture is missing");
    }

    render(<ExecutionAnalysisPanel analysis={analysisWith(signal)} />);

    expect(screen.getByText("postcondition")).toBeInTheDocument();
    expect(
      screen.getByText("condition.postcondition.text_visible.failed"),
    ).toBeInTheDocument();
    expect(screen.getByText("副作用已提交")).toBeInTheDocument();
    expect(
      screen.getByText("来源：执行 #103/steps/3/condition_results/0"),
    ).toBeInTheDocument();
  });

  it("明确展示未知副作用且不伪造 Agent event", () => {
    const signal = fixture.cases.find(
      ({ name }) => name === "unknown_side_effect",
    )?.expected;
    if (!signal) {
      throw new Error("unknown-side-effect fixture is missing");
    }

    render(<ExecutionAnalysisPanel analysis={analysisWith(signal)} />);

    expect(screen.getByText("副作用未知")).toBeInTheDocument();
    expect(screen.queryByText(/Agent/)).not.toBeInTheDocument();
  });
});
