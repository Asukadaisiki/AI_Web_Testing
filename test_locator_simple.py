"""Simple test for locator resolution."""

import sys
sys.path.insert(0, "backend")

from playwright.sync_api import sync_playwright
from app.ai.page_explorer import collect_a11y_nodes


def main():
    print("Testing a11y tree collection...")

    with sync_playwright() as p:
        browser = p.chromium.launch(headless=True)
        page = browser.new_page()

        # Navigate
        print("Navigating to https://automationexercise.com/ ...")
        page.goto("https://automationexercise.com/", timeout=30000)
        page.wait_for_load_state("domcontentloaded", timeout=30000)
        print(f"Current URL: {page.url}")

        # Collect a11y nodes
        print("Collecting a11y nodes ...")
        nodes = collect_a11y_nodes(page, page_state="S0")
        print(f"Total a11y nodes: {len(nodes)}")

        # Show interactive nodes
        interactive_nodes = [n for n in nodes if n.get("focusable")]
        print(f"Interactive nodes: {len(interactive_nodes)}")
        print("\nSample interactive nodes:")
        for node in interactive_nodes[:15]:
            print(f"  - role={node['role']}, name=\"{node['name']}\", id={node['node_id']}")

        # Check for specific elements
        print("\nChecking for specific elements:")
        signup_login = [n for n in nodes if n.get("name") and "Signup" in n["name"]]
        print(f"  - Signup/Login nodes: {len(signup_login)}")
        for n in signup_login:
            print(f"    role={n['role']}, name=\"{n['name']}\", id={n['node_id']}")

        products = [n for n in nodes if n.get("name") and "Product" in n["name"]]
        print(f"  - Products nodes: {len(products)}")
        for n in products:
            print(f"    role={n['role']}, name=\"{n['name']}\", id={n['node_id']}")

        browser.close()

    print("\nTest completed!")


if __name__ == "__main__":
    main()
