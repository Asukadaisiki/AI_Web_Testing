"""Minimal locator helpers for first-phase execution."""

from __future__ import annotations

from dataclasses import dataclass


class LocatorResolutionError(ValueError):
    """Raised when a target cannot be mapped to a Playwright locator."""


@dataclass(frozen=True)
class ResolvedLocator:
    strategy: str
    locator: object


def resolve_semantic_locator(page, target: str, *, prefer_input: bool = False) -> ResolvedLocator:
    normalized_target = target.strip()
    explicit = _resolve_explicit_locator(page, normalized_target)
    if explicit is not None:
        return explicit

    candidate_builders = []
    if prefer_input:
        candidate_builders.extend(
            [
                ("label", lambda: page.get_by_label(normalized_target, exact=True)),
                ("placeholder", lambda: page.get_by_placeholder(normalized_target, exact=True)),
            ]
        )

    candidate_builders.extend(
        [
            ("button_role", lambda: page.get_by_role("button", name=normalized_target, exact=True)),
            ("label", lambda: page.get_by_label(normalized_target, exact=True)),
            ("placeholder", lambda: page.get_by_placeholder(normalized_target, exact=True)),
            ("text", lambda: page.get_by_text(normalized_target, exact=True)),
        ]
    )

    seen = set()
    for strategy, build_locator in candidate_builders:
        if strategy in seen and strategy in {"label", "placeholder"}:
            continue
        seen.add(strategy)
        locator = build_locator()
        try:
            if locator.count() > 0:
                return ResolvedLocator(strategy=strategy, locator=locator.first)
        except Exception:
            continue

    raise LocatorResolutionError(f"Unable to resolve target: {target}")


def _resolve_explicit_locator(page, target: str) -> ResolvedLocator | None:
    if target.startswith("css="):
        return ResolvedLocator(strategy="css", locator=page.locator(target))
    if target.startswith("xpath="):
        return ResolvedLocator(strategy="xpath", locator=page.locator(target))
    if target.startswith("//"):
        return ResolvedLocator(strategy="xpath", locator=page.locator(f"xpath={target}"))
    if target.startswith(("#", ".", "[")):
        return ResolvedLocator(strategy="css", locator=page.locator(target))
    if target.startswith("data-testid="):
        value = target.split("=", 1)[1]
        return ResolvedLocator(strategy="data-testid", locator=page.get_by_test_id(value))
    return None
