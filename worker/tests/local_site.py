"""本地静态站点夹具：Python 标准库 http.server，绑定 127.0.0.1 的随机端口。

不用 `file://`：避免 file 协议与 http 协议在行为上的差异（同源、sessionStorage、
导航语义等），并且与真实执行环境一致。
"""

from __future__ import annotations

import functools
import threading
from http.server import SimpleHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path

#: 夹具目录候选：优先 v2/worker/fixtures/site，其次 v2/fixtures/site
SITE_DIR_CANDIDATES: tuple[Path, ...] = (
    Path(__file__).resolve().parents[1] / "fixtures" / "site",
    Path(__file__).resolve().parents[2] / "fixtures" / "site",
)


def site_dir() -> Path:
    for candidate in SITE_DIR_CANDIDATES:
        if candidate.is_dir():
            return candidate
    raise RuntimeError(f"no static site fixture found in {SITE_DIR_CANDIDATES}")


class _QuietHandler(SimpleHTTPRequestHandler):
    """不把每个请求打到 stderr，测试输出保持干净。"""

    def log_message(self, format: str, *args: object) -> None:  # noqa: A002
        return


class LocalSite:
    """上下文管理器：`with LocalSite() as site: site.url("index.html")`。"""

    def __init__(self, directory: Path | None = None) -> None:
        self._directory = directory or site_dir()
        self._server: ThreadingHTTPServer | None = None
        self._thread: threading.Thread | None = None

    def __enter__(self) -> LocalSite:
        handler = functools.partial(_QuietHandler, directory=str(self._directory))
        self._server = ThreadingHTTPServer(("127.0.0.1", 0), handler)
        self._thread = threading.Thread(target=self._server.serve_forever, daemon=True)
        self._thread.start()
        return self

    def __exit__(self, *exc_info: object) -> None:
        if self._server is not None:
            self._server.shutdown()
            self._server.server_close()
        if self._thread is not None:
            self._thread.join(timeout=5)

    @property
    def port(self) -> int:
        assert self._server is not None, "server is not running"
        return int(self._server.server_address[1])

    @property
    def base_url(self) -> str:
        return f"http://127.0.0.1:{self.port}"

    def url(self, path: str) -> str:
        return f"{self.base_url}/{path.lstrip('/')}"


__all__ = ["SITE_DIR_CANDIDATES", "LocalSite", "site_dir"]
