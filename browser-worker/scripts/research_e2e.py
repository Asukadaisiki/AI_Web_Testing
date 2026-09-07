"""Orchestrate, verify, and export persisted Agentic Research experiments."""

from __future__ import annotations

import argparse
import hashlib
import hmac
import json
import os
import re
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
    DEFAULT_LONG_OPERATION_TIMEOUT_SECONDS,
    DEFAULT_REQUEST_TIMEOUT_SECONDS,
    HTTPAgenticClient,
    PROFILE_CANONICAL_VERSIONS,
    _validate_generation_binding,
    run_agentic_goal,
    validate_goal,
)


REPOSITORY_ROOT = Path(__file__).resolve().parents[2]
DEFAULT_SPEC = (
    REPOSITORY_ROOT / "research" / "experiments" / "stage6-canonical.v1.json"
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
    "backend-go/internal/agent/",
    "backend-go/internal/agentservice/",
    "backend-go/internal/config/",
    "backend-go/internal/dsl/",
    "backend-go/internal/execution/",
    "backend-go/internal/harness/",
    "backend-go/internal/planning/",
    "backend-go/internal/platform/llm/",
    "backend-go/internal/research/",
    "backend-go/internal/tools/",
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
PROVIDER_EVIDENCE_SCHEMA_VERSION = "research.provider-evidence.v1"
PROVIDER_ATTESTATION_SCHEMA_VERSION = "research.provider-attestation.v1"
PROVIDER_ATTESTATION_ALGORITHM = "hmac-sha256:v1"
PROVIDER_ATTESTATION_KEY_ENV = "RESEARCH_PROVIDER_ATTESTATION_KEY"
PROVIDER_ATTESTATION_SOURCES = (
    "deepseek_console_export",
    "deepseek_usage_api",
    "deepseek_billing_export",
    "deepseek_console_screenshot",
)
PROVIDER_ATTESTATION_JSON_SCHEMA: dict[str, Any] = {
    "$schema": "https://json-schema.org/draft/2020-12/schema",
    "$id": "research.provider-attestation.v1",
    "title": "Research provider platform attestation",
    "type": "object",
    "additionalProperties": False,
    "required": [
        "schema_version",
        "experiment_id",
        "source_sha256",
        "evidence_artifact_sha256",
        "runs",
        "platform_evidence",
        "reviewer",
        "signature",
    ],
    "properties": {
        "schema_version": {"const": PROVIDER_ATTESTATION_SCHEMA_VERSION},
        "experiment_id": {"type": "string", "minLength": 1},
        "source_sha256": {
            "type": "string",
            "pattern": "^[0-9a-f]{64}$",
        },
        "evidence_artifact_sha256": {
            "type": "string",
            "pattern": "^[0-9a-f]{64}$",
        },
        "runs": {
            "type": "array",
            "minItems": 1,
            "items": {
                "type": "object",
                "additionalProperties": False,
                "required": [
                    "research_run_id",
                    "agent_run_id",
                    "source_event_seqs",
                    "run_started_at",
                    "run_finished_at",
                    "provider",
                    "endpoint_scheme",
                    "endpoint_host",
                    "model",
                    "credential_fingerprint",
                    "attempts",
                ],
                "properties": {
                    "research_run_id": {"type": "string", "minLength": 1},
                    "agent_run_id": {"type": "string", "minLength": 1},
                    "source_event_seqs": {
                        "type": "array",
                        "minItems": 1,
                        "items": {"type": "integer", "minimum": 1},
                    },
                    "run_started_at": {
                        "type": "string",
                        "format": "date-time",
                    },
                    "run_finished_at": {
                        "type": "string",
                        "format": "date-time",
                    },
                    "provider": {"type": "string", "minLength": 1},
                    "endpoint_scheme": {"const": "https"},
                    "endpoint_host": {"type": "string", "minLength": 1},
                    "model": {"type": "string", "minLength": 1},
                    "credential_fingerprint": {
                        "type": "string",
                        "pattern": "^sha256:v1:[0-9a-f]{64}$",
                    },
                    "attempts": {
                        "type": "array",
                        "minItems": 1,
                        "items": {
                            "type": "object",
                            "additionalProperties": False,
                            "required": [
                                "client_request_id",
                                "provider_response_id",
                                "provider_header_request_id",
                                "provider_header_request_id_header",
                                "attempt_started_at",
                                "usage",
                            ],
                            "properties": {
                                "client_request_id": {
                                    "type": "string",
                                    "minLength": 1,
                                },
                                "provider_response_id": {
                                    "type": "string",
                                    "minLength": 1,
                                },
                                "provider_header_request_id": {
                                    "type": ["string", "null"],
                                },
                                "provider_header_request_id_header": {
                                    "type": ["string", "null"],
                                },
                                "attempt_started_at": {
                                    "type": "string",
                                    "format": "date-time",
                                },
                                "usage": {
                                    "type": "object",
                                    "additionalProperties": False,
                                    "required": [
                                        "status",
                                        "input_tokens",
                                        "output_tokens",
                                        "total_tokens",
                                        "prompt_cache_hit_tokens",
                                        "prompt_cache_miss_tokens",
                                    ],
                                    "properties": {
                                        "status": {"const": "available"},
                                        "input_tokens": {
                                            "type": "integer",
                                            "minimum": 0,
                                        },
                                        "output_tokens": {
                                            "type": "integer",
                                            "minimum": 1,
                                        },
                                        "total_tokens": {
                                            "type": "integer",
                                            "minimum": 1,
                                        },
                                        "prompt_cache_hit_tokens": {
                                            "type": "integer",
                                            "minimum": 0,
                                        },
                                        "prompt_cache_miss_tokens": {
                                            "type": "integer",
                                            "minimum": 0,
                                        },
                                    },
                                },
                            },
                        },
                    },
                },
            },
        },
        "platform_evidence": {
            "type": "object",
            "additionalProperties": False,
            "required": [
                "source",
                "artifact_sha256",
                "observed_at",
                "matched_provider_response_ids",
            ],
            "properties": {
                "source": {
                    "type": ["string", "null"],
                    "enum": [*PROVIDER_ATTESTATION_SOURCES, None],
                },
                "artifact_sha256": {
                    "type": ["string", "null"],
                    "pattern": "^[0-9a-f]{64}$",
                },
                "observed_at": {
                    "type": ["string", "null"],
                    "format": "date-time",
                },
                "matched_provider_response_ids": {
                    "type": "array",
                    "items": {"type": "string", "minLength": 1},
                    "uniqueItems": True,
                },
            },
        },
        "reviewer": {
            "type": "object",
            "additionalProperties": False,
            "required": ["name", "organization", "reviewed_at"],
            "properties": {
                "name": {"type": ["string", "null"]},
                "organization": {"type": ["string", "null"]},
                "reviewed_at": {
                    "type": ["string", "null"],
                    "format": "date-time",
                },
            },
        },
        "signature": {
            "type": "object",
            "additionalProperties": False,
            "required": ["algorithm", "key_id", "value"],
            "properties": {
                "algorithm": {"const": PROVIDER_ATTESTATION_ALGORITHM},
                "key_id": {"type": ["string", "null"]},
                "value": {
                    "type": ["string", "null"],
                    "pattern": "^[0-9a-f]{64}$",
                },
            },
        },
    },
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

    def get_experiment(
        self, experiment_id: str, project_id: int
    ) -> dict[str, Any]:
        query = urlencode({"project_id": project_id})
        return _unwrap_object(
            self._json(
                "GET",
                f"/api/v2/research/experiments/{experiment_id}?{query}",
            ),
            "experiment",
        )

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

    def list_agent_events(self, run_id: str) -> list[dict[str, Any]]:
        payload = self._json(
            "GET", f"/api/v2/agent/runs/{run_id}/events?after_seq=0"
        )
        return _unwrap_list(payload, "events")

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


def _parse_utc_timestamp(value: Any, field: str) -> datetime:
    if not isinstance(value, str) or not value.strip():
        raise ResearchE2EError(f"{field} must be a non-empty RFC3339 timestamp")
    normalized = value.strip()
    if normalized.endswith("Z"):
        normalized = normalized[:-1] + "+00:00"
    try:
        parsed = datetime.fromisoformat(normalized)
    except ValueError as exc:
        raise ResearchE2EError(f"{field} is not a valid timestamp") from exc
    if parsed.tzinfo is None:
        raise ResearchE2EError(f"{field} must include a UTC offset")
    return parsed.astimezone(UTC)


def _required_string(value: Any, field: str) -> str:
    if not isinstance(value, str) or not value.strip():
        raise ResearchE2EError(f"{field} must be a non-empty string")
    return value.strip()


def _required_non_negative_integer(value: Any, field: str) -> int:
    if not isinstance(value, int) or isinstance(value, bool) or value < 0:
        raise ResearchE2EError(f"{field} must be a non-negative integer")
    return value


def _canonical_sha256(value: Any) -> str:
    return hashlib.sha256(_canonical_json_bytes(value)).hexdigest()


def _canonical_json_bytes(value: Any) -> bytes:
    return json.dumps(
        value,
        ensure_ascii=False,
        sort_keys=True,
        separators=(",", ":"),
    ).encode("utf-8")


def _provider_attestation_key() -> bytes:
    raw = os.environ.get(PROVIDER_ATTESTATION_KEY_ENV)
    if raw is None:
        raise ResearchE2EError(
            f"{PROVIDER_ATTESTATION_KEY_ENV} is required"
        )
    key = raw.encode("utf-8")
    if len(key) < 32:
        raise ResearchE2EError(
            f"{PROVIDER_ATTESTATION_KEY_ENV} must contain at least 32 bytes"
        )
    for name, value in os.environ.items():
        if (
            name != PROVIDER_ATTESTATION_KEY_ENV
            and name.endswith("_API_KEY")
            and value
            and hmac.compare_digest(key, value.encode("utf-8"))
        ):
            raise ResearchE2EError(
                f"{PROVIDER_ATTESTATION_KEY_ENV} must not reuse {name}"
            )
    return key


def _attestation_signature_payload(
    attestation: dict[str, Any],
) -> dict[str, Any]:
    payload = json.loads(json.dumps(attestation))
    signature = payload.get("signature")
    if not isinstance(signature, dict):
        raise ResearchE2EError("provider attestation signature is invalid")
    signature.pop("value", None)
    return payload


def _provider_attestation_signature_value(
    attestation: dict[str, Any],
    key: bytes,
) -> str:
    return hmac.new(
        key,
        _canonical_json_bytes(_attestation_signature_payload(attestation)),
        hashlib.sha256,
    ).hexdigest()


def _sha256_file(path: Path) -> str:
    try:
        if not path.is_file():
            raise ResearchE2EError(
                f"platform evidence file is not a regular file: {path}"
            )
        digest = hashlib.sha256()
        with path.open("rb") as stream:
            for chunk in iter(lambda: stream.read(1024 * 1024), b""):
                digest.update(chunk)
        return digest.hexdigest()
    except OSError as exc:
        raise ResearchE2EError(
            f"cannot hash platform evidence file {path}: {exc}"
        ) from exc


def _provider_attempts_for_run(
    *,
    experiment: dict[str, Any],
    run: dict[str, Any],
    agent_run_id: str,
    events: list[dict[str, Any]],
) -> dict[str, Any]:
    run_id = _required_string(run.get("id"), "research run id")
    run_started_at = _parse_utc_timestamp(
        run.get("started_at"), f"research run {run_id} started_at"
    )
    run_finished_at = _parse_utc_timestamp(
        run.get("finished_at"), f"research run {run_id} finished_at"
    )
    if run_finished_at < run_started_at:
        raise ResearchE2EError(
            f"research run {run_id} finished_at precedes started_at"
        )
    expected_provider = _required_string(
        experiment.get("model_provider"), "experiment model_provider"
    )
    expected_model = _required_string(
        experiment.get("model_name"), "experiment model_name"
    )
    attempts: list[dict[str, Any]] = []
    credential_fingerprint = ""
    endpoint_hosts: set[str] = set()
    for event in events:
        if event.get("type") != "research.llm_call":
            continue
        payload = event.get("payload")
        if not isinstance(payload, dict):
            raise ResearchE2EError(
                f"research run {run_id} has a non-object LLM event payload"
            )
        if payload.get("attempt_status") != "succeeded":
            continue
        event_seq = event.get("seq")
        if (
            not isinstance(event_seq, int)
            or isinstance(event_seq, bool)
            or event_seq < 1
        ):
            raise ResearchE2EError(
                f"research run {run_id} has an invalid LLM event sequence"
            )
        if event.get("run_id") != agent_run_id:
            raise ResearchE2EError(
                f"research run {run_id} LLM event {event_seq} has a "
                "mismatched AgentRun"
            )
        if payload.get("schema_version") != "research.llm_call.v1":
            raise ResearchE2EError(
                f"research run {run_id} has an unsupported LLM event schema"
            )
        provider = _required_string(
            payload.get("provider"), f"LLM event {event_seq} provider"
        )
        requested_model = _required_string(
            payload.get("requested_model"),
            f"LLM event {event_seq} requested_model",
        )
        resolved_model = _required_string(
            payload.get("resolved_model"),
            f"LLM event {event_seq} resolved_model",
        )
        if provider != expected_provider:
            raise ResearchE2EError(
                f"research run {run_id} LLM event {event_seq} provider mismatch"
            )
        if (
            requested_model != expected_model
            or resolved_model != expected_model
        ):
            raise ResearchE2EError(
                f"research run {run_id} LLM event {event_seq} model mismatch"
            )
        endpoint_scheme = _required_string(
            payload.get("endpoint_scheme"),
            f"LLM event {event_seq} endpoint_scheme",
        )
        endpoint_host = _required_string(
            payload.get("endpoint_host"),
            f"LLM event {event_seq} endpoint_host",
        )
        if endpoint_scheme != "https":
            raise ResearchE2EError(
                f"research run {run_id} LLM event {event_seq} did not use https"
            )
        if provider == "deepseek" and endpoint_host != "api.deepseek.com":
            raise ResearchE2EError(
                f"research run {run_id} LLM event {event_seq} used invalid "
                f"DeepSeek host {endpoint_host!r}"
            )
        endpoint_hosts.add(endpoint_host)
        http_status = payload.get("http_status")
        if (
            not isinstance(http_status, int)
            or isinstance(http_status, bool)
            or http_status != 200
        ):
            raise ResearchE2EError(
                f"research run {run_id} LLM event {event_seq} HTTP status "
                "is not 200"
            )
        provider_response_id = _required_string(
            payload.get("provider_response_id"),
            f"LLM event {event_seq} provider_response_id",
        )
        client_request_id = _required_string(
            payload.get("client_request_id"),
            f"LLM event {event_seq} client_request_id",
        )
        fingerprint = _required_string(
            payload.get("credential_fingerprint"),
            f"LLM event {event_seq} credential_fingerprint",
        )
        if re.fullmatch(r"sha256:v1:[0-9a-f]{64}", fingerprint) is None:
            raise ResearchE2EError(
                f"research run {run_id} LLM event {event_seq} has an "
                "invalid credential fingerprint"
            )
        if credential_fingerprint and fingerprint != credential_fingerprint:
            raise ResearchE2EError(
                f"research run {run_id} used inconsistent credentials"
            )
        credential_fingerprint = fingerprint
        if payload.get("local_response_cache") != "not_configured":
            raise ResearchE2EError(
                f"research run {run_id} LLM event {event_seq} configured a "
                "local response cache"
            )
        attempt_started_at = _parse_utc_timestamp(
            payload.get("attempt_started_at"),
            f"LLM event {event_seq} attempt_started_at",
        )
        if not run_started_at <= attempt_started_at <= run_finished_at:
            raise ResearchE2EError(
                f"research run {run_id} LLM event {event_seq} started outside "
                "the run window"
            )
        usage = payload.get("usage")
        if not isinstance(usage, dict) or usage.get("status") != "available":
            raise ResearchE2EError(
                f"research run {run_id} LLM event {event_seq} usage is unavailable"
            )
        normalized_usage = {
            field: _required_non_negative_integer(
                usage.get(field), f"LLM event {event_seq} usage.{field}"
            )
            for field in (
                "input_tokens",
                "output_tokens",
                "total_tokens",
                "prompt_cache_hit_tokens",
                "prompt_cache_miss_tokens",
            )
        }
        if (
            normalized_usage["output_tokens"] <= 0
            or normalized_usage["total_tokens"] <= 0
        ):
            raise ResearchE2EError(
                f"research run {run_id} LLM event {event_seq} usage must have "
                "positive output and total tokens"
            )
        header_id = payload.get("provider_header_request_id")
        header_name = payload.get("provider_header_request_id_header")
        for value, field in (
            (header_id, "provider_header_request_id"),
            (header_name, "provider_header_request_id_header"),
        ):
            if value is not None and (
                not isinstance(value, str) or not value.strip()
            ):
                raise ResearchE2EError(
                    f"LLM event {event_seq} {field} must be null or non-empty"
                )
        if bool(header_id) != bool(header_name):
            raise ResearchE2EError(
                f"LLM event {event_seq} provider header request ID is incomplete"
            )
        attempts.append(
            {
                "source_event_seq": event_seq,
                "logical_call_id": _required_string(
                    payload.get("logical_call_id"),
                    f"LLM event {event_seq} logical_call_id",
                ),
                "client_request_id": client_request_id,
                "provider_response_id": provider_response_id,
                "provider_header_request_id": (
                    header_id.strip() if isinstance(header_id, str) else None
                ),
                "provider_header_request_id_header": (
                    header_name.strip()
                    if isinstance(header_name, str)
                    else None
                ),
                "attempt_started_at": attempt_started_at.isoformat(),
                "http_status": http_status,
                "usage": {"status": "available", **normalized_usage},
            }
        )
    if not attempts:
        raise ResearchE2EError(
            f"research run {run_id} has no successful provider LLM attempt"
        )
    if len(endpoint_hosts) != 1:
        raise ResearchE2EError(
            f"research run {run_id} used inconsistent provider hosts"
        )
    attempts.sort(key=lambda attempt: attempt["source_event_seq"])
    return {
        "research_run_id": run_id,
        "agent_run_id": agent_run_id,
        "run_started_at": run_started_at.isoformat(),
        "run_finished_at": run_finished_at.isoformat(),
        "provider": expected_provider,
        "model": expected_model,
        "endpoint_scheme": "https",
        "endpoint_host": next(iter(endpoint_hosts)),
        "credential_fingerprint": credential_fingerprint,
        "local_response_cache": "not_configured",
        "attempts": attempts,
    }


def _provider_evidence_artifact(
    *,
    experiment_id: str,
    project_id: int,
    runs: list[dict[str, Any]],
) -> dict[str, Any]:
    runs = sorted(runs, key=lambda run: run["research_run_id"])
    credential_fingerprints = {
        run["credential_fingerprint"] for run in runs
    }
    if len(credential_fingerprints) != 1:
        raise ResearchE2EError(
            "formal research runs used inconsistent credential fingerprints"
        )
    response_owners: dict[str, str] = {}
    client_owners: dict[str, str] = {}
    for run in runs:
        run_id = run["research_run_id"]
        for attempt in run["attempts"]:
            for value, owners, label in (
                (
                    attempt["provider_response_id"],
                    response_owners,
                    "provider response ID",
                ),
                (
                    attempt["client_request_id"],
                    client_owners,
                    "client request ID",
                ),
            ):
                previous = owners.get(value)
                if previous is not None and previous != run_id:
                    raise ResearchE2EError(
                        f"{label} {value!r} is reused across formal runs "
                        f"{previous} and {run_id}"
                    )
                owners[value] = run_id
    source = {
        "experiment_id": experiment_id,
        "project_id": project_id,
        "runs": runs,
    }
    artifact = {
        "schema_version": PROVIDER_EVIDENCE_SCHEMA_VERSION,
        **source,
        "source_sha256": _canonical_sha256(source),
    }
    artifact["evidence_artifact_sha256"] = _canonical_sha256(artifact)
    return artifact


def _attestation_runs_from_evidence(
    evidence: dict[str, Any],
) -> list[dict[str, Any]]:
    return [
        {
            "research_run_id": run["research_run_id"],
            "agent_run_id": run["agent_run_id"],
            "source_event_seqs": [
                attempt["source_event_seq"] for attempt in run["attempts"]
            ],
            "run_started_at": run["run_started_at"],
            "run_finished_at": run["run_finished_at"],
            "provider": run["provider"],
            "endpoint_scheme": run["endpoint_scheme"],
            "endpoint_host": run["endpoint_host"],
            "model": run["model"],
            "credential_fingerprint": run["credential_fingerprint"],
            "attempts": [
                {
                    key: attempt[key]
                    for key in (
                        "client_request_id",
                        "provider_response_id",
                        "provider_header_request_id",
                        "provider_header_request_id_header",
                        "attempt_started_at",
                        "usage",
                    )
                }
                for attempt in run["attempts"]
            ],
        }
        for run in evidence["runs"]
    ]


def build_pending_provider_attestation(
    evidence: dict[str, Any],
) -> dict[str, Any]:
    return {
        "schema_version": PROVIDER_ATTESTATION_SCHEMA_VERSION,
        "experiment_id": evidence["experiment_id"],
        "source_sha256": evidence["source_sha256"],
        "evidence_artifact_sha256": evidence["evidence_artifact_sha256"],
        "runs": _attestation_runs_from_evidence(evidence),
        "platform_evidence": {
            "source": None,
            "artifact_sha256": None,
            "observed_at": None,
            "matched_provider_response_ids": [],
        },
        "reviewer": {
            "name": None,
            "organization": None,
            "reviewed_at": None,
        },
        "signature": {
            "algorithm": PROVIDER_ATTESTATION_ALGORITHM,
            "key_id": None,
            "value": None,
        },
    }


def _load_json_object(path: Path, label: str) -> dict[str, Any]:
    try:
        payload = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as exc:
        raise ResearchE2EError(f"cannot load {label} {path}: {exc}") from exc
    if not isinstance(payload, dict):
        raise ResearchE2EError(f"{label} must be a JSON object")
    return payload


def _validate_attestation_runs(runs: Any) -> list[datetime]:
    if not isinstance(runs, list) or not runs:
        raise ResearchE2EError("provider attestation runs are invalid")
    run_fields = {
        "research_run_id",
        "agent_run_id",
        "source_event_seqs",
        "run_started_at",
        "run_finished_at",
        "provider",
        "endpoint_scheme",
        "endpoint_host",
        "model",
        "credential_fingerprint",
        "attempts",
    }
    attempt_fields = {
        "client_request_id",
        "provider_response_id",
        "provider_header_request_id",
        "provider_header_request_id_header",
        "attempt_started_at",
        "usage",
    }
    usage_fields = {
        "status",
        "input_tokens",
        "output_tokens",
        "total_tokens",
        "prompt_cache_hit_tokens",
        "prompt_cache_miss_tokens",
    }
    response_ids: set[str] = set()
    finished_times: list[datetime] = []
    for run_index, run in enumerate(runs):
        label = f"provider attestation runs[{run_index}]"
        if not isinstance(run, dict) or set(run) != run_fields:
            raise ResearchE2EError(f"{label} fields are invalid")
        for field in (
            "research_run_id",
            "agent_run_id",
            "provider",
            "endpoint_host",
            "model",
            "credential_fingerprint",
        ):
            _required_string(run.get(field), f"{label}.{field}")
        if run.get("endpoint_scheme") != "https":
            raise ResearchE2EError(f"{label}.endpoint_scheme must be https")
        if re.fullmatch(
            r"sha256:v1:[0-9a-f]{64}",
            str(run.get("credential_fingerprint") or ""),
        ) is None:
            raise ResearchE2EError(
                f"{label}.credential_fingerprint is invalid"
            )
        started_at = _parse_utc_timestamp(
            run.get("run_started_at"), f"{label}.run_started_at"
        )
        finished_at = _parse_utc_timestamp(
            run.get("run_finished_at"), f"{label}.run_finished_at"
        )
        if finished_at < started_at:
            raise ResearchE2EError(
                f"{label}.run_finished_at precedes run_started_at"
            )
        finished_times.append(finished_at)
        attempts = run.get("attempts")
        source_event_seqs = run.get("source_event_seqs")
        if (
            not isinstance(attempts, list)
            or not attempts
            or not isinstance(source_event_seqs, list)
            or len(source_event_seqs) != len(attempts)
            or any(
                not isinstance(seq, int)
                or isinstance(seq, bool)
                or seq < 1
                for seq in source_event_seqs
            )
            or len(set(source_event_seqs)) != len(source_event_seqs)
        ):
            raise ResearchE2EError(f"{label} attempts are invalid")
        for attempt_index, attempt in enumerate(attempts):
            attempt_label = f"{label}.attempts[{attempt_index}]"
            if not isinstance(attempt, dict) or set(attempt) != attempt_fields:
                raise ResearchE2EError(f"{attempt_label} fields are invalid")
            for field in ("client_request_id", "provider_response_id"):
                _required_string(
                    attempt.get(field), f"{attempt_label}.{field}"
                )
            response_id = str(attempt["provider_response_id"])
            if response_id in response_ids:
                raise ResearchE2EError(
                    "provider attestation response IDs must be unique"
                )
            response_ids.add(response_id)
            header_id = attempt.get("provider_header_request_id")
            header_name = attempt.get("provider_header_request_id_header")
            for value, field in (
                (header_id, "provider_header_request_id"),
                (header_name, "provider_header_request_id_header"),
            ):
                if value is not None:
                    _required_string(value, f"{attempt_label}.{field}")
            if bool(header_id) != bool(header_name):
                raise ResearchE2EError(
                    f"{attempt_label} provider header ID is incomplete"
                )
            attempted_at = _parse_utc_timestamp(
                attempt.get("attempt_started_at"),
                f"{attempt_label}.attempt_started_at",
            )
            if not started_at <= attempted_at <= finished_at:
                raise ResearchE2EError(
                    f"{attempt_label} is outside the run window"
                )
            usage = attempt.get("usage")
            if not isinstance(usage, dict) or set(usage) != usage_fields:
                raise ResearchE2EError(f"{attempt_label}.usage is invalid")
            if usage.get("status") != "available":
                raise ResearchE2EError(
                    f"{attempt_label}.usage.status is invalid"
                )
            for field in usage_fields - {"status"}:
                _required_non_negative_integer(
                    usage.get(field), f"{attempt_label}.usage.{field}"
                )
            if usage["output_tokens"] <= 0 or usage["total_tokens"] <= 0:
                raise ResearchE2EError(
                    f"{attempt_label}.usage must contain positive token output"
                )
    return finished_times


def _validate_provider_attestation_shape(
    attestation: dict[str, Any],
    *,
    allow_pending: bool,
) -> None:
    required = set(PROVIDER_ATTESTATION_JSON_SCHEMA["required"])
    if set(attestation) != required:
        raise ResearchE2EError(
            "provider attestation fields do not match the required schema"
        )
    if attestation.get("schema_version") != PROVIDER_ATTESTATION_SCHEMA_VERSION:
        raise ResearchE2EError("provider attestation schema_version is invalid")
    _required_string(
        attestation.get("experiment_id"),
        "provider attestation experiment_id",
    )
    for field in (
        "source_sha256",
        "evidence_artifact_sha256",
    ):
        if (
            re.fullmatch(
                r"[0-9a-f]{64}", str(attestation.get(field) or "")
            )
            is None
        ):
            raise ResearchE2EError(
                f"provider attestation {field} is invalid"
            )
    run_finished_times = _validate_attestation_runs(attestation.get("runs"))
    platform_evidence = attestation.get("platform_evidence")
    reviewer = attestation.get("reviewer")
    signature = attestation.get("signature")
    if not isinstance(platform_evidence, dict) or set(platform_evidence) != {
        "source",
        "artifact_sha256",
        "observed_at",
        "matched_provider_response_ids",
    }:
        raise ResearchE2EError(
            "provider attestation platform_evidence is invalid"
        )
    if not isinstance(reviewer, dict) or set(reviewer) != {
        "name",
        "organization",
        "reviewed_at",
    }:
        raise ResearchE2EError("provider attestation reviewer is invalid")
    if not isinstance(signature, dict) or set(signature) != {
        "algorithm",
        "key_id",
        "value",
    }:
        raise ResearchE2EError("provider attestation signature is invalid")
    if signature.get("algorithm") != PROVIDER_ATTESTATION_ALGORITHM:
        raise ResearchE2EError(
            "provider attestation signature algorithm is invalid"
        )
    if allow_pending:
        if platform_evidence != {
            "source": None,
            "artifact_sha256": None,
            "observed_at": None,
            "matched_provider_response_ids": [],
        }:
            raise ResearchE2EError(
                "pending provider attestation platform_evidence is not empty"
            )
        if any(value is not None for value in reviewer.values()):
            raise ResearchE2EError(
                "pending provider attestation reviewer is not empty"
            )
        if (
            signature.get("key_id") is not None
            or signature.get("value") is not None
        ):
            raise ResearchE2EError(
                "pending provider attestation signature is not empty"
            )
        return

    source = platform_evidence.get("source")
    if source not in PROVIDER_ATTESTATION_SOURCES:
        raise ResearchE2EError(
            "provider attestation platform_evidence.source is invalid"
        )
    if re.fullmatch(
        r"[0-9a-f]{64}",
        str(platform_evidence.get("artifact_sha256") or ""),
    ) is None:
        raise ResearchE2EError(
            "provider attestation platform evidence artifact hash is invalid"
        )
    observed_at = _parse_utc_timestamp(
        platform_evidence.get("observed_at"),
        "provider attestation platform_evidence.observed_at",
    )
    matched_ids = platform_evidence.get("matched_provider_response_ids")
    if (
        not isinstance(matched_ids, list)
        or not matched_ids
        or any(
            not isinstance(value, str) or not value.strip()
            for value in matched_ids
        )
        or len(set(matched_ids)) != len(matched_ids)
    ):
        raise ResearchE2EError(
            "provider attestation matched provider response IDs are invalid"
        )
    _required_string(
        reviewer.get("name"), "provider attestation reviewer.name"
    )
    _required_string(
        reviewer.get("organization"),
        "provider attestation reviewer.organization",
    )
    reviewed_at = _parse_utc_timestamp(
        reviewer.get("reviewed_at"),
        "provider attestation reviewer.reviewed_at",
    )
    if observed_at < max(run_finished_times):
        raise ResearchE2EError(
            "platform evidence was observed before the provider runs finished"
        )
    if reviewed_at < observed_at:
        raise ResearchE2EError(
            "provider attestation was reviewed before platform evidence"
        )
    _required_string(
        signature.get("key_id"), "provider attestation signature.key_id"
    )
    if re.fullmatch(r"[0-9a-f]{64}", str(signature.get("value") or "")) is None:
        raise ResearchE2EError(
            "provider attestation signature.value is invalid"
        )


def _verify_provider_attestation_signature(
    attestation: dict[str, Any],
) -> None:
    expected = _provider_attestation_signature_value(
        attestation, _provider_attestation_key()
    )
    actual = str(attestation["signature"]["value"])
    if not hmac.compare_digest(actual, expected):
        raise ResearchE2EError(
            "provider attestation signature verification failed"
        )


def load_provider_attestation(path: Path) -> dict[str, Any]:
    payload = _load_json_object(path, "provider attestation")
    _validate_provider_attestation_shape(payload, allow_pending=False)
    _verify_provider_attestation_signature(payload)
    return payload


def create_provider_attestation(
    pending_path: Path,
    platform_evidence_path: Path,
    *,
    source: str,
    reviewer: str,
    organization: str,
    key_id: str,
    now: Callable[[], datetime] = lambda: datetime.now(UTC),
) -> dict[str, Any]:
    pending = _load_json_object(
        pending_path, "pending provider attestation"
    )
    _validate_provider_attestation_shape(pending, allow_pending=True)
    source = _required_string(source, "platform evidence source")
    if source not in PROVIDER_ATTESTATION_SOURCES:
        raise ResearchE2EError("platform evidence source is invalid")
    reviewer = _required_string(reviewer, "reviewer")
    organization = _required_string(organization, "organization")
    key_id = _required_string(key_id, "key_id")
    response_ids = sorted(
        attempt["provider_response_id"]
        for run in pending["runs"]
        for attempt in run["attempts"]
    )
    if len(response_ids) != len(set(response_ids)):
        raise ResearchE2EError(
            "pending provider response IDs must be unique"
        )
    observed = now()
    if observed.tzinfo is None:
        raise ResearchE2EError("attestation timestamp must include a UTC offset")
    observed_at = observed.astimezone(UTC).replace(
        microsecond=0
    ).isoformat()
    attestation = json.loads(json.dumps(pending))
    attestation["platform_evidence"] = {
        "source": source,
        "artifact_sha256": _sha256_file(platform_evidence_path),
        "observed_at": observed_at,
        "matched_provider_response_ids": response_ids,
    }
    attestation["reviewer"] = {
        "name": reviewer,
        "organization": organization,
        "reviewed_at": observed_at,
    }
    attestation["signature"] = {
        "algorithm": PROVIDER_ATTESTATION_ALGORITHM,
        "key_id": key_id,
        "value": None,
    }
    key = _provider_attestation_key()
    attestation["signature"]["value"] = (
        _provider_attestation_signature_value(attestation, key)
    )
    _validate_provider_attestation_shape(attestation, allow_pending=False)
    return attestation


def _validate_provider_attestation(
    attestation: dict[str, Any],
    evidence: dict[str, Any],
) -> dict[str, Any]:
    _validate_provider_attestation_shape(attestation, allow_pending=False)
    _verify_provider_attestation_signature(attestation)
    for field in (
        "experiment_id",
        "source_sha256",
        "evidence_artifact_sha256",
    ):
        if attestation.get(field) != evidence.get(field):
            raise ResearchE2EError(
                f"provider attestation {field} does not match local evidence"
            )
    expected_runs = _attestation_runs_from_evidence(evidence)
    if attestation.get("runs") != expected_runs:
        raise ResearchE2EError(
            "provider attestation runs do not match local provider evidence"
        )
    expected_response_ids = sorted(
        attempt["provider_response_id"]
        for run in evidence["runs"]
        for attempt in run["attempts"]
    )
    if (
        attestation["platform_evidence"]["matched_provider_response_ids"]
        != expected_response_ids
    ):
        raise ResearchE2EError(
            "platform evidence response IDs do not exactly cover local evidence"
        )
    return {
        "provided": True,
        "schema_valid": True,
        "local_evidence_binding_verified": True,
        "reviewer_fields_complete": True,
        "signature_fields_complete": True,
        "signature_verified": True,
        "trust_reason": None,
    }


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
        "long_operation_seconds",
        "cancel_grace_seconds",
    }
    if unknown_timeouts:
        raise ValueError(f"unsupported timeout fields: {sorted(unknown_timeouts)}")
    run_seconds = float(timeouts.get("run_seconds", 900))
    request_seconds = float(
        timeouts.get("request_seconds", DEFAULT_REQUEST_TIMEOUT_SECONDS)
    )
    long_operation_seconds = float(
        timeouts.get(
            "long_operation_seconds",
            DEFAULT_LONG_OPERATION_TIMEOUT_SECONDS,
        )
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
        or long_operation_seconds <= 0
        or cancel_grace_seconds < 0
        or experiment_seconds <= 0
        or experiment_seconds > MAX_EXPERIMENT_TIMEOUT_SECONDS
    ):
        raise ValueError("timeouts must be positive and experiment timeout <= 86400s")
    if (
        not run_seconds.is_integer()
        or not request_seconds.is_integer()
        or not long_operation_seconds.is_integer()
        or not cancel_grace_seconds.is_integer()
        or run_seconds < 60
        or run_seconds > 3600
        or request_seconds > 3600
        or long_operation_seconds > 3600
        or cancel_grace_seconds > 300
    ):
        raise ValueError(
            "run/request/long-operation/cancel timeouts must be whole seconds "
            "within supported limits"
        )
    payload["timeouts"] = {
        "run_seconds": run_seconds,
        "experiment_seconds": experiment_seconds,
        "request_seconds": request_seconds,
        "long_operation_seconds": long_operation_seconds,
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


def _is_approval_question(payload: dict[str, Any]) -> bool:
    questions = payload.get("questions")
    return isinstance(questions, list) and any(
        isinstance(question, dict)
        and question.get("id") == "approve_dsl"
        for question in questions
    )


def _approval_answer(payload: dict[str, Any]) -> bool:
    answers = payload.get("answers")
    return (
        payload.get("tool") == "ask_user_question"
        and isinstance(answers, dict)
        and answers.get("approve_dsl") is True
    )


def _generation_approval_binding(
    events: list[dict[str, Any]],
    generation_id: int,
    expected_profile: str,
) -> dict[str, Any]:
    generated: dict[str, Any] | None = None
    artifact_seq = 0
    for event in events:
        payload = event.get("payload") or {}
        content = payload.get("content")
        if (
            event.get("type") == "tool.result"
            and payload.get("tool") == "generate_dsl"
            and isinstance(content, dict)
            and int(content.get("generation_id") or 0) == generation_id
        ):
            generated = content
        if (
            event.get("type") == "artifact.published"
            and payload.get("type") == "dsl_generation"
            and int(payload.get("id") or 0) == generation_id
        ):
            artifact_seq = int(event.get("seq") or 0)
    if generated is None or artifact_seq <= 0:
        raise ResearchE2EError(
            f"generation {generation_id} is missing structured event facts"
        )
    try:
        binding = _validate_generation_binding(
            generated,
            generation_id,
            expected_profile=expected_profile,
        )
    except Exception as exc:
        raise ResearchE2EError(
            f"generation {generation_id} canonical binding is invalid: {exc}"
        ) from exc

    pending: dict[str, Any] | None = None
    for event in events:
        payload = event.get("payload") or {}
        if (
            int(event.get("seq") or 0) > artifact_seq
            and event.get("type") == "tool.pending"
            and _is_approval_question(payload)
        ):
            pending = event
            break
    if pending is None or not pending.get("tool_call_id"):
        raise ResearchE2EError(
            f"generation {generation_id} has no approval checkpoint"
        )
    approval = next(
        (
            event
            for event in events
            if int(event.get("seq") or 0) > int(pending.get("seq") or 0)
            and event.get("type") == "tool.result"
            and event.get("tool_call_id") == pending.get("tool_call_id")
            and _approval_answer(event.get("payload") or {})
        ),
        None,
    )
    if approval is None:
        raise ResearchE2EError(
            f"generation {generation_id} has no positive approval result"
        )
    return {
        **binding,
        "generation_id": generation_id,
        "approval_tool_call_id": pending["tool_call_id"],
        "approval_event_seq": int(approval.get("seq") or 0),
    }


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
                    long_operation_timeout=spec["timeouts"][
                        "long_operation_seconds"
                    ],
                ),
                timeout_seconds=driver_timeout,
                mutation=spec["oracle_mutation"],
                expected_dsl_profile=spec["controls"]["dsl_profile"],
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
    provider_attestation: dict[str, Any] | None = None,
    allow_pending_platform_attestation: bool = False,
) -> dict[str, Any]:
    experiment = client.get_experiment(experiment_id, project_id)
    dsl_profile = experiment.get("dsl_profile")
    if dsl_profile not in PROFILE_CANONICAL_VERSIONS:
        raise ResearchE2EError(
            f"experiment {experiment_id} has unsupported dsl_profile {dsl_profile!r}"
        )
    canonical_version = PROFILE_CANONICAL_VERSIONS[dsl_profile]
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
    provider_runs: list[dict[str, Any]] = []
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
        events = client.list_agent_events(str(links["agent_run_id"]))
        approval = _generation_approval_binding(
            events,
            int(links["generation_id"]),
            str(dsl_profile),
        )
        provider_runs.append(
            _provider_attempts_for_run(
                experiment=experiment,
                run=run,
                agent_run_id=str(links["agent_run_id"]),
                events=events,
            )
        )
        if approval["dsl_sha256"] != links["dsl_sha256"]:
            raise ResearchE2EError(
                f"research run {run_id} generation link SHA mismatch"
            )
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
        job = report["jobs"][0]
        expected_binding = {
            "dsl_profile": dsl_profile,
            "dsl_canonical_version": canonical_version,
            "dsl_sha256": links["dsl_sha256"],
        }
        for label, value in (
            ("approval", approval),
            ("report", report),
            ("job", job),
            ("execution", execution),
            ("structured report", execution.get("report") or {}),
        ):
            actual_binding = {
                field: value.get(field) for field in expected_binding
            }
            if actual_binding != expected_binding:
                raise ResearchE2EError(
                    f"research run {run_id} {label} DSL binding mismatch"
                )
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
                "dsl_profile": dsl_profile,
                "dsl_canonical_version": canonical_version,
                "dsl_sha256": links["dsl_sha256"],
                "task_success": task_success,
                "execution_success": execution_success,
                "verification_success": verification_success,
                "oracle_passed": oracle_passed,
                "vision_calls": vision_calls,
            }
        )

    provider_artifact = _provider_evidence_artifact(
        experiment_id=experiment_id,
        project_id=project_id,
        runs=provider_runs,
    )
    attestation_status = {
        "provided": False,
        "schema_valid": False,
        "local_evidence_binding_verified": False,
        "reviewer_fields_complete": False,
        "signature_fields_complete": False,
        "signature_verified": False,
        "trust_reason": "platform_attestation_not_provided",
    }
    if provider_attestation is not None:
        attestation_status = _validate_provider_attestation(
            provider_attestation, provider_artifact
        )
    provider_verified = attestation_status["signature_verified"]
    business_gate_passed = (
        len(verified) == expected_repetitions
        and all(
            run["task_success"] is expected_task_success
            and run["oracle_passed"] is expected_task_success
            and run["execution_success"] is True
            and run["verification_success"] is True
            and run["vision_calls"] == 0
            for run in verified
        )
    )
    provider_evidence = {
        **provider_artifact,
        "successful_attempt_count": sum(
            len(run["attempts"]) for run in provider_runs
        ),
        "verification_scope": (
            "local_and_platform_attested"
            if provider_verified
            else "local_only"
        ),
        "local_provider_evidence_verified": True,
        "provider_e2e_verified": provider_verified,
        "reason": None if provider_verified else "platform_attestation_required",
        "attestation": attestation_status,
    }
    if allow_pending_platform_attestation:
        provider_evidence["pending_platform_attestation"] = (
            build_pending_provider_attestation(provider_artifact)
        )

    return {
        "schema_version": "research.e2e-verification.v1",
        "experiment_id": experiment_id,
        "project_id": project_id,
        "non_warmup_runs": len(formal_runs),
        "clean_session_count": len(session_ids),
        "expected_task_success": expected_task_success,
        "dsl_profile": dsl_profile,
        "dsl_canonical_version": canonical_version,
        "provider_evidence": provider_evidence,
        "runs": verified,
        "passed": business_gate_passed and provider_verified,
    }


NEGATIVE_CONTRACT_MARKERS = {
    "missing-intent": ("intent",),
    "unknown-action": ("unsupported dsl action",),
    "unexplored-selector": ("preflight", "verified"),
}


def verify_negative_contract_runs(
    cases: dict[str, str],
    *,
    client: ResearchAPIClient,
    project_id: int,
) -> dict[str, Any]:
    if set(cases) != set(NEGATIVE_CONTRACT_MARKERS):
        raise ValueError(
            "negative contract cases must include missing-intent, "
            "unknown-action, and unexplored-selector"
        )
    verified = []
    for name, run_id in sorted(cases.items()):
        run = client.get_agent_run(run_id)
        if int(run.get("project_id") or 0) != project_id:
            raise ResearchE2EError(f"negative run {run_id} project mismatch")
        if run.get("status") not in {"completed", "failed", "cancelled"}:
            raise ResearchE2EError(f"negative run {run_id} is not terminal")
        events = client.list_agent_events(run_id)
        forbidden_artifacts = [
            event
            for event in events
            if event.get("type") == "artifact.published"
            and (event.get("payload") or {}).get("type")
            in {"dsl_generation", "execution_batch"}
        ]
        if forbidden_artifacts:
            raise ResearchE2EError(
                f"negative run {run_id} published Generation/Batch artifacts"
            )
        if any(
            event.get("type") == "tool.started"
            and (event.get("payload") or {}).get("tool") == "execute_dsl"
            for event in events
        ):
            raise ResearchE2EError(
                f"negative run {run_id} reached execute_dsl"
            )
        failures = [
            event
            for event in events
            if event.get("type") == "tool.failed"
            and (event.get("payload") or {}).get("tool") == "generate_dsl"
        ]
        markers = NEGATIVE_CONTRACT_MARKERS[name]
        matched = next(
            (
                event
                for event in failures
                if all(
                    marker
                    in str((event.get("payload") or {}).get("message") or "").casefold()
                    for marker in markers
                )
            ),
            None,
        )
        if matched is None:
            raise ResearchE2EError(
                f"negative run {run_id} has no matching generate_dsl rejection"
            )
        verified.append(
            {
                "case": name,
                "agent_run_id": run_id,
                "failure_event_seq": matched.get("seq"),
                "generation_artifacts": 0,
                "batch_artifacts": 0,
            }
        )
    return {
        "schema_version": "research.negative-contract-verification.v1",
        "project_id": project_id,
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


def _add_provider_attestation_arguments(
    parser: argparse.ArgumentParser,
) -> None:
    parser.add_argument("--provider-attestation", type=Path)
    parser.add_argument(
        "--allow-pending-platform-attestation",
        action="store_true",
        help=(
            "emit a pending attestation package without marking the "
            "experiment verified"
        ),
    )
    parser.add_argument("--pending-platform-attestation-output", type=Path)


def _write_pending_attestation_if_requested(
    args: argparse.Namespace,
    result: dict[str, Any],
) -> None:
    output = args.pending_platform_attestation_output
    if output is None:
        return
    provider_evidence = result.get("provider_evidence")
    pending = (
        provider_evidence.get("pending_platform_attestation")
        if isinstance(provider_evidence, dict)
        else None
    )
    if not isinstance(pending, dict):
        raise ResearchE2EError(
            "pending attestation output requires "
            "--allow-pending-platform-attestation"
        )
    _atomic_write_json(output, pending)


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
    run_parser.add_argument("--long-operation-timeout-seconds", type=float)
    run_parser.add_argument("--output", type=Path)
    _add_provider_attestation_arguments(run_parser)

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
    _add_provider_attestation_arguments(verify_parser)

    negative_parser = subparsers.add_parser("negative-contract")
    negative_parser.add_argument("--project-id", type=int, required=True)
    negative_parser.add_argument(
        "--case",
        action="append",
        required=True,
        metavar="NAME=AGENT_RUN_ID",
    )
    negative_parser.add_argument(
        "--agent-url", default="http://127.0.0.1:8081"
    )
    negative_parser.add_argument(
        "--request-timeout-seconds", type=float, default=30
    )
    negative_parser.add_argument("--output", type=Path)

    export_parser = subparsers.add_parser("export")
    selector = export_parser.add_mutually_exclusive_group(required=True)
    selector.add_argument("--run-id")
    selector.add_argument("--experiment-id")
    export_parser.add_argument("--output", type=Path, required=True)
    export_parser.add_argument("--timeout-seconds", type=float, default=300)
    export_parser.add_argument("--export-binary", type=Path)

    attest_parser = subparsers.add_parser("provider-attest")
    attest_parser.add_argument("--pending", type=Path, required=True)
    attest_parser.add_argument(
        "--platform-evidence", type=Path, required=True
    )
    attest_parser.add_argument(
        "--source",
        choices=PROVIDER_ATTESTATION_SOURCES,
        required=True,
    )
    attest_parser.add_argument("--reviewer", required=True)
    attest_parser.add_argument("--organization", required=True)
    attest_parser.add_argument("--key-id", required=True)
    attest_parser.add_argument("--output", type=Path, required=True)

    subparsers.add_parser("code-sha256")
    schema_parser = subparsers.add_parser("provider-attestation-schema")
    schema_parser.add_argument("--output", type=Path)
    return parser


def main(argv: Sequence[str] | None = None) -> int:
    args = _build_parser().parse_args(argv)
    try:
        if args.command == "provider-attest":
            attestation = create_provider_attestation(
                args.pending,
                args.platform_evidence,
                source=args.source,
                reviewer=args.reviewer,
                organization=args.organization,
                key_id=args.key_id,
            )
            _atomic_write_json(args.output, attestation)
            print(
                json.dumps(
                    {
                        "output": str(args.output.resolve()),
                        "artifact_sha256": attestation[
                            "platform_evidence"
                        ]["artifact_sha256"],
                        "matched_provider_response_ids": attestation[
                            "platform_evidence"
                        ]["matched_provider_response_ids"],
                        "signature": {
                            "algorithm": attestation["signature"][
                                "algorithm"
                            ],
                            "key_id": attestation["signature"]["key_id"],
                        },
                    },
                    ensure_ascii=False,
                )
            )
            return 0
        if args.command == "run":
            if (
                args.pending_platform_attestation_output is not None
                and not args.allow_pending_platform_attestation
            ):
                raise ValueError(
                    "--pending-platform-attestation-output requires "
                    "--allow-pending-platform-attestation"
                )
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
            if args.long_operation_timeout_seconds is not None:
                spec["timeouts"][
                    "long_operation_seconds"
                ] = args.long_operation_timeout_seconds
            spec = load_experiment_spec_from_value(spec)
            research_client = ResearchAPIClient(
                args.agent_url,
                request_timeout=spec["timeouts"]["request_seconds"],
            )
            result = run_experiment(
                spec,
                project_id=args.project_id,
                research_client=research_client,
                agent_url=args.agent_url,
                browser_url=args.browser_url,
            )
            if result["success"]:
                verification = verify_experiment(
                    result["experiment_id"],
                    client=research_client,
                    project_id=args.project_id,
                    expected_repetitions=spec["repetitions"],
                    expected_task_success=True,
                    provider_attestation=(
                        load_provider_attestation(args.provider_attestation)
                        if args.provider_attestation is not None
                        else None
                    ),
                    allow_pending_platform_attestation=(
                        args.allow_pending_platform_attestation
                    ),
                )
                result["verification"] = verification
                result["provider_evidence"] = verification[
                    "provider_evidence"
                ]
                result["success"] = bool(
                    result["success"] and verification["passed"]
                )
                _write_pending_attestation_if_requested(args, result)
            else:
                result["provider_evidence"] = {
                    "schema_version": PROVIDER_EVIDENCE_SCHEMA_VERSION,
                    "local_provider_evidence_verified": False,
                    "provider_e2e_verified": False,
                    "reason": "research_run_failed_before_provider_gate",
                }
            if args.output:
                _atomic_write_json(args.output, result)
            print(json.dumps(result, ensure_ascii=False))
            return 0 if result["success"] else 1
        if args.command == "verify":
            if (
                args.pending_platform_attestation_output is not None
                and not args.allow_pending_platform_attestation
            ):
                raise ValueError(
                    "--pending-platform-attestation-output requires "
                    "--allow-pending-platform-attestation"
                )
            result = verify_experiment(
                args.experiment_id,
                client=ResearchAPIClient(
                    args.agent_url,
                    request_timeout=args.request_timeout_seconds,
                ),
                project_id=args.project_id,
                expected_repetitions=args.expected_repetitions,
                expected_task_success=args.expected_task_success == "true",
                provider_attestation=(
                    load_provider_attestation(args.provider_attestation)
                    if args.provider_attestation is not None
                    else None
                ),
                allow_pending_platform_attestation=(
                    args.allow_pending_platform_attestation
                ),
            )
            _write_pending_attestation_if_requested(args, result)
            if args.output:
                _atomic_write_json(args.output, result)
            print(json.dumps(result, ensure_ascii=False))
            return 0 if result["passed"] else 1
        if args.command == "negative-contract":
            cases: dict[str, str] = {}
            for raw in args.case:
                name, separator, run_id = raw.partition("=")
                if not separator or not name or not run_id or name in cases:
                    raise ValueError(
                        "--case must be unique NAME=AGENT_RUN_ID values"
                    )
                cases[name] = run_id
            result = verify_negative_contract_runs(
                cases,
                client=ResearchAPIClient(
                    args.agent_url,
                    request_timeout=args.request_timeout_seconds,
                ),
                project_id=args.project_id,
            )
            if args.output:
                _atomic_write_json(args.output, result)
            print(json.dumps(result, ensure_ascii=False))
            return 0
        if args.command == "code-sha256":
            print(compute_code_snapshot_sha256())
            return 0
        if args.command == "provider-attestation-schema":
            if args.output:
                _atomic_write_json(
                    args.output, PROVIDER_ATTESTATION_JSON_SCHEMA
                )
            print(
                json.dumps(
                    PROVIDER_ATTESTATION_JSON_SCHEMA,
                    ensure_ascii=False,
                )
            )
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
