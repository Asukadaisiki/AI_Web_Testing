# Complex Test Scenarios for Automation Exercise

**Date**: 2026-04-27
**Status**: Approved
**Target Site**: https://automationexercise.com/

## Overview

Design 3 complex test scenarios using the mixed approach: each scenario follows a real end-to-end business flow while focusing on specific DSL capabilities (input, wait_for, capture_text, assert_text, assert_url_contains, click).

The scenarios are saved as business requirement spec files in the project root, following the existing `test` file format.

## Scenario 1: Register → Login → Delete Account

**File**: `test_register_login`
**Primary DSL focus**: `input` (multi-field forms), `assert_text` (status assertions), `capture_text` (username cross-step passing)

**Business goal**: Verify new user registration, login with the new account, and account deletion all work correctly with proper feedback messages.

**Flow**:
1. Open homepage, click Signup / Login
2. Fill New User Signup form (name + email)
3. Click Signup, wait for detailed registration form
4. Fill full registration form (password, DOB, name, address, country)
5. Check subscription checkboxes
6. Click Create Account
7. Assert "ACCOUNT CREATED!" message
8. Click Continue, verify logged-in username in nav
9. Capture displayed username
10. Logout
11. Login with new credentials
12. Assert username matches captured value
13. Delete account
14. Assert "ACCOUNT DELETED!" message

**Assertions**:
- ACCOUNT CREATED! shown after registration
- Logged-in username displayed in navigation
- Re-login succeeds with same credentials
- Username matches registration name
- ACCOUNT DELETED! shown after deletion

**Test data**: Random username (testuser_timestamp), random email, password Test@123456, DOB 1/January/2000, India/Delhi/110001

**Scope limits**: No email verification, no password reset, no billing vs shipping address

---

## Scenario 2: Brand Filter → Multi-item Cart → Quantity Verification

**File**: `test_brand_filter_cart`
**Primary DSL focus**: `wait_for` (dynamic filter results), `capture_text` (cross-page data comparison), `assert_text` (multi-dimensional assertion chain)

**Business goal**: Verify that after filtering products by brand, adding multiple items to cart preserves correct name, price, quantity, and total across pages.

**Flow**:
1. Open homepage and login (existing test account)
2. Click Products, wait for list to load
3. Click brand filter (e.g., Polo)
4. Wait for filtered results
5. Capture product A name and price
6. Add product A to cart, click Continue Shopping
7. Capture product B name and price
8. Add product B to cart, click View Cart
9. Assert cart item A: name, unit price, quantity(1), total
10. Assert cart item B: name, unit price, quantity(1), total
11. Change product B quantity to 2
12. Assert product B total = unit price × 2

**Assertions**:
- Brand filter shows only matching products
- Cart item names match listing page
- Cart item prices match listing page
- Each item total = unit price × quantity
- Quantity change updates total in real-time

**Test data**: Existing login account, filter brands: Polo/Madame/Babyhug

**Scope limits**: No checkout, no coupons, no shipping, no cross-session persistence

---

## Scenario 3: Dynamic Content + Subscription + Contact Form

**File**: `test_dynamic_content_subscription`
**Primary DSL focus**: `wait_for` (dynamic rendering), `assert_url_contains` (navigation verification), `click` (multi-level navigation)

**Business goal**: Verify homepage dynamic content renders correctly, email subscription works, and contact form submission succeeds with proper feedback.

**Flow**:
1. Open homepage, wait for full load
2. Assert carousel/slider area is visible
3. Assert recommended items section contains products
4. Scroll to bottom, wait for Subscription area
5. Input email in subscription field
6. Click subscribe button
7. Assert "You have been successfully subscribed!" message
8. Click Contact Us nav link
9. Assert URL contains /contact_us
10. Fill contact form (name, email, subject, message)
11. Click Submit, accept confirmation dialog
12. Assert "Success! Your details have been submitted successfully." message
13. Click Home button
14. Assert URL is root path

**Assertions**:
- Carousel visible on homepage
- Recommended items section has at least one product
- Subscription shows success message
- Contact Us URL contains contact_us
- Contact form shows submission success
- Home button returns to root URL

**Test data**: Random subscription email (test_sub_timestamp@mail.com), contact form: name Test User, subject Inquiry, message This is a test message

**Scope limits**: No email delivery verification, no file upload testing, no carousel animation testing

---

## DSL Capability Coverage Matrix

| DSL Action | Scenario 1 | Scenario 2 | Scenario 3 |
|---|---|---|---|
| `goto` | ✓ | ✓ | ✓ |
| `click` | ✓ heavy | ✓ | ✓ heavy |
| `input` | ✓ heavy | ✓ | ✓ |
| `wait_for` | ✓ | ✓ heavy | ✓ heavy |
| `assert_text` | ✓ heavy | ✓ heavy | ✓ |
| `assert_url_contains` | ✓ | ✓ | ✓ heavy |
| `capture_text` | ✓ | ✓ heavy | ✓ |

## Deliverables

3 test spec files in project root:
- `test_register_login` — Scenario 1 spec
- `test_brand_filter_cart` — Scenario 2 spec
- `test_dynamic_content_subscription` — Scenario 3 spec

Each file follows the existing `test` file format (app_under_test, business_goal, entry_url_or_page, core_user_flow, main_assertions, test_data_or_account, scope_limits).
