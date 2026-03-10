# Frontend Status

前端服务于核心规划中的平台层和工作台层，不承载正式执行逻辑。

## 当前状态

当前 `frontend/` 已落地最小可演示平台壳，仍未进入完整产品态。

- 已有：Vite + React + TypeScript 工程、React Router、TanStack Query、Ant Design、Case 列表页、执行列表页、报告详情页、Case 工作台、基础前端测试
- 已补强：用例级 `base_url` 编辑、工作台返回入口、执行详情返回入口、本地草稿缓存与恢复/丢弃交互
- 未落地：登录页、Suite 管理、图表看板、完整定位调试面板、AI 生成入口

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
