#!/usr/bin/env python3
"""Run the complete AgentTrace Eval Hub v3 verification stack.

The runner deliberately leaves the Compose project and service logs intact. It
creates only a uniquely named PostgreSQL database for destructive Go integration
helpers, then drops that isolated database during cleanup.
"""

from __future__ import annotations

import os
import shlex
import subprocess
import sys
import uuid
from pathlib import Path
from typing import Callable, Sequence, TextIO
from urllib.parse import urlsplit


ROOT = Path(__file__).resolve().parent
NPM_CACHE = "/tmp/agenttrace-npm-cache"
TEST_DATABASE_PREFIX = "agenttrace_test_v3_runner_"
CommandExecutor = Callable[..., int]


class CommandFailed(RuntimeError):
    """A verification command returned a non-zero exit status."""


def command_text(command: Sequence[str], cwd: Path) -> str:
    """Render the actual shell-equivalent command without hiding arguments."""
    return f"(cd {shlex.quote(str(cwd))} && {shlex.join(command)})"


def run_process(command: list[str], *, cwd: Path, env: dict[str, str]) -> int:
    """Run a child process with inherited stdout and stderr for diagnosis."""
    return subprocess.run(command, cwd=cwd, env=env, check=False).returncode


class V3TestRunner:
    """Execute required v3 layers in the public acceptance order."""

    def __init__(
        self,
        *,
        root: Path,
        postgres_dsn: str,
        executor: CommandExecutor = run_process,
        output: TextIO = sys.stdout,
    ) -> None:
        self.root = root
        self.postgres_dsn = postgres_dsn
        self.executor = executor
        self.output = output

    def run_layers(self) -> None:
        """Stop at the first failing verification layer."""
        base_env = dict(os.environ)
        base_env["npm_config_cache"] = NPM_CACHE
        go_env = dict(base_env)
        # This override is intentionally unconditional: inherited DSNs must
        # never redirect reset-based integration helpers to the Compose default.
        database = isolated_database_name(self.postgres_dsn)
        go_env["AGENTTRACE_TEST_POSTGRES_DSN"] = (
            "postgres://agenttrace:agenttrace@postgres:5432/"
            f"{database}?sslmode=disable"
        )
        go_env["AGENTTRACE_TEST_KAFKA_BROKERS"] = "kafka:9092"
        go_env["AGENTTRACE_TEST_REDIS_ADDR"] = "redis:6379"
        go_env["AGENTTRACE_TEST_REDIS_DB"] = "15"

        for command, cwd, env in (
            (
                [sys.executable, "-m", "unittest", "run_v3_tests_test.py"],
                self.root,
                base_env,
            ),
            (
                [
                    "docker",
                    "compose",
                    "exec",
                    "-T",
                    "-e",
                    f"AGENTTRACE_TEST_POSTGRES_DSN={go_env['AGENTTRACE_TEST_POSTGRES_DSN']}",
                    "-e",
                    f"AGENTTRACE_TEST_KAFKA_BROKERS={go_env['AGENTTRACE_TEST_KAFKA_BROKERS']}",
                    "-e",
                    f"AGENTTRACE_TEST_REDIS_ADDR={go_env['AGENTTRACE_TEST_REDIS_ADDR']}",
                    "-e",
                    f"AGENTTRACE_TEST_REDIS_DB={go_env['AGENTTRACE_TEST_REDIS_DB']}",
                    "api",
                    "go",
                    "test",
                    "-p",
                    "1",
                    "./...",
                ],
                self.root,
                go_env,
            ),
            (
                ["uv", "run", "python", "-m", "unittest", "discover", "-s", "tests", "-t", "."],
                self.root / "worker",
                base_env,
            ),
            (
                [
                    "uv",
                    "run",
                    "python",
                    "-m",
                    "unittest",
                    "discover",
                    "-s",
                    "../integration/v3",
                    "-p",
                    "test_automation_exercise_scenario.py",
                ],
                self.root / "worker",
                base_env,
            ),
            (
                [
                    "uv",
                    "run",
                    "python",
                    "-m",
                    "unittest",
                    "discover",
                    "-s",
                    "../integration/v3",
                    "-p",
                    "test_real_provider_load_scenario.py",
                ],
                self.root / "worker",
                base_env,
            ),
            (["npm", "test", "--", "--run"], self.root / "web", base_env),
            (["npm", "run", "build"], self.root / "web", base_env),
            (
                [
                    "uv",
                    "run",
                    "--with",
                    "psycopg[binary]>=3.2,<4",
                    "python",
                    "integration/v3/canonical_scenario.py",
                ],
                self.root,
                base_env,
            ),
            (
                [
                    "uv",
                    "run",
                    "--with",
                    "psycopg[binary]>=3.2,<4",
                    "python",
                    "integration/v3/concurrency_rerun_scenario.py",
                ],
                self.root,
                base_env,
            ),
            (
                [
                    "uv",
                    "run",
                    "--with",
                    "psycopg[binary]>=3.2,<4",
                    "python",
                    "integration/v3/concurrent_delete_scenario.py",
                ],
                self.root,
                base_env,
            ),
            (
                [
                    "uv",
                    "run",
                    "--with",
                    "psycopg[binary]>=3.2,<4",
                    "--python",
                    sys.executable,
                    "python",
                    "integration/v3/failure_drills.py",
                ],
                self.root,
                base_env,
            ),
            (["npm", "run", "test:e2e"], self.root / "web", base_env),
        ):
            self._run(command, cwd, env)

    def _run(self, command: list[str], cwd: Path, env: dict[str, str]) -> None:
        rendered = command_text(command, cwd)
        print(f"+ {rendered}", file=self.output, flush=True)
        if self.executor(command, cwd=cwd, env=env) != 0:
            print(f"FAILED COMMAND: {rendered}", file=self.output, flush=True)
            raise CommandFailed(shlex.join(command))


def isolated_database_name(postgres_dsn: str) -> str:
    """Return the only database name this runner is ever allowed to drop."""
    parsed = urlsplit(postgres_dsn)
    database = parsed.path.lstrip("/")
    if not database.startswith(TEST_DATABASE_PREFIX) or len(database) <= len(
        TEST_DATABASE_PREFIX
    ):
        raise ValueError(
            "refusing to operate on non-runner PostgreSQL database "
            f"{database!r}; expected {TEST_DATABASE_PREFIX}*"
        )
    return database


def compose_psql(database: str, sql: str) -> None:
    """Run a database administration statement against only this Compose project."""
    command = [
        "docker",
        "compose",
        "exec",
        "-T",
        "postgres",
        "psql",
        "-U",
        "agenttrace",
        "-d",
        database,
        "-v",
        "ON_ERROR_STOP=1",
        "-c",
        sql,
    ]
    rendered = command_text(command, ROOT)
    print(f"+ {rendered}", flush=True)
    if run_process(command, cwd=ROOT, env=dict(os.environ)) != 0:
        print(f"FAILED COMMAND: {rendered}", file=sys.stderr, flush=True)
        raise CommandFailed(shlex.join(command))


def create_isolated_database() -> str:
    """Create a disposable database for schema-resetting Go integration tests."""
    database = f"{TEST_DATABASE_PREFIX}{uuid.uuid4().hex}"
    isolated_database_name(
        f"postgres://agenttrace:agenttrace@127.0.0.1:5433/{database}?sslmode=disable"
    )
    compose_psql("postgres", f'CREATE DATABASE "{database}"')
    return (
        "postgres://agenttrace:agenttrace@127.0.0.1:5433/"
        f"{database}?sslmode=disable"
    )


def drop_isolated_database(postgres_dsn: str) -> None:
    """Drop only a runner-created database, even if a caller passes a bad DSN."""
    database = isolated_database_name(postgres_dsn)
    compose_psql("postgres", f'DROP DATABASE "{database}" WITH (FORCE)')


def main() -> int:
    postgres_dsn = ""
    exit_code = 0
    try:
        postgres_dsn = create_isolated_database()
        V3TestRunner(root=ROOT, postgres_dsn=postgres_dsn).run_layers()
        print("v3 verification passed", flush=True)
    except (CommandFailed, ValueError) as error:
        print(f"v3 verification failed: {error}", file=sys.stderr, flush=True)
        exit_code = 1
    finally:
        if postgres_dsn:
            try:
                drop_isolated_database(postgres_dsn)
            except (CommandFailed, ValueError) as error:
                print(
                    f"v3 verification cleanup failed: {error}",
                    file=sys.stderr,
                    flush=True,
                )
                exit_code = 1
    return exit_code


if __name__ == "__main__":
    raise SystemExit(main())
