import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";

import { LocatorEvidencePanel, getCandidateCorrection } from "./LocatorEvidencePanel";
import type {
  LocatorCandidateEvidence,
  StepExecutionEvidence,
  StoredCaseExecutionDetail,
} from "../types/api";

const candidate: LocatorCandidateEvidence = {
  strategy: "role",
  preview_text: "提交",
  role: "button",
  attributes: {
    aria_label: "提交",
    placeholder: null,
    data_testid: "submit-order",
  },
  score: 0.96,
  matched_rules: ["role", "text"],
  rejected_reasons: [],
  visible: true,
  enabled: true,
};

const step: StepExecutionEvidence = {
  step_index: 1,
  action: "click",
  target: "提交按钮",
  status: "passed",
  locator_trace: {
    target: "提交按钮",
    match_strategy: "role",
    selection_reason: "最高确定性分数",
    candidates: [candidate],
    selected_candidate: candidate,
    failure_reason: null,
  },
  url: "https://example.test/checkout",
  console_events: [],
  network_events: [],
};

const execution = {
  id: 42,
  case_id: 7,
  case_name: "下单",
  project_id: 3,
  attempt_number: 1,
  report_schema_version: "execution.report.v1",
  triggered_by: 9,
  status: "passed",
  error_message: null,
  started_at: "2026-09-05T10:00:00Z",
  finished_at: "2026-09-05T10:00:01Z",
  total_steps: 1,
  report: { status: "passed", steps: [step] },
  analysis_status: "completed",
} satisfies StoredCaseExecutionDetail;

describe("LocatorEvidencePanel", () => {
  it("展示 target、候选和 final match", () => {
    render(
      <QueryClientProvider client={new QueryClient()}>
        <LocatorEvidencePanel execution={execution} step={step} />
      </QueryClientProvider>,
    );
    expect(screen.getAllByText("提交按钮").length).toBeGreaterThan(0);
    expect(screen.getByText("role / 提交")).toBeInTheDocument();
    expect(screen.getByText("score=0.96")).toBeInTheDocument();
  });

  it("可从 data-testid 候选填入修正值", async () => {
    const user = userEvent.setup();
    render(
      <QueryClientProvider client={new QueryClient()}>
        <LocatorEvidencePanel execution={execution} step={step} />
      </QueryClientProvider>,
    );
    await user.click(screen.getByRole("button", { name: "用于修正" }));
    expect(screen.getByLabelText("修正值")).toHaveValue("submit-order");
    expect(
      screen.getByRole("combobox", { name: "修正类型" }).closest(".ant-select"),
    ).toHaveTextContent("Test ID");
  });
});

describe("getCandidateCorrection", () => {
  it("只为确定的 test id 生成建议", () => {
    expect(getCandidateCorrection(candidate)).toEqual({
      type: "test_id",
      value: "submit-order",
    });
    expect(
      getCandidateCorrection({
        ...candidate,
        attributes: { ...candidate.attributes, data_testid: null },
      }),
    ).toBeNull();
  });
});
