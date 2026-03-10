"""Minimal locator helpers for first-phase execution."""

from __future__ import annotations

from dataclasses import dataclass

from app.schemas.executions import (
    LocatorCandidateAttributes,
    LocatorCandidateEvidence,
    LocatorTrace,
)


class LocatorResolutionError(ValueError):
    """Raised when a target cannot be mapped to a Playwright locator."""

    def __init__(self, message: str, *, trace: LocatorTrace | None = None) -> None:
        super().__init__(message)
        self.trace = trace


@dataclass(frozen=True)
class ResolvedLocator:
    strategy: str
    locator: object
    trace: LocatorTrace


def resolve_semantic_locator(
    page,
    target: str,
    *,
    prefer_input: bool = False,
    require_visible: bool = True,
    require_enabled: bool = False,
) -> ResolvedLocator:
    normalized_target = target.strip()
    candidate_builders = _build_candidate_builders(page, normalized_target, prefer_input=prefer_input)

    candidates: list[LocatorCandidateEvidence] = []
    ranked_candidates: list[tuple[int, int, object, LocatorCandidateEvidence, str]] = []
    selected_locator = None
    selected_candidate = None
    selected_strategy = None

    for strategy, build_locator in candidate_builders:
        try:
            locator_collection = build_locator()
            count = locator_collection.count()
        except Exception:
            continue

        for index in range(min(count, 3)):
            try:
                candidate_locator = locator_collection.nth(index)
                candidate = _build_candidate_evidence(candidate_locator, strategy)
            except Exception:
                continue

            scored_candidate = _score_candidate(
                candidate,
                strategy=strategy,
                require_visible=require_visible,
                require_enabled=require_enabled,
            )
            candidates.append(scored_candidate)
            if not scored_candidate.rejected_reasons:
                ranked_candidates.append((scored_candidate.score, -len(ranked_candidates), candidate_locator, scored_candidate, strategy))

    if ranked_candidates:
        _, _, selected_locator, selected_candidate, selected_strategy = max(ranked_candidates, key=lambda item: (item[0], item[1]))
    candidates = sorted(candidates, key=lambda item: item.score, reverse=True)
    if selected_candidate is not None and all(candidate != selected_candidate for candidate in candidates[:5]):
        candidates = [selected_candidate, *[candidate for candidate in candidates if candidate != selected_candidate]]
    candidates = candidates[:5]

    if selected_locator is not None and selected_candidate is not None and selected_strategy is not None:
        return ResolvedLocator(
            strategy=selected_strategy,
            locator=selected_locator,
            trace=LocatorTrace(
                target=normalized_target,
                match_strategy=selected_strategy,
                candidates=candidates,
                selected_candidate=selected_candidate,
                selection_reason=_build_selection_reason(selected_candidate),
            ),
        )

    failure_reason = _resolve_failure_reason(
        candidates,
        require_visible=require_visible,
        require_enabled=require_enabled,
    )
    raise LocatorResolutionError(
        failure_reason,
        trace=LocatorTrace(
            target=normalized_target,
            candidates=candidates,
            failure_reason=failure_reason,
        ),
    )


def _build_candidate_builders(page, target: str, *, prefer_input: bool) -> list[tuple[str, object]]:
    builders: list[tuple[str, object]] = []
    explicit = _resolve_explicit_locator(page, target)
    if explicit is not None:
        builders.append(explicit)

    if prefer_input:
        builders.extend(
            [
                ("label", lambda: page.get_by_label(target, exact=True)),
                ("placeholder", lambda: page.get_by_placeholder(target, exact=True)),
            ]
        )

    builders.extend(
        [
            ("button_role", lambda: page.get_by_role("button", name=target, exact=True)),
            ("label", lambda: page.get_by_label(target, exact=True)),
            ("placeholder", lambda: page.get_by_placeholder(target, exact=True)),
            ("text", lambda: page.get_by_text(target, exact=True)),
        ]
    )
    return builders


def _resolve_explicit_locator(page, target: str) -> tuple[str, object] | None:
    if target.startswith("css="):
        return ("css", lambda: page.locator(target))
    if target.startswith("xpath="):
        return ("xpath", lambda: page.locator(target))
    if target.startswith("//"):
        return ("xpath", lambda: page.locator(f"xpath={target}"))
    if target.startswith(("#", ".", "[")):
        return ("css", lambda: page.locator(target))
    if target.startswith("data-testid="):
        value = target.split("=", 1)[1]
        return ("data-testid", lambda: page.get_by_test_id(value))
    return None


def _candidate_matches_requirements(
    candidate: LocatorCandidateEvidence,
    *,
    require_visible: bool,
    require_enabled: bool,
) -> bool:
    return not _build_rejected_reasons(
        candidate,
        require_visible=require_visible,
        require_enabled=require_enabled,
    )


def _resolve_failure_reason(
    candidates: list[LocatorCandidateEvidence],
    *,
    require_visible: bool,
    require_enabled: bool,
) -> str:
    if not candidates:
        return "No locator candidates matched target."
    if require_visible and not any(candidate.visible for candidate in candidates):
        return "Locator candidates matched target but none are visible."
    if require_enabled and not any(candidate.enabled for candidate in candidates):
        return "Locator candidates matched target but none are enabled."
    return "Locator candidates matched target but did not satisfy the selection rules."


def _build_candidate_evidence(locator, strategy: str) -> LocatorCandidateEvidence:
    payload = locator.evaluate(
        """
        (element) => {
          const preview = (element.innerText || element.textContent || "").replace(/\\s+/g, " ").trim().slice(0, 120);
          const rect = element.getBoundingClientRect();
          const style = window.getComputedStyle(element);
          const visible = rect.width > 0 && rect.height > 0 && style.visibility !== "hidden" && style.display !== "none";
          const enabled = !element.disabled && element.getAttribute("aria-disabled") !== "true";
          return {
            preview_text: preview || null,
            role: element.getAttribute("role") || element.tagName.toLowerCase(),
            attributes: {
              aria_label: element.getAttribute("aria-label"),
              placeholder: element.getAttribute("placeholder"),
              data_testid: element.getAttribute("data-testid"),
            },
            visible,
            enabled,
          };
        }
        """
    )
    return LocatorCandidateEvidence(
        strategy=strategy,
        preview_text=payload.get("preview_text"),
        role=payload.get("role"),
        attributes=LocatorCandidateAttributes.model_validate(payload.get("attributes", {})),
        visible=bool(payload.get("visible")),
        enabled=bool(payload.get("enabled")),
    )


def _score_candidate(
    candidate: LocatorCandidateEvidence,
    *,
    strategy: str,
    require_visible: bool,
    require_enabled: bool,
) -> LocatorCandidateEvidence:
    matched_rules = _build_matched_rules(candidate, strategy)
    rejected_reasons = _build_rejected_reasons(
        candidate,
        require_visible=require_visible,
        require_enabled=require_enabled,
    )
    score = _strategy_base_score(strategy)
    if candidate.visible:
        score += 10
    if candidate.enabled:
        score += 5
    if candidate.preview_text:
        score += 3

    return candidate.model_copy(
        update={
            "score": score,
            "matched_rules": matched_rules,
            "rejected_reasons": rejected_reasons,
        }
    )


def _build_matched_rules(candidate: LocatorCandidateEvidence, strategy: str) -> list[str]:
    matched_rules = [_strategy_rule_name(strategy)]
    if candidate.visible:
        matched_rules.append("visible")
    if candidate.enabled:
        matched_rules.append("enabled")
    if candidate.preview_text:
        matched_rules.append("has-preview-text")
    return matched_rules


def _build_rejected_reasons(
    candidate: LocatorCandidateEvidence,
    *,
    require_visible: bool,
    require_enabled: bool,
) -> list[str]:
    rejected_reasons: list[str] = []
    if require_visible and not candidate.visible:
        rejected_reasons.append("element-not-visible")
    if require_enabled and not candidate.enabled:
        rejected_reasons.append("element-not-enabled")
    return rejected_reasons


def _strategy_base_score(strategy: str) -> int:
    return {
        "css": 120,
        "xpath": 120,
        "data-testid": 115,
        "button_role": 90,
        "label": 80,
        "placeholder": 75,
        "text": 70,
    }.get(strategy, 50)


def _strategy_rule_name(strategy: str) -> str:
    return {
        "css": "explicit-css-selector",
        "xpath": "explicit-xpath-selector",
        "data-testid": "explicit-data-testid",
        "button_role": "exact-button-role-match",
        "label": "exact-label-match",
        "placeholder": "exact-placeholder-match",
        "text": "exact-text-match",
    }.get(strategy, strategy)


def _build_selection_reason(candidate: LocatorCandidateEvidence) -> str:
    rules = ", ".join(candidate.matched_rules) if candidate.matched_rules else candidate.strategy
    return f"Selected highest-scoring candidate ({candidate.score}) with rules: {rules}."
