#!/usr/bin/env python3
"""Run AgentTrace processes against native PostgreSQL, Kafka and Redis."""

from __future__ import annotations

import argparse
import base64
import json
import os
import getpass
from pathlib import Path
import re
import secrets
import signal
import socket
import subprocess
import tempfile
import time
import urllib.request
from urllib.parse import urlparse
import uuid

from support import load_environment, pg_environment

ROOT = Path(__file__).resolve().parents[2]


def environment() -> dict[str, str]:
    env = load_environment([ROOT / ".env"])
    overrides = {
        "AGENTTRACE_POSTGRES_DSN": f"postgresql://{getpass.getuser()}@127.0.0.1:5432/agenttrace_local?sslmode=disable",
        "AGENTTRACE_KAFKA_BROKERS": "127.0.0.1:9192",
        "AGENTTRACE_REDIS_ADDR": "127.0.0.1:6379",
        "AGENTTRACE_REDIS_DB": "3",
        "AGENTTRACE_HTTP_ADDR": "127.0.0.1:8180",
        "AGENTTRACE_WORKER_URL": "http://127.0.0.1:8110",
        "AGENTTRACE_RUNTIME_CONCURRENCY": "4",
        "AGENTTRACE_KAFKA_GROUP_ID": "agenttrace-runtime-local",
        "AGENTTRACE_MODEL_PROVIDER": "openai-compatible",
        "AGENTTRACE_PI_HARNESS_ENTRY": str(ROOT / "pi-browser-harness/dist/main.js"),
        "AGENTTRACE_PI_HARNESS_DIR": str(ROOT),
        "AGENTTRACE_BUILTIN_HARNESS_DIR": str(ROOT / "pi-browser-harness/resources"),
        "AGENTTRACE_BUILTIN_MCP_ENTRY": str(ROOT / "pi-browser-harness/dist/mcp/server.js"),
    }
    for key, value in overrides.items():
        env.setdefault(key, value)
    address = env["AGENTTRACE_HTTP_ADDR"]
    api = "http://" + (address if not address.startswith(":") else "127.0.0.1" + address)
    env.setdefault("AGENTTRACE_API_URL", api)
    if env["AGENTTRACE_API_URL"].rstrip("/") != api:
        raise SystemExit("AGENTTRACE_API_URL must agree with AGENTTRACE_HTTP_ADDR")
    state = local_state(env)
    configure_management(env, state)
    state.mkdir(parents=True, exist_ok=True)
    env.setdefault("AGENTTRACE_ARTIFACT_DIR", str(state / "artifacts"))
    env["LOOP_ARTIFACTS_DIR"] = env["AGENTTRACE_ARTIFACT_DIR"]
    return env


def local_state(env: dict[str, str]) -> Path:
    return Path(env.get("AGENTTRACE_LOCAL_STATE", str(Path(tempfile.gettempdir()) / "agenttrace-local")))


def configure_management(env: dict[str, str], state: Path) -> None:
    if not env.get("AGENTTRACE_BRIDGE_TOKEN", "").strip():
        raise SystemExit("AGENTTRACE_BRIDGE_TOKEN is required")
    token = env.get("AGENTTRACE_BROWSER_PROVIDER_TOKEN", "").strip()
    if not token:
        raise SystemExit("AGENTTRACE_BROWSER_PROVIDER_TOKEN is required")
    if not env.get("AGENTTRACE_LANGFUSE_PROJECT_ID", "").strip():
        raise SystemExit("AGENTTRACE_LANGFUSE_PROJECT_ID is required for trusted startup bootstrap")
    if env.get("LOOP_WORKER_INTERNAL_TOKEN") and env["LOOP_WORKER_INTERNAL_TOKEN"] != token:
        raise SystemExit("Browser provider and worker internal tokens must match")
    env["LOOP_WORKER_INTERNAL_TOKEN"] = token
    env.setdefault("AGENTTRACE_BROWSER_ALLOWED_DOMAINS", "127.0.0.1,localhost")
    if not env.get("LOOP_ENVIRONMENTS_DIR"):
        env["LOOP_ENVIRONMENTS_DIR"] = str(state / "environments")
    if not env.get("AGENTTRACE_LANGFUSE_BASE_URL"):
        env["AGENTTRACE_LANGFUSE_BASE_URL"] = "http://127.0.0.1:3300"
    key = env.get("LOOP_ENVIRONMENT_ENCRYPTION_KEY", "")
    if not key:
        profiles = Path(env["LOOP_ENVIRONMENTS_DIR"])
        if not (state / "environment.key").exists() and profiles.exists() and any(profiles.iterdir()):
            raise SystemExit("Retained profiles require their existing environment encryption key")
        state.mkdir(parents=True, exist_ok=True)
        path = state / "environment.key"
        try:
            fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
        except FileExistsError:
            fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW)
            with os.fdopen(fd) as source:
                if os.fstat(source.fileno()).st_mode & 0o077:
                    raise SystemExit("Environment key must be private (0600)")
                key = source.read().strip()
        else:
            key = base64.urlsafe_b64encode(secrets.token_bytes(32)).decode("ascii")
            with os.fdopen(fd, "w") as target:
                target.write(key + "\n")
                target.flush()
                os.fsync(target.fileno())
    try:
        valid = len(base64.b64decode(key, altchars=b"-_", validate=True)) == 32
    except ValueError:
        valid = False
    if not valid:
        raise SystemExit("LOOP_ENVIRONMENT_ENCRYPTION_KEY must be a valid Fernet key")
    env["LOOP_ENVIRONMENT_ENCRYPTION_KEY"] = key


def service_plan(env: dict[str, str]):
    fixture_port = env.get("AGENTTRACE_LOCAL_FIXTURE_PORT", "8124")
    worker = urlparse(env.get("AGENTTRACE_WORKER_URL", "http://127.0.0.1:8110"))
    address = env.get("AGENTTRACE_HTTP_ADDR", "127.0.0.1:8180")
    api = "http://" + (address if not address.startswith(":") else "127.0.0.1" + address)
    services = [
        ("fixture", ["python3", "-m", "http.server", fixture_port, "--bind", "127.0.0.1"], ROOT / "worker/fixtures/site", f"http://127.0.0.1:{fixture_port}"),
        ("worker", ["uv", "run", "--no-sync", "uvicorn", "loop_worker.main:app", "--host", worker.hostname or "127.0.0.1", "--port", str(worker.port or 8110)], ROOT / "worker", env.get("AGENTTRACE_WORKER_URL", "http://127.0.0.1:8110").rstrip("/") + "/health"),
        ("api", ["go", "run", "./cmd/agenttrace-api"], ROOT / "backend", api + "/api/v1/health"),
        ("runtime", ["go", "run", "./cmd/agenttrace-runtime"], ROOT / "backend", None),
    ]
    return services


def kafka_topics(env: dict[str, str]) -> tuple[str, ...]:
    topics = tuple(env.get(key, default) for key, default in (
        ("AGENTTRACE_KAFKA_COMMAND_TOPIC", "agenttrace.run.commands.v1"),
        ("AGENTTRACE_KAFKA_EVENT_TOPIC", "agenttrace.run.events.v1"),
        ("AGENTTRACE_KAFKA_DLQ_TOPIC", "agenttrace.run.dlq.v1"),
    ))
    return topics + ("agenttrace.environment.commands.v1",)


def configure_kafka_topic(topic: str, env: dict[str, str]) -> None:
    broker = env.get("AGENTTRACE_KAFKA_BROKERS", "127.0.0.1:9192")
    subprocess.run(["kafka-topics", "--bootstrap-server", broker, "--create", "--if-not-exists",
                    "--topic", topic, "--partitions", "8", "--replication-factor", "1"],
                   check=True, env=env, stdout=subprocess.DEVNULL, timeout=30)
    result = subprocess.run(["kafka-topics", "--bootstrap-server", broker, "--describe",
                             "--topic", topic], check=True, env=env, capture_output=True,
                            text=True, timeout=30)
    counts = re.findall(r"\bPartitionCount:\s*(\d+)", result.stdout)
    if len(counts) != 1 or int(counts[0]) < 8:
        raise RuntimeError("Kafka topic requires at least eight partitions; an operator must review any upgrade")


def seed_management(env: dict[str, str]) -> dict:
    if not env.get("AGENTTRACE_LANGFUSE_PROJECT_ID"):
        raise SystemExit("AGENTTRACE_LANGFUSE_PROJECT_ID is required for trusted startup bootstrap")
    seeded = subprocess.run(["go", "run", "./cmd/agenttrace-management-seed"],
                            cwd=ROOT / "backend", env=env, capture_output=True, text=True,
                            timeout=90)
    if seeded.returncode:
        raise RuntimeError("management seed failed; run the seed CLI separately to check bootstrap inputs")
    try:
        result = json.loads(seeded.stdout)
        env["AGENTTRACE_PROJECT_ID"] = result["project_id"]
        env["AGENTTRACE_CONVERSATION_ID"] = result["conversation_id"]
        if env.get("AGENTTRACE_EXPORT_PROJECT_ID"):
            env["AGENTTRACE_EXPORT_PROJECT_ID"] = result["project_id"]
    except (ValueError, KeyError):
        raise RuntimeError("management seed returned invalid canonical IDs") from None
    return result


def wait_http(url: str, child: subprocess.Popen, timeout: int = 60) -> None:
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        if child.poll() is not None:
            raise RuntimeError(f"service exited before ready: {url}")
        try:
            with urllib.request.urlopen(url, timeout=2) as response:
                if response.status < 400:
                    return
        except OSError:
            time.sleep(0.2)
    raise RuntimeError(f"service did not become ready: {url}")


def postgres_environment(env: dict[str, str], dsn: str) -> dict[str, str]:
    result = pg_environment(dsn, env)
    result["PGDATABASE"] = "postgres"
    return result


def initialize_kafka(kafka_dir: Path, config: Path, env: dict[str, str]) -> None:
    # Kafka checks the supplied cluster ID even with --ignore-formatted.
    # Existing metadata owns the identity; formatting is only for a new store.
    if not (kafka_dir / "data/meta.properties").exists():
        subprocess.run(["kafka-storage", "format", "--standalone",
                        "--cluster-id", uuid.uuid4().hex[:22],
                        "--config", str(config)], check=True, env=env)


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--with-exporter", action="store_true")
    parser.add_argument("--skip-build", action="store_true")
    args = parser.parse_args()
    env = environment()
    sdk_keys = ("AGENTTRACE_MODEL_API_KEY", "AGENTTRACE_CLAUDE_API_KEY", "AGENTTRACE_CODEX_API_KEY")
    if env["AGENTTRACE_MODEL_PROVIDER"] != "scripted" and not (
            any(env.get(key) for key in sdk_keys)):
        raise SystemExit("A configured Pi, Claude Code or Codex SDK key is required for real execution")
    if args.with_exporter and not all(env.get(key) for key in (
            "AGENTTRACE_LANGFUSE_BASE_URL", "AGENTTRACE_LANGFUSE_PUBLIC_KEY",
            "AGENTTRACE_LANGFUSE_SECRET_KEY")):
        raise SystemExit("Load the Langfuse exporter environment before using --with-exporter")
    # Never mistake an unrelated listener for one of this runner's children.
    services = service_plan(env)
    for port in (urlparse(health).port for _, _, _, health in services if health):
        with socket.socket() as probe:
            try:
                probe.bind(("127.0.0.1", port))
            except OSError:
                raise RuntimeError(f"local application port {port} is already occupied") from None
    for host, port in (("127.0.0.1", 5432), ("127.0.0.1", 6379)):
        with socket.create_connection((host, port), timeout=3):
            pass
    dsn = env["AGENTTRACE_POSTGRES_DSN"]
    database = urlparse(dsn).path.strip("/")
    if database == "agenttrace_local":
        pg_env = postgres_environment(env, dsn)
        found = subprocess.run(["psql", "-Atc", "SELECT 1 FROM pg_database WHERE datname='agenttrace_local'"], env=pg_env, capture_output=True, text=True)
        if found.returncode != 0:
            raise RuntimeError("local PostgreSQL catalog check failed; verify connection settings")
        if not found.stdout.strip():
            created = subprocess.run(["createdb", database], env=pg_env, capture_output=True)
            if created.returncode != 0:
                raise RuntimeError("local application database creation failed")
    if not args.skip_build:
        subprocess.run(["npm", "run", "build"], cwd=ROOT / "pi-browser-harness", env=env, check=True)
    state = local_state(env)
    if args.with_exporter:
        env["AGENTTRACE_LANGFUSE_ENABLED"] = "true"
        services.append(("exporter", ["go", "run", "./cmd/agenttrace-exporter"], ROOT / "backend", None))
    children: list[subprocess.Popen] = []
    logs = []

    def stop(_signum=None, _frame=None):
        raise KeyboardInterrupt

    signal.signal(signal.SIGTERM, stop)
    try:
        if env["AGENTTRACE_KAFKA_BROKERS"] == "127.0.0.1:9192":
            try:
                with socket.create_connection(("127.0.0.1", 9192), timeout=1):
                    pass
            except OSError:
                kafka_dir = state / "kafka"
                kafka_dir.mkdir(exist_ok=True)
                config = kafka_dir / "server.properties"
                config.write_text("\n".join([
                    "process.roles=broker,controller", "node.id=1",
                    "controller.quorum.bootstrap.servers=127.0.0.1:9193",
                    "listeners=PLAINTEXT://127.0.0.1:9192,CONTROLLER://127.0.0.1:9193",
                    "advertised.listeners=PLAINTEXT://127.0.0.1:9192,CONTROLLER://127.0.0.1:9193",
                    "inter.broker.listener.name=PLAINTEXT", "controller.listener.names=CONTROLLER",
                    "listener.security.protocol.map=CONTROLLER:PLAINTEXT,PLAINTEXT:PLAINTEXT",
                    f"log.dirs={kafka_dir / 'data'}", "num.partitions=8",
                    "offsets.topic.replication.factor=1", "transaction.state.log.replication.factor=1",
                    "transaction.state.log.min.isr=1", "group.initial.rebalance.delay.ms=0",
                ]) + "\n")
                initialize_kafka(kafka_dir, config, env)
                log = (state / "kafka.log").open("a")
                os.chmod(state / "kafka.log", 0o600)
                logs.append(log)
                child = subprocess.Popen(["kafka-server-start", str(config)], env=env, stdout=log, stderr=subprocess.STDOUT, start_new_session=True)
                children.append(child)
                deadline = time.monotonic() + 45
                while True:
                    if child.poll() is not None or time.monotonic() > deadline:
                        raise RuntimeError("native Kafka failed to start")
                    try:
                        with socket.create_connection(("127.0.0.1", 9192), timeout=1):
                            break
                    except OSError:
                        time.sleep(.2)
        for topic in kafka_topics(env):
            configure_kafka_topic(topic, env)
        seed_management(env)
        for name, command, cwd, health in services:
            log = (state / f"{name}.log").open("a")
            os.chmod(state / f"{name}.log", 0o600)
            logs.append(log)
            child = subprocess.Popen(command, cwd=cwd, env=env, stdout=log, stderr=subprocess.STDOUT, start_new_session=True)
            children.append(child)
            if health:
                wait_http(health, child)
            print(f"{name} started; log: {state / (name + '.log')}", flush=True)
        print("Langfuse (sole UI): " + env["AGENTTRACE_LANGFUSE_BASE_URL"], flush=True)
        while all(child.poll() is None for child in children):
            time.sleep(1)
        raise RuntimeError("a service exited; inspect its local log")
    except KeyboardInterrupt:
        pass
    finally:
        for child in reversed(children):
            if child.poll() is None:
                os.killpg(child.pid, signal.SIGTERM)
        for child in children:
            try:
                child.wait(timeout=15)
            except subprocess.TimeoutExpired:
                os.killpg(child.pid, signal.SIGKILL)
                child.wait()
        for log in logs:
            log.close()


if __name__ == "__main__":
    try:
        main()
    except (OSError, RuntimeError, subprocess.SubprocessError) as error:
        # Command diagnostics may include user-supplied arguments or DSNs.
        raise SystemExit(f"Local startup failed ({type(error).__name__}); inspect private service logs") from None
