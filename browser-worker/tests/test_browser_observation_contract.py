from __future__ import annotations

import gzip
import json
import tempfile
import unittest
from pathlib import Path
from unittest.mock import MagicMock

from browser_worker.contracts.browser_observation import (
    BROWSER_OBSERVATION_VERSION,
    LOCATOR_KINDS,
    BrowserObservation,
    ResolvedTargetEvidence,
    TargetBinding,
    canonical_sha256,
    validate_locator_spec,
)
from browser_worker.exploration.observation import (
    build_browser_observation,
    build_resolved_target_evidence,
)
from browser_worker.locators.compiler import compile_locator

ROOT = Path(__file__).parents[2]


class BrowserObservationContractTest(unittest.TestCase):
    def test_shared_observation_golden_matches_python_contract(self) -> None:
        payload = json.loads(
            (
                ROOT
                / "testdata"
                / "browser_observation_v2_contract.json"
            ).read_text()
        )
        observation = BrowserObservation.model_validate(payload)
        serialized = observation.model_dump(mode="json")
        serialized.pop("artifact", None)

        self.assertEqual(serialized, payload)
        self.assertEqual(observation.probe_id, "probe-1")
        self.assertEqual(
            observation.elements[0].locators[0].candidate_id,
            "candidate-1",
        )
        self.assertEqual(observation.relations[0].source, "form:7")
        self.assertEqual(observation.relations[0].target, "form:8")

    def test_capability_manifest_matches_locator_contract(self) -> None:
        manifest = json.loads(
            (ROOT / "contracts" / "browser-capabilities.v1.json").read_text()
        )
        self.assertEqual(manifest["schema_version"], "browser.capabilities.v1")
        self.assertEqual(set(manifest["locator_kinds"]), LOCATOR_KINDS)

    def test_cross_site_fixture_covers_non_commerce_shapes(self) -> None:
        fixture = json.loads(
            (
                ROOT
                / "research"
                / "fixtures"
                / "browser-contract-cross-site.v1.json"
            ).read_text()
        )
        ids = {scenario["id"] for scenario in fixture["scenarios"]}
        self.assertEqual(
            ids,
            {
                "native-form",
                "government-search",
                "react-spa",
                "large-document-table",
                "dynamic-data-table",
                "aria-composite",
                "open-shadow-dom",
            },
        )

    def test_observation_keeps_a11y_and_dom_text_separate(self) -> None:
        state_hash = canonical_sha256({"url": "https://example.test/form"})
        observation = BrowserObservation.model_validate(
            {
                "schema_version": BROWSER_OBSERVATION_VERSION,
                "probe_id": "probe-1",
                "observation_id": "obs-1",
                "page_state": {
                    "state_id": "state-1",
                    "revision": 1,
                    "url": "https://example.test/form",
                    "title": "Form",
                    "state_sha256": state_hash,
                },
                "elements": [
                    {
                        "element_ref": "el-1",
                        "context_path": {"frames": [], "shadow_hosts": []},
                        "a11y": {
                            "role": "textbox",
                            "name": "Email address",
                            "states": {},
                            "relations": {},
                        },
                        "dom": {
                            "backend_node_id": 7,
                            "tag": "input",
                            "attrs": {"name": "email"},
                            "text": "",
                        },
                        "runtime": {
                            "connected": True,
                            "visible": True,
                            "enabled": True,
                            "editable": True,
                        },
                    }
                ],
                "relations": [],
            }
        )
        self.assertEqual(observation.elements[0].a11y.name, "Email address")
        self.assertEqual(observation.elements[0].dom.text, "")

    def test_legacy_nodes_build_bounded_content_addressed_observation(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            observation = build_browser_observation(
                url="https://example.test/form",
                title="Form",
                state_id="S1",
                revision=2,
                nodes=[
                    {
                        "node_id": "e1",
                        "backend_dom_node_id": 7,
                        "role": "button",
                        "name": "DOM descendant text",
                        "a11y_name": "Submit",
                        "dom_text": "x" * 400,
                        "dom": {
                            "tag": "button",
                            "attrs": {"id": "submit"},
                            "connected": True,
                            "visible": True,
                            "enabled": True,
                        },
                        "verified_selectors": [
                            {"strategy": "css", "selector": "#submit"}
                        ],
                    }
                ],
                artifact_root=Path(directory),
            )

            parsed = BrowserObservation.model_validate(observation)
            self.assertEqual(parsed.elements[0].a11y.name, "Submit")
            self.assertEqual(len(parsed.elements[0].dom.text), 256)
            self.assertEqual(parsed.page_state.revision, 2)
            self.assertTrue(parsed.probe_id.startswith("probe_"))
            self.assertTrue(parsed.elements[0].locators[0].candidate_id.startswith("candidate_"))
            self.assertIsNotNone(parsed.artifact)
            artifacts = list(
                (Path(directory) / "browser-observations").glob("*.json.gz")
            )
            self.assertEqual(len(artifacts), 1)

    def test_target_binding_requires_selected_candidate(self) -> None:
        payload = {
            "schema_version": "grounding.target-binding.v1",
            "binding_id": "binding-1",
            "plan_id": "plan-1",
            "plan_version": 1,
            "plan_step_id": "step-1",
            "probe_id": "probe-1",
            "semantic_target": "Submit form",
            "action": "click",
            "page_state_id": "state-1",
            "observation_id": "obs-1",
            "observation_sha256": "a" * 64,
            "element_refs": ["el-1"],
            "candidates": [
                {
                    "candidate_id": "candidate-1",
                    "element_ref": "el-1",
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
            ],
            "selected_candidate_id": "candidate-1",
            "binding_sha256": "b" * 64,
        }
        binding = TargetBinding.model_validate(payload)
        self.assertEqual(binding.selected_candidate_id, "candidate-1")

        payload["selected_candidate_id"] = "missing"
        with self.assertRaises(ValueError):
            TargetBinding.model_validate(payload)

    def test_large_raw_observation_is_sharded(self) -> None:
        nodes = [
            {
                "node_id": f"e{index}",
                "role": "text",
                "name": f"Row {index}",
                "dom": {
                    "tag": "div",
                    "attrs": {},
                    "textContent": "x" * 3000,
                },
            }
            for index in range(240)
        ]
        with tempfile.TemporaryDirectory() as directory:
            observation = build_browser_observation(
                url="https://example.test/table",
                title="Large table",
                state_id="table",
                revision=1,
                nodes=nodes,
                artifact_root=Path(directory),
            )
            artifact = observation["artifact"]
            manifest_path = (
                Path(directory)
                / "browser-observations"
                / f"{artifact['sha256']}.json.gz"
            )
            with gzip.open(manifest_path, "rt", encoding="utf-8") as stream:
                manifest = json.load(stream)

        self.assertLessEqual(len(observation["elements"]), 120)
        self.assertNotIn("raw_nodes", manifest)
        self.assertGreater(len(manifest["raw_node_shards"]), 1)
        self.assertTrue(
            all(
                shard["content_bytes"] <= 512 << 10
                for shard in manifest["raw_node_shards"]
            )
        )

    def test_locator_compiler_supports_structured_scope(self) -> None:
        page = MagicMock()
        row = MagicMock()
        page.get_by_role.return_value = row
        button = MagicMock()
        row.get_by_role.return_value = button

        locator = compile_locator(
            page,
            {
                "kind": "scoped",
                "scope": {
                    "kind": "role",
                    "role": "row",
                    "name": "Alice",
                    "exact": True,
                },
                "target": {
                    "kind": "role",
                    "role": "button",
                    "name": "Edit",
                    "exact": True,
                },
            },
        )

        self.assertIs(locator, button)
        page.get_by_role.assert_called_once_with(
            "row",
            exact=True,
            name="Alice",
        )
        row.get_by_role.assert_called_once_with(
            "button",
            exact=True,
            name="Edit",
        )

    def test_locator_contract_rejects_custom_string_kind(self) -> None:
        with self.assertRaises(ValueError):
            validate_locator_spec(
                {
                    "kind": "semantic_string",
                    "value": 'button="Save" inside "Dialog"',
                }
            )

    def test_locator_compiler_applies_frame_and_shadow_context(self) -> None:
        page = MagicMock()
        frame = MagicMock()
        host = MagicMock()
        target = MagicMock()
        page.frame_locator.return_value = frame
        frame.locator.return_value = host
        host.get_by_role.return_value = target

        result = compile_locator(
            page,
            {
                "kind": "role",
                "role": "button",
                "name": "Save",
                "exact": True,
            },
            context_path={
                "frames": ["iframe[name=editor]"],
                "shadow_hosts": ["settings-panel"],
            },
        )

        self.assertIs(result, target)
        page.frame_locator.assert_called_once_with("iframe[name=editor]")
        frame.locator.assert_called_once_with("settings-panel")
        host.get_by_role.assert_called_once_with(
            "button",
            exact=True,
            name="Save",
        )

    def test_observation_counts_candidates_in_their_context_path(self) -> None:
        page = MagicMock()
        frame = MagicMock()
        host = MagicMock()
        locator = MagicMock()
        locator.count.return_value = 1
        page.frame_locator.return_value = frame
        frame.locator.return_value = host
        host.get_by_role.return_value = locator

        observation = build_browser_observation(
            url="https://example.test/editor",
            title="Editor",
            state_id="editor",
            revision=1,
            probe_id="probe-editor",
            page=page,
            nodes=[
                {
                    "node_id": "save",
                    "role": "button",
                    "a11y_name": "Save",
                    "context_path": {
                        "frames": ["iframe[name=editor]"],
                        "shadow_hosts": ["settings-panel"],
                    },
                    "dom": {
                        "tag": "button",
                        "attrs": {},
                        "connected": True,
                        "visible": True,
                        "enabled": True,
                    },
                }
            ],
        )

        observed = observation["elements"][0]["locators"][0]
        self.assertEqual(observed["observed_count"], 1)
        self.assertTrue(observed["candidate_id"].startswith("candidate_"))
        page.frame_locator.assert_called_once_with("iframe[name=editor]")
        frame.locator.assert_called_once_with("settings-panel")

    def test_candidate_id_is_unique_across_probes(self) -> None:
        nodes = [
            {
                "node_id": "submit",
                "role": "button",
                "a11y_name": "Submit",
                "dom": {
                    "tag": "button",
                    "attrs": {"id": "submit"},
                    "connected": True,
                    "visible": True,
                    "enabled": True,
                },
            }
        ]
        first = build_browser_observation(
            url="https://example.test/form",
            title="Form",
            state_id="S0",
            revision=1,
            nodes=nodes,
            probe_id="probe-a",
        )
        second = build_browser_observation(
            url="https://example.test/form",
            title="Form",
            state_id="S0",
            revision=1,
            nodes=nodes,
            probe_id="probe-b",
        )

        self.assertNotEqual(
            first["elements"][0]["locators"][0]["candidate_id"],
            second["elements"][0]["locators"][0]["candidate_id"],
        )

    def test_grounding_query_produces_resolved_target_evidence(self) -> None:
        locator = {
            "kind": "placeholder",
            "value": "Search Product",
            "exact": True,
        }
        observation = build_browser_observation(
            url="https://example.test/products",
            title="Products",
            state_id="S0",
            revision=1,
            probe_id="probe-query",
            requested_locators=[locator],
            nodes=[
                {
                    "node_id": "search",
                    "role": "textbox",
                    "a11y_name": "Search Product",
                    "dom": {
                        "tag": "input",
                        "attrs": {
                            "id": "search_product",
                            "placeholder": "Search Product",
                        },
                        "connected": True,
                        "visible": True,
                        "enabled": True,
                    },
                }
            ],
        )

        resolved = build_resolved_target_evidence(
            observation,
            plan_step_id="input_search",
            step_index=0,
            action_index=0,
            action="input",
            locator_value=locator,
            action_status="succeeded",
        )

        self.assertIsNotNone(resolved)
        parsed = ResolvedTargetEvidence.model_validate(resolved)
        self.assertEqual(parsed.plan_step_id, "input_search")
        self.assertEqual(parsed.element_ref, "S0:search")
        self.assertEqual(parsed.locator.kind, "placeholder")
        self.assertEqual(parsed.runtime_match_count, 1)

    def test_target_binding_rejects_xpath_inside_shadow_root(self) -> None:
        payload = {
            "schema_version": "grounding.target-binding.v1",
            "binding_id": "binding-1",
            "plan_id": "plan-1",
            "plan_version": 1,
            "plan_step_id": "step-1",
            "probe_id": "probe-1",
            "semantic_target": "Shadow action",
            "action": "click",
            "page_state_id": "state-1",
            "observation_id": "obs-1",
            "observation_sha256": "a" * 64,
            "element_refs": ["el-1"],
            "candidates": [
                {
                    "candidate_id": "candidate-1",
                    "element_ref": "el-1",
                    "context_path": {
                        "frames": [],
                        "shadow_hosts": ["popup-info"],
                    },
                    "locator": {
                        "kind": "xpath",
                        "value": "//button",
                        "exact": True,
                    },
                    "provenance": "dom_verified",
                    "observed_count": 1,
                    "visible": True,
                    "enabled": True,
                    "score": 0.5,
                }
            ],
            "selected_candidate_id": "candidate-1",
            "binding_sha256": "b" * 64,
        }
        with self.assertRaises(ValueError):
            TargetBinding.model_validate(payload)


if __name__ == "__main__":
    unittest.main()
