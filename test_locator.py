"""Test locator resolution - check if a11y tree works."""

import sys
sys.path.insert(0, "backend")

from playwright.sync_api import sync_playwright
from app.locators import resolve_with_fallback
from app.ai.page_explorer import collect_a11y_nodes, format_a11y_nodes_for_prompt


def test_a11y_tree():
    """Test a11y tree collection and locator resolution."""
    print("=" * 60)
    print("Testing A11y Tree Collection and Locator Resolution")
    print("=" * 60)

    with sync_playwright() as p:
        browser = p.chromium.launch(headless=True)
        context = browser.new_context()
        page = context.new_page()

        # Step 1: Navigate to test site
        print("\n[Step 1] Navigating to https://automationexercise.com/ ...")
        page.goto("https://automationexercise.com/", timeout=30000)
        page.wait_for_load_state("domcontentloaded", timeout=30000)
        print(f"  Current URL: {page.url}")

        # Step 2: Collect a11y nodes
        print("\n[Step 2] Collecting a11y nodes ...")
        nodes = collect_a11y_nodes(page, page_state="S0")
        print(f"  Total a11y nodes: {len(nodes)}")

        # Step 3: Show some sample nodes
        print("\n[Step 3] Sample a11y nodes:")
        interactive_nodes = [n for n in nodes if n.get("focusable")]
        print(f"  Interactive nodes: {len(interactive_nodes)}")
        for node in interactive_nodes[:10]:
            print(f"    - role={node['role']}, name=\"{node['name']}\", id={node['node_id']}")

        # Step 4: Format nodes for prompt
        print("\n[Step 4] Formatted a11y nodes for prompt:")
        formatted = format_a11y_nodes_for_prompt(nodes[:20])
        print(formatted[:1000])

        # Step 5: Test locator resolution with a11y format
        print("\n[Step 5] Testing locator resolution with a11y format ...")
        test_targets = [
            'link="Signup / Login"',
            'link="Products"',
            'button="Home"',
            'Signup / Login',  # plain text
            'Products',  # plain text
        ]

        for target in test_targets:
            try:
                resolved = resolve_with_fallback(
                    page,
                    target,
                    require_visible=True,
                )
                print(f"  ✅ Target '{target}' -> strategy={resolved.strategy}, locator found")
            except Exception as e:
                print(f"  ❌ Target '{target}' -> Error: {type(e).__name__}: {str(e)[:100]}")

        # Step 6: Test clicking with a11y format
        print("\n[Step 6] Testing click with a11y format ...")
        try:
            resolved = resolve_with_fallback(
                page,
                'link="Signup / Login"',
                require_visible=True,
            )
            resolved.locator.click()
            page.wait_for_load_state("domcontentloaded", timeout=10000)
            print(f"  ✅ Click successful, current URL: {page.url}")
        except Exception as e:
            print(f"  ❌ Click failed: {type(e).__name__}: {str(e)[:100]}")

        browser.close()

    print("\n" + "=" * 60)
    print("Test completed!")
    print("=" * 60)


if __name__ == "__main__":
    test_a11y_tree()
