# NotebookLM 风格前端重设计 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [x]`) syntax for tracking.

**Goal:** 将 AI Web Testing 前端从传统顶部导航 + 卡片堆叠布局，重构为 NotebookLM 风格三栏浮岛布局。

**Architecture:** 统一使用 `NotebookLMLayout` 三栏容器（左 280px / 中 flex:1 / 右 340px），左侧栏底部放置页面导航替代顶部 header。全局注入 ConfigProvider 主题 token 实现大圆角、无边框、弱阴影风格。

**Tech Stack:** React 18 + TypeScript + Ant Design 5 + Vite + React Router 6

---

## File Structure

### 新增
- `frontend/src/components/NotebookNav.tsx` — 侧边栏底部页面导航组件
- `frontend/src/components/ChatMessage.tsx` — AI 对话消息气泡组件
- `frontend/src/components/ChatInput.tsx` — 圆角悬挂式 AI 输入框组件
- `frontend/src/components/StepList.tsx` — 测试步骤列表组件（搜索 + 列表 + Add Action）
- `frontend/src/components/InsightCard.tsx` — 右栏信息卡片通用组件

### 重写
- `frontend/src/layouts/NotebookLMLayout.tsx` — 三栏布局 + 导航
- `frontend/src/layouts/AppLayout.tsx` — 简化为纯 Outlet 容器
- `frontend/src/pages/PlanningPage.tsx` — 三栏布局
- `frontend/src/pages/CasesPage.tsx` — 三栏布局（表格→卡片流）
- `frontend/src/pages/CaseWorkbenchPage.tsx` — 三栏布局（核心）
- `frontend/src/pages/ExecutionDetailPage.tsx` — 三栏布局

### 修改
- `frontend/src/main.tsx` — ConfigProvider 主题 token
- `frontend/src/app/AppRouter.tsx` — 调整 layout 嵌套
- `frontend/src/index.css` — 移除旧类，添加新样式

### 不变
- `frontend/src/services/api.ts`
- `frontend/src/types/api.ts`
- `frontend/src/components/PageFeedback.tsx`
- `frontend/src/components/InterventionPanel.tsx`
- `frontend/src/components/executionPresentation.tsx`
- `frontend/src/components/executionMetrics.tsx`

---

## Tasks

### Task 1: 全局主题 Token + CSS 基础

**Files:**
- Modify: `frontend/src/main.tsx` (ConfigProvider theme)
- Modify: `frontend/src/index.css` (remove old classes, add new base styles)

- [x] **Step 1: 更新 ConfigProvider 主题 token**

在 `frontend/src/main.tsx` 中，替换当前 `<ConfigProvider theme={...}>` 为 NotebookLM 风格 token：

```tsx
<ConfigProvider
  theme={{
    token: {
      colorPrimary: "#1a1a2e",
      borderRadius: 8,
      fontFamily: "'Inter', 'PingFang SC', 'Microsoft YaHei', 'Segoe UI', sans-serif",
      colorBgContainer: "#ffffff",
      colorBorderSecondary: "#f0f0f0",
    },
    components: {
      Button: {
        borderRadius: 8,
        colorPrimary: "#1a1a2e",
        primaryShadow: "0 2px 4px rgba(26,26,46,0.12)",
      },
      Input: {
        borderRadius: 12,
        borderWidth: 0,
        activeShadow: "0 0 0 2px rgba(26,26,46,0.08)",
        hoverBorderColor: "#d9d9d9",
      },
      Card: {
        borderRadius: 16,
        boxShadowTertiary: "0 2px 10px rgba(0,0,0,0.03)",
      },
      Table: {
        borderWidth: 0,
        borderRadius: 12,
      },
      Select: {
        borderRadius: 12,
        borderWidth: 0,
        optionSelectedBg: "#f0f4f8",
      },
      List: {
        borderWidth: 0,
      },
      Collapse: {
        borderWidth: 0,
        borderRadius: 12,
      },
      Tag: {
        borderRadiusSM: 12,
      },
    },
  }}
>
```

- [x] **Step 2: 清理 index.css，替换为新基础样式**

删除以下旧类：`.page-shell`, `.page-header`, `.page-title`, `.page-subtitle`, `.workbench-grid`, `.structured-step-grid`, `.dashboard-grid`, `.analytics-grid`, `.summary-strip`, `.summary-tile`, `.summary-label`, `.summary-value`, `.summary-meta`, `.table-footer-actions`, `.active-filter-panel`

保留：截图相关类 (`.step-screenshot-frame`, `.step-screenshot-image`, `.screenshot-empty`)、`.evidence-card`、`.status-tag`

新增全局基础样式：

```css
:root {
  color: #1a1a2e;
  background: #f8f9fa;
  font-family: "Inter", "PingFang SC", "Microsoft YaHei", "Segoe UI", sans-serif;
  line-height: 1.5;
  font-weight: 400;
}

* { box-sizing: border-box; }

body {
  margin: 0;
  min-width: 320px;
  min-height: 100vh;
  background: #f8f9fa;
}

a { color: inherit; }
#root { min-height: 100vh; }

/* NotebookLM base card style — used across all panels */
.nb-card {
  background: #fff;
  border-radius: 16px;
  box-shadow: 0 2px 10px rgba(0, 0, 0, 0.03);
}

/* Chat message bubbles */
.chat-bubble-user {
  background: #1a1a2e;
  color: #fff;
  border-radius: 16px 16px 4px 16px;
  padding: 12px 16px;
  max-width: 75%;
  line-height: 1.7;
}

.chat-bubble-ai {
  background: #f0f4f8;
  border-radius: 16px 16px 16px 4px;
  padding: 12px 16px;
  max-width: 80%;
  line-height: 1.7;
}

/* Step list items */
.step-item {
  padding: 8px 12px;
  border-radius: 8px;
  font-size: 13px;
  cursor: pointer;
  transition: background 0.15s;
}

.step-item:hover {
  background: #f5f5f5;
}

.step-item-active {
  background: #eef2ff;
  border-left: 3px solid #1a1a2e;
}

/* Right panel action grid */
.action-grid-item {
  background: #f5f5f5;
  border-radius: 12px;
  padding: 12px 8px;
  text-align: center;
  font-size: 12px;
  cursor: pointer;
  transition: background 0.15s;
}

.action-grid-item:hover {
  background: #e8e8e8;
}

/* Scrollbar styling for panels */
.panel-scroll::-webkit-scrollbar {
  width: 4px;
}
.panel-scroll::-webkit-scrollbar-thumb {
  background: #d9d9d9;
  border-radius: 4px;
}
```

- [x] **Step 3: 验证构建通过**

Run: `cd frontend && npx vite build`
Expected: 构建成功（页面样式会暂时混乱，因为旧 CSS 类被删除了，后续任务会修复）

- [x] **Step 4: Commit**

```bash
git add frontend/src/main.tsx frontend/src/index.css
git commit -m "style: update global theme tokens and CSS for NotebookLM style"
```

### Task 2: NotebookLMLayout 重构 + 页面导航组件

**Files:**
- Rewrite: `frontend/src/layouts/NotebookLMLayout.tsx`
- Create: `frontend/src/components/NotebookNav.tsx`
- Modify: `frontend/src/layouts/AppLayout.tsx` → 简化为纯 Outlet
- Modify: `frontend/src/app/AppRouter.tsx` → 使用新 layout

- [x] **Step 1: 创建页面导航组件 `NotebookNav.tsx`**

Create `frontend/src/components/NotebookNav.tsx`:

```tsx
import { useLocation, useNavigate } from "react-router-dom";

const NAV_ITEMS = [
  { key: "/", label: "AI 规划", icon: "🧠" },
  { key: "/cases", label: "用例中心", icon: "📋" },
  { key: "/cases/new", label: "工作台", icon: "🔧" },
] as const;

function isActive(currentPath: string, navKey: string): boolean {
  if (navKey === "/") return currentPath === "/";
  return currentPath.startsWith(navKey);
}

export function NotebookNav() {
  const location = useLocation();
  const navigate = useNavigate();

  return (
    <div
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
          <div
            key={item.key}
            onClick={() => navigate(item.key)}
            style={{
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
          </div>
        );
      })}
    </div>
  );
}
```

- [x] **Step 2: 重写 `NotebookLMLayout.tsx`**

Rewrite `frontend/src/layouts/NotebookLMLayout.tsx`:

```tsx
import React from "react";
import { NotebookNav } from "../components/NotebookNav";

interface NotebookLMLayoutProps {
  leftPanel: React.ReactNode;
  centerPanel: React.ReactNode;
  rightCards: React.ReactNode[];  // multiple cards for right column
  navBottom?: boolean;             // show nav in left panel bottom, default true
}

export function NotebookLMLayout({
  leftPanel,
  centerPanel,
  rightCards,
  navBottom = true,
}: NotebookLMLayoutProps) {
  return (
    <div
      style={{
        display: "flex",
        height: "100vh",
        backgroundColor: "#f8f9fa",
        padding: 16,
        gap: 16,
        boxSizing: "border-box",
      }}
    >
      {/* Left Panel */}
      <div
        className="nb-card panel-scroll"
        style={{
          width: 280,
          display: "flex",
          flexDirection: "column",
          overflow: "hidden",
          padding: 16,
        }}
      >
        <div style={{ flex: 1, display: "flex", flexDirection: "column", overflow: "hidden" }}>
          {leftPanel}
        </div>
        {navBottom && <NotebookNav />}
      </div>

      {/* Center Panel */}
      <div
        className="nb-card"
        style={{
          flex: 1,
          display: "flex",
          flexDirection: "column",
          overflow: "hidden",
        }}
      >
        {centerPanel}
      </div>

      {/* Right Panel — transparent background, cards float in it */}
      <div
        style={{
          width: 340,
          display: "flex",
          flexDirection: "column",
          gap: 12,
          overflowY: "auto",
        }}
        className="panel-scroll"
      >
        {rightCards.map((card, index) => (
          <div key={index} className="nb-card" style={{ padding: 16 }}>
            {card}
          </div>
        ))}
      </div>
    </div>
  );
}
```

- [x] **Step 3: 简化 `AppLayout.tsx`**

Rewrite `frontend/src/layouts/AppLayout.tsx` — 移除顶部导航，改为纯 Outlet 容器：

```tsx
import { Outlet } from "react-router-dom";

export function AppLayout() {
  return <Outlet />;
}
```

- [x] **Step 4: 更新 `AppRouter.tsx`**

在 `frontend/src/app/AppRouter.tsx` 中，移除 `AppLayout` 导入，Routes 直接包裹 route（不再需要 layout wrapper）。各页面自己使用 `NotebookLMLayout`：

```tsx
import { Suspense, lazy } from "react";
import { Navigate, Route, Routes, useParams } from "react-router-dom";

import { LoadingBlock } from "../components/PageFeedback";

const PlanningPage = lazy(() =>
  import("../pages/PlanningPage").then((m) => ({ default: m.PlanningPage })),
);
const CasesPage = lazy(() =>
  import("../pages/CasesPage").then((m) => ({ default: m.CasesPage })),
);
const CaseWorkbenchPage = lazy(() =>
  import("../pages/CaseWorkbenchPage").then((m) => ({ default: m.CaseWorkbenchPage })),
);
const ExecutionDetailPage = lazy(() =>
  import("../pages/ExecutionDetailPage").then((m) => ({ default: m.ExecutionDetailPage })),
);

function LegacyExecutionRedirect() {
  const { executionId } = useParams<{ executionId: string }>();
  return <Navigate to={`/run/${executionId}`} replace />;
}

export function AppRouter() {
  return (
    <Suspense fallback={<LoadingBlock />}>
      <Routes>
        <Route path="/" element={<PlanningPage />} />
        <Route path="/cases" element={<CasesPage />} />
        <Route path="/cases/new" element={<CaseWorkbenchPage />} />
        <Route path="/cases/:caseId/edit" element={<CaseWorkbenchPage />} />
        <Route path="/run/:executionId" element={<ExecutionDetailPage />} />
        <Route path="/executions/:executionId" element={<LegacyExecutionRedirect />} />
        <Route path="/dashboard" element={<Navigate to="/" replace />} />
        <Route path="/executions" element={<Navigate to="/cases" replace />} />
        <Route path="/login" element={<Navigate to="/" replace />} />
      </Routes>
    </Suspense>
  );
}
```

- [x] **Step 5: 验证构建通过**

Run: `cd frontend && npx vite build`
Expected: 构建成功（页面此时可能空白/报错，因为各页面还没改为使用 NotebookLMLayout，下一步开始逐页改造）

- [x] **Step 6: Commit**

```bash
git add frontend/src/components/NotebookNav.tsx frontend/src/layouts/NotebookLMLayout.tsx frontend/src/layouts/AppLayout.tsx frontend/src/app/AppRouter.tsx
git commit -m "refactor: NotebookLM 3-panel layout with sidebar navigation"
```

### Task 3: PlanningPage 三栏重写

**Files:**
- Rewrite: `frontend/src/pages/PlanningPage.tsx`

**设计映射：**
- 左栏: 需求收集进度（7 项 checklist）+ 已收集信息列表
- 中栏: AI 对话主区域（消息列表 + 底部圆角输入框 + 建议标签）
- 右栏卡片: 规划进度 + 场景选择 + DSL 草案列表

- [x] **Step 1: 重写 PlanningPage 使用 NotebookLMLayout**

Rewrite `frontend/src/pages/PlanningPage.tsx`:

将现有 `AITestPlanningPanel` 的逻辑拆分到三栏中。保留 `AITestPlanningPanel` 的所有 state 和 hooks 逻辑，但把 UI 渲染拆为三部分传入 `NotebookLMLayout`。

```tsx
import { Alert, Button, Card, Checkbox, Input, Progress, Space, Tag, Typography } from "antd";
import { SendOutlined } from "@ant-design/icons";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "react-router-dom";

import { AITestPlanningPanel } from "../components/AITestPlanningPanel";
import { NotebookLMLayout } from "../layouts/NotebookLMLayout";
import { LoadingBlock } from "../components/PageFeedback";
import { createCase, getAISettings, getProjects } from "../services/api";
import type { AIPlanningDraft } from "../types/api";
```

核心思路：`PlanningPage` 仍然渲染 `<AITestPlanningPanel>`，但 `AITestPlanningPanel` 内部改为使用 `NotebookLMLayout`。需要修改 `AITestPlanningPanel` 接受 `renderMode="notebook"` prop 或直接重写其渲染逻辑。

**具体方案**：将 `AITestPlanningPanel` 的渲染拆为三部分：

1. **左栏渲染函数** — 需求进度区域：
   - 标题 "Requirements"
   - `Progress` 组件显示 `progressPercent`
   - 已收集条目列表（label + value）
   - `missingSlots` 提示

2. **中栏渲染函数** — AI 对话区域：
   - 顶部标题 "AI Planning"
   - `transcript` 消息列表（使用 `.chat-bubble-user` / `.chat-bubble-ai` 样式）
   - 建议问题标签（`suggestedQuestions`）
   - 底部圆角输入框 + 发送按钮

3. **右栏渲染数组** — 规划进度 + 草案：
   - 卡片1: 规划进度（`plan?.summary` + 场景选择 checkboxes）
   - 卡片2: DSL 草案列表（`drafts`）

重写 `AITestPlanningPanel` 的 return 部分，从当前的 Card 堆叠改为：

```tsx
return (
  <NotebookLMLayout
    leftPanel={renderLeftPanel()}
    centerPanel={renderCenterPanel()}
    rightCards={renderRightCards()}
  />
);
```

其中每个 render 函数返回对应的 JSX，保持所有现有 state 和 handler 不变。

- [x] **Step 2: 验证 PlanningPage 构建通过**

Run: `cd frontend && npx vite build`
Expected: 构建成功

- [x] **Step 3: 浏览器验证**

Run: `cd frontend && npx vite dev`
打开 http://localhost:5173，验证：
- 左栏显示需求收集进度
- 中栏显示 AI 对话消息 + 底部输入框
- 右栏显示规划进度卡片
- 左栏底部显示页面导航

- [x] **Step 4: Commit**

```bash
git add frontend/src/pages/PlanningPage.tsx frontend/src/components/AITestPlanningPanel.tsx
git commit -m "feat: rewrite PlanningPage with NotebookLM 3-panel layout"
```

### Task 4: CasesPage 三栏重写

**Files:**
- Rewrite: `frontend/src/pages/CasesPage.tsx`

**设计映射：**
- 左栏: 筛选器（搜索框 + 状态筛选）+ 快速操作按钮
- 中栏: 用例卡片流（替代原 Table，每个 case 一张卡片，显示名称/描述/步骤数/操作按钮）
- 右栏卡片: 统计面板（用例总数 + 最近执行摘要）+ 快速操作卡片

- [x] **Step 1: 重写 CasesPage 使用 NotebookLMLayout**

Rewrite `frontend/src/pages/CasesPage.tsx`:

```tsx
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Alert, Button, Card, Input, Space, Tag, Typography, message } from "antd";
import { Link, useNavigate } from "react-router-dom";
import { SearchOutlined, PlusOutlined, PlayCircleOutlined } from "@ant-design/icons";

import { ErrorBlock, LoadingBlock } from "../components/PageFeedback";
import { NotebookLMLayout } from "../layouts/NotebookLMLayout";
import { executeCase, getCases } from "../services/api";
import type { StoredCaseSummary } from "../types/api";
```

**左栏内容：**
- 标题 "Cases"
- 圆角搜索框（`borderRadius: 24px, background: #F0F4F8`）
- 状态筛选标签（全部 / 待执行 / 已通过 / 已失败）
- 底部 "新建用例" 按钮

**中栏内容：**
- 用例卡片网格（`display: grid; grid-template-columns: 1fr 1fr; gap: 12px`）
- 每张卡片显示：用例名称（bold）、描述（secondary）、步骤数 Tag、base_url
- 卡片底部操作按钮：执行 + 编辑

**右栏内容：**
- 卡片1: 统计摘要（用例总数、步骤分布）
- 卡片2: 快速操作（返回 AI 规划、手动补充/编辑）

保持所有现有 mutation/query 逻辑不变，只改 UI 渲染。

- [x] **Step 2: 验证构建通过**

Run: `cd frontend && npx vite build`
Expected: 构建成功

- [x] **Step 3: 浏览器验证**

Run: `cd frontend && npx vite dev`
打开 http://localhost:5173/cases，验证：
- 左栏显示搜索和筛选
- 中栏显示用例卡片流
- 右栏显示统计和操作
- 点击"执行"和"编辑"按钮功能正常

- [x] **Step 4: Commit**

```bash
git add frontend/src/pages/CasesPage.tsx
git commit -m "feat: rewrite CasesPage with NotebookLM 3-panel card layout"
```

### Task 5: CaseWorkbenchPage 三栏重写（核心页面）

**Files:**
- Rewrite: `frontend/src/pages/CaseWorkbenchPage.tsx`
- Create: `frontend/src/components/ChatMessage.tsx`
- Create: `frontend/src/components/ChatInput.tsx`
- Create: `frontend/src/components/StepList.tsx`

这是最复杂的页面。策略：将现有 ~1600 行 CaseWorkbenchPage 的所有 state/mutation/query 逻辑保持不变，把 UI 渲染拆分为三个面板。新增 3 个小组件辅助渲染。

- [x] **Step 1: 创建 ChatMessage 组件**

Create `frontend/src/components/ChatMessage.tsx`:

```tsx
import { Typography, Tag } from "antd";

interface ChatMessageProps {
  role: "user" | "assistant";
  content: string;
  structuredData?: React.ReactNode;
}

export function ChatMessage({ role, content, structuredData }: ChatMessageProps) {
  if (role === "user") {
    return (
      <div style={{ display: "flex", justifyContent: "flex-end" }}>
        <div className="chat-bubble-user">{content}</div>
      </div>
    );
  }

  return (
    <div style={{ display: "flex", justifyContent: "flex-start" }}>
      <div className="chat-bubble-ai">
        <Typography.Text strong style={{ fontSize: 13 }}>AI 助手</Typography.Text>
        <div style={{ marginTop: 4 }}>{content}</div>
        {structuredData && <div style={{ marginTop: 8 }}>{structuredData}</div>}
      </div>
    </div>
  );
}
```

- [x] **Step 2: 创建 ChatInput 组件**

Create `frontend/src/components/ChatInput.tsx`:

```tsx
import { Input } from "antd";
import { SendOutlined } from "@ant-design/icons";

interface ChatInputProps {
  value: string;
  onChange: (value: string) => void;
  onSend: () => void;
  placeholder?: string;
  loading?: boolean;
}

export function ChatInput({
  value,
  onChange,
  onSend,
  placeholder = "描述你想要的操作或修改...",
  loading = false,
}: ChatInputProps) {
  return (
    <div style={{ padding: "16px 32px 20px", borderTop: "1px solid #f5f5f5" }}>
      <div
        style={{
          background: "#F0F4F8",
          borderRadius: 24,
          padding: "12px 20px",
          display: "flex",
          alignItems: "flex-end",
          gap: 8,
        }}
      >
        <Input.TextArea
          value={value}
          onChange={(e) => onChange(e.target.value)}
          placeholder={placeholder}
          autoSize={{ minRows: 1, maxRows: 4 }}
          bordered={false}
          style={{
            background: "transparent",
            resize: "none",
            fontSize: 14,
            lineHeight: 1.5,
          }}
          onPressEnter={(e) => {
            if (!e.shiftKey) {
              e.preventDefault();
              if (!loading && value.trim()) onSend();
            }
          }}
        />
        <div
          onClick={() => { if (!loading && value.trim()) onSend(); }}
          style={{
            width: 40,
            height: 40,
            background: loading ? "#d9d9d9" : "#1a1a2e",
            borderRadius: "50%",
            display: "flex",
            alignItems: "center",
            justifyContent: "center",
            cursor: loading ? "not-allowed" : "pointer",
            flexShrink: 0,
            transition: "background 0.15s",
          }}
        >
          <SendOutlined style={{ color: "#fff", fontSize: 16 }} />
        </div>
      </div>
    </div>
  );
}
```

- [x] **Step 3: 创建 StepList 组件**

Create `frontend/src/components/StepList.tsx`:

```tsx
import { Button, Input, Tag } from "antd";
import { PlusOutlined, SearchOutlined } from "@ant-design/icons";
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
    goto: "🧭",
    click: "👆",
    input: "⌨️",
    wait_for: "⏳",
    assert_text: "✅",
    assert_url_contains: "🔗",
  };
  return labels[action] ?? "•";
}

export function StepList({ steps, activeIndex, onSelect, onAdd, searchValue, onSearchChange }: StepListProps) {
  return (
    <div style={{ display: "flex", flexDirection: "column", height: "100%" }}>
      <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", marginBottom: 12 }}>
        <span style={{ fontWeight: 700, fontSize: 14 }}>Test Steps</span>
        <span style={{ fontSize: 11, color: "#999" }}>{steps.length} 步骤</span>
      </div>

      <Input
        prefix={<SearchOutlined style={{ color: "#bbb" }} />}
        placeholder="搜索步骤..."
        value={searchValue}
        onChange={(e) => onSearchChange(e.target.value)}
        style={{ borderRadius: 24, background: "#F0F4F8", marginBottom: 12 }}
        bordered={false}
      />

      <div style={{ flex: 1, overflowY: "auto" }} className="panel-scroll">
        {steps.map((step, index) => (
          <div
            key={index}
            onClick={() => onSelect(index)}
            className={`step-item ${index === activeIndex ? "step-item-active" : ""}`}
          >
            <span style={{ marginRight: 6 }}>{getActionLabel(step.action)}</span>
            <strong>{step.action}</strong>
            {step.target ? <span style={{ color: "#999", marginLeft: 4 }}>{String(step.target).slice(0, 20)}</span> : null}
          </div>
        ))}
      </div>

      <div style={{ marginTop: 8, paddingTop: 8, borderTop: "1px solid #f0f0f0" }}>
        <Button
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
```

- [x] **Step 4: 重写 CaseWorkbenchPage 使用 NotebookLMLayout**

Rewrite `frontend/src/pages/CaseWorkbenchPage.tsx`:

保留全部现有 state、mutation、query、effect 逻辑（从 `createDefaultStep` 到所有 hooks）。替换 UI 渲染部分为三栏布局：

**左栏** (传给 `leftPanel`):
- 使用 `StepList` 组件渲染步骤列表
- 点击步骤时 `setActiveStepIndex(index)` 更新选中状态
- "Add Action" 调用 `syncStructuredSteps([...structuredSteps, createDefaultStep()])`

**中栏** (传给 `centerPanel`):
- 顶部：用例标题 + base_url + 操作按钮行（校验 / 保存 / 保存并执行）
- 中间：当前步骤的编辑表单（结构化编辑 / JSON 模式切换）
- 底部：`ChatInput` 组件绑定 `generationPrompt` / `handleGenerateDrafts`

**右栏** (传给 `rightCards`，数组形式):
- 卡片 1 "定位器/元素信息"：当前步骤的 target / action 信息表
- 卡片 2 "操作面板"：2x2 grid（生成 DSL / 校验 DSL / 契约编辑 / AI 规划）
- 卡片 3 "生成设置"：生成模式 / 上下文来源 / 导入方式选择
- 卡片 4 "生成预览"（条件渲染，仅 `generatedDraft` 存在时）

草稿恢复提示（`pendingDraft` Alert）、缺少 base_url 警告保留在中栏顶部。

- [x] **Step 5: 验证构建通过**

Run: `cd frontend && npx vite build`
Expected: 构建成功

- [x] **Step 6: 浏览器验证**

Run: `cd frontend && npx vite dev`
打开 http://localhost:5173/cases/new，验证：
- 左栏：步骤列表 + 搜索 + Add Action
- 中栏：表单 + 步骤编辑 + 底部 ChatInput
- 右栏：操作卡片网格
- 保存/校验/执行功能正常
- 本地草稿恢复正常

- [x] **Step 7: Commit**

```bash
git add frontend/src/components/ChatMessage.tsx frontend/src/components/ChatInput.tsx frontend/src/components/StepList.tsx frontend/src/pages/CaseWorkbenchPage.tsx
git commit -m "feat: rewrite CaseWorkbenchPage with NotebookLM 3-panel layout"
```

### Task 6: ExecutionDetailPage 三栏重写

**Files:**
- Rewrite: `frontend/src/pages/ExecutionDetailPage.tsx`

**设计映射：**
- 左栏: 步骤时间线（垂直列表，passed=绿色圆点、failed=红色圆点）+ 执行摘要（状态/编号/步骤数）
- 中栏: 选中步骤的截图大图 + 完整证据（定位信息、Console/Network 事件、干预面板）
- 右栏卡片: 执行概览统计 + 定位策略分布 + 候选元素列表

- [x] **Step 1: 重写 ExecutionDetailPage 使用 NotebookLMLayout**

Rewrite `frontend/src/pages/ExecutionDetailPage.tsx`:

保留全部现有 state、query、effect、`StepEvidenceBody`、`EventList` 等内部组件逻辑。替换最外层 UI 渲染为三栏：

**左栏**:
- 顶部：执行标题 + 返回按钮（返回用例中心 / 返回用例）
- 步骤时间线列表：每个步骤一行（圆点颜色 + Step N / action 名称），点击切换 `activeStepIndex`
- 底部摘要：执行状态 / 编号 / 步骤数（三个小 tile）

**中栏**:
- 顶部：`isBaseUrlError` Alert（条件渲染）
- 主体：选中步骤的 `<StepEvidenceBody>` 组件
- 如果没有选中步骤，显示所有步骤的 Collapse（兼容当前行为）

**右栏**:
- 卡片 1: 执行概览（通过率 / 平均耗时 / 干预率）
- 卡片 2: 定位策略分布（DOM/VLM/Correction/Manual 的 Tag 列表）
- 卡片 3: 选中步骤的候选元素列表（如果有 `locator_trace.candidates`）

新增 state：`const [activeStepIndex, setActiveStepIndex] = useState<number>(0)` 控制左栏选中步骤，默认选中第一个 failed 步骤（兼容当前 `activeKeys` 逻辑）。

- [x] **Step 2: 验证构建通过**

Run: `cd frontend && npx vite build`
Expected: 构建成功

- [x] **Step 3: 浏览器验证**

Run: `cd frontend && npx vite dev`
打开一个执行详情页面，验证：
- 左栏时间线可点击切换步骤
- 中栏正确显示选中步骤的截图和证据
- 右栏显示统计和策略分布
- 干预面板正常显示

- [x] **Step 4: Commit**

```bash
git add frontend/src/pages/ExecutionDetailPage.tsx
git commit -m "feat: rewrite ExecutionDetailPage with NotebookLM 3-panel layout"
```

