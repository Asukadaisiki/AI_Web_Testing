import {
  BugOutlined,
  ExperimentOutlined,
  FileTextOutlined,
  ProjectOutlined,
} from "@ant-design/icons";
import { useLocation, useNavigate } from "react-router-dom";

const NAV_ITEMS = [
  { key: "/planning", label: "AI 规划", icon: <ExperimentOutlined /> },
  { key: "/cases", label: "用例中心", icon: <ProjectOutlined /> },
  { key: "/regression", label: "回归编排", icon: <FileTextOutlined /> },
  { key: "/locator-debug", label: "定位调试", icon: <BugOutlined /> },
  { key: "/reports", label: "报告", icon: <FileTextOutlined /> },
] as const;

function isActive(currentPath: string, navKey: string): boolean {
  return currentPath.startsWith(navKey);
}

export function NotebookNav() {
  const location = useLocation();
  const navigate = useNavigate();

  return (
    <div
      className="notebook-nav"
      style={{
        borderTop: "1px solid #f0f0f0",
        paddingTop: 8,
        marginTop: 8,
        display: "flex",
        flexDirection: "column",
        gap: 4,
      }}
    >
      {NAV_ITEMS.map((item) => {
        const active = isActive(location.pathname, item.key);
        return (
          <button
            type="button"
            key={item.key}
            onClick={() => navigate(item.key)}
            aria-current={active ? "page" : undefined}
            style={{
              border: 0,
              width: "100%",
              display: "flex",
              alignItems: "center",
              gap: 8,
              padding: "6px 10px",
              borderRadius: 8,
              fontSize: 12,
              cursor: "pointer",
              background: active ? "#1a1a2e" : "transparent",
              color: active ? "#fff" : "#666",
              transition: "background 0.15s",
            }}
          >
            <span style={{ fontSize: 14 }}>{item.icon}</span>
            <span>{item.label}</span>
          </button>
        );
      })}
    </div>
  );
}
