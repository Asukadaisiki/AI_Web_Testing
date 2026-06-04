from app.ai.page_explorer import (
    USEFUL_A11Y_ROLES,
    _a11y_node_in_viewport,
    _cdp_to_a11y_nodes,
    _extract_flow_keywords,
    _filter_a11y_nodes,
)


def test_filter_removes_ignored_nodes():
    nodes = [{"role": "button", "name": "OK", "ignored": True},
             {"role": "link", "name": "Home", "ignored": False}]
    result = _filter_a11y_nodes(nodes, viewport={"width": 1280, "height": 720})
    assert len(result) == 1
    assert result[0]["name"] == "Home"


def test_filter_removes_non_useful_roles():
    """Test blacklist mode: only roles in IGNORED_A11Y_ROLES are excluded."""
    nodes = [{"role": "InlineTextBox", "name": "hello", "ignored": False},
             {"role": "StaticText", "name": "world", "ignored": False},
             {"role": "generic", "name": "div wrapper", "ignored": False},
             {"role": "button", "name": "Submit", "ignored": False}]
    result = _filter_a11y_nodes(nodes, viewport={"width": 1280, "height": 720})
    # Blacklist mode: InlineTextBox and generic are excluded, StaticText and button are kept
    assert len(result) == 2
    assert result[0]["role"] == "StaticText"
    assert result[1]["role"] == "button"


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
    """Test that USEFUL_A11Y_ROLES is None (blacklist mode) and IGNORED_A11Y_ROLES contains expected roles."""
    from app.ai.page_explorer import IGNORED_A11Y_ROLES

    # USEFUL_A11Y_ROLES is None in blacklist mode
    assert USEFUL_A11Y_ROLES is None

    # IGNORED_A11Y_ROLES should contain known useless roles
    assert "InlineTextBox" in IGNORED_A11Y_ROLES
    assert "generic" in IGNORED_A11Y_ROLES
    assert "none" in IGNORED_A11Y_ROLES

    # Useful roles should NOT be in the blacklist
    assert "button" not in IGNORED_A11Y_ROLES
    assert "link" not in IGNORED_A11Y_ROLES
    assert "textbox" not in IGNORED_A11Y_ROLES
    assert "heading" not in IGNORED_A11Y_ROLES
    assert "StaticText" not in IGNORED_A11Y_ROLES


# ── CDP node normalization ───────────────────────────────────────────────

def test_cdp_to_a11y_nodes_basic():
    cdp = {"nodes": [
        {"role": {"value": "button"}, "name": {"value": "Login"},
         "nodeId": "42", "ignored": False,
         "parentId": "7", "boundingBox": {"x": 100, "y": 200, "width": 60, "height": 30},
         "properties": [
             {"name": "focusable", "value": {"value": True}},
             {"name": "disabled", "value": {"value": False}},
         ]},
        {"role": {"value": "InlineTextBox"}, "name": {"value": "nope"},
         "nodeId": "43", "ignored": False,
         "boundingBox": {"x": 10, "y": 10, "width": 50, "height": 20}},
    ]}
    nodes = _cdp_to_a11y_nodes(cdp, page_state="S0")
    assert len(nodes) == 1
    n = nodes[0]
    assert n["node_id"] == "e42"
    assert n["role"] == "button"
    assert n["name"] == "Login"
    assert n["focusable"] is True
    assert n["disabled"] is False
    assert n["page_state"] == "S0"


def test_cdp_defaults_on_missing_props():
    cdp = {"nodes": [
        {"role": {"value": "link"}, "name": {"value": "Products"},
         "nodeId": "5", "ignored": False, "properties": []},
    ]}
    nodes = _cdp_to_a11y_nodes(cdp, page_state="S1")
    assert len(nodes) == 1
    assert nodes[0]["focusable"] is False
    assert nodes[0]["disabled"] is False
    assert nodes[0]["level"] is None
    assert nodes[0]["parent_id"] is None


# ── Keyword extraction ───────────────────────────────────────────────────

def test_extract_keywords_mixed():
    kw = _extract_flow_keywords("点击 Signup / Login，然后 Products")
    assert "signup" in kw or "login" in kw or "products" in kw


def test_extract_keywords_english():
    kw = _extract_flow_keywords("Click Polo brand then Add to cart")
    assert "polo" in kw or "add" in kw or "cart" in kw


def test_extract_keywords_stop_words():
    kw = _extract_flow_keywords("测试 the a an is login products")
    assert "the" not in kw
    assert "a" not in kw
    assert "an" not in kw
    # "login" and "products" are valid keywords (3+ chars), should remain
    assert "login" in kw
    assert "products" in kw


def test_extract_keywords_empty():
    assert _extract_flow_keywords("") == set()
    assert _extract_flow_keywords(None) == set()
