"""Tests for DSL validation endpoint."""

from __future__ import annotations

import json
from types import SimpleNamespace

import pytest

import app.core.config as config_module
from app.models import DslGenerationRun, TestCase, User
from app.ai.dsl_generator import AI_DSL_PROMPT_VERSION
from app.core.auth import hash_password
from app.core.config import get_settings
from app.schemas.dsl import GenerateDslRequest
from app.services import dsl as dsl_service


def test_validate_dsl_case_success(client) -> None:
    response = client.post(
        "/api/v1/dsl/validate",
        json={
            "name": "登录冒烟",
            "base_url": "https://example.com",
            "input_contract": [
                {
                    "name": "username",
                    "context_key": "login_username",
                    "value_type": "string",
                    "required": True,
                }
            ],
            "output_contract": [
                {
                    "name": "sessionToken",
                    "context_key": "session_token",
                    "value_type": "string",
                    "source": "latest_url",
                }
            ],
            "steps": [
                {"action": "goto", "value": "/login"},
                {"action": "input", "target": "用户名输入框", "value": "admin"},
                {"action": "click", "target": "登录按钮"},
                {"action": "assert_url_contains", "value": "/dashboard"},
            ],
        },
    )

    assert response.status_code == 200
    assert response.json() == {
        "valid": True,
        "case": {
            "name": "登录冒烟",
            "description": None,
            "base_url": "https://example.com",
            "input_contract": [
                {
                    "name": "username",
                    "context_key": "login_username",
                    "value_type": "string",
                    "required": True,
                    "description": None,
                    "value": None,
                }
            ],
            "output_contract": [
                {
                    "name": "sessionToken",
                    "context_key": "session_token",
                    "value_type": "string",
                    "source": "latest_url",
                    "description": None,
                }
            ],
            "steps": [
                {"action": "goto", "value": "/login"},
                {"action": "input", "target": "用户名输入框", "value": "admin", "trigger": None, "page_state": None, "target_strategy": None, "locator_confidence": None, "candidates": [], "postconditions": []},
                {"action": "click", "target": "登录按钮", "page_state": None, "target_strategy": None, "locator_confidence": None, "candidates": [], "postconditions": []},
                {"action": "assert_url_contains", "value": "/dashboard"},
            ],
        },
        "supported_actions": [
            "goto",
            "click",
            "input",
            "wait_for",
            "assert_text",
            "assert_url_contains",
            "capture_text",
        ],
    }


def test_validate_dsl_case_rejects_invalid_payload(client) -> None:
    response = client.post(
        "/api/v1/dsl/validate",
        json={
            "name": "非法 DSL",
            "steps": [
                {"action": "click", "value": "缺少 target"},
            ],
        },
    )

    assert response.status_code == 422


def test_locator_candidate_valid():
    from app.schemas.dsl import LocatorCandidate
    cand = LocatorCandidate(strategy="role", selector="getByRole('button')", pre_score=0.87)
    assert cand.strategy == "role"
    assert cand.pre_score == 0.87
    assert cand.selector is not None


def test_locator_candidate_vlm_no_selector():
    from app.schemas.dsl import LocatorCandidate
    cand = LocatorCandidate(strategy="vlm", semantic_value="checkout button", pre_score=0.0)
    assert cand.selector is None
    assert cand.semantic_value == "checkout button"


def test_postcondition_url_contains():
    from app.schemas.dsl import Postcondition
    pc = Postcondition(type="url_contains", value="/success")
    assert pc.type == "url_contains"
    assert pc.timeout_ms == 3000


def test_click_step_with_candidates_and_postconditions():
    from app.schemas.dsl import ClickStep
    step = ClickStep(
        action="click",
        target="Submit",
        candidates=[
            {"strategy": "role", "selector": "getByRole('button', {name: 'Submit'})", "pre_score": 0.9},
            {"strategy": "vlm", "semantic_value": "Submit button", "pre_score": 0.0},
        ],
        postconditions=[
            {"type": "url_contains", "value": "/success"},
        ],
    )
    assert len(step.candidates) == 2
    assert len(step.postconditions) == 1


def test_click_step_backward_compatible():
    from app.schemas.dsl import ClickStep
    step = ClickStep(action="click", target="Submit")
    assert step.candidates == []
    assert step.postconditions == []
