from __future__ import annotations

import unittest
from copy import deepcopy
from pathlib import Path

from browser_worker.reporting.acceptance import (
    acceptance_sha256,
    evaluate_acceptance,
    load_acceptance_spec,
    validate_acceptance_spec,
)

REPOSITORY_ROOT = Path(__file__).resolve().parents[2]
ACCEPTANCE_ROOT = REPOSITORY_ROOT / "research" / "acceptance"


class AcceptanceSpecTest(unittest.TestCase):
    def test_two_tasks_use_the_same_declarative_contract(self) -> None:
        cart = load_acceptance_spec(
            ACCEPTANCE_ROOT / "automationexercise-blue-top-cart.v1.json"
        )
        details = load_acceptance_spec(
            ACCEPTANCE_ROOT / "automationexercise-men-tshirt-details.v1.json"
        )

        self.assertNotEqual(cart["id"], details["id"])
        self.assertNotEqual(cart["goal"], details["goal"])
        self.assertEqual(cart["schema_version"], details["schema_version"])
        self.assertEqual(len(acceptance_sha256(cart)), 64)
        self.assertEqual(len(acceptance_sha256(details)), 64)

    def test_generic_oracle_evaluates_cart_fixture(self) -> None:
        acceptance = load_acceptance_spec(
            ACCEPTANCE_ROOT / "automationexercise-blue-top-cart.v1.json"
        )
        html = """
        <html><body><table>
          <tr id="product-1">
            <td>Blue Top</td><td>Rs. 500</td><td>1</td><td>Rs. 500</td>
          </tr>
        </table></body></html>
        """

        result = evaluate_acceptance(
            acceptance,
            html=html,
            actual_url="https://automationexercise.com/view_cart",
        )

        self.assertTrue(result["passed"])
        self.assertTrue(result["checks"]["final_url"]["passed"])
        self.assertTrue(result["checks"]["cart_row"]["passed"])

    def test_generic_oracle_evaluates_details_fixture(self) -> None:
        acceptance = load_acceptance_spec(
            ACCEPTANCE_ROOT / "automationexercise-men-tshirt-details.v1.json"
        )
        html = """
        <html><body>
          <h2>Men Tshirt</h2>
          <p>Category: Men &gt; Tshirts</p>
          <span>Rs. 400</span>
          <script>const misleading = "Your product has been added to cart.";</script>
        </body></html>
        """

        result = evaluate_acceptance(
            acceptance,
            html=html,
            actual_url="https://automationexercise.com/product_details/2",
        )

        self.assertTrue(result["passed"])
        self.assertTrue(result["checks"]["product_details"]["passed"])

    def test_generic_oracle_ignores_hidden_modal_template_text(self) -> None:
        acceptance = load_acceptance_spec(
            ACCEPTANCE_ROOT / "automationexercise-men-tshirt-details.v1.json"
        )
        html = """
        <html><body>
          <div id="cartModal" class="modal fade">
            <p>Your product has been added to cart.</p>
          </div>
          <h2>Men Tshirt</h2>
          <p>Category: Men &gt; Tshirts</p>
          <span>Rs. 400</span>
        </body></html>
        """

        result = evaluate_acceptance(
            acceptance,
            html=html,
            actual_url="https://automationexercise.com/product_details/2",
        )

        self.assertTrue(result["passed"])
        self.assertNotIn(
            "Your product has been added to cart.",
            result["checks"]["product_details"]["actual"]["texts"][0],
        )

    def test_generic_oracle_rejects_visible_modal_text(self) -> None:
        acceptance = load_acceptance_spec(
            ACCEPTANCE_ROOT / "automationexercise-men-tshirt-details.v1.json"
        )
        html = """
        <html><body>
          <div id="cartModal" class="modal fade show">
            <p>Your product has been added to cart.</p>
          </div>
          <h2>Men Tshirt</h2>
          <p>Category: Men &gt; Tshirts</p>
          <span>Rs. 400</span>
        </body></html>
        """

        result = evaluate_acceptance(
            acceptance,
            html=html,
            actual_url="https://automationexercise.com/product_details/2",
        )

        self.assertFalse(result["passed"])
        self.assertFalse(result["checks"]["product_details"]["passed"])

    def test_generic_oracle_rejects_wrong_outcome(self) -> None:
        acceptance = load_acceptance_spec(
            ACCEPTANCE_ROOT / "automationexercise-men-tshirt-details.v1.json"
        )
        result = evaluate_acceptance(
            acceptance,
            html="<html><body><h2>Blue Top</h2><span>Rs. 500</span></body></html>",
            actual_url="https://automationexercise.com/products",
        )

        self.assertFalse(result["passed"])
        self.assertFalse(result["checks"]["final_url"]["passed"])
        self.assertFalse(result["checks"]["product_details"]["passed"])

    def test_spec_rejects_embedded_task_flow(self) -> None:
        acceptance = load_acceptance_spec(
            ACCEPTANCE_ROOT / "automationexercise-blue-top-cart.v1.json"
        )
        for field, value in (
            ("steps", [{"action": "click"}]),
            ("action_order", ["search", "click"]),
            ("dsl_case", {"steps": []}),
        ):
            with self.subTest(field=field):
                invalid = deepcopy(acceptance)
                invalid[field] = value
                with self.assertRaisesRegex(ValueError, "unsupported fields"):
                    validate_acceptance_spec(invalid)

    def test_spec_rejects_empty_selector_and_text_predicates(self) -> None:
        acceptance = load_acceptance_spec(
            ACCEPTANCE_ROOT / "automationexercise-blue-top-cart.v1.json"
        )
        invalid_selector = deepcopy(acceptance)
        invalid_selector["oracle"]["elements"][0]["selector"] = {"classes": []}
        with self.assertRaisesRegex(ValueError, "effective condition"):
            validate_acceptance_spec(invalid_selector)

        invalid_text = deepcopy(acceptance)
        invalid_text["oracle"]["elements"][0]["text"] = {"contains": []}
        with self.assertRaisesRegex(ValueError, "non-empty assertion"):
            validate_acceptance_spec(invalid_text)

    def test_spec_wraps_invalid_regex_as_contract_error(self) -> None:
        acceptance = load_acceptance_spec(
            ACCEPTANCE_ROOT / "automationexercise-blue-top-cart.v1.json"
        )
        acceptance["oracle"]["elements"][0]["text"] = {"regex": ["("]}

        with self.assertRaisesRegex(ValueError, "invalid pattern"):
            validate_acceptance_spec(acceptance)


if __name__ == "__main__":
    unittest.main()
