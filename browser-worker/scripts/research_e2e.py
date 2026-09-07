"""Orchestrate, verify, and export persisted Agentic Research experiments."""

from __future__ import annotations

import argparse
import hashlib
import json
import os
import subprocess
import sys
import tempfile
import time
from datetime import UTC, datetime, timedelta
from pathlib import Path
from typing import Any, Callable, Sequence
from urllib.error import HTTPError
from urllib.parse import urlencode, urljoin
from urllib.request import Request, urlopen

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from scripts.run_agentic_e2e import (
    DEFAULT_CANCEL_GRACE_SECONDS,
    DEFAULT_REQUEST_TIMEOUT_SECONDS,
    HTTPAgenticClient,
    run_agentic_goal,
    validate_goal,
)


REPOSITORY_ROOT = Path(__file__).resolve().parents[2]
DEFAULT_SPEC = (
    REPOSITORY_ROOT / "research" / "experiments" / "stage5-canonical.v1.json"
)
EXPERIMENT_SPEC_VERSION = "research.experiment-spec.v1"
MAX_EXPERIMENT_TIMEOUT_SECONDS = 24 * 60 * 60
FORBIDDEN_SPEC_KEYS = {
    "dsl",
    "dsl_case",
    "selector",
    "selectors",
    "candidate",
    "candidates",
}
FORBIDDEN_AUDIT_KEYS = {"thought", "reasoning_content", "scratchpad"}
REQUIRED_CONTROL_FIELDS = {
    "dataset_version",
    "model_provider",
    "model_name",
    "model_version",
    "prompt_version",
    "browser_name",
    "browser_version",
    "viewport",
    "code_sha256",
    "policy_version",
    "observation_profile",
    "dsl_profile",
    "seed",
    "variant",
}
CODE_SNAPSHOT_VERSION = b"research-code-snapshot.v1\0"
CODE_SNAPSHOT_EXACT_PATHS = {
    "backend-go/go.mod",
    "backend-go/go.sum",
    "browser-worker/pyproject.toml",
    "browser-worker/uv.lock",
    "browser-worker/scripts/research_e2e.py",
    "browser-worker/scripts/run_agentic_e2e.py",
    "scripts/research-e2e",
}
CODE_SNAPSHOT_SOURCE_ROOTS = (
    "backend-go/cmd/agentservice/",
    "backend-go/internal/agentservice/",
    "backend-go/internal/execution/",
    "backend-go/internal/planning/",
    "backend-go/internal/research/",
    "backend-go/internal/transport/http/",
    "browser-worker/app/",
)
CODE_SNAPSHOT_IGNORED_PARTS = {
    "__pycache__",
    ".cache",
    ".mypy_cache",
    ".pytest_cache",
    ".ruff_cache",
    "artifacts",
    "build",
    "dist",
    "logs",
    "node_modules",
    "results",
}
CODE_SNAPSHOT_SECRET_SUFFIXES = {
    ".crt",
    ".key",
    ".p12",
    ".pem",
}
RunDriver = Callable[..., dict[str, Any]]


class ResearchE2EError(RuntimeError):
    pass


class ResearchAPIClient:
    """Small adapter around the Stage 5 Research API contract."""

    def __init__(
        self,
        base_url: str,
        *,
        request_timeout: float = DEFAULT_REQUEST_TIMEOUT_SECONDS,
    ) -> None:
        if request_timeout <= 0:
            raise ValueError("request_timeout must be positive")
        self.base_url = base_url.rstrip("/") + "/"
        self.request_timeout = request_timeout

    def _json(
        self,
        method: str,
        path: str,
        payload: dict[str, Any] | None = None,
    ) -> Any:
        body = None if payload is None else json.dumps(payload).encode()
        request = Request(
            urljoin(self.base_url, path.lstrip("/")),
            data=body,
            method=method,
            headers={"Content-Type": "application/json"},
        )
        try:
            with urlopen(request, timeout=self.request_timeout) as response:
                raw = response.read()
        except HTTPError as exc:
            detail = exc.read().decode(errors="replace")
            raise ResearchE2EError(
                f"{method} {path} failed: {exc.code} {detail}"
            ) from exc
        if not raw:
            return {}
        return json.loads(raw)

    def create_experiment(self, payload: dict[str, Any]) -> dict[str, Any]:
        return self._json("POST", "/api/v2/research/experiments", payload)

    def start_experiment(
        self, experiment_id: str, project_id: int
    ) -> dict[str, Any]:
        return self._json(
            "POST",
            f"/api/v2/research/experiments/{experiment_id}/start",
            {"project_id": project_id},
        )

    def list_runs(
        self, experiment_id: str, project_id: int
    ) -> list[dict[str, Any]]:
        query = urlencode({"project_id": project_id})
        payload = self._json(
            "GET",
            f"/api/v2/research/experiments/{experiment_id}/runs?{query}",
        )
        scheduled = _unwrap_list(payload, "runs")
        result = []
        for item in scheduled:
            run = item.get("run")
            if isinstance(run, dict):
                run = dict(run)
                run["schedule_order"] = item.get("order")
                result.append(run)
            else:
                result.append(item)
        return result

    def get_run(self, run_id: str, project_id: int) -> dict[str, Any]:
        query = urlencode({"project_id": project_id})
        return _unwrap_object(
            self._json(
                "GET", f"/api/v2/research/runs/{run_id}?{query}"
            ),
            "run",
        )

    def start_run(self, run_id: str, project_id: int) -> dict[str, Any]:
        payload = self._json(
            "POST",
            f"/api/v2/research/runs/{run_id}/start",
            {"project_id": project_id},
        )
        if not isinstance(payload, dict):
            raise ResearchE2EError("start run response must be an object")
        return payload

    def update_links(
        self, run_id: str, project_id: int, links: dict[str, Any]
    ) -> dict[str, Any]:
        return _unwrap_object(
            self._json(
                "PUT",
                f"/api/v2/research/runs/{run_id}/links",
                {"project_id": project_id, "links": links},
            ),
            "run",
        )

    def put_oracle(
        self,
        run_id: str,
        project_id: int,
        execution_id: int,
        oracle: dict[str, Any],
    ) -> dict[str, Any]:
        return self._json(
            "PUT",
            f"/api/v2/research/runs/{run_id}/oracle",
            {
                "project_id": project_id,
                "execution_id": execution_id,
                "decision": oracle,
            },
        )

    def project_metrics(self, run_id: str, project_id: int) -> dict[str, Any]:
        return self._json(
            "POST",
            f"/api/v2/research/runs/{run_id}/project-metrics",
            {"project_id": project_id},
        )

    def finish_run(
        self, run_id: str, project_id: int, status: str = "completed"
    ) -> dict[str, Any]:
        return _unwrap_object(
            self._json(
                "POST",
                f"/api/v2/research/runs/{run_id}/finish",
                {"project_id": project_id, "status": status},
            ),
            "run",
        )

    def cancel_run(self, run_id: str, project_id: int) -> dict[str, Any]:
        return _unwrap_object(
            self._json(
                "POST",
                f"/api/v2/research/runs/{run_id}/cancel",
                {"project_id": project_id},
            ),
            "run",
        )

    def get_oracle(self, run_id: str, project_id: int) -> dict[str, Any]:
        query = urlencode({"project_id": project_id})
        payload = self._json(
            "GET", f"/api/v2/research/runs/{run_id}/oracle?{query}"
        )
        if isinstance(payload, dict) and isinstance(
            payload.get("decision"), dict
        ):
            return payload["decision"]
        return _unwrap_object(payload, "oracle")

    def get_agent_run(self, run_id: str) -> dict[str, Any]:
        return self._json("GET", f"/api/v2/agent/runs/{run_id}")

    def get_planning_session(self, session_id: int) -> dict[str, Any]:
        return self._json(
            "GET", f"/api/v2/planning/sessions/{session_id}"
        )

    def get_batch_report(self, batch_id: int) -> dict[str, Any]:
        return self._json(
            "GET", f"/api/v2/execution-batches/{batch_id}/report"
        )


def _unwrap_object(payload: Any, key: str) -> dict[str, Any]:
    if not isinstance(payload, dict):
        raise ResearchE2EError(f"API response must be an object, got {type(payload).__name__}")
    nested = payload.get(key)
    if isinstance(nested, dict):
        return nested
    return payload


def _unwrap_list(payload: Any, key: str) -> list[dict[str, Any]]:
    if isinstance(payload, list):
        values = payload
    elif isinstance(payload, dict) and isinstance(payload.get(key), list):
        values = payload[key]
    else:
        raise ResearchE2EError(f"API response must contain a {key} array")
    if not all(isinstance(value, dict) for value in values):
        raise ResearchE2EError(f"API {key} array contains a non-object")
    return values


def _reject_forbidden_keys(value: Any, path: str = "$") -> None:
    if isinstance(value, dict):
        for key, nested in value.items():
            normalized = str(key).casefold()
            if normalized in FORBIDDEN_SPEC_KEYS:
                raise ValueError(f"{path}.{key} is forbidden in an experiment spec")
            _reject_forbidden_keys(nested, f"{path}.{key}")
    elif isinstance(value, list):
        for index, nested in enumerate(value):
            _reject_forbidden_keys(nested, f"{path}[{index}]")


def _assert_audit_safe(value: Any, path: str = "$") -> None:
    if isinstance(value, dict):
        for key, nested in value.items():
            if str(key).casefold() in FORBIDDEN_AUDIT_KEYS:
                raise ResearchE2EError(
                    f"audit payload contains forbidden field {path}.{key}"
                )
            _assert_audit_safe(nested, f"{path}.{key}")
    elif isinstance(value, list):
        for index, nested in enumerate(value):
            _assert_audit_safe(nested, f"{path}[{index}]")


def _is_code_snapshot_path(relative_path: str) -> bool:
    path = Path(relative_path)
    lowered_parts = {part.casefold() for part in path.parts}
    name = path.name.casefold()
    if lowered_parts & CODE_SNAPSHOT_IGNORED_PARTS:
        return False
    if (
        name == ".env"
        or name.startswith(".env.")
        or name.startswith("credentials.")
        or name.startswith("secrets.")
        or path.suffix.casefold() in CODE_SNAPSHOT_SECRET_SUFFIXES
    ):
        return False
    if relative_path in CODE_SNAPSHOT_EXACT_PATHS:
        return True
    if not relative_path.startswith(CODE_SNAPSHOT_SOURCE_ROOTS):
        return False
    if path.suffix not in {".go", ".py"}:
        return False
    return (
        "tests" not in lowered_parts
        and not name.startswith("test_")
        and not name.endswith("_test.go")
    )


def code_snapshot_files(
    repository_root: Path = REPOSITORY_ROOT,
) -> list[Path]:
    root = repository_root.resolve()
    completed = subprocess.run(
        [
            "git",
            "-C",
            str(root),
            "ls-files",
            "-z",
            "--cached",
            "--others",
            "--exclude-standard",
            "--",
            "backend-go",
            "browser-worker",
            "scripts",
        ],
        check=True,
        capture_output=True,
    )
    relative_paths = sorted(
        {
            raw.decode("utf-8")
            for raw in completed.stdout.split(b"\0")
            if raw and _is_code_snapshot_path(raw.decode("utf-8"))
        }
    )
    files = [root / relative_path for relative_path in relative_paths]
    for path in files:
        if path.is_symlink() or not path.is_file():
            raise ResearchE2EError(
                f"code snapshot source is not a regular file: "
                f"{path.relative_to(root).as_posix()}"
            )
    return files


def compute_code_snapshot_sha256(
    repository_root: Path = REPOSITORY_ROOT,
) -> str:
    root = repository_root.resolve()
    digest = hashlib.sha256(CODE_SNAPSHOT_VERSION)
    files = code_snapshot_files(root)
    if not files:
        raise ResearchE2EError("code snapshot source set is empty")
    for path in files:
        relative = path.relative_to(root).as_posix().encode("utf-8")
        content = path.read_bytes()
        digest.update(len(relative).to_bytes(8, "big"))
        digest.update(relative)
        digest.update(len(content).to_bytes(8, "big"))
        digest.update(content)
    return digest.hexdigest()


def _parse_utc_deadline(value: Any) -> datetime:
    if not isinstance(value, str) or not value.strip():
        raise ResearchE2EError("start run response has no deadline")
    normalized = value.strip()
    if normalized.endswith("Z"):
        normalized = normalized[:-1] + "+00:00"
    try:
        parsed = datetime.fromisoformat(normalized)
    except ValueError as exc:
        raise ResearchE2EError(
            f"start run response has invalid deadline {value!r}"
        ) from exc
    if parsed.tzinfo is None:
        raise ResearchE2EError("start run deadline must include a UTC offset")
    return parsed.astimezone(UTC)


def load_experiment_spec(path: Path) -> dict[str, Any]:
    return load_experiment_spec_from_value(
        json.loads(path.read_text(encoding="utf-8"))
    )


def load_experiment_spec_from_value(
    source: dict[str, Any],
) -> dict[str, Any]:
    payload = json.loads(json.dumps(source))
    if not isinstance(payload, dict):
        raise ValueError("experiment spec must be a JSON object")
    _reject_forbidden_keys(payload)
    allowed = {
        "schema_version",
        "id",
        "name",
        "goal",
        "controls",
        "repetitions",
        "warmup_runs",
        "timeouts",
        "oracle_mutation",
    }
    unknown = set(payload) - allowed
    if unknown:
        raise ValueError(f"unsupported experiment fields: {sorted(unknown)}")
    if payload.get("schema_version") != EXPERIMENT_SPEC_VERSION:
        raise ValueError(
            f"schema_version must be {EXPERIMENT_SPEC_VERSION}"
        )
    for field in ("id", "name"):
        if not isinstance(payload.get(field), str) or not payload[field].strip():
            raise ValueError(f"{field} must be a non-empty string")
    payload["goal"] = validate_goal(str(payload.get("goal") or ""))

    controls = payload.get("controls")
    if not isinstance(controls, dict):
        raise ValueError("controls must be an object")
    missing = REQUIRED_CONTROL_FIELDS - set(controls)
    unknown_controls = set(controls) - REQUIRED_CONTROL_FIELDS
    if missing or unknown_controls:
        raise ValueError(
            f"controls mismatch: missing={sorted(missing)}, "
            f"unknown={sorted(unknown_controls)}"
        )
    if not isinstance(controls["viewport"], dict) or not controls["viewport"]:
        raise ValueError("controls.viewport must be a non-empty object")
    if (
        not isinstance(controls["code_sha256"], str)
        or len(controls["code_sha256"]) != 64
        or any(
            character not in "0123456789abcdef"
            for character in controls["code_sha256"]
        )
    ):
        raise ValueError("controls.code_sha256 must be lowercase SHA-256")
    if not isinstance(controls["seed"], int):
        raise ValueError("controls.seed must be an integer")
    for field in REQUIRED_CONTROL_FIELDS - {"viewport", "seed", "code_sha256"}:
        if not isinstance(controls[field], str) or not controls[field].strip():
            raise ValueError(f"controls.{field} must be a non-empty string")

    repetitions = payload.get("repetitions", 3)
    warmup_runs = payload.get("warmup_runs", 0)
    if not isinstance(repetitions, int) or repetitions < 1:
        raise ValueError("repetitions must be a positive integer")
    if not isinstance(warmup_runs, int) or warmup_runs < 0:
        raise ValueError("warmup_runs must be a non-negative integer")
    payload["repetitions"] = repetitions
    payload["warmup_runs"] = warmup_runs

    timeouts = payload.get("timeouts") or {}
    if not isinstance(timeouts, dict):
        raise ValueError("timeouts must be an object")
    unknown_timeouts = set(timeouts) - {
        "run_seconds",
        "experiment_seconds",
        "request_seconds",
        "cancel_grace_seconds",
    }
    if unknown_timeouts:
        raise ValueError(f"unsupported timeout fields: {sorted(unknown_timeouts)}")
    run_seconds = float(timeouts.get("run_seconds", 900))
    request_seconds = float(
        timeouts.get("request_seconds", DEFAULT_REQUEST_TIMEOUT_SECONDS)
    )
    cancel_grace_seconds = float(
        timeouts.get("cancel_grace_seconds", DEFAULT_CANCEL_GRACE_SECONDS)
    )
    experiment_seconds = float(
        timeouts.get(
            "experiment_seconds",
            (repetitions + warmup_runs) * run_seconds + 300,
        )
    )
    if (
        run_seconds <= 0
        or request_seconds <= 0
        or cancel_grace_seconds < 0
        or experiment_seconds <= 0
        or experiment_seconds > MAX_EXPERIMENT_TIMEOUT_SECONDS
    ):
        raise ValueError("timeouts must be positive and experiment timeout <= 86400s")
    if (
        not run_seconds.is_integer()
        or not request_seconds.is_integer()
        or not cancel_grace_seconds.is_integer()
        or run_seconds < 60
        or run_seconds > 3600
        or request_seconds > 3600
        or cancel_grace_seconds > 300
    ):
        raise ValueError(
            "run/request/cancel timeouts must be whole seconds within Go API limits"
        )
    payload["timeouts"] = {
        "run_seconds": run_seconds,
        "experiment_seconds": experiment_seconds,
        "request_seconds": request_seconds,
        "cancel_grace_seconds": cancel_grace_seconds,
    }
    mutation = payload.get("oracle_mutation", "none")
    if mutation not in {"none", "wrong-price", "wrong-product"}:
        raise ValueError("oracle_mutation is unsupported")
    payload["oracle_mutation"] = mutation
    return payload


def _experiment_payload(
    spec: dict[str, Any],
    project_id: int,
) -> dict[str, Any]:
    controls = spec["controls"]
    payload = {
        "project_id": project_id,
        "name": spec["name"],
        "goal": spec["goal"],
        **controls,
        "repetitions": spec["repetitions"],
        "config": {
            "schema_version": "research.experiment_config.v1",
            "request_timeout_seconds": int(
                spec["timeouts"]["request_seconds"]
            ),
            "run_timeout_seconds": int(spec["timeouts"]["run_seconds"]),
            "cancel_grace_seconds": int(
                spec["timeouts"]["cancel_grace_seconds"]
            ),
            "warmup_repetitions": spec["warmup_runs"],
            "clean_context": True,
            "schedule_version": "research.schedule.v1",
        },
    }
    _assert_audit_safe(payload)
    return payload


def _wait_for_scheduled_runs(
    client: ResearchAPIClient,
    experiment_id: str,
    project_id: int,
    expected_count: int,
    deadline_monotonic: float,
) -> list[dict[str, Any]]:
    while time.monotonic() < deadline_monotonic:
        runs = client.list_runs(experiment_id, project_id)
        if len(runs) == expected_count:
            return runs
        if len(runs) > expected_count:
            raise ResearchE2EError(
                f"experiment scheduled {len(runs)} runs, expected {expected_count}"
            )
        time.sleep(0.25)
    raise TimeoutError("experiment schedule did not materialize before deadline")


def _links_from_driver_result(result: dict[str, Any]) -> dict[str, Any]:
    ids = result.get("ids") or {}
    approval = result.get("approval") or {}
    links = {
        "agent_run_id": ids.get("agent_run_id"),
        "generation_id": ids.get("generation_id"),
        "batch_id": ids.get("batch_id"),
        "execution_id": ids.get("execution_id"),
        "dsl_sha256": approval.get("dsl_sha256"),
    }
    return {key: value for key, value in links.items() if value is not None}


def _partial_links(exc: Exception) -> dict[str, Any]:
    diagnostic = getattr(exc, "diagnostic", None)
    if not isinstance(diagnostic, dict):
        return {}
    ids = diagnostic.get("ids") or {}
    ordered = (
        ("agent_run_id", "agent_run_id"),
        ("generation_id", "generation_id"),
        ("batch_id", "batch_id"),
        ("execution_id", "execution_id"),
    )
    links: dict[str, Any] = {}
    for output_key, source_key in ordered:
        value = ids.get(source_key)
        if value is None:
            break
        links[output_key] = value
    return links


def _oracle_payload(result: dict[str, Any]) -> dict[str, Any]:
    oracle = dict(result["oracle"])
    execution_id = int(result["ids"]["execution_id"])
    artifact = result["artifacts"]["final_dom"]
    source = {
        "kind": "independent_oracle",
        "id": f"execution:{execution_id}:final_dom",
        "sequence": {
            "status": "unavailable",
            "reason": "oracle_artifact_has_no_global_sequence",
        },
        "content_sha256": artifact["sha256"],
        "schema_version": {
            "status": "unavailable",
            "reason": "raw_dom_snapshot_has_no_schema_version",
        },
    }
    checks = oracle.get("checks")
    if not isinstance(checks, dict) or not checks:
        raise ResearchE2EError("driver oracle has no checks")
    expected = oracle.get("expected") or {}
    actual = oracle.get("actual") or {}
    facts = []
    for name, check in sorted(checks.items()):
        if isinstance(check, dict):
            passed = check.get("passed")
            fact_expected = check.get("expected")
            fact_actual = check.get("actual")
        else:
            passed = check
            if name == "single_cart_row":
                fact_expected = 1
                fact_actual = len(oracle.get("observed_row_ids") or [])
            elif name == "single_product_1":
                fact_expected = 1
                fact_actual = sum(
                    row_id == "product-1"
                    for row_id in oracle.get("observed_row_ids") or []
                )
            else:
                fact_expected = expected.get(name)
                fact_actual = actual.get(name)
        if not isinstance(passed, bool):
            raise ResearchE2EError(
                f"driver oracle check {name} has no boolean result"
            )
        facts.append(
            {
                "name": name,
                "passed": passed,
                "expected": fact_expected,
                "actual": fact_actual,
                "sources": [source],
            }
        )
    facts_passed = all(fact["passed"] for fact in facts)
    if oracle.get("passed") is not facts_passed:
        raise ResearchE2EError(
            "driver oracle passed value disagrees with decision facts"
        )
    decision = {
        "schema_version": "research.oracle.v1",
        "id": f"automationexercise-cart-{execution_id}",
        "evaluator": oracle["schema_version"],
        "passed": facts_passed,
        "reason_code": (
            "task_passed"
            if facts_passed
            else "cart_state_mismatch"
        ),
        "decision_facts": facts,
        "sources": [source],
    }
    _assert_audit_safe(decision)
    return decision


def run_experiment(
    spec: dict[str, Any],
    *,
    project_id: int,
    research_client: ResearchAPIClient,
    agent_url: str,
    browser_url: str,
    driver: RunDriver = run_agentic_goal,
    agent_client_factory: Callable[..., Any] = HTTPAgenticClient,
    code_snapshot_sha256: Callable[[], str] = compute_code_snapshot_sha256,
) -> dict[str, Any]:
    if project_id < 1:
        raise ValueError("project_id must be a positive integer")
    expected_code_sha256 = spec["controls"]["code_sha256"]
    actual_code_sha256 = code_snapshot_sha256()
    if actual_code_sha256 != expected_code_sha256:
        raise ResearchE2EError(
            "code snapshot SHA-256 mismatch: "
            f"spec={expected_code_sha256} actual={actual_code_sha256}"
        )
    started_at = datetime.now(UTC)
    deadline_monotonic = (
        time.monotonic() + spec["timeouts"]["experiment_seconds"]
    )
    deadline_at = started_at + timedelta(
        seconds=spec["timeouts"]["experiment_seconds"]
    )
    experiment = _unwrap_object(
        research_client.create_experiment(
            _experiment_payload(spec, project_id)
        ),
        "experiment",
    )
    experiment_id = str(experiment.get("id") or "")
    if not experiment_id:
        raise ResearchE2EError("create experiment response has no id")
    research_client.start_experiment(experiment_id, project_id)
    expected_count = spec["repetitions"] + spec["warmup_runs"]
    scheduled = _wait_for_scheduled_runs(
        research_client,
        experiment_id,
        project_id,
        expected_count,
        deadline_monotonic,
    )

    summaries: list[dict[str, Any]] = []
    failures: list[dict[str, Any]] = []
    session_ids: set[int] = set()
    for index, scheduled_run in enumerate(scheduled):
        research_run_id = str(scheduled_run.get("id") or "")
        if not research_run_id:
            raise ResearchE2EError("scheduled research run has no id")
        remaining = deadline_monotonic - time.monotonic()
        if remaining <= 0:
            for pending in scheduled[index:]:
                pending_id = str(pending.get("id") or "")
                if pending_id:
                    research_client.cancel_run(
                        pending_id, project_id
                    )
            failures.append(
                {
                    "research_run_id": research_run_id,
                    "error": "experiment absolute deadline exceeded",
                }
            )
            break

        try:
            start = research_client.start_run(research_run_id, project_id)
            started_run = _unwrap_object(start, "run")
            if str(started_run.get("id") or "") != research_run_id:
                raise ResearchE2EError("start run response has a mismatched run")
            server_deadline = _parse_utc_deadline(start.get("deadline"))
            server_remaining = (
                server_deadline - datetime.now(UTC)
            ).total_seconds()
            remaining = deadline_monotonic - time.monotonic()
            driver_timeout = min(
                spec["timeouts"]["run_seconds"],
                remaining,
                server_remaining,
            )
            if driver_timeout <= 0:
                raise TimeoutError(
                    f"research run {research_run_id} server deadline exceeded"
                )
            driver_result = driver(
                spec["goal"],
                client=agent_client_factory(
                    agent_url=agent_url,
                    browser_url=browser_url,
                    request_timeout=spec["timeouts"]["request_seconds"],
                ),
                timeout_seconds=driver_timeout,
                mutation=spec["oracle_mutation"],
                clean_context=True,
                project_id=project_id,
                cancel_grace_seconds=spec["timeouts"][
                    "cancel_grace_seconds"
                ],
            )
            ids = driver_result["ids"]
            session_id = int(ids["planning_session_id"])
            if session_id in session_ids:
                raise ResearchE2EError(
                    f"planning/browser session {session_id} was reused"
                )
            session_ids.add(session_id)
            links = _links_from_driver_result(driver_result)
            if set(links) != {
                "agent_run_id",
                "generation_id",
                "batch_id",
                "execution_id",
                "dsl_sha256",
            }:
                raise ResearchE2EError("driver result has incomplete run links")
            research_client.update_links(
                research_run_id, project_id, links
            )
            research_client.put_oracle(
                research_run_id,
                project_id,
                int(ids["execution_id"]),
                _oracle_payload(driver_result),
            )
            research_client.project_metrics(research_run_id, project_id)
            finished = research_client.finish_run(
                research_run_id, project_id
            )
            driver_success = driver_result.get("success")
            if not isinstance(driver_success, bool):
                raise ResearchE2EError(
                    "driver result has no boolean success value"
                )
            summaries.append(
                {
                    "research_run_id": research_run_id,
                    "repetition_index": scheduled_run.get("repetition_index"),
                    "warmup": bool(scheduled_run.get("warmup")),
                    "status": finished.get("status"),
                    "project_id": ids["project_id"],
                    "planning_session_id": session_id,
                    "browser_context_key": ids["browser_context_key"],
                    "agent_run_id": ids["agent_run_id"],
                    "batch_id": ids["batch_id"],
                    "execution_id": ids["execution_id"],
                    "formal_passed": driver_result["formal_execution"]["passed"],
                    "oracle_passed": driver_result["oracle"]["passed"],
                    "success": driver_success,
                }
            )
            if not driver_success:
                failures.append(
                    {
                        "research_run_id": research_run_id,
                        "error": "driver completed with business failure",
                        "failure_kind": "business_failure",
                    }
                )
        except Exception as exc:
            partial = _partial_links(exc)
            diagnostic = getattr(exc, "diagnostic", None)
            failure = {
                "research_run_id": research_run_id,
                "error": str(exc),
            }
            if isinstance(diagnostic, dict):
                failure["diagnostic"] = diagnostic
            if partial:
                try:
                    research_client.update_links(
                        research_run_id, project_id, partial
                    )
                except Exception:
                    pass
            try:
                research_client.cancel_run(research_run_id, project_id)
            except Exception as cancel_exc:
                failure["cancel_error"] = str(cancel_exc)
            failures.append(failure)

    result = {
        "schema_version": "research.e2e-run-summary.v1",
        "experiment_id": experiment_id,
        "project_id": project_id,
        "started_at": started_at.isoformat(),
        "deadline_at": deadline_at.isoformat(),
        "timeout_mode": "absolute_monotonic_deadline",
        "configuration": {
            "repetitions": spec["repetitions"],
            "warmup_runs": spec["warmup_runs"],
            **spec["timeouts"],
        },
        "runs": summaries,
        "failures": failures,
        "success": not failures and len(summaries) == expected_count,
    }
    _assert_audit_safe(result)
    return result


def _metric_value(metrics: dict[str, Any], name: str) -> Any:
    slot = metrics.get(name)
    if not isinstance(slot, dict):
        raise ResearchE2EError(f"metric {name} is not a persisted metric slot")
    if slot.get("unavailable_reason") is not None or "value" not in slot:
        raise ResearchE2EError(f"metric {name} is unavailable")
    return slot["value"]


def _latest_execution(report: dict[str, Any]) -> dict[str, Any]:
    jobs = report.get("jobs")
    if not isinstance(jobs, list) or len(jobs) != 1:
        raise ResearchE2EError("batch report must contain exactly one job")
    execution = jobs[0].get("latest_execution")
    if not isinstance(execution, dict):
        raise ResearchE2EError("batch report has no latest execution")
    return execution


def _report_verification_success(execution: dict[str, Any]) -> bool:
    steps = (execution.get("report") or {}).get("steps")
    if not isinstance(steps, list):
        raise ResearchE2EError("execution report has no structured steps")
    statuses = []
    for step in steps:
        conditions = step.get("condition_results")
        if not isinstance(conditions, list):
            raise ResearchE2EError(
                "execution report has incomplete verification facts"
            )
        for condition in conditions:
            status = condition.get("status")
            if status not in {"passed", "failed", "error"}:
                raise ResearchE2EError(
                    "execution report has an invalid verification status"
                )
            statuses.append(status)
    if not statuses:
        raise ResearchE2EError("execution report has no verification facts")
    return all(status == "passed" for status in statuses)


def verify_experiment(
    experiment_id: str,
    *,
    client: ResearchAPIClient,
    project_id: int,
    expected_repetitions: int = 3,
    expected_task_success: bool = True,
) -> dict[str, Any]:
    listed = client.list_runs(experiment_id, project_id)
    formal_runs = [run for run in listed if not bool(run.get("warmup"))]
    if len(formal_runs) != expected_repetitions:
        raise ResearchE2EError(
            f"expected {expected_repetitions} non-warmup runs, got {len(formal_runs)}"
        )
    repetition_indexes = {
        run.get("repetition_index") for run in formal_runs
    }
    if repetition_indexes != set(range(expected_repetitions)):
        raise ResearchE2EError(
            "non-warmup repetition indexes are incomplete or duplicated"
        )

    session_ids: set[str] = set()
    verified: list[dict[str, Any]] = []
    for listed_run in formal_runs:
        run_id = str(listed_run.get("id") or "")
        run = client.get_run(run_id, project_id)
        if run.get("status") != "completed":
            raise ResearchE2EError(f"research run {run_id} is not completed")
        if int(run.get("project_id") or 0) != project_id:
            raise ResearchE2EError(f"research run {run_id} project mismatch")
        links = run.get("links")
        required_links = {
            "agent_run_id",
            "generation_id",
            "batch_id",
            "execution_id",
            "dsl_sha256",
        }
        if not isinstance(links, dict) or any(
            links.get(name) in (None, "") for name in required_links
        ):
            raise ResearchE2EError(f"research run {run_id} has incomplete links")
        if (
            any(
                not isinstance(links[name], int) or links[name] <= 0
                for name in ("generation_id", "batch_id", "execution_id")
            )
            or not isinstance(links["agent_run_id"], str)
            or not isinstance(links["dsl_sha256"], str)
            or len(links["dsl_sha256"]) != 64
        ):
            raise ResearchE2EError(f"research run {run_id} has invalid links")

        agent_run = client.get_agent_run(str(links["agent_run_id"]))
        session_id = str(agent_run.get("conversation_id") or "")
        if (
            not session_id.isdigit()
            or int(session_id) <= 0
            or session_id in session_ids
        ):
            raise ResearchE2EError(
                f"research run {run_id} did not use a clean planning/browser session"
            )
        session_ids.add(session_id)
        if agent_run.get("status") != "completed":
            raise ResearchE2EError(f"agent run {links['agent_run_id']} is not completed")
        if int(agent_run.get("project_id") or 0) != int(run.get("project_id") or 0):
            raise ResearchE2EError(f"research run {run_id} project link mismatch")
        session_payload = client.get_planning_session(int(session_id))
        session = _unwrap_object(session_payload, "session")
        requirements = session.get("requirements")
        if not isinstance(requirements, dict) or (
            requirements.get("clean_context") is not True
        ):
            raise ResearchE2EError(
                f"research run {run_id} planning session is not clean"
            )

        report = client.get_batch_report(int(links["batch_id"]))
        execution = _latest_execution(report)
        if (
            int(execution.get("id") or 0) != int(links["execution_id"])
            or execution.get("dsl_sha256") != links["dsl_sha256"]
        ):
            raise ResearchE2EError(f"research run {run_id} report link mismatch")
        formal_passed = (
            report.get("status") == "passed"
            and execution.get("status") == "passed"
        )

        metrics = run.get("metrics")
        if not isinstance(metrics, dict):
            raise ResearchE2EError(f"research run {run_id} has no persisted metrics")
        task_success = _metric_value(metrics, "task_success")
        execution_success = _metric_value(metrics, "execution_success")
        verification_success = _metric_value(metrics, "verification_success")
        vision_calls = _metric_value(metrics, "vision_calls")
        if not all(
            isinstance(value, bool)
            for value in (
                task_success,
                execution_success,
                verification_success,
            )
        ):
            raise ResearchE2EError(
                f"research run {run_id} has invalid boolean metrics"
            )
        if (
            not isinstance(vision_calls, int)
            or isinstance(vision_calls, bool)
            or vision_calls != 0
        ):
            raise ResearchE2EError(
                f"research run {run_id} used vision {vision_calls} time(s)"
            )
        if execution_success != formal_passed:
            raise ResearchE2EError(
                f"research run {run_id} execution metric disagrees with report"
            )
        report_verification = _report_verification_success(execution)
        if verification_success != report_verification:
            raise ResearchE2EError(
                f"research run {run_id} verification metric disagrees with report"
            )

        oracle = client.get_oracle(run_id, project_id)
        oracle_passed = oracle.get("passed")
        if not isinstance(oracle_passed, bool):
            raise ResearchE2EError(f"research run {run_id} oracle is incomplete")
        if task_success != oracle_passed:
            raise ResearchE2EError(
                f"research run {run_id} task_success disagrees with oracle"
            )
        if task_success is not expected_task_success:
            raise ResearchE2EError(
                f"research run {run_id} task_success is {task_success}, "
                f"expected {expected_task_success}"
            )
        if oracle_passed is not expected_task_success:
            raise ResearchE2EError(
                f"research run {run_id} oracle is {oracle_passed}, "
                f"expected {expected_task_success}"
            )
        if execution_success is not True:
            raise ResearchE2EError(
                f"research run {run_id} execution_success is not true"
            )
        if verification_success is not True:
            raise ResearchE2EError(
                f"research run {run_id} verification_success is not true"
            )
        verified.append(
            {
                "research_run_id": run_id,
                "planning_session_id": session_id,
                "agent_run_id": links["agent_run_id"],
                "batch_id": links["batch_id"],
                "execution_id": links["execution_id"],
                "task_success": task_success,
                "execution_success": execution_success,
                "verification_success": verification_success,
                "oracle_passed": oracle_passed,
                "vision_calls": vision_calls,
            }
        )

    return {
        "schema_version": "research.e2e-verification.v1",
        "experiment_id": experiment_id,
        "project_id": project_id,
        "non_warmup_runs": len(formal_runs),
        "clean_session_count": len(session_ids),
        "expected_task_success": expected_task_success,
        "runs": verified,
        "passed": True,
    }


def export_research(
    *,
    output: Path,
    run_id: str | None = None,
    experiment_id: str | None = None,
    timeout_seconds: float = 300,
    command: Sequence[str] | None = None,
) -> None:
    if (run_id is None) == (experiment_id is None):
        raise ValueError("exactly one of run_id or experiment_id is required")
    if timeout_seconds <= 0:
        raise ValueError("export timeout must be positive")
    output = output.resolve()
    output.parent.mkdir(parents=True, exist_ok=True)
    fd, temporary_name = tempfile.mkstemp(
        prefix=".research-e2e-export-",
        suffix=".jsonl",
        dir=output.parent,
    )
    os.close(fd)
    temporary = Path(temporary_name)
    temporary.unlink()
    base_command = list(command or ("go", "run", "./cmd/research-export"))
    selector = (
        ["--run-id", run_id]
        if run_id is not None
        else ["--experiment-id", str(experiment_id)]
    )
    try:
        subprocess.run(
            [*base_command, *selector, "--output", str(temporary)],
            cwd=REPOSITORY_ROOT / "backend-go",
            check=True,
            timeout=timeout_seconds,
        )
        if not temporary.is_file():
            raise ResearchE2EError("research-export did not create its output")
        os.replace(temporary, output)
    finally:
        temporary.unlink(missing_ok=True)


def _atomic_write_json(path: Path, payload: dict[str, Any]) -> None:
    path = path.resolve()
    path.parent.mkdir(parents=True, exist_ok=True)
    fd, temporary_name = tempfile.mkstemp(
        prefix=f".{path.name}.", dir=path.parent
    )
    try:
        with os.fdopen(fd, "w", encoding="utf-8") as stream:
            json.dump(payload, stream, ensure_ascii=False, indent=2)
            stream.write("\n")
            stream.flush()
            os.fsync(stream.fileno())
        os.replace(temporary_name, path)
    finally:
        Path(temporary_name).unlink(missing_ok=True)


def _build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description=__doc__)
    subparsers = parser.add_subparsers(dest="command", required=True)

    run_parser = subparsers.add_parser("run")
    run_parser.add_argument("--spec", type=Path, default=DEFAULT_SPEC)
    run_parser.add_argument("--project-id", type=int, required=True)
    run_parser.add_argument("--agent-url", default="http://127.0.0.1:8081")
    run_parser.add_argument("--browser-url", default="http://127.0.0.1:8000")
    run_parser.add_argument("--repetitions", type=int)
    run_parser.add_argument("--warmup-runs", type=int)
    run_parser.add_argument("--run-timeout-seconds", type=float)
    run_parser.add_argument("--experiment-timeout-seconds", type=float)
    run_parser.add_argument("--output", type=Path)

    verify_parser = subparsers.add_parser("verify")
    verify_parser.add_argument("experiment_id")
    verify_parser.add_argument("--project-id", type=int, required=True)
    verify_parser.add_argument("--agent-url", default="http://127.0.0.1:8081")
    verify_parser.add_argument("--request-timeout-seconds", type=float, default=30)
    verify_parser.add_argument("--expected-repetitions", type=int, default=3)
    verify_parser.add_argument(
        "--expected-task-success",
        choices=("true", "false"),
        default="true",
    )
    verify_parser.add_argument("--output", type=Path)

    export_parser = subparsers.add_parser("export")
    selector = export_parser.add_mutually_exclusive_group(required=True)
    selector.add_argument("--run-id")
    selector.add_argument("--experiment-id")
    export_parser.add_argument("--output", type=Path, required=True)
    export_parser.add_argument("--timeout-seconds", type=float, default=300)
    export_parser.add_argument("--export-binary", type=Path)

    subparsers.add_parser("code-sha256")
    return parser


def main(argv: Sequence[str] | None = None) -> int:
    args = _build_parser().parse_args(argv)
    try:
        if args.command == "run":
            spec = load_experiment_spec(args.spec)
            if args.repetitions is not None:
                spec["repetitions"] = args.repetitions
            if args.warmup_runs is not None:
                spec["warmup_runs"] = args.warmup_runs
            if args.run_timeout_seconds is not None:
                spec["timeouts"]["run_seconds"] = args.run_timeout_seconds
            if args.experiment_timeout_seconds is not None:
                spec["timeouts"][
                    "experiment_seconds"
                ] = args.experiment_timeout_seconds
            spec = load_experiment_spec_from_value(spec)
            result = run_experiment(
                spec,
                project_id=args.project_id,
                research_client=ResearchAPIClient(
                    args.agent_url,
                    request_timeout=spec["timeouts"]["request_seconds"],
                ),
                agent_url=args.agent_url,
                browser_url=args.browser_url,
            )
            if args.output:
                _atomic_write_json(args.output, result)
            print(json.dumps(result, ensure_ascii=False))
            return 0 if result["success"] else 1
        if args.command == "verify":
            result = verify_experiment(
                args.experiment_id,
                client=ResearchAPIClient(
                    args.agent_url,
                    request_timeout=args.request_timeout_seconds,
                ),
                project_id=args.project_id,
                expected_repetitions=args.expected_repetitions,
                expected_task_success=args.expected_task_success == "true",
            )
            if args.output:
                _atomic_write_json(args.output, result)
            print(json.dumps(result, ensure_ascii=False))
            return 0
        if args.command == "code-sha256":
            print(compute_code_snapshot_sha256())
            return 0

        command = (
            [str(args.export_binary.resolve())]
            if args.export_binary
            else None
        )
        export_research(
            output=args.output,
            run_id=args.run_id,
            experiment_id=args.experiment_id,
            timeout_seconds=args.timeout_seconds,
            command=command,
        )
        print(json.dumps({"output": str(args.output.resolve())}))
        return 0
    except Exception as exc:
        print(
            json.dumps(
                {"error": {"type": type(exc).__name__, "message": str(exc)}},
                ensure_ascii=False,
            ),
            file=sys.stderr,
        )
        return 1
if __name__ == "__main__":
    raise SystemExit(main())
