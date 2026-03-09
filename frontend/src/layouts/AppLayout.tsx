import { Layout, Menu, Typography } from "antd";
import type { ItemType } from "antd/es/menu/interface";
import { Link, Outlet, useLocation } from "react-router-dom";

const { Content, Header, Sider } = Layout;

const items: ItemType[] = [
  {
    key: "/cases",
    label: <Link to="/cases">用例列表</Link>,
  },
  {
    key: "/executions",
    label: <Link to="/executions">执行报告</Link>,
  },
];

export function AppLayout() {
  const location = useLocation();
  const selectedKey = location.pathname.startsWith("/executions") ? "/executions" : "/cases";

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
          <Typography.Text style={{ color: "#9db0cb" }}>前端可演示闭环 v1</Typography.Text>
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
