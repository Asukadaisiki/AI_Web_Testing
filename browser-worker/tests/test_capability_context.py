from __future__ import annotations

import unittest

from fastapi.routing import APIRoute

from app.api.router import build_api_router
from app.api.routes.artifacts import router as artifacts_router


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
                "/api/v1/internal/browser-executions",
                "/artifacts/{artifact_path:path}",
            },
        )


if __name__ == "__main__":
    unittest.main()
