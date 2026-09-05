from __future__ import annotations

import unittest

from app.application.browser.service import execute_browser_capability


class BrowserCapabilityContractTest(unittest.TestCase):
    def test_validate_page_elements_returns_grounded_candidates(self) -> None:
        result = execute_browser_capability(
            None,  # type: ignore[arg-type]
            capability="validate_page_elements",
            project_id=1,
            conversation_id="1",
            arguments={
                "dsl_case": {
                    "name": "Login",
                    "steps": [
                        {"action": "click", "target": "Login"},
                    ],
                },
                "a11y_nodes": [
                    {
                        "node_id": "button-1",
                        "role": "button",
                        "name": "Login",
                        "verified_selectors": [],
                    }
                ],
            },
        )

        self.assertTrue(result["valid"])
        self.assertEqual(result["locator_confidence"], "high")
        self.assertEqual(result["dsl_case"]["steps"][0]["match_count"], 1)
        self.assertEqual(result["dsl_case"]["steps"][0]["candidates"][0]["strategy"], "role")

    def test_validate_page_elements_rejects_missing_target(self) -> None:
        result = execute_browser_capability(
            None,  # type: ignore[arg-type]
            capability="validate_page_elements",
            project_id=1,
            conversation_id="1",
            arguments={
                "dsl_case": {
                    "name": "Login",
                    "steps": [
                        {"action": "click", "target": "Missing"},
                    ],
                },
                "a11y_nodes": [
                    {"node_id": "button-1", "role": "button", "name": "Login"},
                ],
            },
        )

        self.assertFalse(result["valid"])
        self.assertEqual(result["locator_confidence"], "low")
        self.assertEqual(len(result["warnings"]), 1)

    def test_validate_required_elements_recommends_reexplore_for_gaps(self) -> None:
        result = execute_browser_capability(
            None,  # type: ignore[arg-type]
            capability="validate_page_elements",
            project_id=1,
            conversation_id="1",
            arguments={
                "required_elements": [
                    {
                        "id": "submit",
                        "description": "登录按钮",
                        "keywords": ["Login", "Sign in"],
                        "roles": ["button"],
                    },
                    {
                        "id": "email",
                        "description": "邮箱输入框",
                        "keywords": ["Email"],
                        "roles": ["textbox"],
                    },
                ],
                "a11y_nodes": [
                    {"node_id": "button-1", "role": "button", "name": "Sign in"},
                ],
            },
        )

        self.assertFalse(result["valid"])
        self.assertEqual(result["missing_requirement_ids"], ["email"])
        self.assertEqual(result["recommended_action"], "re_explore")


if __name__ == "__main__":
    unittest.main()
