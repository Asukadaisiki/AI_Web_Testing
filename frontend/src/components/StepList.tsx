import { PlusOutlined, SearchOutlined } from "@ant-design/icons";
import { Button, Input, Typography } from "antd";

import type { DSLStep } from "../types/api";

interface StepListProps {
  steps: DSLStep[];
  activeIndex: number;
  onSelect: (index: number) => void;
  onAdd: () => void;
  searchValue: string;
  onSearchChange: (value: string) => void;
}

function getActionLabel(action: string) {
  const labels: Record<string, string> = {
    goto: "GO",
    click: "CL",
    input: "IN",
    wait_for: "WT",
    assert_text: "AT",
    assert_url_contains: "AU",
  };
  return labels[action] ?? "ST";
}

export function StepList({ steps, activeIndex, onSelect, onAdd, searchValue, onSearchChange }: StepListProps) {
  const filteredSteps = steps
    .map((step, index) => ({ step, index }))
    .filter(({ step, index }) => {
      const query = searchValue.trim().toLowerCase();
      if (!query) {
        return true;
      }
      return [index + 1, step.action, step.target ?? "", step.value ?? ""]
        .join(" ")
        .toLowerCase()
        .includes(query);
    });

  return (
    <div style={{ display: "flex", flexDirection: "column", height: "100%" }}>
      <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", marginBottom: 12 }}>
        <Typography.Text strong style={{ fontSize: 14 }}>
          Test Steps
        </Typography.Text>
        <Typography.Text type="secondary" style={{ fontSize: 11 }}>
          {steps.length} 步
        </Typography.Text>
      </div>

      <Input
        prefix={<SearchOutlined style={{ color: "#bbb" }} />}
        placeholder="搜索步骤..."
        value={searchValue}
        onChange={(event) => onSearchChange(event.target.value)}
        style={{ borderRadius: 24, background: "#f0f4f8", marginBottom: 12 }}
        variant="borderless"
      />

      <div style={{ flex: 1, overflowY: "auto", paddingRight: 4 }} className="panel-scroll">
        {filteredSteps.map(({ step, index }) => (
          <div
            key={`${step.action}-${index}`}
            onClick={() => onSelect(index)}
            className={`step-item ${index === activeIndex ? "step-item-active" : ""}`}
          >
            <span style={{ display: "inline-block", minWidth: 22, marginRight: 6, color: "#666", fontSize: 11 }}>
              {getActionLabel(step.action)}
            </span>
            <strong>{`#${index + 1}`}</strong>
            <span style={{ color: "#666", marginLeft: 6 }}>{step.action}</span>
            {step.target ? (
              <span style={{ color: "#999", marginLeft: 6 }}>{String(step.target).slice(0, 18)}</span>
            ) : null}
          </div>
        ))}
      </div>

      <div style={{ marginTop: 8, paddingTop: 8, borderTop: "1px solid #f0f0f0" }}>
        <Button
          aria-label="Add Action"
          icon={<PlusOutlined />}
          block
          style={{ borderRadius: 10, background: "#1a1a2e", color: "#fff", border: "none" }}
          onClick={onAdd}
        >
          Add Action
        </Button>
      </div>
    </div>
  );
}
