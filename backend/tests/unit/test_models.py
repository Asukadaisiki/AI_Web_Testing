"""Tests for ORM model metadata."""

from __future__ import annotations

import pytest
from sqlalchemy import inspect
from sqlalchemy.exc import IntegrityError
from sqlalchemy.orm import Session

from app.models import (
    LocatorCorrection,
    LocatorCorrectionEvent,
    TestCase as CaseModel,
    TestCaseRun as CaseRunModel,
)


def test_stage1_tables_exist(db_session: Session) -> None:
    inspector = inspect(db_session.bind)

    assert set(inspector.get_table_names()) == {
        "ai_planning_drafts",
        "ai_planning_flow_steps",
        "ai_planning_messages",
        "ai_planning_sessions",
        "ai_planning_tool_results",
        "dsl_anti_patterns",
        "dsl_generation_runs",
        "locator_attempt_logs",
        "locator_correction_events",
        "locator_corrections",
        "project_members",
        "projects",
        "report_preferences",
        "session_projects",
        "test_cases",
        "test_case_runs",
        "test_point_insights",
        "users",
    }


def test_test_case_foreign_keys_are_declared(db_session: Session) -> None:
    inspector = inspect(db_session.bind)
    foreign_keys = inspector.get_foreign_keys("test_cases")

    assert {foreign_key["referred_table"] for foreign_key in foreign_keys} == {
        "projects",
        "users",
    }


def test_test_case_run_foreign_keys_are_declared(db_session: Session) -> None:
    inspector = inspect(db_session.bind)
    foreign_keys = inspector.get_foreign_keys("test_case_runs")

    assert {foreign_key["referred_table"] for foreign_key in foreign_keys} == {
        "projects",
        "test_cases",
        "users",
    }


def test_suite_tables_are_not_created(db_session: Session) -> None:
    inspector = inspect(db_session.bind)
    table_names = set(inspector.get_table_names())

    assert "test_suites" not in table_names
    assert "suite_cases" not in table_names
    assert "suite_runs" not in table_names
    assert "suite_run_items" not in table_names


def test_locator_corrections_columns_and_foreign_keys_exist(db_session: Session) -> None:
    inspector = inspect(db_session.bind)

    correction_columns = {column["name"] for column in inspector.get_columns("locator_corrections")}
    assert {
        "page_url_pattern",
        "target_description",
        "normalized_target_description",
        "correction_type",
        "correction_value",
        "verified_count",
        "consecutive_failures",
        "is_active",
        "source_execution_id",
        "created_by",
    }.issubset(correction_columns)

    foreign_keys = inspector.get_foreign_keys("locator_corrections")
    assert {foreign_key["referred_table"] for foreign_key in foreign_keys} == {
        "test_case_runs",
        "users",
    }

    correction_indexes = {index["name"] for index in inspector.get_indexes("locator_corrections")}
    assert {"ix_locator_corrections_lookup", "uq_locator_corrections_active_lookup"}.issubset(correction_indexes)


def test_locator_corrections_unique_active_lookup_index_enforced(db_session: Session) -> None:
    case = CaseModel(
        project_id=1,
        created_by=1,
        updated_by=1,
        name="index case",
        description=None,
        dsl={"name": "index case", "steps": [{"action": "click", "target": "submit"}]},
    )
    db_session.add(case)
    db_session.commit()
    db_session.refresh(case)

    execution = CaseRunModel(
        case_id=case.id,
        project_id=1,
        triggered_by=1,
        status="failed",
        error_message="boom",
    )
    db_session.add(execution)
    db_session.commit()
    db_session.refresh(execution)

    db_session.add(
        LocatorCorrection(
            page_url_pattern="https://app.example.com/orders/*",
            target_description="Submit",
            normalized_target_description="submit",
            correction_type="css",
            correction_value="#submit-primary",
            source_execution_id=execution.id,
            created_by=1,
        )
    )
    db_session.commit()

    db_session.add(
        LocatorCorrection(
            page_url_pattern="https://app.example.com/orders/*",
            target_description="submit",
            normalized_target_description="submit",
            correction_type="xpath",
            correction_value="//button[@id='submit-secondary']",
            source_execution_id=execution.id,
            created_by=1,
        )
    )

    with pytest.raises(IntegrityError):
        db_session.commit()


def test_locator_correction_events_columns_and_foreign_keys_exist(db_session: Session) -> None:
    inspector = inspect(db_session.bind)

    event_columns = {column["name"] for column in inspector.get_columns("locator_correction_events")}
    assert {
        "correction_id",
        "event_type",
        "page_url_pattern",
        "target_description",
        "execution_id",
        "verified_count_after",
        "consecutive_failures_after",
        "is_active_after",
        "created_at",
    }.issubset(event_columns)

    foreign_keys = inspector.get_foreign_keys("locator_correction_events")
    assert {foreign_key["referred_table"] for foreign_key in foreign_keys} == {
        "locator_corrections",
        "test_case_runs",
    }


def test_locator_correction_event_persists_snapshot_fields(db_session: Session) -> None:
    case = CaseModel(
        project_id=1,
        created_by=1,
        updated_by=1,
        name="event case",
        description=None,
        dsl={"name": "event case", "steps": [{"action": "click", "target": "submit"}]},
    )
    db_session.add(case)
    db_session.commit()
    db_session.refresh(case)

    execution = CaseRunModel(
        case_id=case.id,
        project_id=1,
        triggered_by=1,
        status="failed",
        error_message="boom",
    )
    db_session.add(execution)
    db_session.commit()
    db_session.refresh(execution)

    correction = LocatorCorrection(
        page_url_pattern="https://app.example.com/orders/*",
        target_description="Submit",
        normalized_target_description="submit",
        correction_type="css",
        correction_value="#submit-primary",
        source_execution_id=execution.id,
        created_by=1,
    )
    db_session.add(correction)
    db_session.commit()
    db_session.refresh(correction)

    event = LocatorCorrectionEvent(
        correction_id=correction.id,
        event_type="created",
        page_url_pattern=correction.page_url_pattern,
        target_description=correction.target_description,
        execution_id=execution.id,
        verified_count_after=0,
        consecutive_failures_after=0,
        is_active_after=True,
    )
    db_session.add(event)
    db_session.commit()

    persisted = db_session.get(LocatorCorrectionEvent, event.id)
    assert persisted is not None
    assert persisted.page_url_pattern == "https://app.example.com/orders/*"
    assert persisted.target_description == "Submit"
    assert persisted.execution_id == execution.id


def test_dsl_generation_run_columns_and_foreign_keys_exist(db_session: Session) -> None:
    inspector = inspect(db_session.bind)

    columns = {column["name"] for column in inspector.get_columns("dsl_generation_runs")}
    assert {
        "actor_user_id",
        "project_id",
        "case_id",
        "prompt_preview",
        "prompt_sha256",
        "prompt_version",
        "prompt_variant",
        "retry_from_generation_id",
        "retry_reason_code",
        "retry_note",
        "request_base_url",
        "generation_mode",
        "import_mode",
        "model_name",
        "success",
        "error_type",
        "error_message",
        "used_current_case_context",
        "used_current_steps_context",
        "context_profile",
        "base_url_source",
        "base_url_backfilled",
        "repaired_invalid_actions",
        "removed_invalid_steps",
        "removed_invalid_contracts",
        "preserve_contracts_requested",
        "preserve_contracts_applied",
        "warnings_count",
        "normalization_notes_count",
        "warnings_json",
        "normalization_notes_json",
        "governance_focus_reasons_json",
        "risk_flags_json",
        "generated_case_json",
        "feedback_status",
        "feedback_import_mode",
        "rejection_reason_code",
        "feedback_note",
        "feedback_recorded_at",
        "created_at",
    }.issubset(columns)

    foreign_keys = inspector.get_foreign_keys("dsl_generation_runs")
    assert {foreign_key["referred_table"] for foreign_key in foreign_keys} == {
        "users",
        "projects",
        "test_cases",
        "dsl_generation_runs",
    }
