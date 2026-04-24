"""Unit tests for _substitute_variables in playwright_runner."""
from __future__ import annotations

from app.runners.playwright_runner import _substitute_variables


class TestSubstituteVariables:
    def test_substitutes_single_variable(self) -> None:
        result = _substitute_variables("${login_email}", {"login_email": "test@example.com"})
        assert result == "test@example.com"

    def test_substitutes_multiple_variables(self) -> None:
        result = _substitute_variables(
            "${login_email}:${login_password}",
            {"login_email": "user@test.com", "login_password": "secret123"},
        )
        assert result == "user@test.com:secret123"

    def test_leaves_unknown_variable_unchanged(self) -> None:
        result = _substitute_variables("${unknown_key}", {"login_email": "a@b.com"})
        assert result == "${unknown_key}"

    def test_handles_empty_input_values(self) -> None:
        result = _substitute_variables("${login_email}", {})
        assert result == "${login_email}"

    def test_handles_none_input_values(self) -> None:
        result = _substitute_variables("${login_email}", None)
        assert result == "${login_email}"

    def test_handles_none_value(self) -> None:
        result = _substitute_variables(None, {"key": "val"})
        assert result is None

    def test_handles_empty_value(self) -> None:
        result = _substitute_variables("", {"key": "val"})
        assert result == ""

    def test_adjacent_variables(self) -> None:
        result = _substitute_variables("${a}${b}", {"a": "X", "b": "Y"})
        assert result == "XY"

    def test_mixed_text_and_variables(self) -> None:
        result = _substitute_variables(
            "user=${user}&pass=${pass}",
            {"user": "admin", "pass": "123"},
        )
        assert result == "user=admin&pass=123"

    def test_only_matches_valid_context_key_pattern(self) -> None:
        result = _substitute_variables("${123invalid}", {"123invalid": "val"})
        assert result == "${123invalid}"

    def test_underscore_prefix_allowed(self) -> None:
        result = _substitute_variables("${_key}", {"_key": "val"})
        assert result == "val"
