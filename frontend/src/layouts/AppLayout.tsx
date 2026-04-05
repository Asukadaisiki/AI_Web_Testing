import { Layout, Steps, Typography } from "antd";
import { Link, Outlet, useLocation } from "react-router-dom";

const { Content, Header } = Layout;

const demoSteps = [
  { key: "/", title: "步骤 1", description: "AI 规划" },
  { key: "/cases", title: "步骤 2", description: "AI 用例" },
  { key: "/run", title: "步骤 3", description: "执行与报告" },
];

function getCurrentStep(pathname: string): number {
  if (pathname === "/") return 0;
  if (pathname.startsWith("/cases")) return 1;
  if (pathname.startsWith("/run") || pathname.startsWith("/executions")) return 2;
  return 0;
}

export function AppLayout() {
  const location = useLocation();
  const current = getCurrentStep(location.pathname);

  return (
    <Layout style={{ minHeight: "100vh", background: "transparent" }}>
      <Header
        style={{
          background: "rgba(255, 255, 255, 0.72)",
          backdropFilter: "blur(14px)",
          borderBottom: "1px solid rgba(189, 201, 219, 0.7)",
          display: "flex",
          alignItems: "center",
          justifyContent: "space-between",
          paddingInline: 24,
        }}
      >
        <Link to="/" style={{ textDecoration: "none" }}>
          <Typography.Title level={4} style={{ margin: 0, color: "#12223b" }}>
            AI Web Testing
          </Typography.Title>
        </Link>
        <Steps
          current={current}
          size="small"
          style={{ maxWidth: 480 }}
          items={demoSteps.map((step) => ({
            title: (
              <Link to={step.key} style={{ textDecoration: "none", color: "inherit" }}>
                {step.title}
              </Link>
            ),
            description: step.description,
          }))}
        />
      </Header>
      <Content className="page-shell">
        <Outlet />
      </Content>
    </Layout>
  );
}
