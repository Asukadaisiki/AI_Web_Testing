"""CONTRACT.md 的 pydantic 镜像。

唯一权威是 `v2/CONTRACT.md`；本文件只是它的可执行副本。改契约的正确流程见 CONTRACT.md 开头。

本模块只做"形状 + 阶段规则"的校验，不含任何浏览器行为：

- §2   case / step / condition 的字段与 action 约束
- §2.2 条件阶段表（全仓库唯一权威在本文件的 PRE_ALLOWED / POST_ALLOWED）
- §3   观测
- §4   执行结果

**与 Go 侧的一致性由 `fixtures/contract/case_contract.json` 保证**：Go 与 Python 共读
该文件，必须给出相同的 accept 与错误码（见 `tests/test_contract_conformance.py`）。
因此这里的检查顺序、错误码、以及"哪些字段用宽松类型再由代码判"都必须与
`backend/internal/contract/contract.go` 逐条对齐——凡是 Go 用代码判定并给出专属错误码的
字段，这里就不能用 Literal/必填来提前抛 ValidationError，否则错误码会漂移成
`case_invalid_json`。
"""

from __future__ import annotations

from datetime import datetime, timezone
from typing import Annotated, Any, Literal, Union

from pydantic import BaseModel, ConfigDict, Field, ValidationError

# --------------------------------------------------------------------------------------
# 常量
# --------------------------------------------------------------------------------------

CASE_VERSION = "loop.case.v1"

#: 缺省超时（CONTRACT §2.2）：条件 10000ms，步骤 20000ms。
#:
#: 按真实站点实测值定：冷启动打开 automationexercise.com 的 domcontentloaded
#: 实测 5.0–6.1s，站内跳转 /products 实测 6.3s。原来的 5000/3000 正好卡在真实
#: 耗时上，干跑（全新 context，无缓存）永远过不了。
DEFAULT_CONDITION_TIMEOUT_MS = 10000
DEFAULT_STEP_TIMEOUT_MS = 20000

ACTION_GOTO = "goto"
ACTION_CLICK = "click"
ACTION_INPUT = "input"
ACTION_SELECT = "select"
ACTION_CHECK = "check"
ACTION_UNCHECK = "uncheck"
ACTION_SCROLL_INTO_VIEW = "scroll_into_view"
ACTION_HOVER = "hover"
ACTION_DISMISS_DIALOG = "dismiss_dialog"
ACTION_UPLOAD_FILE = "upload_file"
ACTION_ASSERT_TEXT = "assert_text"
ACTION_ASSERT_URL = "assert_url"
ACTION_ASSERT_ELEMENT = "assert_element"
ACTION_ASSERT_ATTRIBUTE = "assert_attribute"
ACTION_ASSERT_COUNT = "assert_count"
ACTIONS: tuple[str, ...] = (
    ACTION_GOTO,
    ACTION_CLICK,
    ACTION_INPUT,
    ACTION_SELECT,
    ACTION_CHECK,
    ACTION_UNCHECK,
    ACTION_SCROLL_INTO_VIEW,
    ACTION_HOVER,
    ACTION_DISMISS_DIALOG,
    ACTION_UPLOAD_FILE,
    ACTION_ASSERT_TEXT,
    ACTION_ASSERT_URL,
    ACTION_ASSERT_ELEMENT,
    ACTION_ASSERT_ATTRIBUTE,
    ACTION_ASSERT_COUNT,
)

CONDITION_URL_CONTAINS = "url_contains"
CONDITION_TEXT_VISIBLE = "text_visible"
CONDITION_TEXT_GONE = "text_gone"
CONDITION_URL_CHANGES = "url_changes"
CONDITION_VALUE_EQUALS = "value_equals"
CONDITION_ELEMENT_STATE = "element_state"
CONDITION_ATTRIBUTE_EQUALS = "attribute_equals"
CONDITION_COUNT_EQUALS = "count_equals"

#: 条件阶段表（CONTRACT §2.2）：pre 只能是状态事实
PRE_ALLOWED: frozenset[str] = frozenset(
    {CONDITION_URL_CONTAINS, CONDITION_TEXT_VISIBLE, CONDITION_TEXT_GONE, CONDITION_ELEMENT_STATE}
)
POST_ALLOWED: frozenset[str] = frozenset(
    {
        CONDITION_URL_CONTAINS,
        CONDITION_TEXT_VISIBLE,
        CONDITION_TEXT_GONE,
        CONDITION_URL_CHANGES,
        CONDITION_VALUE_EQUALS,
        CONDITION_ELEMENT_STATE,
        CONDITION_ATTRIBUTE_EQUALS,
        CONDITION_COUNT_EQUALS,
    }
)
ALL_CONDITION_TYPES: tuple[str, ...] = (
    CONDITION_URL_CONTAINS,
    CONDITION_TEXT_VISIBLE,
    CONDITION_TEXT_GONE,
    CONDITION_URL_CHANGES,
    CONDITION_VALUE_EQUALS,
    CONDITION_ELEMENT_STATE,
    CONDITION_ATTRIBUTE_EQUALS,
    CONDITION_COUNT_EQUALS,
)

#: 失败信号 kind（CONTRACT §4.1）
SIGNAL_TARGET_NOT_FOUND = "target_not_found"
SIGNAL_CONDITION_UNMET = "condition_unmet"
SIGNAL_STEP_TIMEOUT = "step_timeout"
SIGNAL_WORKER_ERROR = "worker_error"
SIGNAL_CASE_INVALID = "case_invalid"
SIGNAL_BLOCKED_BY_DIALOG = "blocked_by_dialog"
SIGNAL_BLOCKED_BY_OVERLAY = "blocked_by_overlay"
SIGNAL_BLOCKED_BY_INTERSTITIAL = "blocked_by_interstitial"
SIGNAL_BLOCKED_BY_COOKIE_BANNER = "blocked_by_cookie_banner"
SIGNAL_BLOCKED_BY_AUTH = "blocked_by_auth"
SIGNAL_BLOCKED_BY_CAPTCHA = "blocked_by_captcha"
SIGNAL_BLOCKED_BY_LOADING = "blocked_by_loading"

#: 错误码，与 Go 侧 `internal/contract` 的 Code* 常量逐字一致。
CODE_INVALID_JSON = "case_invalid_json"
CODE_VERSION_MISMATCH = "case_version_mismatch"
CODE_NAME_REQUIRED = "case_name_required"
CODE_EMPTY_STEPS = "case_empty_steps"
CODE_NOT_GOTO_FIRST = "case_not_goto_first"
CODE_STEP_INDEX_MISMATCH = "step_index_mismatch"
CODE_INTENT_REQUIRED = "step_intent_required"
CODE_UNKNOWN_ACTION = "step_unknown_action"
CODE_MISSING_VALUE = "step_missing_value"
CODE_UNEXPECTED_VALUE = "step_unexpected_value"
CODE_MISSING_TARGET = "step_missing_target"
CODE_UNEXPECTED_TARGET = "step_unexpected_target"
CODE_TARGET_UNGROUNDED = "step_target_ungrounded"
CODE_INVALID_LOCATOR = "locator_invalid"
CODE_MISSING_PRECONDITION = "step_missing_precondition"
CODE_MISSING_POSTCONDITION = "step_missing_postcondition"
CODE_GOTO_PRECONDITION_FORBIDDEN = "goto_precondition_forbidden"
CODE_UNKNOWN_CONDITION_TYPE = "condition_unknown_type"
CODE_CONDITION_PHASE = "condition_phase"
CODE_CONDITION_MISSING_VALUE = "condition_missing_value"
CODE_CONDITION_MISSING_TIMEOUT = "condition_missing_timeout"
CODE_URL_NOT_ABSOLUTE = "goto_value_not_absolute"
CODE_UNEXPECTED_SUBMIT = "step_unexpected_submit"

#: HTTP 层错误码
ERROR_SESSION_NOT_FOUND = "session_not_found"
ERROR_WORKER_ERROR = "worker_error"

_ACTIONS_REQUIRING_TARGET: frozenset[str] = frozenset(
    {
        ACTION_CLICK,
        ACTION_INPUT,
        ACTION_SELECT,
        ACTION_CHECK,
        ACTION_UNCHECK,
        ACTION_SCROLL_INTO_VIEW,
        ACTION_HOVER,
        ACTION_DISMISS_DIALOG,
        ACTION_UPLOAD_FILE,
        ACTION_ASSERT_ELEMENT,
        ACTION_ASSERT_ATTRIBUTE,
        ACTION_ASSERT_COUNT,
    }
)


class CaseInvalid(Exception):
    """case 未通过契约校验；`code` 与 Go 侧错误码一致。"""

    code = SIGNAL_CASE_INVALID

    def __init__(self, code: str, detail: str) -> None:
        super().__init__(f"{code}: {detail}")
        self.code = code
        self.detail = detail

    def to_error(self) -> str:
        return self.code


def utc_now_iso() -> str:
    """契约里的时间戳形态：`2026-09-21T10:00:00Z`。"""
    return datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")


def condition_phase_allowed(condition_type: str, phase: str) -> bool:
    """条件阶段表（CONTRACT §2.2）的唯一判定入口。"""
    if phase == "pre":
        return condition_type in PRE_ALLOWED
    if phase == "post":
        return condition_type in POST_ALLOWED
    raise ValueError(f"unknown phase: {phase!r}")


def format_error(kind: str, detail: str) -> str:
    return f"{kind}: {detail}"


# --------------------------------------------------------------------------------------
# §2.1 / §2.2 case 与条件
# --------------------------------------------------------------------------------------

#: 契约模型统一 strict + forbid：多余字段、类型不符都在解码期就被拒绝，
#: 与 Go 的 `DisallowUnknownFields` 对齐。
_CONTRACT_CONFIG = ConfigDict(extra="forbid", strict=True)


class Condition(BaseModel):
    model_config = _CONTRACT_CONFIG

    #: 故意用宽松的 str：未知类型必须落到 condition_unknown_type，而不是解码错误。
    type: str = ""
    value: str = ""
    timeout_ms: int = DEFAULT_CONDITION_TIMEOUT_MS


class Grounding(BaseModel):
    model_config = _CONTRACT_CONFIG

    observation_id: str = ""
    page_state_id: str = ""
    candidate_id: str = ""
    page_url: str = ""


class LocatorSpec(BaseModel):
    """case / act 请求里的定位器（CONTRACT §2.1）。

    与观测里的 `VerifiedLocator` 的区别：这里没有 `match_count` —— 执行期只使用 case 里
    已经记录的那一条定位器，不再重新推导、也不再重新计数。为了容忍把观测里的定位器原样
    贴过来的调用方（`/act` 的 body 常来自观测），多余的字段被忽略而不是拒绝。

    `kind` 同样是宽松 str，未知 kind 必须落到 locator_invalid。
    """

    model_config = ConfigDict(extra="ignore", strict=True)

    kind: str = ""
    role: str | None = None
    name: str | None = None
    text: str | None = None
    css: str | None = None
    exact: bool | None = None

    def describe(self) -> str:
        if self.kind == "role":
            return f"role={self.role} name={self.name!r} exact={bool(self.exact)}"
        if self.kind == "text":
            return f"text={self.text!r} exact={bool(self.exact)}"
        return f"css={self.css!r}"


class Target(BaseModel):
    model_config = _CONTRACT_CONFIG

    #: locator / grounding 在 Go 侧是值类型，缺失即零值并落到专属错误码，
    #: 因此这里也必须是"可缺失"，不能靠 pydantic 必填提前报错。
    locator: LocatorSpec | None = None
    hint: str = ""
    grounding: Grounding | None = None
    spec: "TargetSpec | None" = None


class TargetObject(BaseModel):
    model_config = _CONTRACT_CONFIG

    role: str | None = None
    text: str | None = None
    name: str | None = None
    aliases: list[str] = Field(default_factory=list)


class TargetScope(BaseModel):
    model_config = _CONTRACT_CONFIG

    kind: str | None = None
    contains_text: str | None = None
    ref: str | None = None


class TargetSpec(BaseModel):
    model_config = _CONTRACT_CONFIG

    object: TargetObject = Field(default_factory=TargetObject)
    scope: TargetScope | None = None
    relation: str | None = None
    role: str | None = None
    text: str | None = None
    name: str | None = None
    aliases: list[str] = Field(default_factory=list)


class Step(BaseModel):
    model_config = _CONTRACT_CONFIG

    index: int = 0
    action: str = ""
    intent: str = ""
    value: str | None = None
    target: Target | None = None
    preconditions: list[Condition] = Field(default_factory=list)
    postconditions: list[Condition] = Field(default_factory=list)
    timeout_ms: int = DEFAULT_STEP_TIMEOUT_MS
    #: 只对 input 有意义：填完之后按回车提交。契约把它做成显式字段，
    #: 而不是让执行器"填完顺手回车"——隐式行为会在需要纯填值的场景里帮倒忙。
    submit: bool = False


class Case(BaseModel):
    model_config = _CONTRACT_CONFIG

    case_version: str = ""
    name: str = ""
    goal: str = ""
    base_url: str = ""
    steps: list[Step] = Field(default_factory=list)


def _is_absolute_http_url(value: str) -> bool:
    return value.startswith("http://") or value.startswith("https://")


def _normalize(case: Case) -> None:
    """与 Go 的 `normalize` 对齐：补缺省值、重排 index。"""
    if not case.case_version:
        case.case_version = CASE_VERSION
    for index, step in enumerate(case.steps):
        step.index = index
        if step.timeout_ms <= 0:
            step.timeout_ms = DEFAULT_STEP_TIMEOUT_MS
        for condition in (*step.preconditions, *step.postconditions):
            if condition.timeout_ms <= 0:
                condition.timeout_ms = DEFAULT_CONDITION_TIMEOUT_MS


def _validate_condition(condition: Condition, phase: str) -> None:
    if condition.type not in ALL_CONDITION_TYPES:
        raise CaseInvalid(
            CODE_UNKNOWN_CONDITION_TYPE,
            f"condition has unknown type {condition.type!r}; allowed types: "
            f"{', '.join(ALL_CONDITION_TYPES)}",
        )
    if not condition_phase_allowed(condition.type, phase):
        raise CaseInvalid(
            CODE_CONDITION_PHASE,
            f"condition {condition.type!r} asserts a change or an event and cannot hold before "
            f"the action; declare it as a postcondition or use "
            f"{', '.join(sorted(PRE_ALLOWED))}",
        )
    if condition.value == "":
        raise CaseInvalid(
            CODE_CONDITION_MISSING_VALUE, f"condition {condition.type!r} requires a value"
        )
    if condition.timeout_ms <= 0:
        raise CaseInvalid(
            CODE_CONDITION_MISSING_TIMEOUT,
            f"condition {condition.type!r} requires a positive timeout_ms",
        )


def _validate_locator(locator: LocatorSpec | None) -> None:
    if locator is None:
        raise CaseInvalid(CODE_INVALID_LOCATOR, "target.locator is required")
    if locator.kind == "role":
        if not locator.role or not locator.name:
            raise CaseInvalid(CODE_INVALID_LOCATOR, "role locator requires role and name")
    elif locator.kind == "text":
        if not locator.text:
            raise CaseInvalid(CODE_INVALID_LOCATOR, "text locator requires text")
    elif locator.kind == "css":
        if not locator.css:
            raise CaseInvalid(CODE_INVALID_LOCATOR, "css locator requires css")
    else:
        raise CaseInvalid(
            CODE_INVALID_LOCATOR,
            f"unknown locator kind {locator.kind!r}; allowed kinds: role, text, css",
        )


def _validate_target(target: Target) -> None:
    if not target.hint.strip():
        raise CaseInvalid(CODE_MISSING_TARGET, "target.hint is required")
    _validate_locator(target.locator)
    grounding = target.grounding
    if (
        grounding is None
        or not grounding.observation_id
        or not grounding.candidate_id
        or not grounding.page_url
    ):
        raise CaseInvalid(
            CODE_TARGET_UNGROUNDED,
            "target must be grounded against a real observation: observation_id, "
            "candidate_id and page_url are required",
        )


def _validate_step(step: Step, where: str) -> None:
    if not step.intent.strip():
        raise CaseInvalid(CODE_INTENT_REQUIRED, f"{where}.intent is required")

    action = step.action
    if action == ACTION_GOTO:
        if step.target is not None:
            raise CaseInvalid(CODE_UNEXPECTED_TARGET, f"{where} goto must not carry a target")
        if step.value is None or not step.value.strip():
            raise CaseInvalid(
                CODE_MISSING_VALUE, f"{where} goto requires an absolute url in value"
            )
        if not _is_absolute_http_url(step.value):
            raise CaseInvalid(
                CODE_URL_NOT_ABSOLUTE,
                f"{where} goto value must be an absolute url, got {step.value!r}",
            )
        if step.preconditions:
            raise CaseInvalid(
                CODE_GOTO_PRECONDITION_FORBIDDEN,
                f"{where} goto must not declare preconditions: navigation starts from "
                f"about:blank, so no precondition can hold",
            )
    elif action in _ACTIONS_REQUIRING_TARGET:
        if step.target is None:
            raise CaseInvalid(
                CODE_MISSING_TARGET, f"{where} {action} requires a grounded target"
            )
        _validate_target(step.target)
        if action == ACTION_CLICK and step.value is not None:
            raise CaseInvalid(CODE_UNEXPECTED_VALUE, f"{where} click must not carry a value")
        if action in (ACTION_INPUT, ACTION_SELECT, ACTION_UPLOAD_FILE) and step.value is None:
            raise CaseInvalid(
                CODE_MISSING_VALUE,
                f"{where} {action} requires a value (empty string is allowed)",
            )
        if (
            action not in (ACTION_INPUT, ACTION_SELECT, ACTION_UPLOAD_FILE)
            and step.value is not None
        ):
            raise CaseInvalid(CODE_UNEXPECTED_VALUE, f"{where} {action} must not carry a value")
        if not step.preconditions:
            raise CaseInvalid(
                CODE_MISSING_PRECONDITION, f"{where} {action} requires at least one precondition"
            )
    elif action in (ACTION_ASSERT_TEXT, ACTION_ASSERT_URL):
        if step.target is not None:
            raise CaseInvalid(
                CODE_UNEXPECTED_TARGET,
                f"{where} {action} must not carry a target: it asserts about the page",
            )
        if step.value is None or step.value == "":
            raise CaseInvalid(CODE_MISSING_VALUE, f"{where} {action} requires a value")
        if not step.preconditions:
            raise CaseInvalid(
                CODE_MISSING_PRECONDITION, f"{where} {action} requires at least one precondition"
            )
    else:
        raise CaseInvalid(
            CODE_UNKNOWN_ACTION,
            f"{where} unknown action {action!r}; allowed actions: {', '.join(ACTIONS)}",
        )

    # submit 只对 input 有意义。放在 action 分派之后：未知 action 仍应先报 unknown_action。
    if step.submit and action != ACTION_INPUT:
        raise CaseInvalid(
            CODE_UNEXPECTED_SUBMIT, f"{where} only input may carry submit, got {action}"
        )

    if not step.postconditions:
        raise CaseInvalid(
            CODE_MISSING_POSTCONDITION, f"{where} {action} requires at least one postcondition"
        )
    for condition in step.preconditions:
        _validate_condition(condition, "pre")
    for condition in step.postconditions:
        _validate_condition(condition, "post")


def validate_case(payload: dict[str, Any]) -> Case:
    """校验 case 工件（CONTRACT §2），失败抛 `CaseInvalid`。

    检查顺序与错误码必须与 Go 的 `contract.Validate` 完全一致。
    """
    if not isinstance(payload, dict):
        raise CaseInvalid(CODE_INVALID_JSON, "case must be a JSON object")
    try:
        case = Case.model_validate(payload)
    except ValidationError as exc:
        raise CaseInvalid(CODE_INVALID_JSON, _format_validation_error(exc)) from exc

    _normalize(case)

    if case.case_version != CASE_VERSION:
        raise CaseInvalid(
            CODE_VERSION_MISMATCH,
            f"case.case_version must be {CASE_VERSION!r}, got {case.case_version!r}",
        )
    if not case.name.strip():
        raise CaseInvalid(CODE_NAME_REQUIRED, "case.name is required")
    if not case.steps:
        raise CaseInvalid(CODE_EMPTY_STEPS, "case.steps must contain at least one step")
    if case.steps[0].action != ACTION_GOTO:
        raise CaseInvalid(
            CODE_NOT_GOTO_FIRST,
            f"steps[0] must be a goto step, got {case.steps[0].action!r}: the executor never "
            f"pre-navigates for the first step",
        )
    for index, step in enumerate(case.steps):
        if step.index != index:
            raise CaseInvalid(
                CODE_STEP_INDEX_MISMATCH,
                f"steps[{index}].index must be {index}, got {step.index}",
            )
        _validate_step(step, f"steps[{index}]")
    return case


def _format_validation_error(exc: ValidationError) -> str:
    parts: list[str] = []
    for err in exc.errors()[:5]:
        loc = ".".join(str(p) for p in err.get("loc", ())) or "<root>"
        parts.append(f"{loc}: {err.get('msg')}")
    more = len(exc.errors()) - len(parts)
    if more > 0:
        parts.append(f"... and {more} more error(s)")
    return "; ".join(parts)


# --------------------------------------------------------------------------------------
# §3 观测
# --------------------------------------------------------------------------------------


class RoleLocator(BaseModel):
    model_config = _CONTRACT_CONFIG

    kind: Literal["role"] = "role"
    role: str
    name: str
    exact: bool = True
    match_count: int


class TextLocator(BaseModel):
    model_config = _CONTRACT_CONFIG

    kind: Literal["text"] = "text"
    text: str
    exact: bool = True
    match_count: int


class CssLocator(BaseModel):
    model_config = _CONTRACT_CONFIG

    kind: Literal["css"] = "css"
    css: str
    match_count: int


VerifiedLocator = Annotated[
    Union[RoleLocator, TextLocator, CssLocator], Field(discriminator="kind")
]


class BoundingBox(BaseModel):
    model_config = _CONTRACT_CONFIG

    x: float = 0
    y: float = 0
    width: float = 0
    height: float = 0


class ElementFormInfo(BaseModel):
    model_config = _CONTRACT_CONFIG

    form_ref: str = ""
    submit_candidate_ref: str | None = None
    enter_submittable: bool = False


class ElementObservation(BaseModel):
    model_config = _CONTRACT_CONFIG

    ref: str
    tag: str
    role: str | None = None
    name: str | None = None
    text: str | None = None
    value: str | None = None
    visible: bool
    enabled: bool
    locators: list[VerifiedLocator] = Field(min_length=1)
    parent_ref: str | None = None
    container_ref: str | None = None
    own_text: str | None = None
    full_text: str | None = None
    bbox: BoundingBox = Field(default_factory=BoundingBox)
    visible_in_viewport: bool = False
    z_index: int | None = None
    attributes: dict[str, str] = Field(default_factory=dict)
    form: ElementFormInfo | None = None


class StructureNode(BaseModel):
    model_config = _CONTRACT_CONFIG

    ref: str
    kind: str
    tag: str
    role: str | None = None
    parent_ref: str | None = None
    full_text: str = ""
    visible: bool
    bbox: BoundingBox = Field(default_factory=BoundingBox)
    attributes: dict[str, str] = Field(default_factory=dict)
    submit_candidate_ref: str | None = None
    enter_submittable: bool = False


class Blocker(BaseModel):
    model_config = _CONTRACT_CONFIG

    kind: str
    ref: str | None = None
    confidence: Literal["high", "medium", "low"] = "medium"
    covers_target_ref: str | None = None
    dismiss_candidates: list[VerifiedLocator] = Field(default_factory=list)
    reason: str = ""


class CandidateRelation(BaseModel):
    model_config = _CONTRACT_CONFIG

    type: str
    ref: str
    label: str | None = None


class ActionCandidate(BaseModel):
    model_config = _CONTRACT_CONFIG

    candidate_id: str
    kind: str
    action: str
    target_ref: str
    role: str | None = None
    name: str | None = None
    text: str | None = None
    aliases: list[str] = Field(default_factory=list)
    attributes: dict[str, str] = Field(default_factory=dict)
    relations: list[CandidateRelation] = Field(default_factory=list)
    locator: VerifiedLocator
    confidence: Literal["high", "medium", "low"] = "medium"


class Observation(BaseModel):
    model_config = _CONTRACT_CONFIG

    observation_id: str
    page_state_id: str
    #: 执行器内部的浏览器上下文句柄（`bsess_...`），用完即弃。
    #: 与领域会话（`sess_...`，CONTRACT §9）无关，不得混用。
    browser_session_id: str | None = None
    url: str
    title: str
    elements: list[ElementObservation] = Field(default_factory=list)
    structures: list[StructureNode] = Field(default_factory=list)
    blockers: list[Blocker] = Field(default_factory=list)
    action_candidates: list[ActionCandidate] = Field(default_factory=list)
    truncated: bool = False
    truncation_reason: str | None = None
    screenshot_path: str | None = None


class OpenSessionRequest(BaseModel):
    """开一个浏览器会话，并声明它服务的领域会话（决定证据落哪个目录）。"""

    model_config = _CONTRACT_CONFIG

    session_id: str = Field(min_length=1)


# --------------------------------------------------------------------------------------
# §4 执行结果
# --------------------------------------------------------------------------------------


class StepError(BaseModel):
    """结构化失败原因。`kind` 取值见 CONTRACT §4.1。"""

    model_config = _CONTRACT_CONFIG

    kind: str
    message: str


class ConditionResult(BaseModel):
    model_config = _CONTRACT_CONFIG

    phase: Literal["pre", "post"]
    type: str
    value: str
    satisfied: bool
    detail: str | None = None


class ConsoleEvent(BaseModel):
    model_config = _CONTRACT_CONFIG

    level: str
    text: str


class NetworkEvent(BaseModel):
    model_config = _CONTRACT_CONFIG

    method: str
    url: str
    status: int | None = None


class Evidence(BaseModel):
    model_config = _CONTRACT_CONFIG

    screenshot_path: str | None = None
    console: list[ConsoleEvent] = Field(default_factory=list)
    network: list[NetworkEvent] = Field(default_factory=list)


class HitTest(BaseModel):
    model_config = _CONTRACT_CONFIG

    target_ref: str | None = None
    x: float = 0
    y: float = 0
    hit_ref: str | None = None
    hit_tag: str | None = None
    hit_role: str | None = None
    hit_text: str | None = None
    covered: bool = False
    blocker_kind: str | None = None


class RecoveryAttempt(BaseModel):
    model_config = _CONTRACT_CONFIG

    blocker: Blocker
    action: str
    succeeded: bool
    reason: str = ""
    before_screenshot_path: str | None = None
    after_screenshot_path: str | None = None
    url_before: str | None = None
    url_after: str | None = None
    retried_original_action: bool = False


class StepResult(BaseModel):
    model_config = _CONTRACT_CONFIG

    index: int
    action: str
    status: Literal["passed", "failed"]
    started_at: str
    duration_ms: int
    url_before: str
    url_after: str
    conditions: list[ConditionResult] = Field(default_factory=list)
    evidence: Evidence
    error: StepError | None = None
    blocker: Blocker | None = None
    hit_test: HitTest | None = None
    recovery: list[RecoveryAttempt] = Field(default_factory=list)


class ExecutionResult(BaseModel):
    model_config = _CONTRACT_CONFIG

    execution_id: str
    status: Literal["passed", "failed", "error"]
    started_at: str
    finished_at: str
    final_url: str
    steps: list[StepResult] = Field(default_factory=list)
    error: StepError | None = None


# --------------------------------------------------------------------------------------
# HTTP 请求体
# --------------------------------------------------------------------------------------


class NavigateRequest(BaseModel):
    model_config = _CONTRACT_CONFIG

    url: str


class ActRequest(BaseModel):
    model_config = ConfigDict(extra="forbid", strict=True)

    action: Literal[
        "click",
        "input",
        "select",
        "check",
        "uncheck",
        "scroll_into_view",
        "hover",
        "dismiss_dialog",
        "upload_file",
    ]
    locator: LocatorSpec
    value: str | None = None
    #: 只对 input 有意义：填完之后按回车提交（作者态也必须真的提交）。
    submit: bool = False


class ExecuteRequest(BaseModel):
    model_config = _CONTRACT_CONFIG

    #: 必填：每步截图要落进 `<产物根>/<session_id>/`（CONTRACT §9.2）。
    session_id: str = Field(min_length=1)
    case: dict[str, Any]


class ErrorResponse(BaseModel):
    model_config = _CONTRACT_CONFIG

    error: str
    detail: str
