# 会话历史删除功能设计

## 背景

当前 AI Planning 会话（session）支持创建、列表、切换，但不支持删除。随着使用积累，会话列表会变长，用户需要清理不再需要的会话。

## 方案

硬删除 + 删除前确认弹窗。

会话是 AI 交互的临时产物，删除后可重新生成，不需要软删除或审计追踪。数据库已配置 CASCADE 约束，删除 session 自动清理 messages 和 drafts。

## 后端

### 新增端点

`DELETE /api/v1/ai-planning/sessions/{session_id}`

- 权限校验：只允许 session 的 `actor_user_id` 删除（复用 `_get_session()` 验证逻辑）
- 数据库硬删除 session 行，CASCADE 自动清理关联的 messages 和 drafts
- 返回 `204 No Content`
- 如果 session 不存在或无权限，返回 `404`

### Service 层

在 `backend/app/services/ai_planning.py` 新增：

```python
def delete_planning_session(db: Session, session_id: int, user_id: int) -> None:
    session = _get_session(db, session_id, user_id)
    db.delete(session)
    db.commit()
```

## 前端

### API 层

在 `frontend/src/services/api.ts` 新增：

```typescript
export async function deletePlanningSession(sessionId: number): Promise<void> {
  await fetch(`${API_BASE}/ai-planning/sessions/${sessionId}`, { method: "DELETE" });
}
```

### UI 变更

修改 `AITestPlanningPanel.tsx` 的会话下拉列表区域：

1. 每个会话列表项右侧加一个垃圾桶图标按钮（小尺寸，hover 时显示）
2. 点击垃圾桶 → 弹出 `window.confirm` 确认对话框
3. 确认后调用删除 API → 刷新会话列表

### 边界情况处理

如果删除的是当前活跃会话：
- 清除 localStorage 中的 `ai_planning_last_session`
- 从刷新后的列表中选择第一个可用会话加载
- 如果没有剩余会话，自动创建新会话

## 不做的事

- 不支持删除单条消息
- 不支持批量删除多个会话
- 不做软删除/回收站
- 不做独立的会话管理页面

## 涉及文件

| 文件 | 变更类型 |
|------|---------|
| `backend/app/api/routes/ai_planning.py` | 新增 DELETE 路由 |
| `backend/app/services/ai_planning.py` | 新增 delete_planning_session 函数 |
| `frontend/src/services/api.ts` | 新增 deletePlanningSession 函数 |
| `frontend/src/components/AITestPlanningPanel.tsx` | 会话列表项增加删除按钮 + 确认逻辑 |
