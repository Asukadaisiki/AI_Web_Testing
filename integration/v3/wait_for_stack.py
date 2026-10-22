"""Bounded readiness checks for the live v3 Compose stack."""

from __future__ import annotations

import json
import time
import urllib.error
import urllib.request
from typing import Any


def request_json(url: str, *, timeout: float = 5) -> tuple[int, dict[str, Any]]:
    request = urllib.request.Request(url, headers={"Accept": "application/json"})
    try:
        with urllib.request.urlopen(request, timeout=timeout) as response:
            body = response.read()
            return response.status, json.loads(body) if body else {}
    except urllib.error.HTTPError as error:
        body = error.read()
        return error.code, json.loads(body) if body else {}


def wait_for_stack(api_base_url: str, *, timeout: float = 90) -> dict[str, Any]:
    """Wait for the API and its live dependencies without starting subprocesses."""
    deadline = time.monotonic() + timeout
    last_status: int | None = None
    last_body: dict[str, Any] = {}
    while time.monotonic() < deadline:
        try:
            last_status, last_body = request_json(
                f"{api_base_url.rstrip('/')}/api/v1/health", timeout=3
            )
        except (OSError, urllib.error.URLError, json.JSONDecodeError) as error:
            last_body = {"transport_error": str(error)}
        else:
            dependencies = last_body.get("dependencies", {})
            required = ("postgres", "kafka", "redis", "worker")
            if last_status == 200 and all(
                dependencies.get(name, {}).get("status") == "ok" for name in required
            ):
                return last_body
        time.sleep(0.5)
    raise AssertionError(
        "timed out waiting for the v3 Compose stack; "
        f"last HTTP status={last_status}, body={last_body}"
    )
