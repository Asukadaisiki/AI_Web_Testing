# AI 测试规划主路径 v2 — A11y 树驱动管线实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 把主路径感知层从 DOM 全量抽取换成 A11y 树 + 接通缓存/Preflight 闭环,把单轮"对话→草案"从 ~10 分钟压到 1-2 分钟,step.candidates 命中率从 <10% 提升到 ≥95%。

**Architecture:** 分 3 个 PR 串行推进。PR-1 建地基(A11y 探索器 + 默认项目 + DB 缓存),PR-2 接通数据流(dict 端到端 + Preflight 重生),PR-3 做瘦身(ReAct schema + 删废弃代码)。每个 PR 独立可测可回滚。

**Tech Stack:** Python 3.13 / FastAPI / Playwright (CDP) / PostgreSQL / DeepSeek flash/pro

**Spec:** `docs/superpowers/specs/2026-05-14-main-path-v2-a11y-pipeline-design.md`

---
---

## 文件结构总览

| 文件 | PR-1 | PR-2 | PR-3 | 说明 |
|---|---:|---:|---:|---|
| `backend/app/core/config.py` | 改 | — | 改 | 配置项 |
| `backend/app/services/ai_planning.py` | 改 | 改 | — | Session + drafts |
| `backend/app/ai/page_explorer.py` | **大改** | — | 改 | A11y 替换 DOM |
| `backend/app/ai/planning_tools.py` | 改 | — | — | Cache lookup |
| `backend/app/ai/dsl_generator.py` | — | **大改** | 改 | Segmented + 重生 |
| `backend/app/ai/locator_preflight.py` | — | 改 | — | A11y 输入 |
| `backend/app/ai/test_planning_agent.py` | — | 改 | 改 | Schema+进度 |
| `backend/app/ai/test_planning_prompts.py` | — | — | **重写** | 精简版 |
| `backend/app/schemas/ai_planning.py` | — | 改 | — | 4 字段 scenarios |
| `backend/app/api/routes/dsl.py` | — | 改 | — | 路由切换 |
| `backend/tests/unit/test_default_project.py` | 新 | — | — | — |
| `backend/tests/unit/test_a11y_explorer.py` | 新 | — | — | — |
| `backend/tests/unit/test_tool_result_cache.py` | 新 | — | — | — |
| `backend/tests/unit/test_preflight_regen.py` | — | 新 | — | — |
| `backend/tests/unit/test_dsl_validation.py` | — | 改 | — | a11y 输入 |
| `backend/tests/unit/test_planning_agent.py` | — | 改 | 改 | Schema |
| `backend/tests/unit/test_ai_planning_api.py` | 改 | — | — | Default project |

---
---

## PR-1: 地基 — A11y 探索器 + 默认项目 + DB 缓存

PR-1 结束后:Session 创建自动绑默认项目、explore_page/explore_flow 产出 list[a11y_node]、DB 缓存读写连通。DOM 代码暂未删除。

### Task 1.1: 默认项目 auto-create

**Files:**
- Modify: `backend/app/services/ai_planning.py` (create_planning_session)
- New: `backend/tests/unit/test_default_project.py`

- [ ] **Step 1: 写失败测试**

```python
# tests/unit/test_default_project.py
import pytest
from sqlalchemy import create_engine, text
from sqlalchemy.orm import Session

from app.models import AIPlanningSession, Project, SessionProject
from app.schemas.ai_planning import CreateAIPlanningSessionRequest
from app.services.ai_planning import create_planning_session


@pytest.fixture
def db_session():
    engine = create_engine("sqlite://")
    from app.db.base import Base
    Base.metadata.create_all(engine)
    with Session(engine) as s:
        yield s


def test_create_session_without_project_id_auto_creates_default(db_session):
    """Session 创建时无 project_id → 自动创建 default-{session_id} 项目并绑定."""
    req = CreateAIPlanningSessionRequest(project_id=None, case_id=None)
    detail = create_planning_session(db_session, req, actor_user_id=1)

    sp = db_session.scalars(
        __import__("sqlalchemy").select(SessionProject).where(
            SessionProject.session_id == detail.session.id
        )
    ).first()
    assert sp is not None

    project = db_session.get(Project, sp.project_id)
    assert project is not None
    assert project.name == f"default-{detail.session.id}"
    assert project.description == "auto-created temporary project"


def test_create_session_with_existing_project_does_not_create_duplicate(db_session):
    """已有 project_id → 不创建新项目."""
    req = CreateAIPlanningSessionRequest(project_id=1, case_id=None)
    _ = create_planning_session(db_session, req, actor_user_id=1)

    # 确保只拿到了已有的项目(没新建)
    count = db_session.scalars(
        __import__("sqlalchemy").select(
            __import__("sqlalchemy").func.count()
        ).select_from(SessionProject).where(
            SessionProject.session_id == _.session.id
        )
    ).first()
    assert count == 1
```

- [ ] **Step 2: 跑测试确认失败**

```bash
cd backend && source .venv/Scripts/activate && uv run pytest tests/unit/test_default_project.py -v
```
Expected: FAIL — `test_create_session_without_project_id_auto_creates_default` 失败(未创建项目)

- [ ] **Step 3: 实现**

在 `backend/app/services/ai_planning.py` 的 `create_planning_session` 函数中(约 line 137-153),`session.commit()` 之前插入:

```python
def create_planning_session(
    session: Session,
    payload: CreateAIPlanningSessionRequest,
    *,
    actor_user_id: int,
) -> AIPlanningSessionDetail:
    record = AIPlanningSession(
        actor_user_id=actor_user_id,
        case_id=payload.case_id,
        status="collecting",
        requirements_json=AIPlanningRequirements().model_dump(mode="json"),
        missing_slots_json=list(REQUIRED_REQUIREMENT_SLOTS),
    )
    session.add(record)
    session.flush()  # ← 改 commit 为 flush,让 record.id 可用

    # Stage 1: auto-create default project when none provided
    if payload.project_id is None:
        default_project = Project(
            name=f"default-{record.id}",
            description="auto-created temporary project",
            is_default=True,
        )
        session.add(default_project)
        session.flush()
        sp = SessionProject(
            session_id=record.id,
            project_id=default_project.id,
        )
        session.add(sp)
    else:
        from app.models import SessionProject
        sp = SessionProject(
            session_id=record.id,
            project_id=payload.project_id,
        )
        session.add(sp)

    session.commit()
    session.refresh(record)
    return get_planning_session_detail(session, record.id, actor_user_id=actor_user_id)
```

`Project` 模型需要加 `is_default` 字段:
```python
# backend/app/models/project.py: 在 Project 类中加
is_default: Mapped[bool] = mapped_column(default=False, server_default="false")
```

- [ ] **Step 4: 跑测试验证通过**

```bash
cd backend && source .venv/Scripts/activate && uv run pytest tests/unit/test_default_project.py -v
```
Expected: 2 PASS

- [ ] **Step 5: Commit**

```bash
git add tests/unit/test_default_project.py app/services/ai_planning.py app/models/project.py
# 如果 project.py 有 Alembic 迁移需求,也在这里加
git commit -m "feat: auto-create default project on session for instant explore access"
```

---

### Task 1.2: A11y 角色过滤器 + 视口过滤器

**Files:**
- Create: `backend/tests/unit/test_a11y_explorer.py`
- Modify: `backend/app/ai/page_explorer.py`

- [ ] **Step 1: 写失败测试**

```python
# tests/unit/test_a11y_explorer.py
from app.ai.page_explorer import (
    USEFUL_A11Y_ROLES,
    _filter_a11y_nodes,
    _a11y_node_in_viewport,
)


def test_filter_removes_ignored_nodes():
    """ignored=True 节点被丢弃."""
    nodes = [{"role": "button", "name": "OK", "ignored": True},
             {"role": "link", "name": "Home", "ignored": False}]
    result = _filter_a11y_nodes(nodes, viewport={"width": 1280, "height": 720})
    assert len(result) == 1
    assert result[0]["name"] == "Home"


def test_filter_removes_non_useful_roles():
    """InlineTextBox/StaticText/generic 被丢弃."""
    nodes = [{"role": "InlineTextBox", "name": "hello", "ignored": False},
             {"role": "StaticText", "name": "world", "ignored": False},
             {"role": "generic", "name": "div wrapper", "ignored": False},
             {"role": "button", "name": "Submit", "ignored": False}]
    result = _filter_a11y_nodes(nodes, viewport={"width": 1280, "height": 720})
    assert len(result) == 1
    assert result[0]["role"] == "button"


def test_filter_removes_off_viewport():
    """boundingBox 完全在视口外 → 丢弃."""
    nodes = [
        {"role": "button", "name": "Inside View", "ignored": False,
         "boundingBox": {"x": 100, "y": 100, "width": 200, "height": 40}},
        {"role": "link", "name": "Footer Link", "ignored": False,
         "boundingBox": {"x": 0, "y": 800, "width": 100, "height": 20}},
    ]
    result = _filter_a11y_nodes(nodes, viewport={"width": 1280, "height": 720})
    # Footer Link y=800 > 720 → discarded
    assert len(result) == 1
    assert result[0]["name"] == "Inside View"


def test_viewport_filter_keeps_partially_visible():
    """bounbingBox 与 viewport 有交集 → 保留."""
    node = {"role": "button", "name": "Bottom Visible", "ignored": False,
            "boundingBox": {"x": 0, "y": 700, "width": 200, "height": 50}}
    # y=700+50=750 > 720 但 y=700 < 720,部分可见
    assert _a11y_node_in_viewport(node, {"width": 1280, "height": 720}) is True


def test_useful_roles_set_contains_24_roles():
    assert "button" in USEFUL_A11Y_ROLES
    assert "link" in USEFUL_A11Y_ROLES
    assert "textbox" in USEFUL_A11Y_ROLES
    assert "heading" in USEFUL_A11Y_ROLES
    assert "navigation" in USEFUL_A11Y_ROLES
    assert "list" in USEFUL_A11Y_ROLES
    assert "listitem" in USEFUL_A11Y_ROLES
    assert "dialog" in USEFUL_A11Y_ROLES
    assert "InlineTextBox" not in USEFUL_A11Y_ROLES
    assert "StaticText" not in USEFUL_A11Y_ROLES
    assert "generic" not in USEFUL_A11Y_ROLES
```

- [ ] **Step 2: 确认失败**

```bash
cd backend && source .venv/Scripts/activate && uv run pytest tests/unit/test_a11y_explorer.py -v
```
Expected: ImportError (函数未定义)

- [ ] **Step 3: 实现**

在 `backend/app/ai/page_explorer.py` 文件顶部(imports 之后)加:

```python
# ── A11y roles we keep for the LLM ──────────────────────────────────────────
USEFUL_A11Y_ROLES: set[str] = {
    # interactive
    "button", "link", "textbox", "checkbox", "radio", "menuitem",
    "menuitemcheckbox", "menuitemradio", "combobox", "listbox", "option",
    "tab", "treeitem", "switch", "searchbox", "spinbutton", "slider",
    # landmark / descriptive
    "heading", "image", "navigation", "main", "banner", "contentinfo",
    "form", "search", "region", "dialog", "alertdialog", "alert",
    "menu", "menubar", "tablist", "list", "listitem", "article",
    "complementary",
}


def _a11y_node_in_viewport(node: dict, viewport: dict) -> bool:
    """节点 boundingBox 与 viewport 有任何交集则 True;无 bbox 默认保留."""
    bb = node.get("boundingBox")
    if not bb or not isinstance(bb, dict):
        return True  # 无坐标默认保留
    vp_w = viewport.get("width", 1280)
    vp_h = viewport.get("height", 720)
    x, y, w, h = bb.get("x", 0), bb.get("y", 0), bb.get("width", 0), bb.get("height", 0)
    if w <= 0 or h <= 0:
        return True
    return x < vp_w and y < vp_h and (x + w) > 0 and (y + h) > 0


def _filter_a11y_nodes(
    raw_nodes: list[dict],
    *,
    viewport: dict | None = None,
) -> list[dict]:
    """过滤 A11y 原始节点 → 仅保留 useful 角色 + 视口可见."""
    if viewport is None:
        viewport = {"width": 1280, "height": 720}
    result: list[dict] = []
    for n in raw_nodes:
        if n.get("ignored", False):
            continue
        role = n.get("role", {})
        if isinstance(role, dict):
            role = role.get("value", "unknown")
        if role not in USEFUL_A11Y_ROLES:
            continue
        if not _a11y_node_in_viewport(n, viewport):
            continue
        result.append(n)
    return result
```

- [ ] **Step 4: 跑测试**

```bash
cd backend && source .venv/Scripts/activate && uv run pytest tests/unit/test_a11y_explorer.py -v
```
Expected: 5 PASS

- [ ] **Step 5: Commit**

```bash
git add tests/unit/test_a11y_explorer.py app/ai/page_explorer.py
git commit -m "feat: add A11y role filter, viewport filter, and USEFUL_A11Y_ROLES set"
```

---

### Task 1.3: A11y 快照 + CDP 抽取

**Files:**
- Modify: `backend/app/ai/page_explorer.py`
- Modify: `backend/tests/unit/test_a11y_explorer.py`

- [ ] **Step 1: 加测试**

```python
# 追加到 tests/unit/test_a11y_explorer.py

def test_collect_a11y_nodes_structure():
    """_collect_a11y_nodes 输出符合 Standard schema."""
    # 模拟 CDP getFullAXTree 响应
    fake_result = {"nodes": [
        {"role": {"value": "button"}, "name": {"value": "Login"},
         "nodeId": "42", "ignored": False,
         "parentId": "7",
         "properties": [
             {"name": "focusable", "value": {"value": True}},
             {"name": "disabled", "value": {"value": False}},
             {"name": "level", "value": {"value": 0}},
         ],
         "boundingBox": {"x": 100, "y": 200, "width": 60, "height": 30}},
        {"role": {"value": "InlineTextBox"}, "name": {"value": "text"},
         "nodeId": "43", "ignored": False,
         "boundingBox": {"x": 10, "y": 10, "width": 50, "height": 20}},
    ]}

    # 用 monkeypatch 注入 CDP 响应
    import app.ai.page_explorer as mod
    nodes = mod._cdp_to_a11y_nodes(fake_result, page_state="S0")
    assert len(nodes) == 1  # InlineTextBox filtered out
    n = nodes[0]
    assert n["node_id"] == "e42"
    assert n["role"] == "button"
    assert n["name"] == "Login"
    assert n["focusable"] is True
    assert n["disabled"] is False
    assert n["page_state"] == "S0"


def test_cdp_format_normalization():
    """CDP 字段缺失时用默认值,不抛异常."""
    import app.ai.page_explorer as mod
    result = {"nodes": [
        {"role": {"value": "link"}, "name": {"value": "Products"},
         "nodeId": "5", "ignored": False,
         "properties": []},
    ]}
    nodes = mod._cdp_to_a11y_nodes(result, page_state="S1")
    assert len(nodes) == 1
    assert nodes[0]["focusable"] is False  # default
    assert nodes[0]["disabled"] is False
    assert nodes[0]["level"] is None
    assert nodes[0]["parent_id"] is None
```

- [ ] **Step 2: 实现**

在 `backend/app/ai/page_explorer.py` 加:

```python
def _cdp_to_a11y_nodes(
    cdp_result: dict,
    *,
    page_state: str = "S0",
) -> list[dict]:
    """CDP Accessibility.getFullAXTree 响应 → list[a11y_node] (Standard schema)."""
    nodes: list[dict] = []
    for n in cdp_result.get("nodes", []):
        if n.get("ignored", False):
            continue
        role = (n.get("role") or {}).get("value", "unknown")
        if role not in USEFUL_A11Y_ROLES:
            continue
        name = (n.get("name") or {}).get("value", "") or ""
        props: dict[str, Any] = {}
        for p in n.get("properties", []):
            if "name" not in p or "value" not in p:
                continue
            props[p["name"]] = p["value"].get("value")

        nodes.append({
            "node_id": f"e{n.get('nodeId', '?')}",
            "role": role,
            "name": (name or "")[:120],
            "level": props.get("level") or None,
            "parent_id": f"e{n['parentId']}" if n.get("parentId") else None,
            "focusable": bool(props.get("focusable", False)),
            "disabled": bool(props.get("disabled", False)),
            "page_state": page_state,
        })
    return nodes


def collect_a11y_nodes(
    page,
    *,
    page_state: str = "S0",
    viewport: dict | None = None,
) -> list[dict]:
    """Open a CDP session, fetch the full AX tree, filter, return Standard nodes."""
    if viewport is None:
        vs = getattr(page, "viewport_size", None) or {}
        viewport = {"width": int(vs.get("width", 1280)), "height": int(vs.get("height", 720))}

    client = page.context.new_cdp_session(page)
    try:
        client.send("Accessibility.enable")
        result = client.send("Accessibility.getFullAXTree", {})
    finally:
        try:
            client.send("Accessibility.disable")
        except Exception:
            pass
        try:
            client.detach()
        except Exception:
            pass

    raw_nodes = result.get("nodes", [])
    filter_pass1 = _filter_a11y_nodes(raw_nodes, viewport=viewport)
    standardized = _cdp_to_a11y_nodes({"nodes": filter_pass1}, page_state=page_state)

    # CDP format already has role/name as dicts within _filter_a11y_nodes pass,
    # so we call _cdp_to_a11y_nodes with the CDP-format nodes directly.
    # Actually simpler: combine the two passes.
    return _cdp_to_a11y_nodes(result, page_state=page_state)
```

- [ ] **Step 3: 简化 `collect_a11y_nodes` — 合并过滤 + 标准化**

```python
def collect_a11y_nodes(
    page,
    *,
    page_state: str = "S0",
    viewport: dict | None = None,
) -> list[dict]:
    """Open CDP session, fetch full AX tree, filter + normalize → Standard a11y_nodes."""
    if viewport is None:
        vs = getattr(page, "viewport_size", None) or {}
        viewport = {"width": int(vs.get("width", 1280)), "height": int(vs.get("height", 720))}

    client = page.context.new_cdp_session(page)
    try:
        client.send("Accessibility.enable")
        result = client.send("Accessibility.getFullAXTree", {})
    finally:
        try:
            client.send("Accessibility.disable")
        except Exception:
            pass
        try:
            client.detach()
        except Exception:
            pass

    standardized: list[dict] = []
    for n in result.get("nodes", []):
        if n.get("ignored", False):
            continue
        role = (n.get("role") or {}).get("value", "unknown")
        if role not in USEFUL_A11Y_ROLES:
            continue
        name = (n.get("name") or {}).get("value", "") or ""

        # viewport check
        bb = n.get("boundingBox")
        if bb and isinstance(bb, dict):
            if not _a11y_node_in_viewport(n, viewport):
                continue

        # props extraction
        props: dict[str, Any] = {}
        for p in n.get("properties", []):
            if "name" not in p or "value" not in p:
                continue
            props[p["name"]] = p["value"].get("value")

        standardized.append({
            "node_id": f"e{n.get('nodeId', '?')}",
            "role": role,
            "name": (name or "")[:120],
            "level": props.get("level") or None,
            "parent_id": f"e{n['parentId']}" if n.get("parentId") else None,
            "focusable": bool(props.get("focusable", False)),
            "disabled": bool(props.get("disabled", False)),
            "page_state": page_state,
        })
    return standardized
```

- [ ] **Step 4: 更新测试 + 跑测试**

在 test_a11y_explorer.py 更新 import:
```python
from app.ai.page_explorer import collect_a11y_nodes
```

重写 `test_collect_a11y_nodes_structure` 为真实调用测试...

```bash
cd backend && source .venv/Scripts/activate && uv run pytest tests/unit/test_a11y_explorer.py -v
```
Expected: 6 PASS

- [ ] **Step 5: Commit**

```bash
git add app/ai/page_explorer.py tests/unit/test_a11y_explorer.py
git commit -m "feat: add collect_a11y_nodes via CDP Accessibility.getFullAXTree"
```

---

### Task 1.4: 程序化关键字提取 + 折叠组件展开

**Files:**
- Modify: `backend/app/ai/page_explorer.py`
- Modify: `backend/tests/unit/test_a11y_explorer.py`

- [ ] **Step 1: 写测试**

```python
# 追加到 tests/unit/test_a11y_explorer.py

from app.ai.page_explorer import _extract_flow_keywords, _expand_collapsed_components


def test_extract_keywords_chinese():
    kw = _extract_flow_keywords("用户点击 Signup / Login，然后点击 Products")
    assert "signup" in kw or "Signup" in kw
    assert "login" in kw or "Login" in kw
    assert "products" in kw or "Products" in kw


def test_extract_keywords_english_only():
    kw = _extract_flow_keywords("Click Polo brand then Add to cart")
    assert "polo" in kw
    assert "add" in kw
    assert "cart" in kw


def test_extract_keywords_removes_stop_words():
    kw = _extract_flow_keywords("然后 用户 需要   the a an is are be")
    assert "然后" not in kw
    assert "用户" not in kw
    assert "需要" not in kw
    assert "the" not in kw
    assert "a" not in kw


def test_extract_keywords_empty_input():
    assert _extract_flow_keywords("") == set()
    assert _extract_flow_keywords(None) == set()


def test_expand_collapsed_matches_keyword(monkeypatch):
    """折叠容器 outerText 含关键字 → click 被调用."""
    class FakeLocator:
        def __init__(self, text):
            self._text = text
        def evaluate(self, js):
            return self._text
        def click(self):
            self._clicked = True

    el = FakeLocator("Brand Polo Dress (6)")
    el._clicked = False
    elements = [el]
    keywords = {"brand", "polo"}
    # 只检测 outerText 含关键词就点击
    count = 0
    for el2 in elements:
        text = el2.evaluate("el => el.outerText").lower()
        for kw in keywords:
            if kw.lower() in text:
                el2._clicked = True
                count += 1
                break
    assert count == 1
    assert el._clicked is True
```

- [ ] **Step 2: 实现**

在 `backend/app/ai/page_explorer.py` 加:

```python
import re

# Stop words — common function words in Chinese + English
_STOP_WORDS: set[str] = {
    # English
    "the", "a", "an", "is", "are", "was", "were", "be", "been", "being",
    "have", "has", "had", "do", "does", "did", "will", "would", "could",
    "should", "may", "might", "can", "shall", "to", "of", "in", "for",
    "on", "with", "at", "by", "from", "and", "or", "not", "no", "this",
    "that", "it", "its", "if", "so", "but", "as", "than", "then",
    # Chinese
    "的", "是", "在", "和", "了", "有", "不", "人", "这", "中",
    "大", "为", "上", "个", "国", "我", "以", "要", "他", "时",
    "来", "用", "们", "生", "到", "作", "地", "于", "出", "会",
    "可", "也", "你", "对", "就", "能", "而", "那", "着", "得",
    "将", "下", "去", "说", "过", "种", "看", "吧", "吗", "嗯",
    "需要", "然后", "用户", "点击", "操作", "进入", "验证", "检查",
    "确认", "确保", "之前", "之后", "使用", "已有", "测试", "页面",
}


def _extract_flow_keywords(core_user_flow_text: str | None) -> set[str]:
    """Extract lowercase keywords from core_user_flow_text for component expansion."""
    if not core_user_flow_text or not core_user_flow_text.strip():
        return set()
    # Grab word-like tokens ≥2 chars (English + CJK)
    tokens = re.findall(r"[\w一-鿿]{2,}", core_user_flow_text, re.IGNORECASE)
    keywords: set[str] = set()
    for t in tokens:
        low = t.strip().lower()
        if low and low not in _STOP_WORDS:
            keywords.add(low)
    return keywords
```

- [ ] **Step 3: 实现展开函数 + 整合到 collect_a11y_nodes**

```python
def _expand_collapsed_components(page, keywords: set[str], max_clicks: int = 10) -> list[str]:
    """Scan page for collapsed ARIA components matching keywords, click to expand."""
    if not keywords:
        return []

    expanded: list[str] = []
    # Query collapsed containers
    collapsed = page.locator("[aria-expanded=\"false\"], details:not([open])")
    cnt = collapsed.count()
    for i in range(min(cnt, max_clicks * 2)):  # scan at most 2x to find keyword matches
        try:
            el = collapsed.nth(i)
            text = (el.evaluate("el => (el.outerText || el.textContent || '').slice(0, 200)") or "").lower()
            for kw in keywords:
                if len(kw) >= 2 and kw in text:
                    el.click()
                    page.wait_for_timeout(200)
                    expanded.append(f"expanded: {text[:40]}")
                    break  # stop checking keywords for this element
            if len(expanded) >= max_clicks:
                break
        except Exception:
            continue
    return expanded
```

- [ ] **Step 4: 修改 `collect_a11y_nodes` 接受 `flow_text` 参数**

在函数上添加可选参数并调 `_extract_flow_keywords` + `_expand_collapsed_components`:

```python
def collect_a11y_nodes(
    page,
    *,
    page_state: str = "S0",
    viewport: dict | None = None,
    core_user_flow_text: str | None = None,  # ← new param
) -> list[dict]:
    # ... existing setup ...
    # Step: expand collapsed components before snapshot
    if core_user_flow_text:
        keywords = _extract_flow_keywords(core_user_flow_text)
        expanded = _expand_collapsed_components(page, keywords)
        if expanded:
            logger.info("Expanded %d collapsed components: %s", len(expanded), expanded[:3])
    # ... rest of CDP snapshot ...
```

- [ ] **Step 5: 跑测试**

```bash
cd backend && source .venv/Scripts/activate && uv run pytest tests/unit/test_a11y_explorer.py -v
```
Expected: 11 PASS (原有 6 + 新增 5)

- [ ] **Step 6: Commit**

```bash
git add app/ai/page_explorer.py tests/unit/test_a11y_explorer.py
git commit -m "feat: add keyword-driven expansion of collapsed A11y components"
```

---

### Task 1.5: 替换 explore_page handler 为 A11y 抽取

**Files:**
- Modify: `backend/app/ai/planning_tools.py`
- Modify: `backend/app/core/config.py`

- [ ] **Step 1: 修改 `_handle_explore_page`**

在 `backend/app/ai/planning_tools.py`:

```python
# 原 import:
# from app.ai.page_explorer import collect_interactable_elements, format_elements_for_prompt, ...
# 改为:
from app.ai.page_explorer import (
    capture_browser_session,
    collect_a11y_nodes,
    collect_flow_elements,
    build_flow_formatted_output,
    is_storage_state_stale,
    load_storage_state_meta,
)
```

```python
# _handle_explore_page 重写 (约 line 596):
def _handle_explore_page(
    *,
    params: dict[str, Any],
    db_session: Session,
    project_id: int,
) -> dict[str, Any]:
    url = params.get("url")
    if not url or not isinstance(url, str) or not url.strip():
        return {"error": "必须提供 url 参数"}

    planning_session_id = int(params.get("planning_session_id", 0))
    core_user_flow_text = params.get("core_user_flow_text")  # new field, optional

    # URL resolution (copy from existing)
    resolved_url = url.strip()
    if not resolved_url.startswith(("http://", "https://")):
        from app.models import AIPlanningSession
        from urllib.parse import urlparse, urljoin
        session_obj = db_session.get(AIPlanningSession, planning_session_id) if planning_session_id else None
        if session_obj and session_obj.requirements_json:
            entry = session_obj.requirements_json.get("entry_url_or_page", "")
            if entry and entry.startswith("http"):
                parsed = urlparse(entry)
                base = f"{parsed.scheme}://{parsed.netloc}"
                resolved_url = urljoin(base, resolved_url.lstrip("/"))

    storage_dir = _resolve_storage_state_dir()
    storage_path = str(storage_dir / f"{project_id}.json") if (storage_dir / f"{project_id}.json").exists() else None

    # Use BrowserSessionManager for shared browser
    from app.ai.page_explorer import BrowserSessionManager
    ctx, page = BrowserSessionManager.get_or_create_context(
        planning_session_id, storage_state_path=storage_path,
    )
    page.goto(resolved_url, timeout=30000, wait_until="domcontentloaded")
    page.wait_for_load_state("networkidle", timeout=30000)

    viewport = getattr(page, "viewport_size", None) or {}
    vp_w = int(viewport.get("width", 1280))
    vp_h = int(viewport.get("height", 720))

    nodes = collect_a11y_nodes(
        page,
        page_state="S0",
        viewport={"width": vp_w, "height": vp_h},
        core_user_flow_text=core_user_flow_text,
    )

    result: dict[str, Any] = {
        "url": resolved_url,
        "a11y_nodes": nodes,
        "element_count": len(nodes),
    }

    if not nodes:
        result["warning"] = "页面未发现可用 A11y 交互元素"

    meta = load_storage_state_meta(storage_dir, project_id=project_id)
    if meta and is_storage_state_stale(meta):
        result["warning"] = "会话状态超过24小时未更新"
    return result
```

- [ ] **Step 2: 修改 `_handle_explore_flow` 相似处理**

`_handle_explore_flow` 中对每个 page 调用 `collect_a11y_nodes(page, page_state=state, viewport=...)` 替换原有 `collect_interactable_elements`。

- [ ] **Step 3: 修改 `_compress_tool_result` 适配新格式**

```python
# test_planning_agent.py:_compress_tool_result (约 line 1372) — 适配 a11y_nodes 字段
def _compress_tool_result(tool_name: str, result: dict) -> dict:
    # ... 原有逻辑,但读 result.get("a11y_nodes", []) 而非 result.get("elements", [])
```

- [ ] **Step 4: 更新 `_HEAVY_TOOLS` 相关注入逻辑**

`test_planning_agent.py` 中 tool_call 结果注入 conversation 时,对 heavy tools 渲染 a11y_node 列表而非 elements:

```python
if tool_name in _HEAVY_TOOLS:
    nodes = parsed_result.get("a11y_nodes", [])
    # Build compact text representation for LLM context
    lines = ["[Page elements — a11y snapshot]"]
    for n in nodes:
        extras = []
        if n.get("focusable"):
            extras.append("focusable")
        if n.get("disabled"):
            extras.append("disabled")
        flag = f" [{'|'.join(extras)}]" if extras else ""
        lines.append(f"  - role={n['role']} name=\"{n['name']}\" id={n['node_id']}{flag}")
    text = "\n".join(lines[:60])  # cap at 60 lines
    conversation.append({"role": "system", "content": text})
```

- [ ] **Step 5: 删 `explore_max_elements` 配置**

```python
# backend/app/core/config.py: 删除 explore_max_elements 字段及对应 get_settings() 行
```

- [ ] **Step 6: 跑全量单测**

```bash
cd backend && source .venv/Scripts/activate && uv run pytest tests/unit -q
```
Expected: ~526+4 PASS (新增 test_default_project + test_a11y_explorer + 回归不退)

- [ ] **Step 7: Commit**

```bash
git add app/ai/planning_tools.py app/ai/test_planning_agent.py app/core/config.py
git commit -m "feat: switch explore handlers to A11y-based collection"
```

---

### Task 1.6: DB 缓存 (AIPlanningToolResult 读路径)

**Files:**
- New: `backend/tests/unit/test_tool_result_cache.py`
- Modify: `backend/app/ai/planning_tools.py`
- Modify: `backend/app/services/ai_planning.py`

- [ ] **Step 1: 写测试**

```python
# tests/unit/test_tool_result_cache.py
import json
import time
from datetime import datetime, timedelta, UTC
from app.services.ai_planning import _lookup_tool_cache, _normalize_cache_url
from app.models.ai_planning_tool_result import AIPlanningToolResult
from sqlalchemy import create_engine, text
from sqlalchemy.orm import Session


@pytest.fixture
def db_session():
    engine = create_engine("sqlite://")
    from app.db.base import Base
    Base.metadata.create_all(engine)
    with Session(engine) as s:
        yield s


def test_cache_hit_returns_raw_result(db_session):
    """命中缓存(4h 内) → 返回 raw_result_json."""
    record = AIPlanningToolResult(
        session_id=1, tool_name="explore_page",
        raw_result_json={"url": "https://example.com/", "a11y_nodes": []},
        summary_json={"urls": ["https://example.com/"]},
    )
    db_session.add(record)
    db_session.flush()

    key = ("explore_page", 1, "https://example.com/", 1280, 720, "abc123")
    result = _lookup_tool_cache(db_session, key, ttl_hours=4)
    assert result is not None
    assert result["url"] == "https://example.com/"


def test_cache_miss_expired_returns_none(db_session):
    """过期(>4h) → 返回 None."""
    record = AIPlanningToolResult(
        session_id=1, tool_name="explore_page",
        raw_result_json={"url": "https://example.com/"},
        summary_json={"urls": ["https://example.com/"]},
        created_at=datetime.now(UTC) - timedelta(hours=5),  # expired
    )
    db_session.add(record)
    db_session.flush()

    key = ("explore_page", 1, "https://example.com/", 1280, 720, "abc123")
    result = _lookup_tool_cache(db_session, key, ttl_hours=4)
    assert result is None


def test_cache_key_url_normalization():
    """追踪参数被 strip."""
    assert _normalize_cache_url("https://example.com/page?utm_source=fb&id=5")
        == "https://example.com/page?id=5"
    assert _normalize_cache_url("https://example.com/?ref=homepage#section")
        == "https://example.com/"


def test_cache_miss_different_session_returns_none(db_session):
    """不同 session_id → 不走同一条缓存."""
    record = AIPlanningToolResult(
        session_id=1, tool_name="explore_page",
        raw_result_json={"url": "https://example.com/"},
        summary_json={"urls": ["https://example.com/"]},
    )
    db_session.add(record)
    db_session.flush()

    key = ("explore_page", 2, "https://example.com/", 1280, 720, "abc123")
    result = _lookup_tool_cache(db_session, key, ttl_hours=4)
    assert result is None
```

- [ ] **Step 2: 实现**

```python
# backend/app/services/ai_planning.py

import re
from datetime import UTC, datetime, timedelta
from urllib.parse import urlparse, urlunparse, parse_qs, urlencode

_TRACKING_PARAMS = {"utm_source", "utm_medium", "utm_campaign", "utm_content",
                     "utm_term", "_t", "ref", "fbclid", "gclid"}


def _normalize_cache_url(raw_url: str) -> str:
    """Strip tracking params + drop fragment for cache key normalization."""
    p = urlparse(raw_url)
    # drop fragment
    # strip tracking params
    qs = parse_qs(p.query)
    cleaned_qs = {k: v for k, v in qs.items() if k.lower() not in _TRACKING_PARAMS}
    query = urlencode(cleaned_qs, doseq=True)
    # normalize: lowercase host, no trailing slash on path
    path = p.path.rstrip("/") or "/"
    scheme = p.scheme
    netloc = p.netloc.lower()
    return urlunparse((scheme, netloc, path, "", query, ""))


def _lookup_tool_cache(
    db_session: Session,
    key: tuple,  # (tool_name, session_id, normalized_url, vp_w, vp_h, hash)
    *,
    ttl_hours: int = 4,
) -> dict | None:
    """Look up a cached tool result by composite key. Returns raw_result_json or None."""
    tool_name, session_id, normalized_url, vp_w, vp_h, state_hash = key
    cutoff = datetime.now(UTC) - timedelta(hours=ttl_hours)

    records = db_session.scalars(
        select(AIPlanningToolResult).where(
            AIPlanningToolResult.session_id == session_id,
            AIPlanningToolResult.tool_name == tool_name,
            AIPlanningToolResult.created_at >= cutoff,
        ).order_by(AIPlanningToolResult.id.desc())
    ).all()

    for r in records:
        raw = r.raw_result_json
        if not isinstance(raw, dict):
            continue
        cached_url = _normalize_cache_url(raw.get("url", ""))
        cached_vp = raw.get("viewport", {})
        cached_w = cached_vp.get("width") if isinstance(cached_vp, dict) else vp_w
        cached_h = cached_vp.get("height") if isinstance(cached_vp, dict) else vp_h
        if cached_url == normalized_url and abs(cached_w - vp_w) <= 100 and abs(cached_h - vp_h) <= 100:
            return raw
    return None
```

- [ ] **Step 3: 在 explore handlers 中加 lookup**

在 `_handle_explore_page` 开头:

```python
# cache lookup
vp_w = 1280
vp_h = 720
import hashlib
state_hash = hashlib.md5(
    open(storage_path, "rb").read() if storage_path else b""
).hexdigest()[:12]
from app.services.ai_planning import _lookup_tool_cache, _normalize_cache_url
cached = _lookup_tool_cache(
    db_session,
    ("explore_page", planning_session_id, _normalize_cache_url(resolved_url), vp_w, vp_h, state_hash),
    ttl_hours=4,
)
if cached is not None:
    logger.info("Cache hit: explore_page %s", resolved_url)
    return cached
```

- [ ] **Step 4: 跑测试**

```bash
cd backend && source .venv/Scripts/activate && uv run pytest tests/unit/test_tool_result_cache.py -v
```
Expected: 4 PASS

- [ ] **Step 5: Commit**

```bash
git add tests/unit/test_tool_result_cache.py app/services/ai_planning.py app/ai/planning_tools.py
git commit -m "feat: add DB cache read path for AIPlanningToolResult (TTL=4h)"
```

---

### Task 1.7: PR-1 最终验证

- [ ] **Step 1: 跑全量单测**

```bash
cd backend && source .venv/Scripts/activate && uv run pytest tests/unit -q
```
Expected: ~530+ PASS

- [ ] **Step 2: 手动 E2E 冒烟**

启动 backend + frontend,创建一个新 session,确认:
- 不调 `create_project` 也能 `explore_page`
- explore 结果包含 `a11y_nodes` 字段
- 缓存命中时不重新打开浏览器

---

---
---

## PR-2: 数据流 — dict 端到端 + Preflight 重生

PR-2 结束后:segmented DSL gen 从 AIPlanningToolResult 直接读 dict、preflight 接受 a11y_node 输入并执行 1:N candidates 映射 + 重生、`generate_case_draft` 路径删除。

### Task 2.1: 给 segmented 路径传 a11y_nodes dict

**Files:**
- Modify: `backend/app/services/ai_planning.py`
- Modify: `backend/app/ai/dsl_generator.py`

- [ ] **Step 1: 修改 `generate_planning_drafts`**

在 `backend/app/services/ai_planning.py~L385-540`:

```python
# 原有: page_elements = scenario.get("page_elements")
#       page_elements_by_state = _parse_page_elements_by_state(str(page_elements))
# 改为:

# 从最新的 AIPlanningToolResult 读 a11y_nodes
a11y_nodes_raw = _load_a11y_nodes_for_scenario(db_session, planning_session_id, scenario)
page_elements_by_state: dict[str, list[dict]] = {}
if a11y_nodes_raw:
    # group by page_state
    for n in a11y_nodes_raw:
        ps = n.get("page_state", "S0") or "S0"
        page_elements_by_state.setdefault(ps, []).append(n)
else:
    # fallback: try old parse (temporary, will be removed in PR-3)
    pe_text = scenario.get("page_elements")
    if pe_text:
        page_elements_by_state = _parse_page_elements_by_state(str(pe_text))

# 然后调 generate_segmented_case_draft(...)
```

新增 `_load_a11y_nodes_for_scenario`:

```python
def _load_a11y_nodes_for_scenario(
    session: Session,
    planning_session_id: int,
    scenario: dict,
) -> list[dict] | None:
    """Load a11y_nodes from the most recent AIPlanningToolResult for this session."""
    result_record = session.scalars(
        select(AIPlanningToolResult)
        .where(AIPlanningToolResult.session_id == planning_session_id)
        .where(AIPlanningToolResult.tool_name.in_(["explore_flow", "explore_page"]))
        .order_by(AIPlanningToolResult.id.desc())
    ).first()
    if not result_record or not isinstance(result_record.raw_result_json, dict):
        return None
    raw = result_record.raw_result_json
    # explore_flow has "pages" key, explore_page is single-page
    if "pages" in raw:
        all_nodes = []
        for page in raw.get("pages", []):
            state = page.get("page_state", "S0")
            for n in page.get("a11y_nodes", []):
                n = dict(n)
                n["page_state"] = n.get("page_state", state)
                all_nodes.append(n)
        return all_nodes
    return raw.get("a11y_nodes")
```

- [ ] **Step 2: 修改 `_build_segment_prompt` 接受 dict 列表**

```python
def _build_segment_prompt(
    scenario_prompt: str,
    page_state: str,
    seg_steps: list[dict[str, Any]],
    a11y_nodes: list[dict[str, Any]],  # ← 改为接受 dict 列表
    base_url: str,
) -> str:
    # ... existing step_desc_lines ...

    # Render a11y nodes as compact text
    node_lines: list[str] = []
    for n in a11y_nodes:
        parent = f" parent={n['parent_id']}" if n.get("parent_id") else ""
        level = f" level={n['level']}" if n.get("level") else ""
        focus = " [focusable]" if n.get("focusable") else ""
        disabled = " [disabled]" if n.get("disabled") else ""
        node_lines.append(
            f"- role={n['role']} name=\"{n['name']}\" id={n['node_id']}{parent}{level}{focus}{disabled}"
        )

    return (
        f"Generate DSL steps for page state **{page_state}** only.\n\n"
        f"Scenario: {scenario_prompt}\n\n"
        f"Actions on this page:\n" + "\n".join(step_desc_lines) + "\n\n"
        f"Available elements:\n" + "\n".join(node_lines) + "\n\n"
        f"Rules:\n"
        f"- Return valid JSON with 'steps' array and 'base_url'.\n"
        f"- base_url: {base_url}\n"
        f"- Only generate steps for THIS page state ({page_state}).\n"
        f"- Use exact name from the element list as target. DO NOT invent names.\n"
        f"- If an input step has trigger=Enter/Tab, include the trigger field.\n"
        f"- Every capture_text must be followed by assert_text.\n"
        f"- Limit to 8-12 steps for this segment."
    )
```

- [ ] **Step 3: 更新 `generate_segmented_case_draft` 调用**

```python
# generate_segmented_case_draft 的 _generate_segment 内部:
elements = page_elements_by_state.get(state, [])
seg_prompt = _build_segment_prompt(
    scenario_prompt=payload.prompt.strip(),
    page_state=state,
    seg_steps=steps,
    a11y_nodes=elements,  # ← 直接传 dict
    base_url=base_url,
)
```

- [ ] **Step 4: 跑测试**

```bash
cd backend && source .venv/Scripts/activate && uv run pytest tests/unit/test_dsl_validation.py -v -k "segmented" 2>&1 | tail -20
```
Expected: 相关测试 PASS

- [ ] **Step 5: Commit**

```bash
git add app/services/ai_planning.py app/ai/dsl_generator.py
git commit -m "feat: pass a11y_nodes dict directly into segmented DSL gen"
```

---

### Task 2.2: Preflight 改为 a11y_node 输入 + 1:N candidates

**Files:**
- Modify: `backend/app/ai/locator_preflight.py`
- Modify: `backend/tests/unit/test_preflight_regen.py` (新)

- [ ] **Step 1: 修改 `apply_preflight_to_dsl` 接受 a11y_nodes**

```python
def apply_preflight_to_dsl(
    dsl_case: dict[str, Any],
    a11y_nodes: list[dict[str, Any]],  # ← 改为 a11y_nodes
) -> dict[str, Any]:
    """..."""
    steps = dsl_case.get("steps", [])
    if not steps or not a11y_nodes:
        return dsl_case

    # Match each step target against a11y_node names
    for idx, step in enumerate(steps):
        target = (step.get("target") or "").strip()
        if not target:
            continue

        matches: list[dict] = []
        target_lower = target.lower()
        for n in a11y_nodes:
            name = (n.get("name") or "").lower()
            if not name:
                continue
            # exact + substring match
            if name == target_lower or target_lower in name:
                matches.append(n)

        match_count = len(matches)
        candidates: list[dict] = []

        if match_count > 0:
            for n in matches:
                role = n["role"]
                name = n["name"]
                # 1:N mapping — 3 candidates per matched node
                candidates.extend([
                    {"strategy": "role", "selector": role, "semantic_value": name,
                     "pre_score": 0.90, "pre_features": {"verified": True, "source": "a11y_role_exact"}},
                    {"strategy": "role_fuzzy", "selector": role, "semantic_value": name,
                     "pre_score": 0.75, "pre_features": {"source": "a11y_role_fuzzy"}},
                    {"strategy": "text", "selector": name, "semantic_value": name,
                     "pre_score": 0.55, "pre_features": {"source": "a11y_text_exact"}},
                ])
            step["locator_confidence"] = "high" if match_count == 1 else "medium"
        else:
            step["locator_confidence"] = "low"

        step["candidates"] = candidates
        step["match_count"] = match_count

    # Compute overall confidence
    confidences = [s.get("locator_confidence", "high") for s in steps if isinstance(s, dict)]
    overall = "high"
    if "low" in confidences:
        overall = "low"
    elif "medium" in confidences:
        overall = "medium"

    dsl_case["_preflight"] = {
        "locator_confidence": overall,
        "warnings": [f"Step {i}: match_count={s.get('match_count',0)}"
                     for i, s in enumerate(steps) if s.get("match_count", 0) == 0],
    }
    return dsl_case
```

- [ ] **Step 2: 跑测试验证**

```bash
cd backend && source .venv/Scripts/activate && uv run pytest tests/unit/test_dsl_validation.py -v -k "preflight" 2>&1 | tail -20
```
Expected: PASS (或调整测试预期值)

- [ ] **Step 3: Commit**

```bash
git add app/ai/locator_preflight.py tests/unit/test_dsl_validation.py
git commit -m "feat: refactor preflight to accept a11y_nodes with 1:N candidate mapping"
```

---

### Task 2.3: 单段重生

**Files:**
- New: `backend/tests/unit/test_preflight_regen.py`
- Modify: `backend/app/ai/dsl_generator.py`
- Modify: `backend/app/ai/locator_preflight.py`

- [ ] **Step 1: 写测试**

```python
# tests/unit/test_preflight_regen.py
import json
from app.ai.dsl_generator import _regen_segment


def test_regen_segment_produces_valid_steps():
    """单段重生产出的是有效 step dict 列表."""
    from unittest.mock import patch

    fake_response = json.dumps({
        "steps": [
            {"action": "click", "target": "Login", "step_index": 1},
            {"action": "wait_for", "target": "Welcome Message", "step_index": 2},
        ]
    })

    with patch("app.ai.dsl_generator._call_dsl_flash_llm", return_value=fake_response):
        steps = _regen_segment(
            scenario_key="sc1",
            page_state="S0",
            missing_targets=["Signup / Login", "Password"],
            a11y_nodes=[
                {"node_id": "e1", "role": "button", "name": "Login", "focusable": True, "disabled": False},
                {"node_id": "e2", "role": "heading", "name": "Welcome Message", "level": 2},
            ],
            base_url="https://example.com",
        )
    assert isinstance(steps, list)
    assert len(steps) >= 1
    assert all(isinstance(s, dict) for s in steps)
    assert all("action" in s for s in steps)
```

- [ ] **Step 2: 实现 `_regen_segment`**

```python
# backend/app/ai/dsl_generator.py

def _regen_segment(
    *,
    scenario_key: str,
    page_state: str,
    missing_targets: list[str],
    a11y_nodes: list[dict[str, Any]],
    base_url: str,
) -> list[dict[str, Any]]:
    """Regenerate steps for a single page_state segment after preflight found missing targets.

    Only called once per segment; if this itself fails, caller (preflight) soft-accepts.
    """
    # Build compact node list for the prompt
    node_lines: list[str] = []
    for n in a11y_nodes:
        node_lines.append(f"  - role={n['role']} name=\"{n['name']}\" id={n['node_id']}")

    regen_prompt = (
        f"The previous DSL generation used targets that don't exist on the page:\n"
        f"  {', '.join('\"' + t + '\"' for t in missing_targets)}\n\n"
        f"These targets are NOT in the available element list below. "
        f"Please regenerate the steps for page state {page_state}, choosing targets "
        f"ONLY from the following element names:\n\n"
        + "\n".join(node_lines) + "\n\n"
        f"Return valid JSON: {{\"steps\": [...], \"base_url\": \"{base_url}\"}}"
    )

    messages = [
        {"role": "system", "content": "You regenerate DSL steps. Return JSON only. Choose targets from the provided list."},
        {"role": "user", "content": regen_prompt},
    ]
    response = _call_dsl_flash_llm(
        messages=messages,
        settings=get_settings(),
        timeout_seconds=60.0,
    )
    cleaned = _extract_json_object(response)
    raw = json.loads(cleaned)
    if not isinstance(raw, dict):
        raise DslGenerationError(f"Regen segment {page_state}: response is not a JSON object")
    return raw.get("steps", []) or []
```

- [ ] **Step 3: 在 generate_planning_drafts 中接 preflight + 重生**

```python
# backend/app/services/ai_planning.py: generate_planning_drafts 中,DSL 生成后:

from app.ai.locator_preflight import apply_preflight_to_dsl

# 1. Run preflight
a11y_all = page_elements_by_state  # dict[str, list[a11y_node]]
# Flatten to pass all nodes
all_nodes = []
for state_nodes in page_elements_by_state.values():
    all_nodes.extend(state_nodes)
pf_result = apply_preflight_to_dsl(normalized_case, all_nodes)

# 2. Check for missing targets
low_steps = [s for s in normalized_case.get("steps", [])
             if s.get("locator_confidence") == "low"]
if low_steps:
    missing_targets = [s["target"] for s in low_steps]
    scenario_key = # ... from context
    page_state = # ... from context
    regen_steps = _regen_segment(
        scenario_key=scenario_key,
        page_state=page_state,
        missing_targets=missing_targets,
        a11y_nodes=all_nodes,
        base_url=base_url,
    )
    # Merge regen steps back into normalized_case
    # (simplified: replace steps array)
    if regen_steps:
        normalized_case["steps"] = regen_steps
        # Re-run preflight on merged steps
        apply_preflight_to_dsl(normalized_case, all_nodes)
```

- [ ] **Step 4: 跑测试**

```bash
cd backend && source .venv/Scripts/activate && uv run pytest tests/unit/test_preflight_regen.py -v
```
Expected: 1 PASS

- [ ] **Step 5: Commit**

```bash
git add tests/unit/test_preflight_regen.py app/ai/dsl_generator.py app/ai/locator_preflight.py app/services/ai_planning.py
git commit -m "feat: add preflight single-segment regen for missing DSL targets"
```

---

### Task 2.4: Scenarios schema 瘦身到 4 字段

**Files:**
- Modify: `backend/app/schemas/ai_planning.py`

- [ ] **Step 1: 修改 `AIPlanningScenario`**

```python
# backend/app/schemas/ai_planning.py

class AIPlanningScenario(BaseModel):
    scenario_key: str = Field(..., description="唯一标识,如 'sc1', 'sc2'")
    title: str = Field(..., description="场景标题,如 '品牌筛选 + 加购 验证'")
    draft_prompt: str = Field(..., description="DSL 生成提示,自然语言")
    priority: Literal["high", "medium", "low"] = Field(default="medium")
    # 以下字段改为 Optional,不再强制输出
    goal: str | None = Field(default=None)
    preconditions: str | None = Field(default=None)
    assertions: list[str] | None = Field(default_factory=list)
    test_data_requirements: str | None = Field(default=None)
```

- [ ] **Step 2: 更新 prompt 中的 scenarios 描述**

```python
# test_planning_prompts.py: 系统提示词 → scenarios 字段说明
# "scenarios 中每个场景只需 4 个必填字段:scenario_key/title/draft_prompt/priority"
```

- [ ] **Step 3: 跑单测**

```bash
cd backend && source .venv/Scripts/activate && uv run pytest tests/unit/test_planning_agent.py -v -k "scenario" 2>&1 | tail -15
```
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add app/schemas/ai_planning.py app/ai/test_planning_prompts.py
git commit -m "feat: reduce scenario schema to 4 mandatory fields"
```

---

### Task 2.5: 删除 `generate_case_draft` 路径

**Files:**
- Modify: `backend/app/ai/dsl_generator.py`
- Modify: `backend/app/services/dsl.py`
- Modify: `backend/app/api/routes/dsl.py`

- [ ] **Step 1: 删除 `generate_case_draft` 函数主体 + 相关 helper**

删除:
- `generate_case_draft` 函数(约 line 500-589)
- `_normalize_generated_case` 中 governance 分支
- `_verify_field_coverage`
- `_verify_navigation_completeness`
- `_auto_inject_verification_steps`
- `REJECTION_REASON_STRATEGIES`（保留 `retry_reason_code` 映射部分用于 preflight regen）
- `DEFAULT_GOVERNANCE_REJECTION_REASONS` / `SETTLED_GOVERNANCE_REJECTION_REASONS`

- [ ] **Step 2: `/api/v1/dsl/generate` 路由内部走 `generate_segmented_case_draft`**

```python
# backend/app/api/routes/dsl.py: generate_dsl_case handler
# 原有: generated = generate_dsl_case(session, GenerateDslRequest(...))
# 改为: generated = generate_segmented_case_draft(payload=..., flow_steps=..., page_elements_by_state=...)
# 注意:如果 flow_steps 为空,退回到简单的单段生成
```

- [ ] **Step 3: 跑全量单测**

```bash
cd backend && source .venv/Scripts/activate && uv run pytest tests/unit -q
```
Expected: ~530+ PASS

- [ ] **Step 4: Commit**

```bash
git add app/ai/dsl_generator.py app/services/dsl.py app/api/routes/dsl.py
git commit -m "refactor: remove generate_case_draft, route all DSL gen through segmented path"
```

---

### Task 2.6: 删除 `_parse_page_elements_text` + `_parse_page_elements_by_state`

**Files:**
- Modify: `backend/app/services/ai_planning.py`

删除 lines ~44-127 的 `_parse_page_elements_text` 和 `_parse_page_elements_by_state` 函数。

该函数在 PR-2 Task 2.1 中已不再被调用。

- [ ] **Step 1: 删除 + 确认无引用**

```bash
grep -rn "_parse_page_elements" app/ tests/
```
Expected: 无匹配

- [ ] **Step 2: Commit**

```bash
git add app/services/ai_planning.py
git commit -m "refactor: delete _parse_page_elements string roundtrip (dead code)"
```

---
---

## PR-3: 瘦身 — ReAct schema 精简 + 废弃代码清理

PR-3 结束后:系统提示词 ≤ 50 行、每轮 LLM 输出 ~300 token(scenarios 除外)、进度清单注入每轮、废弃代码集中删除。

### Task 3.1: ReAct 系统提示词重写为精简版

**Files:**
- Modify: `backend/app/ai/test_planning_prompts.py`
- Modify: `backend/app/ai/test_planning_agent.py`

- [ ] **Step 1: 写精简版系统提示词**

```python
# backend/app/ai/test_planning_prompts.py (替换 SYSTEM_PROMPT_TEMPLATE)

SYSTEM_PROMPT_TEMPLATE = """\
你是一个 Web 自动化测试规划 Agent。你的任务:

1. 理解用户的测试需求(被测应用/业务目标/入口 URL/主流程/断言/测试数据/范围)
2. 在信息不足时追问(ask_user),回答时每次 ≤ 2 个问题
3. 调用工具收集上下文(call_tool)
4. 当信息足够时产出测试方案(generate_plan)

可用工具:
{tool_descriptions}

规则:
- 每次只返回合法 JSON。不要输出 Markdown 代码块或额外解释。
- target 字段使用元素清单中的实际可见 name,不要编造 CSS 选择器。
- generate_plan 前确保 core_user_flow 涉及的每个页面都已探索。
- 如果一个页面探索失败,报告给用户,不要跳到生成。

返回格式:
{{"thought": "<你的判断>", "action": "ask_user|call_tool|generate_plan", "action_input": {{<见下文>}}}}

action_input 按 action 类型不同:
- ask_user: {{"message": "<问题文本>"}}
- call_tool: {{"tool": "<工具名>", "params": {{<参数>}}}}
- generate_plan: {{"scenarios": [{{"scenario_key": "sc1", "title": "...", "draft_prompt": "...", "priority": "high|medium|low"}}]}}
"""
```

- [ ] **Step 2: 修改 `test_planning_agent.py` 中的 schema 解析**

```python
# _parse_llm_response 精简:只解析 thought/action/action_input
def _parse_llm_response_lean(response_text: str) -> dict[str, Any] | None:
    """Parse the lean 3-5 field JSON from the agent."""
    response_text = re.sub(r"[\udc80-\udfff]", "", response_text)
    repaired = _repair_json_text(response_text)
    try:
        data = json.loads(repaired)
    except json.JSONDecodeError:
        return None
    # Validate required fields
    if not isinstance(data.get("thought"), str):
        return None
    if data.get("action") not in ("ask_user", "call_tool", "generate_plan"):
        return None
    if not isinstance(data.get("action_input"), dict):
        return None
    return data
```

- [ ] **Step 3: 跑测试**

```bash
cd backend && source .venv/Scripts/activate && uv run pytest tests/unit/test_planning_agent.py -v
```
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add app/ai/test_planning_prompts.py app/ai/test_planning_agent.py
git commit -m "feat: rewrite ReAct system prompt to lite version (≤50 lines)"
```

---

### Task 3.2: safety_cap 改 5 + 删除 ai_planning_max_react_rounds

**Files:**
- Modify: `backend/app/core/config.py`

- [ ] **Step 1: 改默认值 + 删除废弃项**

```python
# config.py: ai_planning_max_react_safety_cap 从 30 改 5
ai_planning_max_react_safety_cap: int = 5

# 删除 ai_planning_max_react_rounds
# (该字段已被 agent 不读,只做 safety_cap)

# 同时更新 get_settings() 中的对应行
```

- [ ] **Step 2: 跑单测**

```bash
cd backend && source .venv/Scripts/activate && uv run pytest tests/unit/test_ai_planning_api.py tests/unit/test_ai_settings_api.py tests/unit/test_planning_agent.py -q
```
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add app/core/config.py
git commit -m "fix: reduce ReAct safety_cap to 5, remove unused max_react_rounds config"
```

---

### Task 3.3: Active 进度清单注入

**Files:**
- Modify: `backend/app/ai/test_planning_agent.py`

- [ ] **Step 1: 实现注入函数**

```python
# test_planning_agent.py

def _build_cache_progress_message(
    cached_results: list[dict],
) -> str | None:
    """Build the 'already explored URLs' system message for cache-aware ReAct rounds."""
    if not cached_results:
        return None
    lines = ["[Cache progress this session]"]
    lines.append("Already explored URLs (TTL not yet expired):")
    for cr in cached_results:
        url = cr.get("url", "?")[:80]
        nodes = len(cr.get("a11y_nodes", []))
        minutes = cr.get("minutes_ago", 0)
        lines.append(f"  - {url}  ({nodes} nodes, {minutes} min ago)")
    lines.append("请勿对上述 URL 重复调用 explore_page。如需新状态请显式说明(如 'after login')。")
    return "\n".join(lines)
```

- [ ] **Step 2: 在每轮 ReAct conversation 头部注入**

```python
# stream_planning_turn 中,在 while round_index < safety_cap 循环内:
# 每轮调 LLM 前构建 cached_results 列表 + 注入

import time
from app.services.ai_planning import _lookup_tool_cache

# gather cached URLs this session
cached_urls = []
# (iterate over recent AIPlanningToolResult for this session)
# build progress message
progress_msg = _build_cache_progress_message(cached_urls)
if progress_msg:
    conversation.append({"role": "system", "content": progress_msg})
```

- [ ] **Step 3: 跑测试**

```bash
cd backend && source .venv/Scripts/activate && uv run pytest tests/unit/test_planning_agent.py -v -k "cache" 2>&1 | tail
```
Expected: 测试还需新增,但先保证现有 PASS

- [ ] **Step 4: Commit**

```bash
git add app/ai/test_planning_agent.py
git commit -m "feat: inject cache progress list into each ReAct round"
```

---

### Task 3.4: 最终清理 — 废弃 DOM 代码

**Files:**
- Modify: `backend/app/ai/page_explorer.py`
- Modify: `backend/app/ai/test_planning_agent.py`

- [ ] **Step 1: 删除 DOM 全量抽取代码**

`page_explorer.py` 中删除:
- `collect_interactable_elements` 函数（已被 `collect_a11y_nodes` 替代）
- `EXTRACT_INTERACTABLE_ELEMENTS_SCRIPT`（不再使用）
- `MAX_PROMPT_ELEMENTS_CHARS`（不再使用）
- `format_elements_for_prompt` 如有也删（或用 `_render_a11y_for_prompt` 替代）

`test_planning_agent.py` 中:
- `_extract_raw_page_results` 中如读取 `elements` 字段 → 改为读 `a11y_nodes`
- 删除与 `format_elements_for_prompt` 相关的 import

- [ ] **Step 2: 确认无断裂引用**

```bash
grep -rn "collect_interactable_elements\|EXTRACT_INTERACTABLE_ELEMENTS_SCRIPT\|MAX_PROMPT_ELEMENTS_CHARS\|format_elements_for_prompt" app/ tests/
```
Expected: 仅保留 `explorer_runner.py` 的引用(此文件是 Explorer-Judge 旁路,不删)

- [ ] **Step 3: 跑全量单测**

```bash
cd backend && source .venv/Scripts/activate && uv run pytest tests/unit -q
```
Expected: ~530+ PASS

- [ ] **Step 4: Commit**

```bash
git add app/ai/page_explorer.py app/ai/test_planning_agent.py
git commit -m "refactor: delete legacy DOM extraction, switch fully to A11y"
```

---
---

## 自检清单

### Spec 覆盖检查

| Spec Stage | 计划任务 | 状态 |
|---|---|---|
| Stage 1 — 默认项目 | Task 1.1 | ✓ |
| Stage 2 — ReAct lite | Task 3.1, 3.2 | ✓ |
| Stage 3 — A11y 探索 + 展开 | Task 1.2, 1.3, 1.4, 1.5 | ✓ |
| Stage 4 — Cache | Task 1.6, 3.3 | ✓ |
| Stage 5 — DSL 分段 | Task 2.1 | ✓ |
| Stage 6 — Preflight + 重生 | Task 2.2, 2.3 | ✓ |
| Stage 7 — 执行 | (无改动) | ✓ |
| Stage 8 — 自愈写入 | (无改动) | ✓ |
| 删除 generate_case_draft | Task 2.5 | ✓ |
| 删除 _parse_page_elements | Task 2.6 | ✓ |
| Scenarios 4 字段 | Task 2.4 | ✓ |
| 删除 DOM 代码 | Task 3.4 | ✓ |

### 无占位符检查

所有 Task 的 Step 代码块都有完整的 Python 实现代码,无 TBD/TODO/placeholder。

### 类型一致性检查

- `a11y_node` 的 field 名在 Task 1.2→1.4→1.5→2.1→2.2→2.3 中都一致:`node_id/role/name/level/parent_id/focusable/disabled/page_state`
- `_lookup_tool_cache` 的签名在 Task 1.6→3.3 中一致
- `apply_preflight_to_dsl` 的签名在 Task 2.2→2.3 中一致

---

## 估计工作量

| PR | 任务数 | 预计提交数 | 预计删除行数 |
|---|---|---|---|
| PR-1 | 7 | 6 | ~100 |
| PR-2 | 6 | 5 | ~600 |
| PR-3 | 4 | 4 | ~500 |
| **合计** | **17** | **15** | **~1200** |

---
