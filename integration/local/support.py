"""Shared offline adapters and bounded evidence/cleanup for managed acceptance.

HTTP is deliberately supplied only by authenticated Management, not this base.
Infrastructure administration uses its own explicitly owned SQL operations.
"""
from __future__ import annotations

import json
import math
import os
from pathlib import Path
import re
import shlex
import subprocess
import time
import urllib.parse

ROOT = Path(__file__).resolve().parents[2]
TERMINAL = {"succeeded", "failed", "cancelled"}
PG_ENV = {
    "host": "PGHOST", "hostaddr": "PGHOSTADDR", "port": "PGPORT",
    "user": "PGUSER", "password": "PGPASSWORD", "dbname": "PGDATABASE",
    "sslmode": "PGSSLMODE", "sslcert": "PGSSLCERT", "sslkey": "PGSSLKEY",
    "sslrootcert": "PGSSLROOTCERT", "sslcrl": "PGSSLCRL", "sslcrldir": "PGSSLCRLDIR",
    "ssl_min_protocol_version": "PGSSLMINPROTOCOLVERSION",
    "ssl_max_protocol_version": "PGSSLMAXPROTOCOLVERSION",
    "connect_timeout": "PGCONNECT_TIMEOUT", "options": "PGOPTIONS",
    "application_name": "PGAPPNAME", "client_encoding": "PGCLIENTENCODING",
    "target_session_attrs": "PGTARGETSESSIONATTRS", "channel_binding": "PGCHANNELBINDING",
    "gssencmode": "PGGSSENCMODE", "krbsrvname": "PGKRBSRVNAME",
    "passfile": "PGPASSFILE", "service": "PGSERVICE",
}


class CheckFailure(RuntimeError):
    pass


def require(condition: bool, message: str) -> None:
    if not condition:
        raise CheckFailure(message)


def _environment_value(value: str) -> str:
    value = value.strip()
    quote, escaped = None, False
    # Locate comments without consuming quotes/escapes inside literal JSON.
    for index, character in enumerate(value):
        if escaped:
            escaped = False
        elif character == "\\" and quote != "'":
            escaped = True
        elif quote:
            if character == quote:
                quote = None
        elif character in "'\"":
            quote = character
        elif character == "#":
            value = value[:index].rstrip()
            break
    if quote:
        raise ValueError("No closing quotation")
    if value.startswith(("'", '"')):
        return " ".join(shlex.split(value))
    return value


def load_environment(files: list[Path]) -> dict[str, str]:
    values: dict[str, str] = {}
    for path in files:
        if not path.exists():
            continue
        for line in path.read_text().splitlines():
            assignment = re.fullmatch(r"\s*(?:export\s+)?([A-Za-z_][A-Za-z0-9_]*)\s*=(.*)", line)
            if assignment:
                values[assignment[1]] = _environment_value(assignment[2])
    values.update(os.environ)
    return values


def sql_id(value: str, prefix: str = "run") -> str:
    require(bool(re.fullmatch(re.escape(prefix) + r"_[A-Za-z0-9]+", value)),
            "invalid scoped record ID")
    return "'" + value + "'"


def _libpq_parameters(dsn: str) -> dict[str, str]:
    parameters = {}
    whitespace = " \t\r\n\v\f"
    position, length = 0, len(dsn)
    message = "invalid PostgreSQL libpq connection parameters"
    require("\0" not in dsn, message)
    while position < length:
        while position < length and dsn[position] in whitespace:
            position += 1
        if position == length:
            break
        start = position
        while position < length and dsn[position] not in whitespace + "=":
            position += 1
        key = dsn[start:position]
        while position < length and dsn[position] in whitespace:
            position += 1
        require(bool(key) and position < length and dsn[position] == "=", message)
        position += 1
        while position < length and dsn[position] in whitespace:
            position += 1
        quoted = position < length and dsn[position] == "'"
        if quoted:
            position += 1
        value, closed = [], False
        # libpq escapes the next character in both quoted and unquoted values;
        # double quotes have no shell meaning.
        while position < length:
            character = dsn[position]
            if quoted and character == "'":
                position += 1
                closed = True
                break
            if not quoted and character in whitespace:
                break
            if character == "\\":
                position += 1
                if position == length:
                    break
                character = dsn[position]
            value.append(character)
            position += 1
        require(not quoted or closed, message)
        parameters[key] = "".join(value)
    return parameters


def pg_environment(dsn: str, base: dict[str, str]) -> dict[str, str]:
    parameters = {}
    if dsn.startswith(("postgres://", "postgresql://")):
        uri = urllib.parse.urlsplit(dsn)
        require(not uri.fragment, "PostgreSQL DSN fragments are unsupported")
        for key, value in (("host", uri.hostname), ("port", uri.port),
                           ("user", uri.username), ("password", uri.password),
                           ("dbname", uri.path.removeprefix("/"))):
            if value is not None and value != "":
                parameters[key] = urllib.parse.unquote(str(value))
        parameters.update(urllib.parse.parse_qsl(uri.query, keep_blank_values=True))
    else:
        parameters = _libpq_parameters(dsn)
    require(bool(parameters), "PostgreSQL connection parameters missing")
    require(all(key in PG_ENV for key in parameters), "unsupported PostgreSQL connection option")
    child = {key: value for key, value in base.items() if key not in PG_ENV.values()}
    child.update({PG_ENV[key]: value for key, value in parameters.items()})
    return child


def parse_sse(lines):
    event: dict = {}
    data: list[str] = []
    for raw in lines:
        line = raw.decode("utf-8").rstrip("\r\n") if isinstance(raw, bytes) else raw.rstrip("\r\n")
        if not line:
            if data:
                event["data"] = json.loads("\n".join(data))
                yield event
            event, data = {}, []
        elif not line.startswith(":"):
            name, _, value = line.partition(":")
            value = value.removeprefix(" ")
            if name == "data":
                data.append(value)
            elif name in {"id", "event"}:
                event[name] = value


class AcceptanceSupport:
    def __init__(self, env: dict[str, str], timeout: float):
        require(math.isfinite(timeout) and timeout > 0, "timeout must be finite and positive")
        self.env = env
        self.timeout = timeout
        self.api = env.get("AGENTTRACE_API_URL", "http://127.0.0.1:8180").rstrip("/") + "/api/v1"
        self.fixture = env.get("AGENTTRACE_E2E_GOAL_URL", "http://127.0.0.1:8124/index.html")
        self.dsn = env.get("AGENTTRACE_E2E_POSTGRES_DSN", "")
        self.created: list[str] = []

    def sql(self, query: str):
        result = subprocess.run(
            ["psql", "--no-psqlrc", "-Atq", "-v", "ON_ERROR_STOP=1", "-c", query],
            env=pg_environment(self.dsn, self.env), capture_output=True, text=True,
            timeout=min(15, self.timeout),
        )
        require(result.returncode == 0, "native PostgreSQL query failed (diagnostics suppressed)")
        return json.loads(result.stdout)

    def poll(self, callback, description: str):
        deadline = time.monotonic() + self.timeout
        while True:
            result = callback()
            if result:
                return result
            remaining = deadline - time.monotonic()
            require(remaining > 0, f"timed out: {description}")
            time.sleep(min(0.5, remaining))

    def cancel_remaining(self):
        for run_id in self.created:
            try:
                sql_id(run_id)
                run = self.http(self.api + f"/runs/{run_id}")
                if run["status"] not in TERMINAL:
                    self.http(self.api + f"/runs/{run_id}/cancel", {}, expected=202)
                    self.poll(lambda: self.http(self.api + f"/runs/{run_id}")["status"] in TERMINAL,
                              "cleanup cancellation")
            except (CheckFailure, OSError, ValueError, KeyError, subprocess.SubprocessError):
                self.report.setdefault("cleanup_incomplete", []).append(run_id)
