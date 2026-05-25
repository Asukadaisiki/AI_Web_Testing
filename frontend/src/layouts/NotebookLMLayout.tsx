import React from "react";

type NotebookLMLayoutProps = {
  leftPanel: React.ReactNode;
  centerPanel: React.ReactNode;
  rightCards?: React.ReactNode;
  navBottom?: boolean;
};

export function NotebookLMLayout({ leftPanel, centerPanel, rightCards, navBottom }: NotebookLMLayoutProps) {
  return (
    <div style={{ display: "flex", height: "100%", gap: 16, padding: 16 }}>
      <div style={{ width: 280, flexShrink: 0, overflow: "auto" }}>{leftPanel}</div>
      <div style={{ flex: 1, overflow: "auto" }}>{centerPanel}</div>
      {rightCards && <div style={{ width: 320, flexShrink: 0, overflow: "auto" }}>{rightCards}</div>}
    </div>
  );
}
