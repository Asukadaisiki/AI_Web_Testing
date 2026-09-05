from __future__ import annotations

import json
import unittest

from app.core.request_logging import _format_json_body, _redact_sensitive


class RequestLoggingRedactionTest(unittest.TestCase):
    def test_redacts_nested_credentials_without_changing_other_fields(self) -> None:
        payload = {
            "email": "owner@example.com",
            "password": "plain-text",
            "settings": {
                "AI_PLANNING_API_KEY": "secret-key",
                "access_token": "token-value",
                "model": "test-model",
            },
            "items": [{"client_secret": "secret"}, {"name": "visible"}],
        }

        redacted = _redact_sensitive(payload)

        self.assertEqual(redacted["email"], "owner@example.com")
        self.assertEqual(redacted["password"], "***")
        self.assertEqual(redacted["settings"]["AI_PLANNING_API_KEY"], "***")
        self.assertEqual(redacted["settings"]["access_token"], "***")
        self.assertEqual(redacted["settings"]["model"], "test-model")
        self.assertEqual(redacted["items"][0]["client_secret"], "***")
        self.assertEqual(redacted["items"][1]["name"], "visible")

    def test_formats_login_json_without_password_value(self) -> None:
        formatted = _format_json_body(
            b'{"email":"owner@example.com","password":"do-not-log"}'
        )

        self.assertNotIn("do-not-log", formatted)
        self.assertEqual(
            json.loads(formatted),
            {"email": "owner@example.com", "password": "***"},
        )


if __name__ == "__main__":
    unittest.main()
