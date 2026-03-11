import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Alert, Button, Card, Empty, Form, Input, InputNumber, Space, Tag, Typography, message } from "antd";
import { Link, useNavigate, useParams } from "react-router-dom";

import { ErrorBlock, LoadingBlock } from "../components/PageFeedback";
import {
  createSuite,
  executeSuite,
  getCases,
  getSuiteDetail,
  updateSuite,
} from "../services/api";
import type {
  StoredCaseSummary,
  StoredSuiteCase,
  StoredSuiteDetail,
  SuiteExecutionResult,
  SuiteMutationPayload,
} from "../types/api";

type SuiteWorkbenchFormValues = {
  name: string;
  description?: string;
  project_id: number;
};

type SelectedSuiteCase = {
  case_id: number;
  case_name: string;
};

const DEFAULT_FORM_VALUES: SuiteWorkbenchFormValues = {
  name: "",
  description: "",
  project_id: 1,
};

function SuiteExecutionSummary({ result }: { result: SuiteExecutionResult }) {
  return (
    <Alert
      type={result.status === "passed" ? "success" : "warning"}
      showIcon
      message={`最近一次执行：${result.suite_name}`}
      description={
        <Space direction="vertical" size="small" style={{ width: "100%" }}>
          <Typography.Text>
            共 {result.total_cases} 条，用例通过 {result.passed_cases} 条，失败 {result.failed_cases} 条。
          </Typography.Text>
          <Space wrap>
            {result.executions.map((item) => (
              <Link key={item.execution_id} to={`/executions/${item.execution_id}`}>
                {item.case_name} ({item.status})
              </Link>
            ))}
          </Space>
        </Space>
      }
    />
  );
}

function toSelectedCases(storedSuiteCases: StoredSuiteCase[]): SelectedSuiteCase[] {
  return storedSuiteCases.map((item) => ({
    case_id: item.case_id,
    case_name: item.case_name,
  }));
}

function buildPayload(values: SuiteWorkbenchFormValues, selectedCases: SelectedSuiteCase[]): SuiteMutationPayload {
  return {
    project_id: values.project_id,
    actor_user_id: 1,
    name: values.name,
    description: values.description || null,
    cases: selectedCases.map((item) => ({ case_id: item.case_id })),
  };
}

export function SuiteWorkbenchPage() {
  const { suiteId } = useParams<{ suiteId: string }>();
  const isEditMode = Boolean(suiteId);
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [form] = Form.useForm<SuiteWorkbenchFormValues>();
  const [messageApi, contextHolder] = message.useMessage();
  const [selectedCases, setSelectedCases] = useState<SelectedSuiteCase[]>([]);
  const [latestExecution, setLatestExecution] = useState<SuiteExecutionResult | null>(null);
  const suiteQuery = useQuery({
    queryKey: ["suite-detail", suiteId],
    queryFn: () => getSuiteDetail(Number(suiteId)),
    enabled: isEditMode,
  });
  const casesQuery = useQuery({
    queryKey: ["cases"],
    queryFn: getCases,
  });

  useEffect(() => {
    if (!suiteQuery.data) {
      return;
    }
    const suite = suiteQuery.data as StoredSuiteDetail;
    form.setFieldsValue({
      name: suite.name,
      description: suite.description ?? "",
      project_id: suite.project_id,
    });
    setSelectedCases(toSelectedCases(suite.cases));
  }, [form, suiteQuery.data]);

  const saveMutation = useMutation({
    mutationFn: async () => {
      const values = await form.validateFields();
      if (selectedCases.length === 0) {
        throw new Error("请至少为 Suite 选择一个用例。");
      }
      const payload = buildPayload(values, selectedCases);
      return isEditMode ? updateSuite(Number(suiteId), payload) : createSuite(payload);
    },
    onSuccess: (suite) => {
      void Promise.all([
        queryClient.invalidateQueries({ queryKey: ["suites"] }),
        queryClient.invalidateQueries({ queryKey: ["suite-detail", String(suite.id)] }),
      ]);
      void messageApi.success("Suite 已保存。");
      void navigate(`/suites/${suite.id}/edit`);
    },
    onError: (error: Error) => {
      void messageApi.error(error.message);
    },
  });

  const executionMutation = useMutation({
    mutationFn: () => executeSuite(Number(suiteId), { actor_user_id: 1 }),
    onSuccess: (result) => {
      setLatestExecution(result);
      void queryClient.invalidateQueries({ queryKey: ["executions"] });
      void messageApi.success("Suite 执行完成。");
    },
    onError: (error: Error) => {
      void messageApi.error(error.message);
    },
  });

  if (suiteQuery.isLoading || casesQuery.isLoading) {
    return <LoadingBlock />;
  }

  if (suiteQuery.isError) {
    return <ErrorBlock message={suiteQuery.error.message} />;
  }

  if (casesQuery.isError) {
    return <ErrorBlock message={casesQuery.error.message} />;
  }

  const allCases = casesQuery.data ?? [];
  const selectedCaseIds = new Set(selectedCases.map((item) => item.case_id));

  const addCase = (record: StoredCaseSummary) => {
    if (selectedCaseIds.has(record.id)) {
      return;
    }
    setSelectedCases((current) => [...current, { case_id: record.id, case_name: record.name }]);
  };

  const removeCase = (caseId: number) => {
    setSelectedCases((current) => current.filter((item) => item.case_id !== caseId));
  };

  const moveCase = (index: number, direction: -1 | 1) => {
    const targetIndex = index + direction;
    if (targetIndex < 0 || targetIndex >= selectedCases.length) {
      return;
    }
    const nextItems = [...selectedCases];
    const [movedItem] = nextItems.splice(index, 1);
    nextItems.splice(targetIndex, 0, movedItem);
    setSelectedCases(nextItems);
  };

  return (
    <>
      {contextHolder}
      <div className="page-header">
        <Space align="start" style={{ justifyContent: "space-between", width: "100%" }} wrap>
          <div>
            <h1 className="page-title">{isEditMode ? "Suite 工作台" : "新建 Suite"}</h1>
            <p className="page-subtitle">选择已有用例，维护顺序，并复用现有执行链路进行批量运行。</p>
          </div>
          <Space wrap>
            <Button onClick={() => navigate("/suites")}>返回 Suite 列表</Button>
            <Button type="primary" loading={saveMutation.isPending} onClick={() => saveMutation.mutate()}>
              保存
            </Button>
            {isEditMode ? (
              <Button
                type="primary"
                ghost
                loading={executionMutation.isPending}
                onClick={() => executionMutation.mutate()}
              >
                执行 Suite
              </Button>
            ) : null}
          </Space>
        </Space>
      </div>
      <Space direction="vertical" size="large" style={{ width: "100%" }}>
        {latestExecution ? <SuiteExecutionSummary result={latestExecution} /> : null}

        <Card>
          <Form form={form} layout="vertical" initialValues={DEFAULT_FORM_VALUES}>
            <div className="workbench-grid">
              <Form.Item label="Suite 名称" name="name" rules={[{ required: true, message: "请输入 Suite 名称" }]}>
                <Input placeholder="例如：登录回归套件" />
              </Form.Item>
              <Form.Item
                label="项目 ID"
                name="project_id"
                rules={[{ required: true, message: "请输入项目 ID" }]}
              >
                <InputNumber min={1} style={{ width: "100%" }} />
              </Form.Item>
            </div>
            <Form.Item label="描述" name="description">
              <Input.TextArea rows={3} placeholder="说明该 Suite 的覆盖范围" />
            </Form.Item>
          </Form>
        </Card>

        <div className="suite-selection-grid">
          <Card className="suite-case-card" title="可选用例">
            <Space direction="vertical" size="middle" style={{ width: "100%" }}>
              {allCases.length === 0 ? (
                <Empty
                  image={Empty.PRESENTED_IMAGE_SIMPLE}
                  description="当前还没有可加入的 Case"
                />
              ) : (
                allCases.map((record) => (
                  <Card key={record.id} size="small">
                    <Space direction="vertical" size="small" style={{ width: "100%" }}>
                      <div>
                        <Typography.Text strong>{record.name}</Typography.Text>
                        <div>
                          <Typography.Text type="secondary">{record.description || "未填写描述"}</Typography.Text>
                        </div>
                      </div>
                      <Space wrap>
                        <Tag>Project {record.project_id}</Tag>
                        <Tag>{record.steps.length} steps</Tag>
                      </Space>
                      <Button onClick={() => addCase(record)} disabled={selectedCaseIds.has(record.id)}>
                        {selectedCaseIds.has(record.id) ? "已加入" : "加入 Suite"}
                      </Button>
                    </Space>
                  </Card>
                ))
              )}
            </Space>
          </Card>

          <Card className="suite-case-card" title="已选用例顺序">
            <Space direction="vertical" size="middle" style={{ width: "100%" }}>
              {selectedCases.length === 0 ? (
                <Alert type="warning" showIcon message="请至少加入一个 Case" />
              ) : (
                selectedCases.map((item, index) => (
                  <Card key={item.case_id} size="small" title={`Case ${index + 1}`}>
                    <Space direction="vertical" size="small" style={{ width: "100%" }}>
                      <div>
                        <Typography.Text strong>{item.case_name}</Typography.Text>
                        <div>
                          <Typography.Text type="secondary">Case ID：{item.case_id}</Typography.Text>
                        </div>
                      </div>
                      <Space wrap>
                        <Button onClick={() => moveCase(index, -1)} disabled={index === 0}>
                          上移
                        </Button>
                        <Button
                          onClick={() => moveCase(index, 1)}
                          disabled={index === selectedCases.length - 1}
                        >
                          下移
                        </Button>
                        <Button danger onClick={() => removeCase(item.case_id)}>
                          移除
                        </Button>
                      </Space>
                    </Space>
                  </Card>
                ))
              )}
            </Space>
          </Card>
        </div>
      </Space>
    </>
  );
}
