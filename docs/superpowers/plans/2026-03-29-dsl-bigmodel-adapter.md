# DSL BigModel Adapter Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [x]`) syntax for tracking.

**Goal:** 让 DSL 生成链路兼容智谱 `glm-4.7-flash` 的 `chat/completions` 请求格式，同时保持非智谱 provider 的现有行为不回归。

**Architecture:** 保持现有 DSL 生成入口、配置结构和前端设置页不变，只在 `backend/app/ai/dsl_generator.py` 的请求层按 `base_url/model` 做 provider 自适配。测试先覆盖 OpenAI 兼容分支与 BigModel 分支的 payload 差异，再更新本地 `.env` 和示例配置。

**Tech Stack:** Python, FastAPI backend, pytest, urllib JSON HTTP client, local `.env`

---

### Task 1: 为 DSL 请求体增加 provider 级回归测试

**Files:**
- Modify: `backend/tests/unit/test_dsl_validation.py`
- Modify: `backend/app/ai/dsl_generator.py`

- [x] **Step 1: Write the failing test**

```python
def test_call_llm_uses_glm_bigmodel_payload(monkeypatch) -> None:
    ...
    _call_llm(
        messages=[{"role": "user", "content": "生成 DSL"}],
        api_key="glm-key",
        model="glm-4.7-flash",
        base_url="https://open.bigmodel.cn/api/paas/v4",
        timeout_seconds=5,
    )
    assert captured["json"]["thinking"] == {"type": "enabled"}
    assert "response_format" not in captured["json"]
```

- [x] **Step 2: Run test to verify it fails**

Run: `cd backend && uv run pytest tests/unit/test_dsl_validation.py -k "call_llm_uses_glm_bigmodel_payload or call_llm_uses_openai_json_payload" -q`
Expected: FAIL because `_call_llm()` 还没有智谱 payload 分支。

- [x] **Step 3: Write minimal implementation**

```python
if _should_use_glm_chat_completion(base_url=base_url, model=model):
    payload.update(
        thinking={"type": "enabled"},
        max_tokens=65536,
        temperature=1.0,
    )
else:
    payload["response_format"] = {"type": "json_object"}
```

- [x] **Step 4: Run test to verify it passes**

Run: `cd backend && uv run pytest tests/unit/test_dsl_validation.py -k "call_llm_uses_glm_bigmodel_payload or call_llm_uses_openai_json_payload" -q`
Expected: PASS

- [x] **Step 5: Commit**

```bash
git add backend/app/ai/dsl_generator.py backend/tests/unit/test_dsl_validation.py
git commit -m "fix: adapt dsl generator payload for bigmodel glm"
```

### Task 2: 更新 DSL 本地配置与示例配置

**Files:**
- Modify: `backend/.env.example`
- Modify: `backend/.env`

- [x] **Step 1: Write the failing test**

```python
def test_env_example_includes_ai_dsl_and_vlm_settings() -> None:
    assert "AI_DSL_BASE_URL=https://open.bigmodel.cn/api/paas/v4" in env_text
```

- [x] **Step 2: Run test to verify it fails**

Run: `cd backend && uv run pytest tests/unit/test_config.py::test_env_example_includes_ai_dsl_and_vlm_settings -q`
Expected: FAIL because `.env.example` 仍是 OpenAI DSL 默认值。

- [x] **Step 3: Write minimal implementation**

```dotenv
ENABLE_AI_DSL_GENERATE=true
AI_DSL_BASE_URL=https://open.bigmodel.cn/api/paas/v4
AI_DSL_MODEL=glm-4.7-flash
```

- [x] **Step 4: Run test to verify it passes**

Run: `cd backend && uv run pytest tests/unit/test_config.py::test_env_example_includes_ai_dsl_and_vlm_settings -q`
Expected: PASS

- [x] **Step 5: Commit**

```bash
git add backend/.env.example
git commit -m "docs: update dsl env example for bigmodel"
```

### Task 3: 验证 DSL 生成链路与真实请求

**Files:**
- Modify: `docs/execution-log.md`
- Modify: `docs/bug-log.md`

- [x] **Step 1: Run targeted backend tests**

Run: `cd backend && uv run pytest tests/unit/test_dsl_validation.py tests/unit/test_config.py tests/unit/test_ai_settings_api.py -q`
Expected: PASS

- [x] **Step 2: Run one real BigModel smoke request**

Run: `cd backend && uv run python -`
Expected: `POST https://open.bigmodel.cn/api/paas/v4/chat/completions` 返回 `200 OK`，能提取 `choices[0].message.content`。

- [x] **Step 3: Append task logs**

```md
- 记录 DSL BigModel 适配、测试与真实联调结果
```

- [x] **Step 4: Review final diff**

Run: `git diff -- backend/app/ai/dsl_generator.py backend/tests/unit/test_dsl_validation.py backend/tests/unit/test_config.py backend/.env.example docs/execution-log.md docs/bug-log.md`
Expected: 仅包含本次 DSL 适配相关改动
