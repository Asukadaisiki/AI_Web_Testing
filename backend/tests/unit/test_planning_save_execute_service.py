from __future__ import annotations

from types import SimpleNamespace

from sqlalchemy.orm import Session

from app.application.planning import save_execute_service
from app.application.planning.save_execute_service import (
    save_and_execute_selected_drafts_streaming,
)
from app.models import AIPlanningDraft, AIPlanningSession, SessionProject


class _RecordingEventLog:
    def __init__(self, **_kwargs) -> None:
        self.events: list[tuple[str, dict]] = []
        self.flushed = False

    def write(self, event_type: str, event: dict) -> None:
        self.events.append((event_type, event))

    def flush(self) -> None:
        self.flushed = True


def test_streaming_save_uses_event_log_write_contract(
    db_session: Session,
) -> None:
    planning_session = AIPlanningSession(
        actor_user_id=1,
        status="drafts_ready",
        requirements_json={},
        missing_slots_json=[],
    )
    db_session.add(planning_session)
    db_session.flush()
    db_session.add(
        SessionProject(
            session_id=planning_session.id,
            project_id=1,
        )
    )
    planning_session.active_project_id = 1
    draft = AIPlanningDraft(
        session_id=planning_session.id,
        scenario_key="invalid",
        title="Invalid draft",
        status="failed",
        dsl_case_json=None,
        error_message="DSL generation failed.",
    )
    db_session.add(draft)
    db_session.commit()

    event_log = _RecordingEventLog()
    events = list(
        save_and_execute_selected_drafts_streaming(
            db_session,
            planning_session.id,
            [draft.id],
            actor_user_id=1,
            event_log_factory=lambda **_kwargs: event_log,
        )
    )

    assert events == [
        {
            "type": "error",
            "message": "没有可保存的测试用例。DSL generation failed.",
            "error_type": "no_saved_cases",
            "phase": "execute",
        }
    ]
    assert event_log.events == [("error", events[0])]
    assert event_log.flushed is True


def test_streaming_save_injects_session_inputs_and_records_failures(
    db_session: Session,
    monkeypatch,
) -> None:
    planning_session = AIPlanningSession(
        actor_user_id=1,
        status="drafts_ready",
        requirements_json={"test_data_or_account": "用户名: fallback-user"},
        missing_slots_json=[],
    )
    db_session.add(planning_session)
    db_session.flush()
    db_session.add(SessionProject(session_id=planning_session.id, project_id=1))
    planning_session.active_project_id = 1
    draft = AIPlanningDraft(
        session_id=planning_session.id,
        scenario_key="login",
        title="Login",
        status="generated",
        dsl_case_json={
            "name": "Login",
            "base_url": "https://example.com",
            "input_contract": [
                {
                    "name": "Username",
                    "context_key": "username",
                    "value_type": "string",
                    "required": True,
                    "value": "contract-user",
                }
            ],
            "output_contract": [],
            "steps": [{"action": "goto", "value": "/"}],
        },
    )
    db_session.add(draft)
    db_session.commit()

    captured_input_values: dict[str, str] = {}

    def fake_execute_case_streaming(_session, _case_id, payload, **_kwargs):
        captured_input_values.update(payload.input_values)
        if False:
            yield None
        return SimpleNamespace(
            id=88,
            case_name="Login",
            status="failed",
            total_steps=1,
            duration_ms=10,
            latest_screenshot_url=None,
            report=SimpleNamespace(steps=[SimpleNamespace(status="failed")]),
        )

    recorded_failures: list[tuple[int, str, int]] = []
    monkeypatch.setattr(save_execute_service, "execute_case_streaming", fake_execute_case_streaming)
    monkeypatch.setattr(save_execute_service, "should_run_analysis", lambda _summaries: False)
    monkeypatch.setattr(
        save_execute_service,
        "_record_execution_anti_patterns",
        lambda _session, case_id, scenario_key, project_id: recorded_failures.append(
            (case_id, scenario_key, project_id)
        ),
    )

    events = list(
        save_and_execute_selected_drafts_streaming(
            db_session,
            planning_session.id,
            [draft.id],
            actor_user_id=1,
            event_log_factory=lambda **_kwargs: _RecordingEventLog(),
        )
    )

    assert captured_input_values == {"username": "contract-user"}
    case_start = next(event for event in events if event["type"] == "case_start")
    assert recorded_failures == [(case_start["case_id"], "login", 1)]
    assert events[-1] == {"type": "done"}
