"""Run the complete ecommerce control plane offline with real Playwright."""

from __future__ import annotations

import json
import os
import signal
import socket
import subprocess
import sys
import tempfile
import time
import urllib.error
import urllib.request
from pathlib import Path
from typing import Any

ROOT = Path(__file__).resolve().parents[1]
WORKER_DIR = ROOT / "worker"
BACKEND_DIR = ROOT / "backend"
SCRIPT_PATH = ROOT / "fixtures" / "scripts" / "ecommerce_login_cart.json"
GOAL = (
    "Log in with shopper@example.test and offline-secret, open Products, search for "
    "Blue Top with the search control, open Blue Top, set quantity to 3, add it to "
    "the cart, use the added modal's View Cart link, and verify the cart row has "
    "quantity 3."
)


def free_port() -> int:
    with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as listener:
        listener.bind(("127.0.0.1", 0))
        return int(listener.getsockname()[1])


def request_json(
    base_url: str,
    path: str,
    *,
    method: str = "GET",
    body: dict[str, Any] | None = None,
    timeout: float = 5,
) -> tuple[int, dict[str, Any]]:
    payload = None if body is None else json.dumps(body).encode("utf-8")
    request = urllib.request.Request(
        base_url + path,
        data=payload,
        method=method,
        headers={"Content-Type": "application/json"},
    )
    try:
        with urllib.request.urlopen(request, timeout=timeout) as response:
            raw = response.read()
            return response.status, json.loads(raw) if raw else {}
    except urllib.error.HTTPError as error:
        raw = error.read()
        detail = json.loads(raw) if raw else {}
        raise AssertionError(
            f"{method} {path} returned {error.code}: {detail}"
        ) from error


def wait_http(
    url: str, process: subprocess.Popen[bytes], log_path: Path, timeout: float = 30
) -> None:
    deadline = time.monotonic() + timeout
    last_error: Exception | None = None
    while time.monotonic() < deadline:
        if process.poll() is not None:
            raise AssertionError(
                f"process exited with {process.returncode}: {log_path.read_text(errors='replace')}"
            )
        try:
            with urllib.request.urlopen(url, timeout=1) as response:
                if response.status == 200:
                    return
        except (OSError, urllib.error.URLError) as error:
            last_error = error
        time.sleep(0.1)
    raise AssertionError(f"timed out waiting for {url}: {last_error}")


def wait_run_status(base_url: str, run_id: str, wanted: set[str]) -> dict[str, Any]:
    deadline = time.monotonic() + 60
    last: dict[str, Any] = {}
    while time.monotonic() < deadline:
        status, last = request_json(base_url, f"/api/runs/{run_id}")
        if status != 200:
            raise AssertionError(f"run lookup returned {status}: {last}")
        if last.get("status") in wanted:
            return last
        time.sleep(0.1)
    raise AssertionError(
        f"run {run_id} did not reach {sorted(wanted)}; last response: {last}"
    )


def start_process(
    name: str,
    command: list[str],
    *,
    cwd: Path,
    env: dict[str, str],
    log_dir: Path,
) -> tuple[subprocess.Popen[bytes], Any, Path]:
    log_path = log_dir / f"{name}.log"
    log_file = log_path.open("wb")
    try:
        process = subprocess.Popen(
            command,
            cwd=cwd,
            env=env,
            stdout=log_file,
            stderr=subprocess.STDOUT,
            start_new_session=os.name != "nt",
        )
    except Exception:
        log_file.close()
        raise
    return process, log_file, log_path


def stop_process(process: subprocess.Popen[bytes]) -> None:
    if process.poll() is not None:
        return
    if os.name == "nt":
        process.terminate()
    else:
        os.killpg(process.pid, signal.SIGTERM)
    try:
        process.wait(timeout=5)
    except subprocess.TimeoutExpired:
        if os.name == "nt":
            process.kill()
        else:
            os.killpg(process.pid, signal.SIGKILL)
        process.wait(timeout=5)


def materialize_script(destination: Path, fixture_base_url: str) -> None:
    source = json.loads(SCRIPT_PATH.read_text(encoding="utf-8"))
    for step in source["steps"]:
        arguments = step.get("arguments") or {}
        url = arguments.get("url")
        if isinstance(url, str):
            arguments["url"] = url.replace(
                "http://127.0.0.1:8123", fixture_base_url
            )
    destination.write_text(
        json.dumps(source, ensure_ascii=False, indent=2) + "\n", encoding="utf-8"
    )


def main() -> int:
    ports: dict[str, int] = {}
    for name in ("fixture", "worker", "loopd"):
        port = free_port()
        while port in ports.values():
            port = free_port()
        ports[name] = port
    processes: list[tuple[subprocess.Popen[bytes], Any]] = []

    with tempfile.TemporaryDirectory(prefix="ecommerce-closed-loop-") as temp:
        temp_dir = Path(temp)
        artifacts_dir = temp_dir / "artifacts"
        data_dir = temp_dir / "data"
        script_path = temp_dir / "ecommerce_login_cart.json"
        fixture_base_url = f"http://127.0.0.1:{ports['fixture']}"
        worker_base_url = f"http://127.0.0.1:{ports['worker']}"
        loopd_base_url = f"http://127.0.0.1:{ports['loopd']}"
        materialize_script(script_path, fixture_base_url)

        common_env = dict(os.environ)
        common_env["LOOP_ARTIFACTS_DIR"] = str(artifacts_dir)
        fixture_env = dict(common_env)
        worker_env = dict(common_env)
        loopd_env = {
            **common_env,
            "LOOP_ADDR": f"127.0.0.1:{ports['loopd']}",
            "LOOP_WORKER_URL": worker_base_url,
            "LOOP_DATA_DIR": str(data_dir),
            "LOOP_DB_PATH": str(data_dir / "loop.db"),
            "LOOP_LLM_SCRIPT": str(script_path),
        }

        try:
            fixture, fixture_log, fixture_log_path = start_process(
                "fixture",
                [
                    sys.executable,
                    "-m",
                    "http.server",
                    str(ports["fixture"]),
                    "--bind",
                    "127.0.0.1",
                    "--directory",
                    "fixtures/site",
                ],
                cwd=WORKER_DIR,
                env=fixture_env,
                log_dir=temp_dir,
            )
            processes.append((fixture, fixture_log))
            wait_http(
                fixture_base_url + "/ecommerce_login.html",
                fixture,
                fixture_log_path,
            )

            worker, worker_log, worker_log_path = start_process(
                "worker",
                [
                    sys.executable,
                    "-m",
                    "uvicorn",
                    "loop_worker.main:app",
                    "--host",
                    "127.0.0.1",
                    "--port",
                    str(ports["worker"]),
                ],
                cwd=WORKER_DIR,
                env=worker_env,
                log_dir=temp_dir,
            )
            processes.append((worker, worker_log))
            wait_http(worker_base_url + "/health", worker, worker_log_path)

            loopd, loopd_log, loopd_log_path = start_process(
                "loopd",
                ["go", "run", "./cmd/loopd"],
                cwd=BACKEND_DIR,
                env=loopd_env,
                log_dir=temp_dir,
            )
            processes.append((loopd, loopd_log))
            wait_http(loopd_base_url + "/api/health", loopd, loopd_log_path)

            status, created = request_json(
                loopd_base_url, "/api/sessions", method="POST", body={"goal": GOAL}
            )
            if status != 202:
                raise AssertionError(f"create session returned {status}: {created}")
            run_id = str(created.get("run_id") or "")
            if not run_id:
                raise AssertionError(f"create session returned no run_id: {created}")

            planned = wait_run_status(
                loopd_base_url, run_id, {"awaiting_approval", "failed"}
            )
            if planned.get("status") != "awaiting_approval":
                raise AssertionError(f"planning failed: {planned}")

            status, approved = request_json(
                loopd_base_url,
                f"/api/runs/{run_id}/approve",
                method="POST",
                body={},
            )
            if status != 202:
                raise AssertionError(f"approval returned {status}: {approved}")

            completed = wait_run_status(
                loopd_base_url, run_id, {"completed", "failed"}
            )
            if completed.get("status") != "completed":
                raise AssertionError(f"execution did not complete: {completed}")

            _, report = request_json(loopd_base_url, f"/api/runs/{run_id}/report")
            if report.get("steps_failed") != 0:
                raise AssertionError(f"report has failed steps: {report}")

            _, execution = request_json(
                loopd_base_url, f"/api/runs/{run_id}/execution"
            )
            result = execution.get("result") or {}
            cart_steps = [
                step
                for step in result.get("steps", [])
                if "ecommerce_cart.html" in str(step.get("url_after", ""))
            ]
            if not cart_steps:
                raise AssertionError(f"execution has no cart step: {execution}")
            screenshot_path = (
                cart_steps[-1].get("evidence", {}).get("screenshot_path") or ""
            )
            if not screenshot_path:
                raise AssertionError(f"cart step has no screenshot: {cart_steps[-1]}")
            if not (artifacts_dir / screenshot_path).is_file():
                raise AssertionError(f"cart screenshot does not exist: {screenshot_path}")
            with urllib.request.urlopen(
                loopd_base_url + "/artifacts/" + screenshot_path, timeout=5
            ) as response:
                if response.status != 200 or not response.read():
                    raise AssertionError(
                        f"cart screenshot was not served: {response.status}"
                    )

            print(
                "ecommerce closed loop passed: "
                f"{report.get('steps_passed')} steps, cart evidence {screenshot_path}"
            )
            return 0
        finally:
            cleanup_errors: list[Exception] = []
            for process, log_file in reversed(processes):
                try:
                    stop_process(process)
                except Exception as error:
                    cleanup_errors.append(error)
                finally:
                    log_file.close()
            if cleanup_errors and sys.exc_info()[0] is None:
                details = "; ".join(str(error) for error in cleanup_errors)
                raise RuntimeError(f"failed to stop integration processes: {details}")


if __name__ == "__main__":
    raise SystemExit(main())
