from __future__ import annotations

import unittest
from unittest.mock import MagicMock

from fastapi import HTTPException
from fastapi.routing import APIRoute
from starlette.requests import Request

from app.api.auth import require_authenticated_user
from app.api.capability_auth import require_capability_access
from app.api.router import build_api_router
from app.api.routes.artifacts import router as artifacts_router
from app.models import User


def _request(session_data: dict[str, object]) -> Request:
    request = Request({"type": "http", "method": "GET", "path": "/"})
    request.scope["session"] = session_data
    return request


class SessionAuthenticationTest(unittest.TestCase):
    def test_missing_session_is_unauthorized(self) -> None:
        database = MagicMock()

        with self.assertRaises(HTTPException) as raised:
            require_authenticated_user(_request({}), database)

        self.assertEqual(raised.exception.status_code, 401)
        database.get.assert_not_called()

    def test_active_session_returns_database_user(self) -> None:
        database = MagicMock()
        user = User(
            id=7,
            email="owner@example.com",
            display_name="Owner",
            password_hash="unused",
            is_active=True,
        )
        database.get.return_value = user

        result = require_authenticated_user(_request({"user_id": 7}), database)

        self.assertIs(result, user)
        database.get.assert_called_once_with(User, 7)

    def test_inactive_user_clears_session_and_is_forbidden(self) -> None:
        database = MagicMock()
        database.get.return_value = User(
            id=7,
            email="owner@example.com",
            display_name="Owner",
            password_hash="unused",
            is_active=False,
        )
        session_data = {"user_id": 7}

        with self.assertRaises(HTTPException) as raised:
            require_authenticated_user(_request(session_data), database)

        self.assertEqual(raised.exception.status_code, 403)
        self.assertEqual(session_data, {})


class CapabilityAuthorizationTest(unittest.TestCase):
    def setUp(self) -> None:
        self.user = User(
            id=7,
            email="owner@example.com",
            display_name="Owner",
            password_hash="unused",
            is_active=True,
        )

    def test_project_member_can_use_owned_planning_context(self) -> None:
        database = MagicMock()
        database.scalar.side_effect = [1, 11]

        require_capability_access(
            database,
            self.user,
            project_id=3,
            conversation_id="11",
        )

        self.assertEqual(database.scalar.call_count, 2)

    def test_non_member_is_forbidden(self) -> None:
        database = MagicMock()
        database.scalar.return_value = None

        with self.assertRaises(HTTPException) as raised:
            require_capability_access(
                database,
                self.user,
                project_id=3,
                conversation_id="11",
            )

        self.assertEqual(raised.exception.status_code, 403)

    def test_foreign_planning_context_is_forbidden(self) -> None:
        database = MagicMock()
        database.scalar.side_effect = [1, None]

        with self.assertRaises(HTTPException) as raised:
            require_capability_access(
                database,
                self.user,
                project_id=3,
                conversation_id="11",
            )

        self.assertEqual(raised.exception.status_code, 403)


class ProtectedRouterTest(unittest.TestCase):
    def test_business_and_artifact_routes_require_authentication(self) -> None:
        routes = [
            route
            for route in [*build_api_router().routes, *artifacts_router.routes]
            if isinstance(route, APIRoute)
        ]
        public_paths = {
            "/api/v1/health",
            "/api/v1/auth/login",
            "/api/v1/auth/me",
            "/api/v1/auth/logout",
        }

        for route in routes:
            if route.path in public_paths:
                continue
            dependency_calls = {
                dependency.call for dependency in route.dependant.dependencies
            }
            self.assertIn(
                require_authenticated_user,
                dependency_calls,
                f"{route.path} must require authentication",
            )


if __name__ == "__main__":
    unittest.main()
