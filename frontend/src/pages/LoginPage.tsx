import { Alert, Button, Card, Form, Input, Typography } from "antd";
import { useState } from "react";
import { Navigate, useNavigate } from "react-router-dom";

import { useAuth } from "../auth/AuthContext";

interface LoginFormValues {
  email: string;
  password: string;
}

export function LoginPage() {
  const navigate = useNavigate();
  const { isAuthResolved, isAuthenticated, login } = useAuth();
  const [errorMessage, setErrorMessage] = useState<string | null>(null);
  const [isSubmitting, setIsSubmitting] = useState(false);

  if (isAuthResolved && isAuthenticated) {
    return <Navigate to="/dashboard" replace />;
  }

  async function handleFinish(values: LoginFormValues) {
    setErrorMessage(null);
    setIsSubmitting(true);
    try {
      await login(values);
      navigate("/dashboard", { replace: true });
    } catch (error) {
      setErrorMessage(error instanceof Error ? error.message : "登录失败，请稍后重试。");
    } finally {
      setIsSubmitting(false);
    }
  }

  return (
    <div
      style={{
        minHeight: "100vh",
        display: "grid",
        placeItems: "center",
        background:
          "radial-gradient(circle at top left, rgba(20, 71, 230, 0.18), transparent 38%), linear-gradient(135deg, #edf4ff 0%, #f8fbff 42%, #e9eef8 100%)",
        padding: 24,
      }}
    >
      <Card
        style={{
          width: "100%",
          maxWidth: 440,
          borderRadius: 24,
          boxShadow: "0 24px 80px rgba(18, 34, 59, 0.14)",
        }}
      >
        <Typography.Title level={2} style={{ marginBottom: 8 }}>
          登录平台
        </Typography.Title>
        <Typography.Paragraph type="secondary" style={{ marginBottom: 24 }}>
          M1 收口版本启用基础认证入口，正式功能页需要登录后访问。
        </Typography.Paragraph>
        {errorMessage ? <Alert type="error" showIcon message={errorMessage} style={{ marginBottom: 16 }} /> : null}
        <Form<LoginFormValues> layout="vertical" onFinish={handleFinish} initialValues={{ email: "", password: "" }}>
          <Form.Item
            label="邮箱"
            name="email"
            rules={[
              { required: true, message: "请输入邮箱。" },
              { type: "email", message: "请输入合法邮箱地址。" },
            ]}
          >
            <Input autoComplete="username" placeholder="seed-owner@example.com" />
          </Form.Item>
          <Form.Item label="密码" name="password" rules={[{ required: true, message: "请输入密码。" }]}>
            <Input.Password autoComplete="current-password" placeholder="请输入密码" />
          </Form.Item>
          <Button type="primary" htmlType="submit" block size="large" loading={isSubmitting}>
            登录
          </Button>
        </Form>
      </Card>
    </div>
  );
}
