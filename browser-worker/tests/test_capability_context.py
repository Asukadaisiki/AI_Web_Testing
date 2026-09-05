from __future__ import annotations

import unittest
from unittest.mock import MagicMock

from fastapi import HTTPException
from fastapi.routing import APIRoute

from app.api.capability_context import validate_capability_context
from app.api.router import build_api_router
from app.api.routes.artifacts import router as artifacts_router


class CapabilityContextTest(unittest.TestCase):
    def test_project_member_can_use_owned_planning_context(self) -> None:
        database = MagicMock()
        database.scalar.side_effect = [1, 11]

        validate_capability_context(
            database,
            actor_user_id=7,
            project_id=3,
            conversation_id="11",
        )

        self.assertEqual(database.scalar.call_count, 2)

    def test_unknown_project_context_is_rejected(self) -> None:
        database = MagicMock()
        database.scalar.return_value = None

        with self.assertRaises(HTTPException) as raised:
            validate_capability_context(
                database,
                actor_user_id=7,
                project_id=3,
                conversation_id="11",
            )

        self.assertEqual(raised.exception.status_code, 422)


class RouterTest(unittest.TestCase):
    def test_worker_exposes_only_health_browser_and_artifact_routes(self) -> None:
        paths = {
            route.path
            for route in [*build_api_router().routes, *artifacts_router.routes]
            if isinstance(route, APIRoute)
        }
        self.assertEqual(
            paths,
            {
                "/api/v1/health",
                "/api/v1/internal/browser-capabilities/{capability}",
                "/artifacts/{artifact_path:path}",
            },
        )


if __name__ == "__main__":
    unittest.main()
