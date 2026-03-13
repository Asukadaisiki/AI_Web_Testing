# AI 自动化测试增强项目规划（基于 Midscene 思路）

## 文档定位

本文件是项目的核心规划文档，用于定义产品目标、总体架构、核心模块和主开发路线。

- `docs/project-plan.md` 必须从本规划展开，只负责执行分解、阶段安排和验收。
- `docs/frontend-design.md` 必须服务于本规划中的平台层、工作台层和报告展示目标。
- 如果其他文档与本文件冲突，以本文件为准。

## 一、项目目标总结

本项目参考 Midscene 的 **AI + 浏览器自动化**设计思路，但重点不是复刻 UI Agent，而是针对自动化测试场景进行增强。目标是构建一个 **AI 自动化测试执行系统**，解决传统 AI UI automation 在测试场景中的三个关键问题：

1. **元素定位稳定性**
    通过 **DOM + 视觉混合定位**，提高 AI 自动化执行稳定性。
2. **执行结果可解释性**
    构建 **步骤级测试报告系统**，帮助 AI 和人类分析问题。
3. **测试流程管理**
    支持 **测试 DSL、测试用例、冒烟测试与回归测试执行**。

------

# 二、系统整体架构

系统将采用 **五层架构**：

1. **Planner 层**
    自然语言 → 测试步骤 DSL
2. **Locator 层**
    DOM + Vision 混合元素定位
3. **Executor 层**
    Playwright 执行测试步骤
4. **Reporter 层**
    步骤级证据采集与报告生成
5. **Suite Manager 层**
    测试用例与测试套件管理

------

# 三、核心模块设计

## 1. 混合元素定位系统（核心技术点）

**目标：**
 解决 AI UI 自动化中最常见的 **元素识别不稳定问题**。

**设计策略：四层降级定位**

系统采用四层降级架构，依次尝试直至命中或标记人工干预：

- **Tier 0 — 人工修正记录**
  查询 `locator_corrections` 表，若有历史人工修正的 selector 且仍活跃，优先使用；命中后 `verified_count++`，连续失败 3 次自动停用。

- **Tier 1 — DOM 语义定位（现有能力）**
  通过以下属性召回候选元素并打分命中：
  - role / text / aria-label / placeholder / data-testid
  - visible / enabled / viewport 校验
  - 候选打分 → 最高分命中 → 失败原因记录

- **Tier 2 — AI 视觉定位**
  截图 → 发送给 VLM（qwen-vl / gemini / gpt-4o 等）→ 返回 bbox 坐标 → `elementFromPoint` 获取 DOM 元素 → 交叉验证语义匹配。
  支持 deepLocate 两阶段定位（先粗区域再裁剪放大精确定位）。

- **Tier 3 — 标记需要人工干预**
  记录完整上下文（截图、URL、DOM 快照、AI 候选、Tier 1 trace），标记 execution 状态为 `needs_intervention`，等待用户在前端提交修正。

**闭环机制：** 用户提交修正 → 存入 `locator_corrections` → 重跑时 Tier 0 命中 → 后续同页面同目标自动复用。页面改版导致 selector 失效时自动停用，触发新一轮人工干预。

> 详细技术设计参见 [`docs/hybrid-locate-and-intervention-design.md`](./hybrid-locate-and-intervention-design.md)

------

## 2. 测试 DSL（测试流程表达层）

设计 **测试 DSL** 用于描述自动化测试步骤，例如：

```
{  "name": "登录冒烟测试",  "steps": [    {"action": "goto", "value": "/login"},    {"action": "input", "target": "用户名输入框", "value": "admin"},    {"action": "click", "target": "登录按钮"},    {"action": "assert_url_contains", "value": "/dashboard"}  ]}
```

DSL 可以：

- 由 **AI 自动生成**
- 也支持 **手动编辑**

------

## 3. 执行证据与报告系统

每一步执行记录 **结构化证据**，包括：

- 操作步骤
- 页面截图
- DOM 摘要
- URL
- Console 日志
- Network 摘要
- 执行结果
- 失败原因

报告系统需要同时支持：

### 人类可读报告

用于：

- 测试人员
- 开发人员
- 调试与复盘

### AI 可分析结构化报告

用于：

- AI 自动失败分析
- 自动生成 Bug 分析
- 自动归因

------

## 4. 测试套件管理

测试组织结构：

```
Step → Case → Suite
```

示例：

### Suite

- Smoke Test
- Regression Test
- Auth Test

支持能力：

- 批量执行
- 失败用例重跑
- 历史记录追踪

------

# 四、开发路线图

## 阶段一：基础执行能力（第1–2周）

- 搭建 **Playwright 执行框架**
- 实现基础 Step 执行
  - goto
  - input
  - click
  - assert
- 保存截图与基本执行日志
- 初版 **HTML 测试报告**

------

## 阶段二：混合定位系统（第3–5周）

- 实现 **DOM 候选元素召回**与候选打分命中（Tier 1，已有基础）
- 新建 **人工修正记录** 数据模型与 API，接入 Tier 0 优先查找
- 实现 **AI 视觉定位模块**（Tier 2）：VLM API 调用、bbox 归一化、deepLocate 两阶段定位、DOM 交叉验证
- 实现 **四层降级定位链路** `resolve_with_fallback`，统一替换现有定位调用
- 实现 **人工干预机制**（Tier 3）：上下文采集、`needs_intervention` 状态、前端干预面板
- 打通 **修正闭环**：提交修正 → 重跑命中 → 置信度追踪 → 失效自动停用

------

## 阶段三：测试 DSL 与自然语言生成（第5周）

- 定义 **测试 DSL**
- 实现

```
DSL → Playwright 执行器
```

- 接入 **LLM 生成 DSL**

------

## 阶段四：测试报告系统（第6周）

- 步骤级证据记录
- 执行轨迹可视化
- 失败分类
- AI 失败分析

------

## 阶段五：测试套件与回归执行（第7周）

- 用例管理
- 冒烟测试执行
- 回归测试执行
- 历史结果对比

------

# 五、项目亮点（适合写入简历）

- 设计 **自然语言 → 测试 DSL** 转换层，实现 AI 驱动测试编排
- 实现 **DOM + Vision 混合定位系统**，提高自动化稳定性
- 构建 **步骤级测试证据报告系统**，支持 AI 失败归因
- 支持 **冒烟测试与回归测试批量执行**
