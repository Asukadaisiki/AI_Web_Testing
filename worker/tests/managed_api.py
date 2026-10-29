"""Private managed HTTP fixture shared by real-browser and admission tests."""

import contextlib
import os
import tempfile
from pathlib import Path
from unittest.mock import patch

import httpx

from loop_worker.main import create_app
from tests.test_environments import AUTH, KEY


IDENTITY = {
    "session_id": "sess_test", "project_id": "prj_a", "environment_id": "env_api",
    "profile_generation": 1, "fencing_token": 2,
}
LEASE_HEADERS = {
    **AUTH, "X-Environment-ID": "env_api",
    "X-Profile-Generation": "1", "X-Fencing-Token": "2",
}


def execute_payload(case, held_handle="bsess_validation", **changes):
    return {**IDENTITY, "browser_session_id": held_handle, "case": case, **changes}


class ManagedClient(httpx.AsyncClient):
    def __init__(self, *args, **kwargs):
        super().__init__(*args, **kwargs)
        self._provisioned = False

    async def provision(self):
        response = await self.post("/environments/env_api/provision", json={
            "project_id": "prj_a", "profile_generation": 1, "fencing_token": 1,
            "policy": {"allowed_domains": ["127.0.0.1"]},
        })
        if response.status_code != 200:
            raise AssertionError(response.text)
        self._provisioned = True

    async def open(self, session_id="sess_test"):
        if not self._provisioned:
            await self.provision()
        response = await self.post("/sessions", json={**IDENTITY, "session_id": session_id})
        if response.status_code != 200:
            raise AssertionError(response.text)
        return response


@contextlib.asynccontextmanager
async def api_client(*, raise_app_exceptions=True, browser_factory=None):
    with tempfile.TemporaryDirectory() as directory:
        root = Path(directory).resolve()
        with patch.dict(os.environ, {
            "LOOP_ENVIRONMENTS_DIR": str(root / "profiles"),
            "LOOP_ENVIRONMENT_ENCRYPTION_KEY": KEY,
            "LOOP_WORKER_INTERNAL_TOKEN": "internal-test-token",
            "LOOP_UPLOADS_DIR": str(root / "uploads"),
            "LOOP_ARTIFACTS_DIR": str(root / "artifacts"),
        }):
            app = create_app()
            manager = app.state.session_manager
            browser_patch = (
                patch.object(manager, "browser", browser_factory)
                if browser_factory is not None else contextlib.nullcontext()
            )
            with browser_patch:
                async with ManagedClient(
                    transport=httpx.ASGITransport(
                        app=app, raise_app_exceptions=raise_app_exceptions,
                    ),
                    base_url="http://worker.test", headers=LEASE_HEADERS,
                ) as client:
                    client.manager = manager
                    client.root = root
                    try:
                        yield client
                    finally:
                        await manager.shutdown()
