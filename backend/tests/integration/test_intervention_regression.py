"""Browser integration tests for the local intervention regression flow."""

from __future__ import annotations

import shutil
from pathlib import Path

import pytest

import app.runners.playwright_runner as playwright_runner
from app.locators.fallback import resolve_with_fallback
from app.locators.semantic import LocatorResolutionError
from app.schemas.executions import DOMElementSnapshot, LocatorCandidateEvidence, LocatorTrace
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


def _create_case(client, *, name: str, base_url: str, steps: list[dict], input_contract=None, output_contract=None) -> int:
    response = client.post(
        "/api/v1/cases",
        json={
            "project_id": 1,
            "actor_user_id": 1,
            "name": name,
            "base_url": base_url,
            "input_contract": input_contract or [],
            "output_contract": output_contract or [],
            "steps": steps,
        },
    )
    assert response.status_code == 201
    return response.json()["id"]


def test_local_single_case_smoke_executes_successfully(client, isolated_artifacts_root: Path) -> None:
    _ensure_chromium_available()

    with serve_static_fixture("intervention_flow") as base_url:
        case_id = _create_case(
            client,
            name="本地单 Case 冒烟",
            base_url=base_url,
            steps=[
                {"action": "goto", "value": "/success.html"},
                {"action": "assert_url_contains", "value": "success.html"},
            ],
        )

        execution_response = client.post(f"/api/v1/cases/{case_id}/execute", json={"actor_user_id": 1})

        assert execution_response.status_code == 200
        payload = execution_response.json()
        assert payload["status"] == "passed"
        assert payload["failed_step_index"] is None
        assert payload["latest_url"] == f"{base_url}/success.html"
        assert payload["report"]["steps"][0]["status"] == "passed"
        assert payload["report"]["steps"][1]["status"] == "passed"
        assert payload["latest_screenshot_url"] is not None


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




def test_browser_dom_candidates_can_be_reranked_by_vlm(monkeypatch: pytest.MonkeyPatch) -> None:
    _ensure_chromium_available()

    from playwright.sync_api import sync_playwright

    close_candidates = [
        LocatorCandidateEvidence(
            strategy="button_text",
            preview_text="提交",
            role="button",
            score=92,
            matched_rules=["text:partial"],
            visible=True,
            enabled=True,
        ),
        LocatorCandidateEvidence(
            strategy="button_text",
            preview_text="提交订单",
            role="button",
            score=89,
            matched_rules=["text:near"],
            visible=True,
            enabled=True,
        ),
    ]

    def fake_try_semantic_candidates(*_args, **_kwargs):
        raise LocatorResolutionError(
            "close candidates need rerank",
            trace=LocatorTrace(target="提交订单", candidates=close_candidates),
        )

    monkeypatch.setattr("app.locators.fallback._try_semantic_candidates_in_order", fake_try_semantic_candidates)
    monkeypatch.setattr("app.locators.fallback._take_screenshot_base64", lambda _page: "ignored")
    monkeypatch.setattr("app.locators.fallback.rank_candidates_by_vision", lambda **_kwargs: 1)

    with sync_playwright() as playwright:
        browser = playwright.chromium.launch(headless=True)
        page = browser.new_page()
        page.set_content(
            """
            <main>
              <button id="submit-basic" aria-label="提交">提交</button>
              <button id="submit-order" aria-label="提交订单">提交订单</button>
            </main>
            """
        )

        candidate_entries = [
            {
                "locator": page.locator("#submit-basic"),
                "candidate": close_candidates[0],
                "snapshot": DOMElementSnapshot(
                    tag="button",
                    text="提交",
                    role="button",
                    aria_label="提交",
                    css_selector="#submit-basic",
                    xpath='//*[@id="submit-basic"]',
                    rect={"x": 0, "y": 0, "width": 100, "height": 32},
                    visible=True,
                    enabled=True,
                ),
            },
            {
                "locator": page.locator("#submit-order"),
                "candidate": close_candidates[1],
                "snapshot": DOMElementSnapshot(
                    tag="button",
                    text="提交订单",
                    role="button",
                    aria_label="提交订单",
                    css_selector="#submit-order",
                    xpath='//*[@id="submit-order"]',
                    rect={"x": 0, "y": 40, "width": 120, "height": 32},
                    visible=True,
                    enabled=True,
                ),
            },
        ]
        monkeypatch.setattr("app.locators.fallback._collect_rankable_semantic_candidates", lambda *_args, **_kwargs: candidate_entries)

        resolved = resolve_with_fallback(page, "提交订单")

        assert resolved.strategy == "semantic_vlm_rank"
        assert resolved.trace.match_strategy == "semantic_vlm_rank"
        assert resolved.locator.text_content() == "提交订单"
        browser.close()
