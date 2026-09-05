import { LockOutlined, MailOutlined } from "@ant-design/icons";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Alert, Button, Form, Input, Typography } from "antd";
import { Navigate, useLocation, useNavigate } from "react-router-dom";

import { currentUserQueryKey } from "./AuthGuard";
import { getCurrentUser, login } from "./api";
import type { LoginPayload } from "./types";

type LoginLocationState = {
  from?: string;
};

export function getSafeDestination(state: LoginLocationState | null): string {
  const candidate = state?.from;
  if (
    typeof candidate !== "string"
    || !candidate.startsWith("/")
    || candidate.startsWith("//")
    || candidate.includes("\\")
  ) {
    return "/planning";
  }
  return candidate;
}

export function LoginPage() {
  const location = useLocation();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const currentUserQuery = useQuery({
    queryKey: currentUserQueryKey,
    queryFn: async () => {
      try {
        return await getCurrentUser();
      } catch {
        return null;
      }
    },
    retry: false,
  });
  const destination = getSafeDestination(
    location.state as LoginLocationState | null,
  );

  const loginMutation = useMutation({
    mutationFn: (payload: LoginPayload) => login(payload),
    onSuccess: (user) => {
      queryClient.setQueryData(currentUserQueryKey, user);
      navigate(destination, { replace: true });
    },
  });

  if (currentUserQuery.data) {
    return <Navigate to={destination} replace />;
  }

  return (
    <main
      style={{
        minHeight: "100vh",
        display: "grid",
        placeItems: "center",
        padding: 24,
        background: "#f8f9fa",
      }}
    >
      <section style={{ width: "min(100%, 360px)" }}>
        <Typography.Title level={2} style={{ marginBottom: 8 }}>
          AI Web Testing
        </Typography.Title>
        <Typography.Paragraph type="secondary" style={{ marginBottom: 24 }}>
          登录测试工作台
        </Typography.Paragraph>
        {loginMutation.error ? (
          <Alert
            type="error"
            showIcon
            message={
              loginMutation.error instanceof Error
                ? loginMutation.error.message
                : "登录失败"
            }
            style={{ marginBottom: 16 }}
          />
        ) : null}
        <Form<LoginPayload>
          layout="vertical"
          requiredMark={false}
          onFinish={(values) => loginMutation.mutate(values)}
        >
          <Form.Item
            name="email"
            label="邮箱"
            rules={[
              { required: true, message: "请输入邮箱" },
              { type: "email", message: "请输入有效邮箱" },
            ]}
          >
            <Input
              autoComplete="username"
              prefix={<MailOutlined />}
              placeholder="name@example.com"
            />
          </Form.Item>
          <Form.Item
            name="password"
            label="密码"
            rules={[{ required: true, message: "请输入密码" }]}
          >
            <Input.Password
              autoComplete="current-password"
              prefix={<LockOutlined />}
              placeholder="请输入密码"
            />
          </Form.Item>
          <Button
            block
            type="primary"
            htmlType="submit"
            aria-label="登录"
            loading={loginMutation.isPending}
          >
            登录
          </Button>
        </Form>
      </section>
    </main>
  );
}
