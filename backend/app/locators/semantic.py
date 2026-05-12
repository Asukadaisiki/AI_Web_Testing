"""Minimal locator helpers for first-phase execution."""

from __future__ import annotations

import logging
import re

logger = logging.getLogger(__name__)
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
    click_coordinates: tuple[int, int] | None = None


@dataclass(frozen=True)
class SemanticCandidateEntry:
    strategy: str
    locator: object
    candidate: LocatorCandidateEvidence


def resolve_semantic_locator(
    page,
    target: str,
    *,
    target_strategy: str | None = None,
    prefer_input: bool = False,
    require_visible: bool = True,
    require_enabled: bool = False,
) -> ResolvedLocator:
    normalized_target = target.strip()
    # Prefer hinted strategy first; on failure fall through to exhaustive scan.
    if target_strategy is not None and target_strategy != "semantic":
        try:
            return _resolve_by_strategy(
                page, normalized_target, target_strategy,
                prefer_input=prefer_input,
                require_visible=require_visible,
                require_enabled=require_enabled,
            )
        except LocatorResolutionError:
            pass  # Fall through to exhaustive semantic scan
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
    target_strategy: str | None = None,
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
        except Exception as exc:
            logger.debug("Candidate builder '%s' failed for target=%r: %s", strategy, normalized_target, exc)
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
    if explicit is None and target and not target.startswith(("css=", "xpath=", "//", "#", ".", "[", "data-testid=")):
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
            ("link_role", lambda: page.get_by_role("link", name=target, exact=True)),
            ("menuitem_role", lambda: page.get_by_role("menuitem", name=target, exact=True)),
            ("label", lambda: page.get_by_label(target, exact=True)),
            ("placeholder", lambda: page.get_by_placeholder(target, exact=True)),
            ("text", lambda: page.get_by_text(target, exact=True)),
            ("button_role_fuzzy", lambda: page.get_by_role("button", name=target)),
            ("link_role_fuzzy", lambda: page.get_by_role("link", name=target)),
            ("menuitem_role_fuzzy", lambda: page.get_by_role("menuitem", name=target)),
            ("label_fuzzy", lambda: page.get_by_label(target)),
            ("placeholder_fuzzy", lambda: page.get_by_placeholder(target)),
            ("text_fuzzy", lambda: page.get_by_text(target)),
        ]
    )
    return builders


_COMPOUND_CSS_RE = re.compile(r"^[a-zA-Z][a-zA-Z0-9]*[\.\#\[\s\>:,~\+]")
_HTML_TAG_NAMES = frozenset({
    "html", "body", "head", "div", "span", "p", "a", "form", "table",
    "tr", "td", "th", "ul", "ol", "li", "h1", "h2", "h3", "h4", "h5", "h6",
    "input", "button", "select", "option", "textarea", "label", "img",
    "section", "article", "nav", "header", "footer", "main", "aside",
})

# Matches: .class text='Value', .class >> text=Value, #id text="Value"
_CHAINED_SELECTOR_RE = re.compile(
    r"""(?P<base>^\s*[.#\[][^\s>]+|^\s*[a-zA-Z][a-zA-Z0-9_-]*(?:\.[a-zA-Z][a-zA-Z0-9_-]*)+)
        \s*(?:>>\s*)?
        (?P<method>text|Text|TEXT)\s*[=:]\s*["']?(?P<value>[^"']+)["']?
        \s*$""",
    re.VERBOSE,
)


_PARENT_SPLIT_RE = re.compile(
    r"""\s*(?:>>|的|附近的)\s*""",
)


def _find_in_ancestor(page, parent_text: str, child_text: str) -> object:
    """Find child_text within the nearest common ancestor of parent_text element.

    Tries depths 2-8, returning the SHALLOWEST ancestor that contains child_text.
    This adapts to any DOM structure: table rows (~3 levels), cards (~5), modals (~2).
    """
    parent_el = page.get_by_text(parent_text, exact=False).first
    for _depth in range(2, 9):
        ancestor = parent_el
        for _ in range(_depth):
            ancestor = ancestor.locator("..")
        try:
            child = ancestor.get_by_text(child_text, exact=False)
            n = child.count()
            if n > 0:
                return child.first
        except Exception:
            continue
    # Fallback: try without ancestor walk
    return page.get_by_text(child_text, exact=False).first


def _resolve_text_parent_chain(page, target: str) -> tuple[str, object] | None:
    """Parse text-based parent chaining: "Blue Top" >> "Add to cart".

    Supports multi-level chains: "A" >> "B" >> "C" finds C within B's ancestor
    which is within A's ancestor.

    Splits on >> / 的 / 附近的, then finds an element with *parent* text,
    walks up to its container, and searches within for *child* text.
    """
    parts = _PARENT_SPLIT_RE.split(target.strip())
    if len(parts) < 2:
        return None

    # Strip quotes from all parts
    cleaned_parts = [p.strip().strip("\"'") for p in parts]
    # Filter out empty parts
    cleaned_parts = [p for p in cleaned_parts if p]

    if len(cleaned_parts) < 2:
        return None

    def _build():
        try:
            # For multi-level chains, iterate from outermost to innermost
            # e.g., "A" >> "B" >> "C" -> find B in A's ancestor, then find C in B's ancestor
            current_locator = page.get_by_text(cleaned_parts[0], exact=False).first

            for i in range(1, len(cleaned_parts)):
                parent_text = cleaned_parts[i - 1]
                child_text = cleaned_parts[i]

                # Try to find child within the current context
                found = False
                for _depth in range(2, 9):
                    ancestor = current_locator
                    for _ in range(_depth):
                        ancestor = ancestor.locator("..")
                    try:
                        child = ancestor.get_by_text(child_text, exact=False)
                        n = child.count()
                        if n > 0:
                            current_locator = child.first
                            found = True
                            break
                    except Exception:
                        continue

                if not found:
                    # Fallback: try direct search
                    current_locator = page.get_by_text(child_text, exact=False).first

            return current_locator
        except Exception as exc:
            logger.debug("text_parent_chain failed for target=%r: %s", target, exc)
            raise
    return ("text_parent_chain", _build)



def _resolve_chained_selector(page, target: str) -> tuple[str, object] | None:
    """Parse Playwright-style chained selectors like ``.class text='Value'``.

    Supported formats:
    - ``.class text='Value'``
    - ``.class >> text=Value``
    - ``#id text="Value"``
    - ``tag.class text='Value'``

    Returns a ``chained_css_text`` strategy that chains ``page.locator(css)`` +
    ``.get_by_text(value)``.
    """
    m = _CHAINED_SELECTOR_RE.match(target)
    if m is None:
        return None
    base_css = m.group("base").strip()
    text_value = m.group("value").strip()
    if not base_css or not text_value:
        return None
    return (
        "chained_css_text",
        lambda: page.locator(base_css).get_by_text(text_value),
    )


def _resolve_explicit_locator(page, target: str) -> tuple[str, object] | None:
    # Chained selector (e.g. ".productinfo text='View Product'")
    chained = _resolve_chained_selector(page, target)
    if chained is not None:
        return chained
    text_parent = _resolve_text_parent_chain(page, target)
    if text_parent is not None:
        return text_parent
    if target.startswith("css="):
        return ("css", lambda: page.locator(target))
    if target.startswith("xpath="):
        return ("xpath", lambda: page.locator(target))
    if target.startswith("//"):
        return ("xpath", lambda: page.locator(f"xpath={target}"))
    if target.startswith(("#", "[")):
        return ("css", lambda: page.locator(target))
    # Single dot-prefixed class without chained suffix → plain CSS
    if target.startswith("."):
        return ("css", lambda: page.locator(target))
    if target.startswith("data-testid="):
        value = target.split("=", 1)[1]
        return ("data-testid", lambda: page.get_by_test_id(value))
    if _COMPOUND_CSS_RE.match(target):
        return ("css", lambda: page.locator(target))
    if target.lower() in _HTML_TAG_NAMES:
        lower_target = target.lower()
        return ("css_tag", lambda: page.locator(lower_target))
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
        "text_parent_chain": 112,
        "chained_css_text": 110,
        "css_tag": 105,
        "element_id": 100,
        "button_role": 90,
        "link_role": 85,
        "menuitem_role": 85,
        "label": 80,
        "placeholder": 75,
        "text": 70,
        "label_fuzzy": 60,
        "button_role_fuzzy": 55,
        "link_role_fuzzy": 55,
        "menuitem_role_fuzzy": 55,
        "placeholder_fuzzy": 55,
        "text_fuzzy": 50,
    }.get(strategy, 50)


def _strategy_rule_name(strategy: str) -> str:
    return {
        "css": "explicit-css-selector",
        "xpath": "explicit-xpath-selector",
        "data-testid": "explicit-data-testid",
        "chained_css_text": "chained-css-text-selector",
        "css_tag": "explicit-html-tag-selector",
        "element_id": "element-id-match",
        "button_role": "exact-button-role-match",
        "link_role": "exact-link-role-match",
        "menuitem_role": "exact-menuitem-role-match",
        "label": "exact-label-match",
        "placeholder": "exact-placeholder-match",
        "text": "exact-text-match",
        "button_role_fuzzy": "fuzzy-button-role-match",
        "link_role_fuzzy": "fuzzy-link-role-match",
        "menuitem_role_fuzzy": "fuzzy-menuitem-role-match",
        "label_fuzzy": "fuzzy-label-match",
        "placeholder_fuzzy": "fuzzy-placeholder-match",
        "text_fuzzy": "fuzzy-text-match",
    }.get(strategy, strategy)


def _build_strategy_builder(
    page, target: str, strategy: str, *, prefer_input: bool = False,
) -> tuple[str, object] | None:
    if strategy == "css":
        css_target = target.removeprefix("css=")
        return ("css", lambda: page.locator(css_target))
    if strategy == "xpath":
        xpath_target = target.removeprefix("xpath=")
        if not xpath_target.startswith("/"):
            xpath_target = f"xpath={xpath_target}"
        return ("xpath", lambda: page.locator(xpath_target))
    if strategy == "data-testid":
        value = target.removeprefix("data-testid=")
        return ("data-testid", lambda: page.get_by_test_id(value))
    if strategy == "element_id":
        return ("element_id", lambda: page.locator(f"#{target}"))
    if strategy == "tag":
        lower_target = target.lower()
        return ("css_tag", lambda: page.locator(lower_target))
    return None


def _resolve_by_strategy(
    page,
    target: str,
    strategy: str,
    *,
    prefer_input: bool = False,
    require_visible: bool = True,
    require_enabled: bool = False,
) -> ResolvedLocator:
    builder = _build_strategy_builder(page, target, strategy, prefer_input=prefer_input)
    if builder is None:
        raise LocatorResolutionError(
            f"Unknown target_strategy: {strategy}",
            trace=LocatorTrace(target=target, failure_reason=f"Unknown target_strategy: {strategy}"),
        )
    strategy_name, build_locator = builder
    locator_collection = build_locator()
    count = locator_collection.count()
    if count == 0:
        raise LocatorResolutionError(
            f"Strategy {strategy} matched 0 elements for target: {target}",
            trace=LocatorTrace(
                target=target,
                match_strategy=strategy_name,
                failure_reason=f"Strategy {strategy} matched 0 elements.",
            ),
        )
    candidate_locator = locator_collection.nth(0)
    candidate = _build_candidate_evidence(candidate_locator, strategy_name)
    scored = _score_candidate(
        candidate,
        strategy=strategy_name,
        require_visible=require_visible,
        require_enabled=require_enabled,
    )
    return ResolvedLocator(
        strategy=strategy_name,
        locator=candidate_locator,
        trace=LocatorTrace(
            target=target,
            match_strategy=strategy_name,
            selected_candidate=scored,
            candidates=[scored],
            selection_reason=f"Resolved by explicit target_strategy={strategy}.",
        ),
    )


def _build_selection_reason(candidate: LocatorCandidateEvidence) -> str:
    rules = ", ".join(candidate.matched_rules) if candidate.matched_rules else candidate.strategy
    return f"Selected highest-scoring candidate ({candidate.score}) with rules: {rules}."
