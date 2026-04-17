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


@dataclass(frozen=True)
class SemanticCandidateEntry:
    strategy: str
    locator: object
    candidate: LocatorCandidateEvidence


def resolve_semantic_locator(
    page,
    target: str,
    *,
    prefer_input: bool = False,
    require_visible: bool = True,
    require_enabled: bool = False,
) -> ResolvedLocator:
    normalized_target = target.strip()
    entries = collect_semantic_candidates(
        page,
        normalized_target,
        prefer_input=prefer_input,
        require_visible=require_visible,
        require_enabled=require_enabled,
    )
    candidates = [entry.candidate for entry in entries[:5]]
    selected_entry = next((entry for entry in entries if not entry.candidate.rejected_reasons), None)

    if selected_entry is not None:
        return ResolvedLocator(
            strategy=selected_entry.strategy,
            locator=selected_entry.locator,
            trace=LocatorTrace(
                target=normalized_target,
                match_strategy=selected_entry.strategy,
                candidates=candidates,
                selected_candidate=selected_entry.candidate,
                selection_reason=_build_selection_reason(selected_entry.candidate),
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


def collect_semantic_candidates(
    page,
    target: str,
    *,
    prefer_input: bool = False,
    require_visible: bool = True,
    require_enabled: bool = False,
    max_per_strategy: int = 3,
    max_candidates: int = 5,
) -> list[SemanticCandidateEntry]:
    normalized_target = target.strip()
    candidate_builders = _build_candidate_builders(page, normalized_target, prefer_input=prefer_input)
    entries: list[SemanticCandidateEntry] = []

    for strategy, build_locator in candidate_builders:
        try:
            locator_collection = build_locator()
            count = locator_collection.count()
        except Exception:
            continue

        for index in range(min(count, max_per_strategy)):
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
            entries.append(
                SemanticCandidateEntry(
                    strategy=strategy,
                    locator=candidate_locator,
                    candidate=scored_candidate,
                )
            )

    entries.sort(key=lambda entry: entry.candidate.score, reverse=True)
    return entries[:max_candidates]


def _build_candidate_builders(page, target: str, *, prefer_input: bool) -> list[tuple[str, object]]:
    builders: list[tuple[str, object]] = []
    explicit = _resolve_explicit_locator(page, target)
    if explicit is not None:
        builders.append(explicit)

    # Try matching the target as an HTML element id attribute.
    if target and not target.startswith(("css=", "xpath=", "//", "#", ".", "[", "data-testid=")):
        id_target = target
        builders.append(("element_id", lambda: page.locator(f"#{id_target}")))

    if prefer_input:
        builders.extend(
            [
                ("label", lambda: page.get_by_label(target, exact=True)),
                ("placeholder", lambda: page.get_by_placeholder(target, exact=True)),
                ("label_fuzzy", lambda: page.get_by_label(target)),
                ("placeholder_fuzzy", lambda: page.get_by_placeholder(target)),
            ]
        )

    builders.extend(
        [
            ("button_role", lambda: page.get_by_role("button", name=target, exact=True)),
            ("label", lambda: page.get_by_label(target, exact=True)),
            ("placeholder", lambda: page.get_by_placeholder(target, exact=True)),
            ("text", lambda: page.get_by_text(target, exact=True)),
            ("button_role_fuzzy", lambda: page.get_by_role("button", name=target)),
            ("label_fuzzy", lambda: page.get_by_label(target)),
            ("placeholder_fuzzy", lambda: page.get_by_placeholder(target)),
            ("text_fuzzy", lambda: page.get_by_text(target)),
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
        "element_id": 100,
        "button_role": 90,
        "label": 80,
        "placeholder": 75,
        "text": 70,
        "label_fuzzy": 60,
        "placeholder_fuzzy": 55,
        "text_fuzzy": 50,
        "button_role_fuzzy": 45,
    }.get(strategy, 50)


def _strategy_rule_name(strategy: str) -> str:
    return {
        "css": "explicit-css-selector",
        "xpath": "explicit-xpath-selector",
        "data-testid": "explicit-data-testid",
        "element_id": "element-id-match",
        "button_role": "exact-button-role-match",
        "label": "exact-label-match",
        "placeholder": "exact-placeholder-match",
        "text": "exact-text-match",
        "button_role_fuzzy": "fuzzy-button-role-match",
        "label_fuzzy": "fuzzy-label-match",
        "placeholder_fuzzy": "fuzzy-placeholder-match",
        "text_fuzzy": "fuzzy-text-match",
    }.get(strategy, strategy)


def _build_selection_reason(candidate: LocatorCandidateEvidence) -> str:
    rules = ", ".join(candidate.matched_rules) if candidate.matched_rules else candidate.strategy
    return f"Selected highest-scoring candidate ({candidate.score}) with rules: {rules}."
