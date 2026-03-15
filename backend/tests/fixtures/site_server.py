"""Helpers for serving local static fixture pages during integration tests."""

from __future__ import annotations

from contextlib import contextmanager
from functools import partial
from http.server import SimpleHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from threading import Thread
from typing import Generator


FIXTURES_ROOT = Path(__file__).resolve().parent


class _QuietHandler(SimpleHTTPRequestHandler):
    def log_message(self, format: str, *args) -> None:  # noqa: A003
        return None


@contextmanager
def serve_static_fixture(directory_name: str) -> Generator[str, None, None]:
    directory = FIXTURES_ROOT / directory_name
    if not directory.is_dir():
        raise FileNotFoundError(f"Fixture directory not found: {directory}")

    handler = partial(_QuietHandler, directory=str(directory))
    server = ThreadingHTTPServer(("127.0.0.1", 0), handler)
    thread = Thread(target=server.serve_forever, daemon=True)
    thread.start()

    try:
        host, port = server.server_address
        yield f"http://{host}:{port}"
    finally:
        server.shutdown()
        server.server_close()
        thread.join(timeout=5)
