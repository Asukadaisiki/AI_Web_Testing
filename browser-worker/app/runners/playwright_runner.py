"""Playwright-backed execution runner for stored DSL cases."""

from __future__ import annotations

import logging
import re
from pathlib import Path
from threading import Event
from time import perf_counter
from typing import Generator, Literal
from urllib.parse import urldefrag, urljoin, urlparse

from app.core.structured_logging import get_structured_logger
from app.locators import InterventionNeededError, LocatorResolutionError, resolve_with_fallback
from app.locators.corrections import CorrectionStore
from app.locators.semantic import ResolvedLocator
from app.runners.click_preprocessor import click_with_precheck
from app.runners.postcondition_verifier import PostconditionVerifier, StepNetworkObserver
from app.schemas.dsl import DSLCase
from app.schemas.executions import (
    ActionOutcome,
    AILocateCandidate,
    ConsoleEvent,
    DOMSummary,
    InterventionRequest,
    LocatorCandidateAttributes,
    LocatorCandidateEvidence,
    LocatorTrace,
    NetworkEvent,
    StepExecutionEvidence,
    ViewportSnapshot,
)

logger = logging.getLogger(__name__)
slog = get_structured_logger(__name__)


ARTIFACTS_ROOT = Path(__file__).resolve().parents[2] / "artifacts" / "executions"

_VARIABLE_PATTERN = re.compile(r"\$\{([A-Za-z_][A-Za-z0-9_]*)\}")


def _resolve_with_confidence_gate(
    page,
    target: str,
    *,
    locator_confidence: str | None = None,
    target_strategy: str | None = None,
    correction_store: CorrectionStore | None = None,
    execution_id: int | None = None,
    prefer_input: bool = False,
    require_visible: bool = True,
    require_enabled: bool = False,
    expected_text: str | None = None,
) -> tuple[ResolvedLocator, bool]:
    """Unified locator resolution: corrections → semantic → VLM (via fallback chain).

    ``locator_confidence`` is currently unused; kept in the signature for the
    callers that still pass it. VLM triggering happens inside
    ``resolve_with_fallback`` itself; this wrapper no longer adds a duplicate
    pre-verify call.

    ``expected_text`` is used for assert_text steps to disambiguate when
    multiple candidates match the target (e.g., target="h2" but we want
    the h2 that contains "BRAND - POLO PRODUCTS").
    """
    resolved = resolve_with_fallback(
        page, target,
        target_strategy=target_strategy,
        correction_store=correction_store,
        execution_id=execution_id,
        prefer_input=prefer_input,
        require_visible=require_visible,
        require_enabled=require_enabled,
        expected_text=expected_text,
    )
    return resolved, False

def _substitute_variables(value: str | None, input_values: dict[str, str] | None) -> str | None:
    """Replace ``${context_key}`` placeholders in *value* with entries from *input_values*."""
    if not value or not input_values:
        return value

    _unmatched_keys: list[str] = []

    def _replace(match: re.Match) -> str:  # type: ignore[type-arg]
        key = match.group(1)
        result = input_values.get(key)
        if result is None:
            _unmatched_keys.append(key)
            return match.group(0)
        return result

    result = _VARIABLE_PATTERN.sub(_replace, value)
    if _unmatched_keys:
        logger.warning(
            "Variable substitution: unresolved keys %s in value '%s'",
            _unmatched_keys, value[:100] if len(value) > 100 else value,
        )
    return result


class RunnerExecutionError(RuntimeError):
    """Raised when a case cannot be executed successfully."""

    def __init__(
        self,
        message: str,
        *,
        step_results: list[StepExecutionEvidence] | None = None,
        step_evidence: StepExecutionEvidence | None = None,
    ) -> None:
        super().__init__(message)
        self.step_results = step_results or []
        self.step_evidence = step_evidence


class RunnerInterventionError(RuntimeError):
    """Raised when execution must stop for manual locator intervention."""

    def __init__(
        self,
        message: str,
        *,
        step_results: list[StepExecutionEvidence] | None = None,
    ) -> None:
        super().__init__(message)
        self.step_results = step_results or []


class RunnerCancelledError(RuntimeError):
    """Raised when the user cancels execution via WebSocket."""

    def __init__(
        self,
        message: str,
        *,
        step_results: list[StepExecutionEvidence] | None = None,
    ) -> None:
        super().__init__(message)
        self.step_results = step_results or []


class StepStreamEvent:
    """Lightweight event yielded by the streaming runner for each step."""

    __slots__ = ("type", "step_index", "action", "target", "value", "status", "duration_ms")

    def __init__(
        self,
        *,
        type: Literal["step_start", "step_complete"],
        step_index: int,
        action: str,
        target: str | None = None,
        value: str | None = None,
        status: Literal["passed", "failed"] | None = None,
        duration_ms: int | None = None,
    ) -> None:
        self.type = type
        self.step_index = step_index
        self.action = action
        self.target = target
        self.value = value
        self.status = status
        self.duration_ms = duration_ms

    def __repr__(self) -> str:
        return f"StepStreamEvent({self.type}, step={self.step_index}, action={self.action})"


# ---------------------------------------------------------------------------
# Dual-layer scoring helpers
# ---------------------------------------------------------------------------


def _has_candidates(step) -> bool:
    """Check if a DSL step has pre-scored candidate locators."""
    return hasattr(step, "candidates") and bool(step.candidates)


def _build_locator_from_candidate(page, candidate_entry) -> object | None:
    """Build a Playwright locator from a candidate's strategy and selector.

    *candidate_entry* may be a ``LocatorCandidate`` model or a plain dict.
    """
    if hasattr(candidate_entry, "strategy"):
        strategy = candidate_entry.strategy
        selector = candidate_entry.selector or ""
        semantic_value = candidate_entry.semantic_value or ""
        pre_features = getattr(candidate_entry, "pre_features", None) or {}
    else:
        strategy = candidate_entry.get("strategy", "")
        selector = candidate_entry.get("selector", "")
        semantic_value = candidate_entry.get("semantic_value", "")
        pre_features = candidate_entry.get("pre_features") or {}

    scope_name = pre_features.get("scope_name")

    try:
        # Scoped strategies: find product container by content, then chain to target
        if strategy.startswith("a11y_scoped_") and scope_name:
            product_containers = page.get_by_role("product")
            scope = product_containers.filter(has=page.get_by_text(scope_name, exact=True))
            if strategy == "a11y_scoped_role_exact":
                return scope.get_by_role(selector or "button", name=semantic_value, exact=True)
            if strategy == "a11y_scoped_role_fuzzy":
                return scope.get_by_role(selector or "button", name=semantic_value)
            if strategy == "a11y_scoped_text_exact":
                return scope.get_by_text(selector, exact=True)
            if strategy == "a11y_scoped_text_fuzzy":
                return scope.get_by_text(selector, exact=False)

        if strategy in ("css", "css_selector"):
            return page.locator(selector)
        if strategy == "xpath":
            return page.locator(f"xpath={selector}")
        if strategy in ("data-testid", "data_testid"):
            clean_id = selector.replace("[data-testid='", "").replace("']", "").replace("[data_testid='","").replace("']","")
            return page.get_by_test_id(clean_id)
        if strategy == "role":
            return page.get_by_role(selector or "button", name=semantic_value)
        if strategy == "text":
            return page.get_by_text(selector, exact=False)
        if strategy == "label":
            return page.get_by_label(semantic_value or selector)
        if strategy == "placeholder":
            return page.get_by_placeholder(semantic_value or selector)
        if strategy == "element_id":
            return page.locator(f"#{selector}" if not selector.startswith("#") else selector)
        if strategy in ("tag", "semantic"):
            return page.locator(selector) if selector else None
        # verified selectors (live-verified during page exploration)
        if strategy == "verified_role":
            return page.get_by_role(selector, name=semantic_value, exact=True)
        if strategy == "verified_role_fuzzy":
            return page.get_by_role(selector, name=semantic_value)
        if strategy == "verified_css":
            return page.locator(selector)
        if strategy == "verified_xpath":
            return page.locator(f"xpath={selector}")
        if strategy == "verified_placeholder":
            return page.get_by_placeholder(selector, exact=True)
        if strategy == "verified_placeholder_fuzzy":
            return page.get_by_placeholder(selector)
        if strategy == "verified_label":
            return page.get_by_label(selector, exact=True)
        if strategy == "verified_label_fuzzy":
            return page.get_by_label(selector)
        if strategy == "verified_text":
            return page.get_by_text(selector, exact=True)
        if strategy == "verified_data-testid":
            return page.get_by_test_id(selector)
        if strategy == "verified_element_id":
            return page.locator(f"#{selector}" if not selector.startswith("#") else selector)
        # vlm / fallback
        return page.locator(selector) if selector else None
    except Exception:
        return None


def _candidate_to_trace_evidence(candidate_entry) -> LocatorCandidateEvidence:
    if hasattr(candidate_entry, "strategy"):
        strategy = candidate_entry.strategy
        selector = candidate_entry.selector or ""
        semantic_value = candidate_entry.semantic_value or ""
        pre_score = candidate_entry.pre_score
    else:
        strategy = candidate_entry.get("strategy", "")
        selector = candidate_entry.get("selector", "")
        semantic_value = candidate_entry.get("semantic_value", "")
        pre_score = candidate_entry.get("pre_score", 0.0)
    return LocatorCandidateEvidence(
        strategy=strategy,
        preview_text=semantic_value or selector or None,
        role=strategy,
        attributes=LocatorCandidateAttributes(),
        score=int(float(pre_score or 0.0) * 100),
        matched_rules=["preflight-verified-candidate"],
        visible=True,
        enabled=True,
    )


def _candidate_feature(candidate, name: str):
    features = (
        getattr(candidate, "pre_features", None)
        if hasattr(candidate, "pre_features")
        else candidate.get("pre_features")
    ) or {}
    return features.get(name)


def _same_origin(left: str, right: str) -> bool:
    left_url = urlparse(left)
    right_url = urlparse(right)

    def origin(parsed):
        try:
            port = parsed.port
        except ValueError:
            return None
        if port is None:
            port = 443 if parsed.scheme == "https" else 80
        return parsed.scheme.lower(), (parsed.hostname or "").lower(), port

    return origin(left_url) == origin(right_url)


def _verified_safe_anchor_href(locator, candidate, before_url: str) -> str | None:
    verified_href = str(_candidate_feature(candidate, "verified_href") or "").strip()
    if not verified_href or verified_href.startswith("#"):
        return None
    try:
        live = locator.evaluate(
            "el => ({tag: el.tagName.toLowerCase(), href: el.getAttribute('href') || '', "
            "download: el.hasAttribute('download')})"
        )
    except Exception:
        return None
    if (
        not isinstance(live, dict)
        or live.get("tag") != "a"
        or live.get("download")
        or str(live.get("href") or "").strip() != verified_href
    ):
        return None
    resolved = urljoin(before_url, verified_href)
    parsed = urlparse(resolved)
    if (
        parsed.scheme not in {"http", "https"}
        or parsed.username
        or parsed.password
        or not _same_origin(before_url, resolved)
    ):
        return None
    if urldefrag(resolved)[0] == urldefrag(before_url)[0]:
        return None
    return resolved


def _verify_postconditions_with_navigation_fallback(
    page,
    step,
    verifier: PostconditionVerifier,
    resolved_href: str | None,
    *,
    network_events: list[NetworkEvent] | None = None,
) -> tuple[object, bool]:
    result = verifier.verify(
        step.postconditions,
        phase="postcondition",
        network_events=network_events,
    )
    if result.passed or not resolved_href:
        return result, False
    expected_url = next(
        (
            postcondition
            for postcondition in step.postconditions
            if postcondition.type == "url_contains" and postcondition.value
        ),
        None,
    )
    if expected_url is None or expected_url.value in page.url:
        return result, False
    page.goto(
        resolved_href,
        wait_until="domcontentloaded",
        timeout=expected_url.timeout_ms,
    )
    return verifier.verify(
        step.postconditions,
        phase="postcondition",
        network_events=network_events,
    ), True


def _successful_side_effect_state(action: str) -> str:
    if action in {"click", "input", "goto"}:
        return "committed"
    return "not_applicable"


def _build_step_evidence(
    page,
    step,
    step_index: int,
    started_at: float,
    *,
    artifact_dir: Path | None,
    action_outcome: ActionOutcome,
    condition_results=None,
    pre_state=None,
    error_message: str | None = None,
    locator_trace=None,
    resolved=None,
    resolved_by: str | None = None,
    step_value=None,
    network_events: list[NetworkEvent] | None = None,
    click_recovery: str | None = None,
    click_recovery_detail: str | None = None,
    vlm_preverify_used: bool = False,
) -> StepExecutionEvidence:
    return StepExecutionEvidence(
        step_index=step_index,
        action=step.action,
        target=getattr(step, "target", None),
        value=step_value if step_value is not None else getattr(step, "value", None),
        status="failed" if error_message else "passed",
        duration_ms=_elapsed_ms(started_at),
        pre_state=pre_state,
        condition_results=condition_results or [],
        action_outcome=action_outcome,
        resolved_by=resolved_by,
        locator_trace=locator_trace or (resolved.trace if resolved else None),
        url=page.url or None,
        page_title=_safe_page_title(page),
        viewport=_safe_viewport(page),
        dom_summary=_safe_dom_summary(page),
        network_events=list(network_events or []),
        screenshot_path=(
            _take_step_screenshot(page, artifact_dir, step_index)
            if artifact_dir
            else None
        ),
        error_message=error_message,
        click_recovery=click_recovery,
        click_recovery_detail=click_recovery_detail,
        locator_confidence=getattr(step, "locator_confidence", None),
        vlm_preverify_used=vlm_preverify_used,
    )


def _execute_step_with_candidates(
    page,
    step,
    step_index: int,
    *,
    artifact_dir: Path | None = None,
    execution_id: int | None = None,
    correction_store=None,
    input_values: dict[str, str] | None = None,
    runtime_context: dict[str, str] | None = None,
) -> StepExecutionEvidence:
    """Resolve before dispatch, then execute exactly once and preserve its outcome."""
    step_started_at = perf_counter()
    vars_map = dict(input_values or {})
    vars_map.update(runtime_context or {})
    verifier = PostconditionVerifier(page)
    pre_state = verifier.capture_pre_state()
    pre_result = verifier.verify(
        getattr(step, "preconditions", []),
        phase="precondition",
    )
    if not pre_result.passed:
        message = f"Precondition check failed: {pre_result.details}"
        evidence = _build_step_evidence(
            page, step, step_index, step_started_at,
            artifact_dir=artifact_dir,
            action_outcome=ActionOutcome(
                status="not_executed",
                side_effect_state="not_committed",
                error=message,
            ),
            condition_results=pre_result.results,
            pre_state=pre_state,
            error_message=message,
        )
        raise RunnerExecutionError(message, step_evidence=evidence)

    resolved_target = _substitute_variables(step.target, vars_map) or step.target
    candidates = sorted(step.candidates, key=lambda c: c.pre_score, reverse=True)
    selected_candidate = None
    locator = None
    for candidate in candidates:
        candidate_locator = _build_locator_from_candidate(page, candidate)
        if candidate_locator is None:
            continue
        try:
            if candidate_locator.count() != 1:
                continue
        except Exception:
            continue
        selected_candidate = candidate
        locator = candidate_locator
        break

    from playwright.sync_api import expect as pw_expect
    resolved = None
    resolved_by = None
    vlm_preverify_used = False
    click_recovery = None
    click_recovery_detail = None
    step_value = getattr(step, "value", None)
    try:
        if locator is None and step.action == "click":
            resolved, vlm_preverify_used = _resolve_with_confidence_gate(
                page, step.target,
                locator_confidence=getattr(step, "locator_confidence", None),
                target_strategy=step.target_strategy,
                correction_store=correction_store,
                execution_id=execution_id,
                require_visible=True, require_enabled=True,
            )
            resolved_by = resolved.strategy
            locator = resolved.locator
        elif locator is None and step.action == "input":
            resolved, vlm_preverify_used = _resolve_with_confidence_gate(
                page, step.target,
                locator_confidence=getattr(step, "locator_confidence", None),
                target_strategy=step.target_strategy,
                correction_store=correction_store,
                execution_id=execution_id,
                prefer_input=True, require_visible=True, require_enabled=True,
            )
            resolved_by = resolved.strategy
            locator = resolved.locator
        elif locator is None and step.action == "wait_for":
            resolved, vlm_preverify_used = _resolve_with_confidence_gate(
                page, step.target,
                locator_confidence=getattr(step, "locator_confidence", None),
                target_strategy=step.target_strategy,
                correction_store=correction_store,
                execution_id=execution_id,
                require_visible=False,
            )
            resolved_by = resolved.strategy
            locator = resolved.locator
        elif locator is None and step.action == "assert_text":
            resolved, vlm_preverify_used = _resolve_with_confidence_gate(
                page, step.target,
                locator_confidence=getattr(step, "locator_confidence", None),
                target_strategy=step.target_strategy,
                correction_store=correction_store,
                execution_id=execution_id,
                require_visible=False,
                expected_text=_substitute_variables(step.value, vars_map),
            )
            resolved_by = resolved.strategy
            locator = resolved.locator
        elif locator is None and step.action == "capture_text":
            resolved, vlm_preverify_used = _resolve_with_confidence_gate(
                page, step.target,
                locator_confidence=getattr(step, "locator_confidence", None),
                target_strategy=step.target_strategy,
                correction_store=correction_store,
                execution_id=execution_id,
                require_visible=False,
            )
            resolved_by = resolved.strategy
            locator = resolved.locator
        elif locator is None:
            raise RunnerExecutionError(f"Unsupported action: {step.action}")
    except Exception as exc:
        outcome = ActionOutcome(status="not_executed", side_effect_state="not_committed", error=str(exc))
        evidence = _build_step_evidence(
            page, step, step_index, step_started_at,
            artifact_dir=artifact_dir,
            action_outcome=outcome,
            condition_results=pre_result.results,
            pre_state=pre_state,
            error_message=str(exc),
            resolved=resolved,
            resolved_by=resolved_by,
            vlm_preverify_used=vlm_preverify_used,
        )
        raise RunnerExecutionError(str(exc), step_evidence=evidence) from exc

    if selected_candidate is not None:
        resolved_by = selected_candidate.strategy
        trace_candidate = _candidate_to_trace_evidence(selected_candidate)
        locator_trace = LocatorTrace(
            target=resolved_target,
            match_strategy=resolved_by,
            candidates=[trace_candidate],
            selected_candidate=trace_candidate,
            selection_reason="Selected verified candidate before action dispatch.",
        )
    else:
        locator_trace = resolved.trace if resolved else None

    resolved_href = (
        _verified_safe_anchor_href(locator, selected_candidate, pre_state.url)
        if step.action == "click" and selected_candidate is not None
        else None
    )
    observer = StepNetworkObserver(page).start()
    dispatched = False
    try:
        dispatched = True
        if step.action == "click":
            if resolved is not None and resolved.click_coordinates is not None:
                cr = click_with_precheck(
                    page,
                    locator,
                    click_coordinates=resolved.click_coordinates,
                )
            else:
                cr = click_with_precheck(page, locator)
            if not cr.succeeded:
                raise cr.original_error or RunnerExecutionError("Click failed")
            click_recovery = cr.recovery_strategy
            click_recovery_detail = cr.recovery_detail
        elif step.action == "input":
            input_value = _substitute_variables(step.value, vars_map)
            if resolved is not None and resolved.click_coordinates is not None:
                cr = click_with_precheck(
                    page,
                    locator,
                    click_coordinates=resolved.click_coordinates,
                )
                if not cr.succeeded:
                    raise cr.original_error or RunnerExecutionError("Click failed")
                click_recovery = cr.recovery_strategy
                click_recovery_detail = cr.recovery_detail
                page.keyboard.type(input_value)
            else:
                tag_name = locator.evaluate("el => el.tagName.toLowerCase()")
                if tag_name == "select":
                    locator.select_option(label=input_value)
                else:
                    locator.fill(input_value)
            if getattr(step, "trigger", None):
                locator.press(step.trigger)
        elif step.action == "wait_for":
            locator.wait_for(state="visible", timeout=step.timeout_ms)
        elif step.action == "assert_text":
            pw_expect(locator).to_contain_text(
                _substitute_variables(step.value, vars_map),
            )
        elif step.action == "capture_text":
            captured = locator.inner_text()
            step_value = captured.strip()
            if runtime_context is not None:
                runtime_context[step.context_key] = step_value

        outcome = ActionOutcome(
            status="succeeded",
            side_effect_state=_successful_side_effect_state(step.action),
        )
        post_result, used_href_fallback = _verify_postconditions_with_navigation_fallback(
            page,
            step,
            verifier,
            resolved_href,
            network_events=observer.events,
        )
        if used_href_fallback:
            click_recovery = "href_navigation_fallback"
            click_recovery_detail = f"navigated once to verified anchor href {resolved_href}"
        if not post_result.passed:
            raise RunnerExecutionError(f"Postcondition check failed: {post_result.details}")
        return _build_step_evidence(
            page, step, step_index, step_started_at,
            artifact_dir=artifact_dir,
            action_outcome=outcome,
            condition_results=[*pre_result.results, *post_result.results],
            pre_state=pre_state,
            locator_trace=locator_trace,
            resolved=resolved,
            resolved_by=resolved_by,
            step_value=step_value,
            network_events=observer.events,
            click_recovery=click_recovery,
            click_recovery_detail=click_recovery_detail,
            vlm_preverify_used=vlm_preverify_used,
        )
    except Exception as exc:
        post_results = getattr(locals().get("post_result"), "results", [])
        if "outcome" in locals():
            outcome = locals()["outcome"]
        elif not dispatched:
            outcome = ActionOutcome(
                status="not_executed",
                side_effect_state="not_committed",
                error=str(exc),
            )
        elif step.action in {"click", "input"}:
            outcome = ActionOutcome(
                status="unknown",
                side_effect_state="unknown",
                error=str(exc),
            )
        else:
            outcome = ActionOutcome(
                status="failed",
                side_effect_state="not_applicable",
                error=str(exc),
            )
        evidence = _build_step_evidence(
            page, step, step_index, step_started_at,
            artifact_dir=artifact_dir,
            action_outcome=outcome,
            condition_results=[*pre_result.results, *post_results],
            pre_state=pre_state,
            error_message=str(exc),
            locator_trace=locator_trace,
            resolved=resolved,
            resolved_by=resolved_by,
            step_value=step_value,
            network_events=observer.events,
            click_recovery=click_recovery,
            click_recovery_detail=click_recovery_detail,
            vlm_preverify_used=vlm_preverify_used,
        )
        raise RunnerExecutionError(str(exc), step_evidence=evidence) from exc
    finally:
        observer.stop()


def _execute_non_target_step(
    page,
    step,
    step_index: int,
    *,
    base_url: str | None,
    artifact_dir: Path,
    input_values: dict[str, str],
) -> StepExecutionEvidence:
    started_at = perf_counter()
    verifier = PostconditionVerifier(page)
    pre_state = verifier.capture_pre_state()
    pre_result = verifier.verify(step.preconditions, phase="precondition")
    if not pre_result.passed:
        message = f"Precondition check failed: {pre_result.details}"
        evidence = _build_step_evidence(
            page, step, step_index, started_at,
            artifact_dir=artifact_dir,
            action_outcome=ActionOutcome(
                status="not_executed",
                side_effect_state="not_committed",
                error=message,
            ),
            condition_results=pre_result.results,
            pre_state=pre_state,
            error_message=message,
        )
        raise RunnerExecutionError(message, step_evidence=evidence)

    observer = StepNetworkObserver(page).start()
    try:
        if step.action == "goto":
            page.goto(
                _resolve_url(_substitute_variables(step.value, input_values), base_url),
                wait_until="domcontentloaded",
            )
        elif step.action == "assert_url_contains":
            expected = _substitute_variables(step.value, input_values)
            if expected not in page.url:
                raise AssertionError(f"URL assertion failed, expected fragment: {expected}")
        else:
            raise RunnerExecutionError(f"Unsupported action: {step.action}")
        outcome = ActionOutcome(
            status="succeeded",
            side_effect_state=_successful_side_effect_state(step.action),
        )
        post_result = verifier.verify(
            step.postconditions,
            phase="postcondition",
            network_events=observer.events,
        )
        if not post_result.passed:
            raise RunnerExecutionError(f"Postcondition check failed: {post_result.details}")
        return _build_step_evidence(
            page, step, step_index, started_at,
            artifact_dir=artifact_dir,
            action_outcome=outcome,
            condition_results=[*pre_result.results, *post_result.results],
            pre_state=pre_state,
            network_events=observer.events,
        )
    except Exception as exc:
        post_results = getattr(locals().get("post_result"), "results", [])
        if "outcome" in locals():
            action_outcome = outcome
        elif step.action == "goto":
            action_outcome = ActionOutcome(
                status="unknown",
                side_effect_state="unknown",
                error=str(exc),
            )
        else:
            action_outcome = ActionOutcome(
                status="failed",
                side_effect_state="not_applicable",
                error=str(exc),
            )
        evidence = _build_step_evidence(
            page, step, step_index, started_at,
            artifact_dir=artifact_dir,
            action_outcome=action_outcome,
            condition_results=[*pre_result.results, *post_results],
            pre_state=pre_state,
            network_events=observer.events,
            error_message=str(exc),
        )
        raise RunnerExecutionError(str(exc), step_evidence=evidence) from exc
    finally:
        observer.stop()


def _execute_case_with_playwright_legacy(
    *,
    case: DSLCase,
    execution_id: int,
    base_url: str | None,
    correction_store: CorrectionStore | None = None,
    input_values: dict[str, str] | None = None,
) -> list[StepExecutionEvidence]:
    try:
        from playwright.sync_api import TimeoutError as PlaywrightTimeoutError
        from playwright.sync_api import expect, sync_playwright
    except ImportError as exc:
        raise RunnerExecutionError(
            "Playwright dependency is not installed. Run `uv sync` in browser-worker/ first."
        ) from exc

    artifact_dir = ARTIFACTS_ROOT / str(execution_id)
    artifact_dir.mkdir(parents=True, exist_ok=True)

    slog.dsl_execution("execution_start", data={
        "execution_id": execution_id,
        "total_steps": len(case.steps),
        "base_url": base_url,
    }, execution_id=execution_id)

    step_results: list[StepExecutionEvidence] = []
    runtime_context: dict[str, str] = {}

    def _vars() -> dict[str, str]:
        combined = dict(input_values or {})
        combined.update(runtime_context)
        return combined

    with sync_playwright() as playwright:
        browser = playwright.chromium.launch(headless=True)
        page = browser.new_page()
        page.set_default_timeout(150_000)  # 2.5 min per operation

        # Clear all storage to avoid residual login sessions from previous runs
        page.context.clear_cookies()
        try:
            page.evaluate("localStorage.clear(); sessionStorage.clear();")
        except Exception:
            pass

        console_buffer: list[ConsoleEvent] = []
        network_buffer: list[NetworkEvent] = []
        page.on("console", lambda message: _capture_console_event(message, console_buffer))
        page.on("requestfailed", lambda request: _capture_request_failed(request, network_buffer))
        page.on("response", lambda response: _capture_error_response(response, network_buffer))

        # Auto-navigate to base_url when first step is not a goto
        if case.steps and case.steps[0].action != "goto" and base_url:
            page.goto(base_url, wait_until="domcontentloaded")

        try:
            _execution_started_at = perf_counter()
            _MAX_EXECUTION_SECONDS = 600
            for index, step in enumerate(case.steps):
                if perf_counter() - _execution_started_at > _MAX_EXECUTION_SECONDS:
                    raise RunnerExecutionError(f"Execution timeout after {_MAX_EXECUTION_SECONDS}s", step_results=step_results)
                resolved_target = _substitute_variables(getattr(step, 'target', None), _vars()) or getattr(step, 'target', None)
                step_started_at = perf_counter()

                slog.dsl_execution("step_start", data={
                    "execution_id": execution_id,
                    "step_index": index,
                    "action": step.action,
                    "target": getattr(step, "target", None),
                    "has_candidates": _has_candidates(step),
                }, execution_id=execution_id)
                console_index = len(console_buffer)
                network_index = len(network_buffer)
                resolved = None
                locator_trace = None

                try:
                    resolved_by = None
                    click_recovery = None
                    click_recovery_detail = None
                    vlm_preverify_used = False
                    step_value = getattr(step, "value", None)

                    if hasattr(step, "candidates"):
                        evidence_for_step = _execute_step_with_candidates(
                            page, step, index,
                            artifact_dir=artifact_dir,
                            execution_id=execution_id,
                            correction_store=correction_store,
                            input_values=_vars(),
                            runtime_context=runtime_context,
                        )
                        step_results.append(evidence_for_step)
                        continue
                    # --- Legacy path below ---

                    if step.action == "goto":
                        page.goto(_resolve_url(_substitute_variables(step.value, _vars()), base_url), wait_until="domcontentloaded")
                    elif step.action == "click":
                        resolved, vlm_preverify_used = _resolve_with_confidence_gate(
                            page,
                            resolved_target,
                            locator_confidence=getattr(step, "locator_confidence", None),
                            target_strategy=step.target_strategy,
                            correction_store=correction_store,
                            execution_id=execution_id,
                            require_visible=True,
                            require_enabled=True,
                        )
                        resolved_by = resolved.strategy
                        locator_trace = resolved.trace
                        cr = click_with_precheck(
                            page, resolved.locator,
                            click_coordinates=resolved.click_coordinates,
                        )
                        if not cr.succeeded:
                            raise cr.original_error or RunnerExecutionError("Click failed")
                        click_recovery = cr.recovery_strategy
                        click_recovery_detail = cr.recovery_detail
                    elif step.action == "input":
                        resolved, vlm_preverify_used = _resolve_with_confidence_gate(
                            page,
                            resolved_target,
                            locator_confidence=getattr(step, "locator_confidence", None),
                            target_strategy=step.target_strategy,
                            correction_store=correction_store,
                            execution_id=execution_id,
                            prefer_input=True,
                            require_visible=True,
                            require_enabled=True,
                        )
                        resolved_by = resolved.strategy
                        locator_trace = resolved.trace
                        input_value = _substitute_variables(step.value, _vars())
                        if resolved.click_coordinates is not None:
                            cr = click_with_precheck(
                                page, resolved.locator,
                                click_coordinates=resolved.click_coordinates,
                            )
                            if not cr.succeeded:
                                raise cr.original_error or RunnerExecutionError("Click failed")
                            click_recovery = cr.recovery_strategy
                            click_recovery_detail = cr.recovery_detail
                            page.keyboard.type(input_value)
                        else:
                            tag_name = resolved.locator.evaluate("el => el.tagName.toLowerCase()")
                            if tag_name == "select":
                                resolved.locator.select_option(label=input_value)
                            else:
                                resolved.locator.fill(input_value)
                    elif step.action == "wait_for":
                        resolved, vlm_preverify_used = _resolve_with_confidence_gate(
                            page,
                            resolved_target,
                            locator_confidence=getattr(step, "locator_confidence", None),
                            target_strategy=step.target_strategy,
                            correction_store=correction_store,
                            execution_id=execution_id,
                            require_visible=False,
                        )
                        resolved_by = resolved.strategy
                        locator_trace = resolved.trace
                        resolved.locator.wait_for(state="visible", timeout=step.timeout_ms)
                    elif step.action == "assert_text":
                        resolved, vlm_preverify_used = _resolve_with_confidence_gate(
                            page,
                            resolved_target,
                            locator_confidence=getattr(step, "locator_confidence", None),
                            target_strategy=step.target_strategy,
                            correction_store=correction_store,
                            execution_id=execution_id,
                            require_visible=False,
                            expected_text=_substitute_variables(step.value, _vars()),
                        )
                        resolved_by = resolved.strategy
                        locator_trace = resolved.trace
                        expect(resolved.locator).to_contain_text(
                            _substitute_variables(step.value, _vars()),
                        )
                    elif step.action == "assert_url_contains":
                        if _substitute_variables(step.value, _vars()) not in page.url:
                            raise RunnerExecutionError(
                                f"URL assertion failed, expected fragment: {_substitute_variables(step.value, _vars())}"
                            )
                    elif step.action == "capture_text":
                        resolved, vlm_preverify_used = _resolve_with_confidence_gate(
                            page,
                            resolved_target,
                            locator_confidence=getattr(step, "locator_confidence", None),
                            target_strategy=step.target_strategy,
                            correction_store=correction_store,
                            execution_id=execution_id,
                            require_visible=False,
                        )
                        resolved_by = resolved.strategy
                        locator_trace = resolved.trace
                        captured = resolved.locator.inner_text()
                        step_value = captured.strip()
                        runtime_context[step.context_key] = captured.strip()
                    else:
                        raise RunnerExecutionError(f"Unsupported action: {step.action}")

                    step_results.append(
                        StepExecutionEvidence(
                            step_index=index,
                            action=step.action,
                            target=getattr(step, "target", None),
                            value=step_value,
                            status="passed",
                            duration_ms=_elapsed_ms(step_started_at),
                            resolved_by=resolved_by,
                            locator_trace=locator_trace,
                            url=page.url or None,
                            page_title=_safe_page_title(page),
                            viewport=_safe_viewport(page),
                            dom_summary=_safe_dom_summary(page),
                            console_events=console_buffer[console_index:],
                            network_events=network_buffer[network_index:],
                            screenshot_path=_take_step_screenshot(page, artifact_dir, index),
                            click_recovery=click_recovery,
                            click_recovery_detail=click_recovery_detail,
                            locator_confidence=getattr(step, "locator_confidence", None),
                            vlm_preverify_used=vlm_preverify_used,
                        )
                    )
                    slog.dsl_execution("step_complete", data={
                        "execution_id": execution_id,
                        "step_index": index,
                        "action": step.action,
                        "target": getattr(step, "target", None),
                        "status": "passed",
                        "duration_ms": _elapsed_ms(step_started_at),
                        "resolved_by": resolved_by,
                    }, execution_id=execution_id)
                except InterventionNeededError as exc:
                    screenshot_path = _take_step_screenshot(page, artifact_dir, index)
                    step_results.append(
                        StepExecutionEvidence(
                            step_index=index,
                            action=step.action,
                            locator_confidence=getattr(step, "locator_confidence", None),
                            vlm_preverify_used=vlm_preverify_used,
                            target=getattr(step, "target", None),
                            value=getattr(step, "value", None),
                            status="failed",
                            duration_ms=_elapsed_ms(step_started_at),
                            resolved_by=None,
                            locator_trace=exc.tier1_trace,
                            url=page.url or None,
                            page_title=_safe_page_title(page),
                            viewport=_safe_viewport(page),
                            dom_summary=_safe_dom_summary(page),
                            console_events=console_buffer[console_index:],
                            network_events=network_buffer[network_index:],
                            screenshot_path=screenshot_path,
                            error_message=str(exc),
                            intervention_request=InterventionRequest(
                                screenshot_url=_artifact_url_for_path(screenshot_path),
                                page_url=exc.page_url,
                                target_description=exc.target,
                                ai_candidate=(
                                    AILocateCandidate(
                                        center=list(exc.ai_candidate.center),
                                        bbox=list(exc.ai_candidate.bbox),
                                        confidence=exc.ai_candidate.confidence,
                                        raw_response=exc.ai_candidate.raw_response,
                                    )
                                    if exc.ai_candidate is not None
                                    else None
                                ),
                                locator_trace=exc.tier1_trace,
                                vlm_failure_reason=getattr(exc, "vlm_failure_reason", None),
                            ),
                        )
                    )
                    raise RunnerInterventionError(str(exc), step_results=step_results) from exc
                except (LocatorResolutionError, PlaywrightTimeoutError, RunnerExecutionError, AssertionError) as exc:
                    if isinstance(exc, LocatorResolutionError):
                        locator_trace = exc.trace
                    elif resolved is not None:
                        locator_trace = resolved.trace

                    step_results.append(
                        StepExecutionEvidence(
                            step_index=index,
                            action=step.action,
                            target=getattr(step, "target", None),
                            value=getattr(step, "value", None),
                            status="failed",
                            duration_ms=_elapsed_ms(step_started_at),
                            resolved_by=resolved.strategy if resolved is not None else None,
                            locator_trace=locator_trace,
                            url=page.url or None,
                            page_title=_safe_page_title(page),
                            viewport=_safe_viewport(page),
                            dom_summary=_safe_dom_summary(page),
                            console_events=console_buffer[console_index:],
                            network_events=network_buffer[network_index:],
                            screenshot_path=_take_step_screenshot(page, artifact_dir, index),
                            error_message=str(exc),
                            locator_confidence=getattr(step, "locator_confidence", None),
                            vlm_preverify_used=vlm_preverify_used,
                        )
                    )
                    slog.dsl_execution("step_failed", data={
                        "execution_id": execution_id,
                        "step_index": index,
                        "action": step.action,
                        "target": getattr(step, "target", None),
                        "status": "failed",
                        "duration_ms": _elapsed_ms(step_started_at),
                        "error_type": type(exc).__name__,
                        "error_message": str(exc)[:500],
                        "resolved_by": resolved.strategy if resolved is not None else None,
                    }, execution_id=execution_id, level=logging.WARNING)
                    raise RunnerExecutionError(
                        str(exc), step_results=step_results
                    ) from exc
            _attach_final_dom_snapshot(page, artifact_dir, step_results)
        finally:
            browser.close()

    return step_results


def execute_case_with_playwright(
    *,
    case: DSLCase,
    execution_id: int,
    base_url: str | None,
    correction_store: CorrectionStore | None = None,
    input_values: dict[str, str] | None = None,
) -> list[StepExecutionEvidence]:
    """Consume the streaming runner so both APIs share one execution path."""
    stream = execute_case_with_playwright_streaming(
        case=case,
        execution_id=execution_id,
        base_url=base_url,
        correction_store=correction_store,
        input_values=input_values,
    )
    while True:
        try:
            next(stream)
        except StopIteration as stop:
            return stop.value


def execute_case_with_playwright_streaming(
    *,
    case: DSLCase,
    execution_id: int,
    base_url: str | None,
    cancel_event: Event | None = None,
    correction_store: CorrectionStore | None = None,
    input_values: dict[str, str] | None = None,
) -> Generator[StepStreamEvent, None, list[StepExecutionEvidence]]:
    """Execute a case and yield :class:`StepStreamEvent` per step.

    Returns the full step evidence list via ``StopIteration.value``.
    Raises :class:`RunnerCancelledError` if *cancel_event* is set between steps.
    """
    try:
        from playwright.sync_api import TimeoutError as PlaywrightTimeoutError
        from playwright.sync_api import expect, sync_playwright
    except ImportError as exc:
        raise RunnerExecutionError(
            "Playwright dependency is not installed. Run `uv sync` in browser-worker/ first."
        ) from exc

    artifact_dir = ARTIFACTS_ROOT / str(execution_id)
    artifact_dir.mkdir(parents=True, exist_ok=True)

    step_results: list[StepExecutionEvidence] = []
    runtime_context: dict[str, str] = {}

    def _vars() -> dict[str, str]:
        combined = dict(input_values or {})
        combined.update(runtime_context)
        return combined

    with sync_playwright() as playwright:
        browser = playwright.chromium.launch(headless=True)
        page = browser.new_page()
        page.set_default_timeout(150_000)  # 2.5 min per operation

        # Clear all storage to avoid residual login sessions from previous runs
        page.context.clear_cookies()
        try:
            page.evaluate("localStorage.clear(); sessionStorage.clear();")
        except Exception:
            pass

        console_buffer: list[ConsoleEvent] = []
        network_buffer: list[NetworkEvent] = []
        page.on("console", lambda message: _capture_console_event(message, console_buffer))
        page.on("requestfailed", lambda request: _capture_request_failed(request, network_buffer))
        page.on("response", lambda response: _capture_error_response(response, network_buffer))

        # Auto-navigate to base_url when first step is not a goto
        if case.steps and case.steps[0].action != "goto" and base_url:
            page.goto(base_url, wait_until="domcontentloaded")

        try:
            _execution_started_at = perf_counter()
            _MAX_EXECUTION_SECONDS = 600  # 10 minutes max total
            for index, step in enumerate(case.steps):
                if perf_counter() - _execution_started_at > _MAX_EXECUTION_SECONDS:
                    raise RunnerExecutionError(f"Execution timeout after {_MAX_EXECUTION_SECONDS}s", step_results=step_results)
                if cancel_event is not None and cancel_event.is_set():
                    raise RunnerCancelledError("Execution cancelled by user.", step_results=step_results)

                # Substitute runtime variables in target (e.g., "${cart_a_total}" → "Rs. 500")
                resolved_target = _substitute_variables(getattr(step, 'target', None), _vars()) or getattr(step, 'target', None)

                yield StepStreamEvent(
                    type="step_start",
                    step_index=index,
                    action=step.action,
                    target=getattr(step, "target", None),
                    value=getattr(step, "value", None),
                )

                step_started_at = perf_counter()
                console_index = len(console_buffer)
                network_index = len(network_buffer)
                resolved = None
                locator_trace = None

                try:
                    resolved_by = None
                    click_recovery = None
                    click_recovery_detail = None
                    vlm_preverify_used = False
                    step_value = getattr(step, "value", None)

                    # --- Dual-layer scoring path (new) ---
                    if hasattr(step, "candidates"):
                        evidence_for_step = _execute_step_with_candidates(
                            page, step, index,
                            artifact_dir=artifact_dir,
                            execution_id=execution_id,
                            correction_store=correction_store,
                            input_values=_vars(),
                            runtime_context=runtime_context,
                        )
                    else:
                        evidence_for_step = _execute_non_target_step(
                            page,
                            step,
                            index,
                            base_url=base_url,
                            artifact_dir=artifact_dir,
                            input_values=_vars(),
                        )
                    step_results.append(evidence_for_step)
                    yield StepStreamEvent(
                        type="step_complete",
                        step_index=index,
                        action=step.action,
                        status="passed",
                        duration_ms=evidence_for_step.duration_ms,
                    )
                    if evidence_for_step.status == "passed":
                        continue

                    if step.action == "goto":
                        page.goto(_resolve_url(_substitute_variables(step.value, _vars()), base_url), wait_until="domcontentloaded")
                    elif step.action == "click":
                        resolved, vlm_preverify_used = _resolve_with_confidence_gate(
                            page, resolved_target,
                            locator_confidence=getattr(step, "locator_confidence", None),
                            target_strategy=step.target_strategy,
                            correction_store=correction_store,
                            execution_id=execution_id,
                            require_visible=True, require_enabled=True,
                        )
                        resolved_by = resolved.strategy
                        locator_trace = resolved.trace
                        cr = click_with_precheck(
                            page, resolved.locator,
                            click_coordinates=resolved.click_coordinates,
                        )
                        if not cr.succeeded:
                            raise cr.original_error or RunnerExecutionError("Click failed")
                        click_recovery = cr.recovery_strategy
                        click_recovery_detail = cr.recovery_detail
                    elif step.action == "input":
                        resolved, vlm_preverify_used = _resolve_with_confidence_gate(
                            page, resolved_target,
                            locator_confidence=getattr(step, "locator_confidence", None),
                            target_strategy=step.target_strategy,
                            correction_store=correction_store,
                            execution_id=execution_id,
                            prefer_input=True, require_visible=True, require_enabled=True,
                        )
                        resolved_by = resolved.strategy
                        locator_trace = resolved.trace
                        input_value = _substitute_variables(step.value, _vars())
                        if resolved.click_coordinates is not None:
                            cr = click_with_precheck(
                                page, resolved.locator,
                                click_coordinates=resolved.click_coordinates,
                            )
                            if not cr.succeeded:
                                raise cr.original_error or RunnerExecutionError("Click failed")
                            click_recovery = cr.recovery_strategy
                            click_recovery_detail = cr.recovery_detail
                            page.keyboard.type(input_value)
                        else:
                            tag_name = resolved.locator.evaluate("el => el.tagName.toLowerCase()")
                            if tag_name == "select":
                                resolved.locator.select_option(label=input_value)
                            else:
                                resolved.locator.fill(input_value)
                    elif step.action == "wait_for":
                        resolved, vlm_preverify_used = _resolve_with_confidence_gate(
                            page, resolved_target,
                            locator_confidence=getattr(step, "locator_confidence", None),
                            target_strategy=step.target_strategy,
                            correction_store=correction_store,
                            execution_id=execution_id,
                            require_visible=False,
                        )
                        resolved_by = resolved.strategy
                        locator_trace = resolved.trace
                        resolved.locator.wait_for(state="visible", timeout=step.timeout_ms)
                    elif step.action == "assert_text":
                        resolved, vlm_preverify_used = _resolve_with_confidence_gate(
                            page, resolved_target,
                            locator_confidence=getattr(step, "locator_confidence", None),
                            target_strategy=step.target_strategy,
                            correction_store=correction_store,
                            execution_id=execution_id,
                            require_visible=False,
                            expected_text=_substitute_variables(step.value, _vars()),
                        )
                        resolved_by = resolved.strategy
                        locator_trace = resolved.trace
                        expect(resolved.locator).to_contain_text(
                            _substitute_variables(step.value, _vars()),
                        )
                    elif step.action == "assert_url_contains":
                        if _substitute_variables(step.value, _vars()) not in page.url:
                            raise RunnerExecutionError(
                                f"URL assertion failed, expected fragment: {_substitute_variables(step.value, _vars())}"
                            )
                    elif step.action == "capture_text":
                        resolved, vlm_preverify_used = _resolve_with_confidence_gate(
                            page, resolved_target,
                            locator_confidence=getattr(step, "locator_confidence", None),
                            target_strategy=step.target_strategy,
                            correction_store=correction_store,
                            execution_id=execution_id,
                            require_visible=False,
                        )
                        resolved_by = resolved.strategy
                        locator_trace = resolved.trace
                        captured = resolved.locator.inner_text()
                        runtime_context[step.context_key] = captured.strip()
                    else:
                        raise RunnerExecutionError(f"Unsupported action: {step.action}")

                    evidence = StepExecutionEvidence(
                        step_index=index,
                        action=step.action,
                        target=getattr(step, "target", None),
                        value=step_value,
                        status="passed",
                        duration_ms=_elapsed_ms(step_started_at),
                        resolved_by=resolved_by,
                        locator_trace=locator_trace,
                        url=page.url or None,
                        page_title=_safe_page_title(page),
                        viewport=_safe_viewport(page),
                        dom_summary=_safe_dom_summary(page),
                        console_events=console_buffer[console_index:],
                        network_events=network_buffer[network_index:],
                        screenshot_path=_take_step_screenshot(page, artifact_dir, index),
                        click_recovery=click_recovery,
                        click_recovery_detail=click_recovery_detail,
                        locator_confidence=getattr(step, "locator_confidence", None),
                        vlm_preverify_used=vlm_preverify_used,
                    )
                    step_results.append(evidence)

                    yield StepStreamEvent(
                        type="step_complete",
                        step_index=index,
                        action=step.action,
                        status="passed",
                        duration_ms=evidence.duration_ms,
                    )
                except InterventionNeededError as exc:
                    screenshot_path = _take_step_screenshot(page, artifact_dir, index)
                    evidence = StepExecutionEvidence(
                        step_index=index, action=step.action,
                        target=getattr(step, "target", None),
                        value=getattr(step, "value", None),
                        status="failed", duration_ms=_elapsed_ms(step_started_at),
                        resolved_by=None, locator_trace=exc.tier1_trace,
                        url=page.url or None, page_title=_safe_page_title(page),
                        viewport=_safe_viewport(page), dom_summary=_safe_dom_summary(page),
                        console_events=console_buffer[console_index:],
                        network_events=network_buffer[network_index:],
                        screenshot_path=screenshot_path,
                        error_message=str(exc),
                        locator_confidence=getattr(step, "locator_confidence", None),
                        vlm_preverify_used=vlm_preverify_used,
                        intervention_request=InterventionRequest(
                            screenshot_url=_artifact_url_for_path(screenshot_path),
                            page_url=exc.page_url, target_description=exc.target,
                            ai_candidate=(
                                AILocateCandidate(
                                    center=list(exc.ai_candidate.center),
                                    bbox=list(exc.ai_candidate.bbox),
                                    confidence=exc.ai_candidate.confidence,
                                    raw_response=exc.ai_candidate.raw_response,
                                ) if exc.ai_candidate is not None else None
                            ),
                            locator_trace=exc.tier1_trace,
                            vlm_failure_reason=getattr(exc, "vlm_failure_reason", None),
                        ),
                    )
                    step_results.append(evidence)
                    yield StepStreamEvent(
                        type="step_complete", step_index=index,
                        action=step.action, status="failed",
                        duration_ms=evidence.duration_ms,
                    )
                    raise RunnerInterventionError(str(exc), step_results=step_results) from exc
                except (LocatorResolutionError, PlaywrightTimeoutError, RunnerExecutionError, AssertionError) as exc:
                    if isinstance(exc, RunnerExecutionError) and exc.step_evidence is not None:
                        evidence = exc.step_evidence
                        step_results.append(evidence)
                        yield StepStreamEvent(
                            type="step_complete",
                            step_index=index,
                            action=step.action,
                            status="failed",
                            duration_ms=evidence.duration_ms,
                        )
                        raise RunnerExecutionError(
                            str(exc),
                            step_results=step_results,
                            step_evidence=evidence,
                        ) from exc
                    if isinstance(exc, LocatorResolutionError):
                        locator_trace = exc.trace
                    elif resolved is not None:
                        locator_trace = resolved.trace

                    evidence = StepExecutionEvidence(
                        step_index=index, action=step.action,
                        target=getattr(step, "target", None),
                        value=getattr(step, "value", None),
                        status="failed", duration_ms=_elapsed_ms(step_started_at),
                        resolved_by=resolved.strategy if resolved is not None else None,
                        locator_trace=locator_trace,
                        url=page.url or None, page_title=_safe_page_title(page),
                        viewport=_safe_viewport(page), dom_summary=_safe_dom_summary(page),
                        console_events=console_buffer[console_index:],
                        network_events=network_buffer[network_index:],
                        screenshot_path=_take_step_screenshot(page, artifact_dir, index),
                        error_message=str(exc),
                        locator_confidence=getattr(step, "locator_confidence", None),
                        vlm_preverify_used=vlm_preverify_used,
                    )
                    step_results.append(evidence)
                    yield StepStreamEvent(
                        type="step_complete", step_index=index,
                        action=step.action, status="failed",
                        duration_ms=evidence.duration_ms,
                    )
                    raise RunnerExecutionError(str(exc), step_results=step_results) from exc
            _attach_final_dom_snapshot(page, artifact_dir, step_results)
        finally:
            browser.close()

    return step_results


def _resolve_url(value: str, base_url: str | None) -> str:
    if value.startswith(("http://", "https://")):
        return value
    if not base_url:
        raise RunnerExecutionError("Relative goto step requires case.base_url or execution request base_url.")
    return urljoin(base_url.rstrip("/") + "/", value.lstrip("/"))


def _take_step_screenshot(page, artifact_dir: Path, step_index: int) -> str | None:
    screenshot_path = artifact_dir / f"step-{step_index + 1:02d}.png"
    try:
        page.screenshot(path=str(screenshot_path), full_page=True)
    except Exception:
        return None
    return str(screenshot_path.relative_to(Path(__file__).resolve().parents[2]))


def _attach_final_dom_snapshot(
    page,
    artifact_dir: Path,
    step_results: list[StepExecutionEvidence],
) -> None:
    if not step_results:
        return
    snapshot_path = artifact_dir / "final.html"
    try:
        snapshot_path.write_text(page.content(), encoding="utf-8")
    except Exception:
        return
    relative_path = str(snapshot_path.relative_to(Path(__file__).resolve().parents[2]))
    step_results[-1] = step_results[-1].model_copy(
        update={
            "dom_snapshot_path": relative_path,
            "dom_snapshot_url": _artifact_url_for_path(relative_path),
        }
    )


def _artifact_url_for_path(screenshot_path: str | None) -> str | None:
    if not screenshot_path:
        return None
    normalized = screenshot_path.replace("\\", "/").lstrip("/")
    return f"/{normalized}" if normalized.startswith("artifacts/") else None


def _elapsed_ms(started_at: float) -> int:
    return max(0, int((perf_counter() - started_at) * 1000))


def _safe_page_title(page) -> str | None:
    try:
        title = page.title()
    except Exception:
        return None
    return title or None


def _safe_viewport(page) -> ViewportSnapshot | None:
    try:
        viewport = page.viewport_size
    except Exception:
        return None
    if not viewport:
        return None
    return ViewportSnapshot(width=viewport.get("width", 0), height=viewport.get("height", 0))


def _safe_dom_summary(page) -> DOMSummary | None:
    try:
        payload = page.evaluate(
            """
            () => {
              const text = (document.body?.innerText || "").replace(/\\s+/g, " ").trim().slice(0, 240);
              return {
                text_preview: text || null,
                button_count: document.querySelectorAll("button").length,
                input_count: document.querySelectorAll("input, textarea, select").length,
                link_count: document.querySelectorAll("a").length,
              };
            }
            """
        )
    except Exception:
        return None
    return DOMSummary.model_validate(payload)


def _capture_console_event(message, console_buffer: list[ConsoleEvent]) -> None:
    level = message.type
    if level not in {"error", "warning"}:
        return
    location = message.location or {}
    console_buffer.append(
        ConsoleEvent(
            level=level,
            text=message.text,
            source_url=location.get("url"),
            line_number=location.get("lineNumber"),
        )
    )


def _capture_request_failed(request, network_buffer: list[NetworkEvent]) -> None:
    failure = request.failure
    failure_text = failure if isinstance(failure, str) else (failure or {}).get("errorText")
    network_buffer.append(
        NetworkEvent(
            url=request.url,
            method=request.method,
            resource_type=request.resource_type,
            failure_text=failure_text,
        )
    )


def _capture_error_response(response, network_buffer: list[NetworkEvent]) -> None:
    if response.status < 400:
        return
    request = response.request
    network_buffer.append(
        NetworkEvent(
            url=response.url,
            method=request.method,
            status=response.status,
            resource_type=request.resource_type,
        )
    )
