"""证据采集：截图 / console / network（CONTRACT §4 evidence）。

产物目录：环境变量 `LOOP_ARTIFACTS_DIR` 覆盖，默认 `v2/data/artifacts`。
`screenshot_path` 的形态与契约例子一致：相对仓库根的 POSIX 路径（如
`v2/data/artifacts/exec_ab12_0.png`）；若目录被覆盖到仓库之外，则给出绝对路径。
"""

from __future__ import annotations

import os
from pathlib import Path

from playwright.async_api import Page

from .contracts import ConsoleEvent, Evidence, NetworkEvent

#: v2/worker/loop_worker/evidence.py → parents[0]=loop_worker, [1]=worker, [2]=v2, [3]=仓库根
V2_ROOT = Path(__file__).resolve().parents[2]
REPO_ROOT = V2_ROOT.parent

DEFAULT_ARTIFACTS_DIR = V2_ROOT / "data" / "artifacts"

#: 单步证据里 console / network 的条数上限（防止长跑页面把结果撑爆）
MAX_EVENTS = 500


def artifacts_dir() -> Path:
    """证据产物目录（环境变量 LOOP_ARTIFACTS_DIR 优先）。"""
    override = os.environ.get("LOOP_ARTIFACTS_DIR")
    if override:
        return Path(override).expanduser().resolve()
    return DEFAULT_ARTIFACTS_DIR


def display_path(path: Path) -> str:
    """仓库内路径 → 相对仓库根的 POSIX 路径；仓库外 → 绝对 POSIX 路径。"""
    resolved = Path(path).resolve()
    try:
        return resolved.relative_to(REPO_ROOT).as_posix()
    except ValueError:
        return resolved.as_posix()


class EvidenceCollector:
    """挂在 page 上的 console / network 采集器。

    每个步骤开始前调用 `snapshot()` 取走并清空缓冲，因此每步证据互不污染。
    """

    def __init__(self, page: Page) -> None:
        self._page = page
        self._console: list[ConsoleEvent] = []
        self._network: list[NetworkEvent] = []
        page.on("console", self._on_console)
        page.on("pageerror", self._on_pageerror)
        page.on("response", self._on_response)
        page.on("requestfailed", self._on_requestfailed)

    # ---- 事件回调（同步） ----

    def _on_console(self, message) -> None:  # noqa: ANN001 - Playwright ConsoleMessage
        if len(self._console) >= MAX_EVENTS:
            return
        try:
            self._console.append(ConsoleEvent(level=str(message.type), text=str(message.text)))
        except Exception:
            pass

    def _on_pageerror(self, error) -> None:  # noqa: ANN001 - Playwright Error
        if len(self._console) >= MAX_EVENTS:
            return
        self._console.append(ConsoleEvent(level="error", text=str(error)))

    def _on_response(self, response) -> None:  # noqa: ANN001 - Playwright Response
        if len(self._network) >= MAX_EVENTS:
            return
        try:
            self._network.append(
                NetworkEvent(
                    method=str(response.request.method),
                    url=str(response.url),
                    status=int(response.status),
                )
            )
        except Exception:
            pass

    def _on_requestfailed(self, request) -> None:  # noqa: ANN001 - Playwright Request
        if len(self._network) >= MAX_EVENTS:
            return
        try:
            self._network.append(
                NetworkEvent(method=str(request.method), url=str(request.url), status=None)
            )
        except Exception:
            pass

    # ---- 取用 ----

    def snapshot(self) -> Evidence:
        """取走并清空当前缓冲。"""
        console, network = self._console, self._network
        self._console, self._network = [], []
        return Evidence(console=list(console), network=list(network))

    async def screenshot(self, filename: str, *, full_page: bool = True) -> str | None:
        """截图并返回契约形态的路径；失败返回 None（证据缺失不应让执行崩掉）。"""
        path = artifacts_dir() / filename
        try:
            path.parent.mkdir(parents=True, exist_ok=True)
            await self._page.screenshot(path=str(path), full_page=full_page)
        except Exception:
            return None
        return display_path(path)


async def capture_screenshot(page: Page, filename: str, *, full_page: bool = True) -> str | None:
    """不持有采集器时的一次性截图。"""
    path = artifacts_dir() / filename
    try:
        path.parent.mkdir(parents=True, exist_ok=True)
        await page.screenshot(path=str(path), full_page=full_page)
    except Exception:
        return None
    return display_path(path)


__all__ = [
    "DEFAULT_ARTIFACTS_DIR",
    "MAX_EVENTS",
    "REPO_ROOT",
    "V2_ROOT",
    "EvidenceCollector",
    "artifacts_dir",
    "capture_screenshot",
    "display_path",
]
