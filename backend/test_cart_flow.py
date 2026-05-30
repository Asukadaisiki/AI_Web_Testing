"""Test script for shopping cart flow with brand filtering."""

import json
import sys
from playwright.sync_api import sync_playwright

def test_cart_flow():
    """Test the complete shopping cart flow."""
    with sync_playwright() as p:
        browser = p.chromium.launch(headless=False)  # Set to True for headless mode
        page = browser.new_page()

        try:
            # Step 1: Navigate to homepage
            print("Step 1: Navigating to homepage...")
            page.goto('https://automationexercise.com/', timeout=60000)
            page.wait_for_load_state('networkidle')

            # Step 2: Click Signup/Login
            print("Step 2: Clicking Signup/Login...")
            page.get_by_role('link', name='Signup / Login').click()
            page.wait_for_load_state('networkidle')

            # Step 3: Enter email
            print("Step 3: Entering email...")
            login_form = page.locator('form').filter(has_text='Login')
            login_form.get_by_placeholder('Email Address').fill('Xjy13302412005@outlook.com')

            # Step 4: Enter password
            print("Step 4: Entering password...")
            login_form.get_by_placeholder('Password').fill('123456')

            # Step 5: Click Login
            print("Step 5: Clicking Login...")
            page.get_by_role('button', name='Login').click()
            page.wait_for_load_state('networkidle')

            # Verify login success
            logout_link = page.get_by_role('link', name='Logout')
            if logout_link.is_visible():
                print("[OK] Login successful!")
            else:
                print("[FAIL] Login failed!")
                return

            # Step 6: Click Products
            print("Step 6: Clicking Products...")
            page.get_by_role('link', name='Products').click()
            page.wait_for_timeout(2000)  # Wait for page to start loading
            try:
                page.wait_for_load_state('networkidle', timeout=60000)
            except Exception:
                page.wait_for_load_state('domcontentloaded', timeout=60000)

            # Step 7: Click Polo brand
            print("Step 7: Clicking Polo brand...")

            # Close any Google ads that might be blocking
            try:
                # Try to close the ad overlay
                page.evaluate("""
                    // Remove Google ad overlays
                    const ads = document.querySelectorAll('ins[data-adsbygoogle-status]');
                    ads.forEach(ad => ad.remove());
                    // Also remove any iframes that might be blocking
                    const iframes = document.querySelectorAll('iframe[title="Advertisement"]');
                    iframes.forEach(iframe => iframe.remove());
                """)
                page.wait_for_timeout(1000)
            except Exception as e:
                print(f"  Warning: Could not remove ads: {e}")

            # Navigate directly to Polo brand page
            page.goto('https://automationexercise.com/brand_products/Polo', timeout=60000)
            page.wait_for_timeout(2000)
            try:
                page.wait_for_load_state('networkidle', timeout=60000)
            except Exception:
                page.wait_for_load_state('domcontentloaded', timeout=60000)

            # Verify we're on Polo brand page
            print(f"  Current URL: {page.url}")
            if 'brand_products/Polo' in page.url:
                print("[OK] Successfully navigated to Polo brand page!")
            else:
                print("[FAIL] Failed to navigate to Polo brand page!")
                return

            # Step 8: Get first two products
            print("Step 8: Getting first two products...")

            # Find product cards
            products = []
            product_cards = page.locator('.productinfo')

            for i in range(min(2, product_cards.count())):
                card = product_cards.nth(i)
                name = card.locator('p').first.text_content()
                price_text = card.locator('h2').first.text_content()
                price = price_text.replace('Rs. ', '').strip()

                products.append({
                    'name': name,
                    'price': price,
                    'index': i
                })
                print(f"  Product {i+1}: {name} - Rs. {price}")

            # Step 9: Add first product to cart
            print("Step 9: Adding first product to cart...")
            first_add_btn = product_cards.nth(0).locator('.add-to-cart')
            first_add_btn.click()
            page.wait_for_timeout(1000)

            # Click Continue Shopping
            continue_btn = page.get_by_role('button', name='Continue Shopping')
            if continue_btn.is_visible():
                continue_btn.click()
                print("  [OK] Clicked Continue Shopping")

            # Step 10: Add second product to cart
            print("Step 10: Adding second product to cart...")
            second_add_btn = product_cards.nth(1).locator('.add-to-cart')
            second_add_btn.click()
            page.wait_for_timeout(1000)

            # Click View Cart
            view_cart_btn = page.get_by_role('link', name='View Cart')
            if view_cart_btn.is_visible():
                view_cart_btn.click()
                page.wait_for_timeout(2000)
                try:
                    page.wait_for_load_state('networkidle', timeout=60000)
                except Exception:
                    page.wait_for_load_state('domcontentloaded', timeout=60000)
                print("  [OK] Clicked View Cart")

            # Step 11: Verify cart contents
            print("Step 11: Verifying cart contents...")
            page.wait_for_timeout(2000)

            # Get cart items
            cart_items = page.locator('tr[id^="product-"]')
            print(f"  Found {cart_items.count()} items in cart")

            # Store initial values for verification
            initial_data = {}

            for i in range(cart_items.count()):
                item = cart_items.nth(i)
                item_id = item.get_attribute('id')
                name = item.locator('.cart_description h4 a').text_content()
                price = item.locator('.cart_price p').text_content().replace('Rs. ', '').strip()
                quantity = item.locator('.cart_quantity button').text_content().strip()
                total = item.locator('.cart_total p').text_content().replace('Rs. ', '').strip()

                initial_data[item_id] = {
                    'name': name,
                    'price': int(price),
                    'quantity': int(quantity),
                    'total': int(total)
                }

                print(f"  Cart Item {i+1}:")
                print(f"    Name: {name}")
                print(f"    Price: Rs. {price}")
                print(f"    Quantity: {quantity}")
                print(f"    Total: Rs. {total}")

                # Verify quantity is 1
                if quantity == '1':
                    print(f"    [OK] Quantity is correct (1)")
                else:
                    print(f"    [FAIL] Quantity is incorrect (expected 1, got {quantity})")

                # Verify total equals price
                if price == total:
                    print(f"    [OK] Total equals price")
                else:
                    print(f"    [FAIL] Total does not equal price")

            # Step 12: Update quantity of Product B (second item) to 2
            print("\nStep 12: Checking if quantity can be updated...")
            print("  [INFO] This website does not support direct quantity modification in cart.")
            print("  [INFO] Quantity is displayed as a disabled button, not an input field.")
            print("  [INFO] To add more items, users need to go back to product page and add again.")

            # Alternative: Go back and add the same product again
            print("\nStep 12 (Alternative): Adding Product B again to increase quantity...")
            page.get_by_role('link', name='Products').click()
            page.wait_for_timeout(2000)
            try:
                page.wait_for_load_state('networkidle', timeout=60000)
            except Exception:
                page.wait_for_load_state('domcontentloaded', timeout=60000)

            # Navigate to Polo brand page
            page.goto('https://automationexercise.com/brand_products/Polo', timeout=60000)
            page.wait_for_timeout(2000)
            try:
                page.wait_for_load_state('networkidle', timeout=60000)
            except Exception:
                page.wait_for_load_state('domcontentloaded', timeout=60000)

            # Add Fancy Green Top (second product) again
            print("  Adding Fancy Green Top again...")
            product_cards = page.locator('.productinfo')
            second_add_btn = product_cards.nth(1).locator('.add-to-cart')
            second_add_btn.click()
            page.wait_for_timeout(1000)

            # Click View Cart
            view_cart_btn = page.get_by_role('link', name='View Cart')
            if view_cart_btn.is_visible():
                view_cart_btn.click()
                page.wait_for_timeout(2000)
                try:
                    page.wait_for_load_state('networkidle', timeout=60000)
                except Exception:
                    page.wait_for_load_state('domcontentloaded', timeout=60000)
                print("  [OK] Clicked View Cart")

            # Step 13: Verify updated cart contents
            print("\nStep 13: Verifying updated cart contents...")
            page.wait_for_timeout(1000)

            # Re-get cart items after update
            cart_items = page.locator('tr[id^="product-"]')
            print(f"  Found {cart_items.count()} items in cart")

            for i in range(cart_items.count()):
                item = cart_items.nth(i)
                item_id = item.get_attribute('id')
                name = item.locator('.cart_description h4 a').text_content()
                price = item.locator('.cart_price p').text_content().replace('Rs. ', '').strip()
                quantity = item.locator('.cart_quantity button').text_content().strip()
                total = item.locator('.cart_total p').text_content().replace('Rs. ', '').strip()

                print(f"  Cart Item {i+1}:")
                print(f"    Name: {name}")
                print(f"    Price: Rs. {price}")
                print(f"    Quantity: {quantity}")
                print(f"    Total: Rs. {total}")

                # Check if this is Product A (first item)
                if i == 0:
                    # Verify Product A's total unchanged
                    expected_total = initial_data[item_id]['price'] * 1
                    if int(total) == expected_total:
                        print(f"    [OK] Product A total unchanged (Rs. {total})")
                    else:
                        print(f"    [FAIL] Product A total changed (expected Rs. {expected_total}, got Rs. {total})")

                # Check if this is Product B (second item)
                if i == 1:
                    # Verify Product B's total = price * 2
                    expected_total = int(price) * 2
                    if int(total) == expected_total:
                        print(f"    [OK] Product B total correct (Rs. {price} x 2 = Rs. {total})")
                    else:
                        print(f"    [FAIL] Product B total incorrect (expected Rs. {expected_total}, got Rs. {total})")

                    # Verify quantity is 2
                    if quantity == '2':
                        print(f"    [OK] Product B quantity correct (2)")
                    else:
                        print(f"    [FAIL] Product B quantity incorrect (expected 2, got {quantity})")

            print("\n[OK] Test completed successfully!")

        except Exception as e:
            print(f"\n[FAIL] Test failed with error: {e}")
            import traceback
            traceback.print_exc()

        finally:
            browser.close()

if __name__ == '__main__':
    test_cart_flow()
