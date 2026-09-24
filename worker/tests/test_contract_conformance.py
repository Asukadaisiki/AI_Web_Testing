"""跨语言契约一致性测试。

Go 与 Python 共读 `v2/fixtures/contract/case_contract.json`，必须给出相同的
accept 与错误码。任何一侧改动契约规则而不同步另一侧，这个测试就会红。
"""

from __future__ import annotations

import json
import sys
import unittest
from pathlib import Path

WORKER_ROOT = Path(__file__).resolve().parents[1]
if str(WORKER_ROOT) not in sys.path:
    sys.path.insert(0, str(WORKER_ROOT))

from loop_worker.contracts import (  # noqa: E402
    ActRequest,
    ActResponse,
    CaseInvalid,
    validate_case,
)

FIXTURE_PATH = WORKER_ROOT.parent / "fixtures" / "contract" / "case_contract.json"


def load_fixtures() -> dict:
    with FIXTURE_PATH.open(encoding="utf-8") as handle:
        return json.load(handle)


class ContractConformanceTest(unittest.TestCase):
    """与 Go 的 TestContractFixtures 一一对应。"""

    def test_fixture_file_exists(self) -> None:
        self.assertTrue(FIXTURE_PATH.is_file(), f"missing fixture: {FIXTURE_PATH}")

    def test_fixture_version_matches(self) -> None:
        from loop_worker.contracts import CASE_VERSION

        self.assertEqual(load_fixtures()["version"], CASE_VERSION)

    def test_every_fixture_case_matches_go(self) -> None:
        fixtures = load_fixtures()
        self.assertTrue(fixtures["cases"], "fixtures must not be empty")
        for item in fixtures["cases"]:
            with self.subTest(name=item["name"]):
                expect = item["expect"]
                if expect["accept"]:
                    validate_case(item["case"])
                    continue
                with self.assertRaises(CaseInvalid) as caught:
                    validate_case(item["case"])
                self.assertEqual(
                    caught.exception.code,
                    expect["code"],
                    f"{item['name']}: got {caught.exception}",
                )

    def test_authoring_action_fixture_matches_python_contract(self) -> None:
        fixture = load_fixtures()["authoring_action"]
        request = ActRequest.model_validate(fixture["request"])
        self.assertEqual(request.action, "assert_element")
        self.assertEqual(len(request.postconditions), 1)
        self.assertEqual(request.postconditions[0].type, "text_visible")

        response = ActResponse.model_validate(fixture["response"])
        self.assertEqual(response.status, "failed")
        self.assertEqual(response.observation.observation_id, "obs_authoring")
        self.assertFalse(response.conditions[0].satisfied)
        self.assertIsNotNone(response.error)
        self.assertEqual(response.error.kind, "condition_unmet")


class CaseValidationTest(unittest.TestCase):
    """夹具之外的边界：两侧都必须一致的行为。"""

    def _goto_case(self, **step_overrides: object) -> dict:
        step = {
            "index": 0,
            "action": "goto",
            "intent": "open",
            "value": "https://shop.test/products",
            "preconditions": [],
            "postconditions": [{"type": "url_contains", "value": "/products"}],
            "timeout_ms": 5000,
        }
        step.update(step_overrides)
        return {
            "case_version": "loop.case.v1",
            "name": "case",
            "goal": "goal",
            "base_url": "https://shop.test",
            "steps": [step],
        }

    def test_missing_case_version_is_normalized_not_rejected(self) -> None:
        payload = self._goto_case()
        payload.pop("case_version")
        case = validate_case(payload)
        self.assertEqual(case.case_version, "loop.case.v1")

    def test_missing_timeouts_are_filled_not_rejected(self) -> None:
        from loop_worker.contracts import (
            DEFAULT_CONDITION_TIMEOUT_MS,
            DEFAULT_STEP_TIMEOUT_MS,
        )

        payload = self._goto_case()
        payload["steps"][0].pop("timeout_ms")
        # postcondition 本来就没写 timeout_ms，归一化必须补默认值。
        # 断言常量而不是字面量：默认值是按真实站点实测调过的，写死会变成假红。
        self.assertNotIn("timeout_ms", payload["steps"][0]["postconditions"][0])
        case = validate_case(payload)
        self.assertEqual(case.steps[0].timeout_ms, DEFAULT_STEP_TIMEOUT_MS)
        self.assertEqual(
            case.steps[0].postconditions[0].timeout_ms, DEFAULT_CONDITION_TIMEOUT_MS
        )

    def test_wrong_case_version_is_rejected(self) -> None:
        payload = self._goto_case()
        payload["case_version"] = "loop.case.v2"
        with self.assertRaises(CaseInvalid) as caught:
            validate_case(payload)
        self.assertEqual(caught.exception.code, "case_version_mismatch")

    def test_step_index_is_normalized(self) -> None:
        payload = self._goto_case(index=7)
        self.assertEqual(validate_case(payload).steps[0].index, 0)

    def test_unknown_field_is_rejected(self) -> None:
        payload = self._goto_case()
        payload["steps"][0]["page_state"] = "products"
        with self.assertRaises(CaseInvalid) as caught:
            validate_case(payload)
        self.assertEqual(caught.exception.code, "case_invalid_json")

    def test_non_absolute_goto_value_is_rejected(self) -> None:
        payload = self._goto_case(value="/products")
        with self.assertRaises(CaseInvalid) as caught:
            validate_case(payload)
        self.assertEqual(caught.exception.code, "goto_value_not_absolute")


if __name__ == "__main__":
    unittest.main()
