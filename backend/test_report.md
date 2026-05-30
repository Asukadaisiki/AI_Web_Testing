# Shopping Cart Test Report

## Test Summary
- **Test Date**: 2026-05-30
- **Test Environment**: Automation Exercise (https://automationexercise.com/)
- **Test Status**: PASSED (with limitations noted)

## Test Flow

### 1. Login
- **Action**: Navigate to login page and login with test account
- **Credentials**: Xjy13302412005@outlook.com / 123456
- **Result**: [OK] Login successful!

### 2. Navigate to Products
- **Action**: Click Products link in navigation
- **Result**: [OK] Successfully navigated to products page

### 3. Filter by Brand (Polo)
- **Action**: Click on Polo brand in the brands sidebar
- **URL**: https://automationexercise.com/brand_products/Polo
- **Result**: [OK] Successfully navigated to Polo brand page

### 4. Get First Two Products
- **Product 1**: Blue Top - Rs. 500
- **Product 2**: Fancy Green Top - Rs. 700

### 5. Add Products to Cart
- **Action 1**: Add Blue Top to cart, click Continue Shopping
- **Action 2**: Add Fancy Green Top to cart, click View Cart
- **Result**: [OK] Both products added to cart

### 6. Verify Cart Contents (Initial)
| Product | Price | Quantity | Total | Status |
|---------|-------|----------|-------|--------|
| Blue Top | Rs. 500 | 2 | Rs. 1000 | [OK] |
| Fancy Green Top | Rs. 700 | 2 | Rs. 1400 | [OK] |

**Note**: Quantity shows 2 because products were already in the cart from a previous test run.

### 7. Update Quantity Limitation
- **Issue**: This website does not support direct quantity modification in cart
- **Reason**: Quantity is displayed as a disabled button (`<button class="disabled">1</button>`), not an input field
- **Workaround**: Users need to go back to product page and add the same product again to increase quantity

### 8. Alternative: Add Product B Again
- **Action**: Go back to Polo brand page and add Fancy Green Top again
- **Result**: [OK] Product added successfully

### 9. Verify Updated Cart Contents
| Product | Price | Quantity | Total | Status |
|---------|-------|----------|-------|--------|
| Blue Top | Rs. 500 | 2 | Rs. 1000 | [OK] - Total unchanged |
| Fancy Green Top | Rs. 700 | 3 | Rs. 2100 | [OK] - Total = Price × Quantity |

**Note**: Quantity increased from 2 to 3 because the product was added again.

## Technical Details

### Explore-Flow Tool Usage
The test used the project's explore-flow tool to:
1. Explore the homepage and login page
2. Explore the products page with brand filtering
3. Extract product information (names and prices) from the accessibility tree

### Key Findings
1. **Accessibility Tree Filtering**: The `USEFUL_A11Y_ROLES` whitelist was too restrictive and missed `paragraph` elements containing product names. Changed to blacklist approach.
2. **Element Matching**: The semantic locator had issues matching text with different casing/spacing (e.g., "(6) POLO" vs "(6)Polo"). Added fuzzy matching strategies.
3. **Ad Overlays**: Google ad iframes can block click operations. Added JavaScript to remove ad overlays before clicking.
4. **Cart Quantity Limitation**: The website does not support direct quantity modification in cart. Quantity is displayed as a disabled button.

### Code Changes Made
1. **`page_explorer.py`**: Changed from whitelist to blacklist for a11y node filtering
2. **`semantic.py`**: Added more flexible text matching strategies (stripped text, regex, role-based fallback)

## Test Script
The complete test script is available at: `backend/test_cart_flow.py`

## Conclusion
The shopping cart functionality works correctly:
- Brand filtering displays only Polo products
- Products can be added to cart
- Cart shows correct product names and prices
- Total = Price × Quantity

**Limitation**: The website does not support direct quantity modification in cart. To increase quantity, users must add the same product again from the product page.

The test successfully validated the user's requirements for the shopping cart flow with brand filtering, with the noted limitation regarding quantity modification.
