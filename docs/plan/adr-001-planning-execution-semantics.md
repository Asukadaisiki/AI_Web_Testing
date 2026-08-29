# ADR-001：Planning 与执行语义基线

日期：2026-08-28
状态：accepted

## 背景

当前 Planning Session 数据模型允许关联多个项目，但部分核心逻辑只读取 `project_ids[0]`；VLM 默认启用，与 DOM/A11y-first 产品目标不一致；同步与流式执行分别维护生命周期逻辑，存在状态和 evidence 分叉风险。

## 决策

### 1. Planning Session 使用单 active project

- M1 中每个 Planning Session 只有一个 active project。
- 历史多项目关联暂不删除，读取时必须显式选择 active project，禁止隐式依赖无序的 `project_ids[0]`。
- P3 先增加 active project 语义和迁移兼容，再收窄写入 API；不得直接删除关联表或历史数据。

### 2. VLM 默认关闭

- DOM/A11y semantic locator 和已验证 candidate 是默认路径。
- VLM 仅在显式设置 `ENABLE_AI_VISUAL_LOCATE=true` 后参与 fallback。
- VLM 失败不得改变结构化 DSL 校验和 evidence 记录要求。

### 3. 同步与流式执行共享单事件源

- Runner 只解释已校验 DSL 并产生步骤事件。
- Execution Service 是状态迁移、事务和持久化唯一事实源。
- 同步 API 等待事件源完成，SSE API 转发同一事件源；不得保留两套执行实现。

## 影响

- P3 需要引入 active project 访问边界，并为历史 session 提供确定性兼容策略。
- P4 需要定义统一执行状态机和事件 schema。
- 配置默认值变化只影响未显式设置 VLM 开关的环境；需要配置测试覆盖。

## 验收

1. 默认配置下 VLM 不发起请求。
2. Planning 代码中不再直接使用无语义的首个 project ID。
3. 同一 DSL 经同步和流式入口产生一致的最终状态、步骤顺序和 evidence。
