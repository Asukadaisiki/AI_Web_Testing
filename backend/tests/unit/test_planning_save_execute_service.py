from __future__ import annotations

from sqlalchemy.orm import Session

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
