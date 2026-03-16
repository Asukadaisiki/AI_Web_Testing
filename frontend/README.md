# Frontend Status

前端服务于核心规划中的平台层和工作台层，不承载正式执行逻辑。

## 当前状态

当前 `frontend/` 已落地最小可演示平台壳，仍未进入完整产品态。

- 已有：Vite + React + TypeScript 工程、React Router、TanStack Query、Ant Design、Case 列表页、执行列表页、报告详情页、Case 工作台、基础前端测试
- 已补强：执行中心总览卡片、失败分类快速筛选、最近失败区，以及与后端 `executions overview` 契约对齐的聚合展示
- 已补强：用例级 `base_url` 编辑、工作台返回入口、执行详情返回入口、本地草稿缓存与恢复/丢弃交互
- 已补强：Dashboard、报告中心、根路由默认跳转仪表盘、近 7/14/30 天趋势与失败聚合图表
- 已补强：报告中心的当前/上一窗口对比、失败根因榜，以及根因榜回流到执行中心 `failure_fingerprint` 筛选链路
- 已补强：Suite 列表页、Suite 工作台、Case 选择排序和批量执行摘要入口
- 已补强：Case 工作台自然语言生成入口，可预览 AI 草案并选择替换当前 DSL 或仅导入步骤
- 未落地：登录页、完整定位调试面板、Suite 历史批次页

## 目标技术栈

- React + TypeScript
- Vite
- React Router
- TanStack Query
- Ant Design
- ECharts

## 前端落地顺序

前端执行顺序必须围绕核心规划：

1. 阶段 1：平台壳、用例编辑最小入口、执行结果查看
2. 阶段 2：定位调试区与候选元素证据展示
3. 阶段 3：DSL 编辑与 AI 生成入口
4. 阶段 4：报告中心与失败分析展示
5. 阶段 5：Suite 管理、执行中心、历史结果对比

## 本地启动

```powershell
cd frontend
npm install
npm run dev
```

默认访问地址：

- `http://127.0.0.1:5173`

当前 Vite 已显式绑定 `127.0.0.1`，用于避免部分 Windows 环境只监听 IPv6 `::1` 导致浏览器访问 `localhost`/IPv4 时被拒绝。

## Smoke 基准联调

前端工作台内置“公共冒烟模板”，默认对应 `example.com` 基准：

- Base URL：`https://example.com`
- 步骤 1：`goto /`
- 步骤 2：`assert_url_contains example.com`

建议使用这条用例验证：

- 工作台保存并执行链路
- 仪表盘、报告中心、执行中心总览与列表筛选
- 报告中心根因榜跳转到执行中心后，`failure_fingerprint` 筛选与清除是否正常
- 执行详情页的步骤证据、截图与跳转行为
