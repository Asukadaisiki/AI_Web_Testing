from app.ai.page_explorer import (
    USEFUL_A11Y_ROLES,
    _filter_a11y_nodes,
    _a11y_node_in_viewport,
)


def test_filter_removes_ignored_nodes():
    nodes = [{"role": "button", "name": "OK", "ignored": True},
             {"role": "link", "name": "Home", "ignored": False}]
    result = _filter_a11y_nodes(nodes, viewport={"width": 1280, "height": 720})
    assert len(result) == 1
    assert result[0]["name"] == "Home"


def test_filter_removes_non_useful_roles():
    nodes = [{"role": "InlineTextBox", "name": "hello", "ignored": False},
             {"role": "StaticText", "name": "world", "ignored": False},
             {"role": "generic", "name": "div wrapper", "ignored": False},
             {"role": "button", "name": "Submit", "ignored": False}]
    result = _filter_a11y_nodes(nodes, viewport={"width": 1280, "height": 720})
    assert len(result) == 1
    assert result[0]["role"] == "button"


def test_filter_removes_off_viewport():
    nodes = [
        {"role": "button", "name": "Inside View", "ignored": False,
         "boundingBox": {"x": 100, "y": 100, "width": 200, "height": 40}},
        {"role": "link", "name": "Footer Link", "ignored": False,
         "boundingBox": {"x": 0, "y": 800, "width": 100, "height": 20}},
    ]
    result = _filter_a11y_nodes(nodes, viewport={"width": 1280, "height": 720})
    assert len(result) == 1
    assert result[0]["name"] == "Inside View"


def test_viewport_filter_keeps_partially_visible():
    node = {"role": "button", "name": "Bottom Visible", "ignored": False,
            "boundingBox": {"x": 0, "y": 700, "width": 200, "height": 50}}
    assert _a11y_node_in_viewport(node, {"width": 1280, "height": 720}) is True


def test_useful_roles_set_contains_expected():
    assert "button" in USEFUL_A11Y_ROLES
    assert "link" in USEFUL_A11Y_ROLES
    assert "textbox" in USEFUL_A11Y_ROLES
    assert "heading" in USEFUL_A11Y_ROLES
    assert "navigation" in USEFUL_A11Y_ROLES
    assert "list" in USEFUL_A11Y_ROLES
    assert "listitem" in USEFUL_A11Y_ROLES
    assert "dialog" in USEFUL_A11Y_ROLES
    assert "InlineTextBox" not in USEFUL_A11Y_ROLES
    assert "StaticText" not in USEFUL_A11Y_ROLES
    assert "generic" not in USEFUL_A11Y_ROLES
