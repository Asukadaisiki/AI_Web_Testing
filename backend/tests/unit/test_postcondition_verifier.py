"""Unit tests for PostconditionVerifier."""
from app.runners.postcondition_verifier import (
    PostconditionResult,
    verify_default_postcondition,
)


def test_postcondition_result_passed():
    r = PostconditionResult(passed=True, details={})
    assert r.passed is True


def test_postcondition_result_failed():
    r = PostconditionResult(passed=False, details={"url_contains": "expected '/success' but url was '/checkout'"})
    assert r.passed is False


def test_verify_default_postcondition_url_changed():
    pre = {"url": "https://example.com/checkout"}
    post = {"url": "https://example.com/success"}
    assert verify_default_postcondition(pre, post) is True


def test_verify_default_postcondition_dom_changed():
    pre = {"url": "https://example.com/page", "dom_hash": "abc123"}
    post = {"url": "https://example.com/page", "dom_hash": "def456"}
    assert verify_default_postcondition(pre, post) is True


def test_verify_default_postcondition_no_change():
    pre = {"url": "https://example.com/page", "dom_hash": "abc123"}
    post = {"url": "https://example.com/page", "dom_hash": "abc123"}
    assert verify_default_postcondition(pre, post) is False
