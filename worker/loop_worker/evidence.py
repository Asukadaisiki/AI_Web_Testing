"""证据采集：截图 / console / network（CONTRACT §4 evidence）。

产物根目录：环境变量 `LOOP_ARTIFACTS_DIR` 覆盖，默认 `data/sessions`。
**每个会话一个子目录**：`<产物根>/<session_id>/<文件名>`（CONTRACT §9.2）。

`screenshot_path` 的形态是**相对产物根**的 POSIX 路径，如 `sess_4d1a/exec_ab12_0.png`；
控制面把它直接拼成 `/artifacts/<screenshot_path>`。这里不写绝对路径，也不写仓库相对路径——
否则控制面得知道"产物根在仓库的哪个位置"才能拼对 URL，换个工作目录就 404。
"""

from __future__ import annotations

import os
from pathlib import Path

from playwright.async_api import Page

from .contracts import ConsoleEvent, Evidence, NetworkEvent

#: worker/loop_worker/evidence.py → parents[0]=loop_worker, [1]=worker, [2]=仓库根
REPO_ROOT = Path(__file__).resolve().parents[2]

DEFAULT_ARTIFACTS_DIR = REPO_ROOT / "data" / "sessions"

#: 单步证据里 console / network 的条数上限（防止长跑页面把结果撑爆）
MAX_EVENTS = 500


def artifacts_dir() -> Path:
    """产物**根**目录（环境变量 LOOP_ARTIFACTS_DIR 优先）。会话目录是它的子目录。"""
    override = os.environ.get("LOOP_ARTIFACTS_DIR")
    if override:
        return Path(override).expanduser().resolve()
    return DEFAULT_ARTIFACTS_DIR


def session_dir(session_id: str) -> Path:
    """某个会话的产物目录。session_id 必填——产物必须能归属到会话。"""
    if not session_id:
        raise ValueError("session_id is required to locate the artifact directory")
    return artifacts_dir() / session_id


def display_path(path: Path) -> str:
    """产物路径 → 相对**产物根**的 POSIX 路径（如 `sess_4d1a/exec_1_0.png`）。"""
    resolved = Path(path).resolve()
    try:
        return resolved.relative_to(artifacts_dir()).as_posix()
    except ValueError:
        # 产物根之外（例如被 LOOP_ARTIFACTS_DIR 指到别处时的相对路径计算失败）：
        # 报绝对路径，宁可控制面 404 也不要静默给出错误路径。
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

    async def screenshot(
        self, session_id: str, filename: str, *, full_page: bool = True
    ) -> str | None:
        """截图到 `<产物根>/<session_id>/<filename>`；失败返回 None（证据缺失不应让执行崩掉）。"""
        path = session_dir(session_id) / filename
        try:
            path.parent.mkdir(parents=True, exist_ok=True)
            await self._page.screenshot(path=str(path), full_page=full_page)
        except Exception:
            return None
        return display_path(path)


async def capture_screenshot(
    page: Page, session_id: str, filename: str, *, full_page: bool = True
) -> str | None:
    """不持有采集器时的一次性截图（同样按会话分目录）。"""
    path = session_dir(session_id) / filename
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
    "EvidenceCollector",
    "artifacts_dir",
    "capture_screenshot",
    "display_path",
    "session_dir",
]
