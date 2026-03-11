"""Tests for ORM model metadata."""

from __future__ import annotations

from sqlalchemy import inspect
from sqlalchemy.orm import Session


def test_stage1_tables_exist(db_session: Session) -> None:
    inspector = inspect(db_session.bind)

    assert set(inspector.get_table_names()) == {
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
