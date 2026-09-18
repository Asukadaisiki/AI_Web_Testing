"""Build bounded, content-addressed browser observations."""

from __future__ import annotations

import gzip
import hashlib
import json
import re
from pathlib import Path
from typing import Any

from playwright.sync_api import Error as PlaywrightError

from browser_worker.contracts.browser_observation import (
    A11yFact,
    BrowserObservation,
    ContextPath,
    DOMFact,
    ElementFact,
    LocatorSpec,
    ObservationArtifact,
    ObservationRelation,
    ObservedLocator,
    PageStateFact,
    ResolvedTargetEvidence,
    RuntimeFact,
    canonical_sha256,
    validate_locator_spec,
    validate_semantic_locator_spec,
)
from browser_worker.locators.compiler import compile_locator

MAX_ELEMENTS = 120
MAX_DOM_TEXT_CHARS = 256
MAX_RAW_SHARD_BYTES = 512 << 10
_INTERACTIVE_ROLES = {
    "button",
    "checkbox",
    "combobox",
    "link",
    "listbox",
    "menuitem",
    "option",
    "radio",
    "searchbox",
    "slider",
    "spinbutton",
    "switch",
    "tab",
    "textbox",
    "treeitem",
}
_RELATION_PROPERTIES = {
    "activedescendant",
    "controls",
    "describedby",
    "details",
    "errormessage",
    "flowto",
    "labelledby",
    "owns",
}


def build_browser_observation(
    *,
    url: str,
    title: str,
    state_id: str,
    revision: int,
    nodes: list[dict[str, Any]],
    probe_id: str | None = None,
    page=None,
    artifact_root: Path | None = None,
    previous_state_sha256: str | None = None,
    requested_locators: list[dict[str, Any]] | None = None,
) -> dict[str, Any]:
    effective_probe_id = probe_id or "probe_" + canonical_sha256(
        {
            "url": url,
            "title": title,
            "state_id": state_id,
            "revision": max(1, revision),
        }
    )[:24]
    elements = [
        _element_fact(
            node,
            state_id,
            effective_probe_id,
            requested_locators=requested_locators,
        )
        for node in _bounded_nodes(nodes)
    ]
    _attach_runtime_requested_locators(
        elements,
        page=page,
        probe_id=effective_probe_id,
        requested_locators=requested_locators,
    )
    _set_observed_counts(elements, page=page)
    relations = _relations(elements)
    state_payload = {
        "url": url,
        "title": title,
        "elements": [
            {
                "element_ref": element.element_ref,
                "a11y": element.a11y.model_dump(mode="json") if element.a11y else None,
                "dom": element.dom.model_dump(mode="json") if element.dom else None,
                "runtime": element.runtime.model_dump(mode="json"),
            }
            for element in elements
        ],
        "relations": [relation.model_dump(mode="json") for relation in relations],
    }
    state_sha256 = canonical_sha256(state_payload)
    observation_id = f"obs_{state_sha256[:24]}"
    observation = BrowserObservation(
        probe_id=effective_probe_id,
        observation_id=observation_id,
        page_state=PageStateFact(
            state_id=state_id,
            revision=max(1, revision),
            url=url,
            title=title,
            state_sha256=state_sha256,
            previous_state_sha256=previous_state_sha256,
        ),
        elements=elements,
        relations=relations,
    )
    if artifact_root is not None:
        artifact = _write_artifact(
            artifact_root,
            observation,
            raw_nodes=nodes,
        )
        observation = observation.model_copy(update={"artifact": artifact})
    return observation.model_dump(mode="json")


def attach_observation_artifact(
    value: dict[str, Any],
    artifact_root: Path,
) -> dict[str, Any]:
    observation = BrowserObservation.model_validate(value)
    artifact = _write_artifact(artifact_root, observation)
    return observation.model_copy(update={"artifact": artifact}).model_dump(
        mode="json"
    )


def _bounded_nodes(nodes: list[dict[str, Any]]) -> list[dict[str, Any]]:
    ranked = sorted(
        (node for node in nodes if isinstance(node, dict)),
        key=lambda node: (
            0
            if node.get("verified_selectors")
            or str(node.get("role") or "").lower() in _INTERACTIVE_ROLES
            else 1,
            str(node.get("node_id") or ""),
        ),
    )
    return ranked[:MAX_ELEMENTS]


def _element_fact(
    node: dict[str, Any],
    state_id: str,
    probe_id: str,
    *,
    requested_locators: list[dict[str, Any]] | None = None,
) -> ElementFact:
    dom = node.get("dom") if isinstance(node.get("dom"), dict) else {}
    attrs = {
        str(key): str(value)
        for key, value in (dom.get("attrs") or {}).items()
        if value is not None
    }
    a11y_name = str(
        node.get("a11y_name")
        or node.get("original_name")
        or node.get("name")
        or ""
    )
    dom_text = str(node.get("dom_text") or dom.get("textContent") or "")
    dom_text = re.sub(r"\s+", " ", dom_text).strip()[:MAX_DOM_TEXT_CHARS]
    backend_id = node.get("backend_dom_node_id")
    identity = str(backend_id or node.get("node_id") or canonical_sha256(node)[:16])
    element_ref = f"{state_id}:{identity}"
    context_path = ContextPath.model_validate(
        node.get("context_path")
        or {"frames": [], "shadow_hosts": []}
    )
    role = str(node.get("role") or "")
    states = dict(node.get("a11y_states") or {})
    states.setdefault("focusable", bool(node.get("focusable", False)))
    states.setdefault("disabled", bool(node.get("disabled", False)))
    relation_values = {
        str(key): value
        for key, value in (node.get("a11y_relations") or {}).items()
        if key in _RELATION_PROPERTIES
    }
    input_type = attrs.get("type", "").lower()
    editable = (
        role in {"textbox", "searchbox", "combobox", "spinbutton"}
        or str(dom.get("tag") or "").lower() in {"input", "select", "textarea"}
    ) and input_type not in {"button", "checkbox", "file", "hidden", "radio", "reset", "submit"}
    if input_type == "password":
        dom_text = ""
    element = ElementFact(
        element_ref=element_ref,
        context_path=context_path,
        a11y=A11yFact(
            role=role,
            name=a11y_name[:256],
            description=_optional_text(node.get("a11y_description"), 256),
            value=None if input_type == "password" else node.get("a11y_value"),
            states=states,
            relations=relation_values,
        ),
        dom=DOMFact(
            backend_node_id=int(backend_id) if backend_id else None,
            tag=str(dom.get("tag") or ""),
            attrs=attrs,
            text=dom_text,
        ),
        runtime=RuntimeFact(
            connected=bool(dom.get("connected", True)),
            visible=bool(dom.get("visible", True)),
            enabled=bool(dom.get("enabled", not node.get("disabled", False))),
            editable=editable,
        ),
        locators=_locator_hints(
            node,
            role,
            a11y_name,
            probe_id=probe_id,
            element_ref=element_ref,
            context_path=context_path,
        ),
    )
    for raw_locator in requested_locators or []:
        try:
            locator = validate_semantic_locator_spec(raw_locator)
        except ValueError:
            continue
        if not _element_matches_locator(element, locator):
            continue
        element.locators.append(
            _observed_locator(
                element_ref=element.element_ref,
                probe_id=probe_id,
                context_path=element.context_path,
                locator=locator.model_dump(mode="json"),
                provenance="grounding_query",
                observed_count=0,
            )
        )
    deduplicated: dict[str, ObservedLocator] = {}
    for locator in element.locators:
        key = canonical_sha256(locator.locator.model_dump(mode="json"))
        deduplicated[key] = locator
    element.locators = list(deduplicated.values())
    return element


_ICON_GLYPH_PATTERN = re.compile(
    "[\ue000-\uf8ff\U000f0000-\U000ffffd\U00100000-\U0010fffd]+"
)


def _normalized_accessible_name(value: str | None) -> str:
    """Collapse whitespace and drop icon-font glyphs from an accessible name.

    Chromium folds CSS-generated icon glyphs into the accessible name, so a link
    rendered as ``<i class="fa fa-plus-square"></i>View Product`` is announced as
    ``"\\uf0fe View Product"`` while the readable summary drops the glyph.
    """
    if not value:
        return ""
    return " ".join(_ICON_GLYPH_PATTERN.sub(" ", value).split()).casefold()


def _locator_equivalent(left: LocatorSpec, right: LocatorSpec) -> bool:
    """Whether two locator specs address the same element.

    ``exact`` selects a matching strategy rather than an element, and an icon
    glyph contaminates only how a name is announced, so neither may decide
    element identity. The evidence linker used to require byte-identical
    locators, which stranded every element whose announced name carries an icon.
    """
    if left.kind != right.kind:
        return False
    if left.kind == "role":
        if str(getattr(left, "role", "")).casefold() != str(
            getattr(right, "role", "")
        ).casefold():
            return False
        return _normalized_accessible_name(
            getattr(left, "name", None)
        ) == _normalized_accessible_name(getattr(right, "name", None))
    if left.kind == "scoped":
        left_scope = getattr(left, "scope", None)
        right_scope = getattr(right, "scope", None)
        left_target = getattr(left, "target", None)
        right_target = getattr(right, "target", None)
        if None in (left_scope, right_scope, left_target, right_target):
            return False
        return _locator_equivalent(
            left_scope, right_scope
        ) and _locator_equivalent(left_target, right_target)
    return str(getattr(left, "value", "")).strip().casefold() == str(
        getattr(right, "value", "")
    ).strip().casefold()


def build_resolved_target_evidence(
    observation_value: dict[str, Any],
    *,
    plan_step_id: str,
    step_index: int,
    action_index: int,
    action: str,
    locator_value: dict[str, Any],
    action_status: str = "resolved",
) -> dict[str, Any] | None:
    observation = BrowserObservation.model_validate(observation_value)
    if not observation.probe_id:
        return None
    locator = validate_semantic_locator_spec(locator_value)
    matched: list[tuple[ElementFact, ObservedLocator]] = []
    for element in observation.elements:
        for candidate in element.locators:
            if candidate.observed_count == 1 and _locator_equivalent(
                candidate.locator, locator
            ):
                matched.append((element, candidate))
    if len(matched) != 1:
        return None
    element, candidate = matched[0]
    if not element.runtime.visible:
        return None
    if action in {"click", "input"} and not element.runtime.enabled:
        return None
    if action == "input" and not element.runtime.editable:
        return None
    return ResolvedTargetEvidence(
        probe_id=observation.probe_id,
        plan_step_id=plan_step_id,
        step_index=step_index,
        action_index=action_index,
        action=action,
        observation_id=observation.observation_id,
        page_state_id=observation.page_state.state_id,
        page_state_sha256=observation.page_state.state_sha256,
        element_ref=element.element_ref,
        candidate_id=candidate.candidate_id or "",
        locator=candidate.locator,
        context_path=element.context_path,
        provenance=candidate.provenance,
        runtime_match_count=1,
        visible=True,
        enabled=element.runtime.enabled,
        editable=element.runtime.editable,
        score=_locator_score(candidate.locator.kind),
        action_status=action_status,
    ).model_dump(mode="json")


def _element_matches_locator(element: ElementFact, locator: LocatorSpec) -> bool:
    if locator.kind == "scoped":
        return False
    a11y = element.a11y
    dom = element.dom
    if locator.kind == "role":
        if a11y is None or a11y.role.casefold() != locator.role.casefold():
            return False
        if locator.name is None:
            return True
        if locator.exact:
            return a11y.name == locator.name
        return locator.name.casefold() in a11y.name.casefold()
    if dom is None:
        return False
    if locator.kind == "placeholder":
        return _value_matches(
            dom.attrs.get("placeholder", ""),
            locator.value,
            locator.exact,
        )
    if locator.kind == "label":
        return _value_matches(
            dom.attrs.get("aria-label", ""),
            locator.value,
            locator.exact,
        )
    if locator.kind == "test_id":
        return dom.attrs.get("data-testid", "") == locator.value
    if locator.kind == "text":
        values = [dom.text, a11y.name if a11y else ""]
        return any(
            _value_matches(value, locator.value, locator.exact)
            for value in values
        )
    return False


def _attach_runtime_requested_locators(
    elements: list[ElementFact],
    *,
    page,
    probe_id: str,
    requested_locators: list[dict[str, Any]] | None,
) -> None:
    if page is None:
        return
    for raw_locator in requested_locators or []:
        try:
            locator = validate_semantic_locator_spec(raw_locator)
            compiled = compile_locator(page, locator)
            if compiled.count() != 1:
                continue
            fingerprint = compiled.first.evaluate(
                """
                element => ({
                  tag: element.tagName.toLowerCase(),
                  text: (element.innerText || element.textContent || "")
                    .replace(/\\s+/g, " ").trim().slice(0, 256),
                  role: element.getAttribute("role") || "",
                  attrs: {
                    id: element.getAttribute("id") || "",
                    name: element.getAttribute("name") || "",
                    href: element.getAttribute("href") || "",
                    placeholder: element.getAttribute("placeholder") || "",
                    aria_label: element.getAttribute("aria-label") || "",
                    data_testid: element.getAttribute("data-testid") || ""
                  }
                })
                """
            )
        except Exception:
            continue
        matched = [
            element
            for element in elements
            if _element_matches_fingerprint(element, fingerprint)
        ]
        if len(matched) != 1:
            continue
        element = matched[0]
        element.locators.append(
            _observed_locator(
                element_ref=element.element_ref,
                probe_id=probe_id,
                context_path=element.context_path,
                locator=locator.model_dump(mode="json"),
                provenance="grounding_query_runtime",
                observed_count=1,
            )
        )
        deduplicated: dict[str, ObservedLocator] = {}
        for candidate in element.locators:
            key = canonical_sha256(candidate.locator.model_dump(mode="json"))
            deduplicated[key] = candidate
        element.locators = list(deduplicated.values())


def _element_matches_fingerprint(
    element: ElementFact,
    fingerprint: object,
) -> bool:
    if not isinstance(fingerprint, dict) or element.dom is None:
        return False
    if element.dom.tag.casefold() != str(
        fingerprint.get("tag") or ""
    ).casefold():
        return False
    raw_attrs = fingerprint.get("attrs")
    attrs = raw_attrs if isinstance(raw_attrs, dict) else {}
    pairs = (
        ("id", "id"),
        ("name", "name"),
        ("href", "href"),
        ("placeholder", "placeholder"),
        ("aria_label", "aria-label"),
        ("data_testid", "data-testid"),
    )
    stable = [
        (dom_key, str(attrs.get(js_key) or ""))
        for js_key, dom_key in pairs
        if str(attrs.get(js_key) or "")
    ]
    if stable:
        return all(
            element.dom.attrs.get(key, "") == value
            for key, value in stable
        )
    text = str(fingerprint.get("text") or "")
    role = str(fingerprint.get("role") or "")
    return bool(
        text
        and element.dom.text == text
        and (
            not role
            or (
                element.a11y is not None
                and element.a11y.role.casefold() == role.casefold()
            )
        )
    )


def _value_matches(actual: str, expected: str, exact: bool) -> bool:
    if exact:
        return actual == expected
    return expected.casefold() in actual.casefold()


def _locator_score(kind: str) -> float:
    return {
        "role": 0.95,
        "label": 0.93,
        "test_id": 0.92,
        "placeholder": 0.9,
        "text": 0.85,
        "scoped": 0.97,
    }.get(kind, 0.5)


def _relations(elements: list[ElementFact]) -> list[ObservationRelation]:
    refs = {element.element_ref for element in elements}
    by_suffix = {element.element_ref.rsplit(":", 1)[-1]: element.element_ref for element in elements}
    result: list[ObservationRelation] = []
    for element in elements:
        if element.a11y is None:
            continue
        for kind, raw_targets in element.a11y.relations.items():
            targets = raw_targets if isinstance(raw_targets, list) else [raw_targets]
            for target in targets:
                if target is None:
                    continue
                target_ref = by_suffix.get(str(target), str(target))
                if target_ref not in refs:
                    continue
                result.append(
                    ObservationRelation(
                        kind=kind,
                        source=element.element_ref,
                        target=target_ref,
                    )
                )
    return result


def _locator_hints(
    node: dict[str, Any],
    role: str,
    accessible_name: str,
    *,
    probe_id: str,
    element_ref: str,
    context_path: ContextPath,
) -> list[ObservedLocator]:
    result: list[ObservedLocator] = []
    if role and accessible_name:
        result.append(
            _observed_locator(
                element_ref=element_ref,
                probe_id=probe_id,
                context_path=context_path,
                locator={
                    "kind": "role",
                    "role": role,
                    "name": accessible_name[:256],
                    "exact": True,
                },
                provenance="a11y_exact",
                observed_count=0,
            )
        )
    elif role and role.lower() in _INTERACTIVE_ROLES:
        # An interactive control can be missing an accessible name while still
        # being addressable by role alone; the quantity spinbutton on a product
        # page is the canonical case. Without this candidate the element only
        # carries css/test_id hints, which the semantic grounding gate rejects,
        # so the step could not be grounded at all and callers were pushed into
        # rewriting the plan. Whether the unnamed role is unique is decided by
        # the probed observed count, exactly as for a named role candidate.
        result.append(
            _observed_locator(
                element_ref=element_ref,
                probe_id=probe_id,
                context_path=context_path,
                locator={"kind": "role", "role": role, "exact": True},
                provenance="a11y_role_only",
                observed_count=0,
            )
        )
    for raw in node.get("verified_selectors") or []:
        if not isinstance(raw, dict):
            continue
        selector = str(raw.get("selector") or "").strip()
        if not selector:
            continue
        kind = "test_id" if raw.get("strategy") == "data-testid" else "css"
        result.append(
            _observed_locator(
                element_ref=element_ref,
                probe_id=probe_id,
                context_path=context_path,
                locator={"kind": kind, "value": selector, "exact": True},
                provenance=str(raw.get("source") or "dom_verified"),
                observed_count=1,
            )
        )
    deduplicated: dict[str, ObservedLocator] = {}
    for locator in result:
        key = canonical_sha256(locator.locator.model_dump(mode="json"))
        deduplicated[key] = locator
    return list(deduplicated.values())


def _observed_locator(
    *,
    probe_id: str,
    element_ref: str,
    context_path: ContextPath,
    locator: dict[str, Any],
    provenance: str,
    observed_count: int,
) -> ObservedLocator:
    validated = validate_locator_spec(locator)
    candidate_id = "candidate_" + canonical_sha256(
        {
            "probe_id": probe_id,
            "element_ref": element_ref,
            "context_path": context_path.model_dump(mode="json"),
            "locator": validated.model_dump(mode="json"),
        }
    )[:16]
    return ObservedLocator(
        candidate_id=candidate_id,
        locator=validated,
        provenance=provenance,
        observed_count=observed_count,
    )


def _relax_unmatched_role_name(page, observed, context_path) -> None:
    """Fall back to a substring role name when the exact name matches nothing.

    Chromium folds CSS-generated icon glyphs into the accessible name, so a link
    rendered as ``<i class="fa fa-plus-square"></i>View Product`` is announced as
    ``"\\uf0fe View Product"``. The recorded name is the readable text, so an
    exact match resolves zero times even though the element is plainly there
    (measured on the Automation Exercise results page: exact=True -> 0 matches,
    exact=False -> 1 match). Publishing a locator that can never match sends
    callers into blind retry loops, so adopt the substring form whenever that is
    the one that actually resolves; its observed count still reports ambiguity.
    """
    locator = observed.locator
    if locator.kind != "role" or locator.name is None or not locator.exact:
        return
    relaxed = locator.model_copy(update={"exact": False})
    try:
        count = compile_locator(page, relaxed, context_path=context_path).count()
    except (AttributeError, PlaywrightError, TypeError, ValueError):
        return
    if count < 1:
        return
    observed.locator = relaxed
    observed.observed_count = count


def _set_observed_counts(elements: list[ElementFact], *, page=None) -> None:
    counts: dict[str, int] = {}
    for element in elements:
        for observed in element.locators:
            key = canonical_sha256(observed.locator.model_dump(mode="json"))
            counts[key] = counts.get(key, 0) + 1
    for element in elements:
        for observed in element.locators:
            if page is not None:
                try:
                    observed.observed_count = compile_locator(
                        page,
                        observed.locator,
                        context_path=element.context_path,
                    ).count()
                except (AttributeError, PlaywrightError, TypeError, ValueError):
                    observed.observed_count = 0
                    continue
                if observed.observed_count == 0:
                    _relax_unmatched_role_name(
                        page, observed, element.context_path
                    )
                continue
            if observed.observed_count == 1 and observed.locator.kind in {
                "css",
                "test_id",
            }:
                continue
            key = canonical_sha256(observed.locator.model_dump(mode="json"))
            observed.observed_count = counts[key]


def _write_artifact(
    artifact_root: Path,
    observation: BrowserObservation,
    *,
    raw_nodes: list[dict[str, Any]] | None = None,
) -> ObservationArtifact:
    observation_payload = observation.model_dump(mode="json", exclude={"artifact"})
    payload: dict[str, Any] = {
        "schema_version": "browser.observation-artifact.v1",
        "observation": observation_payload,
    }
    raw_content_bytes = 0
    if raw_nodes is not None:
        raw_encoded = _canonical_json(raw_nodes)
        raw_content_bytes = len(raw_encoded)
        inline_payload = {**payload, "raw_nodes": raw_nodes}
        if len(_canonical_json(inline_payload)) <= MAX_RAW_SHARD_BYTES:
            payload["raw_nodes"] = raw_nodes
        else:
            payload["raw_node_shards"] = _write_raw_node_shards(
                artifact_root,
                raw_nodes,
            )
    encoded = _canonical_json(payload)
    digest = _write_gzip_payload(artifact_root, encoded)
    return ObservationArtifact(
        uri=f"artifact://browser-observation/{digest}",
        sha256=digest,
        content_bytes=max(len(encoded), raw_content_bytes),
    )


def _write_raw_node_shards(
    artifact_root: Path,
    raw_nodes: list[dict[str, Any]],
) -> list[dict[str, Any]]:
    chunks: list[list[dict[str, Any]]] = []
    current: list[dict[str, Any]] = []
    current_bytes = 2
    for node in raw_nodes:
        encoded = _canonical_json(node)
        if current and current_bytes + len(encoded) + 1 > MAX_RAW_SHARD_BYTES:
            chunks.append(current)
            current = []
            current_bytes = 2
        current.append(node)
        current_bytes += len(encoded) + 1
    if current:
        chunks.append(current)

    refs: list[dict[str, Any]] = []
    for index, chunk in enumerate(chunks):
        payload = _canonical_json(
            {
                "schema_version": "browser.observation-nodes.v1",
                "index": index,
                "nodes": chunk,
            }
        )
        digest = _write_gzip_payload(artifact_root, payload)
        refs.append(
            {
                "uri": f"artifact://browser-observation/{digest}",
                "sha256": digest,
                "content_bytes": len(payload),
            }
        )
    return refs


def _canonical_json(value: object) -> bytes:
    return json.dumps(
        value,
        ensure_ascii=False,
        separators=(",", ":"),
        sort_keys=True,
    ).encode("utf-8")


def _write_gzip_payload(artifact_root: Path, encoded: bytes) -> str:
    digest = hashlib.sha256(encoded).hexdigest()
    directory = artifact_root / "browser-observations"
    directory.mkdir(parents=True, exist_ok=True)
    path = directory / f"{digest}.json.gz"
    if not path.exists():
        with gzip.open(path, "wb", compresslevel=6) as stream:
            stream.write(encoded)
    return digest


def _optional_text(value: object, limit: int) -> str | None:
    text = str(value or "").strip()
    return text[:limit] if text else None
