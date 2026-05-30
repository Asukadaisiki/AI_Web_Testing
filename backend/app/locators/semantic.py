"""Accessibility-tree-based locator resolution.

Resolves DSL targets in ``role="name"`` format (e.g. ``link="Signup / Login"``)
against the browser's accessibility tree using Playwright's ``get_by_role`` API.
"""

from __future__ import annotations

import logging
import re
from dataclasses import dataclass

logger = logging.getLogger(__name__)

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


# ── A11y target parsing ──────────────────────────────────────────────────────

# Matches: role="name", role='name', role="name" id=e123
_A11Y_ROLE_TARGET_RE = re.compile(
    r'^(button|link|textbox|checkbox|radio|menuitem|combobox|listbox|option'
    r'|tab|switch|searchbox|heading|dialog|alert|navigation|main|form|region'
    r'|banner|contentinfo|complementary|article|list|listitem|img|progressbar'
    r'|slider|spinbutton|treeitem|menu|menubar|tablist|toolbar|status|timer'
    r'|tooltip|separator|group|presentation|none)'
    r"""\s*=\s*["'](.+?)["']""",
    re.IGNORECASE,
)

_A11Y_ID_SUFFIX_RE = re.compile(r"""\s+id=(\S+)$""")

# Maps a11y roles to Playwright role names (most are identical)
_A11Y_TO_PLAYWRIGHT_ROLE: dict[str, str] = {
    "button": "button",
    "link": "link",
    "textbox": "textbox",
    "checkbox": "checkbox",
    "radio": "radio",
    "menuitem": "menuitem",
    "combobox": "combobox",
    "listbox": "listbox",
    "option": "option",
    "tab": "tab",
    "switch": "switch",
    "searchbox": "searchbox",
    "heading": "heading",
    "dialog": "dialog",
    "alert": "alert",
    "navigation": "navigation",
    "main": "main",
    "form": "form",
    "region": "region",
    "banner": "banner",
    "contentinfo": "contentinfo",
    "complementary": "complementary",
    "article": "article",
    "list": "list",
    "listitem": "listitem",
    "img": "img",
    "group": "group",
    "toolbar": "toolbar",
    "tablist": "tablist",
    "menu": "menu",
    "menubar": "menubar",
    "progressbar": "progressbar",
    "slider": "slider",
    "spinbutton": "spinbutton",
    "status": "status",
    "timer": "timer",
    "tooltip": "tooltip",
}


def _parse_a11y_target(target: str) -> tuple[str, str, str | None]:
    """Parse ``role="name"`` format, returning ``(role, name, node_id)``.

    Falls back to ``("", target, None)`` for plain-text targets.
    """
    stripped = target.strip()

    # Check for id suffix: role="name" id=e123
    node_id = None
    id_match = _A11Y_ID_SUFFIX_RE.search(stripped)
    if id_match:
        node_id = id_match.group(1)
        stripped = stripped[:id_match.start()].strip()

    m = _A11Y_ROLE_TARGET_RE.match(stripped)
    if m:
        return m.group(1).lower(), m.group(2), node_id

    # Not in role="name" format — treat as plain text
    return "", target.strip(), node_id


# ── Candidate builders ───────────────────────────────────────────────────────


def _build_a11y_candidates(
    page, role: str, name: str, *, prefer_input: bool,
) -> list[tuple[str, object]]:
    """Build locator candidates from parsed a11y role+name."""
    builders: list[tuple[str, object]] = []

    pw_role = _A11Y_TO_PLAYWRIGHT_ROLE.get(role)
    if pw_role and name:
        # Exact match (highest priority for a11y)
        builders.append((
            "a11y_role_exact",
            lambda r=pw_role, n=name: page.get_by_role(r, name=n, exact=True),
        ))
        # Fuzzy match (fallback)
        builders.append((
            "a11y_role_fuzzy",
            lambda r=pw_role, n=name: page.get_by_role(r, name=n, exact=False),
        ))

    # For input roles, also try placeholder/label
    if prefer_input and name:
        builders.append(("a11y_placeholder", lambda n=name: page.get_by_placeholder(n)))
        builders.append(("a11y_label", lambda n=name: page.get_by_label(n)))

    # Plain text fallback
    if name:
        builders.append(("a11y_text_exact", lambda n=name: page.get_by_text(n, exact=True)))
        builders.append(("a11y_text_fuzzy", lambda n=name: page.get_by_text(n, exact=False)))

    return builders


def _build_candidate_builders(
    page, target: str, *, prefer_input: bool,
) -> list[tuple[str, object]]:
    """Build ordered list of ``(strategy, locator_builder)`` for *target*.

    Primary path: parse ``role="name"`` a11y format and use Playwright's
    ``get_by_role``.  Fallback: explicit CSS/XPath selectors (for manual
    corrections).
    """
    # 1. Explicit CSS/XPath selectors (for correction store / manual overrides)
    explicit = _resolve_explicit_locator(page, target)
    if explicit is not None:
        return [explicit]

    # 2. Parse a11y role="name" format
    role, name, node_id = _parse_a11y_target(target)

    # If we have a node_id, try CSS attribute selector first (fastest)
    if node_id:
        return [("a11y_node_id", lambda: page.locator(f"[data-a11y-node-id='{node_id}']"))]

    # 3. Build a11y-based candidates
    if role:
        return _build_a11y_candidates(page, role, name, prefer_input=prefer_input)

    # 4. Plain text target (no role prefix) — try as-is
    if name:
        builders: list[tuple[str, object]] = []
        if prefer_input:
            builders.extend([
                ("a11y_placeholder", lambda n=name: page.get_by_placeholder(n)),
                ("a11y_label", lambda n=name: page.get_by_label(n)),
            ])
        builders.extend([
            ("a11y_text_exact", lambda n=name: page.get_by_text(n, exact=True)),
            ("a11y_text_fuzzy", lambda n=name: page.get_by_text(n)),
        ])
        return builders

    return []


# ── Explicit locator resolution (CSS/XPath only) ─────────────────────────────


def _resolve_explicit_locator(page, target: str) -> tuple[str, object] | None:
    """Resolve explicit CSS/XPath/data-testid selectors.

    These are used for manual corrections and legacy targets.
    """
    if target.startswith("css="):
        return ("css", lambda: page.locator(target))
    if target.startswith("xpath="):
        return ("xpath", lambda: page.locator(target))
    if target.startswith("//"):
        return ("xpath", lambda: page.locator(f"xpath={target}"))
    if target.startswith(("#", ".")):
        return ("css", lambda: page.locator(target))
    if target.startswith("data-testid="):
        value = target.split("=", 1)[1]
        return ("data-testid", lambda: page.get_by_test_id(value))
    return None


# ── Candidate collection and scoring ─────────────────────────────────────────


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


# ── Entry point ──────────────────────────────────────────────────────────────


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


# ── Candidate evidence and scoring ───────────────────────────────────────────


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


# ── Scoring tables ───────────────────────────────────────────────────────────


_STRATEGY_SCORES: dict[str, int] = {
    "a11y_node_id": 130,       # Direct node ID match
    "a11y_role_exact": 120,    # Exact role+name from a11y tree
    "css": 110,                # Explicit CSS selector
    "xpath": 110,              # Explicit XPath selector
    "data-testid": 105,        # data-testid attribute
    "a11y_role_fuzzy": 90,     # Fuzzy role+name match
    "a11y_label": 85,          # aria-label / label element
    "a11y_text_exact": 80,     # Exact text match
    "a11y_placeholder": 75,    # Placeholder attribute
    "a11y_text_fuzzy": 60,     # Fuzzy text match
}


def _strategy_base_score(strategy: str) -> int:
    return _STRATEGY_SCORES.get(strategy, 50)


def _strategy_rule_name(strategy: str) -> str:
    return {
        "a11y_node_id": "a11y-node-id-match",
        "a11y_role_exact": "a11y-role-exact-match",
        "a11y_role_fuzzy": "a11y-role-fuzzy-match",
        "a11y_text_exact": "a11y-text-exact-match",
        "a11y_text_fuzzy": "a11y-text-fuzzy-match",
        "a11y_placeholder": "a11y-placeholder-match",
        "a11y_label": "a11y-label-match",
        "css": "explicit-css-selector",
        "xpath": "explicit-xpath-selector",
        "data-testid": "explicit-data-testid",
    }.get(strategy, strategy)


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


# ── Strategy-based resolution (for explicit strategies) ──────────────────────


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
