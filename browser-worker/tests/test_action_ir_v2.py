from __future__ import annotations

import hashlib
import json
import unittest
from pathlib import Path

from browser_worker.capabilities.browser_capabilities import (
    execute_browser_capability,
)
from browser_worker.contracts.action_ir_v2 import validate_research_v2_dsl
from browser_worker.contracts.dsl import load_canonical_dsl

ROOT = Path(__file__).parents[2]


def _draft() -> dict:
    return {
        "profile": "research-v2",
        "name": "Submit form",
        "steps": [
            {
                "plan_step_id": "submit",
                "action": "click",
                "intent": "Submit form",
                "target_binding_id": "binding-1",
                "preconditions": [
                    {
                        "type": "text_visible",
                        "value": "Submit",
                        "timeout_ms": 3000,
                    }
                ],
                "postconditions": [
                    {
                        "type": "text_visible",
                        "value": "Saved",
                        "timeout_ms": 3000,
                    }
                ],
                "idempotency": "idempotent",
                "side_effect": "browser_state",
            }
        ],
    }


def _executable() -> dict:
    payload = _draft()
    payload["input_contract"] = []
    payload["output_contract"] = []
    payload["plan_binding"] = {
        "plan_id": "plan-1",
        "version": 1,
        "sha256": "a" * 64,
    }
    payload["observation_bindings"] = [
        {
            "binding_id": "binding-1",
            "binding_sha256": "b" * 64,
            "probe_id": "probe-1",
            "observation_id": "obs-1",
            "observation_sha256": "c" * 64,
            "page_state_id": "form",
        }
    ]
    payload["steps"][0]["probe_id"] = "probe-1"
    payload["steps"][0]["observation_id"] = "obs-1"
    payload["steps"][0]["observation_sha256"] = "c" * 64
    payload["steps"][0]["page_state_id"] = "form"
    payload["steps"][0]["selected_candidate_id"] = "candidate-1"
    payload["steps"][0]["semantic_target"] = "Submit"
    payload["steps"][0]["locator_candidates"] = [
        {
            "candidate_id": "candidate-1",
            "element_ref": "form:7",
            "context_path": {"frames": [], "shadow_hosts": []},
            "locator": {
                "kind": "role",
                "role": "button",
                "name": "Submit",
                "exact": True,
            },
            "provenance": "a11y_exact",
            "observed_count": 1,
            "visible": True,
            "enabled": True,
            "score": 0.95,
        }
    ]
    return payload


class ResearchV2ContractTest(unittest.TestCase):
    def test_draft_rejects_compiler_owned_fields(self) -> None:
        validate_research_v2_dsl(_draft(), phase="draft")
        invalid = _draft()
        invalid["steps"][0]["semantic_target"] = "Submit"
        with self.assertRaises(ValueError):
            validate_research_v2_dsl(invalid, phase="draft")

    def test_executable_requires_bound_locator(self) -> None:
        case = validate_research_v2_dsl(_executable())
        self.assertEqual(case.steps[0].target, "Submit")
        self.assertEqual(case.steps[0].candidates[0].locator.kind, "role")
        self.assertEqual(case.steps[0].probe_id, "probe-1")
        self.assertEqual(case.steps[0].selected_candidate_id, "candidate-1")

    def test_executable_rejects_lineage_that_differs_from_binding(self) -> None:
        payload = _executable()
        payload["steps"][0]["observation_id"] = "obs-other"
        with self.assertRaises(ValueError):
            validate_research_v2_dsl(payload)

    def test_executable_accepts_pre_lineage_v2_payload(self) -> None:
        payload = _executable()
        for field in ("probe_id", "page_state_id"):
            payload["observation_bindings"][0].pop(field)
        for field in (
            "probe_id",
            "observation_id",
            "observation_sha256",
            "page_state_id",
            "selected_candidate_id",
        ):
            payload["steps"][0].pop(field)

        case = validate_research_v2_dsl(payload)
        self.assertIsNone(case.steps[0].probe_id)
        self.assertEqual(case.steps[0].target_binding_id, "binding-1")

    def test_canonical_v3_round_trip(self) -> None:
        fixture = json.loads(
            (ROOT / "testdata" / "dsl_research_v2_contract.json").read_text()
        )
        canonical = fixture["canonical_json"]
        digest = fixture["sha256"]
        case, loaded = load_canonical_dsl(
            canonical,
            digest,
            fixture["canonical_version"],
        )
        self.assertEqual(case.profile, "research-v2")
        self.assertEqual(
            hashlib.sha256(canonical.encode()).hexdigest(),
            digest,
        )
        self.assertEqual(
            loaded,
            json.loads(canonical),
        )

    def test_binding_preflight_uses_structured_contract(self) -> None:
        result = execute_browser_capability(
            None,
            capability="validate_page_elements",
            project_id=1,
            conversation_id="1",
            arguments={"dsl_case": _executable()},
        )
        self.assertTrue(result["valid"])
        self.assertEqual(result["validation_mode"], "target_binding")
        self.assertEqual(len(result["case_digest"]), 64)


if __name__ == "__main__":
    unittest.main()
