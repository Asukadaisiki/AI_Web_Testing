# AI 视觉定位 + DOM 辅助 + 人工干预 设计文档

## 文档定位

本文件是 **阶段 2：混合定位系统** 的详细技术设计文档，对应核心规划中的"混合元素定位系统"。本文档基于对 Midscene（`d:\AutoTestingLearingProject\midscene`）源码的深入分析，提取其 AI 视觉定位策略的核心知识，结合本项目现有架构，设计一套 **AI 视觉定位 + DOM 辅助 + 人工干预** 的三层定位体系。

---

## 一、整体架构：四层降级定位

```
定位请求: target="登录按钮", page_url="https://app.com/login"
        │
        ▼
┌── Tier 0: 人工修正记录 ──────────────────────────────────┐
│  查询 locator_corrections 表                              │
│  WHERE target_description = "登录按钮"                    │
│  AND page_url_pattern = "https://app.com/login"           │
│  AND is_active = true                                     │
│  命中 → 用修正的 selector 直接定位 → 成功 → verified_count++ │
│  未命中 → 进入 Tier 1                                     │
└───────────────────────────────────────────────────────────┘
        │
        ▼
┌── Tier 1: DOM 语义定位（现有能力）─────────────────────────┐
│  即现有 resolve_semantic_locator()                         │
│  候选召回: button_role / label / placeholder / text        │
│  候选打分: visible + enabled + strategy_base_score         │
│  命中 → 返回 ResolvedLocator                              │
│  未命中 → 进入 Tier 2                                     │
└───────────────────────────────────────────────────────────┘
        │
        ▼
┌── Tier 2: AI 视觉定位 ──────────────────────────────────┐
│  截图 → 发送给 VLM → 返回 bbox 坐标                      │
│  坐标转像素 → elementFromPoint 获取 DOM 元素              │
│  交叉验证: DOM 元素属性是否与 target 语义匹配              │
│  命中 → 返回定位结果 + 写入 XPath 缓存                    │
│  未命中 → 进入 Tier 3                                     │
└───────────────────────────────────────────────────────────┘
        │
        ▼
┌── Tier 3: 标记为需要人工干预 ────────────────────────────┐
│  记录完整上下文:                                          │
│    - 当前截图                                             │
│    - 页面 URL                                             │
│    - DOM 快照（所有可交互元素）                            │
│    - AI 候选结果（如果有）                                 │
│    - Tier 1 的 LocatorTrace                               │
│  标记 step 状态为 "failed"                                │
│  标记 execution 状态为 "needs_intervention"                │
└───────────────────────────────────────────────────────────┘
```

---

## 二、Tier 2 具体设计：AI 视觉定位模块

### 2.1 Midscene 的 AI 定位本质（协议层）

Midscene 的 AI 定位在协议层非常简单，核心只有 4 步：

1. 截图 → base64
2. 拼装 system prompt + user prompt + image
3. 调用 OpenAI 兼容 API（支持 qwen-vl、gemini、gpt-4o 等）
4. 解析返回的 bbox 坐标 → 转为像素坐标

Midscene 源码的关键 prompt 在 `packages/core/src/ai-model/prompt/llm-locator.ts`：

```
## Role:
You are an AI assistant that helps identify UI elements.

## Objective:
- Identify elements in screenshots that match the user's description.
- Provide the coordinates of the element that matches the user's description.

## Important Notes for Locating Elements:
- When the user describes an element that contains text (such as buttons, input fields,
  dropdown options, radio buttons, etc.), you should locate ONLY the text region of that
  element, not the entire element boundary.

## Output Format:
{
  "bbox": [xmin, ymin, xmax, ymax],  // 2d bounding box
  "errors"?: string[]
}
```

user prompt 极简：`Find: ${targetElementDescription}`

### 2.2 不同模型的 bbox 适配

不同 VLM 返回坐标格式不同，需要归一化处理。来源：`packages/core/src/common.ts`

| 模型族 | 原始格式 | 归一化方式 |
|--------|---------|-----------|
| 默认（qwen-vl 等） | `[xmin,ymin,xmax,ymax]` 归一化 0-1000 | `value / 1000 * imageSize` |
| Gemini | `[ymin,xmin,ymax,xmax]` 归一化 0-1000 | 交换 x/y 后同上 |
| Qwen 2.5 | 像素坐标 | 直接使用 |

### 2.3 deepLocate 两阶段定位（Midscene 的精度优化策略）

来源：`packages/core/src/service/index.ts` 的 `locate()` 方法

当 deepLocate 启用时：

1. **阶段 1 — Section Locate**：让 AI 找到目标所在的大致区域（section），返回粗粒度 bbox
2. **扩展搜索区域**：将区域扩展（至少 400x400 px），确保不会裁剪太小丢失上下文
3. **裁剪 + 放大**：从原始截图裁剪该区域，放大 2 倍
4. **阶段 2 — Element Locate**：在放大后的局部图上精确定位元素
5. **坐标回算**：将局部图上的坐标映射回原始截图坐标系

这是 Midscene 在视觉定位精度上的核心优化手段。

### 2.4 Python 实现方案

在 `backend/app/locators/` 下新建 `ai_visual.py`：

```python
# backend/app/locators/ai_visual.py
"""AI visual locator — 基于截图的视觉元素定位"""

from __future__ import annotations

import base64
from dataclasses import dataclass
from typing import Literal

ModelFamily = Literal["qwen-vl", "gemini", "gpt-4o", "qwen2.5-vl"]

SYSTEM_PROMPT = """
## Role:
You are an AI assistant that helps identify UI elements.

## Objective:
- Identify elements in screenshots that match the user's description.
- Provide the coordinates of the element that matches the user's description.

## Important Notes for Locating Elements:
- When the user describes an element that contains text (such as buttons, input fields,
  dropdown options, radio buttons, etc.), you should locate ONLY the text region of that
  element, not the entire element boundary.

## Output Format:
```json
{
  "bbox": [number, number, number, number],  // [xmin, ymin, xmax, ymax]
  "errors"?: string[]
}
```
"""


@dataclass(frozen=True)
class AILocateResult:
    """AI 视觉定位结果"""
    center: tuple[int, int]           # 中心点像素坐标 (x, y)
    bbox: tuple[int, int, int, int]   # [xmin, ymin, xmax, ymax] 像素坐标
    confidence: float                 # 0-1，是否有 errors
    raw_response: str                 # 原始 AI 响应，用于调试


def locate_element_by_vision(
    screenshot_base64: str,
    target_description: str,
    image_width: int,
    image_height: int,
    model_family: ModelFamily = "qwen-vl",
    deep_locate: bool = False,
) -> AILocateResult | None:
    """
    核心入口：用 VLM 在截图中定位元素。

    参数:
        screenshot_base64: 页面截图的 base64 编码
        target_description: 元素的自然语言描述，如 "登录按钮"
        image_width: 截图宽度（像素）
        image_height: 截图高度（像素）
        model_family: 使用的模型族
        deep_locate: 是否启用两阶段定位

    返回:
        AILocateResult 或 None（未找到时）
    """
    if deep_locate:
        return _deep_locate(screenshot_base64, target_description,
                           image_width, image_height, model_family)

    return _single_locate(screenshot_base64, target_description,
                         image_width, image_height, model_family)


def _single_locate(
    screenshot_base64: str,
    target_description: str,
    image_width: int,
    image_height: int,
    model_family: ModelFamily,
) -> AILocateResult | None:
    """单阶段定位"""
    messages = [
        {"role": "system", "content": SYSTEM_PROMPT},
        {"role": "user", "content": [
            {"type": "image_url", "image_url": {"url": screenshot_base64, "detail": "high"}},
            {"type": "text", "text": f"Find: {target_description}"},
        ]},
    ]

    response = _call_vlm(messages, model_family)
    parsed = _parse_bbox_response(response, image_width, image_height, model_family)
    return parsed


def _deep_locate(
    screenshot_base64: str,
    target_description: str,
    image_width: int,
    image_height: int,
    model_family: ModelFamily,
) -> AILocateResult | None:
    """
    两阶段定位（参考 Midscene deepLocate）：
    1. 先找区域（section）
    2. 裁剪放大后精确定位
    """
    # 阶段 1: 找区域
    section_result = _locate_section(screenshot_base64, target_description,
                                     image_width, image_height, model_family)
    if section_result is None:
        # 区域都找不到，回退单阶段
        return _single_locate(screenshot_base64, target_description,
                             image_width, image_height, model_family)

    # 扩展搜索区域（至少 400x400，参考 Midscene expandSearchArea）
    search_area = _expand_search_area(section_result.bbox, image_width, image_height,
                                      min_size=400)

    # 裁剪 + 放大 2x
    cropped_base64, crop_offset = _crop_and_scale(screenshot_base64, search_area, scale=2)
    cropped_width = (search_area[2] - search_area[0]) * 2
    cropped_height = (search_area[3] - search_area[1]) * 2

    # 阶段 2: 在放大区域中精确定位
    local_result = _single_locate(cropped_base64, target_description,
                                  cropped_width, cropped_height, model_family)
    if local_result is None:
        return None

    # 坐标回算到原始截图坐标系
    global_center = (
        int(local_result.center[0] / 2 + crop_offset[0]),
        int(local_result.center[1] / 2 + crop_offset[1]),
    )
    global_bbox = (
        int(local_result.bbox[0] / 2 + crop_offset[0]),
        int(local_result.bbox[1] / 2 + crop_offset[1]),
        int(local_result.bbox[2] / 2 + crop_offset[0]),
        int(local_result.bbox[3] / 2 + crop_offset[1]),
    )

    return AILocateResult(
        center=global_center,
        bbox=global_bbox,
        confidence=local_result.confidence,
        raw_response=local_result.raw_response,
    )


def _locate_section(
    screenshot_base64: str,
    target_description: str,
    image_width: int,
    image_height: int,
    model_family: ModelFamily,
) -> AILocateResult | None:
    """阶段 1: 找到目标所在的区域"""
    section_prompt = f"Find the section/area that contains: {target_description}"
    return _single_locate(screenshot_base64, section_prompt,
                         image_width, image_height, model_family)


def _expand_search_area(
    bbox: tuple[int, int, int, int],
    image_width: int,
    image_height: int,
    min_size: int = 400,
) -> tuple[int, int, int, int]:
    """
    扩展搜索区域，确保至少 min_size x min_size。
    参考 Midscene 的 expandSearchArea。
    """
    xmin, ymin, xmax, ymax = bbox
    width = xmax - xmin
    height = ymax - ymin

    # 如果太小，向四周扩展
    if width < min_size:
        expand = (min_size - width) // 2
        xmin = max(0, xmin - expand)
        xmax = min(image_width, xmax + expand)
    if height < min_size:
        expand = (min_size - height) // 2
        ymin = max(0, ymin - expand)
        ymax = min(image_height, ymax + expand)

    # 额外加 20% 边距
    margin_x = int((xmax - xmin) * 0.2)
    margin_y = int((ymax - ymin) * 0.2)
    xmin = max(0, xmin - margin_x)
    ymin = max(0, ymin - margin_y)
    xmax = min(image_width, xmax + margin_x)
    ymax = min(image_height, ymax + margin_y)

    return (xmin, ymin, xmax, ymax)


def _adapt_bbox(
    raw_bbox: list[int | float],
    image_width: int,
    image_height: int,
    model_family: ModelFamily,
) -> tuple[int, int, int, int] | None:
    """
    不同模型返回不同格式的 bbox，统一适配为像素坐标 [xmin,ymin,xmax,ymax]。
    参考 Midscene 的 adaptBbox / normalized01000 等函数。
    """
    if not raw_bbox or len(raw_bbox) < 4:
        return None

    if model_family == "gemini":
        # Gemini 返回 [ymin, xmin, ymax, xmax] 归一化 0-1000
        ymin, xmin, ymax, xmax = raw_bbox[:4]
        return (
            int(xmin / 1000 * image_width),
            int(ymin / 1000 * image_height),
            int(xmax / 1000 * image_width),
            int(ymax / 1000 * image_height),
        )
    elif model_family == "qwen2.5-vl":
        # Qwen 2.5 返回像素坐标
        return (int(raw_bbox[0]), int(raw_bbox[1]), int(raw_bbox[2]), int(raw_bbox[3]))
    else:
        # 默认: [xmin,ymin,xmax,ymax] 归一化 0-1000
        xmin, ymin, xmax, ymax = raw_bbox[:4]
        return (
            int(xmin / 1000 * image_width),
            int(ymin / 1000 * image_height),
            int(xmax / 1000 * image_width),
            int(ymax / 1000 * image_height),
        )


def _parse_bbox_response(
    raw_response: str,
    image_width: int,
    image_height: int,
    model_family: ModelFamily,
) -> AILocateResult | None:
    """解析 AI 返回的 JSON，提取 bbox"""
    import json
    try:
        data = json.loads(raw_response)
    except json.JSONDecodeError:
        return None

    raw_bbox = data.get("bbox", [])
    errors = data.get("errors", [])

    if not raw_bbox or len(raw_bbox) < 4:
        return None

    bbox = _adapt_bbox(raw_bbox, image_width, image_height, model_family)
    if bbox is None:
        return None

    xmin, ymin, xmax, ymax = bbox
    center = ((xmin + xmax) // 2, (ymin + ymax) // 2)

    return AILocateResult(
        center=center,
        bbox=bbox,
        confidence=0.0 if errors else 1.0,
        raw_response=raw_response,
    )


def _call_vlm(messages: list[dict], model_family: ModelFamily) -> str:
    """
    调用 VLM API。
    使用 OpenAI 兼容接口，支持 qwen-vl / gemini / gpt-4o 等。
    实现时需要根据实际使用的 API provider 配置 base_url 和 api_key。
    """
    # TODO: 实现实际的 API 调用
    # 建议使用 openai SDK，配置不同 provider 的 base_url
    # 例如:
    #   from openai import OpenAI
    #   client = OpenAI(base_url="...", api_key="...")
    #   response = client.chat.completions.create(
    #       model="qwen-vl-max",
    #       messages=messages,
    #       response_format={"type": "json_object"},
    #   )
    #   return response.choices[0].message.content
    raise NotImplementedError("需要实现实际的 VLM API 调用")


def _crop_and_scale(
    screenshot_base64: str,
    area: tuple[int, int, int, int],
    scale: int = 2,
) -> tuple[str, tuple[int, int]]:
    """
    裁剪截图中的指定区域并放大。
    返回 (裁剪放大后的 base64, 裁剪偏移量)。
    """
    # TODO: 使用 Pillow 实现裁剪和缩放
    # from PIL import Image
    # import io
    # img = Image.open(io.BytesIO(base64.b64decode(screenshot_base64)))
    # cropped = img.crop(area)
    # scaled = cropped.resize((cropped.width * scale, cropped.height * scale))
    # ...
    raise NotImplementedError("需要实现图片裁剪和缩放")
```

### 2.5 AI 定位结果与 DOM 交叉验证

AI 返回坐标后，需要在该坐标处查询 DOM 元素进行验证。在 Runner 中实现：

```python
def _ai_locate_with_dom_verify(page, target: str, screenshot_base64: str) -> ResolvedLocator | None:
    """AI 视觉定位 + DOM 交叉验证"""
    image_size = page.viewport_size
    result = locate_element_by_vision(
        screenshot_base64=screenshot_base64,
        target_description=target,
        image_width=image_size["width"],
        image_height=image_size["height"],
    )
    if result is None:
        return None

    # 在 AI 返回的坐标处查询 DOM 元素
    element_handle = page.evaluate_handle(
        "([x, y]) => document.elementFromPoint(x, y)",
        [result.center[0], result.center[1]],
    )

    # 获取元素信息用于验证
    element_info = page.evaluate(
        """(element) => {
            if (!element || !(element instanceof Element)) return null;
            return {
                tag: element.tagName.toLowerCase(),
                text: (element.innerText || element.textContent || "").trim().slice(0, 120),
                role: element.getAttribute("role"),
                aria_label: element.getAttribute("aria-label"),
                placeholder: element.getAttribute("placeholder"),
                visible: element.getBoundingClientRect().width > 0,
            };
        }""",
        element_handle,
    )

    if element_info is None or not element_info.get("visible"):
        return None

    # 用 AI 返回的坐标构造 Playwright locator
    # 直接用坐标点击（page.mouse.click）或通过 elementFromPoint 获取的元素
    locator = page.locator(f"xpath={_generate_xpath_from_element(page, result.center)}")

    return ResolvedLocator(
        strategy="ai_visual",
        locator=locator,
        trace=LocatorTrace(
            target=target,
            match_strategy="ai_visual",
            candidates=[],  # AI 定位没有候选列表
            selection_reason=f"AI visual locate at ({result.center[0]}, {result.center[1]})",
        ),
    )
```

---

## 三、Tier 3 具体设计：人工干预机制

### 3.1 交互模式：异步两阶段

选择 **异步两阶段（先失败，后修正，再重跑）** 模式，原因：

1. 当前 Runner 是同步的（`execute_case_with_playwright`），改成可暂停需要大改
2. 浏览器在执行完毕后 `browser.close()`，没有"保持会话等人工介入"的能力
3. 测试执行通常是批量的，不是交互式的

流程：

```
Phase 1: 正常执行
Runner → 所有层都定位失败 → 记录完整上下文 → 标记 "needs_intervention"

Phase 2: 人工修正（不限时，用户自行安排）
用户在前端执行详情页看到失败详情 → 在截图上点击/选择候选/输入选择器 → 存入修正记录

Phase 3: 重跑
重跑时 Tier 0 优先查修正记录 → 用修正的选择器定位 → 成功 → verified_count++
```

### 3.2 数据模型变更

#### 3.2.1 ExecutionStatus 扩展

```python
# backend/app/schemas/executions.py
# 修改前:
ExecutionStatus = Literal["running", "passed", "failed"]
# 修改后:
ExecutionStatus = Literal["running", "passed", "failed", "needs_intervention"]
```

#### 3.2.2 InterventionRequest schema

```python
# backend/app/schemas/executions.py 新增

class DOMElementSnapshot(DSLModel):
    """DOM 快照中的单个可交互元素"""
    tag: str                                    # "button", "input", "a"
    text: str | None = None                     # 元素文本内容
    role: str | None = None                     # ARIA role
    aria_label: str | None = None
    placeholder: str | None = None
    data_testid: str | None = None
    css_selector: str | None = None             # 生成的唯一 CSS 选择器
    xpath: str | None = None                    # 生成的 XPath
    rect: dict | None = None                    # {"x", "y", "width", "height"}
    visible: bool = False
    enabled: bool = False

class AILocateCandidate(DSLModel):
    """AI 视觉定位的候选结果"""
    center: list[int]                           # [x, y]
    bbox: list[int]                             # [xmin, ymin, xmax, ymax]
    confidence: float = 0.0
    raw_response: str | None = None

class InterventionRequest(DSLModel):
    """当所有定位层都失败时，记录需要人工干预的上下文"""
    screenshot_url: str                          # 失败时的截图 URL
    page_url: str                                # 当前页面 URL
    target_description: str                      # 定位目标描述 "登录按钮"
    dom_snapshot: list[DOMElementSnapshot] = Field(default_factory=list)
    ai_candidate: AILocateCandidate | None = None
    locator_trace: LocatorTrace | None = None    # Tier 1 的尝试记录
```

#### 3.2.3 StepExecutionEvidence 扩展

```python
# backend/app/schemas/executions.py 修改
class StepExecutionEvidence(DSLModel):
    # ... 所有现有字段保持不变 ...
    intervention_request: InterventionRequest | None = None   # 新增
```

#### 3.2.4 LocatorCorrection 数据库模型

```python
# backend/app/models/locator_correction.py 新建

from datetime import datetime, UTC
from sqlalchemy import Boolean, Column, DateTime, Integer, String, Text
from app.database import Base

class LocatorCorrection(Base):
    __tablename__ = "locator_corrections"

    id = Column(Integer, primary_key=True, autoincrement=True)

    # 匹配键
    page_url_pattern = Column(String(500), nullable=False, index=True)
    target_description = Column(String(200), nullable=False, index=True)

    # 修正内容
    correction_type = Column(String(20), nullable=False)    # "css" | "xpath" | "test_id"
    correction_value = Column(Text, nullable=False)          # "#login-btn" | "//button[@id='login']"

    # 置信度追踪
    verified_count = Column(Integer, nullable=False, default=0)
    consecutive_failures = Column(Integer, nullable=False, default=0)
    is_active = Column(Boolean, nullable=False, default=True)

    # 溯源
    source_execution_id = Column(Integer, nullable=False)
    created_by = Column(Integer, nullable=False)
    created_at = Column(DateTime, nullable=False, default=lambda: datetime.now(UTC).replace(tzinfo=None))
    updated_at = Column(DateTime, nullable=False, default=lambda: datetime.now(UTC).replace(tzinfo=None),
                        onupdate=lambda: datetime.now(UTC).replace(tzinfo=None))
```

需要新增 Alembic 迁移。

### 3.3 page_url_pattern 泛化策略

修正记录的复用取决于 URL 匹配。动态路径段需要自动泛化：

```python
# backend/app/locators/url_pattern.py 新建

import re
from urllib.parse import urlparse

def generalize_url(url: str) -> str:
    """
    将 URL 中的动态部分替换为通配符。

    示例:
      https://app.com/users/123/orders/456?tab=detail
      → https://app.com/users/*/orders/*

      https://app.com/posts/a1b2c3d4-e5f6-7890-abcd-ef1234567890
      → https://app.com/posts/*
    """
    parsed = urlparse(url)
    segments = parsed.path.split("/")
    generalized = [
        "*" if _is_dynamic_segment(seg) else seg
        for seg in segments
    ]
    return f"{parsed.scheme}://{parsed.netloc}{'/'.join(generalized)}"


def _is_dynamic_segment(segment: str) -> bool:
    """判断路径段是否是动态值（ID、UUID、hash 等）"""
    if not segment:
        return False
    if segment.isdigit():
        return True
    # UUID 格式
    if re.match(r'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$', segment, re.I):
        return True
    # 长随机串（16位以上纯字母数字）
    if len(segment) >= 16 and segment.isalnum():
        return True
    return False
```

### 3.4 Runner 中的完整定位链路改造

```python
# backend/app/runners/playwright_runner.py 中的改造

# 现有代码:
#   resolved = resolve_semantic_locator(page, step.target, ...)
#   resolved.locator.click()
#
# 改为:

def resolve_with_fallback(
    page,
    target: str,
    *,
    db_session,                    # 数据库会话，用于查修正记录
    execution_id: int,
    step_index: int,
    artifact_dir: Path,
    prefer_input: bool = False,
    require_visible: bool = True,
    require_enabled: bool = False,
) -> ResolvedLocator:
    """
    四层降级定位。

    Tier 0: 人工修正记录
    Tier 1: DOM 语义定位（现有 resolve_semantic_locator）
    Tier 2: AI 视觉定位
    Tier 3: 标记需要人工干预，抛出 InterventionNeededError
    """

    page_url = page.url
    tier1_trace = None

    # ── Tier 0: 查修正记录 ──
    correction = find_active_correction(db_session, page_url, target)
    if correction is not None:
        try:
            locator = page.locator(correction.correction_value)
            locator.wait_for(state="visible", timeout=3000)
            # 成功 → 更新置信度
            correction.verified_count += 1
            correction.consecutive_failures = 0
            db_session.commit()
            return ResolvedLocator(
                strategy=f"correction:{correction.correction_type}",
                locator=locator,
                trace=LocatorTrace(
                    target=target,
                    match_strategy=f"correction:{correction.correction_type}",
                    selection_reason=f"Human correction #{correction.id}, verified {correction.verified_count} times",
                ),
            )
        except Exception:
            # 修正失效 → 更新失败计数
            correction.consecutive_failures += 1
            if correction.consecutive_failures >= 3:
                correction.is_active = False
            db_session.commit()
            # 继续下一层

    # ── Tier 1: DOM 语义定位 ──
    try:
        return resolve_semantic_locator(
            page, target,
            prefer_input=prefer_input,
            require_visible=require_visible,
            require_enabled=require_enabled,
        )
    except LocatorResolutionError as exc:
        tier1_trace = exc.trace
        # 继续下一层

    # ── Tier 2: AI 视觉定位 ──
    try:
        screenshot_base64 = _take_screenshot_base64(page)
        ai_result = locate_element_by_vision(
            screenshot_base64=screenshot_base64,
            target_description=target,
            image_width=page.viewport_size["width"],
            image_height=page.viewport_size["height"],
        )
        if ai_result is not None:
            # 在 AI 坐标处查找 DOM 元素，生成可复用的 locator
            locator = _build_locator_from_point(page, ai_result.center)
            if locator is not None:
                return ResolvedLocator(
                    strategy="ai_visual",
                    locator=locator,
                    trace=LocatorTrace(
                        target=target,
                        match_strategy="ai_visual",
                        selection_reason=f"AI visual locate at {ai_result.center}",
                    ),
                )
    except Exception:
        pass  # AI 定位失败，继续下一层

    # ── Tier 3: 需要人工干预 ──
    screenshot_path = _take_step_screenshot(page, artifact_dir, step_index)
    dom_snapshot = _extract_interactable_elements(page)
    raise InterventionNeededError(
        target=target,
        page_url=page_url,
        screenshot_path=screenshot_path,
        dom_snapshot=dom_snapshot,
        ai_candidate=ai_result,       # 可能为 None
        tier1_trace=tier1_trace,       # 可能为 None
    )
```

### 3.5 InterventionNeededError 定义

```python
# backend/app/locators/__init__.py 或 backend/app/runners/playwright_runner.py

class InterventionNeededError(Exception):
    """所有定位层都失败，需要人工干预"""

    def __init__(
        self,
        target: str,
        page_url: str,
        screenshot_path: str | None,
        dom_snapshot: list[dict],
        ai_candidate: AILocateResult | None = None,
        tier1_trace: LocatorTrace | None = None,
    ) -> None:
        super().__init__(f"All locate tiers failed for target: {target}")
        self.target = target
        self.page_url = page_url
        self.screenshot_path = screenshot_path
        self.dom_snapshot = dom_snapshot
        self.ai_candidate = ai_candidate
        self.tier1_trace = tier1_trace
```

### 3.6 执行层捕获 InterventionNeededError

在 `playwright_runner.py` 的 step 执行循环中，除了现有的异常处理，新增：

```python
# 在 except 块中新增 InterventionNeededError 处理
except InterventionNeededError as exc:
    step_results.append(
        StepExecutionEvidence(
            step_index=index,
            action=step.action,
            target=getattr(step, "target", None),
            value=getattr(step, "value", None),
            status="failed",
            duration_ms=_elapsed_ms(step_started_at),
            url=page.url or None,
            page_title=_safe_page_title(page),
            viewport=_safe_viewport(page),
            dom_summary=_safe_dom_summary(page),
            console_events=console_buffer[console_index:],
            network_events=network_buffer[network_index:],
            screenshot_path=exc.screenshot_path,
            error_message=str(exc),
            intervention_request=InterventionRequest(
                screenshot_url=_artifact_to_url(exc.screenshot_path),
                page_url=exc.page_url,
                target_description=exc.target,
                dom_snapshot=[DOMElementSnapshot(**elem) for elem in exc.dom_snapshot],
                ai_candidate=AILocateCandidate(
                    center=list(exc.ai_candidate.center),
                    bbox=list(exc.ai_candidate.bbox),
                    confidence=exc.ai_candidate.confidence,
                    raw_response=exc.ai_candidate.raw_response,
                ) if exc.ai_candidate else None,
                locator_trace=exc.tier1_trace,
            ),
        )
    )
    # 抛出特殊异常，让上层将 execution 标记为 needs_intervention
    raise RunnerInterventionError(str(exc), step_results=step_results) from exc
```

### 3.7 执行服务层处理

```python
# backend/app/services/executions.py 中 execute_case() 函数改造

# 新增异常类型
class RunnerInterventionError(RuntimeError):
    def __init__(self, message: str, *, step_results: list[StepExecutionEvidence] | None = None):
        super().__init__(message)
        self.step_results = step_results or []

# 在 execute_case() 中新增 except 分支:
try:
    # ... 现有执行逻辑 ...
except RunnerInterventionError as exc:
    step_results = [_with_artifact_url(step) for step in exc.step_results]
    report = build_execution_report(status="failed", steps=step_results)
    execution.status = "needs_intervention"    # 而不是 "failed"
    execution.report = report.model_dump(mode="json")
    execution.error_message = str(exc)
except RunnerExecutionError as exc:
    # ... 现有逻辑不变 ...
```

### 3.8 修正记录 API

```python
# backend/app/api/routes/corrections.py 新建

from fastapi import APIRouter, Depends
from sqlalchemy.orm import Session

router = APIRouter(prefix="/api/v1/corrections", tags=["corrections"])

@router.post("/")
def create_correction(payload: CreateCorrectionRequest, session: Session = Depends(get_db)):
    """人工提交定位修正"""
    correction = LocatorCorrection(
        page_url_pattern=generalize_url(payload.page_url),
        target_description=payload.target_description,
        correction_type=payload.correction_type,
        correction_value=payload.correction_value,
        source_execution_id=payload.source_execution_id,
        created_by=payload.created_by,
    )
    session.add(correction)
    session.commit()
    session.refresh(correction)
    return correction

@router.get("/")
def list_corrections(
    target: str | None = None,
    is_active: bool | None = None,
    session: Session = Depends(get_db),
):
    """查询修正记录"""
    query = session.query(LocatorCorrection)
    if target is not None:
        query = query.filter(LocatorCorrection.target_description == target)
    if is_active is not None:
        query = query.filter(LocatorCorrection.is_active == is_active)
    return query.order_by(LocatorCorrection.updated_at.desc()).all()

@router.put("/{correction_id}/deactivate")
def deactivate_correction(correction_id: int, session: Session = Depends(get_db)):
    """手动停用修正记录"""
    correction = session.get(LocatorCorrection, correction_id)
    if correction is None:
        raise HTTPException(404, "Correction not found")
    correction.is_active = False
    session.commit()
    return correction
```

### 3.9 修正记录查找函数

```python
# backend/app/locators/corrections.py 新建

from sqlalchemy.orm import Session
from app.models.locator_correction import LocatorCorrection
from app.locators.url_pattern import generalize_url

MAX_CONSECUTIVE_FAILURES = 3

def find_active_correction(
    session: Session,
    page_url: str,
    target_description: str,
) -> LocatorCorrection | None:
    """
    查找当前页面和目标的活跃修正记录。
    返回验证次数最多的那条（置信度最高）。
    """
    pattern = generalize_url(page_url)
    return (
        session.query(LocatorCorrection)
        .filter(
            LocatorCorrection.target_description == target_description,
            LocatorCorrection.page_url_pattern == pattern,
            LocatorCorrection.is_active == True,
        )
        .order_by(LocatorCorrection.verified_count.desc())
        .first()
    )
```

---

## 四、前端交互设计

### 4.1 执行详情页 — 干预面板

当 step 有 `intervention_request` 时，在执行详情页的失败步骤中显示干预面板：

```
┌─────────────────────────────────────────────────────────┐
│  Step 3: click "删除按钮"  ❌ 需要人工干预               │
│                                                         │
│  ┌────────────────────────────────┐                     │
│  │                                │                     │
│  │    [失败时的页面截图]           │                     │
│  │    用户可点击截图上的元素        │                     │
│  │    指定正确位置                 │                     │
│  │                                │                     │
│  └────────────────────────────────┘                     │
│                                                         │
│  或手动输入选择器:                                        │
│  ┌────────────────────────────────────────────┐         │
│  │  css=#delete-btn                           │         │
│  └────────────────────────────────────────────┘         │
│                                                         │
│  页面 DOM 中的候选元素:                                   │
│  ○ <button> "删除" role=button  visible ✓ enabled ✓     │
│  ○ <button> "删除" role=button  visible ✓ enabled ✓     │
│  ○ <a> "删除账户" role=link     visible ✓ enabled ✓     │
│                                                         │
│  [ 提交修正 ]    [ 跳过 ]                                │
└─────────────────────────────────────────────────────────┘
```

### 4.2 修正管理页面（可选，放在后续迭代）

独立的修正记录管理页面，展示所有修正记录：

- 查看所有修正记录
- 按 target / page_url / is_active 筛选
- 手动启用/停用修正
- 查看验证次数和失败次数

---

## 五、闭环流程

```
首次执行 → Tier 1/2 都失败 → needs_intervention
                │
         用户在前端提交修正（selector）
                │
         存入 locator_corrections 表
                │
         重跑 → Tier 0 命中修正 → 成功 → verified_count++
                │
         后续同页面同目标 → 自动命中 → 持续成功
                │
         页面改版 → selector 失效 → consecutive_failures++
                │
         连续失败 3 次 → 自动停用(is_active=false) → 重新 needs_intervention
                │
         用户更新修正 → 新一轮闭环
```

---

## 六、文件变更清单

### 新建文件

| 文件 | 用途 |
|------|------|
| `backend/app/locators/ai_visual.py` | AI 视觉定位模块 |
| `backend/app/locators/url_pattern.py` | URL 泛化工具 |
| `backend/app/locators/corrections.py` | 修正记录查找 |
| `backend/app/models/locator_correction.py` | 修正记录数据库模型 |
| `backend/app/api/routes/corrections.py` | 修正记录 API |
| `backend/alembic/versions/xxx_add_locator_corrections.py` | 数据库迁移 |

### 修改文件

| 文件 | 变更内容 |
|------|---------|
| `backend/app/schemas/executions.py` | 新增 `InterventionRequest`、`DOMElementSnapshot`、`AILocateCandidate` schema；`ExecutionStatus` 增加 `"needs_intervention"` |
| `backend/app/runners/playwright_runner.py` | 用 `resolve_with_fallback` 替换 `resolve_semantic_locator` 调用；新增 `InterventionNeededError` 处理 |
| `backend/app/locators/__init__.py` | 导出新增的类和函数 |
| `backend/app/locators/semantic.py` | 无需修改，保持不变，作为 Tier 1 被 `resolve_with_fallback` 调用 |
| `backend/app/services/executions.py` | 新增 `RunnerInterventionError` 处理分支，将 status 设为 `"needs_intervention"` |
| `backend/app/models/__init__.py` | 导出 `LocatorCorrection` |
| `backend/app/api/routes/__init__.py` | 注册 corrections 路由 |
| `frontend/src/pages/ExecutionDetailPage.tsx` | 新增干预面板组件（当 step 有 `intervention_request` 时显示） |

---

## 七、实施顺序

建议按以下顺序实施，每步都可独立验证：

1. **数据模型先行**：新建 `LocatorCorrection` 模型 + Alembic 迁移 + `ExecutionStatus` 扩展 + 新 schema
2. **修正记录 API**：CRUD 接口 + `find_active_correction` 查找函数 + `generalize_url` 工具
3. **Tier 0 接入 Runner**：在现有 `resolve_semantic_locator` 之前插入修正记录查找
4. **AI 视觉定位模块**：实现 `ai_visual.py`，包括 VLM API 调用、bbox 解析、deepLocate
5. **Tier 2 接入 Runner**：在 Tier 1 失败后调用 AI 视觉定位
6. **InterventionNeededError 链路**：Tier 3 收集上下文 + 标记 needs_intervention
7. **前端干预面板**：执行详情页中的修正提交 UI

---

## 八、与 Midscene 源码的对应关系

供实施时参考 Midscene 源码的关键位置（仓库路径: `d:\AutoTestingLearingProject\midscene`）：

| 本项目模块 | Midscene 对应源码 | 说明 |
|-----------|------------------|------|
| AI 定位 prompt | `packages/core/src/ai-model/prompt/llm-locator.ts` | system prompt 和 user prompt |
| bbox 归一化适配 | `packages/core/src/common.ts` 第 199-225 行 | `adaptBbox` / `normalized01000` |
| deepLocate 两阶段 | `packages/core/src/service/index.ts` 第 66-198 行 | `locate()` 方法中的 searchArea 逻辑 |
| expandSearchArea | `packages/core/src/common.ts` | 区域扩展至少 400x400 |
| 坐标 → DOM 的 XPath | `packages/shared/src/extractor/locator.ts` 第 286-366 行 | `getXpathsByPoint()` |
| 缓存写入/读取 | `packages/core/src/agent/utils.ts` 第 249-299 行 | `matchElementFromCache()` |
| 四层降级定位 | `packages/core/src/agent/task-builder.ts` 第 430-522 行 | `createLocateTask()` 中的 fallback 链 |
