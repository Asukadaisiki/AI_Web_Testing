import { Layout, Menu, Typography } from "antd";
import type { ItemType } from "antd/es/menu/interface";
import { Link, Outlet, useLocation } from "react-router-dom";

const { Content, Header, Sider } = Layout;

const items: ItemType[] = [
  {
    key: "/dashboard",
    label: <Link to="/dashboard">仪表盘</Link>,
  },
  {
    key: "/cases",
    label: <Link to="/cases">用例列表</Link>,
  },
  {
    key: "/suites",
    label: <Link to="/suites">Suite 管理</Link>,
  },
  {
    key: "/executions",
    label: <Link to="/executions">执行中心</Link>,
  },
  {
    key: "/corrections",
    label: <Link to="/corrections">修正记录</Link>,
  },
  {
    key: "/settings/ai",
    label: <Link to="/settings/ai">AI 配置</Link>,
  },
  {
    key: "/reports",
    label: <Link to="/reports">报告中心</Link>,
  },
];

export function AppLayout() {
  const location = useLocation();
  let selectedKey = "/dashboard";
  if (location.pathname.startsWith("/cases")) {
    selectedKey = "/cases";
  } else if (location.pathname.startsWith("/suites")) {
    selectedKey = "/suites";
  } else if (location.pathname.startsWith("/executions")) {
    selectedKey = "/executions";
  } else if (location.pathname.startsWith("/corrections")) {
    selectedKey = "/corrections";
  } else if (location.pathname.startsWith("/settings/ai")) {
    selectedKey = "/settings/ai";
  } else if (location.pathname.startsWith("/reports")) {
    selectedKey = "/reports";
  }

  return (
    <Layout style={{ minHeight: "100vh", background: "transparent" }}>
      <Sider
        breakpoint="lg"
        collapsedWidth="0"
        style={{
          background: "linear-gradient(180deg, #12223b 0%, #101a2d 100%)",
          boxShadow: "10px 0 30px rgba(11, 20, 38, 0.18)",
        }}
      >
        <div style={{ padding: 24 }}>
          <Typography.Title level={4} style={{ color: "#f7fbff", margin: 0 }}>
            AI Web Testing
          </Typography.Title>
          <Typography.Text style={{ color: "#9db0cb" }}>混合定位稳定化 v3.4</Typography.Text>
        </div>
        <Menu
          mode="inline"
          selectedKeys={[selectedKey]}
          items={items}
          style={{ background: "transparent", color: "#dce7f6", borderInlineEnd: "none" }}
          theme="dark"
        />
      </Sider>
      <Layout style={{ background: "transparent" }}>
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
          <Typography.Text strong>平台演示入口</Typography.Text>
          <Typography.Text type="secondary">后端执行为准，前端负责触发与展示</Typography.Text>
        </Header>
        <Content className="page-shell">
          <Outlet />
        </Content>
      </Layout>
    </Layout>
  );
}
