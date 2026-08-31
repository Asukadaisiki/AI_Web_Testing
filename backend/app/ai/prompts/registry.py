"""Centralized prompt definitions and rendering helpers."""

from __future__ import annotations

from dataclasses import dataclass, field
from enum import StrEnum
from string import Formatter
from typing import Any


FORCE_GENERATE_MARKER = "[FORCE_GENERATE]"
FORCE_GENERATE_HINT = "用户要求直接生成方案。以下是用户原始输入："


class PromptStage(StrEnum):
    """Stable prompt loading stages.

    Keep stage names stable because generation audit rows and future policy
    evaluation can reference them without coupling to implementation modules.
    """

    PLANNING_INIT = "planning.init"
    DSL_GENERATE_SYSTEM = "dsl.generate.system"
    VLM_LOCATE_SYSTEM = "vlm.locate.system"
    VLM_SECTION_SYSTEM = "vlm.section.system"
    VLM_RANK_CANDIDATE_SYSTEM = "vlm.rank_candidate.system"
    VLM_PAGE_ANNOTATION_SYSTEM = "vlm.page_annotation.system"


@dataclass(frozen=True)
class PromptDefinition:
    """Versioned prompt template metadata."""

    stage: PromptStage
    version: str
    template: str
    description: str = ""
    required_variables: tuple[str, ...] = field(default_factory=tuple)
    extension_slots: tuple[str, ...] = field(default_factory=tuple)


@dataclass(frozen=True)
class PromptBuildResult:
    """Rendered prompt plus metadata for tracing."""

    stage: PromptStage
    version: str
    content: str
    loaded_variables: tuple[str, ...]
    extension_slots: tuple[str, ...] = field(default_factory=tuple)


PLANNING_INIT_TEMPLATE = """\
你是 Web 自动化测试规划 Agent。

目标：理解用户需求，必要时收集页面信息，输出结构化测试方案。

可用工具：
{tool_descriptions}

只返回合法 JSON，不输出 Markdown 或额外解释：
{{
  "thought": "当前判断",
  "action": "ask_user | call_tool | generate_plan",
  "action_input": {{
    "message": "ask_user 时填写",
    "tool": "call_tool 时填写",
    "params": {{}},
    "summary": "generate_plan 时填写",
    "scenarios": [
      {{
        "scenario_key": "sc1",
        "title": "场景标题",
        "draft_prompt": "给 DSL 生成器的完整场景指令",
        "priority": "high|medium|low",
        "flow_steps": [],
        "variables": []
      }}
    ]
  }},
  "assistant_message": "给用户的回复",
  "collected_info": {{
    "app_under_test": "",
    "business_goal": "",
    "entry_url_or_page": "",
    "core_user_flow": "",
    "main_assertions": [],
    "test_data_or_account": "",
    "scope_limits": ""
  }},
  "todo_list": []
}}

规则：
- 每次追问最多 2 个问题。
- 每次回复都包含 collected_info；未知字段用空字符串或空数组。
- 用户给出的 http:// 或 https:// URL 必须写入 entry_url_or_page。
- generate_plan 前，应先探索核心流程涉及的页面。
- 生成方案时，target 使用页面探索返回的实际元素文本或 a11y role/name。
- draft_prompt 中测试数据变量使用 ${{context_key}}，跨页面变量写入 variables。
- 默认中文输出。
"""


DSL_GENERATE_SYSTEM_TEMPLATE = """You generate web testing DSL in JSON. Return {"name","description","base_url","input_contract","output_contract","steps"}.
No markdown, no explanation - JSON only.

## Data format

The Available elements are grouped by page -> action:
- Each page section shows the URL and page state
- Under each page, actions are listed with the elements that appeared AFTER that action
- The same page may appear multiple times with different actions, showing how elements change
- This is normal and expected - use the most recent state of each element

## Rules (in priority order)

1. **Targets**: Copy the EXACT role="name" format from the Available elements section.
   Include the role prefix - this enables precise locator resolution.
   FORBIDDEN: CSS selectors (#id, .class, [attr]), XPath (//, /html), tag names (div, span),
   data-testid, or ANY DOM-derived selector. The system resolves locators from a11y role+name only.

   **IMPORTANT**: Use the role from the element that HAS the name, NOT from its parent.
   Example: If you see:
   ```
   - [container] Blue Top
     - paragraph="Blue Top"
     - heading="Rs. 500"
     - link="Add to cart"
   ```
   Use: `paragraph "Blue Top"` or role from the indented child element.
   The `[container]` prefix indicates a container element - NEVER use it as a target.

   **Element disambiguation**: When multiple elements have the same role and name (e.g. multiple
   "Add to cart" buttons), you MUST use the scoped format:
   target=<role> "<name>" inside "<container_identifier>"
   The scope name comes from the parent container's identifying text (product name, row label, etc.).
   Look at the page structure: elements are grouped in containers (product cards, table rows, forms, etc.).
   Use the container's unique identifying text as the scope.
   Never target a bare price like "Rs. 500".

   **CRITICAL**: When using `inside`, use the CHILD element's role, NOT the container's role.
   Example:
   - To capture price: `capture_text heading "Rs. 500" inside "Blue Top"` OK
   - WRONG: `capture_text paragraph "Blue Top" inside "Blue Top"`

2. **Page structure understanding**: The Available elements use indentation to show parent-child relationships:
   - Indented elements are children of the element above them
   - Example: `- [container] Blue Top\n  - paragraph="Blue Top"\n  - heading="Rs. 500"\n  - link="Add to cart"`
     means paragraph, heading, and link are children of the Blue Top container
   - Use the parent container's identifying text as the scope name for `inside`
   - Example: To click "Add to cart" inside "Blue Top" product card, use: target=link "Add to cart" inside "Blue Top"

   **Correct DSL examples**:
   - click link "Products" -> target=link "Products"
   - click link "Add to cart" inside "Blue Top" -> target=link "Add to cart" inside "Blue Top"
   - capture_text heading "Rs. 500" inside "Blue Top" -> target=heading "Rs. 500" inside "Blue Top"
   - capture_text paragraph "Blue Top" -> target=paragraph "Blue Top"
   - assert_text link "Blue Top" -> target=link "Blue Top", value="Blue Top"

   **WRONG examples** (NEVER do this):
   - capture_text paragraph "Blue Top" inside "Blue Top" -> WRONG! use container text as scope, not as child target
   - click paragraph "Blue Top" -> OK only for capture_text; for clicking prefer link/button roles

3. **Navigation**: You MUST click/goto to reach a page BEFORE interacting with elements on it.
   The first step after goto / is a navigation click, not a form input.

4. **Login**: The DSL must be self-contained. Include all login steps (input email + password + click Login).
   Do NOT assume the user is already logged in. Use ${var} for credentials.

5. **Wait after actions**: After navigation clicks or form submits, add wait_for for a confirmation element.

6. **Input trigger**: When changing a value that requires keyboard activation (quantity, search),
   add trigger="Enter" on the input step. The executor handles the keypress.

7. **Modify-then-assert**: When changing a value, input -> wait_for update -> assert.
   Do NOT assert a new value without first inputting it.

8. **Capture-then-assert**: capture_text stores element text into a variable
   (use context_key as the variable name). Later assert_text steps can reference
   this variable via ${context_key} in the VALUE field to verify the captured
   text appears on a different page (e.g. cart page).

9. **Form coverage**: Generate a step for EVERY form field mentioned in the flow.
   Dropdown: input action. Checkbox/radio: click action.

10. **Field rules**:
    - goto / assert_url_contains: value=URL, NO target
    - click / wait_for: target only, NO value
    - input / assert_text: BOTH target AND value required
    - capture_text: target + context_key (snake_case variable name)
    - ${var} placeholders can ONLY be used in the VALUE field of input/assert_text, NEVER as a target.

11. **input_contract**: Define every ${var} used in steps. Include context_key AND value.
    CRITICAL: The "value" field MUST be copied VERBATIM from the "## Test data" section.
    NEVER invent, guess, or modify test data values.

Return ONLY the JSON object."""


VLM_LOCATE_SYSTEM_TEMPLATE = """You are an AI assistant that locates a UI element in a screenshot.
Return JSON only in the shape {"bbox":[xmin,ymin,xmax,ymax],"errors":["..."]?}."""

VLM_SECTION_SYSTEM_TEMPLATE = """You are an AI assistant that finds the broad page area that contains a UI element.
Return JSON only in the shape {"bbox":[xmin,ymin,xmax,ymax],"errors":["..."]?}."""

VLM_RANK_CANDIDATE_SYSTEM_TEMPLATE = """You rank numbered UI candidates in a screenshot.
Return JSON only in the shape {"candidate_index": number, "errors":["..."]?}."""

VLM_PAGE_ANNOTATION_SYSTEM_TEMPLATE = (
    "You are an AI assistant that describes the layout structure of a web page screenshot.\n"
    "Return a concise text description (not JSON) covering:\n"
    "1. Overall page layout (header, navigation, main content, sidebar, footer)\n"
    "2. Form sections and their purpose\n"
    "3. Key interactive regions (buttons, links, inputs)\n"
    "4. Any modal or overlay elements\n"
    "Keep the description under 200 words. Focus on spatial layout and element relationships, "
    "not individual element details."
)


_PROMPTS: dict[PromptStage, PromptDefinition] = {
    PromptStage.PLANNING_INIT: PromptDefinition(
        stage=PromptStage.PLANNING_INIT,
        version="planning.init.v1",
        template=PLANNING_INIT_TEMPLATE,
        description="Initial system prompt for the AI planning ReAct loop.",
        required_variables=("tool_descriptions",),
        extension_slots=("runtime_context", "policy_context"),
    ),
    PromptStage.DSL_GENERATE_SYSTEM: PromptDefinition(
        stage=PromptStage.DSL_GENERATE_SYSTEM,
        version="dsl.generate.system.v1",
        template=DSL_GENERATE_SYSTEM_TEMPLATE,
        description="System prompt for DSL JSON generation.",
        extension_slots=("runtime_context", "policy_context"),
    ),
    PromptStage.VLM_LOCATE_SYSTEM: PromptDefinition(
        stage=PromptStage.VLM_LOCATE_SYSTEM,
        version="vlm.locate.system.v1",
        template=VLM_LOCATE_SYSTEM_TEMPLATE,
        description="System prompt for screenshot element localization.",
    ),
    PromptStage.VLM_SECTION_SYSTEM: PromptDefinition(
        stage=PromptStage.VLM_SECTION_SYSTEM,
        version="vlm.section.system.v1",
        template=VLM_SECTION_SYSTEM_TEMPLATE,
        description="System prompt for broad screenshot section localization.",
    ),
    PromptStage.VLM_RANK_CANDIDATE_SYSTEM: PromptDefinition(
        stage=PromptStage.VLM_RANK_CANDIDATE_SYSTEM,
        version="vlm.rank_candidate.system.v1",
        template=VLM_RANK_CANDIDATE_SYSTEM_TEMPLATE,
        description="System prompt for ranking visual locator candidates.",
    ),
    PromptStage.VLM_PAGE_ANNOTATION_SYSTEM: PromptDefinition(
        stage=PromptStage.VLM_PAGE_ANNOTATION_SYSTEM,
        version="vlm.page_annotation.system.v1",
        template=VLM_PAGE_ANNOTATION_SYSTEM_TEMPLATE,
        description="System prompt for planning-phase screenshot layout annotation.",
    ),
}


def get_prompt_definition(stage: PromptStage | str) -> PromptDefinition:
    """Return a registered prompt definition by stage."""
    prompt_stage = PromptStage(stage)
    return _PROMPTS[prompt_stage]


def render_prompt(stage: PromptStage | str, variables: dict[str, Any] | None = None) -> PromptBuildResult:
    """Render a registered prompt template.

    ``extension_slots`` are metadata only in this phase. They reserve explicit
    attachment points for later policy/context modules without changing runtime
    behavior today.
    """
    definition = get_prompt_definition(stage)
    variables = variables or {}
    missing = [name for name in definition.required_variables if name not in variables]
    if missing:
        raise ValueError(f"Missing prompt variables for {definition.stage}: {', '.join(missing)}")

    if definition.required_variables or variables:
        format_fields = {
            field_name
            for _, field_name, _, _ in Formatter().parse(definition.template)
            if field_name
        }
        render_vars = {name: variables.get(name, "") for name in format_fields}
        content = definition.template.format(**render_vars)
    else:
        render_vars = {}
        content = definition.template
    return PromptBuildResult(
        stage=definition.stage,
        version=definition.version,
        content=content,
        loaded_variables=tuple(sorted(render_vars)),
        extension_slots=definition.extension_slots,
    )
