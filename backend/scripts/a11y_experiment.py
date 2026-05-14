"""A11y tree vs DOM extraction — one-off comparison on automationexercise.com.

Runs against the same URLs in the test_brand_filter_cart flow:
  /                  (homepage)
  /products          (full product list, login-free)
  /brand_products/Polo  (brand-filtered list)

For each page produces:
  - element count via current DOM extraction
  - interactive-node count via A11y snapshot
  - serialized size (proxy for token cost)
  - role distribution from A11y
  - elements present in DOM but absent from A11y (coverage gap)

No DB writes, no project state changed.
"""
from __future__ import annotations

import json
import time
from typing import Any

from playwright.sync_api import sync_playwright

from app.ai.page_explorer import collect_interactable_elements


URLS = [
    "https://automationexercise.com/",
    "https://automationexercise.com/products",
    "https://automationexercise.com/brand_products/Polo",
]


def cdp_a11y_tree(page) -> list[dict[str, Any]]:
    """Get the full accessibility tree via CDP. Returns flat list of nodes."""
    client = page.context.new_cdp_session(page)
    client.send("Accessibility.enable")
    try:
        result = client.send("Accessibility.getFullAXTree", {})
    finally:
        try:
            client.send("Accessibility.disable")
        except Exception:
            pass
        try:
            client.detach()
        except Exception:
            pass

    nodes = []
    for n in result.get("nodes", []):
        # CDP format: role.value, name.value, value.value, ignored, etc.
        role = (n.get("role") or {}).get("value")
        name = (n.get("name") or {}).get("value", "")
        ignored = n.get("ignored", False)
        if ignored:
            continue
        # Bounding box if present
        props = {p["name"]: p["value"].get("value") for p in n.get("properties", []) if "name" in p and "value" in p}
        nodes.append({
            "role": role,
            "name": (name or "")[:80],
            "focusable": props.get("focusable"),
            "disabled": props.get("disabled"),
            "level": props.get("level"),
        })
    return nodes


def walk_a11y(node: dict[str, Any] | None, out: list[dict[str, Any]]) -> None:
    """(legacy, unused) Flatten a hierarchical a11y tree."""
    if not isinstance(node, dict):
        return
    out.append({"role": node.get("role"), "name": (node.get("name") or "")[:80]})
    for child in node.get("children", []) or []:
        walk_a11y(child, out)


INTERACTIVE_A11Y_ROLES = {
    "button", "link", "textbox", "checkbox", "radio", "menuitem",
    "menuitemcheckbox", "menuitemradio", "combobox", "listbox", "option",
    "tab", "treeitem", "switch", "searchbox", "spinbutton", "slider",
}

# Roles that are useful for an LLM agent (interactive + landmark + descriptive)
USEFUL_A11Y_ROLES = INTERACTIVE_A11Y_ROLES | {
    "heading", "image", "navigation", "main", "banner", "contentinfo",
    "form", "search", "region", "dialog", "alertdialog", "alert",
    "menu", "menubar", "tablist", "list", "article", "complementary",
}


def explore_one(page, url: str) -> dict[str, Any]:
    t0 = time.monotonic()
    page.goto(url, timeout=60000, wait_until="domcontentloaded")
    page.wait_for_load_state("networkidle", timeout=60000)
    nav_ms = (time.monotonic() - t0) * 1000

    # 1) Current DOM extraction (existing project function)
    t1 = time.monotonic()
    dom_elements = collect_interactable_elements(url, timeout_ms=60000, page=page)
    dom_ms = (time.monotonic() - t1) * 1000
    dom_count = len(dom_elements)
    dom_interactive = sum(1 for e in dom_elements
                          if (e.get("tag", "") or "").lower()
                          in {"button", "input", "a", "select", "textarea"})
    dom_json = json.dumps(dom_elements, ensure_ascii=False, default=str)
    dom_chars = len(dom_json)

    # 2) A11y snapshot via CDP
    t2 = time.monotonic()
    a11y_nodes = cdp_a11y_tree(page)
    a11y_ms = (time.monotonic() - t2) * 1000
    a11y_count = len(a11y_nodes)
    a11y_interactive = sum(1 for n in a11y_nodes
                           if (n.get("role") or "") in INTERACTIVE_A11Y_ROLES)
    # Useful subset = what we'd actually feed an LLM
    a11y_useful = [n for n in a11y_nodes
                   if (n.get("role") or "") in USEFUL_A11Y_ROLES]
    a11y_useful_json = json.dumps(a11y_useful, ensure_ascii=False, default=str)
    a11y_useful_chars = len(a11y_useful_json)
    a11y_json = json.dumps(a11y_nodes, ensure_ascii=False, default=str)
    a11y_chars = len(a11y_json)

    # 3) Coverage gap — DOM interactive elements whose text/aria_label
    # has no matching A11y node by name.
    a11y_names = {(n.get("name") or "").strip().casefold()
                  for n in a11y_nodes if n.get("name")}
    dom_only: list[str] = []
    for e in dom_elements:
        tag = (e.get("tag") or "").lower()
        if tag not in {"button", "input", "a", "select", "textarea"}:
            continue
        text = (e.get("text") or e.get("aria_label")
                or e.get("placeholder") or "").strip().casefold()
        if text and text not in a11y_names:
            dom_only.append(f"{tag}: {text[:40]}")

    role_dist: dict[str, int] = {}
    for n in a11y_nodes:
        r = n.get("role") or "unknown"
        role_dist[r] = role_dist.get(r, 0) + 1

    return {
        "url": url,
        "nav_ms": int(nav_ms),
        "dom": {
            "count": dom_count,
            "interactive": dom_interactive,
            "json_chars": dom_chars,
            "extract_ms": int(dom_ms),
        },
        "a11y": {
            "count": a11y_count,
            "interactive": a11y_interactive,
            "useful_count": len(a11y_useful),
            "json_chars": a11y_chars,
            "useful_json_chars": a11y_useful_chars,
            "extract_ms": int(a11y_ms),
            "top_roles": sorted(role_dist.items(),
                                key=lambda x: -x[1])[:8],
        },
        "dom_only_interactive_count": len(dom_only),
        "dom_only_sample": dom_only[:5],
    }


def main() -> None:
    with sync_playwright() as pw:
        browser = pw.chromium.launch(headless=True)
        context = browser.new_context()
        page = context.new_page()

        results = []
        for url in URLS:
            try:
                r = explore_one(page, url)
            except Exception as exc:
                r = {"url": url, "error": str(exc)}
            results.append(r)

        browser.close()

    print(json.dumps(results, ensure_ascii=False, indent=2))

    # Compact table for the human eye
    print("\n" + "=" * 100)
    print(f"{'URL':<55} {'DOM cnt':>8} {'A11y raw':>9} {'A11y useful':>12} "
          f"{'DOM kB':>7} {'A11y kB raw':>11} {'A11y kB use':>11} {'gain':>6}")
    print("-" * 120)
    for r in results:
        if "error" in r:
            print(f"{r['url']:<55} ERROR: {r['error'][:50]}")
            continue
        gain = (r["dom"]["json_chars"] / r["a11y"]["useful_json_chars"]
                if r["a11y"]["useful_json_chars"] else 0)
        print(f"{r['url']:<55} "
              f"{r['dom']['count']:>8} "
              f"{r['a11y']['count']:>9} "
              f"{r['a11y']['useful_count']:>12} "
              f"{r['dom']['json_chars']//1024:>7} "
              f"{r['a11y']['json_chars']//1024:>11} "
              f"{r['a11y']['useful_json_chars']//1024:>11} "
              f"{gain:>5.1f}x")
    print("=" * 120)

    print("\nDOM-only interactive elements (A11y missed):")
    for r in results:
        if "error" in r:
            continue
        print(f"  {r['url']}: {r['dom_only_interactive_count']} missed")
        for s in r["dom_only_sample"]:
            print(f"    - {s}")

    print("\nA11y role distribution (top 8 per page):")
    for r in results:
        if "error" in r:
            continue
        print(f"  {r['url']}")
        for role, cnt in r["a11y"]["top_roles"]:
            print(f"    {role}: {cnt}")


if __name__ == "__main__":
    main()
