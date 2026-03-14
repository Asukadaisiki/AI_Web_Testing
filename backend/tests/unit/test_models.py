"""Tests for ORM model metadata."""

from __future__ import annotations

from sqlalchemy import inspect
from sqlalchemy.orm import Session


def test_stage1_tables_exist(db_session: Session) -> None:
    inspector = inspect(db_session.bind)

    assert set(inspector.get_table_names()) == {
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
