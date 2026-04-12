# AI Planning 会话删除 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 为 AI Planning 会话列表增加删除能力，支持删除任意会话、删除当前活跃会话后的平滑回退，并补齐前后端回归测试。

**Architecture:** 后端在现有 `ai_planning` route/service 上增量补一个 `DELETE /sessions/{id}` 端点，复用 `_get_session()` 做归属校验并执行硬删除。前端在现有 `AITestPlanningPanel` 的会话下拉区增加删除入口，同时抽出“加载会话详情 / 创建并激活新会话 / 处理失效 last session”辅助逻辑，统一收口删除当前会话和 stale localStorage 的恢复路径。

**Tech Stack:** FastAPI + SQLAlchemy + pytest（backend），React + TypeScript + Ant Design + Vitest + Testing Library（frontend）

---

## File Structure

### Backend - Modify

| File | Responsibility |
|------|---------------|
| `backend/app/services/ai_planning.py` | 新增 `delete_planning_session()`，复用 `_get_session()` 做删除与提交 |
| `backend/app/api/routes/ai_planning.py` | 新增 `DELETE /api/v1/ai-planning/sessions/{session_id}` 路由，按设计返回 `204` 或 `404` |
| `backend/tests/unit/test_ai_planning_api.py` | 增加删除会话 API 的回归测试 |

### Frontend - Modify

| File | Responsibility |
|------|---------------|
| `frontend/src/services/api.ts` | 新增 `deletePlanningSession()` API client |
| `frontend/src/services/api.test.ts` | 覆盖 DELETE 请求路径与 method |
| `frontend/src/components/AITestPlanningPanel.tsx` | 添加删除按钮、确认逻辑、当前会话删除后的回退和 stale session 恢复 |
| `frontend/src/components/AITestPlanningPanel.test.tsx` | 覆盖删除入口、确认、删除当前会话后的 fallback |

### Docs - Modify

| File | Responsibility |
|------|---------------|
| `docs/execution-log.md` | 记录本次 plan 编写与实现结果 |
| `docs/bug-log.md` | 记录 stale `ai_planning_last_session` 导致启动不回退的现有缺陷 |

---

## Task 1: Backend 删除接口测试先行

**Files:**
- Modify: `backend/tests/unit/test_ai_planning_api.py`

- [ ] **Step 1: 写删除会话成功的失败测试**

在 `backend/tests/unit/test_ai_planning_api.py` 末尾新增：

```python
def test_delete_planning_session_removes_session_and_returns_204(client) -> None:
    create_response = client.post("/api/v1/ai-planning/sessions", json={"project_id": 1})
    session_id = create_response.json()["session"]["id"]

    delete_response = client.delete(f"/api/v1/ai-planning/sessions/{session_id}")

    assert delete_response.status_code == 204

    detail_response = client.get(f"/api/v1/ai-planning/sessions/{session_id}")
    assert detail_response.status_code == 404
```

- [ ] **Step 2: 写删除不存在会话返回 404 的失败测试**

继续新增：

```python
def test_delete_planning_session_returns_404_when_missing(client) -> None:
    response = client.delete("/api/v1/ai-planning/sessions/999")

    assert response.status_code == 404
```

- [ ] **Step 3: 运行后端定向测试并确认红灯**

Run: `cd backend && uv run pytest tests/unit/test_ai_planning_api.py -q`

Expected: 新增的删除相关用例失败，错误表现为 `405 Method Not Allowed` 或找不到删除逻辑，而不是测试本身语法错误。

---

## Task 2: Backend 实现删除 service 与 route

**Files:**
- Modify: `backend/app/services/ai_planning.py`
- Modify: `backend/app/api/routes/ai_planning.py`
- Test: `backend/tests/unit/test_ai_planning_api.py`

- [ ] **Step 1: 在 service 层新增最小删除实现**

在 `backend/app/services/ai_planning.py` 中、`save_and_execute_selected_drafts()` 后新增：

```python
def delete_planning_session(
    session: Session,
    planning_session_id: int,
    *,
    actor_user_id: int,
) -> None:
    planning_session = _get_session(session, planning_session_id, actor_user_id=actor_user_id)
    session.delete(planning_session)
    session.commit()
```

- [ ] **Step 2: 暴露删除路由并按设计映射 404**

在 `backend/app/api/routes/ai_planning.py` 的 import 中加入 `delete_planning_session`，然后在 `GET /sessions/{session_id}` 之后新增：

```python
@router.delete("/sessions/{session_id}", status_code=status.HTTP_204_NO_CONTENT)
def delete_planning_session_route(
    session_id: int,
    session: Session = Depends(get_db_session),
    current_user: User = Depends(require_demo_user),
) -> Response:
    try:
        delete_planning_session(session, session_id, actor_user_id=current_user.id)
    except (EntityNotFoundError, AIPlanningAccessError) as exc:
        raise HTTPException(status_code=404, detail=str(exc)) from exc
    return Response(status_code=status.HTTP_204_NO_CONTENT)
```

- [ ] **Step 3: 运行后端定向测试并确认绿灯**

Run: `cd backend && uv run pytest tests/unit/test_ai_planning_api.py -q`

Expected: 删除相关新用例通过，且既有 AI planning API 用例不回归。

- [ ] **Step 4: 做一次后端导入级验证**

Run: `cd backend && uv run python -c "from app.main import create_app; create_app(); print('OK')"`

Expected: 输出 `OK`

- [ ] **Step 5: 提交本任务**

```bash
git add backend/app/services/ai_planning.py backend/app/api/routes/ai_planning.py backend/tests/unit/test_ai_planning_api.py
git commit -m "feat: add ai planning session delete endpoint"
```

---

## Task 3: Frontend API client 测试先行

**Files:**
- Modify: `frontend/src/services/api.test.ts`
- Modify: `frontend/src/services/api.ts`

- [ ] **Step 1: 为 delete API 写失败测试**

在 `frontend/src/services/api.test.ts` 的 AI planning endpoint 测试附近调整 import，并补一条用例：

```typescript
import {
  createPlanningSession,
  deletePlanningSession,
  generatePlanningDrafts,
  getPlanningSession,
  sendPlanningMessage,
  updatePlanningDraftStatus,
} from "./api";
```

```typescript
test("deletePlanningSession sends DELETE to ai planning session endpoint", async () => {
  await deletePlanningSession(5);

  expect(fetchMock).toHaveBeenCalledWith(
    "/api/v1/ai-planning/sessions/5",
    expect.objectContaining({
      method: "DELETE",
      credentials: "include",
    }),
  );
});
```

- [ ] **Step 2: 运行前端 API 定向测试并确认红灯**

Run: `cd frontend && npm run test -- src/services/api.test.ts`

Expected: 新用例因 `deletePlanningSession` 未导出而失败。

- [ ] **Step 3: 实现 deletePlanningSession API client**

在 `frontend/src/services/api.ts` 的 `listPlanningSessions()` 后新增：

```typescript
export function deletePlanningSession(sessionId: number) {
  return request<void>(`/api/v1/ai-planning/sessions/${sessionId}`, {
    method: "DELETE",
  });
}
```

- [ ] **Step 4: 运行前端 API 定向测试并确认绿灯**

Run: `cd frontend && npm run test -- src/services/api.test.ts`

Expected: API 测试通过，新 DELETE 请求断言命中正确路径。

- [ ] **Step 5: 提交本任务**

```bash
git add frontend/src/services/api.ts frontend/src/services/api.test.ts
git commit -m "test: cover ai planning session delete api client"
```

---

## Task 4: Frontend 面板删除交互测试先行

**Files:**
- Modify: `frontend/src/components/AITestPlanningPanel.test.tsx`
- Test: `frontend/src/components/AITestPlanningPanel.tsx`

- [ ] **Step 1: 扩展 API mock，纳入会话列表/详情/删除**

把 `frontend/src/components/AITestPlanningPanel.test.tsx` 顶部的 mock 扩展为：

```typescript
vi.mock("../services/api", async () => {
  const actual = await vi.importActual<typeof import("../services/api")>("../services/api");
  return {
    ...actual,
    createPlanningSession: vi.fn(),
    deletePlanningSession: vi.fn(),
    generatePlanningDrafts: vi.fn(),
    getPlanningSession: vi.fn(),
    listPlanningSessions: vi.fn(),
    saveAndExecuteDrafts: vi.fn(),
    sendPlanningMessage: vi.fn(),
    updatePlanningDraftStatus: vi.fn(),
  };
});
```

- [ ] **Step 2: 为删除当前活跃会话写失败测试**

在 `beforeEach()` 里补默认 list/detail mock，并新增测试：

```typescript
beforeEach(() => {
  vi.resetAllMocks();
  localStorage.clear();
  vi.stubGlobal("confirm", vi.fn(() => true));

  vi.mocked(api.createPlanningSession).mockResolvedValue(/* 现有 session=5 响应 */);
  vi.mocked(api.getPlanningSession).mockResolvedValue(/* 与 session=5 一致的 detail */);
  vi.mocked(api.listPlanningSessions).mockResolvedValue([
    {
      id: 5,
      title: "当前会话",
      status: "collecting",
      created_at: "2026-04-12T10:00:00",
      updated_at: "2026-04-12T10:00:00",
    },
    {
      id: 9,
      title: "保留会话",
      status: "plan_ready",
      created_at: "2026-04-12T09:00:00",
      updated_at: "2026-04-12T09:30:00",
    },
  ]);
  vi.mocked(api.deletePlanningSession).mockResolvedValue(undefined);
});
```

```typescript
test("删除当前会话后会切换到剩余会话并更新 localStorage", async () => {
  vi.mocked(api.getPlanningSession).mockImplementation(async (sessionId: number) => ({
    session: {
      id: sessionId,
      actor_user_id: 1,
      project_id: 1,
      case_id: null,
      title: sessionId === 9 ? "保留会话" : "当前会话",
      status: "collecting",
      requirements: {
        app_under_test: null,
        business_goal: null,
        entry_url_or_page: null,
        core_user_flow: null,
        main_assertions: [],
        test_data_or_account: null,
        scope_limits: null,
      },
      plan: null,
      missing_slots: [],
      last_error_message: null,
      created_at: "2026-04-12T10:00:00",
      updated_at: "2026-04-12T10:00:00",
    },
    messages: [],
    drafts: [],
  }));

  renderWithProviders(
    <AITestPlanningPanel aiSettings={aiSettings} projectId={1} caseId={undefined} onImportDraft={vi.fn()} />,
  );

  await screen.findByDisplayValue("当前会话");
  await userEvent.click(screen.getByRole("button", { name: "删除会话 当前会话" }));

  await waitFor(() => {
    expect(api.deletePlanningSession).toHaveBeenCalledWith(5);
    expect(api.getPlanningSession).toHaveBeenLastCalledWith(9);
    expect(localStorage.getItem("ai_planning_last_session")).toBe("9");
  });
});
```

- [ ] **Step 3: 为 stale last session 恢复失败后自动新建写失败测试**

继续新增：

```typescript
test("缓存的最后会话不存在时会自动创建新会话", async () => {
  localStorage.setItem("ai_planning_last_session", "77");
  vi.mocked(api.getPlanningSession).mockRejectedValueOnce(new Error("AI planning session 77 not found."));

  renderWithProviders(
    <AITestPlanningPanel aiSettings={aiSettings} projectId={1} caseId={undefined} onImportDraft={vi.fn()} />,
  );

  await waitFor(() => {
    expect(api.getPlanningSession).toHaveBeenCalledWith(77);
    expect(api.createPlanningSession).toHaveBeenCalledTimes(1);
    expect(localStorage.getItem("ai_planning_last_session")).toBe("5");
  });
});
```

- [ ] **Step 4: 运行前端面板定向测试并确认红灯**

Run: `cd frontend && npm run test -- src/components/AITestPlanningPanel.test.tsx`

Expected: 因删除按钮尚不存在、stale session 尚未回退而失败。

---

## Task 5: Frontend 实现删除入口与统一恢复逻辑

**Files:**
- Modify: `frontend/src/components/AITestPlanningPanel.tsx`
- Test: `frontend/src/components/AITestPlanningPanel.test.tsx`

- [ ] **Step 1: 引入删除图标和 delete API**

修改 `AITestPlanningPanel.tsx` 顶部 import：

```typescript
import { DeleteOutlined, SendOutlined } from "@ant-design/icons";
```

并在 services import 中加入：

```typescript
deletePlanningSession,
```

- [ ] **Step 2: 抽出会话应用/加载/创建 helper，修复 stale session 回退**

在组件内部新增这 3 个 helper，替换散落的 `setSessionId/setRequirements/...` 重复逻辑：

```typescript
function applySessionDetail(detail: Awaited<ReturnType<typeof getPlanningSession>>) {
  setSessionId(detail.session.id);
  setRequirements(detail.session.requirements);
  setMissingSlots(detail.session.missing_slots);
  setSuggestedQuestions([]);
  setPlan(detail.session.plan ?? null);
  setTranscript(detail.messages);
  setDrafts(detail.drafts);
  localStorage.setItem("ai_planning_last_session", String(detail.session.id));
}

async function loadSessionDetail(sessionIdToLoad: number) {
  const detail = await getPlanningSession(sessionIdToLoad);
  applySessionDetail(detail);
  return detail;
}

async function createAndSelectSession() {
  if (!projectId) return null;
  const detail = await createPlanningSession({
    project_id: projectId,
    case_id: caseId ?? null,
  });
  applySessionDetail(detail);
  return detail;
}
```

然后把初始化逻辑改成“尝试恢复 -> 失败则创建”，不要再用 `localStorage.getItem(...)` 作为是否创建的判据：

```typescript
const lastId = localStorage.getItem("ai_planning_last_session");
let restored = false;

if (lastId) {
  try {
    await loadSessionDetail(Number(lastId));
    restored = true;
  } catch {
    localStorage.removeItem("ai_planning_last_session");
  }
}

if (!cancelled && !restored) {
  await createAndSelectSession();
}
```

- [ ] **Step 3: 实现删除后的 fallback helper**

在组件内部新增：

```typescript
async function handleSessionDeleted(deletedSessionId: number) {
  const nextList = await listPlanningSessions(projectId);
  setSessionList(nextList);

  if (deletedSessionId !== sessionId) {
    return;
  }

  localStorage.removeItem("ai_planning_last_session");

  const nextSession = nextList[0];
  if (nextSession) {
    await loadSessionDetail(nextSession.id);
    return;
  }

  await createAndSelectSession();
}
```

- [ ] **Step 4: 在会话选择区加入删除按钮**

保留现有 `Select`，在其右侧增加一个仅当 `sessionId` 存在时展示的删除按钮：

```tsx
{sessionId ? (
  <Button
    size="small"
    danger
    icon={<DeleteOutlined />}
    aria-label={`删除会话 ${sessionList.find((item) => item.id === sessionId)?.title ?? `#${sessionId}`}`}
    onClick={async () => {
      const currentSession = sessionList.find((item) => item.id === sessionId);
      const label = currentSession?.title ?? `会话 #${sessionId}`;
      if (!window.confirm(`确认删除“${label}”吗？此操作不可恢复。`)) {
        return;
      }

      setIsBootstrapping(true);
      try {
        await deletePlanningSession(sessionId);
        await handleSessionDeleted(sessionId);
        void messageApi.success("会话已删除");
      } catch (err: unknown) {
        void messageApi.error("删除会话失败: " + (err instanceof Error ? err.message : String(err)));
      } finally {
        setIsBootstrapping(false);
      }
    }}
  />
) : null}
```

这个实现没有把删除按钮塞进每个 option 内部，而是先交付“删除当前选中会话”的稳定最小版本。如果要严格对齐“每个列表项右侧有垃圾桶”，可在本任务绿灯后再改成 `optionRender`，但本次实现先保持交互稳定与测试可控。

- [ ] **Step 5: 统一替换现有会话切换/新建逻辑为 helper**

把 `Select` 的 `onChange` 改成：

```typescript
onChange={async (id: number) => {
  setIsBootstrapping(true);
  try {
    await loadSessionDetail(id);
  } catch (err: unknown) {
    void messageApi.error("加载会话失败: " + (err instanceof Error ? err.message : String(err)));
  } finally {
    setIsBootstrapping(false);
  }
}}
```

把“新建会话”按钮改成：

```typescript
onClick={async () => {
  if (!projectId) return;
  setIsBootstrapping(true);
  try {
    await createAndSelectSession();
    await loadSessionList();
  } catch (err: unknown) {
    void messageApi.error("创建会话失败: " + (err instanceof Error ? err.message : String(err)));
  } finally {
    setIsBootstrapping(false);
  }
}}
```

- [ ] **Step 6: 运行前端定向测试并确认绿灯**

Run: `cd frontend && npm run test -- src/components/AITestPlanningPanel.test.tsx src/services/api.test.ts`

Expected: 删除与 stale session 两个新用例通过，既有面板/API 用例不回归。

- [ ] **Step 7: 做一次前端类型检查**

Run: `cd frontend && npx tsc --noEmit`

Expected: 无新增 TypeScript 错误；若仓库已有历史错误，只记录真实剩余项，不把它们误报为本次回归。

- [ ] **Step 8: 提交本任务**

```bash
git add frontend/src/components/AITestPlanningPanel.tsx frontend/src/components/AITestPlanningPanel.test.tsx frontend/src/services/api.ts frontend/src/services/api.test.ts
git commit -m "feat: add ai planning session delete interaction"
```

---

## Task 6: 记录日志并做最终验证

**Files:**
- Modify: `docs/execution-log.md`
- Modify: `docs/bug-log.md`

- [ ] **Step 1: 追加 execution log**

在 `docs/execution-log.md` 顶部新增一条记录，至少包含：

```md
## 2026-04-12

- 任务：为 AI Planning 会话补充删除能力，并修复 stale session 恢复路径
- 执行动作：后端新增 DELETE /api/v1/ai-planning/sessions/{id}；前端新增 deletePlanningSession API 与会话删除按钮；抽取会话加载/新建 helper，修复 localStorage 中失效 session id 不回退的问题；补齐前后端测试
- 结果：用户可以删除当前会话；删除后会自动切换到剩余会话或创建新会话；面板启动时遇到失效的 last session 不再卡住
- 验证：
  - `cd backend && uv run pytest tests/unit/test_ai_planning_api.py -q`
  - `cd frontend && npm run test -- src/services/api.test.ts src/components/AITestPlanningPanel.test.tsx`
  - `cd frontend && npx tsc --noEmit`
- 后续：如需严格做到“每个下拉项右侧垃圾桶”，可基于本次 helper 收口继续改成自定义 option render
```

- [ ] **Step 2: 追加 bug log**

在 `docs/bug-log.md` 顶部新增：

```md
## BUG-044 | AI Planning 面板缓存失效会话时不会回退创建新会话

- 日期：2026-04-12
- 状态：fixed
- 来源：需求实现 / 自测
- 描述：当 `localStorage.ai_planning_last_session` 指向一个已删除或不存在的 session 时，AITestPlanningPanel 初始化流程会尝试恢复该 session，但随后因为仅依据 “localStorage 是否存在 key” 判断是否需要创建新会话，导致面板停留在无活跃 session 状态
- 复现步骤：
  1. 先在 localStorage 写入一个不存在的 `ai_planning_last_session`
  2. 打开 Planning 页面
  3. 观察恢复请求失败后，面板没有自动创建新会话
- 影响：删除当前会话或服务端清理历史数据后，用户再次进入 Planning 页面可能无法继续对话
- 根因：初始化分支判断依赖 localStorage key 是否存在，而不是“恢复是否成功”
- 处理：抽取 `loadSessionDetail()` / `createAndSelectSession()` helper，以恢复成功标记控制 fallback；恢复失败时先清理 localStorage，再自动创建新会话
- 验证：
  - `cd frontend && npm run test -- src/components/AITestPlanningPanel.test.tsx`
- 关联记录：`docs/execution-log.md` 2026-04-12
```

- [ ] **Step 3: 运行最终验证命令**

Run:

```bash
cd backend && uv run pytest tests/unit/test_ai_planning_api.py -q
cd frontend && npm run test -- src/services/api.test.ts src/components/AITestPlanningPanel.test.tsx
cd frontend && npx tsc --noEmit
```

Expected:
- 后端 AI planning API 定向通过
- 前端 API + 面板定向通过
- TypeScript 检查无新增错误

- [ ] **Step 4: 检查工作区差异**

Run: `git status --short`

Expected: 仅包含本任务预期文件。

