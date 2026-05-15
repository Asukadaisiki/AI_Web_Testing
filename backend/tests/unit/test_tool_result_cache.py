import pytest
from datetime import datetime, timedelta, UTC
from sqlalchemy import create_engine
from sqlalchemy.orm import Session

from app.db.base import Base
from app.models.ai_planning_tool_result import AIPlanningToolResult
from app.services.ai_planning import _lookup_tool_cache, _normalize_cache_url


@pytest.fixture
def db_session():
    engine = create_engine("sqlite:///:memory:")
    Base.metadata.create_all(engine)
    with Session(engine) as s:
        yield s


def test_normalize_strips_tracking_params():
    assert _normalize_cache_url(
        "https://example.com/page?utm_source=fb&id=5"
    ) == "https://example.com/page?id=5"


def test_normalize_strips_ref_and_fragment():
    assert _normalize_cache_url(
        "https://example.com/?ref=homepage&_t=123#section"
    ) == "https://example.com/"


def test_cross_session_not_hit(db_session):
    record = AIPlanningToolResult(
        session_id=1, tool_name="explore_page",
        raw_result_json={"url": "https://example.com/"},
        summary_json={"urls": ["https://example.com/"]},
    )
    db_session.add(record)
    db_session.flush()

    key = ("explore_page", 2, "https://example.com/", 1280, 720, "abc123")
    result = _lookup_tool_cache(db_session, key, ttl_hours=4)
    assert result is None
