"""Browser integration tests for the local intervention regression flow."""

from __future__ import annotations

import shutil
from pathlib import Path

import pytest

import app.runners.playwright_runner as playwright_runner
from tests.fixtures.site_server import serve_static_fixture


pytestmark = pytest.mark.browser_integration


def _ensure_chromium_available() -> None:
    try:
        from playwright.sync_api import sync_playwright
    except ImportError as exc:  # pragma: no cover - dependency is installed in normal dev flow
        pytest.skip(f"Playwright is not installed: {exc}")

    try:
        with sync_playwright() as playwright:
            browser = playwright.chromium.launch(headless=True)
            browser.close()
    except Exception as exc:  # pragma: no cover - depends on local browser install state
        pytest.skip(
            "Chromium browser is not installed. Run `uv run playwright install chromium` in backend/ first. "
            f"Original error: {exc}"
        )


@pytest.fixture
def isolated_artifacts_root(monkeypatch: pytest.MonkeyPatch) -> Path:
    root = Path(__file__).resolve().parents[2] / "artifacts" / "test-executions"
    if root.exists():
        shutil.rmtree(root)
    monkeypatch.setattr(playwright_runner, "ARTIFACTS_ROOT", root)
    yield root
    if root.exists():
        shutil.rmtree(root)


def test_local_intervention_flow_rerun_hits_tier_zero(client, isolated_artifacts_root: Path) -> None:
    _ensure_chromium_available()

    with serve_static_fixture("intervention_flow") as base_url:
        create_response = client.post(
            "/api/v1/cases",
            json={
                "project_id": 1,
                "actor_user_id": 1,
                "name": "本地人工干预闭环",
                "base_url": base_url,
                "steps": [
                    {"action": "goto", "value": "/"},
                    {"action": "click", "target": "handoff trigger"},
                    {"action": "assert_text", "target": "#result-message", "value": "Intervention flow finished"},
                ],
            },
        )
        assert create_response.status_code == 201
        case_id = create_response.json()["id"]

        first_execution = client.post(f"/api/v1/cases/{case_id}/execute", json={"actor_user_id": 1})
        assert first_execution.status_code == 200
        first_payload = first_execution.json()
        assert first_payload["status"] == "needs_intervention"
        assert first_payload["failed_step_index"] == 1
        assert first_payload["report"]["steps"][1]["status"] == "failed"
        assert first_payload["report"]["steps"][1]["intervention_request"] is not None
        assert first_payload["report"]["steps"][1]["intervention_request"]["target_description"] == "handoff trigger"
        page_url = first_payload["report"]["steps"][1]["intervention_request"]["page_url"]
        assert page_url.startswith(base_url)

        create_correction = client.post(
            "/api/v1/corrections",
            json={
                "page_url": page_url,
                "target_description": "handoff trigger",
                "correction_type": "test_id",
                "correction_value": "fixture-primary-action",
                "source_execution_id": first_payload["id"],
                "created_by": 1,
            },
        )
        assert create_correction.status_code == 201
        assert create_correction.json()["verified_count"] == 0
        assert create_correction.json()["is_active"] is True

        second_execution = client.post(f"/api/v1/cases/{case_id}/execute", json={"actor_user_id": 1})
        assert second_execution.status_code == 200
        second_payload = second_execution.json()
        assert second_payload["status"] == "passed"
        assert second_payload["failed_step_index"] is None
        assert second_payload["latest_url"] == f"{base_url}/success.html"
        assert second_payload["report"]["steps"][1]["status"] == "passed"
        assert second_payload["report"]["steps"][1]["resolved_by"] == "correction:test_id"
        assert second_payload["report"]["steps"][2]["status"] == "passed"

        corrections = client.get(
            "/api/v1/corrections",
            params={"target_description": "handoff trigger", "page_url": page_url},
        )
        assert corrections.status_code == 200
        assert len(corrections.json()) == 1
        stored_correction = corrections.json()[0]
        assert stored_correction["target_description"] == "handoff trigger"
        assert stored_correction["correction_type"] == "test_id"
        assert stored_correction["correction_value"] == "fixture-primary-action"
        assert stored_correction["verified_count"] == 1
        assert stored_correction["consecutive_failures"] == 0
        assert stored_correction["is_active"] is True


def test_local_intervention_flow_auto_disables_invalid_correction_after_three_failures(
    client,
    isolated_artifacts_root: Path,
) -> None:
    _ensure_chromium_available()

    with serve_static_fixture("intervention_flow") as base_url:
        create_response = client.post(
            "/api/v1/cases",
            json={
                "project_id": 1,
                "actor_user_id": 1,
                "name": "本地修正自动停用",
                "base_url": base_url,
                "steps": [
                    {"action": "goto", "value": "/"},
                    {"action": "click", "target": "handoff trigger"},
                ],
            },
        )
        assert create_response.status_code == 201
        case_id = create_response.json()["id"]

        first_execution = client.post(f"/api/v1/cases/{case_id}/execute", json={"actor_user_id": 1})
        assert first_execution.status_code == 200
        page_url = first_execution.json()["report"]["steps"][1]["intervention_request"]["page_url"]

        create_correction = client.post(
            "/api/v1/corrections",
            json={
                "page_url": page_url,
                "target_description": "handoff trigger",
                "correction_type": "test_id",
                "correction_value": "missing-primary-action",
                "source_execution_id": first_execution.json()["id"],
                "created_by": 1,
            },
        )
        assert create_correction.status_code == 201
        correction_id = create_correction.json()["id"]

        for attempt in range(3):
            execution_response = client.post(f"/api/v1/cases/{case_id}/execute", json={"actor_user_id": 1})
            assert execution_response.status_code == 200
            payload = execution_response.json()
            assert payload["status"] == "needs_intervention"
            assert payload["report"]["steps"][1]["status"] == "failed"
            if attempt < 2:
                assert payload["report"]["steps"][1]["resolved_by"] is None

        events = client.get(f"/api/v1/corrections/{correction_id}/events")
        assert events.status_code == 200
        assert [item["event_type"] for item in events.json()[:4]] == [
            "auto_deactivated",
            "tier0_miss",
            "tier0_miss",
            "tier0_miss",
        ]

        corrections = client.get(
            "/api/v1/corrections",
            params={"target_description": "handoff trigger", "page_url": page_url},
        )
        assert corrections.status_code == 200
        stored_correction = corrections.json()[0]
        assert stored_correction["id"] == correction_id
        assert stored_correction["consecutive_failures"] == 3
        assert stored_correction["is_active"] is False
