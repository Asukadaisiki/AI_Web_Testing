"""Tests for URL generalization helpers."""

from __future__ import annotations

from app.locators.url_pattern import generalize_url


def test_generalize_url_preserves_and_sorts_query_string() -> None:
    assert (
        generalize_url("https://app.example.com/users/123/orders/456?token=abc123def456ghi7&tab=detail")
        == "https://app.example.com/users/*/orders/*?tab=detail&token=*"
    )


def test_generalize_url_keeps_long_alpha_segments_intact() -> None:
    assert generalize_url("https://app.example.com/checkoutconfirm") == "https://app.example.com/checkoutconfirm"


def test_generalize_url_generalizes_uuid_and_mixed_alphanumeric_segments() -> None:
    assert (
        generalize_url("https://app.example.com/items/550e8400-e29b-41d4-a716-446655440000/AB12CD34EF56GH78")
        == "https://app.example.com/items/*/*"
    )
