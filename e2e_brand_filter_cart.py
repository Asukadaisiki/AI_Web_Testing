"""E2E test script for brand filter and cart validation."""
import requests
import json
import time

BASE_URL = "http://127.0.0.1:8000"

# DSL test case for brand filter and cart
DSL_CASE = {
    "name": "品牌筛选购物车验证测试",
    "description": "验证用户按品牌筛选商品后，多件商品加入购物车时，购物车中每件商品的名称、单价、数量、总价均正确无误",
    "base_url": "https://automationexercise.com/",
    "steps": [
        {
            "action": "goto",
            "value": "https://automationexercise.com/"
        },
        {
            "action": "click",
            "target": "Signup / Login",
            "target_strategy": "text",
            "locator_confidence": "high"
        },
        {
            "action": "input",
            "target": "email",
            "value": "Xjy13302412005@outlook.com",
            "target_strategy": "placeholder",
            "locator_confidence": "high"
        },
        {
            "action": "input",
            "target": "password",
            "value": "123456",
            "target_strategy": "placeholder",
            "locator_confidence": "high"
        },
        {
            "action": "click",
            "target": "Login",
            "target_strategy": "role",
            "locator_confidence": "high"
        },
        {
            "action": "wait_for",
            "target": "Logout",
            "target_strategy": "text",
            "timeout_ms": 10000
        },
        {
            "action": "click",
            "target": "Products",
            "target_strategy": "text",
            "locator_confidence": "high"
        },
        {
            "action": "wait_for",
            "target": "Brands",
            "target_strategy": "text",
            "timeout_ms": 10000
        },
        {
            "action": "click",
            "target": "Polo",
            "target_strategy": "text",
            "locator_confidence": "high"
        },
        {
            "action": "wait_for",
            "target": "Polo",
            "target_strategy": "text",
            "timeout_ms": 5000
        },
        {
            "action": "capture_text",
            "target": ".productinfo:first-child .product-name, .product-image-wrapper:first-child .productinfo p, .col-sm-4:first-child .productinfo p",
            "context_key": "product_a_name",
            "target_strategy": "css",
            "locator_confidence": "medium"
        },
        {
            "action": "capture_text",
            "target": ".productinfo:first-child h2, .product-image-wrapper:first-child .productinfo h2, .col-sm-4:first-child .productinfo h2",
            "context_key": "product_a_price",
            "target_strategy": "css",
            "locator_confidence": "medium"
        },
        {
            "action": "click",
            "target": ".productinfo:first-child .add-to-cart, .product-image-wrapper:first-child .add-to-cart, .col-sm-4:first-child .add-to-cart",
            "target_strategy": "css",
            "locator_confidence": "medium"
        },
        {
            "action": "wait_for",
            "target": "Continue Shopping",
            "target_strategy": "text",
            "timeout_ms": 5000
        },
        {
            "action": "click",
            "target": "Continue Shopping",
            "target_strategy": "text",
            "locator_confidence": "high"
        },
        {
            "action": "capture_text",
            "target": ".productinfo:nth-child(2) .product-name, .product-image-wrapper:nth-child(2) .productinfo p, .col-sm-4:nth-child(2) .productinfo p",
            "context_key": "product_b_name",
            "target_strategy": "css",
            "locator_confidence": "medium"
        },
        {
            "action": "capture_text",
            "target": ".productinfo:nth-child(2) h2, .product-image-wrapper:nth-child(2) .productinfo h2, .col-sm-4:nth-child(2) .productinfo h2",
            "context_key": "product_b_price",
            "target_strategy": "css",
            "locator_confidence": "medium"
        },
        {
            "action": "click",
            "target": ".productinfo:nth-child(2) .add-to-cart, .product-image-wrapper:nth-child(2) .add-to-cart, .col-sm-4:nth-child(2) .add-to-cart",
            "target_strategy": "css",
            "locator_confidence": "medium"
        },
        {
            "action": "wait_for",
            "target": "View Cart",
            "target_strategy": "text",
            "timeout_ms": 5000
        },
        {
            "action": "click",
            "target": "View Cart",
            "target_strategy": "text",
            "locator_confidence": "high"
        },
        {
            "action": "wait_for",
            "target": "Shopping Cart",
            "target_strategy": "text",
            "timeout_ms": 10000
        },
        {
            "action": "assert_text",
            "target": "#product-1 .cart_description a, tbody tr:first-child .cart_description a",
            "value": "${product_a_name}",
            "target_strategy": "css",
            "locator_confidence": "medium"
        },
        {
            "action": "assert_text",
            "target": "#product-1 .cart_price p, tbody tr:first-child .cart_price p",
            "value": "${product_a_price}",
            "target_strategy": "css",
            "locator_confidence": "medium"
        },
        {
            "action": "assert_text",
            "target": "#product-1 .cart_quantity button, tbody tr:first-child .cart_quantity button",
            "value": "1",
            "target_strategy": "css",
            "locator_confidence": "medium"
        },
        {
            "action": "assert_text",
            "target": "#product-1 .cart_total p, tbody tr:first-child .cart_total p",
            "value": "${product_a_price}",
            "target_strategy": "css",
            "locator_confidence": "medium"
        },
        {
            "action": "assert_text",
            "target": "#product-2 .cart_description a, tbody tr:nth-child(2) .cart_description a",
            "value": "${product_b_name}",
            "target_strategy": "css",
            "locator_confidence": "medium"
        },
        {
            "action": "assert_text",
            "target": "#product-2 .cart_price p, tbody tr:nth-child(2) .cart_price p",
            "value": "${product_b_price}",
            "target_strategy": "css",
            "locator_confidence": "medium"
        },
        {
            "action": "assert_text",
            "target": "#product-2 .cart_quantity button, tbody tr:nth-child(2) .cart_quantity button",
            "value": "1",
            "target_strategy": "css",
            "locator_confidence": "medium"
        },
        {
            "action": "assert_text",
            "target": "#product-2 .cart_total p, tbody tr:nth-child(2) .cart_total p",
            "value": "${product_b_price}",
            "target_strategy": "css",
            "locator_confidence": "medium"
        },
        {
            "action": "input",
            "target": "#product-2 .cart_quantity input, tbody tr:nth-child(2) .cart_quantity input",
            "value": "2",
            "target_strategy": "css",
            "locator_confidence": "medium"
        },
        {
            "action": "click",
            "target": "#product-2 .cart_quantity .disabled, tbody tr:nth-child(2) .cart_quantity .disabled",
            "target_strategy": "css",
            "locator_confidence": "low"
        },
        {
            "action": "wait_for",
            "target": "tbody tr:nth-child(2) .cart_total p",
            "target_strategy": "css",
            "timeout_ms": 5000
        }
    ]
}


def main():
    print("=" * 60)
    print("E2E Test: Brand Filter & Cart Validation")
    print("=" * 60)

    # First, get or create a test case
    print("\n[1] Creating test case via DSL validation...")

    # Validate DSL
    validate_resp = requests.post(
        f"{BASE_URL}/api/v1/dsl/validate",
        json=DSL_CASE
    )

    if validate_resp.status_code == 200:
        print("[OK] DSL validation passed")
        result = validate_resp.json()
        print(f"  Supported actions: {result.get('supported_actions', [])}")
    else:
        print(f"[FAIL] DSL validation failed: {validate_resp.status_code}")
        print(f"  Error: {validate_resp.text}")
        return

    # Create test case
    print("\n[2] Saving test case...")
    case_resp = requests.post(
        f"{BASE_URL}/api/v1/cases",
        json={
            "name": DSL_CASE["name"],
            "description": DSL_CASE["description"],
            "base_url": DSL_CASE["base_url"],
            "project_id": 113,
            "steps": DSL_CASE["steps"]
        }
    )

    if case_resp.status_code in [200, 201]:
        case_data = case_resp.json()
        case_id = case_data.get("id")
        print(f"[OK] Test case created with ID: {case_id}")
    else:
        print(f"[FAIL] Failed to create test case: {case_resp.status_code}")
        print(f"  Error: {case_resp.text}")
        return

    # Execute test case
    print("\n[3] Executing test case...")
    exec_resp = requests.post(
        f"{BASE_URL}/api/v1/cases/{case_id}/execute",
        json={}
    )

    if exec_resp.status_code in [200, 201]:
        exec_data = exec_resp.json()
        run_id = exec_data.get("id")
        print(f"[OK] Execution started with run ID: {run_id}")

        # Poll for completion
        print("\n[4] Waiting for execution to complete...")
        max_wait = 180  # 3 minutes max
        start_time = time.time()

        while time.time() - start_time < max_wait:
            status_resp = requests.get(f"{BASE_URL}/api/v1/executions/{run_id}")
            if status_resp.status_code == 200:
                status_data = status_resp.json()
                status = status_data.get("status", "unknown")

                if status in ["completed", "failed", "error"]:
                    print(f"\n[OK] Execution finished with status: {status}")

                    # Print results
                    print("\n" + "=" * 60)
                    print("TEST RESULTS")
                    print("=" * 60)
                    # Filter out large fields for display
                    display_data = {k: v for k, v in status_data.items() if k not in ['evidence', 'screenshot']}
                    # Write to file to avoid encoding issues
                    with open("test_results.json", "w", encoding="utf-8") as f:
                        json.dump(display_data, f, indent=2, ensure_ascii=False, default=str)
                    print("Results saved to test_results.json")
                    print(f"Status: {display_data.get('status', 'unknown')}")
                    print(f"Error: {display_data.get('error_message', 'N/A')}")

                    # Print step results if available
                    step_results = status_data.get("step_results", [])
                    if step_results:
                        print("\nSTEP RESULTS:")
                        for i, step in enumerate(step_results, 1):
                            status_icon = "[OK]" if step.get("status") == "passed" else "[FAIL]"
                            print(f"  {status_icon} Step {i}: {step.get('action', 'unknown')} - {step.get('status', 'unknown')}")
                            if step.get("error"):
                                print(f"    Error: {step.get('error')}")
                            if step.get("screenshot"):
                                print(f"    Screenshot: {step.get('screenshot')}")
                    break
                else:
                    print(f"  Status: {status}...", end="\r")
                    time.sleep(2)
            else:
                print(f"  Failed to get status: {status_resp.status_code}")
                time.sleep(2)
        else:
            print("\n[FAIL] Execution timed out after 3 minutes")
    else:
        print(f"[FAIL] Failed to start execution: {exec_resp.status_code}")
        print(f"  Error: {exec_resp.text}")


if __name__ == "__main__":
    main()
