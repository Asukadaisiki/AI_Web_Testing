"""Tests for accessibility tree locator module (arxiv 2603.20358)."""

from __future__ import annotations

import pytest

from app.locators.accessibility import (
    _INTERACTIVE_ROLES,
    find_nodes_by_name,
    flatten_interactive_nodes,
    snapshot_accessibility_tree,
    try_accessibility_locate,
)


# ---------------------------------------------------------------------------
# Fake page for accessibility tree tests
# ---------------------------------------------------------------------------


class FakeAccessibilityPage:
    """Fake page that returns a controlled accessibility tree snapshot."""

    def __init__(self, tree: dict | None = None):
        self._tree = tree
        self.get_by_role_calls: list[tuple[str, str]] = []
        self.accessibility = _FakeAccessibility(tree=tree)

    def get_by_role(self, role: str, name: str):
        self.get_by_role_calls.append((role, name))
        return _FakeAccessibilityLocator(role=role, name=name)

    def locator(self, _target: str):
        return _FakeAccessibilityLocator()


class _FakeAccessibility:
    def __init__(self, tree: dict | None = None):
        self._tree = tree

    def snapshot(self) -> dict | None:
        return self._tree


class _FakeAccessibilityLocator:
    def __init__(self, role: str = "", name: str = ""):
        self.role = role
        self.name = name

    def count(self) -> int:
        return 1

    def wait_for(self, *, state: str, timeout: int) -> None:
        pass

    def evaluate(self, _script: str):
        return "fake preview text"

    @property
    def first(self):
        return self


class FailingAccessibilityPage:
    """Page where accessibility.snapshot() raises."""

    def __init__(self):
        self.accessibility = _FailingAccessibility()


class _FailingAccessibility:
    def snapshot(self) -> dict | None:
        raise RuntimeError("CDP connection lost")


class EmptyAccessibilityPage:
    """Page where accessibility.snapshot() returns None."""

    def __init__(self):
        self.accessibility = _EmptyAccessibility()


class _EmptyAccessibility:
    def snapshot(self) -> dict | None:
        return None


# ---------------------------------------------------------------------------
# Sample tree fixtures
# ---------------------------------------------------------------------------

LOGIN_PAGE_TREE = {
    "role": "WebArea",
    "name": "Automation Exercise",
    "children": [
        {
            "role": "navigation",
            "name": "Main navigation",
            "children": [
                {"role": "link", "name": "Home", "children": []},
                {"role": "link", "name": "Signup / Login", "children": []},
                {"role": "link", "name": "Products", "children": []},
                {"role": "link", "name": "Cart", "children": []},
            ],
        },
        {
            "role": "main",
            "name": "",
            "children": [
                {"role": "heading", "name": "Login to your account", "level": 2},
                {"role": "textbox", "name": "Email Address", "children": []},
                {"role": "textbox", "name": "Password", "children": []},
                {"role": "button", "name": "Login", "children": []},
            ],
        },
    ],
}

NON_INTERACTIVE_TREE = {
    "role": "WebArea",
    "name": "Test",
    "children": [
        {"role": "heading", "name": "Welcome", "level": 1},
        {"role": "paragraph", "name": "", "children": []},
        {"role": "list", "name": "", "children": [
            {"role": "listitem", "name": "Item 1"},
        ]},
    ],
}


# ---------------------------------------------------------------------------
# snapshot_accessibility_tree
# ---------------------------------------------------------------------------


def test_snapshot_returns_tree_on_success() -> None:
    tree = {"role": "WebArea", "name": "Test"}
    page = FakeAccessibilityPage(tree=tree)
    result = snapshot_accessibility_tree(page)
    assert result == tree


def test_snapshot_returns_none_when_api_returns_none() -> None:
    page = EmptyAccessibilityPage()
    result = snapshot_accessibility_tree(page)
    assert result is None


def test_snapshot_returns_none_on_exception() -> None:
    page = FailingAccessibilityPage()
    result = snapshot_accessibility_tree(page)
    assert result is None


# ---------------------------------------------------------------------------
# flatten_interactive_nodes
# ---------------------------------------------------------------------------


def test_flatten_filters_non_interactive_nodes() -> None:
    result = flatten_interactive_nodes(NON_INTERACTIVE_TREE)
    assert len(result) == 0


def test_flatten_preserves_document_order() -> None:
    result = flatten_interactive_nodes(LOGIN_PAGE_TREE)
    roles = [n["role"] for n in result]
    assert roles == ["link", "link", "link", "link", "textbox", "textbox", "button"]


def test_flatten_includes_only_interactive_roles() -> None:
    result = flatten_interactive_nodes(LOGIN_PAGE_TREE)
    for node in result:
        assert node["role"] in _INTERACTIVE_ROLES


# ---------------------------------------------------------------------------
# find_nodes_by_name
# ---------------------------------------------------------------------------


def test_find_exact_match() -> None:
    nodes = [{"role": "button", "name": "Login"}, {"role": "link", "name": "Signup / Login"}]
    result = find_nodes_by_name(nodes, "Login", exact=True)
    assert len(result) == 1
    assert result[0]["name"] == "Login"


def test_find_substring_match() -> None:
    nodes = [{"role": "button", "name": "Login"}, {"role": "link", "name": "Signup / Login"}]
    result = find_nodes_by_name(nodes, "Login", exact=False)
    assert len(result) == 2


def test_find_case_insensitive() -> None:
    nodes = [{"role": "button", "name": "LOGIN"}]
    result = find_nodes_by_name(nodes, "login", exact=True)
    assert len(result) == 1


def test_find_no_match_returns_empty() -> None:
    nodes = [{"role": "button", "name": "Submit"}]
    result = find_nodes_by_name(nodes, "Login")
    assert result == []


def test_find_empty_target_returns_empty() -> None:
    nodes = [{"role": "button", "name": "Login"}]
    result = find_nodes_by_name(nodes, "   ")
    assert result == []


# ---------------------------------------------------------------------------
# try_accessibility_locate
# ---------------------------------------------------------------------------


def test_locate_returns_none_when_snapshot_fails() -> None:
    page = EmptyAccessibilityPage()
    result = try_accessibility_locate(page, "Login")
    assert result is None


def test_locate_finds_link_by_substring_name() -> None:
    page = FakeAccessibilityPage(tree=LOGIN_PAGE_TREE)
    result = try_accessibility_locate(page, "Login")
    assert result is not None
    assert result.strategy == "a11y_role"
    assert result.trace.match_strategy == "a11y_role"
    assert "Accessibility tree matched" in result.trace.selection_reason


def test_locate_finds_exact_button_name() -> None:
    page = FakeAccessibilityPage(tree=LOGIN_PAGE_TREE)
    result = try_accessibility_locate(page, "Login")
    assert result is not None
    # The first exact match is the "Login" button (role=button, name=Login)
    # vs. "Signup / Login" link (which only matches by substring)
    assert result.strategy == "a11y_role"


def test_locate_returns_none_when_no_match() -> None:
    page = FakeAccessibilityPage(tree=LOGIN_PAGE_TREE)
    result = try_accessibility_locate(page, "NonExistentElement")
    assert result is None


def test_locate_builds_correct_role_locator() -> None:
    page = FakeAccessibilityPage(tree=LOGIN_PAGE_TREE)
    result = try_accessibility_locate(page, "Email Address")
    assert result is not None
    # Email Address has role=textbox - should use get_by_role("textbox", ...)
    assert len(page.get_by_role_calls) > 0
    role_used = page.get_by_role_calls[0][0]
    assert role_used == "textbox"
