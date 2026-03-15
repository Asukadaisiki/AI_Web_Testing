"""Tests for ORM model metadata."""

from __future__ import annotations

import pytest
from sqlalchemy import inspect
from sqlalchemy.exc import IntegrityError
from sqlalchemy.orm import Session

from app.models import LocatorCorrection, LocatorCorrectionEvent, TestCase, TestCaseRun


def test_stage1_tables_exist(db_session: Session) -> None:
    inspector = inspect(db_session.bind)

    assert set(inspector.get_table_names()) == {
        "locator_correction_events",
        "locator_corrections",
        "project_members",
        "projects",
        "suite_cases",
        "suite_run_items",
        "suite_runs",
        "test_cases",
        "test_case_runs",
        "test_suites",
        "users",
    }


def test_test_case_foreign_keys_are_declared(db_session: Session) -> None:
    inspector = inspect(db_session.bind)
    foreign_keys = inspector.get_foreign_keys("test_cases")

    assert {foreign_key["referred_table"] for foreign_key in foreign_keys} == {
        "projects",
        "users",
    }


def test_suite_case_supports_ordering_relation(db_session: Session) -> None:
    inspector = inspect(db_session.bind)
    unique_constraints = inspector.get_unique_constraints("suite_cases")

    assert any(
        constraint["column_names"] == ["suite_id", "order_index"]
        for constraint in unique_constraints
    )


def test_test_case_run_foreign_keys_are_declared(db_session: Session) -> None:
    inspector = inspect(db_session.bind)
    foreign_keys = inspector.get_foreign_keys("test_case_runs")

    assert {foreign_key["referred_table"] for foreign_key in foreign_keys} == {
        "projects",
        "test_cases",
        "users",
    }


def test_suite_run_tables_foreign_keys_are_declared(db_session: Session) -> None:
    inspector = inspect(db_session.bind)

    suite_run_foreign_keys = inspector.get_foreign_keys("suite_runs")
    assert {foreign_key["referred_table"] for foreign_key in suite_run_foreign_keys} == {
        "suite_runs",
        "test_suites",
        "users",
    }

    suite_run_item_foreign_keys = inspector.get_foreign_keys("suite_run_items")
    assert {foreign_key["referred_table"] for foreign_key in suite_run_item_foreign_keys} == {
        "suite_runs",
        "test_case_runs",
        "test_cases",
    }


def test_suite_context_columns_exist(db_session: Session) -> None:
    inspector = inspect(db_session.bind)

    suite_run_columns = {column["name"] for column in inspector.get_columns("suite_runs")}
    assert {
        "context_source",
        "context_source_suite_run_id",
        "rerun_context_mode",
        "context_snapshot",
    }.issubset(suite_run_columns)

    suite_run_item_columns = {column["name"] for column in inspector.get_columns("suite_run_items")}
    assert {
        "context_reads",
        "context_writes",
        "context_resolution_error",
    }.issubset(suite_run_item_columns)


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
    case = TestCase(
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

    execution = TestCaseRun(
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
    case = TestCase(
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

    execution = TestCaseRun(
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
