"""Tests for the public DSL service import surface."""

from __future__ import annotations

from app.services import dsl


def test_dsl_service_all_exports_exist() -> None:
    assert "delete_dsl_generation_run" in dsl.__all__
    assert "get_dsl_generation_runtime_stats" not in dsl.__all__
    assert all(hasattr(dsl, name) for name in dsl.__all__)
