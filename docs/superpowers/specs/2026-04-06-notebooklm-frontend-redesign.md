# NotebookLM 风格前端重设计

Date: 2026-04-06

## 概述

将 AI Web Testing 前端从传统的顶部导航 + 卡片堆叠布局，重构为 Google NotebookLM 风格的三栏浮岛布局。所有 4 个主要页面统一使用此布局，移除顶部导航栏，改为左侧栏底部页面导航。

## 设计决策

- **导航方式**: 侧边栏导航（移除顶部 header + 步骤条），页面切换放在左侧栏底部
- **布局**: 三栏浮岛（左 280px / 中 flex:1 / 右 340px），浅灰底色 #f8f9fa，白色圆角卡片
- **AI 对话**: 中栏 AI 对话区域宽敞大气，消息气泡 max-width 80%，padding 40px，底部圆角输入框
- **主题**: Ant Design ConfigProvider 全局 token 调整（大圆角、无边框、弱阴影、Inter 字体）

## 全局布局

### NotebookLMLayout 重构

基于现有 `NotebookLMLayout.tsx`，扩展为支持：

- **左侧栏** (280px): 白色圆角卡片 (border-radius: 16px, box-shadow: 0 2px 10px rgba(0,0,0,0.03))，底部包含页面导航区域
- **中间栏** (flex:1): 白色圆角卡片，主内容区
- **右侧栏** (340px): 背景透明（与外层底色一致），垂直 flex 容器 (gap: 12px)，存放多张分离的小卡片
- **外层容器**: height: 100vh, padding: 16px, gap: 16px, background: #f8f9fa

### 页面导航

左侧栏底部固定区域：
- 分割线 + 4 个导航项（AI 规划 / 用例中心 / 工作台 / 执行详情）
- 当前页高亮（深色背景 #1a1a2e，白色文字）
- 点击切换路由

## 各页面三栏内容映射

### 1. AI 规划页 (PlanningPage)

| 栏位 | 内容 |
|------|------|
| 左栏 | 需求收集进度（7 项 checklist）+ 素材/来源列表 |
| 中栏 | AI 对话主区域（消息列表 + 底部输入框 + 建议标签） |
| 右栏 | 规划进度摘要 + 场景选择卡片 + DSL 草案列表 |

当前 AITestPlanningPanel 的对话部分移入中栏，进度和场景部分移入右栏。

### 2. 用例中心 (CasesPage)

| 栏位 | 内容 |
|------|------|
| 左栏 | 搜索框 + 项目筛选 + 状态筛选器 |
| 中栏 | 用例卡片流（每个用例一张卡片：名称、描述、步骤数、操作按钮） |
| 右栏 | 统计面板（用例总数、最近执行）+ 快速操作（新建用例） |

表格改为卡片流，更符合 NotebookLM 的列表风格。

### 3. 用例工作台 (CaseWorkbenchPage)

这是 docs/notebook_style.md 提示词重点描述的页面。

| 栏位 | 内容 |
|------|------|
| 左栏 | "Test Steps" 标题 + 圆角搜索框 (border-radius: 24px, background: #F0F4F8) + 步骤列表（hover 灰色背景，圆角 8px）+ 底部 "Add Action" 按钮 |
| 中栏 | 顶部：用例标题 + base_url + 操作按钮（保存/校验/执行）；中间：页面预览/截图 + 当前步骤详情；底部：AI 对话输入区（圆角 24px 容器，深色发送按钮） |
| 右栏 | 卡片1：定位器/元素信息（属性表）；卡片2：操作网格（生成定位/验证/契约/AI DSL，2x2 grid，灰色背景圆角方块）；卡片3：快速设置（生成模式/上下文/导入方式） |

步骤编辑器（结构化编辑/JSON 模式）保留在左栏，点击步骤展开编辑。自然语言生成和 AI 规划功能通过中栏底部对话框触发。

### 4. 执行详情 (ExecutionDetailPage)

| 栏位 | 内容 |
|------|------|
| 左栏 | 步骤时间线（垂直列表，passed 绿色 / failed 红色）+ 状态摘要统计 |
| 中栏 | 截图大图预览 + 选中步骤的完整证据（定位信息、Console/Network 事件、干预面板） |
| 右栏 | 执行概览统计（通过率/耗时/干预率）+ 定位策略分布 + 候选元素列表 |

长页面改为三栏并排，步骤时间线替代 Collapse，选中步骤时中栏和右栏联动更新。

## 全局主题 (ConfigProvider)

参考 docs/notebook_style.md 提示词 3：

```typescript
theme: {
  token: {
    fontFamily: "'Inter', 'Roboto', 'PingFang SC', 'Microsoft YaHei', sans-serif",
    borderRadius: 8,
    colorPrimary: '#1a1a2e',  // 深灰/暗色替代刺眼深蓝
  },
  components: {
    Button: { borderRadius: 8, colorPrimary: '#1a1a2e' },
    Input: {
      borderRadius: 12,
      borderWidth: 0,
      activeShadow: '0 0 0 2px rgba(26,26,46,0.1)',
    },
    Card: {
      borderRadius: 16,
      boxShadowTertiary: '0 2px 10px rgba(0,0,0,0.03)',
    },
    Table: { borderWidth: 0 },
    List: { borderWidth: 0 },
    Select: { borderRadius: 12, borderWidth: 0 },
  },
}
```

## 不做的事

- 不改变后端 API 和数据结构
- 不新增功能，只做 UI 布局和样式重构
- 不改变路由结构
- 不改变 localStorage 草稿机制
- 不改变 React Query 数据获取逻辑

## 涉及文件

### 新增/重写
- `NotebookLMLayout.tsx` — 重构为支持页面导航的三栏布局
- `PlanningPage.tsx` — 重写为三栏布局
- `CasesPage.tsx` — 重写为三栏布局（表格→卡片流）
- `CaseWorkbenchPage.tsx` — 重写为三栏布局（核心页面）
- `ExecutionDetailPage.tsx` — 重写为三栏布局

### 修改
- `AppLayout.tsx` — 移除顶部导航，使用 NotebookLMLayout
- `AppRouter.tsx` — 可能需要调整 layout 嵌套
- `main.tsx` — ConfigProvider 主题 token 注入
- `index.css` — 移除旧的样式类（page-shell, workbench-grid 等），添加新样式

### 不变
- `AITestPlanningPanel.tsx` — 拆分为子组件后复用逻辑
- `InterventionPanel.tsx` — 嵌入右栏，不改逻辑
- `PageFeedback.tsx` — 不变
- `services/api.ts` — 不变
- `types/api.ts` — 不变
