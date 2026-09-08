"""Declarative acceptance contracts for agentic E2E results."""

from __future__ import annotations

import hashlib
import json
import re
from copy import deepcopy
from html.parser import HTMLParser
from pathlib import Path
from typing import Any

ACCEPTANCE_SPEC_VERSION = "agentic-e2e.acceptance.v1"
ORACLE_RESULT_VERSION = "agentic-e2e.oracle.v1"
_VOID_ELEMENTS = {
    "area",
    "base",
    "br",
    "col",
    "embed",
    "hr",
    "img",
    "input",
    "link",
    "meta",
    "param",
    "source",
    "track",
    "wbr",
}
_IGNORED_TEXT_ELEMENTS = {"script", "style", "noscript", "template"}


class _Element:
    def __init__(self, tag: str, attrs: list[tuple[str, str | None]]) -> None:
        self.tag = tag
        self.attrs = {name: value or "" for name, value in attrs}
        self.text_parts: list[str] = []

    def text(self) -> str:
        return " ".join(" ".join(self.text_parts).split())


class _DocumentParser(HTMLParser):
    def __init__(self) -> None:
        super().__init__(convert_charrefs=True)
        self.elements: list[_Element] = []
        self._stack: list[_Element] = []
        self._ignored_depth = 0
        self._hidden_depth = 0

    def handle_starttag(
        self,
        tag: str,
        attrs: list[tuple[str, str | None]],
    ) -> None:
        element = _Element(tag, attrs)
        self.elements.append(element)
        self._stack.append(element)
        if tag in _IGNORED_TEXT_ELEMENTS:
            self._ignored_depth += 1
        if _is_hidden_element(element):
            self._hidden_depth += 1
        if tag in _VOID_ELEMENTS:
            if _is_hidden_element(element) and self._hidden_depth > 0:
                self._hidden_depth -= 1
            self._stack.pop()

    def handle_startendtag(
        self,
        tag: str,
        attrs: list[tuple[str, str | None]],
    ) -> None:
        self.elements.append(_Element(tag, attrs))

    def handle_endtag(self, tag: str) -> None:
        if tag in _IGNORED_TEXT_ELEMENTS and self._ignored_depth > 0:
            self._ignored_depth -= 1
        for element in reversed(self._stack):
            if element.tag == tag:
                if _is_hidden_element(element) and self._hidden_depth > 0:
                    self._hidden_depth -= 1
                break
        for index in range(len(self._stack) - 1, -1, -1):
            if self._stack[index].tag == tag:
                del self._stack[index:]
                return

    def handle_data(self, data: str) -> None:
        if self._ignored_depth > 0 or self._hidden_depth > 0:
            return
        normalized = " ".join(data.split())
        if not normalized:
            return
        for element in self._stack:
            element.text_parts.append(normalized)


def _is_hidden_element(element: _Element) -> bool:
    if "hidden" in element.attrs:
        return True
    if element.attrs.get("aria-hidden", "").casefold() == "true":
        return True
    style = element.attrs.get("style", "").replace(" ", "").casefold()
    if "display:none" in style or "visibility:hidden" in style:
        return True
    classes = set(element.attrs.get("class", "").split())
    return "modal" in classes and "show" not in classes


def load_acceptance_spec(path: Path) -> dict[str, Any]:
    return validate_acceptance_spec(json.loads(path.read_text(encoding="utf-8")))


def validate_acceptance_spec(source: dict[str, Any]) -> dict[str, Any]:
    payload = deepcopy(source)
    if not isinstance(payload, dict):
        raise ValueError("acceptance spec must be an object")
    _require_exact_keys(
        payload,
        {"schema_version", "id", "goal", "oracle"},
        "acceptance spec",
    )
    if payload.get("schema_version") != ACCEPTANCE_SPEC_VERSION:
        raise ValueError(f"schema_version must be {ACCEPTANCE_SPEC_VERSION}")
    for field in ("id", "goal"):
        if not isinstance(payload.get(field), str) or not payload[field].strip():
            raise ValueError(f"{field} must be a non-empty string")
        payload[field] = payload[field].strip()
    oracle = payload.get("oracle")
    if not isinstance(oracle, dict):
        raise ValueError("oracle must be an object")
    _require_exact_keys(oracle, {"final_url", "elements"}, "oracle")
    final_url = oracle.get("final_url")
    elements = oracle.get("elements")
    if final_url is None and not elements:
        raise ValueError("oracle requires final_url or element assertions")
    if final_url is not None:
        _validate_final_url(final_url)
    if elements is None:
        oracle["elements"] = []
    elif not isinstance(elements, list):
        raise ValueError("oracle.elements must be an array")
    else:
        seen_ids: set[str] = set()
        for index, assertion in enumerate(elements):
            _validate_element_assertion(assertion, index, seen_ids)
    return payload


def acceptance_sha256(spec: dict[str, Any]) -> str:
    canonical = json.dumps(
        spec,
        ensure_ascii=False,
        separators=(",", ":"),
        sort_keys=True,
    ).encode("utf-8")
    return hashlib.sha256(canonical).hexdigest()


def evaluate_acceptance(
    spec: dict[str, Any],
    *,
    html: str,
    actual_url: str,
) -> dict[str, Any]:
    validated = validate_acceptance_spec(spec)
    parser = _DocumentParser()
    parser.feed(html)
    parser.close()
    checks: dict[str, dict[str, Any]] = {}

    final_url = validated["oracle"].get("final_url")
    if final_url is not None:
        passed = True
        if "equals" in final_url:
            passed = passed and actual_url == final_url["equals"]
        if "contains" in final_url:
            passed = passed and final_url["contains"] in actual_url
        checks["final_url"] = {
            "passed": passed,
            "expected": final_url,
            "actual": actual_url,
        }

    for assertion in validated["oracle"]["elements"]:
        matches = [
            element
            for element in parser.elements
            if _matches_selector(element, assertion["selector"])
        ]
        texts = [element.text() for element in matches]
        count_passed = _matches_count(len(matches), assertion.get("match_count"))
        text_passed = all(
            _matches_text(text, assertion.get("text"))
            for text in texts
        )
        if assertion.get("text") and not texts:
            text_passed = False
        checks[assertion["id"]] = {
            "passed": count_passed and text_passed,
            "expected": {
                "selector": assertion["selector"],
                "match_count": assertion.get("match_count"),
                "text": assertion.get("text"),
            },
            "actual": {
                "match_count": len(matches),
                "texts": [text[:1000] for text in texts[:10]],
            },
        }

    return {
        "schema_version": ORACLE_RESULT_VERSION,
        "acceptance_id": validated["id"],
        "acceptance_sha256": acceptance_sha256(validated),
        "passed": bool(checks) and all(check["passed"] for check in checks.values()),
        "checks": checks,
    }


def _require_exact_keys(value: dict[str, Any], allowed: set[str], label: str) -> None:
    unknown = set(value) - allowed
    if unknown:
        raise ValueError(f"{label} has unsupported fields: {sorted(unknown)}")


def _validate_final_url(value: Any) -> None:
    if not isinstance(value, dict):
        raise ValueError("oracle.final_url must be an object")
    _require_exact_keys(value, {"equals", "contains"}, "oracle.final_url")
    if not value:
        raise ValueError("oracle.final_url must define equals or contains")
    for field, expected in value.items():
        if not isinstance(expected, str) or not expected:
            raise ValueError(f"oracle.final_url.{field} must be non-empty")


def _validate_element_assertion(
    value: Any,
    index: int,
    seen_ids: set[str],
) -> None:
    if not isinstance(value, dict):
        raise ValueError(f"oracle.elements[{index}] must be an object")
    _require_exact_keys(
        value,
        {"id", "selector", "match_count", "text"},
        f"oracle.elements[{index}]",
    )
    assertion_id = value.get("id")
    if not isinstance(assertion_id, str) or not assertion_id.strip():
        raise ValueError(f"oracle.elements[{index}].id must be non-empty")
    if assertion_id in seen_ids:
        raise ValueError(f"duplicate oracle assertion id: {assertion_id}")
    seen_ids.add(assertion_id)
    selector = value.get("selector")
    if not isinstance(selector, dict) or not selector:
        raise ValueError(f"oracle.elements[{index}].selector must be an object")
    _require_exact_keys(
        selector,
        {"tag", "id", "classes", "attributes"},
        f"oracle.elements[{index}].selector",
    )
    for field in ("tag", "id"):
        if field in selector and (
            not isinstance(selector[field], str) or not selector[field]
        ):
            raise ValueError(
                f"oracle.elements[{index}].selector.{field} must be non-empty"
            )
    classes = selector.get("classes", [])
    if not isinstance(classes, list) or any(
        not isinstance(item, str) or not item for item in classes
    ):
        raise ValueError(
            f"oracle.elements[{index}].selector.classes must be strings"
        )
    attributes = selector.get("attributes", {})
    if not isinstance(attributes, dict) or any(
        not isinstance(name, str)
        or not name
        or not isinstance(expected, str)
        for name, expected in attributes.items()
    ):
        raise ValueError(
            f"oracle.elements[{index}].selector.attributes must be strings"
        )
    if not (
        selector.get("tag")
        or selector.get("id")
        or classes
        or attributes
    ):
        raise ValueError(
            f"oracle.elements[{index}].selector requires an effective condition"
        )
    if "match_count" in value:
        _validate_match_count(value["match_count"], index)
    if "text" in value:
        _validate_text_contract(value["text"], index)
    if "match_count" not in value and "text" not in value:
        raise ValueError(
            f"oracle.elements[{index}] requires match_count or text"
        )


def _validate_match_count(value: Any, index: int) -> None:
    if not isinstance(value, dict) or not value:
        raise ValueError(
            f"oracle.elements[{index}].match_count must be an object"
        )
    _require_exact_keys(
        value,
        {"equals", "min", "max"},
        f"oracle.elements[{index}].match_count",
    )
    for field, expected in value.items():
        if not isinstance(expected, int) or isinstance(expected, bool) or expected < 0:
            raise ValueError(
                f"oracle.elements[{index}].match_count.{field} must be non-negative"
            )


def _validate_text_contract(value: Any, index: int) -> None:
    if not isinstance(value, dict) or not value:
        raise ValueError(f"oracle.elements[{index}].text must be an object")
    _require_exact_keys(
        value,
        {"contains", "not_contains", "regex"},
        f"oracle.elements[{index}].text",
    )
    has_entries = False
    for field in ("contains", "not_contains", "regex"):
        entries = value.get(field, [])
        if not isinstance(entries, list) or any(
            not isinstance(item, str) or not item for item in entries
        ):
            raise ValueError(
                f"oracle.elements[{index}].text.{field} must be strings"
            )
        has_entries = has_entries or bool(entries)
        if field == "regex":
            for pattern in entries:
                try:
                    re.compile(pattern)
                except re.error as exc:
                    raise ValueError(
                        f"oracle.elements[{index}].text.regex contains "
                        f"an invalid pattern: {exc}"
                    ) from exc
    if not has_entries:
        raise ValueError(
            f"oracle.elements[{index}].text requires a non-empty assertion"
        )


def _matches_selector(element: _Element, selector: dict[str, Any]) -> bool:
    if selector.get("tag") and element.tag != selector["tag"]:
        return False
    if selector.get("id") and element.attrs.get("id") != selector["id"]:
        return False
    actual_classes = set(element.attrs.get("class", "").split())
    if not set(selector.get("classes", [])).issubset(actual_classes):
        return False
    return all(
        element.attrs.get(name) == expected
        for name, expected in selector.get("attributes", {}).items()
    )


def _matches_count(actual: int, expected: dict[str, int] | None) -> bool:
    if expected is None:
        return actual > 0
    if "equals" in expected and actual != expected["equals"]:
        return False
    if "min" in expected and actual < expected["min"]:
        return False
    return "max" not in expected or actual <= expected["max"]


def _matches_text(actual: str, expected: dict[str, list[str]] | None) -> bool:
    if expected is None:
        return True
    if any(value not in actual for value in expected.get("contains", [])):
        return False
    if any(value in actual for value in expected.get("not_contains", [])):
        return False
    return all(
        re.search(pattern, actual) is not None
        for pattern in expected.get("regex", [])
    )
