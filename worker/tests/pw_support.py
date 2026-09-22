"""测试支撑：sys.path 装配、浏览器路径兜底、事件循环与浏览器夹具。

测试模块既可能在 `discover -s tests`（tests 目录直接进 sys.path，模块以顶层名导入）下运行，
也可能在 `discover -s tests -t .`（以 `tests.` 包导入）下运行，因此这里把 worker 根与 tests
目录都塞进 sys.path，测试里统一用顶层模块名互相导入。
"""

from __future__ import annotations

import asyncio
import contextlib
import sys
from pathlib import Path
from typing import Any, AsyncIterator, Coroutine

WORKER_ROOT = Path(__file__).resolve().parents[1]
TESTS_DIR = Path(__file__).resolve().parent

for _path in (str(WORKER_ROOT), str(TESTS_DIR)):
    if _path not in sys.path:
        sys.path.insert(0, _path)

from loop_worker.sessions import ensure_browsers_path  # noqa: E402


def run(coro: Coroutine[Any, Any, Any]) -> Any:
    """在全新事件循环里跑一个协程（每个测试方法一个循环，互不干扰）。"""
    ensure_browsers_path()
    return asyncio.run(coro)


@contextlib.asynccontextmanager
async def launched_browser() -> AsyncIterator[Any]:
    """启动一个 headless chromium，用完关掉。"""
    ensure_browsers_path()
    from playwright.async_api import async_playwright

    async with async_playwright() as playwright:
        browser = await playwright.chromium.launch(headless=True)
        try:
            yield browser
        finally:
            await browser.close()


__all__ = ["TESTS_DIR", "WORKER_ROOT", "launched_browser", "run"]
