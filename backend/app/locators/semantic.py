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

            candidates.append(candidate)
            if selected_locator is None and _candidate_matches_requirements(
                candidate,
                require_visible=require_visible,
                require_enabled=require_enabled,
            ):
                selected_locator = candidate_locator
                selected_candidate = candidate
                selected_strategy = strategy

    if selected_locator is not None and selected_candidate is not None and selected_strategy is not None:
        return ResolvedLocator(
            strategy=selected_strategy,
            locator=selected_locator,
            trace=LocatorTrace(
                target=normalized_target,
                match_strategy=selected_strategy,
                candidates=candidates,
                selected_candidate=selected_candidate,
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
    if require_visible and not candidate.visible:
        return False
    if require_enabled and not candidate.enabled:
        return False
    return True


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
